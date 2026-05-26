package library

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// statsTee should write a JSON line for `watched:<vp>` keys with a map
// value, and silently skip nulls / unrelated keys / non-map values.
func TestStatsTee(t *testing.T) {
	dir := t.TempDir()
	// statsPath is a package const; cd into tempdir so the relative path lands there.
	prev, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(prev) })
	os.Chdir(dir)
	os.MkdirAll("cache", 0755)

	statsTee("hidden:foo", true, "")                                              // ignored
	statsTee("watched:", map[string]any{"t": 1.0, "at": 1.0}, "")                 // ignored (empty vp)
	statsTee("watched:show/ep1.mp4", nil, "")                                     // ignored (cleared)
	statsTee("watched:show/ep1.mp4", "not a map", "")                             // ignored (wrong type)
	statsTee("watched:show/ep1.mp4", map[string]any{"t": 42.5, "at": 1000.0}, "") // OK
	statsTee("watched:show/ep2.mp4", map[string]any{"t": 10.0, "at": 2000.0, "dur": 1200.0}, "")

	data, err := os.ReadFile(filepath.Join(dir, "cache", "stats.jsonl"))
	if err != nil {
		t.Fatalf("stats file missing: %v", err)
	}
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines != 2 {
		t.Fatalf("expected 2 lines, got %d (data=%q)", lines, string(data))
	}
}

// Realistic ping stream: 2s cadence within a session, > 30s gaps between
// sessions. Aggregator must sum ~ session length, split sessions on gap,
// attribute hours to dir + vp, and bucket the right day in the heatmap.
func TestAggregate_TwoSessionsOneVP(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.Local)
	cutoff := now.AddDate(0, 0, -7)

	vp := "Alpha/ep1.mp4"
	base := now.Add(-2 * time.Hour).UnixMilli() // 2h ago

	// Session A: 30 pings × 2s = ~60s of watching.
	var events []statsEvent
	for i := range 30 {
		events = append(events, statsEvent{VP: vp, At: base + int64(i*2000), T: float64(60 + i*2)})
	}
	// Gap of 2 hours → new session.
	base2 := base + 2*60*60*1000
	for i := range 10 {
		events = append(events, statsEvent{VP: vp, At: base2 + int64(i*2000), T: float64(120 + i*2)})
	}

	s := aggregate(events, "week", cutoff, now)

	if s.Videos != 1 {
		t.Fatalf("videos = %d want 1", s.Videos)
	}
	if s.Sessions != 2 {
		t.Fatalf("sessions = %d want 2", s.Sessions)
	}
	// 30 pings × 2s + 10 pings × 2s = 80s = 1.33 min ≈ 0.022 h.
	// First ping of each session gets pingCapMs (3s) instead of 2s, so:
	// A = 3 + 29×2 = 61s; B = 3 + 9×2 = 21s; total = 82s ≈ 0.0228 h.
	if s.Hours < 0.020 || s.Hours > 0.025 {
		t.Fatalf("hours = %f want ~0.022", s.Hours)
	}
	if len(s.TopShows) != 1 || s.TopShows[0].Key != "Alpha" {
		t.Fatalf("topShows = %+v", s.TopShows)
	}
	if s.LongestSession < 1.0 || s.LongestSession > 1.1 {
		t.Fatalf("longest = %f min, want ~1.0", s.LongestSession)
	}
}

// A 40s ping gap WITHIN a vp should be treated as a session break (no
// double-counted span). Verifies the pingCapMs guard catches buffer/seek.
func TestAggregate_GapClampsTime(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.Local)
	cutoff := now.AddDate(0, 0, -7)
	base := now.Add(-time.Hour).UnixMilli()

	events := []statsEvent{
		{VP: "x.mp4", At: base, T: 100},
		{VP: "x.mp4", At: base + 40_000, T: 102}, // 40s gap → new session, seeded with cap
	}
	s := aggregate(events, "week", cutoff, now)
	if s.Sessions != 2 {
		t.Fatalf("sessions = %d want 2", s.Sessions)
	}
	// Each session seeded with pingCapMs (3s) → 6s total.
	if s.Hours < 0.0015 || s.Hours > 0.0020 {
		t.Fatalf("hours = %f want ~0.0017", s.Hours)
	}
}

// Property test: N distinct IPs all attributed to the same user must each
// get their own tile labelled "user #1" .. "user #N", ordered by hours
// desc (#1 = heaviest). Single-IP users skip the suffix. Empty IP buckets
// as "unknown". Driven by a loop so there's no hand-written fixture row.
func TestAggregate_PerIPLabelling(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.Local)
	cutoff := now.AddDate(0, 0, -7)
	base := now.Add(-time.Hour).UnixMilli()
	const N = 4 // N IPs on shared-user, plus one solo user, plus one unattributed

	var events []statsEvent
	// shared-user on N devices; device i gets (N-i) ping bundles so #1 wins.
	for i := range N {
		for j := 0; j < N-i; j++ {
			events = append(events, statsEvent{
				VP: "shared.mp4", At: base + int64(i*1000+j*50),
				User: "shared-user", IP: fmt.Sprintf("ip-%d", i),
			})
		}
	}
	events = append(events,
		statsEvent{VP: "solo.mp4", At: base + 999_000, User: "solo-user", IP: "ip-solo"},
		statsEvent{VP: "ghost.mp4", At: base + 998_000}, // empty IP → unknown
	)

	s := aggregate(events, "week", cutoff, now)
	if len(s.TopIPs) != N+2 {
		t.Fatalf("want %d tiles, got %d", N+2, len(s.TopIPs))
	}

	byIP := map[string]ipTile{}
	for _, tile := range s.TopIPs {
		byIP[tile.IP] = tile
	}
	for i := range N {
		want := fmt.Sprintf("shared-user #%d", i+1)
		got := byIP[fmt.Sprintf("ip-%d", i)].Label
		if got != want {
			t.Errorf("ip-%d label = %q want %q", i, got, want)
		}
	}
	if byIP["ip-solo"].Label != "solo-user" {
		t.Errorf("solo label = %q want 'solo-user'", byIP["ip-solo"].Label)
	}
	if byIP["unknown"].Label != "unknown" {
		t.Errorf("unknown label = %q", byIP["unknown"].Label)
	}
}

// Switching IPs mid-stream for the same video must split into two sessions
// on the Gantt (a "watch transfer" between devices = distinct bars).
func TestAggregate_GanttSplitsOnIPChange(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.Local)
	cutoff := now.AddDate(0, 0, -7)
	base := now.Add(-time.Hour).UnixMilli()

	mk := func(at int64, ip string) statsEvent {
		return statsEvent{VP: "v.mp4", At: at, IP: ip}
	}
	events := []statsEvent{
		mk(base, "ip-a"),
		mk(base+2000, "ip-a"),
		mk(base+4000, "ip-b"), // roam / handoff within session-gap window
		mk(base+6000, "ip-b"),
	}
	s := aggregate(events, "week", cutoff, now)
	if s.Sessions != 2 {
		t.Errorf("want 2 sessions after IP change, got %d", s.Sessions)
	}
	if len(s.Gantt) == 0 || len(s.Gantt[0].Bars) != 2 {
		t.Errorf("want 2 gantt bars on the one active day, got %+v", s.Gantt)
	}
}

// Streak detection: three consecutive days of activity, one off-day, then
// two more → longest = 3.
func TestAggregate_Streak(t *testing.T) {
	now := time.Date(2026, 5, 23, 23, 0, 0, 0, time.Local)
	cutoff := now.AddDate(0, 0, -30)

	mk := func(day int) statsEvent {
		ts := time.Date(2026, 5, day, 20, 0, 0, 0, time.Local).UnixMilli()
		return statsEvent{VP: "v.mp4", At: ts, T: 100}
	}
	events := []statsEvent{mk(10), mk(11), mk(12), mk(15), mk(16)}
	s := aggregate(events, "month", cutoff, now)
	if s.StreakDays != 3 {
		t.Fatalf("streak = %d want 3", s.StreakDays)
	}
	if s.ActiveDays != 5 {
		t.Fatalf("active = %d want 5", s.ActiveDays)
	}
}

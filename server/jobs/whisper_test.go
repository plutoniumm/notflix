package jobs

import (
	"os/exec"
	"strings"
	"testing"
)

// Bug: A's deferred cleanup deleted by key, dropping B's entry, so the next
// run killed nothing and started a second python racing for the same .vtt.
func TestProcRegistryComparesIdentity(t *testing.T) {
	key := "Show.S01E01.mkv"
	a, b := &exec.Cmd{}, &exec.Cmd{}

	procSwap(key, a)
	procSwap(key, b)

	procDone(key, a)
	cur, ok := activeProcs.Load(key)
	if !ok {
		t.Fatal("a finishing run evicted the live one")
	}
	if cur.(*exec.Cmd) != b {
		t.Fatal("registered run is not the latest one")
	}

	procDone(key, b)
	if _, ok := activeProcs.Load(key); ok {
		t.Fatal("the owning run failed to unregister")
	}
}

func TestProcKillKeepsEntry(t *testing.T) {
	key := "Movie.mkv"
	cmd := &exec.Cmd{}
	procSwap(key, cmd)
	t.Cleanup(func() { procDone(key, cmd) })

	// The entry belongs to the run that stored it, so its procDone still matches.
	procKill(key)
	if _, ok := activeProcs.Load(key); !ok {
		t.Fatal("procKill removed the entry")
	}
}

// Whisper stretches End to the next speech chunk: City Hunter ep05 shipped 17
// cues over 15s, one of them a single syllable held 24.8s.
func TestWhisperVTTClampsCueDuration(t *testing.T) {
	got := string(whisperVTT([]whisperSeg{
		{Start: 264.8, End: 289.6, Text: "ね"},         // 24.8s for one syllable
		{Start: 292.0, End: 295.0, Text: "fine"},      // untouched
		{Start: 300.0, End: 340.0, Text: "next soon"}, // clamped by the following cue
		{Start: 303.0, End: 304.0, Text: "after"},
		{Start: 400.0, End: 400.0, Text: "zero len"},
	}))

	for _, want := range []string{
		"00:04:24.800 --> 00:04:32.800", // 24.8s -> 8s cap
		"00:04:52.000 --> 00:04:55.000", // short cue unchanged
		"00:05:00.000 --> 00:05:03.000", // capped to the next cue's start, not +8s
		"00:06:40.000 --> 00:06:40.500", // zero-length gets a floor
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "00:04:49.600") {
		t.Error("unclamped 24.8s end still present")
	}
}

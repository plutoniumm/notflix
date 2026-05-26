package library

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ipInfo is the per-IP rollup inside aggregate. Package-scoped so it can be
// passed to buildIPTiles without an awkward anonymous struct.
type ipInfo struct {
	user  string
	host  string
	hours float64
	vps   map[string]bool
}

// rawSession is one continuous-playback span emitted by aggregate's flush
// path; buildGantt slices these into per-day bars.
type rawSession struct {
	ip, user, vp   string
	startAt, endAt int64
}

// stats.go writes a tiny append-only log of every watch save and aggregates
// it on demand for the Wrapped-style /api/stats/summary endpoint. No new
// frontend POST: it piggybacks on the existing `watched:<vp>` KV writes via
// statsTee, called from KVSet for any payload that smells like a watch
// progress record ({t, at} optionally with dur).

const statsPath = "./cache/stats.jsonl"

// Per-ping the player records 2s of wall-clock time, so cap each ping's
// counted seconds at this when summing watch time. Larger gaps look like
// pause/seek/buffering and split sessions.
const pingCapMs = 3000

// More than this between two pings for the same video → call it a new
// session for streak / longest-binge accounting.
const sessionGapMs = 30_000

// Device is the render-ready identity attached to each watch ping when
// IdentityFromIP can resolve the client IP. Mirrors jobs.Identity field-
// for-field; declared separately here so the library package stays free
// of any `jobs` import (jobs imports library, not the other way around —
// same pattern as jobs.OnDownloads).
type Device struct {
	User   string   `json:"user"`
	Device string   `json:"device"`
	Host   string   `json:"host"`
	OS     string   `json:"os"`
	Login  string   `json:"login"`
	Tags   []string `json:"tags"`
	Online bool     `json:"online"`
}

// IdentityFromIP, when set, maps a client IP to a rich Device. Wired in
// main.go to jobs.TailscaleIdentity; stays nil under tests so the path
// degrades to the empty Device.
var IdentityFromIP func(ip string) Device

type statsEvent struct {
	VP   string  `json:"vp"`
	T    float64 `json:"t,omitempty"`
	Dur  float64 `json:"dur,omitempty"`
	At   int64   `json:"at"`
	User string  `json:"user,omitempty"` // Tailscale display name; "" if unresolved
	Host string  `json:"host,omitempty"` // device hostname; for tooltip / disambiguation
	IP   string  `json:"ip,omitempty"`   // client IP — the canonical per-device key
}

var statsMu sync.Mutex

// statsTee is called from KVSet for every {key, value} that looks like a
// `watched:<vp>` write. It silently no-ops on anything it can't parse — the
// stats log is best-effort.
//
// clientIP is the request's remote address (gin's c.ClientIP()), used to
// resolve a Tailscale identity via IdentityFromIP. Pass "" to skip.
func statsTee(key string, value any, clientIP string) {
	if !strings.HasPrefix(key, "watched:") {
		return
	}
	vp := strings.TrimPrefix(key, "watched:")
	if vp == "" {
		return
	}
	m, ok := value.(map[string]any)
	if !ok || m == nil {
		return // null = "clear this entry"; nothing to log
	}
	t, _ := m["t"].(float64)
	at, _ := m["at"].(float64)
	dur, _ := m["dur"].(float64)
	if at <= 0 {
		at = float64(time.Now().UnixMilli())
	}

	var user, host string
	if IdentityFromIP != nil && clientIP != "" {
		d := IdentityFromIP(clientIP)
		user, host = d.User, d.Host
	}

	ev := statsEvent{VP: vp, T: t, Dur: dur, At: int64(at), User: user, Host: host, IP: clientIP}
	line, err := json.Marshal(ev)
	if err != nil {
		return
	}

	statsMu.Lock()
	defer statsMu.Unlock()
	f, err := os.OpenFile(statsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(line)
	f.Write([]byte("\n"))
}

func readStatsSince(cutoffMs int64) []statsEvent {
	statsMu.Lock()
	defer statsMu.Unlock()
	f, err := os.Open(statsPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []statsEvent
	sc := bufio.NewScanner(f)
	// Some lines can be long-ish (paths can be deep); bump the buffer.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var ev statsEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue
		}
		if ev.At < cutoffMs {
			continue
		}
		out = append(out, ev)
	}
	return out
}

type topItem struct {
	Key   string  `json:"key"`
	Hours float64 `json:"hours"`
}

// ipTile = per-IP row rendered as a chip. Each distinct client IP gets one
// tile. Device / OS / Login / Tags come from the live Tailscale peer
// table (resolved at read time, not stored in the event), so renaming a
// device or moving it under a different user shows up immediately.
type ipTile struct {
	IP     string   `json:"ip"`
	User   string   `json:"user"`
	Device string   `json:"device,omitempty"`
	Host   string   `json:"host,omitempty"`
	OS     string   `json:"os,omitempty"`
	Login  string   `json:"login,omitempty"`
	Tags   []string `json:"tags,omitempty"`
	Online bool     `json:"online,omitempty"`
	Label  string   `json:"label"`
	Hours  float64  `json:"hours"`
	Videos int      `json:"videos"`
}

// dayBars is one calendar day's worth of session bars for the Gantt chart.
// Date is "YYYY-MM-DD" in server-local time; bars are sorted by StartHour.
// Sessions that cross midnight get split into two bars by the aggregator,
// one on each day, so each bar always falls within [0, 24).
type sessionBar struct {
	IP        string  `json:"ip"`
	User      string  `json:"user"`
	Label     string  `json:"label"`
	VP        string  `json:"vp"`
	StartHour float64 `json:"startHour"` // 0..24
	DurHours  float64 `json:"durHours"`
}
type dayBars struct {
	Date string       `json:"date"`
	Bars []sessionBar `json:"bars"`
}

// userHeatmap is a per-user 7×24 grid where each cell is coloured by which
// device dominated that time slot. Cells holds the dominant IP per cell
// ("" if no activity), Intensity holds the raw ping count for the alpha
// channel. Rendered as one grid per user on the frontend.
type userHeatmap struct {
	User      string       `json:"user"`
	IPs       []ipTile     `json:"ips"`       // this user's devices, hours desc
	Cells     [7][24]string `json:"cells"`    // dominant IP per (dow, hour); "" = empty
	Intensity [7][24]int    `json:"intensity"` // total pings per cell
}

type StatsSummary struct {
	Range          string        `json:"range"`
	Hours          float64       `json:"hours"`
	Videos         int           `json:"videos"`
	Sessions       int           `json:"sessions"`
	LongestSession float64       `json:"longestSessionMin"`
	StreakDays     int           `json:"streakDays"`
	ActiveDays     int           `json:"activeDays"`
	TopShows       []topItem     `json:"topShows"`
	DailyMinutes   []float64     `json:"dailyMinutes"` // per-day minutes, oldest first
	FirstAt        int64         `json:"firstAt"`
	LastAt         int64         `json:"lastAt"`
	TopIPs         []ipTile      `json:"topIPs"`               // all distinct IPs in window, hours desc
	Gantt          []dayBars     `json:"gantt"`                // most recent 30 days, newest first
	UserHeatmaps   []userHeatmap `json:"userHeatmaps"`         // one 7×24 grid per user, cells coloured by dominant device
	FilteredIP     string        `json:"filteredIP,omitempty"` // echoed when ?ip= narrowed the slice
}

// StatsSummaryHandler aggregates the log into Wrapped-style numbers.
// Query:
//   ?range=week|month|year|all  (default year)
//   ?ip=<addr>                  — narrow to one client IP; topIPs still
//                                 reflects everyone in the window so the
//                                 chips stay clickable.
func StatsSummaryHandler(c *gin.Context) {
	rng := c.DefaultQuery("range", "year")
	ip := c.Query("ip")
	now := time.Now()
	var cutoff time.Time
	switch rng {
	case "week":
		cutoff = now.AddDate(0, 0, -7)
	case "month":
		cutoff = now.AddDate(0, -1, 0)
	case "year":
		cutoff = now.AddDate(-1, 0, 0)
	case "all":
		cutoff = time.Unix(0, 0)
	default:
		cutoff = now.AddDate(-1, 0, 0)
		rng = "year"
	}

	events := readStatsSince(cutoff.UnixMilli())

	// Always compute the full-window IP breakdown so the chips show every
	// device even when one is selected. Filtering happens after.
	all := aggregate(events, rng, cutoff, now)
	if ip == "" {
		c.JSON(http.StatusOK, all)
		return
	}
	filtered := make([]statsEvent, 0, len(events))
	for _, e := range events {
		if matchesIP(e, ip) {
			filtered = append(filtered, e)
		}
	}
	out := aggregate(filtered, rng, cutoff, now)
	out.TopIPs = all.TopIPs // preserve the chip list
	out.FilteredIP = ip
	c.JSON(http.StatusOK, out)
}

// matchesIP handles the synthetic "no-ip-recorded" bucket: ?ip=unknown
// matches every event with an empty IP field (those came from pre-feature
// pings or hosts where c.ClientIP() came back blank).
func matchesIP(e statsEvent, want string) bool {
	if want == "unknown" {
		return e.IP == ""
	}
	return e.IP == want
}

// aggregate walks the event stream (ungrouped) and produces a StatsSummary.
// Split out for testability — no I/O.
func aggregate(events []statsEvent, rng string, cutoff, now time.Time) StatsSummary {
	sum := StatsSummary{Range: rng, TopShows: []topItem{}, TopIPs: []ipTile{}, Gantt: []dayBars{}, UserHeatmaps: []userHeatmap{}}
	if len(events) == 0 {
		days := max(int(now.Sub(cutoff).Hours()/24)+1, 1)
		sum.DailyMinutes = make([]float64, days)
		return sum
	}

	// Per-IP device card: filled from the live Tailscale table at read
	// time. Caching the lookup once per IP (instead of per-ping) makes the
	// hot loop a single map read.
	deviceByIP := map[string]Device{}
	if IdentityFromIP != nil {
		seen := map[string]bool{}
		for _, e := range events {
			if e.IP == "" || seen[e.IP] {
				continue
			}
			seen[e.IP] = true
			deviceByIP[e.IP] = IdentityFromIP(e.IP)
		}
	}
	// Re-resolve user/host from the live Tailscale peer table for every
	// event that has an IP. A fallback-ladder change retroactively fixes
	// every entry with an IP, instead of leaving a stale snapshot. Events
	// with no IP keep their stored user/host as the best-we-can-do.
	for i := range events {
		if events[i].IP == "" {
			continue
		}
		if d, ok := deviceByIP[events[i].IP]; ok && d.User != "" {
			events[i].User, events[i].Host = d.User, d.Host
		}
	}

	// Group by vp, sort each by `at`, sum watched-time using pingCapMs +
	// session detection.
	byVP := map[string][]statsEvent{}
	for _, e := range events {
		byVP[e.VP] = append(byVP[e.VP], e)
	}

	// Per-IP buckets. The synthetic "unknown" bucket holds pings whose IP
	// field was never populated (pre-feature entries or hosts where
	// c.ClientIP() came back blank).
	ipMap := map[string]*ipInfo{}
	ipKey := func(ip string) string {
		if ip == "" {
			return "unknown"
		}
		return ip
	}
	addIPHours := func(ip, user, host, vp string, hours float64) {
		key := ipKey(ip)
		v := ipMap[key]
		if v == nil {
			v = &ipInfo{vps: map[string]bool{}}
			ipMap[key] = v
		}
		// Last non-empty user/host wins — handles a peer renaming or
		// moving between tags within the window without dropping the row.
		if user != "" {
			v.user = user
		}
		if host != "" {
			v.host = host
		}
		v.hours += hours
		v.vps[vp] = true
	}

	// Resolve the *canonical* user-per-IP up front (one pass). Then the
	// main loop buckets heatmap cells under that canonical user instead of
	// the per-event stored value. Without this, an IP that ever appeared
	// with an empty user (legacy pings) would spawn an orphan "unknown"
	// heatmap alongside the real one — visible as a ghost row with no
	// devices in it.
	canonicalUserForIP := map[string]string{}
	for _, e := range events {
		if e.User != "" {
			canonicalUserForIP[ipKey(e.IP)] = e.User
		}
	}

	hoursPerDir := map[string]float64{}
	dayHas := map[string]bool{}
	totalMs := int64(0)
	sessions := 0
	longestSessionMs := int64(0)

	// Per-user × (dow, hour, ip) ping counts. At emit time we pick the
	// dominant ip per cell and the total to produce a userHeatmap each.
	// Bumps use the canonical user for the IP — see canonicalUserForIP.
	heatCells := map[string][7][24]map[string]int{}
	bumpCell := func(ip string, dow, hour int) {
		uk := canonicalUserForIP[ipKey(ip)]
		if uk == "" {
			uk = "unknown"
		}
		grid, ok := heatCells[uk]
		if !ok {
			grid = [7][24]map[string]int{}
			heatCells[uk] = grid
		}
		cell := grid[dow][hour]
		if cell == nil {
			cell = map[string]int{}
			grid[dow][hour] = cell
			heatCells[uk] = grid
		}
		cell[ipKey(ip)]++
	}

	// Per-day session bars for the Gantt chart. We collect (ip, user, vp,
	// start_at, end_at) tuples as sessions close, then split midnight-
	// crossing spans below before emitting the dayBars slice.
	var rawSessions []rawSession

	cutoffDay := startOfDay(cutoff)
	totalDays := max(int(startOfDay(now).Sub(cutoffDay).Hours()/24)+1, 1)
	dailyMs := make([]int64, totalDays)

	sum.FirstAt = events[0].At
	sum.LastAt = events[0].At

	for vp, evs := range byVP {
		sort.Slice(evs, func(i, j int) bool { return evs[i].At < evs[j].At })

		// Walk pairs; first ping seeds a session with pingCapMs. A session
		// is bound to the (vp, ip) pair — if you switch devices mid-movie
		// it counts as two sessions (visible side-by-side on the Gantt).
		var sessionStart, sessionLast, sessionMs int64
		var sessionIP, sessionUser string
		flushSession := func() {
			if sessionMs > 0 {
				sessions++
				if sessionMs > longestSessionMs {
					longestSessionMs = sessionMs
				}
				rawSessions = append(rawSessions, rawSession{
					ip: sessionIP, user: sessionUser, vp: vp,
					startAt: sessionStart, endAt: sessionLast,
				})
			}
			sessionStart, sessionLast, sessionMs = 0, 0, 0
			sessionIP, sessionUser = "", ""
		}

		var prev *statsEvent
		for i := range evs {
			e := &evs[i]
			if e.At < sum.FirstAt {
				sum.FirstAt = e.At
			}
			if e.At > sum.LastAt {
				sum.LastAt = e.At
			}

			ipChanged := prev != nil && prev.IP != e.IP
			var addMs int64
			if prev == nil || e.At-prev.At > sessionGapMs || ipChanged {
				flushSession()
				sessionStart = e.At
				sessionIP, sessionUser = e.IP, e.User
				addMs = pingCapMs
			} else {
				addMs = min(e.At-prev.At, pingCapMs)
			}
			sessionMs += addMs
			sessionLast = e.At
			totalMs += addMs
			hours := float64(addMs) / 3600_000.0
			hoursPerDir[dirOf(vp)] += hours
			addIPHours(e.IP, e.User, e.Host, vp, hours)

			tLocal := time.UnixMilli(e.At).Local()
			bumpCell(e.IP, int(tLocal.Weekday()), tLocal.Hour())

			dayKey := tLocal.Format("2006-01-02")
			dayHas[dayKey] = true
			dayIdx := int(startOfDay(tLocal).Sub(cutoffDay).Hours() / 24)
			if dayIdx >= 0 && dayIdx < totalDays {
				dailyMs[dayIdx] += addMs
			}
			prev = e
		}
		flushSession()
	}

	sum.Hours = float64(totalMs) / 3600_000.0
	sum.Videos = len(byVP)
	sum.Sessions = sessions
	sum.LongestSession = float64(longestSessionMs) / 60_000.0
	sum.ActiveDays = len(dayHas)
	sum.StreakDays = longestStreak(dayHas)

	sum.DailyMinutes = make([]float64, totalDays)
	for i, ms := range dailyMs {
		sum.DailyMinutes[i] = float64(ms) / 60_000.0
	}

	sum.TopShows = topN(hoursPerDir, 8)
	sum.TopIPs = buildIPTiles(ipMap, deviceByIP)
	sum.Gantt = buildGantt(rawSessions, sum.TopIPs)
	sum.UserHeatmaps = buildUserHeatmaps(heatCells, sum.TopIPs)
	return sum
}

// buildUserHeatmaps emits one userHeatmap per user with activity, ordered
// by total hours desc so the heaviest user appears first. Each cell's
// dominant IP is the one with the most pings in that slot; ties broken
// arbitrarily but deterministically (first IP iterated). The full per-user
// IP tile list is included so the frontend can render a legend without
// re-joining against TopIPs.
//
// IPs is guaranteed non-nil even when the user has no resolved devices in
// the window (e.g. legacy pings without an IP field) — the frontend
// {#each} would throw on a JSON null.
func buildUserHeatmaps(cells map[string][7][24]map[string]int, allIPs []ipTile) []userHeatmap {
	ipsByUser := map[string][]ipTile{}
	for _, t := range allIPs {
		ipsByUser[t.User] = append(ipsByUser[t.User], t)
	}

	out := make([]userHeatmap, 0, len(cells))
	for user, grid := range cells {
		ips := ipsByUser[user]
		if ips == nil {
			ips = []ipTile{}
		}
		uh := userHeatmap{User: user, IPs: ips}
		for dow := range 7 {
			for hour := range 24 {
				cell := grid[dow][hour]
				if len(cell) == 0 {
					continue
				}
				var dom string
				best := -1
				total := 0
				for ip, n := range cell {
					total += n
					if n > best {
						best, dom = n, ip
					}
				}
				uh.Cells[dow][hour] = dom
				uh.Intensity[dow][hour] = total
			}
		}
		out = append(out, uh)
	}
	// Heaviest user first — sum of tile hours is a cheap proxy.
	sort.Slice(out, func(i, j int) bool {
		var hi, hj float64
		for _, t := range out[i].IPs {
			hi += t.Hours
		}
		for _, t := range out[j].IPs {
			hj += t.Hours
		}
		return hi > hj
	})
	return out
}

// buildIPTiles emits one tile per distinct IP. Each tile is enriched from
// the live Tailscale device table when an entry exists for the IP; for
// pre-IP historical events (deviceByIP empty for that IP), it falls back
// to the stored user/host snapshot in ipInfo. Multi-device users get
// "<user> #N" labels; single-device users keep just "<user>".
func buildIPTiles(ipMap map[string]*ipInfo, deviceByIP map[string]Device) []ipTile {
	tiles := make([]ipTile, 0, len(ipMap))
	for ip, v := range ipMap {
		t := ipTile{IP: ip, Hours: v.hours, Videos: len(v.vps)}
		if d, ok := deviceByIP[ip]; ok && d.User != "" {
			t.User, t.Device, t.Host = d.User, d.Device, d.Host
			t.OS, t.Login, t.Tags, t.Online = d.OS, d.Login, d.Tags, d.Online
		} else {
			t.User = v.user
			t.Host = v.host
		}
		if t.User == "" {
			t.User = "unknown"
		}
		tiles = append(tiles, t)
	}
	sort.Slice(tiles, func(i, j int) bool { return tiles[i].Hours > tiles[j].Hours })

	// Number IPs per user. Single-IP users get no "#N" suffix; prefer the
	// MagicDNS device name as the suffix when available so two devices for
	// one user become e.g. "Nilesh Sharma · admin-vqkeuu90r" instead of
	// "Nilesh Sharma #1".
	count := map[string]int{}
	for _, t := range tiles {
		count[t.User]++
	}
	seen := map[string]int{}
	for i := range tiles {
		u := tiles[i].User
		seen[u]++
		if count[u] == 1 {
			tiles[i].Label = u
		} else if tiles[i].Device != "" && tiles[i].Device != u {
			tiles[i].Label = u + " · " + tiles[i].Device
		} else {
			tiles[i].Label = u + " #" + strconv.Itoa(seen[u])
		}
	}
	return tiles
}

// buildGantt slices each raw session into per-day bars (splitting any span
// that crosses midnight) and returns the most recent 30 days, newest first.
// label is plucked from the matching ipTile so the frontend doesn't need
// to redo the IP→label mapping. Empty days are skipped to keep the chart
// dense — caller renders only what comes back.
const ganttMaxDays = 30

func buildGantt(raw []rawSession, tiles []ipTile) []dayBars {
	if len(raw) == 0 {
		return []dayBars{}
	}
	labelByIP := make(map[string]string, len(tiles))
	userByIP := make(map[string]string, len(tiles))
	for _, t := range tiles {
		labelByIP[t.IP] = t.Label
		userByIP[t.IP] = t.User
	}

	byDay := map[string][]sessionBar{}
	for _, s := range raw {
		segs := splitByLocalDay(s.startAt, s.endAt)
		for _, sg := range segs {
			date := sg.dayKey
			startHour := float64(sg.startMs%(24*3600_000)) / 3600_000.0
			durHours := float64(sg.endMs-sg.startMs) / 3600_000.0
			byDay[date] = append(byDay[date], sessionBar{
				IP:        s.ip,
				User:      userByIP[s.ip],
				Label:     labelByIP[s.ip],
				VP:        s.vp,
				StartHour: startHour,
				DurHours:  durHours,
			})
		}
	}

	dates := make([]string, 0, len(byDay))
	for d := range byDay {
		dates = append(dates, d)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates))) // newest first
	if len(dates) > ganttMaxDays {
		dates = dates[:ganttMaxDays]
	}
	out := make([]dayBars, 0, len(dates))
	for _, d := range dates {
		bars := byDay[d]
		sort.Slice(bars, func(i, j int) bool { return bars[i].StartHour < bars[j].StartHour })
		out = append(out, dayBars{Date: d, Bars: bars})
	}
	return out
}

// splitByLocalDay walks a [startAt, endAt] span in server-local time and
// returns one segment per calendar day it touches. Each segment carries the
// "YYYY-MM-DD" key plus the ms offsets from local-midnight of day 0; the
// caller derives startHour by `startMs % day` so the segment is normalised
// against its own day even when the span spans the year.
type ganttSeg struct {
	dayKey         string
	startMs, endMs int64 // ms since local midnight of dayKey
}

func splitByLocalDay(startAt, endAt int64) []ganttSeg {
	if endAt <= startAt {
		return nil
	}
	var out []ganttSeg
	cur := time.UnixMilli(startAt).Local()
	end := time.UnixMilli(endAt).Local()

	for {
		dayKey := cur.Format("2006-01-02")
		dayStart := startOfDay(cur)
		nextDay := dayStart.AddDate(0, 0, 1)
		segEnd := end
		if segEnd.After(nextDay) {
			segEnd = nextDay
		}
		out = append(out, ganttSeg{
			dayKey:  dayKey,
			startMs: cur.UnixMilli() - dayStart.UnixMilli(),
			endMs:   segEnd.UnixMilli() - dayStart.UnixMilli(),
		})
		if !segEnd.Before(end) {
			break
		}
		cur = nextDay
	}
	return out
}

func dirOf(vp string) string {
	d := path.Dir(vp)
	if d == "" || d == "." {
		return "."
	}
	return d
}

func topN(m map[string]float64, n int) []topItem {
	out := make([]topItem, 0, len(m))
	for k, v := range m {
		out = append(out, topItem{Key: k, Hours: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hours > out[j].Hours })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func longestStreak(days map[string]bool) int {
	if len(days) == 0 {
		return 0
	}
	dates := make([]time.Time, 0, len(days))
	for k := range days {
		if t, err := time.Parse("2006-01-02", k); err == nil {
			dates = append(dates, t)
		}
	}
	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })

	best, cur := 1, 1
	for i := 1; i < len(dates); i++ {
		if dates[i].Sub(dates[i-1]).Hours() == 24 {
			cur++
			if cur > best {
				best = cur
			}
		} else {
			cur = 1
		}
	}
	return best
}

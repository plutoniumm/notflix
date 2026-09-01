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

type ipInfo struct {
	user  string
	host  string
	hours float64
	vps   map[string]bool
}

// rawSession is one continuous-playback span; buildGantt slices it per day.
type rawSession struct {
	ip, user, vp   string
	startAt, endAt int64
}

// An append-only log of every watch save, aggregated on demand. There is no
// frontend POST — statsTee piggybacks on the watched:<vp> KV writes.

const statsPath = "./cache/stats.jsonl"

// The player pings every 2s, so cap each ping's counted seconds here; larger
// gaps are pause/seek/buffering and split the session.
const pingCapMs = 3000

// A gap larger than this between two pings for one video starts a new session.
const sessionGapMs = 30_000

// Device mirrors jobs.Identity field-for-field, declared here so library never
// imports jobs (jobs imports library, not the other way around).
type Device struct {
	User   string   `json:"user"`
	Device string   `json:"device"`
	Host   string   `json:"host"`
	OS     string   `json:"os"`
	Login  string   `json:"login"`
	Tags   []string `json:"tags"`
	Online bool     `json:"online"`
}

// IdentityFromIP is wired in main.go to jobs.TailscaleIdentity; nil in tests.
var IdentityFromIP func(ip string) Device

type statsEvent struct {
	VP   string  `json:"vp"`
	T    float64 `json:"t,omitempty"`
	Dur  float64 `json:"dur,omitempty"`
	At   int64   `json:"at"`
	User string  `json:"user,omitempty"`
	Host string  `json:"host,omitempty"`
	IP   string  `json:"ip,omitempty"`
}

var statsMu sync.Mutex

// statsTee logs every watched:<vp> KV write and silently no-ops on anything it
// can't parse — the log is best-effort. An empty clientIP skips identity.
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
		return // null clears the entry
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
	// Paths can be deep; bump the scanner buffer.
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

// ipTile is one per-IP chip. Device/OS/Login/Tags come from the live Tailscale
// table at read time, so a renamed device shows up immediately.
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

// A session crossing midnight is split into one bar per day by the aggregator,
// so every bar falls within [0, 24).
type sessionBar struct {
	IP        string  `json:"ip"`
	User      string  `json:"user"`
	Label     string  `json:"label"`
	VP        string  `json:"vp"`
	StartHour float64 `json:"startHour"`
	DurHours  float64 `json:"durHours"`
}
type dayBars struct {
	Date string       `json:"date"`
	Bars []sessionBar `json:"bars"`
}

// userHeatmap is a per-user 7×24 grid; Cells holds the dominant device IP per
// slot ("" = no activity), Intensity the raw ping count for the alpha channel.
type userHeatmap struct {
	User      string        `json:"user"`
	IPs       []ipTile      `json:"ips"`
	Cells     [7][24]string `json:"cells"`
	Intensity [7][24]int    `json:"intensity"`
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

// StatsSummaryHandler: ?range=week|month|year|all (default year), ?ip=<addr>
// narrows to one IP while topIPs still lists everyone so chips stay clickable.
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

	// Full-window breakdown first, so the chips still list every device when
	// one is selected.
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
	out.TopIPs = all.TopIPs
	out.FilteredIP = ip
	c.JSON(http.StatusOK, out)
}

// ?ip=unknown is the synthetic bucket: every event with no IP recorded.
func matchesIP(e statsEvent, want string) bool {
	if want == "unknown" {
		return e.IP == ""
	}
	return e.IP == want
}

// aggregate does no I/O — it stays pure so it can be tested directly.
func aggregate(events []statsEvent, rng string, cutoff, now time.Time) StatsSummary {
	sum := StatsSummary{Range: rng, TopShows: []topItem{}, TopIPs: []ipTile{}, Gantt: []dayBars{}, UserHeatmaps: []userHeatmap{}}
	if len(events) == 0 {
		days := max(int(now.Sub(cutoff).Hours()/24)+1, 1)
		sum.DailyMinutes = make([]float64, days)
		return sum
	}

	// Resolve each IP once, not per ping.
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
	// Re-resolve from the live peer table so a ladder change retroactively fixes
	// old events; entries with no IP keep their stored user/host.
	for i := range events {
		if events[i].IP == "" {
			continue
		}
		if d, ok := deviceByIP[events[i].IP]; ok && d.User != "" {
			events[i].User, events[i].Host = d.User, d.Host
		}
	}

	byVP := map[string][]statsEvent{}
	for _, e := range events {
		byVP[e.VP] = append(byVP[e.VP], e)
	}

	// The synthetic "unknown" bucket holds pings whose IP was never populated.
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
		// Last non-empty wins, so a rename mid-window doesn't drop the row.
		if user != "" {
			v.user = user
		}
		if host != "" {
			v.host = host
		}
		v.hours += hours
		v.vps[vp] = true
	}

	// Canonical user per IP, up front: an IP that ever appeared with an empty
	// user would otherwise spawn a ghost "unknown" heatmap beside the real one.
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

	// Per-user × (dow, hour, ip) ping counts; emit picks the dominant ip per cell.
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

	// Sessions are collected as they close, then split at midnight below.
	var rawSessions []rawSession

	cutoffDay := startOfDay(cutoff)
	totalDays := max(int(startOfDay(now).Sub(cutoffDay).Hours()/24)+1, 1)
	dailyMs := make([]int64, totalDays)

	sum.FirstAt = events[0].At
	sum.LastAt = events[0].At

	for vp, evs := range byVP {
		sort.Slice(evs, func(i, j int) bool { return evs[i].At < evs[j].At })

		// A session is bound to (vp, ip): switching devices mid-movie counts as two.
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

// buildUserHeatmaps emits one heatmap per user, heaviest first; each cell's
// dominant IP is the one with the most pings there. IPs is never nil even for
// a user with no resolved devices — the frontend {#each} throws on a JSON null.
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

// buildIPTiles enriches each IP from the live Tailscale table, falling back to
// the stored user/host snapshot for events logged before IPs were recorded.
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

	// Single-IP users get no suffix; prefer the MagicDNS device name over a
	// "#N" number when two devices share a user.
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

// buildGantt slices each session into per-day bars (splitting midnight spans),
// newest first. Empty days are skipped.
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
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
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

// splitByLocalDay returns one segment per calendar day the span touches. The
// ms offsets are from local-midnight of day 0, so callers take startMs % day.
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

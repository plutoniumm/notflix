package jobs

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Resolves a client IP to a Tailscale identity via `tailscale status --json`,
// cached. Ladder: Peer.UserID → User.DisplayName, unless that name is the
// synthetic "Tagged Devices" (the daemon exposes no owner for tagged nodes),
// else the device's own name. Tags are deliberately NOT in the ladder — one
// tag is shared by many devices, which collapses them into useless stats.

// User is the grouping key for per-user stats: the real DisplayName, or the
// per-device short name for tagged peers whose owner the daemon strips.
type Identity struct {
	User   string   `json:"user"`   // grouping key (real user OR per-device for tagged)
	Device string   `json:"device"` // MagicDNS short name (e.g. "dragon"); per-device
	Host   string   `json:"host"`   // raw HostName ("Dragon's Mac Studio")
	OS     string   `json:"os"`     // "macOS", "android", "iOS", "windows", "linux"
	Login  string   `json:"login"`  // email / handle ("AdvithDesu@github"); empty for tagged
	Tags   []string `json:"tags"`   // ["walle", "eve"] — without the "tag:" prefix
	Online bool     `json:"online"` // last known connectivity from the local daemon
}

type tsStatus struct {
	Self *tsPeer            `json:"Self"`
	Peer map[string]*tsPeer `json:"Peer"`
	User map[string]tsUser  `json:"User"`
}

type tsPeer struct {
	HostName     string   `json:"HostName"`
	DNSName      string   `json:"DNSName"`
	OS           string   `json:"OS"`
	UserID       int64    `json:"UserID"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Tags         []string `json:"Tags"`
	Online       bool     `json:"Online"`
}

type tsUser struct {
	DisplayName string `json:"DisplayName"`
	LoginName   string `json:"LoginName"`
}

var (
	tsMu       sync.RWMutex
	tsMap      map[string]Identity // IP → identity
	tsSelf     Identity            // Self's identity (used for localhost requests)
	tsLastLoad time.Time
)

const (
	tsCacheTTL     = 30 * time.Second
	tsRefreshEvery = 5 * time.Minute
	tsExecMax      = 5 * time.Second
)

// Warms the cache and keeps it warm, so a removed device falls out within
// ~5min. Safe when the tailscale CLI is missing — the old map is kept.
func TailscaleInit() {
	refreshTailscale()
	go func() {
		t := time.NewTicker(tsRefreshEvery)
		defer t.Stop()
		for range t.C {
			refreshTailscale()
		}
	}()
}

// Zero value if the IP isn't in the tailnet peer table.
func TailscaleIdentity(ip string) Identity {
	if ip == "" {
		return Identity{}
	}
	ip = strings.TrimPrefix(ip, "::ffff:")
	ip = strings.TrimPrefix(strings.TrimSuffix(ip, "]"), "[")

	// The server host browses in as 127.0.0.1/::1, which is in no peer table.
	if ip == "127.0.0.1" || ip == "::1" || ip == "localhost" {
		ensureTSCache()
		tsMu.RLock()
		defer tsMu.RUnlock()
		return tsSelf
	}

	tsMu.RLock()
	fresh := time.Since(tsLastLoad) < tsCacheTTL
	id, ok := tsMap[ip]
	tsMu.RUnlock()

	if !fresh {
		refreshTailscale()
		tsMu.RLock()
		id, ok = tsMap[ip]
		tsMu.RUnlock()
	}
	if !ok {
		return Identity{}
	}
	return id
}

func ensureTSCache() {
	tsMu.RLock()
	fresh := time.Since(tsLastLoad) < tsCacheTTL
	tsMu.RUnlock()
	if !fresh {
		refreshTailscale()
	}
}

func refreshTailscale() {
	// Claim the slot then drop the lock: this runs inline on every watch ping,
	// so holding the write lock across the CLI call piled up gin workers.
	tsMu.Lock()
	if time.Since(tsLastLoad) < tsCacheTTL {
		tsMu.Unlock()
		return // someone else just refreshed
	}
	tsLastLoad = time.Now()
	tsMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), tsExecMax)
	defer cancel()

	out, err := exec.CommandContext(ctx, "tailscale", "status", "--json").Output()
	if err != nil {
		// Daemon not running / CLI missing — keep whatever stale map we have.
		return
	}
	var st tsStatus
	if err := json.Unmarshal(out, &st); err != nil {
		return
	}

	next := make(map[string]Identity, len(st.Peer)*2+4)
	add := func(p *tsPeer) {
		if p == nil {
			return
		}
		id := resolveIdentity(p, st.User)
		for _, ip := range p.TailscaleIPs {
			next[ip] = id
		}
	}
	add(st.Self)
	for _, p := range st.Peer {
		add(p)
	}

	tsMu.Lock()
	tsMap = next
	if st.Self != nil {
		tsSelf = resolveIdentity(st.Self, st.User)
	}
	tsMu.Unlock()
}

func resolveIdentity(p *tsPeer, users map[string]tsUser) Identity {
	device := magicShortName(p.DNSName)
	if device == "" {
		device = p.HostName
	}

	tags := make([]string, 0, len(p.Tags))
	for _, t := range p.Tags {
		if rest, ok := strings.CutPrefix(t, "tag:"); ok {
			tags = append(tags, rest)
		}
	}

	out := Identity{
		Device: device,
		Host:   p.HostName,
		OS:     p.OS,
		Tags:   tags,
		Online: p.Online,
	}

	if u, ok := users[itoa(p.UserID)]; ok {
		if u.DisplayName != "" && u.DisplayName != "Tagged Devices" {
			out.User = u.DisplayName
			out.Login = u.LoginName
			return out
		}
	}
	// Tagged / unknown owner → group by device so tagged peers stay distinct.
	out.User = device
	return out
}

// "dragon.lamancha-koi.ts.net." → "dragon".
func magicShortName(dnsName string) string {
	if dnsName == "" {
		return ""
	}
	name := strings.TrimSuffix(dnsName, ".")
	if i := strings.IndexByte(name, '.'); i > 0 {
		return name[:i]
	}
	return name
}

// json map keys are stringified ints; sprintf-free.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

package jobs

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// tailscale.go resolves a client IP to a Tailscale identity (display name +
// hostname) by shelling out to `tailscale status --json` and caching the
// peer table. Used by the stats pipeline to attribute watch sessions.
//
// Nothing about a specific tailnet is encoded here — the peer table is
// read at runtime, refreshed every 5 min in background + lazily on cache
// miss, so adding or removing a device picks up without code change.
//
// Resolution rules, in order:
//   1. Peer.UserID → User[UserID].DisplayName, *unless* that name is the
//      synthetic "Tagged Devices" (tagged peers strip per-user attribution
//      from the local daemon — `tailscale whois` confirms no owner info is
//      exposed for tagged nodes).
//   2. Peer.HostName — always unique per-device and human-edited
//      ("Dragon's Mac Studio", "Surtr", etc.), so it's the right per-
//      device identifier when the real user is hidden behind a tag.
//
// Tags are deliberately NOT in the ladder. A tag like "tag:walle" is a
// permission category that several distinct devices can share; collapsing
// devices into a tag name produces useless stats.
//
// Self (the server itself) resolves to its own owner via UserID — the local
// daemon sees its login user even when the device is tagged.
//
// Guests in the tailnet (devices shared from other accounts) already come
// through with their own UserID + DisplayName, so no special handling.

// Identity is the rich, render-ready identity for one tailnet IP. The
// stats pipeline holds these as values — every field is JSON-safe so the
// HTTP layer can pass one through to the frontend without re-massaging.
//
// User is the *grouping key* for the per-user heatmap. For peers with a
// real owner it's the user's DisplayName; for tagged peers (where the
// local Tailscale daemon strips the owner) it's the MagicDNS short name
// so each tagged device stays its own group instead of all collapsing
// under "Tagged Devices".
//
// Device is the per-device identifier (always MagicDNS short name when
// available, falling back to HostName). Stable; safe to use as a map key.
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

// TailscaleInit warms the cache once and then keeps it warm in the
// background. Lazy refresh in TailscaleIdentity still covers the
// add-device-and-immediately-play case (it triggers on cache miss), but
// the periodic refresh means a removed device falls out within ~5 min
// without anyone calling. Safe to call multiple times; safe to call when
// the tailscale CLI is missing — refreshTailscale logs nothing on error
// and leaves the previous map intact.
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

// TailscaleIdentity resolves an IP to a rich Identity. Returns the zero
// value (all fields empty) if the IP isn't in the tailnet peer table.
// Cheap to call repeatedly; the peer table is cached and refresh is
// throttled.
func TailscaleIdentity(ip string) Identity {
	if ip == "" {
		return Identity{}
	}
	// Normalise: strip ::ffff: IPv4-in-IPv6 prefix and brackets.
	ip = strings.TrimPrefix(ip, "::ffff:")
	ip = strings.TrimPrefix(strings.TrimSuffix(ip, "]"), "[")

	// Browsing from the server host itself comes in as 127.0.0.1 / ::1 —
	// none of which appear in the peer table. Resolve those to Self so the
	// owner's plays don't all bucket under "unknown".
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
	// Claim the refresh slot, then drop the lock: TailscaleIdentity runs
	// inline on the stats path of every watch-progress ping, so holding the
	// write lock across the CLI call meant a restarting tailscaled stalled
	// every ping and piled up gin workers. Bounded exec for the same reason.
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

// resolveIdentity builds the rich Identity for one peer.
//
// User (grouping key):
//   - real DisplayName when the local daemon exposes a real owner
//   - else the MagicDNS short name (per-device), so each tagged device
//     stays its own group instead of all collapsing under one tag
//
// Device is always the MagicDNS short name (cleaner than the raw
// HostName for display), falling back to HostName if DNSName is empty.
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
	// Tagged / unknown owner → use the device as the group key so distinct
	// tagged peers stay distinct.
	out.User = device
	return out
}

// magicShortName extracts the leaf label from a Tailscale MagicDNS name
// ("dragon.lamancha-koi.ts.net." → "dragon"). Returns "" if the input
// doesn't look like a MagicDNS FQDN.
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

// json's map key is the stringified int; sprintf-free formatter.
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

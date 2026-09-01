package jobs

import (
	"reflect"
	"testing"
	"time"
)

func TestResolveIdentity(t *testing.T) {
	// "Tagged Devices" is Tailscale's literal DisplayName for the synthetic
	// owner of tagged nodes; must match verbatim or the real-user branch fires.
	users := map[string]tsUser{
		"1": {DisplayName: "real-user", LoginName: "real@example.com"},
		"2": {DisplayName: "Tagged Devices"},
	}

	t.Run("real user wins, login + os propagate", func(t *testing.T) {
		got := resolveIdentity(&tsPeer{
			HostName: "raw-host", DNSName: "shorty.example.ts.net.",
			OS: "macOS", UserID: 1, Tags: []string{"tag:x"},
			Online: true,
		}, users)
		want := Identity{
			User: "real-user", Device: "shorty", Host: "raw-host",
			OS: "macOS", Login: "real@example.com",
			Tags: []string{"x"}, Online: true,
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v want %+v", got, want)
		}
	})

	t.Run("tagged → device name is the group key, login stays empty", func(t *testing.T) {
		got := resolveIdentity(&tsPeer{
			HostName: "raw", DNSName: "tagged-thing.example.ts.net.",
			OS: "android", UserID: 2, Tags: []string{"tag:eve"},
		}, users)
		if got.User != "tagged-thing" || got.Device != "tagged-thing" || got.Login != "" {
			t.Errorf("tagged peer got %+v", got)
		}
		if len(got.Tags) != 1 || got.Tags[0] != "eve" {
			t.Errorf("tags = %v want [eve]", got.Tags)
		}
	})

	t.Run("no DNSName → fall back to HostName", func(t *testing.T) {
		got := resolveIdentity(&tsPeer{HostName: "only-host", UserID: 999}, users)
		if got.Device != "only-host" || got.User != "only-host" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("nothing to resolve to", func(t *testing.T) {
		got := resolveIdentity(&tsPeer{UserID: 999}, users)
		if got.User != "" || got.Device != "" {
			t.Errorf("got %+v, want zero", got)
		}
	})
}

// 192.0.2.x / 203.0.113.x are RFC 5737 documentation IPs.
func TestTailscaleIdentity_Normalises(t *testing.T) {
	const v4 = "192.0.2.10"
	tsMu.Lock()
	tsMap = map[string]Identity{v4: {User: "u", Device: "d"}}
	tsLastLoad = time.Now()
	tsMu.Unlock()

	for _, in := range []string{v4, "::ffff:" + v4} {
		if got := TailscaleIdentity(in); got.User != "u" {
			t.Errorf("input %q resolved to %+v, want User=u", in, got)
		}
	}
	if got := TailscaleIdentity("203.0.113.99"); got.User != "" {
		t.Errorf("unknown IP should resolve to zero Identity, got %+v", got)
	}
}

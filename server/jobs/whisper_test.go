package jobs

import (
	"os/exec"
	"testing"
)

// The bug this locks: A registers, B kills A and registers itself, then A's
// deferred cleanup deletes by key and drops B's entry. C finds nothing to
// kill and starts a third python — two of them racing to write one .vtt.
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

	// procKill only stops the process; the entry belongs to the run that
	// stored it, so a later procDone from that run still matches.
	procKill(key)
	if _, ok := activeProcs.Load(key); !ok {
		t.Fatal("procKill removed the entry")
	}
}

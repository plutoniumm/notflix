package jobs

import (
	"os/exec"
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

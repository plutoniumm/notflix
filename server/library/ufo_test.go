package library

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func ufoFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}

	return p
}

func newUFO() *UFO {
	return &UFO{in: map[string]*abduction{}, out: map[string]*abduction{}}
}

// The whole point: opening a title fired four probes of the same file within
// ~100ms. Concurrent askers must JOIN the running probe, not start their own.
func TestUFOSingleFlights(t *testing.T) {
	u := newUFO()
	path := ufoFile(t, "x")

	var runs atomic.Int32
	release := make(chan struct{})
	var wg sync.WaitGroup

	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u.run("streams", path, func(a *abduction) {
				runs.Add(1)
				<-release
			})
		}()
	}

	// Let them all pile onto the same key before the first one finishes.
	time.Sleep(60 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := runs.Load(); got != 1 {
		t.Errorf("ran the probe %d times, want 1", got)
	}
}

func TestUFOMemoizesSuccess(t *testing.T) {
	u := newUFO()
	path := ufoFile(t, "x")

	var runs atomic.Int32
	for range 3 {
		u.run("streams", path, func(a *abduction) { runs.Add(1) })
	}

	if got := runs.Load(); got != 1 {
		t.Errorf("ran %d times, want 1 (memo missed)", got)
	}
}

// A transient ffprobe failure under load must not be pinned for the life of the
// process — the exact trap getSrcInfo documents.
func TestUFONeverCachesFailure(t *testing.T) {
	u := newUFO()
	path := ufoFile(t, "x")

	var runs atomic.Int32
	for range 3 {
		u.run("streams", path, func(a *abduction) {
			runs.Add(1)
			a.err = fmt.Errorf("boom")
		})
	}

	if got := runs.Load(); got != 3 {
		t.Errorf("ran %d times, want 3 (a failure was cached)", got)
	}
}

// convert and repair rewrite files in place. Serving the old streams for new
// bytes is how a stale probe becomes a wrong segment grid.
func TestUFOReprobesRewrittenFile(t *testing.T) {
	u := newUFO()
	path := ufoFile(t, "x")

	var runs atomic.Int32
	probe := func(a *abduction) { runs.Add(1) }

	u.run("streams", path, probe)
	if err := os.WriteFile(path, []byte("much longer contents"), 0644); err != nil {
		t.Fatal(err)
	}
	u.run("streams", path, probe)

	if got := runs.Load(); got != 2 {
		t.Errorf("ran %d times, want 2 (served a stale probe)", got)
	}
}

// Streams and Format are different questions about the same file.
func TestUFOKindsDoNotCollide(t *testing.T) {
	u := newUFO()
	path := ufoFile(t, "x")

	var runs atomic.Int32
	probe := func(a *abduction) { runs.Add(1) }

	u.run("streams", path, probe)
	u.run("format", path, probe)

	if got := runs.Load(); got != 2 {
		t.Errorf("ran %d times, want 2", got)
	}
}

// A path that cannot be stat'd has no identity to key on, so it must still work
// — uncached rather than wrong.
func TestUFOMissingFileStillRuns(t *testing.T) {
	u := newUFO()

	var runs atomic.Int32
	probe := func(a *abduction) { runs.Add(1) }

	u.run("streams", "/nope/does/not/exist.mp4", probe)
	u.run("streams", "/nope/does/not/exist.mp4", probe)

	if got := runs.Load(); got != 2 {
		t.Errorf("ran %d times, want 2", got)
	}
}

package library

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	astiffprobe "github.com/asticode/go-astiffprobe"
)

// UFO is the probe manager: one ffprobe per file, shared by everyone who asks.
//
// Opening a title fired FOUR separate `ffprobe -show_streams` runs on the same
// path within ~100ms — HLSMaster, HLSAVOffset, AudioInfo and Subctx each forked
// their own — plus a `-show_format` per playlist request. Measured on this
// machine under background load: /api/audio/info 1345ms, /api/subs/info 737ms,
// both repeatable, because nothing was shared and nothing was kept.
//
// Two mechanisms, and the first matters more than the second:
//
//  1. Single flight. A probe already running for a file is JOINED, not
//     duplicated. This is what collapses the open-a-title burst.
//  2. Memo. The finished result is kept, keyed on the file's size and mtime, so
//     a second open costs nothing and a file rewritten in place (convert,
//     repair) re-probes on its own.
//
// Failures are never cached. A transient ffprobe failure under load would
// otherwise be pinned for the life of the process, which is the same trap
// getSrcInfo documents.
type UFO struct {
	mu  sync.Mutex
	in  map[string]*abduction
	out map[string]*abduction
}

// One probe: in-flight while done is open, a result once it is closed.
type abduction struct {
	done    chan struct{}
	streams []astiffprobe.Stream
	format  astiffprobe.Format
	err     error
}

var Probes = &UFO{in: map[string]*abduction{}, out: map[string]*abduction{}}

// Keyed on identity, not just name: a file rewritten in place by convert or
// repair keeps its path, and serving the old streams for new bytes is how a
// stale probe turns into a wrong segment grid.
func probeKey(kind, path string) (string, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", false
	}

	return fmt.Sprintf("%s|%s|%d|%d", kind, path, fi.Size(), fi.ModTime().UnixNano()), true
}

// probeTimeout bounds every probe. It replaces the per-call-site context: most
// sites passed context.Background() with NO deadline at all, and a probe of a
// damaged file could hang a pool slot until shutdown. Generous because a cold
// read of a multi-GB file over the USB volume under load is legitimately slow.
const probeTimeout = 60 * time.Second

// run is the shared body: join an in-flight probe, serve a memo, or do the work.
// fn always runs on a fresh deadline, never the caller's context — one client
// walking away must not kill a probe several other handlers are waiting on.
func (u *UFO) run(kind, path string, fn func(*abduction)) *abduction {
	key, ok := probeKey(kind, path)
	if !ok {
		a := &abduction{done: make(chan struct{})}
		fn(a)
		close(a.done)

		return a
	}

	u.mu.Lock()
	if a, hit := u.out[key]; hit {
		u.mu.Unlock()

		return a
	}
	if a, hit := u.in[key]; hit {
		u.mu.Unlock()
		<-a.done

		return a
	}

	a := &abduction{done: make(chan struct{})}
	u.in[key] = a
	u.mu.Unlock()

	fn(a)
	close(a.done)

	u.mu.Lock()
	delete(u.in, key)
	if a.err == nil {
		// ~2000 titles in this library, a few KB each. Clearing wholesale beats
		// an LRU nobody will tune; a cleared memo only costs the next probe.
		if len(u.out) > 4096 {
			u.out = map[string]*abduction{}
		}
		u.out[key] = a
	}
	u.mu.Unlock()

	return a
}

// Streams replaces library.Prober.Streams at every call site.
func (u *UFO) Streams(path string) ([]astiffprobe.Stream, error) {
	a := u.run("streams", path, func(a *abduction) {
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		defer cancel()
		a.streams, a.err = Prober.Streams(ctx, path)
	})

	return a.streams, a.err
}

// Format replaces library.Prober.Format at every call site.
func (u *UFO) Format(path string) (astiffprobe.Format, error) {
	a := u.run("format", path, func(a *abduction) {
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		defer cancel()
		a.format, a.err = Prober.Format(ctx, path)
	})

	return a.format, a.err
}

// Forget drops a file's memos. Call it after rewriting a file in place; the
// size+mtime key already covers the normal case, but an in-place rewrite that
// lands on the same size within the same nanosecond would not.
func (u *UFO) Forget(path string) {
	u.mu.Lock()
	defer u.mu.Unlock()

	for _, kind := range []string{"streams", "format"} {
		if key, ok := probeKey(kind, path); ok {
			delete(u.out, key)
		}
	}
}

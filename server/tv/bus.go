// Package tv is the command bus between the phone remote (/R) and the TV
// screen, plus the second HTTP listener that serves them.
//
// The TV is a display, not a browser: it subscribes over SSE and does what it
// is told. The phone owns navigation. Keeping the state here rather than in
// either page means a remote that reloads, or a second phone, picks up exactly
// where things are — there is no handshake to get wrong.
package tv

import (
	"encoding/json"
	"log"
	"sync"
	"sync/atomic"
)

// Cmd is one instruction for the screen. Only `Cmd` is always present; the rest
// are set per command and omitted otherwise.
type Cmd struct {
	Cmd  string  `json:"cmd"`            // play | pause | resume | seek | seekBy | stop | volume | unmute | fullscreen | sub
	File string  `json:"file,omitempty"` // rel path, for play
	T    float64 `json:"t,omitempty"`    // absolute seconds, for play/seek
	D    float64 `json:"d,omitempty"`    // delta seconds, for seekBy
	V    float64 `json:"v,omitempty"`    // 0..1, for volume
	// Sub is an index into State.Subs, or -1 for off. A pointer so that 0 — a
	// perfectly good index — survives JSON's omitempty.
	Sub *int `json:"sub,omitempty"`
}

// State is what the screen last reported. The remote renders its scrubber from
// this, so it is the screen's truth, never the remote's intent.
type State struct {
	File   string  `json:"file"`
	Title  string  `json:"title"`
	Pos    float64 `json:"pos"`
	Dur    float64 `json:"dur"`
	Paused bool    `json:"paused"`
	Volume float64 `json:"volume"`
	// Autoplay with sound needs a gesture the TV page can never have, so the
	// screen starts muted and says so. Without this the remote shows a happily
	// playing film and no hint why the room is silent.
	Muted bool `json:"muted"`
	// Whether the screen is actually in fullscreen. Requesting it needs a
	// gesture the TV page cannot have, so the remote has to be able to see that
	// the request was refused rather than assume it worked.
	Full bool `json:"full"`
	// The subtitle tracks this title actually has, and which one is on. The
	// remote cannot offer a choice it has not been told about, and the screen
	// is the only thing that knows what loaded.
	Subs []string `json:"subs"`
	Sub  int      `json:"sub"`
	// Screens is how many displays are listening. A remote with zero screens
	// should say so rather than silently dropping commands into nothing.
	Screens int `json:"screens"`
}

type Bus struct {
	mu    sync.Mutex
	subs  map[int]chan []byte
	next  int
	state State

	// Bumped on every state write so a long-poll can wait for a change rather
	// than the remote asking four times a second forever.
	rev atomic.Uint64
}

func New() *Bus {
	return &Bus{subs: map[int]chan []byte{}}
}

// Subscribe returns a channel of encoded commands and a function to release it.
// Buffered: a screen that stalls must not block the remote's POST.
func (b *Bus) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 16)

	b.mu.Lock()
	id := b.next
	b.next++
	b.subs[id] = ch
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		delete(b.subs, id)
		close(ch)
		b.mu.Unlock()
	}
}

// Send fans a command out to every screen. Returns how many got it.
func (b *Bus) Send(c Cmd) int {
	raw, err := json.Marshal(c)
	if err != nil {
		return 0
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	sent := 0
	for id, ch := range b.subs {
		select {
		case ch <- raw:
			sent++
		default:
			// A screen that cannot keep up with a 16-deep queue is wedged, not
			// slow. Dropping is right: these are live transport commands and a
			// stale one is worse than none.
			log.Printf("[tv] screen %d not draining, dropped %s", id, c.Cmd)
		}
	}

	return sent
}

func (b *Bus) SetState(s State) {
	b.mu.Lock()
	s.Screens = len(b.subs)
	b.state = s
	b.mu.Unlock()
	b.rev.Add(1)
}

func (b *Bus) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()

	s := b.state
	s.Screens = len(b.subs)

	return s
}

func (b *Bus) Rev() uint64 { return b.rev.Load() }

package library

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// FFRun is a single ffmpeg invocation. It always prepends -nostdin / -v error.
// Progress (when requested) goes to its OWN pipe (stdout), parsed and
// discarded — never stored. stderr is captured into a BOUNDED tail so a
// failing/looping ffmpeg can't flood the log with its progress stream or
// binary spew; the actual error is always at the end, which is what's kept.
type FFRun struct {
	Args     []string
	Duration float64           // optional: total seconds, enables progress
	OnPct    func(pct float64) // optional: called on each time= match
	Timeout  time.Duration     // optional: kill ffmpeg after this elapses
}

// max stderr retained for error messages. ffmpeg's real diagnostics are a few
// lines; this is generous headroom while capping pathological output.
const ffStderrCap = 8 << 10

func (r FFRun) Run() (string, error) {
	args := append([]string{"-nostdin", "-v", "error"}, r.Args...)

	progress := r.Duration > 0 && r.OnPct != nil
	if progress {
		// pipe:1 = stdout. ffmpeg encode/remux jobs write their output to file
		// args, never stdout, so it's free for the machine-readable -progress
		// key=value stream — keeping it OUT of the stderr error buffer.
		args = append(args, "-progress", "pipe:1")
	}

	ctx := context.Background()
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	errW := &TailWriter{Max: ffStderrCap}
	cmd.Stderr = errW

	if devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0); err == nil {
		cmd.Stdin = devNull
		cmd.Stdout = devNull
		defer devNull.Close()
	}
	if progress {
		cmd.Stdout = &progressWriter{durationSec: r.Duration, onPct: r.OnPct}
	}

	err := cmd.Run()
	return strings.TrimSpace(errW.String()), err
}

// FF is the common case: no progress, no timeout. Returns stderr, error.
func FF(args ...string) (string, error) {
	return FFRun{Args: args}.Run()
}

// FFErr formats an ffmpeg error with stderr included, trimmed.
func FFErr(stderr string, err error) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return fmt.Errorf("ffmpeg: %w", err)
	}
	return fmt.Errorf("ffmpeg: %w — %s", err, stderr)
}

var (
	timeRe   = regexp.MustCompile(`time=(\d{2}):(\d{2}):(\d{2})\.(\d{2})`)
	usTimeRe = regexp.MustCompile(`out_time_us=(\d+)`)
)

// TailWriter keeps only the last `Max` bytes written and strips NUL bytes —
// a cheap ring so a runaway subprocess can neither grow memory nor flood the
// log with binary garbage (a partial-buffer Read or a busted writer dumps
// `\x00\x00…` for thousands of lines; NUL is never meaningful in text
// stderr). Exported so non-ffmpeg subprocess captures (whisper, etc.) can
// reuse the same defense.
type TailWriter struct {
	Max int
	b   []byte
}

func (w *TailWriter) Write(p []byte) (int, error) {
	// Always report the full length so callers/pipes don't get short writes.
	n := len(p)
	if i := bytes.IndexByte(p, 0); i >= 0 {
		// Hot path stays alloc-free; only copy when NULs present.
		p = bytes.ReplaceAll(p, []byte{0}, nil)
	}
	w.b = append(w.b, p...)
	if len(w.b) > w.Max {
		w.b = w.b[len(w.b)-w.Max:]
	}
	return n, nil
}

func (w *TailWriter) String() string { return string(w.b) }

// progressWriter parses ffmpeg -progress output for the OnPct callback and
// discards it. Nothing is stored — this never feeds error messages.
type progressWriter struct {
	durationSec float64
	onPct       func(pct float64)
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	if pw.durationSec > 0 && pw.onPct != nil {
		s := string(p)
		var t float64
		if m := usTimeRe.FindStringSubmatch(s); m != nil {
			us, _ := strconv.ParseInt(m[1], 10, 64)
			t = float64(us) / 1e6
		} else if m := timeRe.FindStringSubmatch(s); m != nil {
			h, _ := strconv.Atoi(m[1])
			min, _ := strconv.Atoi(m[2])
			sec, _ := strconv.Atoi(m[3])
			cs, _ := strconv.Atoi(m[4])
			t = float64(h*3600+min*60+sec) + float64(cs)/100
		}
		if t > 0 {
			pw.onPct(math.Min(t/pw.durationSec*100, 99))
		}
	}
	return len(p), nil
}

package library

import (
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

// FFRun is a single ffmpeg invocation. It always prepends -nostdin / -v error,
// wires devnull to stdin and stdout, and captures stderr into a buffer that's
// returned alongside the run error so callers can format their own messages.
type FFRun struct {
	Args     []string
	Duration float64           // optional: total seconds, enables progress
	OnPct    func(pct float64) // optional: called on each time= match
	Timeout  time.Duration     // optional: kill ffmpeg after this elapses
}

func (r FFRun) Run() (string, error) {
	args := append([]string{"-nostdin", "-v", "error"}, r.Args...)

	ctx := context.Background()
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	pw := &progressWriter{durationSec: r.Duration, onPct: r.OnPct}
	cmd.Stderr = pw

	if devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0); err == nil {
		cmd.Stdin = devNull
		cmd.Stdout = devNull
		defer devNull.Close()
	}

	err := cmd.Run()
	return pw.buf.String(), err
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

var timeRe = regexp.MustCompile(`time=(\d{2}):(\d{2}):(\d{2})\.(\d{2})`)

type progressWriter struct {
	durationSec float64
	onPct       func(pct float64)
	buf         strings.Builder
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	s := string(p)
	pw.buf.WriteString(s)
	if pw.durationSec > 0 && pw.onPct != nil {
		if m := timeRe.FindStringSubmatch(s); m != nil {
			h, _ := strconv.Atoi(m[1])
			min, _ := strconv.Atoi(m[2])
			sec, _ := strconv.Atoi(m[3])
			cs, _ := strconv.Atoi(m[4])
			t := float64(h*3600+min*60+sec) + float64(cs)/100
			pw.onPct(math.Min(t/pw.durationSec*100, 99))
		}
	}
	return len(p), nil
}

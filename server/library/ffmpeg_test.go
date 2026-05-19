package library

import (
	"strings"
	"testing"
)

// Regression: a runaway subprocess (whisper / ffmpeg / etc.) used to dump
// thousands of `\x00` rows into the log. TailWriter must (a) cap its memory
// and (b) strip NUL bytes so even at the cap there's no visible binary spew.
func TestTailWriter_BoundsAndStripsNUL(t *testing.T) {
	w := &TailWriter{Max: 64}

	// Write 10× the cap of mixed text + NUL bytes.
	big := strings.Repeat("ok\x00\x00", 1024)
	w.Write([]byte(big))

	s := w.String()
	if len(s) > 64 {
		t.Errorf("exceeded Max: got %d bytes, want ≤ 64", len(s))
	}
	if strings.ContainsRune(s, 0) {
		t.Errorf("NUL byte leaked into log buffer: %q", s)
	}
	// The trailing window should be the printable text only (no NULs).
	if !strings.HasSuffix(s, "ok") {
		t.Errorf("tail should end with text content, got %q", s)
	}
}

package library

import (
	"strings"
	"testing"
)

// Regression: a runaway subprocess dumped thousands of `\x00` rows into the log.
func TestTailWriter_BoundsAndStripsNUL(t *testing.T) {
	w := &TailWriter{Max: 64}

	big := strings.Repeat("ok\x00\x00", 1024)
	w.Write([]byte(big))

	s := w.String()
	if len(s) > 64 {
		t.Errorf("exceeded Max: got %d bytes, want ≤ 64", len(s))
	}
	if strings.ContainsRune(s, 0) {
		t.Errorf("NUL byte leaked into log buffer: %q", s)
	}
	if !strings.HasSuffix(s, "ok") {
		t.Errorf("tail should end with text content, got %q", s)
	}
}

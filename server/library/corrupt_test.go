package library

import (
	"os"
	"path/filepath"
	"testing"
)

// inProgress gates the corrupt scan: a partial download (aria2 .aria2 control
// file) or an in-flight conversion (.tmp / .audio.tmp sibling) probes as
// unreadable but is NOT corrupt — it must be skipped, then re-scans clean once
// finished. User intent (2026-05-21): exclude converting + downloading.
func TestInProgress(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	cases := []struct {
		name     string
		sidecars []string
		want     bool
	}{
		{"plain finished file", nil, false},
		{"active aria2 download", []string{"movie.mp4.aria2"}, true},
		{"in-flight conversion target", []string{"movie.mp4.tmp"}, true},
		{"in-place audio remux", []string{"movie.mp4.audio.tmp"}, true},
		{"unrelated sidecar doesn't excuse", []string{"movie.mp4.vtt"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matches, _ := filepath.Glob(filepath.Join(dir, "movie.mp4*"))
			for _, m := range matches {
				os.Remove(m)
			}
			mp4 := mk("movie.mp4")
			for _, s := range tc.sidecars {
				mk(s)
			}
			if got := inProgress(mp4); got != tc.want {
				t.Errorf("inProgress(%v) = %v, want %v", tc.sidecars, got, tc.want)
			}
		})
	}
}

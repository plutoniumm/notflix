package library

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// IsBusy gates every background pass: a partial download (aria2 .aria2 control
// file) or an in-flight conversion (.tmp / .audio.tmp sibling) probes as
// unreadable but is NOT corrupt — it must be skipped, then re-scans clean once
// finished. User intent (2026-05-21): exclude converting + downloading.
// Freshly-written files (mtime <30s) are also treated as busy — bumped mtimes
// backward here so the sidecar semantics stay testable in isolation.
func TestIsBusySidecars(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-2 * time.Minute)
		if err := os.Chtimes(p, old, old); err != nil {
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
			if got := IsBusy(mp4); got != tc.want {
				t.Errorf("IsBusy(%v) = %v, want %v", tc.sidecars, got, tc.want)
			}
		})
	}
}

func TestIsBusyFreshModtime(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "fresh.mp4")
	if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if !IsBusy(p) {
		t.Fatalf("just-written file should read as busy")
	}
	old := time.Now().Add(-2 * time.Minute)
	os.Chtimes(p, old, old)
	if IsBusy(p) {
		t.Fatalf("aged file should no longer read as busy")
	}
}

func TestIsBusyTorrentCallback(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "torr.mp4")
	os.WriteFile(p, []byte("x"), 0644)
	old := time.Now().Add(-2 * time.Minute)
	os.Chtimes(p, old, old)

	orig := IsTorrentBusy
	t.Cleanup(func() { IsTorrentBusy = orig })

	IsTorrentBusy = func(path string) bool { return path == p }
	if !IsBusy(p) {
		t.Fatalf("torrent callback true → IsBusy should be true")
	}
	IsTorrentBusy = func(path string) bool { return false }
	if IsBusy(p) {
		t.Fatalf("torrent callback false + no sidecars + old mtime → IsBusy false")
	}
}

package library

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A partial download or in-flight conversion probes as unreadable but is NOT
// corrupt. Mtimes are aged past the 30s fresh-file window to isolate the
// sidecar rules from it.
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

// 366 permanently-dead files were re-probed and re-repaired on every sweep and
// every restart. The mark has to survive, but only for the exact bytes it was
// proven on — a replaced download must get a fresh chance.
func TestDeadMarkIsPerBytes(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cache"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	p := filepath.Join(dir, "broken.mp4")
	if err := os.WriteFile(p, []byte("\x00\x00\x00\x00"), 0644); err != nil {
		t.Fatal(err)
	}

	if IsDead(p) {
		t.Fatal("unmarked file reported dead")
	}

	MarkDead(p)
	if !IsDead(p) {
		t.Error("mark did not stick")
	}

	// Re-downloaded: different bytes, so the verdict must not carry over.
	if err := os.WriteFile(p, []byte("a genuinely different payload"), 0644); err != nil {
		t.Fatal(err)
	}
	if IsDead(p) {
		t.Error("stale mark applied to replaced bytes")
	}

	MarkDead(p)
	ClearDead(p)
	if IsDead(p) {
		t.Error("ClearDead did not clear")
	}
}

func TestDeadMarkMissingFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cache"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	// No file to stamp: never claim dead, and never panic.
	if IsDead(filepath.Join(dir, "nope.mp4")) {
		t.Error("missing file reported dead")
	}
	MarkDead(filepath.Join(dir, "nope.mp4"))
}

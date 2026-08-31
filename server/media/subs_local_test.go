package media

import (
	"os"
	"path/filepath"
	"testing"

	"notflix/server/library"
)

// A bracket in a release name is a character class to filepath.Glob, so
// "Movie [2020]…" matched nothing (and returned no error) — 7% of the library
// showed no sidecar subtitle tracks at all.
func TestVTTSiblings_BracketedName(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Movie [2020] 1080p-[YTS.MX].mp4")
	for _, n := range []string{
		"Movie [2020] 1080p-[YTS.MX].mp4",
		"Movie [2020] 1080p-[YTS.MX].eng.vtt",
		"Movie [2020] 1080p-[YTS.MX].fre.vtt",
		"Movie [2020] 1080p-[YTS.MX].vtt", // bare sidecar, reported on its own
		"Other.eng.vtt",
	} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}

	got := vttSiblings(video)
	if len(got) != 2 {
		t.Fatalf("got %v, want the eng + fre sidecars", got)
	}

	langs := map[string]bool{got[0].Language: true, got[1].Language: true}
	if !langs["eng"] || !langs["fre"] {
		t.Errorf("languages = %v, want eng + fre", langs)
	}
}

// An unreadable root reads as "these titles are gone", which used to delete
// every cache dir belonging to it — including one being streamed right then.
func TestCleanHLSCache_SkipsUnreadableRoot(t *testing.T) {
	chdirTemp(t)
	cached := filepath.Join(hlsCacheDir, "1234567")
	if err := os.MkdirAll(cached, 0755); err != nil {
		t.Fatal(err)
	}

	CleanHLSCache(&library.Library{Roots: []string{filepath.Join(t.TempDir(), "unmounted")}})

	if _, err := os.Stat(cached); err != nil {
		t.Error("cache deleted while a root was unreadable")
	}
}

func TestCleanHLSCache_RemovesOrphans(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "movie.mp4"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	live := filepath.Join(hlsCacheDir, library.Hash("movie.mp4"))
	orphan := filepath.Join(hlsCacheDir, "9999999")
	for _, d := range []string{live, orphan} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	CleanHLSCache(&library.Library{Roots: []string{root}})

	if _, err := os.Stat(orphan); err == nil {
		t.Error("orphan cache kept")
	}
	if _, err := os.Stat(live); err != nil {
		t.Error("live cache removed")
	}
}

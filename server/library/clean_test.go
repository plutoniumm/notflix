package library

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// touch writes rel under root with a modtime old enough to clear both the
// IsBusy 30s window and the sweep's 1h quiet period.
func touch(t *testing.T, root, rel string) string {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	old := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	return path
}

func exists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

// The sweep deletes only known junk. Containers the allowlist never heard of
// (.m2ts remuxes) and artwork used to be removed an hour after they landed.
func TestCleanAll_KeepsUnknownExtensions(t *testing.T) {
	root := t.TempDir()
	keep := []string{"Disc/movie.m2ts", "Disc/movie.vob", "Disc/poster.jpg", "Disc/movie.srt", "Disc/notes.doc"}
	junk := []string{"Disc/readme.nfo", "Disc/rarbg.txt", "Disc/movie.mkv.part", "Disc/keygen.exe"}
	for _, rel := range append(append([]string{}, keep...), junk...) {
		touch(t, root, rel)
	}

	CleanAll([]string{root})

	for _, rel := range keep {
		if !exists(filepath.Join(root, filepath.Base(rel))) && !exists(filepath.Join(root, rel)) {
			t.Errorf("deleted %s", rel)
		}
	}

	for _, rel := range junk {
		if exists(filepath.Join(root, rel)) {
			t.Errorf("kept junk %s", rel)
		}
	}
}

// "No video we recognise" is not "nothing of value": a directory of .m2ts
// used to be nuked wholesale.
func TestCleanAll_NeverNukesNonEmptyDir(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "Bluray/00001.m2ts")
	touch(t, root, "Extras/artwork.psd")
	empty := filepath.Join(root, "Empty")
	os.MkdirAll(filepath.Join(empty, "nested"), 0755)

	CleanAll([]string{root})

	if !exists(filepath.Join(root, "Extras", "artwork.psd")) {
		t.Error("nuked a directory holding an unrecognised file")
	}
	if !exists(filepath.Join(root, "Bluray", "00001.m2ts")) && !exists(filepath.Join(root, "00001.m2ts")) {
		t.Error("lost the m2ts")
	}
	if exists(empty) {
		t.Error("genuinely empty dir survived")
	}
}

// Three same-named siblings had exactly one conflict slot, so the third
// silently overwrote the second.
func TestFlatDir_KeepsEveryNameCollision(t *testing.T) {
	root := t.TempDir()
	for _, sub := range []string{"a", "b", "c"} {
		touch(t, root, filepath.Join("Show", sub, "01.mkv"))
	}

	CleanAll([]string{root})

	show := filepath.Join(root, "Show")
	for _, name := range []string{"01.mkv", "01_2.mkv", "01_3.mkv"} {
		if !exists(filepath.Join(show, name)) {
			t.Errorf("missing %s after flatten", name)
		}
	}
}

// A single nested video flattens up to the root and its emptied dir goes.
func TestCleanAll_FlattensSingleVideo(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "Movie.2024/Movie.2024.mkv")
	touch(t, root, "Movie.2024/Movie.2024.eng.srt")

	CleanAll([]string{root})

	if !exists(filepath.Join(root, "Movie.2024.mkv")) {
		t.Fatal("video not flattened to root")
	}
	if exists(filepath.Join(root, "Movie.2024")) {
		t.Error("emptied dir survived")
	}
}

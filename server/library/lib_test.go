package library

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// AllVideos feeds LiveHashes, which decides whose HLS cache gets deleted —
// a video it can't see loses its cache mid-stream. It used to stop two
// levels down.
func TestAllVideos_WalksFullDepth(t *testing.T) {
	root := t.TempDir()
	rels := []string{
		"top.mp4",
		"Show/s01e01.mkv",
		"Show/Season 2/s02e01.mkv",
		"Show/Season 2/Extras/behind.mov",
	}
	for _, rel := range rels {
		path := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(path), 0755)
		os.WriteFile(path, []byte("x"), 0644)
	}

	os.MkdirAll(filepath.Join(root, ".hidden"), 0755)
	os.WriteFile(filepath.Join(root, ".hidden", "skip.mp4"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(root, "Show", "notes.txt"), []byte("x"), 0644)

	lib := &Library{Roots: []string{root}}
	var got []string
	for _, v := range lib.AllVideos() {
		if v.Root != root {
			t.Errorf("Root = %q, want %q", v.Root, root)
		}
		got = append(got, v.Rel)
	}
	sort.Strings(got)

	want := append([]string{}, rels...)
	sort.Strings(want)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("AllVideos = %v, want %v", got, want)
	}
}

// A sibling whose name merely extends a root's ("/Volumes/AomineEvil" vs
// root "/Volumes/Aomine") is not inside that root — a bare prefix test says
// it is, and FindRoot then hands callers the wrong root for the path.
func TestFindRoot_SiblingRootIsNotContained(t *testing.T) {
	base := t.TempDir()
	aomine := filepath.Join(base, "Aomine")
	evil := filepath.Join(base, "AomineEvil")
	os.MkdirAll(aomine, 0755)
	os.MkdirAll(evil, 0755)
	os.WriteFile(filepath.Join(evil, "movie.mp4"), []byte("x"), 0644)

	lib := &Library{Roots: []string{aomine, evil}}

	root, abs, ok := lib.FindRoot("movie.mp4")
	if !ok {
		t.Fatal("FindRoot did not find the file")
	}
	if root != evil {
		t.Errorf("root = %q, want %q", root, evil)
	}
	if abs != filepath.Join(evil, "movie.mp4") {
		t.Errorf("abs = %q, want %q", abs, filepath.Join(evil, "movie.mp4"))
	}
}

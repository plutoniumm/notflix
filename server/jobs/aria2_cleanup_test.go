package jobs

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// aria2CleanupTargets gates file deletion on torrent remove. The critical
// invariant: never return the download root itself (that would nuke the whole
// library root), and never escape it.
func TestAria2CleanupTargets(t *testing.T) {
	root := "/Volumes/Ravan"

	cases := []struct {
		name     string
		paths    []string
		wantTops []string
	}{
		{
			name:     "single file directly in root → top is the file, not root",
			paths:    []string{filepath.Join(root, "movie.mkv")},
			wantTops: []string{filepath.Join(root, "movie.mkv")},
		},
		{
			name: "multi-file torrent → one top folder",
			paths: []string{
				filepath.Join(root, "Show.S01", "ep1.mkv"),
				filepath.Join(root, "Show.S01", "ep2.mkv"),
				filepath.Join(root, "Show.S01", "extras", "x.mkv"),
			},
			wantTops: []string{filepath.Join(root, "Show.S01")},
		},
		{
			name:     "path escaping root is ignored (no top)",
			paths:    []string{"/Volumes/Other/sneaky.mkv"},
			wantTops: nil,
		},
		{
			name:     "file exactly at root path yields no destructive top",
			paths:    []string{root},
			wantTops: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files, tops := aria2CleanupTargets(root, tc.paths)
			if !reflect.DeepEqual(files, tc.paths) {
				t.Errorf("files = %v, want %v", files, tc.paths)
			}
			if !reflect.DeepEqual(tops, tc.wantTops) {
				t.Errorf("tops = %v, want %v", tops, tc.wantTops)
			}
			for _, top := range tops {
				if filepath.Clean(top) == filepath.Clean(root) {
					t.Fatalf("DANGER: top %q equals root %q — would delete the library root", top, root)
				}
			}
		})
	}
}

// cleanupDirs feeds the non-recursive sweep. Deepest-first matters: a
// subfolder has to go before its parent can.
func TestCleanupDirs(t *testing.T) {
	root := "/Volumes/Ravan"
	got := cleanupDirs(root, []string{
		filepath.Join(root, "Show.S01", "extras", "x.mkv"),
		filepath.Join(root, "Show.S01", "ep1.mkv"),
		filepath.Join(root, "loose.mkv"),
		"/Volumes/Other/sneaky.mkv",
	})
	want := []string{
		filepath.Join(root, "Show.S01", "extras"),
		filepath.Join(root, "Show.S01"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cleanupDirs = %v, want %v", got, want)
	}

	for _, d := range got {
		if filepath.Clean(d) == filepath.Clean(root) {
			t.Fatalf("DANGER: %q is the library root", d)
		}
	}
}

// Removing a torrent must erase only that torrent. The organizer reuses
// existing folders, so a download dir routinely holds unrelated media — which
// the old os.RemoveAll took with it.
func TestRemoveDownloadSparesUnrelated(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "Documentaries")
	if err := os.MkdirAll(filepath.Join(folder, "extras"), 0755); err != nil {
		t.Fatal(err)
	}

	part := filepath.Join(folder, "partial.mkv")
	extra := filepath.Join(folder, "extras", "sample.mkv")
	keep := filepath.Join(folder, "someone-elses-movie.mp4")
	for _, p := range []string{part, part + ".aria2", extra, keep} {
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	paths := []string{part, extra}
	files, tops := aria2CleanupTargets(root, paths)
	removeDownload(files, cleanupDirs(root, paths), tops)

	for _, gone := range []string{part, part + ".aria2", extra} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("%s should have been deleted", filepath.Base(gone))
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("unrelated media deleted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(folder, "extras")); err == nil {
		t.Error("emptied subfolder should have been swept")
	}
}

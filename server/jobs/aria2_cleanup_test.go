package jobs

import (
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

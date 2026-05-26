package jobs

import (
	"os"
	"path/filepath"
	"testing"
)

// hasEnglishSub gates BackfillWhisper / AutoWhisper / the conversion hook.
// User intent (2026-05-20): files with English subs already → skip; files
// without → generate. Anything else is wasted CPU.
func TestHasEnglishSub(t *testing.T) {
	dir := t.TempDir()
	mp4 := filepath.Join(dir, "movie.mp4")
	if err := os.WriteFile(mp4, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		sidecar string
		want    bool
	}{
		// Skip: any of the english-tagged sidecars OR a prior whisper run.
		{"whisper.vtt skips", "movie.whisper.vtt", true},
		{"en.vtt skips", "movie.en.vtt", true},
		{"eng.vtt skips", "movie.eng.vtt", true},
		{"english.vtt skips", "movie.english.vtt", true},
		{"en.srt skips", "movie.en.srt", true},

		// Generate: no recognised english sidecar.
		{"foreign sub doesn't block", "movie.fr.vtt", false},
		{"raw .vtt doesn't block (might not be english)", "movie.vtt", false},
		{"unrelated sidecar doesn't block", "movie.nfo", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Clean slate for each case.
			matches, _ := filepath.Glob(filepath.Join(dir, "movie.*"))
			for _, m := range matches {
				if m != mp4 {
					os.Remove(m)
				}
			}

			if tc.sidecar != "" {
				if err := os.WriteFile(filepath.Join(dir, tc.sidecar), []byte("x"), 0644); err != nil {
					t.Fatal(err)
				}
			}

			got := hasEnglishSub(mp4)
			if got != tc.want {
				t.Errorf("hasEnglishSub(%s with %s) = %v, want %v",
					filepath.Base(mp4), tc.sidecar, got, tc.want)
			}
		})
	}
}

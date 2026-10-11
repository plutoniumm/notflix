package jobs

import (
	"os"
	"path/filepath"
	"testing"
)

// 50 files (47 City Hunter episodes shipping English subs as a plain `.vtt`)
// were fully re-transcribed because the suffix list only knew `.eng.vtt` etc.
func TestHasEnglishSubPlainSidecar(t *testing.T) {
	dir := t.TempDir()
	eng := "WEBVTT\n\n00:00:05.000 --> 00:00:15.000\nAre you a professional killer?!\n"
	jpn := "WEBVTT\n\n00:00:05.000 --> 00:00:15.000\nあなたはプロの殺し屋なの\n"

	cases := []struct {
		name, sidecar, body string
		want                bool
	}{
		{"no sidecar at all", "", "", false},
		{"plain .vtt, english (City Hunter)", ".vtt", eng, true},
		{"plain .srt, english", ".srt", eng, true},
		{"plain .vtt, japanese — must still transcribe+translate", ".vtt", jpn, false},
		{"suffixed .eng.vtt", ".eng.vtt", eng, true},
		{"our own prior output", ".whisper.vtt", eng, true},
	}
	for _, tc := range cases {
		mp4 := filepath.Join(dir, tc.name+".mp4")
		if err := os.WriteFile(mp4, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if tc.sidecar != "" {
			p := filepath.Join(dir, tc.name+tc.sidecar)
			if err := os.WriteFile(p, []byte(tc.body), 0644); err != nil {
				t.Fatal(err)
			}
		}
		if got := hasEnglishSub(mp4); got != tc.want {
			t.Errorf("%s: hasEnglishSub = %v, want %v", tc.name, got, tc.want)
		}
	}
}

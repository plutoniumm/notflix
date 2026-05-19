package media

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"notflix/server/library"
)

// makeMultiTrackMKV builds a 2s clip with TWO audio tracks (a:0 jpn, a:1 eng —
// English deliberately NOT first) and TWO subtitle tracks (s:0 eng, s:1 spa).
// Skips if ffmpeg is unavailable. Used to prove conversion keeps every track
// and defaults audio to English.
func makeMultiTrackMKV(t *testing.T, root, name string) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}

	srt := func(n string) string {
		p := filepath.Join(t.TempDir(), n)
		if err := os.WriteFile(p, []byte("1\n00:00:00,000 --> 00:00:01,500\nline\n\n"), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	subA, subB := srt("a.srt"), srt("b.srt")

	out := filepath.Join(root, name)
	cmd := exec.Command("ffmpeg", "-y", "-v", "error",
		"-f", "lavfi", "-i", "testsrc2=s=320x240:d=2:r=24",
		"-f", "lavfi", "-i", "sine=f=300:d=2",
		"-f", "lavfi", "-i", "sine=f=600:d=2",
		"-i", subA, "-i", subB,
		"-map", "0:v", "-map", "1:a", "-map", "2:a", "-map", "3:s", "-map", "4:s",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "48k", "-c:s", "srt",
		"-metadata:s:a:0", "language=jpn",
		"-metadata:s:a:1", "language=eng",
		"-metadata:s:s:0", "language=eng",
		"-metadata:s:s:1", "language=spa",
		"-shortest", out,
	)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg multitrack fixture failed: %v\n%s", err, b)
	}
	return out
}

func audioCount(t *testing.T, path string) int {
	t.Helper()
	streams, err := library.Prober.Streams(context.Background(), path)
	if err != nil {
		t.Fatalf("probe %s: %v", path, err)
	}
	n := 0
	for _, s := range streams {
		if s.CodecType == "audio" {
			n++
		}
	}
	return n
}

func TestAudioDispositionArgs_EnglishBecomesDefault(t *testing.T) {
	root := filepath.Join(t.TempDir(), "lib")
	src := makeMultiTrackMKV(t, root, "show.mkv")

	got := strings.Join(audioDispositionArgs(src), " ")
	want := "-disposition:a:0 0 -disposition:a:1 default"
	if got != want {
		t.Errorf("disposition args = %q, want %q", got, want)
	}
}

func TestAudioDispositionArgs_SingleAudioIsNoOp(t *testing.T) {
	root := filepath.Join(t.TempDir(), "lib")
	src := makeMP4(t, root, "movie.mp4") // one audio track

	if a := audioDispositionArgs(src); a != nil {
		t.Errorf("single-audio must be a no-op, got %v", a)
	}
}

func TestExtractAllSubs_EveryTextTrackToSidecar(t *testing.T) {
	root := filepath.Join(t.TempDir(), "lib")
	src := makeMultiTrackMKV(t, root, "show.mkv")
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not in PATH")
	}

	base := filepath.Join(t.TempDir(), "show")
	extractAllSubs(src, base)

	for _, lang := range []string{"eng", "spa"} {
		p := base + "." + lang + ".vtt"
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("missing %s sidecar: %v", lang, err)
		}
		if !strings.HasPrefix(strings.TrimSpace(string(b)), "WEBVTT") {
			t.Errorf("%s sidecar is not valid WebVTT: %.40q", lang, b)
		}
	}
}

// TestE2E_RemuxKeepsAllAudioAndDefaultsEnglish runs the real conversion
// (libx264 source → AV1 MP4) and asserts via ffprobe that BOTH audio tracks
// survive and the English one carries the container default disposition,
// and that both subtitle tracks landed as sidecars.
func TestE2E_RemuxKeepsAllAudioAndDefaultsEnglish(t *testing.T) {
	if testing.Short() {
		t.Skip("ffmpeg-driven; -short")
	}
	if !hasEncoder("libsvtav1") {
		t.Skip("libsvtav1 not in this ffmpeg build")
	}
	chdirTemp(t) // isolate ./cache (kv.json codec mark)
	root := filepath.Join(t.TempDir(), "lib")
	src := makeMultiTrackMKV(t, root, "show.mkv")

	if err := toMP4(src, root); err != nil {
		t.Fatalf("toMP4: %v", err)
	}

	base := filepath.Join(root, library.CleanName("show.mkv"))
	mp4 := base + ".mp4"
	if _, err := os.Stat(mp4); err != nil {
		t.Fatalf("converted mp4 missing: %v", err)
	}
	if n := audioCount(t, mp4); n != 2 {
		t.Fatalf("audio tracks: got %d, want 2 (none may be dropped)", n)
	}

	type stream struct {
		Tags        struct{ Language string } `json:"tags"`
		Disposition struct {
			Default int `json:"default"`
		} `json:"disposition"`
	}
	var probe struct {
		Streams []stream `json:"streams"`
	}
	out, err := exec.Command("ffprobe", "-v", "error",
		"-select_streams", "a",
		"-show_entries", "stream_tags=language:stream_disposition=default",
		"-of", "json", mp4,
	).Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		t.Fatalf("ffprobe json: %v\n%s", err, out)
	}
	for _, s := range probe.Streams {
		lang := strings.ToLower(s.Tags.Language)
		eng := lang == "eng" || lang == "en" || lang == "english"
		if eng && s.Disposition.Default != 1 {
			t.Errorf("English audio (%s) must be default, got default=%d", lang, s.Disposition.Default)
		}
		if !eng && s.Disposition.Default != 0 {
			t.Errorf("non-English audio (%s) must not be default, got default=%d", lang, s.Disposition.Default)
		}
	}

	for _, lang := range []string{"eng", "spa"} {
		if _, err := os.Stat(base + "." + lang + ".vtt"); err != nil {
			t.Errorf("subtitle sidecar .%s.vtt missing after conversion: %v", lang, err)
		}
	}
}

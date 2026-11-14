package media

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"notflix/server/library"
)

// 100px tall — shorter than the smallest ladder rung, to exercise the floor.
func makeTinyMP4(t *testing.T, root, name string) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, name)
	cmd := exec.Command("ffmpeg", "-y", "-v", "error",
		"-f", "lavfi", "-i", "color=c=red:s=256x100:d=2:r=24",
		"-f", "lavfi", "-i", "sine=f=440:d=2",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "48k", "-shortest", "-movflags", "+faststart", out,
	)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tiny fixture failed: %v\n%s", err, b)
	}
	return out
}

func makeSilentAV1MP4(t *testing.T, root, name string) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	if !hasEncoder("libsvtav1") {
		t.Skip("libsvtav1 not in this ffmpeg build")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, name)
	cmd := exec.Command("ffmpeg", "-y", "-v", "error",
		"-f", "lavfi", "-i", "testsrc2=s=320x240:d=2:r=24",
		"-an",
		"-c:v", "libsvtav1", "-preset", "10", "-crf", "50", "-g", "24",
		"-pix_fmt", "yuv420p", "-movflags", "+faststart", out,
	)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("silent av1 fixture failed: %v\n%s", err, b)
	}
	return out
}

func TestLadderFor_NeverEmpty(t *testing.T) {
	if got := ladderFor(hlsProfiles, 0, 90); len(got) != 1 || got[0] != "144p" {
		t.Errorf("sub-144 source ladder = %v, want [144p]", got)
	}
	if got := ladderFor(hlsProfiles, 0, 0); len(got) != len(hlsQualityOrder) {
		t.Errorf("unprobed ladder = %v, want full", got)
	}
	for _, q := range ladderFor(hlsProfiles, 1920, 800) {
		if hlsProfiles[q].h > 800 {
			t.Errorf("ladder included %s (>800px) for an 800px-tall source", q)
		}
	}
	// Portrait clamps by the SHORTER side (1080), not the 1920 height.
	for _, q := range ladderFor(hlsProfiles, 1080, 1920) {
		if hlsProfiles[q].h > 1080 {
			t.Errorf("portrait ladder included %s (>1080) — should clamp to the 1080 short side", q)
		}
	}
	land := ladderFor(hlsProfiles, 1920, 1080)
	port := ladderFor(hlsProfiles, 1080, 1920)
	if len(land) != len(port) {
		t.Errorf("1920x1080 and 1080x1920 must yield the same rung count, got %v vs %v", land, port)
	}
}

// A short side between standard rungs gets a rung of its own: a scope film
// (1920x800) topped out at 720p and a 1280x688 one at 480p before this.
func TestLadderFor_SourceRung(t *testing.T) {
	for _, c := range []struct {
		w, h int
		top  string
	}{
		{1920, 800, "800p"},
		{1280, 688, "688p"},
		{1920, 1037, "1036p"}, // odd → even, yuv420p can't encode it
		{1920, 1080, "1080p"}, // exact rung: nothing added
		{1080, 1920, "1080p"}, // portrait: scale sets the height, so no source rung
		{5120, 2880, "2160p"}, // past the top rung: capped as before
	} {
		got := ladderFor(hlsProfiles, c.w, c.h)
		if top := got[len(got)-1]; top != c.top {
			t.Errorf("%dx%d ladder tops at %s, want %s (%v)", c.w, c.h, top, c.top, got)
		}
		p, ok := rungProfile(hlsProfiles, c.top, c.w, c.h)
		if !ok {
			t.Errorf("%dx%d: %s does not resolve to a profile", c.w, c.h, c.top)
		} else if want := fmt.Sprintf("scale=-2:%d", p.h); p.scale != want || fmt.Sprintf("%dp", p.h) != c.top {
			t.Errorf("%dx%d: %s resolved to %+v", c.w, c.h, c.top, p)
		}
	}
	if _, ok := rungProfile(hlsProfiles, "800p", 1920, 1080); ok {
		t.Error("another title's source rung must not resolve")
	}
	if p, _ := rungProfile(hlsProfiles, "800p", 1920, 800); p.vbr != hlsProfiles["1080p"].vbr {
		t.Errorf("800p should borrow the 1080p caps, got %+v", p)
	}
	if p, _ := rungProfile(hlsProfiles, "800p", 1920, 800); rungBandwidth(&srcInfo{}, "800p", p) != 8_000_000 {
		t.Errorf("800p BANDWIDTH = %d, want the borrowed 1080p cap", rungBandwidth(&srcInfo{}, "800p", p))
	}
}

func TestHLSMaster_TinySourceStillPlayable(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	makeTinyMP4(t, root, "tiny.mp4")

	lib := &library.Library{Roots: []string{root}}
	r := setupRouter(lib)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/hls/master?file=tiny.mp4", nil))

	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, body)
	}
	if n := strings.Count(body, "#EXT-X-STREAM-INF"); n < 1 {
		t.Errorf("tiny source produced an empty (unplayable) master: %s", body)
	}
	if !strings.Contains(body, "/api/hls/playlist?file=tiny.mp4") {
		t.Errorf("master has no playable rung URL: %s", body)
	}
}

func TestHLSMaster_SilentAV1FallsBackToH264(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	makeSilentAV1MP4(t, root, "silent.mp4")

	if err := SetMediaCodec("silent.mp4", CodecAV1); err != nil {
		t.Fatal(err)
	}

	lib := &library.Library{Roots: []string{root}}
	r := setupRouter(lib)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/hls/master?file=silent.mp4", nil))

	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, body)
	}
	// The AV1 demux path's audio rendition would 500 on a silent source and
	// stall hls.js.
	if strings.Contains(body, "av01.") || strings.Contains(body, "&codec=av1") {
		t.Errorf("silent AV1 must fall back to h264, got av1 master: %s", body)
	}
	if strings.Contains(body, "#EXT-X-MEDIA:TYPE=AUDIO") {
		t.Errorf("silent source must not declare an audio rendition: %s", body)
	}
	if !strings.Contains(body, "avc1.") {
		t.Errorf("expected h264 fallback ladder: %s", body)
	}
}

// A file saturn serves (AMOS's wcat) streams by its URL; any other URL is refused.
func TestSource_SaturnOnly(t *testing.T) {
	t.Setenv("SATURN_HOST", "https://hub")
	lib := &library.Library{Roots: []string{t.TempDir()}}
	ok := "https://hub/ring/file?path=%2Ftmp%2Fx.mkv&peer=god&v=%2210-7%22"
	if got := source(lib, ok); got != ok {
		t.Errorf("saturn's file: %q", got)
	}
	for _, bad := range []string{"https://evil/ring/file?path=x", "https://hub.evil/ring/file?x", "https://hub/other?x", "file:///etc/passwd"} {
		if got := source(lib, bad); got != "" {
			t.Errorf("%s: %q, want refused", bad, got)
		}
	}
}

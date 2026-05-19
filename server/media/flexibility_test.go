package media

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"notflix/server/library"
)

// makeTinyMP4 builds a clip SHORTER than the smallest ladder rung (100px tall)
// to exercise the empty-ladder floor.
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

// makeSilentAV1MP4 builds an AV1 clip with NO audio stream. Skips without
// libsvtav1.
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
	// Source shorter than the smallest rung still gets exactly one rung.
	if got := ladderFor(hlsProfiles, 0, 90); len(got) != 1 || got[0] != "144p" {
		t.Errorf("sub-144 source ladder = %v, want [144p]", got)
	}
	// Unprobed (0,0) keeps the full ladder.
	if got := ladderFor(hlsProfiles, 0, 0); len(got) != len(hlsQualityOrder) {
		t.Errorf("unprobed ladder = %v, want full", got)
	}
	// Mid source caps at its height, no upscaling rungs.
	for _, q := range ladderFor(hlsProfiles, 1920, 800) {
		if hlsProfiles[q].h > 800 {
			t.Errorf("ladder included %s (>800px) for an 800px-tall source", q)
		}
	}
	// Portrait 1080x1920: clamp by the SHORTER side (1080), not the 1920
	// height — no over-tall, over-provisioned rungs.
	for _, q := range ladderFor(hlsProfiles, 1080, 1920) {
		if hlsProfiles[q].h > 1080 {
			t.Errorf("portrait ladder included %s (>1080) — should clamp to the 1080 short side", q)
		}
	}
	// Landscape is unchanged: shorter side == height.
	land := ladderFor(hlsProfiles, 1920, 1080)
	port := ladderFor(hlsProfiles, 1080, 1920)
	if len(land) != len(port) {
		t.Errorf("1920x1080 and 1080x1920 must yield the same rung count, got %v vs %v", land, port)
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
	// Must NOT emit the AV1 demux path (whose audio rendition would 500 on a
	// missing audio stream and stall hls.js). h264 ladder, no AUDIO group.
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

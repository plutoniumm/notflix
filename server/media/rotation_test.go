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

// makeRotatedMP4 builds a landscape 640x360 clip with a 90° display matrix —
// i.e. a portrait phone video stored sideways (display = 360x640).
func makeRotatedMP4(t *testing.T, root, name string) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(t.TempDir(), "base.mp4")
	mk := exec.Command("ffmpeg", "-y", "-v", "error",
		"-f", "lavfi", "-i", "testsrc2=s=640x360:d=2:r=24",
		"-f", "lavfi", "-i", "sine=f=440:d=2",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "48k", "-shortest", base)
	if b, err := mk.CombinedOutput(); err != nil {
		t.Fatalf("base fixture: %v\n%s", err, b)
	}
	out := filepath.Join(root, name)
	rot := exec.Command("ffmpeg", "-y", "-v", "error",
		"-display_rotation", "90", "-i", base, "-c", "copy", out)
	if b, err := rot.CombinedOutput(); err != nil {
		t.Fatalf("rotate fixture: %v\n%s", err, b)
	}
	return out
}

func TestGetSrcInfo_RotationCorrectsDisplayDims(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	src := makeRotatedMP4(t, root, "phone.mp4")

	si := getSrcInfo(src)
	if si.vRotation != 90 && si.vRotation != 270 {
		t.Fatalf("rotation = %d, want 90/270", si.vRotation)
	}
	// Coded 640x360 landscape → display 360x640 portrait.
	if si.vWidth != 360 || si.vHeight != 640 {
		t.Errorf("display dims = %dx%d, want 360x640 (rotation-corrected)", si.vWidth, si.vHeight)
	}
	if si.vDAR < 0.55 || si.vDAR > 0.57 {
		t.Errorf("display DAR = %.4f, want ≈0.5625 (9:16 portrait)", si.vDAR)
	}
	// 480-tall rung must be a NARROW portrait frame, not 854 wide.
	if got := scaleFilter(si, 480); got != "scale=270:480:flags=lanczos,setsar=1" {
		t.Errorf("scaleFilter = %q, want scale=270:480:... (portrait)", got)
	}
}

func TestHLSMaster_RotatedSourceAdvertisesPortrait(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	makeRotatedMP4(t, root, "phone.mp4")

	lib := &library.Library{Roots: []string{root}}
	r := setupRouter(lib)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/hls/master?file=phone.mp4", nil))
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, body)
	}
	// Never the coded landscape grid; every rung must be portrait (W < H).
	if strings.Contains(body, "RESOLUTION=640x360") {
		t.Errorf("rotated source advertised coded landscape res: %s", body)
	}
	// Combined with the portrait-ladder clamp: a 360x640 display is capped at
	// its 360 short side, so the top rung is the deanamorphized 360p portrait
	// frame (round(360*0.5625)→even = 204), and there is no 480 rung.
	if !strings.Contains(body, "RESOLUTION=204x360") {
		t.Errorf("expected the deanamorphized portrait 360 rung (204x360): %s", body)
	}
	if strings.Contains(body, "x480,") {
		t.Errorf("portrait short side is 360 — no 480 rung should appear: %s", body)
	}
	for _, ln := range strings.Split(body, "\n") {
		i := strings.Index(ln, "RESOLUTION=")
		if i < 0 {
			continue
		}
		var rw, rh int
		if _, err := fmt.Sscanf(ln[i+len("RESOLUTION="):], "%dx%d", &rw, &rh); err == nil && rw >= rh {
			t.Errorf("non-portrait rung for a rotated source: %q", ln)
		}
	}
}

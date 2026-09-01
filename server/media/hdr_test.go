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

// 10-bit AV1 tagged BT.2020/PQ with a leading keyframe — the copy-eligible
// HDR case.
func make10bitHDR_AV1(t *testing.T, root, name string) string {
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
		"-f", "lavfi", "-i", "sine=f=440:d=2",
		"-c:v", "libsvtav1", "-preset", "10", "-crf", "50", "-g", "24",
		"-pix_fmt", "yuv420p10le",
		"-color_primaries", "bt2020", "-color_trc", "smpte2084", "-colorspace", "bt2020nc",
		"-c:a", "aac", "-b:a", "48k", "-shortest", "-movflags", "+faststart", out,
	)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("10-bit HDR AV1 fixture failed: %v\n%s", err, b)
	}
	return out
}

func TestCanCopyAV1_Accepts10Bit(t *testing.T) {
	ok := &srcInfo{vCodec: "av1", vPixFmt: "yuv420p10le", kfTimes: []float64{0}}
	if !canCopyAV1(ok) {
		t.Errorf("10-bit 4:2:0 AV1 must be copy-eligible (HDR preserved)")
	}
	bad := &srcInfo{vCodec: "av1", vPixFmt: "yuv444p10le", kfTimes: []float64{0}}
	if canCopyAV1(bad) {
		t.Errorf("4:4:4 must NOT be copy-eligible (browser HW decode is 4:2:0)")
	}
}

func TestGetSrcInfo_DetectsHDR(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	src := make10bitHDR_AV1(t, root, "hdr.mp4")

	si := getSrcInfo(src)
	if si.vBitDepth != 10 {
		t.Errorf("bit depth = %d, want 10", si.vBitDepth)
	}
	if !isHDR(si) {
		t.Errorf("isHDR false; transfer=%q primaries=%q (want HDR)", si.vTransfer, si.vPrimaries)
	}
}

func TestHLSMaster_HDR_AV1_AdvertisesTenBit(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	make10bitHDR_AV1(t, root, "hdr.mp4")
	if err := SetMediaCodec("hdr.mp4", CodecAV1); err != nil {
		t.Fatal(err)
	}

	lib := &library.Library{Roots: []string{root}}
	r := setupRouter(lib)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/hls/master?file=hdr.mp4&av1=1", nil))

	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, body)
	}
	// A .08 mislabel makes some players reject the HDR stream.
	if !strings.Contains(body, "av01.") || !strings.Contains(body, "M.10") {
		t.Errorf("HDR AV1 master must advertise a 10-bit av01 codec: %s", body)
	}
	if strings.Contains(body, "M.08") {
		t.Errorf("HDR AV1 master must NOT mislabel as 8-bit: %s", body)
	}
}

func TestE2E_ConvertPreservesHDR(t *testing.T) {
	if testing.Short() {
		t.Skip("ffmpeg-driven; -short")
	}
	if !hasEncoder("libsvtav1") {
		t.Skip("libsvtav1 not in this ffmpeg build")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")

	// Non-mp4 container so convert.go actually re-encodes it.
	src := filepath.Join(root, "movie.mkv")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	mk := exec.Command("ffmpeg", "-y", "-v", "error",
		"-f", "lavfi", "-i", "testsrc2=s=320x240:d=2:r=24",
		"-f", "lavfi", "-i", "sine=f=440:d=2",
		"-c:v", "libsvtav1", "-preset", "10", "-crf", "50", "-g", "24",
		"-pix_fmt", "yuv420p10le",
		"-color_primaries", "bt2020", "-color_trc", "smpte2084", "-colorspace", "bt2020nc",
		"-c:a", "aac", "-b:a", "48k", "-shortest", src)
	if b, err := mk.CombinedOutput(); err != nil {
		t.Fatalf("hdr mkv fixture: %v\n%s", err, b)
	}

	if err := toMP4(src, root); err != nil {
		t.Fatalf("toMP4: %v", err)
	}
	mp4 := filepath.Join(root, library.CleanName("movie.mkv")+".mp4")

	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=codec_name,pix_fmt,color_space",
		"-of", "csv=p=0", mp4).Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	got := strings.TrimSpace(string(out))
	if !strings.Contains(got, "av1") {
		t.Errorf("converted codec not av1: %q", got)
	}
	// libsvtav1 drops the PQ transfer/primaries tags whatever the args, so only
	// the depth and the bt2020 matrix are assertable.
	if !strings.Contains(got, "yuv420p10le") {
		t.Errorf("HDR source must stay 10-bit, got %q (crushed to 8-bit)", got)
	}
	if !strings.Contains(got, "bt2020") {
		t.Errorf("HDR wide-gamut (bt2020) must be preserved, got %q", got)
	}
}

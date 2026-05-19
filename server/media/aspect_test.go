package media

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// makeAnamorphicMP4 builds a 2s clip STORED 640x480 but tagged SAR 4:3, so its
// true display aspect is (640*4)/(480*3) = 16:9 — i.e. anamorphic, the class
// of file (e.g. 2.39:1 scope films) that rendered vertically stretched.
func makeAnamorphicMP4(t *testing.T, root, name string) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, name)
	cmd := exec.Command("ffmpeg", "-y", "-v", "error",
		"-f", "lavfi", "-i", "testsrc2=s=640x480:d=2:r=24",
		"-f", "lavfi", "-i", "sine=f=440:d=2",
		"-vf", "setsar=4/3",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "64k", "-shortest",
		"-movflags", "+faststart", out,
	)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("anamorphic fixture failed: %v\n%s", err, b)
	}
	return out
}

func TestScaleFilter_FallsBackWhenAspectUnknown(t *testing.T) {
	if got := scaleFilter(nil, 720); got != "scale=-2:720" {
		t.Errorf("nil srcInfo must fall back to legacy filter, got %q", got)
	}
	if got := scaleFilter(&srcInfo{}, 480); got != "scale=-2:480" {
		t.Errorf("unknown DAR must fall back to legacy filter, got %q", got)
	}
}

func TestGetSrcInfo_DerivesDisplayAspectFromSAR(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	src := makeAnamorphicMP4(t, root, "scope.mp4")

	si := getSrcInfo(src)
	if si.vWidth != 640 || si.vHeight != 480 {
		t.Fatalf("stored dims = %dx%d, want 640x480", si.vWidth, si.vHeight)
	}
	// 16:9 ≈ 1.7778, NOT the storage 4:3 ≈ 1.3333.
	if si.vDAR < 1.77 || si.vDAR > 1.79 {
		t.Errorf("display aspect = %.4f, want ≈1.7778 (16:9)", si.vDAR)
	}

	// 480p rung must deanamorphize to ~854 wide (16:9) with square pixels,
	// not the 640 storage width.
	if got, want := scaleFilter(si, 480), "scale=854:480:flags=lanczos,setsar=1"; got != want {
		t.Errorf("scaleFilter = %q, want %q", got, want)
	}
	if w := rungWidth(si, 640, 480, 480); w != 854 {
		t.Errorf("rungWidth = %d, want 854 (display width, not storage 640)", w)
	}
}

// TestE2E_AnamorphicSegmentIsDeanamorphized runs the real segment encoder and
// asserts the produced TS is square-pixel at the display width — the actual
// fix for the "stretched vertically" bug (players ignore in-stream SAR on
// hls.js-transmuxed MPEG-TS).
func TestE2E_AnamorphicSegmentIsDeanamorphized(t *testing.T) {
	if testing.Short() {
		t.Skip("ffmpeg-driven; -short")
	}
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	src := makeAnamorphicMP4(t, root, "scope.mp4")

	seg := filepath.Join(t.TempDir(), "seg.ts")
	if err := generateSegment(src, seg, 0.0, 1.0, hlsProfiles["480p"], 0, segMuxed, false); err != nil {
		t.Fatalf("generateSegment: %v", err)
	}

	out, err := exec.Command("ffprobe", "-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height,sample_aspect_ratio",
		"-of", "csv=p=0", seg,
	).Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	// First non-empty record: "W,H,SAR". Square pixels report SAR as "1:1"
	// or, in MPEG-TS where it's simply unspecified, "N/A" — both mean the
	// coded grid IS the display grid (no anamorphic stretch).
	var line string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			line = l
			break
		}
	}
	f := strings.Split(line, ",")
	if len(f) != 3 || f[0] != "854" || f[1] != "480" {
		t.Fatalf("segment dims = %q, want 854x480 (deanamorphized to display width)", line)
	}
	if sar := f[2]; sar != "1:1" && sar != "N/A" {
		t.Errorf("segment SAR = %q, want square (1:1 / N/A) — still anamorphic", sar)
	}
}

package media

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Stored 640x480 but tagged SAR 4:3, so the display aspect is
// (640*4)/(480*3) = 16:9 — the anamorphic class that rendered stretched.
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

	// 854 = the 16:9 display width at 480p, not the 640 storage width.
	if got, want := scaleFilter(si, 480), "scale=854:480:flags=lanczos,setsar=1"; got != want {
		t.Errorf("scaleFilter = %q, want %q", got, want)
	}
	if w := rungWidth(si, 640, 480, 480); w != 854 {
		t.Errorf("rungWidth = %d, want 854 (display width, not storage 640)", w)
	}
}

// hls.js-transmuxed MPEG-TS ignores in-stream SAR, so the segment itself has
// to come out square-pixel at the display width.
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
	// MPEG-TS leaves SAR unspecified, so square pixels read "1:1" or "N/A".
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

// AVERAGE-BANDWIDTH used to be set to the PEAK segment rate: Obsession (4K AV1)
// averages 1.57 Mbps but holds one real 6.7s / 26 MB segment at 31.4 Mbps, so
// the player was told to expect 31 Mbps sustained.
func TestSegmentBitratesPeakVersusAverage(t *testing.T) {
	// 4 segments of 1s. Three are 125 kB (1 Mbps), one is 1.25 MB (10 Mbps).
	bounds := []float64{0, 1, 2, 3, 4}
	var pkts []vpkt
	for i, size := range []int{125_000, 125_000, 1_250_000, 125_000} {
		pkts = append(pkts, vpkt{pts: float64(i) + 0.1, size: size, key: true})
	}

	per, peak, avg := segmentBitrates(pkts, bounds)
	if len(per) != 4 {
		t.Fatalf("want 4 per-segment rates, got %d", len(per))
	}
	if peak != 10_000_000 {
		t.Errorf("peak = %d, want 10000000 (the fat segment)", peak)
	}
	// (125k+125k+1.25M+125k) bytes * 8 / 4s = 3.25 Mbps
	if avg != 3_250_000 {
		t.Errorf("avg = %d, want 3250000", avg)
	}
	if avg >= peak {
		t.Error("average must be below the peak when one segment is fat")
	}
}

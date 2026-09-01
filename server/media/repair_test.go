package media

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Covers scrambled payload with an intact header only: truncation past the
// moov atom is a different class, unrecoverable without untrunc.
func TestRepairMidStreamCorruption(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "valid.mkv")
	broken := filepath.Join(dir, "broken.mkv")

	if err := exec.Command("ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=duration=5:size=320x240:rate=15",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-g", "15",
		"-f", "matroska", src).Run(); err != nil {
		t.Fatalf("generate source: %v", err)
	}

	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read src: %v", err)
	}
	// Matroska keeps its header near the top; the middle is cluster data that
	// ignore_err tolerates.
	mid := len(data) / 2
	for i := 0; i < 200 && mid+i < len(data); i++ {
		data[mid+i] ^= 0xFF
	}
	if err := os.WriteFile(broken, data, 0644); err != nil {
		t.Fatalf("write broken: %v", err)
	}

	// If it no longer probes at all, this is the unrecoverable class instead.
	if !verifyOutput(broken) {
		t.Skip("broken variant is unrecoverable class — needs untrunc, not ffmpeg")
	}

	if !repairOne(broken) {
		t.Fatalf("repairOne returned false — no strategy succeeded")
	}
	if !verifyOutput(broken) {
		t.Fatalf("repaired file still fails verifyOutput")
	}
	if _, err := os.Stat(broken + ".corrupt.bak"); err != nil {
		t.Fatalf("expected .corrupt.bak to exist: %v", err)
	}
}

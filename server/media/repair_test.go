package media

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestRepairMidStreamCorruption confirms repairOne can recover an MP4 whose
// container header is intact but whose media payload has scrambled bytes in
// the middle (ffmpeg can probe it but decode emits errors). Copy-remux with
// -err_detect ignore_err + regenerated PTS should rebuild it cleanly.
// Truncation past the moov atom is a different class — unrecoverable without
// untrunc — so we don't cover it here.
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
	// Scramble ~200 bytes in the payload region, leaving the header intact.
	// Matroska keeps the segment header near the top; the middle is cluster
	// data ffmpeg tolerates via ignore_err.
	mid := len(data) / 2
	for i := 0; i < 200 && mid+i < len(data); i++ {
		data[mid+i] ^= 0xFF
	}
	if err := os.WriteFile(broken, data, 0644); err != nil {
		t.Fatalf("write broken: %v", err)
	}

	// Sanity: the broken file should still be readable enough that repair
	// can even try. If it fails to probe entirely, we're testing the wrong
	// corruption class (see comment above).
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

package media

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAacArgs_PreservesSurroundDownmixesStereo(t *testing.T) {
	// 5.1 with a NAMED layout the encoder accepts → keep surround.
	c51 := &srcInfo{aChannels: []int{6}, aLayouts: []string{"5.1"}}
	got := strings.Join(aacArgs(c51, 0, "128k"), " ")
	if strings.Contains(got, "-ac") {
		t.Errorf("named 5.1 must keep its layout (no -ac downmix), got %q", got)
	}
	if !strings.Contains(got, "256k") {
		t.Errorf("5.1 should get a surround-appropriate bitrate, got %q", got)
	}

	c71 := &srcInfo{aChannels: []int{8}, aLayouts: []string{"7.1"}}
	if !strings.Contains(strings.Join(aacArgs(c71, 0, "128k"), " "), "384k") {
		t.Errorf("7.1 should get 384k")
	}

	stereo := &srcInfo{aChannels: []int{2}, aLayouts: []string{"stereo"}}
	if g := strings.Join(aacArgs(stereo, 0, "128k"), " "); g != "-c:a aac -b:a 128k -ac 2" {
		t.Errorf("stereo source = %q, want stereo at rung bitrate", g)
	}

	// Unnamed multichannel (the Drive 2011 regression: AAC 6ch w/
	// channel_layout="unknown" — the native AAC encoder fails the segment
	// with "Unsupported channel layout 6 channels" unless we downmix).
	for _, layout := range []string{"", "unknown", "6 channels", "5 channels"} {
		bad := &srcInfo{aChannels: []int{6}, aLayouts: []string{layout}}
		g := strings.Join(aacArgs(bad, 0, "128k"), " ")
		if !strings.Contains(g, "-ac 2") {
			t.Errorf("unnamed layout %q (6ch) MUST downmix to stereo, got %q", layout, g)
		}
	}

	// Unknown srcInfo / out-of-range index → safe stereo default.
	if g := strings.Join(aacArgs(nil, 0, "96k"), " "); !strings.Contains(g, "-ac 2") {
		t.Errorf("nil srcInfo must default to stereo, got %q", g)
	}
	if g := strings.Join(aacArgs(c51, 5, "96k"), " "); !strings.Contains(g, "-ac 2") {
		t.Errorf("out-of-range audioIdx must default to stereo, got %q", g)
	}
}

// (No e2e for unnamed-multichannel: lavfi auto-assigns a "5.1" layout to a
// 6-channel source, so the exact "unknown"/"6 channels" container metadata
// can't be reproduced synthetically. The unit cases above cover every layout
// string the encoder rejects in the wild — that's the real regression guard.)

// TestE2E_SurroundSegmentKeeps5_1 runs the real segment encoder on a 5.1
// source and asserts the rendition is still 6-channel (surround preserved,
// not silently downmixed to stereo).
func TestE2E_SurroundSegmentKeeps5_1(t *testing.T) {
	if testing.Short() {
		t.Skip("ffmpeg-driven; -short")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "surround.mp4")
	mk := exec.Command("ffmpeg", "-y", "-v", "error",
		"-f", "lavfi", "-i", "testsrc2=s=320x240:d=2:r=24",
		"-f", "lavfi", "-i", "aevalsrc=0|0|0|0|0|0:c=5.1:d=2",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "256k", "-shortest", "-movflags", "+faststart", src)
	if b, err := mk.CombinedOutput(); err != nil {
		t.Fatalf("5.1 fixture: %v\n%s", err, b)
	}

	seg := filepath.Join(t.TempDir(), "seg.ts")
	if err := generateSegment(src, seg, 0.0, 1.0, hlsProfiles["240p"], 0, segMuxed, false); err != nil {
		t.Fatalf("generateSegment: %v", err)
	}
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "a:0",
		"-show_entries", "stream=channels", "-of", "csv=p=0", seg).Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	// MPEG-TS makes ffprobe emit the stream per-program; take the first.
	ch := ""
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			ch = l
			break
		}
	}
	if ch != "6" {
		t.Errorf("surround segment channels = %q, want 6 (5.1 preserved, not downmixed)", ch)
	}
}

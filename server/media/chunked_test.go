package media

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestPlanChunks(t *testing.T) {
	for _, c := range []struct {
		name string
		cuts []float64
		dur  float64
		want []float64
	}{
		{"no cuts, short film", nil, 40, []float64{0}},
		{"no cuts, long film splits evenly", nil, 150, []float64{0, 50, 100}},
		{"cuts closer than chunkMin are skipped", []float64{3, 12, 15, 40}, 70, []float64{0, 12, 40}},
		{"a cut-free stretch past chunkMax is split evenly", []float64{130}, 150, []float64{0, 130.0 / 3, 260.0 / 3, 130}},
		{"a tail shorter than chunkMin joins the chunk before", []float64{20, 45}, 50, []float64{0, 20}},
		{"unsorted cuts", []float64{45, 20}, 70, []float64{0, 20, 45}},
	} {
		got := planChunks(c.cuts, c.dur)
		if len(got) != len(c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if d := got[i] - c.want[i]; d > 1e-9 || d < -1e-9 {
				t.Errorf("%s: %v, want %v", c.name, got, c.want)
				break
			}
		}
		for i := 1; i < len(got); i++ {
			if d := got[i] - got[i-1]; d < chunkMin || d > chunkMax {
				t.Errorf("%s: chunk %d is %.1fs, outside %v..%v", c.name, i-1, d, chunkMin, chunkMax)
			}
		}
	}
}

func TestCRFFor(t *testing.T) {
	for _, c := range []struct {
		name   string
		lo, hi [2]float64
		target float64
		want   int
	}{
		{"between the probes", [2]float64{30, 96}, [2]float64{40, 90}, 93, 35},
		{"rounds toward quality", [2]float64{30, 96}, [2]float64{40, 90}, 94, 33},
		{"both above: extrapolate past the cheap probe", [2]float64{30, 99}, [2]float64{40, 97}, 93, 46},
		{"both below: extrapolate past the dear probe", [2]float64{30, 85}, [2]float64{40, 75}, 93, 24},
		{"flat and above: as far as allowed", [2]float64{30, 99}, [2]float64{40, 99.5}, 93, crfMax},
		{"flat and below: the dear probe", [2]float64{30, 80}, [2]float64{40, 80}, 93, 30},
	} {
		if got := crfFor(c.lo, c.hi, c.target); got != c.want {
			t.Errorf("%s: crfFor(%v, %v, %v) = %d, want %d", c.name, c.lo, c.hi, c.target, got, c.want)
		}
	}
}

func frameTimes(t *testing.T, path string, keyOnly bool) []float64 {
	t.Helper()
	args := []string{"-v", "error", "-select_streams", "v:0", "-show_entries", "frame=pts_time", "-of", "csv=p=0"}
	if keyOnly {
		args = append(args, "-skip_frame", "nokey")
	}
	out, err := exec.Command("ffprobe", append(args, path)...).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	var ts []float64
	for _, f := range strings.Fields(strings.ReplaceAll(string(out), ",", " ")) {
		if v, err := strconv.ParseFloat(f, 64); err == nil {
			ts = append(ts, v)
		}
	}
	return ts
}

func streamStart(t *testing.T, path string) float64 {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=start_time", "-of", "csv=p=0", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	v, _ := strconv.ParseFloat(strings.TrimSpace(strings.Trim(string(out), ",\n")), 64)
	return v
}

// Three 12s scenes a cut apart — flat grey, heavy noise, static bars — with
// audio and the B-frame start offset real releases carry. The conversion must
// keep every frame exactly once, at an unbroken cadence across chunk joins,
// start a keyframe at each chunk, keep the video's offset (A/V sync), and give
// the flat scene a different CRF from the noisy one.
func TestE2E_ChunkedConversion(t *testing.T) {
	if testing.Short() {
		t.Skip("ffmpeg-driven; -short")
	}
	if pickAV1() != "libsvtav1" {
		t.Skip("no libsvtav1")
	}
	dir := chdirTemp(t)
	src := filepath.Join(dir, "lib", "scenes.mkv")
	os.MkdirAll(filepath.Dir(src), 0o755)
	if out, err := exec.Command("ffmpeg", "-v", "error", "-y",
		"-f", "lavfi", "-i", "color=c=0x6080a0:s=320x180:r=24000/1001:d=12",
		"-f", "lavfi", "-i", "testsrc2=s=320x180:r=24000/1001:d=12",
		"-f", "lavfi", "-i", "smptebars=s=320x180:r=24000/1001:d=12",
		"-f", "lavfi", "-i", "sine=f=440:d=36",
		"-filter_complex", "[1]noise=alls=60:allf=t[n];[0][n][2]concat=n=3:v=1:a=0[v]",
		"-map", "[v]", "-map", "3:a", "-c:v", "libx264", "-bf", "2", "-g", "48", "-pix_fmt", "yuv420p",
		"-c:a", "aac", src).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v\n%s", err, out)
	}
	dur := duration(src)

	paths, _, err := encodeChunked(src, dur, "scenes")
	if err != nil {
		t.Fatalf("encodeChunked: %v", err)
	}
	if len(paths) != 3 {
		t.Fatalf("got %d chunks %v, want one per scene", len(paths), paths)
	}
	crf := regexp.MustCompile(`crf(\d+)`)
	flat, noisy := crf.FindStringSubmatch(paths[0])[1], crf.FindStringSubmatch(paths[1])[1]
	if flat == noisy {
		t.Errorf("flat and noisy scenes both got crf %s", flat)
	}
	t.Logf("chunk CRFs: %v", paths)

	dst := filepath.Join(dir, "lib", "scenes.mp4")
	_, ac := codecs(src)
	if err := remuxChunked(src, dst, dur, "scenes", ac); err != nil {
		t.Fatalf("remuxChunked: %v", err)
	}
	if _, err := os.Stat(chunkDir(src)); !os.IsNotExist(err) {
		t.Error("chunk dir left behind after a successful mux")
	}

	in, out := frameTimes(t, src, false), frameTimes(t, dst, false)
	if len(out) != len(in) {
		t.Fatalf("output has %d frames, source %d", len(out), len(in))
	}
	step := 1001.0 / 24000
	for i := 1; i < len(out); i++ {
		if d := out[i] - out[i-1]; d < step*0.5 || d > step*1.5 {
			t.Fatalf("frame %d at %.4f follows %.4f: a gap or duplicate at a join", i, out[i], out[i-1])
		}
	}
	keys := frameTimes(t, dst, true)
	for _, s := range []float64{12, 24} {
		found := false
		for _, k := range keys {
			if k > s-step && k < s+step*2 {
				found = true
			}
		}
		if !found {
			t.Errorf("no keyframe at the chunk starting %.0fs (keys %v)", s, keys)
		}
	}
	if a, b := streamStart(t, src), streamStart(t, dst); a-b > step/2 || b-a > step/2 {
		t.Errorf("video start moved %.4f → %.4f: audio would drift by the difference", a, b)
	}
}

package media

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"notflix/server/library"
)

func TestBestRun(t *testing.T) {
	good := make([]bool, 100)
	for i := 20; i < 60; i++ {
		good[i] = true
	}
	good[35], good[36] = false, false // small dropout, within gap

	s, e, sc := bestRun(good, 5, 30)
	if sc <= 0 || s != 20 || e != 60 {
		t.Fatalf("got [%d,%d) score=%v, want [20,60)", s, e, sc)
	}

	if _, _, sc := bestRun(good, 5, 50); sc > 0 {
		t.Error("run shorter than minLen should not match")
	}

	sparse := make([]bool, 100)
	for i := 0; i < 100; i += 4 {
		sparse[i] = true
	}
	if _, _, sc := bestRun(sparse, 5, 30); sc > 0 {
		t.Error("sparse run should fail density check")
	}
}

func TestMatchFingerprints(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	nA := int(60 * fpFPS)
	nB := int(50 * fpFPS)
	a := make([]uint32, nA)
	b := make([]uint32, nB)
	for i := range a {
		a[i] = rng.Uint32()
	}
	for i := range b {
		b[i] = rng.Uint32()
	}

	// Shared 20s segment at a[10s:30s] == b[5s:25s], with 3 bits of per-frame
	// noise to mimic codec differences.
	planted := int(20 * fpFPS)
	aOff, bOff := int(10*fpFPS), int(5*fpFPS)
	for i := 0; i < planted; i++ {
		w := a[aOff+i]
		w ^= 1 << rng.Intn(32)
		w ^= 1 << rng.Intn(32)
		w ^= 1 << rng.Intn(32)
		b[bOff+i] = w
	}

	s, e, ok := matchFingerprints(a, b, 15)
	if !ok {
		t.Fatal("planted segment not found")
	}
	tol := int(fpFPS) // 1s
	if s < aOff-tol || s > aOff+tol || e < aOff+planted-tol || e > aOff+planted+tol {
		t.Fatalf("got frames [%d,%d), want ≈[%d,%d)", s, e, aOff, aOff+planted)
	}

	a2 := make([]uint32, nA)
	b2 := make([]uint32, nB)
	for i := range a2 {
		a2[i] = rng.Uint32()
	}
	for i := range b2 {
		b2[i] = rng.Uint32()
	}
	for i := 0; i < int(9*fpFPS); i++ { // 9s < 15s
		b2[bOff+i] = a2[aOff+i]
	}
	if _, _, ok := matchFingerprints(a2, b2, 15); ok {
		t.Error("segment below minimum length should not match")
	}
}

// Each segment is a complete lavfi source including its d= duration; segments
// shared across files are what makes the intro/outro matchable.
func makeEpisode(t *testing.T, path string, segments ...string) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not in PATH")
	}

	var filter, labels string
	for i, src := range segments {
		filter += fmt.Sprintf("%s,aformat=sample_rates=44100:channel_layouts=mono[s%d];", src, i)
		labels += fmt.Sprintf("[s%d]", i)
	}
	filter += fmt.Sprintf("%sconcat=n=%d:v=0:a=1[out]", labels, len(segments))

	cmd := exec.Command("ffmpeg", "-y", "-v", "error",
		"-filter_complex", filter, "-map", "[out]",
		"-c:a", "aac", "-b:a", "96k", path)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg fixture failed: %v\n%s", err, b)
	}

	// DetectSkips treats files written <30s ago as busy; backdate.
	old := time.Now().Add(-5 * time.Minute)
	os.Chtimes(path, old, old)
}

// Keyed by basename so a moved episode keeps its marks; legacy dir-qualified
// keys must still read.
func TestSkipVal_SurvivesMove(t *testing.T) {
	chdirTemp(t)

	if err := library.KVSetValue(skipKey("Show/ep1.mp4"), map[string]any{"v": 1}); err != nil {
		t.Fatal(err)
	}
	if skipVal("Sorted/S01/ep1.mp4") == nil {
		t.Error("skip mark lost when the episode moved dirs")
	}

	if err := library.KVSetValue("skip:"+library.Hash("Show/ep2.mp4"), map[string]any{"v": 1}); err != nil {
		t.Fatal(err)
	}
	if skipVal("Show/ep2.mp4") == nil {
		t.Error("legacy dir-qualified mark no longer readable")
	}
}

func TestE2E_DetectSkips(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e ffmpeg test")
	}
	chdirTemp(t)

	root := filepath.Join(t.TempDir(), "root")
	show := filepath.Join(root, "Show")
	if err := os.MkdirAll(show, 0755); err != nil {
		t.Fatal(err)
	}

	// Shared, spectrally moving themes; noise segments differ per episode.
	intro := "aevalsrc='0.5*sin(2*PI*(600+300*sin(2*PI*0.25*t))*t)':d=25"
	outro := "aevalsrc='0.5*sin(2*PI*(1200+500*sin(2*PI*0.7*t))*t)':d=20"
	noise := func(seed int, d int) string {
		return fmt.Sprintf("anoisesrc=c=pink:s=%d:a=0.4:d=%d", seed, d)
	}

	// ep1: 10s noise | 25s intro | 65s noise | 20s outro  (120s)
	// ep2: 30s noise | 25s intro | 45s noise | 20s outro  (120s)
	makeEpisode(t, filepath.Join(show, "ep1.mp4"),
		noise(1, 10), intro, noise(2, 65), outro)
	makeEpisode(t, filepath.Join(show, "ep2.mp4"),
		noise(3, 30), intro, noise(4, 45), outro)

	lib := &library.Library{Roots: []string{root}}
	DetectSkips(lib)

	get := func(rel string) (intro, outro []float64) {
		v, ok := library.KVGetValue(skipKey(rel)).(map[string]any)
		if !ok {
			t.Fatalf("%s: no skip entry", rel)
		}
		conv := func(x any) []float64 {
			arr, _ := x.([]any)
			out := make([]float64, 0, len(arr))
			for _, n := range arr {
				f, _ := n.(float64)
				out = append(out, f)
			}
			return out
		}
		return conv(v["intro"]), conv(v["outro"])
	}

	near := func(got, want, tol float64) bool { return got > want-tol && got < want+tol }

	in1, out1 := get("Show/ep1.mp4")
	if len(in1) != 2 || !near(in1[0], 10, 4) || !near(in1[1], 35, 4) {
		t.Errorf("ep1 intro = %v, want ≈[10,35]", in1)
	}
	if len(out1) != 2 || !near(out1[0], 100, 4) || !near(out1[1], 120, 4) {
		t.Errorf("ep1 outro = %v, want ≈[100,120]", out1)
	}

	in2, out2 := get("Show/ep2.mp4")
	if len(in2) != 2 || !near(in2[0], 30, 4) || !near(in2[1], 55, 4) {
		t.Errorf("ep2 intro = %v, want ≈[30,55]", in2)
	}
	if len(out2) != 2 || !near(out2[0], 100, 4) || !near(out2[1], 120, 4) {
		t.Errorf("ep2 outro = %v, want ≈[100,120]", out2)
	}
}

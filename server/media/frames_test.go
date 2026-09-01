package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"notflix/server/library"
)

const tEps = 1e-9

func TestShotTimes_CentredAndOrdered(t *testing.T) {
	times, at, err := shotTimes(10, 24, 600)
	if err != nil {
		t.Fatal(err)
	}
	if len(times) != shotCount {
		t.Fatalf("got %d times, want %d", len(times), shotCount)
	}
	if at != 2 {
		t.Errorf("at = %d, want 2", at)
	}

	step := 1.0 / 24
	for i, want := range []float64{10 - 2*step, 10 - step, 10, 10 + step, 10 + 2*step} {
		if math.Abs(times[i]-want) > tEps {
			t.Errorf("times[%d] = %.9f, want %.9f", i, times[i], want)
		}
		if i > 0 && times[i] <= times[i-1] {
			t.Errorf("times not ascending at %d: %v", i, times)
		}
	}
}

// A frame step is 1/fps, never 1/round(fps): at 23.976 the two differ by 1.7ms
// per frame, which is a whole frame after 25 of them.
func TestShotTimes_FractionalRates(t *testing.T) {
	for _, fps := range []float64{24000.0 / 1001, 30000.0 / 1001, 60000.0 / 1001, 25} {
		times, at, err := shotTimes(300, fps, 3600)
		if err != nil {
			t.Fatalf("fps %.4f: %v", fps, err)
		}
		if at != 2 || math.Abs(times[at]-300) > tEps {
			t.Errorf("fps %.4f: at=%d times[at]=%.9f, want 2 / 300", fps, at, times[at])
		}
		for i := 1; i < len(times); i++ {
			if d := times[i] - times[i-1]; math.Abs(d-1/fps) > tEps {
				t.Errorf("fps %.4f: step %d = %.9f, want %.9f", fps, i, d, 1/fps)
			}
		}
	}
}

func TestShotTimes_ClampsAtHead(t *testing.T) {
	step := 1.0 / 24

	for _, ts := range []float64{0, 0.01, step, 1.5 * step} {
		times, at, err := shotTimes(ts, 24, 600)
		if err != nil {
			t.Fatalf("ts %.4f: %v", ts, err)
		}
		if times[0] < 0 {
			t.Errorf("ts %.4f: negative seek %.6f", ts, times[0])
		}
		// The window slides instead of shrinking, so `at` tracks the request.
		if got := times[at]; math.Abs(got-ts) > step {
			t.Errorf("ts %.4f: times[at] = %.6f, more than a frame away", ts, got)
		}
	}

	times, at, _ := shotTimes(0, 24, 600)
	if times[0] != 0 || at != 0 {
		t.Errorf("ts=0 → times[0]=%.6f at=%d, want 0 / 0", times[0], at)
	}
}

func TestShotTimes_ClampsAtTail(t *testing.T) {
	const dur, fps = 600.0, 24.0
	step := 1 / fps

	for _, ts := range []float64{dur, dur - 0.001, dur - step} {
		times, at, err := shotTimes(ts, fps, dur)
		if err != nil {
			t.Fatalf("ts %.4f: %v", ts, err)
		}
		if last := times[len(times)-1]; last > dur-step+tEps {
			t.Errorf("ts %.4f: last frame %.6f past EOF (dur %.1f, step %.6f)", ts, last, dur, step)
		}
		if at != shotCount-1 {
			t.Errorf("ts %.4f: at = %d, want %d", ts, at, shotCount-1)
		}
	}
}

// Fewer frames in the file than in a strip: still no negative seek, still
// ascending, and `at` stays inside the strip.
func TestShotTimes_ShorterThanStrip(t *testing.T) {
	times, at, err := shotTimes(0.05, 24, 0.1)
	if err != nil {
		t.Fatal(err)
	}
	if times[0] != 0 {
		t.Errorf("times[0] = %.6f, want 0", times[0])
	}
	if at < 0 || at >= shotCount {
		t.Errorf("at = %d out of range", at)
	}
}

func TestShotTimes_Rejects(t *testing.T) {
	cases := []struct {
		name         string
		ts, fps, dur float64
	}{
		{"no frame rate", 10, 0, 600},
		{"negative rate", 10, -24, 600},
		{"infinite rate", 10, math.Inf(1), 600},
		{"negative ts", -1, 24, 600},
		{"past EOF", 601, 24, 600},
		{"NaN ts", math.NaN(), 24, 600},
	}

	for _, tc := range cases {
		if _, _, err := shotTimes(tc.ts, tc.fps, tc.dur); err == nil {
			t.Errorf("%s: want error, got none", tc.name)
		}
	}
}

func TestDeanamorph(t *testing.T) {
	if got := deanamorph(shotGeom{rawW: 1920, rawH: 1080, w: 1920, h: 1080}); got != "" {
		t.Errorf("square pixels should skip the filter, got %q", got)
	}

	want := "scale=960:480:flags=lanczos,setsar=1"
	if got := deanamorph(shotGeom{rawW: 720, rawH: 480, w: 960, h: 480}); got != want {
		t.Errorf("anamorphic filter = %q, want %q", got, want)
	}
}

func TestHMS(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want string
	}{
		{12.417, "00m12.417s"},
		{3723.5, "1h02m03.500s"},
	} {
		if got := hms(tc.in); got != tc.want {
			t.Errorf("hms(%.3f) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestShotName(t *testing.T) {
	if got := shotName(`../a/b"c`); got != "..-a-b-c" {
		t.Errorf("shotName = %q", got)
	}
}

// ---- ffmpeg-driven ----

func makeClip(t *testing.T, root, name string, args ...string) string {
	t.Helper()
	if testing.Short() {
		t.Skip("short mode")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(root, name)
	cmd := exec.Command("ffmpeg", append([]string{"-y", "-v", "error"}, append(args, out)...)...)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg fixture failed: %v\n%s", err, b)
	}

	return out
}

// testsrc2 draws a different picture every frame, which is what makes "five
// copies of one frame" — the failure a wrong seek produces — detectable.
func makeShotClip(t *testing.T, root, name string, fps string) string {
	return makeClip(t, root, name,
		"-f", "lavfi", "-i", "testsrc2=s=320x240:d=6:r="+fps,
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-g", "48")
}

func framesRouter(lib *library.Library) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/frames", func(c *gin.Context) { Frames(c, lib) })
	r.GET("/api/frames/img", FrameImage)

	return r
}

func getShot(t *testing.T, r *gin.Engine, url string) shotReply {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s → %d: %s", url, w.Code, w.Body.String())
	}

	var reply shotReply
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatalf("decode %s: %v (%s)", url, err, w.Body.String())
	}

	return reply
}

func hit(t *testing.T, r *gin.Engine, url string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", url, nil))

	return w
}

func pngSize(t *testing.T, b []byte) (int, int) {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	return cfg.Width, cfg.Height
}

func TestE2E_Frames_FiveDistinctAtSourceSize(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	makeShotClip(t, root, "clip.mp4", "24")

	lib := &library.Library{Roots: []string{root}}
	r := framesRouter(lib)

	reply := getShot(t, r, "/api/frames?file=clip.mp4&t=3")
	if reply.At != 2 {
		t.Errorf("at = %d, want 2", reply.At)
	}
	if reply.W != 320 || reply.H != 240 {
		t.Errorf("reported size %dx%d, want 320x240", reply.W, reply.H)
	}
	if math.Abs(reply.FPS-24) > 1e-6 {
		t.Errorf("fps = %v, want 24", reply.FPS)
	}

	seen := map[string]int{}
	for i, f := range reply.Frames {
		w := hit(t, r, f.URL)
		if w.Code != http.StatusOK {
			t.Fatalf("frame %d → %d", i, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "image/png" {
			t.Errorf("frame %d content-type %q", i, ct)
		}

		if pw, ph := pngSize(t, w.Body.Bytes()); pw != 320 || ph != 240 {
			t.Errorf("frame %d is %dx%d, want 320x240", i, pw, ph)
		}

		sum := fmt.Sprintf("%x", sha256.Sum256(w.Body.Bytes()))
		if prev, dup := seen[sum]; dup {
			t.Fatalf("frame %d is a duplicate of frame %d — the seek is wrong", i, prev)
		}
		seen[sum] = i
	}
}

// The strip must be the RIGHT five frames, not merely five different ones:
// ground truth comes from selecting by frame NUMBER, which no seek arithmetic
// is involved in.
func TestE2E_Frames_MatchFrameNumbers(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	src := makeShotClip(t, root, "clip.mp4", "24")

	lib := &library.Library{Roots: []string{root}}
	r := framesRouter(lib)

	reply := getShot(t, r, "/api/frames?file=clip.mp4&t=3")

	n0 := int(math.Round(reply.Frames[0].T * reply.FPS))
	ref := t.TempDir()
	cmd := exec.Command("ffmpeg", "-y", "-v", "error", "-i", src,
		"-vf", fmt.Sprintf("select='between(n,%d,%d)'", n0, n0+shotCount-1),
		"-fps_mode", "passthrough", filepath.Join(ref, "%d.png"))
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("reference extract failed: %v\n%s", err, b)
	}

	for i, f := range reply.Frames {
		want, err := os.ReadFile(filepath.Join(ref, fmt.Sprintf("%d.png", i+1)))
		if err != nil {
			t.Fatal(err)
		}

		got := hit(t, r, f.URL).Body.Bytes()
		if sha256.Sum256(got) != sha256.Sum256(want) {
			t.Errorf("frame %d (t=%.6f) is not source frame %d", i, f.T, n0+i)
		}
	}
}

func TestE2E_Frames_NTSCRate(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	makeShotClip(t, root, "clip.mp4", "24000/1001")

	lib := &library.Library{Roots: []string{root}}
	r := framesRouter(lib)

	reply := getShot(t, r, "/api/frames?file=clip.mp4&t=3")
	if math.Abs(reply.FPS-24000.0/1001) > 1e-6 {
		t.Fatalf("fps = %v, want 23.976", reply.FPS)
	}

	seen := map[string]bool{}
	for i, f := range reply.Frames {
		sum := fmt.Sprintf("%x", sha256.Sum256(hit(t, r, f.URL).Body.Bytes()))
		if seen[sum] {
			t.Fatalf("frame %d duplicated on a 23.976 source", i)
		}
		seen[sum] = true
	}
}

// A raw grab of an anamorphic source is squashed; the still has to arrive on
// the display grid the video plays on.
func TestE2E_Frames_Deanamorphizes(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	makeClip(t, root, "ana.mp4",
		"-f", "lavfi", "-i", "testsrc2=s=720x480:d=3:r=25",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-vf", "setsar=4/3")

	lib := &library.Library{Roots: []string{root}}
	r := framesRouter(lib)

	reply := getShot(t, r, "/api/frames?file=ana.mp4&t=1.5")
	if reply.W != 960 || reply.H != 480 {
		t.Fatalf("reported %dx%d, want 960x480 (720x480 at SAR 4:3)", reply.W, reply.H)
	}

	if w, h := pngSize(t, hit(t, r, reply.Frames[reply.At].URL).Body.Bytes()); w != 960 || h != 480 {
		t.Errorf("still is %dx%d, want 960x480", w, h)
	}
}

// ffmpeg autorotates on decode, so the still is portrait without a filter —
// this pins that the reported geometry agrees with what actually comes out.
func TestE2E_Frames_Rotated(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	flat := makeClip(t, root, "flat.mp4",
		"-f", "lavfi", "-i", "testsrc2=s=640x360:d=3:r=25",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p")

	rot := filepath.Join(root, "rot.mp4")
	cmd := exec.Command("ffmpeg", "-y", "-v", "error", "-display_rotation", "90",
		"-i", flat, "-c", "copy", "-movflags", "+faststart", rot)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("this ffmpeg cannot write a display matrix: %v\n%s", err, b)
	}

	lib := &library.Library{Roots: []string{root}}
	r := framesRouter(lib)

	reply := getShot(t, r, "/api/frames?file=rot.mp4&t=1.5")
	if reply.W != 360 || reply.H != 640 {
		t.Fatalf("reported %dx%d, want 360x640", reply.W, reply.H)
	}

	if w, h := pngSize(t, hit(t, r, reply.Frames[reply.At].URL).Body.Bytes()); w != 360 || h != 640 {
		t.Errorf("still is %dx%d, want 360x640", w, h)
	}
}

func TestE2E_Frames_PreviewIsSmallerJPEG(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	makeClip(t, root, "big.mp4",
		"-f", "lavfi", "-i", "testsrc2=s=1280x720:d=3:r=25",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p")

	lib := &library.Library{Roots: []string{root}}
	r := framesRouter(lib)

	reply := getShot(t, r, "/api/frames?file=big.mp4&t=1.5")
	full := hit(t, r, reply.Frames[2].URL)
	prev := hit(t, r, reply.Frames[2].Prev)

	if prev.Code != http.StatusOK || prev.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("preview → %d %q", prev.Code, prev.Header().Get("Content-Type"))
	}
	if prev.Body.Len() >= full.Body.Len() {
		t.Errorf("preview %d bytes is not smaller than full %d", prev.Body.Len(), full.Body.Len())
	}
}

func TestE2E_Frames_DownloadIsAttachment(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	makeShotClip(t, root, "My Movie.mp4", "24")

	lib := &library.Library{Roots: []string{root}}
	r := framesRouter(lib)

	reply := getShot(t, r, "/api/frames?file=My%20Movie.mp4&t=3")
	w := hit(t, r, reply.Frames[2].URL+"&dl=1")

	cd := w.Header().Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment;") {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	if !strings.Contains(cd, "My Movie @ ") || !strings.Contains(cd, ".png") {
		t.Errorf("download name lost the title or extension: %q", cd)
	}
}

func TestFrames_RejectsBadInput(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	makeShotClip(t, root, "clip.mp4", "24")

	lib := &library.Library{Roots: []string{root}}
	r := framesRouter(lib)

	cases := []struct {
		url  string
		want int
	}{
		{"/api/frames?file=../../etc/passwd&t=1", http.StatusNotFound},
		{"/api/frames?file=nope.mp4&t=1", http.StatusNotFound},
		{"/api/frames?file=clip.mp4&t=abc", http.StatusBadRequest},
		{"/api/frames?file=clip.mp4&t=-5", http.StatusBadRequest},
		{"/api/frames?file=clip.mp4&t=99999", http.StatusBadRequest},
	}

	for _, tc := range cases {
		if w := hit(t, r, tc.url); w.Code != tc.want {
			t.Errorf("GET %s → %d, want %d (%s)", tc.url, w.Code, tc.want, w.Body.String())
		}
	}
}

func TestFrameImage_RejectsBadShotID(t *testing.T) {
	chdirTemp(t)
	r := framesRouter(&library.Library{})

	for _, url := range []string{
		"/api/frames/img?shot=../../etc&i=0",
		"/api/frames/img?shot=abc&i=0",
		"/api/frames/img?shot=1234567-1&i=9",
		"/api/frames/img?shot=1234567-1&i=-1",
	} {
		if w := hit(t, r, url); w.Code != http.StatusBadRequest {
			t.Errorf("GET %s → %d, want 400", url, w.Code)
		}
	}
}

func TestE2E_Frames_ReusesCachedShot(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	makeShotClip(t, root, "clip.mp4", "24")

	lib := &library.Library{Roots: []string{root}}
	r := framesRouter(lib)

	first := getShot(t, r, "/api/frames?file=clip.mp4&t=3")
	stat, err := os.Stat(filepath.Join(shotPath(first.ID), "f1.png"))
	if err != nil {
		t.Fatal(err)
	}

	second := getShot(t, r, "/api/frames?file=clip.mp4&t=3")
	if second.ID != first.ID {
		t.Fatalf("id changed across identical requests: %s vs %s", first.ID, second.ID)
	}

	again, err := os.Stat(filepath.Join(shotPath(second.ID), "f1.png"))
	if err != nil {
		t.Fatal(err)
	}
	if !again.ModTime().Equal(stat.ModTime()) {
		t.Error("second request re-ran ffmpeg instead of serving the cached strip")
	}
}

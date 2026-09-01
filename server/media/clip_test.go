package media

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"notflix/server/library"
)

func TestClipRange_RejectsEmptySelection(t *testing.T) {
	for _, c := range [][2]float64{{10, 10}, {10, 9.5}, {0, 0}} {
		if _, _, err := clipRange(c[0], c[1], 600); err == nil {
			t.Errorf("in=%v out=%v accepted; out must come after in", c[0], c[1])
		}
	}
}

func TestClipRange_RejectsOverLength(t *testing.T) {
	if _, _, err := clipRange(0, ClipMaxSec+0.5, 3600); err == nil {
		t.Fatalf("a %.1fs clip was accepted over a %.0fs cap", ClipMaxSec+0.5, ClipMaxSec)
	}
	if _, _, err := clipRange(0, ClipMaxSec, 3600); err != nil {
		t.Fatalf("exactly the cap must be allowed: %v", err)
	}
}

func TestClipRange_ClampsNegativeIn(t *testing.T) {
	in, out, err := clipRange(-30, 12, 600)
	if err != nil {
		t.Fatal(err)
	}
	if in != 0 || out != 12 {
		t.Fatalf("got in=%v out=%v, want 0/12", in, out)
	}
}

func TestClipRange_ClampsOutPastDuration(t *testing.T) {
	in, out, err := clipRange(5, 900, 30)
	if err != nil {
		t.Fatal(err)
	}
	if in != 5 || out != 30 {
		t.Fatalf("got in=%v out=%v, want 5/30", in, out)
	}
}

// Clamping both ends onto a duration can collapse the selection; that is an
// empty clip, not a zero-length encode.
func TestClipRange_ClampCollapseIsRejected(t *testing.T) {
	if _, _, err := clipRange(90, 120, 30); err == nil {
		t.Fatal("a selection entirely past the end was accepted")
	}
}

func TestClipRange_UnprobedDurationSkipsClamp(t *testing.T) {
	in, out, err := clipRange(5, 20, 0)
	if err != nil || in != 5 || out != 20 {
		t.Fatalf("got in=%v out=%v err=%v, want 5/20/nil", in, out, err)
	}
}

func TestClipRange_RejectsNaNAndInf(t *testing.T) {
	bad := [][2]float64{
		{math.NaN(), 10},
		{0, math.NaN()},
		{math.Inf(-1), 10},
		{0, math.Inf(1)},
	}
	for _, c := range bad {
		if _, _, err := clipRange(c[0], c[1], 600); err == nil {
			t.Errorf("in=%v out=%v accepted", c[0], c[1])
		}
	}
}

func TestClipID_SeparatesRanges(t *testing.T) {
	a := clipID("Show/ep1.mp4", 10, 20)
	if a != clipID("Show/ep1.mp4", 10, 20) {
		t.Fatal("same request produced two ids")
	}
	for _, other := range []string{
		clipID("Show/ep1.mp4", 10, 21),
		clipID("Show/ep1.mp4", 11, 20),
		clipID("Show/ep2.mp4", 10, 20),
	} {
		if a == other {
			t.Fatalf("id %s collides across requests", a)
		}
	}
	if !clipIDRe.MatchString(a) {
		t.Fatalf("id %q is not servable by ClipFile", a)
	}
}

func TestClipName_StripsPathAndQuotes(t *testing.T) {
	got := clipName("/roots/a/Some \"Movie\"/../Big.Buck.Bunny.2008.1080p.mp4", 61, 75)
	if strings.ContainsAny(got, `"/\`) {
		t.Fatalf("name %q still carries a separator or quote", got)
	}
	if !strings.HasSuffix(got, ".mp4") || !strings.Contains(got, "00.01.01-00.01.15") {
		t.Fatalf("name %q lost its stamp or extension", got)
	}
}

func TestDisposition_ForcesAttachment(t *testing.T) {
	h := disposition(`he said "hi"/cut.mp4`)
	if !strings.HasPrefix(h, "attachment; ") {
		t.Fatalf("not an attachment: %s", h)
	}
	// One trailing quote closes filename="…"; a second would end the header early.
	if strings.Count(h, `"`) != 2 {
		t.Fatalf("quote escaped the filename= value: %s", h)
	}
	if !strings.Contains(h, "filename*=UTF-8''") {
		t.Fatalf("missing the UTF-8 form: %s", h)
	}
}

func TestClipScale_CapsLongEdgeAndDeanamorphizes(t *testing.T) {
	if got := clipScale(&srcInfo{vHeight: 2160, vDAR: 16.0 / 9.0}); got != "scale=1920:1080:flags=lanczos,setsar=1" {
		t.Errorf("4K: got %q", got)
	}
	// Portrait keeps its height: the cap is on the LONG edge, not on height.
	if got := clipScale(&srcInfo{vHeight: 1920, vDAR: 1080.0 / 1920.0}); got != "scale=1080:1920:flags=lanczos,setsar=1" {
		t.Errorf("portrait: got %q", got)
	}
	// Anamorphic 720x576 PAL, 16:9 display → square pixels at display width.
	if got := clipScale(&srcInfo{vHeight: 576, vDAR: 16.0 / 9.0}); got != "scale=1024:576:flags=lanczos,setsar=1" {
		t.Errorf("anamorphic: got %q", got)
	}
	if got := clipScale(&srcInfo{}); !strings.Contains(got, "force_original_aspect_ratio=decrease") {
		t.Errorf("unprobed source needs the expression fallback: %q", got)
	}
}

func TestClipTimeout_ScalesWithSpan(t *testing.T) {
	short, long := clipTimeout(2), clipTimeout(ClipMaxSec)
	if short <= 0 || long <= short {
		t.Fatalf("timeouts do not scale: %v then %v", short, long)
	}
}

func TestClipArgs_TranscodesToH264AAC(t *testing.T) {
	args := strings.Join(clipArgs("/src.mkv", "/out.mp4.tmp", 12.5, 8), " ")

	if strings.Contains(args, "-c:v copy") || strings.Contains(args, "-c:a copy") {
		t.Fatalf("a share clip must never stream-copy: %s", args)
	}
	for _, want := range []string{
		"-accurate_seek", "-ss 12.500", "-t 8.000",
		"-pix_fmt yuv420p", "-movflags +faststart", "-f mp4",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %q in: %s", want, args)
		}
	}
	// An absent audio stream must not fail the whole cut.
	if !strings.Contains(args, "-map 0:a:0?") {
		t.Errorf("audio map is not optional: %s", args)
	}
}

func clipRouter(lib *library.Library) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/clip", func(c *gin.Context) { ClipStart(c, lib) })
	r.GET("/api/clip/status", ClipStatus)
	r.GET("/api/clip/file", ClipFile)

	return r
}

func postClip(t *testing.T, r *gin.Engine, file string, in, out float64) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(clipReq{File: file, In: in, Out: out})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/clip", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	return w
}

func TestClipStart_TraversalIsNotFound(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(filepath.Dir(root), "secret.mp4")
	if err := os.WriteFile(secret, []byte("nope"), 0644); err != nil {
		t.Fatal(err)
	}

	r := clipRouter(&library.Library{Roots: []string{root}})

	for _, rel := range []string{
		"../secret.mp4",
		"../../etc/passwd",
		"/etc/passwd",
		"sub/../../secret.mp4",
	} {
		if w := postClip(t, r, rel, 0, 5); w.Code != http.StatusNotFound {
			t.Errorf("%q → %d, want 404: %s", rel, w.Code, w.Body.String())
		}
	}
}

// Long enough that clamping to the real duration can't rescue an over-length
// request — 2fps keeps the fixture cheap.
func TestClipStart_BadRangeIsRejected(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	makeH264GOP(t, root, "bounds.mp4", int(ClipMaxSec)+30, 4, 2)

	r := clipRouter(&library.Library{Roots: []string{root}})

	cases := []struct {
		name    string
		in, out float64
	}{
		{"out == in", 1, 1},
		{"out before in", 20, 10},
		{"over the cap", 0, ClipMaxSec + 20},
		{"over the cap after clamping", -50, ClipMaxSec + 20},
	}
	for _, c := range cases {
		if w := postClip(t, r, "bounds.mp4", c.in, c.out); w.Code != http.StatusBadRequest {
			t.Errorf("%s → %d, want 400: %s", c.name, w.Code, w.Body.String())
		}
	}

	// The same clamping must still ACCEPT an in-range selection off both ends.
	w := postClip(t, r, "bounds.mp4", -5, 10)
	if w.Code != http.StatusOK {
		t.Fatalf("clamped-but-valid → %d, want 200: %s", w.Code, w.Body.String())
	}

	var started ClipJob
	if err := json.Unmarshal(w.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	// Drained here, not left running: the fixture dies with the temp dir.
	awaitClip(t, r, started.ID)
}

func TestClipFile_RejectsNonHexID(t *testing.T) {
	r := clipRouter(&library.Library{})

	for _, id := range []string{"../../../etc/passwd", "..%2f..%2fkv.json", "", "zzzz"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/clip/file?id="+id, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("id %q → %d, want 400", id, w.Code)
		}
	}
}

// A 12s h264 source with a 4s GOP: an in-point at 5s and an out at 11s both
// land mid-GOP, which is exactly where `-c copy` would snap and this must not.
func makeH264GOP(t *testing.T, root, name string, secs, gop, fps int) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(root, name)
	cmd := exec.Command("ffmpeg", "-y", "-v", "error",
		"-f", "lavfi", "-i", fmt.Sprintf("testsrc2=s=320x240:d=%d:r=%d", secs, fps),
		"-f", "lavfi", "-i", fmt.Sprintf("sine=f=440:d=%d", secs),
		"-c:v", "libx264", "-preset", "ultrafast", "-g", strconv.Itoa(gop*fps),
		"-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "64k", "-shortest",
		"-movflags", "+faststart",
		out,
	)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg h264 fixture failed: %v\n%s", err, b)
	}

	return out
}

func awaitClip(t *testing.T, r *gin.Engine, id string) ClipJob {
	t.Helper()

	for range 600 {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/clip/status?id="+id, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status → %d: %s", w.Code, w.Body.String())
		}

		var j ClipJob
		if err := json.Unmarshal(w.Body.Bytes(), &j); err != nil {
			t.Fatal(err)
		}
		if j.Stage == "done" {
			return j
		}
		if j.Stage == "error" {
			t.Fatalf("clip failed: %s", j.Error)
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("clip %s never finished", id)

	return ClipJob{}
}

// The whole compatibility argument: without +faststart a player must fetch the
// entire file before it can start, which on a phone reads as a broken clip.
func moovBeforeMdat(t *testing.T, path string) bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	moov, mdat := bytes.Index(b, []byte("moov")), bytes.Index(b, []byte("mdat"))
	if moov < 0 || mdat < 0 {
		t.Fatalf("%s is not an mp4 (moov=%d mdat=%d)", filepath.Base(path), moov, mdat)
	}

	return moov < mdat
}

func fetchClip(t *testing.T, r *gin.Engine, url string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s → %d", url, w.Code)
	}

	out := filepath.Join(t.TempDir(), "got.mp4")
	if err := os.WriteFile(out, w.Body.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	return w, out
}

func TestE2E_ClipIsExactAndShareable(t *testing.T) {
	if testing.Short() {
		t.Skip("ffmpeg-driven; -short")
	}

	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	makeH264GOP(t, root, "exact.mp4", 12, 4, 24)

	r := clipRouter(&library.Library{Roots: []string{root}})

	const in, out = 5.0, 11.0
	w := postClip(t, r, "exact.mp4", in, out)
	if w.Code != http.StatusOK {
		t.Fatalf("start → %d: %s", w.Code, w.Body.String())
	}

	var started ClipJob
	if err := json.Unmarshal(w.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	job := awaitClip(t, r, started.ID)

	resp, file := fetchClip(t, r, job.URL)

	if cd := resp.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") {
		t.Errorf("Content-Disposition %q will open in Safari instead of saving", cd)
	}

	if got := probeCSV(t, file, "stream=codec_name:stream=codec_type"); !strings.Contains(got, "h264") {
		t.Errorf("video is not h264: %s", got)
	}
	if got := probeCSV(t, file, "stream=codec_name"); !strings.Contains(got, "aac") {
		t.Errorf("audio is not aac: %s", got)
	}

	got, err := strconv.ParseFloat(probeCSV(t, file, "format=duration"), 64)
	if err != nil {
		t.Fatalf("no duration on the clip: %v", err)
	}
	// Two frames at 24fps. A keyframe-snapped cut would be off by seconds.
	if math.Abs(got-(out-in)) > 2.0/24.0 {
		t.Errorf("clip is %.3fs, want %.3fs — the cut is not frame-exact", got, out-in)
	}

	if !moovBeforeMdat(t, file) {
		t.Error("moov is behind mdat: +faststart did not take")
	}
}

// The majority case in this library. An AV1 clip is refused by iOS Photos,
// Messages and WhatsApp, so the transcode is the feature.
func TestE2E_ClipFromAV1IsH264(t *testing.T) {
	if testing.Short() {
		t.Skip("ffmpeg-driven; -short")
	}

	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	src := makeAV1GOP(t, root, "av1src.mp4", 8, 2)

	if codec := probeCSV(t, src, "stream=codec_name:stream=codec_type"); !strings.Contains(codec, "av1") {
		t.Fatalf("fixture is not av1: %s", codec)
	}

	r := clipRouter(&library.Library{Roots: []string{root}})

	w := postClip(t, r, "av1src.mp4", 1.5, 4.5)
	if w.Code != http.StatusOK {
		t.Fatalf("start → %d: %s", w.Code, w.Body.String())
	}

	var started ClipJob
	if err := json.Unmarshal(w.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	job := awaitClip(t, r, started.ID)
	_, file := fetchClip(t, r, job.URL)

	codecs := probeCSV(t, file, "stream=codec_name")
	if strings.Contains(codecs, "av1") {
		t.Fatalf("clip kept AV1 — it will not play where it is shared: %s", codecs)
	}
	if !strings.Contains(codecs, "h264") || !strings.Contains(codecs, "aac") {
		t.Fatalf("want h264 + aac, got: %s", codecs)
	}
	if pf := probeCSV(t, file, "stream=pix_fmt"); !strings.HasPrefix(pf, "yuv420p") || strings.Contains(pf, "10") {
		t.Errorf("pix_fmt %q is not the 8-bit 4:2:0 every device decodes", pf)
	}
	if !moovBeforeMdat(t, file) {
		t.Error("moov is behind mdat: +faststart did not take")
	}
}

// A mashed button must not queue a second encode of the same range.
func TestE2E_RepeatRequestJoinsTheSameClip(t *testing.T) {
	if testing.Short() {
		t.Skip("ffmpeg-driven; -short")
	}

	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	makeH264GOP(t, root, "dedupe.mp4", 6, 2, 24)

	r := clipRouter(&library.Library{Roots: []string{root}})

	var ids []string
	for range 4 {
		w := postClip(t, r, "dedupe.mp4", 1, 3)
		if w.Code != http.StatusOK {
			t.Fatalf("start → %d: %s", w.Code, w.Body.String())
		}
		var j ClipJob
		if err := json.Unmarshal(w.Body.Bytes(), &j); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, j.ID)
	}

	for _, id := range ids[1:] {
		if id != ids[0] {
			t.Fatalf("four identical requests produced %v", ids)
		}
	}

	awaitClip(t, r, ids[0])

	ents, err := os.ReadDir(clipDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 {
		t.Fatalf("want one clip on disk, got %d", len(ents))
	}
}

func TestEvictClips_DropsAgedAndOverflow(t *testing.T) {
	chdirTemp(t)
	if !library.EnsureDir(clipDir) {
		t.Fatal("cannot create clip dir")
	}

	old := time.Now().Add(-2 * clipTTL)
	write := func(name string, mod time.Time) string {
		p := filepath.Join(clipDir, name)
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mod, mod); err != nil {
			t.Fatal(err)
		}

		return p
	}

	aged := write("aged.mp4", old)
	tmp := write("half.mp4.tmp", old)
	fresh := write("fresh.mp4", time.Now())

	var overflow []string
	for i := range clipKeepMax + 5 {
		overflow = append(overflow, write(fmt.Sprintf("f%02d.mp4", i),
			time.Now().Add(-time.Duration(i)*time.Minute)))
	}

	evictClips()

	if _, err := os.Stat(aged); err == nil {
		t.Error("an aged clip survived")
	}
	if _, err := os.Stat(tmp); err == nil {
		t.Error("an abandoned .tmp survived")
	}

	left, err := os.ReadDir(clipDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) > clipKeepMax {
		t.Fatalf("%d clips left, cap is %d", len(left), clipKeepMax)
	}
	// Newest-first eviction: the freshly written ones are the ones kept.
	if _, err := os.Stat(fresh); err != nil {
		t.Error("the newest clip was evicted before older ones")
	}
	if _, err := os.Stat(overflow[len(overflow)-1]); err == nil {
		t.Error("the oldest overflow clip survived")
	}
}

// A 10-bit source encodes to High10 unless the pix_fmt is forced, and no
// iPhone decodes High10 — the clip would be a black rectangle in Messages.
func TestE2E_ClipFrom10BitIs8Bit(t *testing.T) {
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

	src := filepath.Join(root, "tenbit.mp4")
	cmd := exec.Command("ffmpeg", "-y", "-v", "error",
		"-f", "lavfi", "-i", "testsrc2=s=320x240:d=6:r=24",
		"-f", "lavfi", "-i", "sine=f=440:d=6",
		"-c:v", "libx264", "-preset", "ultrafast",
		"-pix_fmt", "yuv420p10le", "-profile:v", "high10",
		"-c:a", "aac", "-b:a", "64k", "-shortest",
		"-movflags", "+faststart", src,
	)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("no 10-bit x264 in this build: %v\n%s", err, b)
	}
	if pf := probeCSV(t, src, "stream=pix_fmt"); !strings.Contains(pf, "10le") {
		t.Skipf("fixture is not 10-bit: %s", pf)
	}

	r := clipRouter(&library.Library{Roots: []string{root}})

	w := postClip(t, r, "tenbit.mp4", 1, 4)
	if w.Code != http.StatusOK {
		t.Fatalf("start → %d: %s", w.Code, w.Body.String())
	}

	var started ClipJob
	if err := json.Unmarshal(w.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	job := awaitClip(t, r, started.ID)
	_, file := fetchClip(t, r, job.URL)

	if pf := probeCSV(t, file, "stream=pix_fmt"); pf != "yuv420p" {
		t.Fatalf("pix_fmt %q — a 10-bit clip does not play where it is shared", pf)
	}
	if prof := probeCSV(t, file, "stream=profile"); strings.Contains(prof, "10") {
		t.Fatalf("profile %q is High10", prof)
	}
}

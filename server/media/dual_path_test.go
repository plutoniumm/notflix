package media

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"notflix/server/library"
)

// Isolates kv.json and the HLS cache under a temp cwd.
func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cache"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	return dir
}

func makeMP4(t *testing.T, root, name string) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, name)
	cmd := exec.Command("ffmpeg", "-y", "-v", "error",
		"-f", "lavfi", "-i", "color=c=blue:s=320x240:d=2:r=24",
		"-f", "lavfi", "-i", "sine=f=440:d=2",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "64k", "-shortest",
		"-movflags", "+faststart",
		out,
	)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg fixture failed: %v\n%s", err, b)
	}
	return out
}

// Leading keyframe, so the clip is copy-passthrough eligible.
func makeAV1MP4(t *testing.T, root, name string) string {
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
	// testsrc2, not a static color: TestE2E_AV1CopyPassthrough hashes decoded
	// frames across the GOP boundary, which solid color can't distinguish.
	cmd := exec.Command("ffmpeg", "-y", "-v", "error",
		"-f", "lavfi", "-i", "testsrc2=s=320x240:d=2:r=24",
		"-f", "lavfi", "-i", "sine=f=440:d=2",
		"-c:v", "libsvtav1", "-preset", "10", "-crf", "50", "-g", "24",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "64k", "-shortest",
		"-movflags", "+faststart",
		out,
	)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg av1 fixture failed: %v\n%s", err, b)
	}
	return out
}

func TestMediaCodec_DefaultsToH264(t *testing.T) {
	chdirTemp(t)
	if got := MediaCodec("nothing.mp4"); got != CodecH264 {
		t.Errorf("default = %q, want h264", got)
	}
}

func TestMediaCodec_RoundTrip(t *testing.T) {
	chdirTemp(t)
	if err := SetMediaCodec("foo.mp4", CodecAV1); err != nil {
		t.Fatal(err)
	}
	if got := MediaCodec("foo.mp4"); got != CodecAV1 {
		t.Errorf("after set = %q, want av1", got)
	}
}

func TestMediaCodec_UnknownValueFallsBackToH264(t *testing.T) {
	chdirTemp(t)
	if err := SetMediaCodec("bar.mp4", "vp9"); err != nil {
		t.Fatal(err)
	}
	if got := MediaCodec("bar.mp4"); got != CodecH264 {
		t.Errorf("vp9 → %q, want h264", got)
	}
}

func TestSegPaths_H264UsesTSAndDefaultRoot(t *testing.T) {
	p, _ := segPaths("HASH", CodecH264, "720p", 3, 0, segMuxed, false)
	if !strings.HasSuffix(p, "/000003.ts") {
		t.Errorf("h264 ext: %s", p)
	}
	if !strings.Contains(p, "/d4/720p/") {
		t.Errorf("h264 cache root: %s", p)
	}
}

func TestSegPaths_AV1UsesM4SAndAV1Root(t *testing.T) {
	p, _ := segPaths("HASH", CodecAV1, "720p", 3, 0, segMuxed, false)
	if !strings.HasSuffix(p, "/000003.m4s") {
		t.Errorf("av1 ext: %s", p)
	}
	if !strings.Contains(p, "/d4-av1/720p/") {
		t.Errorf("av1 cache root: %s", p)
	}
}

func TestSegPaths_H264CopyStaysOnTS(t *testing.T) {
	p, _ := segPaths("HASH", CodecH264, "720p", 0, 0, segMuxed, true)
	if !strings.Contains(p, "/d4-copy/") {
		t.Errorf("copy root not honored: %s", p)
	}
	if !strings.HasSuffix(p, ".ts") {
		t.Errorf("h264 copy stays on .ts: %s", p)
	}
}

func TestSegPaths_AV1CopyVideoUsesM4SAndAV1CopyRoot(t *testing.T) {
	p, _ := segPaths("HASH", CodecAV1, qSrc, 0, 0, segVideoOnly, true)
	if !strings.Contains(p, "/d4-av1-copy/src/v/") {
		t.Errorf("av1-copy video root not honored: %s", p)
	}
	if !strings.HasSuffix(p, ".m4s") {
		t.Errorf("av1 copy video must be fMP4 .m4s: %s", p)
	}
}

func TestSegPaths_AV1AudioUsesM4SInAV1CopyRoot(t *testing.T) {
	// hls.js can't demux fMP4, so AV1 audio is its own fMP4 rendition, not TS.
	p, _ := segPaths("HASH", CodecAV1, "audio", 0, 1, segAudioOnly, true)
	if !strings.HasSuffix(p, ".m4s") {
		t.Errorf("av1 audio rendition must be fMP4 .m4s: %s", p)
	}
	if !strings.Contains(p, "/d4-av1-copy/audio/a1/") {
		t.Errorf("av1 audio cache root: %s", p)
	}
}

func TestInitPath_TranscodeRoot(t *testing.T) {
	p := initPath("HASH", "1080p", 0, false)
	if !strings.HasSuffix(p, "/d4-av1/1080p/a0/init.m4s") {
		t.Errorf("transcode init path: %s", p)
	}
}

func TestInitPath_CopyRoot(t *testing.T) {
	p := initPath("HASH", qSrc, 0, true)
	if !strings.HasSuffix(p, "/d4-av1-copy/src/a0/init.m4s") {
		t.Errorf("copy init path: %s", p)
	}
}

func TestAV1Codec_LevelsByHeight(t *testing.T) {
	cases := []struct {
		h, depth int
		want     string
	}{
		{144, 8, "av01.0.04M.08"},
		{720, 8, "av01.0.05M.08"},
		{1080, 8, "av01.0.09M.08"},
		{2160, 8, "av01.0.13M.08"},
		{1080, 10, "av01.0.09M.10"}, // 10-bit HDR must label .10
		{2160, 12, "av01.0.13M.12"},
		{1080, 0, "av01.0.09M.08"}, // unknown depth → 8
	}
	for _, tc := range cases {
		if got := av1Codec(tc.h, tc.depth); got != tc.want {
			t.Errorf("av1Codec(%d,%d) = %q, want %q", tc.h, tc.depth, got, tc.want)
		}
	}
}

func setupRouter(lib *library.Library) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/hls/master", func(c *gin.Context) { HLSMaster(c, lib) })
	r.GET("/api/hls/playlist", func(c *gin.Context) { HLSPlaylist(c, lib) })
	return r
}

func TestHLSMaster_DefaultIsH264(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	src := makeMP4(t, root, "movie.mp4")
	_ = src

	lib := &library.Library{Roots: []string{root}}
	r := setupRouter(lib)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/hls/master?file=movie.mp4", nil)
	r.ServeHTTP(w, req)

	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, body)
	}
	if !strings.Contains(body, "avc1.") {
		t.Errorf("default master missing avc1 codec: %s", body)
	}
	if strings.Contains(body, "av01.") {
		t.Errorf("default master should not have av01: %s", body)
	}
	if strings.Contains(body, "&codec=av1") {
		t.Errorf("default master should not embed codec=av1: %s", body)
	}
}

func TestHLSMaster_AV1MarkedFile(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	makeAV1MP4(t, root, "movie.mp4")

	if err := SetMediaCodec("movie.mp4", CodecAV1); err != nil {
		t.Fatal(err)
	}

	lib := &library.Library{Roots: []string{root}}
	r := setupRouter(lib)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/hls/master?file=movie.mp4&av1=1", nil)
	r.ServeHTTP(w, req)

	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, body)
	}
	if !strings.Contains(body, "av01.") {
		t.Errorf("av1 master missing av01 codec: %s", body)
	}
	if strings.Contains(body, "avc1.") {
		t.Errorf("av1 master should not have avc1: %s", body)
	}
	if !strings.Contains(body, "#EXT-X-MEDIA:TYPE=AUDIO") || !strings.Contains(body, "GROUP-ID=\"aud\"") {
		t.Errorf("av1 master must declare a demuxed audio rendition: %s", body)
	}
	if !strings.Contains(body, "AUDIO=\"aud\"") {
		t.Errorf("av1 video STREAM-INF must reference the audio group: %s", body)
	}
	if !strings.Contains(body, "q=audio&audio=0&codec=av1") {
		t.Errorf("av1 master must point the audio rendition at q=audio: %s", body)
	}
	if !strings.Contains(body, "/api/hls/playlist?file=movie.mp4&q=src&codec=av1\n") {
		t.Errorf("av1 video rung must be q=src with NO audio param: %s", body)
	}
	if strings.Count(body, "#EXT-X-STREAM-INF") != 1 {
		t.Errorf("av1 master must be a single video rung (no ladder): %s", body)
	}
}

// The AV1 master has no h264 alternate and iOS plays HLS natively (no MSE
// negotiation), so a device without an AV1 decoder gets a black screen. The
// player declares itself with &av1=0/1; without it, unsure means h264.
func TestHLSMaster_AV1CapabilityGate(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	makeAV1MP4(t, root, "movie.mp4")

	if err := SetMediaCodec("movie.mp4", CodecAV1); err != nil {
		t.Fatal(err)
	}
	r := setupRouter(&library.Library{Roots: []string{root}})

	const (
		iosUA    = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1"
		chromeUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
		safariUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15"
	)

	for _, tc := range []struct {
		name, url, ua string
		wantAV1       bool
	}{
		{"param says no", "/api/hls/master?file=movie.mp4&av1=0", chromeUA, false},
		{"param says yes", "/api/hls/master?file=movie.mp4&av1=1", iosUA, true},
		{"no param, ios", "/api/hls/master?file=movie.mp4", iosUA, false},
		{"no param, desktop chrome", "/api/hls/master?file=movie.mp4", chromeUA, true},
		{"no param, desktop safari", "/api/hls/master?file=movie.mp4", safariUA, false},
		{"no param, no ua", "/api/hls/master?file=movie.mp4", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", tc.url, nil)
			req.Header.Set("User-Agent", tc.ua)
			r.ServeHTTP(w, req)

			body := w.Body.String()
			if w.Code != http.StatusOK {
				t.Fatalf("status %d body %s", w.Code, body)
			}

			gotAV1 := strings.Contains(body, "av01.")
			if gotAV1 != tc.wantAV1 {
				t.Fatalf("av1 master = %v, want %v: %s", gotAV1, tc.wantAV1, body)
			}
			if tc.wantAV1 {
				if !strings.Contains(body, "GROUP-ID=\"aud\"") {
					t.Errorf("capable client must get the demuxed CMAF master: %s", body)
				}
				return
			}
			// Same URLs as any h264 client (no codec param), so the cache is shared.
			if !strings.Contains(body, "avc1.") {
				t.Errorf("downgraded client must get the h264 ladder: %s", body)
			}
			if strings.Contains(body, "codec=av1") {
				t.Errorf("downgraded client must not be pointed at av1 renditions: %s", body)
			}
			if strings.Count(body, "#EXT-X-STREAM-INF") < 2 {
				t.Errorf("h264 fallback must be the full ladder: %s", body)
			}
		})
	}
}

func TestHLSMaster_AV1MarkedButNotActuallyAV1FallsBackToH264(t *testing.T) {
	// Marking an h264 file av1 must not force the av1 path — that was the jank bug.
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	makeMP4(t, root, "movie.mp4") // h264

	if err := SetMediaCodec("movie.mp4", CodecAV1); err != nil {
		t.Fatal(err)
	}

	lib := &library.Library{Roots: []string{root}}
	r := setupRouter(lib)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/hls/master?file=movie.mp4", nil)
	r.ServeHTTP(w, req)

	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, body)
	}
	if strings.Contains(body, "av01.") || strings.Contains(body, "&codec=av1") {
		t.Errorf("non-AV1 file marked av1 must fall back to h264: %s", body)
	}
	if !strings.Contains(body, "avc1.") {
		t.Errorf("expected h264 fallback ladder: %s", body)
	}
}

func TestHLSPlaylist_AV1HasMapAndV6(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	makeAV1MP4(t, root, "movie.mp4")

	lib := &library.Library{Roots: []string{root}}
	r := setupRouter(lib)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/hls/playlist?file=movie.mp4&q=src&codec=av1", nil)
	r.ServeHTTP(w, req)

	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, body)
	}
	if !strings.Contains(body, "#EXT-X-VERSION:6") {
		t.Errorf("av1 playlist needs version 6: %s", body)
	}
	if !strings.Contains(body, "#EXT-X-MAP:URI=\"/api/hls/init?file=movie.mp4&q=src&codec=av1&m=copy&cv=") {
		t.Errorf("av1 video EXT-X-MAP must be the video-only copy init (cache-versioned): %s", body)
	}
	if strings.Contains(body, "q=src&audio=") {
		t.Errorf("av1 video rung must not carry an audio param (muxed): %s", body)
	}
	if !strings.Contains(body, "&codec=av1") || !strings.Contains(body, "&m=copy") {
		t.Errorf("av1 video segment URLs need codec=av1 & m=copy: %s", body)
	}
}

func TestHLSPlaylist_AV1AudioRenditionIsFMP4(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	makeAV1MP4(t, root, "movie.mp4")

	lib := &library.Library{Roots: []string{root}}
	r := setupRouter(lib)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/hls/playlist?file=movie.mp4&q=audio&audio=0&codec=av1", nil)
	r.ServeHTTP(w, req)

	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, body)
	}
	if !strings.Contains(body, "#EXT-X-VERSION:6") {
		t.Errorf("av1 audio rendition must be fMP4 (version 6): %s", body)
	}
	if !strings.Contains(body, "#EXT-X-MAP:URI=\"/api/hls/init?file=movie.mp4&q=audio&audio=0&codec=av1&m=copy&cv=") {
		t.Errorf("av1 audio rendition needs its own AAC fMP4 init: %s", body)
	}
	if !strings.Contains(body, "q=audio&seg=0&audio=0&m=copy&codec=av1") {
		t.Errorf("av1 audio segments must be copy fMP4: %s", body)
	}
}

func TestHLSPlaylist_H264NoMap(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	makeMP4(t, root, "movie.mp4")

	lib := &library.Library{Roots: []string{root}}
	r := setupRouter(lib)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/hls/playlist?file=movie.mp4&q=720p&audio=0", nil)
	r.ServeHTTP(w, req)

	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, body)
	}
	if strings.Contains(body, "#EXT-X-MAP") {
		t.Errorf("h264 playlist must not have EXT-X-MAP: %s", body)
	}
	if !strings.Contains(body, "#EXT-X-VERSION:3") {
		t.Errorf("h264 playlist stays at version 3: %s", body)
	}
}

// seg= is client-supplied: past the playlist end it used to spawn a real
// ffmpeg for media that doesn't exist.
func TestEnsureSegment_RejectsPastEnd(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	src := makeMP4(t, root, "movie.mp4") // 2s → one 4s segment

	if _, err := ensureSegment(src, "HASH", CodecH264, "720p", 999999, 0, segMuxed, false); err == nil {
		t.Error("seg 999999 accepted — that's an ffmpeg spawn per bogus request")
	}
	if _, err := ensureSegment(src, "HASH", CodecH264, "720p", 0, 0, segMuxed, false); err != nil {
		t.Errorf("seg 0 must still generate: %v", err)
	}
}

// A waiter on the in-flight slot used to get (segPath, nil) when the generator
// failed, then 500 later with a misleading message.
func TestEnsureSegment_WaiterInheritsError(t *testing.T) {
	chdirTemp(t)
	_, key := segPaths("HASH", CodecH264, "720p", 3, 0, segMuxed, false)

	fl := &inflight{done: make(chan struct{}), err: fmt.Errorf("generator boom")}
	close(fl.done)
	hlsInFlight.Store(key, fl)
	defer hlsInFlight.Delete(key)

	_, err := ensureSegment("missing.mp4", "HASH", CodecH264, "720p", 3, 0, segMuxed, false)
	if err == nil || !strings.Contains(err.Error(), "generator boom") {
		t.Errorf("waiter err = %v, want the generator's", err)
	}
}

func TestE2E_GenerateAV1Init(t *testing.T) {
	if testing.Short() {
		t.Skip("ffmpeg-driven; -short")
	}
	if !hasEncoder("libsvtav1") {
		t.Skip("libsvtav1 not in this ffmpeg build")
	}
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	src := makeMP4(t, root, "movie.mp4")

	out := filepath.Join(t.TempDir(), "init.m4s")
	if err := generateAV1Init(src, out, av1Profiles["240p"], 0, segMuxed); err != nil {
		t.Fatalf("generateAV1Init: %v", err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("init not created: %v", err)
	}
	if info.Size() < 200 {
		t.Errorf("init too small (%d bytes), likely malformed", info.Size())
	}
	// ftyp box at offset 0; first 8 bytes = size + "ftyp".
	head := make([]byte, 32)
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Read(head); err != nil {
		t.Fatal(err)
	}
	if string(head[4:8]) != "ftyp" {
		t.Errorf("init missing ftyp box; head=% x", head)
	}
	// The av01 brand sits in compatible_brands, inside the first 32 bytes.
	if !strings.Contains(string(head), "av01") {
		t.Errorf("init missing av01 brand; head=% x", head)
	}
}

func TestE2E_GenerateAV1Segment(t *testing.T) {
	if testing.Short() {
		t.Skip("ffmpeg-driven; -short")
	}
	if !hasEncoder("libsvtav1") {
		t.Skip("libsvtav1 not in this ffmpeg build")
	}
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	src := makeMP4(t, root, "movie.mp4")

	out := filepath.Join(t.TempDir(), "seg0.m4s")
	if err := generateAV1Segment(src, out, 0.0, 1.0, av1Profiles["240p"], 0, segMuxed); err != nil {
		t.Fatalf("generateAV1Segment: %v", err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("segment not created: %v", err)
	}
	if info.Size() < 500 {
		t.Errorf("segment too small (%d bytes), likely malformed", info.Size())
	}
	// styp box for fragment-only segments.
	head := make([]byte, 16)
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Read(head); err != nil {
		t.Fatal(err)
	}
	if string(head[4:8]) != "styp" {
		t.Errorf("segment missing styp box; head=% x", head)
	}
}

// Crude byte scan, but enough to assert single-track-ness.
func boxCount(t *testing.T, path, box string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Count(b, []byte(box))
}

// Video and audio must be SEPARATE single-track fMP4 renditions: hls.js cannot
// demux fMP4, so a muxed segment (2 traf) serves 200 and plays frozen.
func TestE2E_AV1CopyPassthrough(t *testing.T) {
	if testing.Short() {
		t.Skip("ffmpeg-driven; -short")
	}
	chdirTemp(t) // isolate ./cache — both continuous passes write there
	root := filepath.Join(t.TempDir(), "lib")
	src := makeAV1MP4(t, root, "movie.mp4")

	si := getSrcInfo(src)
	if !canCopyAV1(si) {
		t.Fatalf("AV1 fixture not copy-eligible: codec=%q pix=%q kf=%v",
			si.vCodec, si.vPixFmt, si.kfTimes)
	}

	// hls_time 0.5 with the 1s-GOP 2s fixture → exactly 2 keyframe-cut segs.
	const vhash = "VIDHASH"
	start := time.Now()
	if err := ensureAV1Video(src, vhash, 0.5); err != nil {
		t.Fatalf("ensureAV1Video: %v", err)
	}
	v0p, _ := segPaths(vhash, CodecAV1, qSrc, 0, 0, segVideoOnly, true)
	vDir := filepath.Dir(v0p)
	vInit := filepath.Join(vDir, "init.m4s")
	vSeg0 := filepath.Join(vDir, "000000.m4s")
	vSeg1 := filepath.Join(vDir, "000001.m4s")
	if err := waitForFile(vSeg1, 30*time.Second); err != nil {
		t.Fatalf("video segments never produced (need ≥2 from the 2s fixture): %v", err)
	}
	elapsed := time.Since(start)

	vib, _ := os.ReadFile(vInit)
	if string(vib[4:8]) != "ftyp" || !strings.Contains(string(vib[:64]), "av01") {
		t.Errorf("video init must be ftyp+av01; head=% x", vib[:32])
	}
	if c := boxCount(t, vInit, "trak"); c != 1 {
		t.Errorf("video init must be single-track, got %d trak", c)
	}
	if boxCount(t, vInit, "trex") != 1 {
		t.Errorf("video init must be fragmented (one trex)")
	}
	if string([]byte(mustHead(t, vSeg0))[4:8]) != "styp" {
		t.Errorf("video segment missing styp")
	}
	if c := boxCount(t, vSeg0, "traf"); c != 1 {
		t.Errorf("video segment must have exactly 1 traf (demuxed), got %d", c)
	}
	// One muxer instance writes cumulative tfdt natively; the old per-segment
	// path needed av1PatchFragment to fake it.
	if got := readTfdt(t, vSeg0); got != 0 {
		t.Errorf("video seg0 tfdt = %d, want 0", got)
	}
	if got := readTfdt(t, vSeg1); got == 0 {
		t.Errorf("video seg1 tfdt = 0 — segments not contiguous (stacked at t=0)")
	}

	// Off-by-one-GOP bug: the old -ss+copy path snapped every segment back one
	// keyframe with tfdt patched to look right, so only a frame-level decode
	// catches it. Fixture is 24fps/1s GOP: seg0 = frames 0.., seg1 = 24..
	srcF0 := frameMD5(t, src, 0)
	srcF24 := frameMD5(t, src, 24)
	if srcF0 == srcF24 {
		t.Fatalf("fixture frames 0 and 24 hash equal — can't distinguish GOPs")
	}
	if got := frameMD5(t, mustCat(t, vInit, vSeg0), 0); got != srcF0 {
		t.Errorf("video seg0 first frame %s != source frame 0 %s", got, srcF0)
	}
	if got := frameMD5(t, mustCat(t, vInit, vSeg1), 0); got != srcF24 {
		t.Errorf("video seg1 first frame %s != source frame 24 %s — off-by-one GOP", got, srcF24)
		if got == srcF0 {
			t.Errorf("→ seg1 decoded to the PREVIOUS keyframe (the exact -ss-snap regression)")
		}
	}
	if elapsed > 10*time.Second {
		t.Errorf("video copy pass took %v — not a remux, likely transcoding", elapsed)
	}

	const ahash = "AUDHASH"
	if err := ensureAV1Audio(src, ahash, 0, 1.0); err != nil {
		t.Fatalf("ensureAV1Audio: %v", err)
	}
	aDir := filepath.Dir(initPath(ahash, "audio", 0, true))
	aInit := filepath.Join(aDir, "init.m4s")
	a0 := filepath.Join(aDir, "000000.m4s")
	a1 := filepath.Join(aDir, "000001.m4s")
	if err := waitForFile(aInit, 30*time.Second); err != nil {
		t.Fatalf("audio init never produced: %v", err)
	}
	if err := waitForFile(a1, 30*time.Second); err != nil {
		t.Fatalf("audio segments never produced (need ≥2 from the 2s fixture): %v", err)
	}
	if string([]byte(mustHead(t, aInit))[4:8]) != "ftyp" {
		t.Errorf("audio init missing ftyp")
	}
	if c := boxCount(t, aInit, "trak"); c != 1 {
		t.Errorf("audio init must be single-track, got %d trak", c)
	}
	if strings.Contains(string(mustHead(t, aInit)), "av01") {
		t.Errorf("audio init must NOT carry the video track")
	}
	if c := boxCount(t, a0, "traf"); c != 1 {
		t.Errorf("audio segment must have exactly 1 traf (demuxed), got %d", c)
	}
	if string([]byte(mustHead(t, a0))[4:8]) != "styp" {
		t.Errorf("audio segment missing styp")
	}
	// Per-segment audio overlapped ~5ms/seg and MEDIA_ERR_DECODE'd mid-file, so
	// seg1 must start exactly where seg0's samples end.
	if got := readTfdt(t, a0); got != 0 {
		t.Errorf("audio seg0 tfdt = %d, want 0", got)
	}
	if t0, t1 := readTfdt(t, a0), readTfdt(t, a1); t1 != uint64(trunSamples(t, a0))*1024 {
		t.Errorf("audio not gapless: seg0 tfdt=%d (%d samples), seg1 tfdt=%d — seg1 must equal seg0's sample span", t0, trunSamples(t, a0), t1)
	}
	a0c := mustCat(t, aInit, a0)
	if cn := probeCSV(t, a0c, "stream=codec_name,channels"); cn != "aac,2" {
		t.Errorf("audio rendition must be stereo AAC, got %q", cn)
	}
	t.Logf("demuxed: video seg in %v (remux), one-pass gapless stereo AAC, single-traf", elapsed)
}

func trunSamples(t *testing.T, path string) int {
	t.Helper()
	d, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	j := bytes.Index(d, []byte("trun"))
	if j < 0 {
		t.Fatalf("no trun in %s", path)
	}
	return int(binary.BigEndian.Uint32(d[j+8 : j+12]))
}

func mustCat(t *testing.T, parts ...string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "joined.mp4")
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range parts {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		f.Write(b)
	}
	f.Close()
	return out
}

func probeCSV(t *testing.T, path, entries string) string {
	t.Helper()
	out, _ := exec.Command("ffprobe", "-v", "error", "-show_entries", entries, "-of", "csv=p=0", path).Output()
	return strings.TrimSpace(string(out))
}

// Decoding, not byte-comparing: a copy segment holding the wrong GOP still
// passes every container and tfdt check.
func frameMD5(t *testing.T, path string, n int) string {
	t.Helper()
	out, err := exec.Command("ffmpeg", "-v", "error",
		"-i", path,
		"-vf", fmt.Sprintf("select=eq(n\\,%d)", n),
		"-frames:v", "1", "-an", "-f", "framemd5", "-",
	).Output()
	if err != nil {
		t.Fatalf("frameMD5 %s n=%d: %v", filepath.Base(path), n, err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, ",")
		return strings.TrimSpace(f[len(f)-1])
	}
	t.Fatalf("frameMD5 %s n=%d: no frame decoded", filepath.Base(path), n)
	return ""
}

func readTfdt(t *testing.T, path string) uint64 {
	t.Helper()
	d, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	j := bytes.Index(d, []byte("tfdt"))
	if j < 0 {
		t.Fatalf("no tfdt in %s", path)
	}
	if d[j+4] == 1 {
		return binary.BigEndian.Uint64(d[j+8 : j+16])
	}
	return uint64(binary.BigEndian.Uint32(d[j+8 : j+12]))
}

func mustHead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > 256 {
		b = b[:256]
	}
	return string(b)
}

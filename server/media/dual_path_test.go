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

// chdirTemp moves cwd to a fresh temp dir with a `cache/` subdir, so kv.json
// and the HLS cache write into isolated state. Restored on test cleanup.
func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cache"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	return dir
}

// makeMP4 generates a 2s 320x240 test clip with sine audio under root/name.
// Caller must have ffmpeg in PATH; skips otherwise.
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

// makeAV1MP4 generates a 2s AV1 (8-bit yuv420p) + aac clip under root/name,
// with a leading keyframe so it's copy-passthrough eligible. Skips if
// libsvtav1 is unavailable.
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
	// testsrc2 (a moving pattern) — not a static color: TestE2E_AV1CopyPassthrough
	// hashes decoded frames across the GOP boundary to catch a copy segment that
	// holds the wrong GOP, which solid-color frames could never distinguish.
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

// --- codec selector ----------------------------------------------------------

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
	// We treat anything that isn't "av1" as h264 — defensive against KV state
	// from older code paths or hand-edits.
	if got := MediaCodec("bar.mp4"); got != CodecH264 {
		t.Errorf("vp9 → %q, want h264", got)
	}
}

// --- segPaths ----------------------------------------------------------------

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
	// h264 source passthrough: MPEG-TS in the -copy cache root.
	p, _ := segPaths("HASH", CodecH264, "720p", 0, 0, segMuxed, true)
	if !strings.Contains(p, "/d4-copy/") {
		t.Errorf("copy root not honored: %s", p)
	}
	if !strings.HasSuffix(p, ".ts") {
		t.Errorf("h264 copy stays on .ts: %s", p)
	}
}

func TestSegPaths_AV1CopyVideoUsesM4SAndAV1CopyRoot(t *testing.T) {
	// AV1 demuxed video rung: fMP4 (.m4s) single-track under the dedicated
	// -av1-copy root, in the audio-independent /<q>/v/ subtree.
	p, _ := segPaths("HASH", CodecAV1, qSrc, 0, 0, segVideoOnly, true)
	if !strings.Contains(p, "/d4-av1-copy/src/v/") {
		t.Errorf("av1-copy video root not honored: %s", p)
	}
	if !strings.HasSuffix(p, ".m4s") {
		t.Errorf("av1 copy video must be fMP4 .m4s: %s", p)
	}
}

func TestSegPaths_AV1AudioUsesM4SInAV1CopyRoot(t *testing.T) {
	// hls.js can't demux fMP4, so the AV1 title's audio is its OWN fMP4
	// rendition — .m4s under -av1-copy/audio/a<idx>/, NOT MPEG-TS.
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

// --- codec strings -----------------------------------------------------------

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

// --- HLSMaster routing -------------------------------------------------------

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
	req := httptest.NewRequest("GET", "/api/hls/master?file=movie.mp4", nil)
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
	// Demuxed CMAF: a separate audio rendition + a video-only rung that
	// carries NO audio param (so its segments are video-only fMP4).
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

func TestHLSMaster_AV1MarkedButNotActuallyAV1FallsBackToH264(t *testing.T) {
	// Marking an h264 file av1 must NOT force the av1 path (that was the jank
	// bug). canCopyAV1 fails → safe h264 ladder.
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

// --- HLSPlaylist EXT-X-MAP ---------------------------------------------------

func TestHLSPlaylist_AV1HasMapAndV6(t *testing.T) {
	chdirTemp(t)
	root := filepath.Join(t.TempDir(), "lib")
	makeAV1MP4(t, root, "movie.mp4")

	lib := &library.Library{Roots: []string{root}}
	r := setupRouter(lib)

	// Video rung: requested with NO audio param (demuxed → video-only fMP4).
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
	// Video init must be the av01-only copy init — no audio param in the URL.
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

	// Audio rendition request, as emitted by the demuxed master.
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

// --- E2E: real ffmpeg ---------------------------------------------------------

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
	// AV1 brand should appear in compatible_brands (within first 32 bytes).
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

// boxCount counts top-level/anywhere occurrences of a 4-byte box type. Crude
// but sufficient to assert single-track-ness (the hls.js fMP4 requirement).
func boxCount(t *testing.T, path, box string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Count(b, []byte(box))
}

// TestE2E_AV1CopyPassthrough is the jank-free demuxed path: an AV1-on-disk
// source streamed by copy. The hard requirement is that video and audio are
// SEPARATE single-track fMP4 renditions — hls.js cannot demux fMP4, so a muxed
// segment (2 traf / both av01+mp4a) plays as a frozen 200. Also asserts the
// video copy runs far faster than realtime (remux, not re-encode).
func TestE2E_AV1CopyPassthrough(t *testing.T) {
	if testing.Short() {
		t.Skip("ffmpeg-driven; -short")
	}
	chdirTemp(t) // isolate ./cache — both continuous passes write there
	root := filepath.Join(t.TempDir(), "lib")
	src := makeAV1MP4(t, root, "movie.mp4") // skips if no libsvtav1

	si := getSrcInfo(src)
	if !canCopyAV1(si) {
		t.Fatalf("AV1 fixture not copy-eligible: codec=%q pix=%q kf=%v",
			si.vCodec, si.vPixFmt, si.kfTimes)
	}

	// --- video rendition: ONE continuous -c:v copy pass ---
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
	// The single muxer instance writes correct cumulative tfdt natively —
	// seg0 at 0, seg1 one GOP later — so hls.js stacks them contiguously with
	// NO post-hoc patch (the old per-segment path needed av1PatchFragment).
	if got := readTfdt(t, vSeg0); got != 0 {
		t.Errorf("video seg0 tfdt = %d, want 0", got)
	}
	if got := readTfdt(t, vSeg1); got == 0 {
		t.Errorf("video seg1 tfdt = 0 — segments not contiguous (stacked at t=0)")
	}

	// THE assertion whose absence shipped the off-by-one-GOP bug: a segment's
	// DECODED CONTENT must be its OWN GOP, not the previous keyframe's. The old
	// -ss+copy path snapped every segment back one keyframe while tfdt was
	// patched to look correct (ffprobe + every structural check passed — only a
	// frame-level decode catches it). Fixture is 24fps, 1s GOP: seg0 = source
	// frames 0.., seg1 = source frames 24..
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

	// --- audio rendition: ONE continuous pass, gapless, single-track mp4a ---
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
	// Gapless: ffmpeg's one-pass writes absolute cumulative tfdt — seg1 must
	// start exactly where seg0's samples end (this is what per-segment audio
	// failed; it overlapped ~5ms/seg and MEDIA_ERR_DECODE'd mid-file).
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

// trunSamples returns the sample_count field of the first trun box.
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

// frameMD5 returns the md5 of decoded video frame index n of path. Decoding
// (not byte-comparing) is what catches a copy segment that holds the wrong
// GOP: the bitstream differs but every container/tfdt check still passes.
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

// readTfdt returns the moof/traf/tfdt baseMediaDecodeTime of an fMP4 segment.
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

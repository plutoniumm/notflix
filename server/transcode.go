package server

import (
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	CacheDir        = "./cache"
	cacheLimitBytes = 10 * 1024 * 1024 * 1024 // 10 GB
)

// inFlight tracks files currently being transcoded: cacheKey → struct{}.
var inFlight sync.Map

func init() {
	os.MkdirAll(CacheDir, 0755)
}

func vidCacheKey(videoPath string) string {
	return Hash(filepath.Base(videoPath))
}

func vidCachePath(videoPath string) string {
	return filepath.Join(CacheDir, vidCacheKey(videoPath)+".mp4")
}

func vidTempPath(videoPath string) string {
	return filepath.Join(CacheDir, vidCacheKey(videoPath)+".tmp")
}

func cacheSize() int64 {
	var total int64
	entries, _ := os.ReadDir(CacheDir)
	for _, e := range entries {
		if !e.IsDir() {
			if info, err := e.Info(); err == nil {
				total += info.Size()
			}
		}
	}
	return total
}

func wipeCacheIfNeeded() {
	if cacheSize() > cacheLimitBytes {
		log.Printf("Transcode cache exceeded 10 GB — wiping")
		os.RemoveAll(CacheDir)
		os.MkdirAll(CacheDir, 0755)
	}
}

// codecArgs returns the -c:v / -c:a flags for the given codec pair.
func codecArgs(videoCodec, audioCodec string) []string {
	var args []string
	switch videoCodec {
	case "h264":
		args = append(args, "-c:v", "copy")
	case "hevc":
		args = append(args, "-c:v", "copy", "-tag:v", "hvc1")
	default:
		args = append(args, "-c:v", "libx264", "-preset", "fast", "-crf", "23")
	}
	switch audioCodec {
	case "aac", "mp3":
		args = append(args, "-c:a", "copy")
	default:
		args = append(args, "-c:a", "aac")
	}
	return args
}

// buildFFmpegArgs builds a single-output fMP4 pipe command (used by directStream).
func buildFFmpegArgs(videoPath, videoCodec, audioCodec string) []string {
	args := []string{"-fflags", "+genpts", "-i", videoPath}
	args = append(args, codecArgs(videoCodec, audioCodec)...)
	args = append(args,
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"-f", "mp4",
		"pipe:1",
	)
	return args
}

// buildFFmpegArgsDual builds a dual-output command:
//   - Output 1: tempPath  — regular MP4 with -movflags +faststart (moov atom at front,
//     full seek table) written to a cache file.
//   - Output 2: pipe:1    — fragmented MP4 streamed to the HTTP response immediately.
//
// A single ffmpeg process handles both; codec work is shared between outputs.
func buildFFmpegArgsDual(videoPath, tempPath, videoCodec, audioCodec string) []string {
	ca := codecArgs(videoCodec, audioCodec)
	args := []string{"-fflags", "+genpts", "-i", videoPath}
	// Output 1: seekable cache file
	args = append(args, ca...)
	args = append(args, "-movflags", "+faststart", tempPath)
	// Output 2: fMP4 pipe for immediate playback (no seek table needed here)
	args = append(args, ca...)
	args = append(args,
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"-f", "mp4",
		"pipe:1",
	)
	return args
}

// softWriter wraps an io.Writer and silently swallows write errors after the
// first failure. This lets io.Copy keep draining ffmpeg's stdout — ensuring
// ffmpeg also finishes writing the cache file — even after the HTTP client
// has disconnected.
type softWriter struct {
	w  io.Writer
	ok bool
}

func newSoftWriter(w io.Writer) *softWriter { return &softWriter{w: w, ok: true} }

func (s *softWriter) Write(p []byte) (int, error) {
	if !s.ok {
		return len(p), nil
	}
	if _, err := s.w.Write(p); err != nil {
		s.ok = false
	}
	return len(p), nil
}

// streamTranscoded handles MKV/MOV playback with a transcode cache.
//
//   - Cache hit  → serve the cached faststart MP4 with full range/seek support.
//   - In-flight  → directStream (separate ffmpeg, no double-caching).
//   - Cold miss  → dual-output ffmpeg: fMP4 pipe to the client now, faststart
//     MP4 written to disk simultaneously. The handler blocks until ffmpeg
//     finishes so the cache file is always complete when the handler returns,
//     even if the client disconnected mid-stream.
func streamTranscoded(c *gin.Context, videoPath string) {
	cp := vidCachePath(videoPath)

	// 1. Cache hit — serve with full range/seek support.
	if f, info, err := openVideoFile(cp); err == nil {
		defer f.Close()
		log.Printf("Cache hit: %s", filepath.Base(videoPath))
		serveVideoContent(c, f, info, filepath.Base(cp))
		return
	}

	key := vidCacheKey(videoPath)

	// 2. Another request is already transcoding this file — stream directly.
	if _, exists := inFlight.Load(key); exists {
		log.Printf("Transcode in-flight for %s, streaming directly", filepath.Base(videoPath))
		directStream(c, videoPath)
		return
	}

	// 3. Cold miss — dual-output transcode.
	wipeCacheIfNeeded()
	inFlight.Store(key, struct{}{})
	defer inFlight.Delete(key)

	tp := vidTempPath(videoPath)
	videoCodec, audioCodec := probeCodecs(videoPath)
	log.Printf("Transcoding %s (video:%s audio:%s)", filepath.Base(videoPath), videoCodec, audioCodec)

	args := buildFFmpegArgsDual(videoPath, tp, videoCodec, audioCodec)

	// exec.Command (not CommandContext) — not tied to the request; ffmpeg runs
	// to completion and finishes writing the cache file even if the client leaves.
	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = io.Discard

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		c.String(http.StatusInternalServerError, "ffmpeg pipe error")
		return
	}
	if err := cmd.Start(); err != nil {
		c.String(http.StatusInternalServerError, "ffmpeg start: "+err.Error())
		return
	}

	c.Header("Content-Type", "video/mp4")
	c.Header("Accept-Ranges", "none")
	c.Header("Cache-Control", "no-cache")
	c.Status(http.StatusOK)

	// Drain stdout → HTTP client. softWriter silences HTTP errors so ffmpeg
	// keeps running and finishes the cache file after client disconnect.
	io.Copy(newSoftWriter(c.Writer), stdout)

	// Wait until ffmpeg fully exits (cache file write + faststart second pass done).
	if err := cmd.Wait(); err != nil {
		log.Printf("ffmpeg failed for %s: %v — discarding cache", filepath.Base(videoPath), err)
		os.Remove(tp)
		return
	}

	if err := os.Rename(tp, cp); err != nil {
		log.Printf("Failed to finalise cache for %s: %v", filepath.Base(videoPath), err)
		os.Remove(tp)
	} else {
		log.Printf("Cached: %s", filepath.Base(videoPath))
	}
}

// directStream runs ffmpeg and pipes a fragmented MP4 straight to the client
// without caching. Used when a transcode for the same file is already in flight.
func directStream(c *gin.Context, videoPath string) {
	videoCodec, audioCodec := probeCodecs(videoPath)
	args := buildFFmpegArgs(videoPath, videoCodec, audioCodec)

	cmd := exec.CommandContext(c.Request.Context(), "ffmpeg", args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
	cmd.Stderr = io.Discard

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		c.String(http.StatusInternalServerError, "ffmpeg pipe error")
		return
	}
	if err := cmd.Start(); err != nil {
		c.String(http.StatusInternalServerError, "ffmpeg start: "+err.Error())
		return
	}
	defer cmd.Wait()

	c.Header("Content-Type", "video/mp4")
	c.Header("Accept-Ranges", "none")
	c.Header("Cache-Control", "no-cache")
	c.Status(http.StatusOK)

	if _, err := io.Copy(c.Writer, stdout); err != nil {
		log.Printf("Direct stream ended for %s: %v", filepath.Base(videoPath), err)
	}
}

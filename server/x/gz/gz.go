// Package gz is a gzip response middleware. Hand-rolled rather than
// gin-contrib/gzip because this repo takes no new dependencies for something
// this small.
package gz

import (
	"compress/gzip"
	"io"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// Nothing was compressed before this: 932 KB of JS and 158 KB of CSS shipped
// raw, and an h264 HLS playlist is 295 KB of 1590 near-identical lines. On
// localhost that is invisible; over the tailnet it is the whole of first load.
//
// The rule is an ALLOWLIST of paths, not a content-type sniff. Two things must
// never be gzipped and a sniff gets both wrong:
//
//   - /video and /subs serve HTTP 206 Range responses. Compressing a byte range
//     makes the offsets lie, and the player seeks into garbage.
//   - segments, inits, images and clips are already-compressed media. Deflating
//     them spends the one resource that is actually scarce here — CPU — to make
//     the payload very slightly larger.
var allow = []string{"/assets/", "/api/", "/list/", "/kv/", "/manifest.json", "/sw.js"}

var deny = []string{
	"/api/hls/segment", "/api/hls/init", // media bytes
	"/api/clip/file", "/api/frames/img", // media bytes
	// Server-sent events. gzip.Writer buffers until Close, so the whisper
	// transcript would arrive in one lump when the job ended instead of line by
	// line — the stream exists precisely so it does not do that.
	"/api/subs/whisper/stream",
}

func wanted(path string) bool {
	for _, d := range deny {
		if strings.HasPrefix(path, d) {
			return false
		}
	}
	for _, a := range allow {
		if strings.HasPrefix(path, a) {
			return true
		}
	}

	return path == "/" || path == "/manage" || path == "/history"
}

type writer struct {
	gin.ResponseWriter
	gz *gzip.Writer
}

func (w *writer) Write(b []byte) (int, error) { return w.gz.Write(b) }

// A handler that flushes wants its bytes on the wire now. Honour it by pushing
// the deflate buffer out too, so a streaming response that slipped past the
// deny list degrades to "chunked but working" rather than silently buffering.
func (w *writer) Flush() {
	w.gz.Flush()
	w.ResponseWriter.Flush()
}

func (w *writer) WriteString(s string) (int, error) { return w.gz.Write([]byte(s)) }

// The body length changes, so the handler's own Content-Length is now a lie —
// and a wrong Content-Length is worse than none: the client truncates the body.
func (w *writer) WriteHeader(code int) {
	w.Header().Del("Content-Length")
	w.ResponseWriter.WriteHeader(code)
}

// BestSpeed, not BestCompression. This box is CPU-bound, and on text the
// difference is a few percent of size for several times the work.
var pool = sync.Pool{New: func() any {
	w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)

	return w
}}

func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") || !wanted(c.Request.URL.Path) {
			c.Next()

			return
		}
		// A Range request must be answered in the units it asked for.
		if c.GetHeader("Range") != "" {
			c.Next()

			return
		}

		gw := pool.Get().(*gzip.Writer)
		gw.Reset(c.Writer)

		c.Header("Content-Encoding", "gzip")
		// Shared caches key on this or they serve a gzipped body to a client
		// that never asked for one.
		c.Writer.Header().Add("Vary", "Accept-Encoding")

		orig := c.Writer
		c.Writer = &writer{ResponseWriter: orig, gz: gw}

		defer func() {
			gw.Close()
			pool.Put(gw)
			c.Writer = orig
		}()

		c.Next()
	}
}

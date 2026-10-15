package gz

import "testing"

func TestWanted(t *testing.T) {
	cases := map[string]bool{
		// Text worth compressing.
		"/assets/notflix.js":  true,
		"/assets/notflix.css": true,
		"/list/video":         true,
		"/kv/get":             true,
		"/api/hls/playlist":   true,
		"/api/hls/master":     true,
		"/api/blurbs":         true,
		"/":                   true,
		"/manage":             true,

		// Already-compressed media: deflating it spends the scarce resource
		// (CPU) to make the payload slightly larger.
		"/api/hls/segment": false,
		"/api/hls/init":    false,
		"/api/clip/file":   false,
		"/api/frames/img":  false,

		// Server-sent events: gzip.Writer buffers until Close, which would turn
		// a live transcript into one lump at the end.
		"/api/subs/whisper/stream": false,

		// Range responses. Compressing a byte range makes the offsets lie.
		"/video/Some Movie.mp4": false,
		"/subs/x.vtt":           false,
		"/images/123.jpg":       false,
	}

	for path, want := range cases {
		if got := wanted(path); got != want {
			t.Errorf("wanted(%q) = %v, want %v", path, got, want)
		}
	}
}

package media

import (
	"fmt"
	"math"
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

// makeAV1GOP generates a secs-long AV1 (8-bit yuv420p) + aac clip with an
// exact gop-second keyframe interval, so the segment grid is predictable.
func makeAV1GOP(t *testing.T, root, name string, secs, gop int) string {
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
	cmd := exec.Command("ffmpeg", "-y", "-v", "error",
		"-f", "lavfi", "-i", fmt.Sprintf("testsrc2=s=320x240:d=%d:r=24", secs),
		"-f", "lavfi", "-i", fmt.Sprintf("sine=f=440:d=%d", secs),
		"-c:v", "libsvtav1", "-preset", "12", "-crf", "55", "-g", fmt.Sprint(gop*24),
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

// extinfs pulls the EXTINF durations out of an m3u8 body.
func extinfs(t *testing.T, body string) []float64 {
	t.Helper()
	var ds []float64
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "#EXTINF:") {
			continue
		}
		var d float64
		fmt.Sscanf(strings.TrimPrefix(line, "#EXTINF:"), "%f", &d)
		ds = append(ds, d)
	}

	return ds
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

func servePlaylist(t *testing.T, r *gin.Engine, url string) string {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s → %d: %s", url, w.Code, w.Body.String())
	}

	return w.Body.String()
}

func writtenSegs(t *testing.T, dir string) int {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	n := 0
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".m4s") && e.Name() != "init.m4s" {
			n++
		}
	}

	return n
}

// TestE2E_AV1GridMatchesFFmpeg is the regression for the two-grids bug: the
// playlist derived its EXTINFs from a RELATIVE rule ("first keyframe ≥ 4s
// after the last cut") while ffmpeg's hls muxer cuts on an ABSOLUTE grid
// (pts ≥ 4s × segments_written). On a uniform GOP longer than the target both
// rules agree, which is why it shipped; on a 3s-GOP source the playlist
// declared 6 × 6s segments for the 9 ffmpeg actually wrote — seg 1 served
// [6,9) where the playlist promised [6,12), and the last 12s were unreachable.
// Each rendition must be declared exactly as its own continuous pass writes it.
func TestE2E_AV1GridMatchesFFmpeg(t *testing.T) {
	if testing.Short() {
		t.Skip("ffmpeg-driven; -short")
	}

	for _, gop := range []int{3, 6} {
		t.Run(fmt.Sprintf("gop%ds", gop), func(t *testing.T) {
			chdirTemp(t)
			root := filepath.Join(t.TempDir(), "lib")
			src := makeAV1GOP(t, root, "movie.mp4", 36, gop)

			si := getSrcInfo(src)
			if !canCopyAV1(si) {
				t.Fatalf("fixture not copy-eligible: codec=%q pix=%q", si.vCodec, si.vPixFmt)
			}
			r := setupRouter(&library.Library{Roots: []string{root}})
			hash := library.Hash("movie.mp4")

			if err := ensureAV1Video(src, hash, hlsSegDur); err != nil {
				t.Fatal(err)
			}
			v0, _ := segPaths(hash, CodecAV1, qSrc, 0, 0, segVideoOnly, true)
			vDir := filepath.Dir(v0)
			if err := waitForFile(filepath.Join(vDir, ".done"), 60*time.Second); err != nil {
				t.Fatalf("video pass never finished: %v", err)
			}

			ff := extinfs(t, readFile(t, filepath.Join(vDir, "video.m3u8")))
			ours := extinfs(t, servePlaylist(t, r, "/api/hls/playlist?file=movie.mp4&q=src&codec=av1"))
			if len(ours) != len(ff) {
				t.Fatalf("playlist declares %d segments, ffmpeg wrote %d: %v vs %v", len(ours), len(ff), ours, ff)
			}
			if n := writtenSegs(t, vDir); n != len(ours) {
				t.Errorf("playlist declares %d segments, %d .m4s on disk", len(ours), n)
			}

			for i := range ff {
				// The tail EXTINF rides the container duration, which can run
				// a frame past the video stream's own end.
				tol := 0.02
				if i == len(ff)-1 {
					tol = 0.2
				}
				if math.Abs(ours[i]-ff[i]) > tol {
					t.Errorf("seg %d EXTINF %.3f, ffmpeg wrote %.3f", i, ours[i], ff[i])
				}
			}
			for i := range ours {
				p, _ := segPaths(hash, CodecAV1, qSrc, i, 0, segVideoOnly, true)
				if _, err := os.Stat(p); err != nil {
					t.Fatalf("playlist references seg %d, not on disk: %v", i, err)
				}
			}

			// The audio pass has no keyframes to snap to, so it rides a
			// uniform grid of its own. It may keep a sub-frame runt past the
			// declared end, but the playlist must never come up long.
			step := audioStep(si)
			if err := ensureAV1Audio(src, hash, 0, step); err != nil {
				t.Fatal(err)
			}
			aDir := filepath.Dir(initPath(hash, "audio", 0, true))
			if err := waitForFile(filepath.Join(aDir, ".done"), 60*time.Second); err != nil {
				t.Fatalf("audio pass never finished: %v", err)
			}

			aff := extinfs(t, readFile(t, filepath.Join(aDir, "audio.m3u8")))
			aours := extinfs(t, servePlaylist(t, r, "/api/hls/playlist?file=movie.mp4&q=audio&audio=0&codec=av1"))
			if len(aours) > len(aff) {
				t.Errorf("audio playlist declares %d segments, ffmpeg wrote %d", len(aours), len(aff))
			}
			if len(aours) < len(aff)-1 {
				t.Errorf("audio playlist declares %d segments for the %d ffmpeg wrote — short by more than a tail runt", len(aours), len(aff))
			}
			t.Logf("gop=%ds → video %d segs %v, audio %d segs (step %.3f)", gop, len(ours), ours, len(aours), step)
		})
	}
}

// TestMuxerBounds_AbsoluteGrid pins the rule itself, with the numbers the
// empirical ffmpeg run produced for a 36s / 3s-GOP source.
func TestMuxerBounds_AbsoluteGrid(t *testing.T) {
	var kfs []float64
	for s := 0.0; s < 36; s += 3 {
		kfs = append(kfs, s)
	}

	got := muxerBounds(kfs, 0, 36, 4)
	want := []float64{0, 6, 9, 12, 18, 21, 24, 30, 33, 36}
	if len(got) != len(want) {
		t.Fatalf("bounds = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("bounds = %v, want %v", got, want)
		}
	}
}

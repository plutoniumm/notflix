package media

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"notflix/server/jobs"
	"notflix/server/library"
)

// A clip leaves this app for a phone's camera roll, a chat app, someone else's
// device — so it is H.264 + AAC in MP4 with the moov atom up front, whatever
// the source was. Most of this library is stored as AV1, which iOS Photos,
// Messages and WhatsApp all refuse: a `-c copy` clip would be unshareable,
// which is the only thing a clip is for.
const (
	// Long enough for a scene, short enough that the encode is over while the
	// user is still looking at the screen.
	ClipMaxSec = 120.0

	clipDir     = "./cache/clips"
	clipTTL     = 6 * time.Hour
	clipKeepMax = 24
	clipCRF     = 21
	clipMaxEdge = 1920
	// Bump when the encode recipe changes: the id is content-addressed, so a
	// stale file on disk would otherwise be served forever.
	clipVer = 1
)

var (
	clipPool = jobs.NewPool(2)
	clipIDRe = regexp.MustCompile(`^[0-9a-f]{16}$`)

	clipMu   sync.Mutex
	clipJobs = map[string]*ClipJob{}
)

type ClipJob struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Stage string  `json:"stage"`
	Pct   float64 `json:"pct"`
	URL   string  `json:"url"`
	Error string  `json:"error,omitempty"`

	path string
}

type clipReq struct {
	File string  `json:"file"`
	In   float64 `json:"in"`
	Out  float64 `json:"out"`
}

// Clamps rather than rejects at the edges — a drag past either end of the
// timeline is a normal gesture, not an error. Only an empty or over-long
// selection is refused.
func clipRange(in, out, dur float64) (float64, float64, error) {
	for _, v := range []float64{in, out} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, 0, errors.New("in and out must be numbers")
		}
	}

	in = math.Max(0, in)
	out = math.Max(0, out)
	if dur > 0 {
		in = math.Min(in, dur)
		out = math.Min(out, dur)
	}

	if out <= in {
		return 0, 0, errors.New("out must come after in")
	}
	if out-in > ClipMaxSec {
		return 0, 0, fmt.Errorf("a clip can be at most %ds", int(ClipMaxSec))
	}

	return in, out, nil
}

func clipID(rel string, in, out float64) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%d|%s|%.3f|%.3f", clipVer, rel, in, out))

	return hex.EncodeToString(sum[:8])
}

func clipPath(id string) string { return filepath.Join(clipDir, id+".mp4") }

var clipUnsafe = regexp.MustCompile(`[^\p{L}\p{N} _.,'()\[\]-]+`)

func clipStamp(sec float64) string {
	t := int(sec)

	return fmt.Sprintf("%02d.%02d.%02d", t/3600, (t/60)%60, t%60)
}

func clipName(abs string, in, out float64) string {
	base := library.CleanName(filepath.Base(abs))
	base = strings.TrimSpace(clipUnsafe.ReplaceAllString(base, " "))
	if base == "" {
		base = "clip"
	}

	return fmt.Sprintf("%s %s-%s.mp4", base, clipStamp(in), clipStamp(out))
}

// Short clips still pay a fixed open/seek/probe cost; the rest scales with the
// span, so a software libx264 fallback on a 4K source still finishes.
func clipTimeout(span float64) time.Duration {
	return time.Duration(45+span*3) * time.Second
}

// Display geometry, not stored geometry: an anamorphic source only lands square
// on a phone via an explicit scale + setsar=1. The long edge is capped so a 4K
// grab stays small enough to actually send.
func clipScale(si *srcInfo) string {
	h, dar := si.vHeight, si.vDAR
	if h <= 0 || dar <= 0 {
		return `scale=w=min(1920\,iw):h=min(1920\,ih):force_original_aspect_ratio=decrease:force_divisible_by=2,setsar=1`
	}

	w := float64(h) * dar
	if m := math.Max(w, float64(h)); m > clipMaxEdge {
		s := clipMaxEdge / m
		w, h = w*s, int(math.Round(float64(h)*s))
	}

	return fmt.Sprintf("scale=%d:%d:flags=lanczos,setsar=1", evenWidth(int(math.Round(w))), evenWidth(h))
}

// -ss ahead of -i is the FAST seek, and since ffmpeg 2.1 it is also the exact
// one when re-encoding: it lands on the previous keyframe and decodes-and-drops
// up to the target. Only `-c copy` snaps the cut to that keyframe.
// yuv420p is not decoration — a 10-bit source would otherwise produce High10,
// which no iPhone will decode.
func clipArgs(src, dst string, in, span float64) []string {
	args := []string{
		"-accurate_seek",
		"-ss", fmt.Sprintf("%.3f", in),
		"-i", src,
		"-t", fmt.Sprintf("%.3f", span),
		"-map", "0:v:0", "-map", "0:a:0?",
	}

	args = append(args, h264File(clipCRF)...)
	args = append(args,
		"-vf", clipScale(getSrcInfo(src)),
		"-pix_fmt", "yuv420p",
		"-profile:v", "high",
		"-c:a", pickAAC(), "-b:a", "160k", "-ac", "2", "-ar", "48000",
		"-movflags", "+faststart",
		"-f", "mp4", dst,
	)

	return args
}

func clipSet(id string, fn func(*ClipJob)) {
	clipMu.Lock()
	defer clipMu.Unlock()

	if j := clipJobs[id]; j != nil {
		fn(j)
	}
}

func runClip(j *ClipJob, src string, in, out float64) {
	clipPool.Acquire()
	defer clipPool.Release()

	span := out - in
	tmp := j.path + ".tmp"
	os.Remove(tmp)

	stderr, err := library.FFRun{
		Args:     clipArgs(src, tmp, in, span),
		Duration: span,
		OnPct:    func(p float64) { clipSet(j.ID, func(x *ClipJob) { x.Pct = p }) },
		Timeout:  clipTimeout(span),
	}.Run()

	if err == nil {
		err = os.Rename(tmp, j.path)
	}

	if err != nil {
		os.Remove(tmp)
		log.Printf("[clip] %s failed: %v\n%s", filepath.Base(src), err, stderr)
		clipSet(j.ID, func(x *ClipJob) {
			x.Stage = "error"
			x.Error = "could not cut this clip"
		})

		return
	}

	clipSet(j.ID, func(x *ClipJob) {
		x.Stage = "done"
		x.Pct = 100
	})
}

// Clips are disposable: they exist to be downloaded once. Age out first, then
// oldest-first down to a file count, so a long session can't fill the disk.
// Runs on every start rather than on a timer — nothing else creates them.
func evictClips() {
	clipMu.Lock()
	for id, j := range clipJobs {
		if j.Stage != "done" {
			continue
		}
		if _, err := os.Stat(j.path); err != nil {
			delete(clipJobs, id)
		}
	}
	clipMu.Unlock()

	ents, err := os.ReadDir(clipDir)
	if err != nil {
		return
	}

	type ent struct {
		path string
		mod  time.Time
	}
	var keep []ent
	now := time.Now()

	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}

		path := filepath.Join(clipDir, e.Name())
		age := now.Sub(fi.ModTime())

		// A .tmp this old belongs to an encode a restart killed.
		if strings.HasSuffix(e.Name(), ".tmp") {
			if age > time.Hour {
				os.Remove(path)
			}
			continue
		}

		if age > clipTTL {
			os.Remove(path)
			continue
		}

		keep = append(keep, ent{path, fi.ModTime()})
	}

	sort.Slice(keep, func(a, b int) bool { return keep[a].mod.After(keep[b].mod) })
	for _, e := range keep[min(len(keep), clipKeepMax):] {
		os.Remove(e.path)
	}
}

// Encoding takes seconds, not milliseconds, so this starts a job and answers
// immediately: a held-open request has nothing to report, dies to a proxy or a
// backgrounded tab, and cannot be turned into a download anyway — Safari only
// saves an attachment it navigated to, which has to be a second request.
func ClipStart(c *gin.Context, lib *library.Library) {
	var req clipReq
	if err := c.ShouldBindJSON(&req); err != nil {
		library.Error("bad request body", c, http.StatusBadRequest)
		return
	}

	// Never the raw param: FindFile resolves inside a root or returns nothing.
	abs := lib.FindFile(req.File)
	if abs == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if info, err := os.Stat(abs); err != nil || !info.Mode().IsRegular() {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	in, out, err := clipRange(req.In, req.Out, duration(abs))
	if err != nil {
		library.Error(err.Error(), c, http.StatusBadRequest)
		return
	}

	if !library.EnsureDir(clipDir) {
		library.Error("clip cache unavailable", c, http.StatusInternalServerError)
		return
	}
	evictClips()

	id := clipID(req.File, in, out)
	job := &ClipJob{
		ID:   id,
		Name: clipName(abs, in, out),
		URL:  "/api/clip/file?id=" + id,
		path: clipPath(id),
	}

	clipMu.Lock()
	// An identical request while one is in flight joins it instead of queueing
	// a second encode, so a mashed button costs nothing.
	prev := clipJobs[id]
	start := prev == nil || prev.Stage == "error"
	if start {
		if _, err := os.Stat(job.path); err == nil {
			job.Stage, job.Pct = "done", 100
			now := time.Now()
			os.Chtimes(job.path, now, now)
			start = false
		} else {
			job.Stage = "encoding"
		}
		clipJobs[id] = job
	}
	live := clipJobs[id]
	snap := *live
	clipMu.Unlock()

	if start {
		go runClip(live, abs, in, out)
	}

	c.JSON(http.StatusOK, snap)
}

func ClipStatus(c *gin.Context) {
	clipMu.Lock()
	j := clipJobs[c.Query("id")]
	var snap ClipJob
	if j != nil {
		snap = *j
	}
	clipMu.Unlock()

	if j == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no such clip"})
		return
	}

	c.JSON(http.StatusOK, snap)
}

// Safari ignores the HTML `download` attribute, so the save has to come from
// the response: Content-Disposition is the only thing that reaches it. On iOS
// the clip lands in Files, which is where a share sheet picks it up.
//
// Served off the id alone — the hex-only guard is the whole path check, and the
// in-memory job is consulted only for the download name. The client reaches
// this by NAVIGATION, so a restart that forgot the job must not answer 404 and
// take the player's page down with it; the file is content-addressed and still
// on disk.
func ClipFile(c *gin.Context) {
	id := c.Query("id")
	if !clipIDRe.MatchString(id) {
		c.Status(http.StatusBadRequest)
		return
	}

	path := clipPath(id)
	if _, err := os.Stat(path); err != nil {
		c.String(http.StatusNotFound, "clip expired")
		return
	}

	name := "clip-" + id + ".mp4"
	clipMu.Lock()
	if j := clipJobs[id]; j != nil && j.Name != "" {
		name = j.Name
	}
	clipMu.Unlock()

	c.Header("Content-Type", "video/mp4")
	c.Header("Content-Disposition", disposition(name))
	c.File(path)
}

// Two filename forms: the quoted one is ASCII-only for old parsers, filename*
// carries the real title. A quote or a separator would break out of the header,
// so neither survives into the fallback.
func disposition(name string) string {
	ascii := make([]rune, 0, len(name))
	for _, r := range name {
		if r < 32 || r > 126 || r == '"' || r == '\\' || r == '/' {
			ascii = append(ascii, '_')
			continue
		}
		ascii = append(ascii, r)
	}

	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`,
		string(ascii), url.PathEscape(name))
}

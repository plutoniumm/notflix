package media

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"notflix/server/jobs"
	"notflix/server/library"
	"notflix/server/x/atom"
)

// Frame grab. What's on screen is whatever HLS rung is playing, so a canvas
// capture is neither source resolution nor addressable by frame — both have to
// come from the original file, which means ffmpeg.
//
// One decode produces all five stills AND their strip previews: a full-res PNG
// is megabytes, so the picker shows the small ones and only the frame actually
// being looked at (or saved) pulls the real thing.

const (
	shotDir   = "./cache/shots"
	shotCount = 5 // the frame asked for, plus two either side
	shotPreW  = 360
	shotTTL   = 2 * time.Hour
	shotMax   = 24

	// Fine-seek pre-roll. The coarse seek before -i is keyframe-snapped, so
	// decoding a second of real frames before the first wanted one leaves the
	// reference chain primed.
	shotRoll = 1.0

	shotTimeout = 90 * time.Second
)

// Interactive, so no Nice — but still bounded: a mashed button must not fan out
// into twenty concurrent decodes.
var shotPool = jobs.NewPool(2)

var shotIDRe = regexp.MustCompile(`^\d{7}-\d+$`)

type shotGeom struct {
	rawW, rawH int // decoded frame size — ffmpeg applies the display matrix itself
	w, h       int // display size, SAR-corrected
	fps, dur   float64
}

type shot struct {
	ID    string    `json:"id"`
	Name  string    `json:"name"`
	FPS   float64   `json:"fps"`
	W     int       `json:"w"`
	H     int       `json:"h"`
	At    int       `json:"at"`
	Times []float64 `json:"times"`
}

type shotFrame struct {
	T    float64 `json:"t"`
	URL  string  `json:"url"`
	Prev string  `json:"prev"`
}

type shotReply struct {
	shot
	Frames []shotFrame `json:"frames"`
}

// Keyed on identity, not just path: convert.go rewrites files in place, and a
// path-only cache would then hand back the old geometry for good.
type geomEntry struct {
	g    shotGeom
	mod  time.Time
	size int64
}

var geomCache sync.Map // path -> geomEntry

func secStr(v float64) string { return strconv.FormatFloat(v, 'f', 6, 64) }

// r_frame_rate is the real per-frame rate; avg_frame_rate is a mean and drifts
// on VFR or telecined files, so it is only the fallback.
func probeShot(path string) (shotGeom, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return shotGeom{}, err
	}

	if v, ok := geomCache.Load(path); ok {
		if e := v.(geomEntry); e.mod.Equal(fi.ModTime()) && e.size == fi.Size() {
			return e.g, nil
		}
	}

	streams, err := library.Probes.Streams(path)
	if err != nil {
		return shotGeom{}, err
	}

	for _, s := range streams {
		if s.CodecType != "video" {
			continue
		}

		w, h := s.Width, s.Height
		sn, sd := s.SampleAspectRatio.Num(), s.SampleAspectRatio.Den()
		if sn <= 0 || sd <= 0 {
			sn, sd = 1, 1
		}

		dar := 0.0
		if w > 0 && h > 0 {
			dar = float64(w) * float64(sn) / (float64(h) * float64(sd))
		}

		// Coded dims are pre-rotation and ffmpeg autorotates on decode, so the
		// grab lands on the swapped grid whether or not we ask for it.
		if r := normRotation(path, s.Tags["rotate"]); r == 90 || r == 270 {
			w, h = h, w
			if dar > 0 {
				dar = 1 / dar
			}
		}

		g := shotGeom{rawW: w, rawH: h, w: w, h: h, dur: duration(path)}
		for _, r := range [][2]int{
			{s.RFrameRate.Num(), s.RFrameRate.Den()},
			{s.AvgFramerate.Num(), s.AvgFramerate.Den()},
		} {
			if r[0] > 0 && r[1] > 0 {
				g.fps = float64(r[0]) / float64(r[1])
				break
			}
		}
		if dar > 0 {
			g.w = evenWidth(int(math.Round(float64(h) * dar)))
		}

		if g.fps > 0 && g.w > 0 && g.h > 0 {
			geomCache.Store(path, geomEntry{g, fi.ModTime(), fi.Size()})
		}

		return g, nil
	}

	return shotGeom{}, fmt.Errorf("no video stream")
}

// shotTimes centres shotCount frame times on ts, one frame apart. Near either
// end the window SLIDES rather than running negative or past EOF, so `at` — the
// index of the frame the caller asked for — is not always the middle.
func shotTimes(ts, fps, dur float64) ([]float64, int, error) {
	if !(fps > 0) || math.IsInf(fps, 0) {
		return nil, 0, fmt.Errorf("unknown frame rate")
	}

	if math.IsNaN(ts) || ts < 0 || (dur > 0 && ts > dur) {
		return nil, 0, fmt.Errorf("timestamp out of range")
	}

	step := 1 / fps
	last := math.Inf(1)
	if dur > 0 {
		last = math.Max(0, dur-shotCount*step)
	}
	start := math.Min(math.Max(ts-(shotCount-1)/2*step, 0), last)

	times := make([]float64, shotCount)
	for i := range times {
		times[i] = start + float64(i)*step
	}

	at := min(max(int(math.Round((ts-start)/step)), 0), shotCount-1)

	return times, at, nil
}

// The decoded frame is already rotated and already the coded size, so only a
// non-square SAR is left to correct; square-pixel sources skip the filter
// entirely and stay a pure decode.
func deanamorph(g shotGeom) string {
	if g.w <= 0 || g.h <= 0 || g.w == g.rawW {
		return ""
	}

	return fmt.Sprintf("scale=%d:%d:flags=lanczos,setsar=1", g.w, g.h)
}

func previewFilter(g shotGeom) string {
	w := evenWidth(min(shotPreW, g.w))
	h := evenWidth(int(math.Round(float64(w) * float64(g.h) / float64(g.w))))

	return fmt.Sprintf("scale=%d:%d:flags=lanczos,setsar=1", w, h)
}

// grabStrip decodes once and writes both sets. Two gotchas are load-bearing:
// -fps_mode passthrough (image2 otherwise resamples to its own output rate and
// hands back duplicated frames) and the half-frame slack on the seek (the
// output-side -ss keeps frames at or after it, and a target landing exactly on
// a frame's pts rounds either way in float, dropping it).
func grabStrip(src, dst string, g shotGeom, times []float64) error {
	seek := math.Max(0, times[0]-0.5/g.fps)
	coarse := math.Max(0, seek-shotRoll)
	fine := seek - coarse

	out := func(vf, pattern string, extra ...string) []string {
		a := []string{"-ss", secStr(fine), "-frames:v", strconv.Itoa(shotCount), "-fps_mode", "passthrough"}
		if vf != "" {
			a = append(a, "-vf", vf)
		}
		a = append(a, extra...)

		return append(a, "-f", "image2", filepath.Join(dst, pattern))
	}

	args := []string{"-y", "-ss", secStr(coarse), "-i", src}
	args = append(args, out(deanamorph(g), "f%d.png")...)
	args = append(args, out(previewFilter(g), "p%d.jpg", "-q:v", "4")...)

	if stderr, err := (library.FFRun{Args: args, Timeout: shotTimeout}).Run(); err != nil {
		return library.FFErr(stderr, err)
	}

	for i := 1; i <= shotCount; i++ {
		if _, err := os.Stat(filepath.Join(dst, fmt.Sprintf("f%d.png", i))); err != nil {
			return fmt.Errorf("frame %d missing: %w", i, err)
		}
	}

	return nil
}

// Built aside and renamed in, so two clients asking for the same frame never
// see a half-written strip and neither needs a lock.
func makeShot(src string, g shotGeom, s shot) error {
	shotPool.Acquire()
	defer shotPool.Release()

	tmp := shotPath(s.ID) + ".tmp"
	os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0755); err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	if err := grabStrip(src, tmp, g, s.Times); err != nil {
		return err
	}

	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := atom.WriteFile(filepath.Join(tmp, "shot.json"), b, 0644); err != nil {
		return err
	}

	sweepShots()
	if err := os.Rename(tmp, shotPath(s.ID)); err != nil {
		if _, e := readShot(s.ID); e == nil {
			return nil // lost the race; the winner's strip is identical
		}

		return err
	}

	return nil
}

// sweepShots keeps the shot cache bounded. It lives outside the ./cache/<hash>
// layout CleanHLSCache walks, so nothing else prunes it.
func sweepShots() {
	entries, err := os.ReadDir(shotDir)
	if err != nil {
		return
	}

	type aged struct {
		name string
		at   time.Time
	}
	var live []aged
	now := time.Now()

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		fi, err := e.Info()
		if err != nil {
			continue
		}

		if now.Sub(fi.ModTime()) > shotTTL {
			os.RemoveAll(filepath.Join(shotDir, e.Name()))
			continue
		}

		live = append(live, aged{e.Name(), fi.ModTime()})
	}

	if len(live) <= shotMax {
		return
	}

	sort.Slice(live, func(i, j int) bool { return live[i].at.Before(live[j].at) })
	for _, e := range live[:len(live)-shotMax] {
		os.RemoveAll(filepath.Join(shotDir, e.name))
	}
}

func shotPath(id string) string { return filepath.Join(shotDir, id) }

func readShot(id string) (shot, error) {
	var s shot
	b, err := os.ReadFile(filepath.Join(shotPath(id), "shot.json"))
	if err != nil {
		return s, err
	}

	return s, json.Unmarshal(b, &s)
}

// Path separators and quotes out; the rest of the name is the user's own file
// and belongs in the download.
func shotName(name string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == '"' || r < 0x20 {
			return '-'
		}

		return r
	}, name)
}

func hms(t float64) string {
	h, rem := int(t)/3600, math.Mod(t, 3600)
	m, s := int(rem)/60, math.Mod(rem, 60)
	if h > 0 {
		return fmt.Sprintf("%dh%02dm%06.3fs", h, m, s)
	}

	return fmt.Sprintf("%02dm%06.3fs", m, s)
}

// Frames extracts the frame at ?t= plus its two neighbours either side and
// answers with their metadata; the images themselves come from FrameImage, so
// the browser gets five small previews immediately and pays for a full-res PNG
// only for the frame actually being looked at.
func Frames(c *gin.Context, lib *library.Library) {
	rel := strings.TrimPrefix(c.Query("file"), "/")
	src := library.FindFile(rel, lib.Roots)
	if src == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})

		return
	}

	ts, err := strconv.ParseFloat(c.Query("t"), 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad t"})

		return
	}

	g, err := probeShot(src)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "probe failed: " + err.Error()})

		return
	}

	times, at, err := shotTimes(ts, g.fps, g.dur)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})

		return
	}

	s := shot{
		ID:    fmt.Sprintf("%s-%d", library.Hash(rel), int64(math.Round(times[0]*1000))),
		Name:  strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel)),
		FPS:   g.fps,
		W:     g.w,
		H:     g.h,
		At:    at,
		Times: times,
	}

	if _, err := readShot(s.ID); err != nil {
		if err := makeShot(src, g, s); err != nil {
			log.Printf("[frames] %s @ %.3f: %v", filepath.Base(rel), ts, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "frame extraction failed"})

			return
		}
	}

	reply := shotReply{shot: s, Frames: make([]shotFrame, shotCount)}
	for i, t := range times {
		reply.Frames[i] = shotFrame{
			T:    t,
			URL:  fmt.Sprintf("/api/frames/img?shot=%s&i=%d", s.ID, i),
			Prev: fmt.Sprintf("/api/frames/img?shot=%s&i=%d&p=1", s.ID, i),
		}
	}

	c.JSON(http.StatusOK, reply)
}

// FrameImage serves one still from a shot: ?p=1 for the strip preview, ?dl=1 to
// save it. Safari ignores the HTML download attribute on an <a>, so the save has
// to come back as an attachment — the same reason player.go does it.
func FrameImage(c *gin.Context) {
	id := c.Query("shot")
	if !shotIDRe.MatchString(id) {
		c.Status(http.StatusBadRequest)

		return
	}

	i, err := strconv.Atoi(c.Query("i"))
	if err != nil || i < 0 || i >= shotCount {
		c.Status(http.StatusBadRequest)

		return
	}

	file, ct, ext := fmt.Sprintf("f%d.png", i+1), "image/png", ".png"
	if c.Query("p") == "1" {
		file, ct, ext = fmt.Sprintf("p%d.jpg", i+1), "image/jpeg", ".jpg"
	}

	path := filepath.Join(shotPath(id), file)
	if _, err := os.Stat(path); err != nil {
		c.Status(http.StatusNotFound)

		return
	}

	if c.Query("dl") == "1" {
		name := id
		if s, err := readShot(id); err == nil && i < len(s.Times) {
			name = fmt.Sprintf("%s @ %s", s.Name, hms(s.Times[i]))
		}
		c.Header("Content-Disposition", `attachment; filename="`+shotName(name)+ext+`"`)
	}

	c.Header("Content-Type", ct)
	c.Header("Cache-Control", "private, max-age=3600")
	c.File(path)
}

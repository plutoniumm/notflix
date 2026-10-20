package media

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
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

// A downloaded subtitle was timed against somebody else's release: a different
// intro, a different cut, or a different frame rate. The fix is the transform
// t_video = scale·t_sub + offset, found by sliding the subtitle's cue/no-cue
// pattern over the film's speech activity until the two agree best.
//
// The sidecar is never rewritten. The fit lives in KV as `subsync:<basename
// hash>` and is applied as the file is served, so undoing it is deleting a key.

const (
	vadRate = 8000
	vadHop  = 80 // 10ms frames
	vadFPS  = vadRate / vadHop
	syncWin = 300.0 // seconds searched either way
	// Window, in seconds, whose mean is taken off the speech signal. Measured
	// on three titles: none, 30, 16, 8, 4, 2 gave a peak of z≈4, 8–14, 10–17,
	// 12–20, 14–22, 14–26 — and with none the Korean film never aligned at all.
	// 2 starts eating into the cue-level structure the fit is made of.
	syncDetrend = 4
	// Below this many cues it is a forced-subs track (signs, the odd foreign
	// line), and any peak it makes is luck.
	syncMinCues = 60
	// How far, in scale 1's standard deviations, another frame rate must beat
	// the native one. Measured: a real 4% PAL mismatch wins by 10–41 and a real
	// 0.1% one by 4–39, except on one 25-minute episode (1.3, so it gets the
	// best constant offset instead: ≤0.74s left); the wrong rate on a
	// correctly-timed file never got above +0.3.
	syncScaleEdge = 3.0
)

// The frame-rate pairs releases actually disagree on: NTSC film vs PAL
// speed-up, and 24 vs 23.976.
var syncScales = []float64{1, 25 / 23.976, 23.976 / 25, 25.0 / 24, 24.0 / 25, 24 / 23.976, 23.976 / 24}

type syncFit struct {
	Scale   float64 `json:"scale"`
	Offset  float64 `json:"offset"`
	Score   float64 `json:"score"`
	Base    float64 `json:"base"`
	Peak    float64 `json:"peak"`
	Lead    float64 `json:"lead"`
	Gain    float64 `json:"gain"`
	Applied bool    `json:"applied"`
	Verdict string  `json:"verdict"`
	Size    int64   `json:"size,omitempty"`
}

type span struct{ a, b float64 }

// One extraction at a time: two decodes of the same film would read it twice,
// and the second caller finds the first one's cache once it gets the lock.
var speechMu sync.Mutex

func speechPath(path string) string {
	return filepath.Join("./cache", "speech", library.Hash(filepath.Base(path))+".bin")
}

// speech is the film's voice-band loudness in dB, one byte per 10ms frame,
// cached on disk behind a size+mtime stamp.
//
// Never niced. Every caller follows a click in the player with the viewer
// waiting, and under the throttled IO tier a film's audio took minutes to read
// where it takes 2–60s at normal priority.
func speech(path string) ([]byte, error) {
	speechMu.Lock()
	defer speechMu.Unlock()

	size, mod, ok := stampOf(path)
	if !ok {
		return nil, os.ErrNotExist
	}

	cp := speechPath(path)
	if b, err := os.ReadFile(cp); err == nil && len(b) > 16 &&
		int64(binary.LittleEndian.Uint64(b)) == size && int64(binary.LittleEndian.Uint64(b[8:])) == mod {
		return b[16:], nil
	}

	tmp, err := os.CreateTemp("", "speech*.raw")
	if err != nil {
		return nil, err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	args := []string{"-i", path, "-map", "0:a:0", "-vn", "-sn", "-dn",
		"-af", "highpass=f=300,lowpass=f=3000", "-ac", "1", "-ar", strconv.Itoa(vadRate),
		"-f", "s16le", "-y", tmp.Name()}
	if stderr, err := (library.FFRun{Args: args, Timeout: 30 * time.Minute}).Run(); err != nil {
		return nil, fmt.Errorf("speech: %w — %s", err, stderr)
	}

	f, err := os.Open(tmp.Name())
	if err != nil {
		return nil, err
	}
	defer f.Close()

	env := make([]byte, 16, 16+1<<20)
	binary.LittleEndian.PutUint64(env, uint64(size))
	binary.LittleEndian.PutUint64(env[8:], uint64(mod))

	r := bufio.NewReaderSize(f, 1<<16)
	frame := make([]byte, 2*vadHop)
	for {
		if _, err := io.ReadFull(r, frame); err != nil {
			break
		}

		var sum float64
		for i := 0; i < vadHop; i++ {
			v := float64(int16(binary.LittleEndian.Uint16(frame[2*i:])))
			sum += v * v
		}
		env = append(env, byte(min(255, 10*math.Log10(sum/vadHop+1))))
	}

	os.MkdirAll(filepath.Dir(cp), 0755)
	atom.WriteFile(cp, env, 0644)

	return env[16:], nil
}

// spans merges overlapping cues, so a frame two cues share counts once.
func spans(cues []jobs.Cue) []span {
	out := make([]span, 0, len(cues))
	for _, c := range cues {
		if c.End > c.Start {
			out = append(out, span{c.Start, c.End})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].a < out[j].a })

	merged := out[:0]
	for _, s := range out {
		if n := len(merged); n > 0 && s.a <= merged[n-1].b {
			merged[n-1].b = max(merged[n-1].b, s.b)

			continue
		}
		merged = append(merged, s)
	}

	return merged
}

// voiced turns loudness into ±1 and returns its prefix sums. The threshold is
// set so the voiced share of the film matches the subtitle's coverage: a fixed
// dB level would call a quiet drama silent and a loud action film all speech.
//
// Each frame then has its neighbourhood's mean taken off. Dialogue density
// drifts over minutes (a talky scene, a chase), and correlating that drift
// buried the sharp peak of the true alignment under broad false hills.
func voiced(env []byte, sp []span) []float64 {
	n := len(env)

	var covered float64
	for _, s := range sp {
		covered += s.b - s.a
	}
	share := min(0.95, max(0.05, covered*vadFPS/float64(n)))

	var hist [256]int
	for _, v := range env {
		hist[v]++
	}
	thr, acc := 255, 0
	for ; thr > 0; thr-- {
		acc += hist[thr]
		if float64(acc) >= share*float64(n) {
			break
		}
	}

	raw := make([]float64, n+1)
	for i, v := range env {
		x := -1.0
		if int(v) >= thr {
			x = 1
		}
		raw[i+1] = raw[i] + x
	}

	h := syncDetrend * vadFPS / 2
	p := make([]float64, n+1)
	for i := 0; i < n; i++ {
		lo, hi := max(0, i-h), min(n, i+h+1)
		x := raw[i+1] - raw[i]
		p[i+1] = p[i] + x - (raw[hi]-raw[lo])/float64(hi-lo)
	}

	return p
}

// agree is the ±1 correlation between speech and "a cue is showing" once the
// cues are mapped through (scale, offset). Outside a cue the subtitle claims
// silence, so Σx·y = 2·Σ_cues x − Σ_all x, and prefix sums make each cue O(1).
func agree(p []float64, sp []span, scale, off float64) float64 {
	n := len(p) - 1
	at := func(t float64) int {
		return min(n, max(0, int(math.Round((scale*t+off)*vadFPS))))
	}

	var sum float64
	for _, s := range sp {
		sum += p[at(s.b)] - p[at(s.a)]
	}

	return (2*sum - p[n]) / float64(n)
}

// floor describes a correlation curve away from its peak: the ±2s lobe around
// `at` is cut out, and what is left is what chance alignment looks like.
func floor(curve []float64, at int) (mean, sd, rival float64) {
	var sum, sq float64
	rival = -2
	k := 0
	for i, v := range curve {
		if i >= at-20 && i <= at+20 {
			continue
		}
		sum += v
		sq += v * v
		rival = max(rival, v)
		k++
	}

	if k < 2 {
		return 0, 1, rival
	}
	mean = sum / float64(k)

	return mean, math.Sqrt(max(1e-12, sq/float64(k)-mean*mean)), rival
}

func align(env []byte, cues []jobs.Cue) syncFit {
	sp := spans(cues)
	if len(sp) < syncMinCues || len(env) == 0 {
		return syncFit{Scale: 1}
	}

	p := voiced(env, sp)

	const steps = int(syncWin * 10)
	curves := make([][]float64, len(syncScales))
	tops := make([]int, len(syncScales))
	for j, s := range syncScales {
		c := make([]float64, 2*steps+1)
		for i := range c {
			c[i] = agree(p, sp, s, float64(i-steps)/10)
			if c[i] > c[tops[j]] {
				tops[j] = i
			}
		}
		curves[j] = c
	}

	// Over a short file a 0.1% stretch is nearly the same thing as a small
	// offset, and noise decides between them: a 15-minute clip fitted 24/23.976
	// at −3.97s when the truth was 1.0 at −3.2s. So the native rate wins unless
	// another one is clearly better, measured in scale 1's own spread.
	_, sd1, _ := floor(curves[0], tops[0])
	pick := 0
	for j := 1; j < len(syncScales); j++ {
		if curves[j][tops[j]]-curves[0][tops[0]] >= syncScaleEdge*sd1 && curves[j][tops[j]] > curves[pick][tops[pick]] {
			pick = j
		}
	}

	fit := syncFit{Scale: syncScales[pick], Offset: float64(tops[pick]-steps) / 10, Score: curves[pick][tops[pick]], Base: agree(p, sp, 1, 0)}
	coarse := fit.Offset
	for d := -10; d <= 10; d++ {
		o := coarse + float64(d)/100
		if v := agree(p, sp, fit.Scale, o); v > fit.Score {
			fit.Offset, fit.Score = o, v
		}
	}

	// The peak is judged in standard deviations of its own curve: how far it
	// stands above the floor, above the next best place the subtitle could
	// have gone, and above leaving it where it is.
	mean, sd, rival := floor(curves[pick], tops[pick])
	fit.Peak = (fit.Score - mean) / sd
	fit.Lead = (fit.Score - rival) / sd
	fit.Gain = (fit.Score - fit.Base) / sd

	return fit
}

// The gate, in standard deviations of the fit's own curve. Measured: true
// alignments on six titles scored peak 14–26 / lead 11–22; the same films
// given the WRONG subtitle (sibling episode, another film) never beat peak 4.8
// / lead 1.5. Gain keeps a correct file from being nudged by the subtitler's
// habitual lead on speech onset, which is a fraction of a second.
const (
	syncMinPeak  = 8.0
	syncMinLead  = 4.0
	syncMinGain  = 2.0
	syncNoopSecs = 0.25
)

func verdict(f syncFit) string {
	switch {
	case f.Scale == 1 && math.Abs(f.Offset) < syncNoopSecs:
		return "in sync"
	case f.Peak < syncMinPeak || f.Lead < syncMinLead || f.Gain < syncMinGain:
		return "unsure"
	}

	return "shifted"
}

func confident(f syncFit) bool {
	return verdict(f) == "shifted"
}

func syncKey(sub string) string {
	return "subsync:" + library.Hash(filepath.Base(sub))
}

// subShift returns the stored fit for a sidecar, ignoring one recorded against
// different bytes: a deleted and re-downloaded `.eng.vtt` reuses the name.
func subShift(sub string) (syncFit, bool) {
	v := library.KVGetValue(syncKey(sub))
	if v == nil {
		return syncFit{}, false
	}

	var f syncFit
	b, _ := json.Marshal(v)
	if json.Unmarshal(b, &f) != nil {
		return syncFit{}, false
	}

	fi, err := os.Stat(sub)
	if err != nil || fi.Size() != f.Size {
		return syncFit{}, false
	}

	return f, true
}

// SyncSub fits a sidecar against its video and stores the fit when it is
// clearly better than leaving the file alone. A fit that isn't confident
// clears any earlier one, so re-running never leaves a stale shift behind.
func SyncSub(video, sub string) (syncFit, error) {
	raw, err := os.ReadFile(sub)
	if err != nil {
		return syncFit{}, err
	}

	env, err := speech(video)
	if err != nil {
		return syncFit{}, err
	}

	t0 := time.Now()
	fit := align(env, jobs.ParseCues(decodeText(string(raw))))
	fit.Verdict = verdict(fit)
	fit.Applied = fit.Verdict == "shifted"
	log.Printf("[subsync] %s: scale=%.5f offset=%+.2fs peak=%.1f lead=%.1f gain=%.1f → %s (%s)",
		filepath.Base(sub), fit.Scale, fit.Offset, fit.Peak, fit.Lead, fit.Gain, fit.Verdict, time.Since(t0).Round(time.Millisecond))

	if !fit.Applied {
		return fit, library.KVDelete(syncKey(sub))
	}

	fit.Size = int64(len(raw))

	return fit, library.KVSetValue(syncKey(sub), fit)
}

// autoSync runs after a provider download lands. Never for local release,
// embedded or whisper tracks: those are timed against this very file.
func autoSync(video, sub string) {
	if library.IsBusy(video) {
		return
	}

	if _, err := SyncSub(video, sub); err != nil {
		log.Printf("[subsync] %s: %v", filepath.Base(sub), err)
	}
}

var stampRe = regexp.MustCompile(`(?:(\d+):)?(\d{1,2}):(\d{2})[.,](\d{3})`)

func shiftVTT(raw string, scale, off float64) string {
	lines := strings.Split(raw, "\n")
	for i, l := range lines {
		if !strings.Contains(l, "-->") {
			continue
		}

		lines[i] = stampRe.ReplaceAllStringFunc(l, func(s string) string {
			m := stampRe.FindStringSubmatch(s)
			h, _ := strconv.Atoi(m[1])
			mi, _ := strconv.Atoi(m[2])
			se, _ := strconv.Atoi(m[3])
			ms, _ := strconv.Atoi(m[4])
			t := float64(h*3600+mi*60+se) + float64(ms)/1000

			x := int64(math.Round(max(0, scale*t+off) * 1000))

			return fmt.Sprintf("%02d:%02d:%02d.%03d", x/3600000, x/60000%60, x/1000%60, x%1000)
		})
	}

	return strings.Join(lines, "\n")
}

// sendVTT serves a sidecar with its fit applied. no-cache because the bytes
// can change while the file doesn't: a heuristic-fresh copy would keep showing
// the old timing after a sync.
func sendVTT(c *gin.Context, path, body string) {
	c.Header("Cache-Control", "no-cache")

	fit, shifted := subShift(path)
	if !shifted && body == "" {
		c.Header("Content-Type", "text/vtt")
		c.File(path)

		return
	}

	if body == "" {
		b, err := os.ReadFile(path)
		if err != nil {
			c.String(http.StatusNotFound, "Subtitle not found")

			return
		}
		body = string(b)
	}

	if shifted {
		body = shiftVTT(body, fit.Scale, fit.Offset)
	}

	c.Data(http.StatusOK, "text/vtt; charset=utf-8", []byte(body))
}

// subOf resolves a sidecar named relative to its video. Only the base name is
// honoured, so a request can't step out of the video's directory.
func subOf(lib *library.Library, file, sub string) (string, string, bool) {
	video, ok := lib.FindVid(file)
	if !ok {
		return "", "", false
	}

	s := filepath.Join(filepath.Dir(video), filepath.Base(sub))
	if strings.ToLower(filepath.Ext(s)) != ".vtt" {
		return "", "", false
	}

	if _, err := os.Stat(s); err != nil {
		return "", "", false
	}

	return video, s, true
}

func SubsSync(c *gin.Context, lib *library.Library) {
	var body struct {
		File string `json:"file"`
		Sub  string `json:"sub"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})

		return
	}

	video, sub, ok := subOf(lib, body.File, body.Sub)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "subtitle not found"})

		return
	}

	if library.IsBusy(video) {
		c.JSON(http.StatusConflict, gin.H{"error": "video is still being written"})

		return
	}

	fit, err := SyncSub(video, sub)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})

		return
	}

	c.JSON(http.StatusOK, fit)
}

func SubsUnsync(c *gin.Context, lib *library.Library) {
	_, sub, ok := subOf(lib, c.Query("file"), c.Query("sub"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "subtitle not found"})

		return
	}

	if err := library.KVDelete(syncKey(sub)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})

		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

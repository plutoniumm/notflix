package media

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"notflix/server/jobs"
	"notflix/server/library"
	"notflix/server/x/atom"
	"notflix/server/x/httpc"
)

// Poster selection. The old thumbnail was one ffmpeg grab at duration/2 ± 5s,
// which lands on a black frame, a fade or the middle of the credits often
// enough to be the most visible flaw in a poster-grid UI.
//
// The candidates are already on disk: the trickplay sprite sheet holds one
// frame every SpriteIntervalSec. So this pass decodes that sheet, scores every
// tile with cheap classical statistics, and only then — optionally — asks a
// vision model to choose among the handful of survivors.
//
// The classical stage is the product. The vision stage is a garnish that must
// never block it: the ollama host is a Tailscale peer that sleeps, and a hung
// call there once wedged a whole worker pool.

const (
	// Trim head and tail: distributor logos, cold-open title cards and end
	// credits live there and are never a good poster. 5% of a 24-minute
	// episode is ~70s, of a 2-hour film ~6 minutes — about right for both.
	posterTrim = 0.05

	// How many classical survivors the vision model gets to choose between.
	// Six fits in one request comfortably and the classical top-6 are already
	// all defensible posters, so a parse failure costs nothing.
	posterFinalists = 6
)

// Hard rejects. These are absolute thresholds on 0..1 luma, so a genuinely
// dark film can have every tile rejected — rank still orders them by raw score
// in that case, so the least-bad frame ships rather than nothing.
const (
	rejectDark   = 0.06  // mean luma: fade-to-black, unlit shot
	rejectBright = 0.90  // mean luma: fade-to-white, blown-out flash frame
	rejectFlat   = 0.05  // luma stddev: solid colour card, blank wall, fade mid-way
	rejectSmooth = 0.008 // edge energy: out of focus, motion-blurred, or empty
)

// Score weights and the value at which each component saturates. Tuned against
// all 1958 sprite sheets in the live library, where real content tiles sit
// around edge 0.03-0.07, stddev 0.13-0.26, colourfulness 0.08-0.30. Detail
// carries the most weight because it is the single best "this is a real,
// in-focus, composed shot" signal; exposure carries the least because a
// deliberately dark or bright frame can still be a great poster. Over that
// library the old midpoint grab was hard-rejected for 5.7% of files (median
// score 0.588, p10 0.298); this ranking rejects 0.1% (median 0.878, p10 0.772).
const (
	wDetail   = 0.34
	wContrast = 0.26
	wColor    = 0.22
	wExposure = 0.18

	fullDetail   = 0.055
	fullContrast = 0.24
	fullColor    = 0.28

	// Target mean luma for a well-exposed frame. Slightly under mid-grey
	// because letterbox bars pull the mean of a scope film down and we do not
	// want to systematically prefer the brightest shot in the movie.
	idealLuma = 0.42
)

// posterKey marks a video on the move-proof BASENAME hash, the same scheme
// codecKey and skipKey use: a dir-qualified key orphans the moment CleanAll or
// Organize moves the file, and the pass then re-picks a poster it already
// picked. Presence of the mark means the choice was made.
func posterKey(file string) string {
	return posterMark(library.Hash(filepath.Base(file)))
}

// posterMark is the same key built from an already-hashed sprite key.
func posterMark(key string) string {
	return "poster:" + key
}

func hasPoster(key string) bool {
	return library.KVGetValue(posterMark(key)) != nil
}

// stats reduces one tile to the four numbers every decision below is made
// from: mean and stddev of luma, Hasler-Süsstrunk colourfulness, and mean
// absolute luma gradient (edge energy). One pass over the pixels; the luma
// plane is kept so the gradient does not re-convert colour.
func stats(img image.Image) (mean, sd, colorful, edge float64) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 3 || h < 3 {
		return
	}

	lum := make([]float64, w*h)
	var sum, sumsq, rgSum, rgSq, ybSum, ybSq float64

	for y := range h {
		for x := range w {
			r32, g32, b32, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			r, g, bl := float64(r32>>8), float64(g32>>8), float64(b32>>8)

			l := (0.2126*r + 0.7152*g + 0.0722*bl) / 255
			lum[y*w+x] = l
			sum += l
			sumsq += l * l

			rg := (r - g) / 255
			yb := (0.5*(r+g) - bl) / 255
			rgSum += rg
			rgSq += rg * rg
			ybSum += yb
			ybSq += yb * yb
		}
	}

	n := float64(w * h)
	mean = sum / n
	sd = math.Sqrt(math.Max(sumsq/n-mean*mean, 0))

	rgM, ybM := rgSum/n, ybSum/n
	rgSD := math.Sqrt(math.Max(rgSq/n-rgM*rgM, 0))
	ybSD := math.Sqrt(math.Max(ybSq/n-ybM*ybM, 0))
	colorful = math.Hypot(rgSD, ybSD) + 0.3*math.Hypot(rgM, ybM)

	var e float64
	for y := range h - 1 {
		for x := range w - 1 {
			c := lum[y*w+x]
			e += math.Abs(lum[y*w+x+1]-c) + math.Abs(lum[(y+1)*w+x]-c)
		}
	}
	edge = e / (2 * float64((w-1)*(h-1)))

	return
}

// scoreTile is the classical judge. ok is false for a frame that is disqualified
// outright; score still ranks it, so an all-dark film degrades to "least bad"
// instead of to nothing.
func scoreTile(img image.Image) (score float64, ok bool) {
	mean, sd, colorful, edge := stats(img)

	detail := math.Min(edge/fullDetail, 1)
	contrast := math.Min(sd/fullContrast, 1)
	color := math.Min(colorful/fullColor, 1)
	exposure := math.Max(1-math.Abs(mean-idealLuma)/idealLuma, 0)

	score = wDetail*detail + wContrast*contrast + wColor*color + wExposure*exposure
	ok = mean >= rejectDark && mean <= rejectBright && sd >= rejectFlat && edge >= rejectSmooth

	return score, ok
}

// loadSheet decodes the sprite JPEG and its manifest, repairing the two fields
// that can legitimately be missing: TileH is 0 when the post-generation probe
// failed, and Count can overshoot the tiles ffmpeg actually emitted.
func loadSheet(key string) (image.Image, SpriteManifest, error) {
	var m SpriteManifest
	jpgPath, mfstPath := spritePaths(key)

	if err := atom.LoadJSON(mfstPath, &m); err != nil {
		return nil, m, err
	}

	if m.Cols <= 0 || m.Rows <= 0 || m.Interval <= 0 {
		return nil, m, fmt.Errorf("sprite manifest %s: cols=%d rows=%d interval=%d", key, m.Cols, m.Rows, m.Interval)
	}

	f, err := os.Open(jpgPath)
	if err != nil {
		return nil, m, err
	}
	defer f.Close()

	sheet, err := jpeg.Decode(f)
	if err != nil {
		return nil, m, fmt.Errorf("sprite jpeg %s: %w", key, err)
	}

	b := sheet.Bounds()
	if m.TileW <= 0 {
		m.TileW = b.Dx() / m.Cols
	}
	if m.TileH <= 0 {
		m.TileH = b.Dy() / m.Rows
	}
	if m.TileW <= 0 || m.TileH <= 0 {
		return nil, m, fmt.Errorf("sprite %s: degenerate tile %dx%d", key, m.TileW, m.TileH)
	}

	m.Count = min(m.Count, m.Cols*m.Rows)

	return sheet, m, nil
}

// tileAt copies tile i out of the sheet into its own origin-zero image. The
// copy is deliberate: sub-images carry a non-zero origin that both the JPEG
// encoder and the base64 path would otherwise have to reason about.
func tileAt(sheet image.Image, m SpriteManifest, i int) image.Image {
	x := (i % m.Cols) * m.TileW
	y := (i / m.Cols) * m.TileH

	out := image.NewRGBA(image.Rect(0, 0, m.TileW, m.TileH))
	src := sheet.Bounds().Min.Add(image.Pt(x, y))
	draw.Draw(out, out.Bounds(), sheet, src, draw.Src)

	return out
}

// rank scores every in-range tile and returns indices best-first: every
// accepted tile ahead of every rejected one, each group by descending score.
func rank(sheet image.Image, m SpriteManifest) []int {
	lo := int(float64(m.Count) * posterTrim)
	hi := m.Count - lo
	if hi-lo < 1 {
		lo, hi = 0, m.Count
	}

	type cand struct {
		idx   int
		score float64
		ok    bool
	}

	var cands []cand
	for i := lo; i < hi; i++ {
		s, ok := scoreTile(tileAt(sheet, m, i))
		cands = append(cands, cand{i, s, ok})
	}

	sort.SliceStable(cands, func(a, b int) bool {
		if cands[a].ok != cands[b].ok {
			return cands[a].ok
		}

		return cands[a].score > cands[b].score
	})

	out := make([]int, len(cands))
	for i, c := range cands {
		out[i] = c.idx
	}

	return out
}

// spread thins a best-first ranking so the finalists are not six frames of one
// shot. Consecutive tiles are 10s apart, so neighbours are usually the same
// camera setup; requiring a gap buys the vision model genuinely different
// options for free.
func spread(ranked []int, n, minGap int) []int {
	var out []int

	for _, i := range ranked {
		if len(out) >= n {
			break
		}

		near := false
		for _, j := range out {
			if abs(i-j) < minGap {
				near = true

				break
			}
		}
		if !near {
			out = append(out, i)
		}
	}

	// A very short video may not have n well-separated tiles; backfill rather
	// than hand the model a one-image "choice".
	for _, i := range ranked {
		if len(out) >= n {
			break
		}
		if !slicesHas(out, i) {
			out = append(out, i)
		}
	}

	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}

	return n
}

func slicesHas(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}

	return false
}

// PickPoster chooses and writes images/<key>.jpg. Idempotent: a stamped
// poster: key means the choice was already made.
func PickPoster(src, key string) error {
	if hasPoster(key) {
		return nil
	}

	sheet, m, err := loadSheet(key)
	if err != nil {
		return err
	}
	if m.Count < 1 {
		return fmt.Errorf("sprite %s: no tiles", key)
	}

	ranked := rank(sheet, m)
	if len(ranked) == 0 {
		return fmt.Errorf("sprite %s: nothing to rank", key)
	}

	best := ranked[0]
	finalists := spread(ranked, posterFinalists, 2)

	if len(finalists) > 1 {
		if i, ok := visionPick(sheet, m, finalists); ok {
			best = finalists[i]
		}
	}

	if err := writePoster(src, key, best, sheet, m); err != nil {
		return err
	}

	return library.KVSetValue(posterMark(key), best)
}

// writePoster prefers a full-resolution grab at the winning tile's timestamp:
// the tiles are 160px wide and the poster grid renders at 1080p, so shipping
// the crop would trade one visible flaw for another. The sheet already told us
// WHICH frame — this is one cheap seek to fetch it properly, the same single
// pass the old makeThumb did. The crop is the fallback for a source that moved,
// went busy or refuses to seek.
func writePoster(src, key string, idx int, sheet image.Image, m SpriteManifest) error {
	dst := filepath.Join(imgDir, key+".jpg")
	if err := os.MkdirAll(imgDir, 0755); err != nil {
		return err
	}

	if data, err := grabFrame(src, float64(idx*m.Interval)); err == nil {
		// Trust but verify: a seek can land past a cut on a different frame,
		// so re-run the classical judge and fall back to the crop if what came
		// back is one of the failures we set out to avoid.
		if img, derr := jpeg.Decode(bytes.NewReader(data)); derr == nil {
			if _, ok := scoreTile(img); ok {
				return atom.WriteFile(dst, data, 0644)
			}
		}
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, tileAt(sheet, m, idx), &jpeg.Options{Quality: 90}); err != nil {
		return err
	}

	return atom.WriteFile(dst, buf.Bytes(), 0644)
}

func grabFrame(src string, ts float64) ([]byte, error) {
	if _, err := os.Stat(src); err != nil {
		return nil, err
	}

	f, err := os.CreateTemp("", "poster*.jpg")
	if err != nil {
		return nil, err
	}
	tmp := f.Name()
	f.Close()
	defer os.Remove(tmp)

	run := library.FFRun{
		Args:    []string{"-y", "-ss", strconv.FormatFloat(ts, 'f', 2, 64), "-i", src, "-frames:v", "1", "-q:v", "2", tmp},
		Timeout: 60 * time.Second,
		Nice:    true,
	}
	if stderr, err := run.Run(); err != nil {
		return nil, library.FFErr(stderr, err)
	}

	return os.ReadFile(tmp)
}

// RepickPoster clears the mark and chooses again, for when a pick is bad.
// src is the absolute path to the video.
func RepickPoster(src string) error {
	key := library.Hash(filepath.Base(src))
	if err := library.KVDelete(posterMark(key)); err != nil {
		return err
	}

	return PickPoster(src, key)
}

// PosterRedo re-picks the poster for ?file=<library-relative path>. The vision
// stage is probed per sweep, so re-arm it: a single manual redo is exactly when
// the user is watching and a sleeping host is worth one wait.
func PosterRedo(c *gin.Context, lib *library.Library) {
	file := strings.TrimPrefix(c.Query("file"), "/")
	if file == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file param"})

		return
	}

	abs := lib.FindFile(file)
	if abs == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})

		return
	}

	visionReset()
	if err := RepickPoster(abs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})

		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "tile": library.KVGetValue(posterKey(abs))})
}

// posterPool is small on purpose: each file decodes a multi-megapixel JPEG and
// may run one ffmpeg grab, and this pass shares the machine with live HLS.
var posterPool = jobs.NewPool(3)

func PosterAll(lib *library.Library) {
	visionReset()

	// Walk to completion before picking anything: a vision call can run for
	// minutes, and acquiring the pool inside the walk would leave the phase
	// total creeping up for hours. Keyed by sprite key, so two copies of the
	// same basename — which share one poster — are picked once.
	todo := map[string]string{}

	for _, root := range lib.Roots {
		if _, err := os.Stat(root); err != nil {
			continue
		}

		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") {
				return nil
			}
			if strings.ToLower(filepath.Ext(d.Name())) != ".mp4" {
				return nil
			}
			if library.IsCorrupt(path) || library.IsBusy(path) {
				return nil
			}

			key := library.Hash(d.Name())
			if HasSprite(key) && !hasPoster(key) {
				todo[key] = path
			}

			return nil
		})
	}

	PhaseSet("posters", len(todo), 0)

	var wg sync.WaitGroup
	for key, path := range todo {
		wg.Add(1)

		posterPool.Go(func() {
			defer wg.Done()
			defer PhaseInc("posters", 0, 1)

			if library.IsBusy(path) {
				return
			}
			if err := PickPoster(path, key); err != nil {
				log.Printf("[poster] %s: %v", filepath.Base(path), err)
			}
		})
	}

	wg.Wait()
}

// ---- vision stage ----

const (
	visionModelDefault = "qwen3.8-flash-next:125b-a6b-nvfp4"

	visionProbe = 10 * time.Second // /api/tags — is the model even there
	visionLoad  = 6 * time.Minute  // the model is ~100GB; a cold load is not quick
	visionCall  = 3 * time.Minute
	visionAlive = "15m" // keep the weights resident between picks

	// The host is a Tailscale peer that sleeps. Three consecutive failures
	// means it is gone, not that we were unlucky: stop calling for the rest of
	// the run rather than paying the timeout once per file.
	visionMaxFails = 3
)

var (
	visionMu     sync.Mutex
	visionProbed bool
	visionOn     bool
	visionFails  atomic.Int32

	// ollama runs one request per model at a time, so firing the pool's
	// workers at it concurrently only queues them — and a queued request
	// still burns its own deadline, which trips the breaker on a host that is
	// merely busy. One vision call at a time; the classical work stays
	// parallel because it never touches this gate.
	visionGate = jobs.NewPool(1)
)

func visionHost() string {
	if h := os.Getenv("OLLAMA_HOST"); h != "" {
		return strings.TrimRight(h, "/")
	}

	return "http://100.117.92.56:11434"
}

func visionModel() string {
	if m := os.Getenv("POSTER_VISION_MODEL"); m != "" {
		return m
	}

	return visionModelDefault
}

// visionReset re-arms the stage for a new sweep: a host that was asleep during
// the last pass may be up now.
func visionReset() {
	visionMu.Lock()
	visionProbed, visionOn = false, false
	visionMu.Unlock()

	visionFails.Store(0)
}

// visionReady probes once per sweep, under the lock so the pool's workers make
// one probe between them. A missing model or an unreachable host disables the
// stage for the whole sweep — the classical winner is already shipping, so
// there is nothing to recover from.
func visionReady() bool {
	if visionFails.Load() >= visionMaxFails {
		return false
	}

	visionMu.Lock()
	defer visionMu.Unlock()

	if !visionProbed {
		visionProbed = true
		visionOn = visionProbeHost()
	}

	return visionOn
}

func visionProbeHost() bool {
	if os.Getenv("POSTER_VISION") == "0" {
		return false
	}

	host, model := visionHost(), visionModel()

	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := httpc.GetJSON(context.Background(), host+"/api/tags", &tags, visionProbe); err != nil {
		log.Printf("[poster] vision off: %v", err)

		return false
	}

	found := false
	for _, m := range tags.Models {
		if m.Name == model {
			found = true

			break
		}
	}
	if !found {
		log.Printf("[poster] vision off: model %q not on %s — set POSTER_VISION_MODEL", model, host)

		return false
	}

	// Load the weights here, once, under the probe lock. A ~100GB cold load
	// outlasts any sane per-pick budget, and paying it inside the first pick
	// makes a slow load indistinguishable from a dead host to the breaker.
	load := map[string]any{"model": model, "prompt": "", "keep_alive": visionAlive}
	if err := httpc.PostJSON(context.Background(), host+"/api/generate", load, nil, visionLoad); err != nil {
		log.Printf("[poster] vision off: %s did not load: %v", model, err)

		return false
	}

	log.Printf("[poster] vision on: %s", model)

	return true
}

// The classical stage cannot tell a title card from a scene: measured across
// the live library's sheets, dominance of the modal luma bin, colour coverage
// and edge-band skew all overlap completely between the two. So the prompt
// spends its words on exactly the judgement the model can make and the
// statistics cannot — branded title art is a fine poster, a cast list is not.
const visionPrompt = `You are choosing the poster frame for a video from %d candidate stills, numbered 1 to %d in the order given.
Pick the one that best represents the video and would most make someone want to watch it: a clear subject, readable composition, expressive lighting.
Title art naming the show is fine. A list of cast or crew names is not, nor is a blurry frame, an empty room, or a wide shot with no subject in it.
Answer with only JSON: {"pick": <number>}`

// visionPick asks the model to choose among the finalists and returns an index
// INTO finalists. Any failure at all returns false and the caller keeps the
// classical winner.
func visionPick(sheet image.Image, m SpriteManifest, finalists []int) (int, bool) {
	if !visionReady() {
		return 0, false
	}

	imgs := make([]string, 0, len(finalists))
	for _, i := range finalists {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, tileAt(sheet, m, i), &jpeg.Options{Quality: 92}); err != nil {
			return 0, false
		}
		imgs = append(imgs, base64.StdEncoding.EncodeToString(buf.Bytes()))
	}

	req := map[string]any{
		"model":      visionModel(),
		"prompt":     fmt.Sprintf(visionPrompt, len(finalists), len(finalists)),
		"images":     imgs,
		"stream":     false,
		"format":     "json",
		"think":      false,
		"keep_alive": visionAlive,
		"options":    map[string]any{"temperature": 0},
	}

	var resp struct {
		Response string `json:"response"`
		Error    string `json:"error"`
	}

	visionGate.Acquire()
	err := httpc.PostJSON(context.Background(), visionHost()+"/api/generate", req, &resp, visionCall)
	visionGate.Release()

	if err != nil {
		if visionFails.Add(1) >= visionMaxFails {
			log.Printf("[poster] vision off for this sweep after %d failures: %v", visionMaxFails, err)
		}

		return 0, false
	}

	if resp.Error != "" {
		visionFails.Add(1)

		return 0, false
	}

	i, ok := parsePick(resp.Response, len(finalists))
	if !ok {
		// A parse miss is the model being chatty, not the host being down, so
		// it does not count toward the breaker.
		return 0, false
	}
	visionFails.Store(0)

	return i, true
}

// parsePick pulls a 1-based choice out of whatever the model said and returns
// it 0-based. Tries the requested JSON object, then any integer in range that
// survives stripping a reasoning block — a thinking model narrates with numbers
// before it answers, so the tail is more trustworthy than the head.
func parsePick(s string, n int) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" || n < 1 {
		return 0, false
	}

	// The model was asked for {"pick": N}. If it answered in that shape the
	// answer is final: an out-of-range or non-numeric pick means "unusable",
	// not "go hunting through the reply for some other digit".
	var obj map[string]any
	if json.Unmarshal([]byte(s), &obj) == nil {
		v, has := obj["pick"]
		if !has {
			return 0, false
		}

		switch t := v.(type) {
		case float64:
			return oneBased(int(t), n)
		case string:
			if i, err := strconv.Atoi(strings.TrimSpace(t)); err == nil {
				return oneBased(i, n)
			}
		}

		return 0, false
	}

	if i := strings.LastIndex(s, "</think>"); i >= 0 {
		s = s[i+len("</think>"):]
	}

	last, found := 0, false
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			continue
		}

		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if v, err := strconv.Atoi(s[i:j]); err == nil && v >= 1 && v <= n {
			last, found = v-1, true
		}
		i = j
	}

	return last, found
}

func oneBased(v, n int) (int, bool) {
	if v < 1 || v > n {
		return 0, false
	}

	return v - 1, true
}

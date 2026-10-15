package media

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"log"
	"os"
	"path/filepath"
	"strings"

	"notflix/server/jobs"
	"notflix/server/library"
	"notflix/server/x/httpc"
)

// Which spatial upscaler suits a title: FSR 1.0 reads edges out of a photographic
// gradient, Anime4K pushes colour along the luma gradient to thin and darken
// drawn lines. Picking wrong is not catastrophic, just wasted — Anime4K on live
// action smears skin, FSR on cel art has almost nothing to lock onto.
//
// Decided ONCE, at pipeline time, off the sprite sheet that already exists (one
// frame per 10s). Nothing is computed during playback.
//
// Mark is `anim:<basename hash>` like codec:/skip:/poster:/blurb:, so it
// survives the moves CleanAll and Organize make. Presence means decided;
// "1" means animation.
func animKey(file string) string {
	return "anim:" + library.Hash(filepath.Base(file))
}

// IsAnimation reports the stored verdict. Absent means "not decided", which the
// player reads as live action — FSR is the safe default.
func IsAnimation(file string) bool {
	v, _ := library.KVGetValue(animKey(file)).(string)

	return v == "1"
}

func hasAnimMark(file string) bool {
	return library.KVGetValue(animKey(file)) != nil
}

const animPrompt = `These are %d frames sampled from one video.
Is this ANIMATION (hand-drawn, anime, cartoon, cel or CG animation) or LIVE ACTION (filmed with a camera)?
Judge the whole set, not one frame. Stylised colour grading or VFX in filmed footage is still live action.
Answer with only JSON: {"animation": true} or {"animation": false}`

// animFrames is enough to outvote one unrepresentative frame — a title card, a
// black transition, an animated logo in front of a live-action film — without
// paying for a long prompt.
const animFrames = 5

// DetectAnimation decides and stores the verdict for one title. Idempotent: a
// stored mark short-circuits, so a sweep costs nothing on the second pass.
func DetectAnimation(file, key string) error {
	if hasAnimMark(file) {
		return nil
	}
	if !visionReady() {
		return fmt.Errorf("vision model unavailable")
	}

	sheet, m, err := loadSheet(key)
	if err != nil {
		return err
	}
	if m.Count < 1 {
		return fmt.Errorf("sprite %s: no tiles", key)
	}

	// Evenly spaced across the runtime, trimming the first and last 10%: logos,
	// title cards and credits are exactly where a drawn frame misrepresents a
	// filmed title.
	lo := m.Count / 10
	hi := m.Count - m.Count/10
	if hi-lo < animFrames {
		lo, hi = 0, m.Count
	}

	imgs := make([]string, 0, animFrames)
	for i := range animFrames {
		idx := lo + (hi-lo)*i/animFrames
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, tileAt(sheet, m, idx), &jpeg.Options{Quality: 90}); err != nil {
			return err
		}
		imgs = append(imgs, base64.StdEncoding.EncodeToString(buf.Bytes()))
	}

	ok, err := askAnimation(imgs)
	if err != nil {
		return err
	}

	mark := "0"
	if ok {
		mark = "1"
	}

	return library.KVSetValue(animKey(file), mark)
}

func askAnimation(imgs []string) (bool, error) {
	req := map[string]any{
		"model":      visionModel(),
		"prompt":     fmt.Sprintf(animPrompt, len(imgs)),
		"images":     imgs,
		"stream":     false,
		"format":     "json",
		"think":      false,
		"keep_alive": visionAlive,
		"options":    jobs.OllamaOpts(map[string]any{"temperature": 0}),
	}

	var resp struct {
		Response string `json:"response"`
		Error    string `json:"error"`
	}

	// Same 1-wide gate as the poster pick: ollama serves one request per model,
	// so parallel calls only queue and then time out together.
	visionGate.Acquire()
	err := httpc.PostJSON(context.Background(), visionHost()+"/api/generate", req, &resp, visionCall)
	visionGate.Release()

	if err != nil {
		return false, err
	}
	if resp.Error != "" {
		return false, fmt.Errorf("vision: %s", resp.Error)
	}

	var out struct {
		Animation *bool `json:"animation"`
	}
	s := strings.TrimSpace(resp.Response)
	if i := strings.Index(s, "{"); i >= 0 {
		s = s[i:]
	}
	if j := strings.LastIndex(s, "}"); j >= 0 {
		s = s[:j+1]
	}
	if err := json.Unmarshal([]byte(s), &out); err != nil || out.Animation == nil {
		return false, fmt.Errorf("unparseable verdict: %.80s", resp.Response)
	}

	return *out.Animation, nil
}

// AnimationList serves every stored verdict, joined client-side against the key
// the video list already carries — same shape as /api/blurbs, so the list
// payload stays small.
func AnimationList() map[string]bool {
	out := map[string]bool{}
	for k, v := range library.KVPrefix("anim:") {
		s, _ := v.(string)
		out[strings.TrimPrefix(k, "anim:")] = s == "1"
	}

	return out
}

// AnimationAll decides every title that has a sprite sheet but no verdict yet.
// Walks to completion first for the same reason PosterAll does: a vision call
// can run for minutes, and acquiring the gate inside the walk would leave the
// phase total creeping up for hours.
func AnimationAll(lib *library.Library) {
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
			if HasSprite(key) && !hasAnimMark(path) {
				todo[key] = path
			}

			return nil
		})
	}
	if len(todo) == 0 {
		return
	}

	PhaseSet("anim", len(todo), 0)
	log.Printf("[anim] %d title(s) to classify", len(todo))

	done := 0
	for key, path := range todo {
		PhaseNote("anim", filepath.Base(path))
		if err := DetectAnimation(path, key); err != nil {
			log.Printf("[anim] %s: %v", filepath.Base(path), err)
			// The host being down fails every remaining title the same way;
			// stop rather than grind out one timeout per file.
			if strings.Contains(err.Error(), "unavailable") {
				break
			}
		} else {
			done++
		}
		PhaseInc("anim", 0, 1)
	}
	PhaseNote("anim", "")
	log.Printf("[anim] classified %d of %d", done, len(todo))
}

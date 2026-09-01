package media

import (
	"encoding/binary"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"notflix/server/jobs"
	"notflix/server/library"
)

// Episodes in one folder share opening/ending audio, so fingerprinting each
// episode's head and tail and cross-matching against a sibling gives the
// intro/outro bounds. Needs ≥2 episodes; results persist under skip:<hash>.

const (
	skipHeadSec = 480.0 // intro must live in the first 8 minutes
	skipTailSec = 360.0 // outro search window before the end
	skipMinSec  = 15.0
	skipMaxSec  = 300.0 // longer "shared audio" than this means duplicate files, not an intro
)

// skipKey uses the BASENAME hash: marks are stamped with the rel path at scan
// time and CleanAll/Organize move files later, orphaning a dir-qualified key.
func skipKey(rel string) string {
	return "skip:" + library.Hash(filepath.Base(rel))
}

// skipVal also reads the legacy dir-qualified key.
func skipVal(rel string) any {
	if v := library.KVGetValue("skip:" + library.Hash(rel)); v != nil {
		return v
	}
	return library.KVGetValue(skipKey(rel))
}

// extractPCM decodes a window of the first audio track to 5512 Hz mono s16le
// through a temp file, keeping it on the shared FFRun runner (shutdown-cancelled,
// timeouts, nice -n 19) instead of a bare exec that outlives Shutdown().
func extractPCM(path string, ss, dur float64) ([]int16, error) {
	tmp, err := os.CreateTemp("", "pcm*.raw")
	if err != nil {
		return nil, err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	var args []string
	if ss > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.2f", ss))
	}
	args = append(args, "-i", path,
		"-map", "0:a:0", "-ac", "1", "-ar", fmt.Sprint(fpRate),
		"-t", fmt.Sprintf("%.2f", dur),
		"-f", "s16le", "-y", tmp.Name())

	if stderr, err := (library.FFRun{Args: args, Timeout: 10 * time.Minute, Nice: true}).Run(); err != nil {
		return nil, fmt.Errorf("pcm extract: %w — %s", err, stderr)
	}

	raw, err := os.ReadFile(tmp.Name())
	if err != nil {
		return nil, err
	}

	pcm := make([]int16, len(raw)/2)
	for i := range pcm {
		pcm[i] = int16(binary.LittleEndian.Uint16(raw[2*i:]))
	}

	return pcm, nil
}

type episode struct {
	ref      library.VideoRef
	dur      float64
	head     []uint32
	tail     []uint32
	tailBase float64
	prepped  bool
}

func (e *episode) prep() {
	if e.prepped {
		return
	}
	e.prepped = true

	e.dur = duration(e.ref.Abs())
	if e.dur < 4*skipMinSec {
		return
	}

	if pcm, err := extractPCM(e.ref.Abs(), 0, min(skipHeadSec, e.dur/2)); err == nil {
		e.head = Fingerprint(pcm)
	} else {
		log.Printf("[skips] %s head: %v", e.ref.Rel, err)
	}

	e.tailBase = max(e.dur/2, e.dur-skipTailSec)
	if pcm, err := extractPCM(e.ref.Abs(), e.tailBase, e.dur-e.tailBase); err == nil {
		e.tail = Fingerprint(pcm)
	}
}

func matchWindow(a, b []uint32, base float64) []float64 {
	s, e, ok := matchFingerprints(a, b, skipMinSec)
	if !ok {
		return nil
	}
	start, end := base+fpTime(s), base+fpTime(e)
	if end-start > skipMaxSec {
		return nil
	}
	return []float64{start, end}
}

// Stores an explicit empty record too, so unscannable episodes aren't
// re-fingerprinted on every pass.
func detectEpisode(ep *episode, partners []*episode) {
	var intro, outro []float64
	for _, p := range partners {
		p.prep()
		if intro == nil {
			intro = matchWindow(ep.head, p.head, 0)
		}
		if outro == nil {
			outro = matchWindow(ep.tail, p.tail, ep.tailBase)
		}
		if intro != nil && outro != nil {
			break
		}
	}

	val := map[string]any{"v": 1}
	if intro != nil {
		val["intro"] = intro
	}
	if outro != nil {
		val["outro"] = outro
	}
	if err := library.KVSetValue(skipKey(ep.ref.Rel), val); err != nil {
		log.Printf("[skips] kv write %s: %v", ep.ref.Rel, err)
		return
	}
	if intro != nil || outro != nil {
		log.Printf("[skips] %s intro=%v outro=%v", ep.ref.Rel, intro, outro)
	}
}

func DetectSkips(lib *library.Library) {
	byDir := map[string][]*episode{}
	for _, v := range lib.AllVideos() {
		dir := filepath.Dir(v.Rel)
		if dir == "." || library.IsBusy(v.Abs()) {
			continue
		}
		byDir[dir] = append(byDir[dir], &episode{ref: v})
	}

	for dir, eps := range byDir {
		if len(eps) < 2 {
			continue
		}

		var pending []*episode
		for _, e := range eps {
			if skipVal(e.ref.Rel) == nil {
				pending = append(pending, e)
			}
		}
		if len(pending) == 0 {
			continue
		}
		PhaseInc("skips", len(pending), 0)
		log.Printf("[skips] %s: scanning %d/%d episodes", dir, len(pending), len(eps))

		// Partners are the next two siblings in AllVideos order, wrapping; they prep
		// lazily inside this single-threaded loop, so nothing races.
		idx := map[*episode]int{}
		for i, e := range eps {
			idx[e] = i
		}

		pool := jobs.NewPool(2)
		var wg sync.WaitGroup
		for _, e := range pending {
			wg.Add(1)
			pool.Go(func() {
				defer wg.Done()
				e.prep()
			})
		}
		wg.Wait()

		for _, e := range pending {
			i := idx[e]
			partners := []*episode{eps[(i+1)%len(eps)], eps[(i+2)%len(eps)]}
			if partners[0] == e {
				continue
			}
			if partners[1] == e {
				partners = partners[:1]
			}
			detectEpisode(e, partners)
			PhaseInc("skips", 0, 1)
		}
	}
	log.Println("[skips] done")
}

func SkipInfo(c *gin.Context, lib *library.Library) {
	file := strings.TrimPrefix(c.Query("file"), "/")
	if file == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file param"})
		return
	}

	if v, ok := skipVal(file).(map[string]any); ok {
		c.JSON(http.StatusOK, v)
		return
	}
	c.JSON(http.StatusOK, gin.H{})
}

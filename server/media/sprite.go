package media

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"notflix/server/jobs"
	"notflix/server/library"
)

// Trickplay sprite: one frame per `SpriteIntervalSec`, scaled to `SpriteTileW`
// (height auto-even), tiled `SpriteCols` wide. The player overlays these on
// scrub hover. Cheap to generate (one ffmpeg pass, sequential decode), cheap
// to serve (one JPG + a tiny JSON manifest).
//
// Sprite JPG → `./images/{key}.sprite.jpg` (served by the existing static
// `/images/:filename` route). Manifest → `./images/{key}.sprite.json`,
// returned by `GET /api/sprite/:key`.
//
// Key = library.Hash(basename) — same scheme as thumbnails. Idempotent: if
// the sprite already exists and the source isn't newer, skip.
const (
	SpriteIntervalSec = 10
	SpriteTileW       = 160
	SpriteCols        = 10
)

type SpriteManifest struct {
	Interval int `json:"interval"` // seconds per tile
	Cols     int `json:"cols"`
	Rows     int `json:"rows"`
	Count    int `json:"count"`
	TileW    int `json:"tileW"`
	TileH    int `json:"tileH"`
}

func spritePaths(key string) (jpg, manifest string) {
	return filepath.Join(imgDir, key+".sprite.jpg"),
		filepath.Join(imgDir, key+".sprite.json")
}

// spritePool throttles concurrent sprite generation. ffmpeg's fps=1/N filter
// has to seek-decode every Nth frame, which can be noisy on slow disks —
// 2-wide is plenty alongside conversion and HLS work.
var spritePool = jobs.NewPool(2)

// generateInflight prevents double-launching the same key (e.g. when both
// the conversion hook and a lazy backfill fire for the same file).
var generateInflight sync.Map

func GenerateSprite(srcMP4, key string) error {
	jpg, mfst := spritePaths(key)

	if _, err := os.Stat(jpg); err == nil {
		if _, err := os.Stat(mfst); err == nil {
			return nil // already done
		}
	}

	if _, loaded := generateInflight.LoadOrStore(key, true); loaded {
		return nil
	}
	defer generateInflight.Delete(key)

	// The source may have been moved/renamed/deleted between the walk that
	// queued us and now (CleanAll flattens, convert renames). Silently skip
	// so we don't log a scary "duration unknown" and pollute the ledger with
	// StepFailed for a file that's actually fine at a new path.
	if _, err := os.Stat(srcMP4); err != nil {
		return nil
	}

	// duration<=0 means the source isn't currently probeable — usually because
	// convert is mid-remux, ScanCorrupt is about to flag it, or the file was
	// renamed after our stat. Any of those either resolves itself (next sweep
	// retries) or gets caught by the corrupt path elsewhere. Skip silently so
	// we don't spam the log or mark StepFailed for a transient state.
	dur := duration(srcMP4)
	if dur <= 0 {
		return nil
	}

	count := max(int(dur)/SpriteIntervalSec+1, 1)
	rows := (count + SpriteCols - 1) / SpriteCols

	vf := fmt.Sprintf("fps=1/%d,scale=%d:-2,tile=%dx%d",
		SpriteIntervalSec, SpriteTileW, SpriteCols, rows)

	// tmp keeps the `.jpg` tail so ffmpeg's format auto-detect works; passing
	// `-f image2` is belt-and-braces against shell quirks.
	tmp := strings.TrimSuffix(jpg, ".jpg") + ".tmp.jpg"
	if stderr, err := library.FF(
		"-y", "-i", srcMP4,
		"-vf", vf,
		"-frames:v", "1",
		"-q:v", "4",
		"-f", "image2",
		tmp,
	); err != nil {
		os.Remove(tmp)
		// Truncate stderr — the tail-writer keeps it bounded, but per-file
		// failures don't need the full dump.
		head := stderr
		if len(head) > 240 {
			head = head[:240] + "…"
		}
		return fmt.Errorf("sprite ffmpeg: %v %s", err, head)
	}

	// Read back the actual tile dimensions — ffmpeg may round height to keep
	// even, so we trust the result, not our pre-math.
	tileH := 0
	if streams, perr := library.Prober.Streams(context.Background(), tmp); perr == nil {
		for _, s := range streams {
			if s.CodecType == "video" && s.Width > 0 && s.Height > 0 {
				// The whole sprite is one image; tileH = height/rows.
				tileH = s.Height / rows
				break
			}
		}
	}

	manifest := SpriteManifest{
		Interval: SpriteIntervalSec,
		Cols:     SpriteCols,
		Rows:     rows,
		Count:    count,
		TileW:    SpriteTileW,
		TileH:    tileH,
	}
	mjson, _ := json.Marshal(manifest)
	if err := os.WriteFile(mfst+".tmp", mjson, 0644); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("sprite manifest write: %w", err)
	}

	if err := os.Rename(tmp, jpg); err != nil {
		os.Remove(mfst + ".tmp")
		return fmt.Errorf("sprite jpg rename: %w", err)
	}
	if err := os.Rename(mfst+".tmp", mfst); err != nil {
		os.Remove(jpg)
		return fmt.Errorf("sprite manifest rename: %w", err)
	}

	log.Printf("[sprite] %s: %d tiles @ %dx%d (%d×%d)",
		filepath.Base(srcMP4), count, SpriteTileW, tileH, SpriteCols, rows)
	return nil
}

// GenerateSpriteAsync queues sprite generation on the shared pool. Safe to
// fire-and-forget from conversion completion or thumbnail walks. Worker-time
// IsBusy check defers files that convert grabbed after we queued — silent,
// not recorded as failed, retries on the next sweep.
func GenerateSpriteAsync(srcMP4, key string) {
	spritePool.Go(func() {
		if library.IsBusy(srcMP4) {
			return
		}
		library.PipelineMark(srcMP4, "sprite", library.StepRunning)
		if err := GenerateSprite(srcMP4, key); err != nil {
			log.Printf("[sprite] failed %s: %v", filepath.Base(srcMP4), err)
			library.PipelineMark(srcMP4, "sprite", library.StepFailed)
			return
		}
		library.PipelineMark(srcMP4, "sprite", library.StepDone)
	})
}

// HasSprite is the cheap "is it ready?" probe.
func HasSprite(key string) bool {
	jpg, mfst := spritePaths(key)
	if _, err := os.Stat(jpg); err != nil {
		return false
	}
	_, err := os.Stat(mfst)
	return err == nil
}

// BackfillSprites walks each root once and queues any MP4 that's missing a
// sprite. Called from the same goroutine that regenerates thumbnails.
func BackfillSprites(lib *library.Library) {
	queued := 0
	for _, root := range lib.Roots {
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
			if HasSprite(key) {
				return nil
			}
			GenerateSpriteAsync(path, key)
			queued++
			return nil
		})
	}
	if queued > 0 {
		log.Printf("[sprite] backfill queued %d", queued)
	}
}

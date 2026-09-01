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

// Trickplay sprite: one frame per SpriteIntervalSec tiled SpriteCols wide, at
// ./images/{key}.sprite.{jpg,json} with key = library.Hash(basename), the same
// scheme as thumbnails.
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

// 2-wide: the fps=1/N filter seek-decodes and is noisy on slow disks.
var spritePool = jobs.NewPool(2)

// generateInflight stops the convert hook and a backfill double-launching a key.
var generateInflight sync.Map

func GenerateSprite(srcMP4, key string) error {
	jpg, mfst := spritePaths(key)

	if _, err := os.Stat(jpg); err == nil {
		if _, err := os.Stat(mfst); err == nil {
			return nil
		}
	}

	if _, loaded := generateInflight.LoadOrStore(key, true); loaded {
		return nil
	}
	defer generateInflight.Delete(key)

	// The source may have been moved or renamed since the walk queued us (CleanAll
	// flattens, convert renames) — skip without marking StepFailed.
	if _, err := os.Stat(srcMP4); err != nil {
		return nil
	}

	// duration<=0 means transiently unprobeable (mid-remux, renamed, about to be
	// flagged corrupt) — skip silently; the next sweep retries.
	dur := duration(srcMP4)
	if dur <= 0 {
		return nil
	}

	count := max(int(dur)/SpriteIntervalSec+1, 1)
	rows := (count + SpriteCols - 1) / SpriteCols

	vf := fmt.Sprintf("fps=1/%d,scale=%d:-2,tile=%dx%d",
		SpriteIntervalSec, SpriteTileW, SpriteCols, rows)

	// tmp keeps the `.jpg` tail so ffmpeg's format auto-detect works.
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
		head := stderr
		if len(head) > 240 {
			head = head[:240] + "…"
		}
		return fmt.Errorf("sprite ffmpeg: %v %s", err, head)
	}

	// Trust ffmpeg's actual output height — it rounds to keep the tile even.
	tileH := 0
	if streams, perr := library.Prober.Streams(context.Background(), tmp); perr == nil {
		for _, s := range streams {
			if s.CodecType == "video" && s.Width > 0 && s.Height > 0 {
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

// Worker-time IsBusy re-check defers files convert grabbed after queueing;
// silent, retried next sweep.
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

func HasSprite(key string) bool {
	jpg, mfst := spritePaths(key)
	if _, err := os.Stat(jpg); err != nil {
		return false
	}
	_, err := os.Stat(mfst)
	return err == nil
}

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

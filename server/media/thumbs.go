package media

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"notflix/server/jobs"
	"notflix/server/library"
)

// Small pool so per-file thumb calls from toMP4 don't blow up ffmpeg
// concurrency when a batch of conversions all land at once. The bulk
// RegenerateThumbnails path is separate and single-threaded.
var thumbPool = jobs.NewPool(4)

// GenerateThumbAsync queues thumbnail creation for src on the shared pool.
// Skips silently if the destination already exists. Mirrors
// GenerateSpriteAsync / jobs.AutoWhisper so toMP4 can fan out downstream
// work per-file without blocking convert throughput.
func GenerateThumbAsync(src string) {
	go func() {
		thumbPool.Acquire()
		defer thumbPool.Release()

		if library.IsBusy(src) {
			return
		}
		tdir := "images"
		os.MkdirAll(tdir, 0755)
		dst := filepath.Join(tdir, library.Hash(filepath.Base(src))+".jpg")
		if _, err := os.Stat(dst); err == nil {
			return
		}
		library.PipelineMark(src, "thumb", library.StepRunning)
		if err := makeThumb(src, dst); err != nil {
			log.Printf("[thumbs] async failed %s: %v", filepath.Base(src), err)
			library.PipelineMark(src, "thumb", library.StepFailed)
			return
		}
		library.PipelineMark(src, "thumb", library.StepDone)
	}()
}

func makeThumb(src, dst string) error {
	f, err := library.Prober.Format(context.Background(), src)
	if err != nil {
		return fmt.Errorf("ffprobe: %w", err)
	}

	dur := f.Duration.Duration.Seconds()
	if dur == 0 {
		return fmt.Errorf("invalid duration (file may still be downloading)")
	}

	ts := fmt.Sprintf("%.2f", dur/2+rand.Float64()*10-5)
	if stderr, err := library.FF("-y", "-ss", ts, "-i", src, "-vframes", "1", dst); err != nil {
		return library.FFErr(stderr, err)
	}
	return nil
}

func GenerateThumbnails(dir string) {
	tdir := "images"
	os.MkdirAll(tdir, 0755)
	var skipped int

	walk := func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext == ".mp4" {
			if library.IsCorrupt(path) {
				skipped++
				return nil
			}
			name := library.Hash(d.Name()) + ".jpg"
			dst := filepath.Join(tdir, name)
			if _, err := os.Stat(dst); err != nil {
				if err := makeThumb(path, dst); err != nil {
					log.Printf("[thumbs] failed %s: %v", d.Name(), err)
				}
			}
		}

		return nil
	}

	filepath.WalkDir(dir, walk)
	if skipped > 0 {
		log.Printf("[thumbs] skipped %d corrupt file(s) in %s", skipped, dir)
	}
}

var (
	thumbRegen   atomic.Bool
	thumbLastRun atomic.Int64
)

func RegenerateThumbnails(lib *library.Library, minInterval time.Duration) {
	if minInterval > 0 {
		last := time.Unix(thumbLastRun.Load(), 0)
		if time.Since(last) < minInterval {
			return
		}
	}

	if !thumbRegen.CompareAndSwap(false, true) {
		return
	}

	defer func() {
		thumbLastRun.Store(time.Now().Unix())
		thumbRegen.Store(false)
	}()

	count, skipped := 0, 0
	tdir := "images"
	os.MkdirAll(tdir, 0755)

	for _, root := range lib.Roots {
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") {
				return nil
			}

			if strings.ToLower(filepath.Ext(d.Name())) != ".mp4" {
				return nil
			}

			if library.IsBusy(path) {
				return nil
			}

			// ScanCorrupt runs ahead of RegenerateThumbnails in ProcessAll
			// (convert.go) — skipping here silences the per-file `moov atom
			// not found` log dumps that otherwise repeat on every pass.
			if library.IsCorrupt(path) {
				skipped++
				return nil
			}

			name := library.Hash(d.Name()) + ".jpg"
			dst := filepath.Join(tdir, name)
			if _, err := os.Stat(dst); err != nil {
				if err := makeThumb(path, dst); err != nil {
					log.Printf("[thumbs] failed %s: %v", d.Name(), err)
				} else {
					count++
				}
			}

			return nil
		})
	}

	if count > 0 {
		log.Printf("[thumbs] regenerated %d missing thumbnails", count)
	}
	if skipped > 0 {
		log.Printf("[thumbs] skipped %d known-corrupt file(s)", skipped)
	}
}

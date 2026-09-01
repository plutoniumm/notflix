package media

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"notflix/server/jobs"
	"notflix/server/library"
)

var repairPool = jobs.NewPool(2)

// RepairAll tries progressively harder strategies on the corrupt set; recovered
// files replace the original, whose bytes stay as <file>.corrupt.bak.
func RepairAll(lib *library.Library) {
	paths := library.CorruptPaths()
	if len(paths) == 0 {
		return
	}
	log.Printf("[repair] attempting repair on %d file(s)", len(paths))
	PhaseSet("repair", len(paths), 0)

	var wg sync.WaitGroup
	var ok, fail atomic.Int32
	for _, p := range paths {
		wg.Add(1)
		repairPool.Acquire()
		go func(path string) {
			defer wg.Done()
			defer repairPool.Release()
			defer PhaseInc("repair", 0, 1)
			PhaseNote("repair", filepath.Base(path))
			if repairOne(path) {
				ok.Add(1)
			} else {
				fail.Add(1)
			}
		}(p)
	}
	wg.Wait()
	PhaseNote("repair", "")
	log.Printf("[repair] done: recovered=%d unrecoverable=%d", ok.Load(), fail.Load())
	if ok.Load() > 0 {
		library.ScanCorrupt(lib)
	}
}

func repairOne(path string) bool {
	name := filepath.Base(path)
	strategies := []struct {
		label string
		fn    func(string) error
	}{
		{"copy-remux", repairCopyRemux},
		{"reencode", repairReencode},
	}
	for _, s := range strategies {
		if err := s.fn(path); err == nil {
			log.Printf("[repair] %s: recovered via %s", name, s.label)
			return true
		} else {
			log.Printf("[repair] %s: %s failed: %v", name, s.label, err)
		}
	}
	return false
}

// repairCopyRemux rebuilds the container with regenerated PTS/DTS — fixes
// moov/index/timestamp damage without re-encoding. Extension preserved.
func repairCopyRemux(path string) error {
	ext := filepath.Ext(path)
	tmp := path + ".repair.tmp" + ext
	args := []string{
		"-y",
		"-err_detect", "ignore_err",
		"-fflags", "+genpts+igndts",
		"-i", path,
		"-map", "0", "-c", "copy",
	}
	if isMP4Container(ext) {
		args = append(args, "-movflags", "+faststart")
	}
	args = append(args, tmp)
	stderr, err := library.FFRun{Args: args, Timeout: 5 * time.Minute}.Run()
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("%w: %s", err, tail(stderr))
	}
	if !verifyOutput(tmp) {
		os.Remove(tmp)
		return fmt.Errorf("output failed verify")
	}
	return commitRepair(path, tmp, path)
}

// Lossy fallback. Always outputs .mp4 so the result slots into the AV1 HLS
// pipeline; the original extension is retired (source kept as .corrupt.bak).
func repairReencode(path string) error {
	base := strings.TrimSuffix(path, filepath.Ext(path))
	tmp := base + ".repair.tmp.mp4"
	dst := base + ".mp4"
	args := []string{
		"-y",
		"-err_detect", "ignore_err",
		"-fflags", "+genpts+igndts",
		"-i", path,
		"-map", "0:v:0?", "-map", "0:a?", "-map_chapters", "-1",
	}
	args = append(args, av1File(32, false)...)
	args = append(args, "-c:a", pickAAC(), "-b:a", "192k")
	if anyUnnamedMultichannel(getSrcInfo(path)) {
		args = append(args, "-ac", "2")
	}
	args = append(args, "-movflags", "+faststart", "-f", "mp4", tmp)
	stderr, err := library.FFRun{Args: args, Timeout: 60 * time.Minute}.Run()
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("%w: %s", err, tail(stderr))
	}
	if !verifyOutput(tmp) {
		os.Remove(tmp)
		return fmt.Errorf("output failed verify")
	}
	return commitRepair(path, tmp, dst)
}

func verifyOutput(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	streams, err := library.Prober.Streams(ctx, path)
	if err != nil {
		return false
	}
	for _, s := range streams {
		if s.CodecType == "video" {
			return true
		}
	}
	return false
}

// commitRepair backs the original up before swapping, bails rather than clobber
// an existing dst, and rolls the backup back on failure.
func commitRepair(origPath, tmp, dst string) error {
	if dst != origPath {
		if _, err := os.Stat(dst); err == nil {
			os.Remove(tmp)
			return fmt.Errorf("dst %s already exists", dst)
		}
	}
	bak := origPath + ".corrupt.bak"
	if err := os.Rename(origPath, bak); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("backup: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Rename(bak, origPath)
		return fmt.Errorf("swap-in: %w", err)
	}
	return nil
}

func isMP4Container(ext string) bool {
	switch strings.ToLower(ext) {
	case ".mp4", ".m4v", ".mov":
		return true
	}
	return false
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 240 {
		return "…" + s[len(s)-240:]
	}
	return s
}

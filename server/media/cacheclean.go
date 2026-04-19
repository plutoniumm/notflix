package media

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"notflix/server/library"
)

const hlsCacheMaxAge = 7 * 24 * time.Hour

var hashDirRe = regexp.MustCompile(`^\d{7}$`)

func liveHashes(roots []string) map[string]bool {
	live := make(map[string]bool)
	add := func(rel string) {
		live[library.Hash(rel)] = true
	}

	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if !e.IsDir() {
				if isVideoFile(e.Name()) {
					add(e.Name())
				}
				continue
			}
			sub := filepath.Join(root, e.Name())
			subEntries, err := os.ReadDir(sub)
			if err != nil {
				continue
			}
			for _, se := range subEntries {
				if se.IsDir() || strings.HasPrefix(se.Name(), ".") {
					continue
				}
				if isVideoFile(se.Name()) {
					add(e.Name() + "/" + se.Name())
				}
			}
		}
	}
	return live
}

func isVideoFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".mp4" || ext == ".mkv" || ext == ".mov"
}

// dirLatestMTime returns the newest mtime of any file within dir. Falls back to
// the dir's own mtime if the walk fails or yields nothing.
func dirLatestMTime(dir string) time.Time {
	info, err := os.Stat(dir)
	if err != nil {
		return time.Time{}
	}
	latest := info.ModTime()
	filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil && fi.ModTime().After(latest) {
			latest = fi.ModTime()
		}
		return nil
	})
	return latest
}

func CleanHLSCache(roots []string) {
	entries, err := os.ReadDir(hlsCacheDir)
	if err != nil {
		return
	}

	live := liveHashes(roots)
	now := time.Now()
	orphans, aged := 0, 0

	for _, ce := range entries {
		if !ce.IsDir() || !hashDirRe.MatchString(ce.Name()) {
			continue
		}
		path := filepath.Join(hlsCacheDir, ce.Name())

		reason := ""
		if !live[ce.Name()] {
			reason = "orphan"
			orphans++
		} else if now.Sub(dirLatestMTime(path)) > hlsCacheMaxAge {
			reason = "aged"
			aged++
		}

		if reason == "" {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			log.Printf("[hls-clean] failed to remove %s: %v", path, err)
			continue
		}
		log.Printf("[hls-clean] removed %s cache: %s", reason, ce.Name())
	}

	if orphans+aged > 0 {
		log.Printf("[hls-clean] pass done: %d orphan, %d aged", orphans, aged)
	}
}

func StartCacheCleanLoop(roots []string, interval time.Duration) {
	go func() {
		for {
			CleanHLSCache(roots)
			time.Sleep(interval)
		}
	}()
}

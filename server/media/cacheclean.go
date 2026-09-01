package media

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"notflix/server/library"
)

const hlsCacheMaxAge = 7 * 24 * time.Hour

var hashDirRe = regexp.MustCompile(`^\d{7}$`)

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

func CleanHLSCache(lib *library.Library) {
	entries, err := os.ReadDir(hlsCacheDir)
	if err != nil {
		return
	}

	// A root that can't be read is an unknown, not an absence: AllVideos would
	// report its titles as gone and this pass would delete live cache dirs.
	for _, root := range lib.Roots {
		if _, err := os.ReadDir(root); err != nil {
			log.Printf("[hls-clean] root %s unreadable (%v) — skipping pass", root, err)
			return
		}
	}

	// An empty census is a broken census, not an empty library.
	live := lib.LiveHashes()
	if len(live) == 0 {
		return
	}

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

func StartCacheCleanLoop(lib *library.Library, interval time.Duration) {
	go func() {
		for {
			CleanHLSCache(lib)
			time.Sleep(interval)
		}
	}()
}

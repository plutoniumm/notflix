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

	// AllVideos skips a root it can't read, so an unmounted drive reads as
	// "those titles no longer exist" and this pass would delete every cache
	// dir belonging to it — including whatever is streaming right now, which
	// then 404s mid-playback. A root that isn't there is an unknown, not an
	// absence: sit the pass out entirely.
	for _, root := range lib.Roots {
		if _, err := os.ReadDir(root); err != nil {
			log.Printf("[hls-clean] root %s unreadable (%v) — skipping pass", root, err)
			return
		}
	}

	// A census that comes back completely empty is a broken census, not an
	// empty library: acting on it deletes every cache dir there is.
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

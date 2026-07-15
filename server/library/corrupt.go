package library

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var corruptMu sync.RWMutex
var corruptSet = map[string]bool{}

// IsTorrentBusy, when set, reports whether a path is currently claimed by
// an aria2 torrent (active/waiting/stopped-not-yet-purged). Wired from
// main.go against the maintained jobs torrent-paths cache. Nil in tests
// and before jobs.Aria2Init has run — treated as "no torrent claim".
var IsTorrentBusy func(path string) bool

func IsCorrupt(absPath string) bool {
	corruptMu.RLock()
	defer corruptMu.RUnlock()
	return corruptSet[absPath]
}

func CorruptPaths() []string {
	corruptMu.RLock()
	defer corruptMu.RUnlock()
	paths := make([]string, 0, len(corruptSet))
	for p := range corruptSet {
		paths = append(paths, p)
	}
	return paths
}

// IsBusy reports whether a path should be exempt from library-side work
// (convert, thumbs, sprite, corrupt-scan) right now. Reasons:
//   - aria2 currently owns the file (via injected IsTorrentBusy callback)
//   - a control-file sidecar is present (.aria2 download, .tmp/.audio.tmp
//     conversion) — the file is mid-write
//   - the file was modified in the last 30 seconds — probably still being
//     written by aria2 or ffmpeg before its rename
//
// This is the single "hands off, come back later" gate for background work.
// If you're wondering "should I skip this file?", ask IsBusy.
func IsBusy(path string) bool {
	if IsTorrentBusy != nil && IsTorrentBusy(path) {
		return true
	}
	for _, suf := range []string{".aria2", ".tmp", ".audio.tmp"} {
		if _, err := os.Stat(path + suf); err == nil {
			return true
		}
	}
	if fi, err := os.Stat(path); err == nil && time.Since(fi.ModTime()) < 30*time.Second {
		return true
	}
	return false
}

// ScanCorrupt probes every video file in lib.Roots; any path that ffprobe
// can't open (truncated download, missing moov atom, etc.) is added to the
// in-memory set. Replaces the previous set atomically when done.
func ScanCorrupt(lib *Library) {
	next := map[string]bool{}
	var nextMu sync.Mutex

	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup

	for _, root := range lib.Roots {
		if _, err := os.Stat(root); err != nil {
			continue
		}
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if ext != ".mp4" && ext != ".mkv" && ext != ".mov" {
				return nil
			}
			// Skip files that are still downloading or mid-conversion — they
			// fail to probe but aren't corrupt; they re-scan clean once done.
			if IsBusy(path) {
				return nil
			}

			wg.Add(1)
			sem <- struct{}{}
			go func(p string) {
				defer wg.Done()
				defer func() { <-sem }()

				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if _, err := Prober.Format(ctx, p); err != nil {
					nextMu.Lock()
					next[p] = true
					nextMu.Unlock()
				}
			}(path)
			return nil
		})
	}

	wg.Wait()

	corruptMu.Lock()
	corruptSet = next
	corruptMu.Unlock()

	log.Printf("[corrupt] scan complete: %d corrupt files", len(next))
}

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

// IsTorrentBusy reports whether aria2 currently claims a path. Wired from
// main.go; nil in tests and before Aria2Init — treated as "no claim".
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

// IsBusy is the single "hands off, come back later" gate for background work:
// aria2 owns it, a .aria2/.tmp sidecar exists, or it was written in the last 30s.
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

// ScanCorrupt flags every video ffprobe can't open (truncated download,
// missing moov atom) and replaces the previous set atomically.
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
			// Mid-write files fail to probe but aren't corrupt; they re-scan clean.
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

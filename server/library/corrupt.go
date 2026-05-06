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

func IsCorrupt(absPath string) bool {
	corruptMu.RLock()
	defer corruptMu.RUnlock()
	return corruptSet[absPath]
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

package library

import (
	"fmt"
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

// freshWindow is how long after its last write a file is still assumed to be
// mid-download. It is a trade: too long and a finished download sits untouched
// through every sweep that happens to land inside it — a Bleach episode landed
// at 20:56:44 and was skipped by the sweeps at 20:56:49 and 20:57:13, including
// a manual Process Library press, which looks exactly like the feature being
// broken. Too short and the pipeline grabs a file still being written.
//
// 10s is enough: the real guards are the .aria2/.tmp sidecars and aria2
// ownership, both of which are exact. This only catches a writer that leaves no
// trace, and no such writer pauses for ten seconds mid-file.
const freshWindow = 10 * time.Second

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
	if fi, err := os.Stat(path); err == nil && time.Since(fi.ModTime()) < freshWindow {
		return true
	}
	return false
}

// deadKey marks bytes that every repair strategy has already failed on. Keyed
// on the basename hash like skip:/codec:/poster:, and stamped with size+mtime
// so a replaced or re-downloaded file re-qualifies on its own.
//
// Without this, 366 permanently-dead files were re-probed by ScanCorrupt (8
// wide) and re-attempted by RepairAll (732 ffmpeg spawns, one of them a full
// AV1 re-encode with a 60-minute timeout) on every sweep and every restart —
// about six minutes of churn per boot, on files that are physically megabytes
// of zeros, competing with live playback for the same USB drive.
func deadKey(path string) string {
	return "dead:" + Hash(filepath.Base(path))
}

func deadStamp(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}

	return fmt.Sprintf("%d:%d", fi.Size(), fi.ModTime().UnixNano())
}

// IsDead reports whether this exact file has already failed every repair
// strategy. A stamp mismatch means different bytes, so it gets a fresh chance.
func IsDead(path string) bool {
	stamp := deadStamp(path)
	if stamp == "" {
		return false
	}
	v, _ := KVGetValue(deadKey(path)).(string)

	return v != "" && v == stamp
}

func MarkDead(path string) {
	if stamp := deadStamp(path); stamp != "" {
		KVSetValue(deadKey(path), stamp)
	}
}

// ClearDead is called when a repair finally succeeds.
func ClearDead(path string) {
	KVDelete(deadKey(path))
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
			// Already proven unopenable on these exact bytes. It still belongs
			// in the corrupt set — dropping it would let the convert and thumbs
			// phases treat it as healthy — but it does not need another probe.
			if IsDead(path) {
				nextMu.Lock()
				next[path] = true
				nextMu.Unlock()

				return nil
			}

			wg.Add(1)
			sem <- struct{}{}
			go func(p string) {
				defer wg.Done()
				defer func() { <-sem }()

				// Timeout lives in UFO now, which bounds every probe rather
				// than only this one.
				if _, err := Probes.Format(p); err != nil {
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

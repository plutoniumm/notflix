package library

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var videoExts = map[string]bool{
	".mp4": true, ".mkv": true, ".mov": true, ".webm": true,
	".avi": true, ".flv": true, ".wmv": true, ".m4v": true,
	".mpg": true, ".mpeg": true, ".ts": true, ".3gp": true,
	".m2ts": true, ".mts": true, ".vob": true, ".divx": true,
	".ogv": true, ".rmvb": true, ".m2v": true,
}

var subExts = map[string]bool{
	".vtt": true, ".srt": true, ".ass": true, ".ssa": true,
}

// junkExts is a DENYLIST and must stay one: sweeping on an allowlist miss
// destroyed containers the list hadn't heard of (a .m2ts remux) an hour in.
var junkExts = map[string]bool{
	".part": true, ".!qb": true, ".crdownload": true, ".download": true,
	".aria2__temp": true, ".tmp": true,
	".exe": true, ".msi": true, ".bat": true, ".cmd": true, ".scr": true,
	".lnk": true, ".url": true, ".website": true,
	".txt": true, ".nfo": true, ".sfv": true, ".md5": true,
	".htm": true, ".html": true,
}

// keepSuffixes are compound suffixes filepath.Ext can't match — recovery
// files like .corrupt.bak must survive the junk sweep.
var keepSuffixes = []string{".corrupt.bak"}

func CleanAll(roots []string) {
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			continue
		}

		cleanRoot(root)
	}

	log.Println("[CleanAll] finished")
}

func cleanRoot(root string) {
	active := activeDirs(root)

	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}

		if active[filepath.Dir(path)] {
			return nil
		}

		lower := strings.ToLower(d.Name())
		for _, sfx := range keepSuffixes {
			if strings.HasSuffix(lower, sfx) {
				return nil
			}
		}

		if !junkExts[filepath.Ext(lower)] {
			return nil
		}

		// A recently-written file is someone's live output (an in-flight
		// ffmpeg .tmp): deleting it unlinks the path under the writer.
		if fi, err := d.Info(); err == nil && time.Since(fi.ModTime()) < time.Hour {
			return nil
		}

		log.Printf("[CleanAll] junk: %s", path)
		os.Remove(path)

		return nil
	})

	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}

	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}

		dir := filepath.Join(root, e.Name())
		if dirActive(dir, active) {
			log.Printf("[CleanAll] aria2 active, skipping: %s", e.Name())
			continue
		}

		processSubdir(root, dir)
	}
}

// activeDirs marks every dir holding an IsBusy file; those are exempt from
// clean/flatten.
func activeDirs(root string) map[string]bool {
	dirs := map[string]bool{}
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if IsBusy(path) {
			dirs[filepath.Dir(path)] = true
		}
		return nil
	})
	return dirs
}

func dirActive(dir string, active map[string]bool) bool {
	prefix := dir + string(filepath.Separator)
	for d := range active {
		if d == dir || strings.HasPrefix(d, prefix) {
			return true
		}
	}

	return false
}

func collect(dir string) (videos, subs []string) {
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(d.Name()))

		if videoExts[ext] {
			videos = append(videos, path)
		} else if subExts[ext] {
			subs = append(subs, path)
		}

		return nil
	})

	return
}

func processSubdir(root, dir string) {
	videos, subs := collect(dir)

	if len(videos) == 0 {
		pruneEmpty(dir)

		return
	}

	if len(videos) == 1 {
		flatVol(root, dir, videos[0], subs)
		return
	}

	flatDir(dir, videos, subs)
}

func flatVol(root, dir, videoPath string, subPaths []string) {
	videoName := filepath.Base(videoPath)
	videoBase := strings.TrimSuffix(videoName, filepath.Ext(videoName))
	dst := filepath.Join(root, videoName)

	if _, err := os.Stat(dst); err == nil {
		log.Printf("[CleanAll] flatten conflict, skipping: %s", videoName)
		return
	}
	if err := os.Rename(videoPath, dst); err != nil {
		log.Printf("[CleanAll] move video failed: %v", err)
		return
	}
	log.Printf("[CleanAll] → root: %s", videoName)

	for _, sp := range subPaths {
		dstSub := filepath.Join(root, rebaseSub(filepath.Base(sp), videoBase))
		if _, err := os.Stat(dstSub); err != nil {
			os.Rename(sp, dstSub)
		}
	}

	pruneEmpty(dir)
}

func flatDir(dir string, videoPaths, subPaths []string) {
	for _, vp := range videoPaths {
		if filepath.Dir(vp) == dir {
			continue
		}

		dst := safeMoveDst(dir, filepath.Base(vp))
		if err := os.Rename(vp, dst); err != nil {
			log.Printf("[CleanAll] move failed, keeping %s: %v", vp, err)

			continue
		}

		log.Printf("[CleanAll] → %s: %s", filepath.Base(dir), filepath.Base(dst))
	}

	for _, sp := range subPaths {
		if filepath.Dir(sp) == dir {
			continue
		}

		dst := filepath.Join(dir, filepath.Base(sp))
		if _, err := os.Stat(dst); err != nil {
			os.Rename(sp, dst)
		}
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			pruneEmpty(filepath.Join(dir, e.Name()))
		}
	}
}

// pruneEmpty replaces a RemoveAll: "no video we recognise" is not "nothing
// of value", and a failed move leaves the file right where it sits.
func pruneEmpty(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.IsDir() {
			pruneEmpty(filepath.Join(dir, e.Name()))
		}
	}

	rest, err := os.ReadDir(dir)
	if err != nil || len(rest) > 0 {
		return
	}

	if err := os.Remove(dir); err == nil {
		log.Printf("[CleanAll] removed empty dir: %s", dir)
	}
}

func rebaseSub(subName, videoBase string) string {
	ext := filepath.Ext(subName)
	base := strings.TrimSuffix(subName, ext)

	if strings.Contains(base, ".") {
		return videoBase + filepath.Ext(base) + ext
	}

	return videoBase + ext
}

func safeMoveDst(dir, name string) string {
	dst := filepath.Join(dir, name)
	if _, err := os.Stat(dst); err != nil {
		return dst
	}

	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)

	// One fixed "_conflict" slot silently overwrote the third same-named sibling.
	for i := 2; ; i++ {
		dst = filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, i, ext))
		if _, err := os.Stat(dst); err != nil {
			return dst
		}
	}
}

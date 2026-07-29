package library

import (
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
}

var subExts = map[string]bool{
	".vtt": true, ".srt": true, ".ass": true, ".ssa": true,
}

var keepExts = map[string]bool{
	".torrent": true, ".aria2": true,
}

// keepSuffixes are compound suffixes (multi-dot) that keepExts can't match
// against filepath.Ext (which only returns the trailing extension). Kept
// literally in the tree so recovery paths — .corrupt.bak from repair — are
// never nuked by the junk sweep.
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

		ext := strings.ToLower(filepath.Ext(d.Name()))
		lower := strings.ToLower(d.Name())
		kept := false
		for _, sfx := range keepSuffixes {
			if strings.HasSuffix(lower, sfx) {
				kept = true
				break
			}
		}
		if !kept && !videoExts[ext] && !subExts[ext] && !keepExts[ext] {
			// A recently-written non-video file is someone's live output (an
			// in-flight ffmpeg .tmp — possibly from an encoder orphaned by a
			// restart). Deleting it unlinks the path under the writer and the
			// finished file is lost. Only sweep files that have gone quiet.
			if fi, err := d.Info(); err == nil && time.Since(fi.ModTime()) < time.Hour {
				return nil
			}
			log.Printf("[CleanAll] junk: %s", path)
			os.Remove(path)
		}

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

// activeDirs walks root and marks every directory that contains a "busy"
// file (torrent-owned / sidecar / freshly-written) — the single-source-of-
// -truth IsBusy check. Those directories are exempt from clean/flatten.
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
		log.Printf("[CleanAll] no videos, nuking: %s", filepath.Base(dir))
		os.RemoveAll(dir)
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

	os.RemoveAll(dir)
}

func flatDir(dir string, videoPaths, subPaths []string) {
	for _, vp := range videoPaths {
		if filepath.Dir(vp) == dir {
			continue
		}

		dst := safeMoveDst(dir, filepath.Base(vp))
		os.Rename(vp, dst)
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
			os.RemoveAll(filepath.Join(dir, e.Name()))
			log.Printf("[CleanAll] removed subdir: %s/%s", filepath.Base(dir), e.Name())
		}
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

	return filepath.Join(dir, base+"_conflict"+ext)
}

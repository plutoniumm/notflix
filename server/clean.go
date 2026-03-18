package server

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

var videoExts = map[string]bool{
	".mp4": true, ".mkv": true, ".mov": true, ".webm": true,
	".avi": true, ".flv": true, ".wmv": true, ".m4v": true,
	".mpg": true, ".mpeg": true, ".ts": true, ".3gp": true,
}

var subExts = map[string]bool{
	".vtt": true, ".srt": true, ".ass": true, ".ssa": true,
}

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
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(d.Name()))
		if !videoExts[ext] && !subExts[ext] {
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

		processSubdir(root, filepath.Join(root, e.Name()))
	}
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

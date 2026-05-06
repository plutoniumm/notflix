package media

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"notflix/server/jobs"
	"notflix/server/library"
)

var progress = jobs.NewTracker()

func GetProgress() []jobs.Progress {
	return progress.List()
}

var processing atomic.Bool

func ProcessAll(lib *library.Library) {
	if !processing.CompareAndSwap(false, true) {
		log.Println("[ProcessAll] already running, skipping")
		return
	}
	defer processing.Store(false)

	jobs.WaitAria2()
	ConvertAll(lib)
	library.CleanAll(lib.Roots, jobs.Aria2ActivePaths())
	library.ScanCorrupt(lib)
	RegenerateThumbnails(lib, 0)
	log.Println("[ProcessAll] done")
}

func IsProcessing() bool {
	return processing.Load()
}

func ConvertAll(lib *library.Library) {
	var wg sync.WaitGroup
	for _, root := range lib.Roots {
		if _, err := os.Stat(root); err != nil {
			continue
		}

		wg.Add(1)
		go func(root string) {
			defer wg.Done()
			convertRoot(root, jobs.NewPool(3))
		}(root)
	}

	wg.Wait()
	log.Println("ConvertAll: done")
}

func freeBytes(dir string) int64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return -1
	}
	return int64(st.Bavail) * int64(st.Bsize)
}

func convertRoot(root string, pool *jobs.Pool) {
	var wg sync.WaitGroup
	downloading := jobs.Aria2ActivePaths()
	var diskFull atomic.Bool

	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		inValid := d.IsDir() || strings.HasPrefix(d.Name(), ".")
		if err != nil || inValid {
			return nil
		}

		if diskFull.Load() {
			return filepath.SkipAll
		}

		name := d.Name()
		if strings.HasSuffix(name, ".audio.tmp") || strings.HasSuffix(name, ".audio.tmp.mp4") {
			if err := os.Remove(path); err == nil {
				log.Printf("[convert] removed stale temp: %s", name)
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(d.Name()))
		isMP4 := ext == ".mp4"
		switch ext {
		case ".mp4":
		case ".mkv", ".mov", ".webm", ".avi", ".flv", ".wmv", ".m4v", ".mpg", ".mpeg", ".ts", ".3gp":
		default:
			return nil
		}

		if isMP4 && !audioNeedsTranscode(path) {
			return nil
		}

		if downloading[path] {
			log.Printf("[convert] skipping (aria2 active): %s", d.Name())
			return nil
		}
		if _, err := os.Stat(path + ".aria2"); err == nil {
			return nil
		}
		info, err := d.Info()
		if err == nil && time.Since(info.ModTime()) < 30*time.Second {
			return nil
		}

		if info != nil {
			free := freeBytes(filepath.Dir(path))
			if free >= 0 && free < info.Size()+int64(512<<20) {
				log.Printf("[convert] low disk space on %s (free=%dMB), skipping root", root, free>>20)
				diskFull.Store(true)
				return filepath.SkipAll
			}
		}

		pool.Acquire()
		wg.Add(1)

		go func(p string, audioOnly bool) {
			defer wg.Done()
			defer pool.Release()

			var err error
			if audioOnly {
				err = remuxAudio(p)
			} else {
				err = toMP4(p)
			}
			if err == errNoSpace {
				diskFull.Store(true)
			}
		}(path, isMP4)

		return nil
	})

	wg.Wait()
}

var errNoSpace = fmt.Errorf("no space left on device")

func toMP4(srcPath string) error {
	dir := filepath.Dir(srcPath)
	srcName := filepath.Base(srcPath)
	cleanedBase := library.CleanName(srcName)

	mp4Path := filepath.Join(dir, cleanedBase+".mp4")
	name := srcName

	if _, err := os.Stat(mp4Path); err == nil {
		log.Printf("Incomplete conversion detected, restarting: %s", name)
		os.Remove(mp4Path)
	}

	progress.Update(name, 0)
	defer progress.Finish(name)

	dur := duration(srcPath)
	if dur <= 0 {
		log.Printf("[convert] skipping unreadable/corrupt file: %s", name)
		return nil
	}

	extractAllSubs(srcPath, filepath.Join(dir, cleanedBase))

	if err := remux(srcPath, mp4Path, dur, name); err != nil {
		log.Printf("Conversion failed %s: %v", name, err)
		return err
	}

	os.Remove(srcPath)
	return nil
}

func duration(path string) float64 {
	f, err := library.Prober.Format(context.Background(), path)
	if err != nil {
		return 0
	}
	return f.Duration.Duration.Seconds()
}

func codecs(videoPath string) (videoCodec string, audioCodecs []string) {
	streams, err := library.Prober.Streams(context.Background(), videoPath)
	if err != nil {
		return "", nil
	}

	for _, s := range streams {
		switch s.CodecType {
		case "video":
			if videoCodec == "" {
				videoCodec = s.CodecName
			}
		case "audio":
			audioCodecs = append(audioCodecs, s.CodecName)
		}
	}
	return
}

func audioOK(codec string) bool {
	switch strings.ToLower(codec) {
	case "aac", "mp3":
		return true
	}
	return false
}

func mp4AudioOK(codecs []string) bool {
	for _, c := range codecs {
		if !audioOK(c) {
			return false
		}
	}
	return len(codecs) > 0
}

func audioNeedsTranscode(path string) bool {
	streams, err := library.Prober.Streams(context.Background(), path)
	if err != nil {
		return false
	}
	for _, s := range streams {
		if s.CodecType == "audio" && !audioOK(s.CodecName) {
			return true
		}
	}
	return false
}

func remuxAudio(srcPath string) error {
	name := filepath.Base(srcPath)

	progress.Update(name, 0)
	defer progress.Finish(name)

	dur := duration(srcPath)
	if dur <= 0 {
		log.Printf("[convert] skipping unreadable/corrupt file: %s", name)
		return nil
	}

	tmp := srcPath + ".audio.tmp"

	vc, _ := codecs(srcPath)
	args := []string{
		"-i", srcPath,
		"-map", "0:v:0", "-map", "0:a", "-map_chapters", "-1",
		"-c:v", "copy",
	}
	if vc == "hevc" {
		args = append(args, "-tag:v", "hvc1")
	}
	args = append(args,
		"-c:a", "aac", "-b:a", "192k",
		"-movflags", "+faststart", "-f", "mp4", tmp,
	)

	stderr, err := library.FFRun{
		Args:     args,
		Duration: dur,
		OnPct:    func(p float64) { progress.Update(name, p) },
	}.Run()
	if err != nil {
		log.Printf("ffmpeg audio remux error for %s:\n%s", name, stderr)
		os.Remove(tmp)
		if strings.Contains(stderr, "No space left on device") {
			return errNoSpace
		}
		return err
	}

	return os.Rename(tmp, srcPath)
}

func remux(src, dst string, durationSec float64, name string) error {
	tmp := dst + ".tmp"

	vc, ac := codecs(src)
	args := []string{"-i", src, "-map", "0:v:0", "-map", "0:a"}

	switch vc {
	case "h264":
		args = append(args, "-c:v", "copy")
	case "hevc":
		args = append(args, "-c:v", "copy", "-tag:v", "hvc1")
	default:
		args = append(args, "-c:v", "libx264", "-preset", "fast", "-crf", "23")
	}

	if mp4AudioOK(ac) {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args, "-c:a", "aac", "-b:a", "192k")
	}

	args = append(args, "-movflags", "+faststart", "-f", "mp4", tmp)

	stderr, err := library.FFRun{
		Args:     args,
		Duration: durationSec,
		OnPct:    func(p float64) { progress.Update(name, p) },
	}.Run()
	if err != nil {
		log.Printf("ffmpeg error for %s:\n%s", name, stderr)
		os.Remove(tmp)
		if strings.Contains(stderr, "No space left on device") {
			return errNoSpace
		}
		return err
	}

	return os.Rename(tmp, dst)
}

func extractAllSubs(srcPath, base string) {
	streams, err := library.Prober.Streams(context.Background(), srcPath)
	if err != nil {
		return
	}

	textCodecs := map[string]bool{"subrip": true, "ass": true, "webvtt": true, "mov_text": true}
	langCount := map[string]int{}

	for _, s := range streams {
		if s.CodecType != "subtitle" || !textCodecs[strings.ToLower(s.CodecName)] {
			continue
		}

		lang := "und"
		if l, ok := s.Tags["language"]; ok && l != "" {
			lang = strings.ToLower(l)
		}

		langCount[lang]++
		outPath := base + "." + lang + ".vtt"
		if langCount[lang] > 1 {
			outPath = fmt.Sprintf("%s.%s%d.vtt", base, lang, langCount[lang])
		}

		if _, err := os.Stat(outPath); err == nil {
			continue
		}

		if stderr, err := library.FF("-y", "-i", srcPath,
			"-map", fmt.Sprintf("0:%d", s.Index),
			"-c:s", "webvtt", outPath); err != nil {
			log.Printf("[convert] sub extraction failed (stream %d, %s): %v %s", s.Index, lang, err, stderr)
			os.Remove(outPath)
		} else {
			log.Printf("[convert] extracted sub: %s", filepath.Base(outPath))
		}
	}
}

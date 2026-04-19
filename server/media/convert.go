package media

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"notflix/server/jobs"
	"notflix/server/library"
)

type Progress struct {
	Name    string  `json:"name"`
	Percent float64 `json:"percent"`
}

var (
	mut    sync.Mutex
	active = map[string]float64{}
)

func GetProgress() []Progress {
	mut.Lock()
	defer mut.Unlock()
	out := make([]Progress, 0, len(active))

	for name, pct := range active {
		out = append(out, Progress{
			Name:    name,
			Percent: pct,
		})
	}

	return out
}

func setProgress(name string, pct float64) {
	mut.Lock()
	active[name] = pct
	mut.Unlock()
}

func clearProgress(name string) {
	mut.Lock()
	delete(active, name)
	mut.Unlock()
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
	library.CleanAll(lib.Roots)
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
			convertRoot(root)
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

func convertRoot(root string) {
	sem := make(chan struct{}, 3)
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

		ext := strings.ToLower(filepath.Ext(d.Name()))
		switch ext {
		case ".mp4":
			return nil
		case ".mkv", ".mov", ".webm", ".avi", ".flv", ".wmv", ".m4v", ".mpg", ".mpeg", ".ts", ".3gp":

		default:
			return nil
		}

		// skip files still being downloaded
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

		sem <- struct{}{}
		wg.Add(1)

		go func(p string) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := toMP4(p); err == errNoSpace {
				diskFull.Store(true)
			}
		}(path)

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

	setProgress(name, 0)
	defer clearProgress(name)

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

var timeRe = regexp.MustCompile(`time=(\d{2}):(\d{2}):(\d{2})\.(\d{2})`)

type progressWriter struct {
	name        string
	durationSec float64
	buf         strings.Builder
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	s := string(p)
	pw.buf.WriteString(s)
	if pw.durationSec > 0 {
		if m := timeRe.FindStringSubmatch(s); m != nil {
			h, _ := strconv.Atoi(m[1])
			min, _ := strconv.Atoi(m[2])
			sec, _ := strconv.Atoi(m[3])
			cs, _ := strconv.Atoi(m[4])
			t := float64(h*3600+min*60+sec) + float64(cs)/100
			pct := math.Min(t/pw.durationSec*100, 99)
			setProgress(pw.name, pct)
		}
	}
	return len(p), nil
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

func mp4AudioOK(codecs []string) bool {
	ok := map[string]bool{"aac": true, "mp3": true, "ac3": true}
	for _, c := range codecs {
		if !ok[strings.ToLower(c)] {
			return false
		}
	}
	return len(codecs) > 0
}

func remux(src, dst string, durationSec float64, name string) error {
	tmp := dst + ".tmp"

	vc, ac := codecs(src)
	args := []string{
		"-nostdin", "-v", "error", "-i", src,
		"-map", "0:v:0", "-map", "0:a",
	}

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

	pw := &progressWriter{name: name, durationSec: durationSec}
	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = pw

	if devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0); err == nil {
		cmd.Stdin = devNull
		cmd.Stdout = devNull
		defer devNull.Close()
	}

	if err := cmd.Run(); err != nil {
		stderr := pw.buf.String()
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

		cmd := exec.Command("ffmpeg", "-y", "-v", "quiet", "-i", srcPath,
			"-map", fmt.Sprintf("0:%d", s.Index),
			"-c:s", "webvtt",
			outPath,
		)
		if err := cmd.Run(); err != nil {
			log.Printf("[convert] sub extraction failed (stream %d, %s): %v", s.Index, lang, err)
			os.Remove(outPath)
		} else {
			log.Printf("[convert] extracted sub: %s", filepath.Base(outPath))
		}
	}
}

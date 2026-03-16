package server

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

func ConvertAll(roots []string) {
	var wg sync.WaitGroup
	for _, root := range roots {
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

func convertRoot(root string) {
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup

	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		inValid := d.IsDir() || strings.HasPrefix(d.Name(), ".")
		if err != nil || inValid {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext == ".mp4" || ext == ".webm" {
			return nil
		}
		if ext != ".mkv" && ext != ".mov" {
			return nil
		}

		sem <- struct{}{}
		wg.Add(1)

		go func(p string) {
			defer wg.Done()
			defer func() { <-sem }()
			toMP4(p)
		}(path)

		return nil
	})

	wg.Wait()
}

func toMP4(srcPath string) {
	dir := filepath.Dir(srcPath)
	srcName := filepath.Base(srcPath)
	cleanedBase := CleanName(srcName)

	mp4Path := filepath.Join(dir, cleanedBase+".mp4")
	vttPath := filepath.Join(dir, cleanedBase+".vtt")
	name := srcName

	if _, err := os.Stat(mp4Path); err == nil {
		log.Printf("Incomplete conversion detected, restarting: %s", name)
		os.Remove(mp4Path)
	}

	setProgress(name, 0)
	defer clearProgress(name)

	if _, err := os.Stat(vttPath); os.IsNotExist(err) {
		extractSubs(srcPath, vttPath)
	}

	dur := duration(srcPath)

	if err := remux(srcPath, mp4Path, dur, name); err != nil {
		log.Printf("Conversion failed %s: %v", name, err)
		return
	}

	os.Remove(srcPath)
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
	f, err := prober.Format(context.Background(), path)
	if err != nil {
		return 0
	}
	return f.Duration.Duration.Seconds()
}

func codecs(videoPath string) (videoCodec, audioCodec string) {
	streams, err := prober.Streams(context.Background(), videoPath)
	if err != nil {
		return "", ""
	}
	
	for _, s := range streams {
		switch s.CodecType {
		case "video":
			videoCodec = s.CodecName
		case "audio":
			audioCodec = s.CodecName
		}
	}
	return
}

func remux(src, dst string, durationSec float64, name string) error {
	tmp := dst + ".tmp"

	vc, ac := codecs(src)
	args := []string{
		"-nostdin", "-v", "error", "-i", src,
		"-map", "0:v:0", "-map", "0:a:0",
	}
	args = append(args, codecArgs(vc, ac)...)
	args = append(args,
		"-movflags", "+faststart",
		"-f", "mp4", tmp,
	)

	pw := &progressWriter{name: name, durationSec: durationSec}
	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = pw

	if devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0); err == nil {
		cmd.Stdin = devNull
		cmd.Stdout = devNull
		defer devNull.Close()
	}

	if err := cmd.Run(); err != nil {
		log.Printf("ffmpeg error for %s:\n%s", name, pw.buf.String())
		os.Remove(tmp)
		return err
	}

	return os.Rename(tmp, dst)
}

func codecArgs(video, audio string) []string {
	var args []string

	switch video {
	case "h264":
		args = append(args, "-c:v", "copy")
	case "hevc":
		args = append(args, "-c:v", "copy", "-tag:v", "hvc1")
	default:
		args = append(args, "-c:v", "libx264", "-preset", "fast", "-crf", "23")
	}

	switch audio {
	case "aac", "mp3":
		args = append(args, "-c:a", "copy")
	default:
		args = append(args, "-c:a", "aac")
	}

	return args
}

func extractSubs(srcPath, vttPath string) {
	streams, err := prober.Streams(context.Background(), srcPath)
	if err != nil {
		return
	}

	english := map[string]bool{"en": true, "eng": true, "english": true, "sdh": true}
	textCodecs := map[string]bool{"subrip": true, "ass": true, "webvtt": true, "mov_text": true}

	idx := -1
	for _, s := range streams {
		if s.CodecType != "subtitle" || !textCodecs[strings.ToLower(s.CodecName)] {
			continue
		}
		if lang, ok := s.Tags["language"]; ok && english[strings.ToLower(lang)] {
			idx = s.Index
			break
		}
	}

	if idx < 0 {
		return
	}

	extractCmd := exec.Command("ffmpeg", "-y", "-v", "quiet", "-i", srcPath,
		"-map", fmt.Sprintf("0:%d", idx),
		vttPath,
	)

	if err := extractCmd.Run(); err != nil {
		log.Printf("Sub extraction failed for %s: %v", filepath.Base(srcPath), err)
		os.Remove(vttPath)
	}
}

package server

import (
	"encoding/json"
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
			toWebM(p)
		}(path)

		return nil
	})

	wg.Wait()
}

func toWebM(srcPath string) {
	dir := filepath.Dir(srcPath)
	srcName := filepath.Base(srcPath)
	cleanedBase := CleanName(srcName)

	webmPath := filepath.Join(dir, cleanedBase+".webm")
	vttPath := filepath.Join(dir, cleanedBase+".vtt")
	name := srcName

	if _, err := os.Stat(webmPath); err == nil {
		log.Printf("Incomplete conversion detected, restarting: %s", name)
		os.Remove(webmPath)
	}

	setProgress(name, 0)
	defer clearProgress(name)

	if _, err := os.Stat(vttPath); os.IsNotExist(err) {
		extractSubs(srcPath, vttPath)
	}

	dur := duration(srcPath)

	if err := remux(srcPath, webmPath, dur, name); err != nil {
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
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)

	out, err := cmd.Output()
	if err != nil {
		return 0
	}

	d, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)

	return d
}

func codecs(videoPath string) (videoCodec, audioCodec string) {
	run := func(streamSpec string) string {
		cmd := exec.Command("ffprobe",
			"-v", "error",
			"-select_streams", streamSpec,
			"-show_entries", "stream=codec_name",
			"-of", "default=noprint_wrappers=1:nokey=1",
			videoPath,
		)

		out, err := cmd.Output()
		if err != nil {
			return ""
		}

		return strings.TrimSpace(string(out))
	}

	return run("v:0"), run("a:0")
}

func remux(src, dst string, durationSec float64, name string) error {
	tmp := dst + ".tmp"

	args := []string{
		"-nostdin", "-v", "error", "-i", src,
		"-map", "0:v:0", "-map", "0:a:0",
		"-c:v", "libvpx-vp9", "-quality", "good", "-cpu-used", "4",
		"-b:v", "0", "-crf", "28", "-row-mt", "1",
		"-c:a", "libopus", "-b:a", "128k", "-ac", "2",
		"-f", "webm", tmp,
	}

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
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "s",
		"-show_entries", "stream=index,codec_name:stream_tags=language",
		"-of", "json",
		srcPath,
	)
	out, err := cmd.Output()
	if err != nil {
		return
	}

	var probe struct {
		Streams []struct {
			Index     int               `json:"index"`
			CodecName string            `json:"codec_name"`
			Tags      map[string]string `json:"tags"`
		} `json:"streams"`
	}

	if err := json.Unmarshal(out, &probe); err != nil {
		return
	}

	english := map[string]bool{"en": true, "eng": true, "english": true, "sdh": true}
	textCodecs := map[string]bool{"subrip": true, "ass": true, "webvtt": true, "mov_text": true}

	idx := -1
	for _, s := range probe.Streams {
		if !textCodecs[strings.ToLower(s.CodecName)] {
			continue
		}

		lang := ""
		if s.Tags != nil {
			lang = strings.ToLower(s.Tags["language"])
		}

		if english[lang] {
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

package server

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	cleanKeywords = func() []*regexp.Regexp {
		words := []string{
			"nflx", "amzn", "hevc", "hdtv", "hdrip", "bluray", "web-dl", "webrip", "web",
			"hevc-psa", "webdl", "eac3", "avi", "hdr", "mp4", "mkv", "dvdrip", "repack",
			"split", "scenes", "rq", "aac", "10bit", "atmos", "ddp5", "dd5", "ac3", "2025",
			"x264", "x265", "h264", "h265", "720p", "480p", "1080p", "2160p", "4k",
		}
		res := make([]*regexp.Regexp, len(words))
		for i, w := range words {
			res[i] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(w) + `\b`)
		}
		return res
	}()
	bracketRe = regexp.MustCompile(`\[.*?\]|\(.*?\)`)
	spacesRe  = regexp.MustCompile(`\s+`)
)

func CleanName(filename string) string {
	s := strings.TrimSuffix(filename, filepath.Ext(filename))
	for _, re := range cleanKeywords {
		s = re.ReplaceAllString(s, "")
	}
	s = bracketRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, ".", " ")
	s = spacesRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func GetVids(dir string) []string {
	var out []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("Error reading directory %s: %v", dir, err)
		return out
	}

	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if strings.HasSuffix(e.Name(), ".mp4") {
			out = append(out, e.Name())
		}
	}
	return out
}

func makeThumb(src, dst string) error {
	f, err := prober.Format(context.Background(), src)
	if err != nil {
		return err
	}
	dur := f.Duration.Duration.Seconds()
	if dur == 0 {
		return fmt.Errorf("invalid duration")
	}
	ts := fmt.Sprintf("%.2f", dur/2+rand.Float64()*10-5)
	cmd := exec.Command("ffmpeg", "-y", "-v", "quiet", "-ss", ts, "-i", src, "-vframes", "1", dst)
	return cmd.Run()
}

func GenerateThumbnails(dir string) {
	tdir := "images"
	os.MkdirAll(tdir, 0755)
	existing := make(map[string]struct{})

	entries, _ := os.ReadDir(tdir)
	for _, e := range entries {
		existing[e.Name()] = struct{}{}
	}

	walk := func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext == ".mp4" {
			name := Hash(d.Name()) + ".jpg"
			dst := filepath.Join(tdir, name)
			if _, err := os.Stat(dst); err != nil {
				makeThumb(path, dst)
			}
		}
		return nil
	}

	filepath.WalkDir(dir, walk)
}

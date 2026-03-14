package server

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

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
		if strings.HasSuffix(e.Name(), ".mp4") || strings.HasSuffix(e.Name(), ".webm") {
			out = append(out, e.Name())
		}
	}
	return out
}

func makeThumb(src, dst string) error {
	cmd := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", src)
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	dur, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || dur == 0 {
		return fmt.Errorf("invalid duration")
	}
	ts := fmt.Sprintf("%.2f", dur/2+rand.Float64()*10-5)
	cmd = exec.Command("ffmpeg", "-y", "-ss", ts, "-i", src, "-vframes", "1", dst)
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
		if ext == ".mp4" || ext == ".webm" {
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

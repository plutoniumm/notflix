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
	var videos []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("Error reading directory %s: %v", dir, err)
		return videos
	}

	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".mp4") {
			videos = append(videos, entry.Name())
		}
	}
	return videos
}

func makeThumbnail(videoPath, thumbPath string) error {
	cmd := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", videoPath)
	output, err := cmd.Output()
	if err != nil {
		return err
	}
	dur, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil || dur == 0 {
		return fmt.Errorf("invalid duration")
	}
	timestamp := fmt.Sprintf("%.2f", dur/2+rand.Float64()*10-5)
	cmd = exec.Command("ffmpeg", "-y", "-ss", timestamp, "-i", videoPath, "-vframes", "1", thumbPath)
	return cmd.Run()
}

func GenerateThumbnails(videosDir string) {
	thumbDir := "images"
	os.MkdirAll(thumbDir, 0755)
	thumbsExisting := make(map[string]struct{})

	entries, _ := os.ReadDir(thumbDir)
	for _, e := range entries {
		thumbsExisting[e.Name()] = struct{}{}
	}

	thumbsDone := make(map[string]struct{})

	walkFunc := func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".mp4") {
			id := Hash(d.Name())
			thumbName := id + ".jpg"
			thumbPath := filepath.Join(thumbDir, thumbName)
			thumbsDone[thumbName] = struct{}{}
			if _, statErr := os.Stat(thumbPath); statErr != nil {
				makeThumbnail(path, thumbPath)
			}
		}
		return nil
	}

	filepath.WalkDir(videosDir, walkFunc)
}

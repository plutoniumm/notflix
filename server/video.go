package server

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
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

	allowedExts := []string{".mp4", ".mkv", ".mov"}

	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		for _, ext := range allowedExts {
			if strings.HasSuffix(entry.Name(), ext) {
				videos = append(videos, entry.Name())
				break
			}
		}
	}
	return videos
}

func VideoPlayer(c *gin.Context, videosDir string) {
	filename := c.Param("filename")
	filename, err := url.QueryUnescape(filename)
	if err != nil {
		log.Printf("Error unescaping filename: %v", err)
		return
	}

	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".mp4" {
		c.String(http.StatusBadRequest, "Only MP4 videos are supported")
		return
	}

	videoPath := filepath.Join(videosDir, filename)

	absVideoPath, err := filepath.Abs(videoPath)
	if err != nil {
		Error("Error resolving video path", c, 500)
		return
	}
	absVideosDir, _ := filepath.Abs(videosDir)
	if !strings.HasPrefix(absVideoPath, absVideosDir) {
		Error("Invalid video path", c, 400)
		return
	}

	file, err := os.Open(videoPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.String(http.StatusNotFound, "Video not found")
		} else {
			Error("Error opening video file: "+err.Error(), c, 500)
		}
		return
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		Error("Error stating video file: "+videoPath+":: "+err.Error(), c, 500)
		return
	}

	fileSize := fileInfo.Size()
	rangeHeader := c.GetHeader("Range")

	if rangeHeader == "" {
		c.Header("Content-Type", "video/mp4")
		c.Header("Content-Length", strconv.FormatInt(fileSize, 10))
		_, err = io.Copy(c.Writer, file)
		if err != nil {
			log.Printf("Error serving full video content for %s: %v", filename, err)
		}
		return
	}

	start, end, cLenStr := ParseRangeHeader(rangeHeader, fileSize)

	c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Length", cLenStr)
	c.Header("Content-Type", "video/mp4")
	c.Status(http.StatusPartialContent)

	_, err = file.Seek(start, io.SeekStart)
	if err != nil {
		Error("Error seeking video file: "+videoPath+":: "+err.Error(), c, 500)
		return
	}

	bytesToServe, _ := strconv.ParseInt(cLenStr, 10, 64)
	_, err = io.CopyN(c.Writer, file, bytesToServe)
	if err != nil && err != io.EOF {
		log.Printf("Error serving partial video content for %s: %v", filename, err)
	}
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
		allowedExts := []string{".mp4", ".mkv", ".mov"}
		for _, ext := range allowedExts {
			if strings.HasSuffix(d.Name(), ext) {
				thumbName := d.Name() + ".jpg"
				thumbPath := filepath.Join(thumbDir, thumbName)
				thumbsDone[thumbName] = struct{}{}
				if _, exists := thumbsExisting[thumbName]; exists {
					continue
				}
				makeThumbnail(path, thumbPath)
				break
			}
		}
		return nil
	}

	filepath.WalkDir(videosDir, walkFunc)

	// delete stale thumbs
	for thumb := range thumbsExisting {
		if _, ok := thumbsDone[thumb]; !ok {
			os.Remove(filepath.Join(thumbDir, thumb))
		}
	}
}

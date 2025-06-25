package server

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"

	"path/filepath"
	"strconv"
	"strings"
)

func VideoPlayer(c *gin.Context, videosDir string) {
	filename, err := getSafeFilename(c)
	if err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	videoPath, err := resolveVideoPath(videosDir, filename)
	if err != nil {
		Error(err.Error(), c, http.StatusBadRequest)
		return
	}

	file, fileInfo, err := openVideoFile(videoPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.String(http.StatusNotFound, "Video not found")
		} else {
			Error("Error opening video file: "+err.Error(), c, http.StatusInternalServerError)
		}
		return
	}
	defer file.Close()

	serveVideoContent(c, file, fileInfo, filename)
}

func getSafeFilename(c *gin.Context) (string, error) {
	name, err := url.QueryUnescape(c.Param("filename"))
	if err != nil {
		return "", fmt.Errorf("invalid filename")
	}
	if strings.Contains(name, "..") {
		return "", fmt.Errorf("invalid path traversal attempt")
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".mp4" {
		return "", fmt.Errorf("only MP4 videos are supported")
	}
	return name, nil
}

func resolveVideoPath(baseDir, filename string) (string, error) {
	videoPath := filepath.Join(baseDir, filename)
	absVideoPath, err := filepath.Abs(videoPath)
	if err != nil {
		return "", fmt.Errorf("error resolving path")
	}
	absBaseDir, _ := filepath.Abs(baseDir)
	if !strings.HasPrefix(absVideoPath, absBaseDir) {
		return "", fmt.Errorf("invalid video path")
	}
	return absVideoPath, nil
}

func openVideoFile(path string) (*os.File, os.FileInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	return file, info, nil
}

func serveVideoContent(c *gin.Context, file *os.File, info os.FileInfo, filename string) {
	fileSize := info.Size()
	rangeHeader := c.GetHeader("Range")

	if rangeHeader == "" {
		c.Header("Content-Type", "video/mp4")
		c.Header("Content-Length", strconv.FormatInt(fileSize, 10))
		_, err := io.Copy(c.Writer, file)
		if err != nil {
			log.Printf("Error serving full video %s: %v", filename, err)
		}
		return
	}

	start, end, cLenStr := ParseRangeHeader(rangeHeader, fileSize)
	c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Length", cLenStr)
	c.Header("Content-Type", "video/mp4")
	c.Status(http.StatusPartialContent)

	_, err := file.Seek(start, io.SeekStart)
	if err != nil {
		Error("seek error: "+err.Error(), c, http.StatusInternalServerError)
		return
	}

	bytesToServe, _ := strconv.ParseInt(cLenStr, 10, 64)
	_, err = io.CopyN(c.Writer, file, bytesToServe)
	if err != nil && err != io.EOF {
		log.Printf("Error serving partial video %s: %v", filename, err)
	}
}

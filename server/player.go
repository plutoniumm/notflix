package server

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func VideoPlayer(c *gin.Context, videosDir string) {
	name, err := getfname(c)
	if err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	path, err := resolve(videosDir, name)
	if err != nil {
		Error(err.Error(), c, http.StatusBadRequest)
		return
	}

	file, info, err := openVid(path)
	if err != nil {
		if os.IsNotExist(err) {
			c.String(http.StatusNotFound, "Video not found")
		} else {
			Error("Error opening video file: "+err.Error(), c, http.StatusInternalServerError)
		}
		return
	}
	defer file.Close()

	serve(c, file, info, name)
}

func VideoHead(c *gin.Context, videosDir string) {
	name, err := getfname(c)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	path, err := resolve(videosDir, name)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			c.Status(http.StatusNotFound)
		} else {
			c.Status(http.StatusInternalServerError)
		}
		return
	}

	c.Header("Content-Type", "video/mp4")
	c.Header("Content-Length", strconv.FormatInt(info.Size(), 10))
	c.Header("Accept-Ranges", "bytes")
	c.Status(http.StatusOK)
}

func getfname(c *gin.Context) (string, error) {
	name, err := url.QueryUnescape(c.Param("filename"))
	if err != nil {
		return "", fmt.Errorf("invalid filename")
	}

	if strings.Contains(name, "..") {
		return "", fmt.Errorf("invalid path traversal attempt")
	}

	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".mp4" {
		return "", fmt.Errorf("unsupported video format")
	}

	return name, nil
}

func resolve(base, name string) (string, error) {
	abs, err := filepath.Abs(filepath.Join(base, name))
	if err != nil {
		return "", fmt.Errorf("error resolving path")
	}

	absBase, _ := filepath.Abs(base)
	if !strings.HasPrefix(abs, absBase) {
		return "", fmt.Errorf("invalid video path")
	}

	return abs, nil
}

func openVid(path string) (*os.File, os.FileInfo, error) {
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

func serve(c *gin.Context, file *os.File, info os.FileInfo, name string) {
	size := info.Size()
	rng := c.GetHeader("Range")
	ct := "video/mp4"

	if rng == "" {
		c.Header("Content-Type", ct)
		c.Header("Content-Length", strconv.FormatInt(size, 10))
		c.Header("Accept-Ranges", "bytes")

		if _, err := io.Copy(c.Writer, file); err != nil && !clientGone(err) {
			log.Printf("Error serving full video %s: %v", name, err)
		}

		return
	}

	start, end, clen := Range(rng, size)
	c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Length", clen)
	c.Header("Content-Type", ct)
	c.Status(http.StatusPartialContent)

	if _, err := file.Seek(start, io.SeekStart); err != nil {
		Error("seek error: "+err.Error(), c, http.StatusInternalServerError)
		return
	}

	blen, _ := strconv.ParseInt(clen, 10, 64)

	if _, err := io.CopyN(c.Writer, file, blen); err != nil && err != io.EOF && !clientGone(err) {
		log.Printf("Error serving partial video %s: %v", name, err)
	}
}

func VideoInfo(c *gin.Context, roots []string) {
	file := c.Query("file")
	if file == "" || strings.Contains(file, "..") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid"})
		return
	}

	var path string
	for _, root := range roots {
		absR, _ := filepath.Abs(root)
		candidate := filepath.Join(root, file)
		abs, err := filepath.Abs(candidate)
		if err != nil || !strings.HasPrefix(abs, absR) {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			path = candidate
			break
		}
	}

	if path == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	streams, err := prober.Streams(context.Background(), path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	height := 0
	for _, s := range streams {
		if s.CodecType == "video" {
			height = s.Height
			break
		}
	}
	dur := duration(path)
	c.JSON(http.StatusOK, gin.H{"height": height, "duration": dur})
}

func clientGone(err error) bool {
	if err == nil {
		return false
	}

	s := err.Error()

	return strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "connection reset by peer") ||
		strings.Contains(s, "write: connection timed out")
}

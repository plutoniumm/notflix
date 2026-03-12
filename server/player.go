package server

import (
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
	filename, err := getfname(c)
	if err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	videoPath, err := resolve(videosDir, filename)
	if err != nil {
		Error(err.Error(), c, http.StatusBadRequest)
		return
	}

	file, fileInfo, err := openVid(videoPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.String(http.StatusNotFound, "Video not found")
		} else {
			Error("Error opening video file: "+err.Error(), c, http.StatusInternalServerError)
		}
		return
	}
	defer file.Close()

	serve(c, file, fileInfo, filename)
}

func getfname(c *gin.Context) (string, error) {
	name, err := url.QueryUnescape(c.Param("filename"))
	if err != nil {
		return "", fmt.Errorf("invalid filename")
	}

	if strings.Contains(name, "..") {
		return "", fmt.Errorf("invalid path traversal attempt")
	}

	if strings.ToLower(filepath.Ext(name)) != ".mp4" {
		return "", fmt.Errorf("unsupported video format")
	}

	return name, nil
}

func resolve(baseDir, filename string) (string, error) {
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

func serve(c *gin.Context, file *os.File, info os.FileInfo, filename string) {
	fileSize := info.Size()
	rangeHeader := c.GetHeader("Range")

	if rangeHeader == "" {
		c.Header("Content-Type", "video/mp4")
		c.Header("Content-Length", strconv.FormatInt(fileSize, 10))
		c.Header("Accept-Ranges", "bytes")
		_, err := io.Copy(c.Writer, file)
		if err != nil && !isClientGone(err) {
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
	if err != nil && err != io.EOF && !isClientGone(err) {
		log.Printf("Error serving partial video %s: %v", filename, err)
	}
}

func isClientGone(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "connection reset by peer") ||
		strings.Contains(s, "write: connection timed out")
}

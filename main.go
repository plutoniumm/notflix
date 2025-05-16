package main

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

	server "notflix/server"

	"github.com/gin-gonic/gin"
)

const (
	videosDir = "./videos"
	publicDir = "./public"
	assetsDir = "./public/assets"
	port      = "8080"
)

func listRender(c *gin.Context) {
	var videoFiles []string
	entries, err := os.ReadDir(videosDir)
	if err != nil {
		log.Printf("Error reading videos directory: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not list videos"})
		return
	}

	for _, entry := range entries {
		fullPath := filepath.Join(videosDir, entry.Name())
		fileInfo, err := os.Stat(fullPath)
		if err != nil {
			log.Printf("Error stating file %s: %v", fullPath, err)
			continue
		}

		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		allowed_exts := []string{".mp4", ".mkv", ".mov"}
		isAllowed := false
		for _, ext := range allowed_exts {
			if strings.HasSuffix(entry.Name(), ext) {
				isAllowed = true
				break
			}
		}
		if !isAllowed {
			continue
		}

		if !fileInfo.IsDir() {
			videoFiles = append(videoFiles, entry.Name())
		}
	}
	c.JSON(http.StatusOK, videoFiles)
}

func videoPlayer(c *gin.Context) {
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
		server.Error("Error resolving video path", c, 500)
		return
	}
	absVideosDir, _ := filepath.Abs(videosDir)
	if !strings.HasPrefix(absVideoPath, absVideosDir) {
		server.Error("Invalid video path", c, 400)
		return
	}

	file, err := os.Open(videoPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.String(http.StatusNotFound, "Video not found")
		} else {
			server.Error("Error opening video file: "+err.Error(), c, 500)
		}
		return
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		server.Error("Error stating video file: "+videoPath+":: "+err.Error(), c, 500)
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

	start, end, cLenStr := server.ParseRangeHeader(rangeHeader, fileSize)

	c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Length", cLenStr)
	c.Header("Content-Type", "video/mp4")
	c.Status(http.StatusPartialContent)

	_, err = file.Seek(start, io.SeekStart)
	if err != nil {
		server.Error("Error seeking video file: "+videoPath+":: "+err.Error(), c, 500)
		return
	}

	bytesToServe, _ := strconv.ParseInt(cLenStr, 10, 64)
	_, err = io.CopyN(c.Writer, file, bytesToServe)
	if err != nil && err != io.EOF {
		log.Printf("Error serving partial video content for %s: %v", filename, err)
	}
}

func subSend(c *gin.Context) {
	filename := c.Param("filename")
	subtitlePath := filepath.Join(videosDir, filename)
	fmt.Println("Subtitle path:", subtitlePath)

	absSubtitlePath, err := filepath.Abs(subtitlePath)
	if err != nil {
		c.String(http.StatusInternalServerError, "Error resolving subtitle path")
		return
	}
	absvideosDir, _ := filepath.Abs(videosDir)
	if !strings.HasPrefix(absSubtitlePath, absvideosDir) {
		c.String(http.StatusBadRequest, "Invalid subtitle path")
		return
	}

	if _, err := os.Stat(subtitlePath); os.IsNotExist(err) {
		c.String(http.StatusNotFound, "Subtitle not found")
		return
	}

	c.Header("Content-Type", "text/srt")
	c.File(subtitlePath)
}

func main() {
	server.EnsureDir(videosDir)
	server.EnsureDir(publicDir)
	server.EnsureDir(assetsDir)
	server.EnsureDir(videosDir)

	gin.SetMode("release")
	router := gin.New()
	router.Use(gin.Recovery())

	router.GET("/", func(c *gin.Context) {
		c.File(filepath.Join(publicDir, "index.html"))
	})
	router.Static("/assets", assetsDir)
	router.GET("/list", listRender)
	router.GET("/video/:filename", videoPlayer)
	router.DELETE("/video/:filename", func(c *gin.Context) {
		fname := c.Param("filename")
		videoPath := filepath.Join(videosDir, fname)
		srtPath := filepath.Join(videosDir, strings.TrimSuffix(fname, filepath.Ext(fname))+".srt")
		vttPath := filepath.Join(videosDir, strings.TrimSuffix(fname, filepath.Ext(fname))+".vtt")

		server.DelFile(videoPath)
		server.DelFile(srtPath)
		server.DelFile(vttPath)

		c.String(http.StatusOK, "true")
	})

	router.GET("/subs/:filename", subSend)

	log.Printf("Starting server on http://localhost:%s", port)
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}

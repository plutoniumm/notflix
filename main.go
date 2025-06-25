package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	server "notflix/server"

	"github.com/gin-gonic/gin"
)

const (
	imagesDir = "./images"
	videosDir = "./videos"
	publicDir = "./public"
	assetsDir = "./public/assets"
	port      = "8080"
)

func listRender(c *gin.Context) {
	result := make(map[string][]map[string]string)

	entries, err := os.ReadDir(videosDir)
	if err != nil {
		log.Printf("Error reading videos directory: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not list videos"})
		return
	}

	processDir := func(dir string) []map[string]string {
		files := server.GetVids(dir)
		var res []map[string]string
		for _, name := range files {
			res = append(res, map[string]string{
				"name": name,
				"key":  server.Hash(name),
			})
		}
		return res
	}

	result["."] = processDir(videosDir)

	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			subDir := filepath.Join(videosDir, entry.Name())
			result[entry.Name()] = processDir(subDir)
		}
	}

	c.JSON(http.StatusOK, result)
}

func subSend(c *gin.Context) {
	filename := c.Param("filename")
	subtitlePath := filepath.Join(videosDir, filename)

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
		c.File("index.html")
	})
	router.Static("/assets", assetsDir)
	router.GET("/list", listRender)

	router.GET("/video/*filename", func(c *gin.Context) {
		server.VideoPlayer(c, videosDir)
	})
	router.GET("/images/:filename", func(c *gin.Context) {
		c.File(filepath.Join(imagesDir, c.Param("filename")))
	})

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

	router.GET("/subs/*filename", subSend)

	go func() {
		server.GenerateThumbnails(videosDir)
	}()

	log.Printf("Starting server on http://localhost:%s", port)
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}

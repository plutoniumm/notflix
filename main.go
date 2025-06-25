package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

func listMovies() []Serie {
	var series []Serie

	// Root-level movies
	rootMovies := []Movie{}
	for _, vid := range server.GetVids(videosDir) {
		rootMovies = append(rootMovies, Movie{
			Title:  strings.TrimSuffix(vid, filepath.Ext(vid)),
			URL:    "/video/" + url.PathEscape(vid),
			Poster: "/assets/" + strings.TrimSuffix(vid, filepath.Ext(vid)) + ".jpg",
		})
	}
	if len(rootMovies) > 0 {
		series = append(series, Serie{Title: ".", Movies: rootMovies})
	}

	// Subdirectory series
	entries, err := os.ReadDir(videosDir)
	if err != nil {
		log.Printf("Error reading videos directory: %v", err)
		return series
	}

	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			subDir := filepath.Join(videosDir, entry.Name())
			movies := []Movie{}
			for _, vid := range server.GetVids(subDir) {
				movies = append(movies, Movie{
					Title:  strings.TrimSuffix(vid, filepath.Ext(vid)),
					URL:    "/video/" + url.PathEscape(filepath.Join(entry.Name(), vid)),
					Poster: "/assets/" + entry.Name() + "/" + strings.TrimSuffix(vid, filepath.Ext(vid)) + ".jpg",
				})
			}
			if len(movies) > 0 {
				series = append(series, Serie{Title: entry.Name(), Movies: movies})
			}
		}
	}

	return series
}

func listRender(c *gin.Context) {
	result := make(map[string][]string)

	// vids in root
	result["."] = server.GetVids(videosDir)

	entries, err := os.ReadDir(videosDir)
	if err != nil {
		log.Printf("Error reading videos directory: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not list videos"})
		return
	}

	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			subDir := filepath.Join(videosDir, entry.Name())
			result[entry.Name()] = server.GetVids(subDir)
		}
	}

	c.JSON(http.StatusOK, result)
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
		c.File("index.html")
	})
	router.Static("/assets", assetsDir)
	router.GET("/list", listRender)
	router.GET("/list_ui", func(c *gin.Context) {
		movies := listMovies()
		component := Series_list(movies)
		component.Render(context.Background(), os.Stdout)
	})

	router.GET("/video/:filename", func(c *gin.Context) {
		server.VideoPlayer(c, videosDir)
	})
	router.GET("/images/:filename", func(c *gin.Context) {
		c.File(filepath.Join(assetsDir, c.Param("filename")))
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

	router.GET("/subs/:filename", subSend)

	go func() {
		server.GenerateThumbnails(videosDir)
	}()

	log.Printf("Starting server on http://localhost:%s", port)
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}

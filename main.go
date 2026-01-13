package main

import (
	"log"
	"math/rand"
	"net"
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

var videosRoots = []string{videosDir, "/Users/gojira/Downloads/DC++"}

func pulse() {
	if rand.Float64() < 0.1 {
		for _, root := range videosRoots {
			go server.GenerateThumbnails(root)
		}
	}
}

func findRootFor(filename string, roots []string) (string, string, bool) {
	rel := strings.TrimPrefix(filename, "/")
	for _, r := range roots {
		absRoot, _ := filepath.Abs(r)
		candidate := filepath.Join(r, rel)
		absCandidate, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(absCandidate, absRoot) {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return r, absCandidate, true
		}
	}
	return "", "", false
}

func buildList(dir string) map[string][]map[string]string {
	result := make(map[string][]map[string]string)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return result
	}

	processDir := func(sub string) []map[string]string {
		files := server.GetVids(sub)
		var res []map[string]string
		for _, name := range files {
			res = append(res, map[string]string{
				"name": name,
				"key":  server.Hash(name),
			})
		}
		return res
	}

	result["."] = processDir(dir)

	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			subDir := filepath.Join(dir, entry.Name())
			result[entry.Name()] = processDir(subDir)
		}
	}

	return result
}

func listMedia(c *gin.Context, dir string) {
	pulse()
	c.JSON(http.StatusOK, buildList(dir))
}

func listMediaMulti(c *gin.Context, dirs []string) {
	pulse()
	merged := make(map[string][]map[string]string)
	for _, d := range dirs {
		m := buildList(d)
		for k, v := range m {
			merged[k] = append(merged[k], v...)
		}
	}
	c.JSON(http.StatusOK, merged)
}

func subSend(c *gin.Context) {
	pulse()

	filename := c.Param("filename")
	_, absSubtitlePath, ok := findRootFor(filename, videosRoots)
	if !ok {
		c.String(http.StatusNotFound, "Subtitle not found")
		return
	}

	c.Header("Content-Type", "text/srt")
	c.File(absSubtitlePath)
}

func main() {
	for _, root := range videosRoots {
		server.EnsureDir(root)
	}
	server.EnsureDir(publicDir)
	server.EnsureDir(assetsDir)

	// Ensure log directory exists
	logDir := "./log"
	err := os.MkdirAll(logDir, 0755)
	if err != nil {
		log.Fatalf("Failed to create log directory: %v", err)
	}

	gin.SetMode("release")
	router := gin.New()
	router.Use(gin.Recovery())

	router.Use(func(c *gin.Context) {
		c.Header("Content-Security-Policy", "")
		c.Next()
	})

	router.GET("/", func(c *gin.Context) {
		pulse()
		c.File("index.html")
	})
	router.GET("/embed", func(c *gin.Context) {
		pulse()
		c.File("embed.html")
	})
	router.Static("/assets", assetsDir)

	router.GET("/list/video", func(c *gin.Context) {
		listMediaMulti(c, videosRoots)
	})

	router.GET("/video/*filename", func(c *gin.Context) {
		pulse()
		fname := c.Param("filename")
		if root, _, ok := findRootFor(fname, videosRoots); ok {
			server.VideoPlayer(c, root)
			return
		}
		server.VideoPlayer(c, videosDir)
	})
	router.GET("/images/:filename", func(c *gin.Context) {
		pulse()
		c.File(filepath.Join(imagesDir, c.Param("filename")))
	})

	router.DELETE("/video/:filename", func(c *gin.Context) {
		pulse()
		fname := c.Param("filename")
		rel := strings.TrimPrefix(fname, "/")
		for _, root := range videosRoots {
			videoPath := filepath.Join(root, rel)
			srtPath := filepath.Join(root, strings.TrimSuffix(rel, filepath.Ext(rel))+".srt")
			vttPath := filepath.Join(root, strings.TrimSuffix(rel, filepath.Ext(rel))+".vtt")

			server.DelFile(videoPath)
			server.DelFile(srtPath)
			server.DelFile(vttPath)
		}

		c.String(http.StatusOK, "true")
	})

	router.GET("/subs/*filename", subSend)

	router.GET("/remote", func(c *gin.Context) {
		c.File("remote.html")
	})

	router.GET("/cmd", func(c *gin.Context) {
		c.File("./log/cmd")
		os.WriteFile("./log/cmd", []byte{}, 0644)
	})

	router.POST("/cmd", func(c *gin.Context) {
		data, err := c.GetRawData()
		if err != nil {
			c.String(http.StatusBadRequest, "Could not read body")
			return
		}
		err = os.WriteFile("./log/cmd", data, 0644)
		if err != nil {
			c.String(http.StatusInternalServerError, "Could not write file")
			return
		}
		c.String(http.StatusOK, "written")
	})

	router.POST("/error", func(c *gin.Context) {
		data, err := c.GetRawData()
		if err != nil {
			c.String(http.StatusBadRequest, "Could not read body")
			return
		}
		errLogPath := filepath.Join(logDir, "errors.log")
		f, err := os.OpenFile(errLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			c.String(http.StatusInternalServerError, "Log file error")
			return
		}
		defer f.Close()
		f.Write(data)
		f.Write([]byte("\n"))
		c.String(http.StatusOK, "logged")
	})

	log.Printf("Starting server on http://localhost:%s", port)

	// Print LAN address
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
				log.Printf("LAN URL: http://%s:%s", ipnet.IP.String(), port)
			}
		}
	}

	if err := router.Run(":" + port); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}

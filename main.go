package main

import (
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	server "notflix/server"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

const (
	imagesDir = "./images"
	publicDir = "./public"
	assetsDir = "./public/assets"
	port      = "4242"
)

var videosRoots = []string{
	"/Volumes/Ravan",
	"/Volumes/Oni",
	"/Volumes/Kumbhakarn",
}

func pulse() {
	if rand.Float64() < 0.01 {
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

func main() {
	// Load .env if present (silently ignored if missing)
	_ = godotenv.Load()

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

	go server.ConvertAll(videosRoots)

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
	router.GET("/manage", func(c *gin.Context) {
		c.File("index.html")
	})
	router.Static("/assets", assetsDir)

	router.GET("/list/video", func(c *gin.Context) {
		listMediaMulti(c, videosRoots)
	})

	router.GET("/video/*filename", func(c *gin.Context) {
		pulse()
		fname := c.Param("filename")
		root, _, ok := findRootFor(fname, videosRoots)
		if !ok {
			c.String(http.StatusNotFound, "Video not found")
			return
		}
		server.VideoPlayer(c, root)
	})
	router.GET("/images/:filename", func(c *gin.Context) {
		pulse()
		c.File(filepath.Join(imagesDir, c.Param("filename")))
	})

	router.DELETE("/video/*filename", func(c *gin.Context) {
		fname := c.Param("filename")
		rel := strings.TrimPrefix(fname, "/")
		base := strings.TrimSuffix(rel, filepath.Ext(rel))
		for _, root := range videosRoots {
			server.DelFile(filepath.Join(root, rel))
			server.DelFile(filepath.Join(root, base+".srt"))
			server.DelFile(filepath.Join(root, base+".vtt"))
			server.DelFile(filepath.Join(root, base+".whisper.vtt"))
		}
		c.String(http.StatusOK, "true")
	})

	router.POST("/api/rename", func(c *gin.Context) {
		var body struct {
			Path    string `json:"path"`
			NewName string `json:"name"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
			return
		}
		if body.NewName == "" || strings.ContainsAny(body.NewName, "/\\") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid name"})
			return
		}

		rel := strings.TrimPrefix(body.Path, "/")
		for _, root := range videosRoots {
			absRoot, _ := filepath.Abs(root)
			candidate := filepath.Join(root, rel)
			absCandidate, err := filepath.Abs(candidate)
			if err != nil || !strings.HasPrefix(absCandidate, absRoot) {
				continue
			}
			if _, err := os.Stat(candidate); err != nil {
				continue
			}

			newPath := filepath.Join(filepath.Dir(candidate), body.NewName)
			if err := os.Rename(candidate, newPath); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			// Also rename associated subtitle files for video renames
			oldExt := strings.ToLower(filepath.Ext(candidate))
			if oldExt == ".mp4" {
				oldBase := candidate[:len(candidate)-len(oldExt)]
				newBase := newPath[:len(newPath)-len(filepath.Ext(newPath))]
				for _, suf := range []string{".vtt", ".whisper.vtt", ".srt"} {
					old := oldBase + suf
					if _, err := os.Stat(old); err == nil {
						os.Rename(old, newBase+suf)
					}
				}
			}

			c.JSON(http.StatusOK, gin.H{"ok": true})
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	})

	router.GET("/api/conversions", func(c *gin.Context) {
		c.JSON(http.StatusOK, server.GetConversions())
	})

	// All video files (mp4/mkv/mov) for the manage view
	router.GET("/api/manage/list", func(c *gin.Context) {
		merged := make(map[string][]string)
		for _, d := range videosRoots {
			entries, err := os.ReadDir(d)
			if err != nil {
				continue
			}
			allVids := func(dir string) []string {
				var out []string
				e, _ := os.ReadDir(dir)
				for _, f := range e {
					if f.IsDir() || strings.HasPrefix(f.Name(), ".") {
						continue
					}
					ext := strings.ToLower(filepath.Ext(f.Name()))
					if ext == ".mp4" || ext == ".mkv" || ext == ".mov" {
						out = append(out, f.Name())
					}
				}
				return out
			}
			if files := allVids(d); len(files) > 0 {
				merged["."] = append(merged["."], files...)
			}
			for _, entry := range entries {
				if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
					if files := allVids(filepath.Join(d, entry.Name())); len(files) > 0 {
						merged[entry.Name()] = append(merged[entry.Name()], files...)
					}
				}
			}
		}
		c.JSON(http.StatusOK, merged)
	})

	// Disk space info per root
	router.GET("/api/manage/diskinfo", func(c *gin.Context) {
		type DiskInfo struct {
			Root  string `json:"root"`
			Free  uint64 `json:"free"`
			Total uint64 `json:"total"`
		}
		var infos []DiskInfo
		for _, root := range videosRoots {
			var stat syscall.Statfs_t
			if err := syscall.Statfs(root, &stat); err != nil {
				continue
			}
			infos = append(infos, DiskInfo{
				Root:  filepath.Base(root),
				Free:  stat.Bavail * uint64(stat.Bsize),
				Total: stat.Blocks * uint64(stat.Bsize),
			})
		}
		c.JSON(http.StatusOK, infos)
	})

	// Delete an entire folder (top-level dirs only)
	router.DELETE("/api/dir", func(c *gin.Context) {
		path := c.Query("path")
		if path == "" || strings.ContainsAny(path, "/\\") || path == "." {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
			return
		}
		for _, root := range videosRoots {
			absRoot, _ := filepath.Abs(root)
			candidate := filepath.Join(root, path)
			absCandidate, err := filepath.Abs(candidate)
			if err != nil || !strings.HasPrefix(absCandidate, absRoot) {
				continue
			}
			info, err := os.Stat(candidate)
			if err != nil || !info.IsDir() {
				continue
			}
			if err := os.RemoveAll(candidate); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"ok": true})
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	})

	// Subtitle file serving — wildcard must be alone under /subs/
	router.GET("/subs/*filename", func(c *gin.Context) { server.SubsSend(c, videosRoots) })

	// Subtitle API — separate prefix avoids wildcard conflict
	router.GET("/api/subs/info", func(c *gin.Context) { server.SubsInfo(c, videosRoots) })
	router.GET("/api/subs/search", func(c *gin.Context) { server.SubsSearch(c, videosRoots) })
	router.POST("/api/subs/download", func(c *gin.Context) { server.SubsDownload(c, videosRoots) })
	router.POST("/api/subs/extract", func(c *gin.Context) { server.SubsExtract(c, videosRoots) })
	router.POST("/api/subs/whisper", func(c *gin.Context) { server.SubsWhisper(c, videosRoots) })
	router.GET("/api/subs/whisper/status", server.SubsWhisperStatus)

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

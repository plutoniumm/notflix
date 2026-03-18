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
	imgDir = "./images"
	pubDir = "./public"
	assDir = "./public/assets"
	port   = "4242"
)

var roots = []string{
	"/Volumes/Ravan",
	"/Volumes/Oni",
	"/Volumes/Kumbhakarn",
}

func pulse() {
	if rand.Float64() < 0.01 {
		for _, root := range roots {
			go server.GenerateThumbnails(root)
		}
	}
}

func findRoot(name string, rts []string) (string, string, bool) {
	rel := strings.TrimPrefix(name, "/")
	for _, r := range rts {
		absR, _ := filepath.Abs(r)
		candidate := filepath.Join(r, rel)
		abs, err := filepath.Abs(candidate)

		if err != nil {
			continue
		}
		if !strings.HasPrefix(abs, absR) {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return r, abs, true
		}
	}

	return "", "", false
}

func buildList(dir string) map[string][]map[string]string {
	res := make(map[string][]map[string]string)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return res
	}

	procDir := func(sub string) []map[string]string {
		files := server.GetVids(sub)
		var out []map[string]string
		for _, name := range files {
			out = append(out, map[string]string{"name": name, "key": server.Hash(name)})
		}

		return out
	}

	res["."] = procDir(dir)

	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			res[e.Name()] = procDir(filepath.Join(dir, e.Name()))
		}
	}

	return res
}

func listMedia(c *gin.Context, dir string) {
	pulse()
	c.JSON(http.StatusOK, buildList(dir))
}

func listAll(c *gin.Context, dirs []string) {
	pulse()
	out := make(map[string][]map[string]string)
	for _, d := range dirs {
		for k, v := range buildList(d) {
			out[k] = append(out[k], v...)
		}
	}

	c.JSON(http.StatusOK, out)
}

func main() {
	_ = godotenv.Load()

	for _, root := range roots {
		server.EnsureDir(root)
	}
	server.EnsureDir(pubDir)
	server.EnsureDir(assDir)
	server.EnsureDir("./cache")

	go func() {
		server.ConvertAll(roots)
		server.SubAll(roots)
		server.CleanAll(roots)
	}()
	go server.Aria2Init()

	gin.SetMode("release")
	router := gin.New()
	router.Use(gin.Recovery())

	router.Use(func(c *gin.Context) {
		c.Header("Content-Security-Policy", "")
		c.Next()
	})

	router.GET("/", func(c *gin.Context) { pulse(); c.File("index.html") })
	router.GET("/manage", func(c *gin.Context) { c.File("index.html") })
	router.StaticFile("/manifest.json", "./public/manifest.json")
	router.StaticFile("/sw.js", "./public/sw.js")
	router.Static("/assets", assDir)

	router.GET("/list/video", func(c *gin.Context) { listAll(c, roots) })

	router.GET("/video/*filename", func(c *gin.Context) {
		pulse()
		fname := c.Param("filename")
		root, _, ok := findRoot(fname, roots)
		if !ok {
			c.String(http.StatusNotFound, "Video not found")
			return
		}
		server.VideoPlayer(c, root)
	})

	router.HEAD("/video/*filename", func(c *gin.Context) {
		fname := c.Param("filename")
		root, _, ok := findRoot(fname, roots)
		if !ok {
			c.Status(http.StatusNotFound)
			return
		}
		server.VideoHead(c, root)
	})

	router.GET("/images/:filename", func(c *gin.Context) {
		pulse()
		c.File(filepath.Join(imgDir, c.Param("filename")))
	})

	router.DELETE("/video/*filename", func(c *gin.Context) {
		fname := c.Param("filename")
		rel := strings.TrimPrefix(fname, "/")
		base := strings.TrimSuffix(rel, filepath.Ext(rel))
		for _, root := range roots {
			server.DelFile(filepath.Join(root, rel))
			for _, suf := range []string{".srt", ".vtt", ".whisper.vtt"} {
				server.DelFile(filepath.Join(root, base+suf))
			}
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
		for _, root := range roots {
			absR, _ := filepath.Abs(root)
			candidate := filepath.Join(root, rel)
			abs, err := filepath.Abs(candidate)
			if err != nil || !strings.HasPrefix(abs, absR) {
				continue
			}
			if _, err := os.Stat(candidate); err != nil {
				continue
			}

			dst := filepath.Join(filepath.Dir(candidate), body.NewName)
			if err := os.Rename(candidate, dst); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			ext := strings.ToLower(filepath.Ext(candidate))
			if ext == ".mp4" {
				base := candidate[:len(candidate)-len(ext)]
				nbase := dst[:len(dst)-len(filepath.Ext(dst))]

				for _, suf := range []string{".vtt", ".whisper.vtt", ".srt"} {
					old := base + suf
					if _, err := os.Stat(old); err == nil {
						os.Rename(old, nbase+suf)
					}
				}
			}

			c.JSON(http.StatusOK, gin.H{"ok": true})
			return
		}

		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	})

	router.GET("/api/conversions", func(c *gin.Context) {
		c.JSON(http.StatusOK, server.GetProgress())
	})

	router.GET("/api/manage/list", func(c *gin.Context) {
		out := make(map[string][]string)
		listVids := func(dir string) []string {
			var files []string
			entries, _ := os.ReadDir(dir)

			for _, f := range entries {
				if f.IsDir() || strings.HasPrefix(f.Name(), ".") {
					continue
				}

				ext := strings.ToLower(filepath.Ext(f.Name()))
				if ext == ".mp4" || ext == ".mkv" || ext == ".mov" {
					files = append(files, f.Name())
				}
			}
			return files
		}

		for _, d := range roots {
			entries, err := os.ReadDir(d)
			if err != nil {
				continue
			}

			if files := listVids(d); len(files) > 0 {
				out["."] = append(out["."], files...)
			}

			for _, e := range entries {
				if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
					if files := listVids(filepath.Join(d, e.Name())); len(files) > 0 {
						out[e.Name()] = append(out[e.Name()], files...)
					}
				}
			}
		}
		c.JSON(http.StatusOK, out)
	})

	router.GET("/api/manage/diskinfo", func(c *gin.Context) {
		type DiskInfo struct {
			Root  string `json:"root"`
			Path  string `json:"path"`
			Free  uint64 `json:"free"`
			Total uint64 `json:"total"`
		}
		var out []DiskInfo

		for _, root := range roots {
			var stat syscall.Statfs_t
			if err := syscall.Statfs(root, &stat); err != nil {
				continue
			}

			out = append(out, DiskInfo{
				Root:  filepath.Base(root),
				Path:  root,
				Free:  stat.Bavail * uint64(stat.Bsize),
				Total: stat.Blocks * uint64(stat.Bsize),
			})
		}
		c.JSON(http.StatusOK, out)
	})

	router.POST("/api/aria2/add", func(c *gin.Context) { server.Aria2Add(c, roots) })
	router.GET("/api/aria2/list", server.Aria2List)
	router.DELETE("/api/aria2/remove", server.Aria2Remove)

	router.GET("/kv/get", server.KVGet)
	router.POST("/kv/set", server.KVSet)

	router.DELETE("/api/dir", func(c *gin.Context) {
		path := c.Query("path")
		if path == "" || strings.ContainsAny(path, "/\\") || path == "." {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
			return
		}

		for _, root := range roots {
			absR, _ := filepath.Abs(root)
			candidate := filepath.Join(root, path)
			abs, err := filepath.Abs(candidate)
			if err != nil || !strings.HasPrefix(abs, absR) {
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

	router.GET("/subs/*filename", func(c *gin.Context) { server.SubsSend(c, roots) })

	router.GET("/api/hls/master", func(c *gin.Context) { server.HLSMaster(c, roots) })
	router.GET("/api/hls/playlist", func(c *gin.Context) { server.HLSPlaylist(c, roots) })
	router.GET("/api/hls/segment", func(c *gin.Context) { server.HLSSegment(c, roots) })

	router.GET("/api/video/info", func(c *gin.Context) { server.VideoInfo(c, roots) })
	router.GET("/api/subs/info", func(c *gin.Context) { server.Subctx(c, roots) })
	router.GET("/api/subs/search", func(c *gin.Context) { server.SubsSearch(c, roots) })
	router.POST("/api/subs/download", func(c *gin.Context) { server.GetSubs(c, roots) })
	router.POST("/api/subs/extract", func(c *gin.Context) { server.SubsExtract(c, roots) })
	router.POST("/api/subs/whisper", func(c *gin.Context) { server.SubsWhisper(c, roots) })
	router.GET("/api/subs/whisper/status", server.SubsWhisperStatus)
	router.GET("/api/subs/whisper/stream", func(c *gin.Context) { server.SubsWhisperStream(c, roots) })

	log.Printf("Starting server on http://localhost:%s", port)

	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			ipnert, ok := addr.(*net.IPNet)
			if !ok || ipnert.IP.IsLoopback() || ipnert.IP.To4() == nil {
				continue
			}
			log.Printf("LAN URL: http://%s:%s", ipnert.IP.String(), port)
		}
	}

	if err := router.Run(":" + port); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}

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
	"time"

	"notflix/server/jobs"
	"notflix/server/library"
	"notflix/server/media"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

var buildTime = "dev"

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
			go media.GenerateThumbnails(root)
		}
	}
}

func findRoot(name string, rts []string) (string, string, bool) {
	abs := library.FindFile(name, rts)
	if abs == "" {
		return "", "", false
	}

	rel := strings.TrimPrefix(name, "/")
	for _, r := range rts {
		if strings.HasPrefix(abs, r) || strings.HasPrefix(abs, filepath.Join(r, rel)) {
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
		files := library.GetVids(sub)
		var out []map[string]string
		for _, name := range files {
			out = append(out, map[string]string{"name": name, "key": library.Hash(name)})
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

func listAll(c *gin.Context, dirs []string) {
	pulse()
	hidden := library.HiddenDirs()
	out := make(map[string][]map[string]string)

	for _, d := range dirs {
		for k, v := range buildList(d) {
			if hidden[k] {
				continue
			}
			out[k] = append(out[k], v...)
		}
	}

	c.JSON(http.StatusOK, out)
}

func main() {
	_ = godotenv.Load()

	for _, root := range roots {
		library.EnsureDir(root)
	}

	library.EnsureDir(pubDir)
	library.EnsureDir(assDir)
	library.EnsureDir("./cache")

	jobs.OnDownloads = media.ProcessAll

	go media.ProcessAll(roots)
	go jobs.Aria2Init(roots)
	media.StartCacheCleanLoop(roots, time.Hour)

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

		media.VideoPlayer(c, root)
	})

	router.HEAD("/video/*filename", func(c *gin.Context) {
		fname := c.Param("filename")
		root, _, ok := findRoot(fname, roots)
		if !ok {
			c.Status(http.StatusNotFound)
			return
		}

		media.VideoHead(c, root)
	})

	router.GET("/images/:filename", func(c *gin.Context) {
		path := filepath.Join(imgDir, c.Param("filename"))
		if _, err := os.Stat(path); err != nil {
			go media.RegenerateThumbnails(roots, 60*time.Second)
			c.Status(http.StatusNotFound)
			return
		}

		c.File(path)
	})

	router.DELETE("/video/*filename", func(c *gin.Context) {
		fname := c.Param("filename")
		rel := strings.TrimPrefix(fname, "/")
		base := strings.TrimSuffix(rel, filepath.Ext(rel))

		for _, root := range roots {
			library.DelFile(filepath.Join(root, rel))
			for _, suf := range []string{".vtt", ".srt"} {
				library.DelFile(filepath.Join(root, base+suf))
			}
			matches, _ := filepath.Glob(filepath.Join(root, base+".*.vtt"))
			for _, m := range matches {
				library.DelFile(m)
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

		abs := library.FindFile(body.Path, roots)
		if abs != "" {
			dst := filepath.Join(filepath.Dir(abs), body.NewName)
			if err := os.Rename(abs, dst); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			ext := strings.ToLower(filepath.Ext(abs))
			if ext == ".mp4" {
				base := abs[:len(abs)-len(ext)]
				nbase := dst[:len(dst)-len(filepath.Ext(dst))]

				for _, suf := range []string{".vtt", ".srt"} {
					old := base + suf
					if _, err := os.Stat(old); err == nil {
						os.Rename(old, nbase+suf)
					}
				}

				matches, _ := filepath.Glob(base + ".*.vtt")
				for _, old := range matches {
					suffix := strings.TrimPrefix(old, base)
					os.Rename(old, nbase+suffix)
				}
			}

			c.JSON(http.StatusOK, gin.H{"ok": true})
			return
		}

		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	})

	router.GET("/api/conversions", func(c *gin.Context) {
		c.JSON(http.StatusOK, media.GetProgress())
	})

	router.POST("/api/process", func(c *gin.Context) {
		if media.IsProcessing() {
			c.JSON(http.StatusOK, gin.H{"ok": true, "status": "already running"})
			return
		}

		go media.ProcessAll(roots)
		c.JSON(http.StatusOK, gin.H{"ok": true, "status": "started"})
	})

	router.GET("/api/manage/list", func(c *gin.Context) {
		type FileEntry struct {
			Name string `json:"name"`
			Root string `json:"root"`
		}
		out := make(map[string][]FileEntry)

		listVids := func(dir, root string) []FileEntry {
			var files []FileEntry
			entries, _ := os.ReadDir(dir)

			for _, f := range entries {
				if f.IsDir() || strings.HasPrefix(f.Name(), ".") {
					continue
				}

				ext := strings.ToLower(filepath.Ext(f.Name()))
				if ext == ".mp4" || ext == ".mkv" || ext == ".mov" {
					files = append(files, FileEntry{Name: f.Name(), Root: root})
				}
			}

			return files
		}

		for _, d := range roots {
			entries, err := os.ReadDir(d)
			if err != nil {
				continue
			}

			rootBase := filepath.Base(d)

			if files := listVids(d, rootBase); len(files) > 0 {
				out["."] = append(out["."], files...)
			}

			for _, e := range entries {
				if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
					if files := listVids(filepath.Join(d, e.Name()), rootBase); len(files) > 0 {
						out[e.Name()] = append(out[e.Name()], files...)
					}
				}
			}
		}

		c.JSON(http.StatusOK, out)
	})

	router.GET("/api/build", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"backend": buildTime})
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

	router.GET("/api/manage/dirsizes", func(c *gin.Context) {
		type DirSize struct {
			Dir   string `json:"dir"`
			Bytes int64  `json:"bytes"`
			Root  string `json:"root"`
		}
		var out []DirSize

		for _, root := range roots {
			entries, err := os.ReadDir(root)
			if err != nil {
				continue
			}

			rootBase := filepath.Base(root)
			for _, e := range entries {
				if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
					continue
				}

				var total int64
				filepath.WalkDir(filepath.Join(root, e.Name()), func(_ string, d os.DirEntry, err error) error {
					if err != nil || d.IsDir() {
						return nil
					}
					if info, err := d.Info(); err == nil {
						total += info.Size()
					}
					return nil
				})

				if total > 0 {
					out = append(out, DirSize{Dir: e.Name(), Bytes: total, Root: rootBase})
				}
			}
		}

		c.JSON(http.StatusOK, out)
	})

	router.POST("/api/aria2/add", func(c *gin.Context) { jobs.Aria2Add(c, roots) })
	router.POST("/api/aria2/add-torrent", func(c *gin.Context) { jobs.Aria2AddTorrent(c, roots) })
	router.GET("/api/aria2/list", jobs.Aria2List)
	router.POST("/api/aria2/pause", jobs.Aria2Pause)
	router.POST("/api/aria2/resume", jobs.Aria2Resume)
	router.DELETE("/api/aria2/remove", jobs.Aria2Remove)

	router.GET("/kv/get", library.KVGet)
	router.POST("/kv/set", library.KVSet)
	router.GET("/api/manage/hidden", library.HiddenList)

	router.DELETE("/api/dir", func(c *gin.Context) {
		path := c.Query("path")
		if path == "" || strings.ContainsAny(path, "/\\") || path == "." {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
			return
		}

		abs := library.FindFile(path, roots)
		if abs == "" {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}

		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}

		if err := os.RemoveAll(abs); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	router.GET("/subs/*filename", func(c *gin.Context) { media.SubsSend(c, roots) })

	router.GET("/api/hls/master", func(c *gin.Context) { media.HLSMaster(c, roots) })
	router.GET("/api/hls/playlist", func(c *gin.Context) { media.HLSPlaylist(c, roots) })
	router.GET("/api/hls/segment", func(c *gin.Context) { media.HLSSegment(c, roots) })
	router.GET("/api/hls/avoffset", func(c *gin.Context) { media.HLSAVOffset(c, roots) })

	router.GET("/api/video/info", func(c *gin.Context) { media.VideoInfo(c, roots) })
	router.GET("/api/audio/info", func(c *gin.Context) { media.AudioInfo(c, roots) })
	router.GET("/api/subs/info", func(c *gin.Context) { media.Subctx(c, roots) })
	router.GET("/api/subs/search", func(c *gin.Context) { media.SubsSearch(c, roots) })
	router.POST("/api/subs/download", func(c *gin.Context) { media.GetSubs(c, roots) })
	router.POST("/api/subs/extract", func(c *gin.Context) { media.SubsExtract(c, roots) })
	router.POST("/api/subs/whisper", func(c *gin.Context) { jobs.SubsWhisper(c, roots) })
	router.GET("/api/subs/whisper/status", jobs.SubsWhisperStatus)
	router.GET("/api/subs/whisper/stream", func(c *gin.Context) { jobs.SubsWhisperStream(c, roots) })

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

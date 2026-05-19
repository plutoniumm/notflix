package main

import (
	"log"
	"math/rand"
	"net"
	"net/http"
	"time"

	"notflix/server/jobs"
	"notflix/server/library"
	"notflix/server/media"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

var buildTime = "dev"

const (
	pubDir = "./public"
	assDir = "./public/assets"
	port   = "4242"
)

var lib = &library.Library{
	Roots: []string{
		"/Volumes/Ravan",
		"/Volumes/Oni",
		"/Volumes/Kumbhakarn",
	},
}

func pulse() {
	if rand.Float64() < 0.01 {
		for _, root := range lib.Roots {
			go media.GenerateThumbnails(root)
		}
	}
}

func main() {
	_ = godotenv.Load()

	for _, root := range lib.Roots {
		library.EnsureDir(root)
	}

	library.EnsureDir(pubDir)
	library.EnsureDir(assDir)
	library.EnsureDir("./cache")

	jobs.OnDownloads = media.ProcessAll

	go media.ProcessAll(lib) // runs ScanCorrupt internally after Convert+Clean
	go media.NormalizeSubs(lib)
	go jobs.Aria2Init(lib)
	media.StartCacheCleanLoop(lib, time.Hour)

	gin.SetMode("release")
	router := gin.New()
	router.Use(gin.Recovery())

	router.Use(func(c *gin.Context) {
		c.Header("Content-Security-Policy", "")
		c.Next()
	})

	router.GET("/", func(c *gin.Context) { pulse(); c.File("index.html") })
	router.GET("/manage", func(c *gin.Context) { c.File("index.html") })
	router.GET("/search", func(c *gin.Context) { c.File("index.html") })
	router.StaticFile("/manifest.json", "./public/manifest.json")
	router.StaticFile("/sw.js", "./public/sw.js")
	router.Static("/assets", assDir)

	router.GET("/list/video", func(c *gin.Context) { pulse(); library.VideoList(c, lib) })
	router.GET("/video/*filename", func(c *gin.Context) { pulse(); media.VideoServe(c, lib) })
	router.HEAD("/video/*filename", func(c *gin.Context) { media.VideoHeadServe(c, lib) })
	router.GET("/images/:filename", func(c *gin.Context) { media.Thumbnail(c, lib) })
	router.DELETE("/video/*filename", func(c *gin.Context) { library.VideoDelete(c, lib) })

	router.POST("/api/rename", func(c *gin.Context) { library.Rename(c, lib) })
	router.GET("/api/conversions", media.Conversions)
	router.POST("/api/process", func(c *gin.Context) { media.ProcessStart(c, lib) })

	router.GET("/api/manage/list", func(c *gin.Context) { library.ManageList(c, lib) })
	router.GET("/api/manage/diskinfo", func(c *gin.Context) { library.DiskInfo(c, lib) })
	router.GET("/api/manage/dirsizes", func(c *gin.Context) { library.DirSizes(c, lib) })
	router.GET("/api/manage/hidden", library.HiddenList)

	router.GET("/api/build", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"backend": buildTime})
	})

	router.POST("/api/aria2/add", func(c *gin.Context) { jobs.Aria2Add(c, lib) })
	router.POST("/api/aria2/add-torrent", func(c *gin.Context) { jobs.Aria2AddTorrent(c, lib) })
	router.GET("/api/search", jobs.TPBSearch)
	router.GET("/api/aria2/list", jobs.Aria2List)
	router.POST("/api/aria2/pause", jobs.Aria2Pause)
	router.POST("/api/aria2/resume", jobs.Aria2Resume)
	router.DELETE("/api/aria2/remove", jobs.Aria2Remove)

	router.GET("/kv/get", library.KVGet)
	router.POST("/kv/set", library.KVSet)

	router.DELETE("/api/dir", func(c *gin.Context) { library.DirDelete(c, lib) })

	router.GET("/subs/*filename", func(c *gin.Context) { media.SubsSend(c, lib) })

	router.GET("/api/hls/master", func(c *gin.Context) { media.HLSMaster(c, lib) })
	router.GET("/api/hls/playlist", func(c *gin.Context) { media.HLSPlaylist(c, lib) })
	router.GET("/api/hls/segment", func(c *gin.Context) { media.HLSSegment(c, lib) })
	router.GET("/api/hls/init", func(c *gin.Context) { media.HLSInit(c, lib) })
	router.GET("/api/hls/avoffset", func(c *gin.Context) { media.HLSAVOffset(c, lib) })

	router.GET("/api/audio/info", func(c *gin.Context) { media.AudioInfo(c, lib) })
	router.GET("/api/subs/info", func(c *gin.Context) { media.Subctx(c, lib) })
	router.GET("/api/subs/search", func(c *gin.Context) { media.SubsSearch(c, lib) })
	router.POST("/api/subs/download", func(c *gin.Context) { media.GetSubs(c, lib) })
	router.POST("/api/subs/extract", func(c *gin.Context) { media.SubsExtract(c, lib) })
	router.POST("/api/subs/whisper", func(c *gin.Context) { jobs.SubsWhisper(c, lib) })
	router.GET("/api/subs/whisper/status", jobs.SubsWhisperStatus)
	router.GET("/api/subs/whisper/stream", func(c *gin.Context) { jobs.SubsWhisperStream(c, lib) })

	log.Printf("Starting server on http://localhost:%s", port)

	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok || ipnet.IP.IsLoopback() || ipnet.IP.To4() == nil {
				continue
			}
			log.Printf("LAN URL: http://%s:%s", ipnet.IP.String(), port)
		}
	}

	if err := router.Run(":" + port); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}

package main

import (
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
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
	pubDir = "./public"
	assDir = "./public/assets"
	port   = "4242"
)

// "~/…" roots are auto-created; any other absolute path must pre-exist —
// creating a dir at a mountpoint would silently shadow the unmounted volume.
var configuredRoots = []string{
	"~/Videos",
	"/Volumes/Aomine",
}

// rootCaps is a UI-only soft quota — writes aren't enforced; it only shapes
// what the UI budgets against for roots sharing the boot disk.
var rootCaps = map[string]uint64{
	"~/Videos": 64 << 30,
}

var lib = &library.Library{}

func resolveRoot(raw, home string, homeOK bool) (string, bool) {
	if strings.HasPrefix(raw, "~/") {
		if !homeOK {
			return "", false
		}
		return filepath.Join(home, raw[2:]), true
	}
	if raw == "~" {
		if !homeOK {
			return "", false
		}
		return home, true
	}
	return raw, true
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

	// Every step below is non-fatal: boot must survive missing drives.
	lib.Caps = map[string]uint64{}

	home, homeErr := os.UserHomeDir()
	if homeErr != nil {
		log.Printf("warn: cannot resolve home dir: %v — ~ roots will be skipped", homeErr)
	}

	for _, raw := range configuredRoots {
		abs, ok := resolveRoot(raw, home, homeErr == nil)
		if !ok {
			continue
		}

		isManaged := strings.HasPrefix(raw, "~")
		if isManaged {
			if !library.EnsureDir(abs) {
				log.Printf("skipping %s — cannot create or access (permission?)", abs)
				continue
			}
		} else {
			info, err := os.Stat(abs)
			if err != nil {
				log.Printf("skipping %s — %v", abs, err)
				continue
			}
			if !info.IsDir() {
				log.Printf("skipping %s — not a directory", abs)
				continue
			}
		}

		lib.Roots = append(lib.Roots, abs)
		if cap, ok := rootCaps[raw]; ok {
			lib.Caps[abs] = cap
		}
	}

	if len(lib.Roots) == 0 {
		log.Printf("warn: no library roots are reachable; server will still serve UI but library will be empty")
	}

	library.EnsureDir(pubDir)
	library.EnsureDir(assDir)
	library.EnsureDir("./cache")

	jobs.OnDownloads = media.ProcessAll
	// library can't import jobs (cycle), so Device mirrors jobs.Identity.
	library.IdentityFromIP = func(ip string) library.Device {
		i := jobs.TailscaleIdentity(ip)
		return library.Device{
			User: i.User, Device: i.Device, Host: i.Host,
			OS: i.OS, Login: i.Login, Tags: i.Tags, Online: i.Online,
		}
	}
	jobs.TailscaleInit()

	library.IsTorrentBusy = jobs.Aria2Owns

	recovered := library.PipelineInit()
	for _, r := range recovered {
		// Convert's <dst>.tmp is a truncated partial after a crash — delete so
		// the redo starts clean. No other step leaks a .tmp sibling.
		if r.Step == "convert" {
			base := strings.TrimSuffix(r.Path, filepath.Ext(r.Path))
			for _, sfx := range []string{".mp4.tmp", ".audio.tmp", ".audio.tmp.mp4"} {
				if err := os.Remove(base + sfx); err == nil {
					log.Printf("[pipeline] cleaned stale %s%s", filepath.Base(base), sfx)
				}
			}
		}
	}

	media.LogEncoders()
	go media.ProcessAll(lib)
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
	router.GET("/api/process/status", media.ProcessStatusHandler)
	router.GET("/api/processing", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"processing": media.IsProcessing()})
	})
	router.GET("/api/sprite/:key", media.SpriteManifestHandler)
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
	router.GET("/api/search", jobs.TorrentSearch)
	router.GET("/api/blurbs", jobs.BlurbList)
	router.POST("/api/poster/redo", func(c *gin.Context) { media.PosterRedo(c, lib) })
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

	router.GET("/api/skip", func(c *gin.Context) { media.SkipInfo(c, lib) })

	router.POST("/api/organize/plan", func(c *gin.Context) { jobs.OrganizePlan(c, lib) })
	router.GET("/api/organize/status", jobs.OrganizeStatus)
	router.POST("/api/organize/warm", jobs.OrganizeWarm)
	router.POST("/api/organize/apply", func(c *gin.Context) { jobs.OrganizeApply(c, lib) })

	router.GET("/api/audio/info", func(c *gin.Context) { media.AudioInfo(c, lib) })
	router.GET("/api/subs/info", func(c *gin.Context) { media.Subctx(c, lib) })
	router.GET("/api/subs/search", func(c *gin.Context) { media.SubsSearch(c, lib) })
	router.POST("/api/subs/download", func(c *gin.Context) { media.GetSubs(c, lib) })
	router.POST("/api/subs/extract", func(c *gin.Context) { media.SubsExtract(c, lib) })
	router.POST("/api/subs/whisper", func(c *gin.Context) { jobs.SubsWhisper(c, lib) })
	router.GET("/api/subs/whisper/status", jobs.SubsWhisperStatus)
	router.GET("/api/subs/whisper/stream", func(c *gin.Context) { jobs.SubsWhisperStream(c, lib) })

	// Take the children down too: an orphaned ffmpeg keeps writing outputs the
	// next run then stomps them, and an orphaned aria2c squats :6800.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		log.Println("[shutdown] stopping subprocesses")
		jobs.Aria2Stop()
		jobs.YTDlpStopAll()
		library.Shutdown()
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()

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

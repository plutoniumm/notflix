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

// configuredRoots is the desired root set. Two flavours coexist:
//   - "~/…" paths: expanded via $HOME, auto-created on boot. The home
//     dir is presumed writable; if not, the root is skipped non-fatally.
//   - "/Volumes/…" paths: aspirational mounts. Never created (creating
//     a regular dir at a mountpoint would silently shadow the volume).
//     Skipped non-fatally if the drive isn't mounted.
//
// Any other absolute path is treated the same as a /Volumes path: must
// pre-exist or it's dropped.
var configuredRoots = []string{
	"~/Videos",
	"/Volumes/Aomine",
}

// rootCaps maps a (possibly-tilde'd) configured root to a UI-only soft
// quota in bytes. Cosmetic only — writes aren't enforced; the user can
// blow past the cap and the UI just stops budgeting. Use this for roots
// that share a filesystem with the boot disk (~/Videos), so the disk
// strip doesn't drown out the others by showing the full Mac SSD.
var rootCaps = map[string]uint64{
	"~/Videos": 64 << 30, // 64 GiB
}

var lib = &library.Library{}

// resolveRoot expands a configured root entry to an absolute path.
// "~/X" → "$HOME/X" (only if home was resolvable); other strings are
// passed through. Returns ok=false when a "~" entry can't be expanded
// because the home dir is unknown.
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

	// Build the active root set. Every step is non-fatal — boot must
	// survive missing drives, unwritable home dirs, or any other per-root
	// permission glitch so the server still answers requests against
	// whatever roots are actually reachable.
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

		// Tilde'd entries are ours to create; bare absolute paths must
		// pre-exist (creating them would shadow unmounted volumes or
		// require root permission we don't have).
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
	// Adapter: jobs.Identity → library.Device. Library can't import jobs
	// (would cycle), so it declares its own struct with the same shape.
	library.IdentityFromIP = func(ip string) library.Device {
		i := jobs.TailscaleIdentity(ip)
		return library.Device{
			User: i.User, Device: i.Device, Host: i.Host,
			OS: i.OS, Login: i.Login, Tags: i.Tags, Online: i.Online,
		}
	}
	jobs.TailscaleInit() // warm the IP→identity cache + keep it warm in bg

	library.IsTorrentBusy = jobs.Aria2Owns

	recovered := library.PipelineInit()
	for _, r := range recovered {
		// Convert writes to <dst>.tmp before an atomic rename. If it died
		// mid-run, the .tmp is a truncated partial — delete so the redo
		// starts clean. Sprite/thumb/whisper don't leak .tmp siblings; the
		// ledger reset alone is enough to trigger a redo on next sweep.
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

	// On TERM/INT, take the children down too: cancel every FFRun (ffmpeg)
	// and TERM aria2c. Orphaned encoders from past restarts kept writing
	// outputs the next server run then stomped (Bleach 001–010, 2026-07-29),
	// and an orphaned aria2c squats :6800 wedging every later spawn.
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

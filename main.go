package main

import (
	"log"
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
	"notflix/server/tv"
	"notflix/server/x/gz"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

var buildTime = "dev"

// One bus, shared by the :4242 router and the :4444 TV listener, so a desktop
// tab can drive the screen exactly like the phone does.
var tvBus = tv.New()

const (
	pubDir        = "./public"
	assDir        = "./public/assets"
	defaultPort   = "4242"
	defaultTVPort = "4444"
)

// "~/…" roots are auto-created; any other absolute path must pre-exist —
// creating a dir at a mountpoint would silently shadow the unmounted volume.
// Aomine first: every "first root" default reads off this order — DiskInfo and
// DirSizes both iterate it, and the download dir picker preselects disks[0].
// The big external drive is where downloads should land, not the capped boot
// root. Nothing takes Roots[0] as a semantic default, so order is cosmetic
// everywhere else.
var configuredRoots = []string{
	"/Volumes/Aomine",
	"~/Videos",
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

// pulse used to call media.GenerateThumbnails per root on 1% of navigation
// requests. That function has no CAS guard (unlike RegenerateThumbnails) and
// runs ffmpeg INLINE in its WalkDir, so several unbounded tree walks could
// overlap — fired by /list/video, which is the first thing Home fetches. The
// thumbs phase of ProcessAll and toMP4's per-file dispatch already cover the
// work, so this is a deletion, not a replacement.
func pulse() {}

// Ports are overridable so a second instance can be stood up beside the live
// one — testing the TV bus against the running server means every command fans
// out to the real screen in the living room, which is exactly as disruptive as
// it sounds.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return fallback
}

func main() {
	_ = godotenv.Load()

	port := envOr("PORT", defaultPort)
	tvPort := envOr("TV_PORT", defaultTVPort)

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
	router.Use(gz.Middleware())

	router.Use(func(c *gin.Context) {
		c.Header("Content-Security-Policy", "")
		c.Next()
	})

	router.GET("/", func(c *gin.Context) { pulse(); c.File("index.html") })
	router.GET("/manage", func(c *gin.Context) { c.File("index.html") })

	// The TV pages are also served from the MAIN router, not just the :4444
	// listener. The tailnet hostname proxies to :4242, so without these the
	// remote is reachable only by raw LAN IP and port — which is the one device
	// (a phone, away from the house) most likely to want the tailnet name.
	// Same bundle; it routes on pathname, and the bus is registered on both.
	tvShell := func(c *gin.Context) { c.File(filepath.Join(pubDir, "tv.html")) }
	router.GET("/rc", tvShell)
	router.GET("/tv", tvShell)
	router.GET("/history", func(c *gin.Context) { c.File("index.html") })
	router.StaticFile("/manifest.json", "./public/manifest.json")
	router.StaticFile("/sw.js", "./public/sw.js")
	router.Static("/assets", assDir)

	router.GET("/list/video", func(c *gin.Context) { pulse(); library.VideoList(c, lib) })
	// No pulse() here: this is the byte-range streaming path, and a browser
	// issues hundreds of range requests per title. pulse() spawns
	// GenerateThumbnails for EVERY root on 1% of calls — a full WalkDir with a
	// per-file IsBusy/IsCorrupt stat storm plus ffmpeg grabs, unguarded, so
	// several could overlap mid-playback. Exactly the "batch work must never
	// starve live streaming" rule. Thumbs are covered by ProcessAll's thumbs
	// phase and toMP4's per-file dispatch; the navigation routes still pulse.
	router.GET("/video/*filename", func(c *gin.Context) { media.VideoServe(c, lib) })
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
	router.GET("/api/history", func(c *gin.Context) { library.HistoryHandler(c, lib) })

	router.GET("/api/build", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"backend": buildTime})
	})

	router.POST("/api/aria2/add", func(c *gin.Context) { jobs.Aria2Add(c, lib) })
	router.POST("/api/aria2/add-torrent", func(c *gin.Context) { jobs.Aria2AddTorrent(c, lib) })
	router.GET("/api/search", jobs.TorrentSearch)
	router.GET("/api/blurbs", jobs.BlurbList)
	router.POST("/api/compat", media.CompatSet)
	router.GET("/api/compat", media.CompatList)
	router.GET("/api/anim", func(c *gin.Context) { c.JSON(http.StatusOK, media.AnimationList()) })
	router.POST("/api/poster/redo", func(c *gin.Context) { media.PosterRedo(c, lib) })
	router.GET("/api/aria2/list", jobs.Aria2List)
	router.POST("/api/aria2/pause", jobs.Aria2Pause)
	router.POST("/api/aria2/resume", jobs.Aria2Resume)
	router.DELETE("/api/aria2/remove", jobs.Aria2Remove)

	tvBus.Routes(router)

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
	router.GET("/api/frames", func(c *gin.Context) { media.Frames(c, lib) })
	router.GET("/api/frames/img", media.FrameImage)
	router.POST("/api/clip", func(c *gin.Context) { media.ClipStart(c, lib) })
	router.GET("/api/clip/status", media.ClipStatus)
	router.GET("/api/clip/file", media.ClipFile)
	router.GET("/api/reco", func(c *gin.Context) { jobs.RecoHandler(c, lib) })

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
	// Repairs .whisper.vtt sidecars whose translate step failed, without
	// redoing the ASR that didn't. Returns immediately; the 1-wide pool grinds.
	router.POST("/api/subs/retranslate", func(c *gin.Context) {
		go jobs.RetranslateAll(lib)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

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

	// The TV app is a SECOND LISTENER in this same process, not a second
	// service: it shares the library, the KV store and every media handler by
	// calling the same functions. A separate binary would have bought a proxy
	// hop, a CORS story and two caches of the same probe results.
	//
	// Its route set is deliberately tiny — a TV screen and a phone remote need
	// to list, play and seek, nothing else. Manage, organize, torrents and
	// downloads stay off :4444 entirely.
	go func() {
		t := gin.New()
		t.Use(gin.Recovery(), gz.Middleware())

		// Both pages are the same bundle; it routes on pathname.
		shell := func(c *gin.Context) { c.File(filepath.Join(pubDir, "tv.html")) }
		t.GET("/", shell)
		t.GET("/rc", shell)
		t.Static("/assets", assDir)

		t.GET("/list/video", func(c *gin.Context) { library.VideoList(c, lib) })
		t.GET("/video/*filename", func(c *gin.Context) { media.VideoServe(c, lib) })
		t.GET("/images/:filename", func(c *gin.Context) { media.Thumbnail(c, lib) })
		t.GET("/subs/*filename", func(c *gin.Context) { media.SubsSend(c, lib) })
		t.GET("/api/subs/info", func(c *gin.Context) { media.Subctx(c, lib) })
		t.GET("/api/tv/plan", func(c *gin.Context) { media.TVPlan(c, lib) })
		t.GET("/api/hls/master", func(c *gin.Context) { media.HLSMaster(c, lib) })
		t.GET("/api/hls/playlist", func(c *gin.Context) { media.HLSPlaylist(c, lib) })
		t.GET("/api/hls/segment", func(c *gin.Context) { media.HLSSegment(c, lib) })
		t.GET("/api/hls/init", func(c *gin.Context) { media.HLSInit(c, lib) })
		t.GET("/api/blurbs", jobs.BlurbList)
		t.GET("/kv/get", library.KVGet)
		t.POST("/kv/set", library.KVSet)

		tvBus.Routes(t)

		log.Printf("TV screen on http://localhost:%s  — remote on /rc", tvPort)
		if err := t.Run(":" + tvPort); err != nil {
			log.Printf("[tv] listener died: %v", err)
		}
	}()

	if err := router.Run(":" + port); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}

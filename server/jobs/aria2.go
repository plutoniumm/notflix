package jobs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"notflix/server/library"
	"notflix/server/x/httpc"
)

const aria2RPC = "http://localhost:6800/jsonrpc"

// A wedged aria2c accepts on :6800 but never answers: without a deadline the
// watchdog's own aria2.shutdown and the paths refresher hang on it too.
const rpcTimeout = 10 * time.Second

var rpcSeq atomic.Int64

func a2call(method string, params ...any) (json.RawMessage, error) {
	id := fmt.Sprintf("%d", rpcSeq.Add(1))
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}

	if len(params) > 0 {
		req["params"] = params
	}

	var out struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := httpc.PostJSON(context.Background(), aria2RPC, req, &out, rpcTimeout); err != nil {
		return nil, err
	}
	if out.Error != nil {
		return nil, fmt.Errorf("aria2 rpc %d: %s", out.Error.Code, out.Error.Message)
	}

	return out.Result, nil
}

const aria2Session = "./cache/aria2.session"

var (
	aria2Ready  = make(chan struct{})
	OnDownloads func(*library.Library)
)

func WaitAria2() {
	select {
	case <-aria2Ready:
	case <-time.After(15 * time.Second):
		log.Println("[aria2] timed out waiting for RPC, proceeding without it")
	}
}

var aria2StartedAt atomic.Int64

var aria2Proc atomic.Pointer[exec.Cmd]

// Aria2Stop TERMs the supervised aria2c (it saves its session on TERM). Must
// run on shutdown: an orphan keeps :6800, later spawns die, and new flags
// silently never apply until it's pkill'd.
func Aria2Stop() {
	cmd := aria2Proc.Load()
	if cmd == nil || cmd.Process == nil {
		return
	}
	// SIGINT asks aria2 to shut down gracefully, which it does from the same
	// command queue that serves RPC — and that queue is exactly what wedges
	// (seen 09-30: downloads still writing, RPC and the save-session timer both
	// dead for 14min). A graceful stop it can never run leaves the old process
	// squatting :6800, and then every later spawn silently gets no RPC at all.
	// Killing is safe: --continue=true resumes from the .aria2 control files.
	cmd.Process.Signal(os.Interrupt)
	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		// Signal(0) probes liveness. NOT cmd.Wait(): the supervisor goroutine
		// already owns it, and a second Wait returns instantly with "already
		// called", which would report exit and skip the kill every time.
		if cmd.Process.Signal(syscall.Signal(0)) != nil {
			return
		}
	}
	cmd.Process.Kill()
}

func Aria2Init(lib *library.Library) {
	go a2SpeedTracker()
	go a2PurgeLoop(lib)
	go a2WedgeWatchdog()
	go torrentPathsRefresher()

	var readyOnce sync.Once
	backoff := 2 * time.Second
	for {
		started := time.Now()
		args := []string{
			"--enable-rpc",
			"--rpc-listen-port=6800",
			"--seed-time=0",
			"--quiet=true",
			"--save-session=" + aria2Session,
			"--save-session-interval=60",
			"--continue=true",
			"--max-connection-per-server=16",
			"--split=16",
			"--min-split-size=1M",
			"--max-concurrent-downloads=20",
			"--optimize-concurrent-downloads=true",
			"--max-overall-download-limit=0",
			"--file-allocation=falloc",
			"--disk-cache=64M",
			// Not 0 (unlimited): 3 torrents held 244 established peers / 778 network
			// fds, and aria2 services peers and the RPC listener on ONE loop. 60 is
			// just above aria2's own default; raise only with the fd count in hand.
			"--bt-max-peers=60",
			"--bt-request-peer-speed-limit=50M",
			"--bt-max-open-files=1024",
			// Fixed so DHT NAT discovery sees one address across restarts; random
			// per-run ports left popular torrents at 0 peers indefinitely.
			"--listen-port=6881-6999",
			"--dht-listen-port=6881-6999",
			"--enable-dht=true",
			"--enable-dht6=true",
			"--enable-peer-exchange=true",
			"--bt-enable-lpd=true",
			// Persist the DHT table so restarts don't re-bootstrap from zero. NOT
			// ~/.aria2/dht.dat — a past sudo run leaves that root-owned, unreadable.
			"--dht-file-path=./cache/aria2.dht.dat",
			"--dht-file-path6=./cache/aria2.dht6.dat",
			// Reuse the .torrent written after metadata fetch across a restart.
			"--bt-save-metadata=true",
			"--bt-load-saved-metadata=true",
			// Fallback announce list for magnets sourced outside our own search.
			"--bt-tracker=" + strings.Join(tpbTrackers, ","),
		}
		if _, err := os.Stat(aria2Session); err == nil {
			args = append(args, "--input-file="+aria2Session)
		}
		cmd := exec.Command("aria2c", args...)
		if err := cmd.Start(); err != nil {
			log.Printf("[aria2] failed to start aria2c: %v", err)
			readyOnce.Do(func() { close(aria2Ready) })
			time.Sleep(backoff)
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		log.Printf("[aria2] started aria2c pid=%d", cmd.Process.Pid)
		aria2Proc.Store(cmd)
		aria2StartedAt.Store(time.Now().UnixNano())

		ready := false
		for range 30 {
			time.Sleep(300 * time.Millisecond)
			if _, err := a2call("aria2.getVersion"); err == nil {
				log.Println("[aria2] RPC ready")
				ready = true
				break
			}
		}
		// Warm the paths cache BEFORE unblocking WaitAria2: on an empty cache
		// IsBusy says no and ProcessAll grabs files aria2 is still downloading.
		if ready {
			refreshTorrentPaths()
		}
		readyOnce.Do(func() { close(aria2Ready) })
		if !ready {
			log.Println("[aria2] RPC never became ready")
		}

		// The session reload drags in stale stopped entries (e.g. vanished dir).
		if ready {
			if raw, err := a2call("aria2.tellStopped", 0, 200); err == nil {
				var items []struct {
					GID    string `json:"gid"`
					Status string `json:"status"`
				}
				if json.Unmarshal(raw, &items) == nil && len(items) > 0 {
					if _, err := a2call("aria2.purgeDownloadResult"); err == nil {
						_, _ = a2call("aria2.saveSession")
						log.Printf("[aria2] purged %d boot-time stopped entries", len(items))
					}
				}
			}
		}

		cmd.Wait()
		aria2StartedAt.Store(0)
		log.Printf("[aria2] aria2c exited")
		if time.Since(started) > 5*time.Minute {
			backoff = 2 * time.Second
		}
		time.Sleep(backoff)
		backoff = min(backoff*2, 30*time.Second)
	}
}

func a2SpeedTracker() {
	for {
		time.Sleep(2 * time.Second)
		raw, err := a2call("aria2.tellActive")
		if err != nil {
			continue
		}
		var act []a2StatusRaw
		if json.Unmarshal(raw, &act) != nil {
			continue
		}
		live := make(map[string]bool, len(act))
		for _, s := range act {
			recordSpeed(s.GID, scanInt(s.CompletedLength))
			live[s.GID] = true
		}
		pruneSpeed(live)
	}
}

func a2PurgeLoop(lib *library.Library) {
	seenActive := make(map[string]bool)
	for {
		time.Sleep(5 * time.Second)

		if raw, err := a2call("aria2.tellActive"); err == nil {
			var act []struct {
				GID string `json:"gid"`
			}
			if json.Unmarshal(raw, &act) == nil {
				for _, a := range act {
					seenActive[a.GID] = true
				}
			}
		}

		raw, err := a2call("aria2.tellStopped", 0, 50)
		if err != nil {
			continue
		}

		var items []struct {
			GID    string `json:"gid"`
			Status string `json:"status"`
		}
		if json.Unmarshal(raw, &items) != nil {
			continue
		}

		realComplete := false
		for _, it := range items {
			if it.Status == "complete" || it.Status == "error" || it.Status == "removed" {
				wasActive := seenActive[it.GID]
				log.Printf("[aria2] purging gid=%s status=%s observed-active=%v", it.GID, it.Status, wasActive)
				if _, err := a2call("aria2.removeDownloadResult", it.GID); err != nil {
					log.Printf("[aria2] purge error: %v", err)
				}
				delete(seenActive, it.GID)
				if wasActive && it.Status == "complete" {
					realComplete = true
				}
			}
		}
		if realComplete && OnDownloads != nil {
			go OnDownloads(lib)
		}
	}
}

// Heals the long-uptime wedge where every active torrent sits at 0 peers
// (expired NAT mappings): shut down, and Aria2Init respawns with DHT+session.
func a2WedgeWatchdog() {
	const (
		checkEvery = 60 * time.Second
		graceMin   = 10 * time.Minute
		wedgeMax   = 15 * time.Minute
	)
	var wedgeSince time.Time
	for {
		time.Sleep(checkEvery)
		startedNs := aria2StartedAt.Load()
		if startedNs == 0 {
			wedgeSince = time.Time{}
			continue
		}
		if time.Since(time.Unix(0, startedNs)) < graceMin {
			wedgeSince = time.Time{}
			continue
		}
		raw, err := a2call("aria2.tellActive")
		if err != nil {
			wedgeSince = time.Time{}
			continue
		}
		var act []struct {
			GID         string `json:"gid"`
			Connections string `json:"connections"`
		}
		if json.Unmarshal(raw, &act) != nil {
			continue
		}
		if len(act) == 0 {
			wedgeSince = time.Time{}
			continue
		}
		anyPeers := false
		for _, a := range act {
			if scanInt(a.Connections) > 0 {
				anyPeers = true
				break
			}
		}
		if anyPeers {
			wedgeSince = time.Time{}
			continue
		}
		if wedgeSince.IsZero() {
			wedgeSince = time.Now()
			log.Printf("[aria2] watchdog: %d actives all at 0 peers, arming wedge timer", len(act))
			continue
		}
		if time.Since(wedgeSince) < wedgeMax {
			continue
		}
		log.Printf("[aria2] watchdog: wedged %v — shutting aria2c down to force respawn", time.Since(wedgeSince).Round(time.Second))
		_, _ = a2call("aria2.shutdown")
		wedgeSince = time.Time{}
	}
}

type Downjob struct {
	GID     string  `json:"gid"`
	Name    string  `json:"name"`
	Status  string  `json:"status"`
	Total   int64   `json:"total"`
	Done    int64   `json:"done"`
	Percent float64 `json:"percent"`
	Speed   int64   `json:"speed"`
}

type a2StatusRaw struct {
	GID             string `json:"gid"`
	Status          string `json:"status"`
	TotalLength     string `json:"totalLength"`
	CompletedLength string `json:"completedLength"`
	DownloadSpeed   string `json:"downloadSpeed"`

	BitTorrent struct {
		Info struct {
			Name string `json:"name"`
		} `json:"info"`
	} `json:"bittorrent"`

	Files []struct {
		Path string `json:"path"`
	} `json:"files"`
}

func (s a2StatusRaw) toItem() Downjob {
	name := s.BitTorrent.Info.Name
	if name == "" && len(s.Files) > 0 {
		name = filepath.Base(s.Files[0].Path)
	}

	total := scanInt(s.TotalLength)
	done := scanInt(s.CompletedLength)
	var pct float64
	if total > 0 {
		pct = float64(done) / float64(total) * 100
	}

	return Downjob{
		GID:     s.GID,
		Name:    name,
		Status:  s.Status,
		Total:   total,
		Done:    done,
		Percent: pct,
		Speed:   avgSpeed(s.GID, scanInt(s.DownloadSpeed)),
	}
}

func scanInt(s string) int64 {
	var n int64
	fmt.Sscan(s, &n)
	return n
}

const speedWindow = 10 * time.Second

type speedSample struct {
	t    time.Time
	done int64
}

var (
	speedMu      sync.Mutex
	speedHistory = map[string][]speedSample{} // gid -> samples, oldest first
)

// Keeps the one sample before the window so it stays fully bracketed.
func recordSpeed(gid string, done int64) {
	now := time.Now()
	speedMu.Lock()
	defer speedMu.Unlock()

	s := append(speedHistory[gid], speedSample{now, done})
	cut := now.Add(-speedWindow)
	i := 0
	for i < len(s)-1 && s[i+1].t.Before(cut) {
		i++
	}
	speedHistory[gid] = s[i:]
}

// fallback is aria2's instantaneous figure, used until there's history.
func avgSpeed(gid string, fallback int64) int64 {
	speedMu.Lock()
	defer speedMu.Unlock()

	s := speedHistory[gid]
	if len(s) < 2 {
		return fallback
	}
	first, last := s[0], s[len(s)-1]
	dt := last.t.Sub(first.t).Seconds()
	db := last.done - first.done
	if dt <= 0 || db < 0 {
		return fallback
	}
	return int64(float64(db) / dt)
}

func pruneSpeed(live map[string]bool) {
	speedMu.Lock()
	defer speedMu.Unlock()
	for gid := range speedHistory {
		if !live[gid] {
			delete(speedHistory, gid)
		}
	}
}

func Aria2Add(c *gin.Context, lib *library.Library) {
	var body struct {
		Magnet string `json:"magnet"`
		Dir    string `json:"dir"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Magnet == "" {
		log.Printf("[aria2] add: bad body: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	abbrev := body.Magnet
	if len(abbrev) > 80 {
		abbrev = abbrev[:80] + "…"
	}
	log.Printf("[aria2] add: uri=%s dir=%q", abbrev, body.Dir)

	absDir, err := filepath.Abs(body.Dir)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid dir"})
		return
	}
	allowed := false
	for _, r := range lib.Roots {
		absR, _ := filepath.Abs(r)
		if absDir == absR {
			allowed = true
			break
		}
	}
	if !allowed {
		log.Printf("[aria2] add: dir %q not in roots", absDir)
		c.JSON(http.StatusBadRequest, gin.H{"error": "dir not allowed"})
		return
	}

	if IsYTCandidate(body.Magnet) {
		gid, err := YTDlpAdd(body.Magnet, absDir, lib)
		if err != nil {
			log.Printf("[aria2] add: yt-dlp start failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "gid": gid, "via": "yt-dlp"})
		return
	}

	raw, err := a2call("aria2.addUri", []string{body.Magnet}, map[string]string{"dir": absDir})
	if err != nil {
		log.Printf("[aria2] add: RPC error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var gid string
	if err := json.Unmarshal(raw, &gid); err != nil {
		log.Printf("[aria2] add: unmarshal gid: %v", err)
	}
	log.Printf("[aria2] add: ok gid=%s", gid)
	go refreshTorrentPaths()
	c.JSON(http.StatusOK, gin.H{"ok": true, "gid": gid})
}

func Aria2AddTorrent(c *gin.Context, lib *library.Library) {
	file, err := c.FormFile("torrent")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing torrent file"})
		return
	}
	dir := c.PostForm("dir")
	if dir == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing dir"})
		return
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid dir"})
		return
	}
	allowed := false
	for _, r := range lib.Roots {
		absR, _ := filepath.Abs(r)
		if absDir == absR {
			allowed = true
			break
		}
	}
	if !allowed {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dir not allowed"})
		return
	}

	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read file"})
		return
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read file"})
		return
	}

	b64 := base64.StdEncoding.EncodeToString(data)
	raw, err := a2call("aria2.addTorrent", b64, []string{}, map[string]string{"dir": absDir})
	if err != nil {
		log.Printf("[aria2] addTorrent: RPC error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var gid string
	json.Unmarshal(raw, &gid)
	log.Printf("[aria2] addTorrent: ok gid=%s", gid)
	go refreshTorrentPaths()
	c.JSON(http.StatusOK, gin.H{"ok": true, "gid": gid})
}

// Every path aria2 has in session: active + waiting + stopped-but-not-purged.
var (
	torrentPathsMu sync.RWMutex
	torrentPaths   = map[string]bool{}
)

func refreshTorrentPaths() {
	next := map[string]bool{}
	activeRaw, err1 := a2call("aria2.tellActive")
	waitRaw, err2 := a2call("aria2.tellWaiting", 0, 200)
	stopRaw, err3 := a2call("aria2.tellStopped", 0, 200)

	var active, waiting, stopped []a2StatusRaw
	if err1 == nil {
		json.Unmarshal(activeRaw, &active)
	}
	if err2 == nil {
		json.Unmarshal(waitRaw, &waiting)
	}
	if err3 == nil {
		json.Unmarshal(stopRaw, &stopped)
	}

	all := append(active, waiting...)
	all = append(all, stopped...)
	for _, s := range all {
		for _, f := range s.Files {
			if f.Path != "" {
				next[f.Path] = true
			}
		}
	}

	torrentPathsMu.Lock()
	torrentPaths = next
	torrentPathsMu.Unlock()
}

func torrentPathsRefresher() {
	for {
		time.Sleep(3 * time.Second)
		refreshTorrentPaths()
	}
}

// Hot path: library.IsBusy asks per file per background pass, so this stays a
// single locked lookup instead of copying the whole set.
func Aria2Owns(path string) bool {
	torrentPathsMu.RLock()
	defer torrentPathsMu.RUnlock()

	return torrentPaths[path]
}

// Snapshot, safe to iterate unlocked. Includes stopped-but-not-yet-purged
// paths, so a just-finished download stays exempt until the purge (~5s).
func Aria2ActivePaths() map[string]bool {
	torrentPathsMu.RLock()
	defer torrentPathsMu.RUnlock()
	out := make(map[string]bool, len(torrentPaths))
	for k := range torrentPaths {
		out[k] = true
	}
	return out
}

func Aria2List(c *gin.Context) {
	activeRaw, err1 := a2call("aria2.tellActive")
	waitRaw, err2 := a2call("aria2.tellWaiting", 0, 100)

	// Never swallow this into an empty list. aria2 can keep downloading while
	// its control plane wedges (RPC silent, save-session timer stopped) — and a
	// 200 with [] reads as "nothing is downloading", so the one symptom that
	// says "aria2 needs a kick" is the one we used to hide.
	if err1 != nil && err2 != nil {
		log.Printf("aria2 rpc unreachable: %v", err1)
		c.JSON(http.StatusBadGateway, gin.H{"error": "aria2 rpc unreachable: " + err1.Error()})
		return
	}

	var active, waiting []a2StatusRaw
	if err1 == nil {
		json.Unmarshal(activeRaw, &active)
	}
	if err2 == nil {
		json.Unmarshal(waitRaw, &waiting)
	}

	yt := YTDlpListItems()
	items := make([]Downjob, 0, len(active)+len(waiting)+len(yt))
	for _, s := range append(active, waiting...) {
		items = append(items, s.toItem())
	}
	items = append(items, yt...)

	c.JSON(http.StatusOK, items)
}

func Aria2Pause(c *gin.Context) {
	gid := c.Query("gid")
	if gid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing gid"})
		return
	}
	if IsYT(gid) {
		if err := YTDlpPause(gid); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	if _, err := a2call("aria2.pause", gid); err != nil {
		log.Printf("[aria2] pause gid=%s: %v", gid, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Printf("[aria2] pause gid=%s", gid)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func Aria2Resume(c *gin.Context) {
	gid := c.Query("gid")
	if gid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing gid"})
		return
	}
	if IsYT(gid) {
		if err := YTDlpResume(gid); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	// aria2 rejects unpause until a just-issued pause has fully transitioned
	// (the UI fires pause→resume back-to-back), so retry the transient.
	var err error
	for range 5 {
		if _, err = a2call("aria2.unpause", gid); err == nil {
			break
		}
		if !strings.Contains(err.Error(), "cannot be unpaused") {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if err != nil {
		log.Printf("[aria2] resume gid=%s: %v", gid, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Printf("[aria2] resume gid=%s", gid)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func Aria2Remove(c *gin.Context) {
	gid := c.Query("gid")
	if gid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing gid"})
		return
	}
	log.Printf("[aria2] remove: gid=%s", gid)

	if IsYT(gid) {
		YTDlpRemove(gid)
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	// Snapshot before removal — afterwards aria2 can't say what it left behind.
	cleanup := aria2OrphanFiles(gid)

	a2call("aria2.forceRemove", gid)          //nolint
	a2call("aria2.removeDownloadResult", gid) //nolint

	// forceRemove leaves the partial payload + .aria2 file behind; a user
	// remove means "make it go away". Skipped for completed downloads.
	cleanup()

	log.Printf("[aria2] remove: done")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Snapshot before removeDownloadResult, delete after. Only non-complete
// downloads, only enumerated files, dirs non-recursively — a folder shared
// with unrelated media must survive.
func aria2OrphanFiles(gid string) func() {
	noop := func() {}

	raw, err := a2call("aria2.tellStatus", gid, []string{"dir", "status", "files"})
	if err != nil {
		return noop
	}
	var st struct {
		Dir    string `json:"dir"`
		Status string `json:"status"`
		Files  []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if json.Unmarshal(raw, &st) != nil || st.Dir == "" || st.Status == "complete" {
		return noop
	}

	paths := make([]string, 0, len(st.Files))
	for _, f := range st.Files {
		if f.Path != "" {
			paths = append(paths, f.Path)
		}
	}

	files, tops := aria2CleanupTargets(st.Dir, paths)
	dirs := cleanupDirs(st.Dir, paths)

	return func() { removeDownload(files, dirs, tops) }
}

// Never RemoveAll: the download dir is shared, so only enumerated files go and
// os.Remove fails harmlessly on a dir still holding someone else's media.
func removeDownload(files, dirs, tops []string) {
	for _, p := range files {
		os.Remove(p)
		os.Remove(p + ".aria2")
	}

	for _, d := range dirs {
		os.Remove(d)
	}
	for _, t := range tops {
		os.Remove(t + ".aria2")
		os.Remove(t)
	}
}

// Deepest first, so the non-recursive sweep stops at the first dir that still
// holds something. Never root itself or anything outside it.
func cleanupDirs(dir string, paths []string) []string {
	root := filepath.Clean(dir)
	seen := map[string]bool{}

	var out []string
	for _, p := range paths {
		for d := filepath.Dir(filepath.Clean(p)); d != root; d = filepath.Dir(d) {
			rel, err := filepath.Rel(root, d)
			if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
				break
			}
			if !seen[d] {
				seen[d] = true
				out = append(out, d)
			}
		}
	}
	sep := string(filepath.Separator)
	sort.SliceStable(out, func(i, j int) bool {
		return strings.Count(out[i], sep) > strings.Count(out[j], sep)
	})

	return out
}

// The files plus each torrent's top-level folder under the download dir —
// never root itself or anything outside it.
func aria2CleanupTargets(dir string, paths []string) (files, tops []string) {
	root := filepath.Clean(dir)
	seen := map[string]struct{}{}
	for _, p := range paths {
		files = append(files, p)

		rel, err := filepath.Rel(root, filepath.Clean(p))
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		top := rel
		if i := strings.IndexRune(rel, filepath.Separator); i >= 0 {
			top = rel[:i]
		}
		if top == "" || top == "." || top == ".." {
			continue
		}
		full := filepath.Join(root, top)
		if _, ok := seen[full]; !ok {
			seen[full] = struct{}{}
			tops = append(tops, full)
		}
	}
	return files, tops
}

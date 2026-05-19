package jobs

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"notflix/server/library"
)

const aria2RPC = "http://localhost:6800/jsonrpc"

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

	body, _ := json.Marshal(req)
	resp, err := http.Post(aria2RPC, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	var out struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
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

func Aria2Init(lib *library.Library) {
	args := []string{
		"--enable-rpc", "--rpc-listen-all",
		"--rpc-listen-port=6800",
		"--seed-time=0",
		"--quiet=true",
		"--save-session=" + aria2Session,
		"--save-session-interval=60",
		"--continue=true",
		"--max-connection-per-server=16",
		"--split=16",
		"--min-split-size=1M",
		"--max-concurrent-downloads=5",
		"--optimize-concurrent-downloads=true",
		"--max-overall-download-limit=0",
		"--file-allocation=falloc",
		"--disk-cache=64M",
		"--bt-max-peers=0",
		"--bt-request-peer-speed-limit=50M",
		"--bt-max-open-files=256",
		"--enable-dht=true",
		"--enable-peer-exchange=true",
	}
	if _, err := os.Stat(aria2Session); err == nil {
		args = append(args, "--input-file="+aria2Session)
	}
	cmd := exec.Command("aria2c", args...)

	if err := cmd.Start(); err != nil {
		log.Printf("[aria2] failed to start aria2c: %v", err)
		close(aria2Ready)
		return
	}
	log.Printf("[aria2] started aria2c pid=%d", cmd.Process.Pid)

	ready := false
	for range 30 {
		time.Sleep(300 * time.Millisecond)
		if _, err := a2call("aria2.getVersion"); err == nil {
			log.Println("[aria2] RPC ready")
			ready = true
			break
		}
	}
	close(aria2Ready)
	if !ready {
		log.Println("[aria2] RPC never became ready")
	}

	go func() {
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
	}()

	go func() {
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
				if it.Status == "complete" {
					wasActive := seenActive[it.GID]
					log.Printf("[aria2] purging completed gid=%s observed-active=%v", it.GID, wasActive)
					if _, err := a2call("aria2.removeDownloadResult", it.GID); err != nil {
						log.Printf("[aria2] purge error: %v", err)
					}
					delete(seenActive, it.GID)
					if wasActive {
						realComplete = true
					}
				}
			}
			if realComplete && OnDownloads != nil {
				go OnDownloads(lib)
			}
		}
	}()

	cmd.Wait()
	log.Printf("[aria2] aria2c exited")
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

// recordSpeed appends a (now, completedLength) sample and trims everything
// before the trailing window, keeping the one bracketing sample so the
// window stays fully covered.
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

// avgSpeed returns bytes/s averaged over the sampled window, or fallback
// (aria2's instantaneous figure) when there isn't enough history yet.
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
	c.JSON(http.StatusOK, gin.H{"ok": true, "gid": gid})
}

func Aria2ActivePaths() map[string]bool {
	paths := map[string]bool{}
	activeRaw, err1 := a2call("aria2.tellActive")
	waitRaw, err2 := a2call("aria2.tellWaiting", 0, 100)

	var active, waiting []a2StatusRaw
	if err1 == nil {
		json.Unmarshal(activeRaw, &active)
	}
	if err2 == nil {
		json.Unmarshal(waitRaw, &waiting)
	}

	for _, s := range append(active, waiting...) {
		for _, f := range s.Files {
			if f.Path != "" {
				paths[f.Path] = true
			}
		}
	}
	return paths
}

func Aria2List(c *gin.Context) {
	activeRaw, err1 := a2call("aria2.tellActive")
	waitRaw, err2 := a2call("aria2.tellWaiting", 0, 100)

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
	if _, err := a2call("aria2.unpause", gid); err != nil {
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

	a2call("aria2.forceRemove", gid)          //nolint
	a2call("aria2.removeDownloadResult", gid) //nolint

	log.Printf("[aria2] remove: done")
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

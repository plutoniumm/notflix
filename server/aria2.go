package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
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

func Aria2Init() {
	cmd := exec.Command("aria2c",
		"--enable-rpc", "--rpc-listen-all",
		"--rpc-listen-port=6800",
		"--seed-time=0",
		"--quiet=true",
	)

	if err := cmd.Start(); err != nil {
		log.Printf("[aria2] failed to start aria2c: %v", err)
		return
	}
	log.Printf("[aria2] started aria2c pid=%d", cmd.Process.Pid)

	for i := 0; i < 30; i++ {
		time.Sleep(300 * time.Millisecond)
		if _, err := a2call("aria2.getVersion"); err == nil {
			log.Println("[aria2] RPC ready")
			break
		}
	}

	go func() {
		for {
			time.Sleep(5 * time.Second)
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

			for _, it := range items {
				if it.Status == "complete" {
					log.Printf("[aria2] purging completed gid=%s", it.GID)

					if _, err := a2call("aria2.removeDownloadResult", it.GID); err != nil {
						log.Printf("[aria2] purge error: %v", err)
					}
				}
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
		Speed:   scanInt(s.DownloadSpeed),
	}
}

func scanInt(s string) int64 {
	var n int64
	fmt.Sscan(s, &n)
	return n
}

func Aria2Add(c *gin.Context, roots []string) {
	var body struct {
		Magnet string `json:"magnet"`
		Dir    string `json:"dir"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Magnet == "" {
		log.Printf("[aria2] add: bad body: %v", err)
		c.JSON(400, gin.H{"error": "invalid body"})
		return
	}

	abbrev := body.Magnet
	if len(abbrev) > 80 {
		abbrev = abbrev[:80] + "…"
	}
	log.Printf("[aria2] add: magnet=%s dir=%q", abbrev, body.Dir)

	absDir, err := filepath.Abs(body.Dir)
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid dir"})
		return
	}
	allowed := false
	for _, r := range roots {
		absR, _ := filepath.Abs(r)
		if absDir == absR {
			allowed = true
			break
		}
	}
	if !allowed {
		log.Printf("[aria2] add: dir %q not in roots", absDir)
		c.JSON(400, gin.H{"error": "dir not allowed"})
		return
	}

	raw, err := a2call("aria2.addUri", []string{body.Magnet}, map[string]string{"dir": absDir})
	if err != nil {
		log.Printf("[aria2] add: RPC error: %v", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	var gid string
	json.Unmarshal(raw, &gid)
	log.Printf("[aria2] add: ok gid=%s", gid)
	c.JSON(200, gin.H{"ok": true, "gid": gid})
}

func Aria2List(c *gin.Context) {
	activeRaw, err1 := a2call("aria2.tellActive")
	waitRaw, err2 := a2call("aria2.tellWaiting", 0, 100)

	if err1 != nil && err2 != nil {
		c.JSON(200, []Downjob{})
		return
	}

	var active, waiting []a2StatusRaw
	if err1 == nil {
		json.Unmarshal(activeRaw, &active)
	}
	if err2 == nil {
		json.Unmarshal(waitRaw, &waiting)
	}

	items := make([]Downjob, 0, len(active)+len(waiting))
	for _, s := range append(active, waiting...) {
		items = append(items, s.toItem())
	}

	c.JSON(200, items)
}

func Aria2Remove(c *gin.Context) {
	gid := c.Query("gid")
	if gid == "" {
		c.JSON(400, gin.H{"error": "missing gid"})
		return
	}
	log.Printf("[aria2] remove: gid=%s", gid)

	a2call("aria2.forceRemove", gid)          //nolint
	a2call("aria2.removeDownloadResult", gid) //nolint

	log.Printf("[aria2] remove: done")
	c.JSON(200, gin.H{"ok": true})
}

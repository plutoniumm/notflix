package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"notflix/server/x/httpc"
)

// TPB's JSON backend. An empty result set comes back as a single sentinel row
// with id "0" / name "No results returned".
const apibayURL = "https://apibay.org/q.php"

// Baked into every magnet so new torrents find peers without waiting on DHT.
var tpbTrackers = []string{
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://open.stealth.si:80/announce",
	"udp://tracker.torrent.eu.org:451/announce",
	"udp://open.demonii.com:1337/announce",
	"udp://tracker.dler.org:6969/announce",
	"udp://exodus.desync.com:6969/announce",
	"udp://tracker.bittor.pw:1337/announce",
	"udp://opentracker.i2p.rocks:6969/announce",
}

var tpbCats = map[string]string{
	"101": "Audio", "102": "Audio", "103": "Audio", "104": "Audio",
	"199": "Audio", "201": "Movies", "202": "Movies DVDR", "203": "Music",
	"204": "Movie clips", "205": "TV", "206": "Handheld", "207": "HD Movies",
	"208": "HD TV", "209": "3D", "299": "Video", "301": "Software",
	"303": "Software", "401": "Games", "402": "Games", "403": "Games",
	"404": "Games", "406": "Games", "408": "Games", "601": "Other",
}

func catLabel(c string) string {
	if l, ok := tpbCats[c]; ok {
		return l
	}
	if strings.HasPrefix(c, "1") {
		return "Audio"
	}
	if strings.HasPrefix(c, "2") {
		return "Video"
	}
	return "Other"
}

type tpbRaw struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	InfoHash string `json:"info_hash"`
	Leechers string `json:"leechers"`
	Seeders  string `json:"seeders"`
	Size     string `json:"size"`
	NumFiles string `json:"num_files"`
	Added    string `json:"added"`
	Category string `json:"category"`
	Status   string `json:"status"`
}

type Torrent struct {
	Name     string `json:"name"`
	Magnet   string `json:"magnet"`
	InfoHash string `json:"infoHash"`
	Size     int64  `json:"size"`
	Seeders  int    `json:"seeders"`
	Leechers int    `json:"leechers"`
	Files    int    `json:"files"`
	Added    int64  `json:"added"`
	Category string `json:"category"`
	Status   string `json:"status"`
	Source   string `json:"source"`
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func magnetFor(name, infoHash string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "magnet:?xt=urn:btih:%s&dn=%s", infoHash, url.QueryEscape(name))
	for _, tr := range tpbTrackers {
		b.WriteString("&tr=")
		b.WriteString(url.QueryEscape(tr))
	}
	return b.String()
}

const searchTimeout = 10 * time.Second

// ctx is the caller's request context: a user who navigates away cancels the
// fan-out instead of leaving four upstream fetches running.
func searchFetch(ctx context.Context, method, u string, body []byte) (io.Reader, error) {
	var payload io.Reader
	if body != nil {
		payload = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, payload)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (notflix)")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	out, err := httpc.Send(req, searchTimeout)
	if err != nil {
		return nil, err
	}

	return bytes.NewReader(out), nil
}

func parseTPB(r io.Reader) ([]Torrent, error) {
	var raw []tpbRaw
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]Torrent, 0, len(raw))
	for _, t := range raw {
		if t.ID == "0" || t.InfoHash == "" {
			continue // "No results returned" sentinel
		}
		out = append(out, Torrent{
			Name:     t.Name,
			Magnet:   magnetFor(t.Name, t.InfoHash),
			InfoHash: t.InfoHash,
			Size:     int64(atoi(t.Size)),
			Seeders:  atoi(t.Seeders),
			Leechers: atoi(t.Leechers),
			Files:    atoi(t.NumFiles),
			Added:    int64(atoi(t.Added)),
			Category: catLabel(t.Category),
			Status:   t.Status,
			Source:   "tpb",
		})
	}
	return out, nil
}

func tpbSearch(ctx context.Context, q, cat string) ([]Torrent, error) {
	u := fmt.Sprintf("%s?q=%s&cat=%s", apibayURL, url.QueryEscape(q), url.QueryEscape(cat))
	body, err := searchFetch(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	return parseTPB(body)
}

// Dedupe by infohash keeping the higher-seeded entry; hashless entries pass
// through only if they already carry a magnet.
func mergeTorrents(lists ...[]Torrent) []Torrent {
	out := []Torrent{}
	seen := map[string]int{}
	for _, list := range lists {
		for _, t := range list {
			key := strings.ToLower(t.InfoHash)
			if key == "" {
				if t.Magnet != "" {
					out = append(out, t)
				}
				continue
			}
			if i, dup := seen[key]; dup {
				if t.Seeders > out[i].Seeders {
					out[i] = t
				}
				continue
			}
			seen[key] = len(out)
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seeders > out[j].Seeders })
	return out
}

type provResult struct {
	name string
	ts   []Torrent
	err  error
}

func TorrentSearch(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing query"})
		return
	}
	cat := c.DefaultQuery("cat", "0")
	ctx := c.Request.Context()

	provs := []struct {
		name string
		fn   func(context.Context, string) ([]Torrent, error)
	}{
		{"tpb", func(ctx context.Context, q string) ([]Torrent, error) { return tpbSearch(ctx, q, cat) }},
		{"knaben", knabenSearch},
		{"tcsv", tcsvSearch},
		{"nyaa", nyaaSearch},
	}

	ch := make(chan provResult, len(provs))
	for _, p := range provs {
		go func(name string, fn func(context.Context, string) ([]Torrent, error)) {
			ts, err := fn(ctx, q)
			ch <- provResult{name, ts, err}
		}(p.name, p.fn)
	}

	lists := make([][]Torrent, 0, len(provs))
	reached := false
	for range provs {
		r := <-ch
		if r.err != nil {
			log.Printf("[search] %s %q: %v", r.name, q, r.err)
			continue
		}
		reached = true
		log.Printf("[search] %s %q → %d results", r.name, q, len(r.ts))
		lists = append(lists, r.ts)
	}
	if !reached {
		c.JSON(http.StatusBadGateway, gin.H{"error": "search upstream unreachable"})
		return
	}

	out := mergeTorrents(lists...)
	log.Printf("[search] %q → %d merged", q, len(out))
	c.JSON(http.StatusOK, out)
}

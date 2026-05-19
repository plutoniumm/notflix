package jobs

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// The Pirate Bay's JSON backend. q.php returns up to ~100 results sorted by
// seeders; an empty result set comes back as a single sentinel row with
// id "0" / name "No results returned".
const apibayURL = "https://apibay.org/q.php"

// Trackers baked into magnets so newly-added torrents find peers fast
// instead of relying on DHT bootstrap alone.
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

var tpbClient = &http.Client{Timeout: 12 * time.Second}

func TPBSearch(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing query"})
		return
	}
	cat := c.DefaultQuery("cat", "0")

	u := fmt.Sprintf("%s?q=%s&cat=%s", apibayURL, url.QueryEscape(q), url.QueryEscape(cat))
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (notflix)")

	resp, err := tpbClient.Do(req)
	if err != nil {
		log.Printf("[search] tpb fetch %q: %v", q, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "search upstream unreachable"})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[search] tpb status %d for %q", resp.StatusCode, q)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("upstream HTTP %d", resp.StatusCode)})
		return
	}

	var raw []tpbRaw
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		log.Printf("[search] tpb decode %q: %v", q, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "bad upstream response"})
		return
	}

	out := make([]Torrent, 0, len(raw))
	for _, r := range raw {
		if r.ID == "0" || r.InfoHash == "" {
			continue // "No results returned" sentinel
		}
		out = append(out, Torrent{
			Name:     r.Name,
			Magnet:   magnetFor(r.Name, r.InfoHash),
			InfoHash: r.InfoHash,
			Size:     int64(atoi(r.Size)),
			Seeders:  atoi(r.Seeders),
			Leechers: atoi(r.Leechers),
			Files:    atoi(r.NumFiles),
			Added:    int64(atoi(r.Added)),
			Category: catLabel(r.Category),
			Status:   r.Status,
		})
	}

	log.Printf("[search] %q → %d results", q, len(out))
	c.JSON(http.StatusOK, out)
}

package jobs

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	knabenURL = "https://api.knaben.org/v1"
	tcsvURL   = "https://torrents-csv.com/service/search"
	nyaaURL   = "https://nyaa.si/?page=rss&c=0_0&f=0"
)

type knabenRaw struct {
	Hits []struct {
		Title     string `json:"title"`
		Hash      string `json:"hash"`
		Bytes     int64  `json:"bytes"`
		Seeders   int    `json:"seeders"`
		Peers     int    `json:"peers"`
		Date      string `json:"date"`
		Category  string `json:"category"`
		MagnetUrl string `json:"magnetUrl"`
	} `json:"hits"`
}

func parseKnaben(r io.Reader) ([]Torrent, error) {
	var raw knabenRaw
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]Torrent, 0, len(raw.Hits))
	for _, h := range raw.Hits {
		magnet := h.MagnetUrl
		if magnet == "" && h.Hash != "" {
			magnet = magnetFor(h.Title, h.Hash)
		}
		if magnet == "" {
			continue
		}
		var added int64
		if t, err := time.Parse(time.RFC3339, h.Date); err == nil {
			added = t.Unix()
		}
		out = append(out, Torrent{
			Name:     h.Title,
			Magnet:   magnet,
			InfoHash: h.Hash,
			Size:     h.Bytes,
			Seeders:  h.Seeders,
			Leechers: h.Peers,
			Added:    added,
			Category: h.Category,
			Source:   "knaben",
		})
	}
	return out, nil
}

func knabenSearch(q string) ([]Torrent, error) {
	body, _ := json.Marshal(map[string]any{
		"search_type": "100%", "search_field": "title", "query": q,
		"order_by": "seeders", "order_direction": "desc", "size": 100,
		"hide_unsafe": true, "hide_xxx": true,
	})
	resp, err := searchFetch(http.MethodPost, knabenURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	defer resp.Close()
	return parseKnaben(resp)
}

type tcsvRaw struct {
	Torrents []struct {
		InfoHash    string `json:"infohash"`
		Name        string `json:"name"`
		SizeBytes   int64  `json:"size_bytes"`
		CreatedUnix int64  `json:"created_unix"`
		Seeders     int    `json:"seeders"`
		Leechers    int    `json:"leechers"`
	} `json:"torrents"`
}

func parseTCSV(r io.Reader) ([]Torrent, error) {
	var raw tcsvRaw
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]Torrent, 0, len(raw.Torrents))
	for _, t := range raw.Torrents {
		if t.InfoHash == "" {
			continue
		}
		out = append(out, Torrent{
			Name:     t.Name,
			Magnet:   magnetFor(t.Name, t.InfoHash),
			InfoHash: t.InfoHash,
			Size:     t.SizeBytes,
			Seeders:  t.Seeders,
			Leechers: t.Leechers,
			Added:    t.CreatedUnix,
			Source:   "tcsv",
		})
	}
	return out, nil
}

func tcsvSearch(q string) ([]Torrent, error) {
	body, err := searchFetch(http.MethodGet, tcsvURL+"?q="+url.QueryEscape(q), nil)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return parseTCSV(body)
}

type nyaaRaw struct {
	Items []struct {
		Title    string `xml:"title"`
		PubDate  string `xml:"pubDate"`
		Seeders  int    `xml:"seeders"`
		Leechers int    `xml:"leechers"`
		InfoHash string `xml:"infoHash"`
		Category string `xml:"category"`
		Size     string `xml:"size"`
		Trusted  string `xml:"trusted"`
	} `xml:"channel>item"`
}

// "38.0 GiB" → bytes
func nyaaSize(s string) int64 {
	parts := strings.Fields(s)
	if len(parts) != 2 {
		return 0
	}
	v, _ := strconv.ParseFloat(parts[0], 64)
	mult := map[string]float64{
		"B": 1, "KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40,
	}
	return int64(v * mult[parts[1]])
}

func parseNyaa(r io.Reader) ([]Torrent, error) {
	var raw nyaaRaw
	if err := xml.NewDecoder(r).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]Torrent, 0, len(raw.Items))
	for _, it := range raw.Items {
		if it.InfoHash == "" {
			continue
		}
		var added int64
		if t, err := time.Parse(time.RFC1123Z, it.PubDate); err == nil {
			added = t.Unix()
		}
		status := ""
		if it.Trusted == "Yes" {
			status = "trusted"
		}
		out = append(out, Torrent{
			Name:     it.Title,
			Magnet:   magnetFor(it.Title, it.InfoHash),
			InfoHash: it.InfoHash,
			Size:     nyaaSize(it.Size),
			Seeders:  it.Seeders,
			Leechers: it.Leechers,
			Added:    added,
			Category: it.Category,
			Status:   status,
			Source:   "nyaa",
		})
	}
	return out, nil
}

func nyaaSearch(q string) ([]Torrent, error) {
	body, err := searchFetch(http.MethodGet, fmt.Sprintf("%s&q=%s", nyaaURL, url.QueryEscape(q)), nil)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return parseNyaa(body)
}

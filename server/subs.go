package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/martinlindhe/subtitles"
	"github.com/opensubtitlescli/moviehash"
)

var (
	tok    string
	tokExp time.Time
	tokMu  sync.Mutex
)

type embedTrack struct {
	Index    int    `json:"index"`
	Language string `json:"language"`
}

type SubResult struct {
	FileID        int    `json:"file_id"`
	Language      string `json:"language"`
	Release       string `json:"release"`
	DownloadCount int    `json:"download_count"`
	HashMatch     bool   `json:"hash_match"`
}

func findVid(file string, roots []string) (string, bool) {
	rel := strings.TrimPrefix(file, "/")
	for _, root := range roots {
		absR, err := filepath.Abs(root)
		if err != nil {
			continue
		}

		candidate := filepath.Join(root, rel)
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}

		if !strings.HasPrefix(abs, absR) {
			continue
		}

		if _, err := os.Stat(candidate); err == nil {
			return abs, true
		}
	}

	return "", false
}

func vttOf(path string) string {
	ext := filepath.Ext(path)

	return path[:len(path)-len(ext)] + ".vtt"
}

func whisperVTTOf(path string) string {
	ext := filepath.Ext(path)

	return path[:len(path)-len(ext)] + ".whisper.vtt"
}

func Subctx(c *gin.Context, roots []string) {
	raw := c.Query("file")
	if raw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file param"})
		return
	}

	path, ok := findVid(raw, roots)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return
	}

	base := path[:len(path)-len(filepath.Ext(path))]
	hasVTT := false
	hasSRT := false

	if _, err := os.Stat(base + ".vtt"); err == nil {
		hasVTT = true
	}
	if _, err := os.Stat(base + ".srt"); err == nil {
		hasSRT = true
	}

	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "s",
		"-show_entries", "stream=index,codec_name:stream_tags=language",
		"-of", "json",
		path,
	)
	out, err := cmd.Output()

	var embedded []embedTrack
	if err == nil {
		var probe struct {
			Streams []struct {
				Index     int               `json:"index"`
				CodecName string            `json:"codec_name"`
				Tags      map[string]string `json:"tags"`
			} `json:"streams"`
		}

		if json.Unmarshal(out, &probe) == nil {
			textCodecs := map[string]bool{
				"subrip": true, "ass": true, "webvtt": true, "mov_text": true,
			}

			for _, s := range probe.Streams {
				if !textCodecs[strings.ToLower(s.CodecName)] {
					continue
				}

				lang := ""
				if s.Tags != nil {
					lang = s.Tags["language"]
				}

				embedded = append(embedded, embedTrack{Index: s.Index, Language: lang})
			}
		}
	}

	if embedded == nil {
		embedded = []embedTrack{}
	}

	c.JSON(http.StatusOK, gin.H{"vtt": hasVTT, "srt": hasSRT, "embedded": embedded})
}

func SubsExtract(c *gin.Context, roots []string) {
	var body struct {
		File  string `json:"file"`
		Track int    `json:"track"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	path, ok := findVid(body.File, roots)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return
	}

	spec := fmt.Sprintf("0:s:%d", body.Track)
	cmd := exec.Command("ffmpeg", "-y", "-i", path, "-map", spec, vttOf(path))
	if out, err := cmd.CombinedOutput(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("ffmpeg failed: %v — %s", err, string(out)),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func SubsSearch(c *gin.Context, roots []string) {
	raw := c.Query("file")
	if raw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file param"})
		return
	}

	key := os.Getenv("OPENSUBTITLES_API_KEY")
	if key == "" {
		c.JSON(http.StatusOK, gin.H{"results": []SubResult{}, "error": "no_api_key"})
		return
	}

	path, ok := findVid(raw, roots)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return
	}

	f, err := os.Open(path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot open video"})
		return
	}
	hash, err := moviehash.Sum(f)
	f.Close()

	var results []SubResult

	if err == nil && hash != "" {
		results = osSearch(key, "moviehash="+hash+"&languages=en&order_by=download_count", true)
	}

	if len(results) == 0 {
		base := filepath.Base(path)
		ext := filepath.Ext(base)
		q := url.QueryEscape(cleanTitle(base[:len(base)-len(ext)]))
		results = osSearch(key, "query="+q+"&languages=en&order_by=download_count", false)
	}

	if len(results) > 10 {
		results = results[:10]
	}
	if results == nil {
		results = []SubResult{}
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}

var tagRe = regexp.MustCompile(`(?i)\b(720p|1080p|2160p|4k|480p|bluray|blu-ray|webrip|web-dl|webdl|web|hdrip|hdtv|dvdrip|x264|x265|h264|h265|hevc|avc|aac|ac3|dd5|eac3|atmos|nflx|amzn|hdr|10bit|repack|proper|extended|theatrical|directors\.cut|unrated)\b`)

func cleanTitle(name string) string {
	r := strings.NewReplacer(".", " ", "_", " ", "-", " ")
	s := r.Replace(name)

	s = tagRe.ReplaceAllString(s, " ")

	return strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(s, " "))
}

func osSearch(key, params string, byHash bool) []SubResult {
	req, err := http.NewRequest("GET", "https://api.opensubtitles.com/api/v1/subtitles?"+params, nil)

	if err != nil {
		return nil
	}

	req.Header.Set("Api-Key", key)
	req.Header.Set("User-Agent", "notflix v1.0")

	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var payload struct {
		Data []struct {
			Attributes struct {
				Language      string `json:"language"`
				Release       string `json:"release"`
				DownloadCount int    `json:"download_count"`
				Files         []struct {
					FileID int `json:"file_id"`
				} `json:"files"`
			} `json:"attributes"`
		} `json:"data"`
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}

	var out []SubResult
	for _, item := range payload.Data {
		a := item.Attributes
		fid := 0
		if len(a.Files) > 0 {
			fid = a.Files[0].FileID
		}

		out = append(out, SubResult{
			FileID:        fid,
			Language:      a.Language,
			Release:       a.Release,
			DownloadCount: a.DownloadCount,
			HashMatch:     byHash,
		})
	}
	return out
}

func fetchToken(key string) string {
	user := os.Getenv("OPENSUBTITLES_USER")
	pass := os.Getenv("OPENSUBTITLES_PASS")
	if user == "" || pass == "" {
		return ""
	}

	tokMu.Lock()
	defer tokMu.Unlock()

	if tok != "" && time.Now().Before(tokExp) {
		return tok
	}

	body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	req, err := http.NewRequest("POST", "https://api.opensubtitles.com/api/v1/login", bytes.NewReader(body))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Api-Key", key)
	req.Header.Set("User-Agent", "notflix v1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var result struct {
		Token string `json:"token"`
	}

	data, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(data, &result); err != nil {
		return ""
	}

	tok = result.Token
	tokExp = time.Now().Add(23 * time.Hour)
	return tok
}

func GetSubs(c *gin.Context, roots []string) {
	var body struct {
		FileID int    `json:"file_id"`
		File   string `json:"file"`
	}

	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	path, ok := findVid(body.File, roots)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return
	}

	key := os.Getenv("OPENSUBTITLES_API_KEY")

	rb, _ := json.Marshal(map[string]int{"file_id": body.FileID})
	req, err := http.NewRequest("POST", "https://api.opensubtitles.com/api/v1/download", bytes.NewReader(rb))

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "request error"})
		return
	}

	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Api-Key", key)
	}
	req.Header.Set("User-Agent", "notflix v1.0")
	if t := fetchToken(key); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "download request failed"})
		return
	}
	defer resp.Body.Close()

	var dl struct {
		Link     string `json:"link"`
		FileName string `json:"file_name"`
	}

	data, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(data, &dl); err != nil || dl.Link == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid download response"})
		return
	}

	sr, err := http.Get(dl.Link)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to download subtitle"})
		return
	}
	defer sr.Body.Close()

	raw, err := io.ReadAll(sr.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read subtitle"})
		return
	}

	ext := strings.ToLower(filepath.Ext(dl.FileName))
	var vtt string

	switch ext {
	case ".srt":
		parsed, err := subtitles.NewFromSRT(string(raw))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "srt parse error: " + err.Error()})
			return
		}
		vtt = parsed.AsVTT()
	case ".vtt":
		parsed, err := subtitles.NewFromVTT(string(raw))
		if err != nil {
			vtt = string(raw)
		} else {
			vtt = parsed.AsVTT()
		}
	default:
		parsed, err := subtitles.NewFromSRT(string(raw))
		if err != nil {
			vtt = string(raw)
		} else {
			vtt = parsed.AsVTT()
		}
	}

	if err := os.WriteFile(vttOf(path), []byte(vtt), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save vtt"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func SubsSend(c *gin.Context, roots []string) {
	filename := strings.TrimPrefix(c.Param("filename"), "/")

	path, ok := findVid(filename, roots)
	if ok && strings.ToLower(filepath.Ext(path)) == ".vtt" {
		c.Header("Content-Type", "text/vtt")
		c.File(path)
		return
	}

	if strings.ToLower(filepath.Ext(filename)) == ".vtt" {
		srtName := filename[:len(filename)-len(".vtt")] + ".srt"
		srtP, ok := findVid(srtName, roots)
		if ok {
			data, err := os.ReadFile(srtP)

			if err == nil {
				parsed, err := subtitles.NewFromSRT(string(data))
				if err == nil {
					vtt := parsed.AsVTT()
					out := srtP[:len(srtP)-len(".srt")] + ".vtt"

					if os.WriteFile(out, []byte(vtt), 0644) == nil {
						os.Remove(srtP)
						c.Header("Content-Type", "text/vtt")
						c.File(out)
						return
					}

					c.Header("Content-Type", "text/vtt")
					c.String(http.StatusOK, "%s", vtt)
					return
				}
			}
		}
	}

	c.String(http.StatusNotFound, "Subtitle not found")
}

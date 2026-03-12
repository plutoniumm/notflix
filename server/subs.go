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

// ─── JWT cache ────────────────────────────────────────────────────────────────

var (
	jwtToken     string
	jwtExpiresAt time.Time
	jwtMu        sync.Mutex
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// findVideoPath finds the actual video file given a relative path like
// "dir/name.mkv" across all roots. The leading "/" is stripped if present.
func findVideoPath(file string, roots []string) (string, bool) {
	rel := strings.TrimPrefix(file, "/")
	for _, root := range roots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		candidate := filepath.Join(root, rel)
		absCandidate, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(absCandidate, absRoot) {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return absCandidate, true
		}
	}
	return "", false
}

// videoToVTTPath replaces the video extension with .vtt.
func videoToVTTPath(videoPath string) string {
	ext := filepath.Ext(videoPath)
	return videoPath[:len(videoPath)-len(ext)] + ".vtt"
}

// videoToWhisperVTTPath returns the .whisper.vtt path for a video file.
func videoToWhisperVTTPath(videoPath string) string {
	ext := filepath.Ext(videoPath)
	return videoPath[:len(videoPath)-len(ext)] + ".whisper.vtt"
}

// ─── SubsInfo ─────────────────────────────────────────────────────────────────

type ffprobeStream struct {
	Index int               `json:"index"`
	Tags  map[string]string `json:"tags"`
	// codec_name is only present when we ask for it; use a separate struct
	CodecName string `json:"codec_name"`
}

type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
}

type embeddedTrack struct {
	Index    int    `json:"index"`
	Language string `json:"language"`
}

func SubsInfo(c *gin.Context, roots []string) {
	rawFile := c.Query("file")
	if rawFile == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file param"})
		return
	}

	videoPath, ok := findVideoPath(rawFile, roots)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return
	}

	base := videoPath[:len(videoPath)-len(filepath.Ext(videoPath))]
	vttExists := false
	srtExists := false

	if _, err := os.Stat(base + ".vtt"); err == nil {
		vttExists = true
	}
	if _, err := os.Stat(base + ".srt"); err == nil {
		srtExists = true
	}

	// Run ffprobe to get embedded subtitle streams (codec name + language tag)
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "s",
		"-show_entries", "stream=index,codec_name:stream_tags=language",
		"-of", "json",
		videoPath,
	)
	out, err := cmd.Output()

	var embedded []embeddedTrack
	if err == nil {
		var probe struct {
			Streams []struct {
				Index     int               `json:"index"`
				CodecName string            `json:"codec_name"`
				Tags      map[string]string `json:"tags"`
			} `json:"streams"`
		}
		if jsonErr := json.Unmarshal(out, &probe); jsonErr == nil {
			textCodecs := map[string]bool{
				"subrip":   true,
				"ass":      true,
				"webvtt":   true,
				"mov_text": true,
			}
			for _, s := range probe.Streams {
				if !textCodecs[strings.ToLower(s.CodecName)] {
					continue
				}
				lang := ""
				if s.Tags != nil {
					lang = s.Tags["language"]
				}
				embedded = append(embedded, embeddedTrack{Index: s.Index, Language: lang})
			}
		}
	}

	if embedded == nil {
		embedded = []embeddedTrack{}
	}

	c.JSON(http.StatusOK, gin.H{
		"vtt":      vttExists,
		"srt":      srtExists,
		"embedded": embedded,
	})
}

// ─── SubsExtract ──────────────────────────────────────────────────────────────

func SubsExtract(c *gin.Context, roots []string) {
	var body struct {
		File  string `json:"file"`
		Track int    `json:"track"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	videoPath, ok := findVideoPath(body.File, roots)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return
	}

	vttPath := videoToVTTPath(videoPath)
	trackSpec := fmt.Sprintf("0:s:%d", body.Track)

	cmd := exec.Command("ffmpeg", "-y", "-i", videoPath, "-map", trackSpec, vttPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("ffmpeg failed: %v — %s", err, string(out)),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ─── SubsSearch ───────────────────────────────────────────────────────────────

type SubResult struct {
	FileID        int    `json:"file_id"`
	Language      string `json:"language"`
	Release       string `json:"release"`
	DownloadCount int    `json:"download_count"`
	HashMatch     bool   `json:"hash_match"`
}

func SubsSearch(c *gin.Context, roots []string) {
	rawFile := c.Query("file")
	if rawFile == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file param"})
		return
	}

	apiKey := os.Getenv("OPENSUBTITLES_API_KEY")
	if apiKey == "" {
		c.JSON(http.StatusOK, gin.H{"results": []SubResult{}, "error": "no_api_key"})
		return
	}

	videoPath, ok := findVideoPath(rawFile, roots)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return
	}

	// Compute OSHash
	f, err := os.Open(videoPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot open video"})
		return
	}
	hash, err := moviehash.Sum(f)
	f.Close()

	var results []SubResult

	if err == nil && hash != "" {
		results = osSearch(apiKey, "moviehash="+hash+"&languages=en&order_by=download_count", true)
	}

	if len(results) == 0 {
		// Fallback: clean title search
		base := filepath.Base(videoPath)
		ext := filepath.Ext(base)
		name := base[:len(base)-len(ext)]
		clean := cleanTitle(name)
		qParam := "query=" + url.QueryEscape(clean) + "&languages=en&order_by=download_count"
		results = osSearch(apiKey, qParam, false)
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
	// Replace dots/underscores/dashes with spaces
	r := strings.NewReplacer(".", " ", "_", " ", "-", " ")
	s := r.Replace(name)
	// Strip common tags
	s = tagRe.ReplaceAllString(s, " ")
	// Collapse whitespace
	s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func osSearch(apiKey, queryParams string, hashMatch bool) []SubResult {
	reqURL := "https://api.opensubtitles.com/api/v1/subtitles?" + queryParams
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Api-Key", apiKey)
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}

	var out []SubResult
	for _, item := range payload.Data {
		a := item.Attributes
		fileID := 0
		if len(a.Files) > 0 {
			fileID = a.Files[0].FileID
		}
		out = append(out, SubResult{
			FileID:        fileID,
			Language:      a.Language,
			Release:       a.Release,
			DownloadCount: a.DownloadCount,
			HashMatch:     hashMatch,
		})
	}
	return out
}

// ─── SubsDownload ─────────────────────────────────────────────────────────────

func getJWT(apiKey string) string {
	user := os.Getenv("OPENSUBTITLES_USER")
	pass := os.Getenv("OPENSUBTITLES_PASS")
	if user == "" || pass == "" {
		return ""
	}

	jwtMu.Lock()
	defer jwtMu.Unlock()

	if jwtToken != "" && time.Now().Before(jwtExpiresAt) {
		return jwtToken
	}

	body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	req, err := http.NewRequest("POST", "https://api.opensubtitles.com/api/v1/login", bytes.NewReader(body))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Api-Key", apiKey)
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

	jwtToken = result.Token
	jwtExpiresAt = time.Now().Add(23 * time.Hour)
	return jwtToken
}

func SubsDownload(c *gin.Context, roots []string) {
	var body struct {
		FileID int    `json:"file_id"`
		File   string `json:"file"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	videoPath, ok := findVideoPath(body.File, roots)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return
	}

	apiKey := os.Getenv("OPENSUBTITLES_API_KEY")

	// Request download link
	dlBody, _ := json.Marshal(map[string]int{"file_id": body.FileID})
	req, err := http.NewRequest("POST", "https://api.opensubtitles.com/api/v1/download", bytes.NewReader(dlBody))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "request error"})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Api-Key", apiKey)
	}
	req.Header.Set("User-Agent", "notflix v1.0")

	jwt := getJWT(apiKey)
	if jwt != "" {
		req.Header.Set("Authorization", "Bearer "+jwt)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "download request failed"})
		return
	}
	defer resp.Body.Close()

	var dlResp struct {
		Link     string `json:"link"`
		FileName string `json:"file_name"`
	}
	respData, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(respData, &dlResp); err != nil || dlResp.Link == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid download response"})
		return
	}

	// Download the subtitle file
	subResp, err := http.Get(dlResp.Link)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to download subtitle"})
		return
	}
	defer subResp.Body.Close()

	subData, err := io.ReadAll(subResp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read subtitle"})
		return
	}

	// Convert to VTT based on extension
	ext := strings.ToLower(filepath.Ext(dlResp.FileName))
	var vttContent string

	switch ext {
	case ".srt":
		parsed, err := subtitles.NewFromSRT(string(subData))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "srt parse error: " + err.Error()})
			return
		}
		vttContent = parsed.AsVTT()
	case ".vtt":
		parsed, err := subtitles.NewFromVTT(string(subData))
		if err != nil {
			// If parsing fails, use raw content
			vttContent = string(subData)
		} else {
			vttContent = parsed.AsVTT()
		}
	default:
		// Try SRT as fallback
		parsed, err := subtitles.NewFromSRT(string(subData))
		if err != nil {
			vttContent = string(subData)
		} else {
			vttContent = parsed.AsVTT()
		}
	}

	vttPath := videoToVTTPath(videoPath)
	if err := os.WriteFile(vttPath, []byte(vttContent), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save vtt"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ─── SubsSend ─────────────────────────────────────────────────────────────────

func SubsSend(c *gin.Context, roots []string) {
	filename := c.Param("filename")
	filename = strings.TrimPrefix(filename, "/")

	// Try to find the .vtt file directly
	vttPath, ok := findVideoPath(filename, roots)
	if ok && strings.ToLower(filepath.Ext(vttPath)) == ".vtt" {
		c.Header("Content-Type", "text/vtt")
		c.File(vttPath)
		return
	}

	// Check if .srt exists at the same base path, convert and serve
	if strings.ToLower(filepath.Ext(filename)) == ".vtt" {
		srtFilename := filename[:len(filename)-len(".vtt")] + ".srt"
		srtPath, srtOk := findVideoPath(srtFilename, roots)
		if srtOk {
			data, err := os.ReadFile(srtPath)
			if err == nil {
				parsed, err := subtitles.NewFromSRT(string(data))
				if err == nil {
					vttContent := parsed.AsVTT()
					// Determine vtt output path next to the srt file
					outVTT := srtPath[:len(srtPath)-len(".srt")] + ".vtt"
					if writeErr := os.WriteFile(outVTT, []byte(vttContent), 0644); writeErr == nil {
						os.Remove(srtPath)
						c.Header("Content-Type", "text/vtt")
						c.File(outVTT)
						return
					}
					// If write failed, still serve the content
					c.Header("Content-Type", "text/vtt")
					c.String(http.StatusOK, "%s", vttContent)
					return
				}
			}
		}
	}

	c.String(http.StatusNotFound, "Subtitle not found")
}

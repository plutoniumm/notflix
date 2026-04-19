package media

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

	"notflix/server/library"
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

type localTrack struct {
	File     string `json:"file"`
	Language string `json:"language"`
}

func nextSubPath(base, lang string) string {
	if lang == "" {
		lang = "und"
	}
	lang = strings.ToLower(lang)

	candidate := base + "." + lang + ".vtt"
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}
	for i := 2; i < 100; i++ {
		candidate = fmt.Sprintf("%s.%s%d.vtt", base, lang, i)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return base + "." + lang + ".vtt"
}

type SubResult struct {
	Provider      string `json:"provider"`
	FileID        int    `json:"file_id,omitempty"`
	URL           string `json:"url,omitempty"`
	Language      string `json:"language"`
	Release       string `json:"release"`
	DownloadCount int    `json:"download_count"`
	HashMatch     bool   `json:"hash_match"`
}

func vttOf(path string) string {
	ext := filepath.Ext(path)

	return path[:len(path)-len(ext)] + ".vtt"
}

func whisperVTTOf(path string) string {
	ext := filepath.Ext(path)

	return path[:len(path)-len(ext)] + ".whisper.vtt"
}

func Subctx(c *gin.Context, lib *library.Library) {
	raw := c.Query("file")
	if raw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file param"})
		return
	}

	path, ok := lib.FindVid(raw)
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

	var local []localTrack
	matches, _ := filepath.Glob(base + ".*.vtt")
	dir := filepath.Dir(path)
	for _, m := range matches {
		bn := filepath.Base(m)
		prefix := filepath.Base(base) + "."
		suffix := ".vtt"
		lang := strings.TrimSuffix(strings.TrimPrefix(bn, prefix), suffix)
		if lang == bn {
			continue
		}
		local = append(local, localTrack{
			File:     strings.TrimPrefix(m, dir+string(os.PathSeparator)),
			Language: lang,
		})
	}
	if hasVTT {
		local = append(local, localTrack{
			File:     filepath.Base(base) + ".vtt",
			Language: "",
		})
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

	hasWhisper := false
	if _, err := os.Stat(base + ".whisper.vtt"); err == nil {
		hasWhisper = true
	}

	if local == nil {
		local = []localTrack{}
	}
	c.JSON(http.StatusOK, gin.H{"vtt": hasVTT, "srt": hasSRT, "embedded": embedded, "whisper": hasWhisper, "local": local})
}

func SubsExtract(c *gin.Context, lib *library.Library) {
	var body struct {
		File     string `json:"file"`
		Track    int    `json:"track"`
		Language string `json:"language"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	path, ok := lib.FindVid(body.File)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return
	}

	base := path[:len(path)-len(filepath.Ext(path))]
	outPath := nextSubPath(base, body.Language)

	spec := fmt.Sprintf("0:%d", body.Track)
	cmd := exec.Command("ffmpeg", "-y", "-i", path, "-map", spec, "-c:s", "webvtt", outPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("ffmpeg failed: %v — %s", err, string(out)),
		})
		return
	}

	dir := filepath.Dir(path)
	rel := strings.TrimPrefix(outPath, dir+string(os.PathSeparator))
	c.JSON(http.StatusOK, gin.H{"ok": true, "file": rel})
}

func SubsSearch(c *gin.Context, lib *library.Library) {
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

	path, ok := lib.FindVid(raw)
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

	base := filepath.Base(path)
	ext := filepath.Ext(base)
	title := cleanTitle(base[:len(base)-len(ext)])

	if len(results) == 0 {
		q := url.QueryEscape(title)
		results = osSearch(key, "query="+q+"&languages=en&order_by=download_count", false)
	}

	if len(results) == 0 {
		results = subdlSearch(title)
	}

	if len(results) > 10 {
		results = results[:10]
	}

	if results == nil {
		results = []SubResult{}
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}

func subdlSearch(title string) []SubResult {
	key := os.Getenv("SUBDL_API_KEY")
	if key == "" {
		return nil
	}

	q := url.Values{}
	q.Set("api_key", key)
	q.Set("film_name", title)
	q.Set("languages", "EN")
	q.Set("subs_per_page", "30")

	resp, err := http.Get("https://api.subdl.com/api/v1/subtitles?" + q.Encode())
	if err != nil {
		log.Printf("[subs] subdl request error: %v", err)

		return nil
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[subs] subdl status=%d body=%s", resp.StatusCode, string(data))

		return nil
	}

	var payload struct {
		Status    bool `json:"status"`
		Subtitles []struct {
			Lang        string `json:"lang"`
			Language    string `json:"language"`
			URL         string `json:"url"`
			ReleaseName string `json:"release_name"`
		} `json:"subtitles"`
	}

	if err := json.Unmarshal(data, &payload); err != nil {
		log.Printf("[subs] subdl parse error: %v body=%s", err, string(data))

		return nil
	}

	var out []SubResult
	for _, s := range payload.Subtitles {
		u := s.URL
		if u == "" {
			continue
		}

		if !strings.HasPrefix(u, "http") {
			u = "https://dl.subdl.com" + u
		}

		lang := s.Lang
		if lang == "" {
			lang = s.Language
		}

		out = append(out, SubResult{
			Provider: "subdl",
			URL:      u,
			Language: strings.ToLower(lang),
			Release:  s.ReleaseName,
		})
	}

	return out
}

var tagRe = regexp.MustCompile(`(?i)\b(720p|1080p|2160p|4k|480p|bluray|blu-ray|webrip|web-dl|webdl|web|hdrip|hdtv|dvdrip|x264|x265|h264|h265|hevc|avc|aac|ac3|dd5|eac3|atmos|nflx|amzn|hdr|10bit|repack|proper|extended|theatrical|directors\.cut|unrated)\b`)

func cleanTitle(name string) string {
	r := strings.NewReplacer(".", " ", "_", " ", "-", " ")
	s := r.Replace(name)

	s = tagRe.ReplaceAllString(s, " ")

	return strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(s, " "))
}

const osUA = "notflix v1.0.0"

func osSearch(key, params string, byHash bool) []SubResult {
	req, err := http.NewRequest("GET", "https://api.opensubtitles.com/api/v1/subtitles?"+params, nil)
	if err != nil {
		return nil
	}

	req.Header.Set("Api-Key", key)
	req.Header.Set("User-Agent", osUA)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[subs] osSearch request error: %v", err)

		return nil
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[subs] osSearch status=%d body=%s params=%q", resp.StatusCode, string(data), params)

		return nil
	}

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

	if err := json.Unmarshal(data, &payload); err != nil {
		log.Printf("[subs] osSearch parse error: %v body=%s", err, string(data))

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
			Provider:      "os",
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
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Api-Key", key)
	req.Header.Set("User-Agent", osUA)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[subs] login request failed: %v", err)
		return ""
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Printf("[subs] login failed: status=%d body=%s", resp.StatusCode, string(data))
		return ""
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		log.Printf("[subs] login parse error: %v", err)
		return ""
	}

	log.Printf("[subs] login ok, token acquired")
	tok = result.Token
	tokExp = time.Now().Add(23 * time.Hour)
	return tok
}

func GetSubs(c *gin.Context, lib *library.Library) {
	var body struct {
		Provider string `json:"provider"`
		FileID   int    `json:"file_id"`
		URL      string `json:"url"`
		File     string `json:"file"`
	}

	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	path, ok := lib.FindVid(body.File)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return
	}

	if body.Provider == "subdl" {
		subdlFetch(c, path, body.URL)
		return
	}

	key := os.Getenv("OPENSUBTITLES_API_KEY")
	token := fetchToken(key)
	rb, _ := json.Marshal(map[string]int{"file_id": body.FileID})

	var dl struct {
		Link     string `json:"link"`
		FileName string `json:"file_name"`
	}

	var lastErr string
	for attempt := range 3 {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		req, err := http.NewRequest("POST", "https://api.opensubtitles.com/api/v1/download", bytes.NewReader(rb))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "request error"})
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if key != "" {
			req.Header.Set("Api-Key", key)
		}
		req.Header.Set("User-Agent", osUA)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("[subs] download request error (attempt %d): %v", attempt+1, err)
			lastErr = "download request failed"
			continue
		}

		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		log.Printf("[subs] OS download attempt=%d status=%d body=%s", attempt+1, resp.StatusCode, string(data))

		if resp.StatusCode == http.StatusOK {
			if err := json.Unmarshal(data, &dl); err != nil || dl.Link == "" {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid download response"})
				return
			}
			break
		}

		var osErr struct {
			Message string   `json:"message"`
			Errors  []string `json:"errors"`
		}
		lastErr = fmt.Sprintf("OpenSubtitles error (HTTP %d)", resp.StatusCode)
		if json.Unmarshal(data, &osErr) == nil && osErr.Message != "" {
			lastErr = osErr.Message
			if len(osErr.Errors) > 0 {
				lastErr += ": " + strings.Join(osErr.Errors, "; ")
			}
		}

		if resp.StatusCode != http.StatusServiceUnavailable && resp.StatusCode != http.StatusTooManyRequests {
			break
		}
	}

	if dl.Link == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": lastErr})
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

	base := path[:len(path)-len(filepath.Ext(path))]
	outPath := nextSubPath(base, "eng")

	if err := os.WriteFile(outPath, []byte(vtt), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save vtt"})
		return
	}

	dir := filepath.Dir(path)
	rel := strings.TrimPrefix(outPath, dir+string(os.PathSeparator))
	c.JSON(http.StatusOK, gin.H{"ok": true, "file": rel})
}

func subdlFetch(c *gin.Context, videoPath, src string) {
	resp, err := http.Get(src)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "subdl download: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("subdl HTTP %d", resp.StatusCode)})
		return
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "subdl read failed"})
		return
	}

	srt, err := firstSRT(data, src)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	parsed, err := subtitles.NewFromSRT(string(srt))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "srt parse: " + err.Error()})
		return
	}

	base := videoPath[:len(videoPath)-len(filepath.Ext(videoPath))]
	outPath := nextSubPath(base, "eng")

	if err := os.WriteFile(outPath, []byte(parsed.AsVTT()), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save vtt"})
		return
	}

	dir := filepath.Dir(videoPath)
	rel := strings.TrimPrefix(outPath, dir+string(os.PathSeparator))
	c.JSON(http.StatusOK, gin.H{"ok": true, "file": rel})
}

func firstSRT(data []byte, src string) ([]byte, error) {
	lower := strings.ToLower(src)
	if strings.HasSuffix(lower, ".srt") {
		return data, nil
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("subdl: not a zip and not a .srt")
	}

	for _, f := range zr.File {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".srt") {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			continue
		}

		body, err := io.ReadAll(rc)
		rc.Close()

		if err == nil && len(body) > 0 {
			return body, nil
		}
	}

	return nil, fmt.Errorf("subdl: no .srt in archive")
}

func SubsSend(c *gin.Context, lib *library.Library) {
	filename := strings.TrimPrefix(c.Param("filename"), "/")

	path, ok := lib.FindVid(filename)
	if ok && strings.ToLower(filepath.Ext(path)) == ".vtt" {
		c.Header("Content-Type", "text/vtt")
		c.File(path)
		return
	}

	if strings.ToLower(filepath.Ext(filename)) == ".vtt" {
		srtName := filename[:len(filename)-len(".vtt")] + ".srt"
		srtP, ok := lib.FindVid(srtName)
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

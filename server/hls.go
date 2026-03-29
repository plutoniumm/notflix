package server

import (
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	hlsSegDur   = 4.0
	hlsCacheDir = "./cache"
)

var hlsInFlight sync.Map

type hlsProfile struct {
	h              int
	scale, vbr, ab string
}

var hlsProfiles = map[string]hlsProfile{
	"144p":  {144, "scale=-2:144", "150k", "64k"},
	"240p":  {240, "scale=-2:240", "300k", "80k"},
	"360p":  {360, "scale=-2:360", "500k", "96k"},
	"480p":  {480, "scale=-2:480", "800k", "112k"},
	"720p":  {720, "scale=-2:720", "2000k", "128k"},
	"1080p": {1080, "scale=-2:1080", "4000k", "192k"},
	"2160p": {2160, "scale=-2:2160", "12000k", "256k"},
}

// ordered from lowest to highest — master playlist lists them low→high so
// players start at the lowest then ramp up via ABR.
var hlsQualityOrder = []string{"144p", "240p", "360p", "480p", "720p", "1080p", "2160p"}

// bandwidth in bits/s for each quality (used in #EXT-X-STREAM-INF:BANDWIDTH)
// biased 25% below actual encode rate so ABR favours the next tier up
var hlsBandwidth = map[string]int{
	"144p":  112_000,
	"240p":  225_000,
	"360p":  375_000,
	"480p":  600_000,
	"720p":  1_500_000,
	"1080p": 3_000_000,
	"2160p": 9_000_000,
}

// HLSAVOffset probes the source file and returns the A/V start-time offset in ms.
// A positive value means audio starts before video (audio is ahead); the frontend
// should delay audio by that amount to re-align.
func HLSAVOffset(c *gin.Context, roots []string) {
	file := c.Query("file")
	path := hlsFindFile(file, roots)
	if path == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	streams, err := prober.Streams(context.Background(), path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var vStart, aStart float64
	var hasV, hasA bool
	for _, s := range streams {
		if s.CodecType == "video" && !hasV {
			vStart = s.StartTime.Duration.Seconds()
			hasV = true
		} else if s.CodecType == "audio" && !hasA {
			aStart = s.StartTime.Duration.Seconds()
			hasA = true
		}
	}

	if !hasV || !hasA {
		c.JSON(http.StatusOK, gin.H{"offset_ms": 0})
		return
	}

	// positive → audio ahead of video → delay audio by this amount
	// negative → video ahead of audio → not correctable in frontend (backend aresample handles it)
	offsetMs := int((vStart - aStart) * 1000)
	c.JSON(http.StatusOK, gin.H{"offset_ms": offsetMs})
}

func hlsFindFile(file string, roots []string) string {
	return FindFile(file, roots)
}

func HLSMaster(c *gin.Context, roots []string) {
	file := c.Query("file")
	path := hlsFindFile(file, roots)
	if path == "" {
		c.String(http.StatusNotFound, "not found")
		return
	}

	// Probe source height so we don't offer upscaled levels.
	srcHeight := 0
	if streams, err := prober.Streams(context.Background(), path); err == nil {
		for _, s := range streams {
			if s.CodecType == "video" {
				srcHeight = s.Height
				break
			}
		}
	}

	audio := c.DefaultQuery("audio", "0")
	encFile := url.QueryEscape(file)
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n\n")

	for _, q := range hlsQualityOrder {
		p := hlsProfiles[q]
		if srcHeight > 0 && p.h > srcHeight {
			continue
		}
		sb.WriteString(fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d\n", hlsBandwidth[q]))
		sb.WriteString(fmt.Sprintf("/api/hls/playlist?file=%s&q=%s&audio=%s\n", encFile, q, audio))
	}

	c.Header("Cache-Control", "no-cache")
	c.Data(http.StatusOK, "application/vnd.apple.mpegurl", []byte(sb.String()))
}

func HLSPlaylist(c *gin.Context, roots []string) {
	file := c.Query("file")
	q := c.Query("q")
	audio := c.DefaultQuery("audio", "0")

	if _, ok := hlsProfiles[q]; !ok {
		c.String(http.StatusBadRequest, "invalid quality")
		return
	}

	path := hlsFindFile(file, roots)
	if path == "" {
		c.String(http.StatusNotFound, "not found")
		return
	}

	dur := duration(path)
	if dur <= 0 {
		c.String(http.StatusInternalServerError, "could not probe duration")
		return
	}

	numSegs := int(math.Ceil(dur / hlsSegDur))
	encFile := url.QueryEscape(file)

	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")
	sb.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", int(hlsSegDur)))
	sb.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	sb.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")

	for i := 0; i < numSegs; i++ {
		segDur := hlsSegDur
		if remaining := dur - float64(i)*hlsSegDur; remaining < hlsSegDur {
			segDur = remaining
		}
		sb.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", segDur))
		sb.WriteString(fmt.Sprintf("/api/hls/segment?file=%s&q=%s&seg=%d&audio=%s\n", encFile, q, i, audio))
	}

	sb.WriteString("#EXT-X-ENDLIST\n")

	// c.Data preserves the Content-Type we pass — c.String forces text/plain
	c.Header("Cache-Control", "no-cache")
	c.Data(http.StatusOK, "application/vnd.apple.mpegurl", []byte(sb.String()))
}

func HLSSegment(c *gin.Context, roots []string) {
	file := c.Query("file")
	q := c.Query("q")
	segStr := c.Query("seg")
	audioStr := c.DefaultQuery("audio", "0")

	p, ok := hlsProfiles[q]
	if !ok {
		c.String(http.StatusBadRequest, "invalid quality")
		return
	}

	var segNum int
	if n, err := fmt.Sscanf(segStr, "%d", &segNum); n != 1 || err != nil || segNum < 0 {
		c.String(http.StatusBadRequest, "invalid segment")
		return
	}

	var audioIdx int
	if n, err := fmt.Sscanf(audioStr, "%d", &audioIdx); n != 1 || err != nil || audioIdx < 0 {
		audioIdx = 0
	}

	path := hlsFindFile(file, roots)
	if path == "" {
		c.String(http.StatusNotFound, "not found")
		return
	}

	hash := Hash(file)
	cacheKey := fmt.Sprintf("%s/%s/a%d/%06d", hash, q, audioIdx, segNum)
	segPath := filepath.Join(hlsCacheDir, hash, q, fmt.Sprintf("a%d", audioIdx), fmt.Sprintf("%06d.ts", segNum))

	if _, err := os.Stat(segPath); err == nil {
		serveSegment(c, segPath)
		return
	}

	// In-flight dedup: if another goroutine is generating this segment, wait for it
	ch := make(chan struct{})
	if actual, loaded := hlsInFlight.LoadOrStore(cacheKey, ch); loaded {
		select {
		case <-actual.(chan struct{}):
			serveSegment(c, segPath)
		case <-time.After(90 * time.Second):
			c.String(http.StatusGatewayTimeout, "segment generation timed out")
		}
		return
	}

	defer func() {
		hlsInFlight.Delete(cacheKey)
		close(ch)
	}()

	if err := generateSegment(path, segPath, segNum, p, audioIdx); err != nil {
		log.Printf("HLS segment error [%s q=%s seg=%d audio=%d]: %v", filepath.Base(path), q, segNum, audioIdx, err)
		c.String(http.StatusInternalServerError, fmt.Sprintf("transcode failed: %v", err))
		return
	}

	serveSegment(c, segPath)
}

func generateSegment(srcPath, segPath string, segNum int, p hlsProfile, audioIdx int) error {
	if err := os.MkdirAll(filepath.Dir(segPath), 0755); err != nil {
		return err
	}

	start := float64(segNum) * hlsSegDur
	tmp := segPath + ".tmp"

	args := []string{
		"-nostdin", "-y", "-v", "error",
		"-ss", fmt.Sprintf("%.3f", start),
		"-i", srcPath,
		"-t", fmt.Sprintf("%.3f", hlsSegDur),
		"-map", "0:v:0", "-map", fmt.Sprintf("0:a:%d?", audioIdx),
		"-c:v", "libx264", "-preset", "ultrafast", "-b:v", p.vbr,
		"-vf", p.scale,
		"-c:a", "aac", "-b:a", p.ab, "-ac", "2",
		"-af", "aresample=async=1", // compensate A/V drift from AAC encoder priming at segment boundaries
		"-output_ts_offset", fmt.Sprintf("%.3f", start), // TS timestamps must be monotonic across segments
		"-f", "mpegts", tmp,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("%v: %s", err, string(out))
	}

	return os.Rename(tmp, segPath)
}

func serveSegment(c *gin.Context, segPath string) {
	f, err := os.Open(segPath)
	if err != nil {
		log.Printf("serveSegment open: %v", err)
		c.String(http.StatusInternalServerError, "failed to open segment")
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to stat segment")
		return
	}

	c.Header("Content-Type", "video/mp2t")
	c.Header("Content-Length", fmt.Sprintf("%d", info.Size()))
	c.Header("Cache-Control", "public, max-age=31536000")
	// Don't call c.Status() — let the first Write flush headers with 200
	io.Copy(c.Writer, f)
}

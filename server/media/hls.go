package media

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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"notflix/server/library"
)

const (
	hlsSegDur   = 4.0
	hlsCacheDir = "./cache"
)

var hlsInFlight sync.Map

type hlsProfile struct {
	h              int
	scale, vbr, ab string
	crf            int // libx264 quality target; lower = better
}

// vbr here doubles as the maxrate cap; bufsize = 2× vbr. CRF targets quality,
// so calm scenes naturally use fewer bits while action scenes ride the cap.
var hlsProfiles = map[string]hlsProfile{
	"144p":  {144, "scale=-2:144", "200k", "64k", 28},
	"240p":  {240, "scale=-2:240", "400k", "80k", 27},
	"360p":  {360, "scale=-2:360", "650k", "96k", 26},
	"480p":  {480, "scale=-2:480", "1000k", "112k", 24},
	"720p":  {720, "scale=-2:720", "4000k", "128k", 21},
	"1080p": {1080, "scale=-2:1080", "8000k", "192k", 20},
	"2160p": {2160, "scale=-2:2160", "25000k", "256k", 19},
}

func doubleKRate(s string) string {
	if n, err := strconv.Atoi(strings.TrimSuffix(s, "k")); err == nil {
		return fmt.Sprintf("%dk", 2*n)
	}
	return s
}

var hlsQualityOrder = []string{"144p", "240p", "360p", "480p", "720p", "1080p", "2160p"}

type srcInfo struct {
	vCodec      string
	vHeight     int
	kfTimes     []float64
	bounds      []float64 // segment boundaries, keyframe-aligned
	segBitrates []int     // bps per segment, aligned with bounds
	peakBitrate int       // peak over all segments (bps)
	once        sync.Once
}

var srcInfoMap sync.Map // path -> *srcInfo

func getSrcInfo(path string) *srcInfo {
	v, _ := srcInfoMap.LoadOrStore(path, &srcInfo{})
	si := v.(*srcInfo)
	si.once.Do(func() {
		streams, err := library.Prober.Streams(context.Background(), path)
		if err != nil {
			return
		}
		for _, s := range streams {
			if s.CodecType == "video" && si.vCodec == "" {
				si.vCodec = s.CodecName
				si.vHeight = s.Height
			}
		}
		if si.vCodec != "h264" {
			return
		}
		pkts, err := probePackets(path)
		if err != nil || len(pkts) == 0 {
			return
		}
		for _, p := range pkts {
			if p.key {
				si.kfTimes = append(si.kfTimes, p.pts)
			}
		}
		si.bounds = segmentBoundaries(si.kfTimes, duration(path))
		si.segBitrates, si.peakBitrate = segmentBitrates(pkts, si.bounds)
	})
	return si
}

type vpkt struct {
	pts  float64
	size int
	key  bool
}

func probePackets(path string) ([]vpkt, error) {
	cmd := exec.Command("ffprobe", "-v", "error",
		"-select_streams", "v:0",
		"-show_packets",
		"-show_entries", "packet=pts_time,flags,size",
		"-of", "csv=p=0",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var pkts []vpkt
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}
		pts, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			continue
		}
		size, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}
		pkts = append(pkts, vpkt{pts: pts, size: size, key: strings.HasPrefix(parts[1], "K")})
	}
	return pkts, nil
}

// Group keyframes into segment boundaries no shorter than hlsSegDur. Returns
// [start0, start1, ..., totalDur]; segment i runs from bounds[i] to bounds[i+1].
func segmentBoundaries(kfs []float64, totalDur float64) []float64 {
	if len(kfs) == 0 {
		return nil
	}
	bounds := []float64{kfs[0]}
	last := kfs[0]
	for _, kf := range kfs[1:] {
		if kf-last >= hlsSegDur {
			bounds = append(bounds, kf)
			last = kf
		}
	}
	if totalDur > last {
		bounds = append(bounds, totalDur)
	}
	return bounds
}

// Returns per-segment video bitrate (bps) and the peak across all segments.
// pkts must be sorted by pts (ffprobe output already is).
func segmentBitrates(pkts []vpkt, bounds []float64) ([]int, int) {
	if len(bounds) < 2 {
		return nil, 0
	}
	numSegs := len(bounds) - 1
	segBytes := make([]int, numSegs)

	segIdx := 0
	for _, p := range pkts {
		if p.pts < bounds[0] {
			continue
		}
		for segIdx < numSegs-1 && p.pts >= bounds[segIdx+1] {
			segIdx++
		}
		segBytes[segIdx] += p.size
	}

	perSeg := make([]int, numSegs)
	peak := 0
	for i, b := range segBytes {
		dur := bounds[i+1] - bounds[i]
		if dur <= 0 {
			continue
		}
		perSeg[i] = int(float64(b) * 8 / dur)
		if perSeg[i] > peak {
			peak = perSeg[i]
		}
	}
	return perSeg, peak
}

func canCopyVideo(si *srcInfo, profile hlsProfile) bool {
	if si.vCodec != "h264" || profile.h != si.vHeight || len(si.kfTimes) == 0 {
		return false
	}
	// First keyframe must be near t=0 — otherwise the HLS timeline won't align
	// with the source's playback time, breaking duration/seek bookkeeping.
	return si.kfTimes[0] < 0.5
}

// BANDWIDTH for the master playlist. Copy-eligible rung uses the probed peak
// source bitrate (plus 20% for audio+TS overhead); other rungs fall back to
// the VBR-based table.
func rungBandwidth(si *srcInfo, q string, profile hlsProfile) int {
	if canCopyVideo(si, profile) && si.peakBitrate > 0 {
		return si.peakBitrate + si.peakBitrate/5
	}
	return hlsBandwidth[q]
}

// AVERAGE-BANDWIDTH: copy-mode reports the actual probed peak (pre-overhead);
// transcode rungs report the configured VBR target so ABR has a realistic
// sustained estimate instead of the padded peak we use for BANDWIDTH.
func rungAvgBandwidth(si *srcInfo, q string, profile hlsProfile) int {
	if canCopyVideo(si, profile) && si.peakBitrate > 0 {
		return si.peakBitrate
	}
	if n, err := strconv.Atoi(strings.TrimSuffix(profile.vbr, "k")); err == nil {
		return n * 1000
	}
	return hlsBandwidth[q]
}

// H.264 codec string for the STREAM-INF CODECS= attribute. Levels cover the
// segment encoder's likely output: ≤480p=L3.1, ≤720p=L3.1, ≤1080p=L4.0,
// 2160p=L5.1. All High profile (libx264 default).
func avcCodec(h int) string {
	switch {
	case h > 1080:
		return "avc1.640033"
	case h > 720:
		return "avc1.640028"
	case h > 480:
		return "avc1.64001f"
	default:
		return "avc1.640016"
	}
}

// Rung width derived from source aspect. Scale filter is "-2:H", so output
// width matches the source's pixel aspect and rounds to an even number.
func rungWidth(srcW, srcH, rungH int) int {
	if srcW <= 0 || srcH <= 0 {
		// Fall back to 16:9 when probing failed.
		w := rungH * 16 / 9
		if w%2 != 0 {
			w++
		}
		return w
	}
	w := srcW * rungH / srcH
	if w%2 != 0 {
		w++
	}
	return w
}

// Build a full #EXT-X-STREAM-INF attribute list with BANDWIDTH,
// AVERAGE-BANDWIDTH, RESOLUTION, CODECS, and any caller-supplied extras
// (e.g. AUDIO="audio" for multi-audio masters).
func streamInfLine(si *srcInfo, q string, p hlsProfile, srcW, srcH int, extra string) string {
	peak := rungBandwidth(si, q, p)
	avg := rungAvgBandwidth(si, q, p)
	if avg > peak {
		avg = peak
	}
	w := rungWidth(srcW, srcH, p.h)
	codecs := avcCodec(p.h) + ",mp4a.40.2"
	attrs := fmt.Sprintf("BANDWIDTH=%d,AVERAGE-BANDWIDTH=%d,RESOLUTION=%dx%d,CODECS=\"%s\"", peak, avg, w, p.h, codecs)
	if extra != "" {
		attrs += "," + extra
	}
	return "#EXT-X-STREAM-INF:" + attrs + "\n"
}

// Peak-bitrate hints for ABR. Copy-mode rungs substitute the probed source
// peak via rungBandwidth; these fall-back values mirror the VBR caps in
// hlsProfiles and are what ABR uses to pick a sustainable rung.
var hlsBandwidth = map[string]int{
	"144p":  200_000,
	"240p":  400_000,
	"360p":  650_000,
	"480p":  1_000_000,
	"720p":  4_000_000,
	"1080p": 8_000_000,
	"2160p": 25_000_000,
}

func HLSAVOffset(c *gin.Context, lib *library.Library) {
	file := c.Query("file")
	path := lib.FindFile(file)
	if path == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	streams, err := library.Prober.Streams(context.Background(), path)
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
	offsetMs := int((vStart - aStart) * 1000)
	c.JSON(http.StatusOK, gin.H{"offset_ms": offsetMs})
}

func HLSMaster(c *gin.Context, lib *library.Library) {
	file := c.Query("file")
	path := lib.FindFile(file)
	if path == "" {
		c.String(http.StatusNotFound, "not found")
		return
	}

	srcHeight := 0
	srcWidth := 0
	type aTrack struct {
		language string
		codec    string
		channels int
	}
	var aTracks []aTrack

	if streams, err := library.Prober.Streams(context.Background(), path); err == nil {
		for _, s := range streams {
			if s.CodecType == "video" && srcHeight == 0 {
				srcHeight = s.Height
				srcWidth = s.Width
			}
			if s.CodecType == "audio" {
				lang := s.Tags["language"]
				aTracks = append(aTracks, aTrack{language: lang, codec: s.CodecName, channels: s.Channels})
			}
		}
	}

	multiAudio := len(aTracks) > 1

	defIdx := 0
	for i, t := range aTracks {
		lang := strings.ToLower(t.language)
		if lang == "eng" || lang == "en" || lang == "english" {
			defIdx = i
			break
		}
	}

	encFile := url.QueryEscape(file)
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")

	if multiAudio {
		sb.WriteString("#EXT-X-VERSION:4\n\n")

		for i, t := range aTracks {
			def := "NO"
			if i == defIdx {
				def = "YES"
			}
			lang := t.language
			if lang == "" {
				lang = fmt.Sprintf("und%d", i)
			}
			ch := fmt.Sprintf("%dch", t.channels)
			switch t.channels {
			case 2:
				ch = "2.0"
			case 6:
				ch = "5.1"
			case 8:
				ch = "7.1"
			}
			name := fmt.Sprintf("%s — %s %s", strings.ToUpper(lang), strings.ToUpper(t.codec), ch)
			sb.WriteString(fmt.Sprintf(
				"#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio\",NAME=\"%s\",LANGUAGE=\"%s\",DEFAULT=%s,AUTOSELECT=%s,URI=\"/api/hls/playlist?file=%s&q=audio&audio=%d\"\n",
				name, lang, def, def, encFile, i,
			))
		}
		sb.WriteString("\n")

		si := getSrcInfo(path)
		for _, q := range hlsQualityOrder {
			p := hlsProfiles[q]
			if srcHeight > 0 && p.h > srcHeight {
				continue
			}
			sb.WriteString(streamInfLine(si, q, p, srcWidth, srcHeight, "AUDIO=\"audio\""))
			sb.WriteString(fmt.Sprintf("/api/hls/playlist?file=%s&q=%s\n", encFile, q))
		}
	} else {
		sb.WriteString("#EXT-X-VERSION:3\n\n")
		audio := fmt.Sprintf("%d", defIdx)
		si := getSrcInfo(path)
		for _, q := range hlsQualityOrder {
			p := hlsProfiles[q]
			if srcHeight > 0 && p.h > srcHeight {
				continue
			}
			sb.WriteString(streamInfLine(si, q, p, srcWidth, srcHeight, ""))
			sb.WriteString(fmt.Sprintf("/api/hls/playlist?file=%s&q=%s&audio=%s\n", encFile, q, audio))
		}
	}

	c.Header("Cache-Control", "no-cache")
	c.Data(http.StatusOK, "application/vnd.apple.mpegurl", []byte(sb.String()))
}

func HLSPlaylist(c *gin.Context, lib *library.Library) {
	file := c.Query("file")
	q := c.Query("q")
	audioStr, hasAudio := c.GetQuery("audio")

	audioOnly := q == "audio"
	var profile hlsProfile
	if !audioOnly {
		var ok bool
		profile, ok = hlsProfiles[q]
		if !ok {
			c.String(http.StatusBadRequest, "invalid quality")
			return
		}
	}

	path := lib.FindFile(file)
	if path == "" {
		c.String(http.StatusNotFound, "not found")
		return
	}

	dur := duration(path)
	if dur <= 0 {
		c.String(http.StatusInternalServerError, "could not probe duration")
		return
	}

	var segDurs []float64
	var segBitrates []int
	copyMode := false
	if !audioOnly {
		si := getSrcInfo(path)
		if canCopyVideo(si, profile) && len(si.bounds) >= 2 {
			copyMode = true
			for i := 0; i < len(si.bounds)-1; i++ {
				segDurs = append(segDurs, si.bounds[i+1]-si.bounds[i])
			}
			segBitrates = si.segBitrates
		}
	}
	if !copyMode {
		numSegs := int(math.Ceil(dur / hlsSegDur))
		for i := 0; i < numSegs; i++ {
			d := hlsSegDur
			if remaining := dur - float64(i)*hlsSegDur; remaining < hlsSegDur {
				d = remaining
			}
			segDurs = append(segDurs, d)
		}
	}

	target := int(hlsSegDur)
	for _, d := range segDurs {
		if t := int(math.Ceil(d)); t > target {
			target = t
		}
	}

	encFile := url.QueryEscape(file)
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")
	sb.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", target))
	sb.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	sb.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")

	modeParam := ""
	if copyMode {
		modeParam = "&m=copy"
	}

	for i, segDur := range segDurs {
		if i < len(segBitrates) && segBitrates[i] > 0 {
			sb.WriteString(fmt.Sprintf("#EXT-X-BITRATE:%d\n", segBitrates[i]/1000))
		}
		sb.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", segDur))

		if audioOnly {
			sb.WriteString(fmt.Sprintf("/api/hls/segment?file=%s&q=audio&seg=%d&audio=%s\n", encFile, i, audioStr))
		} else if !hasAudio {
			sb.WriteString(fmt.Sprintf("/api/hls/segment?file=%s&q=%s&seg=%d%s\n", encFile, q, i, modeParam))
		} else {
			sb.WriteString(fmt.Sprintf("/api/hls/segment?file=%s&q=%s&seg=%d&audio=%s%s\n", encFile, q, i, audioStr, modeParam))
		}
	}

	sb.WriteString("#EXT-X-ENDLIST\n")
	c.Header("Cache-Control", "no-cache")
	c.Data(http.StatusOK, "application/vnd.apple.mpegurl", []byte(sb.String()))
}

const (
	segMuxed     = 0
	segVideoOnly = 1
	segAudioOnly = 2
)

func HLSSegment(c *gin.Context, lib *library.Library) {
	file := c.Query("file")
	q := c.Query("q")
	segStr := c.Query("seg")
	audioStr, hasAudio := c.GetQuery("audio")
	useCopy := c.Query("m") == "copy"

	audioOnly := q == "audio"

	var segNum int
	if n, err := fmt.Sscanf(segStr, "%d", &segNum); n != 1 || err != nil || segNum < 0 {
		c.String(http.StatusBadRequest, "invalid segment")
		return
	}

	var audioIdx int
	if hasAudio {
		if n, err := fmt.Sscanf(audioStr, "%d", &audioIdx); n != 1 || err != nil || audioIdx < 0 {
			audioIdx = 0
		}
	}

	path := lib.FindFile(file)
	if path == "" {
		c.String(http.StatusNotFound, "not found")
		return
	}

	hash := library.Hash(file)
	cacheRoot := hash
	if useCopy {
		cacheRoot = filepath.Join(hash, "copy")
	}

	var segPath, cacheKey string
	var mode int

	if audioOnly {
		mode = segAudioOnly
		cacheKey = fmt.Sprintf("%s/audio/a%d/%06d", cacheRoot, audioIdx, segNum)
		segPath = filepath.Join(hlsCacheDir, cacheRoot, "audio", fmt.Sprintf("a%d", audioIdx), fmt.Sprintf("%06d.ts", segNum))
	} else if !hasAudio {
		mode = segVideoOnly
		cacheKey = fmt.Sprintf("%s/%s/v/%06d", cacheRoot, q, segNum)
		segPath = filepath.Join(hlsCacheDir, cacheRoot, q, "v", fmt.Sprintf("%06d.ts", segNum))
	} else {
		mode = segMuxed
		cacheKey = fmt.Sprintf("%s/%s/a%d/%06d", cacheRoot, q, audioIdx, segNum)
		segPath = filepath.Join(hlsCacheDir, cacheRoot, q, fmt.Sprintf("a%d", audioIdx), fmt.Sprintf("%06d.ts", segNum))
	}

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

	var p hlsProfile
	if !audioOnly {
		var ok bool
		p, ok = hlsProfiles[q]
		if !ok {
			c.String(http.StatusBadRequest, "invalid quality")
			return
		}
	}

	// Compute start/duration: keyframe-aligned for copy mode, fixed-grid otherwise
	start := float64(segNum) * hlsSegDur
	segDur := hlsSegDur
	if useCopy && !audioOnly {
		si := getSrcInfo(path)
		if segNum >= len(si.bounds)-1 {
			c.String(http.StatusBadRequest, "segment out of range")
			return
		}
		start = si.bounds[segNum]
		segDur = si.bounds[segNum+1] - si.bounds[segNum]
	}

	if err := generateSegment(path, segPath, start, segDur, p, audioIdx, mode, useCopy); err != nil {
		log.Printf("HLS segment error [%s q=%s seg=%d audio=%d mode=%d copy=%v]: %v", filepath.Base(path), q, segNum, audioIdx, mode, useCopy, err)
		c.String(http.StatusInternalServerError, fmt.Sprintf("transcode failed: %v", err))
		return
	}

	serveSegment(c, segPath)
}

func generateSegment(srcPath, segPath string, start, segDur float64, p hlsProfile, audioIdx int, mode int, useCopy bool) error {
	if err := os.MkdirAll(filepath.Dir(segPath), 0755); err != nil {
		return err
	}

	tmp := segPath + ".tmp"

	args := []string{
		"-y",
		"-ss", fmt.Sprintf("%.3f", start),
		"-i", srcPath,
		"-t", fmt.Sprintf("%.3f", segDur),
	}

	encodeArgs := []string{
		"-c:v", "libx264", "-preset", "medium",
		"-crf", strconv.Itoa(p.crf),
		"-maxrate", p.vbr, "-bufsize", doubleKRate(p.vbr),
		"-vf", p.scale,
	}

	switch mode {
	case segMuxed:
		args = append(args, "-map", "0:v:0", "-map", fmt.Sprintf("0:a:%d?", audioIdx))
		if useCopy {
			args = append(args, "-c:v", "copy")
		} else {
			args = append(args, encodeArgs...)
		}
		args = append(args,
			"-c:a", "aac", "-b:a", p.ab, "-ac", "2",
			"-af", "aresample=async=1",
		)
	case segVideoOnly:
		args = append(args, "-map", "0:v:0", "-an")
		if useCopy {
			args = append(args, "-c:v", "copy")
		} else {
			args = append(args, encodeArgs...)
		}
	case segAudioOnly:
		args = append(args,
			"-map", fmt.Sprintf("0:a:%d", audioIdx), "-vn",
			"-c:a", "aac", "-b:a", "128k", "-ac", "2",
			"-af", "aresample=async=1", // compensate A/V drift from AAC encoder priming
		)
	}

	args = append(args,
		"-output_ts_offset", fmt.Sprintf("%.3f", start), // TS timestamps must be monotonic across segments
		"-f", "mpegts", tmp,
	)

	stderr, err := library.FFRun{Args: args, Timeout: 60 * time.Second}.Run()
	if err != nil {
		os.Remove(tmp)
		return library.FFErr(stderr, err)
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
	io.Copy(c.Writer, f) // don't call c.Status() — first Write flushes headers with 200
}

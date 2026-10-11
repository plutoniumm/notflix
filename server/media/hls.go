package media

import (
	"context"
	"encoding/binary"
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

	"notflix/server/jobs"
	"notflix/server/library"
)

const (
	hlsSegDur   = 4.0
	hlsCacheDir = "./cache"
	// AV1 source-passthrough pseudo-quality; deliberately absent from every
	// profile map — no scaling, no bitrate target, just a copy.
	qSrc = "src"
	// Segment/init URLs are immutable-cached: bump on ANY change to segment or
	// init production, or clients replay stale fragments forever.
	// 8: the AV1 audio renditions were regenerated stereo on 09-29, days AFTER
	// cv=7 went live, so the same cv=7 init URL served a 5.1 moov and then a
	// stereo one. Segments/inits go out immutable for a year, so every browser
	// that touched one of those 9 titles in the window has the 5.1 init pinned
	// against stereo segments — it plays for curl and is dead in the client.
	// Bump on ANY change to segment or init BYTES, not just to the code path.
	// 9: cv=8 is itself poisoned. ensureInit's fast path was a bare os.Stat, so
	// during a cold start it served a 0-BYTE audio init under max-age=31536000 —
	// pinned in that browser for a year while the file on disk became correct
	// seconds later, so nothing server-side could ever find it. Every title
	// opened after the cache wipe was exposed. Fixed in ensureInit + HLSInit;
	// this bump evicts the empty inits already sitting in clients.
	// 10: an already-AAC <=2ch source now copies instead of re-encoding, so a
	// regenerated audio rendition carries different bytes than cv=9 served.
	hlsURLVer = "10"
)

var hlsInFlight sync.Map

// Single-flight slot; waiters inherit err, so one failed generation fails all.
type inflight struct {
	done chan struct{}
	err  error
}

// Caps concurrent live segment transcodes. Copy/remux passes are exempt.
var segPool = jobs.NewPool(4)

type hlsProfile struct {
	h              int
	scale, vbr, ab string
	crf            int // lower = better
}

// vbr doubles as the maxrate cap (bufsize = 2× vbr); CRF drives actual quality.
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

// Clamps to the SHORTER side, or portrait sources get wildly over-provisioned
// rungs (bitrates are tuned for landscape). Never empty: an empty master is unplayable.
func ladderFor(profiles map[string]hlsProfile, srcW, srcH int) []string {
	lim := srcH
	if srcW > 0 && (lim <= 0 || srcW < lim) {
		lim = srcW
	}
	var qs []string
	for _, q := range hlsQualityOrder {
		p, ok := profiles[q]
		if !ok || (lim > 0 && p.h > lim) {
			continue
		}
		qs = append(qs, q)
	}
	if len(qs) == 0 {
		for _, q := range hlsQualityOrder {
			if _, ok := profiles[q]; ok {
				return []string{q}
			}
		}
	}
	return qs
}

type srcInfo struct {
	vCodec      string
	vHeight     int      // rotation-corrected display height
	vWidth      int      // rotation-corrected display width
	vDAR        float64  // display aspect, 0 if unknown
	vBitDepth   int      // 8 / 10 / 12, 0 if unknown
	vTransfer   string   // smpte2084 / arib-std-b67 = HDR
	vPrimaries  string   // bt2020 = wide gamut
	vColorspace string   // bt2020nc — the only color field that survives SVT-AV1
	vRotation   int      // normalized 0/90/180/270
	aChannels   []int    // per audio stream, in stream order
	aLayouts    []string // empty/unknown → unnamed layout, AAC rejects it
	aMeta       []audioMeta
	vPixFmt     string
	dur         float64 // 0 if unprobed
	kfTimes     []float64
	bounds      []float64 // keyframe-aligned segment boundaries
	segBitrates []int     // bps, aligned with bounds
	peakBitrate int       // bps, max over segments — HLS BANDWIDTH
	avgBitrate  int       // bps, whole-file mean — HLS AVERAGE-BANDWIDTH
	mu          sync.Mutex
	ready       bool
}

var srcInfoMap sync.Map // path -> *srcInfo

func pixBitDepth(pf string) int {
	pf = strings.ToLower(pf)
	switch {
	case strings.Contains(pf, "p016") || strings.Contains(pf, "16le") || strings.Contains(pf, "16be"):
		return 12
	case strings.Contains(pf, "p012") || strings.Contains(pf, "12le") || strings.Contains(pf, "12be"):
		return 12
	case strings.Contains(pf, "p010") || strings.Contains(pf, "10le") || strings.Contains(pf, "10be"):
		return 10
	case pf == "":
		return 0
	default:
		return 8
	}
}

// displaymatrix needs its own ffprobe (astiffprobe hides side_data); tags.rotate
// is the legacy fallback. ffmpeg autorotates pixels — only 90/270 parity matters.
func normRotation(path, tagRotate string) int {
	norm := func(v int) int { return ((v % 360) + 360) % 360 }

	raw, _ := exec.Command("ffprobe", "-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream_side_data=rotation",
		"-of", "default=noprint_wrappers=1:nokey=1", path,
	).Output()
	for _, line := range strings.Split(string(raw), "\n") {
		if v, err := strconv.Atoi(strings.TrimSpace(line)); err == nil {
			return norm(v)
		}
	}
	if v, err := strconv.Atoi(strings.TrimSpace(tagRotate)); err == nil {
		return norm(v)
	}
	return 0
}

// Layouts the AAC encoder accepts; anything else (incl. the generic "unknown" /
// "6 channels") fails the encode. Lower-cased to match ffprobe output.
var namedLayouts = map[string]bool{
	"mono": true, "stereo": true, "2.1": true,
	"3.0": true, "3.0(back)": true,
	"4.0": true, "quad": true, "quad(side)": true,
	"5.0": true, "5.0(side)": true,
	"5.1": true, "5.1(side)": true,
	"6.0": true, "6.0(front)": true, "hexagonal": true,
	"6.1": true, "6.1(back)": true, "6.1(front)": true,
	"7.0": true, "7.0(front)": true,
	"7.1": true, "7.1(wide)": true, "7.1(wide-side)": true,
	"octagonal": true, "downmix": true,
}

// Keeps a multichannel layout only when the source declares a named one the AAC
// encoder accepts; unnamed multichannel must downmix or the segment fails outright.
func aacArgs(si *srcInfo, audioIdx int, stereoBitrate string) []string {
	ch, layout := 0, ""
	if si != nil && audioIdx >= 0 && audioIdx < len(si.aChannels) {
		ch = si.aChannels[audioIdx]
		if audioIdx < len(si.aLayouts) {
			layout = si.aLayouts[audioIdx]
		}
	}
	if ch > 2 && namedLayouts[layout] {
		br := "256k"
		if ch >= 8 {
			br = "384k"
		}
		return []string{"-c:a", "aac", "-b:a", br}
	}
	return []string{"-c:a", "aac", "-b:a", stereoBitrate, "-ac", "2"}
}

// Convert's whole-file re-encode maps all audio streams uniformly, so ANY
// unnamed multichannel stream forces -ac 2 on the lot.
func anyUnnamedMultichannel(si *srcInfo) bool {
	if si == nil {
		return false
	}
	for i, ch := range si.aChannels {
		layout := ""
		if i < len(si.aLayouts) {
			layout = si.aLayouts[i]
		}
		if ch > 2 && !namedLayouts[layout] {
			return true
		}
	}
	return false
}

func isHDR(si *srcInfo) bool {
	switch si.vTransfer {
	case "smpte2084", "arib-std-b67":
		return true
	}
	// SVT-AV1 drops transfer/primaries but keeps the matrix, so bt2020 in any
	// color field is the only HDR signal that survives a re-encode.
	return strings.HasPrefix(si.vPrimaries, "bt2020") ||
		strings.HasPrefix(si.vColorspace, "bt2020")
}

// Caches only SUCCESSFUL probes: a sync.Once here froze failures (ffprobe under
// the startup ProcessAll storm) and condemned copy-eligible AV1 to the h264 ladder.
func getSrcInfo(path string) *srcInfo {
	v, _ := srcInfoMap.LoadOrStore(path, &srcInfo{})
	si := v.(*srcInfo)
	si.mu.Lock()
	defer si.mu.Unlock()
	if !si.ready {
		streams, err := library.Prober.Streams(context.Background(), path)
		if err != nil {
			log.Printf("[srcinfo] stream probe failed (will retry): %s: %v", filepath.Base(path), err)
			return si
		}
		si.vCodec = ""
		si.aChannels, si.aLayouts, si.aMeta, si.kfTimes = nil, nil, nil, nil
		for _, s := range streams {
			if s.CodecType == "video" && si.vCodec == "" {
				si.vCodec = s.CodecName
				si.vPixFmt = s.PixFmt
				si.vBitDepth = pixBitDepth(s.PixFmt)
				si.vTransfer = strings.ToLower(s.ColorTransfer)
				si.vPrimaries = strings.ToLower(s.ColorPrimaries)
				si.vColorspace = strings.ToLower(s.ColorSpace)
				si.vRotation = normRotation(path, s.Tags["rotate"])

				w, h := s.Width, s.Height
				sn, sd := s.SampleAspectRatio.Num(), s.SampleAspectRatio.Den()
				if sn <= 0 || sd <= 0 {
					sn, sd = 1, 1
				}
				// Anamorphic sources (SAR ≠ 1:1) MUST be deanamorphized for
				// HLS or players render the coded grid stretched.
				dar := 0.0
				if w > 0 && h > 0 {
					dar = float64(w) * float64(sn) / (float64(h) * float64(sd))
				}
				// Coded dims are pre-rotation; everything downstream must use
				// the rotated display grid or phone video comes out sideways.
				if si.vRotation == 90 || si.vRotation == 270 {
					w, h = h, w
					if dar > 0 {
						dar = 1 / dar
					}
				}
				si.vWidth, si.vHeight, si.vDAR = w, h, dar
			}
			if s.CodecType == "audio" {
				si.aChannels = append(si.aChannels, s.Channels)
				si.aLayouts = append(si.aLayouts, strings.ToLower(s.ChannelLayout))
				si.aMeta = append(si.aMeta, audioMeta{language: s.Tags["language"], codec: s.CodecName, channels: s.Channels})
			}
		}
		if si.vCodec == "" {
			log.Printf("[srcinfo] stream probe returned no video stream (will retry): %s", filepath.Base(path))
			return si
		}
		si.dur = duration(path)
		if si.vCodec != "h264" && si.vCodec != "av1" {
			si.ready = true
			return si
		}
		pkts, err := probePackets(path)
		if err != nil || len(pkts) == 0 {
			log.Printf("[srcinfo] packet probe failed (will retry): %s: %v (%d pkts)", filepath.Base(path), err, len(pkts))
			return si
		}
		for _, p := range pkts {
			if p.key {
				si.kfTimes = append(si.kfTimes, p.pts)
			}
		}
		si.bounds = muxerBounds(si.kfTimes, pkts[0].pts, si.dur, hlsSegDur)
		si.segBitrates, si.peakBitrate, si.avgBitrate = segmentBitrates(pkts, si.bounds)
		si.ready = true
	}
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
		size, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		pkts = append(pkts, vpkt{pts: pts, size: size, key: strings.HasPrefix(parts[2], "K")})
	}
	return pkts, nil
}

// Boundaries ffmpeg's hls muxer cuts a `-hls_time segDur` copy pass at.
// hlsenc.c's rule is ABSOLUTE, not relative — first keyframe with
// `pts - origin >= segDur*n` on a fixed grid from the file start. Relative
// cutting agrees only when every GOP ≥ segDur; short GOPs otherwise ENDLIST
// early with segments holding the wrong time range. origin is the first
// packet's pts, not kfs[0].
func muxerBounds(kfs []float64, origin, totalDur, segDur float64) []float64 {
	if len(kfs) == 0 {
		return nil
	}
	bounds := []float64{kfs[0]}
	for _, kf := range kfs[1:] {
		if kf-origin >= segDur*float64(len(bounds)) {
			bounds = append(bounds, kf)
		}
	}
	if last := bounds[len(bounds)-1]; totalDur > last {
		bounds = append(bounds, totalDur)
	}
	return bounds
}

// -hls_time for the AV1 audio pass. Audio has no keyframes, so the muxer cuts
// uniformly at step*n; reusing the video grid's first segment keeps both
// renditions on the same instants for the uniform-GOP sources we produce.
func audioStep(si *srcInfo) float64 {
	if len(si.bounds) >= 2 {
		return si.bounds[1] - si.bounds[0]
	}
	return hlsSegDur
}

func srcDur(si *srcInfo, path string) float64 {
	if si.dur > 0 {
		return si.dur
	}
	return duration(path)
}

// Deliberately floor-ish: ffmpeg can emit a sub-frame runt past the declared
// end, and an unreferenced extra file beats a referenced missing one.
func uniformDurs(dur, step float64) []float64 {
	var ds []float64
	for i := 0; i < int(math.Ceil(dur/step)); i++ {
		d := step
		if rem := dur - float64(i)*step; rem < step {
			d = rem
		}
		ds = append(ds, d)
	}
	return ds
}

// pkts must be sorted by pts (ffprobe output already is).
// Returns per-segment rates, the PEAK (HLS BANDWIDTH) and the whole-file MEAN
// (HLS AVERAGE-BANDWIDTH). These are genuinely far apart on an efficient AV1
// encode: Obsession (4K AV1) averages 1.57 Mbps but has one real 6.7s segment
// holding 26 MB = 31.4 Mbps. Reporting the peak as the average told the player
// the stream needed 31 Mbps sustained and it buffered accordingly.
func segmentBitrates(pkts []vpkt, bounds []float64) ([]int, int, int) {
	if len(bounds) < 2 {
		return nil, 0, 0
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
	peak, totBytes, totDur := 0, 0, 0.0
	for i, b := range segBytes {
		dur := bounds[i+1] - bounds[i]
		if dur <= 0 {
			continue
		}
		perSeg[i] = int(float64(b) * 8 / dur)
		if perSeg[i] > peak {
			peak = perSeg[i]
		}
		totBytes += b
		totDur += dur
	}
	avg := 0
	if totDur > 0 {
		avg = int(float64(totBytes) * 8 / totDur)
	}
	return perSeg, peak, avg
}

// h264 source-passthrough copy is DISABLED: generateSegment's copy mode can't
// trim mid-GOP, so segments overshoot their EXTINF and accumulate MINUTES of
// drift over a feature. Transcode is exact and hardware-fast, so ALL h264 goes
// through it. Params kept so the bandwidth/playlist callers compile unchanged.
func canCopyVideo(si *srcInfo, profile hlsProfile) bool {
	_ = si
	_ = profile
	return false
}

// BANDWIDTH: copy-eligible rungs report the probed peak +20% for audio+TS overhead.
func rungBandwidth(si *srcInfo, q string, profile hlsProfile) int {
	if canCopyVideo(si, profile) && si.peakBitrate > 0 {
		return si.peakBitrate + si.peakBitrate/5
	}
	return hlsBandwidth[q]
}

// AVERAGE-BANDWIDTH: transcode rungs report the VBR target so ABR gets a
// sustained estimate rather than BANDWIDTH's padded peak.
func rungAvgBandwidth(si *srcInfo, q string, profile hlsProfile) int {
	if canCopyVideo(si, profile) && si.peakBitrate > 0 {
		return si.peakBitrate
	}
	if n, err := strconv.Atoi(strings.TrimSuffix(profile.vbr, "k")); err == nil {
		return n * 1000
	}
	return hlsBandwidth[q]
}

// CODECS= for STREAM-INF. All High profile (libx264 default), level by height.
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

func evenWidth(w int) int {
	if w < 2 {
		w = 2
	}
	if w%2 != 0 {
		w++
	}
	return w
}

// Square-pixel width preserving the source's DISPLAY aspect (SAR-corrected).
func rungWidth(si *srcInfo, srcW, srcH, rungH int) int {
	dar := 0.0
	if si != nil {
		dar = si.vDAR
	}
	if dar <= 0 && srcW > 0 && srcH > 0 {
		dar = float64(srcW) / float64(srcH)
	}
	if dar <= 0 {
		dar = 16.0 / 9.0
	}
	return evenWidth(int(math.Round(float64(rungH) * dar)))
}

// Deanamorphizes explicitly (display width + setsar=1): hls.js's transmuxed
// MPEG-TS ignores in-stream SAR, which stretched anamorphic films.
func scaleFilter(si *srcInfo, rungH int) string {
	if si == nil || si.vDAR <= 0 {
		return fmt.Sprintf("scale=-2:%d", rungH)
	}
	return fmt.Sprintf("scale=%d:%d:flags=lanczos,setsar=1",
		evenWidth(int(math.Round(float64(rungH)*si.vDAR))), rungH)
}

func streamInfLine(si *srcInfo, q string, p hlsProfile, codec string, srcW, srcH int, extra string) string {
	var peak, avg int
	var codecs string
	if codec == CodecAV1 {
		if n, err := strconv.Atoi(strings.TrimSuffix(p.vbr, "k")); err == nil {
			avg = n * 1000
		}
		peak = avg + avg/5
		codecs = av1Codec(p.h, 8) + ",mp4a.40.2"
	} else {
		peak = rungBandwidth(si, q, p)
		avg = rungAvgBandwidth(si, q, p)
		if avg > peak {
			avg = peak
		}
		codecs = avcCodec(p.h) + ",mp4a.40.2"
	}
	w := rungWidth(si, srcW, srcH, p.h)
	attrs := fmt.Sprintf("BANDWIDTH=%d,AVERAGE-BANDWIDTH=%d,RESOLUTION=%dx%d,CODECS=\"%s\"", peak, avg, w, p.h, codecs)
	if extra != "" {
		attrs += "," + extra
	}
	return "#EXT-X-STREAM-INF:" + attrs + "\n"
}

// ABR fallback peaks, mirroring the hlsProfiles VBR caps.
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

	// positive → audio ahead of video → delay audio by this much
	offsetMs := int((vStart - aStart) * 1000)
	c.JSON(http.StatusOK, gin.H{"offset_ms": offsetMs})
}

// The player answers authoritatively via &av1=1/0; the UA guess is deliberately
// lopsided because iOS plays HLS NATIVELY (no MSE capability check to catch an
// undecodable codec — just a bare error overlay). So all Apple UAs fall back to
// h264, and only desktop Chromium/Firefox (dav1d) are assumed AV1-capable.
func av1Client(c *gin.Context) bool {
	switch c.Query("av1") {
	case "1":
		return true
	case "0":
		return false
	}

	ua := strings.ToLower(c.GetHeader("User-Agent"))
	if strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad") || strings.Contains(ua, "ipod") {
		return false
	}

	for _, engine := range []string{"chrome", "chromium", "firefox", "edg/"} {
		if strings.Contains(ua, engine) {
			return true
		}
	}

	return false
}

func HLSMaster(c *gin.Context, lib *library.Library) {
	file := c.Query("file")
	path := lib.FindFile(file)
	if path == "" {
		c.String(http.StatusNotFound, "not found")
		return
	}

	// Dims and audio tracks both come from the success-gated probe: an unchecked
	// per-request probe read one failure as "zero audio" and silently downgraded AV1.
	srcInfoP := getSrcInfo(path)
	srcWidth, srcHeight := srcInfoP.vWidth, srcInfoP.vHeight
	aTracks := srcInfoP.aMeta

	multiAudio := len(aTracks) > 1

	codec := MediaCodec(file)
	// The ONLY per-client dimension of the master; everything it points at is
	// content-addressed, so a downgraded client shares the h264 cache.
	if codec == CodecAV1 && !av1Client(c) {
		codec = CodecH264
	}
	// AV1 multi-audio would need one fMP4 rendition per track; unsupported.
	if codec == CodecAV1 && multiAudio {
		codec = CodecH264
	}
	// A zero-audio AV1 demux emits an audio rendition whose non-optional 0:a:0
	// mapping fails → hls.js stalls. h264's optional 0:a:? plays video-only.
	if codec == CodecAV1 && len(aTracks) == 0 {
		codec = CodecH264
	}
	// AV1 serves only via copy-passthrough; anything not copy-eligible takes the
	// h264 ladder rather than a rung needing a janky live AV1 transcode.
	if codec == CodecAV1 && !canCopyAV1(getSrcInfo(path)) {
		codec = CodecH264
	}
	profiles := hlsProfiles
	codecParam := ""

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

	if codec == CodecAV1 {
		// Demuxed CMAF: hls.js can't demux fMP4, so the native-res AV1 video copy
		// and the AAC audio ship as separate single-track renditions, cut on the
		// SAME keyframe boundaries.
		si := getSrcInfo(path)
		// BANDWIDTH is the peak segment rate, AVERAGE-BANDWIDTH the mean. Both
		// used to be the peak (with BANDWIDTH inflated a further 20%).
		peak, avg := si.peakBitrate, si.avgBitrate
		if peak <= 0 {
			peak = 6_000_000
		}
		if avg <= 0 || avg > peak {
			avg = peak
		}
		codecs := av1Codec(srcHeight, si.vBitDepth) + ",mp4a.40.2"

		lang := "und"
		if defIdx < len(aTracks) && aTracks[defIdx].language != "" {
			lang = aTracks[defIdx].language
		}
		sb.WriteString("#EXT-X-VERSION:6\n\n")
		sb.WriteString(fmt.Sprintf(
			"#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"aud\",NAME=\"%s\",LANGUAGE=\"%s\",DEFAULT=YES,AUTOSELECT=YES,URI=\"/api/hls/playlist?file=%s&q=audio&audio=%d&codec=av1\"\n",
			strings.ToUpper(lang), lang, encFile, defIdx))
		sb.WriteString(fmt.Sprintf(
			"#EXT-X-STREAM-INF:BANDWIDTH=%d,AVERAGE-BANDWIDTH=%d,RESOLUTION=%dx%d,CODECS=\"%s\",AUDIO=\"aud\"\n",
			peak, avg, rungWidth(si, srcWidth, srcHeight, srcHeight), srcHeight, codecs))
		sb.WriteString(fmt.Sprintf("/api/hls/playlist?file=%s&q=%s&codec=av1\n",
			encFile, qSrc))
	} else if multiAudio {
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
		for _, q := range ladderFor(profiles, srcWidth, srcHeight) {
			p := profiles[q]
			sb.WriteString(streamInfLine(si, q, p, codec, srcWidth, srcHeight, "AUDIO=\"audio\""))
			sb.WriteString(fmt.Sprintf("/api/hls/playlist?file=%s&q=%s%s\n", encFile, q, codecParam))
		}
	} else {
		sb.WriteString("#EXT-X-VERSION:3\n\n")
		audio := fmt.Sprintf("%d", defIdx)
		si := getSrcInfo(path)
		for _, q := range ladderFor(profiles, srcWidth, srcHeight) {
			p := profiles[q]
			sb.WriteString(streamInfLine(si, q, p, codec, srcWidth, srcHeight, ""))
			sb.WriteString(fmt.Sprintf("/api/hls/playlist?file=%s&q=%s&audio=%s%s\n", encFile, q, audio, codecParam))
		}
	}

	c.Header("Cache-Control", "no-cache")
	c.Header("Vary", "User-Agent") // the master's codec choice depends on it
	c.Data(http.StatusOK, "application/vnd.apple.mpegurl", []byte(sb.String()))

	go Prewarm(file, path, codec, srcHeight, aTracks, defIdx)
}

func HLSPlaylist(c *gin.Context, lib *library.Library) {
	file := c.Query("file")
	q := c.Query("q")
	audioStr, hasAudio := c.GetQuery("audio")
	codec := c.DefaultQuery("codec", CodecH264)
	if codec != CodecAV1 {
		codec = CodecH264
	}

	audioOnly := q == "audio"
	// hls.js demuxes TS itself but not fMP4, so an AV1 title's audio must stay its
	// own fMP4 rendition — only the non-AV1 audio playlist is forced to h264.
	if audioOnly && codec != CodecAV1 {
		codec = CodecH264
	}

	// AV1 is copy-passthrough (q=qSrc); profiles exist only for the h264 ladder.
	var profile hlsProfile
	if !audioOnly && codec == CodecH264 {
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
	si := getSrcInfo(path)
	if codec == CodecAV1 {
		// Each rendition is declared on the grid its own pass actually writes:
		// video on the muxer's keyframe grid, audio on the uniform audioStep grid.
		if !canCopyAV1(si) || len(si.bounds) < 2 {
			c.String(http.StatusInternalServerError, "av1 source not copy-eligible")
			return
		}
		copyMode = true
	} else if !audioOnly && canCopyVideo(si, profile) && len(si.bounds) >= 2 {
		copyMode = true
	}
	switch {
	case copyMode && audioOnly:
		segDurs = uniformDurs(dur, audioStep(si))
	case copyMode:
		for i := 0; i < len(si.bounds)-1; i++ {
			segDurs = append(segDurs, si.bounds[i+1]-si.bounds[i])
		}
		segBitrates = si.segBitrates // bitrate hint is video-only
	default:
		segDurs = uniformDurs(dur, hlsSegDur)
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
	if codec == CodecAV1 {
		// fMP4 segments require HLS protocol v6+.
		sb.WriteString("#EXT-X-VERSION:6\n")
	} else {
		sb.WriteString("#EXT-X-VERSION:3\n")
	}
	sb.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", target))
	sb.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	sb.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")

	if codec == CodecAV1 {
		// Single-track inits: AAC-only moov for audio, av01-only for the video rung.
		if audioOnly {
			sb.WriteString(fmt.Sprintf("#EXT-X-MAP:URI=\"/api/hls/init?file=%s&q=audio&audio=%s&codec=av1&m=copy&cv=%s\"\n",
				encFile, audioStr, hlsURLVer))
		} else {
			sb.WriteString(fmt.Sprintf("#EXT-X-MAP:URI=\"/api/hls/init?file=%s&q=%s&codec=av1&m=copy&cv=%s\"\n",
				encFile, q, hlsURLVer))
		}
	}

	modeParam := ""
	if copyMode {
		modeParam = "&m=copy"
	}
	codecParam := ""
	if codec == CodecAV1 {
		codecParam = "&codec=av1"
	}
	cv := "&cv=" + hlsURLVer // cache-bust immutable segment URLs

	for i, segDur := range segDurs {
		if i < len(segBitrates) && segBitrates[i] > 0 {
			sb.WriteString(fmt.Sprintf("#EXT-X-BITRATE:%d\n", segBitrates[i]/1000))
		}
		sb.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", segDur))

		if audioOnly {
			// h264 → bare TS; AV1 → &m=copy&codec=av1 fMP4 AAC on the video boundaries.
			sb.WriteString(fmt.Sprintf("/api/hls/segment?file=%s&q=audio&seg=%d&audio=%s%s%s%s\n", encFile, i, audioStr, modeParam, codecParam, cv))
		} else if !hasAudio {
			sb.WriteString(fmt.Sprintf("/api/hls/segment?file=%s&q=%s&seg=%d%s%s%s\n", encFile, q, i, modeParam, codecParam, cv))
		} else {
			sb.WriteString(fmt.Sprintf("/api/hls/segment?file=%s&q=%s&seg=%d&audio=%s%s%s%s\n", encFile, q, i, audioStr, modeParam, codecParam, cv))
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

// segDur is stamped into the path so a future hlsSegDur tweak invalidates the
// old layout instead of serving 4s segments to a player expecting 10s ones.
func segPaths(hash, codec, q string, segNum, audioIdx, mode int, useCopy bool) (string, string) {
	dKey := fmt.Sprintf("d%d", int(hlsSegDur))
	cacheRoot := filepath.Join(hash, dKey)
	switch {
	case useCopy && codec == CodecAV1:
		cacheRoot = filepath.Join(hash, dKey+"-av1-copy")
	case useCopy:
		cacheRoot = filepath.Join(hash, dKey+"-copy")
	case codec == CodecAV1:
		cacheRoot = filepath.Join(hash, dKey+"-av1")
	}
	// AV1 is demuxed fMP4 (.m4s) for both renditions; h264 stays muxed TS.
	ext := ".ts"
	if codec == CodecAV1 {
		ext = ".m4s"
	}
	switch mode {
	case segAudioOnly:
		return filepath.Join(hlsCacheDir, cacheRoot, "audio", fmt.Sprintf("a%d", audioIdx), fmt.Sprintf("%06d%s", segNum, ext)),
			fmt.Sprintf("%s/audio/a%d/%06d", cacheRoot, audioIdx, segNum)
	case segVideoOnly:
		return filepath.Join(hlsCacheDir, cacheRoot, q, "v", fmt.Sprintf("%06d%s", segNum, ext)),
			fmt.Sprintf("%s/%s/v/%06d", cacheRoot, q, segNum)
	default: // segMuxed
		return filepath.Join(hlsCacheDir, cacheRoot, q, fmt.Sprintf("a%d", audioIdx), fmt.Sprintf("%06d%s", segNum, ext)),
			fmt.Sprintf("%s/%s/a%d/%06d", cacheRoot, q, audioIdx, segNum)
	}
}

// Co-located with the media segments so cache eviction removes them together.
func initPath(hash, q string, audioIdx int, useCopy bool) string {
	dKey := fmt.Sprintf("d%d", int(hlsSegDur))
	sub := dKey + "-av1"
	if useCopy {
		sub = dKey + "-av1-copy"
	}
	return filepath.Join(hlsCacheDir, filepath.Join(hash, sub), q, fmt.Sprintf("a%d", audioIdx), "init.m4s")
}

func ensureSegment(srcPath, hash, codec, q string, segNum, audioIdx, mode int, useCopy bool) (segPath string, err error) {
	segPath, cacheKey := segPaths(hash, codec, q, segNum, audioIdx, mode, useCopy)

	if _, err := os.Stat(segPath); err == nil {
		return segPath, nil
	}

	fl := &inflight{done: make(chan struct{})}
	if actual, loaded := hlsInFlight.LoadOrStore(cacheKey, fl); loaded {
		other := actual.(*inflight)
		select {
		case <-other.done:
			return segPath, other.err
		case <-time.After(90 * time.Second):
			return segPath, fmt.Errorf("segment generation timed out")
		}
	}

	defer func() {
		fl.err = err
		hlsInFlight.Delete(cacheKey)
		close(fl.done)
	}()

	// Copy-passthrough is a remux, so it needs no profile.
	avCopy := codec == CodecAV1 && useCopy
	var p hlsProfile
	if mode != segAudioOnly && !avCopy {
		var ok bool
		if codec == CodecAV1 {
			p, ok = av1Profiles[q]
		} else {
			p, ok = hlsProfiles[q]
		}
		if !ok {
			return segPath, fmt.Errorf("invalid quality %q for codec %q", q, codec)
		}
	}

	si := getSrcInfo(srcPath)
	start := float64(segNum) * hlsSegDur
	segDur := hlsSegDur
	switch {
	case avCopy && mode == segAudioOnly:
		// Audio rides its own uniform grid: indexing si.bounds here would make the
		// continuous pass's -hls_time depend on which segment was asked for first.
		segDur = audioStep(si)
		if segNum >= len(uniformDurs(srcDur(si, srcPath), segDur)) {
			return segPath, fmt.Errorf("segment %d out of range", segNum)
		}
	case useCopy && mode != segAudioOnly:
		if segNum >= len(si.bounds)-1 {
			return segPath, fmt.Errorf("segment %d out of range", segNum)
		}
		start = si.bounds[segNum]
		segDur = si.bounds[segNum+1] - si.bounds[segNum]
	default:
		// Out-of-range would spawn a real ffmpeg for media that doesn't exist.
		if n := len(uniformDurs(srcDur(si, srcPath), hlsSegDur)); n > 0 && segNum >= n {
			return segPath, fmt.Errorf("segment %d out of range", segNum)
		}
	}

	if avCopy && mode == segAudioOnly {
		// One continuous pass: per-segment AAC priming reproducibly died ~seg 367.
		// It outruns playback, so just wait for this segment's file to land.
		err = ensureAV1Audio(srcPath, hash, audioIdx, segDur)
		if err == nil {
			err = waitForFile(segPath, 120*time.Second)
		}
	} else if avCopy {
		// One continuous -c:v copy pass: per-segment -ss+copy snapped every segment
		// to the previous keyframe (the audio-drift bug).
		err = ensureAV1Video(srcPath, hash, hlsSegDur)
		if err == nil {
			err = waitForFile(segPath, 120*time.Second)
		}
	} else {
		// Live transcodes only: one ffmpeg per SEGMENT, and a seek fans out a
		// burst, so past a handful concurrently nothing lands on time.
		segPool.Acquire()
		defer segPool.Release()

		if codec == CodecAV1 && mode != segAudioOnly {
			err = generateAV1Segment(srcPath, segPath, start, segDur, p, audioIdx, mode)
		} else {
			err = generateSegment(srcPath, segPath, start, segDur, p, audioIdx, mode, useCopy)
		}
	}
	if err != nil {
		log.Printf("HLS segment error [%s codec=%s q=%s seg=%d audio=%d mode=%d copy=%v]: %v", filepath.Base(srcPath), codec, q, segNum, audioIdx, mode, useCopy, err)
		return segPath, err
	}
	return segPath, nil
}

func HLSSegment(c *gin.Context, lib *library.Library) {
	file := c.Query("file")
	q := c.Query("q")
	segStr := c.Query("seg")
	audioStr, hasAudio := c.GetQuery("audio")
	useCopy := c.Query("m") == "copy"
	codec := c.DefaultQuery("codec", CodecH264)
	if codec != CodecAV1 {
		codec = CodecH264
	}

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

	mode := segMuxed
	if audioOnly {
		mode = segAudioOnly
	} else if !hasAudio {
		mode = segVideoOnly
	}

	segPath, err := ensureSegment(path, library.Hash(file), codec, q, segNum, audioIdx, mode, useCopy)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("transcode failed: %v", err))
		return
	}
	serveSegment(c, segPath, codec)
}

// codec must be the one the master just served THIS client — warming AV1 for a
// downgraded client heats a cache it will never read.
func Prewarm(file, path, codec string, srcHeight int, aTracks []audioMeta, defAudio int) {
	const N = 10
	multiAudio := len(aTracks) > 1
	// These three must mirror HLSMaster's AV1→h264 fallbacks exactly.
	if codec == CodecAV1 && multiAudio {
		codec = CodecH264
	}
	if codec == CodecAV1 && len(aTracks) == 0 {
		codec = CodecH264
	}
	if codec == CodecAV1 && !canCopyAV1(getSrcInfo(path)) {
		codec = CodecH264
	}
	hash := library.Hash(file)

	if codec == CodecAV1 {
		// Kick both continuous passes; each flushes init + segments far ahead of playback.
		go ensureAV1Audio(path, hash, defAudio, audioStep(getSrcInfo(path)))
		go ensureAV1Video(path, hash, hlsSegDur)
		return
	}

	q := pickPrewarmRung(srcHeight, codec)
	if q == "" {
		return
	}

	si := getSrcInfo(path)
	useCopy := canCopyVideo(si, hlsProfiles[q])

	for seg := 0; seg < N; seg++ {
		s := seg
		if multiAudio {
			go ensureSegment(path, hash, codec, q, s, 0, segVideoOnly, useCopy)
			go ensureSegment(path, hash, codec, "", s, defAudio, segAudioOnly, false)
		} else {
			go ensureSegment(path, hash, codec, q, s, defAudio, segMuxed, useCopy)
		}
	}
}

func pickPrewarmRung(srcHeight int, codec string) string {
	if srcHeight == 0 {
		return "1080p"
	}
	profiles := hlsProfiles
	if codec == CodecAV1 {
		profiles = av1Profiles
	}
	best := ""
	for _, q := range hlsQualityOrder {
		if p, ok := profiles[q]; ok && p.h <= srcHeight {
			best = q
		}
	}
	return best
}

type audioMeta struct {
	language string
	codec    string
	channels int
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

	p.scale = scaleFilter(getSrcInfo(srcPath), p.h)
	encodeArgs := h264HLS(p)

	switch mode {
	case segMuxed:
		args = append(args, "-map", "0:v:0", "-map", fmt.Sprintf("0:a:%d?", audioIdx))
		if useCopy {
			args = append(args, "-c:v", "copy")
		} else {
			args = append(args, encodeArgs...)
		}
		args = append(args, aacArgs(getSrcInfo(srcPath), audioIdx, p.ab)...)
		args = append(args, "-af", "aresample=async=1")
	case segVideoOnly:
		args = append(args, "-map", "0:v:0", "-an")
		if useCopy {
			args = append(args, "-c:v", "copy")
		} else {
			args = append(args, encodeArgs...)
		}
	case segAudioOnly:
		args = append(args, "-map", fmt.Sprintf("0:a:%d", audioIdx), "-vn")
		args = append(args, aacArgs(getSrcInfo(srcPath), audioIdx, "128k")...)
		args = append(args, "-af", "aresample=async=1") // A/V drift from AAC priming
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

func serveSegment(c *gin.Context, segPath, codec string) {
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

	c.Header("Content-Type", segMime(codec))
	c.Header("Content-Length", fmt.Sprintf("%d", info.Size()))
	c.Header("Cache-Control", "public, max-age=31536000")
	io.Copy(c.Writer, f) // don't call c.Status() — first Write flushes headers with 200
}

// The HLS-fmp4 muxer writes init+seg+playlist into a scratch dir; only the seg
// is kept.
func generateAV1Segment(srcPath, segPath string, start, segDur float64, p hlsProfile, audioIdx int, mode int) error {
	if err := os.MkdirAll(filepath.Dir(segPath), 0755); err != nil {
		return err
	}

	work := segPath + ".work"
	if err := os.MkdirAll(work, 0755); err != nil {
		return err
	}
	defer os.RemoveAll(work)

	args := []string{
		"-y",
		"-ss", fmt.Sprintf("%.3f", start),
		"-i", srcPath,
		"-t", fmt.Sprintf("%.3f", segDur),
	}

	p.scale = scaleFilter(getSrcInfo(srcPath), p.h)
	encArgs := av1HLS(p)
	if encArgs == nil {
		return fmt.Errorf("no AV1 encoder available")
	}

	switch mode {
	case segMuxed:
		args = append(args, "-map", "0:v:0", "-map", fmt.Sprintf("0:a:%d?", audioIdx))
		args = append(args, encArgs...)
		args = append(args, aacArgs(getSrcInfo(srcPath), audioIdx, p.ab)...)
		args = append(args, "-af", "aresample=async=1")
	case segVideoOnly:
		args = append(args, "-map", "0:v:0", "-an")
		args = append(args, encArgs...)
	default:
		return fmt.Errorf("av1 path does not support mode %d", mode)
	}

	args = append(args,
		"-output_ts_offset", fmt.Sprintf("%.3f", start),
		"-f", "hls",
		"-hls_segment_type", "fmp4",
		"-hls_fmp4_init_filename", "init.m4s",
		"-hls_segment_filename", filepath.Join(work, "seg%d.m4s"),
		"-hls_time", "9999",
		filepath.Join(work, "playlist.m3u8"),
	)

	stderr, err := library.FFRun{Args: args, Timeout: codecTimeout("libsvtav1")}.Run()
	if err != nil {
		return library.FFErr(stderr, err)
	}

	src := filepath.Join(work, "seg0.m4s")
	if _, statErr := os.Stat(src); statErr != nil {
		return fmt.Errorf("av1 segment not produced: %w", statErr)
	}
	return os.Rename(src, segPath)
}

// One ffmpeg run per (file, q, audioIdx) — cached forever after.
func generateAV1Init(srcPath, initFile string, p hlsProfile, audioIdx int, mode int) error {
	if err := os.MkdirAll(filepath.Dir(initFile), 0755); err != nil {
		return err
	}

	work := initFile + ".work"
	if err := os.MkdirAll(work, 0755); err != nil {
		return err
	}
	defer os.RemoveAll(work)

	// 1s is enough for a complete moov; the media itself is discarded.
	args := []string{"-y", "-i", srcPath, "-t", "1.0"}

	p.scale = scaleFilter(getSrcInfo(srcPath), p.h)
	encArgs := av1HLS(p)
	if encArgs == nil {
		return fmt.Errorf("no AV1 encoder available")
	}

	switch mode {
	case segMuxed:
		args = append(args, "-map", "0:v:0", "-map", fmt.Sprintf("0:a:%d?", audioIdx))
		args = append(args, encArgs...)
		args = append(args, aacArgs(getSrcInfo(srcPath), audioIdx, p.ab)...)
	case segVideoOnly:
		args = append(args, "-map", "0:v:0", "-an")
		args = append(args, encArgs...)
	default:
		return fmt.Errorf("av1 init does not support mode %d", mode)
	}

	args = append(args,
		"-f", "hls",
		"-hls_segment_type", "fmp4",
		"-hls_fmp4_init_filename", "init.m4s",
		"-hls_segment_filename", filepath.Join(work, "seg%d.m4s"),
		"-hls_time", "0.5",
		filepath.Join(work, "playlist.m3u8"),
	)

	stderr, err := library.FFRun{Args: args, Timeout: codecTimeout("libsvtav1")}.Run()
	if err != nil {
		return library.FFErr(stderr, err)
	}

	src := filepath.Join(work, "init.m4s")
	if _, statErr := os.Stat(src); statErr != nil {
		return fmt.Errorf("av1 init not produced: %w", statErr)
	}
	return os.Rename(src, initFile)
}

var av1AudioInFlight sync.Map // audio dir -> chan struct{}

// ONE continuous pass for the whole track: per-segment audio reproducibly
// MEDIA_ERR_DECODE'd ~seg 367 on encoder re-priming. The muxer flushes segments
// ~100× realtime, so callers waitForFile instead of blocking on the pass.
// A `.done` marker means fully rendered; without it the dir is a partial.
// srcAudio returns the probed metadata for one audio track, or nil when the
// probe failed or the index is out of range — callers must treat nil as "assume
// nothing and transcode", never as "no audio".
func srcAudio(srcPath string, audioIdx int) *audioMeta {
	m := getSrcInfo(srcPath).aMeta
	if audioIdx < 0 || audioIdx >= len(m) {
		return nil
	}
	return &m[audioIdx]
}

func ensureAV1Audio(srcPath, hash string, audioIdx int, segDur float64) error {
	dir := filepath.Dir(initPath(hash, "audio", audioIdx, true))
	if _, err := os.Stat(filepath.Join(dir, ".done")); err == nil {
		return nil
	}
	gate := make(chan struct{})
	if _, loaded := av1AudioInFlight.LoadOrStore(dir, gate); loaded {
		return nil // a pass is already running; waitForFile tracks progress
	}

	if segDur <= 0 {
		segDur = hlsSegDur
	}
	go func() {
		defer func() {
			av1AudioInFlight.Delete(dir)
			close(gate)
		}()
		// No .done → any prior content is a partial from an interrupted pass.
		os.RemoveAll(dir)
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("[av1audio] mkdir %s: %v", dir, err)
			return
		}
		args := []string{
			"-y", "-i", srcPath,
			"-map", fmt.Sprintf("0:a:%d", audioIdx), "-vn",
		}
		// Stereo, unlike the h264 ladder: >2ch AAC in this fMP4 audio-only
		// rendition never plays. 176 multichannel files across the library have
		// zero watch records (Orville S02, Breaking Bad S01 both fetched a full
		// segment set and never reported a frame), while stereo AV1 titles and
		// multichannel h264/TS (Sopranos S01E01, 55min watched) both play. The
		// master advertises mp4a.40.2 with no CHANNELS attribute either way.
		//
		// But when the source track is ALREADY AAC at <=2ch, that target is what
		// the file holds, so re-encoding buys nothing and costs everything: The
		// Fix (2h21m, source aac/stereo/193k) was 3 hours from finishing at 3
		// segs/min under load, to reproduce audio it already had. Copy also
		// preserves a source above 192k instead of capping it, and the init's
		// esds then describes the very frames being copied.
		//
		// Exactly 2, not <=2: a mono source copied straight through yields a
		// 1-channel rendition, and the CMAF audio track this master points at
		// is declared stereo. Re-encoding mono to stereo is the cheap case
		// anyway, so the copy fast path loses nothing by excluding it.
		if m := srcAudio(srcPath, audioIdx); m != nil && m.codec == "aac" && m.channels == 2 {
			args = append(args, "-c:a", "copy")
		} else {
			args = append(args, "-c:a", "aac", "-b:a", "192k", "-ac", "2")
		}
		args = append(args,
			"-f", "hls",
			"-hls_segment_type", "fmp4",
			"-hls_fmp4_init_filename", "init.m4s",
			"-hls_flags", "independent_segments+temp_file",
			"-hls_segment_filename", filepath.Join(dir, "%06d.m4s"),
			"-hls_time", fmt.Sprintf("%.3f", segDur),
			"-hls_list_size", "0",
			filepath.Join(dir, "audio.m3u8"),
		)
		stderr, err := library.FFRun{Args: args, Timeout: 30 * time.Minute}.Run()
		if err != nil {
			log.Printf("[av1audio] pass failed [%s a%d]: %v\n%s", filepath.Base(srcPath), audioIdx, err, stderr)
			return
		}
		os.WriteFile(filepath.Join(dir, ".done"), nil, 0644)
	}()
	return nil
}

var av1VideoInFlight sync.Map // video dir -> chan struct{}

// ONE continuous `-c:v copy` pass for the whole stream, mirroring
// ensureAV1Audio. Per-segment `-ss bounds[N] -c:v copy` snapped to the keyframe
// at-or-BEFORE the timestamp, so every segment carried the PREVIOUS GOP and
// video ran a GOP behind audio. One muxer instance keeps a running clock, so
// tfdt is correct natively. EXTINFs come from muxerBounds, which replicates this
// muxer's absolute grid — keep the two in step (TestE2E_AV1GridMatchesFFmpeg).
func ensureAV1Video(srcPath, hash string, segDur float64) error {
	seg0, _ := segPaths(hash, CodecAV1, qSrc, 0, 0, segVideoOnly, true)
	dir := filepath.Dir(seg0)
	if _, err := os.Stat(filepath.Join(dir, ".done")); err == nil {
		return nil
	}
	gate := make(chan struct{})
	if _, loaded := av1VideoInFlight.LoadOrStore(dir, gate); loaded {
		return nil // a pass is already running; waitForFile tracks progress
	}

	if segDur <= 0 {
		segDur = hlsSegDur
	}
	go func() {
		defer func() {
			av1VideoInFlight.Delete(dir)
			close(gate)
		}()
		// No .done → prior content is a partial; start clean.
		os.RemoveAll(dir)
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("[av1video] mkdir %s: %v", dir, err)
			return
		}
		args := []string{
			"-y", "-i", srcPath,
			"-map", "0:v:0", "-an",
			"-c:v", "copy",
			"-f", "hls",
			"-hls_segment_type", "fmp4",
			"-hls_fmp4_init_filename", "init.m4s",
			"-hls_flags", "independent_segments+temp_file",
			"-hls_segment_filename", filepath.Join(dir, "%06d.m4s"),
			"-hls_time", fmt.Sprintf("%.3f", segDur),
			"-hls_list_size", "0",
			filepath.Join(dir, "video.m3u8"),
		}
		stderr, err := library.FFRun{Args: args, Timeout: 30 * time.Minute}.Run()
		if err != nil {
			log.Printf("[av1video] pass failed [%s]: %v\n%s", filepath.Base(srcPath), err, stderr)
			return
		}
		os.WriteFile(filepath.Join(dir, ".done"), nil, 0644)
	}()
	return nil
}

// Existence is NOT readiness for an init segment: ffmpeg creates init.m4s and
// fills it in place, so waitForFile's bare os.Stat returned the instant the file
// appeared and HLSInit served 0 bytes under `Cache-Control: immutable` — that
// client's rendition then never initializes again, not even across a restart.
// Segments are atomic (`-hls_flags temp_file`) and keep using waitForFile.
// Complete means every top-level box closes exactly at EOF and a moov is among
// them; a half-written box leaves the walk short of the end.
func waitForInit(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if b, err := os.ReadFile(path); err == nil && initComplete(b) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for a complete %s", filepath.Base(path))
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func initComplete(b []byte) bool {
	moov := false
	for off := 0; off < len(b); {
		if len(b)-off < 8 {
			return false
		}
		size := int(binary.BigEndian.Uint32(b[off : off+4]))
		if string(b[off+4:off+8]) == "moov" {
			moov = true
		}
		if size < 8 || off+size > len(b) {
			return false
		}
		off += size
	}
	return moov
}

func waitForFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s", filepath.Base(path))
		}
		time.Sleep(150 * time.Millisecond)
	}
}

var initInFlight sync.Map

func ensureInit(srcPath, hash, q string, audioIdx int, useCopy bool) (out string, err error) {
	out = initPath(hash, q, audioIdx, useCopy)
	if useCopy && q != "audio" {
		// The video copy rendition writes its init beside its segments; the
		// a0/ path initPath hands back for it is never written at all, so the
		// fast path below could never hit and every request re-entered the pass.
		seg0, _ := segPaths(hash, CodecAV1, qSrc, 0, 0, segVideoOnly, true)
		out = filepath.Join(filepath.Dir(seg0), "init.m4s")
	}
	// NOT a bare os.Stat. ffmpeg creates init.m4s and fills it IN PLACE, so
	// existence means "started", not "usable" — and this fast path handed back
	// a 0-byte init under Cache-Control: max-age=31536000. hls.js cannot
	// initialize a SourceBuffer from an empty init, so the title is dead in
	// that browser for a YEAR, while curl and a fresh profile both play it.
	// The audio rendition is the one that bites: its pass starts after the
	// video pass, so a cold start asks for the audio init before it has bytes.
	// waitForInit below guards generation; this shortcut used to bypass it.
	if b, rerr := os.ReadFile(out); rerr == nil && initComplete(b) {
		return out, nil
	}

	cacheKey := fmt.Sprintf("init:%s:%s:%d:%v", hash, q, audioIdx, useCopy)
	fl := &inflight{done: make(chan struct{})}
	if actual, loaded := initInFlight.LoadOrStore(cacheKey, fl); loaded {
		other := actual.(*inflight)
		select {
		case <-other.done:
			return out, other.err
		case <-time.After(2 * time.Minute):
			return out, fmt.Errorf("init generation timed out")
		}
	}
	defer func() {
		fl.err = err
		initInFlight.Delete(cacheKey)
		close(fl.done)
	}()

	if useCopy {
		// Single track each: hls.js requires that shape for fMP4.
		if q == "audio" {
			// Emitted by the same pass that writes the segments → matching esds.
			if err := ensureAV1Audio(srcPath, hash, audioIdx, audioStep(getSrcInfo(srcPath))); err != nil {
				return out, err
			}
			if err := waitForInit(out, 120*time.Second); err != nil {
				return out, err
			}
			return out, nil
		}
		// Same muxer instance as the segments, so its av01 config matches the
		// copied bytes exactly.
		if err := ensureAV1Video(srcPath, hash, hlsSegDur); err != nil {
			return out, err
		}
		if err := waitForInit(out, 120*time.Second); err != nil {
			return out, err
		}
		return out, nil
	}

	p, ok := av1Profiles[q]
	if !ok {
		return out, fmt.Errorf("invalid quality %q", q)
	}
	if err := generateAV1Init(srcPath, out, p, audioIdx, segMuxed); err != nil {
		log.Printf("AV1 init error [%s q=%s audio=%d]: %v", filepath.Base(srcPath), q, audioIdx, err)
		return out, err
	}
	return out, nil
}

func HLSInit(c *gin.Context, lib *library.Library) {
	file := c.Query("file")
	q := c.Query("q")
	audioStr := c.DefaultQuery("audio", "0")
	useCopy := c.Query("m") == "copy"

	var audioIdx int
	fmt.Sscanf(audioStr, "%d", &audioIdx)

	path := lib.FindFile(file)
	if path == "" {
		c.String(http.StatusNotFound, "not found")
		return
	}

	out, err := ensureInit(path, library.Hash(file), q, audioIdx, useCopy)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("init failed: %v", err))
		return
	}

	// Last gate before an immutable cache header goes out. ensureInit is meant
	// to guarantee this, but it has bypassed its own check once already, and the
	// cost of being wrong here is asymmetric: a 200 + max-age=31536000 on a
	// truncated init poisons that browser for a year and looks like a dead
	// title, while a 503 just makes the player retry a second later.
	b, rerr := os.ReadFile(out)
	if rerr != nil || !initComplete(b) {
		log.Printf("init incomplete, refusing to cache [%s q=%s audio=%d]: %d bytes",
			filepath.Base(path), q, audioIdx, len(b))
		c.Header("Cache-Control", "no-store")
		c.String(http.StatusServiceUnavailable, "init not ready")
		return
	}

	c.Header("Content-Type", "video/mp4")
	c.Header("Cache-Control", "public, max-age=31536000")
	c.Data(http.StatusOK, "video/mp4", b)
}

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
	// qSrc is the pseudo-quality for AV1 source-passthrough: the on-disk AV1
	// stream copied (remuxed) into fMP4 at its native resolution. It is not in
	// any profile map — there is no scaling or bitrate target, just a copy.
	qSrc = "src"
	// hlsURLVer cache-busts segment & init URLs. They're served immutable
	// (Cache-Control: max-age=1yr) for bandwidth, but the BYTES at a given
	// seg/init URL change whenever the encoding pipeline changes — browsers
	// and proxies (Caddy/CDN) would then replay a stale, now-invalid segment
	// forever. Playlists are no-cache, so bumping this constant on any change
	// to segment/init production instantly invalidates every cached fragment
	// for every client. Bump it whenever AV1/h264 segment output changes.
	hlsURLVer = "6"
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

// ladderFor returns the rung keys to emit for a source whose display grid is
// srcW×srcH. Rungs are clamped to the SHORTER side (= height for landscape,
// width for portrait) so a tall phone video doesn't get over-tall, massively
// over-provisioned rungs (the rung scales by height; bitrates are tuned for
// landscape). No upscaling, in hlsQualityOrder. Never empty — a source
// shorter than the smallest rung still gets that one rung (an empty master is
// unplayable). srcW/srcH ≤ 0 (unprobed) keeps the full ladder.
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
	vHeight     int     // display height (rotation-corrected)
	vWidth      int     // display width (rotation-corrected)
	vDAR        float64 // display aspect ratio, 0 if unknown
	vBitDepth   int     // 8 / 10 / 12, 0 if unknown
	vTransfer   string  // color_transfer (smpte2084 / arib-std-b67 = HDR)
	vPrimaries  string  // color_primaries (bt2020 = wide gamut)
	vColorspace string  // color_space/matrix (bt2020nc — survives SVT-AV1)
	vRotation   int     // normalized 0/90/180/270 display rotation
	aChannels   []int   // channel count per audio stream, in stream order
	aLayouts    []string // channel_layout per audio stream (empty/unknown → unnamed)
	vPixFmt     string
	kfTimes     []float64
	bounds      []float64 // segment boundaries, keyframe-aligned
	segBitrates []int     // bps per segment, aligned with bounds
	peakBitrate int       // peak over all segments (bps)
	once        sync.Once
}

var srcInfoMap sync.Map // path -> *srcInfo

// pixBitDepth extracts the luma bit depth from an ffmpeg pix_fmt name
// ("yuv420p10le" → 10, "p010le" → 10, "yuv420p" → 8). Defaults to 8.
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

// normRotation returns the display rotation normalized to 0/90/180/270. The
// astiffprobe Stream struct doesn't surface side_data, so the modern
// displaymatrix is read with a dedicated ffprobe; the legacy tags.rotate
// (passed in) is the fallback. Only the 90/270 parity matters downstream
// (dimension swap); ffmpeg autorotates the actual pixels itself.
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

// AAC encoder accepts these layouts; anything else (incl. the common
// container-level "unknown" / "6 channels" generic) makes it fail with
// "Unsupported channel layout". Lower-cased to match ffprobe output.
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

// aacArgs builds the AAC encode for an audio rendition. Upgrade-if-available:
// a multichannel source keeps its layout (surround preserved) at a
// layout-appropriate bitrate — but ONLY when the source declares a named
// layout the AAC encoder accepts. Unnamed multichannel ("6 channels" /
// "unknown" — common in scene rips like the Drive 2011 regression) downmixes
// to stereo, because the encoder otherwise fails the segment outright.
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

// anyUnnamedMultichannel reports whether any audio stream is multichannel
// with an unnamed/unknown layout — the AAC encoder will reject it. Used by
// the convert pipeline to decide whether to force `-ac 2` on its whole-file
// re-encode (which maps all audio streams uniformly, so per-stream layout
// args don't apply).
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

// isHDR reports whether the source carries an HDR transfer or wide-gamut
// primaries (PQ / HLG / BT.2020). Used to decide preserve-vs-tonemap.
func isHDR(si *srcInfo) bool {
	switch si.vTransfer {
	case "smpte2084", "arib-std-b67":
		return true
	}
	// SVT-AV1 (this ffmpeg) drops transfer/primaries but keeps the matrix, so
	// bt2020 in any of the three color fields is the reliable wide-gamut/HDR
	// signal across both source codecs and re-encoded AV1.
	return strings.HasPrefix(si.vPrimaries, "bt2020") ||
		strings.HasPrefix(si.vColorspace, "bt2020")
}

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
				// Display aspect = coded W:H corrected by the pixel (sample)
				// aspect. Anamorphic sources (SAR ≠ 1:1) MUST be deanamorphized
				// for HLS or players render the coded grid square (stretched).
				dar := 0.0
				if w > 0 && h > 0 {
					dar = float64(w) * float64(sn) / (float64(h) * float64(sd))
				}
				// ffmpeg autorotates pixels for ±90/270; coded dims are
				// pre-rotation, so the *display* grid is swapped. Everything
				// downstream (ladder, RESOLUTION, deanamorphize) must use the
				// rotated dims or rotated phone video comes out sideways.
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
			}
		}
		if si.vCodec != "h264" && si.vCodec != "av1" {
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
		size, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		pkts = append(pkts, vpkt{pts: pts, size: size, key: strings.HasPrefix(parts[2], "K")})
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

// canCopyVideo: h264 source-passthrough copy is DISABLED. `generateSegment`'s
// copy mode (-ss start -i src -t segDur -c:v copy -f mpegts) can't trim
// mid-GOP, so each segment overshoots its bounds while the playlist EXTINF
// states the nominal duration. Over a feature-length film that mismatch
// accumulates into MINUTES of timeline drift → scenes jump back-and-forth,
// playback hangs, sidecar subtitles end up minutes off the drifted playhead
// (observed: How.To.Lose.A.Guy on the copy/1080p rung). The transcode path
// re-encodes to an exact -t segDur (zero drift) and is hardware-fast
// (h264_videotoolbox), so route ALL h264 through it. AV1 copy-passthrough is a
// separate, correct path (canCopyAV1) and is unaffected. Params kept so the
// rung-bandwidth/playlist callers compile unchanged.
func canCopyVideo(si *srcInfo, profile hlsProfile) bool {
	_ = si
	_ = profile
	return false
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

func evenWidth(w int) int {
	if w < 2 {
		w = 2
	}
	if w%2 != 0 {
		w++
	}
	return w
}

// rungWidth is the square-pixel width for a rung at rungH that preserves the
// source's *display* aspect (i.e. deanamorphized: storage W:H corrected by
// SAR). Falls back to the storage ratio, then 16:9, when aspect is unknown.
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

// scaleFilter builds the -vf scale for a transcoded rung. It deanamorphizes —
// explicit display width + setsar=1 — so every HLS player renders correctly
// regardless of whether it honors in-stream SAR (hls.js transmuxed MPEG-TS
// does not, which stretched anamorphic films). For square-pixel sources this
// is equivalent to the old "scale=-2:H". Falls back to that when the source
// aspect couldn't be probed, so behavior is unchanged for those.
func scaleFilter(si *srcInfo, rungH int) string {
	if si == nil || si.vDAR <= 0 {
		return fmt.Sprintf("scale=-2:%d", rungH)
	}
	return fmt.Sprintf("scale=%d:%d:flags=lanczos,setsar=1",
		evenWidth(int(math.Round(float64(rungH)*si.vDAR))), rungH)
}

// Build a full #EXT-X-STREAM-INF attribute list with BANDWIDTH,
// AVERAGE-BANDWIDTH, RESOLUTION, CODECS, and any caller-supplied extras
// (e.g. AUDIO="audio" for multi-audio masters). codec selects the CODECS
// attribute (avc1.* vs av01.*) and the bandwidth source (probed copy-eligible
// peak for h264, VBV cap for AV1).
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
	var aTracks []audioMeta

	if streams, err := library.Prober.Streams(context.Background(), path); err == nil {
		for _, s := range streams {
			if s.CodecType == "audio" {
				aTracks = append(aTracks, audioMeta{language: s.Tags["language"], codec: s.CodecName, channels: s.Channels})
			}
		}
	}
	// Single source of truth for dimensions: rotation- and SAR-corrected
	// display grid. Drives the ladder height and advertised RESOLUTION so
	// rotated/anamorphic sources aren't sideways or stretched.
	srcInfoP := getSrcInfo(path)
	srcWidth, srcHeight = srcInfoP.vWidth, srcInfoP.vHeight

	multiAudio := len(aTracks) > 1

	codec := MediaCodec(file)
	// Multi-audio AV1 not supported in this round (would need fMP4 audio
	// renditions to keep the master's segment-type uniform). Fall back.
	if codec == CodecAV1 && multiAudio {
		codec = CodecH264
	}
	// Zero-audio AV1 demux would emit an audio rendition whose non-optional
	// `0:a:0` mapping fails (no audio stream) → hls.js stalls on the broken
	// AUDIO group. h264's optional `0:a:?` mapping plays video-only cleanly.
	if codec == CodecAV1 && len(aTracks) == 0 {
		codec = CodecH264
	}
	// AV1 is served only via copy-passthrough. If the marked file isn't
	// actually a copy-eligible AV1 stream on disk (wrong codec, 10-bit, no
	// leading keyframe), fall back to the safe h264 ladder rather than emit a
	// rung whose segments can't be produced without a janky live transcode.
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
		// Demuxed CMAF. hls.js cannot demux fMP4, so video and audio ship as
		// separate single-track renditions: one source-passthrough video rung
		// (on-disk AV1 -c:v copy'd into fMP4 at native resolution — no ladder,
		// no live re-encode) plus a separate AAC-fMP4 audio rendition cut on
		// the SAME keyframe boundaries so the two stay segment-aligned.
		si := getSrcInfo(path)
		avg := si.peakBitrate
		if avg <= 0 {
			avg = 6_000_000
		}
		peak := avg + avg/5
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
	c.Data(http.StatusOK, "application/vnd.apple.mpegurl", []byte(sb.String()))

	go Prewarm(file, path, srcHeight, aTracks, defIdx)
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
	// h264 audio renditions ride MPEG-TS (hls.js demuxes TS itself). AV1 video
	// is fMP4, and hls.js cannot demux fMP4 — so an AV1 title's audio must also
	// be its own fMP4 rendition. Only force h264 for the non-AV1 audio playlist.
	if audioOnly && codec != CodecAV1 {
		codec = CodecH264
	}

	// AV1 is copy-passthrough only (q=qSrc, no profile/scaling). Profiles are
	// needed solely for the h264 transcode ladder.
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
		// Both the AV1 video rung (q=src) and its AAC audio rendition
		// (q=audio) are cut on the SAME keyframe-aligned boundaries, so
		// demuxed fMP4 segment i covers the same presentation interval in
		// each — a hard CMAF requirement for hls.js to sync the two.
		if !canCopyAV1(si) || len(si.bounds) < 2 {
			c.String(http.StatusInternalServerError, "av1 source not copy-eligible")
			return
		}
		copyMode = true
	} else if !audioOnly && canCopyVideo(si, profile) && len(si.bounds) >= 2 {
		copyMode = true
	}
	if copyMode {
		for i := 0; i < len(si.bounds)-1; i++ {
			segDurs = append(segDurs, si.bounds[i+1]-si.bounds[i])
		}
		if !audioOnly {
			segBitrates = si.segBitrates // bitrate hint is video-only
		}
	} else {
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
		// Each rendition gets its own single-track init: the audio rendition's
		// init carries only the AAC moov, the video rung's only the av01 moov.
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
	cv := "&cv=" + hlsURLVer // cache-bust immutable segment URLs across pipeline changes

	for i, segDur := range segDurs {
		if i < len(segBitrates) && segBitrates[i] > 0 {
			sb.WriteString(fmt.Sprintf("#EXT-X-BITRATE:%d\n", segBitrates[i]/1000))
		}
		sb.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", segDur))

		if audioOnly {
			// h264: bare TS audio segment (modeParam/codecParam empty).
			// AV1: &m=copy&codec=av1 → fMP4 AAC segment, same boundaries as video.
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

// segPaths returns (segPath, cacheKey). Stamping segDur into the path means a
// future tweak to hlsSegDur silently invalidates the old layout instead of
// serving 4s segments to a player expecting 10s ones. The codec dimension
// keeps h264 (.ts) and AV1 (.m4s) caches segregated.
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
	// AV1 ships demuxed fMP4 (.m4s) for BOTH its video rung and its AAC audio
	// rendition — hls.js can't demux fMP4, so the two are separate single-track
	// files. h264 stays muxed MPEG-TS (.ts), audio rendition included.
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

// initPath is the on-disk location of the AV1 init segment for (hash, q,
// audioIdx). Lives alongside the media segments so cache-cleanup eviction
// removes them together when the rung ages out.
func initPath(hash, q string, audioIdx int, useCopy bool) string {
	dKey := fmt.Sprintf("d%d", int(hlsSegDur))
	sub := dKey + "-av1"
	if useCopy {
		sub = dKey + "-av1-copy"
	}
	return filepath.Join(hlsCacheDir, filepath.Join(hash, sub), q, fmt.Sprintf("a%d", audioIdx), "init.m4s")
}

// ensureSegment guarantees a segment exists on disk at segPath. Cache hits
// return immediately; in-flight requests for the same segment wait on a shared
// channel so we never run two ffmpegs for the same key.
func ensureSegment(srcPath, hash, codec, q string, segNum, audioIdx, mode int, useCopy bool) (string, error) {
	segPath, cacheKey := segPaths(hash, codec, q, segNum, audioIdx, mode, useCopy)

	if _, err := os.Stat(segPath); err == nil {
		return segPath, nil
	}

	ch := make(chan struct{})
	if actual, loaded := hlsInFlight.LoadOrStore(cacheKey, ch); loaded {
		select {
		case <-actual.(chan struct{}):
			return segPath, nil
		case <-time.After(90 * time.Second):
			return segPath, fmt.Errorf("segment generation timed out")
		}
	}

	defer func() {
		hlsInFlight.Delete(cacheKey)
		close(ch)
	}()

	// AV1 copy-passthrough (q=qSrc) needs no profile — it's a remux, no encode.
	// Profiles drive the h264 ladder and the dormant AV1 transcode fallback.
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

	start := float64(segNum) * hlsSegDur
	segDur := hlsSegDur
	// Copy-passthrough segments ride keyframe-aligned bounds. The AV1 audio
	// rendition (avCopy + segAudioOnly) MUST use the same bounds as the video
	// rung so the demuxed renditions stay segment-aligned; h264's TS audio
	// stays on the uniform grid.
	if useCopy && (mode != segAudioOnly || avCopy) {
		si := getSrcInfo(srcPath)
		if segNum >= len(si.bounds)-1 {
			return segPath, fmt.Errorf("segment %d out of range", segNum)
		}
		start = si.bounds[segNum]
		segDur = si.bounds[segNum+1] - si.bounds[segNum]
	}

	var err error
	if avCopy && mode == segAudioOnly {
		// Audio: one continuous pass for the whole track (gapless, no
		// per-segment AAC priming/overlap, no mid-file decode failures —
		// per-segment audio reproducibly died ~seg 367). ffmpeg writes
		// segments progressively (~100× realtime) so it always outruns
		// playback; we just wait for this segment's file to land.
		err = ensureAV1Audio(srcPath, hash, audioIdx, segDur)
		if err == nil {
			err = waitForFile(segPath, 120*time.Second)
		}
	} else if avCopy {
		// Video rung: ONE continuous -c:v copy pass (same gapless contract as
		// audio). Per-segment -ss+copy snapped every segment to the previous
		// keyframe; the single muxer writes correct native tfdt and cuts on
		// real keyframes. Pass outruns playback, so just wait for this seg.
		err = ensureAV1Video(srcPath, hash, hlsSegDur)
		if err == nil {
			err = waitForFile(segPath, 120*time.Second)
		}
	} else if codec == CodecAV1 && mode != segAudioOnly {
		err = generateAV1Segment(srcPath, segPath, start, segDur, p, audioIdx, mode)
	} else {
		err = generateSegment(srcPath, segPath, start, segDur, p, audioIdx, mode, useCopy)
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

// Prewarm fires off ffmpeg jobs for the first N segments of the source-height
// rung (copy mode if eligible) plus a few audio segments, so VHS's initial
// fetches hit a populated cache instead of waiting on cold ffmpegs.
func Prewarm(file, path string, srcHeight int, aTracks []audioMeta, defAudio int) {
	const N = 10
	codec := MediaCodec(file)
	multiAudio := len(aTracks) > 1
	// AV1 multi-audio isn't supported in this round; fall back to h264 prewarm.
	if codec == CodecAV1 && multiAudio {
		codec = CodecH264
	}
	// Mirror the master's zero-audio fallback (silent AV1 → h264).
	if codec == CodecAV1 && len(aTracks) == 0 {
		codec = CodecH264
	}
	// AV1 serves only via copy-passthrough; if not eligible the master falls
	// back to h264, so prewarm must mirror that or it warms the wrong cache.
	if codec == CodecAV1 && !canCopyAV1(getSrcInfo(path)) {
		codec = CodecH264
	}
	hash := library.Hash(file)

	if codec == CodecAV1 {
		// Demuxed CMAF: kick BOTH continuous passes (audio re-encode, video
		// -c:v copy) now. Each flushes init + segments far ahead of playback,
		// so by the time hls.js asks the cache is already populated. The video
		// rung is audio-independent (the player's video MAP has no audio).
		segDur := hlsSegDur
		if si := getSrcInfo(path); len(si.bounds) >= 2 {
			segDur = si.bounds[1] - si.bounds[0]
		}
		go ensureAV1Audio(path, hash, defAudio, segDur)
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

// generateAV1Segment writes a fragment-only .m4s for the [start, start+segDur)
// slice. Uses ffmpeg's HLS-fmp4 muxer in a scratch dir; the muxer writes
// init+seg+playlist, we keep only the seg.
//
// Mode: segMuxed (V+A) or segVideoOnly. segAudioOnly stays in MPEG-TS land —
// a multi-audio AV1 master playlist would need a fully fMP4 audio rendition,
// which this round doesn't support.
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

// generateAV1Init writes the moov-only init segment for the given rung+audio.
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

	// 1s of source is enough for the muxer to emit a complete moov box with
	// codec config; we throw the media away.
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

// ensureAV1Audio renders the ENTIRE audio track in ONE continuous ffmpeg pass
// (gapless, single encoder-prime, zero accumulating drift — per-segment audio
// reproducibly MEDIA_ERR_DECODE'd ~seg 367). ffmpeg's hls/fmp4 muxer flushes
// init.m4s + %06d.m4s progressively (~100× realtime, far outrunning playback)
// so callers don't block on the whole pass — they waitForFile the one segment
// they need. Idempotent: a `.done` marker means fully rendered; a restart
// mid-pass (no marker) re-renders from scratch. Returns immediately; the pass
// runs in a background goroutine guarded by an in-flight gate.
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
		args = append(args, aacArgs(getSrcInfo(srcPath), audioIdx, "192k")...)
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

// ensureAV1Video remuxes the ENTIRE AV1 video stream in ONE continuous
// `-c:v copy` pass into keyframe-aligned fMP4 fragments, exactly mirroring
// ensureAV1Audio. The old per-segment `-ss <bounds[N]> -c:v copy` path was
// fatally wrong: ffmpeg snaps -ss to the keyframe at-or-BEFORE the timestamp,
// and bounds[N] formatted "%.3f" rounds just under the real keyframe pts, so
// every segment carried the PREVIOUS GOP while av1PatchFragment stamped it
// with the current segment's tfdt — video ran exactly one segment (~one GOP,
// ~6.7s) behind audio, perceived as audio drift plus scenes replaying at
// every seam (Superbad: seg N decoded to source bounds[N-1]). A single muxer
// instance keeps a running decode clock, so the hls/fmp4 muxer writes correct
// cumulative tfdt natively and cuts only at real source keyframes — no -ss,
// no tfdt patch, no off-by-one. `-hls_time hlsSegDur` reproduces
// segmentBoundaries' rule exactly, so the on-disk segments line up with the
// playlist's si.bounds EXTINFs. Copy is a pure remux so it outruns playback
// just like the audio pass. Same idempotent .done / in-flight contract.
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
		// No .done → any prior content is a partial (or the broken off-by-one
		// generation); start clean.
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

// waitForFile blocks until path exists or timeout. The AV1 audio pass writes
// segments far faster than realtime, so a needed segment lands quickly.
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

func ensureInit(srcPath, hash, q string, audioIdx int, useCopy bool) (string, error) {
	out := initPath(hash, q, audioIdx, useCopy)
	if _, err := os.Stat(out); err == nil {
		return out, nil
	}

	cacheKey := fmt.Sprintf("init:%s:%s:%d:%v", hash, q, audioIdx, useCopy)
	ch := make(chan struct{})
	if actual, loaded := initInFlight.LoadOrStore(cacheKey, ch); loaded {
		select {
		case <-actual.(chan struct{}):
			return out, nil
		case <-time.After(2 * time.Minute):
			return out, fmt.Errorf("init generation timed out")
		}
	}
	defer func() {
		initInFlight.Delete(cacheKey)
		close(ch)
	}()

	if useCopy {
		// Demuxed CMAF: the audio rendition (q=audio) gets an AAC-only init,
		// the video rung an av01-only init — single track each, the shape
		// hls.js requires for fMP4.
		if q == "audio" {
			// The audio init.m4s is emitted by the single continuous pass
			// (same encoder that writes the segments → matching esds).
			segDur := hlsSegDur
			if si := getSrcInfo(srcPath); len(si.bounds) >= 2 {
				segDur = si.bounds[1] - si.bounds[0]
			}
			if err := ensureAV1Audio(srcPath, hash, audioIdx, segDur); err != nil {
				return out, err
			}
			if err := waitForFile(out, 120*time.Second); err != nil {
				return out, err
			}
			return out, nil
		}
		// The video init is emitted by the single continuous copy pass (same
		// muxer instance that writes the segments → its av01 config matches the
		// -c:v copy bytes exactly), co-located with the segments.
		if err := ensureAV1Video(srcPath, hash, hlsSegDur); err != nil {
			return out, err
		}
		seg0, _ := segPaths(hash, CodecAV1, qSrc, 0, 0, segVideoOnly, true)
		vInit := filepath.Join(filepath.Dir(seg0), "init.m4s")
		if err := waitForFile(vInit, 120*time.Second); err != nil {
			return out, err
		}
		return vInit, nil
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

	c.Header("Content-Type", "video/mp4")
	c.Header("Cache-Control", "public, max-age=31536000")
	c.File(out)
}

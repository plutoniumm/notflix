package media

import (
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	encOnce sync.Once
	encSet  map[string]bool
)

func encoders() map[string]bool {
	encOnce.Do(func() {
		encSet = map[string]bool{}
		out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
		if err != nil {
			return
		}
		for line := range strings.SplitSeq(string(out), "\n") {
			f := strings.Fields(line)
			if len(f) < 2 || len(f[0]) != 6 {
				continue
			}
			// Flag char 1 is the stream kind; skips header rows like " V..... = Video".
			switch f[0][0] {
			case 'V', 'A', 'S':
			default:
				continue
			}
			encSet[f[1]] = true
		}
	})
	return encSet
}

func hasEncoder(name string) bool { return encoders()[name] }

func pickH264() string {
	for _, n := range []string{"h264_videotoolbox", "h264_nvenc", "h264_qsv", "h264_amf"} {
		if hasEncoder(n) {
			return n
		}
	}
	return "libx264"
}

func pickHEVC() string {
	for _, n := range []string{"hevc_videotoolbox", "hevc_nvenc", "hevc_qsv", "hevc_amf"} {
		if hasEncoder(n) {
			return n
		}
	}
	if hasEncoder("libx265") {
		return "libx265"
	}
	return ""
}

// pickAV1 returns "" if none. libaom-av1 is excluded: too slow for HLS.
func pickAV1() string {
	for _, n := range []string{"av1_videotoolbox", "av1_nvenc", "av1_qsv", "av1_amf"} {
		if hasEncoder(n) {
			return n
		}
	}
	if hasEncoder("libsvtav1") {
		return "libsvtav1"
	}
	return ""
}

// VideoToolbox/NVENC/etc. ignore -crf and -preset, so HW paths use a bitrate
// target with the same VBV cap; libx264 keeps the CRF+maxrate hybrid.
func h264HLS(p hlsProfile) []string {
	enc := pickH264()
	switch enc {
	case "h264_videotoolbox":
		return []string{
			"-c:v", "h264_videotoolbox",
			"-b:v", p.vbr,
			"-maxrate", p.vbr,
			"-bufsize", doubleKRate(p.vbr),
			"-profile:v", "high",
			"-allow_sw", "1",
			"-vf", p.scale,
		}
	case "h264_nvenc":
		return []string{
			"-c:v", "h264_nvenc",
			"-preset", "p5", "-rc", "vbr",
			"-b:v", p.vbr, "-maxrate", p.vbr, "-bufsize", doubleKRate(p.vbr),
			"-profile:v", "high",
			"-vf", p.scale,
		}
	case "h264_qsv":
		return []string{
			"-c:v", "h264_qsv",
			"-b:v", p.vbr, "-maxrate", p.vbr, "-bufsize", doubleKRate(p.vbr),
			"-profile:v", "high",
			"-vf", p.scale,
		}
	case "h264_amf":
		return []string{
			"-c:v", "h264_amf",
			"-rc", "vbr_peak",
			"-b:v", p.vbr, "-maxrate", p.vbr, "-bufsize", doubleKRate(p.vbr),
			"-profile:v", "high",
			"-vf", p.scale,
		}
	}
	return []string{
		"-c:v", "libx264", "-preset", "faster",
		"-crf", strconv.Itoa(p.crf),
		"-maxrate", p.vbr, "-bufsize", doubleKRate(p.vbr),
		"-vf", p.scale,
	}
}

// VideoToolbox takes -q:v (1-100, higher=better) instead of CRF.
func h264File(crf int) []string {
	enc := pickH264()
	switch enc {
	case "h264_videotoolbox":
		return []string{"-c:v", "h264_videotoolbox", "-q:v", strconv.Itoa(crfToQ(crf)), "-allow_sw", "1"}
	case "h264_nvenc":
		return []string{"-c:v", "h264_nvenc", "-preset", "p5", "-rc", "vbr", "-cq", strconv.Itoa(crf)}
	case "h264_qsv":
		return []string{"-c:v", "h264_qsv", "-global_quality", strconv.Itoa(crf)}
	case "h264_amf":
		return []string{"-c:v", "h264_amf", "-rc", "cqp", "-qp_i", strconv.Itoa(crf), "-qp_p", strconv.Itoa(crf)}
	}
	return []string{"-c:v", "libx264", "-preset", "fast", "-crf", strconv.Itoa(crf)}
}

// No -vf scale: source resolution is kept, so ffmpeg carries the source color
// metadata through. 10-bit only on sw/NVENC/QSV — HW 10-bit AV1 support is
// uneven, so VideoToolbox/AMF stay 8-bit.
func av1File(crf int, tenbit bool) []string {
	pf := "yuv420p"
	switch pickAV1() {
	case "av1_videotoolbox":
		return []string{"-c:v", "av1_videotoolbox", "-q:v", strconv.Itoa(crfToQ(crf)), "-allow_sw", "1", "-pix_fmt", pf}
	case "av1_nvenc":
		if tenbit {
			pf = "p010le"
		}
		return []string{"-c:v", "av1_nvenc", "-preset", "p5", "-rc", "vbr", "-cq", strconv.Itoa(crf), "-pix_fmt", pf}
	case "av1_qsv":
		if tenbit {
			pf = "p010le"
		}
		return []string{"-c:v", "av1_qsv", "-global_quality", strconv.Itoa(crf), "-pix_fmt", pf}
	case "av1_amf":
		return []string{"-c:v", "av1_amf", "-rc", "cqp", "-qp_i", strconv.Itoa(crf), "-qp_p", strconv.Itoa(crf), "-pix_fmt", pf}
	}
	if tenbit {
		pf = "yuv420p10le"
	}
	return []string{"-c:v", "libsvtav1", "-preset", "6", "-crf", strconv.Itoa(crf), "-pix_fmt", pf}
}

// Returns nil when no AV1 encoder exists; caller falls back to h264.
func av1HLS(p hlsProfile) []string {
	enc := pickAV1()
	switch enc {
	case "av1_videotoolbox":
		return []string{
			"-c:v", "av1_videotoolbox",
			"-b:v", p.vbr,
			"-maxrate", p.vbr,
			"-bufsize", doubleKRate(p.vbr),
			"-allow_sw", "1",
			"-vf", p.scale,
		}
	case "av1_nvenc":
		return []string{
			"-c:v", "av1_nvenc",
			"-preset", "p5", "-rc", "vbr",
			"-b:v", p.vbr, "-maxrate", p.vbr, "-bufsize", doubleKRate(p.vbr),
			"-vf", p.scale,
		}
	case "av1_qsv":
		return []string{
			"-c:v", "av1_qsv",
			"-b:v", p.vbr, "-maxrate", p.vbr, "-bufsize", doubleKRate(p.vbr),
			"-vf", p.scale,
		}
	case "av1_amf":
		return []string{
			"-c:v", "av1_amf",
			"-rc", "vbr_peak",
			"-b:v", p.vbr, "-maxrate", p.vbr, "-bufsize", doubleKRate(p.vbr),
			"-vf", p.scale,
		}
	case "libsvtav1":
		// preset 6: ~10-15% better compression than 8 at ~2× encode time, still
		// inside the per-codec timeout.
		return []string{
			"-c:v", "libsvtav1",
			"-preset", "6",
			"-crf", strconv.Itoa(p.crf),
			"-maxrate", p.vbr, "-bufsize", doubleKRate(p.vbr),
			"-vf", p.scale,
		}
	}
	return nil
}

// crfToQ maps libx264 CRF (lower=better) to VideoToolbox -q:v (higher=better).
func crfToQ(crf int) int {
	q := 100 - crf*2
	q = max(q, 1)
	q = min(q, 100)
	return q
}

// Form: av01.<profile>.<level><tier>.<bitdepth>. Bit depth must match the
// stream — 10-bit labelled .08 makes some players refuse it.
func av1Codec(h, depth int) string {
	bd := "08"
	switch {
	case depth >= 12:
		bd = "12"
	case depth == 10:
		bd = "10"
	}
	switch {
	case h > 1080:
		return "av01.0.13M." + bd
	case h > 720:
		return "av01.0.09M." + bd
	case h > 480:
		return "av01.0.05M." + bd
	default:
		return "av01.0.04M." + bd
	}
}

// Level encoding is L<n*30>: L93=3.1, L120=4.0, L150=5.0, L153=5.1.
func hevcCodec(h int) string {
	switch {
	case h > 1080:
		return "hev1.1.6.L153.B0"
	case h > 720:
		return "hev1.1.6.L120.B0"
	default:
		return "hev1.1.6.L93.B0"
	}
}

// AV1 ladder, parallel to hlsProfiles. CRF is AV1-native (1-63, ~30 ≈ x264 23).
var av1Profiles = map[string]hlsProfile{
	"144p":  {144, "scale=-2:144", "120k", "64k", 35},
	"240p":  {240, "scale=-2:240", "240k", "80k", 34},
	"360p":  {360, "scale=-2:360", "400k", "96k", 33},
	"480p":  {480, "scale=-2:480", "600k", "112k", 31},
	"720p":  {720, "scale=-2:720", "2200k", "128k", 28},
	"1080p": {1080, "scale=-2:1080", "4500k", "192k", 27},
	"2160p": {2160, "scale=-2:2160", "14000k", "256k", 26},
}

// HEVC ladder. CRF reuses the H.264 scale; libx265 behaves comparably there.
var hevcProfiles = map[string]hlsProfile{
	"144p":  {144, "scale=-2:144", "140k", "64k", 28},
	"240p":  {240, "scale=-2:240", "280k", "80k", 27},
	"360p":  {360, "scale=-2:360", "450k", "96k", 26},
	"480p":  {480, "scale=-2:480", "700k", "112k", 24},
	"720p":  {720, "scale=-2:720", "2800k", "128k", 21},
	"1080p": {1080, "scale=-2:1080", "5500k", "192k", 20},
	"2160p": {2160, "scale=-2:2160", "17500k", "256k", 19},
}

// Hardware encoders are nearly free; software AV1 needs real headroom at 4K.
func codecTimeout(encName string) time.Duration {
	switch encName {
	case "h264_videotoolbox", "hevc_videotoolbox", "av1_videotoolbox",
		"h264_nvenc", "hevc_nvenc", "av1_nvenc",
		"h264_qsv", "hevc_qsv", "av1_qsv",
		"h264_amf", "hevc_amf", "av1_amf":
		return 60 * time.Second
	case "libx264":
		return 60 * time.Second
	case "libx265":
		return 90 * time.Second
	case "libsvtav1":
		return 180 * time.Second
	case "libaom-av1":
		return 600 * time.Second
	}
	return 60 * time.Second
}

// AV1 (and HEVC on some clients) cannot ride MPEG-TS: those rungs need ISO BMFF
// fragments (moof+mdat, no moov) via #EXT-X-MAP. Pair with `-f mp4` + `.m4s`.
func fmp4SegFlags() string {
	return "+frag_keyframe+empty_moov+default_base_moof+separate_moof"
}

// #EXT-X-MAP init: moov header only, no samples. Combine with `-frames:v 0`.
func fmp4InitFlags() string {
	return "+empty_moov+default_base_moof"
}

// Returns nil when no HEVC encoder exists.
func hevcHLS(p hlsProfile) []string {
	enc := pickHEVC()
	switch enc {
	case "hevc_videotoolbox":
		return []string{
			"-c:v", "hevc_videotoolbox",
			"-b:v", p.vbr,
			"-maxrate", p.vbr,
			"-bufsize", doubleKRate(p.vbr),
			"-tag:v", "hvc1",
			"-allow_sw", "1",
			"-vf", p.scale,
		}
	case "hevc_nvenc":
		return []string{
			"-c:v", "hevc_nvenc",
			"-preset", "p5", "-rc", "vbr",
			"-b:v", p.vbr, "-maxrate", p.vbr, "-bufsize", doubleKRate(p.vbr),
			"-tag:v", "hvc1",
			"-vf", p.scale,
		}
	case "hevc_qsv":
		return []string{
			"-c:v", "hevc_qsv",
			"-b:v", p.vbr, "-maxrate", p.vbr, "-bufsize", doubleKRate(p.vbr),
			"-tag:v", "hvc1",
			"-vf", p.scale,
		}
	case "hevc_amf":
		return []string{
			"-c:v", "hevc_amf",
			"-rc", "vbr_peak",
			"-b:v", p.vbr, "-maxrate", p.vbr, "-bufsize", doubleKRate(p.vbr),
			"-tag:v", "hvc1",
			"-vf", p.scale,
		}
	case "libx265":
		return []string{
			"-c:v", "libx265", "-preset", "faster",
			"-crf", strconv.Itoa(p.crf),
			"-maxrate", p.vbr, "-bufsize", doubleKRate(p.vbr),
			"-tag:v", "hvc1",
			"-vf", p.scale,
		}
	}
	return nil
}

// h264 rides MPEG-TS; av1/hevc need fMP4.
func segExt(codec string) string {
	switch codec {
	case "av1", "hevc":
		return ".m4s"
	default:
		return ".ts"
	}
}

// MPEG-TS for h264, fMP4 for av1/hevc.
func segMime(codec string) string {
	switch codec {
	case "av1", "hevc":
		return "video/mp4"
	default:
		return "video/mp2t"
	}
}

func pickAAC() string {
	if hasEncoder("aac_at") {
		return "aac_at"
	}
	return "aac"
}

func LogEncoders() {
	log.Printf("[media] encoders picked: h264=%s hevc=%s av1=%s aac=%s",
		emptyNone(pickH264()), emptyNone(pickHEVC()), emptyNone(pickAV1()), emptyNone(pickAAC()))
}

func emptyNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// AV1 is served as a single source-resolution rung, so unlike canCopyVideo no
// ladder-height match is required. 4:2:0 8/10-bit only (10-bit copy preserves
// HDR), first keyframe near t=0.
func canCopyAV1(si *srcInfo) bool {
	if si.vCodec != "av1" || len(si.kfTimes) == 0 {
		return false
	}
	switch si.vPixFmt {
	case "", "yuv420p", "yuvj420p", "yuv420p10le", "yuv420p10be":
	default:
		return false
	}
	return si.kfTimes[0] < 0.5
}

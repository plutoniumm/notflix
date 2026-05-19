package media

import (
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Available ffmpeg encoders, populated lazily from `ffmpeg -encoders`.
// Used to pick hardware-accelerated paths (VideoToolbox on macOS, NVENC/QSV/AMF
// on Linux/Windows) when present, falling back to software libx264/libsvtav1.
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
			// First flag char is the stream kind (V/A/S). Skip header rows like
			// " V..... = Video" whose second field is "=".
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

// pickH264 picks the best available H.264 encoder, in HW-preferred order.
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

// pickAV1 returns the best AV1 encoder available, or "" if none. SVT-AV1 is the
// practical software choice (libaom-av1 is too slow for on-demand HLS).
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

// h264HLS returns the -c:v ... block for an HLS rung. VideoToolbox/NVENC/etc.
// don't honor -crf or -preset, so we use bitrate-target rate control with the
// same VBV cap; libx264 keeps the CRF+maxrate hybrid.
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

// h264File is the convert.go re-encode case: single-pass MP4, no rung context.
// VideoToolbox uses -q:v (1-100, higher=better) instead of CRF; map crf→q.
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

// av1File is the whole-file (non-HLS) AV1 encoder for the conversion pipeline.
// No -vf scale: keep source resolution, only change codec. tenbit preserves a
// 10-bit/HDR source as 10-bit AV1 (yuv420p10le) — AV1 Main does 10-bit and
// canCopyAV1 now streams it by copy, so HDR survives end-to-end instead of
// being crushed to 8-bit. SDR stays 8-bit yuv420p. ffmpeg carries the source
// color_primaries/trc/space through (no -vf), so HDR metadata is preserved.
// HW 10-bit AV1 support is uneven, so only the software/NVENC/QSV paths take
// 10-bit; VideoToolbox/AMF stay 8-bit for safety.
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

// av1HLS mirrors h264HLS for AV1. Returns nil if no AV1 encoder is available
// (caller should fall back to h264). SVT-AV1 CRF is offset +7 from libx264 to
// match perceived quality; other params keep the same VBV-cap shape.
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
		// preset 6: ~10-15% better compression than preset 8, ~2× encode time.
		// On the user's M4 Max P-cores even 4K stays within the per-codec
		// timeout. "Reduce network stress" prioritizes bitrate over encode CPU.
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

// crfToQ maps libx264 CRF (lower=better, ~18-28) to VideoToolbox -q:v
// (higher=better, 1-100). CRF 18→75, 23→55, 28→35.
func crfToQ(crf int) int {
	q := 100 - crf*2
	q = max(q, 1)
	q = min(q, 100)
	return q
}

// av1Codec is the CODECS= attribute for an AV1 rung in the master playlist.
// Form: av01.<profile>.<level><tier>.<bitdepth>. Profile 0 (Main). Bit depth
// must match the actual stream (10-bit HDR mislabeled as .08 → some players
// refuse the stream). Levels: 4.0 (≤1080p30), 5.0 (≤2160p30), 5.1 (≤2160p60).
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

// hevcCodec is the CODECS= attribute for an HEVC rung. Main profile, Main tier,
// 8-bit. Level encoding: L<n*30> — L93=3.1, L120=4.0, L150=5.0, L153=5.1.
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

// av1Profiles is the AV1 quality ladder, parallel to hlsProfiles. Bitrates run
// ~40% below H.264 at matched perceptual quality (Netflix/YouTube AV1 ladders
// are similar). CRF values are AV1-native (1-63 range, ~30 ≈ libx264 CRF 23).
//
// Dormant: no callers yet. Wires in once the master playlist gains AV1 rungs
// and the segment muxer switches to fMP4 for the AV1 path.
var av1Profiles = map[string]hlsProfile{
	"144p":  {144, "scale=-2:144", "120k", "64k", 35},
	"240p":  {240, "scale=-2:240", "240k", "80k", 34},
	"360p":  {360, "scale=-2:360", "400k", "96k", 33},
	"480p":  {480, "scale=-2:480", "600k", "112k", 31},
	"720p":  {720, "scale=-2:720", "2200k", "128k", 28},
	"1080p": {1080, "scale=-2:1080", "4500k", "192k", 27},
	"2160p": {2160, "scale=-2:2160", "14000k", "256k", 26},
}

// hevcProfiles mirrors hlsProfiles for HEVC. ~30% bitrate reduction vs H.264.
// CRF values reuse the H.264 scale (HEVC CRF behaves comparably in this range).
var hevcProfiles = map[string]hlsProfile{
	"144p":  {144, "scale=-2:144", "140k", "64k", 28},
	"240p":  {240, "scale=-2:240", "280k", "80k", 27},
	"360p":  {360, "scale=-2:360", "450k", "96k", 26},
	"480p":  {480, "scale=-2:480", "700k", "112k", 24},
	"720p":  {720, "scale=-2:720", "2800k", "128k", 21},
	"1080p": {1080, "scale=-2:1080", "5500k", "192k", 20},
	"2160p": {2160, "scale=-2:2160", "17500k", "256k", 19},
}

// codecTimeout returns a per-encoder ffmpeg timeout for segment generation.
// Hardware encoders are nearly free; software AV1 needs real headroom at 4K.
// Used in place of the current 60s default once segments span codecs.
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

// fMP4 segment output. AV1 (and HEVC for some clients) cannot ride MPEG-TS,
// so the AV1 rung path needs ISO BMFF fragments served via #EXT-X-MAP+.m4s.
// These return the +movflags string only — the surrounding -i/-map/-f mp4 args
// belong with the call site that knows about input + cache layout.

// fmp4SegFlags is the -movflags value for a self-contained fragment segment
// (moof + mdat, no moov). Pair with `-f mp4` and a `.m4s` output.
func fmp4SegFlags() string {
	return "+frag_keyframe+empty_moov+default_base_moof+separate_moof"
}

// fmp4InitFlags is for the init segment served via #EXT-X-MAP — moov header
// only, no media samples. Combined with `-frames:v 0`.
func fmp4InitFlags() string {
	return "+empty_moov+default_base_moof"
}

// hevcHLS mirrors h264HLS / av1HLS for HEVC. Returns nil if no HEVC encoder is
// available. HW-preferred; libx265 fallback. CRF on libx265 behaves comparably
// to libx264 in this range, so the same hlsProfile.crf values work.
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

// segExt returns the on-disk file extension for a segment of the given codec.
// h264 still rides MPEG-TS; av1 + hevc need fMP4 (`.m4s`) for browser compat.
// Used by the future segPaths refactor.
func segExt(codec string) string {
	switch codec {
	case "av1", "hevc":
		return ".m4s"
	default:
		return ".ts"
	}
}

// segMime returns the HTTP Content-Type for a segment of the given codec.
// MPEG-TS for h264, fMP4 for av1 + hevc. Used by the future serveSegment
// refactor.
func segMime(codec string) string {
	switch codec {
	case "av1", "hevc":
		return "video/mp4"
	default:
		return "video/mp2t"
	}
}

// LogEncoders prints which encoders were selected for each codec at startup.
// Not auto-wired — call from main.go after lib init when desired. Useful when
// debugging "why is my AV1 path slow" (answer: probably libsvtav1 software).
func LogEncoders() {
	log.Printf("[media] encoders picked: h264=%s hevc=%s av1=%s",
		emptyNone(pickH264()), emptyNone(pickHEVC()), emptyNone(pickAV1()))
}

func emptyNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// canCopyAV1 reports whether an AV1-on-disk source can be streamed by
// copy-passthrough (remux into fMP4, no re-encode). Unlike canCopyVideo this
// does NOT require a ladder-height match: AV1 is served as a single
// source-resolution rung (no scaling), so any height is fine. Requirements:
// AV1 codec, 4:2:0 8- or 10-bit (AV1 Main profile — browsers that HW-decode
// AV1 do 10-bit, so preserving HDR by copy beats a lossy 8-bit re-encode),
// first keyframe near t=0.
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

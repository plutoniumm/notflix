package media

import (
	"os"
	"path/filepath"

	"notflix/server/library"
	"notflix/server/x/atom"
)

// srcInfo is expensive and was memory-only, so every restart re-paid it on the
// first open of every title. Measured on this library: a cold
// /api/hls/master took **31.9 seconds**, essentially all of it inside
// probePackets — a full-file `ffprobe -show_packets` that must read the whole
// source over the USB volume before the master playlist can be written. Five
// restarts in one evening is five times 31.9s per title touched.
//
// The derived values are tiny, so they go to a per-file sidecar rather than the
// KV store: kv.json is one blob read whole, and ~2000 bounds arrays of hundreds
// of floats would make every unrelated KV get pay for them.
//
// kfTimes itself is NOT persisted. It is only ever read to derive bounds (done,
// and persisted) and by canCopyAV1, which looks at len() and [0] — so one float
// and a count reproduce both behaviours exactly.
type srcInfoDisk struct {
	// Identity of the bytes this was derived from. A file rewritten in place by
	// convert or repair must re-probe, and a change to the segment grid must
	// invalidate every record — the same reason hlsURLVer exists.
	Size int64  `json:"size"`
	Mod  int64  `json:"mod"`
	Ver  string `json:"ver"`

	VCodec      string      `json:"vcodec"`
	VWidth      int         `json:"vw"`
	VHeight     int         `json:"vh"`
	VDAR        float64     `json:"vdar"`
	VBitDepth   int         `json:"vbd"`
	VTransfer   string      `json:"vtrc"`
	VPrimaries  string      `json:"vpri"`
	VColorspace string      `json:"vcsp"`
	VRotation   int         `json:"vrot"`
	VPixFmt     string      `json:"vpix"`
	AChannels   []int       `json:"ach"`
	ALayouts    []string    `json:"alay"`
	AMeta       []audioMeta `json:"ameta"`
	Dur         float64     `json:"dur"`
	KFFirst     float64     `json:"kf0"`
	KFCount     int         `json:"kfn"`
	Bounds      []float64   `json:"bounds"`
	SegBitrates []int       `json:"segbr"`
	PeakBitrate int         `json:"peak"`
	AvgBitrate  int         `json:"avg"`
}

func srcInfoPath(path string) string {
	return filepath.Join("./cache", "probe", library.Hash(filepath.Base(path))+".json")
}

func stampOf(path string) (size, mod int64, ok bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, 0, false
	}

	return fi.Size(), fi.ModTime().UnixNano(), true
}

// loadSrcInfo fills si from disk. Reports false on any mismatch, which simply
// means "probe it", never "serve something stale".
func loadSrcInfo(path string, si *srcInfo) bool {
	size, mod, ok := stampOf(path)
	if !ok {
		return false
	}

	var d srcInfoDisk
	if err := atom.LoadJSON(srcInfoPath(path), &d); err != nil {
		return false
	}
	if d.Size != size || d.Mod != mod || d.Ver != hlsURLVer || d.VCodec == "" {
		return false
	}

	si.vCodec, si.vPixFmt = d.VCodec, d.VPixFmt
	si.vWidth, si.vHeight, si.vDAR = d.VWidth, d.VHeight, d.VDAR
	si.vBitDepth, si.vRotation = d.VBitDepth, d.VRotation
	si.vTransfer, si.vPrimaries, si.vColorspace = d.VTransfer, d.VPrimaries, d.VColorspace
	si.aChannels, si.aLayouts, si.aMeta = d.AChannels, d.ALayouts, d.AMeta
	si.dur = d.Dur
	si.bounds, si.segBitrates = d.Bounds, d.SegBitrates
	si.peakBitrate, si.avgBitrate = d.PeakBitrate, d.AvgBitrate
	// One entry stands in for the whole array: canCopyAV1 reads len() and [0].
	if d.KFCount > 0 {
		si.kfTimes = []float64{d.KFFirst}
	}

	return true
}

func saveSrcInfo(path string, si *srcInfo) {
	size, mod, ok := stampOf(path)
	if !ok {
		return
	}

	d := srcInfoDisk{
		Size: size, Mod: mod, Ver: hlsURLVer,
		VCodec: si.vCodec, VPixFmt: si.vPixFmt,
		VWidth: si.vWidth, VHeight: si.vHeight, VDAR: si.vDAR,
		VBitDepth: si.vBitDepth, VRotation: si.vRotation,
		VTransfer: si.vTransfer, VPrimaries: si.vPrimaries, VColorspace: si.vColorspace,
		AChannels: si.aChannels, ALayouts: si.aLayouts, AMeta: si.aMeta,
		Dur: si.dur, KFCount: len(si.kfTimes),
		Bounds: si.bounds, SegBitrates: si.segBitrates,
		PeakBitrate: si.peakBitrate, AvgBitrate: si.avgBitrate,
	}
	if len(si.kfTimes) > 0 {
		d.KFFirst = si.kfTimes[0]
	}

	os.MkdirAll(filepath.Dir(srcInfoPath(path)), 0755)
	atom.SaveJSON(srcInfoPath(path), &d)
}

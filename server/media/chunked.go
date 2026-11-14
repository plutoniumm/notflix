package media

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"notflix/server/jobs"
	"notflix/server/library"
	"notflix/server/x/atom"
)

// Per-scene quality targeting (the av1an / Netflix per-shot idea), ffmpeg only.
// One CRF for a whole film spends the same quantiser on a static dialogue scene
// as on a battle, so the static scene is over-served and the battle starved.
// Instead the film is cut at scene changes, each chunk is probe-encoded at a
// fast preset at two CRFs and scored with VMAF against the source, and the
// chunk is then encoded for real at the highest CRF still predicted to reach
// targetVMAF. A file is encoded once and streamed for years, so the extra
// encode time is the cheap side of the trade.
//
// Chunks are cut by `trim` on the source's own timestamps (-copyts), never by
// -ss/-t: a half-open [start, end) partition of decoded frames puts every frame
// in exactly one chunk, where an output -t once dropped one per HLS segment.
const (
	// Mean VMAF (vmaf_v0.6.1, the 4K model above 1440p) each chunk must reach.
	// Calibrated to what the old fixed CRF 32 / preset 3 delivered on average,
	// so the gain comes from moving bits between scenes rather than from
	// raising the bar.
	targetVMAF = 93.0

	crfMin, crfMax = 24, 46

	chunkMin, chunkMax = 10.0, 60.0
	sceneThreshold     = 0.3

	// Two SVT processes per film measured faster in total than one at preset 3.
	chunkWorkers = 2
	// Probes run at a fast preset; slower presets only score higher at the same
	// CRF, so the CRF a probe picks is conservative for the final encode.
	probePreset = "10"
	// VMAF on every third frame: scoring every frame cost more than the probe
	// encodes themselves.
	vmafSubsample = 3
)

var probeCRFs = [2]int{30, 40}

func chunksRoot() string { return filepath.Join("cache", "av1chunks") }

// One dir per source BYTES: size+mtime in the name, so a replaced file never
// reuses another encode's chunks, and a restarted conversion resumes from the
// chunks it already finished.
func chunkDir(src string) string {
	key := library.Hash(filepath.Base(src))
	if fi, err := os.Stat(src); err == nil {
		key = fmt.Sprintf("%s-%d-%d", key, fi.Size(), fi.ModTime().Unix())
	}
	return filepath.Join(chunksRoot(), key)
}

// sweepChunks drops chunk dirs nothing has touched for two days: a conversion
// that is abandoned (source deleted, or converted elsewhere) would otherwise
// strand gigabytes of chunks on the boot disk. A live one touches its dir per
// chunk, and a resume within the window keeps its work.
func sweepChunks() {
	entries, err := os.ReadDir(chunksRoot())
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err == nil && time.Since(info.ModTime()) > 48*time.Hour {
			os.RemoveAll(filepath.Join(chunksRoot(), e.Name()))
			log.Printf("[convert] swept stale chunks %s", e.Name())
		}
	}
}

// planChunks turns scene cuts into chunk start times: every chunk starts on a
// cut and runs chunkMin..chunkMax seconds; a cut-free stretch longer than
// chunkMax is split evenly, and a tail shorter than chunkMin joins the chunk
// before it. The first start is always 0.
func planChunks(cuts []float64, dur float64) []float64 {
	starts := []float64{0}
	last := 0.0
	split := func(to float64) {
		if n := math.Ceil((to - last) / chunkMax); n > 1 {
			step := (to - last) / n
			for i := 1; i < int(n); i++ {
				starts = append(starts, last+step*float64(i))
			}
		}
	}
	sorted := append([]float64(nil), cuts...)
	sort.Float64s(sorted)
	for _, c := range sorted {
		if c-last < chunkMin || dur-c < chunkMin {
			continue
		}
		split(c)
		starts = append(starts, c)
		last = c
	}
	split(dur)
	return starts
}

// crfFor interpolates the highest CRF whose VMAF still reaches target from two
// probes (lower CRF first). Linear between the points, extrapolated past them,
// rounded toward the safe side and clamped.
func crfFor(lo, hi [2]float64, target float64) int {
	c := lo[0]
	switch {
	case lo[1] <= hi[1]:
		// Flat or inverted (a near-static chunk): more compression costs it no
		// quality, so it goes as far as allowed if it clears the bar at all.
		if hi[1] >= target {
			c = crfMax
		}
	default:
		c = lo[0] + (lo[1]-target)*(hi[0]-lo[0])/(lo[1]-hi[1])
	}
	return max(crfMin, min(crfMax, int(math.Floor(c))))
}

// sceneCuts decodes a small copy of the whole film once and returns the source
// timestamps (-copyts) of its scene changes, plus the first frame's timestamp,
// which the final mux needs to keep A/V sync. Cached beside the chunks.
func sceneCuts(src, dir string) (first float64, cuts []float64, err error) {
	cache := filepath.Join(dir, "cuts.json")
	var saved struct {
		First float64
		Cuts  []float64
	}
	if atom.LoadJSON(cache, &saved) == nil {
		return saved.First, saved.Cuts, nil
	}

	out := filepath.Join(dir, "scenes.txt")
	var args []string
	// Decode on the media engine where it can (a 4K HEVC film in software is the
	// slow part of this pass); frames come back to system memory on their own.
	if pickH264() == "h264_videotoolbox" {
		args = append(args, "-hwaccel", "videotoolbox")
	}
	args = append(args, "-y", "-copyts", "-i", src, "-map", "0:v:0", "-an", "-sn",
		"-vf", fmt.Sprintf("scale=320:-2,select='eq(n,0)+gt(scene,%g)',metadata=print:file=%s", sceneThreshold, out),
		"-f", "null", "-")
	stderr, err := library.FFRun{Nice: true, Args: args}.Run()
	if err != nil {
		return 0, nil, library.FFErr(stderr, err)
	}

	f, err := os.Open(out)
	if err != nil {
		return 0, nil, err
	}
	defer f.Close()
	var times []float64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if i := strings.Index(sc.Text(), "pts_time:"); i >= 0 {
			if t, err := strconv.ParseFloat(strings.Fields(sc.Text()[i+len("pts_time:"):])[0], 64); err == nil {
				times = append(times, t)
			}
		}
	}
	if len(times) == 0 {
		return 0, nil, fmt.Errorf("scene pass saw no frames")
	}
	saved.First, saved.Cuts = times[0], times[1:]
	atom.SaveJSON(cache, saved)
	return saved.First, saved.Cuts, nil
}

// chunkIn is the decode side of one chunk: a fast seek a little before it, then
// an exact frame selection on the source's own timestamps.
func chunkIn(src string, s, e float64, last bool) (in []string, vf string) {
	if s > 0 {
		in = append(in, "-ss", strconv.FormatFloat(max(0, s-5), 'f', 3, 64))
	}
	in = append(in, "-copyts", "-i", src)
	trim := "trim=start=" + strconv.FormatFloat(s, 'f', 6, 64)
	if !last {
		trim += ":end=" + strconv.FormatFloat(e, 'f', 6, 64)
	}
	return in, trim + ",setpts=PTS-STARTPTS"
}

// av1EncodeArgs is the file encode for src at crf: av1File plus the source's
// CICP, so HDR survives on encoders that honour it.
func av1EncodeArgs(src string, crf int) []string {
	si := getSrcInfo(src)
	args := av1File(crf, si.vBitDepth >= 10)
	for flag, val := range map[string]string{
		"-color_primaries": si.vPrimaries,
		"-color_trc":       si.vTransfer,
		"-colorspace":      si.vColorspace,
	} {
		if val != "" && val != "unknown" {
			args = append(args, flag, val)
		}
	}
	return args
}

// probeArgs is av1EncodeArgs at the probe preset, minus -svtav1-params: grain
// synthesis and tuning are final-encode concerns, and VMAF scores synthesised
// grain as error, which would drag every chosen CRF down.
func probeArgs(src string, crf int) []string {
	var out []string
	args := av1EncodeArgs(src, crf)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-preset":
			out = append(out, "-preset", probePreset)
			i++
		case "-svtav1-params":
			i++
		default:
			out = append(out, args[i])
		}
	}
	return out
}

// SVT-AV1 4.2 was seen to stall for good with every frame delivered (encoder
// thread parked in svt_av1_enc_get_packet, 0% CPU) — once, and not again in a
// dozen reruns — but one stalled chunk would freeze the whole conversion. So a
// chunk encode gets a deadline far past any real speed (30s per second of
// video at the slowest preset is ~0.8 fps at 24p) and one retry.
func encodeChunk(src, path string, s, e float64, last bool, enc []string, onPct func(float64)) error {
	in, vf := chunkIn(src, s, e, last)
	args := append(in, "-map", "0:v:0", "-an", "-sn", "-vf", vf)
	args = append(args, enc...)
	// The demuxer's time base, as for HLS segments: an encoder grid starting at
	// the chunk would re-time the source's frames.
	args = append(args, "-enc_time_base:v", "demux", "-f", "mp4", path+".part")
	run := library.FFRun{Args: args, Nice: true, Timeout: 10*time.Minute + time.Duration(max(e-s, chunkMax)*30)*time.Second}
	if onPct != nil && e > s {
		run.Duration, run.OnPct = e-s, onPct
	}
	var err error
	for try := 0; try < 2; try++ {
		var stderr string
		if stderr, err = run.Run(); err == nil {
			return os.Rename(path+".part", path)
		}
		os.Remove(path + ".part")
		err = library.FFErr(stderr, err)
		log.Printf("[convert] chunk %s attempt %d: %v", filepath.Base(path), try+1, err)
	}
	return err
}

func vmafModel(src string) string {
	if si := getSrcInfo(src); si.vWidth > 2560 || si.vHeight > 1440 {
		return "vmaf_4k_v0.6.1"
	}
	return "vmaf_v0.6.1"
}

// chunkVMAF scores an encoded chunk against the same frames of the source.
func chunkVMAF(src, enc, logPath string, s, e float64, last bool) (float64, error) {
	in, vf := chunkIn(src, s, e, last)
	args := append([]string{"-i", enc}, in...)
	args = append(args, "-lavfi", fmt.Sprintf(
		"[1:v]%s,format=yuv420p[r];[0:v]setpts=PTS-STARTPTS,format=yuv420p[d];[d][r]libvmaf=model=version=%s:n_subsample=%d:n_threads=8:log_fmt=json:log_path=%s",
		vf, vmafModel(src), vmafSubsample, logPath),
		"-f", "null", "-")
	if stderr, err := (library.FFRun{Args: args, Nice: true}).Run(); err != nil {
		return 0, library.FFErr(stderr, err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		return 0, err
	}
	var log struct {
		Pooled struct {
			VMAF struct{ Mean float64 } `json:"vmaf"`
		} `json:"pooled_metrics"`
	}
	if err := json.Unmarshal(raw, &log); err != nil {
		return 0, err
	}
	return log.Pooled.VMAF.Mean, nil
}

// targetCRF probes one chunk at both probe CRFs and interpolates.
func targetCRF(src, dir string, i int, s, e float64, last bool) (int, [2][2]float64, error) {
	var pts [2][2]float64
	for k, crf := range probeCRFs {
		p := filepath.Join(dir, fmt.Sprintf("p%04d.crf%d.mp4", i, crf))
		if err := encodeChunk(src, p, s, e, last, probeArgs(src, crf), nil); err != nil {
			return 0, pts, err
		}
		v, err := chunkVMAF(src, p, p+".json", s, e, last)
		os.Remove(p)
		os.Remove(p + ".json")
		if err != nil {
			return 0, pts, err
		}
		pts[k] = [2]float64{float64(crf), v}
	}
	return crfFor(pts[0], pts[1], targetVMAF), pts, nil
}

// finished reports a chunk already encoded by an earlier, interrupted run.
func finished(dir string, i int) string {
	entries, _ := os.ReadDir(dir)
	prefix := fmt.Sprintf("c%04d.crf", i)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".mp4") {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// encodeChunked writes src's video, scene-targeted, as one AV1 stream per chunk
// and returns the chunk paths in order plus the first frame's source timestamp.
func encodeChunked(src string, dur float64, name string) ([]string, float64, error) {
	dir := chunkDir(src)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, 0, err
	}
	first, cuts, err := sceneCuts(src, dir)
	if err != nil {
		return nil, 0, err
	}
	rel := make([]float64, len(cuts))
	for i, c := range cuts {
		rel[i] = c - first
	}
	starts := planChunks(rel, dur)
	n := len(starts)
	log.Printf("[convert] %s: %d scene cuts → %d chunks", name, len(cuts), n)

	var (
		mu       sync.Mutex
		paths    = make([]string, n)
		done     float64
		live     = map[int]float64{}
		firstErr error
		wg       sync.WaitGroup
	)
	report := func() {
		sum := done
		for _, v := range live {
			sum += v
		}
		progress.Update(name, math.Min(sum/dur*100, 99))
	}

	pool := jobs.NewPool(chunkWorkers)
	for i := range starts {
		s := starts[i]
		e := dur
		if i+1 < n {
			e = starts[i+1]
		}
		last := i == n-1
		// Timestamps are the source's own (-copyts), offset by its first frame.
		s, e = s+first, e+first
		if i == 0 {
			s = 0
		}

		wg.Add(1)
		pool.Go(func() {
			defer wg.Done()
			mu.Lock()
			stop := firstErr != nil
			mu.Unlock()
			if stop {
				return
			}

			path := finished(dir, i)
			if path == "" {
				crf, pts, err := targetCRF(src, dir, i, s, e, last)
				if err == nil {
					log.Printf("[convert] %s chunk %d/%d (%.0fs) crf %d — vmaf %.1f@%d, %.1f@%d",
						name, i+1, n, e-s, crf, pts[0][1], int(pts[0][0]), pts[1][1], int(pts[1][0]))
					path = filepath.Join(dir, fmt.Sprintf("c%04d.crf%d.mp4", i, crf))
					err = encodeChunk(src, path, s, e, last, av1EncodeArgs(src, crf), func(p float64) {
						mu.Lock()
						live[i] = (e - s) * p / 100
						report()
						mu.Unlock()
					})
				}
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					return
				}
			}

			mu.Lock()
			paths[i] = path
			delete(live, i)
			done += e - s
			report()
			mu.Unlock()
			os.Chtimes(dir, time.Now(), time.Now())
		})
	}
	wg.Wait()
	if firstErr != nil {
		return nil, 0, firstErr
	}
	return paths, first, nil
}

// remuxChunked is remux's AV1-encode path: the chunks are concatenated into one
// stream and muxed with the source's audio exactly as the single pass muxes it.
func remuxChunked(src, dst string, dur float64, name string, ac []string) error {
	tmp := dst + ".tmp"
	if fresh(tmp) {
		return fmt.Errorf("%s: tmp output busy (another encoder)", name)
	}
	os.Remove(tmp)

	paths, first, err := encodeChunked(src, dur, name)
	if err != nil {
		if strings.Contains(err.Error(), "No space left on device") {
			return errNoSpace
		}
		return err
	}

	var list strings.Builder
	for _, p := range paths {
		abs, _ := filepath.Abs(p)
		fmt.Fprintf(&list, "file '%s'\n", strings.ReplaceAll(abs, "'", `'\''`))
	}
	listPath := filepath.Join(chunkDir(src), "list.txt")
	if err := os.WriteFile(listPath, []byte(list.String()), 0o644); err != nil {
		return err
	}

	// The concat starts at 0; the single pass kept the video's own offset from
	// the file's start (B-frame delay), and audio sync depends on it.
	offset := 0.0
	if f, err := library.Probes.Format(src); err == nil {
		offset = max(0, first-f.StartTime.Seconds())
	}

	args := []string{
		"-itsoffset", strconv.FormatFloat(offset, 'f', 6, 64),
		"-f", "concat", "-safe", "0", "-i", listPath,
		"-i", src,
		"-map", "0:v:0", "-map", "1:a", "-c:v", "copy",
		"-map_metadata", "1", "-map_chapters", "1",
	}
	if mp4AudioOK(ac) {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args, "-c:a", pickAAC(), "-b:a", "192k")
		if anyUnnamedMultichannel(getSrcInfo(src)) {
			args = append(args, "-ac", "2")
		}
	}
	args = append(args, audioDispositionArgs(src)...)
	args = append(args, "-movflags", "+faststart", "-f", "mp4", tmp)

	if stderr, err := (library.FFRun{Args: args, Nice: true}).Run(); err != nil {
		log.Printf("ffmpeg mux error for %s:\n%s", name, stderr)
		os.Remove(tmp)
		if strings.Contains(stderr, "No space left on device") {
			return errNoSpace
		}
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	os.RemoveAll(chunkDir(src))
	return nil
}

package media

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"notflix/server/jobs"
	"notflix/server/library"
)

var progress = jobs.NewTracker()

func GetProgress() []jobs.Progress {
	return progress.List()
}

var processing atomic.Bool

func ProcessAll(lib *library.Library) {
	if !processing.CompareAndSwap(false, true) {
		log.Println("[ProcessAll] already running, skipping")
		return
	}
	defer processing.Store(false)

	jobs.WaitAria2()
	StatusReset()
	defer StatusFinish()

	// Clean is a tree-mutating pass (rename/move/flatten). Run alone before
	// fan-out so the concurrent phases below walk a settled tree. Cheap:
	// metadata-only, seconds not minutes. Every phase gates on IsBusy, so
	// clean itself is safe with respect to in-flight aria2 downloads.
	PhaseStart("clean")
	library.CleanAll(lib.Roots)
	PhaseEnd("clean")

	// Convert, scan+repair, thumbs, sprites, and whisper all fan out
	// concurrently. Each has its own worker pool and each skips busy files
	// (IsBusy = torrent-owned, sidecar-present, or freshly-written), so the
	// stages don't step on each other. toMP4 also dispatches downstream work
	// (sprite/thumb/whisper) per-file as each conversion completes — so a
	// file can gain its thumbnail while the next file is still transcoding.
	var wg sync.WaitGroup
	wg.Add(7)
	go func() {
		defer wg.Done()
		PhaseStart("convert")
		ConvertAll(lib)
		PhaseEnd("convert")
	}()
	go func() {
		defer wg.Done()
		PhaseStart("scan")
		library.ScanCorrupt(lib)
		PhaseSet("scan", 0, len(library.CorruptPaths()))
		PhaseEnd("scan")
		PhaseStart("repair")
		RepairAll(lib)
		PhaseEnd("repair")
	}()
	go func() {
		defer wg.Done()
		PhaseStart("thumbs")
		RegenerateThumbnails(lib, 0)
		PhaseEnd("thumbs")
	}()
	go func() {
		defer wg.Done()
		PhaseStart("sprites")
		BackfillSprites(lib)
		PhaseEnd("sprites")
	}()
	go func() {
		defer wg.Done()
		PhaseStart("whisper")
		jobs.BackfillWhisper(lib)
		PhaseEnd("whisper")
	}()
	// Blurbs read the .vtt sidecars whisper is still writing in the lane
	// beside this one, so a title transcribed during this sweep gets its
	// blurb on the next one. Deliberate: waiting on whisper would make an
	// optional, network-bound phase hold the sweep open for hours.
	go func() {
		defer wg.Done()
		PhaseStart("blurbs")
		jobs.BackfillBlurbs(lib, func(total, done int) { PhaseSet("blurbs", total, done) })
		PhaseEnd("blurbs")
	}()
	go func() {
		defer wg.Done()
		PhaseStart("skips")
		DetectSkips(lib)
		PhaseEnd("skips")
	}()
	wg.Wait()
	log.Println("[ProcessAll] done")
}

func IsProcessing() bool {
	return processing.Load()
}

func ConvertAll(lib *library.Library) {
	var wg sync.WaitGroup
	for _, root := range lib.Roots {
		if _, err := os.Stat(root); err != nil {
			continue
		}

		wg.Add(1)
		go func(root string) {
			defer wg.Done()
			convertRoot(root, jobs.NewPool(2))
		}(root)
	}

	wg.Wait()
	log.Println("ConvertAll: done")
}

func freeBytes(dir string) int64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return -1
	}
	return int64(st.Bavail) * int64(st.Bsize)
}

func convertRoot(root string, pool *jobs.Pool) {
	var wg sync.WaitGroup
	var diskFull atomic.Bool

	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		inValid := d.IsDir() || strings.HasPrefix(d.Name(), ".")
		if err != nil || inValid {
			return nil
		}

		if diskFull.Load() {
			return filepath.SkipAll
		}

		name := d.Name()
		if strings.HasSuffix(name, ".audio.tmp") || strings.HasSuffix(name, ".audio.tmp.mp4") {
			if !fresh(path) {
				if err := os.Remove(path); err == nil {
					log.Printf("[convert] removed stale temp: %s", name)
				}
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(d.Name()))
		isMP4 := ext == ".mp4"
		switch ext {
		case ".mp4":
		case ".mkv", ".mov", ".webm", ".avi", ".flv", ".wmv", ".m4v", ".mpg", ".mpeg", ".ts", ".3gp":
		default:
			return nil
		}

		if isMP4 && !audioNeedsTranscode(path) {
			return nil
		}

		if library.IsBusy(path) {
			log.Printf("[convert] busy, skipping: %s", d.Name())
			return nil
		}
		info, _ := d.Info()

		if info != nil {
			free := freeBytes(filepath.Dir(path))
			if free >= 0 && free < info.Size()+int64(512<<20) {
				log.Printf("[convert] low disk space on %s (free=%dMB), skipping root", root, free>>20)
				diskFull.Store(true)
				return filepath.SkipAll
			}
		}

		pool.Acquire()
		wg.Add(1)
		PhaseInc("convert", 1, 0)

		go func(p string, audioOnly bool) {
			defer wg.Done()
			defer pool.Release()
			defer PhaseInc("convert", 0, 1)

			var err error
			if audioOnly {
				err = remuxAudio(p)
			} else {
				err = toMP4(p, root)
			}
			if err == errNoSpace {
				diskFull.Store(true)
			}
		}(path, isMP4)

		return nil
	})

	wg.Wait()
}

var errNoSpace = fmt.Errorf("no space left on device")

func toMP4(srcPath, root string) error {
	dir := filepath.Dir(srcPath)
	srcName := filepath.Base(srcPath)
	cleanedBase := library.CleanName(srcName)

	mp4Path := filepath.Join(dir, cleanedBase+".mp4")
	name := srcName

	// Both a source and a same-basename .mp4 exist. Historically we assumed
	// the .mp4 was a partial-write from a previous killed run and deleted it
	// to restart convert — but that destroys a good .mp4 whenever the prior
	// run finished the convert but was killed before removing the source
	// (data-loss root cause of the Modern Family Joy corruption, 2026-07-14).
	// Probe the .mp4 first: if it's readable, the previous convert really did
	// finish, so drop the source and be done. Only when the .mp4 is itself
	// unreadable is it safe to blow away and re-convert.
	// A fresh-mtime output (.mp4 or its .tmp) means another encoder — possibly
	// an ffmpeg orphaned by a server restart — is writing it RIGHT NOW.
	// Probing it reads "broken" (no moov yet) and the old delete-and-restart
	// path would stomp the live encode: the orphan keeps writing an unlinked
	// inode and the finished file is silently lost (Bleach 001–010, 2026-07-29).
	// Skip and let a later pass handle it once it's quiet.
	if fresh(mp4Path) || fresh(mp4Path+".tmp") {
		log.Printf("[convert] %s: output being written by another encoder, skipping", name)
		return nil
	}
	if _, err := os.Stat(mp4Path); err == nil {
		if duration(mp4Path) > 0 {
			log.Printf("[convert] %s: existing .mp4 is valid, dropping source", name)
			os.Remove(srcPath)
			library.PipelineForget(srcPath)
			library.PipelineMark(mp4Path, "convert", library.StepDone)
			return nil
		}
		log.Printf("[convert] %s: existing .mp4 is broken, restarting", name)
		os.Remove(mp4Path)
	}

	progress.Update(name, 0)
	defer progress.Finish(name)

	dur := duration(srcPath)
	if dur <= 0 {
		log.Printf("[convert] skipping unreadable/corrupt file: %s", name)
		return nil
	}

	library.PipelineMark(srcPath, "convert", library.StepRunning)

	extractAllSubs(srcPath, filepath.Join(dir, cleanedBase))

	if err := remux(srcPath, mp4Path, dur, name); err != nil {
		log.Printf("Conversion failed %s: %v", name, err)
		library.PipelineMark(srcPath, "convert", library.StepFailed)
		return err
	}

	os.Remove(srcPath)
	library.PipelineForget(srcPath) // source is gone; ledger key is now mp4Path
	library.PipelineMark(mp4Path, "convert", library.StepDone)

	// remux() now stores AV1 on disk (copy if the source was already AV1, else
	// a one-time libsvtav1 re-encode). The mark is honest: the bytes really are
	// AV1, and HLS serves them via single-rung copy-passthrough (remux into
	// fMP4, no live transcode) — jank-free. HLS endpoints key videos by their
	// library-relative path; library.Hash hashes that exact string, so the
	// marker must use the same key.
	if rel, err := filepath.Rel(root, mp4Path); err == nil {
		if err := SetMediaCodec(rel, CodecAV1); err != nil {
			log.Printf("[convert] codec mark failed for %s: %v", name, err)
		}
	}

	// Downstream per-file dispatch: each stage has its own pool and skips if
	// its output already exists, so fan-out is safe and non-blocking. The
	// bulk backfill loops in ProcessAll still pick up anything convert misses.
	GenerateSpriteAsync(mp4Path, library.Hash(filepath.Base(mp4Path)))
	GenerateThumbAsync(mp4Path)
	jobs.AutoWhisper(mp4Path)

	return nil
}

func duration(path string) float64 {
	f, err := library.Prober.Format(context.Background(), path)
	if err != nil {
		return 0
	}
	return f.Duration.Duration.Seconds()
}

func codecs(videoPath string) (videoCodec string, audioCodecs []string) {
	streams, err := library.Prober.Streams(context.Background(), videoPath)
	if err != nil {
		return "", nil
	}

	for _, s := range streams {
		switch s.CodecType {
		case "video":
			if videoCodec == "" {
				videoCodec = s.CodecName
			}
		case "audio":
			audioCodecs = append(audioCodecs, s.CodecName)
		}
	}
	return
}

func audioOK(codec string) bool {
	switch strings.ToLower(codec) {
	case "aac", "mp3":
		return true
	}
	return false
}

func mp4AudioOK(codecs []string) bool {
	for _, c := range codecs {
		if !audioOK(c) {
			return false
		}
	}
	return len(codecs) > 0
}

// audioDispositionArgs marks the English audio track as the container default
// (every other audio track's default flag cleared), so direct-MP4 playback
// picks English. Order follows `-map 0:a` (output audio i == source audio i).
// No-op when there's a single audio track, or no English-tagged track — then
// ffmpeg's "first track is default" is left untouched.
func audioDispositionArgs(srcPath string) []string {
	streams, err := library.Prober.Streams(context.Background(), srcPath)
	if err != nil {
		return nil
	}

	var langs []string
	for _, s := range streams {
		if s.CodecType == "audio" {
			langs = append(langs, strings.ToLower(s.Tags["language"]))
		}
	}
	if len(langs) < 2 {
		return nil
	}

	eng := -1
	for i, l := range langs {
		if l == "eng" || l == "en" || l == "english" {
			eng = i
			break
		}
	}
	if eng < 0 {
		return nil
	}

	args := make([]string, 0, len(langs)*2)
	for i := range langs {
		flag := "0"
		if i == eng {
			flag = "default"
		}
		args = append(args, fmt.Sprintf("-disposition:a:%d", i), flag)
	}
	return args
}

func audioNeedsTranscode(path string) bool {
	streams, err := library.Prober.Streams(context.Background(), path)
	if err != nil {
		return false
	}
	for _, s := range streams {
		if s.CodecType == "audio" && !audioOK(s.CodecName) {
			return true
		}
	}
	return false
}

func remuxAudio(srcPath string) error {
	name := filepath.Base(srcPath)

	progress.Update(name, 0)
	defer progress.Finish(name)

	dur := duration(srcPath)
	if dur <= 0 {
		log.Printf("[convert] skipping unreadable/corrupt file: %s", name)
		return nil
	}

	// This remux maps only video+audio, so any embedded subtitle streams are
	// dropped from the rebuilt MP4. Extract them to sidecars first or they're
	// gone for good (toMP4 already does this; the audio-only path didn't).
	extractAllSubs(srcPath, strings.TrimSuffix(srcPath, filepath.Ext(srcPath)))

	tmp := srcPath + ".audio.tmp"

	vc, _ := codecs(srcPath)
	args := []string{
		"-i", srcPath,
		"-map", "0:v:0", "-map", "0:a", "-map_chapters", "-1",
		"-c:v", "copy",
	}
	if vc == "hevc" {
		args = append(args, "-tag:v", "hvc1")
	}
	args = append(args, "-c:a", "aac", "-b:a", "192k")
	if anyUnnamedMultichannel(getSrcInfo(srcPath)) {
		args = append(args, "-ac", "2") // unnamed multichannel → AAC encoder fails; downmix
	}
	args = append(args, audioDispositionArgs(srcPath)...)
	args = append(args, "-movflags", "+faststart", "-f", "mp4", tmp)

	stderr, err := library.FFRun{
		Args:     args,
		Duration: dur,
		OnPct:    func(p float64) { progress.Update(name, p) },
		Nice:     true,
	}.Run()
	if err != nil {
		log.Printf("ffmpeg audio remux error for %s:\n%s", name, stderr)
		os.Remove(tmp)
		if strings.Contains(stderr, "No space left on device") {
			return errNoSpace
		}
		return err
	}

	if err := os.Rename(tmp, srcPath); err != nil {
		return err
	}
	srcInfoMap.Delete(srcPath) // rewritten in place; the probe cache is now stale

	// `-c:v copy` kept the source's AV1 video, so the file on disk really is
	// AV1 — and without the mark MediaCodec answers h264, which makes HLS
	// decode AV1 and re-encode H.264 per segment: exactly the live transcode
	// the AV1 path exists to avoid. toMP4 stamps its output the same way.
	if vc == "av1" {
		if err := SetMediaCodec(srcPath, CodecAV1); err != nil {
			log.Printf("[convert] codec mark failed for %s: %v", name, err)
		}
	}

	return nil
}

// fresh reports whether path exists and was modified in the last 10 minutes —
// the signature of a file some process is still writing.
func fresh(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && time.Since(fi.ModTime()) < 10*time.Minute
}

func remux(src, dst string, durationSec float64, name string) error {
	tmp := dst + ".tmp"
	if fresh(tmp) {
		return fmt.Errorf("%s: tmp output busy (another encoder)", name)
	}
	os.Remove(tmp)

	vc, ac := codecs(src)
	args := []string{"-i", src, "-map", "0:v:0", "-map", "0:a"}

	// New library is stored AV1: copy if the source is already AV1, otherwise
	// a one-time re-encode (libsvtav1; HW AV1 if ever available). This is the
	// slow part of conversion but it's a background job and pays for itself —
	// the stored AV1 then streams by copy-passthrough with no live transcode.
	if vc == "av1" {
		args = append(args, "-c:v", "copy")
	} else {
		// Preserve a 10-bit / HDR source as 10-bit AV1 rather than crushing it
		// to 8-bit SDR (no zscale here for a correct tonemap anyway). Re-apply
		// the source CICP so HDR metadata survives the re-encode on encoders
		// that honor it (SVT-AV1 keeps the matrix; HW keeps all).
		si := getSrcInfo(src)
		args = append(args, av1File(32, si.vBitDepth >= 10)...)
		for flag, val := range map[string]string{
			"-color_primaries": si.vPrimaries,
			"-color_trc":       si.vTransfer,
			"-colorspace":      si.vColorspace,
		} {
			if val != "" && val != "unknown" {
				args = append(args, flag, val)
			}
		}
	}

	if mp4AudioOK(ac) {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args, "-c:a", pickAAC(), "-b:a", "192k")
		if anyUnnamedMultichannel(getSrcInfo(src)) {
			args = append(args, "-ac", "2") // unnamed multichannel → AAC encoder fails; downmix
		}
	}

	args = append(args, audioDispositionArgs(src)...)
	args = append(args, "-movflags", "+faststart", "-f", "mp4", tmp)

	stderr, err := library.FFRun{
		Args:     args,
		Duration: durationSec,
		OnPct:    func(p float64) { progress.Update(name, p) },
		Nice:     true,
	}.Run()
	if err != nil {
		log.Printf("ffmpeg error for %s:\n%s", name, stderr)
		os.Remove(tmp)
		if strings.Contains(stderr, "No space left on device") {
			return errNoSpace
		}
		return err
	}

	return os.Rename(tmp, dst)
}

func extractAllSubs(srcPath, base string) {
	streams, err := library.Prober.Streams(context.Background(), srcPath)
	if err != nil {
		return
	}

	textCodecs := map[string]bool{"subrip": true, "srt": true, "ass": true, "ssa": true, "webvtt": true, "mov_text": true, "text": true}
	langCount := map[string]int{}

	for _, s := range streams {
		if s.CodecType != "subtitle" || !textCodecs[strings.ToLower(s.CodecName)] {
			continue
		}

		lang := "und"
		if l, ok := s.Tags["language"]; ok && l != "" {
			lang = strings.ToLower(l)
		}

		langCount[lang]++
		outPath := base + "." + lang + ".vtt"
		if langCount[lang] > 1 {
			outPath = fmt.Sprintf("%s.%s%d.vtt", base, lang, langCount[lang])
		}

		if _, err := os.Stat(outPath); err == nil {
			continue
		}

		if stderr, err := library.FF("-y", "-i", srcPath,
			"-map", fmt.Sprintf("0:%d", s.Index),
			"-c:s", "webvtt", outPath); err != nil {
			log.Printf("[convert] sub extraction failed (stream %d, %s): %v %s", s.Index, lang, err, stderr)
			os.Remove(outPath)
		} else {
			log.Printf("[convert] extracted sub: %s", filepath.Base(outPath))
		}
	}
}

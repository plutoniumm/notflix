package media

import (
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

	// Tree-mutating (rename/move/flatten): run alone so the concurrent phases
	// below walk a settled tree.
	PhaseStart("clean")
	library.CleanAll(lib.Roots)
	PhaseEnd("clean")

	// Fan-out is safe because every stage owns its pool and gates on IsBusy
	// (torrent-owned, sidecar-present, or freshly-written).
	var wg sync.WaitGroup
	wg.Add(6)
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

	// Thumbs and sprites stay concurrent with each other, but posters runs
	// strictly after both: it reads the sprite sheet and it OVERWRITES the
	// thumbnail, so overlapping with a thumbs pass that writes the same path
	// would sometimes stomp a chosen poster with a fixed-timestamp grab.
	// BackfillSprites only queues, so a sheet that lands late is picked up by
	// the next sweep rather than skipped forever.
	go func() {
		defer wg.Done()

		var art sync.WaitGroup
		art.Add(2)
		go func() {
			defer art.Done()
			PhaseStart("thumbs")
			RegenerateThumbnails(lib, 0)
			PhaseEnd("thumbs")
		}()
		go func() {
			defer art.Done()
			PhaseStart("sprites")
			BackfillSprites(lib)
			PhaseEnd("sprites")
		}()
		art.Wait()

		PhaseStart("posters")
		PosterAll(lib)
		PhaseEnd("posters")

		// Same sprite sheet, same 1-wide vision gate — running it beside
		// posters would only queue behind it.
		PhaseStart("anim")
		AnimationAll(lib)
		PhaseEnd("anim")
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

	// Never blow away an existing .mp4 to restart: a fresh mtime on it or its
	// .tmp means another encoder (possibly one orphaned by a restart) is still
	// writing, and a probe reads "broken" until moov lands — deleting loses the
	// finished file. Probe first; only a quiet, unreadable .mp4 is safe to redo.
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

	// Before AutoWhisper below: it skips a file that already has an English
	// sidecar, and it keys that check on the VIDEO's basename. Leaving the
	// release's own subtitles on the pre-conversion stem both stranded them and
	// bought a redundant transcription of a file that already had subs.
	if n := library.MoveSidecars(strings.TrimSuffix(srcPath, filepath.Ext(srcPath)),
		filepath.Join(dir, cleanedBase)); n > 0 {
		log.Printf("[convert] %s: carried %d sidecar(s) onto the converted name", name, n)
	}

	os.Remove(srcPath)
	library.PipelineForget(srcPath) // source is gone; ledger key is now mp4Path
	library.PipelineMark(mp4Path, "convert", library.StepDone)

	// The bytes really are AV1 now; without the mark HLS live-transcodes them.
	if rel, err := filepath.Rel(root, mp4Path); err == nil {
		if err := SetMediaCodec(rel, CodecAV1); err != nil {
			log.Printf("[convert] codec mark failed for %s: %v", name, err)
		}
	}

	GenerateSpriteAsync(mp4Path, library.Hash(filepath.Base(mp4Path)))
	GenerateThumbAsync(mp4Path)
	jobs.AutoWhisper(mp4Path)

	return nil
}

func duration(path string) float64 {
	f, err := library.Probes.Format(path)
	if err != nil {
		return 0
	}
	return f.Duration.Duration.Seconds()
}

func codecs(videoPath string) (videoCodec string, audioCodecs []string) {
	streams, err := library.Probes.Streams(videoPath)
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

// Marks the English audio track as the container default. Order follows
// `-map 0:a` — output audio i == source audio i.
func audioDispositionArgs(srcPath string) []string {
	streams, err := library.Probes.Streams(srcPath)
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
	streams, err := library.Probes.Streams(path)
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

	// This remux maps only video+audio, so embedded subs are dropped — extract
	// sidecars first or they're gone for good.
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
	library.Probes.Forget(srcPath)

	// `-c:v copy` kept the source's AV1; without the mark MediaCodec answers
	// h264 and HLS re-encodes every segment.
	if vc == "av1" {
		if err := SetMediaCodec(srcPath, CodecAV1); err != nil {
			log.Printf("[convert] codec mark failed for %s: %v", name, err)
		}
	}

	return nil
}

// fresh: modified in the last 10 minutes — something is still writing it.
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

	if vc == "av1" {
		args = append(args, "-c:v", "copy")
	} else {
		// Preserve a 10-bit/HDR source as 10-bit AV1 (no zscale here for a correct
		// tonemap) and re-apply the source CICP so HDR survives on encoders that
		// honor it.
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
	streams, err := library.Probes.Streams(srcPath)
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

		if stderr, err := library.FFNice("-y", "-i", srcPath,
			"-map", fmt.Sprintf("0:%d", s.Index),
			"-c:s", "webvtt", outPath); err != nil {
			log.Printf("[convert] sub extraction failed (stream %d, %s): %v %s", s.Index, lang, err, stderr)
			os.Remove(outPath)
		} else {
			log.Printf("[convert] extracted sub: %s", filepath.Base(outPath))
		}
	}
}

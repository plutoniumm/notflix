package jobs

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"notflix/server/library"
)

// ONE concurrent run: whisper is CPU-heavy and conversion already keeps
// several ffmpegs busy; stacking them breaks interactivity for everyone.
var autoWhisperPool = NewPool(1)

// Dedupes the conversion hook and BackfillWhisper hitting the same file.
var autoWhisperSeen sync.Map

// Any of these means "already subtitled, skip"; .whisper.vtt for idempotency.
var englishSubExts = []string{
	".whisper.vtt",
	".en.vtt", ".eng.vtt", ".english.vtt",
	".en.srt", ".eng.srt", ".english.srt",
}

// The plain, unsuffixed sidecar — the most common shape, and it was missing
// above: 50 files (47 of them City Hunter episodes that ship English subs) got
// fully re-transcribed for nothing, each producing a worse track (untranslated,
// run-on cues) next to the good one. Unsuffixed carries no language though, so
// it only counts when it sniffs Latin-script: a Japanese-only sidecar must still
// let whisper+translate add an English track.
var plainSubExts = []string{".vtt", ".srt"}

// >80% of sampled letters ASCII. A UTF-16 sidecar reads as mostly non-letters
// and fails this, which errs toward transcribing — the safe direction.
func latinScript(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 8192)
	n, _ := io.ReadFull(f, buf)
	if n < 32 {
		return false
	}

	letters, ascii := 0, 0
	for _, r := range string(buf[:n]) {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if r <= unicode.MaxASCII {
			ascii++
		}
	}
	return letters > 0 && float64(ascii)/float64(letters) > 0.8
}

func hasEnglishSub(mp4Path string) bool {
	base := strings.TrimSuffix(mp4Path, filepath.Ext(mp4Path))
	for _, ext := range englishSubExts {
		if _, err := os.Stat(base + ext); err == nil {
			return true
		}
	}
	for _, ext := range plainSubExts {
		if p := base + ext; latinScript(p) {
			return true
		}
	}
	return false
}

// Best-effort: silently skips when subs already exist or python is missing.
func AutoWhisper(srcMP4 string) {
	if hasEnglishSub(srcMP4) {
		return
	}
	if _, loaded := autoWhisperSeen.LoadOrStore(srcMP4, true); loaded {
		return
	}

	// Outer goroutine keeps pool.Acquire off the caller — the walker must not
	// block on a busy pool.
	go autoWhisperPool.Go(func() {
		defer autoWhisperSeen.Delete(srcMP4)

		// Another path may have produced the vtt while we waited.
		if hasEnglishSub(srcMP4) {
			return
		}
		// May have been moved by CleanAll, deleted, or flagged corrupt while
		// queued — extraction would just throw.
		if _, err := os.Stat(srcMP4); err != nil {
			return
		}
		if library.IsCorrupt(srcMP4) || library.IsBusy(srcMP4) {
			return
		}
		// python check deferred to runJob — 270 fork+execs in a backfill walk.
		dst := whisperVTTOf(srcMP4)
		job := &WhisperJob{Status: "pending"}
		log.Printf("[auto-whisper] start %s", filepath.Base(srcMP4))
		library.PipelineMark(srcMP4, "whisper", library.StepRunning)
		runJob(srcMP4, dst, srcMP4, job)
		status, msg := job.read()
		if status == "done" {
			log.Printf("[auto-whisper] done %s", filepath.Base(srcMP4))
			library.PipelineMark(srcMP4, "whisper", library.StepDone)
		} else {
			log.Printf("[auto-whisper] failed %s: %s", filepath.Base(srcMP4), msg)
			library.PipelineMark(srcMP4, "whisper", library.StepFailed)
		}
	})
}

// Idempotent and non-blocking: autoWhisperSeen dedupes, the 1-wide pool
// throttles, hasEnglishSub catches files that finished in the interim.
func BackfillWhisper(lib *library.Library) {
	queued := 0
	for _, root := range lib.Roots {
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") {
				return nil
			}
			if strings.ToLower(filepath.Ext(d.Name())) != ".mp4" {
				return nil
			}
			if hasEnglishSub(path) {
				return nil
			}
			if library.IsCorrupt(path) || library.IsBusy(path) {
				return nil
			}
			AutoWhisper(path)
			queued++
			return nil
		})
	}
	if queued > 0 {
		log.Printf("[auto-whisper] backfill queued %d (1-wide pool — will grind)", queued)
	}
}

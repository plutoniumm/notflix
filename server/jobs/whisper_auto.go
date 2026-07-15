package jobs

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"notflix/server/library"
)

// AutoWhisper runs the whisper transcription pipeline for a newly-converted
// MP4 in the background, IF:
//   - faster-whisper is installed
//   - no English `.vtt` sidecar already exists (extracted embedded sub or
//     existing manual sub takes precedence; whisper is the fallback)
//   - no `.whisper.vtt` already exists (idempotency — re-running conversion
//     for the same file shouldn't redo whisper)
//
// Throttled to ONE concurrent run: whisper is CPU-heavy and conversion
// already keeps up to 3 ffmpegs busy. Stacking whisper alongside breaks
// interactivity for everyone.
var autoWhisperPool = NewPool(1)

// autoWhisperSeen prevents double-firing when ProcessAll runs twice or the
// conversion hook fires for a file that's already being processed.
var autoWhisperSeen sync.Map

// englishSubExts: extensions we treat as "user has English subs, skip
// whisper." `whisper.vtt` is included for idempotency.
var englishSubExts = []string{
	".whisper.vtt",
	".en.vtt", ".eng.vtt", ".english.vtt",
	".en.srt", ".eng.srt", ".english.srt",
}

func hasEnglishSub(mp4Path string) bool {
	base := strings.TrimSuffix(mp4Path, filepath.Ext(mp4Path))
	for _, ext := range englishSubExts {
		if _, err := os.Stat(base + ext); err == nil {
			return true
		}
	}
	return false
}

// AutoWhisper queues a background whisper run for srcMP4. Returns immediately
// (the dispatcher goroutine handles pool.Acquire, which would otherwise block
// the caller until a worker slot is free).
//
// Best-effort: silently skips if subs already exist or python is missing;
// errors only show up in logs. The autoWhisperSeen map dedupes when the
// conversion hook and BackfillWhisper both target the same file.
func AutoWhisper(srcMP4 string) {
	if hasEnglishSub(srcMP4) {
		return
	}
	if _, loaded := autoWhisperSeen.LoadOrStore(srcMP4, true); loaded {
		return
	}

	// Fire-and-forget: the outer goroutine is cheap (~few KB stack), keeps
	// the pool's 1-wide throttle, and unblocks the walker immediately.
	go autoWhisperPool.Go(func() {
		defer autoWhisperSeen.Delete(srcMP4)

		// Re-check inside the worker: another path (manual sub download,
		// embedded extraction) may have produced the vtt while we waited.
		if hasEnglishSub(srcMP4) {
			return
		}
		// The file may have been moved (CleanAll flatten) or deleted while
		// queued, or flagged corrupt by ScanCorrupt. Either way extraction
		// just throws "moov atom not found" / "No such file" — skip quietly.
		if _, err := os.Stat(srcMP4); err != nil {
			return
		}
		if library.IsCorrupt(srcMP4) || library.IsBusy(srcMP4) {
			return
		}
		// The python check is deferred to runJob — checking 270 times during
		// a backfill walk would burn ~2min on fork+exec alone.
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

// BackfillWhisper walks every root and queues AutoWhisper for each MP4 that
// doesn't already have an English sidecar. The pool throttles actual whisper
// runs to 1 at a time; the walker returns immediately because AutoWhisper is
// non-blocking.
//
// Idempotent: re-running while a previous backfill is still grinding is safe
// (autoWhisperSeen dedupes; hasEnglishSub catches files that finished in the
// interim).
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

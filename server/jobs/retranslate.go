package jobs

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"notflix/server/library"
	"notflix/server/x/atom"
)

// Whisper's ASR output and its English translation fail independently, and when
// the translate step fails the sidecar ships in the source language. A library
// scan on 2026-10-15 found 24,609 such cues across 68 of 181 .whisper.vtt
// files: 46 fully untranslated (ollama unreachable for the whole run) and 22
// partially, nine of those missing exactly 50 cues — one lost batch each.
//
// Re-running whisper to fix them would redo hours of ASR that was never wrong.
// This pass re-translates the text in place instead.
//
// It edits LINES, not parsed cues: parseCues is the right reader for blurbs but
// drops `[DOOR SLAMS]` and `♪` cues, and a repair pass must not quietly delete
// cues from a file it was asked to fix. Substituting text lines leaves every
// timing byte untouched.

// scripts a translate pass should have removed. Latin is absent on purpose:
// Spanish or French left untranslated is readable, and guessing from accents
// would false-positive on English loanwords.
var foreignScripts = []*unicode.RangeTable{
	unicode.Hiragana, unicode.Katakana, unicode.Han, unicode.Hangul,
	unicode.Cyrillic, unicode.Arabic, unicode.Devanagari, unicode.Thai,
	unicode.Hebrew, unicode.Greek,
}

func foreignRunes(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsOneOf(foreignScripts, r) {
			n++
		}
	}
	return n
}

// langOf names the source language for the translate prompt. It is a hint only
// — a wrong guess still translates — so script is signal enough, and kana is
// checked before Han because Japanese mixes the two.
func langOf(s string) string {
	has := func(t *unicode.RangeTable) bool {
		return strings.IndexFunc(s, func(r rune) bool { return unicode.Is(t, r) }) >= 0
	}
	switch {
	case has(unicode.Hangul):
		return "Korean"
	case has(unicode.Hiragana) || has(unicode.Katakana):
		return "Japanese"
	case has(unicode.Han):
		return "Chinese"
	case has(unicode.Devanagari):
		return "Hindi"
	case has(unicode.Cyrillic):
		return "Russian"
	case has(unicode.Arabic):
		return "Arabic"
	case has(unicode.Thai):
		return "Thai"
	case has(unicode.Hebrew):
		return "Hebrew"
	case has(unicode.Greek):
		return "Greek"
	}
	return "foreign"
}

// isCueText is true for a line carrying subtitle text rather than structure.
func isCueText(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" || strings.Contains(t, "-->") || t == "WEBVTT" {
		return false
	}
	if strings.HasPrefix(t, "WEBVTT") || strings.HasPrefix(t, "NOTE") ||
		strings.HasPrefix(t, "STYLE") || strings.HasPrefix(t, "REGION") {
		return false
	}
	// A bare integer is a cue identifier, not dialogue.
	return strings.IndexFunc(t, func(r rune) bool { return !unicode.IsDigit(r) }) >= 0
}

// UntranslatedCues counts the lines a retranslate pass would act on. Cheap
// enough to call on every sidecar in a walk.
func UntranslatedCues(raw string) int {
	n := 0
	for _, line := range strings.Split(raw, "\n") {
		if isCueText(line) && foreignRunes(line) > 0 {
			n++
		}
	}
	return n
}

// RetranslateVTT rewrites the untranslated lines of one sidecar, returning how
// many it fixed. The file is left alone unless most of the batch came back:
// a mostly-failed run would otherwise interleave two languages inside cues
// that at least read consistently now.
func RetranslateVTT(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")

	var at []int
	var src []string
	for i, line := range lines {
		if isCueText(line) && foreignRunes(line) > 0 {
			at = append(at, i)
			src = append(src, strings.TrimSpace(line))
		}
	}
	if len(src) == 0 {
		return 0, nil
	}

	// The glossary wants the video path: sibling English sidecars are where the
	// correct spelling of every character name already lives.
	vid := strings.TrimSuffix(path, ".whisper.vtt")
	out, err := TranslateSegments(src, langOf(strings.Join(src, "\n")), ShowGlossary(vid+".mp4"))
	if err != nil {
		return 0, err
	}

	fixed := 0
	for j, i := range at {
		t := strings.TrimSpace(out[j])
		// TranslateSegments hands back the original for anything it could not
		// place, so an unchanged or still-foreign line is a miss, not a result.
		if t == "" || t == src[j] || foreignRunes(t) > 0 {
			continue
		}
		lines[i] = t
		fixed++
	}

	if fixed*2 < len(src) {
		return 0, nil
	}
	if err := atom.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return 0, err
	}
	return fixed, nil
}

// RetranslatePool is 1-wide for the same reason autoWhisperPool is: ollama
// serves one request per model, so parallel batches only queue and then time
// out together.
var retranslatePool = NewPool(1)

// RetranslateAll walks every root for .whisper.vtt sidecars still carrying
// source-language cues and repairs them one at a time. Idempotent: a repaired
// file has nothing left to match.
func RetranslateAll(lib *library.Library) {
	type job struct {
		path string
		n    int
	}
	var todo []job

	for _, root := range lib.Roots {
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".whisper.vtt") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			if n := UntranslatedCues(string(raw)); n > 0 {
				todo = append(todo, job{path, n})
			}
			return nil
		})
	}
	if len(todo) == 0 {
		log.Printf("[retranslate] nothing to do")
		return
	}

	total := 0
	for _, j := range todo {
		total += j.n
	}
	log.Printf("[retranslate] %d sidecars, %d cues (1-wide pool — will grind)", len(todo), total)

	for _, j := range todo {
		j := j
		retranslatePool.Go(func() {
			fixed, err := RetranslateVTT(j.path)
			switch {
			case err != nil:
				log.Printf("[retranslate] %s: %v", filepath.Base(j.path), err)
			case fixed == 0:
				log.Printf("[retranslate] %s: no usable translations, left as-is", filepath.Base(j.path))
			default:
				log.Printf("[retranslate] %s: %d of %d cues", filepath.Base(j.path), fixed, j.n)
			}
		})
	}
}

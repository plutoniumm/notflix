package jobs

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Whisper mis-hears proper nouns and no prompt instruction fixes it: asking the
// model to infer them from surrounding context measured flat (15.2% vs 15.5%
// content-word overlap) and recovered none of them — サイバリオ stayed "Cybario"
// where the character is 冴羽獠 / Ryo Saeba. A model cannot infer a name it has
// never been told. But the names are already on disk: episodes in a folder share
// characters, and a sibling episode usually ships an English sidecar. Lift them
// from there and hand them to the translator.

// Caps what reaches the prompt — the tail is single-appearance noise, and every
// entry costs tokens on every batch of every file.
const glossaryMax = 24

var tagRe = regexp.MustCompile(`<[^>]*>`)

// Capitalized words that are grammar or address rather than names. Everything
// else is filtered structurally by the mid-sentence rule below.
var notNames = map[string]string{
	"the": "", "and": "", "but": "", "for": "", "you": "", "your": "", "yes": "",
	"not": "", "oh": "", "ah": "", "hey": "", "well": "", "what": "", "who": "",
	"why": "", "how": "", "are": "", "our": "", "was": "", "were": "", "mister": "", "mr": "", "mrs": "", "miss": "", "sir": "",
	"madam": "", "boss": "", "god": "", "okay": "", "hello": "", "goodbye": "",
	"thanks": "", "sorry": "", "please": "", "damn": "", "wow": "",
}

// properNouns ranks capitalized tokens that appear at least once MID-sentence.
// Sentence-initial capitals are just grammar, so position is the whole signal —
// no dictionary needed. Requires two appearances: a recurring character is what
// a glossary is for, and a single hit is as likely to be noise.
func properNouns(text string, max int) []string {
	count := map[string]int{}
	mid := map[string]bool{}

	// Tags first: an <i> wrapper let the "i" count as a word, so the next token
	// looked mid-sentence and grammar ("Are") scored as a name. Then fold the
	// typographic apostrophe to ASCII, or "I\u2019m" survives as a name-shaped word.
	text = tagRe.ReplaceAllString(text, " ")
	text = strings.NewReplacer("\u2019", "'", "\u2018", "'").Replace(text)

	for _, line := range strings.Split(text, "\n") {
		// Single pass tracking the byte offset: an earlier version used
		// strings.Index(line, tok) to look at the character after the token,
		// which finds the token's FIRST occurrence, not the current one — so a
		// word repeated in one line was scored against the wrong position and
		// grammar ("Are", "Our") leaked in as names.
		startOfSentence := true
		for i := 0; i < len(line); {
			r := rune(line[i])
			if !isWordByte(line[i]) {
				if strings.ContainsRune(".!?", r) {
					startOfSentence = true
				}
				i++
				continue
			}
			j := i
			for j < len(line) && isWordByte(line[j]) {
				j++
			}
			word := strings.Trim(line[i:j], "'")
			if isNameShaped(word) {
				count[word]++
				if !startOfSentence {
					mid[word] = true
				}
			}
			startOfSentence = false
			i = j
		}
	}

	var out []string
	for w := range count {
		if mid[w] && count[w] >= 2 {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if count[out[i]] != count[out[j]] {
			return count[out[i]] > count[out[j]]
		}
		return out[i] < out[j] // stable for tests
	})
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// ASCII letters and the apostrophe only. Anything multibyte (CJK, accents) ends
// a word, and isNameShaped then rejects it for lacking an uppercase ASCII head.
func isWordByte(b byte) bool {
	return b == '\'' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isNameShaped(w string) bool {
	if len(w) < 3 {
		return false
	}
	r := []rune(w)
	if !unicode.IsUpper(r[0]) {
		return false
	}
	for _, c := range r[1:] {
		if unicode.IsUpper(c) { // ALLCAPS is shouting, not a name
			return false
		}
	}
	if strings.Contains(w, "'") { // I'm, Hunter's — contraction or possessive
		return false
	}
	if _, bad := notNames[strings.ToLower(w)]; bad {
		return false
	}
	return true
}

// ShowGlossary returns proper nouns from an English sidecar belonging to a
// DIFFERENT file in the same folder — the episode being transcribed has no
// usable sub by definition, or whisper would not be running on it. Picks the
// largest candidate: more dialogue, better name coverage.
func ShowGlossary(absPath string) []string {
	dir := filepath.Dir(absPath)
	self := strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath))

	// os.ReadDir, never filepath.Glob — `[` in a release name is a character
	// class and Glob returns no match with a nil error.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	best, bestSize := "", int64(0)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".vtt") && !strings.HasSuffix(name, ".srt") {
			continue
		}
		if strings.HasPrefix(name, self) { // our own file's sidecars
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() <= bestSize {
			continue
		}
		p := filepath.Join(dir, name)
		if !latinScript(p) {
			continue
		}
		best, bestSize = p, info.Size()
	}
	if best == "" {
		return nil
	}

	raw, err := os.ReadFile(best)
	if err != nil {
		return nil
	}
	var sb strings.Builder
	for _, c := range parseCues(string(raw)) {
		sb.WriteString(c.Text)
		sb.WriteString("\n")
	}
	return properNouns(sb.String(), glossaryMax)
}

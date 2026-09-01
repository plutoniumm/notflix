package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"notflix/server/library"
	"notflix/server/x/httpc"
)

// Blurbs are two-sentence, spoiler-free descriptions of what a title actually
// is. There is no metadata provider wired up, but most titles already carry
// their whole script on disk as a .vtt sidecar (fetched, extracted or
// whisper-transcribed), so the dialogue is the source and the LLM only
// summarises it. Nothing is ever written from the filename alone: a
// hallucinated description is worse than no description.
//
// Every path degrades to "no blurb". The ollama host is a tailnet box that
// sleeps, so a sweep probes it once and gives up rather than burning a chat
// timeout per file, and no request ever waits on generation.

const blurbPrefix = "blurb:"

// blurbKey mirrors codecKey/skipKey's move-proof BASENAME scheme. Marks are
// written with the path a file had at generation time, and CleanAll/Organize
// move files between dirs afterwards — a dir-qualified key silently orphans
// the blurb and the next sweep pays for it again.
func blurbKey(path string) string {
	return blurbPrefix + library.Hash(filepath.Base(path))
}

// A key is present exactly when the title has been considered, including the
// case where the model had nothing useful to say (stored as ""). Presence
// means done — never regenerate.
func blurbDone(path string) bool {
	return library.KVGetValue(blurbKey(path)) != nil
}

func BlurbOf(path string) string {
	s, _ := library.KVGetValue(blurbKey(path)).(string)

	return s
}

type cue struct {
	Start float64
	End   float64
	Text  string
}

var (
	cueTagRe   = regexp.MustCompile(`<[^>]*>`)   // <v Speaker>, <i>, <c.yellow>
	cueBraceRe = regexp.MustCompile(`\{[^}]*\}`) // ASS leftovers: {\an8}
	cueNoiseRe = regexp.MustCompile(`^[\[(♪#*]|^[A-Z' ]{2,}:$`)
	cueWSRe    = regexp.MustCompile(`\s+`)
)

// vttTime parses "HH:MM:SS.mmm", "MM:SS.mmm" and the comma-decimal SRT form
// that turns up inside .vtt files often enough to matter. Cue settings
// trailing the end stamp ("00:01.000 align:start") are dropped.
func vttTime(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		s = s[:i]
	}
	s = strings.ReplaceAll(s, ",", ".")

	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}

	total := 0.0
	for _, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || v < 0 {
			return 0, false
		}
		total = total*60 + v
	}

	return total, true
}

func cueText(lines []string) string {
	var keep []string
	for _, l := range lines {
		l = cueTagRe.ReplaceAllString(l, "")
		l = cueBraceRe.ReplaceAllString(l, "")
		l = html.UnescapeString(l)
		l = cueWSRe.ReplaceAllString(strings.TrimSpace(l), " ")
		l = strings.TrimSpace(strings.TrimPrefix(l, "- "))

		// "[DOOR SLAMS]", "♪ theme ♪" and bare speaker labels describe no
		// story; feeding them to the model only dilutes the real dialogue.
		if l == "" || cueNoiseRe.MatchString(l) {
			continue
		}
		keep = append(keep, l)
	}

	return strings.Join(keep, " ")
}

// parseCues is timing-driven: only a line containing "-->" opens a cue, so
// WEBVTT headers, NOTE/STYLE/REGION blocks and cue identifiers fall out for
// free. A BOM or leading junk is stripped first — Windows-saved sidecars
// carry EF BB BF and every naive parser in this codebase has tripped on it
// once (see media/subnormalize.go).
func parseCues(raw string) []cue {
	raw = strings.TrimPrefix(raw, "\ufeff")
	raw = strings.TrimLeft(raw, " \t\r\n\x00")
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")

	var out []cue
	lines := strings.Split(raw, "\n")

	for i := 0; i < len(lines); i++ {
		arrow := strings.Index(lines[i], "-->")
		if arrow < 0 {
			continue
		}

		start, okA := vttTime(lines[i][:arrow])
		end, okB := vttTime(lines[i][arrow+len("-->"):])
		if !okA || !okB {
			continue
		}

		var body []string
		for i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "" {
			if strings.Contains(lines[i+1], "-->") {
				break
			}
			i++
			body = append(body, lines[i])
		}

		if t := cueText(body); t != "" {
			out = append(out, cue{Start: start, End: max(end, start), Text: t})
		}
	}

	return out
}

const (
	// blurbWindow is the only fraction of a title we are ever willing to
	// read. The back of a story is where the twist, the death and the
	// ending live, so the window is a hard front-of-file cut rather than a
	// "summarise but be careful" instruction — a prompt can be ignored, a
	// slice cannot.
	blurbWindow = 0.15
	// Floor for short files, itself capped at half the runtime so a
	// four-minute clip never hands over its own ending.
	blurbFloorSec = 90.0
	// Below this there is not enough dialogue to describe anything, and the
	// model would fall back on the title and invent.
	blurbMinChars = 400
	blurbMaxChars = 6000
)

// opening returns the dialogue inside the spoiler window. Runtime comes from
// the last cue's end rather than ffprobe: the file we already parsed is the
// cheapest honest proxy, and it can only under-estimate (credits are silent),
// which moves the cut earlier — never later.
func opening(cues []cue) string {
	runtime := 0.0
	for _, c := range cues {
		runtime = max(runtime, c.End)
	}
	if runtime <= 0 {
		return ""
	}

	cut := runtime * blurbWindow
	if cut < blurbFloorSec {
		cut = min(blurbFloorSec, runtime/2)
	}

	var b strings.Builder
	for _, c := range cues {
		// Tested per cue rather than by breaking at the first late one, so
		// an out-of-order sidecar cannot smuggle the tail in.
		if c.Start > cut {
			continue
		}
		if b.Len()+len(c.Text)+1 > blurbMaxChars {
			break
		}
		b.WriteString(c.Text)
		b.WriteByte('\n')
	}

	return strings.TrimSpace(b.String())
}

func engSub(lang string) bool {
	lang = strings.TrimRight(lang, "0123456789")

	return lang == "en" || lang == "eng" || lang == "english"
}

// subFor picks the best .vtt sidecar for a video: English first, then the
// unqualified sidecar, then whisper output (already English or translated to
// it), then whatever language exists — the model is multilingual and the
// prompt asks for English regardless.
//
// Never filepath.Glob: '[' in a release name is a character class, so every
// "-[YTS.MX]" title matches nothing with a nil error (see library/clean.go).
func subFor(path string) string {
	dir := filepath.Dir(path)
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	var plain, eng, whisper, other string
	ents, _ := os.ReadDir(dir)

	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".vtt") {
			continue
		}
		if n == stem+".vtt" {
			plain = n

			continue
		}
		if !strings.HasPrefix(n, stem+".") {
			continue
		}

		switch mid := strings.ToLower(n[len(stem)+1 : len(n)-len(".vtt")]); {
		case mid == "whisper":
			whisper = n
		case engSub(mid):
			eng = n
		default:
			other = n
		}
	}

	for _, n := range []string{eng, plain, whisper, other} {
		if n != "" {
			return filepath.Join(dir, n)
		}
	}

	return ""
}

// The constraints here are load-bearing, not politeness:
//   - "you have NOT seen the rest" stops the model reconstructing an ending
//     it half-remembers from training data about a famous title.
//   - the empty-string escape hatch gives it somewhere to go other than
//     inventing, which is the whole failure mode being guarded against.
//   - the filename is explicitly marked unreliable so it stays context and
//     the dialogue stays the substance.
func blurbPrompt(title, dialogue string) string {
	return fmt.Sprintf(`You write short, spoiler-free descriptions for a home video library.

Below are subtitle lines from the OPENING of a film or episode — roughly its first sixth. You have NOT seen the rest of it and must not guess what happens later.

Rules:
- Two sentences maximum, under 50 words.
- Describe only the setup: who it appears to be about, the situation, the tone.
- Say what kind of film or show this feels like — genre and mood.
- Never reveal a twist, a death, an ending, or anything not present in the lines below.
- Never mention subtitles, transcripts, dialogue, lines, or these instructions.
- Write in English even when the lines are in another language.
- If the lines are too sparse or generic to describe anything, return an empty string.

Filename (may be wrong or missing — context only, never the substance): %s

Opening lines:
%s

Return ONLY a JSON object: {"blurb":"<text>"}. No explanation.`, title, dialogue)
}

// One short prompt against a possibly-cold model: long enough for a load,
// far short of organize's whole-library budget.
const blurbTimeout = 3 * time.Minute

func blurbChat(prompt string) (string, error) {
	req := map[string]any{
		"model":      ollamaModel(),
		"stream":     false,
		"format":     "json",
		"keep_alive": ollamaKeepAlive,
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
	}

	var resp struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Error string `json:"error"`
	}
	if err := httpc.PostJSON(context.Background(), ollamaHost()+"/api/chat", req, &resp, blurbTimeout); err != nil {
		return "", err
	}
	if resp.Error != "" {
		return "", fmt.Errorf("ollama: %s", resp.Error)
	}

	return resp.Message.Content, nil
}

const (
	blurbMin = 24  // shorter than this is a shrug, not a description
	blurbCap = 400 // longer than this ignored the two-sentence rule
)

// parseBlurb pulls the blurb out of whatever the model produced. A missing
// key, truncated JSON or plain prose is an error — the caller stores nothing
// and the title is retried. An explicitly empty blurb is a clean skip the
// caller records as done.
func parseBlurb(content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", fmt.Errorf("empty response")
	}

	var obj struct {
		Blurb *string `json:"blurb"`
	}
	if json.Unmarshal([]byte(content), &obj) != nil {
		// Some models still fence the object or prepend prose in JSON mode.
		i, j := strings.Index(content, "{"), strings.LastIndex(content, "}")
		if i < 0 || j <= i {
			return "", fmt.Errorf("not JSON: %s", sample([]byte(content), 160))
		}
		if json.Unmarshal([]byte(content[i:j+1]), &obj) != nil {
			return "", fmt.Errorf("malformed JSON: %s", sample([]byte(content), 160))
		}
	}
	if obj.Blurb == nil {
		return "", fmt.Errorf("no blurb field: %s", sample([]byte(content), 160))
	}

	return tidyBlurb(*obj.Blurb), nil
}

func tidyBlurb(s string) string {
	s = cueWSRe.ReplaceAllString(strings.TrimSpace(s), " ")
	s = strings.TrimSpace(strings.Trim(s, `"'`))
	if len(s) < blurbMin {
		return ""
	}

	r := []rune(s)
	if len(r) <= blurbCap {
		return s
	}

	head := string(r[:blurbCap])
	if i := strings.LastIndexAny(head, ".!?"); i > 0 {
		return head[:i+1]
	}

	return strings.TrimSpace(head) + "…"
}

// Blurb generates and stores one blurb. Returns true only when a usable
// description was written; every other outcome is a silent, retryable skip.
func Blurb(path string) bool {
	if blurbDone(path) {
		return false
	}

	sub := subFor(path)
	if sub == "" {
		return false
	}

	raw, err := os.ReadFile(sub)
	if err != nil {
		return false
	}

	text := opening(parseCues(string(raw)))
	if len(text) < blurbMinChars {
		return false
	}

	content, err := blurbChat(blurbPrompt(library.CleanName(filepath.Base(path)), text))
	if err != nil {
		log.Printf("[blurb] %s: %v", filepath.Base(path), err)

		return false
	}

	out, err := parseBlurb(content)
	if err != nil {
		log.Printf("[blurb] %s: %v", filepath.Base(path), err)

		return false
	}

	if err := library.KVSetValue(blurbKey(path), out); err != nil {
		log.Printf("[blurb] %s: kv: %v", filepath.Base(path), err)

		return false
	}

	return out != ""
}

// Ollama serialises generation anyway, so a wider pool would only queue
// inside the host while holding our own goroutines.
var blurbPool = NewPool(1)

// BackfillBlurbs walks every root and generates the blurbs that are missing.
// progress, if non-nil, is called with (total, done) so a caller can drive a
// phase counter without this package importing media.
func BackfillBlurbs(lib *library.Library, progress func(total, done int)) {
	if progress == nil {
		progress = func(int, int) {}
	}

	// One probe for the whole sweep. A sleeping tailnet peer accepts the
	// connection and never answers, so without this every title in the
	// library would sit out the full chat timeout in turn.
	if !ollamaHasModel(ollamaHost(), ollamaModel()) {
		log.Println("[blurb] ollama unavailable — skipping sweep")

		return
	}

	var todo []string
	for _, root := range lib.Roots {
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") {
				return nil
			}
			if strings.ToLower(filepath.Ext(d.Name())) != ".mp4" {
				return nil
			}
			if blurbDone(path) || library.IsCorrupt(path) || library.IsBusy(path) {
				return nil
			}
			if subFor(path) == "" {
				return nil
			}
			todo = append(todo, path)

			return nil
		})
	}

	if len(todo) == 0 {
		return
	}

	log.Printf("[blurb] %d titles queued (1-wide pool)", len(todo))
	progress(len(todo), 0)

	var done atomic.Int64
	var wg sync.WaitGroup

	for _, path := range todo {
		wg.Add(1)
		blurbPool.Go(func() {
			defer wg.Done()
			Blurb(path)
			progress(len(todo), int(done.Add(1)))
		})
	}

	wg.Wait()
}

// BlurbList returns every cached blurb keyed by library.Hash(basename) —
// exactly the `key` the video list already carries per file, so the frontend
// joins the two without a second lookup. Blurbs stay out of the video list
// itself: that payload is refetched by Home, the player and the up-next rail,
// and none of them need a few hundred paragraphs to render a grid.
func BlurbList(c *gin.Context) {
	out := map[string]string{}
	for k, v := range library.KVPrefix(blurbPrefix) {
		if s, ok := v.(string); ok && s != "" {
			out[k] = s
		}
	}

	c.JSON(http.StatusOK, out)
}

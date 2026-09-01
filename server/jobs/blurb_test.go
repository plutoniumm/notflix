package jobs

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"notflix/server/library"
)

// synthVTT builds a 60-cue, 60-minute transcript whose lines are numbered, so
// a test can name exactly which minute a piece of text came from.
func synthVTT() string {
	var b strings.Builder
	b.WriteString("WEBVTT\n\nNOTE this header must not become a cue\n\n")
	for i := range 60 {
		fmt.Fprintf(&b, "cue-%d\n00:%02d:00.000 --> 00:%02d:30.000\nline at minute %d\n\n", i, i, i, i)
	}

	return b.String()
}

func TestParseCues(t *testing.T) {
	cues := parseCues(synthVTT())
	if len(cues) != 60 {
		t.Fatalf("want 60 cues, got %d", len(cues))
	}

	if cues[0].Start != 0 || cues[0].End != 30 {
		t.Errorf("cue 0 timestamps = %v/%v, want 0/30", cues[0].Start, cues[0].End)
	}
	if cues[59].Start != 59*60 {
		t.Errorf("cue 59 start = %v, want %v", cues[59].Start, 59*60)
	}
	if cues[7].Text != "line at minute 7" {
		t.Errorf("cue 7 text = %q", cues[7].Text)
	}

	for _, c := range cues {
		if strings.Contains(c.Text, "NOTE") || strings.Contains(c.Text, "WEBVTT") {
			t.Fatalf("header leaked into a cue: %q", c.Text)
		}
	}
}

func TestParseCuesTimeForms(t *testing.T) {
	raw := "WEBVTT\n\n" +
		"01:02.500 --> 01:04.000\nshort form\n\n" +
		"00:00:10,000 --> 00:00:12,000\ncomma decimals\n\n" +
		"00:00:20.000 --> 00:00:22.000 align:start position:10%\ncue settings\n\n" +
		"garbage --> alsogarbage\nignored\n\n"

	cues := parseCues(raw)
	if len(cues) != 3 {
		t.Fatalf("want 3 cues, got %d: %+v", len(cues), cues)
	}
	if cues[0].Start != 62.5 {
		t.Errorf("MM:SS.mmm start = %v, want 62.5", cues[0].Start)
	}
	if cues[1].Start != 10 || cues[1].End != 12 {
		t.Errorf("comma form = %v/%v, want 10/12", cues[1].Start, cues[1].End)
	}
	if cues[2].End != 22 {
		t.Errorf("cue-settings end = %v, want 22", cues[2].End)
	}
}

func TestParseCuesCleansText(t *testing.T) {
	raw := "WEBVTT\n\n" +
		"00:00:01.000 --> 00:00:03.000\n<v Ana>Hello &amp; welcome</v>\n\n" +
		"00:00:04.000 --> 00:00:06.000\n{\\an8}<i>She's late.</i>\n\n" +
		"00:00:07.000 --> 00:00:09.000\n[DOOR SLAMS]\n\n" +
		"00:00:10.000 --> 00:00:12.000\n♪ theme music ♪\n\n" +
		"00:00:13.000 --> 00:00:15.000\nANA:\n\n"

	cues := parseCues(raw)
	if len(cues) != 2 {
		t.Fatalf("want 2 cues after noise removal, got %d: %+v", len(cues), cues)
	}
	if cues[0].Text != "Hello & welcome" {
		t.Errorf("cue 0 = %q", cues[0].Text)
	}
	if cues[1].Text != "She's late." {
		t.Errorf("cue 1 = %q", cues[1].Text)
	}
}

// The whole point of the feature: what we hand the model must come from the
// front of the file and must never contain the tail.
func TestOpeningStaysInSpoilerWindow(t *testing.T) {
	cues := parseCues(synthVTT())
	text := opening(cues)
	if text == "" {
		t.Fatal("empty opening")
	}

	// Runtime is 59.5 min → the window closes at ~8.9 min, so minutes 0..8
	// are in and everything from minute 9 on is out.
	for i := range 9 {
		if !strings.Contains(text, fmt.Sprintf("line at minute %d\n", i)) &&
			!strings.HasSuffix(text, fmt.Sprintf("line at minute %d", i)) {
			t.Errorf("minute %d missing from opening", i)
		}
	}
	for i := 9; i < 60; i++ {
		if strings.Contains(text, fmt.Sprintf("minute %d", i)) {
			t.Fatalf("minute %d leaked past the spoiler window", i)
		}
	}

	last := cues[len(cues)-1].Text
	if strings.Contains(text, last) {
		t.Fatalf("final cue %q reached the prompt", last)
	}
}

func TestOpeningShortRuntimeHalvesTheWindow(t *testing.T) {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for i := range 20 {
		fmt.Fprintf(&b, "00:00:%02d.000 --> 00:00:%02d.000\nsecond %d\n\n", i*3, i*3+2, i*3)
	}

	// 59s of runtime: 15% is under the floor, so the floor applies — but
	// capped at half the runtime, never the ending.
	text := opening(parseCues(b.String()))
	if !strings.Contains(text, "second 0") {
		t.Error("opening dropped the first line")
	}
	if strings.Contains(text, "second 57") {
		t.Fatal("a 59s clip handed over its own ending")
	}
}

func TestOpeningIgnoresCueOrder(t *testing.T) {
	raw := "WEBVTT\n\n" +
		"00:50:00.000 --> 00:50:02.000\nTHE ENDING\n\n" +
		"00:00:01.000 --> 00:00:03.000\nthe beginning\n\n"

	if text := opening(parseCues(raw)); strings.Contains(text, "THE ENDING") {
		t.Fatalf("out-of-order tail cue smuggled in: %q", text)
	}
}

func TestParseCuesDegradesOnJunk(t *testing.T) {
	cases := map[string]string{
		"empty":         "",
		"binary":        "\x00\x01\x02\xff\xfe",
		"header only":   "WEBVTT\n\n",
		"no timestamps": "just some prose\nwith no cues at all\n",
		"half a cue":    "WEBVTT\n\n00:00:01.000 -->",
		"no colons":     "WEBVTT\n\n1 --> 2\ntext\n",
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if got := opening(parseCues(raw)); got != "" {
				t.Errorf("want empty opening, got %q", got)
			}
		})
	}
}

func TestParseCuesStripsBOM(t *testing.T) {
	// The real bug from subnormalize.go: a Windows-saved sidecar starts
	// EF BB BF, which every naive parser reads as part of the first token.
	cues := parseCues("\ufeffWEBVTT\n\n00:00:01.000 --> 00:00:03.000\nhello\n\n")
	if len(cues) != 1 || cues[0].Text != "hello" {
		t.Fatalf("BOM broke parsing: %+v", cues)
	}

	if got := parseCues("\ufeff"); len(got) != 0 {
		t.Errorf("bare BOM should yield no cues, got %+v", got)
	}
}

func TestParseBlurb(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"clean", `{"blurb":"A courier takes one last job. Wry, neon-lit crime caper."}`,
			"A courier takes one last job. Wry, neon-lit crime caper.", false},
		{"fenced", "```json\n{\"blurb\":\"Two sisters reopen their mother's diner. Warm, slow-moving drama.\"}\n```",
			"Two sisters reopen their mother's diner. Warm, slow-moving drama.", false},
		{"prose prefix", `Sure! {"blurb":"A detective arrives in a town that will not talk. Bleak procedural."}`,
			"A detective arrives in a town that will not talk. Bleak procedural.", false},
		{"whitespace collapse", "{\"blurb\":\"  Spread   over\\n  lines and padded out here.  \"}",
			"Spread over lines and padded out here.", false},
		{"model declines", `{"blurb":""}`, "", false},
		{"too short to be a blurb", `{"blurb":"N/A"}`, "", false},
		{"truncated", `{"blurb":"A courier takes one la`, "", true},
		{"empty", "", "", true},
		{"whitespace only", "   \n\t ", "", true},
		{"not json", "I'm sorry, I can't summarise that.", "", true},
		{"wrong shape", `{"summary":"A courier takes one last job."}`, "", true},
		{"empty object", `{}`, "", true},
		{"array", `["a blurb that came back in the wrong container"]`, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseBlurb(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTidyBlurbTrimsRunaway(t *testing.T) {
	long := strings.Repeat("A sentence that keeps going. ", 40)
	got := tidyBlurb(long)

	if len([]rune(got)) > blurbCap {
		t.Fatalf("not capped: %d runes", len([]rune(got)))
	}
	if !strings.HasSuffix(got, ".") {
		t.Errorf("want a sentence boundary, got %q", got[max(0, len(got)-40):])
	}

	// A cap that splits a rune would corrupt the stored string.
	multi := strings.Repeat("寿", 600)
	if !strings.HasSuffix(tidyBlurb(multi), "…") || strings.Contains(tidyBlurb(multi), "�") {
		t.Errorf("multibyte truncation is not rune-safe")
	}
}

func TestSubForPrefersEnglish(t *testing.T) {
	dir := t.TempDir()
	// A bracketed release name: filepath.Glob would silently match nothing.
	stem := "The Movie [2020] 1080p-[YTS.MX]"
	vid := filepath.Join(dir, stem+".mp4")

	write := func(name string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("WEBVTT\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if got := subFor(vid); got != "" {
		t.Fatalf("no sidecars yet, got %q", got)
	}

	write(stem + ".fr.vtt")
	if got := filepath.Base(subFor(vid)); got != stem+".fr.vtt" {
		t.Errorf("foreign-only fallback = %q", got)
	}

	write(stem + ".whisper.vtt")
	if got := filepath.Base(subFor(vid)); got != stem+".whisper.vtt" {
		t.Errorf("whisper should beat a foreign sidecar, got %q", got)
	}

	write(stem + ".vtt")
	if got := filepath.Base(subFor(vid)); got != stem+".vtt" {
		t.Errorf("plain sidecar should beat whisper, got %q", got)
	}

	write(stem + ".eng2.vtt")
	if got := filepath.Base(subFor(vid)); got != stem+".eng2.vtt" {
		t.Errorf("english should win outright, got %q", got)
	}
}

func TestSubForIgnoresNeighbours(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Show S01E02.en.vtt"), []byte("WEBVTT\n"), 0644)

	if got := subFor(filepath.Join(dir, "Show S01E01.mp4")); got != "" {
		t.Fatalf("picked up a sibling episode's subtitle: %q", got)
	}
}

// The store lives at ./cache/kv.json relative to the process, so a test that
// touches it has to own its own cwd.
func kvSandbox(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cache"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
}

func TestBlurbKeySurvivesAMove(t *testing.T) {
	kvSandbox(t)

	a := "/Volumes/Aomine/The Movie.mp4"
	b := "/Volumes/Aomine/2000s/The Movie.mp4"
	if blurbKey(a) != blurbKey(b) {
		t.Fatal("key is dir-qualified; Organize would orphan it")
	}

	if blurbDone(a) {
		t.Fatal("absent key reported done")
	}
	if err := library.KVSetValue(blurbKey(a), ""); err != nil {
		t.Fatal(err)
	}

	// An empty blurb is still an answer: the title must never be retried.
	if !blurbDone(b) {
		t.Error("empty value not treated as done")
	}
	if got := BlurbOf(b); got != "" {
		t.Errorf("BlurbOf = %q", got)
	}
}

func TestBlurbListOmitsEmpties(t *testing.T) {
	kvSandbox(t)
	gin.SetMode(gin.TestMode)

	library.KVSetValue("blurb:0000001", "A courier takes one last job.")
	library.KVSetValue("blurb:0000002", "")
	library.KVSetValue("codec:0000001", "av1")

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("GET", "/api/blurbs", nil)
	BlurbList(c)

	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("%v: %s", err, rec.Body.String())
	}
	if len(got) != 1 || got["0000001"] != "A courier takes one last job." {
		t.Fatalf("payload = %v", got)
	}
}

func TestBackfillBlurbsSkipsWhenOllamaIsAway(t *testing.T) {
	kvSandbox(t)
	t.Setenv("OLLAMA_HOST", "http://127.0.0.1:1")

	// No panic, no hang, no writes — the sweep just gives up.
	BackfillBlurbs(&library.Library{Roots: []string{t.TempDir()}}, nil)
}

func TestBlurbPromptCarriesConstraints(t *testing.T) {
	p := blurbPrompt("The Movie", "some dialogue")
	for _, must := range []string{"spoiler-free", "OPENING", "empty string", `{"blurb"`, "English", "some dialogue"} {
		if !strings.Contains(p, must) {
			t.Errorf("prompt lost %q", must)
		}
	}
}

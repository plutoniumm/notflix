package jobs

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"notflix/server/library"
)

func TestParseOrganize(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    int
	}{
		{"clean", `{"moves":[{"i":0,"folder":"Harry Potter"},{"i":1,"folder":"Harry Potter"}]}`, 2},
		{"prose wrapped", "Here is the plan:\n{\"moves\":[{\"i\":3,\"folder\":\"Documentaries\"}]}\nDone.", 1},
		{"empty moves", `{"moves":[]}`, 0},
	}
	for _, c := range cases {
		got, err := parseOrganize(c.content)
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
			continue
		}
		if len(got) != c.want {
			t.Errorf("%s: got %d moves, want %d", c.name, len(got), c.want)
		}
	}

	if _, err := parseOrganize("no json at all"); err == nil {
		t.Error("garbage content should error")
	}
}

func TestFolderOK(t *testing.T) {
	for _, bad := range []string{"", ".hidden", "a/b", `a\b`, "..", strings.Repeat("x", folderMaxLen+1)} {
		if folderOK(bad) {
			t.Errorf("folderOK(%q) should be false", bad)
		}
	}
	// The long one is a REAL folder in this library and is why the bound moved
	// off 80: at 80 the grammar truncated it mid-string and every held-out file
	// from it was assigned to a folder that does not exist — 12 of 12 benchmark
	// errors. Shortening this bound again reintroduces exactly that.
	for _, good := range []string{
		"Harry Potter", "2000s", "Hindi 1990s", "Documentaries",
		"Sapne vs Everyone S01 720p 10bit DS4K AMZN WEBRip x265 HEVC Hindi DDP 5.1 ESub - Immortal",
	} {
		if !folderOK(good) {
			t.Errorf("folderOK(%q) should be true", good)
		}
	}
}

func TestValidMoves(t *testing.T) {
	items := []OrgItem{
		{File: "hp1.mp4", Root: "Ravan"},
		{File: "hp2.mp4", Root: "Ravan"},
		{File: "lone.mp4", Root: "Ravan"},
		{File: "doc.mp4", Root: "Oni"},
		{File: "outlaws.mp4", Root: "Oni"},
	}
	existing := map[string]map[string]int{
		"Oni": {"Documentaries": 3, "The Roundup (2022)": 2},
	}

	assigns := []orgAssign{
		{I: 0, Folder: "Harry Potter"},
		{I: 1, Folder: " Harry Potter "},         // whitespace trimmed, same group
		{I: 2, Folder: "Solo"},                   // singleton folder → dropped
		{I: 3, Folder: "Documentaries"},          // 1 new + 3 existing → kept
		{I: 4, Folder: "The Roundup (2022) (2)"}, // listing's count copied back → existing folder
		{I: 3, Folder: "Dup"},                    // duplicate index → ignored
		{I: 99, Folder: "OutOfRange"},
		{I: 0, Folder: "bad/name"},
	}

	moves := validMoves(items, assigns, existing)
	if len(moves) != 4 {
		t.Fatalf("got %d moves, want 4: %+v", len(moves), moves)
	}
	byFile := map[string]string{}
	for _, m := range moves {
		byFile[m.File] = m.Folder
	}
	if byFile["hp1.mp4"] != "Harry Potter" || byFile["hp2.mp4"] != "Harry Potter" {
		t.Errorf("franchise group broken: %v", byFile)
	}
	if byFile["doc.mp4"] != "Documentaries" {
		t.Errorf("existing-folder top-up dropped: %v", byFile)
	}
	if byFile["outlaws.mp4"] != "The Roundup (2022)" {
		t.Errorf("count suffix not snapped to the existing folder: %v", byFile)
	}
	if _, ok := byFile["lone.mp4"]; ok {
		t.Error("singleton folder should have been dropped")
	}
}

// The model-index indirection only covers /plan; /apply takes paths straight
// from the body, so a move absent from the stored plan must be refused.
func TestOrganizeApplyRejectsUnplanned(t *testing.T) {
	gin.SetMode(gin.TestMode)

	root := t.TempDir()
	base := filepath.Base(root)
	for _, name := range []string{"planned.mp4", "elsewhere.mp4"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	orgMu.Lock()
	orgState = orgStatus{Stage: "done", Moves: []OrgMove{{File: "planned.mp4", Root: base, Folder: "2000s"}}}
	orgMu.Unlock()
	t.Cleanup(func() {
		orgMu.Lock()
		orgState = orgStatus{Stage: "idle", Moves: []OrgMove{}}
		orgMu.Unlock()
	})

	body := fmt.Sprintf(`{"moves":[
		{"file":"planned.mp4","root":%q,"folder":"2000s"},
		{"file":"elsewhere.mp4","root":%q,"folder":"2000s"}]}`, base, base)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/organize/apply", strings.NewReader(body))
	OrganizeApply(c, &library.Library{Roots: []string{root}})

	var out struct {
		Moved  int      `json:"moved"`
		Errors []string `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Moved != 1 {
		t.Fatalf("moved = %d, want only the planned one (%v)", out.Moved, out.Errors)
	}
	if _, err := os.Stat(filepath.Join(root, "elsewhere.mp4")); err != nil {
		t.Fatalf("unplanned move was executed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "2000s", "planned.mp4")); err != nil {
		t.Fatalf("planned move did not run: %v", err)
	}
}

// filepath.Glob reads the "[" in -[YTS.MX] releases as a character class:
// the old sidecar lookup matched nothing, with a nil error, stranding .srts.
func TestSidecarsBracketedName(t *testing.T) {
	dir := t.TempDir()
	video := "Movie [2020] 1080p BluRay x264-[YTS.MX].mp4"
	base := strings.TrimSuffix(video, ".mp4")
	for _, name := range []string{
		video,
		base + ".en.srt",
		base + ".vtt",
		base + ".nfo",
		"Unrelated.srt",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if m, err := filepath.Glob(filepath.Join(dir, base+".*")); err == nil && len(m) > 0 {
		t.Fatalf("premise gone: Glob now matches bracketed names (%v)", m)
	}

	got := sidecars(dir, video)
	sort.Strings(got)
	want := []string{base + ".en.srt", base + ".vtt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sidecars = %v, want %v", got, want)
	}
}

func TestApplyMoveTakesSidecars(t *testing.T) {
	root := t.TempDir()
	video := "Movie [2020] 1080p BluRay x264-[YTS.MX].mp4"
	sub := strings.TrimSuffix(video, ".mp4") + ".en.srt"
	for _, name := range []string{video, sub} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if err := applyMove(root, OrgMove{File: video, Root: filepath.Base(root), Folder: "2020s"}); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{video, sub} {
		if _, err := os.Stat(filepath.Join(root, "2020s", name)); err != nil {
			t.Errorf("%s did not move: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			t.Errorf("%s left behind at the root", name)
		}
	}
}

func TestOrganizePrompt(t *testing.T) {
	items := []OrgItem{
		{File: "Harry.Potter.2001.1080p.mp4", Root: "Ravan"},
		{File: "The.Outlaws.2017.1080p.mp4", Root: "Ravan"},
	}
	web := map[int][]string{1: {"The Outlaws (2017 film): the first installment of The Roundup series"}, 0: {"not in this batch"}}
	p := organizePrompt(items, map[string]map[string]int{"Ravan": {"Marvel": 5}}, []int{1}, web)
	for _, want := range []string{`"i":0`, "Harry Potter", `"year":2001`, "Ravan/Marvel (5)", `"moves"`,
		"Decide only these files: [1]", "first installment of The Roundup"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	if strings.Contains(p, "1080p") {
		t.Error("prompt should carry cleaned names, not raw rip tags")
	}
	if strings.Contains(p, "not in this batch") {
		t.Error("web results for files outside the batch leaked into the prompt")
	}
}

// The year is parsed off the RAW filename because CleanName destroys it:
// bracketRe eats "(2021)" and the keyword list eats a bare "2025", which left
// every decade bucket a guess from the title alone.
func TestFileYear(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
	}{
		{"Dune (2021) [1080p] [WEBRip] [5.1] [YTS.MX].mp4", 2021},
		{"Companion 2025 1080p WEBRip x264.mkv", 2025},
		{"Oppenheimer.2023.2160p.WEB-DL.mkv", 2023},
		{"Blade Runner 2049 (2017) 1080p.mkv", 2017}, // title digits must not win
		{"Ran 1985 Remastered DTS SARTRE.mp4", 1985},
		{"The Orville S02E11 Lasting Impressions LESS.mp4", 0},
	} {
		if got := fileYear(c.in); got != c.want {
			t.Errorf("fileYear(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// A schema is only worth having if it makes the bad answer impossible, so pin
// the two bounds that do the work: the index cannot leave the file list, and
// the folder bound matches the validator's.
func TestOrganizeSchemaBounds(t *testing.T) {
	sch := organizeSchema(7)
	props := sch["properties"].(map[string]any)
	item := props["moves"].(map[string]any)["items"].(map[string]any)
	ip := item["properties"].(map[string]any)
	if got := ip["i"].(map[string]any)["maximum"]; got != 6 {
		t.Errorf("i maximum = %v, want 6 for 7 files", got)
	}
	if got := ip["folder"].(map[string]any)["maxLength"]; got != folderMaxLen {
		t.Errorf("folder maxLength = %v, want folderMaxLen (%d)", got, folderMaxLen)
	}
	if item["additionalProperties"] != false {
		t.Error("a move must not accept extra keys")
	}
}

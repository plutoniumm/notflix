package jobs

import (
	"strings"
	"testing"
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

func TestMergeAssigns(t *testing.T) {
	runs := [][]orgAssign{
		{{I: 0, Folder: "A"}, {I: 1, Folder: "A"}, {I: 5, Folder: "X"}},
		{{I: 0, Folder: "B"}, {I: 5, Folder: "Y"}},
		{{I: 0, Folder: "B"}, {I: 2, Folder: "C"}, {I: 2, Folder: "Z"}, {I: 3, Folder: " "}},
	}
	got := map[int]string{}
	for _, a := range mergeAssigns(runs) {
		got[a.I] = a.Folder
	}

	if got[0] != "B" {
		t.Errorf("i=0: majority should win, got %q", got[0])
	}
	if got[5] != "X" {
		t.Errorf("i=5: tie should resolve to earlier run, got %q", got[5])
	}
	if got[1] != "A" {
		t.Errorf("i=1: single-run assignment should pass through, got %q", got[1])
	}
	if got[2] != "C" {
		t.Errorf("i=2: duplicate index within a run should vote once, got %q", got[2])
	}
	if _, ok := got[3]; ok {
		t.Error("blank folder should not vote")
	}
	if len(mergeAssigns(nil)) != 0 {
		t.Error("no runs should merge to nothing")
	}
}

func TestRunsAgree(t *testing.T) {
	a := []orgAssign{{0, "A"}, {1, "B"}, {2, "C"}, {3, "D"}, {4, "E"}}

	same := []orgAssign{{4, "E"}, {3, "D"}, {2, "C"}, {1, "B"}, {0, "A"}}
	if got := runsAgree(a, same); got != 1 {
		t.Errorf("identical runs: got %v, want 1", got)
	}

	oneOff := []orgAssign{{0, "A"}, {1, "B"}, {2, "C"}, {3, "D"}, {4, "X"}}
	if got := runsAgree(a, oneOff); got < organizeConverge {
		t.Errorf("4/5 agreement should converge: got %v", got)
	}

	// 3 matches over a union of 6 indices — well under the bar.
	drift := []orgAssign{{0, "A"}, {1, "B"}, {2, "C"}, {5, "F"}}
	if got := runsAgree(a, drift); got >= organizeConverge {
		t.Errorf("3/6 agreement should not converge: got %v", got)
	}

	disjoint := []orgAssign{{9, "Z"}}
	if got := runsAgree(a, disjoint); got != 0 {
		t.Errorf("disjoint runs: got %v, want 0", got)
	}

	if got := runsAgree(nil, nil); got != 0 {
		t.Errorf("empty runs: got %v, want 0", got)
	}

	// Blank folders and duplicate indices dedup like mergeAssigns votes.
	messy := []orgAssign{{0, " A "}, {0, "Q"}, {1, ""}, {2, "C"}}
	clean := []orgAssign{{0, "A"}, {2, "C"}}
	if got := runsAgree(messy, clean); got != 1 {
		t.Errorf("trim+dedup: got %v, want 1", got)
	}
}

func TestFolderOK(t *testing.T) {
	for _, bad := range []string{"", ".hidden", "a/b", `a\b`, "..", strings.Repeat("x", 81)} {
		if folderOK(bad) {
			t.Errorf("folderOK(%q) should be false", bad)
		}
	}
	for _, good := range []string{"Harry Potter", "2000s", "Hindi 1990s", "Documentaries"} {
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
	}
	existing := map[string]map[string]int{
		"Oni": {"Documentaries": 3},
	}

	assigns := []orgAssign{
		{I: 0, Folder: "Harry Potter"},
		{I: 1, Folder: " Harry Potter "}, // whitespace trimmed, same group
		{I: 2, Folder: "Solo"},           // singleton folder → dropped
		{I: 3, Folder: "Documentaries"},  // 1 new + 3 existing → kept
		{I: 3, Folder: "Dup"},            // duplicate index → ignored
		{I: 99, Folder: "OutOfRange"},
		{I: 0, Folder: "bad/name"},
	}

	moves := validMoves(items, assigns, existing)
	if len(moves) != 3 {
		t.Fatalf("got %d moves, want 3: %+v", len(moves), moves)
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
	if _, ok := byFile["lone.mp4"]; ok {
		t.Error("singleton folder should have been dropped")
	}
}

func TestOrganizePrompt(t *testing.T) {
	items := []OrgItem{{File: "Harry.Potter.2001.1080p.mp4", Root: "Ravan"}}
	p := organizePrompt(items, map[string]map[string]int{"Ravan": {"Marvel": 5}})
	for _, want := range []string{`"i":0`, "Harry Potter", "Marvel", `"moves"`} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	if strings.Contains(p, "1080p") {
		t.Error("prompt should carry cleaned names, not raw rip tags")
	}
}

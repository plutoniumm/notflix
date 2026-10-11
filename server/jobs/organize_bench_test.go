package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"notflix/server/library"
)

// Ground truth for free: files that ALREADY live in a folder are presented as
// loose and the model is scored on putting them back. No labelling, and it is
// the exact case the prompt cares most about ("reuse an existing folder").
//
// NOTFLIX_BENCH=1 go test ./server/jobs/ -run TestOrganizeBench -v -timeout 40m
func heldOut(t *testing.T, roots []string, perFolder int) ([]OrgItem, []string, map[string]map[string]int) {
	var items []OrgItem
	var truth []string
	folders := map[string]map[string]int{}

	for _, root := range roots {
		base := filepath.Base(root)
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("read %s: %v", root, err)
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			vids := library.GetVids(filepath.Join(root, e.Name()))
			if len(vids) < 3 {
				continue // need occupants left behind, or the folder stops existing
			}
			sort.Strings(vids)
			if folders[base] == nil {
				folders[base] = map[string]int{}
			}
			take := perFolder
			if take > len(vids)-2 {
				take = len(vids) - 2
			}
			for _, v := range vids[:take] {
				items = append(items, OrgItem{File: v, Root: base, root: root})
				truth = append(truth, e.Name())
			}
			folders[base][e.Name()] = len(vids) - take
		}
	}
	return items, truth, folders
}

func scoreRun(t *testing.T, label, prompt string, nFiles int, schema map[string]any, truth []string, items []OrgItem) {
	start := time.Now()
	raw, err := saturnChat(context.Background(), "", prompt, jsonSchema("organize_moves", schema), 10*time.Minute)
	el := time.Since(start)
	if err != nil {
		t.Errorf("  %-22s ERROR %v", label, err)
		return
	}

	var obj struct {
		Reasoning string      `json:"reasoning"`
		Moves     []orgAssign `json:"moves"`
	}
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		t.Errorf("  %-22s UNPARSEABLE (%v) raw=%s", label, err, sample([]byte(raw), 160))
		return
	}

	seen := map[int]bool{}
	right, wrong := 0, 0
	var misses []string
	for _, m := range obj.Moves {
		if m.I < 0 || m.I >= nFiles || seen[m.I] {
			continue
		}
		seen[m.I] = true
		if strings.TrimSpace(m.Folder) == truth[m.I] {
			right++
		} else {
			wrong++
			if len(misses) < 4 {
				misses = append(misses, fmt.Sprintf("%q→%q(want %q)",
					library.CleanName(items[m.I].File), m.Folder, truth[m.I]))
			}
		}
	}
	t.Logf("  %-22s covered %2d/%2d  correct %2d (%.0f%% of covered, %.0f%% of all)  wrong %d  %4.0fs  think=%dch",
		label, len(seen), nFiles, right,
		pct(right, len(seen)), pct(right, nFiles), wrong, el.Seconds(), len(obj.Reasoning))
	for _, m := range misses {
		t.Logf("       miss: %s", m)
	}
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b) * 100
}

func TestOrganizeBench(t *testing.T) {
	if os.Getenv("NOTFLIX_BENCH") == "" {
		t.Skip("set NOTFLIX_BENCH=1 to measure against the live library + saturn")
	}
	roots := []string{"/Volumes/Aomine", filepath.Join(os.Getenv("HOME"), "Videos")}
	items, truth, folders := heldOut(t, roots, 3)
	if len(items) < 8 {
		t.Skipf("only %d held-out files; need a library with populated folders", len(items))
	}
	t.Logf("held out %d files across %d roots, %d folders", len(items), len(roots), len(organizeFolders(folders)))

	rich := organizePrompt(items, folders)

	// Same files, the OLD context: cleaned name only, bare folder names.
	type lean struct {
		I    int    `json:"i"`
		Name string `json:"name"`
		Root string `json:"root"`
	}
	ls := make([]lean, len(items))
	for i, it := range items {
		ls[i] = lean{i, library.CleanName(it.File), it.Root}
	}
	lf := []string{}
	for _, m := range folders {
		for name := range m {
			lf = append(lf, name)
		}
	}
	sort.Strings(lf)
	lj, _ := json.Marshal(ls)
	fj, _ := json.Marshal(lf)
	leanPrompt := strings.Replace(strings.Replace(rich,
		"Existing folders: "+mustJSON(organizeFolders(folders)), "Existing folders: "+string(fj), 1),
		"Files: "+mustJSON(organizeFiles(items)), "Files: "+string(lj), 1)

	noThink := map[string]any{
		"type": "object", "additionalProperties": false,
		"required":   []string{"moves"},
		"properties": map[string]any{"moves": map[string]any{"type": "array", "items": moveItem(len(items))}},
	}
	withThink := map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"reasoning", "moves"},
		"properties": map[string]any{
			"reasoning": map[string]any{"type": "string"},
			"moves":     map[string]any{"type": "array", "items": moveItem(len(items))},
		},
	}

	scoreRun(t, "lean ctx, no think", leanPrompt, len(items), noThink, truth, items)
	scoreRun(t, "rich ctx, no think", rich, len(items), noThink, truth, items)
	scoreRun(t, "rich ctx, thinking", rich, len(items), withThink, truth, items)
}

func moveItem(n int) map[string]any {
	return map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"i", "folder"},
		"properties": map[string]any{
			"i":      map[string]any{"type": "integer", "minimum": 0, "maximum": n - 1},
			"folder": map[string]any{"type": "string", "minLength": 1, "maxLength": folderMaxLen},
		},
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

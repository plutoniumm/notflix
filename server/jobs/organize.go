package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"notflix/server/library"
	"notflix/server/x/httpc"
)

// The model only ever sees an indexed file list + existing folder names and
// answers {index → folder} — filenames never round-trip, so a hallucination
// can't corrupt a move. Nothing touches disk until the user approves a subset.
// A single completion is flaky, so runs are merged by per-file vote.

type OrgItem struct {
	File string `json:"file"` // basename, lives at the root level
	Root string `json:"root"` // filepath.Base of the owning root
	root string // absolute root path, server-side only
}

type OrgMove struct {
	File   string `json:"file"`
	Root   string `json:"root"`
	Folder string `json:"folder"`
}

// Loose videos at each root's top level only; anything already in a folder is
// a curated grouping and is left alone.
func organizeItems(lib *library.Library) []OrgItem {
	var out []OrgItem
	for _, root := range lib.Roots {
		for _, name := range library.GetVids(root) {
			if library.IsBusy(filepath.Join(root, name)) {
				continue
			}
			out = append(out, OrgItem{File: name, Root: filepath.Base(root), root: root})
		}
	}
	return out
}

// root-base → folder → video count; the count feeds validMoves' ≥2 rule.
func existingFolders(lib *library.Library) map[string]map[string]int {
	out := map[string]map[string]int{}
	for _, root := range lib.Roots {
		base := filepath.Base(root)
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if n := len(library.GetVids(filepath.Join(root, e.Name()))); n > 0 {
				if out[base] == nil {
					out[base] = map[string]int{}
				}
				out[base][e.Name()] = n
			}
		}
	}
	return out
}

func organizePrompt(items []OrgItem, folders map[string]map[string]int) string {
	type f struct {
		I    int    `json:"i"`
		Name string `json:"name"`
		Root string `json:"root"`
	}
	files := make([]f, len(items))
	for i, it := range items {
		files[i] = f{I: i, Name: library.CleanName(it.File), Root: it.Root}
	}
	filesJSON, _ := json.Marshal(files)

	var names []string
	for _, m := range folders {
		for name := range m {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	foldersJSON, _ := json.Marshal(names)

	return fmt.Sprintf(`You organize a home movie library. Below is a list of loose video files, plus the folders that already exist.

Assign files to folders:
- Films of one franchise or series belong in one folder named after it (e.g. "Harry Potter", "Mission Impossible").
- Documentaries and docuseries go in "Documentaries".
- Reuse an existing folder when one fits.
- Remaining movies: group by decade (e.g. "1980s", "2000s"). If a decade group would mix languages, split it by language (e.g. "Hindi 2000s"). If a group grows past ~15 files, split it into parts (e.g. "2000s A-M", "2000s N-Z").
- A file may only move to a folder on its own root.
- Never invent a folder that would hold a single file; leave such files out entirely.
- If a file is ambiguous, leave it out.

Existing folders: %s
Files: %s

Return ONLY a JSON object: {"moves":[{"i":<file index>,"folder":"<folder name>"}]}. No explanation.`,
		string(foldersJSON), string(filesJSON))
}

// Generous (whole-library prompt, cold model) but finite: an asleep ollama
// host accepts the connection and never answers.
const organizeTimeout = 10 * time.Minute

func organizeChat(prompt string) (string, error) {
	req := map[string]any{
		"model":      ollamaModel(),
		"stream":     false,
		"format":     "json",
		"keep_alive": ollamaKeepAlive,
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
	}

	var chatResp struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Error string `json:"error"`
	}
	if err := httpc.PostJSON(context.Background(), ollamaHost()+"/api/chat", req, &chatResp, organizeTimeout); err != nil {
		return "", err
	}
	if chatResp.Error != "" {
		return "", fmt.Errorf("ollama: %s", chatResp.Error)
	}

	return chatResp.Message.Content, nil
}

type orgAssign struct {
	I      int    `json:"i"`
	Folder string `json:"folder"`
}

func parseOrganize(content string) ([]orgAssign, error) {
	content = strings.TrimSpace(content)
	if i := strings.Index(content, "{"); i > 0 {
		content = content[i:]
	}
	if i := strings.LastIndex(content, "}"); i >= 0 {
		content = content[:i+1]
	}

	var obj struct {
		Moves []orgAssign `json:"moves"`
	}
	if err := json.Unmarshal([]byte(content), &obj); err != nil {
		return nil, fmt.Errorf("organize parse: %w", err)
	}
	return obj.Moves, nil
}

// Per-index vote, ties toward earlier runs. An index only one run assigned
// still passes through — flaky runs leave gaps the others fill.
func mergeAssigns(runs [][]orgAssign) []orgAssign {
	votes := map[int]map[string]int{}
	best := map[int]string{}
	for _, run := range runs {
		seen := map[int]bool{}
		for _, a := range run {
			folder := strings.TrimSpace(a.Folder)
			if folder == "" || seen[a.I] {
				continue
			}
			seen[a.I] = true
			if votes[a.I] == nil {
				votes[a.I] = map[string]int{}
			}
			votes[a.I][folder]++
			if b, ok := best[a.I]; !ok || votes[a.I][folder] > votes[a.I][b] {
				best[a.I] = folder
			}
		}
	}

	out := make([]orgAssign, 0, len(best))
	for i, folder := range best {
		out = append(out, orgAssign{I: i, Folder: folder})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].I < out[b].I })
	return out
}

func folderOK(name string) bool {
	if name == "" || len(name) > 80 {
		return false
	}
	if strings.HasPrefix(name, ".") || strings.Contains(name, "..") {
		return false
	}
	return !strings.ContainsAny(name, `/\`)
}

// A folder that would hold a single video is dropped — CleanAll flattens it
// straight back to the root. existing counts occupants already in the folder.
func validMoves(items []OrgItem, assigns []orgAssign, existing map[string]map[string]int) []OrgMove {
	type group struct{ root, folder string }
	byGroup := map[group][]OrgMove{}
	seen := map[int]bool{}

	for _, a := range assigns {
		folder := strings.TrimSpace(a.Folder)
		if a.I < 0 || a.I >= len(items) || seen[a.I] || !folderOK(folder) {
			continue
		}
		seen[a.I] = true
		it := items[a.I]
		g := group{it.Root, folder}
		byGroup[g] = append(byGroup[g], OrgMove{File: it.File, Root: it.Root, Folder: folder})
	}

	var out []OrgMove
	for g, moves := range byGroup {
		if len(moves)+existing[g.root][g.folder] < 2 {
			continue
		}
		out = append(out, moves...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Folder != out[j].Folder {
			return out[i].Folder < out[j].Folder
		}
		return out[i].File < out[j].File
	})
	return out
}

const (
	organizeRuns     = 3
	organizeConverge = 0.8
)

// First non-blank assignment per index wins — the dedup mergeAssigns votes on.
func assignMap(run []orgAssign) map[int]string {
	m := map[int]string{}
	for _, a := range run {
		folder := strings.TrimSpace(a.Folder)
		if folder == "" {
			continue
		}
		if _, ok := m[a.I]; !ok {
			m[a.I] = folder
		}
	}
	return m
}

// Identical assignments over the union of indices either run covered; at
// ≥organizeConverge the third run can't change the vote, so it's skipped.
func runsAgree(a, b []orgAssign) float64 {
	am, bm := assignMap(a), assignMap(b)
	union := len(am)
	agree := 0
	for i, folder := range bm {
		if af, ok := am[i]; ok {
			if af == folder {
				agree++
			}
		} else {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(agree) / float64(union)
}

type orgStatus struct {
	Stage string    `json:"stage"` // idle | loading model | classifying | merging | done | error
	Run   int       `json:"run"`
	Runs  int       `json:"runs"`
	Note  string    `json:"note"`
	Moves []OrgMove `json:"moves"`
	Total int       `json:"total"`
	Error string    `json:"error"`
}

// A finished plan stays until the next plan or apply resets it, so a reloaded
// page can still fetch it.
var (
	orgMu      sync.Mutex
	orgState   = orgStatus{Stage: "idle", Moves: []OrgMove{}}
	orgWarming atomic.Bool
)

func orgSet(mut func(*orgStatus)) {
	orgMu.Lock()
	mut(&orgState)
	orgMu.Unlock()
}

func orgSnap() orgStatus {
	orgMu.Lock()
	defer orgMu.Unlock()
	return orgState
}

func orgRunning(stage string) bool {
	return stage == "loading model" || stage == "classifying" || stage == "merging"
}

func runOrganize(lib *library.Library) {
	fail := func(err error) {
		log.Printf("[organize] %v", err)
		orgSet(func(s *orgStatus) { s.Stage = "error"; s.Error = err.Error() })
	}

	items := organizeItems(lib)
	if len(items) == 0 {
		orgSet(func(s *orgStatus) { s.Stage = "done" })
		return
	}

	host, model := ollamaHost(), ollamaModel()
	if !ollamaHasModel(host, model) {
		fail(fmt.Errorf("ollama model %q unavailable at %s", model, host))
		return
	}
	if !ollamaLoaded(host, model) {
		orgSet(func(s *orgStatus) { s.Stage = "loading model" })
		if err := ollamaWarm(host, model); err != nil {
			fail(fmt.Errorf("model load: %w", err))
			return
		}
	}

	folders := existingFolders(lib)
	prompt := organizePrompt(items, folders)

	var runs [][]orgAssign
	var lastErr error
	for r := 1; r <= organizeRuns; r++ {
		orgSet(func(s *orgStatus) { s.Stage = "classifying"; s.Run = r })
		content, err := organizeChat(prompt)
		var assigns []orgAssign
		if err == nil {
			assigns, err = parseOrganize(content)
			if err != nil {
				log.Printf("[organize] run %d parse: %v — content=%s", r, err, sample([]byte(content), 240))
			}
		}
		if err != nil {
			lastErr = err
			log.Printf("[organize] run %d: %v", r, err)
			continue
		}
		if len(assigns) == 0 {
			log.Printf("[organize] run %d: blank", r)
			continue
		}
		log.Printf("[organize] run %d: %d assignments", r, len(assigns))
		runs = append(runs, assigns)
		if len(runs) == 2 && runsAgree(runs[0], runs[1]) >= organizeConverge {
			log.Printf("[organize] runs agree ≥%.0f%% — skipping run %d", organizeConverge*100, organizeRuns)
			orgSet(func(s *orgStatus) { s.Note = "first two runs agreed — skipped run 3" })
			break
		}
	}
	if len(runs) == 0 {
		if lastErr == nil {
			lastErr = fmt.Errorf("all runs returned blank plans")
		}
		fail(lastErr)
		return
	}

	orgSet(func(s *orgStatus) { s.Stage = "merging" })
	moves := validMoves(items, mergeAssigns(runs), folders)
	if moves == nil {
		moves = []OrgMove{}
	}
	orgSet(func(s *orgStatus) { s.Stage = "done"; s.Moves = moves; s.Total = len(items) })
}

// Returns immediately; an already-running job just gets its state back.
func OrganizePlan(c *gin.Context, lib *library.Library) {
	orgMu.Lock()
	if !orgRunning(orgState.Stage) {
		orgState = orgStatus{Stage: "classifying", Runs: organizeRuns, Moves: []OrgMove{}}
		go runOrganize(lib)
	}
	s := orgState
	orgMu.Unlock()
	c.JSON(http.StatusOK, s)
}

func OrganizeStatus(c *gin.Context) {
	c.JSON(http.StatusOK, orgSnap())
}

// Fired when the Organize section mounts, so the cold-load minute is usually
// paid before the user asks for a plan.
func OrganizeWarm(c *gin.Context) {
	if orgWarming.CompareAndSwap(false, true) {
		go func() {
			defer orgWarming.Store(false)
			if err := ollamaWarm(ollamaHost(), ollamaModel()); err != nil {
				log.Printf("[organize] warm: %v", err)
			}
		}()
	}
	c.JSON(http.StatusOK, gin.H{"warming": true})
}

// Same-filesystem rename only — plan moves never cross roots.
func applyMove(root string, m OrgMove) error {
	src := filepath.Join(root, m.File)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("%s: not found", m.File)
	}

	dir := filepath.Join(root, m.Folder)
	if !library.EnsureDir(dir) {
		return fmt.Errorf("cannot create %s", m.Folder)
	}

	dst := filepath.Join(dir, m.File)
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("%s: already exists in %s", m.File, m.Folder)
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}

	for _, sc := range sidecars(root, m.File) {
		os.Rename(filepath.Join(root, sc), filepath.Join(dir, sc))
	}

	return nil
}

// filepath.Glob is unusable here: a "[" in a release name (-[YTS.MX], ~7% of
// the library) reads as a character class — no match, nil error. Scan the dir.
func sidecars(dir, video string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	prefix := strings.TrimSuffix(video, filepath.Ext(video)) + "."

	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, prefix) {
			continue
		}
		if ext := strings.ToLower(filepath.Ext(name)); ext == ".vtt" || ext == ".srt" {
			out = append(out, name)
		}
	}

	return out
}

func OrganizeApply(c *gin.Context, lib *library.Library) {
	var body struct {
		Moves []OrgMove `json:"moves"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || len(body.Moves) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expected {moves:[{file,root,folder}]}"})
		return
	}

	roots := map[string]string{}
	for _, r := range lib.Roots {
		roots[filepath.Base(r)] = r
	}

	// The client posts back whatever it likes, so every applied move must be one
	// this server planned — else /apply is an arbitrary file-mover.
	planned := map[OrgMove]bool{}
	for _, m := range orgSnap().Moves {
		planned[m] = true
	}

	moved := 0
	var errs []string
	for _, m := range body.Moves {
		root, ok := roots[m.Root]
		if !ok || !planned[m] || !folderOK(m.Folder) || m.File == "" || strings.ContainsAny(m.File, `/\`) {
			errs = append(errs, fmt.Sprintf("%s: invalid move", m.File))
			continue
		}
		if err := applyMove(root, m); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		log.Printf("[organize] %s → %s/%s", m.File, m.Root, m.Folder)
		moved++
	}

	// The plan is consumed — a reloaded page shouldn't re-offer applied moves.
	orgMu.Lock()
	if !orgRunning(orgState.Stage) {
		orgState = orgStatus{Stage: "idle", Moves: []OrgMove{}}
	}
	orgMu.Unlock()

	c.JSON(http.StatusOK, gin.H{"moved": moved, "errors": errs})
}

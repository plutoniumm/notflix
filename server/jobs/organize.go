package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"notflix/server/library"
)

// The model only ever sees an indexed file list + existing folder names and
// answers {index → folder} — filenames never round-trip, so a hallucination
// can't corrupt a move. Nothing touches disk until the user approves a subset.
//
// Only series and franchises are grouped; a standalone film stays loose. The
// model reasons ~1.5k chars PER FILE whatever the prompt says (a rules-free
// prompt measured the same 63k chars as the full one), so one call over the
// library took 70–120s and the gateway cuts at 120s. Small batches fired
// together keep every call short: the plan takes about as long as one batch.
// Each batch is sent twice at once and the answers unioned: a reply's failure
// mode is OMISSION (a batch of four obvious episodes came back {"moves": []}
// one run and fully placed the next), never a wrong index.

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

// yearRe runs on the RAW filename, never on CleanName's output: bracketRe eats
// "(2021)" and the keyword list eats a bare "2025", so by the time a name is
// clean the year is usually gone — while the prompt asks for decade grouping.
// "Dune (2021) [1080p]" cleaned to "Dune" and "Companion 2025" to "Companion",
// so every decade bucket was a guess from the title alone.
var yearRe = regexp.MustCompile(`\b(19[3-9]\d|20[0-4]\d)\b`)

func fileYear(raw string) int {
	m := yearRe.FindAllString(raw, -1)
	if len(m) == 0 {
		return 0
	}
	// Last match: "Blade Runner 2049 (2017)" must read 2017, not the title.
	y, err := strconv.Atoi(m[len(m)-1])
	if err != nil {
		return 0
	}
	return y
}

type orgFile struct {
	I    int    `json:"i"`
	Name string `json:"name"`
	Year int    `json:"year,omitempty"`
	Root string `json:"root"`
}

func organizeFiles(items []OrgItem) []orgFile {
	out := make([]orgFile, len(items))
	for i, it := range items {
		out[i] = orgFile{I: i, Name: library.CleanName(it.File), Year: fileYear(it.File), Root: it.Root}
	}
	return out
}

// Folder names carry their occupant count: "Documentaries (12)" is an
// established grouping, "Misc (2)" is barely one, and the model reuses the
// former far more readily when it can tell them apart.
func organizeFolders(folders map[string]map[string]int) []string {
	var out []string
	for root, m := range folders {
		for name, n := range m {
			out = append(out, fmt.Sprintf("%s/%s (%d)", root, name, n))
		}
	}
	sort.Strings(out)
	return out
}

func organizePrompt(items []OrgItem, folders map[string]map[string]int, idx []int, web map[int][]string) string {
	filesJSON, _ := json.Marshal(organizeFiles(items))
	foldersJSON, _ := json.Marshal(organizeFolders(folders))
	idxJSON, _ := json.Marshal(idx)
	hits := map[int][]string{}
	for _, i := range idx {
		if len(web[i]) > 0 {
			hits[i] = web[i]
		}
	}
	webJSON, _ := json.Marshal(hits)

	return fmt.Sprintf(`You tidy a home video library. Below are its loose video files and the folders that already exist ("root/folder (file count)").

Group ONLY series and franchises:
- an episode or part of a show, web series or video series goes in that series' folder
- a film that is one installment of a film series or franchise goes in the franchise's folder (name the folder after the franchise)
- reuse an existing folder when it already holds that series or franchise: answer its exact name, without the root
- a file may only move to a folder on its own root
- a standalone film, a one-off video, or anything you are unsure of: leave it out
Web results are given for some of the files you decide; trust them on which franchise or series a title belongs to.

Answer with JSON only, no prose: {"moves": [{"i": <file index>, "folder": "<folder name>"}]}

Existing folders: %s
All files (context): %s

Decide only these files: %s
Web results: %s`, foldersJSON, filesJSON, idxJSON, webJSON)
}

// Finite: an asleep host accepts the connection and never answers. The
// gateway itself gives up at 120s.
const organizeTimeout = 3 * time.Minute

const (
	organizeBatch    = 4
	organizeReplicas = 2
)

// organizeSchema makes a malformed plan unrepresentable rather than merely
// discouraged: `i` is bounded to the actual file count, so the out-of-range
// index that validMoves used to drop silently can't be emitted, and the
// envelope can't carry prose. `folder` stays a free string on purpose — an enum
// of existing folders was measured correct for reuse but it also forbids the
// NEW franchise folders the prompt exists to create. A hosted model ignores the
// schema entirely, so the prompt spells the shape out too.
func organizeSchema(nFiles int) map[string]any {
	move := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"i", "folder"},
		"properties": map[string]any{
			"i":      map[string]any{"type": "integer", "minimum": 0, "maximum": nFiles - 1},
			"folder": map[string]any{"type": "string", "minLength": 1, "maxLength": folderMaxLen},
		},
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"moves"},
		"properties":           map[string]any{"moves": map[string]any{"type": "array", "items": move}},
	}
}

func organizeChat(model, prompt string, nFiles int) (string, error) {
	return saturnChat(context.Background(), model, "", prompt,
		jsonSchema("organize_moves", organizeSchema(nFiles)), organizeTimeout)
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

// Release-name folders in this library genuinely run past 80 chars ("Sapne vs
// Everyone S01 720p 10bit DS4K AMZN WEBRip x265 HEVC Hindi DDP 5.1 ESub -
// Immortal" is 89). At 80 the grammar TRUNCATED the name mid-string and the
// model's answer then matched no folder — 12 of 12 benchmark errors were this,
// not a judgement failure. The bound is shared with organizeSchema so the
// validator and the grammar can never disagree.
const folderMaxLen = 120

var countSfxRe = regexp.MustCompile(` \(\d+\)$`)

func folderOK(name string) bool {
	if name == "" || len(name) > folderMaxLen {
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
		// The listing shows "root/folder (count)" and a model copies the count
		// back, which named a NEW folder "The Roundup … (2)" beside the real one.
		if trimmed := countSfxRe.ReplaceAllString(folder, ""); existing[it.Root][folder] == 0 && existing[it.Root][trimmed] > 0 {
			folder = trimmed
		}
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

type orgStatus struct {
	Stage string    `json:"stage"` // idle | searching | classifying | done | error
	Run   int       `json:"run"`   // batches finished
	Runs  int       `json:"runs"`  // batches in flight
	Note  string    `json:"note"`
	Moves []OrgMove `json:"moves"`
	Total int       `json:"total"`
	Error string    `json:"error"`
}

// A finished plan stays until the next plan or apply resets it, so a reloaded
// page can still fetch it.
var (
	orgMu    sync.Mutex
	orgState = orgStatus{Stage: "idle", Moves: []OrgMove{}}
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
	return stage == "searching" || stage == "classifying"
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

	// One cheap probe up front: a down gateway would otherwise cost a full
	// timeout per run.
	model, err := organizeModel(context.Background(), 20*time.Second)
	if err != nil {
		fail(fmt.Errorf("saturn unavailable at %s: %w", SaturnHost(), err))
		return
	}
	log.Printf("[organize] model %s", model)

	folders := existingFolders(lib)
	web := organizeSearch(items)

	var batches [][]int
	for i := 0; i < len(items); i += organizeBatch {
		var idx []int
		for j := i; j < min(i+organizeBatch, len(items)); j++ {
			idx = append(idx, j)
		}
		for range organizeReplicas {
			batches = append(batches, idx)
		}
	}
	orgSet(func(s *orgStatus) { s.Stage = "classifying"; s.Runs = len(batches) })

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		assigns []orgAssign
		lastErr error
		failed  int
	)
	for _, idx := range batches {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := organizeDecide(model, items, folders, idx, web)
			mu.Lock()
			if err != nil {
				lastErr = err
				failed++
				log.Printf("[organize] batch %v: %v", idx, err)
			}
			assigns = append(assigns, got...)
			mu.Unlock()
			orgSet(func(s *orgStatus) { s.Run++ })
		}()
	}
	wg.Wait()
	if failed == len(batches) {
		fail(lastErr)
		return
	}

	moves := validMoves(items, assigns, folders)
	if moves == nil {
		moves = []OrgMove{}
	}
	log.Printf("[organize] %d moves from %d files, %d/%d batches failed", len(moves), len(items), failed, len(batches))
	orgSet(func(s *orgStatus) {
		s.Stage = "done"
		s.Moves = moves
		s.Total = len(items)
		if failed > 0 {
			s.Note = fmt.Sprintf("%d of %d batches failed — some files weren't considered", failed, len(batches))
		}
	})
}

// organizeDecide keeps only answers about its own batch: every call sees the
// whole list as context and a model will happily answer for a neighbour.
func organizeDecide(model string, items []OrgItem, folders map[string]map[string]int, idx []int, web map[int][]string) ([]orgAssign, error) {
	content, err := organizeChat(model, organizePrompt(items, folders, idx, web), len(items))
	if err != nil {
		return nil, err
	}
	got, err := parseOrganize(content)
	if err != nil {
		return nil, fmt.Errorf("%w — content=%s", err, sample([]byte(content), 240))
	}
	mine := map[int]bool{}
	for _, i := range idx {
		mine[i] = true
	}
	var out []orgAssign
	for _, a := range got {
		if mine[a.I] {
			out = append(out, a)
		}
	}
	return out, nil
}

// organizeSearch looks up films only (a year in the name). Episodes and web
// series read off their names; which franchise a film belongs to is a fact
// the model has to be told — The Outlaws (2017) is the first Roundup film and
// nothing in its name says so, and the model missed it without the lookup.
// 3 wide: saturn's search provider rate-limits a burst (8 wide lost 24 of 34
// lookups; 3 wide lost none).
func organizeSearch(items []OrgItem) map[int][]string {
	orgSet(func(s *orgStatus) { s.Stage = "searching" })
	web := map[int][]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	pool := NewPool(3)
	for i, it := range items {
		y := fileYear(it.File)
		if y == 0 {
			continue
		}
		q := library.CleanName(it.File)
		if !strings.Contains(q, strconv.Itoa(y)) {
			q += " " + strconv.Itoa(y)
		}
		wg.Add(1)
		pool.Go(func() {
			defer wg.Done()
			hits, err := saturnSearch(context.Background(), q+" film", 3)
			if err != nil {
				log.Printf("[organize] search %q: %v", q, err)
				return
			}
			mu.Lock()
			web[i] = hits
			mu.Unlock()
		})
	}
	wg.Wait()
	return web
}

// Returns immediately; an already-running job just gets its state back.
func OrganizePlan(c *gin.Context, lib *library.Library) {
	orgMu.Lock()
	if !orgRunning(orgState.Stage) {
		orgState = orgStatus{Stage: "searching", Moves: []OrgMove{}}
		go runOrganize(lib)
	}
	s := orgState
	orgMu.Unlock()
	c.JSON(http.StatusOK, s)
}

func OrganizeStatus(c *gin.Context) {
	c.JSON(http.StatusOK, orgSnap())
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

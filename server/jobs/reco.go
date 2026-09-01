package jobs

import (
	"context"
	"log"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"notflix/server/library"
)

// Local recommendations: embed every title with an ollama embedding model,
// take taste from what the user actually finished, and rank the rest of the
// library by cosine similarity. Everything is local and everything is
// optional — a sleeping ollama host means an empty row, never an error.

// A title counts as watched only when a real fraction of it played. The
// progress record carries `t` and (on newer saves) `dur`; 60% of a known
// duration is past the point where somebody sampled a film and bailed. Only
// 83 of this library's 100 live records have a duration at all, so a record
// without one falls back to the absolute floor alone — three minutes, which
// still rejects the thirty-second "what is this" open that is the thing the
// threshold exists to reject.
const (
	watchFrac   = 0.6
	watchMinSec = 180.0
)

const (
	// Anchors are the finished titles taste is measured against. Ten recent
	// ones is plenty; more just dilutes with things watched a year ago.
	maxAnchors = 10

	// Centered cosine over this library runs about -0.35 to 0.75 with a
	// median near -0.05, so 0.15 is roughly the top fifth — a real neighbour
	// rather than two titles that happen to both be English.
	minSim = 0.15

	recoMax = 12

	// Cap per anchor so one finished show cannot supply the whole row: with
	// eight Harry Potter films in the history, an uncapped row was six
	// Harry-Potter-adjacent picks.
	perAnchorMax = 2

	recoTTL = 30 * time.Minute

	// An empty row usually means the host was asleep when the last build ran,
	// so retry sooner than a good row is refreshed — but not so fast that a
	// sleeping peer gets hammered by every page load.
	recoRetryTTL = 3 * time.Minute
)

var (
	// Release tags carry no meaning but plenty of token overlap: two unrelated
	// films that share "BONE" or "NeoNoir" scored as neighbours before this
	// list existed.
	recoJunk = junkRes(`nflx amzn hevc hdtv hdrip bluray webdl webrip web bd brrip br rip dvdrip dvd
		yify yts rarbg evo xvid etrg psa anoxmous sujaidr deejayahmed rdlinks immortal rawr kin now
		sartre bone neonoir mkvcage rapta elite bokutox scorp ettv ntb ion10 galaxyrg tgx qxr cm8
		fgt sparks amiable drones publichd mzabi playweb edith flux ntg cakes minx scorp
		x264 x265 h264 h265 avc av1 atmos truehd 6ch
		720p 480p 1080p 2160p 4k 10bit 8bit sd hd hq uhd ds4k hdscr fs
		dual audio multi subs sub subbed dubbed esub esubs vostfr eng ita hin jpn kor
		complete repack proper extended internal uncut remastered reenc split scenes rq
		season episode ep part`)

	recoBracket = regexp.MustCompile(`\[[^\]]*\]|\([^)]*\)`)
	recoSeason  = regexp.MustCompile(`(?i)\bs\d{1,2}(\s*e\d{1,3})?\b`)
	recoUnit    = regexp.MustCompile(`(?i)\b\d+(mb|gb|ch|bit|bits|kbps|k)\b`)
	// Audio tags glue a channel count straight onto the codec — AAC5, DDP2,
	// DD5 — so a plain word boundary never sees them, and two unrelated films
	// that share "AAC5" scored as neighbours.
	recoCodec  = regexp.MustCompile(`(?i)\b(aac|ac3|eac3|ddp|dd|dts|opus|flac)\d?\b`)
	recoDecade = regexp.MustCompile(`(?i)\b(19|20)\d0s\b`)
	recoNum    = regexp.MustCompile(`\b\d{1,4}\b`)
	recoSpace  = regexp.MustCompile(`\s+`)
	recoWord   = regexp.MustCompile(`[^a-z0-9]+`)
	recoExt    = regexp.MustCompile(`(?i)\.(mp4|mkv|mov)$`)
)

func junkRes(s string) []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, w := range strings.Fields(s) {
		out = append(out, regexp.MustCompile(`(?i)\b`+regexp.QuoteMeta(w)+`\b`))
	}

	return out
}

// cleanTitle strips a release name down to the part a language model can
// reason about. library.CleanName is close but keeps season markers, decade
// folders and group tags, all of which are pure token noise here.
func cleanTitle(s string) string {
	s = recoExt.ReplaceAllString(s, "")
	s = recoBracket.ReplaceAllString(s, " ")
	s = strings.NewReplacer("-", " ", ".", " ", "_", " ", "+", " ").Replace(s)
	s = recoSeason.ReplaceAllString(s, " ")
	s = recoUnit.ReplaceAllString(s, " ")
	s = recoDecade.ReplaceAllString(s, " ")
	s = recoCodec.ReplaceAllString(s, " ")

	for _, re := range recoJunk {
		s = re.ReplaceAllString(s, " ")
	}

	s = recoNum.ReplaceAllString(s, " ")

	return strings.TrimSpace(recoSpace.ReplaceAllString(s, " "))
}

// titleOf builds the text that gets embedded. The folder usually holds the
// show or franchise name and the file the episode, so an episode of a show
// carries its show name into the vector; when one already contains the other
// the shorter is dropped rather than repeated.
func titleOf(rel string) string {
	dir, base := path.Split(rel)
	ct, cb := cleanTitle(strings.Trim(dir, "/")), cleanTitle(base)

	if ct == "" {
		return cb
	}

	if cb == "" {
		return ct
	}

	lt, lb := strings.ToLower(ct), strings.ToLower(cb)
	if strings.Contains(lb, lt) {
		return cb
	}

	if strings.Contains(lt, lb) {
		return ct
	}

	return ct + " " + cb
}

func normKey(s string) string {
	return strings.TrimSpace(recoWord.ReplaceAllString(strings.ToLower(s), " "))
}

// groupAll assigns every path a cluster key, so diversification can ask "is
// this the same show as one I already picked". Two rules, because neither
// alone survives this library: a folder that names its own contents ("Bleach",
// "Scorpion Season 3") is one cluster, but a folder that is a bucket of
// unrelated films ("2000s", "Documentaries") is not, and clustering it whole
// would collapse a decade into a single recommendation.
//
// The second pass merges keys that share their first two tokens, which is what
// reunites "The Sopranos" across six season folders, "Breaking Bad" with
// "Breaking Bad DeeJayAhmed", and the eight Harry Potter films the history
// still remembers after the files themselves moved. Two shared tokens and not
// one: "Transformers Age of Extinction" and "Transformers The Last Knight" are
// genuinely different films, and so are "The Sopranos" and "The Sheep
// Detectives".
func groupAll(cat, hist []string) map[string]string {
	raw := make(map[string]string, len(cat)+len(hist))

	byDir := map[string][]string{}
	for _, rel := range cat {
		byDir[path.Dir(rel)] = append(byDir[path.Dir(rel)], rel)
	}

	for dir, rels := range byDir {
		dk := ""
		if dir != "." && dir != "" {
			dk = normKey(cleanTitle(path.Base(dir)))
		}

		hits := 0
		if dk != "" {
			for _, rel := range rels {
				if strings.Contains(normKey(titleOf(rel)), dk) {
					hits++
				}
			}
		}

		named := dk != "" && len(rels) >= 3 && hits*10 >= len(rels)*6
		for _, rel := range rels {
			if named {
				raw[rel] = dk

				continue
			}

			raw[rel] = normKey(titleOf(rel))
		}
	}

	for _, p := range hist {
		if _, ok := raw[p]; !ok {
			raw[p] = normKey(titleOf(p))
		}
	}

	return mergeGroups(raw)
}

// mergeGroups unions cluster keys that share a two-token prefix. Sorting puts every
// such pair adjacent, so one linear pass over the sorted keys finds them all.
func mergeGroups(raw map[string]string) map[string]string {
	seen := map[string]bool{}
	for _, k := range raw {
		seen[k] = true
	}

	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parent := make(map[string]string, len(keys))
	for _, k := range keys {
		parent[k] = k
	}

	var find func(string) string
	find = func(k string) string {
		if parent[k] != k {
			parent[k] = find(parent[k])
		}

		return parent[k]
	}

	for i := 1; i < len(keys); i++ {
		a, b := strings.Fields(keys[i-1]), strings.Fields(keys[i])

		n := 0
		for n < len(a) && n < len(b) && a[n] == b[n] {
			n++
		}

		if n < 2 {
			continue
		}

		ra, rb := find(keys[i-1]), find(keys[i])
		if ra == rb {
			continue
		}

		// The shorter key is the canonical one: "breaking bad" over
		// "breaking bad deejayahmed".
		if len(rb) < len(ra) {
			ra, rb = rb, ra
		}
		parent[rb] = ra
	}

	out := make(map[string]string, len(raw))
	for p, k := range raw {
		out[p] = find(k)
	}

	return out
}

// anchor is one finished title that taste is measured against.
type anchor struct {
	Param string
	Title string
	At    float64
}

// pickAnchors returns the most recent genuinely-watched titles, at most one
// per cluster. Without the per-cluster cap a single binge owns the whole
// anchor set — a Harry Potter weekend put eight of ten anchors on one
// franchise and the recommendations read like a Harry Potter row.
func pickAnchors(watched map[string]library.Watch, group map[string]string, n int) []anchor {
	var all []anchor
	for p, w := range watched {
		if !isWatched(w) {
			continue
		}

		all = append(all, anchor{Param: p, Title: titleOf(p), At: w.At})
	}

	sort.Slice(all, func(i, j int) bool { return all[i].At > all[j].At })

	used := map[string]bool{}
	out := make([]anchor, 0, n)

	for _, a := range all {
		if a.Title == "" {
			continue
		}

		g := group[a.Param]
		if g == "" {
			g = normKey(a.Title)
		}

		if used[g] {
			continue
		}
		used[g] = true

		out = append(out, a)
		if len(out) == n {
			break
		}
	}

	return out
}

func isWatched(w library.Watch) bool {
	if w.T < watchMinSec {
		return false
	}

	if w.Dur > 0 && w.T/w.Dur < watchFrac {
		return false
	}

	return true
}

type Rec struct {
	Dir     string  `json:"dir"`
	Name    string  `json:"name"`
	Key     string  `json:"key"`
	Title   string  `json:"title"`
	Score   float64 `json:"score"`
	Because string  `json:"because"`
	Reason  string  `json:"reason,omitempty"`
}

type candidate struct {
	rel   string
	title string
	group string
}

// pickCands drops everything the history has already touched. Anything with a
// progress record is out — that covers Continue Watching, which is exactly the
// row this one must not duplicate — and so is anything in the same cluster,
// because "you are three episodes into this" is a resume prompt, not a
// discovery. History keys are dir-qualified and files move, so a title is
// matched by basename as well as by full path.
func pickCands(cat []string, watched map[string]library.Watch, group map[string]string) []candidate {
	base := make(map[string]string, len(cat))
	for _, rel := range cat {
		base[path.Base(rel)] = rel
	}

	seen := map[string]bool{}
	seenGroup := map[string]bool{}

	for p := range watched {
		seen[p] = true

		if rel, ok := base[path.Base(p)]; ok {
			seen[rel] = true
			seenGroup[group[rel]] = true

			continue
		}

		seenGroup[group[p]] = true
	}

	var out []candidate
	for _, rel := range cat {
		if seen[rel] || seenGroup[group[rel]] {
			continue
		}

		out = append(out, candidate{rel: rel, title: titleOf(rel), group: group[rel]})
	}

	return out
}

// rankRecs scores every candidate against its best anchor and returns a
// diversified top slice. Split out from the I/O so the maths and the
// selection are testable on synthetic vectors.
func rankRecs(cands []candidate, anchors []anchor, vecs map[string]vec) []Rec {
	type scored struct {
		c      candidate
		anchor anchor
		sim    float64
	}

	var all []scored
	for _, c := range cands {
		cv := vecs[c.title]
		if len(cv) == 0 {
			continue
		}

		best := scored{c: c, sim: minSim}
		for _, a := range anchors {
			av := vecs[a.Title]
			if len(av) == 0 {
				continue
			}

			if s := cosine(cv, av); s > best.sim {
				best.sim, best.anchor = s, a
			}
		}

		if best.anchor.Param != "" {
			all = append(all, best)
		}
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].sim != all[j].sim {
			return all[i].sim > all[j].sim
		}

		return all[i].c.rel < all[j].c.rel
	})

	usedGroup := map[string]bool{}
	perAnchor := map[string]int{}
	out := make([]Rec, 0, recoMax)

	for _, s := range all {
		if usedGroup[s.c.group] || perAnchor[s.anchor.Param] >= perAnchorMax {
			continue
		}
		usedGroup[s.c.group] = true
		perAnchor[s.anchor.Param]++

		dir, name := path.Split(s.c.rel)
		dir = strings.TrimSuffix(dir, "/")
		if dir == "" {
			dir = "."
		}

		out = append(out, Rec{
			Dir:     dir,
			Name:    name,
			Key:     library.Hash(name),
			Title:   s.c.title,
			Score:   s.sim,
			Because: s.anchor.Title,
		})

		if len(out) == recoMax {
			break
		}
	}

	return out
}

type recoState struct {
	mu       sync.Mutex
	items    []Rec
	at       time.Time
	model    string
	building atomic.Bool
}

var reco recoState

type recoBody struct {
	Items    []Rec  `json:"items"`
	At       int64  `json:"at"`
	Model    string `json:"model"`
	Building bool   `json:"building"`
}

// RecoHandler serves the cached row and never blocks on it. A stale or empty
// cache kicks a background rebuild and answers with whatever is already there,
// so a sleeping embedding host costs an empty row and nothing else.
func RecoHandler(c *gin.Context, lib *library.Library) {
	reco.mu.Lock()
	body := recoBody{Items: reco.items, Model: reco.model}

	ttl := recoTTL
	if len(reco.items) == 0 {
		ttl = recoRetryTTL
	}
	fresh := !reco.at.IsZero() && time.Since(reco.at) < ttl
	reco.mu.Unlock()

	if body.Items == nil {
		body.Items = []Rec{}
	}

	if !fresh {
		body.Building = RecoRefresh(lib)
	}

	reco.mu.Lock()
	if !reco.at.IsZero() {
		body.At = reco.at.UnixMilli()
	}
	reco.mu.Unlock()

	c.JSON(http.StatusOK, body)
}

// RecoRefresh starts a rebuild unless one is already running. Reports whether
// a build is in flight.
func RecoRefresh(lib *library.Library) bool {
	if !reco.building.CompareAndSwap(false, true) {
		return true
	}

	go func() {
		defer reco.building.Store(false)

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()

		buildReco(ctx, lib)
	}()

	return true
}

func buildReco(ctx context.Context, lib *library.Library) {
	if lib == nil || !embedReady() {
		return
	}

	model := embedModel()
	store := loadVecs(vecPath, model)

	hidden := lib.HiddenDirs()
	var cat []string
	for _, v := range lib.AllVideos() {
		if i := strings.Index(v.Rel, "/"); i > 0 && hidden[v.Rel[:i]] {
			continue
		}

		cat = append(cat, v.Rel)
	}

	watched := library.Watched()
	hist := make([]string, 0, len(watched))
	for p := range watched {
		hist = append(hist, p)
	}
	sort.Strings(hist)

	group := groupAll(cat, hist)
	anchors := pickAnchors(watched, group, maxAnchors)

	// The history is dir-qualified and files move, so an anchor's file is
	// often gone from the catalogue. Its vector is still worth having — taste
	// does not evaporate because a file was reorganised — so anchors are
	// embedded under their own basename hash whether or not they still exist.
	want := map[string]string{}
	for _, rel := range cat {
		want[library.Hash(path.Base(rel))] = titleOf(rel)
	}
	for _, a := range anchors {
		want[library.Hash(path.Base(a.Param))] = a.Title
	}

	if n := store.fill(ctx, want); n > 0 {
		if err := store.save(vecPath); err != nil {
			log.Printf("[reco] save vectors: %v", err)
		}
		log.Printf("[reco] embedded %d titles with %s (dim %d)", n, model, store.Dim)
	}

	// One vector per distinct title, keyed by the title itself: episodes of
	// one show share a title and there is no reason to hold or centre the
	// same vector 145 times.
	byTitle := map[string]vec{}
	for k, t := range want {
		if v, ok := store.get(k, t); ok {
			byTitle[t] = v
		}
	}

	centered := centerVecs(byTitle)

	cands := pickCands(cat, watched, group)

	if len(centered) == 0 {
		// No vectors at all means the embedding host is unreachable, not that
		// the library has run out of suggestions. Leave whatever row is
		// already up and let the next request try again.
		return
	}

	items := rankRecs(cands, anchors, centered)
	reasons(ctx, items)

	reco.mu.Lock()
	reco.items, reco.at, reco.model = items, time.Now(), model
	reco.mu.Unlock()
}

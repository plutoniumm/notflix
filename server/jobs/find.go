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
	"time"

	"github.com/gin-gonic/gin"

	"notflix/server/library"
)

// Plain-language search: "korean crime", "something short and funny". One
// vector per TITLE — a film, or a show whose episodes share one — embedded
// from its cleaned name plus a few of its blurbs. A bare name carries almost
// no meaning ("The Fix"); the blurbs are what let a query find a title whose
// name says nothing of the sort. Everything is optional: a sleeping ollama
// host means no semantic hits, never an error.
//
// The cutoffs were measured on this library with qwen3-embedding:4b and the
// instruct query below; another embedder needs them re-measured. True matches
// land 0.37–0.61, a query with no real answer here ("space travel") tops out
// near 0.31, and the floor stays at 0.30 because "cozy feel-good" → Notting
// Hill is a real match at 0.31. The spread trims the tail behind a strong one:
// "street racing" is Wangan Midnight at 0.61, and the 0.44 runner-up is a
// basketball film.
const (
	findFloor  = 0.30
	findSpread = 0.12
	findMax    = 12
	findBlurbs = 3
	findMinLen = 3

	// A typing pause re-sends the query it already embedded, and a cold
	// embedder load on the tailnet host can take this long.
	findQueryTTL     = 10 * time.Minute
	findQueryTimeout = 30 * time.Second
	findQueryCache   = 256
)

var (
	// Release tags carry no meaning but plenty of token overlap.
	titleJunk = junkRes(`nflx amzn hevc hdtv hdrip bluray webdl webrip web bd brrip br rip dvdrip dvd
		bdrip nfrip camrip msubs
		yify yts rarbg evo xvid etrg psa anoxmous sujaidr deejayahmed rdlinks immortal rawr kin now
		sartre bone neonoir mkvcage rapta elite bokutox scorp ettv ntb ion10 galaxyrg tgx qxr cm8
		fgt sparks amiable drones publichd mzabi playweb edith flux ntg cakes minx scorp
		x264 x265 h264 h265 avc av1 atmos truehd 6ch
		360p 576p 720p 480p 1080p 2160p 4k 10bit 8bit sd hd hq uhd ds4k hdscr fs
		dual audio multi subs sub subbed dubbed esub esubs vostfr eng ita hin jpn kor
		complete repack proper extended internal uncut remastered reenc split scenes rq
		season episode ep part`)

	bracketRe = regexp.MustCompile(`\[[^\]]*\]|\([^)]*\)`)
	seasonRe  = regexp.MustCompile(`(?i)\bs\d{1,2}(\s*e\d{1,3})?\b`)
	// No trailing \b: a re-release tags the episode itself ("S01E02v2").
	episodeRe = regexp.MustCompile(`(?i)\bs\d{1,2}\s*e\d{1,3}`)
	unitRe    = regexp.MustCompile(`(?i)\b\d+(mb|gb|ch|bit|bits|kbps|k)\b`)
	// Audio tags glue a channel count straight onto the codec — AAC5, DDP2,
	// DD5 — so a plain word boundary never sees them.
	codecRe  = regexp.MustCompile(`(?i)\b(aac|ac3|eac3|ddp|dd|dts|opus|flac)\d?\b`)
	decadeRe = regexp.MustCompile(`(?i)\b(19|20)\d0s\b`)
	numRe    = regexp.MustCompile(`\b\d{1,4}\b`)
	spaceRe  = regexp.MustCompile(`\s+`)
	wordRe   = regexp.MustCompile(`[^a-z0-9]+`)
	extRe    = regexp.MustCompile(`(?i)\.(mp4|mkv|mov)$`)
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
	s = extRe.ReplaceAllString(s, "")
	s = bracketRe.ReplaceAllString(s, " ")
	s = strings.NewReplacer("-", " ", ".", " ", "_", " ", "+", " ").Replace(s)
	s = seasonRe.ReplaceAllString(s, " ")
	s = unitRe.ReplaceAllString(s, " ")
	s = decadeRe.ReplaceAllString(s, " ")
	s = codecRe.ReplaceAllString(s, " ")

	for _, re := range titleJunk {
		s = re.ReplaceAllString(s, " ")
	}

	s = numRe.ReplaceAllString(s, " ")

	return strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
}

// titleOf names the title one file belongs to. An episode is whatever precedes
// its SxxEyy marker, which reunites a show across season folders that are each
// named differently ("Breaking Bad Season 3 Complete…", "Breaking Bad (2008)
// Season 4 S04 + Extras…") without guessing which folders are shows.
//
// Anime numbers episodes bare ("Wangan Midnight 01 Devil Z"), and a bare number
// can't be the marker — "Punjab 95 Satluj" is a film. So inside a folder big
// enough to be a show (many), a file named after its folder belongs to it. Two
// sequels in a two-film folder ("The Roundup", "The Roundup Punishment") stay
// apart. Anything else is its folder plus its file, the shorter dropped when
// one already contains the other.
func titleOf(rel string, many bool) string {
	dir, base := path.Split(rel)
	ct := cleanTitle(strings.Trim(dir, "/"))

	if loc := episodeRe.FindStringIndex(base); loc != nil {
		if show := cleanTitle(base[:loc[0]]); show != "" {
			return show
		}

		return ct
	}

	cb := cleanTitle(base)

	if ct == "" {
		return cb
	}

	if cb == "" {
		return ct
	}

	lt, lb := strings.ToLower(ct), strings.ToLower(cb)
	if many && strings.HasPrefix(normKey(lb), normKey(lt)) {
		return ct
	}

	if strings.Contains(lb, lt) {
		return cb
	}

	if strings.Contains(lt, lb) {
		return ct
	}

	return ct + " " + cb
}

func normKey(s string) string {
	return strings.TrimSpace(wordRe.ReplaceAllString(strings.ToLower(s), " "))
}

type title struct {
	key  string
	name string
	rels []string
}

// groupTitles folds a catalogue into titles by normalised name, so a show's
// episodes share one while a franchise's films ("Transformers The Last Knight",
// "Transformers Age of Extinction") stay apart. Files sort by basename first,
// case-folded, so a show's representative is its earliest episode even when
// its season folders sort the other way ("…(2008) Season 4" < "…Season 3").
func groupTitles(cat []string) []title {
	rels := append([]string(nil), cat...)
	sort.Slice(rels, func(i, j int) bool {
		a, b := strings.ToLower(path.Base(rels[i])), strings.ToLower(path.Base(rels[j]))
		if a != b {
			return a < b
		}

		return rels[i] < rels[j]
	})

	count := map[string]int{}
	for _, rel := range rels {
		count[path.Dir(rel)]++
	}

	at := map[string]int{}
	var out []title

	for _, rel := range rels {
		dir := path.Dir(rel)
		name := titleOf(rel, dir != "." && count[dir] >= 3)
		k := normKey(name)
		if k == "" {
			continue
		}

		i, ok := at[k]
		if !ok {
			i = len(out)
			at[k] = i
			out = append(out, title{key: k, name: name})
		}

		// "breaking bad s03e01 brrip" must not name the show: a capitalised
		// proper noun is what lets the embedder recall what it is.
		if out[i].name == strings.ToLower(out[i].name) {
			out[i].name = name
		}

		out[i].rels = append(out[i].rels, rel)
	}

	return out
}

// docOf is the text a title is embedded from: its name, then the first few
// distinct blurbs among its files. Early episodes describe the premise, which
// is what a query is usually reaching for.
func docOf(t title, blurbs map[string]string) string {
	parts := []string{t.name}
	seen := map[string]bool{}

	for _, rel := range t.rels {
		b := blurbs[library.Hash(path.Base(rel))]
		if b == "" || seen[b] {
			continue
		}
		seen[b] = true

		parts = append(parts, b)
		if len(parts) > findBlurbs {
			break
		}
	}

	return strings.Join(parts, "\n")
}

// Embedders are trained with an asymmetric format: a short query and a long
// document are different tasks. Raw text on both sides measurably blurs the
// ranking, and each model family spells the task differently.
func asQuery(model, q string) string {
	switch {
	case strings.Contains(model, "qwen3-embedding"):
		return "Instruct: Given a description of a film or TV show, retrieve the titles that match it\nQuery: " + q
	case strings.Contains(model, "nomic"):
		return "search_query: " + q
	}

	return q
}

func asDoc(model, d string) string {
	if strings.Contains(model, "nomic") {
		return "search_document: " + d
	}

	return d
}

// catalogue mirrors what the video list serves — .mp4 only, one level deep,
// hidden dirs dropped — so every hit is a tile the frontend already has.
func catalogue(lib *library.Library) []string {
	hidden := lib.HiddenDirs()

	var out []string
	for _, v := range lib.AllVideos() {
		if !strings.HasSuffix(v.Rel, ".mp4") || strings.Count(v.Rel, "/") > 1 {
			continue
		}

		if i := strings.Index(v.Rel, "/"); i > 0 && hidden[v.Rel[:i]] {
			continue
		}

		out = append(out, v.Rel)
	}

	return out
}

func blurbMap() map[string]string {
	out := map[string]string{}
	for k, v := range library.KVPrefix(blurbPrefix) {
		if s, ok := v.(string); ok && s != "" {
			out[k] = s
		}
	}

	return out
}

type findIdx struct {
	model string
	rels  []string
	vecs  []vec
}

type qvec struct {
	v  vec
	at time.Time
}

var finder struct {
	mu  sync.Mutex
	idx *findIdx
	qs  map[string]qvec
}

// indexOf joins titles to the vectors the store holds for their CURRENT text,
// so a title whose blurbs changed since it was embedded is simply absent until
// the next pass rather than matched on stale meaning.
func indexOf(ts []title, docs map[string]string, store *vecs) *findIdx {
	idx := &findIdx{model: store.Model}

	for _, t := range ts {
		v, ok := store.get(t.key, docs[t.key])
		if !ok {
			continue
		}

		idx.rels = append(idx.rels, t.rels[0])
		idx.vecs = append(idx.vecs, unit(v))
	}

	return idx
}

func docsOf(ts []title, model string) map[string]string {
	blurbs := blurbMap()

	out := make(map[string]string, len(ts))
	for _, t := range ts {
		out[t.key] = asDoc(model, docOf(t, blurbs))
	}

	return out
}

// EmbedTitles is the `embed` phase: embed every title whose text is new or
// changed, drop the ones that left the library, and swap in the new index.
func EmbedTitles(lib *library.Library) {
	model := embedModel()
	if !ollamaHasModel(ollamaHost(), model) {
		log.Printf("[find] embedder %q unavailable — skipping", model)

		return
	}

	ts := groupTitles(catalogue(lib))
	docs := docsOf(ts, model)
	store := loadVecs(vecPath, model)

	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	n := store.fill(ctx, docs)
	gone := store.prune(docs)

	if n > 0 || gone > 0 {
		if err := store.save(vecPath); err != nil {
			log.Printf("[find] save vectors: %v", err)
		}
		log.Printf("[find] embedded %d titles, dropped %d (%s, dim %d)", n, gone, model, store.Dim)
	}

	finder.mu.Lock()
	finder.idx = indexOf(ts, docs, store)
	finder.mu.Unlock()
}

// current serves the in-memory index, loading it from disk on the first query
// after a restart so search works before this boot's embed phase has run.
func current(lib *library.Library) *findIdx {
	finder.mu.Lock()
	defer finder.mu.Unlock()

	if finder.idx == nil {
		model := embedModel()
		ts := groupTitles(catalogue(lib))
		finder.idx = indexOf(ts, docsOf(ts, model), loadVecs(vecPath, model))
	}

	return finder.idx
}

func queryVec(ctx context.Context, model, q string) (vec, error) {
	k := model + "\x00" + q

	finder.mu.Lock()
	hit, ok := finder.qs[k]
	finder.mu.Unlock()

	if ok && time.Since(hit.at) < findQueryTTL {
		return hit.v, nil
	}

	got, err := embedOnce(ctx, model, []string{asQuery(model, q)})
	if err != nil {
		return nil, err
	}

	v := unit(got[0])

	finder.mu.Lock()
	// ponytail: wholesale reset at the cap, an LRU if typing patterns ever thrash it
	if finder.qs == nil || len(finder.qs) >= findQueryCache {
		finder.qs = map[string]qvec{}
	}
	finder.qs[k] = qvec{v: v, at: time.Now()}
	finder.mu.Unlock()

	return v, nil
}

type Hit struct {
	Dir   string  `json:"dir"`
	Name  string  `json:"name"`
	Key   string  `json:"key"`
	Score float64 `json:"score"`
}

func rank(idx *findIdx, q vec) []Hit {
	type scored struct {
		rel string
		sim float64
	}

	var all []scored
	for i, v := range idx.vecs {
		if s := cosine(q, v); s >= findFloor {
			all = append(all, scored{idx.rels[i], s})
		}
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].sim != all[j].sim {
			return all[i].sim > all[j].sim
		}

		return all[i].rel < all[j].rel
	})

	out := []Hit{}
	for _, s := range all[:min(len(all), findMax)] {
		if s.sim < all[0].sim-findSpread {
			break
		}

		dir, name := path.Split(s.rel)
		dir = strings.TrimSuffix(dir, "/")
		if dir == "" {
			dir = "."
		}

		out = append(out, Hit{Dir: dir, Name: name, Key: library.Hash(name), Score: s.sim})
	}

	return out
}

// FindHandler answers `[]` for anything it cannot do — a short query, an
// empty index, a sleeping host — so the search box never toasts an error.
func FindHandler(c *gin.Context, lib *library.Library) {
	q := strings.TrimSpace(c.Query("q"))
	if lib == nil || len(q) < findMinLen {
		c.JSON(http.StatusOK, []Hit{})

		return
	}

	idx := current(lib)
	if len(idx.vecs) == 0 {
		c.JSON(http.StatusOK, []Hit{})

		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), findQueryTimeout)
	defer cancel()

	v, err := queryVec(ctx, idx.model, q)
	if err != nil {
		log.Printf("[find] embed query: %v", err)
		c.JSON(http.StatusOK, []Hit{})

		return
	}

	c.JSON(http.StatusOK, rank(idx, v))
}

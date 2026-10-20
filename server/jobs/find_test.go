package jobs

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"notflix/server/library"
)

func near(a, b float64) bool {
	return math.Abs(a-b) < 1e-6
}

func TestCosineKnownVectors(t *testing.T) {
	cases := []struct {
		name string
		a, b vec
		want float64
	}{
		{"identical", vec{1, 0, 0}, vec{1, 0, 0}, 1},
		{"orthogonal", vec{1, 0, 0}, vec{0, 1, 0}, 0},
		{"opposite", vec{1, 0, 0}, vec{-1, 0, 0}, -1},
		{"45deg", unit(vec{1, 1, 0}), vec{1, 0, 0}, math.Sqrt2 / 2},
		{"mismatched length scores zero", vec{1, 0, 0}, vec{1, 0}, 0},
		{"empty scores zero", vec{}, vec{}, 0},
		{"nil scores zero", nil, vec{1, 0, 0}, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cosine(c.a, c.b); !near(got, c.want) {
				t.Fatalf("cosine = %v, want %v", got, c.want)
			}
		})
	}
}

func TestUnitZeroVectorSurvives(t *testing.T) {
	if got := unit(vec{0, 0, 0}); len(got) != 3 || got[0] != 0 {
		t.Fatalf("zero vector: %v", got)
	}

	u := unit(vec{3, 4})
	if !near(cosine(u, u), 1) {
		t.Fatalf("unit not normalised: %v", u)
	}
}

func TestCleanTitleStripsReleaseNoise(t *testing.T) {
	cases := map[string]string{
		"The.Sopranos.S06.1080p.BluRay.x265-RARBG":                       "The Sopranos",
		"[Judas] Bleach 280-308 [BD 1080p][HEVC x265 10bit][Dual-Audio]": "Bleach",
		// A spelled-out language is content signal, not a release tag.
		// Track-code abbreviations (eng/ita/hin) are stripped, full names kept.
		"Udta.Punjab.2016.1080p.BluRay.x264.Hindi.AAC-ETRG.mp4":           "Udta Punjab Hindi",
		"The Mentalist 2008 Season 4 Complete 720p AMZN WEBRip x264":      "The Mentalist",
		"Transformers.The.Last.Knight.2017.1080p.BluRay.x264-[YTS.AG]":    "Transformers The Last Knight",
		"Seven.Samurai.1954.CRITERION.1080p.BluRay.x264.anoXmous_.mp4":    "Seven Samurai CRITERION",
		"Killers Of The Flower Moon 2023 1080p WEBRip AAC5 1 10bits BONE": "Killers Of The Flower Moon",
		"Primetime.2026.camrip.360p.mp4":                                  "Primetime",
		"Paprika.2006.BDRip.mp4":                                          "Paprika",
	}

	for in, want := range cases {
		if got := cleanTitle(in); got != want {
			t.Errorf("cleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTitleOf(t *testing.T) {
	cases := []struct {
		rel  string
		many bool
		want string
	}{
		{"The.Sopranos.S01.1080p.BluRay.x265-RARBG/The.Sopranos.S01E01.mp4", false, "The Sopranos"},
		{"2000s/Gladiator.2000.1080p.BluRay.x264.mp4", true, "Gladiator"},
		// The marker decides, not the folder, so a doc series filed in a
		// bucket folder still names itself.
		{"Documentaries/World War II with Tom Hanks S01E14 ELiTE.mp4", false, "World War II with Tom Hanks"},
		{"Smoking/Smoking Behind the Supermarket with You S01E02v2.mp4", false, "Smoking Behind the Supermarket with You"},
		{"Show/S01E01.mp4", false, "Show"},
		{"Wangan Midnight/Wangan Midnight 01 Devil Z.mp4", true, "Wangan Midnight"},
		// Two sequels in a two-film folder are two films.
		{"The Roundup (2022) [1080p]/The.Roundup.Punishment.2024.1080p.mp4", false, "The Roundup Punishment"},
		{"Normal.mp4", false, "Normal"},
	}

	for _, c := range cases {
		if got := titleOf(c.rel, c.many); got != c.want {
			t.Errorf("titleOf(%q, %v) = %q, want %q", c.rel, c.many, got, c.want)
		}
	}
}

func TestGroupTitles(t *testing.T) {
	cat := []string{
		"Breaking Bad (2008) Season 4 S04 + Extras (1080p BluRay x265)/Breaking Bad S04E01 Box Cutter.mp4",
		"Breaking Bad Season 3 Complete 720p.BRrip.Sujaidr/breaking bad s03e02 brrip sujaidr.mp4",
		"Breaking Bad Season 3 Complete 720p.BRrip.Sujaidr/breaking bad s03e01 brrip sujaidr.mp4",
		"2000s/Gladiator.2000.1080p.mp4",
		"2000s/Adaptation.2002.1080p.mp4",
		"2000s/Millennium Actress.mp4",
		"The Roundup (2022) [1080p]/The.Roundup.2022.1080p.mp4",
		"The Roundup (2022) [1080p]/The.Roundup.Punishment.2024.1080p.mp4",
		"Wangan Midnight/Wangan Midnight 02 Blackbird.mp4",
		"Wangan Midnight/Wangan Midnight 01 Devil Z.mp4",
		"Wangan Midnight/Wangan Midnight 03 Akio.mp4",
	}

	got := map[string]title{}
	for _, tt := range groupTitles(cat) {
		got[tt.key] = tt
	}

	if len(got) != 7 {
		t.Fatalf("want 7 titles, got %d: %v", len(got), got)
	}

	bb := got["breaking bad"]
	if len(bb.rels) != 3 {
		t.Fatalf("Breaking Bad split across its season folders: %+v", bb)
	}

	if !strings.HasSuffix(bb.rels[0], "s03e01 brrip sujaidr.mp4") {
		t.Fatalf("representative should be the earliest episode, got %q", bb.rels[0])
	}

	if bb.name != "Breaking Bad" {
		t.Fatalf("a lowercase file named the show: %q", bb.name)
	}

	if w := got["wangan midnight"]; len(w.rels) != 3 || !strings.Contains(w.rels[0], " 01 ") {
		t.Fatalf("bare-numbered anime not folded: %+v", w)
	}

	if _, ok := got["the roundup punishment"]; !ok {
		t.Fatal("a sequel merged into its first film")
	}

	if _, ok := got["gladiator"]; !ok {
		t.Fatal("a decade bucket collapsed")
	}
}

func TestDocOf(t *testing.T) {
	ep := func(n string) string { return "Show/Show S01E0" + n + ".mp4" }
	blurbs := map[string]string{
		library.Hash("Show S01E01.mp4"): "one",
		library.Hash("Show S01E02.mp4"): "one",
		library.Hash("Show S01E03.mp4"): "two",
		library.Hash("Show S01E05.mp4"): "three",
		library.Hash("Show S01E06.mp4"): "four",
	}

	got := docOf(title{name: "Show", rels: []string{ep("1"), ep("2"), ep("3"), ep("4"), ep("5"), ep("6")}}, blurbs)
	if got != "Show\none\ntwo\nthree" {
		t.Fatalf("want name + %d distinct blurbs, got %q", findBlurbs, got)
	}

	if got := docOf(title{name: "Bare", rels: []string{"Bare.mp4"}}, blurbs); got != "Bare" {
		t.Fatalf("no blurb should embed the name alone, got %q", got)
	}
}

func TestAsQueryAsDoc(t *testing.T) {
	if q := asQuery("qwen3-embedding:4b-q8_0", "heist"); !strings.HasPrefix(q, "Instruct: ") || !strings.HasSuffix(q, "\nQuery: heist") {
		t.Fatalf("qwen query = %q", q)
	}

	if d := asDoc("qwen3-embedding:4b-q8_0", "x"); d != "x" {
		t.Fatalf("qwen documents take no prefix, got %q", d)
	}

	if q, d := asQuery("nomic-embed-text:latest", "q"), asDoc("nomic-embed-text:latest", "d"); q != "search_query: q" || d != "search_document: d" {
		t.Fatalf("nomic = %q / %q", q, d)
	}

	if q := asQuery("other", "q"); q != "q" {
		t.Fatalf("unknown model got a prefix: %q", q)
	}
}

// at returns a unit vector at angle a (radians) from the x axis, so a cosine
// against {1, 0} is exactly cos(a).
func at(cos float64) vec {
	return vec{float32(cos), float32(math.Sqrt(1 - cos*cos))}
}

func TestRankFloorSpreadAndCap(t *testing.T) {
	q := vec{1, 0}

	idx := &findIdx{
		rels: []string{"Strong.mp4", "Show/Runner Up.mp4", "Tail.mp4", "Noise.mp4"},
		vecs: []vec{at(0.60), at(0.50), at(0.40), at(0.25)},
	}

	got := rank(idx, q)
	if len(got) != 2 || got[0].Name != "Strong.mp4" || got[1].Name != "Runner Up.mp4" {
		t.Fatalf("want the two within %v of the best, got %+v", findSpread, got)
	}

	if got[0].Dir != "." || got[1].Dir != "Show" || got[1].Key != library.Hash("Runner Up.mp4") {
		t.Fatalf("card fields wrong: %+v", got)
	}

	weak := &findIdx{rels: []string{"A.mp4", "B.mp4"}, vecs: []vec{at(0.29), at(0.10)}}
	if got := rank(weak, q); len(got) != 0 {
		t.Fatalf("a query with no real answer must return nothing, got %+v", got)
	}

	many := &findIdx{}
	for i := range findMax + 5 {
		many.rels = append(many.rels, strings.Repeat("x", i+1)+".mp4")
		many.vecs = append(many.vecs, at(0.5))
	}

	if got := rank(many, q); len(got) != findMax {
		t.Fatalf("want %d hits, got %d", findMax, len(got))
	}
}

func TestIndexSkipsStaleText(t *testing.T) {
	ts := []title{
		{key: "fresh", name: "Fresh", rels: []string{"Fresh.mp4"}},
		{key: "stale", name: "Stale", rels: []string{"Stale.mp4"}},
		{key: "never", name: "Never", rels: []string{"Never.mp4"}},
	}

	store := newVecs("m")
	store.put("fresh", "Fresh\nnew blurb", vec{1, 0})
	store.put("stale", "Stale", vec{0, 1})

	docs := map[string]string{"fresh": "Fresh\nnew blurb", "stale": "Stale\nblurb arrived", "never": "Never"}

	idx := indexOf(ts, docs, store)
	if len(idx.rels) != 1 || idx.rels[0] != "Fresh.mp4" {
		t.Fatalf("only the title whose text still matches belongs in the index: %v", idx.rels)
	}
}

func TestPruneDropsDepartedTitles(t *testing.T) {
	s := newVecs("m")
	s.put("kept", "a", vec{1})
	s.put("gone", "b", vec{1})

	if n := s.prune(map[string]string{"kept": "a"}); n != 1 {
		t.Fatalf("pruned %d, want 1", n)
	}

	if _, ok := s.Rec["gone"]; ok {
		t.Fatal("departed title survived")
	}

	if _, ok := s.Rec["kept"]; !ok {
		t.Fatal("live title pruned")
	}
}

func findReq(t *testing.T, lib *library.Library, q string) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/find?q="+url.QueryEscape(q), nil)

	FindHandler(c, lib)

	return w.Code, strings.TrimSpace(w.Body.String())
}

func withIndex(t *testing.T, idx *findIdx) {
	t.Helper()

	finder.mu.Lock()
	finder.idx, finder.qs = idx, nil
	finder.mu.Unlock()

	t.Cleanup(func() {
		finder.mu.Lock()
		finder.idx, finder.qs = nil, nil
		finder.mu.Unlock()
	})
}

func TestFindHandlerNeverErrors(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "http://127.0.0.1:1")
	withIndex(t, &findIdx{model: "m", rels: []string{"A.mp4"}, vecs: []vec{{1, 0}}})

	for _, c := range []struct {
		lib *library.Library
		q   string
	}{
		{nil, "heist"},
		{&library.Library{}, "hi"},
		{&library.Library{}, "a sleeping host"},
	} {
		if code, body := findReq(t, c.lib, c.q); code != http.StatusOK || body != "[]" {
			t.Fatalf("q=%q: %d %s", c.q, code, body)
		}
	}
}

func TestFindHandlerRanksAgainstQuery(t *testing.T) {
	srv := fakeOllama(t, 2)
	t.Setenv("OLLAMA_HOST", srv.URL)
	withIndex(t, &findIdx{model: "m", rels: []string{"Hit.mp4", "Miss.mp4"}, vecs: []vec{{1, 0}, {0, 1}}})

	code, body := findReq(t, &library.Library{}, "korean crime")
	if code != http.StatusOK || !strings.Contains(body, `"name":"Hit.mp4"`) || strings.Contains(body, "Miss.mp4") {
		t.Fatalf("%d %s", code, body)
	}

	v, err := queryVec(context.Background(), "m", "korean crime")
	if err != nil || len(v) != 2 {
		t.Fatalf("cached query vector: %v %v", v, err)
	}
}

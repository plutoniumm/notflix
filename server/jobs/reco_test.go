package jobs

import (
	"math"
	"strings"
	"testing"

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

func TestCenterRemovesSharedComponent(t *testing.T) {
	// Every vector shares a big constant component, which is what makes raw
	// cosine over short titles useless. Centering must strip it.
	in := map[string]vec{
		"a": {10, 1, 0},
		"b": {10, 0, 1},
		"c": {10, 0, -1},
	}

	raw := cosine(unit(in["a"]), unit(in["b"]))
	out := centerVecs(in)
	got := cosine(out["a"], out["b"])

	if raw < 0.9 {
		t.Fatalf("expected raw similarity to be crushed high, got %v", raw)
	}

	if got >= raw {
		t.Fatalf("centering did not spread: raw %v centered %v", raw, got)
	}

	for k, v := range out {
		if !near(cosine(v, v), 1) {
			t.Fatalf("%s not unit after centering", k)
		}
	}
}

func TestCenterEmptyAndRagged(t *testing.T) {
	if got := centerVecs(nil); len(got) != 0 {
		t.Fatalf("centerVecs(nil) = %v", got)
	}

	// A vector of the wrong length is from another model's space; it must be
	// dropped, never truncated into a plausible-looking answer.
	out := centerVecs(map[string]vec{"a": {1, 2, 3}, "b": {1, 2, 3}, "short": {1}})
	if _, ok := out["short"]; ok {
		t.Fatal("ragged vector survived centering")
	}
}

func TestIsWatched(t *testing.T) {
	cases := []struct {
		name string
		w    library.Watch
		want bool
	}{
		{"thirty second open", library.Watch{T: 30, Dur: 6000}, false},
		{"five minutes of a two hour film", library.Watch{T: 300, Dur: 7200}, false},
		{"three quarters of a film", library.Watch{T: 5400, Dur: 7200}, true},
		{"exactly the fraction", library.Watch{T: 4320, Dur: 7200}, true},
		{"just under the fraction", library.Watch{T: 4319, Dur: 7200}, false},
		{"no duration, long watch", library.Watch{T: 1200}, true},
		{"no duration, brief open", library.Watch{T: 120}, false},
		{"finished short under the floor", library.Watch{T: 100, Dur: 100}, false},
		{"zero", library.Watch{}, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isWatched(c.w); got != c.want {
				t.Fatalf("isWatched(%+v) = %v, want %v", c.w, got, c.want)
			}
		})
	}
}

func TestCleanTitleStripsReleaseNoise(t *testing.T) {
	cases := map[string]string{
		"The.Sopranos.S06.1080p.BluRay.x265-RARBG":                       "The Sopranos",
		"[Judas] Bleach 280-308 [BD 1080p][HEVC x265 10bit][Dual-Audio]": "Bleach",
		// A spelled-out language is content signal, not a release tag: it is
		// what pulls this library's Hindi cinema together. Track-code
		// abbreviations (eng/ita/hin) are stripped, full names are kept.
		"Udta.Punjab.2016.1080p.BluRay.x264.Hindi.AAC-ETRG.mp4":           "Udta Punjab Hindi",
		"The Mentalist 2008 Season 4 Complete 720p AMZN WEBRip x264":      "The Mentalist",
		"Transformers.The.Last.Knight.2017.1080p.BluRay.x264-[YTS.AG]":    "Transformers The Last Knight",
		"Seven.Samurai.1954.CRITERION.1080p.BluRay.x264.anoXmous_.mp4":    "Seven Samurai CRITERION",
		"Killers Of The Flower Moon 2023 1080p WEBRip AAC5 1 10bits BONE": "Killers Of The Flower Moon",
	}

	for in, want := range cases {
		if got := cleanTitle(in); got != want {
			t.Errorf("cleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTitleOfComposesFolderAndFile(t *testing.T) {
	cases := map[string]string{
		"The.Sopranos.S01.1080p.BluRay.x265-RARBG/The.Sopranos.S01E01.mp4": "The Sopranos",
		"2000s/Gladiator.2000.1080p.BluRay.x264.mp4":                       "Gladiator",
		"Documentaries/World War II with Tom Hanks S01E14 ELiTE.mp4":       "Documentaries World War II with Tom Hanks",
		"Normal.mp4": "Normal",
	}

	for in, want := range cases {
		if got := titleOf(in); got != want {
			t.Errorf("titleOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGroupAllClustersShowsNotBuckets(t *testing.T) {
	cat := []string{
		"The.Sopranos.S01.1080p.BluRay.x265-RARBG/The.Sopranos.S01E01.mp4",
		"The.Sopranos.S01.1080p.BluRay.x265-RARBG/The.Sopranos.S01E02.mp4",
		"The.Sopranos.S01.1080p.BluRay.x265-RARBG/The.Sopranos.S01E03.mp4",
		"The.Sopranos.S02.1080p.BluRay.x265-RARBG/The.Sopranos.S02E01.mp4",
		"The.Sopranos.S02.1080p.BluRay.x265-RARBG/The.Sopranos.S02E02.mp4",
		"The.Sopranos.S02.1080p.BluRay.x265-RARBG/The.Sopranos.S02E03.mp4",
		"Scorpion Season 1 Complete 720p HDTV x264/Scorpion S01E01 Pilot.mp4",
		"Scorpion Season 1 Complete 720p HDTV x264/Scorpion S01E02 Single Point of Failure.mp4",
		"Scorpion Season 1 Complete 720p HDTV x264/Scorpion S01E03 A Cyclone.mp4",
		"2000s/Gladiator.2000.1080p.mp4",
		"2000s/Adaptation.2002.1080p.mp4",
		"2000s/Millennium.Actress.2001.1080p.mp4",
	}

	g := groupAll(cat, nil)

	if g[cat[0]] != g[cat[3]] {
		t.Fatalf("Sopranos seasons split: %q vs %q", g[cat[0]], g[cat[3]])
	}

	if g[cat[6]] != g[cat[8]] {
		t.Fatalf("Scorpion episodes split: %q vs %q", g[cat[6]], g[cat[8]])
	}

	if g[cat[9]] == g[cat[10]] {
		t.Fatalf("a decade bucket collapsed into one cluster: %q", g[cat[9]])
	}

	if g[cat[0]] == g[cat[6]] {
		t.Fatal("two unrelated shows merged")
	}
}

func TestGroupAllReachesHistoryWhoseFilesMoved(t *testing.T) {
	// The watch history is dir-qualified and CleanAll/Organize move files, so
	// history entries routinely name paths the catalogue no longer has. They
	// still need a cluster key, and films of one franchise must land together.
	hist := []string{
		"Harry Potter/Harry.Potter.and.the.Goblet.of.Fire.2005.1080p.mp4",
		"Harry.Potter.and.the.Chamber.of.Secrets.2002.1080p.mp4",
		"Harry Potter/Harry.Potter.and.the.Order.of.the.Phoenix.2007.1080p.mp4",
	}

	g := groupAll([]string{"Normal.mp4"}, hist)

	if g[hist[0]] != g[hist[1]] || g[hist[1]] != g[hist[2]] {
		t.Fatalf("franchise split: %q %q %q", g[hist[0]], g[hist[1]], g[hist[2]])
	}
}

func TestMergeKeepsDistinctFilmsApart(t *testing.T) {
	raw := map[string]string{
		"a": "transformers age of extinction",
		"b": "transformers the last knight",
		"c": "the sopranos",
		"d": "the sheep detectives",
		"e": "breaking bad",
		"f": "breaking bad deejayahmed",
	}

	g := mergeGroups(raw)

	if g["a"] == g["b"] {
		t.Fatal("two different Transformers films merged on one shared token")
	}

	if g["c"] == g["d"] {
		t.Fatal(`merged on the shared leading word "the"`)
	}

	if g["e"] != g["f"] {
		t.Fatalf("release-group variant not merged: %q vs %q", g["e"], g["f"])
	}

	if g["e"] != "breaking bad" {
		t.Fatalf("canonical key should be the shorter one, got %q", g["e"])
	}
}

func TestPickAnchorsRecentAndOnePerShow(t *testing.T) {
	watched := map[string]library.Watch{
		"HP/Harry.Potter.and.the.Goblet.of.Fire.mp4":       {T: 8000, Dur: 9000, At: 500},
		"HP/Harry.Potter.and.the.Chamber.of.Secrets.mp4":   {T: 8000, Dur: 9000, At: 400},
		"HP/Harry.Potter.and.the.Order.of.the.Phoenix.mp4": {T: 8000, Dur: 9000, At: 300},
		"Gladiator.mp4":                        {T: 6000, Dur: 8000, At: 200},
		"Sampled.mp4":                          {T: 20, Dur: 8000, At: 999},
		"The Sopranos/The.Sopranos.S01E01.mp4": {T: 3000, Dur: 3600, At: 100},
	}

	var hist []string
	for p := range watched {
		hist = append(hist, p)
	}
	group := groupAll(nil, hist)

	got := pickAnchors(watched, group, 10)

	if len(got) != 3 {
		t.Fatalf("want 3 anchors (one Potter, Gladiator, Sopranos), got %d: %+v", len(got), got)
	}

	if got[0].At != 500 {
		t.Fatalf("anchors not recency-ordered: %+v", got)
	}

	for _, a := range got {
		if strings.Contains(a.Param, "Sampled") {
			t.Fatal("a 20-second open became an anchor")
		}
	}

	seen := map[string]bool{}
	for _, a := range got {
		g := group[a.Param]
		if seen[g] {
			t.Fatalf("two anchors from cluster %q", g)
		}
		seen[g] = true
	}
}

func TestPickAnchorsEmptyHistory(t *testing.T) {
	if got := pickAnchors(nil, nil, 10); len(got) != 0 {
		t.Fatalf("want none, got %+v", got)
	}
}

func TestPickExcludesWatchedAndContinueWatching(t *testing.T) {
	cat := []string{
		"Gladiator.mp4",
		"Sub/Moved.Film.mp4",
		"Fresh.Film.mp4",
		"Half.Watched.mp4",
	}

	watched := map[string]library.Watch{
		// Finished — must not be recommended back.
		"Gladiator.mp4": {T: 7000, Dur: 8000, At: 3},
		// Continue Watching: part-way through, so it is already on the home
		// screen and must not appear here too.
		"Half.Watched.mp4": {T: 400, Dur: 8000, At: 2},
		// The history remembers an old path; the file now lives in Sub/.
		"Old/Moved.Film.mp4": {T: 7000, Dur: 8000, At: 1},
	}

	group := groupAll(cat, []string{"Old/Moved.Film.mp4"})
	got := pickCands(cat, watched, group)

	if len(got) != 1 || got[0].rel != "Fresh.Film.mp4" {
		t.Fatalf("want only Fresh.Film.mp4, got %+v", got)
	}
}

func TestPickEmptyLibrary(t *testing.T) {
	if got := pickCands(nil, nil, nil); len(got) != 0 {
		t.Fatalf("want none, got %+v", got)
	}
}

// axis builds a synthetic unit vector pointing along one dimension, mixed
// slightly toward another so similarity is tunable without real embeddings.
func axis(dim, i int, tilt float32, j int) vec {
	v := make(vec, dim)
	v[i] = 1
	v[j] += tilt

	return unit(v)
}

func TestRankSuppressesSameShowClustering(t *testing.T) {
	const dim = 8
	anchorVec := axis(dim, 0, 0, 0)

	vecs := map[string]vec{
		"Anchor Show": anchorVec,
		// Five near-identical episode titles of one show, all closer to the
		// anchor than the lone unrelated film.
		"Cluster Show": axis(dim, 0, 0.2, 1),
		"Other Film":   axis(dim, 0, 1.6, 2),
	}

	var cands []candidate
	for i, name := range []string{"e01", "e02", "e03", "e04", "e05"} {
		_ = i
		cands = append(cands, candidate{rel: "Cluster Show/" + name + ".mp4", title: "Cluster Show", group: "cluster show"})
	}
	cands = append(cands, candidate{rel: "Other Film.mp4", title: "Other Film", group: "other film"})

	got := rankRecs(cands, []anchor{{Param: "a.mp4", Title: "Anchor Show"}}, vecs)

	if len(got) != 2 {
		t.Fatalf("want 2 picks (one per cluster), got %d: %+v", len(got), got)
	}

	if got[0].Title != "Cluster Show" || got[1].Title != "Other Film" {
		t.Fatalf("wrong picks or order: %+v", got)
	}

	if got[0].Because != "Anchor Show" {
		t.Fatalf("attribution lost: %+v", got[0])
	}
}

func TestRankCapsPerAnchor(t *testing.T) {
	const dim = 8
	vecs := map[string]vec{"A": axis(dim, 0, 0, 0)}

	var cands []candidate
	for i, n := range []string{"one", "two", "three", "four"} {
		title := "cand " + n
		vecs[title] = axis(dim, 0, float32(i)*0.05+0.05, 1)
		cands = append(cands, candidate{rel: n + ".mp4", title: title, group: n})
	}

	got := rankRecs(cands, []anchor{{Param: "a.mp4", Title: "A"}}, vecs)

	if len(got) != perAnchorMax {
		t.Fatalf("want %d picks from one anchor, got %d: %+v", perAnchorMax, len(got), got)
	}
}

func TestRankDropsWeakMatches(t *testing.T) {
	const dim = 8
	vecs := map[string]vec{
		"A":       axis(dim, 0, 0, 0),
		"far":     axis(dim, 3, 0, 3),
		"further": axis(dim, 4, 0, 4),
	}

	cands := []candidate{
		{rel: "far.mp4", title: "far", group: "far"},
		{rel: "further.mp4", title: "further", group: "further"},
	}

	if got := rankRecs(cands, []anchor{{Param: "a.mp4", Title: "A"}}, vecs); len(got) != 0 {
		t.Fatalf("orthogonal titles scored above the floor: %+v", got)
	}
}

func TestRankWithoutVectorsOrAnchors(t *testing.T) {
	cands := []candidate{{rel: "a.mp4", title: "a", group: "a"}}

	if got := rankRecs(cands, nil, nil); len(got) != 0 {
		t.Fatalf("no anchors should mean no picks, got %+v", got)
	}

	if got := rankRecs(cands, []anchor{{Param: "x", Title: "x"}}, map[string]vec{}); len(got) != 0 {
		t.Fatalf("no vectors should mean no picks, got %+v", got)
	}
}

func TestRankSingleTitleLibrary(t *testing.T) {
	const dim = 4
	v := axis(dim, 0, 0, 0)

	// One title, and it is the one that was watched — nothing left to suggest.
	got := rankRecs(nil, []anchor{{Param: "only.mp4", Title: "Only"}}, map[string]vec{"Only": v})
	if len(got) != 0 {
		t.Fatalf("want none, got %+v", got)
	}
}

func TestRankFillsCardFields(t *testing.T) {
	const dim = 4
	vecs := map[string]vec{"A": axis(dim, 0, 0, 0), "Near": axis(dim, 0, 0.3, 1)}
	cands := []candidate{{rel: "Some Dir/Near.mp4", title: "Near", group: "near"}}

	got := rankRecs(cands, []anchor{{Param: "a.mp4", Title: "A"}}, vecs)
	if len(got) != 1 {
		t.Fatalf("want 1, got %+v", got)
	}

	// The card links and its thumbnail both key off these exactly as
	// /list/video does, so they must match library.Hash(basename).
	if got[0].Dir != "Some Dir" || got[0].Name != "Near.mp4" || got[0].Key != library.Hash("Near.mp4") {
		t.Fatalf("card fields wrong: %+v", got[0])
	}
}

func TestRankRootFileGetsDotDir(t *testing.T) {
	const dim = 4
	vecs := map[string]vec{"A": axis(dim, 0, 0, 0), "Near": axis(dim, 0, 0.3, 1)}
	cands := []candidate{{rel: "Near.mp4", title: "Near", group: "near"}}

	got := rankRecs(cands, []anchor{{Param: "a.mp4", Title: "A"}}, vecs)
	if len(got) != 1 || got[0].Dir != "." {
		t.Fatalf("root file should carry dir \".\": %+v", got)
	}
}

// centerVecs used to read `dim` from whichever key Go's random map order handed
// back first, so a lone stray vector could define the space and drop every good
// one. -count=50 makes the old 1-in-3 flake a certainty.
func TestCenterVecsPicksMajorityDimension(t *testing.T) {
	out := centerVecs(map[string]vec{
		"a": {1, 2, 3}, "b": {4, 5, 6}, "c": {7, 8, 9}, "stray": {1},
	})
	if _, ok := out["stray"]; ok {
		t.Error("stray 1-dim vector survived")
	}
	for _, k := range []string{"a", "b", "c"} {
		if _, ok := out[k]; !ok {
			t.Errorf("majority-dimension vector %q was dropped", k)
		}
	}
}

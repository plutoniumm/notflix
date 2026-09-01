package media

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
)

// The classical stage has to stand alone — ollama is optional and the host
// sleeps — so it is pinned against images built here rather than fixtures.

func solid(w, h int, c color.RGBA) *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(m, m.Bounds(), &image.Uniform{c}, image.Point{}, draw.Src)

	return m
}

// noisy is a deterministic stand-in for a real frame: full luma spread, real
// chroma, detail in every pixel. A fixed LCG keeps the test reproducible.
func noisy(w, h int, seed uint32) *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	s := seed | 1

	for y := range h {
		for x := range w {
			s = s*1664525 + 1013904223
			r := uint8(s >> 24)
			s = s*1664525 + 1013904223
			g := uint8(s >> 24)
			s = s*1664525 + 1013904223
			b := uint8(s >> 24)
			m.Set(x, y, color.RGBA{r, g, b, 255})
		}
	}

	return m
}

// dim is a real frame shot at night: it must survive the rejects but rank
// below a well-exposed one.
func dim(w, h int, seed uint32) *image.RGBA {
	m := noisy(w, h, seed)
	for i := 0; i < len(m.Pix); i += 4 {
		m.Pix[i] /= 3
		m.Pix[i+1] /= 3
		m.Pix[i+2] /= 3
	}

	return m
}

func TestScoreTileRejectsDegenerate(t *testing.T) {
	const w, h = 64, 36

	cases := []struct {
		name string
		img  image.Image
		want bool
	}{
		{"black", solid(w, h, color.RGBA{0, 0, 0, 255}), false},
		{"nearBlack", solid(w, h, color.RGBA{8, 8, 8, 255}), false},
		{"white", solid(w, h, color.RGBA{255, 255, 255, 255}), false},
		{"grey", solid(w, h, color.RGBA{128, 128, 128, 255}), false},
		{"flatBlue", solid(w, h, color.RGBA{20, 40, 190, 255}), false},
		{"noisy", noisy(w, h, 7), true},
		{"dim", dim(w, h, 11), true},
	}

	for _, c := range cases {
		_, ok := scoreTile(c.img)
		if ok != c.want {
			mean, sd, col, edge := stats(c.img)
			t.Errorf("%s: ok=%v want %v (mean=%.3f sd=%.3f col=%.3f edge=%.4f)",
				c.name, ok, c.want, mean, sd, col, edge)
		}
	}
}

func TestScoreTileRanksGoodFirst(t *testing.T) {
	const w, h = 64, 36

	good, _ := scoreTile(noisy(w, h, 7))
	night, _ := scoreTile(dim(w, h, 11))
	grey, _ := scoreTile(solid(w, h, color.RGBA{128, 128, 128, 255}))
	black, _ := scoreTile(solid(w, h, color.RGBA{0, 0, 0, 255}))
	white, _ := scoreTile(solid(w, h, color.RGBA{255, 255, 255, 255}))

	if !(good > night && night > grey) {
		t.Errorf("want good > dim > grey, got %.3f %.3f %.3f", good, night, grey)
	}
	if grey <= black || grey <= white {
		t.Errorf("a flat grey should still outrank pure black/white: %.3f vs %.3f/%.3f", grey, black, white)
	}
}

// sheet lays tiles out exactly as sprite.go does so rank/tileAt are exercised
// against the real geometry.
func sheet(tiles []image.Image, cols, tw, th int) (image.Image, SpriteManifest) {
	rows := (len(tiles) + cols - 1) / cols
	m := image.NewRGBA(image.Rect(0, 0, cols*tw, rows*th))

	for i, tl := range tiles {
		x, y := (i%cols)*tw, (i/cols)*th
		draw.Draw(m, image.Rect(x, y, x+tw, y+th), tl, tl.Bounds().Min, draw.Src)
	}

	return m, SpriteManifest{Interval: 10, Cols: cols, Rows: rows, Count: len(tiles), TileW: tw, TileH: th}
}

func TestTileAt(t *testing.T) {
	const tw, th = 16, 9

	want := []color.RGBA{{200, 10, 10, 255}, {10, 200, 10, 255}, {10, 10, 200, 255}, {200, 200, 10, 255}}
	tiles := make([]image.Image, len(want))
	for i, c := range want {
		tiles[i] = solid(tw, th, c)
	}

	sh, m := sheet(tiles, 2, tw, th)
	for i, c := range want {
		got := tileAt(sh, m, i).At(3, 3)
		r, g, b, _ := got.RGBA()
		if uint8(r>>8) != c.R || uint8(g>>8) != c.G || uint8(b>>8) != c.B {
			t.Errorf("tile %d = %v, want %v", i, got, c)
		}
	}
}

func TestRankPutsGoodFirstAndDegenerateLast(t *testing.T) {
	const tw, th = 64, 36

	black := solid(tw, th, color.RGBA{0, 0, 0, 255})
	white := solid(tw, th, color.RGBA{255, 255, 255, 255})
	grey := solid(tw, th, color.RGBA{128, 128, 128, 255})
	good := noisy(tw, th, 7)

	sh, m := sheet([]image.Image{black, white, grey, good}, 2, tw, th)
	got := rank(sh, m)

	if len(got) != 4 {
		t.Fatalf("rank returned %d entries, want 4", len(got))
	}
	if got[0] != 3 {
		t.Errorf("rank[0] = %d, want the noisy tile (3); full order %v", got[0], got)
	}
	for _, i := range got[1:] {
		if i == 3 {
			t.Errorf("the good tile ranked below a degenerate one: %v", got)
		}
	}
}

func TestRankTrimsHeadAndTail(t *testing.T) {
	const tw, th = 32, 18

	// 100 tiles: the head and tail 5 are the *best* frames in the video, so
	// only the trim can keep them out of the result.
	tiles := make([]image.Image, 100)
	for i := range tiles {
		if i < 5 || i >= 95 {
			tiles[i] = noisy(tw, th, uint32(i+1))
		} else {
			tiles[i] = dim(tw, th, uint32(i+1))
		}
	}

	sh, m := sheet(tiles, 10, tw, th)
	for _, i := range rank(sh, m) {
		if i < 5 || i >= 95 {
			t.Fatalf("tile %d survived the head/tail trim", i)
		}
	}
}

func TestRankSurvivesAnAllBlackVideo(t *testing.T) {
	const tw, th = 32, 18

	tiles := make([]image.Image, 12)
	for i := range tiles {
		tiles[i] = solid(tw, th, color.RGBA{uint8(i), uint8(i), uint8(i), 255})
	}

	sh, m := sheet(tiles, 4, tw, th)
	got := rank(sh, m)

	if len(got) == 0 {
		t.Fatal("rank gave up on an all-black video; it must still name a least-bad frame")
	}
}

func TestSpreadKeepsFinalistsApart(t *testing.T) {
	ranked := []int{40, 41, 42, 43, 60, 44, 80, 45}

	got := spread(ranked, 4, 2)
	if len(got) != 4 {
		t.Fatalf("spread returned %v, want 4 entries", got)
	}
	for a := range got {
		for b := a + 1; b < len(got); b++ {
			if abs(got[a]-got[b]) < 2 {
				t.Errorf("finalists %d and %d are adjacent: %v", got[a], got[b], got)
			}
		}
	}
	if got[0] != 40 {
		t.Errorf("spread dropped the classical winner: %v", got)
	}
}

func TestSpreadBackfillsWhenTooFewSeparatedTiles(t *testing.T) {
	ranked := []int{3, 4, 5}

	got := spread(ranked, 6, 2)
	if len(got) != 3 {
		t.Fatalf("spread returned %v, want all 3 candidates backfilled", got)
	}
	for _, want := range ranked {
		if !slicesHas(got, want) {
			t.Errorf("spread dropped %d: %v", want, got)
		}
	}
}

func TestPosterKeyIsBasenameOnly(t *testing.T) {
	// codec: and skip: learned this the hard way — a dir-qualified key orphans
	// the moment CleanAll or Organize moves the file.
	if a, b := posterKey("Shows/S01/ep.mp4"), posterKey("ep.mp4"); a != b {
		t.Errorf("poster key is dir-qualified: %s != %s", a, b)
	}
}

func TestParsePick(t *testing.T) {
	cases := []struct {
		name string
		in   string
		n    int
		want int
		ok   bool
	}{
		{"json", `{"pick": 3}`, 6, 2, true},
		{"jsonSpaced", "  {\"pick\":1}\n", 6, 0, true},
		{"jsonString", `{"pick": "4"}`, 6, 3, true},
		{"bareNumber", "5", 6, 4, true},
		{"prose", "Frame 2 is the best choice.", 6, 1, true},
		{"thinkBlock", "<think>1 is dark, 4 is blurry</think>\n{\"pick\": 6}", 6, 5, true},
		{"empty", "", 6, 0, false},
		{"whitespace", "   \n ", 6, 0, false},
		{"noDigits", "I cannot decide.", 6, 0, false},
		{"outOfRange", `{"pick": 9}`, 6, 0, false},
		{"zero", `{"pick": 0}`, 6, 0, false},
		{"negative", `{"pick": -2}`, 6, 0, false},
		{"truncatedJSON", `{"pick":`, 6, 0, false},
		{"wrongType", `{"pick": [1,2]}`, 6, 0, false},
		{"nullPick", `{"pick": null}`, 6, 0, false},
		{"noFinalists", "1", 0, 0, false},
		{"markdown", "```json\n{\"pick\": 2}\n```", 6, 1, true},
	}

	for _, c := range cases {
		got, ok := parsePick(c.in, c.n)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%s: parsePick(%q, %d) = %d,%v want %d,%v", c.name, c.in, c.n, got, ok, c.want, c.ok)
		}
	}
}

// A parse failure must leave the caller on the classical winner, which is what
// visionPick returning false means. Pinned separately because the fallback is
// the whole reason the vision stage is safe to ship.
func TestParsePickFallbackIsIndexZero(t *testing.T) {
	for _, bad := range []string{"", "no", `{"pick":99}`, "{}"} {
		if i, ok := parsePick(bad, 6); ok || i != 0 {
			t.Errorf("parsePick(%q) = %d,%v — must be 0,false so the caller keeps classical #1", bad, i, ok)
		}
	}
}

func TestVisionDisabledByEnv(t *testing.T) {
	t.Setenv("POSTER_VISION", "0")
	visionReset()

	if visionReady() {
		t.Error("POSTER_VISION=0 must disable the stage without touching the network")
	}
}

func TestVisionBreakerStopsCallingAfterRepeatedFailures(t *testing.T) {
	visionReset()
	defer visionReset()

	visionFails.Store(visionMaxFails)
	if visionReady() {
		t.Error("the breaker must stay open after consecutive failures")
	}
}

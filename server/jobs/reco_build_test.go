package jobs

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"notflix/server/library"
)

// themedOllama fakes an embedder with crude but deterministic semantics: each
// theme is a set of words sharing one axis, so titles of a theme really are
// neighbours and titles of different themes really are not.
func themedOllama(t *testing.T, themes [][]string) *httptest.Server {
	t.Helper()

	dim := len(themes) + 4

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		out := make([][]float32, len(body.Input))
		for i, in := range body.Input {
			v := make([]float32, dim)
			low := strings.ToLower(in)

			for j, theme := range themes {
				for _, w := range theme {
					if strings.Contains(low, w) {
						v[j] = 1

						break
					}
				}
			}

			// A little per-title jitter, so two titles of one theme are close
			// but never identical — which is what real embeddings look like.
			h := fnv.New32a()
			h.Write([]byte(in))
			v[len(themes)+int(h.Sum32()%4)] += 0.15

			out[i] = v
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"embeddings": out})
	}))
	t.Cleanup(srv.Close)

	return srv
}

// libRoot lays out a temp library and points the process at it, so buildReco()
// exercises the whole path: catalogue walk, KV history, vector store, ranking.
func libRoot(t *testing.T, files []string, kv map[string]any) *library.Library {
	t.Helper()

	dir := t.TempDir()
	t.Chdir(dir)

	root := filepath.Join(dir, "media")
	if err := os.MkdirAll(filepath.Join(dir, "cache"), 0755); err != nil {
		t.Fatal(err)
	}

	for _, f := range files {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if kv != nil {
		raw, err := json.Marshal(kv)
		if err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(dir, "cache", "kv.json"), raw, 0644); err != nil {
			t.Fatal(err)
		}
	}

	return &library.Library{Roots: []string{root}}
}

func resetReco() {
	reco.mu.Lock()
	defer reco.mu.Unlock()

	reco.items, reco.at, reco.model = nil, time.Time{}, ""
}

func TestBuildEndToEnd(t *testing.T) {
	resetReco()
	t.Cleanup(resetReco)

	files := []string{
		"Gladiator.2000.1080p.BluRay.x264.mp4",
		"Spartacus.1960.1080p.BluRay.x264.mp4",
		"Ben Hur 1959 1080p BluRay x264.mp4",
		"Cooking with Fire 2019 1080p WEBRip.mp4",
		"Baking Bread 2021 1080p WEBRip.mp4",
		"The Sopranos S02/The.Sopranos.S02E01.1080p.mp4",
		"The Sopranos S02/The.Sopranos.S02E02.1080p.mp4",
		"The Sopranos S02/The.Sopranos.S02E03.1080p.mp4",
	}

	kv := map[string]any{
		// Finished a Roman epic — the anchor.
		"watched:Spartacus.1960.1080p.BluRay.x264.mp4": map[string]any{
			"t": 7000.0, "dur": 8000.0, "at": 1000.0,
		},
		// Opened for half a minute; must not become an anchor.
		"watched:Cooking with Fire 2019 1080p WEBRip.mp4": map[string]any{
			"t": 30.0, "dur": 3600.0, "at": 2000.0,
		},
		// Dismissed from Continue Watching, stored as null. Never an anchor
		// (nothing is known about how much played) but still seen, so it must
		// not come back as a discovery.
		"watched:Ben Hur 1959 1080p BluRay x264.mp4": nil,
	}

	lib := libRoot(t, files, kv)

	srv := themedOllama(t, [][]string{
		{"gladiator", "spartacus", "ben hur"},
		{"cooking", "baking"},
		{"sopranos"},
	})
	t.Setenv("OLLAMA_HOST", srv.URL)
	t.Setenv("EMBED_MODEL", "fake-embed")

	buildReco(context.Background(), lib)

	reco.mu.Lock()
	items := reco.items
	model := reco.model
	reco.mu.Unlock()

	if model != "fake-embed" {
		t.Fatalf("model = %q", model)
	}

	if len(items) == 0 {
		t.Fatal("no recommendations from a library with an anchor and unwatched titles")
	}

	for _, it := range items {
		if strings.Contains(it.Name, "Spartacus") {
			t.Fatal("recommended the very title that was watched")
		}

		if strings.Contains(it.Name, "Ben Hur") {
			t.Fatal("recommended a title dismissed from Continue Watching")
		}

		if it.Because == "" {
			t.Fatalf("no attribution on %+v", it)
		}

		if it.Key != library.Hash(it.Name) {
			t.Fatalf("card key %q does not match the thumbnail key for %q", it.Key, it.Name)
		}
	}

	// The vector store must survive as a file the next build can reuse.
	if _, err := os.Stat(vecPath); err != nil {
		t.Fatalf("vector store not written: %v", err)
	}

	// And the second build must reuse it rather than re-embedding.
	store := loadVecs(vecPath, "fake-embed")
	if len(store.Rec) < len(files) {
		t.Fatalf("store holds %d vectors for %d files", len(store.Rec), len(files))
	}

	if n := store.fill(context.Background(), map[string]string{
		library.Hash("Gladiator.2000.1080p.BluRay.x264.mp4"): titleOf("Gladiator.2000.1080p.BluRay.x264.mp4"),
	}); n != 0 {
		t.Fatalf("reloaded store re-embedded %d titles", n)
	}
}

func TestBuildOneTitleLibrary(t *testing.T) {
	resetReco()
	t.Cleanup(resetReco)

	lib := libRoot(t, []string{"Only.Film.2020.1080p.mp4"}, nil)

	srv := themedOllama(t, [][]string{{"only"}})
	t.Setenv("OLLAMA_HOST", srv.URL)
	t.Setenv("EMBED_MODEL", "fake-embed")

	buildReco(context.Background(), lib)

	reco.mu.Lock()
	items, at := reco.items, reco.at
	reco.mu.Unlock()

	// One title and no history: nothing to anchor against, so an empty row —
	// and a completed build, not a crash.
	if len(items) != 0 {
		t.Fatalf("want none, got %+v", items)
	}

	if at.IsZero() {
		t.Fatal("build did not complete")
	}
}

func TestBuildKeepsPreviousRowWhenHostDies(t *testing.T) {
	resetReco()
	t.Cleanup(resetReco)

	// Centering measures every title against the catalogue mean, so a
	// two-title library has nothing to centre against. Real ones do.
	files := []string{
		"Gladiator.2000.1080p.mp4",
		"Spartacus.1960.1080p.mp4",
		"Cooking with Fire 2019 1080p.mp4",
		"Baking Bread 2021 1080p.mp4",
		"The Sopranos S02/The.Sopranos.S02E01.1080p.mp4",
		"The Sopranos S02/The.Sopranos.S02E02.1080p.mp4",
	}
	kv := map[string]any{
		"watched:Spartacus.1960.1080p.mp4": map[string]any{"t": 7000.0, "dur": 8000.0, "at": 1.0},
	}

	lib := libRoot(t, files, kv)

	srv := themedOllama(t, [][]string{
		{"gladiator", "spartacus"},
		{"cooking", "baking"},
		{"sopranos"},
	})
	t.Setenv("OLLAMA_HOST", srv.URL)
	t.Setenv("EMBED_MODEL", "fake-embed")

	buildReco(context.Background(), lib)

	reco.mu.Lock()
	before := len(reco.items)
	reco.mu.Unlock()

	if before == 0 {
		t.Fatal("no baseline row to preserve")
	}

	// Host goes to sleep and the cached vectors go with it.
	if err := os.Remove(vecPath); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OLLAMA_HOST", "http://127.0.0.1:1")

	buildReco(context.Background(), lib)

	reco.mu.Lock()
	after := len(reco.items)
	reco.mu.Unlock()

	if after != before {
		t.Fatalf("an unreachable host wiped the row: %d → %d", before, after)
	}
}

func TestBuildIgnoresHiddenDirs(t *testing.T) {
	resetReco()
	t.Cleanup(resetReco)

	files := []string{
		"Spartacus.1960.1080p.mp4",
		"Ben Hur 1959 1080p.mp4",
		"Cooking with Fire 2019 1080p.mp4",
		"Baking Bread 2021 1080p.mp4",
		"Secret/Gladiator.2000.1080p.mp4",
	}
	kv := map[string]any{
		"watched:Spartacus.1960.1080p.mp4": map[string]any{"t": 7000.0, "dur": 8000.0, "at": 1.0},
		"hidden:Secret":                    true,
	}

	lib := libRoot(t, files, kv)

	srv := themedOllama(t, [][]string{
		{"gladiator", "spartacus", "ben hur"},
		{"cooking", "baking"},
	})
	t.Setenv("OLLAMA_HOST", srv.URL)
	t.Setenv("EMBED_MODEL", "fake-embed")

	buildReco(context.Background(), lib)

	reco.mu.Lock()
	items := reco.items
	reco.mu.Unlock()

	if len(items) == 0 {
		t.Fatal("nothing recommended, so the hidden-folder check proves nothing")
	}

	for _, it := range items {
		if it.Dir == "Secret" {
			t.Fatalf("recommended from a hidden folder: %+v", it)
		}
	}
}

func TestBuildWithoutAnchorsRecommendsNothing(t *testing.T) {
	resetReco()
	t.Cleanup(resetReco)

	// Plenty of library, no finished title: discovery has nothing to reason
	// from, and guessing would be worse than an absent row.
	lib := libRoot(t, []string{"A.2020.1080p.mp4", "B.2020.1080p.mp4", "C.2020.1080p.mp4"}, map[string]any{
		"watched:A.2020.1080p.mp4": map[string]any{"t": 12.0, "dur": 6000.0, "at": 1.0},
	})

	srv := themedOllama(t, [][]string{{"a", "b", "c"}})
	t.Setenv("OLLAMA_HOST", srv.URL)
	t.Setenv("EMBED_MODEL", "fake-embed")

	buildReco(context.Background(), lib)

	reco.mu.Lock()
	items := reco.items
	reco.mu.Unlock()

	if len(items) != 0 {
		t.Fatalf("want none, got %+v", items)
	}
}

func TestBuildDoesNotBlowUpOnRaggedStore(t *testing.T) {
	resetReco()
	t.Cleanup(resetReco)

	lib := libRoot(t, []string{"A.2020.1080p.mp4", "B.2020.1080p.mp4"}, map[string]any{
		"watched:A.2020.1080p.mp4": map[string]any{"t": 7000.0, "dur": 8000.0, "at": 1.0},
	})

	// A store left behind by a different model, at a different dimension.
	stale := newVecs("fake-embed")
	stale.put(library.Hash("B.2020.1080p.mp4"), titleOf("B.2020.1080p.mp4"), vec{1, 2, 3})
	if err := stale.save(vecPath); err != nil {
		t.Fatal(err)
	}

	srv := themedOllama(t, [][]string{{"a", "b"}})
	t.Setenv("OLLAMA_HOST", srv.URL)
	t.Setenv("EMBED_MODEL", "fake-embed")

	buildReco(context.Background(), lib)

	reco.mu.Lock()
	items := reco.items
	reco.mu.Unlock()

	for _, it := range items {
		if math.IsNaN(it.Score) {
			t.Fatalf("NaN score on %+v", it)
		}
	}
}

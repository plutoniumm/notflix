package jobs

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"notflix/server/library"
)

func tmpVecs(t *testing.T, model string, n, dim int) (*vecs, string) {
	t.Helper()

	s := newVecs(model)
	for i := range n {
		v := make(vec, dim)
		for j := range v {
			v[j] = float32(i*dim + j)
		}
		s.put(library.Hash(string(rune('a'+i))+".mp4"), "title "+string(rune('a'+i)), v)
	}

	path := filepath.Join(t.TempDir(), "embed.bin")
	if err := s.save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	return s, path
}

func TestVecsRoundTrip(t *testing.T) {
	want, path := tmpVecs(t, "model-a", 5, 7)

	got := loadVecs(path, "model-a")
	if got.Dim != 7 || len(got.Rec) != 5 {
		t.Fatalf("dim %d, %d records", got.Dim, len(got.Rec))
	}

	for k, r := range want.Rec {
		v, ok := got.get(k, r.Text)
		if !ok {
			t.Fatalf("lost %s", k)
		}

		if !near(cosine(unit(v), unit(r.V)), 1) {
			t.Fatalf("%s came back different", k)
		}
	}
}

func TestVecsModelChangeDropsEverything(t *testing.T) {
	// Two models' vectors live in different spaces; mixing them is silent
	// nonsense, so a model change must throw the cache away.
	_, path := tmpVecs(t, "nomic-embed-text:latest", 5, 7)

	got := loadVecs(path, "kalm-embedding-gemma3:Q8_0")
	if len(got.Rec) != 0 || got.Dim != 0 {
		t.Fatalf("stale vectors survived a model change: %d records, dim %d", len(got.Rec), got.Dim)
	}

	if got.Model != "kalm-embedding-gemma3:Q8_0" {
		t.Fatalf("fresh store carries the wrong model: %q", got.Model)
	}
}

func TestVecsRejectsGarbage(t *testing.T) {
	dir := t.TempDir()

	cases := map[string][]byte{
		"missing":   nil,
		"empty":     {},
		"bad magic": []byte("XXXX\x00\x00\x00\x00"),
		"header only": func() []byte {
			b := append([]byte{'N', 'F', 'E', '1'}, make([]byte, 4)...)

			return b
		}(),
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			if body != nil {
				if err := os.WriteFile(path, body, 0644); err != nil {
					t.Fatal(err)
				}
			}

			if got := loadVecs(path, "m"); len(got.Rec) != 0 {
				t.Fatalf("accepted garbage: %d records", len(got.Rec))
			}
		})
	}
}

func TestVecsRejectsTruncatedTail(t *testing.T) {
	_, path := tmpVecs(t, "m", 5, 7)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	cut := filepath.Join(t.TempDir(), "cut.bin")
	if err := os.WriteFile(cut, raw[:len(raw)-9], 0644); err != nil {
		t.Fatal(err)
	}

	if got := loadVecs(cut, "m"); len(got.Rec) != 0 {
		t.Fatalf("a truncated file yielded %d records", len(got.Rec))
	}
}

func TestVecsRejectsImplausibleDim(t *testing.T) {
	b := append([]byte{}, vecMagic[:]...)
	b = binary.LittleEndian.AppendUint32(b, uint32(vecMaxDim+1))
	b = binary.LittleEndian.AppendUint32(b, 1)
	b = binary.LittleEndian.AppendUint16(b, 1)
	b = append(b, 'm')

	path := filepath.Join(t.TempDir(), "big.bin")
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}

	if got := loadVecs(path, "m"); len(got.Rec) != 0 || got.Dim != 0 {
		t.Fatalf("accepted dim %d", got.Dim)
	}
}

func TestVecsGetRejectsStaleTextAndWrongDim(t *testing.T) {
	s := newVecs("m")
	s.put("k", "old title", vec{1, 2, 3})

	if _, ok := s.get("k", "new title"); ok {
		t.Fatal("a retitled entry served its old vector")
	}

	// A vector of a different length can only be from another space.
	s.Rec["k"] = embRec{Text: "old title", V: vec{1, 2}}
	if _, ok := s.get("k", "old title"); ok {
		t.Fatal("served a vector of the wrong dimension")
	}
}

func TestVecsPutIgnoresRaggedVectors(t *testing.T) {
	s := newVecs("m")
	s.put("a", "a", vec{1, 2, 3})
	s.put("b", "b", vec{1, 2})

	if _, ok := s.Rec["b"]; ok {
		t.Fatal("stored a vector of the wrong dimension")
	}
}

func TestEmbedModelFromEnv(t *testing.T) {
	if got := embedModel(); got != embedDefault {
		t.Fatalf("default = %q", got)
	}

	t.Setenv("EMBED_MODEL", "kalm:Q8_0")
	if got := embedModel(); got != "kalm:Q8_0" {
		t.Fatalf("env override = %q", got)
	}
}

// fakeOllama serves a fixed-dimension embedding for every input.
func fakeOllama(t *testing.T, dim int) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		out := make([][]float32, len(body.Input))
		for i := range out {
			v := make([]float32, dim)
			v[i%dim] = 1
			out[i] = v
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"embeddings": out})
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestFillEmbedsMissingOnly(t *testing.T) {
	srv := fakeOllama(t, 6)
	t.Setenv("OLLAMA_HOST", srv.URL)

	s := newVecs("m")
	want := map[string]string{"a": "alpha", "b": "beta", "c": ""}

	if n := s.fill(context.Background(), want); n != 2 {
		t.Fatalf("embedded %d, want 2 (the empty title is skipped)", n)
	}

	if n := s.fill(context.Background(), want); n != 0 {
		t.Fatalf("re-embedded %d already-cached titles", n)
	}
}

func TestFillDropsCacheWhenDimensionChanges(t *testing.T) {
	// The whole point of the guard: a model swap must never leave two
	// dimensionalities in one store where a cosine could reach them.
	srv := fakeOllama(t, 6)
	t.Setenv("OLLAMA_HOST", srv.URL)

	s := newVecs("m")
	s.fill(context.Background(), map[string]string{"a": "alpha"})

	wide := fakeOllama(t, 12)
	t.Setenv("OLLAMA_HOST", wide.URL)

	s.fill(context.Background(), map[string]string{"b": "beta"})

	if len(s.Rec) != 0 || s.Dim != 0 {
		t.Fatalf("mixed dimensions survived: %d records at dim %d", len(s.Rec), s.Dim)
	}
}

func TestFillSurvivesDeadHost(t *testing.T) {
	// The tailnet box sleeps. That has to cost an empty row, not an error.
	t.Setenv("OLLAMA_HOST", "http://127.0.0.1:1")

	s := newVecs("m")
	if n := s.fill(context.Background(), map[string]string{"a": "alpha"}); n != 0 {
		t.Fatalf("embedded %d against a dead host", n)
	}
}

func TestFillStopsOnCancelledContext(t *testing.T) {
	srv := fakeOllama(t, 6)
	t.Setenv("OLLAMA_HOST", srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := newVecs("m")
	if n := s.fill(ctx, map[string]string{"a": "alpha"}); n != 0 {
		t.Fatalf("embedded %d after cancellation", n)
	}
}

func TestRecoHandlerNeverErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Setenv("OLLAMA_HOST", "http://127.0.0.1:1")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/reco", nil)

	RecoHandler(c, &library.Library{})

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}

	if body := w.Body.String(); !strings.Contains(body, `"items":[]`) {
		t.Fatalf("want an empty item list, got %s", body)
	}
}

func TestRecoHandlerWithNilLibrary(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/reco", nil)

	RecoHandler(c, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
}

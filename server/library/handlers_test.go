package library

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

// libTemp chdirs into a fresh temp dir with cache/kv.json holding `kv`, so
// DirRecency/VideoList read isolated state. Returns the lib root.
func libTemp(t *testing.T, kv map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cache"), 0755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(kv)
	if err := os.WriteFile(filepath.Join(dir, "cache", "kv.json"), b, 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	return dir
}

func watched(at float64) map[string]any {
	return map[string]any{"t": 120.0, "at": at}
}

func TestDirRecency_MaxPerDirFromWatchedKeys(t *testing.T) {
	libTemp(t, map[string]any{
		"watched:Alpha/e1.mp4": watched(5000),
		"watched:Alpha/e2.mp4": watched(8000), // newer ep wins for the dir
		"watched:Beta/e1.mp4":  watched(9000),
		"watched:solo.mp4":     watched(7000), // top-level → "."
		"hidden:Junk":          true,          // ignored
		"misc":                 "noise",       // ignored
	})

	got := DirRecency()
	want := map[string]float64{"Alpha": 8000, "Beta": 9000, ".": 7000}
	if len(got) != len(want) {
		t.Fatalf("DirRecency = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("DirRecency[%q] = %v, want %v", k, got[k], v)
		}
	}
}

func TestVideoList_OrdersDirsByRecency(t *testing.T) {
	root := filepath.Join(libTemp(t, map[string]any{
		"watched:Beta/x.mp4":  watched(9000), // most recent
		"watched:Alpha/x.mp4": watched(5000), // older
		// Charlie + "." (top-level) never watched → alphabetical tail
	}), "media")

	for _, d := range []string{"Alpha", "Beta", "Charlie"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, d, "x.mp4"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "movie.mp4"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	lib := &Library{Roots: []string{root}}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/list/video", func(c *gin.Context) { VideoList(c, lib) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/list/video", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}

	var groups []struct {
		Dir   string              `json:"dir"`
		Files []map[string]string `json:"files"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &groups); err != nil {
		t.Fatalf("response is not an ordered array: %v\n%s", err, w.Body.String())
	}

	var order []string
	for _, g := range groups {
		if len(g.Files) == 0 {
			t.Errorf("empty group leaked into response: %q", g.Dir)
		}
		order = append(order, g.Dir)
	}

	// Beta(9000) > Alpha(5000) > unwatched alphabetical: "." then "Charlie".
	want := []string{"Beta", "Alpha", ".", "Charlie"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

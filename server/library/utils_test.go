package library

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func touch(t *testing.T, path string) string {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	return path
}

func exists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

func delReq(t *testing.T, lib *Library, url string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.DELETE("/video/*filename", func(c *gin.Context) { VideoDelete(c, lib) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("DELETE", url, nil))

	return w
}

func TestVideoDelete_Traversal(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "media")
	touch(t, filepath.Join(root, "keep.mp4"))

	for _, url := range []string{"/video/../victim.txt", "/video/%2e%2e%2fvictim.txt", "/video/..%2Fvictim.txt"} {
		victim := touch(t, filepath.Join(tmp, "victim.txt"))
		lib := &Library{Roots: []string{root}}

		w := delReq(t, lib, url)
		if !exists(victim) {
			t.Fatalf("%s deleted a file outside the roots", url)
		}
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404 (%s)", url, w.Code, w.Body.String())
		}
	}
}

func TestVideoDelete_OnlyResolvedRoot(t *testing.T) {
	tmp := t.TempDir()
	one := filepath.Join(tmp, "one")
	two := filepath.Join(tmp, "two")

	hit := touch(t, filepath.Join(one, "Show", "01.mp4"))
	sub := touch(t, filepath.Join(one, "Show", "01.vtt"))
	lang := touch(t, filepath.Join(one, "Show", "01.en.vtt"))
	other := touch(t, filepath.Join(two, "Show", "01.mp4"))

	lib := &Library{Roots: []string{one, two}}
	w := delReq(t, lib, "/video/Show/01.mp4")

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 (%s)", w.Code, w.Body.String())
	}
	if exists(hit) || exists(sub) || exists(lang) {
		t.Error("resolved video or its sidecars survived")
	}
	if !exists(other) {
		t.Error("deleted the same rel path out of a second root")
	}
}

func TestVideoDelete_Missing(t *testing.T) {
	lib := &Library{Roots: []string{t.TempDir()}}

	if w := delReq(t, lib, "/video/nope.mp4"); w.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", w.Code)
	}
}

func TestFindFile_DotsInName(t *testing.T) {
	root := t.TempDir()

	for _, name := range []string{"Movie..2020.mp4", "S01E01..mkv"} {
		want := touch(t, filepath.Join(root, name))
		if got := FindFile(name, []string{root}); got != want {
			t.Errorf("FindFile(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestFindFile_Contained(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "Aomine")
	touch(t, filepath.Join(root, "ok.mp4"))
	touch(t, filepath.Join(tmp, "AomineEvil", "x.mp4"))
	touch(t, filepath.Join(tmp, "secret.txt"))

	for _, rel := range []string{"../secret.txt", "/../secret.txt", "a/../../secret.txt", "../AomineEvil/x.mp4", ""} {
		if got := FindFile(rel, []string{root}); got != "" {
			t.Errorf("FindFile(%q) = %q, want \"\"", rel, got)
		}
	}
}

func TestRename_ConflictKeepsBoth(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "A.mp4")
	dst := filepath.Join(root, "B.mp4")

	if err := os.WriteFile(src, []byte("aaa"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("bbb"), 0644); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	lib := &Library{Roots: []string{root}}
	r.POST("/api/rename", func(c *gin.Context) { Rename(c, lib) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/rename", strings.NewReader(`{"path":"A.mp4","name":"B.mp4"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409 (%s)", w.Code, w.Body.String())
	}
	if !exists(src) {
		t.Error("source vanished on a conflicting rename")
	}

	b, _ := os.ReadFile(dst)
	if string(b) != "bbb" {
		t.Errorf("destination overwritten: %q", b)
	}
}

func TestRange(t *testing.T) {
	const size = 1000

	cases := []struct {
		hdr   string
		start int64
		end   int64
		clen  string
	}{
		{"bytes=0-99", 0, 99, "100"},
		{"bytes=0-0", 0, 0, "1"},
		{"bytes=500-", 500, 999, "500"},
		{"bytes", 0, 999, "1000"},               // panicked: index out of range
		{"bytes=0", 0, 999, "1000"},             // panicked: index out of range
		{"bytes=-500", 500, 999, "500"},         // suffix range = LAST 500 bytes
		{"bytes=-2000", 0, 999, "1000"},         // suffix longer than the file
		{"bytes=0-99999999999", 0, 999, "1000"}, // clamped to size-1
		{"bytes=1000-", 0, 999, "1000"},         // unsatisfiable → whole file
		{"bytes=99-9", 0, 999, "1000"},          // inverted → whole file
		{"bytes=abc-def", 0, 999, "1000"},
		{"", 0, 999, "1000"},
		{"items=0-99", 0, 999, "1000"},
		{"bytes=0-99, 200-299", 0, 99, "100"}, // first spec only
	}

	for _, c := range cases {
		start, end, clen := Range(c.hdr, size)
		if start != c.start || end != c.end || clen != c.clen {
			t.Errorf("Range(%q, %d) = %d, %d, %q; want %d, %d, %q",
				c.hdr, int64(size), start, end, clen, c.start, c.end, c.clen)
		}
	}
}

func TestParseRange(t *testing.T) {
	cases := []struct {
		hdr   string
		size  int64
		start int64
		end   int64
		ok    bool
	}{
		{"bytes=0-99", 1000, 0, 99, true},
		{"bytes=-500", 1000, 500, 999, true},
		{"bytes=500-", 1000, 500, 999, true},
		{"bytes=0-99999999999", 1000, 0, 999, true},
		{"bytes=1000-", 1000, 0, 0, false}, // start past EOF → 416
		{"bytes=-0", 1000, 0, 0, false},
		{"bytes", 1000, 0, 0, false},
		{"bytes=0", 1000, 0, 0, false},
		{"bytes=-", 1000, 0, 0, false},
		{"bytes=0-99", 0, 0, 0, false},
	}

	for _, c := range cases {
		start, end, ok := ParseRange(c.hdr, c.size)
		if start != c.start || end != c.end || ok != c.ok {
			t.Errorf("ParseRange(%q, %d) = %d, %d, %v; want %d, %d, %v",
				c.hdr, c.size, start, end, ok, c.start, c.end, c.ok)
		}
	}
}

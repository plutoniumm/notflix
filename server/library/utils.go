package library

import (
	"fmt"
	"hash/fnv"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	astiffprobe "github.com/asticode/go-astiffprobe"
)

var Prober = astiffprobe.New(astiffprobe.Configuration{BinaryPath: "ffprobe"})

// The trailing separator on the prefix test is load-bearing: without it
// "/Volumes/AomineEvil" passes a "/Volumes/Aomine" containment check.
// Rejecting the ".." path element (not the substring) keeps "Movie..2020.mp4".
func resolve(root, rel string) (string, bool) {
	absR, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}

	abs, err := filepath.Abs(filepath.Join(absR, rel))
	if err != nil {
		return "", false
	}

	if abs != absR && !strings.HasPrefix(abs, absR+string(filepath.Separator)) {
		return "", false
	}

	return abs, true
}

func FindFile(rel string, roots []string) string {
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return ""
	}

	for _, root := range roots {
		abs, ok := resolve(root, rel)
		if !ok {
			continue
		}

		if _, err := os.Stat(abs); err == nil {
			return abs
		}
	}

	return ""
}

func FindVid(file string, roots []string) (string, bool) {
	path := FindFile(file, roots)

	return path, path != ""
}

func Error(message string, c *gin.Context, statusCode int) {
	fmt.Println("Error:", message)
	c.JSON(statusCode, gin.H{"error": message})
}

func Hash(name string) string {
	h := fnv.New32a()
	h.Write([]byte(name))
	return fmt.Sprintf("%07d", h.Sum32()%1e7)
}

// EnsureDir returns false when the dir isn't usable (read-only parent,
// unmounted volume) — non-fatal, boot must survive that.
func EnsureDir(dir string) bool {
	if info, err := os.Stat(dir); err == nil {
		return info.IsDir()
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("warn: cannot create directory %s: %v", dir, err)
		return false
	}
	return true
}

// Sidecars are optional, so a missing file is normal, not a failure.
func DelFile(path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("Failed to delete file %s: %v", path, err)
	}
}

// ParseRange returns ok=false for a malformed or unsatisfiable range —
// the caller's cue to answer 416.
func ParseRange(hdr string, size int64) (start, end int64, ok bool) {
	spec, found := strings.CutPrefix(strings.ToLower(strings.TrimSpace(hdr)), "bytes=")
	if !found || size <= 0 {
		return 0, 0, false
	}

	if i := strings.IndexByte(spec, ','); i >= 0 {
		spec = spec[:i]
	}

	lo, hi, found := strings.Cut(strings.TrimSpace(spec), "-")
	if !found {
		return 0, 0, false
	}
	lo, hi = strings.TrimSpace(lo), strings.TrimSpace(hi)

	// "bytes=-N" is a suffix range: the LAST N bytes, not a start offset.
	if lo == "" {
		n, err := strconv.ParseInt(hi, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}

		if n > size {
			n = size
		}

		return size - n, size - 1, true
	}

	start, err := strconv.ParseInt(lo, 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false
	}

	end = size - 1
	if hi != "" {
		last, err := strconv.ParseInt(hi, 10, 64)
		if err != nil || last < start {
			return 0, 0, false
		}

		if last < end {
			end = last
		}
	}

	return start, end, true
}

// Range never fails: a bad header degrades to the whole file.
func Range(hdr string, size int64) (start int64, end int64, clen string) {
	start, end, ok := ParseRange(hdr, size)
	if !ok {
		start, end = 0, size-1
	}

	if end < start {
		end = start
	}

	return start, end, strconv.FormatInt(end-start+1, 10)
}

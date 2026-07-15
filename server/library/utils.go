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

func FindFile(rel string, roots []string) string {
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" || strings.Contains(rel, "..") {
		return ""
	}

	for _, root := range roots {
		absR, err := filepath.Abs(root)
		if err != nil {
			continue
		}

		candidate := filepath.Join(root, rel)
		abs, err := filepath.Abs(candidate)
		if err != nil || !strings.HasPrefix(abs, absR) {
			continue
		}

		if _, err := os.Stat(candidate); err == nil {
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
	c.String(statusCode, message)
}

func Hash(name string) string {
	h := fnv.New32a()
	h.Write([]byte(name))
	return fmt.Sprintf("%07d", h.Sum32()%1e7)
}

// EnsureDir makes sure dir exists. Returns true when the directory is
// available afterwards (whether pre-existing or freshly created), false
// when it couldn't be created (e.g. parent is read-only, or a removable
// volume's mountpoint isn't actually mounted). Non-fatal — boot must
// survive missing drives so the server still answers requests against
// whatever roots are reachable.
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

func DelFile(path string) {
	if err := os.Remove(path); err != nil {
		log.Printf("Failed to delete file %s: %v", path, err)
	}
}

func Range(hdr string, size int64) (start int64, end int64, clen string) {
	parts := strings.Split(hdr, "=")
	lr := strings.Split(parts[1], "-")

	start, _ = strconv.ParseInt(lr[0], 10, 64)
	if lr[1] != "" {
		end, _ = strconv.ParseInt(lr[1], 10, 64)
	} else {
		end = size - 1
	}
	clen = strconv.FormatInt(end-start+1, 10)

	return start, end, clen
}

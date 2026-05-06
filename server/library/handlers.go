package library

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/gin-gonic/gin"
)

type FileEntry struct {
	Name    string `json:"name"`
	Root    string `json:"root"`
	Corrupt bool   `json:"corrupt,omitempty"`
}

type DiskStat struct {
	Root  string `json:"root"`
	Path  string `json:"path"`
	Free  uint64 `json:"free"`
	Total uint64 `json:"total"`
}

type DirSize struct {
	Dir   string `json:"dir"`
	Bytes int64  `json:"bytes"`
	Root  string `json:"root"`
}

func buildList(dir string) map[string][]map[string]string {
	res := make(map[string][]map[string]string)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return res
	}

	procDir := func(sub string) []map[string]string {
		files := GetVids(sub)
		var out []map[string]string
		for _, name := range files {
			out = append(out, map[string]string{"name": name, "key": Hash(name)})
		}

		return out
	}

	res["."] = procDir(dir)

	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			res[e.Name()] = procDir(filepath.Join(dir, e.Name()))
		}
	}

	return res
}

func VideoList(c *gin.Context, lib *Library) {
	hidden := lib.HiddenDirs()
	out := make(map[string][]map[string]string)

	for _, d := range lib.Roots {
		for k, v := range buildList(d) {
			if hidden[k] {
				continue
			}
			out[k] = append(out[k], v...)
		}
	}

	c.JSON(http.StatusOK, out)
}

func ManageList(c *gin.Context, lib *Library) {
	out := make(map[string][]FileEntry)

	listVids := func(dir, root string) []FileEntry {
		var files []FileEntry
		entries, _ := os.ReadDir(dir)

		for _, f := range entries {
			if f.IsDir() || strings.HasPrefix(f.Name(), ".") {
				continue
			}

			ext := strings.ToLower(filepath.Ext(f.Name()))
			if ext == ".mp4" || ext == ".mkv" || ext == ".mov" {
				abs := filepath.Join(dir, f.Name())
				files = append(files, FileEntry{Name: f.Name(), Root: root, Corrupt: IsCorrupt(abs)})
			}
		}

		return files
	}

	for _, d := range lib.Roots {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}

		rootBase := filepath.Base(d)

		if files := listVids(d, rootBase); len(files) > 0 {
			out["."] = append(out["."], files...)
		}

		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				if files := listVids(filepath.Join(d, e.Name()), rootBase); len(files) > 0 {
					out[e.Name()] = append(out[e.Name()], files...)
				}
			}
		}
	}

	c.JSON(http.StatusOK, out)
}

func DiskInfo(c *gin.Context, lib *Library) {
	var out []DiskStat

	for _, root := range lib.Roots {
		var stat syscall.Statfs_t
		if err := syscall.Statfs(root, &stat); err != nil {
			continue
		}

		out = append(out, DiskStat{
			Root:  filepath.Base(root),
			Path:  root,
			Free:  stat.Bavail * uint64(stat.Bsize),
			Total: stat.Blocks * uint64(stat.Bsize),
		})
	}

	c.JSON(http.StatusOK, out)
}

func DirSizes(c *gin.Context, lib *Library) {
	var out []DirSize

	for _, root := range lib.Roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}

		rootBase := filepath.Base(root)
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}

			var total int64
			filepath.WalkDir(filepath.Join(root, e.Name()), func(_ string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}

				if info, err := d.Info(); err == nil {
					total += info.Size()
				}

				return nil
			})

			if total > 0 {
				out = append(out, DirSize{Dir: e.Name(), Bytes: total, Root: rootBase})
			}
		}
	}

	c.JSON(http.StatusOK, out)
}

func Rename(c *gin.Context, lib *Library) {
	var body struct {
		Path    string `json:"path"`
		NewName string `json:"name"`
	}

	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	if body.NewName == "" || strings.ContainsAny(body.NewName, "/\\") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid name"})
		return
	}

	abs := lib.FindFile(body.Path)
	if abs == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	dst := filepath.Join(filepath.Dir(abs), body.NewName)
	if err := os.Rename(abs, dst); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ext := strings.ToLower(filepath.Ext(abs))
	if ext == ".mp4" {
		base := abs[:len(abs)-len(ext)]
		nbase := dst[:len(dst)-len(filepath.Ext(dst))]

		for _, suf := range []string{".vtt", ".srt"} {
			old := base + suf
			if _, err := os.Stat(old); err == nil {
				os.Rename(old, nbase+suf)
			}
		}

		matches, _ := filepath.Glob(base + ".*.vtt")
		for _, old := range matches {
			suffix := strings.TrimPrefix(old, base)
			os.Rename(old, nbase+suffix)
		}
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func DirDelete(c *gin.Context, lib *Library) {
	path := c.Query("path")
	if path == "" || strings.ContainsAny(path, "/\\") || path == "." {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path"})
		return
	}

	abs := lib.FindFile(path)
	if abs == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	if err := os.RemoveAll(abs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func VideoDelete(c *gin.Context, lib *Library) {
	fname := c.Param("filename")
	rel := strings.TrimPrefix(fname, "/")
	base := strings.TrimSuffix(rel, filepath.Ext(rel))

	for _, root := range lib.Roots {
		DelFile(filepath.Join(root, rel))
		for _, suf := range []string{".vtt", ".srt"} {
			DelFile(filepath.Join(root, base+suf))
		}

		matches, _ := filepath.Glob(filepath.Join(root, base+".*.vtt"))
		for _, m := range matches {
			DelFile(m)
		}
	}

	c.String(http.StatusOK, "true")
}

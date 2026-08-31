package library

import (
	"os"
	"path/filepath"
	"strings"
)

type Library struct {
	Roots []string
	// Caps holds a UI-only capacity cap (in bytes) keyed by absolute root
	// path. DiskInfo reports min(actualTotal, cap) for the total and
	// max(0, cap - used) for free. Writes aren't enforced — the underlying
	// filesystem keeps whatever real capacity it has; this only shapes what
	// the UI shows so the user can budget against a soft quota.
	Caps map[string]uint64
}

type VideoRef struct {
	Root string
	Rel  string
}

func (v VideoRef) Hash() string {
	return Hash(v.Rel)
}

func (v VideoRef) Abs() string {
	return filepath.Join(v.Root, v.Rel)
}

func (l *Library) FindFile(rel string) string {
	return FindFile(rel, l.Roots)
}

func (l *Library) FindVid(file string) (string, bool) {
	return FindVid(file, l.Roots)
}

func (l *Library) FindRoot(name string) (root, abs string, ok bool) {
	abs = FindFile(name, l.Roots)
	if abs == "" {
		return "", "", false
	}

	rel := strings.TrimPrefix(name, "/")
	for _, r := range l.Roots {
		if strings.HasPrefix(abs, r) || strings.HasPrefix(abs, filepath.Join(r, rel)) {
			return r, abs, true
		}
	}

	return "", "", false
}

func (l *Library) HiddenDirs() map[string]bool {
	return HiddenDirs()
}

func isVid(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))

	return ext == ".mp4" || ext == ".mkv" || ext == ".mov"
}

// AllVideos walks each root to full depth. It used to stop at two levels,
// so anything nested deeper was invisible to LiveHashes — and CleanHLSCache
// deletes the cache of every hash it can't see, mid-stream.
func (l *Library) AllVideos() []VideoRef {
	var out []VideoRef

	for _, root := range l.Roots {
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}

			if strings.HasPrefix(d.Name(), ".") && path != root {
				if d.IsDir() {
					return filepath.SkipDir
				}

				return nil
			}

			if d.IsDir() || !isVid(d.Name()) {
				return nil
			}

			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}

			out = append(out, VideoRef{Root: root, Rel: filepath.ToSlash(rel)})

			return nil
		})
	}

	return out
}

func (l *Library) LiveHashes() map[string]bool {
	live := make(map[string]bool)
	for _, v := range l.AllVideos() {
		live[v.Hash()] = true
	}

	return live
}

package library

import (
	"os"
	"path/filepath"
	"strings"
)

type Library struct {
	Roots []string
	// Caps is a UI-only capacity cap per root; writes aren't enforced.
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

	for _, r := range l.Roots {
		absR, err := filepath.Abs(r)
		if err != nil {
			continue
		}

		// The trailing separator is load-bearing: a bare prefix test reads
		// /Volumes/AomineEvil as living inside root /Volumes/Aomine.
		if abs == absR || strings.HasPrefix(abs, absR+string(filepath.Separator)) {
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

// AllVideos must walk full depth: CleanHLSCache deletes the cache of every
// hash LiveHashes can't see, mid-stream.
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

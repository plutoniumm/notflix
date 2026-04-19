package library

import (
	"os"
	"path/filepath"
	"strings"
)

type Library struct {
	Roots []string
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

func (l *Library) HiddenDirs() map[string]bool {
	return HiddenDirs()
}

func isVid(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))

	return ext == ".mp4" || ext == ".mkv" || ext == ".mov"
}

func (l *Library) AllVideos() []VideoRef {
	var out []VideoRef

	for _, root := range l.Roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}

		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}

			if !e.IsDir() {
				if isVid(e.Name()) {
					out = append(out, VideoRef{Root: root, Rel: e.Name()})
				}
				continue
			}

			sub := filepath.Join(root, e.Name())
			subEntries, err := os.ReadDir(sub)
			if err != nil {
				continue
			}

			for _, se := range subEntries {
				if se.IsDir() || strings.HasPrefix(se.Name(), ".") {
					continue
				}

				if isVid(se.Name()) {
					out = append(out, VideoRef{Root: root, Rel: e.Name() + "/" + se.Name()})
				}
			}
		}
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

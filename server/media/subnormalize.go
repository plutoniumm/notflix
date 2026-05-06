package media

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/martinlindhe/subtitles"

	"notflix/server/library"
)

// NormalizeSubs walks every root and ensures all .srt and .vtt files are
// served as WebVTT:
//   - .srt → parsed and rewritten as .vtt, original removed
//   - .vtt without a WEBVTT header → reparsed as SRT and rewritten in place
//   - .vtt with a WEBVTT header → left alone (browsers are forgiving)
func NormalizeSubs(lib *library.Library) {
	var converted, fixed, failed atomic.Int64

	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup

	for _, root := range lib.Roots {
		if _, err := os.Stat(root); err != nil {
			continue
		}
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if ext != ".srt" && ext != ".vtt" {
				return nil
			}

			wg.Add(1)
			sem <- struct{}{}
			go func(p, ext string) {
				defer wg.Done()
				defer func() { <-sem }()

				switch ext {
				case ".srt":
					if normalizeSRT(p) {
						converted.Add(1)
					} else {
						failed.Add(1)
					}
				case ".vtt":
					if needsVTTFix(p) {
						if normalizeVTT(p) {
							fixed.Add(1)
						} else {
							failed.Add(1)
						}
					}
				}
			}(path, ext)
			return nil
		})
	}

	wg.Wait()
	if c := converted.Load(); c > 0 {
		log.Printf("[subs] normalized %d .srt → .vtt", c)
	}
	if f := fixed.Load(); f > 0 {
		log.Printf("[subs] rewrote %d malformed .vtt", f)
	}
	if f := failed.Load(); f > 0 {
		log.Printf("[subs] %d files failed to parse", f)
	}
}

func needsVTTFix(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	head := make([]byte, 32)
	n, _ := f.Read(head)
	head = head[:n]
	head = bytes.TrimPrefix(head, []byte{0xEF, 0xBB, 0xBF}) // strip BOM
	head = bytes.TrimLeft(head, " \t\r\n")
	return !bytes.HasPrefix(head, []byte("WEBVTT"))
}

func normalizeSRT(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[subs] read failed %s: %v", filepath.Base(path), err)
		return false
	}
	parsed, err := subtitles.NewFromSRT(string(data))
	if err != nil {
		log.Printf("[subs] srt parse failed %s: %v", filepath.Base(path), err)
		return false
	}
	out := strings.TrimSuffix(path, filepath.Ext(path)) + ".vtt"
	if err := os.WriteFile(out, []byte(parsed.AsVTT()), 0644); err != nil {
		log.Printf("[subs] write failed %s: %v", filepath.Base(out), err)
		return false
	}
	os.Remove(path)
	return true
}

func normalizeVTT(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[subs] read failed %s: %v", filepath.Base(path), err)
		return false
	}
	parsed, err := subtitles.NewFromSRT(string(data))
	if err != nil {
		log.Printf("[subs] vtt-as-srt parse failed %s: %v", filepath.Base(path), err)
		return false
	}
	if err := os.WriteFile(path, []byte(parsed.AsVTT()), 0644); err != nil {
		log.Printf("[subs] write failed %s: %v", filepath.Base(path), err)
		return false
	}
	return true
}

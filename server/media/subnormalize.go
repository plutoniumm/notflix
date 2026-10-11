package media

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf16"

	"github.com/martinlindhe/subtitles"

	"notflix/server/library"
)

// UTF-16 sidecars ship from some release groups (three Pirates .srt files in
// this library are UTF-16 LE). Both parsers below rejected them outright on the
// 0xff BOM byte as "likely binary", so the subtitle silently never existed.
// Decoded here, ahead of the UTF-8 BOM trim, so every parse site inherits it.
func decodeText(s string) string {
	if len(s) < 2 {
		return s
	}
	var le bool
	switch {
	case s[0] == 0xff && s[1] == 0xfe:
		le = true
	case s[0] == 0xfe && s[1] == 0xff:
		le = false
	default:
		return s
	}
	b := []byte(s[2:])
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if le {
			u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
		} else {
			u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
		}
	}
	return string(utf16.Decode(u))
}

// Strip the UTF-8 BOM before parsing: many Windows-saved SRTs start with
// EF BB BF, which makes martinlindhe/subtitles fail on the header line.
func parseSRT(s string) (subtitles.Subtitle, error) {
	s = decodeText(s)
	s = strings.TrimPrefix(s, "\ufeff")
	s = strings.TrimLeft(s, " \t\r\n\x00")
	if s == "" {
		return subtitles.Subtitle{}, fmt.Errorf("not an SRT: empty after trim")
	}
	if c := s[0]; c < '0' || c > '9' {
		return subtitles.Subtitle{}, fmt.Errorf(
			"not an SRT: leading byte 0x%02x (likely binary, wrong encoding, or corrupt)", c)
	}
	return subtitles.NewFromSRT(s)
}
func parseVTT(s string) (subtitles.Subtitle, error) {
	s = decodeText(s)
	s = strings.TrimPrefix(s, "\ufeff")
	s = strings.TrimLeft(s, " \t\r\n\x00")
	if s == "" {
		return subtitles.Subtitle{}, fmt.Errorf("not a VTT: empty after trim")
	}
	if !strings.HasPrefix(s, "WEBVTT") {
		if c := s[0]; c < ' ' && c != '\t' {
			return subtitles.Subtitle{}, fmt.Errorf(
				"not a VTT: leading byte 0x%02x (likely binary or corrupt)", c)
		}
	}
	return subtitles.NewFromVTT(s)
}

// A cue timing in either dialect, with or without an hours field.
var cueTiming = regexp.MustCompile(`\d{1,2}:\d{2}(:\d{2})?[.,]\d{3}\s*-->`)

func snippet(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if len(s) > 80 {
		s = s[:80] + "..."
	}
	return s
}

// AsVTT is the one gate between a provider's bytes and a <track>.
//
// The download handler used to fall back to `vtt = string(raw)` whenever a
// parse failed, and the result was written to disk as the sidecar — so an API
// error object or an HTML error page BECAME the subtitle, rendered on screen as
// raw JSON, and nothing ever retried because the file now existed. The existing
// status-code check guarded only the HTTP-level version of the same bug.
//
// A parse failure is now a failure. The sole fallback left is a file that is
// already self-declared VTT with at least one cue timing, which covers valid
// subtitles this particular parser happens to dislike.
func AsVTT(raw []byte) (string, error) {
	s := decodeText(string(raw))
	s = strings.TrimPrefix(s, "\ufeff")
	s = strings.TrimLeft(s, " \t\r\n\x00")
	if s == "" {
		return "", fmt.Errorf("empty subtitle")
	}

	switch {
	case s[0] == '{' || s[0] == '[':
		return "", fmt.Errorf("provider returned JSON, not a subtitle: %s", snippet(s))
	case s[0] == '<':
		return "", fmt.Errorf("provider returned markup, not a subtitle: %s", snippet(s))
	case strings.HasPrefix(s, "PK\x03\x04"):
		return "", fmt.Errorf("provider returned a zip archive, not a subtitle")
	}

	if p, err := parseVTT(s); err == nil && len(p.Captions) > 0 {
		return p.AsVTT(), nil
	}
	if p, err := parseSRT(s); err == nil && len(p.Captions) > 0 {
		return p.AsVTT(), nil
	}
	if strings.HasPrefix(s, "WEBVTT") && cueTiming.MatchString(s) {
		return s, nil
	}

	return "", fmt.Errorf("unrecognised subtitle format: %s", snippet(s))
}

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
	parsed, err := parseSRT(string(data))
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
	parsed, err := parseSRT(string(data))
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

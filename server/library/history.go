package library

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// History is one row per FILE, not per torrent: a season pack contributes an
// episode each, because every other store here keys on the episode's own name.
// The saved .torrent files (aria2 --bt-save-metadata) are only the discovery
// mechanism — they enumerate what ever existed, then each file is looked up
// across the watch log, the KV marks, the pipeline ledger, the HLS cache and
// the disk itself. Anything a store doesn't know stays zero: the frontend
// prints a dash and never guesses.

// --- bencode ---------------------------------------------------------------

// Four types, no dependency: d…e dict, l…e list, i<int>e, <len>:<bytes>.
// Everything is decoded and the unneeded keys dropped; a truncated or corrupt
// file must error, never panic — one bad .torrent can't take the handler down.
type bdec struct {
	b []byte
	i int
}

const bdecMaxDepth = 32

func (d *bdec) value(depth int) (any, error) {
	if depth > bdecMaxDepth {
		return nil, errors.New("bencode: nested too deep")
	}
	if d.i >= len(d.b) {
		return nil, io.ErrUnexpectedEOF
	}

	switch c := d.b[d.i]; {
	case c == 'd':
		d.i++
		m := map[string]any{}
		for {
			if d.i >= len(d.b) {
				return nil, io.ErrUnexpectedEOF
			}
			if d.b[d.i] == 'e' {
				d.i++

				return m, nil
			}
			k, err := d.str()
			if err != nil {
				return nil, err
			}
			v, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			m[k] = v
		}

	case c == 'l':
		d.i++
		l := []any{}
		for {
			if d.i >= len(d.b) {
				return nil, io.ErrUnexpectedEOF
			}
			if d.b[d.i] == 'e' {
				d.i++

				return l, nil
			}
			v, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			l = append(l, v)
		}

	case c == 'i':
		d.i++
		j := bytes.IndexByte(d.b[d.i:], 'e')
		if j < 0 {
			return nil, errors.New("bencode: unterminated int")
		}
		n, err := strconv.ParseInt(string(d.b[d.i:d.i+j]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("bencode: bad int: %w", err)
		}
		d.i += j + 1

		return n, nil

	case c >= '0' && c <= '9':
		return d.str()
	}

	return nil, fmt.Errorf("bencode: unexpected %q at %d", d.b[d.i], d.i)
}

func (d *bdec) str() (string, error) {
	j := bytes.IndexByte(d.b[d.i:], ':')
	if j < 0 {
		return "", errors.New("bencode: unterminated string")
	}
	n, err := strconv.Atoi(string(d.b[d.i : d.i+j]))
	if err != nil || n < 0 {
		return "", errors.New("bencode: bad string length")
	}
	start := d.i + j + 1
	if start+n > len(d.b) {
		return "", io.ErrUnexpectedEOF
	}
	d.i = start + n

	return string(d.b[start : start+n]), nil
}

func bdecode(b []byte) (any, error) {
	d := &bdec{b: b}

	return d.value(0)
}

type torFile struct {
	Name string
	Rel  string
	Size int64
}

// torrentMeta returns the torrent's display name and one entry per contained
// file. info.files is the multi-file form; info.length the single-file one.
func torrentMeta(path string) (string, []torFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	v, err := bdecode(raw)
	if err != nil {
		return "", nil, err
	}
	top, _ := v.(map[string]any)
	info, _ := top["info"].(map[string]any)
	if info == nil {
		return "", nil, errors.New("torrent: no info dict")
	}
	name, _ := info["name"].(string)

	if fl, ok := info["files"].([]any); ok {
		var out []torFile
		for _, f := range fl {
			fm, _ := f.(map[string]any)
			if fm == nil {
				continue
			}
			var seg []string
			parts, _ := fm["path"].([]any)
			for _, p := range parts {
				if s, ok := p.(string); ok && s != "" {
					seg = append(seg, s)
				}
			}
			if len(seg) == 0 {
				continue
			}
			size, _ := fm["length"].(int64)
			out = append(out, torFile{Name: seg[len(seg)-1], Rel: strings.Join(seg, "/"), Size: size})
		}

		return name, out, nil
	}

	size, _ := info["length"].(int64)
	if name == "" {
		return "", nil, errors.New("torrent: no name")
	}

	return name, []torFile{{Name: name, Rel: name, Size: size}}, nil
}

// --- watch log -------------------------------------------------------------

type watchAgg struct {
	First    int64   `json:"firstAt"`
	Last     int64   `json:"lastAt"`
	Sessions int     `json:"sess"`
	MaxT     float64 `json:"t"`
	Dur      float64 `json:"dur"`
	Device   string  `json:"dev"`
	VP       string  `json:"vp"`
}

var (
	wMu    sync.Mutex
	wAgg   = map[string]*watchAgg{}
	wOff   int64
	wLines int
)

// scanStats is the ONE reader of stats.jsonl: from a byte offset, line by
// line (120 MB and growing — it never gets loaded whole). The offset only
// advances past complete, newline-terminated lines, so a half-written tail is
// re-read next pass instead of being skipped.
func scanStats(from int64, fn func(statsEvent)) (lines int, end int64) {
	statsMu.Lock()
	defer statsMu.Unlock()

	f, err := os.Open(statsPath)
	if err != nil {
		return 0, from
	}
	defer f.Close()

	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return 0, from
	}

	end = from
	r := bufio.NewReaderSize(f, 256*1024)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			end += int64(len(line))
			var ev statsEvent
			if json.Unmarshal(line, &ev) == nil && ev.VP != "" {
				lines++
				fn(ev)
			}
		}
		if err != nil {
			return lines, end
		}
	}
}

// watchIndex aggregates the log into per-video sessions, incrementally: the
// log is append-only, so a re-poll only reads what was appended since. A gap
// over sessionGapMs (the same 30s stats.go splits on — the player pings every
// 2s, so anything larger is a pause, a seek or a closed tab) starts a new
// session.
func watchIndex() (map[string]*watchAgg, int) {
	wMu.Lock()
	defer wMu.Unlock()

	st, err := os.Stat(statsPath)
	if err != nil {
		return wAgg, wLines
	}
	if st.Size() < wOff { // truncated or rotated — the cached tail is meaningless
		wAgg, wOff, wLines = map[string]*watchAgg{}, 0, 0
	}
	if st.Size() == wOff {
		return wAgg, wLines
	}

	n, end := scanStats(wOff, func(e statsEvent) {
		a := wAgg[e.VP]
		if a == nil {
			a = &watchAgg{First: e.At, VP: e.VP}
			wAgg[e.VP] = a
		}
		if e.At < a.First {
			a.First = e.At
		}
		if e.At-a.Last > sessionGapMs {
			a.Sessions++
		}
		if e.At > a.Last {
			a.Last = e.At
		}
		if e.T > a.MaxT {
			a.MaxT = e.T
		}
		if e.Dur > 0 {
			a.Dur = e.Dur
		}
		if e.Host != "" {
			a.Device = e.Host
		} else if e.User != "" && a.Device == "" {
			a.Device = e.User
		}
	})
	wOff, wLines = end, wLines+n

	return wAgg, wLines
}

// --- joins -----------------------------------------------------------------

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// normKey joins the stores. CleanAll and conversion RENAME files after
// download — "The.Orville.S02E11.Lasting.Impressions.1080p.WEBRip.AC3.x264-LESS.mkv"
// is on disk as "The Orville S02E11 Lasting Impressions LESS.mp4", a new name
// AND a new extension — so an exact path match misses nearly every join.
// CleanName already drops bracketed groups and release tags; lowercasing and
// dropping punctuation closes the rest. Episode digits survive, which is what
// keeps two episodes of one show apart.
func normKey(name string) string {
	return nonAlnum.ReplaceAllString(strings.ToLower(CleanName(name)), "")
}

type HistRow struct {
	Key      string            `json:"key"`
	Name     string            `json:"name"`
	Rel      string            `json:"rel,omitempty"`
	Size     int64             `json:"size,omitempty"`
	OnDisk   bool              `json:"disk"`
	Torrent  string            `json:"tor,omitempty"`
	Infohash string            `json:"ih,omitempty"`
	DlAt     int64             `json:"dlAt,omitempty"`
	ProcAt   int64             `json:"procAt,omitempty"`
	FirstAt  int64             `json:"firstAt,omitempty"`
	LastAt   int64             `json:"lastAt,omitempty"`
	Sessions int               `json:"sess,omitempty"`
	MaxT     float64           `json:"t,omitempty"`
	Dur      float64           `json:"dur,omitempty"`
	Device   string            `json:"dev,omitempty"`
	Marks    []string          `json:"marks,omitempty"`
	Steps    map[string]string `json:"steps,omitempty"`
	HLS      bool              `json:"hls,omitempty"`
}

type HistStats struct {
	Rows        int   `json:"rows"`
	Torrents    int   `json:"torrents"`
	TorrentsBad int   `json:"torrentsBad"`
	DiskFiles   int   `json:"diskFiles"`
	WatchLines  int   `json:"watchLines"`
	WatchVideos int   `json:"watchVideos"`
	Missing     int   `json:"missing"`
	ScanMs      int64 `json:"scanMs"`
}

type diskFile struct {
	rel, abs string
	size     int64
}

// walkRoots collects the videos and the saved .torrent files in one pass.
// No filepath.Glob anywhere near a filename: "[YTS.MX]" is a character class
// and Glob returns nothing with a nil error.
func walkRoots(roots []string) (vids []diskFile, tors []string) {
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			continue // an unmounted drive is not an error here
		}
		filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if p != root && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}

				return nil
			}
			if strings.HasPrefix(d.Name(), ".") {
				return nil
			}

			switch ext := strings.ToLower(filepath.Ext(d.Name())); {
			case ext == ".torrent":
				tors = append(tors, p)
			case videoExts[ext]:
				rel, rerr := filepath.Rel(root, p)
				if rerr != nil {
					return nil
				}
				var size int64
				if info, ierr := d.Info(); ierr == nil {
					size = info.Size()
				}
				vids = append(vids, diskFile{rel: filepath.ToSlash(rel), abs: p, size: size})
			}

			return nil
		})
	}

	return vids, tors
}

// hlsKeys are the content-addressed HLS cache dirs, keyed on Hash(rel path) —
// a DIFFERENT hash input from the KV marks, which key on Hash(basename).
func hlsKeys() map[string]bool {
	out := map[string]bool{}
	ents, err := os.ReadDir("./cache")
	if err != nil {
		return out
	}
	for _, e := range ents {
		if e.IsDir() && len(e.Name()) == 7 {
			if _, err := strconv.Atoi(e.Name()); err == nil {
				out[e.Name()] = true
			}
		}
	}

	return out
}

var histMu sync.Mutex

// History builds the joined table. Walking the roots and parsing ~100 small
// torrents is cheap; the watch log is the expensive side and that one is
// cached incrementally, so a re-poll costs only the newly appended bytes.
func History(lib *Library) ([]HistRow, HistStats) {
	histMu.Lock()
	defer histMu.Unlock()

	t0 := time.Now()
	var st HistStats

	vids, tors := walkRoots(lib.Roots)
	st.DiskFiles, st.Torrents = len(vids), len(tors)

	rows := map[string]*HistRow{}
	row := func(name string) *HistRow {
		k := normKey(name)
		if k == "" {
			k = strings.ToLower(name)
		}
		r := rows[k]
		if r == nil {
			r = &HistRow{Key: k, Name: name}
			rows[k] = r
		}

		return r
	}

	// 1. Discovery: every file any saved torrent ever carried.
	for _, p := range tors {
		name, files, err := torrentMeta(p)
		if err != nil {
			st.TorrentsBad++
			log.Printf("[history] skipping %s: %v", filepath.Base(p), err)

			continue
		}
		var at int64
		if fi, err := os.Stat(p); err == nil {
			at = fi.ModTime().UnixMilli() // the .torrent's mtime IS the download date
		}
		ih := strings.TrimSuffix(filepath.Base(p), ".torrent")

		for _, f := range files {
			if !videoExts[strings.ToLower(filepath.Ext(f.Name))] {
				continue
			}
			r := row(f.Name)
			r.Torrent, r.Infohash, r.DlAt = name, ih, at
			if r.Size == 0 {
				r.Size = f.Size
			}
			if r.Rel == "" {
				r.Rel = f.Rel
			}
		}
	}

	// 2. What is actually on disk now.
	byAbs := map[string]*HistRow{}
	for _, v := range vids {
		r := row(filepath.Base(v.rel))
		r.OnDisk, r.Rel, r.Size = true, v.rel, v.size
		r.Name = filepath.Base(v.rel)
		byAbs[v.abs] = r
	}

	// 3. Watches. A vp with no torrent and no file is still a row: watched,
	// since deleted.
	watch, lines := watchIndex()
	st.WatchLines, st.WatchVideos = lines, len(watch)
	for vp, a := range watch {
		r := row(filepath.Base(vp))
		r.FirstAt, r.LastAt, r.Sessions = a.First, a.Last, a.Sessions
		r.MaxT, r.Dur, r.Device = a.MaxT, a.Dur, a.Device
		if !r.OnDisk && r.Rel == "" {
			r.Rel = vp
		}
	}

	// 4. Pipeline ledger, keyed by ABSOLUTE path. A stale key still joins
	// through normKey, and one that joins nothing is itself history: the file
	// provably existed here (something processed it) and is now gone, so it
	// gets its own row rather than being dropped.
	for abs, fp := range PipelineSnapshot() {
		if len(fp.Steps) == 0 {
			continue
		}
		r := byAbs[abs]
		if r == nil {
			r = row(filepath.Base(abs))
		}
		if r.Steps == nil {
			r.Steps = map[string]string{}
		}
		for k, v := range fp.Steps {
			r.Steps[k] = string(v)
		}
		// The ledger's own timestamp — when a step last ran, NOT a download or
		// a watch date. It stays in its own field; nothing derives one date
		// from another.
		if fp.Updated*1000 > r.ProcAt {
			r.ProcAt = fp.Updated * 1000
		}
	}

	// 5. KV marks (basename hash) and HLS cache (rel-path hash).
	kvStore := kvRead()
	hls := hlsKeys()
	out := make([]HistRow, 0, len(rows))
	for _, r := range rows {
		h := Hash(r.Name)
		for _, m := range []string{"codec", "skip", "poster", "blurb"} {
			if v, ok := kvStore[m+":"+h]; ok {
				if s, isStr := v.(string); isStr && m == "codec" {
					r.Marks = append(r.Marks, s)
				} else {
					r.Marks = append(r.Marks, m)
				}
			}
		}
		if r.Rel != "" {
			r.HLS = hls[Hash(r.Rel)]
		}
		if !r.OnDisk {
			st.Missing++
		}
		out = append(out, *r)
	}

	st.Rows = len(out)
	st.ScanMs = time.Since(t0).Milliseconds()

	return out, st
}

func HistoryHandler(c *gin.Context, lib *Library) {
	rows, st := History(lib)
	c.JSON(http.StatusOK, gin.H{"rows": rows, "stats": st})
}

package jobs

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"notflix/server/library"
)

type ytJob struct {
	GID    string
	URL    string
	Dir    string
	Title  string
	Status string
	Pct    float64
	Speed  int64
	Total  int64
	Cmd    *exec.Cmd
	Cancel context.CancelFunc
}

var (
	ytMu   sync.Mutex
	ytJobs = map[string]*ytJob{}
)

func newYTGID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "yt" + hex.EncodeToString(b)
}

// IsYT reports whether a gid was issued by the yt-dlp manager.
func IsYT(gid string) bool { return strings.HasPrefix(gid, "yt") }

// IsYTCandidate decides whether to hand a URL to yt-dlp instead of aria2.
// magnet/torrent and URLs ending in a known media extension stay on aria2;
// everything else http(s) goes to yt-dlp.
func IsYTCandidate(uri string) bool {
	low := strings.ToLower(strings.TrimSpace(uri))
	if !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
		return false
	}
	path := low
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	for _, ext := range []string{
		".mp4", ".mkv", ".webm", ".m4v", ".avi", ".mov", ".flv", ".wmv",
		".mpg", ".mpeg", ".ts", ".3gp", ".mp3", ".m4a", ".flac", ".wav",
		".torrent",
	} {
		if strings.HasSuffix(path, ext) {
			return false
		}
	}
	return true
}

var (
	reYTPct  = regexp.MustCompile(`\[download\]\s+([0-9.]+)%`)
	reYTSize = regexp.MustCompile(`of\s+~?\s*([0-9.]+)\s*([KMGTP]?i?B)`)
	reYTSpd  = regexp.MustCompile(`at\s+([0-9.]+)\s*([KMGTP]?i?B)/s`)
	reYTDest = regexp.MustCompile(`(?:Destination:|Merging formats into)\s+"?(.+?\.[A-Za-z0-9]+)"?\s*$`)
)

func parseSize(n float64, unit string) int64 {
	mul := int64(1)
	switch strings.ToUpper(unit) {
	case "KIB", "KB":
		mul = 1 << 10
	case "MIB", "MB":
		mul = 1 << 20
	case "GIB", "GB":
		mul = 1 << 30
	case "TIB", "TB":
		mul = 1 << 40
	}
	return int64(n * float64(mul))
}

func YTDlpAdd(url, dir string, lib *library.Library) (string, error) {
	gid := newYTGID()
	ctx, cancel := context.WithCancel(context.Background())

	args := []string{
		"--newline", "--no-color", "--no-playlist", "--continue",
		"--restrict-filenames",
		"--merge-output-format", "mp4",
		"-f", "bv*[ext=mp4]+ba[ext=m4a]/b[ext=mp4]/bv*+ba/b",
		"-o", "%(title)s.%(ext)s",
		"-P", dir,
		url,
	}
	cmd := exec.CommandContext(ctx, "yt-dlp", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return "", err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		cancel()
		return "", err
	}

	job := &ytJob{
		GID: gid, URL: url, Dir: dir,
		Status: "active", Cmd: cmd, Cancel: cancel,
	}
	ytMu.Lock()
	ytJobs[gid] = job
	ytMu.Unlock()
	log.Printf("[yt-dlp] start gid=%s url=%s dir=%s", gid, url, dir)

	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			parseYTLine(job, sc.Text())
		}
		waitErr := cmd.Wait()

		ytMu.Lock()
		switch {
		case ctx.Err() != nil:
			job.Status = "removed"
			log.Printf("[yt-dlp] cancelled gid=%s", gid)
		case waitErr != nil:
			job.Status = "error"
			log.Printf("[yt-dlp] error gid=%s: %v", gid, waitErr)
		default:
			job.Status = "complete"
			job.Pct = 100
			log.Printf("[yt-dlp] complete gid=%s title=%s", gid, job.Title)
		}
		final := job.Status
		ytMu.Unlock()

		if final == "complete" && OnDownloads != nil {
			go OnDownloads(lib)
		}

		// Give the UI a moment to display the final state, then drop.
		time.Sleep(8 * time.Second)
		ytMu.Lock()
		delete(ytJobs, gid)
		ytMu.Unlock()
	}()

	return gid, nil
}

func parseYTLine(j *ytJob, line string) {
	ytMu.Lock()
	defer ytMu.Unlock()

	if m := reYTDest.FindStringSubmatch(line); m != nil {
		j.Title = filepath.Base(m[1])
	}
	if m := reYTPct.FindStringSubmatch(line); m != nil {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			j.Pct = v
		}
	}
	if m := reYTSize.FindStringSubmatch(line); m != nil {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			j.Total = parseSize(v, m[2])
		}
	}
	if m := reYTSpd.FindStringSubmatch(line); m != nil {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			j.Speed = parseSize(v, m[2])
		}
	}
}

func YTDlpRemove(gid string) {
	ytMu.Lock()
	j, ok := ytJobs[gid]
	if ok {
		delete(ytJobs, gid)
	}
	ytMu.Unlock()
	if !ok {
		return
	}
	if j.Cmd != nil && j.Cmd.Process != nil {
		_ = j.Cmd.Process.Signal(syscall.SIGCONT)
		_ = j.Cmd.Process.Signal(syscall.SIGTERM)
	}
	if j.Cancel != nil {
		j.Cancel()
	}
	log.Printf("[yt-dlp] remove gid=%s", gid)
}

func YTDlpPause(gid string) error {
	ytMu.Lock()
	j, ok := ytJobs[gid]
	ytMu.Unlock()
	if !ok {
		return fmt.Errorf("not found")
	}
	if j.Cmd == nil || j.Cmd.Process == nil {
		return fmt.Errorf("no process")
	}
	if err := j.Cmd.Process.Signal(syscall.SIGSTOP); err != nil {
		return err
	}
	ytMu.Lock()
	j.Status = "paused"
	j.Speed = 0
	ytMu.Unlock()
	return nil
}

func YTDlpResume(gid string) error {
	ytMu.Lock()
	j, ok := ytJobs[gid]
	ytMu.Unlock()
	if !ok {
		return fmt.Errorf("not found")
	}
	if j.Cmd == nil || j.Cmd.Process == nil {
		return fmt.Errorf("no process")
	}
	if err := j.Cmd.Process.Signal(syscall.SIGCONT); err != nil {
		return err
	}
	ytMu.Lock()
	j.Status = "active"
	ytMu.Unlock()
	return nil
}

func YTDlpListItems() []Downjob {
	ytMu.Lock()
	defer ytMu.Unlock()
	out := make([]Downjob, 0, len(ytJobs))
	for _, j := range ytJobs {
		name := j.Title
		if name == "" {
			name = j.URL
		}
		var done int64
		if j.Total > 0 {
			done = int64(float64(j.Total) * j.Pct / 100)
		}
		out = append(out, Downjob{
			GID:     j.GID,
			Name:    name,
			Status:  j.Status,
			Total:   j.Total,
			Done:    done,
			Percent: j.Pct,
			Speed:   j.Speed,
		})
	}
	return out
}

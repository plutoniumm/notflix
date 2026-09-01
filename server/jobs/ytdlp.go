package jobs

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
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

func IsYT(gid string) bool { return strings.HasPrefix(gid, "yt") }

// magnet/torrent and direct-media URLs stay on aria2; other http(s) is yt-dlp.
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
	// yt-dlp forks its own ffmpeg for the mux: signal the group, or that ffmpeg
	// keeps writing into a library root and the next run stomps it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd, syscall.SIGKILL) }

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
		if err := sc.Err(); err != nil {
			log.Printf("[yt-dlp] output scan gid=%s: %v", gid, err)
		}

		// A token past the scanner's limit stops the loop with the pipe still
		// filling; yt-dlp then blocks writing and cmd.Wait never returns.
		io.Copy(io.Discard, stdout)

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
	stopJob(j)
	log.Printf("[yt-dlp] remove gid=%s", gid)
}

// Nothing else covers these on shutdown: an orphaned yt-dlp keeps writing into
// a library root and the next run stomps it. Wire in alongside Aria2Stop.
func YTDlpStopAll() {
	ytMu.Lock()
	running := make([]*ytJob, 0, len(ytJobs))
	for _, j := range ytJobs {
		running = append(running, j)
	}
	ytMu.Unlock()

	for _, j := range running {
		stopJob(j)
	}
	if len(running) > 0 {
		log.Printf("[yt-dlp] stopped %d download(s)", len(running))
	}
}

// CONT first, so a paused download can act on the TERM; Cancel then KILLs.
func stopJob(j *ytJob) {
	killGroup(j.Cmd, syscall.SIGCONT)
	killGroup(j.Cmd, syscall.SIGTERM)
	if j.Cancel != nil {
		j.Cancel()
	}
}

func killGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// Signal the handle first: os.Process refuses once reaped (ErrProcessDone),
	// so the raw group kill can't land on a recycled pid.
	if err := cmd.Process.Signal(sig); err != nil {
		return err
	}

	return syscall.Kill(-cmd.Process.Pid, sig)
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
	if err := killGroup(j.Cmd, syscall.SIGSTOP); err != nil {
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
	if err := killGroup(j.Cmd, syscall.SIGCONT); err != nil {
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

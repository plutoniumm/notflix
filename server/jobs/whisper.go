package jobs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"notflix/server/library"
)

const pythonPath = "/opt/homebrew/Caskroom/miniconda/base/envs/global/bin/python"

const whisperMax = 4 * time.Hour

var (
	// Entries leave by process identity, never by key alone: deleting by key
	// dropped a LATER run's entry and two pythons raced on one .whisper.vtt.
	activeProcs sync.Map
	whisperJobs sync.Map
)

func procKill(key string) {
	if cur, ok := activeProcs.Load(key); ok {
		log.Printf("[whisper] killing existing run for %q", key)
		killCmd(cur.(*exec.Cmd))
	}
}

func procSwap(key string, cmd *exec.Cmd) {
	if old, loaded := activeProcs.Swap(key, cmd); loaded {
		killCmd(old.(*exec.Cmd))
	}
}

func procDone(key string, cmd *exec.Cmd) {
	activeProcs.CompareAndDelete(key, cmd)
}

func killCmd(cmd *exec.Cmd) {
	if cmd.Process != nil {
		cmd.Process.Kill()
	}
}

func whisperVTTOf(path string) string {
	ext := filepath.Ext(path)

	return path[:len(path)-len(ext)] + ".whisper.vtt"
}

type whisperSeg struct {
	Start float64
	End   float64
	Text  string
}

type WhisperJob struct {
	mu     sync.Mutex
	Status string
	Err    string
}

func (j *WhisperJob) set(status, msg string) {
	j.mu.Lock()
	j.Status = status
	j.Err = msg
	j.mu.Unlock()
}

func (j *WhisperJob) read() (string, string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.Status, j.Err
}

func fmtDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	ms := int(d.Milliseconds()) % 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}

// Whisper stretches a segment's End out to the next speech chunk, so a
// one-syllable line can carry a 40s duration and sit on screen for most of a
// scene (City Hunter ep05, small/cond-off: `ね` held for 24.8s, 17 such cues in
// one episode). The TEXT is fine — only End is wrong. Measured against the
// release sub for that episode, whose longest cue is 7.8s: hand-authored subs
// never linger, so clamp, and never run past the next cue's start.
const whisperMaxCue = 8.0

func whisperVTT(segs []whisperSeg) []byte {
	var sb bytes.Buffer
	sb.WriteString("WEBVTT\n\n")
	for i, s := range segs {
		end := s.End
		if lim := s.Start + whisperMaxCue; end > lim {
			end = lim
		}
		if i+1 < len(segs) && end > segs[i+1].Start {
			end = segs[i+1].Start
		}
		if end <= s.Start {
			end = s.Start + 0.5
		}
		sb.WriteString(fmtSecs(s.Start) + " --> " + fmtSecs(end) + "\n")
		sb.WriteString(s.Text + "\n\n")
	}
	return sb.Bytes()
}

func fmtSecs(secs float64) string {
	return fmtDur(time.Duration(secs * float64(time.Second)))
}

var fasterWhisperOK atomic.Bool

// ~500ms fork, cached once-positive so the backfill walker doesn't pay per
// file; a failure re-probes, so installing it needs no restart.
func checkFasterWhisper() bool {
	if fasterWhisperOK.Load() {
		return true
	}
	cmd := exec.Command(pythonPath, "-c", "import faster_whisper")
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[whisper] faster-whisper check failed: %v — %s", err, string(out))
		return false
	}
	fasterWhisperOK.Store(true)
	return true
}

func extractToWAV(src string) (string, error) {
	tmp, err := os.CreateTemp("", "notflix-whisper-*.wav")
	if err != nil {
		return "", err
	}
	tmp.Close()
	tmpPath := tmp.Name()

	log.Printf("[whisper] extracting audio: %s → %s", src, tmpPath)
	if stderr, err := library.FFNice("-y", "-i", src, "-ar", "16000", "-ac", "1", tmpPath); err != nil {
		os.Remove(tmpPath)
		return "", library.FFErr(stderr, err)
	}

	if fi, err := os.Stat(tmpPath); err == nil {
		log.Printf("[whisper] wav extracted: %s (%d bytes)", tmpPath, fi.Size())
	}
	return tmpPath, nil
}

func SubsWhisperStream(c *gin.Context, lib *library.Library) {
	raw := c.Query("file")
	if raw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file param"})
		return
	}
	log.Printf("[whisper] stream request: file=%q", raw)

	path, ok := lib.FindVid(raw)
	if !ok {
		log.Printf("[whisper] video not found: %q", raw)
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return
	}
	log.Printf("[whisper] resolved path: %s", path)

	if !checkFasterWhisper() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "faster-whisper not installed. Run: pip install faster-whisper"})
		return
	}
	log.Printf("[whisper] faster-whisper OK")

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		fmt.Fprintf(c.Writer, "data: {\"error\":\"streaming unsupported\"}\n\n")
		return
	}

	sendEvent := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(c.Writer, "data: %s\n\n", b)
		flusher.Flush()
	}

	// Existing transcript → done. 100B is above an empty `WEBVTT` header and
	// under any real transcript.
	if fi, err := os.Stat(whisperVTTOf(path)); err == nil && fi.Size() > 100 {
		log.Printf("[whisper] reuse existing vtt for %q (%d bytes)", raw, fi.Size())
		sendEvent(map[string]any{"done": true})
		return
	}

	procKill(raw)

	tmpWAV, err := extractToWAV(path)
	if err != nil {
		log.Printf("[whisper] audio extraction failed: %v", err)
		sendEvent(map[string]string{"error": err.Error()})
		return
	}
	defer os.Remove(tmpWAV)

	modelName := c.DefaultQuery("model", "small") // see stream_whisper.py — base measured 68.6% vs small 91.4%
	log.Printf("[whisper] starting python: model=%s wav=%s", modelName, tmpWAV)

	ctx := c.Request.Context()
	cmd := exec.CommandContext(ctx, pythonPath, "tools/stream_whisper.py", tmpWAV, modelName)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("[whisper] stdout pipe error: %v", err)
		sendEvent(map[string]string{"error": "pipe error: " + err.Error()})
		return
	}

	// Bounded, NUL-stripped tail: a misbehaving python once flooded the log
	// with thousands of `\x00…` rows.
	stderrBuf := &library.TailWriter{Max: 8 << 10}
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		log.Printf("[whisper] python start error: %v", err)
		sendEvent(map[string]string{"error": "failed to start python: " + err.Error()})
		return
	}
	log.Printf("[whisper] python pid=%d", cmd.Process.Pid)

	procSwap(raw, cmd)
	defer procDone(raw, cmd)

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)

	// First line from python is {"lang": "…"}.
	var lang string
	if scanner.Scan() {
		langLine := scanner.Text()
		log.Printf("[whisper] lang line: %s", langLine)
		var langObj struct {
			Lang string `json:"lang"`
		}
		if err := json.Unmarshal([]byte(langLine), &langObj); err == nil {
			lang = langObj.Lang
		}
	}

	var segs []whisperSeg
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var seg struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Text  string  `json:"text"`
		}
		if err := json.Unmarshal([]byte(line), &seg); err != nil {
			log.Printf("[whisper] bad JSON line %q: %v", line, err)
			continue
		}
		if len(segs) == 0 {
			log.Printf("[whisper] first segment: start=%.2f end=%.2f text=%q", seg.Start, seg.End, seg.Text)
		}
		segs = append(segs, whisperSeg{seg.Start, seg.End, seg.Text})
		sendEvent(map[string]any{"start": seg.Start, "end": seg.End, "text": seg.Text})
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[whisper] stdout scan: %v", err)
	}

	// A token past the scanner's limit stops the loop with the pipe still
	// filling; python then blocks on the write and cmd.Wait never returns.
	io.Copy(io.Discard, stdout)

	// stderrBuf is a TailWriter, so os/exec fills it from an internal
	// goroutine that only Wait joins — reading it before Wait races.
	waitErr := cmd.Wait()
	log.Printf("[whisper] %d segments read, stderr: %s", len(segs), stderrBuf.String())

	if waitErr != nil && ctx.Err() == nil {
		log.Printf("[whisper] python wait error: %v | stderr: %s", waitErr, stderrBuf.String())
		sendEvent(map[string]string{"error": "python error: " + stderrBuf.String()})
		return
	}

	if ctx.Err() != nil {
		log.Printf("[whisper] context cancelled (client disconnected)")
		return
	}

	if lang != "en" && lang != "" && len(segs) > 0 {
		log.Printf("[whisper] detected lang=%q, translating %d segments via Ollama", lang, len(segs))
		sendEvent(map[string]any{"translating": true})
		texts := make([]string, len(segs))
		for i, s := range segs {
			texts[i] = s.Text
		}
		if translated, err := TranslateSegments(texts, lang, ShowGlossary(path)); err == nil {
			for i := range segs {
				segs[i].Text = translated[i]
			}
		} else {
			log.Printf("[whisper] translation failed: %v — saving raw", err)
		}
	}

	vtt := whisperVTT(segs)

	vttPath := whisperVTTOf(path)
	if err := os.WriteFile(vttPath, vtt, 0644); err != nil {
		log.Printf("[whisper] failed to write vtt: %v", err)
	} else {
		log.Printf("[whisper] vtt saved: %s (%d bytes)", vttPath, len(vtt))
	}
	sendEvent(map[string]any{"done": true})
	log.Printf("[whisper] stream complete: %d segments", len(segs))
}

func SubsWhisper(c *gin.Context, lib *library.Library) {
	var body struct {
		File string `json:"file"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.File == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	path, ok := lib.FindVid(body.File)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return
	}

	// Same idempotency guard as the SSE handler: existing vtt → done.
	if fi, err := os.Stat(whisperVTTOf(path)); err == nil && fi.Size() > 100 {
		c.JSON(http.StatusOK, gin.H{"status": "done"})
		return
	}

	// A terminal entry is replaced, not reported: leaving it made one transient
	// failure permanent until restart.
	job := &WhisperJob{Status: "pending"}
	if prev, loaded := whisperJobs.LoadOrStore(body.File, job); loaded {
		status, msg := prev.(*WhisperJob).read()
		if status == "pending" {
			c.JSON(http.StatusOK, gin.H{"status": "pending"})
			return
		}
		if !whisperJobs.CompareAndSwap(body.File, prev, job) {
			c.JSON(http.StatusOK, gin.H{"status": "pending"})
			return
		}
		log.Printf("[whisper] retrying %q after %s %s", body.File, status, msg)
	}

	go runJob(path, whisperVTTOf(path), body.File, job)
	c.JSON(http.StatusOK, gin.H{"status": "pending"})
}

func SubsWhisperStatus(c *gin.Context) {
	raw := c.Query("file")
	if raw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file param"})
		return
	}

	existing, loaded := whisperJobs.Load(raw)
	if !loaded {
		c.JSON(http.StatusOK, gin.H{"status": "not_started"})
		return
	}

	job := existing.(*WhisperJob)
	status, msg := job.read()
	c.JSON(http.StatusOK, gin.H{"status": status, "error": msg})
}

func runJob(src, dst, key string, job *WhisperJob) {
	if !checkFasterWhisper() {
		job.set("error", "faster-whisper not installed")
		return
	}

	tmpWAV, err := extractToWAV(src)
	if err != nil {
		job.set("error", "audio extraction failed: "+err.Error())
		return
	}
	defer os.Remove(tmpWAV)

	// A hung python would otherwise hold the 1-wide auto-whisper pool forever.
	ctx, cancel := context.WithTimeout(context.Background(), whisperMax)
	defer cancel()

	// Niced like FFRun.Nice, for the same reason: this is backfill, and
	// faster-whisper saturates every core. Unniced it starved the AV1 copy pass
	// hard enough to blow HLSSegment's 120s wait and 500 a cold first play. The
	// interactive SSE transcribe above stays at normal priority.
	cmd := exec.CommandContext(ctx, "nice",
		append([]string{"-n", "19", pythonPath}, "tools/stream_whisper.py", tmpWAV)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd, syscall.SIGKILL) }

	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			log.Printf("[whisper] %s timed out after %v", key, whisperMax)
		}
		job.set("error", "python error: "+err.Error())
		return
	}

	// First line is {"lang": "..."} — parse and skip
	lines := strings.Split(string(out), "\n")
	start := 0
	var lang string
	if len(lines) > 0 {
		var langObj struct {
			Lang string `json:"lang"`
		}
		if err := json.Unmarshal([]byte(lines[0]), &langObj); err == nil {
			lang = langObj.Lang
		}
		start = 1
	}

	var segs []whisperSeg
	for _, line := range lines[start:] {
		if line == "" {
			continue
		}
		var seg struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Text  string  `json:"text"`
		}
		if err := json.Unmarshal([]byte(line), &seg); err != nil {
			continue
		}
		segs = append(segs, whisperSeg{seg.Start, seg.End, seg.Text})
	}

	if lang != "en" && lang != "" && len(segs) > 0 {
		log.Printf("[whisper] runJob lang=%q, translating %d segments via Ollama", lang, len(segs))
		texts := make([]string, len(segs))
		for i, s := range segs {
			texts[i] = s.Text
		}
		if translated, err := TranslateSegments(texts, lang, ShowGlossary(src)); err == nil {
			for i := range segs {
				segs[i].Text = translated[i]
			}
		} else {
			log.Printf("[whisper] runJob translation failed: %v — saving raw", err)
		}
	}

	vtt := whisperVTT(segs)

	if err := os.WriteFile(dst, vtt, 0644); err != nil {
		job.set("error", "failed to write vtt: "+err.Error())
		return
	}
	job.set("done", "")
}

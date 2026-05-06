package jobs

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"notflix/server/library"
)

const pythonPath = "/opt/homebrew/Caskroom/miniconda/base/envs/global/bin/python"

var (
	activeProcs sync.Map
	whisperJobs sync.Map
)

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

func fmtSecs(secs float64) string {
	return fmtDur(time.Duration(secs * float64(time.Second)))
}

func checkFasterWhisper() bool {
	cmd := exec.Command(pythonPath, "-c", "import faster_whisper")
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[whisper] faster-whisper check failed: %v — %s", err, string(out))
		return false
	}
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
	if stderr, err := library.FF("-y", "-i", src, "-ar", "16000", "-ac", "1", tmpPath); err != nil {
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

	if old, ok := activeProcs.Load(raw); ok {
		log.Printf("[whisper] killing existing process for %q", raw)
		old.(*exec.Cmd).Process.Kill()
		activeProcs.Delete(raw)
	}

	tmpWAV, err := extractToWAV(path)
	if err != nil {
		log.Printf("[whisper] audio extraction failed: %v", err)
		sendEvent(map[string]string{"error": err.Error()})
		return
	}
	defer os.Remove(tmpWAV)

	modelName := c.DefaultQuery("model", "base")
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

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		log.Printf("[whisper] python start error: %v", err)
		sendEvent(map[string]string{"error": "failed to start python: " + err.Error()})
		return
	}
	log.Printf("[whisper] python pid=%d", cmd.Process.Pid)

	activeProcs.Store(raw, cmd)
	defer activeProcs.Delete(raw)

	scanner := bufio.NewScanner(stdout)

	// First line from Python: {"lang": "ja"} — capture detected language
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
	log.Printf("[whisper] scanner done, %d segments read, stderr: %s", len(segs), stderrBuf.String())

	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		log.Printf("[whisper] python wait error: %v | stderr: %s", err, stderrBuf.String())
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
		if translated, err := TranslateSegments(texts, lang); err == nil {
			for i := range segs {
				segs[i].Text = translated[i]
			}
		} else {
			log.Printf("[whisper] translation failed: %v — saving raw", err)
		}
	}

	var sb bytes.Buffer
	sb.WriteString("WEBVTT\n\n")
	for _, s := range segs {
		sb.WriteString(fmtSecs(s.Start) + " --> " + fmtSecs(s.End) + "\n")
		sb.WriteString(s.Text + "\n\n")
	}

	vttPath := whisperVTTOf(path)
	if err := os.WriteFile(vttPath, sb.Bytes(), 0644); err != nil {
		log.Printf("[whisper] failed to write vtt: %v", err)
	} else {
		log.Printf("[whisper] vtt saved: %s (%d bytes)", vttPath, sb.Len())
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

	if existing, loaded := whisperJobs.Load(body.File); loaded {
		job := existing.(*WhisperJob)
		status, msg := job.read()
		if status == "pending" {
			c.JSON(http.StatusOK, gin.H{"status": "pending"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": status, "error": msg})
		return
	}

	job := &WhisperJob{Status: "pending"}
	whisperJobs.Store(body.File, job)
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

	cmd := exec.Command(pythonPath, "tools/stream_whisper.py", tmpWAV)
	out, err := cmd.Output()
	if err != nil {
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
		if translated, err := TranslateSegments(texts, lang); err == nil {
			for i := range segs {
				segs[i].Text = translated[i]
			}
		} else {
			log.Printf("[whisper] runJob translation failed: %v — saving raw", err)
		}
	}

	var sb bytes.Buffer
	sb.WriteString("WEBVTT\n\n")
	for _, s := range segs {
		sb.WriteString(fmtSecs(s.Start) + " --> " + fmtSecs(s.End) + "\n")
		sb.WriteString(s.Text + "\n\n")
	}

	if err := os.WriteFile(dst, sb.Bytes(), 0644); err != nil {
		job.set("error", "failed to write vtt: "+err.Error())
		return
	}
	job.set("done", "")
}

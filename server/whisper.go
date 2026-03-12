package server

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/ggerganov/whisper.cpp/bindings/go/pkg/whisper"
	"github.com/gin-gonic/gin"
)

var (
	wModel whisper.Model
	wOnce  sync.Once
	wErr   error
	jobs   sync.Map
)

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

func loadModel() (whisper.Model, error) {
	wOnce.Do(func() {
		p := os.Getenv("WHISPER_MODEL")
		if p == "" {
			wErr = fmt.Errorf("WHISPER_MODEL env var not set")
			return
		}
		m, err := whisper.New(p)
		if err != nil {
			wErr = err
			return
		}
		wModel = m
	})

	return wModel, wErr
}

func SubsWhisper(c *gin.Context, roots []string) {
	var body struct {
		File string `json:"file"`
	}

	if err := c.ShouldBindJSON(&body); err != nil || body.File == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	path, ok := findVid(body.File, roots)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return
	}

	if existing, loaded := jobs.Load(body.File); loaded {
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
	jobs.Store(body.File, job)
	go runJob(path, whisperVTTOf(path), body.File, job)

	c.JSON(http.StatusOK, gin.H{"status": "pending"})
}

func SubsWhisperStatus(c *gin.Context) {
	raw := c.Query("file")
	if raw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file param"})
		return
	}

	existing, loaded := jobs.Load(raw)
	if !loaded {
		c.JSON(http.StatusOK, gin.H{"status": "not_started"})
		return
	}

	job := existing.(*WhisperJob)
	status, msg := job.read()
	c.JSON(http.StatusOK, gin.H{"status": status, "error": msg})
}

func runJob(src, dst, key string, job *WhisperJob) {
	model, err := loadModel()
	if err != nil {
		job.set("error", "failed to load model: "+err.Error())
		return
	}

	cmd := exec.Command("ffmpeg", "-y", "-i", src, "-ar", "16000", "-ac", "1", "-f", "f32le", "pipe:1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		job.set("error", "ffmpeg pipe error: "+err.Error())
		return
	}
	if err := cmd.Start(); err != nil {
		job.set("error", "ffmpeg start error: "+err.Error())
		return
	}

	raw, err := io.ReadAll(stdout)
	werr := cmd.Wait()
	if err == nil {
		err = werr
	}
	if err != nil {
		job.set("error", fmt.Sprintf("ffmpeg error: %v — %s", err, stderr.String()))
		return
	}

	if len(raw)%4 != 0 {
		raw = raw[:len(raw)-(len(raw)%4)]
	}
	samples := make([]float32, len(raw)/4)
	if err := binary.Read(bytes.NewReader(raw), binary.LittleEndian, &samples); err != nil {
		job.set("error", "audio decode error: "+err.Error())
		return
	}

	ctx, err := model.NewContext()
	if err != nil {
		job.set("error", "whisper context error: "+err.Error())
		return
	}
	ctx.SetTranslate(true)
	ctx.SetMaxSegmentLength(42)
	ctx.SetSplitOnWord(true)

	var segs []whisper.Segment
	var mu sync.Mutex
	onSeg := func(seg whisper.Segment) {
		mu.Lock()
		segs = append(segs, seg)
		mu.Unlock()
	}

	if err := ctx.Process(samples, nil, onSeg, nil); err != nil {
		job.set("error", "whisper process error: "+err.Error())
		return
	}

	var sb bytes.Buffer
	sb.WriteString("WEBVTT\n\n")
	for _, seg := range segs {
		sb.WriteString(fmtDur(seg.Start))
		sb.WriteString(" --> ")
		sb.WriteString(fmtDur(seg.End))
		sb.WriteString("\n")
		sb.WriteString(seg.Text)
		sb.WriteString("\n\n")
	}

	if err := os.WriteFile(dst, sb.Bytes(), 0644); err != nil {
		job.set("error", "failed to write vtt: "+err.Error())
		return
	}

	job.set("done", "")
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

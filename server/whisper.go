//go:build whisper

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

// ─── package-level state ──────────────────────────────────────────────────────

var (
	whisperModel     whisper.Model
	whisperModelOnce sync.Once
	whisperModelErr  error
	whisperJobs      sync.Map // key: raw file string → *WhisperJob
)

// WhisperJob tracks the status of an async transcription.
type WhisperJob struct {
	mu     sync.Mutex
	Status string // "pending" | "done" | "error"
	Err    string
}

func (j *WhisperJob) set(status, errMsg string) {
	j.mu.Lock()
	j.Status = status
	j.Err = errMsg
	j.mu.Unlock()
}

func (j *WhisperJob) read() (string, string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.Status, j.Err
}

// ─── model loading ────────────────────────────────────────────────────────────

func loadWhisperModel() (whisper.Model, error) {
	whisperModelOnce.Do(func() {
		path := os.Getenv("WHISPER_MODEL")
		if path == "" {
			whisperModelErr = fmt.Errorf("WHISPER_MODEL env var not set")
			return
		}
		m, err := whisper.New(path)
		if err != nil {
			whisperModelErr = err
			return
		}
		whisperModel = m
	})
	return whisperModel, whisperModelErr
}

// ─── SubsWhisper ─────────────────────────────────────────────────────────────

func SubsWhisper(c *gin.Context, roots []string) {
	var body struct {
		File string `json:"file"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.File == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	videoPath, ok := findVideoPath(body.File, roots)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return
	}

	// If job already running, return current status
	if existing, loaded := whisperJobs.Load(body.File); loaded {
		job := existing.(*WhisperJob)
		status, errMsg := job.read()
		if status == "pending" {
			c.JSON(http.StatusOK, gin.H{"status": "pending"})
			return
		}
		// Finished previously — return status but allow re-run if needed
		c.JSON(http.StatusOK, gin.H{"status": status, "error": errMsg})
		return
	}

	job := &WhisperJob{Status: "pending"}
	whisperJobs.Store(body.File, job)

	vttPath := videoToWhisperVTTPath(videoPath)
	go runWhisperJob(videoPath, vttPath, body.File, job)

	c.JSON(http.StatusOK, gin.H{"status": "pending"})
}

// ─── SubsWhisperStatus ────────────────────────────────────────────────────────

func SubsWhisperStatus(c *gin.Context) {
	rawFile := c.Query("file")
	if rawFile == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file param"})
		return
	}

	existing, loaded := whisperJobs.Load(rawFile)
	if !loaded {
		c.JSON(http.StatusOK, gin.H{"status": "not_started"})
		return
	}

	job := existing.(*WhisperJob)
	status, errMsg := job.read()
	c.JSON(http.StatusOK, gin.H{"status": status, "error": errMsg})
}

// ─── runWhisperJob ────────────────────────────────────────────────────────────

func runWhisperJob(videoPath, vttPath, key string, job *WhisperJob) {
	model, err := loadWhisperModel()
	if err != nil {
		job.set("error", "failed to load model: "+err.Error())
		return
	}

	// Extract audio with ffmpeg: 16 kHz, mono, 32-bit float PCM → stdout
	cmd := exec.Command("ffmpeg",
		"-y",
		"-i", videoPath,
		"-ar", "16000",
		"-ac", "1",
		"-f", "f32le",
		"pipe:1",
	)
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
	if waitErr := cmd.Wait(); waitErr != nil && err == nil {
		err = waitErr
	}
	if err != nil {
		job.set("error", fmt.Sprintf("ffmpeg error: %v — %s", err, stderr.String()))
		return
	}

	// Convert []byte to []float32 (little-endian f32le)
	if len(raw)%4 != 0 {
		raw = raw[:len(raw)-(len(raw)%4)]
	}
	samples := make([]float32, len(raw)/4)
	if err := binary.Read(bytes.NewReader(raw), binary.LittleEndian, &samples); err != nil {
		job.set("error", "audio decode error: "+err.Error())
		return
	}

	// Create whisper context
	ctx, err := model.NewContext()
	if err != nil {
		job.set("error", "whisper context error: "+err.Error())
		return
	}
	if err := ctx.SetLanguage("en"); err != nil {
		// Non-fatal: some models auto-detect
		_ = err
	}

	// Collect segments via callback
	var segments []whisper.Segment
	var mu sync.Mutex

	segCallback := func(seg whisper.Segment) {
		mu.Lock()
		segments = append(segments, seg)
		mu.Unlock()
	}

	if err := ctx.Process(samples, nil, segCallback, nil); err != nil {
		job.set("error", "whisper process error: "+err.Error())
		return
	}

	// Build WebVTT content
	var sb bytes.Buffer
	sb.WriteString("WEBVTT\n\n")
	for _, seg := range segments {
		sb.WriteString(formatDuration(seg.Start))
		sb.WriteString(" --> ")
		sb.WriteString(formatDuration(seg.End))
		sb.WriteString("\n")
		sb.WriteString(seg.Text)
		sb.WriteString("\n\n")
	}

	if err := os.WriteFile(vttPath, sb.Bytes(), 0644); err != nil {
		job.set("error", "failed to write vtt: "+err.Error())
		return
	}

	job.set("done", "")
}

// formatDuration formats a time.Duration as HH:MM:SS.mmm for WebVTT.
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	ms := int(d.Milliseconds()) % 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}

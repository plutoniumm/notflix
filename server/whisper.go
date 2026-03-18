package server

import (
	"bytes"
	"encoding/binary"
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
	"time"

	"github.com/ggerganov/whisper.cpp/bindings/go/pkg/whisper"
	"github.com/gin-gonic/gin"
)

var (
	wModel  whisper.Model
	wOnce   sync.Once
	wErr    error
	jobs    sync.Map
	wProcMu sync.Mutex
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

	wProcMu.Lock()
	procErr := ctx.Process(samples, nil, onSeg, nil)
	wProcMu.Unlock()
	if procErr != nil {
		job.set("error", "whisper process error: "+procErr.Error())
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

func SubAll(roots []string) {
	var videos []string
	for _, root := range roots {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}

			if strings.ToLower(filepath.Ext(path)) != ".mp4" {
				return nil
			}

			base := path[:len(path)-len(filepath.Ext(path))]
			if _, e := os.Stat(base + ".vtt"); e == nil {
				return nil
			}

			if _, e := os.Stat(base + ".whisper.vtt"); e == nil {
				return nil
			}
			videos = append(videos, path)

			return nil
		})
	}

	log.Printf("[SubAll] %d videos to transcribe", len(videos))

	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for _, v := range videos {
		v := v
		wg.Add(1)

		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			dst := whisperVTTOf(v)
			log.Printf("[SubAll] start %s", filepath.Base(v))

			if err := transcribeChunked(v, dst); err != nil {
				log.Printf("[SubAll] error %s: %v", filepath.Base(v), err)
			} else {
				log.Printf("[SubAll] done %s", filepath.Base(v))
			}
		}()
	}

	wg.Wait()
	log.Printf("[SubAll] finished")
}

func transcribeChunked(src, dst string) error {
	model, err := loadModel()
	if err != nil {
		return err
	}

	out, err := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		src,
	).Output()

	if err != nil {
		return fmt.Errorf("ffprobe: %w", err)
	}

	var totalDur float64
	fmt.Sscanf(strings.TrimSpace(string(out)), "%f", &totalDur)
	if totalDur <= 0 {
		return fmt.Errorf("invalid duration")
	}

	const nChunks = 3
	chunkDur := totalDur / float64(nChunks)

	type chunkResult struct {
		segs []whisper.Segment
		err  error
	}
	results := make([]chunkResult, nChunks)
	var wg sync.WaitGroup

	for i := 0; i < nChunks; i++ {
		i := i
		start := float64(i) * chunkDur
		dur := chunkDur
		if i == nChunks-1 {
			dur = totalDur - start
		}

		wg.Add(1)
		go func() {
			defer wg.Done()

			samples, err := extractAudioChunk(src, start, dur)
			if err != nil {
				results[i] = chunkResult{err: err}
				return
			}

			ctx, err := model.NewContext()
			if err != nil {
				results[i] = chunkResult{err: err}
				return
			}

			ctx.SetTranslate(true)
			ctx.SetMaxSegmentLength(42)
			ctx.SetSplitOnWord(true)

			offset := time.Duration(float64(time.Second) * start)
			var segs []whisper.Segment
			wProcMu.Lock()
			err = ctx.Process(samples, nil, func(seg whisper.Segment) {
				seg.Start += offset
				seg.End += offset
				segs = append(segs, seg)
			}, nil)

			wProcMu.Unlock()
			results[i] = chunkResult{segs: segs, err: err}
		}()
	}

	wg.Wait()

	var sb bytes.Buffer
	sb.WriteString("WEBVTT\n\n")
	for i, r := range results {
		if r.err != nil {
			return fmt.Errorf("chunk %d: %w", i, r.err)
		}

		for _, seg := range r.segs {
			sb.WriteString(fmtDur(seg.Start))
			sb.WriteString(" --> ")
			sb.WriteString(fmtDur(seg.End))
			sb.WriteString("\n")
			sb.WriteString(strings.TrimSpace(seg.Text))
			sb.WriteString("\n\n")
		}
	}

	return os.WriteFile(dst, sb.Bytes(), 0644)
}

func extractAudioChunk(src string, start, dur float64) ([]float32, error) {
	cmd := exec.Command("ffmpeg",
		"-ss", fmt.Sprintf("%.3f", start),
		"-t", fmt.Sprintf("%.3f", dur),
		"-i", src,
		"-ar", "16000",
		"-ac", "1",
		"-f", "f32le",
		"pipe:1",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	raw, err := io.ReadAll(stdout)
	werr := cmd.Wait()
	if err == nil {
		err = werr
	}
	if err != nil {
		return nil, fmt.Errorf("ffmpeg: %v — %s", err, stderr.String())
	}

	if len(raw)%4 != 0 {
		raw = raw[:len(raw)-(len(raw)%4)]
	}
	samples := make([]float32, len(raw)/4)
	if err := binary.Read(bytes.NewReader(raw), binary.LittleEndian, &samples); err != nil {
		return nil, err
	}

	return samples, nil
}

func SubsWhisperStream(c *gin.Context, roots []string) {
	raw := c.Query("file")
	if raw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file param"})
		return
	}

	path, ok := findVid(raw, roots)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return
	}

	model, err := loadModel()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "model: " + err.Error()})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		fmt.Fprintf(c.Writer, "data: {\"error\":\"streaming unsupported\"}\n\n")
		return
	}

	sendErr := func(msg string) {
		ev, _ := json.Marshal(map[string]string{"error": msg})
		fmt.Fprintf(c.Writer, "data: %s\n\n", ev)
		flusher.Flush()
	}

	// Extract full audio
	cmd := exec.Command("ffmpeg", "-y", "-i", path, "-ar", "16000", "-ac", "1", "-f", "f32le", "pipe:1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		sendErr("pipe error")
		return
	}

	if err := cmd.Start(); err != nil {
		sendErr("ffmpeg start error")
		return
	}

	audioRaw, err := io.ReadAll(stdout)
	werr := cmd.Wait()
	if err == nil {
		err = werr
	}

	if err != nil {
		sendErr(fmt.Sprintf("ffmpeg error: %v", err))
		return
	}

	if len(audioRaw)%4 != 0 {
		audioRaw = audioRaw[:len(audioRaw)-(len(audioRaw)%4)]
	}

	samples := make([]float32, len(audioRaw)/4)
	if err := binary.Read(bytes.NewReader(audioRaw), binary.LittleEndian, &samples); err != nil {
		sendErr("decode error")
		return
	}

	ctx, err := model.NewContext()
	if err != nil {
		sendErr("context error")
		return
	}
	ctx.SetTranslate(true)
	ctx.SetMaxSegmentLength(42)
	ctx.SetSplitOnWord(true)

	segCh := make(chan whisper.Segment, 64)
	doneCh := make(chan error, 1)
	go func() {
		wProcMu.Lock()
		err := ctx.Process(samples, nil, func(seg whisper.Segment) {
			segCh <- seg
		}, nil)
		wProcMu.Unlock()
		close(segCh)
		doneCh <- err
	}()

	var sb bytes.Buffer
	sb.WriteString("WEBVTT\n\n")

	for seg := range segCh {
		text := strings.TrimSpace(seg.Text)
		ev, _ := json.Marshal(map[string]any{
			"start": seg.Start.Seconds(),
			"end":   seg.End.Seconds(),
			"text":  text,
		})
		fmt.Fprintf(c.Writer, "data: %s\n\n", ev)
		flusher.Flush()

		sb.WriteString(fmtDur(seg.Start))
		sb.WriteString(" --> ")
		sb.WriteString(fmtDur(seg.End))
		sb.WriteString("\n")
		sb.WriteString(text)
		sb.WriteString("\n\n")
	}

	if err := <-doneCh; err != nil {
		sendErr("process error: " + err.Error())
		return
	}

	_ = os.WriteFile(whisperVTTOf(path), sb.Bytes(), 0644)
	fmt.Fprintf(c.Writer, "data: {\"done\":true}\n\n")
	flusher.Flush()
}

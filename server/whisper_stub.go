//go:build !whisper

package server

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
)

// whisperJobs is still needed for SubsWhisperStatus even in stub mode.
var whisperJobs sync.Map // key: raw file string → *WhisperJob

// WhisperJob tracks the status of an async transcription.
type WhisperJob struct {
	mu     sync.Mutex
	Status string
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

// SubsWhisper returns an error indicating whisper is not compiled in.
func SubsWhisper(c *gin.Context, roots []string) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": "whisper support not compiled in — rebuild with -tags whisper (requires whisper.cpp installed)",
	})
}

// SubsWhisperStatus returns the current job status, or not_started.
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

package library

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestPipelineRecover confirms PipelineInit treats any persisted "running"
// step as interrupted: the step is dropped from the ledger and returned so
// the caller can clean up partial output. "done" and "failed" survive.
func TestPipelineRecover(t *testing.T) {
	dir := t.TempDir()
	orig := plFilePath
	plFilePath = filepath.Join(dir, "pipeline.json")
	t.Cleanup(func() { plFilePath = orig; plMap = map[string]*FilePipeline{}; plRecover = nil })

	seed := map[string]*FilePipeline{
		"/a.mkv": {Steps: map[string]StepState{"convert": StepRunning}, Updated: 1},
		"/b.mp4": {Steps: map[string]StepState{"convert": StepDone, "sprite": StepRunning}, Updated: 2},
		"/c.mp4": {Steps: map[string]StepState{"whisper": StepDone}, Updated: 3},
	}
	data, _ := json.Marshal(seed)
	if err := os.WriteFile(plFilePath, data, 0644); err != nil {
		t.Fatal(err)
	}

	rec := PipelineInit()
	if len(rec) != 2 {
		t.Fatalf("expected 2 recovered, got %d: %+v", len(rec), rec)
	}

	got := PipelineStep("/a.mkv", "convert")
	if got != "" {
		t.Errorf("interrupted convert should be cleared, got %q", got)
	}
	got = PipelineStep("/b.mp4", "convert")
	if got != StepDone {
		t.Errorf("done convert must survive, got %q", got)
	}
	got = PipelineStep("/b.mp4", "sprite")
	if got != "" {
		t.Errorf("interrupted sprite should be cleared, got %q", got)
	}
	got = PipelineStep("/c.mp4", "whisper")
	if got != StepDone {
		t.Errorf("done whisper must survive, got %q", got)
	}
}

func TestPipelineMarkAndForget(t *testing.T) {
	dir := t.TempDir()
	orig := plFilePath
	plFilePath = filepath.Join(dir, "pipeline.json")
	t.Cleanup(func() { plFilePath = orig; plMap = map[string]*FilePipeline{}; plRecover = nil })

	PipelineMark("/x.mkv", "convert", StepDone)
	if s := PipelineStep("/x.mkv", "convert"); s != StepDone {
		t.Fatalf("expected done, got %q", s)
	}
	PipelineForget("/x.mkv")
	if s := PipelineStep("/x.mkv", "convert"); s != "" {
		t.Fatalf("expected empty after forget, got %q", s)
	}
}

package library

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// StepState is a step's lifecycle. A missing entry means "never started"; a
// "running" that survives a restart is interrupted and reset to missing.
type StepState string

const (
	StepRunning StepState = "running"
	StepDone    StepState = "done"
	StepFailed  StepState = "failed"
)

type FilePipeline struct {
	Steps   map[string]StepState `json:"steps"`
	Updated int64                `json:"t,omitempty"` // unix seconds
}

var (
	plMu       sync.RWMutex
	plMap      = map[string]*FilePipeline{}
	plFilePath = "./cache/pipeline.json"
	plDirty    atomic.Bool
	plRecover  []PipelineRecoverEntry
)

// PipelineRecoverEntry is a step that was mid-flight when the process exited;
// callers delete its stale .tmp output before the redo lands.
type PipelineRecoverEntry struct {
	Path string
	Step string
}

// PipelineInit loads the ledger, resets any surviving "running" step to
// missing (interrupted → redo), and starts the periodic flush.
func PipelineInit() []PipelineRecoverEntry {
	if data, err := os.ReadFile(plFilePath); err == nil {
		var m map[string]*FilePipeline
		if err := json.Unmarshal(data, &m); err == nil && m != nil {
			plMap = m
			log.Printf("[pipeline] loaded %d file entries", len(m))
		} else {
			log.Printf("[pipeline] ledger unreadable, starting fresh: %v", err)
		}
	}

	var recover []PipelineRecoverEntry
	plMu.Lock()
	for path, st := range plMap {
		if st.Steps == nil {
			continue
		}
		for step, s := range st.Steps {
			if s == StepRunning {
				delete(st.Steps, step)
				recover = append(recover, PipelineRecoverEntry{Path: path, Step: step})
			}
		}
	}
	plMu.Unlock()
	if len(recover) > 0 {
		log.Printf("[pipeline] recovered %d interrupted step(s)", len(recover))
		plDirty.Store(true)
	}
	plRecover = recover
	go pipelineSaver()
	return recover
}

func PipelineRecovered() []PipelineRecoverEntry { return plRecover }

func PipelineMark(path, step string, s StepState) {
	plMu.Lock()
	st, ok := plMap[path]
	if !ok {
		st = &FilePipeline{Steps: map[string]StepState{}}
		plMap[path] = st
	}
	if st.Steps == nil {
		st.Steps = map[string]StepState{}
	}
	st.Steps[step] = s
	st.Updated = time.Now().Unix()
	plMu.Unlock()
	plDirty.Store(true)
}

func PipelineStep(path, step string) StepState {
	plMu.RLock()
	defer plMu.RUnlock()
	if st, ok := plMap[path]; ok && st.Steps != nil {
		return st.Steps[step]
	}
	return ""
}

// PipelineForget drops path's ledger; call on delete/rename so stale keys
// don't pile up.
func PipelineForget(path string) {
	plMu.Lock()
	delete(plMap, path)
	plMu.Unlock()
	plDirty.Store(true)
}

// PipelineSnapshot returns a deep copy for read-only use.
func PipelineSnapshot() map[string]*FilePipeline {
	plMu.RLock()
	defer plMu.RUnlock()
	out := make(map[string]*FilePipeline, len(plMap))
	for k, v := range plMap {
		steps := make(map[string]StepState, len(v.Steps))
		for sk, sv := range v.Steps {
			steps[sk] = sv
		}
		out[k] = &FilePipeline{Steps: steps, Updated: v.Updated}
	}
	return out
}

func pipelineSaver() {
	for {
		time.Sleep(30 * time.Second)
		if plDirty.CompareAndSwap(true, false) {
			pipelineFlush()
		}
	}
}

func pipelineFlush() {
	plMu.RLock()
	data, err := json.MarshalIndent(plMap, "", "  ")
	plMu.RUnlock()
	if err != nil {
		log.Printf("[pipeline] marshal failed: %v", err)
		return
	}
	tmp := plFilePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		log.Printf("[pipeline] save failed: %v", err)
		plDirty.Store(true)
		return
	}
	if err := os.Rename(tmp, plFilePath); err != nil {
		log.Printf("[pipeline] rename failed: %v", err)
		plDirty.Store(true)
	}
}

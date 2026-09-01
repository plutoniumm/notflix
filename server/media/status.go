package media

import (
	"sync"
	"time"
)

// PhaseState is the lifecycle of a single ProcessAll phase.
type PhaseState string

const (
	PhasePending PhaseState = "pending"
	PhaseRunning PhaseState = "running"
	PhaseDone    PhaseState = "done"
)

type PhaseInfo struct {
	Name    string     `json:"name"`
	State   PhaseState `json:"state"`
	Started int64      `json:"startedAt,omitempty"` // unix millis
	Ended   int64      `json:"endedAt,omitempty"`
	Total   int        `json:"total,omitempty"` // 0 = unknown
	Done    int        `json:"done"`
	Note    string     `json:"note,omitempty"` // free-form current-activity text
}

// ProcessStatus is the snapshot the UI polls. Phases stay in fixed order so
// the timeline is stable across renders.
type ProcessStatus struct {
	Running bool        `json:"running"`
	Started int64       `json:"startedAt,omitempty"`
	Ended   int64       `json:"endedAt,omitempty"`
	Phases  []PhaseInfo `json:"phases"`
}

var phaseOrder = []string{"convert", "clean", "scan", "repair", "thumbs", "sprites", "whisper", "blurbs", "skips"}

var (
	statusMu sync.Mutex
	status   = ProcessStatus{Phases: freshPhases()}
)

func freshPhases() []PhaseInfo {
	out := make([]PhaseInfo, 0, len(phaseOrder))
	for _, n := range phaseOrder {
		out = append(out, PhaseInfo{Name: n, State: PhasePending})
	}
	return out
}

// StatusReset marks a new ProcessAll run. Called at the start of every run so
// the previous run's phase timings don't linger in the UI mid-cycle.
func StatusReset() {
	statusMu.Lock()
	defer statusMu.Unlock()
	status = ProcessStatus{
		Running: true,
		Started: time.Now().UnixMilli(),
		Phases:  freshPhases(),
	}
}

func StatusFinish() {
	statusMu.Lock()
	defer statusMu.Unlock()
	status.Running = false
	status.Ended = time.Now().UnixMilli()
}

func phaseIdx(name string) int {
	for i, p := range status.Phases {
		if p.Name == name {
			return i
		}
	}
	return -1
}

func PhaseStart(name string) {
	statusMu.Lock()
	defer statusMu.Unlock()
	if i := phaseIdx(name); i >= 0 {
		status.Phases[i].State = PhaseRunning
		status.Phases[i].Started = time.Now().UnixMilli()
	}
}

func PhaseEnd(name string) {
	statusMu.Lock()
	defer statusMu.Unlock()
	if i := phaseIdx(name); i >= 0 {
		status.Phases[i].State = PhaseDone
		status.Phases[i].Ended = time.Now().UnixMilli()
		status.Phases[i].Note = ""
	}
}

// PhaseInc increments the total-seen and done-so-far counters. Pass 0 for
// either delta to leave it unchanged. Total may grow lazily as a walking
// stage discovers work; the UI renders "done / total" honestly either way.
func PhaseInc(name string, deltaTotal, deltaDone int) {
	statusMu.Lock()
	defer statusMu.Unlock()
	if i := phaseIdx(name); i >= 0 {
		status.Phases[i].Total += deltaTotal
		status.Phases[i].Done += deltaDone
	}
}

// PhaseSet is the absolute counterpart for phases that know their total up
// front (repair knows CorruptPaths() size before it starts).
func PhaseSet(name string, total, done int) {
	statusMu.Lock()
	defer statusMu.Unlock()
	if i := phaseIdx(name); i >= 0 {
		status.Phases[i].Total = total
		status.Phases[i].Done = done
	}
}

func PhaseNote(name, note string) {
	statusMu.Lock()
	defer statusMu.Unlock()
	if i := phaseIdx(name); i >= 0 {
		status.Phases[i].Note = note
	}
}

// GetStatus returns a deep-enough snapshot for JSON serialization. The
// caller mutates neither the returned struct nor the phase slice.
func GetStatus() ProcessStatus {
	statusMu.Lock()
	defer statusMu.Unlock()
	phases := make([]PhaseInfo, len(status.Phases))
	copy(phases, status.Phases)
	return ProcessStatus{
		Running: status.Running,
		Started: status.Started,
		Ended:   status.Ended,
		Phases:  phases,
	}
}

package jobs

import "sync"

type Progress struct {
	Name    string  `json:"name"`
	Percent float64 `json:"percent"`
}

type Tracker struct {
	mu    sync.Mutex
	items map[string]float64
}

func NewTracker() *Tracker {
	return &Tracker{items: make(map[string]float64)}
}

func (t *Tracker) Start(name string) {
	t.mu.Lock()
	t.items[name] = 0
	t.mu.Unlock()
}

func (t *Tracker) Update(name string, pct float64) {
	t.mu.Lock()
	t.items[name] = pct
	t.mu.Unlock()
}

func (t *Tracker) Finish(name string) {
	t.mu.Lock()
	delete(t.items, name)
	t.mu.Unlock()
}

func (t *Tracker) List() []Progress {
	t.mu.Lock()
	defer t.mu.Unlock()

	out := make([]Progress, 0, len(t.items))
	for name, pct := range t.items {
		out = append(out, Progress{Name: name, Percent: pct})
	}

	return out
}

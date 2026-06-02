// Package recorder captures and replays browser action sequences.
package recorder

import (
	"fmt"
	"sync"
	"time"
)

// Step is one captured tool call.
type Step struct {
	Index  int            `json:"index"`
	Tool   string         `json:"tool"`
	Params map[string]any `json:"params"`
}

// Scene is a complete recorded sequence.
type Scene struct {
	Version     int       `json:"version"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	EnvID       string    `json:"envId"`
	CreatedAt   time.Time `json:"createdAt"`
	Steps       []Step    `json:"steps"`
}

// Sane upper bound so a single scene stays manageable.
const MaxSteps = 200

// Status describes current recording state.
type Status struct {
	Recording bool   `json:"recording"`
	EnvID     string `json:"envId,omitempty"`
	StepCount int    `json:"stepCount"`
	ElapsedMs int64  `json:"elapsedMs"`
}

// Recorder is the global recording singleton.
type Recorder struct {
	mu        sync.Mutex
	running   bool
	envID     string
	steps     []Step
	startTime time.Time
}

var globalRec *Recorder

// Get returns the shared Recorder instance.
func Get() *Recorder {
	if globalRec == nil {
		globalRec = &Recorder{}
	}
	return globalRec
}

// Start begins recording for the given environment.
func (r *Recorder) Start(envID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return fmt.Errorf("recording already active for env %s", r.envID)
	}
	r.running = true
	r.envID = envID
	r.steps = nil
	r.startTime = time.Now()
	return nil
}

// Stop ends recording and returns the raw scene.
func (r *Recorder) Stop() (*Scene, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return nil, fmt.Errorf("no active recording")
	}
	r.running = false
	return &Scene{
		Version:   1,
		EnvID:     r.envID,
		CreatedAt: r.startTime,
		Steps:     r.steps,
	}, nil
}

// Status returns current recording state.
func (r *Recorder) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := Status{
		Recording: r.running,
		EnvID:     r.envID,
		StepCount: len(r.steps),
	}
	if r.running {
		st.ElapsedMs = time.Since(r.startTime).Milliseconds()
	}
	return st
}

// IsRecording quickly checks whether a recording is active.
func (r *Recorder) IsRecording() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// Capture records a tool call step. Silently drops when not recording.
func (r *Recorder) Capture(tool string, params map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return
	}
	// Copy the params map so future mutations don't affect stored steps.
	paramsCopy := make(map[string]any, len(params))
	for k, v := range params {
		paramsCopy[k] = v
	}
	step := Step{
		Index:  len(r.steps),
		Tool:   tool,
		Params: paramsCopy,
	}
	r.steps = append(r.steps, step)
}

// SanitizeParams returns a shallow copy of p with envId and sessionId removed.
// The original map is never modified.
func SanitizeParams(p map[string]any) map[string]any {
	out := make(map[string]any, len(p))
	for k, v := range p {
		if k == "envId" || k == "sessionId" {
			continue
		}
		out[k] = v
	}
	return out
}

// Package recorder captures and replays browser action sequences.
package recorder

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Step is one captured tool call.
type Step struct {
	Index        int                  `json:"index"`
	Tool         string               `json:"tool"`
	Params       map[string]any       `json:"params"`
	Fingerprint  *ElementFingerprint  `json:"fingerprint,omitempty"`
	WaitFor      *WaitGuard           `json:"waitFor,omitempty"`      // post-step condition guard
	HumanDelayMs int                  `json:"humanDelayMs,omitempty"` // recorded human pause (ms) before this action
}

// Scene is a complete recorded sequence.
type Scene struct {
	Version     int       `json:"version"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	Steps       []Step    `json:"steps"`
}

// Sane upper bound so a single scene stays manageable.
const MaxSteps = 200

// Status describes current recording state.
type Status struct {
	Recording bool  `json:"recording"`
	StepCount int   `json:"stepCount"`
	ElapsedMs int64 `json:"elapsedMs"`
}

// Recorder is the global recording singleton.
type Recorder struct {
	mu              sync.Mutex
	running         bool
	steps           []Step
	startTime       time.Time
	lastSnapshot    json.RawMessage
	lastCaptureTime time.Time // for HumanDelayMs tracking
}

var globalRec *Recorder

// Get returns the shared Recorder instance.
func Get() *Recorder {
	if globalRec == nil {
		globalRec = &Recorder{}
	}
	return globalRec
}

// Start begins recording. Caller is responsible for staying on one env.
func (r *Recorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return fmt.Errorf("recording already active")
	}
	r.running = true
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

// SetLastSnapshot caches the latest browser_snapshot result for _ref fingerprint extraction.
func (r *Recorder) SetLastSnapshot(raw json.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastSnapshot = raw
}

// Capture records a tool call step. Silently drops when not recording.
// For _ref tools, extracts a stable fingerprint from the cached snapshot
// so the replay can resolve the ref in a new browser session.
// Auto-infers a WaitFor guard based on the tool type and captures the
// inter-step human delay for natural-paced replay.
func (r *Recorder) Capture(tool string, params map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return
	}

	now := time.Now()

	// Copy the params map so future mutations don't affect stored steps.
	paramsCopy := make(map[string]any, len(params))
	for k, v := range params {
		paramsCopy[k] = v
	}

	// Compute human delay: time elapsed since the previous step capture.
	// The first step always has HumanDelayMs=0.
	var humanDelayMs int
	if !r.lastCaptureTime.IsZero() {
		humanDelayMs = int(now.Sub(r.lastCaptureTime).Milliseconds())
	}
	r.lastCaptureTime = now

	step := Step{
		Index:        len(r.steps),
		Tool:         tool,
		Params:       paramsCopy,
		WaitFor:      InferWaitFor(tool),
		HumanDelayMs: humanDelayMs,
	}

	// For _ref tools, attach a fingerprint so replay can find the element.
	if IsRefTool(tool) && len(r.lastSnapshot) > 0 {
		if ref, ok := paramsCopy["ref"].(string); ok && ref != "" {
			if node, err := FindNodeByRef(r.lastSnapshot, ref); err == nil {
				fp := FingerprintFromNode(node)
				step.Fingerprint = &fp
			}
		}
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

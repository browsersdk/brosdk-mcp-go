package recorder

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/browsersdk/brosdk-mcp-go/internal/brosdk"
)

// DispatchFunc is the signature for tool dispatch.
// Matches tools.Dispatch so Player can reuse the same switch.
type DispatchFunc func(mgr *brosdk.Manager, name string, params map[string]any) (string, error)

// StepResult is the replay outcome for a single step.
type StepResult struct {
	Index      int   `json:"index"`
	Tool       string `json:"tool"`
	Ok         bool   `json:"ok"`
	DurationMs int64  `json:"durationMs"`
	Result     string `json:"result,omitempty"`
	Error      string `json:"error,omitempty"`
}

// ReplayResult is the aggregate replay outcome.
type ReplayResult struct {
	Ok       bool         `json:"ok"`
	Steps    []StepResult `json:"steps"`
	Executed int          `json:"executed"`
	Failed   int          `json:"failed"`
}

// ReplayOptions controls replay behavior.
type ReplayOptions struct {
	EnvID          string
	Variables      map[string]string
	StopOnError    bool
	StepDelay      time.Duration
	ApplyHumanDelay bool // if true, insert recorded human pauses during replay (default true)
}

// Player replays scene steps through the dispatch function.
type Player struct {
	mgr      *brosdk.Manager
	dispatch DispatchFunc
	// guardFn allows tests to override the waitFor logic.
	// When nil (production), executeGuard is used directly.
	guardFn func(mgr *brosdk.Manager, envID string, g *WaitGuard) error
}

// NewPlayer creates a Player that routes steps through dispatch.
func NewPlayer(mgr *brosdk.Manager, dispatch DispatchFunc) *Player {
	return &Player{mgr: mgr, dispatch: dispatch}
}

// Replay executes all steps in the scene and returns per-step results.
func (p *Player) Replay(scene *Scene, opts ReplayOptions) *ReplayResult {
	if opts.StepDelay <= 0 {
		opts.StepDelay = 500 * time.Millisecond
	}

	result := &ReplayResult{Ok: true}
	for _, step := range scene.Steps {
		sr := p.executeStep(step, opts)
		result.Steps = append(result.Steps, sr)
		if sr.Ok {
			result.Executed++
		} else {
			result.Failed++
			result.Ok = false
			if opts.StopOnError {
				break
			}
		}
	}
	return result
}

func (p *Player) executeStep(step Step, opts ReplayOptions) (sr StepResult) {
	start := time.Now()
	sr = StepResult{Index: step.Index, Tool: step.Tool}

	// Recover from panics (e.g., chromedp allocator crash on stale connection).
	defer func() {
		if rec := recover(); rec != nil {
			sr.Ok = false
			sr.Error = fmt.Sprintf("panic: %v", rec)
		}
		sr.DurationMs = time.Since(start).Milliseconds()
	}()

	// Clone params and inject envId.
	params := make(map[string]any, len(step.Params)+1)
	for k, v := range step.Params {
		params[k] = v
	}
	params["envId"] = opts.EnvID

	// Variable substitution.
	params = substituteVars(params, opts.Variables)

	// Resolve _ref via fingerprint — take a fresh snapshot and match the element.
	if step.Fingerprint != nil && IsRefTool(step.Tool) && p.mgr != nil {
		raw, snapErr := p.mgr.Snapshot(opts.EnvID, "")
		if snapErr == nil {
			if node, findErr := FindNodeByFingerprint(raw, *step.Fingerprint); findErr == nil {
				params["ref"] = strconv.Itoa(node.BackendDOMNodeID)
			}
		}
		// On failure: keep the original ref (will likely fail, but StepResult.Error will report it).
	}

	// Dispatch through the same switch as normal tool calls.
	resultText, err := p.dispatch(p.mgr, step.Tool, params)

	if err != nil {
		sr.Ok = false
		sr.Error = err.Error()
	} else {
		sr.Ok = true
		sr.Result = resultText
	}

	// WaitFor guard: block until the post-step condition is satisfied.
	// This is the primary reliability mechanism — it replaces blind sleep
	// with condition-based waiting (readyState, element existence, etc.).
	if sr.Ok && !step.WaitFor.IsNone() && p.mgr != nil {
		exec := p.guardFn
		if exec == nil {
			exec = executeGuard
		}
		if guardErr := exec(p.mgr, opts.EnvID, step.WaitFor); guardErr != nil {
			// Guard failure is a soft error: the step itself succeeded,
			// but the expected side effect didn't materialize. Log it as
			// an annotation so the caller can decide whether to stop.
			sr.Result = fmt.Sprintf(`%s{"guardWarn":%q}`, sr.Result, guardErr.Error())
		}
	}

	// Human delay: simulate the natural pause the human took between actions.
	// Capped at 3s to avoid unnaturally long waits on slow recordings.
	if sr.Ok && opts.ApplyHumanDelay && step.HumanDelayMs > 0 {
		d := step.HumanDelayMs
		if d > 3000 {
			d = 3000
		}
		time.Sleep(time.Duration(d) * time.Millisecond)
	}

	// Blind step delay (fallback). In practice the WaitFor guard already
	// covers most cases; this is a small safety margin.
	time.Sleep(opts.StepDelay)

	return sr
}

// substituteVars replaces {{key}} placeholders in string values (and only strings).
// Nested objects and arrays are recursed into.
func substituteVars(params map[string]any, variables map[string]string) map[string]any {
	if len(variables) == 0 {
		return params
	}
	out := make(map[string]any, len(params))
	for k, v := range params {
		out[k] = substituteValue(v, variables)
	}
	return out
}

func substituteValue(v any, variables map[string]string) any {
	switch val := v.(type) {
	case string:
		return substituteString(val, variables)
	case map[string]any:
		return substituteVars(val, variables)
	case []any:
		arr := make([]any, len(val))
		for i, item := range val {
			arr[i] = substituteValue(item, variables)
		}
		return arr
	default:
		return v
	}
}

func substituteString(s string, variables map[string]string) string {
	for key, value := range variables {
		placeholder := "{{" + key + "}}"
		s = strings.ReplaceAll(s, placeholder, value)
	}
	return s
}

// LoadScene reads a scene JSON file from disk.
func LoadScene(path string) (*Scene, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("scene not found: %w", err)
	}
	var scene Scene
	if err := json.Unmarshal(data, &scene); err != nil {
		return nil, fmt.Errorf("invalid scene JSON: %w", err)
	}
	return &scene, nil
}

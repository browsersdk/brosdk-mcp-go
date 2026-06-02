package recorder

import (
	"fmt"
	"time"

	"github.com/browsersdk/brosdk-mcp-go/internal/brosdk"
)

// WaitGuard describes a post-step condition that must be satisfied before
// the replay can proceed to the next step.  It is auto-inferred during
// recording (the human naturally waits for these conditions) and enforced
// during replay to handle AJAX, SPA rendering, and variable network latency.
type WaitGuard struct {
	// Type selects the guard strategy:
	//   "readyState" – poll PageState() until readyState matches
	//   "exists"     – poll Exists() until the element appears
	//   "wait"       – call Wait() (polls internally, blocks until match or timeout)
	//   "none"       – no wait (default for tools with no side effects)
	Type string `json:"type"`

	ReadyState string `json:"readyState,omitempty"` // "complete" or "interactive"
	Role       string `json:"role,omitempty"`
	Name       string `json:"name,omitempty"`
	Text       string `json:"text,omitempty"`
	TimeoutMs  int    `json:"timeoutMs,omitempty"` // default 10000
}

// InferWaitFor returns a sensible WaitGuard for the given tool.
// This is used during recording — the guard captures what the human was
// implicitly waiting for (page loads, DOM updates, etc.).
//
// Navigation tools → wait for readyState:complete.
// Click tools       → wait for readyState:complete (click may trigger nav).
// All others        → no guard.
func InferWaitFor(tool string) *WaitGuard {
	switch tool {
	case "browser_navigate", "browser_open":
		return &WaitGuard{Type: "readyState", ReadyState: "complete", TimeoutMs: 10000}
	case "browser_click", "browser_click_ref",
		"browser_back", "browser_forward", "browser_reload":
		return &WaitGuard{Type: "readyState", ReadyState: "complete", TimeoutMs: 5000}
	default:
		return &WaitGuard{Type: "none"}
	}
}

// IsNone reports whether the guard is a no-op.
func (g *WaitGuard) IsNone() bool {
	return g == nil || g.Type == "" || g.Type == "none"
}

func (g *WaitGuard) timeoutOrDefault() time.Duration {
	if g.TimeoutMs <= 0 {
		return 10 * time.Second
	}
	return time.Duration(g.TimeoutMs) * time.Millisecond
}

// executeGuard runs the waitFor guard against a live Manager.
// Returns nil when the condition is satisfied, or an error on timeout.
func executeGuard(mgr *brosdk.Manager, envID string, g *WaitGuard) error {
	if g.IsNone() || mgr == nil {
		return nil
	}

	switch g.Type {
	case "readyState":
		return guardReadyState(mgr, envID, g)
	case "exists":
		return guardExists(mgr, envID, g)
	case "wait":
		return guardWait(mgr, envID, g)
	default:
		return fmt.Errorf("unknown guard type: %q", g.Type)
	}
}

func guardReadyState(mgr *brosdk.Manager, envID string, g *WaitGuard) error {
	expected := g.ReadyState
	if expected == "" {
		expected = "complete"
	}
	deadline := time.Now().Add(g.timeoutOrDefault())

	for time.Now().Before(deadline) {
		ps, err := mgr.PageState(envID)
		if err == nil && ps.ReadyState == expected {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("guard readyState=%q: timed out after %s", expected, g.timeoutOrDefault())
}

func guardExists(mgr *brosdk.Manager, envID string, g *WaitGuard) error {
	deadline := time.Now().Add(g.timeoutOrDefault())

	for time.Now().Before(deadline) {
		found, err := mgr.Exists(envID, g.Role, g.Name, "")
		if err == nil && found {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("guard exists role=%q name=%q: timed out after %s", g.Role, g.Name, g.timeoutOrDefault())
}

func guardWait(mgr *brosdk.Manager, envID string, g *WaitGuard) error {
	return mgr.Wait(envID, g.Text, g.Role, g.Name, "", g.TimeoutMs)
}

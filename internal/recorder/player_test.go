// Package recorder — player unit tests (no browser required).
package recorder

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/browsersdk/brosdk-mcp-go/internal/brosdk"
)

// mockDispatch records each call so we can verify the player dispatched correctly.
func mockDispatch() (DispatchFunc, *[]struct {
	Tool   string
	Params map[string]any
}) {
	calls := make([]struct {
		Tool   string
		Params map[string]any
	}, 0)
	fn := func(mgr *brosdk.Manager, tool string, params map[string]any) (string, error) {
		// Verify envId was injected.
		if params["envId"] == nil || params["envId"] == "" {
			return "", fmt.Errorf("envId not injected")
		}
		calls = append(calls, struct {
			Tool   string
			Params map[string]any
		}{tool, params})
		return fmt.Sprintf(`{"ok":true,"tool":%q}`, tool), nil
	}
	return fn, &calls
}

// mockDispatchWithPanic returns a dispatch that panics for the given tool.
func mockDispatchWithPanic(panicTool, panicMsg string) (DispatchFunc, *int) {
	callCount := 0
	fn := func(mgr *brosdk.Manager, tool string, params map[string]any) (string, error) {
		callCount++
		if tool == panicTool {
			panic(panicMsg)
		}
		return `{"ok":true}`, nil
	}
	return fn, &callCount
}

func TestPlayer_Replay(t *testing.T) {
	dispatch, calls := mockDispatch()
	p := &Player{dispatch: dispatch}

	scene := &Scene{
		Version: 1,
		Name:    "test-flow",
		Steps: []Step{
			{Index: 0, Tool: "browser_navigate", Params: map[string]any{"url": "https://example.com"}},
			{Index: 1, Tool: "browser_click", Params: map[string]any{"selector": "#btn"}},
			{Index: 2, Tool: "browser_type", Params: map[string]any{"selector": "#input", "text": "hello"}},
		},
	}

	result := p.Replay(scene, ReplayOptions{
		EnvID:     "env-1",
		StepDelay: 1 * time.Millisecond, // minimal delay for unit test
	})

	if !result.Ok {
		t.Fatal("replay should succeed")
	}
	if result.Executed != 3 || result.Failed != 0 {
		t.Fatalf("expected 3 executed / 0 failed, got %d/%d", result.Executed, result.Failed)
	}
	if len(*calls) != 3 {
		t.Fatalf("expected 3 dispatch calls, got %d", len(*calls))
	}
	// Verify envId was injected into first step.
	if (*calls)[0].Params["envId"] != "env-1" {
		t.Fatalf("envId not injected in first step: %v", (*calls)[0].Params)
	}
}

func TestPlayer_VariableSubstitution(t *testing.T) {
	dispatch, calls := mockDispatch()
	p := &Player{dispatch: dispatch}

	scene := &Scene{
		Version: 1,
		Name:    "login",
		Steps: []Step{
			{Index: 0, Tool: "browser_type", Params: map[string]any{"selector": "#user", "text": "{{username}}"}},
			{Index: 1, Tool: "browser_type", Params: map[string]any{"selector": "#pass", "text": "{{password}}"}},
		},
	}

	result := p.Replay(scene, ReplayOptions{
		EnvID:     "env-1",
		Variables: map[string]string{"username": "alice", "password": "s3cret"},
		StepDelay: 1 * time.Millisecond,
	})

	if !result.Ok {
		t.Fatal("replay should succeed")
	}
	if len(*calls) != 2 {
		t.Fatalf("expected 2 dispatch calls, got %d", len(*calls))
	}
	if (*calls)[0].Params["text"] != "alice" {
		t.Fatalf("username not substituted: %v", (*calls)[0].Params["text"])
	}
	if (*calls)[1].Params["text"] != "s3cret" {
		t.Fatalf("password not substituted: %v", (*calls)[1].Params["text"])
	}
}

func TestPlayer_StopOnError(t *testing.T) {
	// Create a dispatch that fails on the second call.
	callCount := 0
	failDispatch := func(mgr *brosdk.Manager, tool string, params map[string]any) (string, error) {
		callCount++
		if callCount == 2 {
			return "", fmt.Errorf("simulated failure")
		}
		return `{"ok":true}`, nil
	}

	p := &Player{dispatch: failDispatch}
	scene := &Scene{
		Version: 1,
		Name:    "fail-test",
		Steps: []Step{
			{Index: 0, Tool: "browser_navigate", Params: map[string]any{"url": "https://example.com"}},
			{Index: 1, Tool: "browser_click", Params: map[string]any{"selector": "#btn"}},
			{Index: 2, Tool: "browser_type", Params: map[string]any{"text": "should not execute"}},
		},
	}

	result := p.Replay(scene, ReplayOptions{
		EnvID:       "env-1",
		StopOnError: true,
		StepDelay:   1 * time.Millisecond,
	})

	if result.Ok {
		t.Fatal("replay should report failure with stopOnError")
	}
	if callCount != 2 {
		t.Fatalf("expected 2 dispatch calls (stopped on error), got %d", callCount)
	}
	if result.Executed != 1 || result.Failed != 1 {
		t.Fatalf("expected 1 executed / 1 failed, got %d/%d", result.Executed, result.Failed)
	}
}

func TestPlayer_ContinueOnError(t *testing.T) {
	callCount := 0
	failDispatch := func(mgr *brosdk.Manager, tool string, params map[string]any) (string, error) {
		callCount++
		if callCount == 2 {
			return "", fmt.Errorf("simulated failure")
		}
		return `{"ok":true}`, nil
	}

	p := &Player{dispatch: failDispatch}
	scene := &Scene{
		Version: 1,
		Name:    "continue-test",
		Steps: []Step{
			{Index: 0, Tool: "browser_navigate", Params: map[string]any{"url": "https://example.com"}},
			{Index: 1, Tool: "browser_click", Params: map[string]any{"selector": "#btn"}},
			{Index: 2, Tool: "browser_type", Params: map[string]any{"text": "still executes"}},
		},
	}

	result := p.Replay(scene, ReplayOptions{
		EnvID:       "env-1",
		StopOnError: false,
		StepDelay:   1 * time.Millisecond,
	})

	if result.Ok {
		t.Fatal("replay should report failure because at least one step failed")
	}
	if callCount != 3 {
		t.Fatalf("expected 3 dispatch calls (continued after error), got %d", callCount)
	}
	if result.Executed != 2 || result.Failed != 1 {
		t.Fatalf("expected 2 executed / 1 failed, got %d/%d", result.Executed, result.Failed)
	}
}

func TestPlayer_EmptySteps(t *testing.T) {
	p := &Player{dispatch: func(mgr *brosdk.Manager, tool string, params map[string]any) (string, error) {
		return `{}`, nil
	}}
	scene := &Scene{Version: 1, Name: "empty"}
	result := p.Replay(scene, ReplayOptions{EnvID: "env-1"})
	if !result.Ok {
		t.Fatal("empty scene should be ok")
	}
	if result.Executed != 0 {
		t.Fatalf("expected 0 executed, got %d", result.Executed)
	}
}

func TestLoadScene(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	content := `{"version":1,"name":"test","createdAt":"2025-01-01T00:00:00Z","steps":[{"index":0,"tool":"browser_navigate","params":{"url":"https://example.com"}}]}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write test scene: %v", err)
	}

	scene, err := LoadScene(path)
	if err != nil {
		t.Fatalf("LoadScene: %v", err)
	}
	if scene.Name != "test" || len(scene.Steps) != 1 {
		t.Fatalf("scene mismatch: name=%s steps=%d", scene.Name, len(scene.Steps))
	}
}

func TestLoadScene_NotFound(t *testing.T) {
	_, err := LoadScene("/nonexistent/scene.json")
	if err == nil {
		t.Fatal("expected error for nonexistent scene")
	}
}

func TestLoadScene_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte("not json"), 0644)

	_, err := LoadScene(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSubstituteVars_Nested(t *testing.T) {
	params := map[string]any{
		"text":   "Hello {{name}}",
		"nested": map[string]any{"key": "{{value}}"},
		"list":   []any{"{{a}}", "{{b}}"},
		"number": 42,
	}
	out := substituteVars(params, map[string]string{
		"name":  "World",
		"value": "nested_val",
		"a":     "first",
		"b":     "second",
	})

	if out["text"] != "Hello World" {
		t.Fatalf("top-level substitution failed: %v", out["text"])
	}
	nested := out["nested"].(map[string]any)
	if nested["key"] != "nested_val" {
		t.Fatalf("nested substitution failed: %v", nested["key"])
	}
	lst := out["list"].([]any)
	if lst[0] != "first" || lst[1] != "second" {
		t.Fatalf("list substitution failed: %v", lst)
	}
	if out["number"] != 42 {
		t.Fatal("numbers should not be affected by substitution")
	}
}

func TestSubstituteVars_NoVars(t *testing.T) {
	params := map[string]any{"text": "{{unchanged}}", "num": 1}
	out := substituteVars(params, nil)
	if out["text"] != "{{unchanged}}" {
		t.Fatal("should not substitute when no variables provided")
	}
}

func TestPlayer_PanicRecovery(t *testing.T) {
	dispatch, calls := mockDispatchWithPanic("browser_navigate", "BOOM")
	p := &Player{dispatch: dispatch}

	scene := &Scene{
		Version: 1,
		Name:    "crash-test",
		Steps: []Step{
			{Index: 0, Tool: "browser_navigate", Params: map[string]any{"url": "https://example.com"}},
		},
	}

	result := p.Replay(scene, ReplayOptions{EnvID: "env-1", StepDelay: 1 * time.Millisecond})
	if result.Ok {
		t.Fatal("replay should report failure after panic")
	}
	if len(result.Steps) != 1 {
		t.Fatalf("expected 1 step result, got %d", len(result.Steps))
	}
	sr := result.Steps[0]
	if sr.Ok {
		t.Fatal("step should not be ok after panic")
	}
	if sr.Error == "" {
		t.Fatal("step should have error message after panic")
	}
	if result.Executed != 0 || result.Failed != 1 {
		t.Fatalf("expected 0 executed / 1 failed, got %d/%d", result.Executed, result.Failed)
	}
	_ = calls // dispatch was called and panicked, but callCount won't be incremented due to panic
}

func TestStepResult_Timing(t *testing.T) {
	p := &Player{dispatch: func(mgr *brosdk.Manager, tool string, params map[string]any) (string, error) {
		return `{"ok":true}`, nil
	}}
	scene := &Scene{
		Version: 1,
		Name:    "timing",
		Steps:   []Step{{Index: 0, Tool: "browser_navigate", Params: map[string]any{"url": "https://example.com"}}},
	}
	result := p.Replay(scene, ReplayOptions{EnvID: "env-1", StepDelay: 5 * time.Millisecond})
	if len(result.Steps) != 1 {
		t.Fatalf("expected 1 step result, got %d", len(result.Steps))
	}
	if !result.Steps[0].Ok {
		t.Fatal("step should be ok")
	}
	if result.Steps[0].DurationMs < 0 {
		t.Fatal("duration should be non-negative")
	}
}

// TestPlayer_RefKeepsFingerprint verifies that when a _ref step has no mgr
// (i.e. snapshot resolution is skipped), the original ref is dispatched as-is.
func TestPlayer_RefWithoutMgr(t *testing.T) {
	dispatch, calls := mockDispatch()
	p := &Player{dispatch: dispatch, mgr: nil} // no mgr → fingerprint resolution skipped

	fp := &ElementFingerprint{Role: "button", Name: "Login"}
	scene := &Scene{
		Version: 1,
		Name:    "ref-test",
		Steps: []Step{
			{Index: 0, Tool: "browser_click_ref", Params: map[string]any{"ref": "42"}, Fingerprint: fp},
		},
	}

	result := p.Replay(scene, ReplayOptions{EnvID: "env-1", StepDelay: 1 * time.Millisecond})
	if !result.Ok {
		t.Fatal("replay should succeed even without fingerprint resolution")
	}
	// The original ref should have been kept.
	if (*calls)[0].Params["ref"] != "42" {
		t.Fatalf("ref should stay '42' when mgr is nil, got %v", (*calls)[0].Params["ref"])
	}
}

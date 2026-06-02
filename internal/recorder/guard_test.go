// Package recorder — WaitGuard and HumanDelay unit tests.
package recorder

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/browsersdk/brosdk-mcp-go/internal/brosdk"
)

// ── InferWaitFor ──

func TestInferWaitFor(t *testing.T) {
	tests := []struct {
		tool     string
		wantType string
		wantRS   string
	}{
		{"browser_navigate", "readyState", "complete"},
		{"browser_open", "readyState", "complete"},
		{"browser_click", "readyState", "complete"},
		{"browser_click_ref", "readyState", "complete"},
		{"browser_back", "readyState", "complete"},
		{"browser_forward", "readyState", "complete"},
		{"browser_reload", "readyState", "complete"},
		{"browser_type", "none", ""},
		{"browser_fill_form", "none", ""},
		{"browser_snapshot", "none", ""},
		{"browser_wait", "none", ""},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			g := InferWaitFor(tt.tool)
			if g.Type != tt.wantType {
				t.Errorf("type=%q, want %q", g.Type, tt.wantType)
			}
			if tt.wantRS != "" && g.ReadyState != tt.wantRS {
				t.Errorf("readyState=%q, want %q", g.ReadyState, tt.wantRS)
			}
		})
	}
}

// ── WaitGuard.IsNone ──

func TestWaitGuard_IsNone(t *testing.T) {
	if !((*WaitGuard)(nil).IsNone()) {
		t.Error("nil guard should be none")
	}
	if !(&WaitGuard{Type: ""}).IsNone() {
		t.Error("empty type should be none")
	}
	if !(&WaitGuard{Type: "none"}).IsNone() {
		t.Error("type=none should be none")
	}
	if (&WaitGuard{Type: "readyState"}).IsNone() {
		t.Error("type=readyState should NOT be none")
	}
}

// ── WaitGuard JSON round-trip ──

func TestWaitGuard_JSON(t *testing.T) {
	original := WaitGuard{
		Type:       "readyState",
		ReadyState: "complete",
		TimeoutMs:  5000,
	}
	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded WaitGuard
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Type != original.Type || decoded.ReadyState != original.ReadyState || decoded.TimeoutMs != original.TimeoutMs {
		t.Errorf("round-trip mismatch: %+v vs %+v", original, decoded)
	}
}

// ── Step JSON includes WaitFor and HumanDelayMs ──

func TestStep_JSON_WithWaitFor(t *testing.T) {
	step := Step{
		Index:        1,
		Tool:         "browser_navigate",
		Params:       map[string]any{"url": "https://example.com"},
		WaitFor:      &WaitGuard{Type: "readyState", ReadyState: "complete", TimeoutMs: 10000},
		HumanDelayMs: 3500,
	}
	b, err := json.Marshal(step)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded Step
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.WaitFor == nil || decoded.WaitFor.Type != "readyState" {
		t.Error("WaitFor lost in round-trip")
	}
	if decoded.HumanDelayMs != 3500 {
		t.Errorf("HumanDelayMs=%d, want 3500", decoded.HumanDelayMs)
	}
}

// ── Recorder.Capture tracks HumanDelayMs ──

func TestRecorder_HumanDelay(t *testing.T) {
	rec := &Recorder{}
	if err := rec.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer rec.Stop()

	// First step: HumanDelayMs=0
	rec.Capture("browser_navigate", map[string]any{"url": "https://example.com"})
	// Wait a bit to simulate human pause.
	time.Sleep(50 * time.Millisecond)
	rec.Capture("browser_click", map[string]any{"selector": "#btn"})

	scene, err := rec.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if len(scene.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(scene.Steps))
	}
	if scene.Steps[0].HumanDelayMs != 0 {
		t.Errorf("first step HumanDelayMs=%d, want 0", scene.Steps[0].HumanDelayMs)
	}
	if scene.Steps[1].HumanDelayMs < 40 {
		t.Errorf("second step HumanDelayMs=%d, want >=40", scene.Steps[1].HumanDelayMs)
	}
}

// ── Recorder.Capture auto-infers WaitFor ──

func TestRecorder_AutoInferWaitFor(t *testing.T) {
	rec := &Recorder{}
	rec.Start()
	defer rec.Stop()

	rec.Capture("browser_navigate", map[string]any{"url": "https://example.com"})
	rec.Capture("browser_type", map[string]any{"selector": "#input", "text": "hello"})
	rec.Capture("browser_click_ref", map[string]any{"ref": "42"})

	scene, _ := rec.Stop()

	// Navigate → readyState guard
	if scene.Steps[0].WaitFor == nil || scene.Steps[0].WaitFor.Type != "readyState" {
		t.Errorf("step 0 WaitFor=%+v, want type=readyState", scene.Steps[0].WaitFor)
	}
	// Type → none
	if scene.Steps[1].WaitFor == nil || scene.Steps[1].WaitFor.Type != "none" {
		t.Errorf("step 1 WaitFor=%+v, want type=none", scene.Steps[1].WaitFor)
	}
	// Click_ref → readyState guard
	if scene.Steps[2].WaitFor == nil || scene.Steps[2].WaitFor.Type != "readyState" {
		t.Errorf("step 2 WaitFor=%+v, want type=readyState", scene.Steps[2].WaitFor)
	}
}

// ── Player executeStep: WaitFor guard is called ──

func TestPlayer_GuardCalled(t *testing.T) {
	guardCalled := false
	dispatch := func(mgr *brosdk.Manager, tool string, params map[string]any) (string, error) {
		return `{"ok":true}`, nil
	}

	p := &Player{
		dispatch: dispatch,
		mgr:      &brosdk.Manager{}, // dummy (guard won't be called without mgr)
		guardFn: func(mgr *brosdk.Manager, envID string, g *WaitGuard) error {
			guardCalled = true
			return nil
		},
	}

	scene := &Scene{
		Version: 1,
		Name:    "guard-test",
		Steps: []Step{
			{
				Index:   0,
				Tool:    "browser_navigate",
				Params:  map[string]any{"url": "https://example.com"},
				WaitFor: &WaitGuard{Type: "readyState", ReadyState: "complete", TimeoutMs: 1000},
			},
		},
	}

	result := p.Replay(scene, ReplayOptions{EnvID: "env-1", StepDelay: 1 * time.Millisecond})
	if !result.Ok {
		t.Fatal("replay should succeed")
	}
	if !guardCalled {
		t.Error("WaitFor guard was never called")
	}
}

// ── Player executeStep: WaitFor guard failure is a soft error ──

func TestPlayer_GuardFailure(t *testing.T) {
	dispatch := func(mgr *brosdk.Manager, tool string, params map[string]any) (string, error) {
		return `{"ok":true}`, nil
	}
	p := &Player{
		dispatch: dispatch,
		mgr:      &brosdk.Manager{},
		guardFn: func(mgr *brosdk.Manager, envID string, g *WaitGuard) error {
			return fmt.Errorf("simulated guard timeout")
		},
	}

	scene := &Scene{
		Version: 1,
		Name:    "guard-fail",
		Steps: []Step{
			{Index: 0, Tool: "browser_navigate", Params: map[string]any{"url": "https://example.com"},
				WaitFor: &WaitGuard{Type: "readyState"}},
		},
	}

	result := p.Replay(scene, ReplayOptions{EnvID: "env-1", StepDelay: 1 * time.Millisecond, StopOnError: false})
	// Guard failure is soft — step still marked ok, but result has guardWarn annotation.
	sr := result.Steps[0]
	if !sr.Ok {
		t.Error("step should still be ok (guard failure is soft)")
	}
	if !strings.Contains(sr.Result, "guardWarn") {
		t.Errorf("result should contain guardWarn, got: %s", sr.Result)
	}
}

// ── Player executeStep: step with none guard skips waiting ──

func TestPlayer_GuardNoneIsSkipped(t *testing.T) {
	guardCalled := false
	dispatch := func(mgr *brosdk.Manager, tool string, params map[string]any) (string, error) {
		return `{"ok":true}`, nil
	}
	p := &Player{
		dispatch: dispatch,
		mgr:      &brosdk.Manager{},
		guardFn: func(mgr *brosdk.Manager, envID string, g *WaitGuard) error {
			guardCalled = true
			return nil
		},
	}

	scene := &Scene{
		Version: 1,
		Name:    "none-test",
		Steps: []Step{
			{Index: 0, Tool: "browser_type", Params: map[string]any{"text": "hello"},
				WaitFor: &WaitGuard{Type: "none"}},
		},
	}

	result := p.Replay(scene, ReplayOptions{EnvID: "env-1", StepDelay: 1 * time.Millisecond})
	if !result.Ok {
		t.Fatal("replay should succeed")
	}
	if guardCalled {
		t.Error("guard should NOT be called for type=none")
	}
}

// ── HumanDelayMs: applied during replay ──

func TestPlayer_HumanDelayApplied(t *testing.T) {
	dispatch := func(mgr *brosdk.Manager, tool string, params map[string]any) (string, error) {
		return `{"ok":true}`, nil
	}
	p := &Player{dispatch: dispatch, mgr: &brosdk.Manager{}}

	scene := &Scene{
		Version: 1,
		Name:    "delay-test",
		Steps: []Step{
			{Index: 0, Tool: "browser_navigate", Params: map[string]any{"url": "https://example.com"},
				HumanDelayMs: 0, WaitFor: &WaitGuard{Type: "none"}},
			{Index: 1, Tool: "browser_click", Params: map[string]any{"selector": "#btn"},
				HumanDelayMs: 100, WaitFor: &WaitGuard{Type: "none"}},
		},
	}

	// With ApplyHumanDelay=true: step 1 should take at least 100ms total.
	start := time.Now()
	result := p.Replay(scene, ReplayOptions{EnvID: "env-1", StepDelay: 1 * time.Millisecond, ApplyHumanDelay: true})
	elapsed := time.Since(start)
	if !result.Ok {
		t.Fatal("replay should succeed")
	}
	if elapsed < 90*time.Millisecond {
		t.Errorf("expected >=90ms with human delay, got %v", elapsed)
	}

	// With ApplyHumanDelay=false: much faster (only StepDelay).
	start = time.Now()
	result = p.Replay(scene, ReplayOptions{EnvID: "env-1", StepDelay: 1 * time.Millisecond, ApplyHumanDelay: false})
	elapsed = time.Since(start)
	if !result.Ok {
		t.Fatal("replay should succeed")
	}
	if elapsed > 50*time.Millisecond {
		t.Errorf("expected <50ms without human delay, got %v", elapsed)
	}
}

// ── HumanDelayMs: cap at 3 seconds ──

func TestPlayer_HumanDelayCapped(t *testing.T) {
	dispatch := func(mgr *brosdk.Manager, tool string, params map[string]any) (string, error) {
		return `{"ok":true}`, nil
	}
	p := &Player{dispatch: dispatch, mgr: &brosdk.Manager{}}

	scene := &Scene{
		Version: 1,
		Name:    "cap-test",
		Steps: []Step{
			{Index: 0, Tool: "browser_type", Params: map[string]any{"text": "hello"},
				HumanDelayMs: 10000, WaitFor: &WaitGuard{Type: "none"}}, // exceeds 3s cap
		},
	}

	start := time.Now()
	result := p.Replay(scene, ReplayOptions{EnvID: "env-1", StepDelay: 1 * time.Millisecond, ApplyHumanDelay: true})
	elapsed := time.Since(start)
	if !result.Ok {
		t.Fatal("replay should succeed")
	}
	// Capped at 3s + 1ms StepDelay = ~3001ms max. Verify it's well under 10s.
	if elapsed >= 5*time.Second {
		t.Errorf("delay should be capped at 3s, but took %v", elapsed)
	}
}

// ── Full Scene JSON with waitFor and humanDelay serialization ──

func TestScene_FullRoundTrip(t *testing.T) {
	scene := &Scene{
		Version:     1,
		Name:        "login-flow",
		Description: "Test login with guards",
		CreatedAt:   time.Now(),
		Steps: []Step{
			{
				Index:        0,
				Tool:         "browser_navigate",
				Params:       map[string]any{"url": "https://example.com/login"},
				WaitFor:      &WaitGuard{Type: "readyState", ReadyState: "complete", TimeoutMs: 10000},
				HumanDelayMs: 0,
			},
			{
				Index:        1,
				Tool:         "browser_type_ref",
				Params:       map[string]any{"ref": "5", "text": "admin"},
				WaitFor:      &WaitGuard{Type: "none"},
				HumanDelayMs: 1200,
			},
			{
				Index:        2,
				Tool:         "browser_click_ref",
				Params:       map[string]any{"ref": "10"},
				Fingerprint:  &ElementFingerprint{Role: "button", Name: "Login"},
				WaitFor:      &WaitGuard{Type: "exists", Role: "heading", Name: "Dashboard", TimeoutMs: 5000},
				HumanDelayMs: 2300,
			},
		},
	}

	b, err := json.Marshal(scene)
	if err != nil {
		t.Fatalf("marshal scene: %v", err)
	}

	var decoded Scene
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal scene: %v", err)
	}
	if len(decoded.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(decoded.Steps))
	}

	// Step 0: navigate → readyState guard
	if decoded.Steps[0].WaitFor.Type != "readyState" {
		t.Errorf("step 0 guard type=%q", decoded.Steps[0].WaitFor.Type)
	}
	if decoded.Steps[0].HumanDelayMs != 0 {
		t.Errorf("step 0 HumanDelayMs=%d", decoded.Steps[0].HumanDelayMs)
	}

	// Step 2: click_ref → exists guard + fingerprint
	if decoded.Steps[2].WaitFor.Type != "exists" {
		t.Errorf("step 2 guard type=%q", decoded.Steps[2].WaitFor.Type)
	}
	if decoded.Steps[2].WaitFor.Role != "heading" || decoded.Steps[2].WaitFor.Name != "Dashboard" {
		t.Errorf("step 2 guard role/name=%s/%s", decoded.Steps[2].WaitFor.Role, decoded.Steps[2].WaitFor.Name)
	}
	if decoded.Steps[2].Fingerprint == nil || decoded.Steps[2].Fingerprint.Role != "button" {
		t.Error("step 2 fingerprint lost")
	}
	if decoded.Steps[2].HumanDelayMs != 2300 {
		t.Errorf("step 2 HumanDelayMs=%d", decoded.Steps[2].HumanDelayMs)
	}
}

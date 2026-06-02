// Package recorder — unit tests (no browser required).
package recorder

import (
	"strconv"
	"testing"
)

func TestStartStop(t *testing.T) {
	r := &Recorder{}

	if r.IsRecording() {
		t.Fatal("should not be recording initially")
	}
	if st := r.Status(); st.Recording {
		t.Fatal("status should report not recording")
	}

	if err := r.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !r.IsRecording() {
		t.Fatal("should be recording after start")
	}

	st := r.Status()
	if !st.Recording || st.StepCount != 0 {
		t.Fatalf("status mismatch: %+v", st)
	}

	// Second start while active should fail.
	if err := r.Start(); err == nil {
		t.Fatal("expected error on double start")
	}

	scene, err := r.Stop()
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if len(scene.Steps) != 0 {
		t.Fatalf("scene mismatch: steps=%d", len(scene.Steps))
	}
	if r.IsRecording() {
		t.Fatal("should not be recording after stop")
	}

	// Second stop should fail.
	if _, err := r.Stop(); err == nil {
		t.Fatal("expected error on double stop")
	}
}

func TestCapture(t *testing.T) {
	r := &Recorder{}
	r.Start()

	r.Capture("browser_navigate", map[string]any{"url": "https://example.com"})
	r.Capture("browser_click", map[string]any{"selector": "#btn"})

	st := r.Status()
	if st.StepCount != 2 {
		t.Fatalf("expected 2 steps, got %d", st.StepCount)
	}

	// Capture when not recording should be a no-op.
	r.Stop()
	r.Capture("browser_type", map[string]any{"text": "hello"})
	if r.IsRecording() {
		t.Fatal("should not be recording after stop")
	}
}

func TestCapture_ParamsAreCopied(t *testing.T) {
	r := &Recorder{}
	r.Start()

	params := map[string]any{"key": "original"}
	r.Capture("some_tool", params)
	params["key"] = "mutated" // mutate origin

	scene, _ := r.Stop()
	val := scene.Steps[0].Params["key"].(string)
	if val != "original" {
		t.Fatalf("params should be copied, got %q", val)
	}
}

func TestSanitizeParams(t *testing.T) {
	orig := map[string]any{
		"envId":     "abc",
		"sessionId": "sess-1",
		"url":       "https://example.com",
		"selector":  "#btn",
	}
	out := SanitizeParams(orig)

	if _, ok := out["envId"]; ok {
		t.Fatal("envId should be removed")
	}
	if _, ok := out["sessionId"]; ok {
		t.Fatal("sessionId should be removed")
	}
	if out["url"] != "https://example.com" {
		t.Fatalf("url should be preserved, got %v", out["url"])
	}
	if out["selector"] != "#btn" {
		t.Fatalf("selector should be preserved, got %v", out["selector"])
	}

	// Original map should be unchanged.
	if orig["envId"] != "abc" {
		t.Fatal("original map should not be mutated")
	}

	// Empty map.
	out2 := SanitizeParams(map[string]any{})
	if len(out2) != 0 {
		t.Fatalf("empty map should stay empty, got %d keys", len(out2))
	}
}

func TestMaxSteps_SoftCap(t *testing.T) {
	r := &Recorder{}
	r.Start()

	for i := 0; i < MaxSteps+10; i++ {
		r.Capture("browser_click", map[string]any{"selector": "#item"})
	}

	st := r.Status()
	if st.StepCount != MaxSteps+10 {
		t.Fatalf("expect MaxSteps+10 steps captured (soft cap), got %d", st.StepCount)
	}
	// Note: validation happens at scene_save / record_stop time.
}

func TestScene_Version(t *testing.T) {
	r := &Recorder{}
	r.Start()
	r.Capture("tool_a", nil)
	scene, _ := r.Stop()
	if scene.Version != 1 {
		t.Fatalf("expected version 1, got %d", scene.Version)
	}
}

// ── _ref → fingerprint tests ──────────────────────────────────────────

func makeSnapshotJSON(backendID int, role, name string) []byte {
	return []byte(`{"nodes":[{"nodeId":"1","backendDOMNodeId":` +
		strconv.Itoa(backendID) + `,"ignored":false,` +
		`"role":{"value":"` + role + `"},"name":{"value":"` + name + `"},` +
		`"value":{"value":""}}]}`)
}

func TestCapture_RefToolGetsFingerprint(t *testing.T) {
	r := &Recorder{}
	r.Start()

	// Simulate a prior snapshot call.
	r.SetLastSnapshot(makeSnapshotJSON(42, "button", "Submit"))

	// Capture a _ref tool — should attach fingerprint.
	r.Capture("browser_click_ref", map[string]any{"ref": "42"})

	scene, _ := r.Stop()
	if len(scene.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(scene.Steps))
	}
	step := scene.Steps[0]
	if step.Fingerprint == nil {
		t.Fatal("expected fingerprint on _ref step, got nil")
	}
	if step.Fingerprint.Role != "button" || step.Fingerprint.Name != "Submit" {
		t.Fatalf("fingerprint mismatch: %+v", step.Fingerprint)
	}
}

func TestCapture_NonRefToolNoFingerprint(t *testing.T) {
	r := &Recorder{}
	r.Start()

	r.SetLastSnapshot(makeSnapshotJSON(42, "button", "Submit"))
	r.Capture("browser_click", map[string]any{"selector": "#btn"})

	scene, _ := r.Stop()
	if scene.Steps[0].Fingerprint != nil {
		t.Fatal("non-_ref tool should not have fingerprint")
	}
}

func TestCapture_RefToolWithoutSnapshot(t *testing.T) {
	r := &Recorder{}
	r.Start()

	// No snapshot cached — _ref tool still recorded but without fingerprint.
	r.Capture("browser_click_ref", map[string]any{"ref": "42"})

	scene, _ := r.Stop()
	if scene.Steps[0].Fingerprint != nil {
		t.Fatal("fingerprint should be nil when no snapshot cached")
	}
}

func TestFindNodeByRef(t *testing.T) {
	snap := makeSnapshotJSON(42, "button", "Click Me")

	node, err := FindNodeByRef(snap, "42")
	if err != nil {
		t.Fatalf("FindNodeByRef: %v", err)
	}
	if node.BackendDOMNodeID != 42 {
		t.Fatalf("expected backendDOMNodeId 42, got %d", node.BackendDOMNodeID)
	}
}

func TestFindNodeByRef_NotFound(t *testing.T) {
	snap := makeSnapshotJSON(42, "button", "Click Me")
	_, err := FindNodeByRef(snap, "999")
	if err == nil {
		t.Fatal("expected error for missing ref")
	}
}

func TestFindNodeByRef_EmptySnapshot(t *testing.T) {
	_, err := FindNodeByRef(nil, "42")
	if err == nil {
		t.Fatal("expected error for empty snapshot")
	}
}

func TestFindNodeByFingerprint_Exact(t *testing.T) {
	snap := makeSnapshotJSON(42, "textbox", "Email")
	node, err := FindNodeByFingerprint(snap, ElementFingerprint{Role: "textbox", Name: "Email"})
	if err != nil {
		t.Fatalf("FindNodeByFingerprint: %v", err)
	}
	if node.BackendDOMNodeID != 42 {
		t.Fatalf("expected backendDOMNodeId 42, got %d", node.BackendDOMNodeID)
	}
}

func TestFindNodeByFingerprint_IgnoresIgnored(t *testing.T) {
	// Node with ignored=true should not match.
	snap := []byte(`{"nodes":[{"nodeId":"1","backendDOMNodeId":42,"ignored":true,"role":{"value":"button"},"name":{"value":"Submit"},"value":{"value":""}},{"nodeId":"2","backendDOMNodeId":43,"ignored":false,"role":{"value":"button"},"name":{"value":"Submit"},"value":{"value":""}}]}`)

	node, err := FindNodeByFingerprint(snap, ElementFingerprint{Role: "button", Name: "Submit"})
	if err != nil {
		t.Fatalf("FindNodeByFingerprint: %v", err)
	}
	if node.BackendDOMNodeID != 43 {
		t.Fatalf("should skip ignored node, expected 43, got %d", node.BackendDOMNodeID)
	}
}

func TestFindNodeByFingerprint_NotFound(t *testing.T) {
	snap := makeSnapshotJSON(42, "button", "Submit")
	_, err := FindNodeByFingerprint(snap, ElementFingerprint{Role: "link", Name: "Home"})
	if err == nil {
		t.Fatal("expected error for non-matching fingerprint")
	}
}

func TestIsRefTool(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"browser_click_ref", true},
		{"browser_type_ref", true},
		{"browser_fill_ref", true},
		{"browser_hover_ref", true},
		{"browser_focus_ref", true},
		{"browser_select_option_ref", true},
		{"browser_check_ref", true},
		{"browser_uncheck_ref", true},
		// Non-ref tools
		{"browser_click", false},
		{"browser_navigate", false},
		{"browser_snapshot", false},
		{"browser_type", false},
		{"record_start", false},
	}
	for _, tt := range tests {
		if got := IsRefTool(tt.name); got != tt.expected {
			t.Errorf("IsRefTool(%q) = %v, want %v", tt.name, got, tt.expected)
		}
	}
}

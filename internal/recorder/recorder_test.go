// Package recorder — unit tests (no browser required).
package recorder

import (
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

	if err := r.Start("env-1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !r.IsRecording() {
		t.Fatal("should be recording after start")
	}

	st := r.Status()
	if !st.Recording || st.EnvID != "env-1" || st.StepCount != 0 {
		t.Fatalf("status mismatch: %+v", st)
	}

	// Second start while active should fail.
	if err := r.Start("env-2"); err == nil {
		t.Fatal("expected error on double start")
	}

	scene, err := r.Stop()
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if scene.EnvID != "env-1" || len(scene.Steps) != 0 {
		t.Fatalf("scene mismatch: envID=%s steps=%d", scene.EnvID, len(scene.Steps))
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
	r.Start("env-1")

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
	r.Start("env-x")

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
	r.Start("env-1")

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
	r.Start("env-v")
	r.Capture("tool_a", nil)
	scene, _ := r.Stop()
	if scene.Version != 1 {
		t.Fatalf("expected version 1, got %d", scene.Version)
	}
}

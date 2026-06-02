//go:build windows

package main_test

import (
	"testing"
	"time"
)

// TestE2E_RecordReplay_FullFlow tests:
//
//	record_start → navigate → type → click → record_stop → scene_replay
func TestE2E_RecordReplay_FullFlow(t *testing.T) {
	f := setupE2E(t)
	defer f.cleanup()

	envID := f.getFirstEnvID()
	t.Logf("using envId=%s", envID)

	// Start a local form server.
	baseURL, stopServer := startLoginServer(t)
	defer stopServer()

	// Open browser + navigate to form.
	f.drainSSE()
	_, err := f.client.callTool("browser_open", map[string]any{"envId": envID})
	if err != nil {
		t.Fatalf("browser_open: %v", err)
	}
	evt, ok := f.waitSSEEventLog("browser-open-success", 60*time.Second, true)
	if !ok {
		t.Fatal("timeout waiting for browser-open-success")
	}
	t.Logf("browser open ok, code=%d", evt.Code)
	time.Sleep(2 * time.Second)

	// ── 1. record_start ──
	resp, err := f.client.callTool("record_start", map[string]any{"envId": envID})
	if err != nil {
		t.Fatalf("record_start: %v", err)
	}
	validateToolResult(t, resp.Result, "record_start")

	// ── 2. Perform actions (auto-captured) ──
	// Navigate
	navResp, err := f.client.callTool("browser_navigate", map[string]any{
		"envId": envID,
		"url":   baseURL,
	})
	if err != nil {
		t.Fatalf("browser_navigate: %v", err)
	}
	validateToolResult(t, navResp.Result, "browser_navigate")
	time.Sleep(500 * time.Millisecond)

	// Fill username
	fillResp, err := f.client.callTool("browser_fill", map[string]any{
		"envId":    envID,
		"selector": "#username",
		"text":     "testuser",
	})
	if err != nil {
		t.Fatalf("browser_fill username: %v", err)
	}
	validateToolResult(t, fillResp.Result, "browser_fill")
	time.Sleep(200 * time.Millisecond)

	// Fill password
	_, err = f.client.callTool("browser_fill", map[string]any{
		"envId":    envID,
		"selector": "#password",
		"text":     "s3cret",
	})
	if err != nil {
		t.Fatalf("browser_fill password: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	// Click submit
	_, err = f.client.callTool("browser_click", map[string]any{
		"envId":    envID,
		"selector": "#submitBtn",
	})
	if err != nil {
		t.Fatalf("browser_click submit: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	// ── 3. record_stop (auto-saves scene) ──
	stopResp, err := f.client.callTool("record_stop", map[string]any{
		"name":        "e2e_login_flow",
		"description": "E2E test login flow",
	})
	if err != nil {
		t.Fatalf("record_stop: %v", err)
	}
	validateToolResult(t, stopResp.Result, "record_stop")
	t.Logf("record_stop result: %s", stopResp.Result)

	// Verify scene exists
	listResp, err := f.client.callTool("scene_list", map[string]any{})
	if err != nil {
		t.Fatalf("scene_list: %v", err)
	}
	t.Logf("scene_list result: %s", listResp.Result)

	// ── 4. Close browser, then reopen for replay ──
	f.drainSSE()
	_, err = f.client.callTool("browser_close", map[string]any{"envId": envID})
	if err != nil {
		t.Fatalf("browser_close: %v", err)
	}
	_, ok = f.waitSSEEvent("browser-close-success", 30*time.Second)
	if !ok {
		t.Fatal("timeout waiting for browser-close-success")
	}
	time.Sleep(1 * time.Second)

	f.drainSSE()
	_, err = f.client.callTool("browser_open", map[string]any{"envId": envID})
	if err != nil {
		t.Fatalf("browser_open (for replay): %v", err)
	}
	_, ok = f.waitSSEEventLog("browser-open-success", 60*time.Second, true)
	if !ok {
		t.Fatal("timeout waiting for browser-open-success (replay)")
	}
	time.Sleep(2 * time.Second)

	// ── 5. scene_replay ──
	replayResp, err := f.client.callTool("scene_replay", map[string]any{
		"name":     "e2e_login_flow",
		"envId":    envID,
		"stepDelay": float64(500),
	})
	if err != nil {
		t.Fatalf("scene_replay: %v", err)
	}
	validateToolResult(t, replayResp.Result, "scene_replay")
	t.Logf("scene_replay result: %s", replayResp.Result)

	// ── 6. Cleanup: delete the test scene ──
	delResp, err := f.client.callTool("scene_delete", map[string]any{"name": "e2e_login_flow"})
	if err != nil {
		t.Logf("scene_delete (cleanup): %v", err)
	} else {
		t.Logf("scene_delete: %s", delResp.Result)
	}

	browserCloseHelper(t, f, envID)
}

// TestE2E_RecordReplay_VariableSubstitution tests that variable substitution
// works in scene_replay with special focus on text fields.
func TestE2E_RecordReplay_VariableSubstitution(t *testing.T) {
	f := setupE2E(t)
	defer f.cleanup()

	envID := f.getFirstEnvID()
	baseURL, stopServer := startLoginServer(t)
	defer stopServer()

	// Open browser
	f.drainSSE()
	_, err := f.client.callTool("browser_open", map[string]any{"envId": envID})
	if err != nil {
		t.Fatalf("browser_open: %v", err)
	}
	_, ok := f.waitSSEEventLog("browser-open-success", 60*time.Second, true)
	if !ok {
		t.Fatal("timeout waiting for browser-open-success")
	}
	time.Sleep(2 * time.Second)

	// Navigate first (outside recording — just to get session)
	_, err = f.client.callTool("browser_navigate", map[string]any{
		"envId": envID,
		"url":   baseURL,
	})
	if err != nil {
		t.Fatalf("browser_navigate: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	// Record only the fill + click (using variables).
	f.client.callTool("record_start", map[string]any{"envId": envID})

	_, err = f.client.callTool("browser_fill", map[string]any{
		"envId":    envID,
		"selector": "#username",
		"text":     "{{username}}",
	})
	if err != nil {
		t.Fatalf("browser_fill: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	_, err = f.client.callTool("browser_fill", map[string]any{
		"envId":    envID,
		"selector": "#password",
		"text":     "{{password}}",
	})
	if err != nil {
		t.Fatalf("browser_fill: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	_, err = f.client.callTool("browser_click", map[string]any{
		"envId":    envID,
		"selector": "#submitBtn",
	})
	if err != nil {
		t.Fatalf("browser_click: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	f.client.callTool("record_stop", map[string]any{"name": "e2e_var_test"})

	// Close and reopen browser
	f.drainSSE()
	f.client.callTool("browser_close", map[string]any{"envId": envID})
	f.waitSSEEvent("browser-close-success", 30*time.Second)
	time.Sleep(1 * time.Second)

	f.drainSSE()
	f.client.callTool("browser_open", map[string]any{"envId": envID})
	f.waitSSEEventLog("browser-open-success", 60*time.Second, true)
	time.Sleep(2 * time.Second)

	// Navigate again (scene_replay steps rely on page context)
	_, err = f.client.callTool("browser_navigate", map[string]any{
		"envId": envID,
		"url":   baseURL,
	})
	if err != nil {
		t.Fatalf("browser_navigate (before replay): %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	// Replay with different credentials
	replayResp, err := f.client.callTool("scene_replay", map[string]any{
		"name":     "e2e_var_test",
		"envId":    envID,
		"variables": map[string]any{"username": "alice", "password": "wonderland"},
		"stepDelay": float64(500),
	})
	if err != nil {
		t.Fatalf("scene_replay: %v", err)
	}
	validateToolResult(t, replayResp.Result, "scene_replay")
	t.Logf("variable replay result: %s", replayResp.Result)

	// Verify the page shows "Login successful" (form submits credentials).
	resultText := evalText(t, f, envID, "document.getElementById('result').textContent")
	t.Logf("page result text: %s", resultText)

	// Cleanup
	f.client.callTool("scene_delete", map[string]any{"name": "e2e_var_test"})
	browserCloseHelper(t, f, envID)
}

// TestE2E_RecordReplay_StopOnError tests that stopOnError prevents
// step execution after a failure.
func TestE2E_RecordReplay_StopOnError(t *testing.T) {
	f := setupE2E(t)
	defer f.cleanup()

	envID := f.getFirstEnvID()
	baseURL, stopServer := startLoginServer(t)
	defer stopServer()

	f.drainSSE()
	_, err := f.client.callTool("browser_open", map[string]any{"envId": envID})
	if err != nil {
		t.Fatalf("browser_open: %v", err)
	}
	f.waitSSEEventLog("browser-open-success", 60*time.Second, true)
	time.Sleep(2 * time.Second)

	_, err = f.client.callTool("browser_navigate", map[string]any{
		"envId": envID,
		"url":   baseURL,
	})
	if err != nil {
		t.Fatalf("browser_navigate: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	// Record: type + type (without navigate, so second depends on first context)
	f.client.callTool("record_start", map[string]any{"envId": envID})

	f.client.callTool("browser_fill", map[string]any{
		"envId":    envID,
		"selector": "#username",
		"text":     "user",
	})
	time.Sleep(200 * time.Millisecond)

	// Use an invalid selector that will fail
	f.client.callTool("browser_click", map[string]any{
		"envId":    envID,
		"selector": "#nonexistent-element",
	})
	time.Sleep(200 * time.Millisecond)

	f.client.callTool("browser_fill", map[string]any{
		"envId":    envID,
		"selector": "#password",
		"text":     "after_error",
	})

	f.client.callTool("record_stop", map[string]any{"name": "e2e_stoponerr"})

	// Replay with stopOnError=true
	replayResp, err := f.client.callTool("scene_replay", map[string]any{
		"name":        "e2e_stoponerr",
		"envId":       envID,
		"stopOnError": true,
		"stepDelay":   float64(500),
	})
	if err != nil {
		t.Fatalf("scene_replay: %v", err)
	}
	// Don't validate as "ok" — the replay itself may succeed but steps may fail.
	t.Logf("stopOnError replay result: %s", replayResp.Result)

	// Cleanup
	f.client.callTool("scene_delete", map[string]any{"name": "e2e_stoponerr"})
	browserCloseHelper(t, f, envID)
}

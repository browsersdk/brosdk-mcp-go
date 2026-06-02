//go:build windows

package main_test

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// startAgentTestServer serves a page with elements for all agent-friendly tools:
//   - find_ref: buttons, textbox, checkbox, link, heading
//   - wait: delayed element (appears after 1s)
//   - page_state: title + URL
//   - exists: present/absent checks
//   - dialog: alert/confirm trigger
//   - fill_form: multi-field form with submit
//   - snapshot interactiveOnly
func startAgentTestServer(t *testing.T) (string, func()) {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Agent Test Page</title></head><body>
<h1>Agent Test</h1>
<button id="btnSubmit">Submit Form</button>
<button id="btnCancel">Cancel</button>
<a href="#top" id="linkHome">Home</a>
<input id="txtName" type="text" placeholder="Enter name" aria-label="Name input">
<input id="txtEmail" type="email" placeholder="Enter email" aria-label="Email input">
<input id="cbTerms" type="checkbox" aria-label="Terms checkbox">
<select id="selCountry" aria-label="Country selector">
  <option value="">-- Choose --</option>
  <option value="us">United States</option>
  <option value="cn">China</option>
</select>
<div id="delayed" style="display:none">I am here now!</div>
<div id="output"></div>
<form id="mainForm" action="/submit" method="post">
  <input id="formName" name="name" type="text" placeholder="Your name">
  <input id="formEmail" name="email" type="email" placeholder="Your email">
  <button id="formSubmit" type="submit">Send</button>
</form>
<script>
(function(){
  var o=document.getElementById('output');
  function log(e){o.textContent+=e+';';}

  // Delayed element appears after 1s.
  setTimeout(function(){
    document.getElementById('delayed').style.display='block';
    document.getElementById('delayed').setAttribute('aria-label','Delayed element');
  }, 1000);

  // Output logging for form events.
  document.getElementById('txtName').addEventListener('input', function(){
    log('name:'+this.value+';');
  });
  document.getElementById('cbTerms').addEventListener('change', function(){
    log('terms:'+this.checked+';');
  });
  document.getElementById('selCountry').addEventListener('change', function(){
    log('country:'+this.value+';');
  });
  document.getElementById('btnSubmit').addEventListener('click', function(){
    log('submit_clicked;');
  });

  // Dialog triggers.
  document.getElementById('btnCancel').addEventListener('click', function(){
    alert('Are you sure you want to cancel?');
    log('alert_triggered;');
  });

  // Form submit handler.
  document.getElementById('mainForm').addEventListener('submit', function(e){
    e.preventDefault();
    log('form_submitted;');
  });
})();
</script>
</body></html>`)
	})

	mux.HandleFunc("/submit", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "OK")
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(listener)

	addr := fmt.Sprintf("http://%s", listener.Addr().String())
	return addr, func() { srv.Close() }
}

// ======================================================================
// TestE2E_AgentFriendlyTools
// ======================================================================

func TestE2E_AgentFriendlyTools(t *testing.T) {
	f := setupE2E(t)
	defer f.cleanup()

	testURL, stopServer := startAgentTestServer(t)
	defer stopServer()
	t.Logf("agent test server: %s", testURL)

	envID := f.getFirstEnvID()
	sessionID := browserOpenNavigate(t, f, envID, testURL)
	defer browserCloseHelper(t, f, envID)

	outputFn := func() string { return evalText(t, f, envID, `document.getElementById('output').textContent`) }

	// ── 1. browser_find_ref: by role ──
	t.Run("find_ref_by_role", func(t *testing.T) {
		resp, err := f.client.callTool("browser_find_ref", map[string]any{
			"envId": envID,
			"role":  "button",
			"limit": float64(10),
		})
		if err != nil {
			t.Fatalf("find_ref(role=button): %v", err)
		}
		validateToolResult(t, resp.Result, "find_ref(role=button)")

		var data struct {
			Refs  []any `json:"refs"`
			Count int   `json:"count"`
		}
		if err := parseToolText(resp.Result, &data); err != nil {
			t.Fatalf("parse find_ref: %v", err)
		}
		// Should find at least btnSubmit, btnCancel, formSubmit
		if data.Count < 3 {
			t.Errorf("expected at least 3 buttons, got %d (data=%+v)", data.Count, data.Refs)
		}
		t.Logf("find_ref(role=button): %d results", data.Count)
	})

	// ── 2. browser_find_ref: by role + name ──
	t.Run("find_ref_by_name", func(t *testing.T) {
		resp, err := f.client.callTool("browser_find_ref", map[string]any{
			"envId": envID,
			"role":  "button",
			"name":  "Cancel",
		})
		if err != nil {
			t.Fatalf("find_ref(button+Cancel): %v", err)
		}
		var data struct {
			Refs  []any `json:"refs"`
			Count int   `json:"count"`
		}
		if err := parseToolText(resp.Result, &data); err != nil {
			t.Fatalf("parse find_ref: %v", err)
		}
		if data.Count != 1 {
			t.Errorf("expected 1 Cancel button, got %d", data.Count)
		}
		t.Logf("find_ref(button+Cancel): %d result(s)", data.Count)
	})

	// ── 3. browser_find_ref: textbox ──
	t.Run("find_ref_textbox", func(t *testing.T) {
		resp, err := f.client.callTool("browser_find_ref", map[string]any{
			"envId": envID,
			"role":  "textbox",
		})
		if err != nil {
			t.Fatalf("find_ref(textbox): %v", err)
		}
		var data struct {
			Count int `json:"count"`
		}
		if err := parseToolText(resp.Result, &data); err != nil {
			t.Fatalf("parse find_ref: %v", err)
		}
		// Should find txtName, txtEmail, formName, formEmail
		if data.Count < 2 {
			t.Errorf("expected at least 2 textboxes, got %d", data.Count)
		}
		t.Logf("find_ref(textbox): %d results", data.Count)
	})

	// ── 4. browser_snapshot interactiveOnly ──
	t.Run("snapshot_interactive", func(t *testing.T) {
		// Full snapshot first.
		fullResp, err := f.client.callTool("browser_snapshot", map[string]any{
			"envId":     envID,
			"sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("full snapshot: %v", err)
		}

		// Interactive-only snapshot.
		interResp, err := f.client.callTool("browser_snapshot", map[string]any{
			"envId":          envID,
			"sessionId":      sessionID,
			"interactiveOnly": true,
		})
		if err != nil {
			t.Fatalf("interactive snapshot: %v", err)
		}

		if len(interResp.Result) >= len(fullResp.Result) {
			t.Errorf("interactiveOnly should be smaller: full=%d inter=%d",
				len(fullResp.Result), len(interResp.Result))
		}
		t.Logf("full=%d bytes, interactive=%d bytes (%.1f%%)",
			len(fullResp.Result), len(interResp.Result),
			float64(len(interResp.Result))/float64(len(fullResp.Result))*100)
	})

	// ── 5. browser_page_state ──
	t.Run("page_state", func(t *testing.T) {
		resp, err := f.client.callTool("browser_page_state", map[string]any{
			"envId": envID,
		})
		if err != nil {
			t.Fatalf("page_state: %v", err)
		}
		validateToolResult(t, resp.Result, "page_state")

		title, ok := extractTextField(resp.Result, "title")
		if !ok {
			t.Fatal("page_state: no title field")
		}
		if title != "Agent Test Page" {
			t.Errorf("expected 'Agent Test Page', got %q", title)
		}
		t.Logf("page_state: title=%q ok", title)
	})

	// ── 6. browser_exists (positive) ──
	t.Run("exists_positive", func(t *testing.T) {
		resp, err := f.client.callTool("browser_exists", map[string]any{
			"envId": envID,
			"role":  "button",
			"name":  "Submit Form",
		})
		if err != nil {
			t.Fatalf("exists(Submit Form): %v", err)
		}
		var data struct {
			Exists bool `json:"exists"`
		}
		if err := parseToolText(resp.Result, &data); err != nil {
			t.Fatalf("parse exists: %v", err)
		}
		if !data.Exists {
			t.Error("expected exists=true for Submit Form button")
		}
		t.Logf("exists(Submit Form): %v", data.Exists)
	})

	// ── 7. browser_exists (negative) ──
	t.Run("exists_negative", func(t *testing.T) {
		resp, err := f.client.callTool("browser_exists", map[string]any{
			"envId": envID,
			"role":  "button",
			"name":  "Nonexistent Button",
		})
		if err != nil {
			t.Fatalf("exists(nonexistent): %v", err)
		}
		var data struct {
			Exists bool `json:"exists"`
		}
		if err := parseToolText(resp.Result, &data); err != nil {
			t.Fatalf("parse exists: %v", err)
		}
		if data.Exists {
			t.Error("expected exists=false for nonexistent button")
		}
		t.Logf("exists(nonexistent): %v", data.Exists)
	})

	// ── 8. browser_wait (text match) ──
	t.Run("wait_text", func(t *testing.T) {
		// The delayed element takes 1s to appear.
		start := time.Now()
		_, err := f.client.callTool("browser_wait", map[string]any{
			"envId":   envID,
			"text":    "I am here now",
			"timeout": float64(10000),
		})
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("wait(text): %v", err)
		}
		if elapsed < 800*time.Millisecond {
			t.Errorf("wait returned too quickly: %v", elapsed)
		}
		t.Logf("wait(text) ✓ elapsed=%v", elapsed)
	})

	// ── 9. browser_wait (role+name match) ──
	t.Run("wait_role_name", func(t *testing.T) {
		_, err := f.client.callTool("browser_wait", map[string]any{
			"envId":   envID,
			"role":    "textbox",
			"name":    "Email input",
			"timeout": float64(5000),
		})
		if err != nil {
			t.Fatalf("wait(role+name): %v", err)
		}
		t.Log("wait(role+name) ✓")
	})

	// ── 10. browser_dialog: accept ──
	t.Run("dialog_accept", func(t *testing.T) {
		// First ensure the dialog will be triggered by clicking Cancel button.
		// The dialog handling is async with page lifecycle, so we trigger and handle.
		_, _ = f.client.callTool("browser_evaluate", map[string]any{
			"envId": envID, "expression": `document.getElementById('output').textContent=''`,
		})

		// Click Cancel which triggers alert(). Use evaluate to trigger alert directly.
		_, err := f.client.callTool("browser_evaluate", map[string]any{
			"envId":      envID,
			"expression": `setTimeout(() => alert('E2E test dialog'), 100)`,
		})
		if err != nil {
			t.Fatalf("trigger alert: %v", err)
		}
		time.Sleep(200 * time.Millisecond)

		// Accept the dialog.
		dResp, err := f.client.callTool("browser_dialog", map[string]any{
			"envId":  envID,
			"action": "accept",
		})
		if err != nil {
			t.Fatalf("dialog(accept): %v", err)
		}
		t.Logf("dialog(accept): %s", dResp.Result)

		var dResult struct {
			Action string `json:"action"`
		}
		parseToolText(dResp.Result, &dResult)
		if dResult.Action != "accept" {
			t.Errorf("expected action=accept, got %q", dResult.Action)
		}
	})

	// ── 11. browser_dialog: dismiss ──
	t.Run("dialog_dismiss", func(t *testing.T) {
		// Trigger another alert.
		_, err := f.client.callTool("browser_evaluate", map[string]any{
			"envId":      envID,
			"expression": `setTimeout(() => confirm('Delete?'), 100)`,
		})
		if err != nil {
			t.Fatalf("trigger confirm: %v", err)
		}
		time.Sleep(200 * time.Millisecond)

		dResp, err := f.client.callTool("browser_dialog", map[string]any{
			"envId":  envID,
			"action": "dismiss",
		})
		if err != nil {
			t.Fatalf("dialog(dismiss): %v", err)
		}
		t.Logf("dialog(dismiss): %s", dResp.Result)

		var dResult struct {
			Action string `json:"action"`
		}
		parseToolText(dResp.Result, &dResult)
		if dResult.Action != "dismiss" {
			t.Errorf("expected action=dismiss, got %q", dResult.Action)
		}
	})

	// ── 12. browser_fill_form ──
	t.Run("fill_form", func(t *testing.T) {
		_, _ = f.client.callTool("browser_evaluate", map[string]any{
			"envId": envID, "expression": `document.getElementById('output').textContent=''`,
		})

		resp, err := f.client.callTool("browser_fill_form", map[string]any{
			"envId": envID,
			"fields": map[string]any{
				"#formName":  "Alice",
				"#formEmail": "alice@test.com",
			},
		})
		if err != nil {
			t.Fatalf("fill_form: %v", err)
		}
		validateToolResult(t, resp.Result, "fill_form")

		// Verify fields were filled.
		nameVal := evalText(t, f, envID, `document.getElementById('formName').value`)
		if nameVal != "Alice" {
			t.Errorf("expected formName=Alice, got %q", nameVal)
		}
		emailVal := evalText(t, f, envID, `document.getElementById('formEmail').value`)
		if emailVal != "alice@test.com" {
			t.Errorf("expected formEmail=alice@test.com, got %q", emailVal)
		}
		t.Logf("fill_form ✓ name=%s email=%s", nameVal, emailVal)
	})

	// ── 13. browser_fill_form with submit ──
	t.Run("fill_form_submit", func(t *testing.T) {
		_, _ = f.client.callTool("browser_evaluate", map[string]any{
			"envId": envID, "expression": `document.getElementById('output').textContent=''`,
		})

		resp, err := f.client.callTool("browser_fill_form", map[string]any{
			"envId": envID,
			"fields": map[string]any{
				"#formName":  "Bob",
				"#formEmail": "bob@test.com",
			},
			"submitSelector": "#formSubmit",
		})
		if err != nil {
			t.Fatalf("fill_form+submit: %v", err)
		}
		validateToolResult(t, resp.Result, "fill_form+submit")

		// Verify output shows form_submitted.
		time.Sleep(200 * time.Millisecond)
		out := outputFn()
		if !strings.Contains(out, "form_submitted") {
			t.Errorf("expected form_submitted in output, got %q", out)
		}
		t.Logf("fill_form+submit ✓ output=%s", out)
	})

	t.Log("agent-friendly tools test complete ✓")
}

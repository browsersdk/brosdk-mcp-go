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

// startLoginServer starts a local HTTP server on a random port with a login
// form and a POST /login endpoint. Returns the base URL and a stop function.
func startLoginServer(t *testing.T) (string, func()) {
	t.Helper()

	mux := http.NewServeMux()

	// Serve the login form page.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Login Test</title></head><body>
<h1>Login</h1>
<form id="loginForm">
  <input type="text" id="username" name="username" placeholder="Username"><br>
  <input type="password" id="password" name="password" placeholder="Password"><br>
  <button type="submit" id="submitBtn">Sign In</button>
</form>
<div id="result"></div>
<script>
document.getElementById('loginForm').addEventListener('submit', async function(e) {
  e.preventDefault();
  var fd = new FormData(this);
  var params = new URLSearchParams();
  fd.forEach(function(v,k) { params.append(k,v); });
  var resp = await fetch('/login', {method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:params.toString()});
  document.getElementById('result').innerHTML = await resp.text();
});
</script>
</body></html>`)
	})

	// Handle login POST.
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", 405)
			return
		}
		r.ParseForm()
		username := r.FormValue("username")
		password := r.FormValue("password")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if username == "admin" && password == "secret123" {
			// put id="status" on the element for CDP assertion
			fmt.Fprint(w, `<p id="status" style="color:green;font-weight:bold">login_success</p>`)
		} else {
			fmt.Fprint(w, `<p id="status" style="color:red;font-weight:bold">login_failed</p>`)
		}
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
// E2E Tests
// ======================================================================

// TestE2E_AllTools runs a comprehensive E2E suite covering all 15 MCP tools.
// Subtests run sequentially and share a single SDK instance.
func TestE2E_AllTools(t *testing.T) {
	f := setupE2E(t)
	defer f.cleanup()

	// ── 1. sdk_info ───────────────────────────────────────────────────
	t.Run("sdk_info", func(t *testing.T) {
		resp, err := f.client.callTool("sdk_info", nil)
		if err != nil {
			t.Fatalf("sdk_info: %v", err)
		}
		validateToolResult(t, resp.Result, "sdk_info")
	})

	// ── 2. sdk_get_user_sig ───────────────────────────────────────────
	t.Run("sdk_get_user_sig", func(t *testing.T) {
		resp, err := f.client.callTool("sdk_get_user_sig", map[string]any{
			"apiKey": f.cfg.ApiKey,
		})
		if err != nil {
			t.Fatalf("sdk_get_user_sig: %v", err)
		}
		// Expect {"userSig":"..."}
		userSig, ok := extractTextField(resp.Result, "userSig")
		if !ok || userSig == "" {
			t.Fatalf("sdk_get_user_sig: no userSig in result: %s", resp.Result)
		}
		t.Logf("sdk_get_user_sig: userSig=%s", maskStr(userSig))
	})

	// ── 3. sdk_token_update ───────────────────────────────────────────
	t.Run("sdk_token_update", func(t *testing.T) {
		// First get a fresh userSig.
		sigResp, err := f.client.callTool("sdk_get_user_sig", map[string]any{
			"apiKey": f.cfg.ApiKey,
		})
		if err != nil {
			t.Fatalf("sdk_get_user_sig (pre): %v", err)
		}
		userSig, ok := extractTextField(sigResp.Result, "userSig")
		if !ok || userSig == "" {
			t.Fatalf("no userSig from sdk_get_user_sig")
		}

		// Drain any stale SSE events.
		f.drainSSE()

		resp, err := f.client.callTool("sdk_token_update", map[string]any{
			"userSig": userSig,
		})
		if err != nil {
			t.Fatalf("sdk_token_update: %v", err)
		}
		t.Logf("sdk_token_update response: %s", resp.Result)

		// Wait for SSE confirmation.
		evt, ok := f.waitSSEEvent("success", 15*time.Second)
		if !ok {
			t.Fatal("timeout waiting for token-update-success SSE event")
		}
		_ = evt
	})

	// ── 4. browser_info ───────────────────────────────────────────────
	t.Run("browser_info", func(t *testing.T) {
		resp, err := f.client.callTool("browser_info", nil)
		if err != nil {
			t.Fatalf("browser_info: %v", err)
		}
		validateToolResult(t, resp.Result, "browser_info")
	})

	// ── 5. env_page ───────────────────────────────────────────────────
	t.Run("env_page", func(t *testing.T) {
		envID := f.getFirstEnvID()
		if envID == "" {
			t.Fatal("env_page returned empty envId")
		}
	})

	// ── 6. env_getinfo ────────────────────────────────────────────────
	t.Run("env_getinfo", func(t *testing.T) {
		envID := f.getFirstEnvID()
		resp, err := f.client.callTool("env_getinfo", map[string]any{
			"envId": envID,
		})
		if err != nil {
			t.Fatalf("env_getinfo(%s): %v", envID, err)
		}
		// Verify the returned envId matches.
		returnedID, _ := extractEnvIDFromResult(resp.Result)
		if returnedID != "" && returnedID != envID {
			t.Errorf("env_getinfo returned envId=%q, want %q", returnedID, envID)
		}
		validateToolResult(t, resp.Result, "env_getinfo")
	})

	// ── 7. env_create ─────────────────────────────────────────────────
	var tempEnvID string
	t.Run("env_create", func(t *testing.T) {
		envName := fmt.Sprintf("e2e-test-%s", time.Now().Format("150405"))
		resp, err := f.client.callTool("env_create", map[string]any{
			"name": envName,
		})
		if err != nil {
			// env_create may fail due to account permissions — skip
			// dependent subtests rather than failing the whole suite.
			if strings.Contains(err.Error(), "permission") ||
				strings.Contains(err.Error(), "limit") ||
				strings.Contains(err.Error(), "quota") {
				t.Skipf("env_create skipped (permission/limit): %v", err)
			}
			t.Fatalf("env_create: %v", err)
		}
		id, err := extractEnvIDFromResult(resp.Result)
		if err != nil {
			t.Fatalf("extract envId from create: %v (raw=%s)", err, resp.Result)
		}
		tempEnvID = id
		t.Logf("created temp env: %s (name=%s)", tempEnvID, envName)
	})

	// ── 8. env_update ─────────────────────────────────────────────────
	t.Run("env_update", func(t *testing.T) {
		if tempEnvID == "" {
			t.Skip("no temp env from env_create")
		}
		newName := fmt.Sprintf("e2e-test-updated-%s", time.Now().Format("150405"))
		body := fmt.Sprintf(`{"envId":"%s","name":"%s"}`, tempEnvID, newName)
		resp, err := f.client.callTool("env_update", map[string]any{
			"envId": tempEnvID,
			"body":  body,
		})
		if err != nil {
			t.Fatalf("env_update: %v", err)
		}
		validateToolResult(t, resp.Result, "env_update")
		t.Logf("updated temp env %s -> %s", tempEnvID, newName)
	})

	// ── 9. browser_open + browser_close ───────────────────────────────
	t.Run("browser_open_close", func(t *testing.T) {
		envID := f.getFirstEnvID()

		// Drain stale SSE events.
		f.drainSSE()

		// Open browser.
		openResp, err := f.client.callTool("browser_open", map[string]any{
			"envId": envID,
		})
		if err != nil {
			t.Fatalf("browser_open: %v", err)
		}
		t.Logf("browser_open: %s", openResp.Result)

		// Wait for browser-open-success.
		evt, ok := f.waitSSEEvent("browser-open-success", 60*time.Second)
		if !ok {
			t.Fatal("timeout waiting for browser-open-success SSE event")
		}
		t.Logf("browser-open-success: code=%d", evt.Code)

		// Close browser.
		f.drainSSE()

		closeResp, err := f.client.callTool("browser_close", map[string]any{
			"envId": envID,
		})
		if err != nil {
			t.Fatalf("browser_close: %v", err)
		}
		t.Logf("browser_close: %s", closeResp.Result)

		// Wait for browser-close-success.
		evt, ok = f.waitSSEEvent("browser-close-success", 30*time.Second)
		if !ok {
			t.Fatal("timeout waiting for browser-close-success SSE event")
		}
		t.Logf("browser-close-success: code=%d", evt.Code)
	})

	// ── 10. browser_command ───────────────────────────────────────────
	t.Run("browser_command", func(t *testing.T) {
		envID := f.getFirstEnvID()

		// Open browser.
		f.drainSSE()
		_, err := f.client.callTool("browser_open", map[string]any{
			"envId": envID,
		})
		if err != nil {
			t.Fatalf("browser_open: %v", err)
		}
		evt, ok := f.waitSSEEventLog("browser-open-success", 60*time.Second, true)
		if !ok {
			t.Fatal("timeout waiting for browser-open-success")
		}
		t.Logf("browser open ok, code=%d", evt.Code)

		// Give browser a moment to fully initialize CDP.
		time.Sleep(2 * time.Second)

		// Send CDP command: Browser.getVersion (browser-level endpoint).
		cmdResp, err := f.client.callTool("browser_command", map[string]any{
			"envId":  envID,
			"method": "Browser.getVersion",
		})
		if err != nil {
			t.Fatalf("browser_command Browser.getVersion: %v", err)
		}
		t.Logf("browser_command Browser.getVersion: %s", cmdResp.Result)

		// Parse result to verify.
		var cdpResult map[string]any
		if err := parseToolText(cmdResp.Result, &cdpResult); err != nil {
			t.Fatalf("parse browser_command result: %v", err)
		}
		if prod, ok := cdpResult["product"].(string); ok && prod != "" {
			t.Logf("Browser.getVersion: product=%s ✓", prod)
		} else {
			// Some CDP responses embed result directly.
			if resultObj, ok := cdpResult["result"].(map[string]any); ok {
				if prod, ok := resultObj["product"].(string); ok && prod != "" {
					t.Logf("Browser.getVersion: product=%s ✓", prod)
				} else {
					t.Errorf("Browser.getVersion: missing product, got %v", cdpResult)
				}
			} else {
				t.Errorf("Browser.getVersion: unexpected response %v", cdpResult)
			}
		}

		// Close browser.
		f.drainSSE()
		_, err = f.client.callTool("browser_close", map[string]any{
			"envId": envID,
		})
		if err != nil {
			t.Fatalf("browser_close: %v", err)
		}
		_, ok = f.waitSSEEvent("browser-close-success", 30*time.Second)
		if !ok {
			t.Fatal("timeout waiting for browser-close-success")
		}
		t.Log("browser_command test complete")
	})

	// ── 11. env_destroy ───────────────────────────────────────────────
	t.Run("env_destroy", func(t *testing.T) {
		if tempEnvID == "" {
			t.Skip("no temp env to destroy")
		}
		resp, err := f.client.callTool("env_destroy", map[string]any{
			"envId": tempEnvID,
		})
		if err != nil {
			t.Fatalf("env_destroy(%s): %v", tempEnvID, err)
		}
		validateToolResult(t, resp.Result, "env_destroy")
		t.Logf("destroyed temp env %s", tempEnvID)
	})
}

// ======================================================================
// CDP Form Interaction Test
// ======================================================================

// TestE2E_CDPFormInteraction tests high-level browser automation tools:
//  1. Starts a local HTTP server with a login form.
//  2. Opens a browser, navigates to the form via browser_navigate.
//  3. Fills username/password via browser_fill, clicks submit via browser_click.
//  4. Verifies the login result text on the page.
func TestE2E_CDPFormInteraction(t *testing.T) {
	f := setupE2E(t)
	defer f.cleanup()

	// ── Start login server ────────────────────────────────────────────
	loginURL, stopServer := startLoginServer(t)
	defer stopServer()
	t.Logf("login server: %s", loginURL)

	envID := f.getFirstEnvID()

	// ── Open browser ──────────────────────────────────────────────────
	f.drainSSE()
	_, err := f.client.callTool("browser_open", map[string]any{
		"envId": envID,
	})
	if err != nil {
		t.Fatalf("browser_open: %v", err)
	}
	evt, ok := f.waitSSEEventLog("browser-open-success", 60*time.Second, true)
	if !ok {
		t.Fatal("timeout waiting for browser-open-success")
	}
	t.Logf("browser open ok, code=%d", evt.Code)

	// Give browser a moment to fully initialize CDP.
	time.Sleep(2 * time.Second)

	// ── Step 1+2+3: browser_navigate (create target + attach + navigate) ──
	navResp, err := f.client.callTool("browser_navigate", map[string]any{
		"envId": envID,
		"url":   loginURL,
	})
	if err != nil {
		t.Fatalf("browser_navigate: %v", err)
	}
	t.Logf("browser_navigate: %s", navResp.Result)

	var navResult map[string]any
	if err := parseToolText(navResp.Result, &navResult); err != nil {
		t.Fatalf("parse browser_navigate: %v", err)
	}
	sessionID, _ := navResult["sessionId"].(string)
	targetID, _ := navResult["targetId"].(string)
	t.Logf("  targetId=%s sessionId=%s", targetID, sessionID)

	// Small wait for page to fully render.
	time.Sleep(500 * time.Millisecond)

	// ── Step 4: browser_fill username ──────────────────────────────────
	_, err = f.client.callTool("browser_fill", map[string]any{
		"envId":     envID,
		"selector":  "#username",
		"text":      "admin",
		"sessionId": sessionID,
	})
	if err != nil {
		t.Fatalf("fill username: %v", err)
	}
	t.Log("  filled username: admin ✓")

	// ── Step 5: browser_fill password ──────────────────────────────────
	_, err = f.client.callTool("browser_fill", map[string]any{
		"envId":     envID,
		"selector":  "#password",
		"text":      "secret123",
		"sessionId": sessionID,
	})
	if err != nil {
		t.Fatalf("fill password: %v", err)
	}
	t.Log("  filled password: secret123 ✓")

	// ── Step 6: browser_click submit ───────────────────────────────────
	_, err = f.client.callTool("browser_click", map[string]any{
		"envId":     envID,
		"selector":  "#submitBtn",
		"sessionId": sessionID,
	})
	if err != nil {
		t.Fatalf("click submit: %v", err)
	}
	t.Log("  clicked submit ✓")

	// Wait for fetch + DOM update.
	time.Sleep(1 * time.Second)

	// ── Step 7: Verify result via browser_evaluate ──────────────────────
	checkResp, err := f.client.callTool("browser_evaluate", map[string]any{
		"envId":      envID,
		"expression": `document.getElementById('status').textContent`,
	})
	if err != nil {
		t.Fatalf("browser_evaluate: %v", err)
	}
	t.Logf("  evaluate result: %s", checkResp.Result)

	var evaluateValue string
	// browser_evaluate returns the JSON value directly
	if err := parseToolText(checkResp.Result, &evaluateValue); err != nil {
		// Try direct JSON parse
		var raw map[string]any
		if err2 := parseToolText(checkResp.Result, &raw); err2 != nil {
			t.Fatalf("parse evaluate result: %v", err)
		}
		evaluateValue, _ = raw["value"].(string)
	}
	if evaluateValue != "login_success" {
		t.Errorf("expected login_success, got %q (full: %s)", evaluateValue, checkResp.Result)
	} else {
		t.Log("  login_success ✓")
	}

	// ── Cleanup: close browser ────────────────────────────────────────
	f.drainSSE()
	_, err = f.client.callTool("browser_close", map[string]any{
		"envId": envID,
	})
	if err != nil {
		t.Fatalf("browser_close: %v", err)
	}
	_, ok = f.waitSSEEvent("browser-close-success", 30*time.Second)
	if !ok {
		t.Fatal("timeout waiting for browser-close-success")
	}
	t.Log("cdp form interaction test complete ✓")
}

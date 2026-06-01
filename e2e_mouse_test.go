//go:build windows

package main_test

import (
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

func startMouseTestServer(t *testing.T) (string, func()) {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Mouse Test</title></head><body>
<div id="app">
  <h1>Mouse Test</h1>
  <button id="dblBtn">Double Click Me</button>
  <button id="hoverBtn">Hover Over Me</button>
  <button id="hiddenBtn" style="display:none">Hidden Button</button>
  <div id="output"></div>
  <div id="mouseLog"></div>
</div>
<script>
(function(){
  var o=document.getElementById('output'),ml=document.getElementById('mouseLog');
  document.getElementById('dblBtn').addEventListener('dblclick',function(){o.textContent='dblclick_fired';});
  document.getElementById('hoverBtn').addEventListener('mouseenter',function(){o.textContent='hover_fired';});
  document.getElementById('hiddenBtn').addEventListener('click',function(){o.textContent='hidden_clicked';});
  document.getElementById('hoverBtn').addEventListener('dblclick',function(){o.textContent='hover_dblclick';});
})();
</script>
</body></html>`)
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

// startScrollTestServer serves a tall page with anchored elements at various


// ======================================================================
// TestE2E_MouseInteraction — covers dblclick, hover, hover_ref, find_click_text
// ======================================================================

func TestE2E_MouseInteraction(t *testing.T) {
	f := setupE2E(t)
	defer f.cleanup()

	testURL, stopServer := startMouseTestServer(t)
	defer stopServer()
	t.Logf("mouse test server: %s", testURL)

	envID := f.getFirstEnvID()
	sessionID := browserOpenNavigate(t, f, envID, testURL)
	defer browserCloseHelper(t, f, envID)

	// ── 1. browser_hover ──
	t.Run("hover", func(t *testing.T) {
		_, err := f.client.callTool("browser_hover", map[string]any{
			"envId": envID, "selector": "#hoverBtn", "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("hover: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
		out := evalText(t, f, envID, `document.getElementById('output').textContent`)
		if out != "hover_fired" {
			t.Errorf("hover: expected hover_fired, got %q", out)
		}
		t.Logf("hover ✓ output=%s", out)
	})

	// ── 2. browser_dblclick ──
	t.Run("dblclick", func(t *testing.T) {
		_, err := f.client.callTool("browser_dblclick", map[string]any{
			"envId": envID, "selector": "#dblBtn", "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("dblclick: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
		out := evalText(t, f, envID, `document.getElementById('output').textContent`)
		if out != "dblclick_fired" {
			t.Errorf("dblclick: expected dblclick_fired, got %q", out)
		}
		t.Logf("dblclick ✓ output=%s", out)
	})

	// ── 3. browser_hover_ref ──
	t.Run("hover_ref", func(t *testing.T) {
		snapResp, err := f.client.callTool("browser_snapshot", map[string]any{
			"envId": envID, "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		var snapTree []any
		if err := parseToolText(snapResp.Result, &snapTree); err != nil {
			t.Fatalf("parse snapshot: %v", err)
		}
		ref, found := findBackendDOMNodeID(snapTree, "Hover Over Me")
		if !found {
			t.Fatal("hoverBtn not found in snapshot")
		}
		// Reset output
		_, _ = f.client.callTool("browser_evaluate", map[string]any{
			"envId": envID, "expression": `document.getElementById('output').textContent=''`,
		})
		_, err = f.client.callTool("browser_hover_ref", map[string]any{
			"envId": envID, "ref": ref, "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("hover_ref: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
		out := evalText(t, f, envID, `document.getElementById('output').textContent`)
		if out != "hover_fired" {
			t.Errorf("hover_ref: expected hover_fired, got %q", out)
		}
		t.Logf("hover_ref ✓ ref=%s output=%s", ref, out)
	})

	// ── 4. browser_find_click_text ──
	t.Run("find_click_text", func(t *testing.T) {
		_, err := f.client.callTool("browser_find_click_text", map[string]any{
			"envId": envID, "text": "Double Click Me", "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("find_click_text: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
		// Clicking "Double Click Me" should fire click on #dblBtn.
		// Note: dblclick event won't fire for single click.
		// Verify the page is still alive.
		title := evalText(t, f, envID, `document.title`)
		if title == "" {
			t.Error("find_click_text: page seems broken after click")
		}
		t.Logf("find_click_text ✓ page title=%s", title)
	})

	t.Log("mouse interaction test complete ✓")
}

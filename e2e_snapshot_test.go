//go:build windows

package main_test

import (
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

// startSnapshotTestServer starts a local HTTP server serving a page with
// buttons and JS event handlers for snapshot+click_ref testing.
func startSnapshotTestServer(t *testing.T) (string, func()) {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Snapshot Test</title></head><body>
<div id="app">
  <h1 id="title">Snapshot Test</h1>
  <button id="btn1">Button 1</button>
  <button id="btn2">Target Button</button>
  <input id="input1" type="text" placeholder="Type here">
  <div id="output"></div>
</div>
<script>
document.getElementById('btn1').addEventListener('click', function() {
  document.getElementById('output').textContent = 'btn1_clicked';
});
document.getElementById('btn2').addEventListener('click', function() {
  document.getElementById('output').textContent = 'btn2_clicked';
});
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

// findBackendDOMNodeID walks a parsed JSON tree from browser_snapshot and
// returns the backendDOMNodeId for the first node whose name.value matches targetName.
func findBackendDOMNodeID(tree []any, targetName string) (string, bool) {
	for _, node := range tree {
		if nodeMap, ok := node.(map[string]any); ok {
			if ref, found := walkAXNode(nodeMap, targetName); found {
				return ref, true
			}
		}
	}
	return "", false
}

func walkAXNode(node map[string]any, targetName string) (string, bool) {
	// Check name
	if name, ok := node["name"].(map[string]any); ok {
		if v, ok := name["value"].(string); ok && v == targetName {
			if bid, ok := node["backendDOMNodeId"].(float64); ok {
				return fmt.Sprintf("%d", int64(bid)), true
			}
		}
	}
	// Walk children
	if children, ok := node["children"].([]any); ok {
		for _, child := range children {
			if childMap, ok := child.(map[string]any); ok {
				if ref, found := walkAXNode(childMap, targetName); found {
					return ref, true
				}
			}
		}
	}
	return "", false
}

// TestE2E_SnapshotClickRef tests the snapshot → click_ref workflow:
//  1. Start a local HTTP server with buttons.
//  2. browser_open → browser_navigate.
//  3. browser_snapshot → extract backendDOMNodeId for "Target Button".
//  4. browser_click_ref with the extracted ref.
//  5. browser_evaluate to verify the click handler fired.
func TestE2E_SnapshotClickRef(t *testing.T) {
	f := setupE2E(t)
	defer f.cleanup()

	// ── Start snapshot test server ─────────────────────────────────────
	testURL, stopServer := startSnapshotTestServer(t)
	defer stopServer()
	t.Logf("snapshot test server: %s", testURL)

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

	time.Sleep(2 * time.Second)

	// ── Navigate to test page ─────────────────────────────────────────
	navResp, err := f.client.callTool("browser_navigate", map[string]any{
		"envId": envID,
		"url":   testURL,
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

	time.Sleep(500 * time.Millisecond)

	// ── Step 1: browser_snapshot ───────────────────────────────────────
	snapResp, err := f.client.callTool("browser_snapshot", map[string]any{
		"envId":     envID,
		"sessionId": sessionID,
	})
	if err != nil {
		t.Fatalf("browser_snapshot: %v", err)
	}
	t.Logf("browser_snapshot: %d bytes", len(snapResp.Result))

	// Parse the accessibility tree (GetFullAXTree returns []*AXNode, i.e. a JSON array).
	var snapResult []any
	if err := parseToolText(snapResp.Result, &snapResult); err != nil {
		t.Fatalf("parse snapshot result: %v", err)
	}

	// ── Step 2: Extract backendDOMNodeId for "Target Button" ───────────
	ref, found := findBackendDOMNodeID(snapResult, "Target Button")
	if !found {
		t.Fatal("Target Button not found in accessibility tree")
	}
	t.Logf("  Target Button backendDOMNodeId: %s", ref)

	// ── Step 3: browser_click_ref ──────────────────────────────────────
	_, err = f.client.callTool("browser_click_ref", map[string]any{
		"envId":     envID,
		"ref":       ref,
		"sessionId": sessionID,
	})
	if err != nil {
		t.Fatalf("browser_click_ref: %v", err)
	}
	t.Log("  browser_click_ref ✓")

	time.Sleep(500 * time.Millisecond)

	// ── Step 4: Verify click via browser_evaluate ──────────────────────
	checkResp, err := f.client.callTool("browser_evaluate", map[string]any{
		"envId":      envID,
		"expression": `document.getElementById('output').textContent`,
	})
	if err != nil {
		t.Fatalf("browser_evaluate: %v", err)
	}
	t.Logf("  evaluate result: %s", checkResp.Result)

	var outputText string
	if err := parseToolText(checkResp.Result, &outputText); err != nil {
		t.Fatalf("parse evaluate result: %v", err)
	}
	if outputText != "btn2_clicked" {
		t.Errorf("expected 'btn2_clicked', got %q (full: %s)", outputText, checkResp.Result)
	} else {
		t.Log("  btn2_clicked ✓")
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
	t.Log("snapshot+click_ref test complete ✓")
}

// ======================================================================
// Test Servers
// ======================================================================

// startFormElementsServer serves a page with inputs, select, checkboxes, and
// focus/change event logging to an <output> element.

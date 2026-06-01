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

func startFormElementsServer(t *testing.T) (string, func()) {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Form Elements Test</title></head><body>
<div id="app">
  <h1 id="title">Form Elements Test</h1>
  <input id="textInput" type="text" placeholder="Text input">
  <input id="focusInput" type="text" placeholder="Focus target">
  <select id="fruitSelect" aria-label="Fruit selector">
    <option value="">-- Choose --</option>
    <option value="apple">Apple</option>
    <option value="banana">Banana</option>
    <option value="cherry">Cherry</option>
  </select>
  <label><input id="cbAgree" type="checkbox" name="agree"> Agree</label>
  <label><input id="cbNews" type="checkbox" name="news" aria-label="Newsletter checkbox"> Newsletter</label>
  <input id="radioA" type="radio" name="group" value="a"> A
  <input id="radioB" type="radio" name="group" value="b"> B
  <div id="output"></div>
</div>
<script>
(function(){
  var o=document.getElementById('output');
  function log(e){o.textContent+=e+';';}
  document.getElementById('focusInput').addEventListener('focus',function(){log('focus_fired');});
  document.getElementById('textInput').addEventListener('input',function(){log('input:'+this.value+';');});
  document.getElementById('fruitSelect').addEventListener('change',function(){log('select:'+this.value+';');});
  document.getElementById('cbAgree').addEventListener('change',function(){log('cbAgree:'+this.checked+';');});
  document.getElementById('cbNews').addEventListener('change',function(){log('cbNews:'+this.checked+';');});
  document.getElementById('radioA').addEventListener('change',function(){log('radioA_checked;');});
  document.getElementById('radioB').addEventListener('change',function(){log('radioB_checked;');});
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



// ======================================================================
// TestE2E_FormElements — covers focus, type, select, check, uncheck + refs
// ======================================================================

func TestE2E_FormElements(t *testing.T) {
	f := setupE2E(t)
	defer f.cleanup()

	testURL, stopServer := startFormElementsServer(t)
	defer stopServer()
	t.Logf("form elements server: %s", testURL)

	envID := f.getFirstEnvID()
	sessionID := browserOpenNavigate(t, f, envID, testURL)
	defer browserCloseHelper(t, f, envID)

	outputFn := func() string { return evalText(t, f, envID, `document.getElementById('output').textContent`) }

	// ── 1. browser_focus → verify focus event fired ──
	t.Run("focus", func(t *testing.T) {
		_, err := f.client.callTool("browser_focus", map[string]any{
			"envId": envID, "selector": "#focusInput", "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("focus: %v", err)
		}
		out := outputFn()
		if !strings.Contains(out, "focus_fired") {
			t.Errorf("expected focus_fired, got %q", out)
		}
		t.Logf("focus ✓ output=%s", out)
	})

	// ── 2. browser_type → verifies text appended ──
	t.Run("type", func(t *testing.T) {
		_, err := f.client.callTool("browser_type", map[string]any{
			"envId": envID, "selector": "#textInput", "text": "Hello", "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("type: %v", err)
		}
		out := outputFn()
		if !strings.Contains(out, "Hello") {
			t.Errorf("expected Hello in output, got %q", out)
		}
		t.Logf("type ✓ output=%s", out)
	})

	// ── 3. browser_fill → verifies text cleared + replaced ──
	t.Run("fill", func(t *testing.T) {
		_, err := f.client.callTool("browser_fill", map[string]any{
			"envId": envID, "selector": "#textInput", "text": "World", "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("fill: %v", err)
		}
		// Clear output between tests
		_, _ = f.client.callTool("browser_evaluate", map[string]any{
			"envId": envID, "expression": `document.getElementById('output').textContent=''`,
		})
		// Type again to see value
		_, _ = f.client.callTool("browser_fill", map[string]any{
			"envId": envID, "selector": "#textInput", "text": "World", "sessionId": sessionID,
		})
		out := evalText(t, f, envID, `document.getElementById('textInput').value`)
		if out != "World" {
			t.Errorf("expected World, got %q", out)
		}
		t.Logf("fill ✓ value=%s", out)
	})

	// ── 4. browser_select_option ──
	t.Run("select_option", func(t *testing.T) {
		_, err := f.client.callTool("browser_select_option", map[string]any{
			"envId": envID, "selector": "#fruitSelect", "value": "banana", "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("select_option: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
		out := outputFn()
		if !strings.Contains(out, "select:banana") {
			t.Errorf("expected select:banana, got %q", out)
		}
		t.Logf("select_option ✓ output=%s", out)
	})

	// ── 5. browser_check ──
	t.Run("check", func(t *testing.T) {
		_, err := f.client.callTool("browser_check", map[string]any{
			"envId": envID, "selector": "#cbAgree", "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("check: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
		out := outputFn()
		if !strings.Contains(out, "cbAgree:true") {
			t.Errorf("expected cbAgree:true, got %q", out)
		}
		t.Logf("check ✓ output=%s", out)
	})

	// ── 6. browser_uncheck ──
	t.Run("uncheck", func(t *testing.T) {
		_, err := f.client.callTool("browser_uncheck", map[string]any{
			"envId": envID, "selector": "#cbAgree", "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("uncheck: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
		out := outputFn()
		if !strings.Contains(out, "cbAgree:false") {
			t.Errorf("expected cbAgree:false, got %q", out)
		}
		t.Logf("uncheck ✓ output=%s", out)
	})

	// ── 7. snapshot → focus_ref, type_ref, fill_ref ──
	t.Run("snapshot_refs", func(t *testing.T) {
		// Get snapshot
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

		// focus_ref on #focusInput
		refFocus, found := findBackendDOMNodeID(snapTree, "Focus target")
		if !found {
			t.Fatal("focusInput not found in snapshot")
		}
		// Clear output first
		_, _ = f.client.callTool("browser_evaluate", map[string]any{
			"envId": envID, "expression": `document.getElementById('output').textContent=''`,
		})
		_, err = f.client.callTool("browser_focus_ref", map[string]any{
			"envId": envID, "ref": refFocus, "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("focus_ref: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
		out := outputFn()
		if !strings.Contains(out, "focus_fired") {
			t.Errorf("focus_ref: expected focus_fired, got %q", out)
		}
		t.Logf("focus_ref ✓ ref=%s output=%s", refFocus, out)

		// type_ref on #textInput
		refInput, found := findBackendDOMNodeID(snapTree, "Text input")
		if !found {
			t.Fatal("textInput not found in snapshot")
		}
		_, err = f.client.callTool("browser_type_ref", map[string]any{
			"envId": envID, "ref": refInput, "text": "Ref", "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("type_ref: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
		val := evalText(t, f, envID, `document.getElementById('textInput').value`)
		if !strings.Contains(val, "Ref") {
			t.Errorf("type_ref: expected value containing Ref, got %q", val)
		}
		t.Logf("type_ref ✓ ref=%s value=%s", refInput, val)

		// fill_ref on #textInput
		_, err = f.client.callTool("browser_fill_ref", map[string]any{
			"envId": envID, "ref": refInput, "text": "Filled", "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("fill_ref: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
		val = evalText(t, f, envID, `document.getElementById('textInput').value`)
		if val != "Filled" {
			t.Errorf("fill_ref: expected Filled, got %q", val)
		}
		t.Logf("fill_ref ✓ ref=%s value=%s", refInput, val)
	})

	// ── 8. select_option_ref ──
	t.Run("select_option_ref", func(t *testing.T) {
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
		// Search by the <select> aria-label, not the <option> text,
		// so SelectOptionRef resolves the select element (not an option child).
		ref, found := findBackendDOMNodeID(snapTree, "Fruit selector")
		if !found {
			t.Fatal("select element not found in snapshot (looking for Fruit selector)")
		}
		_, _ = f.client.callTool("browser_evaluate", map[string]any{
			"envId": envID, "expression": `document.getElementById('output').textContent=''`,
		})
		_, err = f.client.callTool("browser_select_option_ref", map[string]any{
			"envId": envID, "ref": ref, "value": "cherry", "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("select_option_ref: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
		out := outputFn()
		if !strings.Contains(out, "select:cherry") {
			t.Errorf("select_option_ref: expected select:cherry, got %q", out)
		}
		t.Logf("select_option_ref ✓ ref=%s output=%s", ref, out)
	})

	// ── 9. check_ref ──
	t.Run("check_ref", func(t *testing.T) {
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
		ref, found := findBackendDOMNodeID(snapTree, "Newsletter checkbox")
		if !found {
			t.Fatal("newsletter checkbox not found in snapshot (looking for Newsletter checkbox)")
		}
		_, _ = f.client.callTool("browser_evaluate", map[string]any{
			"envId": envID, "expression": `document.getElementById('output').textContent=''`,
		})
		_, err = f.client.callTool("browser_check_ref", map[string]any{
			"envId": envID, "ref": ref, "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("check_ref: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
		out := outputFn()
		if !strings.Contains(out, "cbNews:true") {
			t.Errorf("check_ref: expected cbNews:true, got %q", out)
		}
		t.Logf("check_ref ✓ ref=%s output=%s", ref, out)
	})

	t.Log("form elements test complete ✓")
}

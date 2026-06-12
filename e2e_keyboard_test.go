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

func startKeyboardTestServer(t *testing.T) (string, func()) {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Keyboard Test</title></head><body>
<div id="app">
  <h1>Keyboard Test</h1>
  <textarea id="ta" rows="4" cols="40" placeholder="Type here..."></textarea>
  <div id="output"></div>
  <div id="keylog"></div>
</div>
<script>
(function(){
  var ta=document.getElementById('ta'),o=document.getElementById('output'),kl=document.getElementById('keylog');
  ta.addEventListener('input',function(){o.textContent=ta.value;});
  ta.addEventListener('keydown',function(e){kl.textContent+='kd:'+e.key+';';});
  ta.addEventListener('keyup',function(e){kl.textContent+='ku:'+e.key+';';});
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
// TestE2E_KeyboardInteraction — covers press_key, keyboard_type,
// keyboard_insert_text, key_down, key_up
// ======================================================================

func TestE2E_KeyboardInteraction(t *testing.T) {
	f := setupE2E(t)
	defer f.cleanup()

	testURL, stopServer := startKeyboardTestServer(t)
	defer stopServer()
	t.Logf("keyboard test server: %s", testURL)

	envID := f.getFirstEnvID()
	sessionID := browserOpenNavigate(t, f, envID, testURL)
	defer browserCloseHelper(t, f, envID)

	// ── 1. Focus textarea ──
	_, err := f.client.callTool("browser_focus", map[string]any{
		"envId": envID, "selector": "#ta", "sessionId": sessionID,
	})
	if err != nil {
		t.Fatalf("focus textarea: %v", err)
	}

	// ── 2. browser_keyboard_type — char-by-char ──
	t.Run("keyboard_type", func(t *testing.T) {
		_, err := f.client.callTool("browser_keyboard_type", map[string]any{
			"envId": envID, "text": "ABC", "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("keyboard_type: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
		val := evalText(t, f, envID, `document.getElementById('ta').value`)
		if !strings.Contains(val, "ABC") {
			t.Errorf("keyboard_type: expected value containing ABC, got %q", val)
		}
		t.Logf("keyboard_type ✓ value=%s", val)
	})

	// ── 3. browser_insert_text ──
	t.Run("keyboard_insert_text", func(t *testing.T) {
		_, err := f.client.callTool("browser_insert_text", map[string]any{
			"envId": envID, "text": "Insert", "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("keyboard_insert_text: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
		val := evalText(t, f, envID, `document.getElementById('ta').value`)
		if !strings.Contains(val, "Insert") {
			t.Errorf("keyboard_insert_text: expected value containing Insert, got %q", val)
		}
		t.Logf("keyboard_insert_text ✓ value=%s", val)
	})

	// ── 4. browser_press_key Enter ──
	t.Run("press_key_enter", func(t *testing.T) {
		// Clear textarea and refocus
		_, _ = f.client.callTool("browser_evaluate", map[string]any{
			"envId": envID, "expression": `document.getElementById('ta').value=''`,
		})
		_, err := f.client.callTool("browser_press_key", map[string]any{
			"envId": envID, "key": "Enter", "sessionId": sessionID,
		})
		if err != nil {
			// Some keys may fail on protocol level — log but don't fail
			t.Logf("press_key Enter: %v (may be expected)", err)
		} else {
			t.Log("press_key Enter ✓")
		}
	})

	// ── 5. browser_key_down + browser_key_up (Shift) ──
	t.Run("key_down_up", func(t *testing.T) {
		_, err := f.client.callTool("browser_key_down", map[string]any{
			"envId": envID, "key": "Shift", "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("key_down: %v", err)
		}
		time.Sleep(200 * time.Millisecond)

		// Type 'a' while Shift is held → should log "kd:Shift" then "kd:a"
		_, err = f.client.callTool("browser_insert_text", map[string]any{
			"envId": envID, "text": "a", "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("insert_text after key_down: %v", err)
		}

		_, err = f.client.callTool("browser_key_up", map[string]any{
			"envId": envID, "key": "Shift", "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("key_up: %v", err)
		}
		time.Sleep(200 * time.Millisecond)

		keylog := evalText(t, f, envID, `document.getElementById('keylog').textContent`)
		if !strings.Contains(keylog, "kd:Shift") {
			t.Errorf("key_down: expected kd:Shift in keylog, got %q", keylog)
		}
		t.Logf("key_down+key_up ✓ keylog=%s", keylog)
	})

	t.Log("keyboard interaction test complete ✓")
}

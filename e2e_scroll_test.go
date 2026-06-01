//go:build windows

package main_test

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func startScrollTestServer(t *testing.T) (string, func()) {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Scroll Test</title>
<style>
  body { margin: 0; font-family: sans-serif; }
  section { height: 600px; display: flex; align-items: center; justify-content: center; border-bottom: 2px solid #ccc; }
  #top { background: #e3f2fd; } #middle { background: #fff3e0; } #bottom { background: #e8f5e9; }
  #anchored { padding: 20px; background: #fce4ec; text-align: center; }
</style>
</head><body>
<section id="top"><h1>Top of Page</h1></section>
<section id="middle"><h1>Middle of Page</h1></section>
<section id="bottom"><h1>Bottom of Page — <span id="bottomMarker">reached</span></h1></section>
<div id="anchored"><p>This element is at the bottom</p></div>
<script>
window.addEventListener('scroll',function(){
  var m=document.getElementById('bottomMarker');
  if(m.getBoundingClientRect().top<window.innerHeight){m.textContent='visible';}
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

// startDragTestServer serves a page with draggable elements, drop zones, and


// ======================================================================
// TestE2E_ScrollAndScreenshot — covers scroll, scroll_into_view, screenshot, pdf
// ======================================================================

func TestE2E_ScrollAndScreenshot(t *testing.T) {
	f := setupE2E(t)
	defer f.cleanup()

	testURL, stopServer := startScrollTestServer(t)
	defer stopServer()
	t.Logf("scroll test server: %s", testURL)

	envID := f.getFirstEnvID()
	sessionID := browserOpenNavigate(t, f, envID, testURL)
	defer browserCloseHelper(t, f, envID)

	// ── 1. browser_scroll down ──
	t.Run("scroll_down", func(t *testing.T) {
		_, err := f.client.callTool("browser_scroll", map[string]any{
			"envId": envID, "direction": "down", "px": float64(500), "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("scroll down: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
		scrollY := evalText(t, f, envID, `window.scrollY`)
		if scrollY == "0" {
			t.Error("scroll down: window.scrollY is still 0")
		}
		t.Logf("scroll_down ✓ scrollY=%s", scrollY)
	})

	// ── 2. browser_scroll up ──
	t.Run("scroll_up", func(t *testing.T) {
		// Go back up
		scrollUpY := evalText(t, f, envID, `window.scrollY`)
		_, err := f.client.callTool("browser_scroll", map[string]any{
			"envId": envID, "direction": "up", "px": float64(500), "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("scroll up: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
		newY := evalText(t, f, envID, `window.scrollY`)
		t.Logf("scroll_up ✓ scrollY: %s → %s", scrollUpY, newY)
	})

	// ── 3. browser_scroll_into_view ──
	t.Run("scroll_into_view", func(t *testing.T) {
		_, err := f.client.callTool("browser_scroll_into_view", map[string]any{
			"envId": envID, "selector": "#anchored", "sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("scroll_into_view: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
		// Verify the anchored element is visible
		visible := evalText(t, f, envID, `document.getElementById('bottomMarker').textContent`)
		if visible != "visible" {
			t.Errorf("scroll_into_view: bottomMarker should be 'visible', got %q", visible)
		}
		t.Logf("scroll_into_view ✓ bottomMarker=%s", visible)
	})

	// ── 4. browser_screenshot ──
	t.Run("screenshot", func(t *testing.T) {
		tmpDir := filepath.Join(os.TempDir(), "brosdk-e2e")
		os.MkdirAll(tmpDir, 0755)

		resp, err := f.client.callTool("browser_screenshot", map[string]any{
			"envId":         envID,
			"screenshotDir": tmpDir,
			"fullPage":      true,
			"sessionId":     sessionID,
		})
		if err != nil {
			t.Fatalf("screenshot: %v", err)
		}
		var scrResult map[string]any
		if err := parseToolText(resp.Result, &scrResult); err != nil {
			t.Fatalf("parse screenshot result: %v", err)
		}
		path, _ := scrResult["path"].(string)
		if path == "" {
			t.Fatal("screenshot: no path in result")
		}
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 {
			t.Fatalf("screenshot: file missing or empty: %s (err=%v)", path, err)
		}
		t.Logf("screenshot ✓ path=%s size=%d", path, info.Size())
	})

	// ── 5. browser_pdf ──
	t.Run("pdf", func(t *testing.T) {
		tmpDir := filepath.Join(os.TempDir(), "brosdk-e2e")
		os.MkdirAll(tmpDir, 0755)
		pdfPath := filepath.Join(tmpDir, "e2e-test.pdf")

		resp, err := f.client.callTool("browser_pdf", map[string]any{
			"envId":     envID,
			"path":      pdfPath,
			"sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("pdf: %v", err)
		}
		var pdfResult map[string]any
		if err := parseToolText(resp.Result, &pdfResult); err != nil {
			t.Fatalf("parse pdf result: %v", err)
		}
		path, _ := pdfResult["path"].(string)
		if path == "" {
			t.Fatal("pdf: no path in result")
		}
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 {
			t.Fatalf("pdf: file missing or empty: %s (err=%v)", path, err)
		}
		t.Logf("pdf ✓ path=%s size=%d", path, info.Size())
	})

	t.Log("scroll & screenshot test complete ✓")
}

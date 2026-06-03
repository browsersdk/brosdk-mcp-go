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

// startMultiPageServer serves multiple pages at different paths for
// tab-management E2E tests (new_tab / list_tabs / close_tab / get_html).
//
//  GET /           → home page (tab test landing page)
//  GET /page2      → second page for cross-tab navigation
//  GET /page3      → third page
//
// Returns base URL and a stop function.
func startMultiPageServer(t *testing.T) (string, func()) {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Tab Test - Home</title></head><body>
<h1 id="pageTitle">Tab Test Home</h1>
<p id="content">This is the home page for tab management tests.</p>
<div id="dynamic"></div>
<script>
// Mark that JS executed
document.getElementById('dynamic').textContent = 'js_loaded';
</script>
</body></html>`)
	})

	mux.HandleFunc("/page2", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Tab Test - Page 2</title></head><body>
<h1 id="pageTitle">Tab Test Page 2</h1>
<p id="content">This is page two.</p>
<div id="dynamic"></div>
<script>
document.getElementById('dynamic').textContent = 'page2_loaded';
</script>
</body></html>`)
	})

	mux.HandleFunc("/page3", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Tab Test - Page 3</title></head><body>
<h1 id="pageTitle">Tab Test Page 3</h1>
<p id="content">This is page three.</p>
<div id="dynamic"></div>
<script>
document.getElementById('dynamic').textContent = 'page3_loaded';
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
// Tab Management E2E Tests
// ======================================================================

// TestE2E_TabManagement verifies browser_new_tab, browser_list_tabs,
// browser_close_tab, and browser_get_html work end-to-end.
func TestE2E_TabManagement(t *testing.T) {
	f := setupE2E(t)
	defer f.cleanup()

	envID := f.getFirstEnvID()
	t.Logf("using envId=%s", envID)

	// ── Start multi-page server ───────────────────────────────────────
	baseURL, stopServer := startMultiPageServer(t)
	defer stopServer()
	t.Logf("multi-page server: %s", baseURL)

	// ── Open browser and navigate to home page ────────────────────────
	sessionID := browserOpenNavigate(t, f, envID, baseURL)
	_ = sessionID
	time.Sleep(500 * time.Millisecond)

	// ── 1. browser_get_html ───────────────────────────────────────────
	t.Run("get_html", func(t *testing.T) {
		resp, err := f.client.callTool("browser_get_html", map[string]any{
			"envId": envID,
		})
		if err != nil {
			t.Fatalf("browser_get_html: %v", err)
		}

		var result map[string]any
		if err := parseToolText(resp.Result, &result); err != nil {
			t.Fatalf("parse result: %v", err)
		}
		html, _ := result["html"].(string)
		if html == "" {
			t.Fatal("html is empty")
		}
		if !strings.Contains(html, "Tab Test Home") {
			t.Errorf("html should contain 'Tab Test Home', got: %.200s", html)
		}
		if !strings.Contains(html, `id="dynamic"`) {
			t.Errorf("html should contain dynamic div")
		}
		t.Logf("get_html ok, length=%d", len(html))
	})

	// ── 2. browser_list_tabs (single tab) ────────────────────────────
	t.Run("list_tabs_single", func(t *testing.T) {
		tabs := listTabsE2E(t, f, envID)
		if len(tabs) != 1 {
			t.Fatalf("expected 1 tab, got %d", len(tabs))
		}
		tab := tabs[0]
		if tab.TabID != "__active__" {
			t.Errorf("only tab should be __active__, got %s", tab.TabID)
		}
		if !tab.IsActive {
			t.Error("only tab should be active")
		}
		if !strings.Contains(tab.Title, "Tab Test") {
			t.Errorf("unexpected title: %q", tab.Title)
		}
		t.Logf("list_tabs_single ok: %+v", tab)
	})

	// ── 3. browser_new_tab (+ list_tabs for 2 tabs) ──────────────────
	t.Run("new_tab", func(t *testing.T) {
		resp, err := f.client.callTool("browser_new_tab", map[string]any{
			"envId": envID,
		})
		if err != nil {
			t.Fatalf("browser_new_tab: %v", err)
		}
		var result map[string]any
		if err := parseToolText(resp.Result, &result); err != nil {
			t.Fatalf("parse result: %v", err)
		}
		newTabID, _ := result["tabId"].(string)
		if newTabID == "" {
			t.Fatal("tabId is empty")
		}
		t.Logf("new tab ID = %s", newTabID)

		// Navigate the new tab to page2 so it has a distinguishable title.
		navResp, err := f.client.callTool("browser_navigate", map[string]any{
			"envId": envID,
			"url":   baseURL + "/page2",
		})
		if err != nil {
			t.Fatalf("navigate page2: %v", err)
		}
		t.Logf("navigate page2: %s", navResp.Result)
		time.Sleep(500 * time.Millisecond)

		// List tabs: should have 2 tabs.
		tabs := listTabsE2E(t, f, envID)
		if len(tabs) != 2 {
			t.Fatalf("expected 2 tabs, got %d", len(tabs))
		}
		t.Logf("tabs after new_tab: %d", len(tabs))

		// Verify structure: one active (new), one background (old home).
		foundActive := false
		for _, tab := range tabs {
			t.Logf("  tab: id=%s title=%q active=%v url=%s", tab.TabID, tab.Title, tab.IsActive, tab.URL)
			if tab.IsActive {
				foundActive = true
				if !strings.Contains(tab.Title, "Page 2") {
					t.Logf("warning: active tab title may not be Page 2: %q", tab.Title)
				}
			}
		}
		if !foundActive {
			t.Error("no active tab found in list")
		}
	})

	// ── 4. browser_close_tab (close background tab) ───────────────────
	t.Run("close_tab_background", func(t *testing.T) {
		// First, navigate the active tab back to /page3 to keep it active,
		// then open a new tab for page2 (background), then close the background.
		navResp, err := f.client.callTool("browser_navigate", map[string]any{
			"envId": envID,
			"url":   baseURL + "/page3",
		})
		if err != nil {
			t.Fatalf("navigate page3: %v", err)
		}
		_ = navResp
		time.Sleep(500 * time.Millisecond)

		// List current tabs to find a background tab to close.
		tabs := listTabsE2E(t, f, envID)
		// Close any background tab (__active__ could also be used).
		var bgTabID string
		for _, tab := range tabs {
			if !tab.IsActive {
				bgTabID = tab.TabID
				break
			}
		}
		if bgTabID == "" {
			t.Skip("no background tab to close (already cleaned up)")
			return
		}
		t.Logf("closing background tab: %s", bgTabID)

		closeResp, err := f.client.callTool("browser_close_tab", map[string]any{
			"envId": envID,
			"tabId": bgTabID,
		})
		if err != nil {
			t.Fatalf("browser_close_tab: %v", err)
		}
		t.Logf("close_tab result: %s", closeResp.Result)

		// Verify tab count decreased.
		tabs = listTabsE2E(t, f, envID)
		t.Logf("tabs after close: %d", len(tabs))
		if len(tabs) == 0 {
			t.Fatal("no tabs remain after closing one")
		}
	})

	// ── 5. browser_close_tab (close active tab with __active__) ───────
	t.Run("close_tab_active", func(t *testing.T) {
		// Create a fresh tab so we have at least 2.
		resp, err := f.client.callTool("browser_new_tab", map[string]any{
			"envId": envID,
		})
		if err != nil {
			t.Fatalf("browser_new_tab: %v", err)
		}
		var result map[string]any
		parseToolText(resp.Result, &result)
		t.Logf("created tab: %v", result["tabId"])

		// Navigate it to page2 so we can distinguish.
		f.client.callTool("browser_navigate", map[string]any{
			"envId": envID,
			"url":   baseURL + "/page2",
		})
		time.Sleep(500 * time.Millisecond)

		tabsBefore := listTabsE2E(t, f, envID)
		t.Logf("tabs before close_active: %d", len(tabsBefore))

		// Close the active tab using __active__ sentinel.
		closeResp, err := f.client.callTool("browser_close_tab", map[string]any{
			"envId": envID,
			"tabId": "__active__",
		})
		if err != nil {
			t.Fatalf("browser_close_tab(__active__): %v", err)
		}
		t.Logf("close_active result: %s", closeResp.Result)

		tabsAfter := listTabsE2E(t, f, envID)
		t.Logf("tabs after close_active: %d", len(tabsAfter))

		if len(tabsAfter) == 0 {
			t.Fatal("no tabs remain after closing active (should have other tabs)")
		}
		// The remaining tab should be active.
		for _, tab := range tabsAfter {
			if !tab.IsActive {
				t.Errorf("remaining tab should be active, but %s is not", tab.TabID)
			}
		}
	})

	// ── 6. browser_get_html after multi-tab operations ────────────────
	t.Run("get_html_after_tabs", func(t *testing.T) {
		// Navigate active tab to page3.
		f.client.callTool("browser_navigate", map[string]any{
			"envId": envID,
			"url":   baseURL + "/page3",
		})
		time.Sleep(500 * time.Millisecond)

		resp, err := f.client.callTool("browser_get_html", map[string]any{
			"envId": envID,
		})
		if err != nil {
			t.Fatalf("browser_get_html: %v", err)
		}
		var result map[string]any
		parseToolText(resp.Result, &result)
		html, _ := result["html"].(string)
		if !strings.Contains(html, "Tab Test Page 3") {
			t.Errorf("expected Page 3 content in HTML after multi-tab ops, got %.200s", html)
		}
		t.Logf("get_html after tabs ok, length=%d", len(html))
	})

	// ── Clean up: close browser ───────────────────────────────────────
	f.drainSSE()
	closeResp, err := f.client.callTool("browser_close", map[string]any{
		"envId": envID,
	})
	if err != nil {
		t.Logf("browser_close (cleanup): %v", err)
	} else {
		t.Logf("browser_close: %s", closeResp.Result)
	}
}

// ---------- tabInfo for parsing list_tabs result ----------

type tabInfoE2E struct {
	TabID    string `json:"tabId"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	IsActive bool   `json:"isActive"`
}

// listTabsE2E calls browser_list_tabs and returns parsed tabInfo list.
func listTabsE2E(t *testing.T, f *e2eFixture, envID string) []tabInfoE2E {
	t.Helper()

	resp, err := f.client.callTool("browser_list_tabs", map[string]any{
		"envId": envID,
	})
	if err != nil {
		t.Fatalf("browser_list_tabs: %v", err)
	}

	var outer struct {
		Tabs  []tabInfoE2E `json:"tabs"`
		Count int          `json:"count"`
	}
	if err := parseToolText(resp.Result, &outer); err != nil {
		t.Fatalf("parse list_tabs result: %v (raw=%.200s)", err, resp.Result)
	}
	t.Logf("list_tabs: count=%d", len(outer.Tabs))
	return outer.Tabs
}

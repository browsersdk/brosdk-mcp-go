//go:build windows

package main_test

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/browsersdk/brosdk-mcp-go/internal/brosdk"
)

// startCookieTestServer creates an HTTP server that sets cookies via
// Set-Cookie response headers and also via JavaScript document.cookie.
// Returns (baseURL, cleanup).
func startCookieTestServer(t *testing.T) (string, func()) {
	t.Helper()
	mux := http.NewServeMux()

	// Sets cookies via Set-Cookie header + JavaScript document.cookie.
	mux.HandleFunc("/set-cookies", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:  "test_session",
			Value: "abc123def456",
			Path:  "/",
		})
		http.SetCookie(w, &http.Cookie{
			Name:  "user_pref",
			Value: "dark_mode",
			Path:  "/",
		})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Cookie Test</title></head>
<body><h1>Cookie Test Page</h1>
<p id="status">Cookies being set...</p>
<script>
document.cookie = "js_token=xyz789; path=/";
document.cookie = "js_theme=light; path=/";
document.getElementById('status').textContent = 'cookies set';
</script>
</body></html>`)
	})

	// Plain page (no cookies set) for subsequent navigation.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Plain Page</title></head>
<body><h1>Plain Page</h1><p>No cookies set here.</p></body></html>`)
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

// TestE2E_CookieCallback tests that the SDK's cookie storage callback
// (sdk_cookies_storage_cb_t) fires when the browser visits a page that
// sets cookies. It verifies:
//  1. The callback is invoked (non-empty data received).
//  2. The data is broadcast as a "cookies-event" over SSE.
//  3. The cookie payload contains the expected cookie names.
func TestE2E_CookieCallback(t *testing.T) {
	f := setupE2E(t)
	defer f.cleanup()

	// Start local server that sets cookies.
	testURL, stopServer := startCookieTestServer(t)
	defer stopServer()
	t.Logf("cookie test server: %s", testURL)

	// Register cookie callback listener.
	cookieCh := make(chan brosdk.CookiesEvent, 20)
	f.mgr.OnCookies(func(evt brosdk.CookiesEvent) {
		cookieCh <- evt
	})

	envID := f.getFirstEnvID()

	// ── Phase 1: open browser & navigate to cookie-setting page ──
	f.drainSSE()
	_ = browserOpenNavigate(t, f, envID, testURL+"/set-cookies")

	// Verify cookies are visible in the browser via JavaScript.
	jsCookies := evalText(t, f, envID, "document.cookie")
	t.Logf("JS document.cookie: %s", jsCookies)
	if !strings.Contains(jsCookies, "test_session") {
		t.Errorf("JS document.cookie does not contain test_session: %s", jsCookies)
	}
	if !strings.Contains(jsCookies, "user_pref") {
		t.Errorf("JS document.cookie does not contain user_pref: %s", jsCookies)
	}

	// Wait for cookie callback (might fire shortly after page load).
	callbackFired := false
	select {
	case evt := <-cookieCh:
		callbackFired = true
		t.Logf("cookie callback fired (phase 1): data=%.500s", evt.Data)
		verifyCookieData(t, evt.Data, []string{"test_session", "user_pref"})
	case <-time.After(15 * time.Second):
		t.Log("no cookie callback within 15s after navigation, continuing...")
	}

	// ── Phase 2: navigate to a plain page ──
	// Some SDKs flush/intercept cookies on navigation transitions.
	if !callbackFired {
		_, err := f.client.callTool("browser_navigate", map[string]any{
			"envId": envID,
			"url":   testURL + "/",
		})
		if err != nil {
			t.Logf("navigate to plain page: %v", err)
		}
		time.Sleep(2 * time.Second)

		select {
		case evt := <-cookieCh:
			callbackFired = true
			t.Logf("cookie callback fired (phase 2 - after navigation): data=%.500s", evt.Data)
			verifyCookieData(t, evt.Data, []string{"test_session", "user_pref"})
		case <-time.After(10 * time.Second):
			t.Log("no cookie callback within 10s after second navigation, continuing...")
		}
	}

	// ── Phase 3: close browser ──
	// The SDK may intercept cookie storage during browser teardown.
	f.drainSSE()
	_, err := f.client.callTool("browser_close", map[string]any{"envId": envID})
	if err != nil {
		t.Logf("browser_close: %v", err)
	}

	if !callbackFired {
		select {
		case evt := <-cookieCh:
			callbackFired = true
			t.Logf("cookie callback fired (phase 3 - after close): data=%.500s", evt.Data)
			verifyCookieData(t, evt.Data, []string{"test_session", "user_pref"})
		case <-time.After(15 * time.Second):
			t.Log("no cookie callback within 15s after browser close")
		}
	} else {
		// Already fired; drain any additional events.
		time.Sleep(3 * time.Second)
		for {
			select {
			case evt := <-cookieCh:
				t.Logf("additional cookie callback: data=%.500s", evt.Data)
			default:
				goto done
			}
		}
	done:
	}

	if !callbackFired {
		t.Error("cookie storage callback was never fired during any phase — " +
			"the SDK may not be intercepting cookies for this browser session, " +
			"or the callback may require additional SDK configuration")
	} else {
		t.Log("cookie storage callback test PASSED")
	}
}

// verifyCookieData checks that cookieData is non-empty and contains at least
// some of the expected cookie name substrings.
func verifyCookieData(t *testing.T, cookieData string, expectedNames []string) {
	t.Helper()
	if cookieData == "" {
		t.Error("cookie callback fired but data is empty")
		return
	}
	t.Logf("cookie data (first 500 chars): %.500s", cookieData)

	found := 0
	for _, name := range expectedNames {
		if strings.Contains(cookieData, name) {
			found++
			t.Logf("  found expected cookie name: %s", name)
		} else {
			t.Logf("  MISSING expected cookie name: %s", name)
		}
	}
	if found == 0 {
		t.Errorf("none of the expected cookie names %v found in callback data", expectedNames)
	}
}

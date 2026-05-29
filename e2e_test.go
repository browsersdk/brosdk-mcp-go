//go:build windows

package main_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/browsersdk/brosdk-mcp-go/internal/brosdk"
	"github.com/browsersdk/brosdk-mcp-go/internal/config"
	"github.com/browsersdk/brosdk-mcp-go/internal/mcp"
	"github.com/browsersdk/brosdk-mcp-go/internal/tools"
)

// ---------- JSON-RPC helpers ----------

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// mcpClient sends JSON-RPC requests to the MCP SSE server.
type mcpClient struct {
	baseURL string
	idSeq   int
}

func newMCPClient(baseURL string) *mcpClient {
	return &mcpClient{baseURL: baseURL}
}

func (c *mcpClient) nextID() int {
	c.idSeq++
	return c.idSeq
}

func (c *mcpClient) call(method string, params any) (*jsonRPCResponse, error) {
	id := c.nextID()
	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		rawParams = b
	}

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  rawParams,
	}
	body, _ := json.Marshal(req)

	httpResp, err := http.Post(c.baseURL+"/message", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("http post: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", httpResp.StatusCode)
	}

	var resp jsonRPCResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return &resp, nil
}

// callTool is a convenience wrapper: tools/call with name + arguments.
func (c *mcpClient) callTool(name string, args map[string]any) (*jsonRPCResponse, error) {
	argBytes, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("marshal args: %w", err)
	}
	return c.call("tools/call", toolCallParams{
		Name:      name,
		Arguments: argBytes,
	})
}

// ---------- SSE event reader ----------

type sseEvent struct {
	Event string
	Data  string
}

// readSSE opens an SSE stream and pushes parsed events into ch until ctx is
// done or the stream ends. It never blocks the caller.
func readSSE(ctx context.Context, url string, ch chan<- sseEvent) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Accept", "text/event-stream")

	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return err
	}

	go func() {
		defer httpResp.Body.Close()
		scanner := bufio.NewScanner(httpResp.Body)
		// SSE lines can be long; bump buffer.
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		var ev sseEvent
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				// empty line -> dispatch accumulated event
				if ev.Event != "" || ev.Data != "" {
					select {
					case ch <- ev:
					case <-ctx.Done():
						return
					}
					ev = sseEvent{}
				}
				continue
			}
			if strings.HasPrefix(line, ":") {
				continue // comment / heartbeat
			}
			if strings.HasPrefix(line, "event:") {
				ev.Event = strings.TrimSpace(line[6:])
			} else if strings.HasPrefix(line, "data:") {
				ev.Data = strings.TrimSpace(line[5:])
			}
		}
	}()
	return nil
}

// ---------- E2E fixture ----------

// e2eFixture holds all shared state for E2E subtests.
type e2eFixture struct {
	t       *testing.T
	mgr     *brosdk.Manager
	srv     *mcp.Server
	httpSrv *http.Server
	client  *mcpClient
	sseCh   chan sseEvent
	cancel  context.CancelFunc
	baseURL string
	cfg     *config.Config

	// cached environment id from env_page.
	firstEnvID string
}

// setupE2E loads config, initialises the SDK, starts the MCP HTTP server,
// and connects an SSE stream. Call f.cleanup() when done.
func setupE2E(t *testing.T) *e2eFixture {
	t.Helper()

	// ── Resolve working directory ──────────────────────────────────────
	wd, _ := os.Getwd()
	t.Logf("working directory: %s", wd)

	// ── Load config ───────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg == nil {
		t.Skip("no config.local.json or config.json found — skipping e2e test")
	}
	t.Logf("config: apiKey=%s port=%d workDir=%s", maskStr(cfg.ApiKey), cfg.Port, cfg.WorkDir)

	// ── Resolve DLL ───────────────────────────────────────────────────
	dllPath := resolveDLL()
	t.Logf("dll: %s", dllPath)

	// ── Load native library ───────────────────────────────────────────
	mgr := brosdk.NewManager()
	if err := mgr.Load(dllPath); err != nil {
		t.Fatalf("load dll: %v", err)
	}

	// ── sdk_init ──────────────────────────────────────────────────────
	opts := brosdk.InitOptions{
		UserSig:   cfg.UserSig,
		ApiKey:    cfg.ApiKey,
		WorkDir:   cfg.WorkDir,
		Port:      cfg.Port,
		SdkApiURL: cfg.SdkApiURL,
		Debug:     cfg.Debug,
	}
	if opts.WorkDir == "" {
		opts.WorkDir = filepath.Join(".", "brosdk")
	}
	if opts.Port <= 0 {
		opts.Port = 5811
	}
	if err := os.MkdirAll(opts.WorkDir, 0755); err != nil {
		t.Fatalf("mkdir workDir: %v", err)
	}

	resp, err := mgr.Init(opts)
	if err != nil {
		t.Fatalf("sdk_init: %v", err)
	}
	t.Logf("sdk_init ok, code=%d", resp.Code)

	// ── Start MCP HTTP server ─────────────────────────────────────────
	srv := mcp.NewServer(
		mcp.ServerInfo{Name: "brosdk-mcp", Version: "test"},
		tools.All(),
		tools.Handler(mgr),
	)

	// Wire SDK async events into SSE broadcast.
	mgr.OnEvent(func(evt brosdk.Event) {
		data, _ := json.Marshal(evt)
		srv.Broadcast("sdk-event", string(data))
	})

	mux := http.NewServeMux()
	srv.Register(mux)

	httpSrv := &http.Server{Addr: "127.0.0.1:18766", Handler: mux}
	go func() { _ = httpSrv.ListenAndServe() }()

	time.Sleep(200 * time.Millisecond)

	baseURL := "http://127.0.0.1:18766"
	t.Logf("MCP server at %s", baseURL)

	// Health check
	hcResp, err := http.Get(baseURL + "/health")
	if err != nil {
		t.Fatalf("health check: %v", err)
	}
	hcResp.Body.Close()
	if hcResp.StatusCode != http.StatusOK {
		t.Fatalf("health: %d", hcResp.StatusCode)
	}

	// ── MCP client ────────────────────────────────────────────────────
	client := newMCPClient(baseURL)

	// MCP handshake
	initResp, err := client.call("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]string{"name": "e2e-test"},
	})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	t.Logf("initialize: ok (result=%s)", initResp.Result)

	// Send initialized notification
	client.call("notifications/initialized", nil)

	// ── Connect SSE stream ────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())

	sseCh := make(chan sseEvent, 64)
	if err := readSSE(ctx, baseURL+"/sse", sseCh); err != nil {
		cancel()
		t.Fatalf("sse connect: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	t.Log("SSE connected")

	return &e2eFixture{
		t:       t,
		mgr:     mgr,
		srv:     srv,
		httpSrv: httpSrv,
		client:  client,
		sseCh:   sseCh,
		cancel:  cancel,
		baseURL: baseURL,
		cfg:     cfg,
	}
}

func (f *e2eFixture) cleanup() {
	f.cancel()
	f.httpSrv.Close()
	// Best-effort SDK shutdown.
	if f.mgr.Loaded() {
		_ = f.mgr.Shutdown()
	}
}

	// waitSSEEvent drains SSE events until one matches dataContains, or timeout.
// Returns the matching event and true, or zero-value + false on timeout.
// If logFull is true, logs the complete data payload (useful for debugging).
func (f *e2eFixture) waitSSEEvent(dataContains string, timeout time.Duration) (brosdk.Event, bool) {
	return f.waitSSEEventLog(dataContains, timeout, false)
}

func (f *e2eFixture) waitSSEEventLog(dataContains string, timeout time.Duration, logFull bool) (brosdk.Event, bool) {
	timer := time.After(timeout)
	for {
		select {
		case ev := <-f.sseCh:
			if ev.Event == "sdk-event" {
				var sdkEvt brosdk.Event
				if json.Unmarshal([]byte(ev.Data), &sdkEvt) == nil {
					if logFull {
						f.t.Logf("  SSE: code=%d data=%s", sdkEvt.Code, sdkEvt.Data)
					} else {
						f.t.Logf("  SSE: code=%d data=%.120s", sdkEvt.Code, sdkEvt.Data)
					}
					if dataContains == "" || strings.Contains(sdkEvt.Data, dataContains) {
						return sdkEvt, true
					}
				}
			}
		case <-timer:
			return brosdk.Event{}, false
		}
	}
}

// drainSSE reads and discards all currently queued SSE events (non-blocking).
func (f *e2eFixture) drainSSE() {
	for {
		select {
		case <-f.sseCh:
		default:
			return
		}
	}
}

// getFirstEnvID calls env_page and extracts the first envId.
// Result is cached in f.firstEnvID.
func (f *e2eFixture) getFirstEnvID() string {
	f.t.Helper()
	if f.firstEnvID != "" {
		return f.firstEnvID
	}
	resp, err := f.client.callTool("env_page", map[string]any{
		"page":     float64(1),
		"pageSize": float64(5),
	})
	if err != nil {
		f.t.Fatalf("env_page: %v", err)
	}
	id, err := extractFirstEnvID(resp.Result)
	if err != nil {
		f.t.Fatalf("extract envId: %v (raw=%.200s)", err, resp.Result)
	}
	f.firstEnvID = id
	f.t.Logf("first envId = %s", id)
	return id
}

// ---------- Result extractors ----------

// extractFirstEnvID parses a tools/call result for env_page and returns
// the first environment's envId.
func extractFirstEnvID(raw json.RawMessage) (string, error) {
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("unmarshal tool result: %w", err)
	}
	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty content")
	}

	var envData map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &envData); err != nil {
		return "", fmt.Errorf("unmarshal env text: %w (raw: %.200s)", err, result.Content[0].Text)
	}

	// Try "list" array at top level.
	if list, ok := envData["list"].([]any); ok && len(list) > 0 {
		if entry, ok := list[0].(map[string]any); ok {
			if id, ok := entry["envId"].(string); ok && id != "" {
				return id, nil
			}
		}
	}

	// Try "data.list" (SDK-wrapped response).
	if data, ok := envData["data"].(map[string]any); ok {
		if list, ok := data["list"].([]any); ok && len(list) > 0 {
			if entry, ok := list[0].(map[string]any); ok {
				if id, ok := entry["envId"].(string); ok && id != "" {
					return id, nil
				}
			}
		}
	}

	// Try single-env response with envId at top level.
	if id, ok := envData["envId"].(string); ok && id != "" {
		return id, nil
	}

	return "", fmt.Errorf("could not find envId in response: %.200s", result.Content[0].Text)
}

// extractEnvIDFromResult parses a tool result and looks for an "envId" field.
// Used for env_create, env_update, env_destroy responses.
func extractEnvIDFromResult(raw json.RawMessage) (string, error) {
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("unmarshal: %w", err)
	}
	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty content")
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &data); err != nil {
		return "", fmt.Errorf("unmarshal text: %w (raw: %.200s)", err, result.Content[0].Text)
	}

	// Direct envId.
	if id, ok := data["envId"].(string); ok && id != "" {
		return id, nil
	}
	// Wrapped in "data".
	if inner, ok := data["data"].(map[string]any); ok {
		if id, ok := inner["envId"].(string); ok && id != "" {
			return id, nil
		}
	}

	return "", fmt.Errorf("no envId found in response: %.200s", result.Content[0].Text)
}

// ---------- Helper utils ----------

func resolveDLL() string {
	candidates := []string{
		"brosdk.dll",
		filepath.Join("libs", "windows-x64", "brosdk.dll"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "brosdk.dll"
}

func maskStr(s string) string {
	if len(s) <= 8 {
		return "***"
	}
	return s[:4] + "***" + s[len(s)-4:]
}

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

// ---------- Validation helpers ----------

// validateToolResult checks that a tool result has the expected MCP content structure.
func validateToolResult(t *testing.T, raw json.RawMessage, toolName string) {
	t.Helper()
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Errorf("%s: invalid result JSON: %v", toolName, err)
		return
	}
	if result.IsError {
		text := ""
		if len(result.Content) > 0 {
			text = result.Content[0].Text
		}
		t.Errorf("%s: returned isError=true: %s", toolName, text)
		return
	}
	if len(result.Content) == 0 {
		t.Errorf("%s: empty content", toolName)
		return
	}
	if result.Content[0].Type != "text" {
		t.Errorf("%s: unexpected content type %q", toolName, result.Content[0].Type)
	}
	t.Logf("%s: ok (%.120s)", toolName, result.Content[0].Text)
}

// extractTextField extracts a string field from a tool result's text content.
func extractTextField(raw json.RawMessage, field string) (string, bool) {
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || len(result.Content) == 0 {
		return "", false
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &data); err != nil {
		return "", false
	}
	if v, ok := data[field].(string); ok {
		return v, true
	}
	return "", false
}

// parseToolText unmarshals a tool result's text content into v.
func parseToolText(raw json.RawMessage, v any) error {
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return err
	}
	if len(result.Content) == 0 {
		return fmt.Errorf("empty content")
	}
	return json.Unmarshal([]byte(result.Content[0].Text), v)
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
func findBackendDOMNodeID(tree map[string]any, targetName string) (string, bool) {
	return walkAXNode(tree, targetName)
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

	// Parse the accessibility tree.
	var snapResult map[string]any
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

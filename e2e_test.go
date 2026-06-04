//go:build windows

package main_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/browsersdk/brosdk-mcp-go/internal/brosdk"
	"github.com/browsersdk/brosdk-mcp-go/internal/config"
	"github.com/browsersdk/brosdk-mcp-go/internal/mcp"
	"github.com/browsersdk/brosdk-mcp-go/internal/tools"
)

var sharedE2E struct {
	mu      sync.Mutex
	ready   bool
	skipped string
	mgr     *brosdk.Manager
	srv     *mcp.Server
	httpSrv *http.Server
	client  *mcpClient
	sseCh   chan sseEvent
	cancel  context.CancelFunc
	baseURL string
	cfg     *config.Config
	envID   string
}

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
	if reason := ensureSharedE2E(t); reason != "" {
		t.Skip(reason)
	}
	sharedE2E.mu.Lock()
	defer sharedE2E.mu.Unlock()

	return &e2eFixture{
		t:          t,
		mgr:        sharedE2E.mgr,
		srv:        sharedE2E.srv,
		httpSrv:    sharedE2E.httpSrv,
		client:     sharedE2E.client,
		sseCh:      sharedE2E.sseCh,
		cancel:     func() {},
		baseURL:    sharedE2E.baseURL,
		cfg:        sharedE2E.cfg,
		firstEnvID: sharedE2E.envID,
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	sharedE2E.mu.Lock()
	if sharedE2E.cancel != nil {
		sharedE2E.cancel()
	}
	if sharedE2E.httpSrv != nil {
		sharedE2E.httpSrv.Close()
	}
	if sharedE2E.mgr != nil && sharedE2E.mgr.Loaded() {
		sharedE2E.mgr.CloseAllBrowsers()
		_ = sharedE2E.mgr.Shutdown()
	}
	sharedE2E.mu.Unlock()
	os.Exit(code)
}

func ensureSharedE2E(t *testing.T) string {
	t.Helper()
	sharedE2E.mu.Lock()
	defer sharedE2E.mu.Unlock()
	if sharedE2E.ready || sharedE2E.skipped != "" {
		return sharedE2E.skipped
	}

	// ── Resolve working directory ──────────────────────────────────────
	wd, _ := os.Getwd()
	t.Logf("working directory: %s", wd)

	// ── Load config ───────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg == nil {
		sharedE2E.skipped = "no config.local.json or config.json found — skipping e2e test"
		return sharedE2E.skipped
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

	sharedE2E.ready = true
	sharedE2E.mgr = mgr
	sharedE2E.srv = srv
	sharedE2E.httpSrv = httpSrv
	sharedE2E.client = client
	sharedE2E.sseCh = sseCh
	sharedE2E.cancel = cancel
	sharedE2E.baseURL = baseURL
	sharedE2E.cfg = cfg
	return ""
}

func (f *e2eFixture) cleanup() {
	// SDK is a process singleton; final cleanup happens in TestMain.
	if f.mgr != nil {
		f.mgr.CloseAllBrowsers()
	}
	f.drainSSE()
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
	sharedE2E.mu.Lock()
	if sharedE2E.envID == "" {
		sharedE2E.envID = id
	}
	sharedE2E.mu.Unlock()
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

// browserOpenNavigate is a shared helper for all browser action E2E tests.
// It opens a browser for envID, navigates to url, and returns sessionID.
func browserOpenNavigate(t *testing.T, f *e2eFixture, envID, url string) string {
	t.Helper()

	f.drainSSE()
	_, err := f.client.callTool("browser_open", map[string]any{"envId": envID})
	if err != nil {
		t.Fatalf("browser_open: %v", err)
	}
	evt, ok := f.waitSSEEventLog("browser-open-success", 60*time.Second, true)
	if !ok {
		t.Fatal("timeout waiting for browser-open-success")
	}
	t.Logf("browser open ok, code=%d", evt.Code)

	time.Sleep(2 * time.Second)

	navResp, err := f.client.callTool("browser_navigate", map[string]any{
		"envId": envID,
		"url":   url,
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
	t.Logf("  sessionId=%s", sessionID)

	time.Sleep(500 * time.Millisecond)
	return sessionID
}

// browserCloseHelper is a shared helper for cleaning up the browser.
func browserCloseHelper(t *testing.T, f *e2eFixture, envID string) {
	t.Helper()
	f.drainSSE()
	_, err := f.client.callTool("browser_close", map[string]any{"envId": envID})
	if err != nil {
		t.Errorf("browser_close: %v", err)
		return
	}
	_, ok := f.waitSSEEvent("browser-close-success", 30*time.Second)
	if !ok {
		t.Error("timeout waiting for browser-close-success")
	}
}

// evalText evaluates a JS expression and returns the result as a string.
func evalText(t *testing.T, f *e2eFixture, envID string, expr string) string {
	t.Helper()
	resp, err := f.client.callTool("browser_evaluate", map[string]any{
		"envId":      envID,
		"expression": expr,
	})
	if err != nil {
		t.Fatalf("browser_evaluate(%q): %v", expr, err)
	}
	var result string
	if err := parseToolText(resp.Result, &result); err != nil {
		// Try to parse as number
		var n float64
		if err2 := parseToolText(resp.Result, &n); err2 != nil {
			t.Fatalf("parse evaluate(%q): %v / %v (raw=%s)", expr, err, err2, resp.Result)
		}
		return fmt.Sprintf("%v", n)
	}
	return result
}

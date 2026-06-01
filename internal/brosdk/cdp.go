//go:build windows || darwin

package brosdk

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ---------- Browser debug URL resolution ----------

// findDebugURL returns the WebSocket debug URL for an environment.
//
// Primary path: stored remoteDebuggingPort from browser-open-success event
// (obtained via Manager.emit() parsing). This is reliable because the SDK
// pushes the port in every browser-open-success callback.
//
// Fallback: browser_info → extract webSocketDebuggerUrl / debugPort.
func (m *Manager) findDebugURL(envID string) (string, error) {
	// 1) Try the stored debug port from the last browser-open-success event.
	m.mu.RLock()
	port, hasPort := m.debugPorts[envID]
	m.mu.RUnlock()
	if hasPort && port > 0 {
		return fetchBrowserWSEndpoint("127.0.0.1", port)
	}

	// 2) Fallback: query browser_info (may return empty depending on SDK).
	info, err := m.BrowserInfo()
	if err != nil {
		return "", fmt.Errorf("browser_info failed: %w", err)
	}

	var browsers []map[string]any
	if err := json.Unmarshal([]byte(info.Response), &browsers); err != nil {
		return "", fmt.Errorf("parse browser_info: %w", err)
	}

	for _, b := range browsers {
		eid, _ := b["envId"].(string)
		if eid != envID {
			continue
		}
		if ws, ok := b["webSocketDebuggerUrl"].(string); ok && ws != "" {
			return ws, nil
		}
		if dt, ok := b["devtoolsFrontendUrl"].(string); ok && dt != "" {
			return devtoolsToWS(dt)
		}
		host, _ := b["host"].(string)
		if host == "" {
			host = "127.0.0.1"
		}
		if p, ok := b["debugPort"].(float64); ok && p > 0 {
			return fetchBrowserWSEndpoint(host, int(p))
		}
	}
	return "", fmt.Errorf("envId %q not found — no stored debug port and browser_info returned no match", envID)
}

// devtoolsToWS converts a devtools://devtools/bundled/...?ws=host:port/path URL
// to ws://host:port/path.
func devtoolsToWS(dt string) (string, error) {
	u, err := url.Parse(dt)
	if err != nil {
		return "", fmt.Errorf("parse devtools URL %q: %w", dt, err)
	}
	ws := u.Query().Get("ws")
	if ws == "" {
		return "", fmt.Errorf("no ws query param in devtools URL %q", dt)
	}
	return "ws://" + ws, nil
}

// fetchBrowserWSEndpoint hits http://host:port/json/version to get the
// browser-level WebSocket URL, then returns it.
func fetchBrowserWSEndpoint(host string, port int) (string, error) {
	u := fmt.Sprintf("http://%s:%d/json/version", host, port)
	body, err := httpGetBytes(u)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", u, err)
	}
	var v struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return "", fmt.Errorf("parse /json/version: %w", err)
	}
	if v.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("no webSocketDebuggerUrl in /json/version response")
	}
	return v.WebSocketDebuggerURL, nil
}

// ---------- CDP WebSocket client ----------

var (
	cdpMu       sync.Mutex
	cdpNextID   int
	cdpConns    = map[string]*cdpConn{} // envID → pooled connection
)

type cdpConn struct {
	ws   *websocket.Conn
	mu   sync.Mutex
	resp map[int]chan CDPResponse // id → response channel
	done chan struct{}
}

// BrowserCommand sends a CDP command to the running browser identified by envID.
// This is implemented MCP-side: it connects to the browser's DevTools
// WebSocket endpoint and proxies the CDP request/response.
func (m *Manager) BrowserCommand(envID, method string, params map[string]any, sessionID string) (*CDPResponse, error) {
	wsURL, err := m.findDebugURL(envID)
	if err != nil {
		return nil, err
	}

	conn, err := m.getOrDial(envID, wsURL)
	if err != nil {
		return nil, err
	}

	cdpMu.Lock()
	cdpNextID++
	id := cdpNextID
	cdpMu.Unlock()

	req := CDPRequest{
		ID:        id,
		Method:    method,
		Params:    params,
		SessionID: sessionID,
	}
	body, _ := json.Marshal(req)

	ch := make(chan CDPResponse, 1)
	conn.mu.Lock()
	conn.resp[id] = ch
	conn.mu.Unlock()

	defer func() {
		conn.mu.Lock()
		delete(conn.resp, id)
		conn.mu.Unlock()
	}()

	if err := conn.ws.WriteMessage(websocket.TextMessage, body); err != nil {
		// Connection likely broken; close and retry once
		conn.ws.Close()
		cdpMu.Lock()
		delete(cdpConns, envID)
		cdpMu.Unlock()

		conn2, err := m.getOrDial(envID, wsURL)
		if err != nil {
			return nil, fmt.Errorf("cdp write (reconnect): %w", err)
		}
		conn2.mu.Lock()
		conn2.resp[id] = ch
		conn2.mu.Unlock()
		defer func() {
			conn2.mu.Lock()
			delete(conn2.resp, id)
			conn2.mu.Unlock()
		}()
		if err := conn2.ws.WriteMessage(websocket.TextMessage, body); err != nil {
			return nil, fmt.Errorf("cdp write: %w", err)
		}
	}

	select {
	case resp := <-ch:
		return &resp, nil
	case <-time.After(30 * time.Second):
		return nil, sdkError("cdp command timed out")
	}
}

// getOrDial returns a pooled WebSocket connection, creating one if needed.
func (m *Manager) getOrDial(envID, wsURL string) (*cdpConn, error) {
	cdpMu.Lock()
	conn, ok := cdpConns[envID]
	cdpMu.Unlock()
	if ok {
		return conn, nil
	}

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("cdp dial %s: %w", wsURL, err)
	}

	conn = &cdpConn{
		ws:   ws,
		resp: make(map[int]chan CDPResponse),
		done: make(chan struct{}),
	}

	cdpMu.Lock()
	cdpConns[envID] = conn
	cdpMu.Unlock()

	go conn.readLoop()
	return conn, nil
}

// readLoop continuously reads CDP messages from the WebSocket and
// dispatches them to waiting response channels.
func (c *cdpConn) readLoop() {
	defer func() {
		c.ws.Close()
		close(c.done)
	}()

	for {
		_, msg, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		var resp CDPResponse
		if err := json.Unmarshal(msg, &resp); err != nil {
			continue
		}

		// Route to waiting caller by id
		if resp.ID > 0 {
			c.mu.Lock()
			ch, ok := c.resp[resp.ID]
			c.mu.Unlock()
			if ok {
				select {
				case ch <- resp:
				default:
				}
			}
		}
	}
}

// CloseCDP closes all pooled CDP connections.
func CloseCDP() {
	cdpMu.Lock()
	defer cdpMu.Unlock()
	for _, conn := range cdpConns {
		conn.ws.Close()
	}
	cdpConns = nil
}

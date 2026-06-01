// Package mcp implements a minimal MCP (Model Context Protocol) SSE server.
//
// Endpoints:
//
//	GET  /inspector – embedded MCP Inspector web UI
//	GET  /sse       – opens an SSE stream; server sends "endpoint" event first
//	POST /message   – client sends JSON-RPC 2.0 requests here
package mcp

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// ---------- JSON-RPC 2.0 types ----------

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ---------- MCP capability types ----------

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ToolResult struct {
	Content []TextContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ---------- SSE client ----------

type sseClient struct {
	id  uint64
	ch  chan string
	req *http.Request
}

// ---------- Server ----------

// Handler is the function signature for a single MCP tool invocation.
// name is the tool name; params is the raw JSON params object.
// Returns (result text, isError).
type Handler func(name string, params json.RawMessage) (string, bool)

// Server is a minimal MCP SSE server.
type Server struct {
	info    ServerInfo
	tools   []ToolDef
	handler Handler

	mu      sync.RWMutex
	clients map[uint64]*sseClient

	nextID atomic.Uint64
}

// NewServer creates a new Server.
func NewServer(info ServerInfo, tools []ToolDef, handler Handler) *Server {
	return &Server{
		info:    info,
		tools:   tools,
		handler: handler,
		clients: make(map[uint64]*sseClient),
	}
}

// Register registers the SSE, message, inspector, and health handlers on mux.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("/sse", s.handleSSE)
	mux.HandleFunc("/message", s.handleMessage)
	mux.HandleFunc("/inspector", s.handleInspector)
	mux.HandleFunc("/health", s.handleHealth)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(w, `{"status":"ok","version":"`+s.info.Version+`"}`)
}

// Broadcast pushes a custom SSE event to all connected clients.
func (s *Server) Broadcast(eventType, data string) {
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, data)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.clients {
		select {
		case c.ch <- msg:
		default:
		}
	}
}

// ---------- SSE handler ----------

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	id := s.nextID.Add(1)
	client := &sseClient{
		id:  id,
		ch:  make(chan string, 64),
		req: r,
	}

	s.mu.Lock()
	s.clients[id] = client
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, id)
		s.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Send the endpoint event as required by MCP SSE spec.
	endpointEvent := fmt.Sprintf("event: endpoint\ndata: /message\n\n")
	fmt.Fprint(w, endpointEvent)
	flusher.Flush()

	// Heartbeat ticker
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg := <-client.ch:
			fmt.Fprint(w, msg)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// ---------- Message handler ----------

func (s *Server) handleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, nil, -32700, "parse error")
		return
	}

	resp := s.dispatch(req)

	// Always push the response back over SSE as well (for streaming clients).
	if data, err := json.Marshal(resp); err == nil {
		s.Broadcast("message", string(data))
	}

	// Also respond to the HTTP POST synchronously.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(resp)
}

// ---------- JSON-RPC dispatch ----------

func (s *Server) dispatch(req Request) Response {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "notifications/initialized":
		return Response{JSONRPC: "2.0"} // no-op
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	case "ping":
		return Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}
	default:
		log.Printf("mcp: unknown method %q", req.Method)
		return errorResp(req.ID, -32601, "method not found: "+req.Method)
	}
}

func (s *Server) handleInitialize(req Request) Response {
	result := map[string]any{
		"protocolVersion": "2024-11-05",
		"serverInfo":      s.info,
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
	}
	return Response{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func (s *Server) handleToolsList(req Request) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]any{"tools": s.tools},
	}
}

func (s *Server) handleToolsCall(req Request) Response {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResp(req.ID, -32602, "invalid params: "+err.Error())
	}

	text, isErr := s.handler(p.Name, p.Arguments)
	result := ToolResult{
		Content: []TextContent{{Type: "text", Text: text}},
		IsError: isErr,
	}
	return Response{JSONRPC: "2.0", ID: req.ID, Result: result}
}

// ---------- helpers ----------

func errorResp(id any, code int, msg string) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &Error{Code: code, Message: msg},
	}
}

func writeError(w http.ResponseWriter, id any, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(errorResp(id, code, msg))
}

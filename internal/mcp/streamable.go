// Streamable HTTP transport for MCP 2025-03-26.
//
// The /mcp endpoint supports:
//   - POST: client sends JSON-RPC requests/notifications
//   - GET:  client opens an SSE stream for server-initiated events
//   - DELETE: client terminates a session
//
// Backward compatible: the legacy /sse + /message endpoints remain active.

package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ---------- Session ----------

type session struct {
	id string
	ch chan string
}

// ---------- Streamable HTTP handler ----------

// handleStreamable is the unified MCP endpoint for Streamable HTTP transport.
func (s *Server) handleStreamable(w http.ResponseWriter, r *http.Request) {
	// CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Mcp-Session-Id, Accept")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch r.Method {
	case http.MethodPost:
		s.handleStreamablePOST(w, r)
	case http.MethodGet:
		s.handleStreamableGET(w, r)
	case http.MethodDelete:
		s.handleStreamableDELETE(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleStreamablePOST processes JSON-RPC requests sent via POST.
// Response format is chosen per-request based on the Accept header:
//   - text/event-stream: SSE stream (for long-running operations)
//   - application/json:  single JSON response (default)
//
// Notifications (no id) receive 202 Accepted with no body.
func (s *Server) handleStreamablePOST(w http.ResponseWriter, r *http.Request) {
	// Validate Accept header.
	accept := r.Header.Get("Accept")
	if accept != "" &&
		!strings.Contains(accept, "application/json") &&
		!strings.Contains(accept, "text/event-stream") &&
		accept != "*/*" {
		http.Error(w, "not acceptable: must accept application/json or text/event-stream", http.StatusNotAcceptable)
		return
	}

	// Parse request body.
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResp(nil, -32700, "parse error"))
		return
	}
	if req.JSONRPC != "2.0" {
		writeJSON(w, http.StatusBadRequest, errorResp(req.ID, -32600, "invalid request: jsonrpc must be \"2.0\""))
		return
	}

	// Dispatch the JSON-RPC request.
	resp := s.dispatch(req)

	// Notifications (no id) get 202 Accepted with no body.
	isNotification := req.ID == nil
	if isNotification {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// Create session on successful initialize and return session ID in header.
	if req.Method == "initialize" && resp.Error == nil {
		sess := s.createSession()
		w.Header().Set("Mcp-Session-Id", sess.id)
	}

	// If client accepts SSE and explicitly requests streaming, use SSE.
	if strings.Contains(accept, "text/event-stream") && !strings.Contains(accept, "application/json") {
		s.streamPOSTResponse(w, r, resp)
		return
	}

	// Default: single JSON response.
	writeJSON(w, http.StatusOK, resp)
}

// streamPOSTResponse sends the response as an SSE stream.
// This is used when the client Accept header prefers text/event-stream.
func (s *Server) streamPOSTResponse(w http.ResponseWriter, r *http.Request, resp Response) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, errorResp(nil, -32000, "streaming not supported"))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	data, _ := json.Marshal(resp)
	fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
	flusher.Flush()
}

// handleStreamableGET opens an SSE stream for server-initiated events
// (SDK events, broadcasts). Requires a valid Mcp-Session-Id header.
func (s *Server) handleStreamableGET(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		sessionID = r.URL.Query().Get("sessionId")
	}
	if sessionID == "" {
		http.Error(w, "Mcp-Session-Id header or sessionId query parameter required", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	sess, ok := s.sessions[sessionID]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "invalid or expired session", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)

	// Send initial endpoint event per spec.
	fmt.Fprintf(w, "event: endpoint\ndata: /mcp\n\n")
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg := <-sess.ch:
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

// handleStreamableDELETE terminates a session.
func (s *Server) handleStreamableDELETE(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID != "" {
		s.mu.Lock()
		if sess, ok := s.sessions[sessionID]; ok {
			close(sess.ch)
			delete(s.sessions, sessionID)
		}
		s.mu.Unlock()
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- Session management ----------

func (s *Server) createSession() *session {
	sess := &session{
		id: generateSessionID(),
		ch: make(chan string, 64),
	}
	s.mu.Lock()
	s.sessions[sess.id] = sess
	s.mu.Unlock()
	return sess
}

func generateSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

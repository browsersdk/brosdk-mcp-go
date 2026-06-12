// Package mcp — embedded MCP Inspector web UI.
//
// GET /inspector returns a self-contained HTML page that can:
//   - list all registered MCP tools with search/filter
//   - call any tool with a JSON parameter editor
//   - display real-time SSE events (sdk-event + message)
//   - keep a scrollable call history
//
// Zero external dependencies — all CSS/JS embedded in the page.
package mcp

import (
	_ "embed"
	"net/http"
)

// inspectorHTML is the self-contained single-page application,
// embedded from inspector.html at compile time.
//
//go:embed inspector.html
var inspectorHTML string

// handleInspector serves the embedded MCP Inspector HTML page.
func (s *Server) handleInspector(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(inspectorHTML))
}

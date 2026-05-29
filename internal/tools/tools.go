// Package tools registers all BroSDK MCP tools and their handlers.
package tools

import (
	"encoding/json"
	"fmt"

	"github.com/browsersdk/brosdk-mcp-go/internal/brosdk"
	"github.com/browsersdk/brosdk-mcp-go/internal/mcp"
)

// All returns the full list of tool definitions.
func All() []mcp.ToolDef {
	return []mcp.ToolDef{
		// ── SDK info ──────────────────────────────────────────────────────────
		{
			Name:        "sdk_info",
			Description: "Return current SDK runtime information (version, status, config).",
			InputSchema: schema(`{"type":"object","properties":{}}`),
		},
		{
			Name:        "sdk_token_update",
			Description: "Refresh the userSig asynchronously. Returns a reqId; result arrives via SSE event.",
			InputSchema: schema(`{
				"type":"object",
				"required":["userSig"],
				"properties":{
					"userSig":{"type":"string","description":"New user signature"}
				}
			}`),
		},
		{
			Name:        "sdk_get_user_sig",
			Description: "Obtain a userSig synchronously using an apiKey. Can be called before sdk_init to bootstrap authentication.",
			InputSchema: schema(`{
				"type":"object",
				"required":["apiKey"],
				"properties":{
					"apiKey":{"type":"string","description":"API key for the BroSDK account"},
					"duration":{"type":"integer","description":"Validity duration in seconds (optional)"}
				}
			}`),
		},
		// ── Browser control ───────────────────────────────────────────────────
		{
			Name:        "browser_install",
			Description: "Install or update browser core resources asynchronously. Returns a reqId; progress events arrive via SSE.",
			InputSchema: schema(`{
				"type":"object",
				"properties":{
					"channel":{"type":"string","description":"Browser channel to install (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_info",
			Description: "Return the list of currently running browser environments.",
			InputSchema: schema(`{"type":"object","properties":{}}`),
		},
		{
			Name:        "browser_open",
			Description: "Open a browser environment asynchronously. Returns a reqId; the 'browser-open-success' SSE event signals CDP readiness.",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId"],
				"properties":{
					"envId":{"type":"string","description":"Environment ID to open"},
					"urls":{"type":"array","items":{"type":"string"},"description":"Initial URLs (optional)"},
					"args":{"type":"array","items":{"type":"string"},"description":"Extra browser launch arguments (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_close",
			Description: "Close a running browser environment asynchronously. Returns a reqId; 'browser-close-success' SSE event signals completion.",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId"],
				"properties":{
					"envId":{"type":"string","description":"Environment ID to close"}
				}
			}`),
		},
		{
			Name:        "browser_command",
			Description: "[Advanced] Send a raw CDP command to a running browser. Prefer the high-level browser_* tools (browser_navigate, browser_click, etc.) for common operations.",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","method"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"method":{"type":"string","description":"CDP method name, e.g. 'Runtime.evaluate'"},
					"params":{"type":"object","description":"CDP command params (optional)"},
					"sessionId":{"type":"string","description":"CDP session ID for session-scoped commands (optional)"},
					"body":{"type":"string","description":"Raw JSON body; overrides other fields if provided"}
				}
			}`),
		},
		// ── Browser Actions (high-level) ──────────────────────────────────────
		{
			Name:        "browser_navigate",
			Description: "Open a URL in a new page tab. Creates a new target, attaches to it, and stores the session for subsequent actions. Returns {targetId, sessionId}.",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","url"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"url":{"type":"string","description":"URL to navigate to"}
				}
			}`),
		},
		{
			Name:        "browser_snapshot",
			Description: "Capture the accessibility tree of the current page. Returns the full AX tree with backendNodeId refs usable by browser_click_ref / browser_type_ref.",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_click",
			Description: "Click an element matching a CSS selector. Scrolls the element into view, focuses it, then clicks.",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","selector"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector for the element"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_click_ref",
			Description: "Click an element by its accessibility ref (backendNodeId from browser_snapshot).",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","ref"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"ref":{"type":"string","description":"Accessibility backendNodeId from snapshot"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_dblclick",
			Description: "Double-click an element matching a CSS selector.",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","selector"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector for the element"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_focus",
			Description: "Focus an element matching a CSS selector.",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","selector"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector for the element"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_type",
			Description: "Type text into an element matching a CSS selector (appends to existing value, fires input/change events).",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","selector","text"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector for the input element"},
					"text":{"type":"string","description":"Text to type"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_type_ref",
			Description: "Type text into an element identified by its accessibility ref (backendNodeId from browser_snapshot).",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","ref","text"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"ref":{"type":"string","description":"Accessibility backendNodeId from snapshot"},
					"text":{"type":"string","description":"Text to type"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_fill",
			Description: "Clear an input field and type new text (fires input + change events).",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","selector","text"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector for the input element"},
					"text":{"type":"string","description":"Text to fill"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_fill_ref",
			Description: "Clear an input field (identified by ref) and type new text. Ref is the backendNodeId from browser_snapshot.",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","ref","text"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"ref":{"type":"string","description":"Accessibility backendNodeId from snapshot"},
					"text":{"type":"string","description":"Text to fill"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_find_click_text",
			Description: "Find visible text on the page (XPath contains) and click the containing element.",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","text"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"text":{"type":"string","description":"Text to search for on the page"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_press_key",
			Description: "Press a key (keyDown + keyUp). Use this for special keys like 'Enter', 'Escape', 'Tab', 'ArrowDown', etc.",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","key"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"key":{"type":"string","description":"Key name, e.g. 'Enter', 'Escape', 'Tab', 'ArrowDown'"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_keyboard_type",
			Description: "Dispatch char-level key events for each character (use when a focused element needs character-by-character input).",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","text"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"text":{"type":"string","description":"Text to type character by character"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_keyboard_insert_text",
			Description: "Insert text via Input.insertText (preferred for text input into a focused field).",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","text"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"text":{"type":"string","description":"Text to insert"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_key_down",
			Description: "Send a keyDown event for a specific key. Pair with browser_key_up for modifiers.",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","key"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"key":{"type":"string","description":"Key name, e.g. 'Shift', 'Control', 'Alt', 'a'"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_key_up",
			Description: "Send a keyUp event for a specific key.",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","key"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"key":{"type":"string","description":"Key name"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_evaluate",
			Description: "Execute a JavaScript expression in the current page and return the result as JSON. Useful for reading page state after interactions.",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","expression"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"expression":{"type":"string","description":"JavaScript expression, e.g. 'document.title' or 'document.querySelector(\"#status\").textContent'"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_hover",
			Description: "Hover over an element matching a CSS selector (dispatches mouseover + mouseenter).",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","selector"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector for the element"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_select_option",
			Description: "Set the value of a <select> element and fire change event.",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","selector","value"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector for the <select> element"},
					"value":{"type":"string","description":"Option value to select"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_check",
			Description: "Check a checkbox or radio input (sets checked=true and fires change event).",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","selector"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector for the checkbox/radio"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_uncheck",
			Description: "Uncheck a checkbox (sets checked=false and fires change event).",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","selector"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector for the checkbox"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_scroll",
			Description: "Scroll the page or a specific element. direction: 'up'|'down'|'left'|'right'. px defaults to 300 if omitted.",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","direction"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"direction":{"type":"string","description":"Scroll direction: up, down, left, right"},
					"px":{"type":"integer","description":"Pixels to scroll (default 300)"},
					"selector":{"type":"string","description":"CSS selector of scrollable container (optional; scrolls window if omitted)"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_scroll_into_view",
			Description: "Scroll an element into view (aligns to center).",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","selector"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector for the element"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_drag",
			Description: "Drag a source element onto a target element (simulates dragstart → dragover → drop → dragend).",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","sourceSelector","targetSelector"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"sourceSelector":{"type":"string","description":"CSS selector for the draggable source element"},
					"targetSelector":{"type":"string","description":"CSS selector for the drop target element"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_upload_file",
			Description: "Set files on a file input element (DOM.setFileInputFiles). Paths are resolved to absolute paths.",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","selector","files"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector for the file input element"},
					"files":{"type":"array","items":{"type":"string"},"description":"Array of file paths to upload"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_screenshot",
			Description: "Take a screenshot of the current page. Supports PNG/JPEG, full-page capture, element clipping, and annotation. Saves to disk and returns the file path.",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"path":{"type":"string","description":"Output file path (auto-generated if omitted)"},
					"screenshotDir":{"type":"string","description":"Directory for auto-named screenshots (default: '.')"},
					"format":{"type":"string","description":"Image format: 'png' (default) or 'jpeg'"},
					"quality":{"type":"integer","description":"JPEG quality 0-100 (jpeg only)"},
					"fullPage":{"type":"boolean","description":"Capture the full scrollable page"},
					"annotate":{"type":"boolean","description":"Annotate interactive elements (not yet implemented)"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		{
			Name:        "browser_pdf",
			Description: "Generate a PDF of the current page. Saves to disk and returns the file path.",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId","path"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"path":{"type":"string","description":"Output PDF file path"},
					"sessionId":{"type":"string","description":"CDP session ID (optional)"}
				}
			}`),
		},
		// ── Environment management ─────────────────────────────────────────────
		{
			Name:        "env_create",
			Description: "Create a new fingerprint browser environment. Returns the environment JSON.",
			InputSchema: schema(`{
				"type":"object",
				"properties":{
					"name":{"type":"string","description":"Human-readable environment name"},
					"os":{"type":"string","description":"Simulated OS platform (e.g. 'Windows')"},
					"body":{"type":"string","description":"Raw JSON body forwarded to the backend env/create API (overrides other fields if provided)"}
				}
			}`),
		},
		{
			Name:        "env_page",
			Description: "Query/list environments with optional filters and pagination.",
			InputSchema: schema(`{
				"type":"object",
				"properties":{
					"page":{"type":"integer","description":"Page number (1-based, default 1)"},
					"pageSize":{"type":"integer","description":"Page size (default 20)"},
					"body":{"type":"string","description":"Raw JSON body forwarded to the backend env/page API"}
				}
			}`),
		},
		{
			Name:        "env_update",
			Description: "Update the configuration of an existing environment.",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId"],
				"properties":{
					"envId":{"type":"string","description":"ID of the environment to update"},
					"body":{"type":"string","description":"Raw JSON body forwarded to the backend env/update API"}
				}
			}`),
		},
		{
			Name:        "env_destroy",
			Description: "Permanently delete an environment and all its data.",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId"],
				"properties":{
					"envId":{"type":"string","description":"ID of the environment to destroy"}
				}
			}`),
		},
		{
			Name:        "env_getinfo",
			Description: "Get detailed info for a single environment (shorthand for env_page with an envId filter).",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId"],
				"properties":{
					"envId":{"type":"string","description":"ID of the environment to query"}
				}
			}`),
		},
	}
}

// schema parses a JSON string into json.RawMessage. Panics on invalid JSON.
func schema(s string) json.RawMessage {
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		panic("brosdk-mcp-go: invalid tool schema: " + err.Error())
	}
	return raw
}

// ---------- Handler ----------

// Handler returns a mcp.Handler that dispatches tool calls to the Manager.
func Handler(mgr *brosdk.Manager) mcp.Handler {
	return func(name string, params json.RawMessage) (string, bool) {
		result, err := dispatch(mgr, name, params)
		if err != nil {
			return err.Error(), true
		}
		return result, false
	}
}

func dispatch(mgr *brosdk.Manager, name string, params json.RawMessage) (string, error) {
	var p map[string]any
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return "", fmt.Errorf("invalid params: %w", err)
		}
	}
	if p == nil {
		p = map[string]any{}
	}

	switch name {
	// ── SDK ──
	case "sdk_info":
		resp, err := mgr.SDKInfo()
		if err != nil {
			return "", err
		}
		return resp.Response, nil

	case "sdk_token_update":
		body := rawOrJSON(p, "userSig", "userSig")
		reqID, err := mgr.TokenUpdate(body)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"reqId":%d}`, reqID), nil

	case "sdk_get_user_sig":
		apiKey := str(p, "apiKey")
		if apiKey == "" {
			return "", fmt.Errorf("apiKey is required")
		}
		userSig, err := mgr.GetUserSig(apiKey)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"userSig":%q}`, userSig), nil

	// ── Browser ──
	case "browser_install":
		body := rawBodyOr(p, `{}`)
		reqID, err := mgr.BrowserInstall(body)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"reqId":%d}`, reqID), nil

	case "browser_info":
		resp, err := mgr.BrowserInfo()
		if err != nil {
			return "", err
		}
		return resp.Response, nil

	case "browser_open":
		envID := str(p, "envId")
		if envID == "" {
			return "", fmt.Errorf("envId is required")
		}
		urls := strSlice(p, "urls")
		args := strSlice(p, "args")
		reqID, err := mgr.BrowserOpen(envID, urls, args)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"reqId":%d,"envId":%q}`, reqID, envID), nil

	case "browser_close":
		envID := str(p, "envId")
		if envID == "" {
			return "", fmt.Errorf("envId is required")
		}
		reqID, err := mgr.BrowserClose(envID)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"reqId":%d,"envId":%q}`, reqID, envID), nil

	case "browser_command":
		envID := str(p, "envId")
		method := str(p, "method")
		if envID == "" || method == "" {
			return "", fmt.Errorf("envId and method are required")
		}
		var cdpParams map[string]any
		if raw, ok := p["params"]; ok {
			if m, ok := raw.(map[string]any); ok {
				cdpParams = m
			}
		}
		sessionID := str(p, "sessionId")
		resp, err := mgr.BrowserCommand(envID, method, cdpParams, sessionID)
		if err != nil {
			return "", err
		}
		if resp.Error != nil {
			return "", fmt.Errorf("cdp error [%d]: %s", resp.Error.Code, resp.Error.Message)
		}
		out := resp.Result
		if len(out) == 0 {
			out = json.RawMessage(`{}`)
		}
		return string(out), nil

	// ── Browser Actions ──
	case "browser_navigate":
		envID := str(p, "envId")
		url := str(p, "url")
		if envID == "" || url == "" {
			return "", fmt.Errorf("envId and url are required")
		}
		result, err := mgr.Navigate(envID, url)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(result)
		return string(b), nil

	case "browser_snapshot":
		envID := str(p, "envId")
		if envID == "" {
			return "", fmt.Errorf("envId is required")
		}
		sid := str(p, "sessionId")
		raw, err := mgr.Snapshot(envID, sid)
		if err != nil {
			return "", err
		}
		return string(raw), nil

	case "browser_click":
		envID, sel := str(p, "envId"), str(p, "selector")
		if envID == "" || sel == "" {
			return "", fmt.Errorf("envId and selector are required")
		}
		if err := mgr.Click(envID, str(p, "sessionId"), sel); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_click_ref":
		envID, ref := str(p, "envId"), str(p, "ref")
		if envID == "" || ref == "" {
			return "", fmt.Errorf("envId and ref are required")
		}
		if err := mgr.ClickRef(envID, str(p, "sessionId"), ref); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_dblclick":
		envID, sel := str(p, "envId"), str(p, "selector")
		if envID == "" || sel == "" {
			return "", fmt.Errorf("envId and selector are required")
		}
		if err := mgr.DblClick(envID, str(p, "sessionId"), sel); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_focus":
		envID, sel := str(p, "envId"), str(p, "selector")
		if envID == "" || sel == "" {
			return "", fmt.Errorf("envId and selector are required")
		}
		if err := mgr.Focus(envID, str(p, "sessionId"), sel); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_type":
		envID, sel, text := str(p, "envId"), str(p, "selector"), str(p, "text")
		if envID == "" || sel == "" || text == "" {
			return "", fmt.Errorf("envId, selector and text are required")
		}
		if err := mgr.Type(envID, str(p, "sessionId"), sel, text); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_type_ref":
		envID, ref, text := str(p, "envId"), str(p, "ref"), str(p, "text")
		if envID == "" || ref == "" || text == "" {
			return "", fmt.Errorf("envId, ref and text are required")
		}
		if err := mgr.TypeRef(envID, str(p, "sessionId"), ref, text); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_fill":
		envID, sel, text := str(p, "envId"), str(p, "selector"), str(p, "text")
		if envID == "" || sel == "" || text == "" {
			return "", fmt.Errorf("envId, selector and text are required")
		}
		if err := mgr.Fill(envID, str(p, "sessionId"), sel, text); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_fill_ref":
		envID, ref, text := str(p, "envId"), str(p, "ref"), str(p, "text")
		if envID == "" || ref == "" || text == "" {
			return "", fmt.Errorf("envId, ref and text are required")
		}
		if err := mgr.FillRef(envID, str(p, "sessionId"), ref, text); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_find_click_text":
		envID, text := str(p, "envId"), str(p, "text")
		if envID == "" || text == "" {
			return "", fmt.Errorf("envId and text are required")
		}
		if err := mgr.FindClickText(envID, str(p, "sessionId"), text); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_press_key":
		envID, key := str(p, "envId"), str(p, "key")
		if envID == "" || key == "" {
			return "", fmt.Errorf("envId and key are required")
		}
		if err := mgr.PressKey(envID, str(p, "sessionId"), key); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_keyboard_type":
		envID, text := str(p, "envId"), str(p, "text")
		if envID == "" || text == "" {
			return "", fmt.Errorf("envId and text are required")
		}
		if err := mgr.KeyboardType(envID, str(p, "sessionId"), text); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_keyboard_insert_text":
		envID, text := str(p, "envId"), str(p, "text")
		if envID == "" || text == "" {
			return "", fmt.Errorf("envId and text are required")
		}
		if err := mgr.KeyboardInsertText(envID, str(p, "sessionId"), text); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_key_down":
		envID, key := str(p, "envId"), str(p, "key")
		if envID == "" || key == "" {
			return "", fmt.Errorf("envId and key are required")
		}
		if err := mgr.KeyDown(envID, str(p, "sessionId"), key); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_key_up":
		envID, key := str(p, "envId"), str(p, "key")
		if envID == "" || key == "" {
			return "", fmt.Errorf("envId and key are required")
		}
		if err := mgr.KeyUp(envID, str(p, "sessionId"), key); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_hover":
		envID, sel := str(p, "envId"), str(p, "selector")
		if envID == "" || sel == "" {
			return "", fmt.Errorf("envId and selector are required")
		}
		if err := mgr.Hover(envID, str(p, "sessionId"), sel); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_evaluate":
		envID, expr := str(p, "envId"), str(p, "expression")
		if envID == "" || expr == "" {
			return "", fmt.Errorf("envId and expression are required")
		}
		var result any
		if err := mgr.Evaluate(envID, str(p, "sessionId"), expr, &result); err != nil {
			return "", err
		}
		b, _ := json.Marshal(result)
		return string(b), nil

	case "browser_select_option":
		envID, sel, val := str(p, "envId"), str(p, "selector"), str(p, "value")
		if envID == "" || sel == "" || val == "" {
			return "", fmt.Errorf("envId, selector and value are required")
		}
		if err := mgr.SelectOption(envID, str(p, "sessionId"), sel, val); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_check":
		envID, sel := str(p, "envId"), str(p, "selector")
		if envID == "" || sel == "" {
			return "", fmt.Errorf("envId and selector are required")
		}
		if err := mgr.Check(envID, str(p, "sessionId"), sel); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_uncheck":
		envID, sel := str(p, "envId"), str(p, "selector")
		if envID == "" || sel == "" {
			return "", fmt.Errorf("envId and selector are required")
		}
		if err := mgr.Uncheck(envID, str(p, "sessionId"), sel); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_scroll":
		envID, dir := str(p, "envId"), str(p, "direction")
		if envID == "" || dir == "" {
			return "", fmt.Errorf("envId and direction are required")
		}
		px := 0
		if v, ok := p["px"].(float64); ok {
			px = int(v)
		}
		sel := str(p, "selector")
		if err := mgr.Scroll(envID, str(p, "sessionId"), dir, px, sel); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_scroll_into_view":
		envID, sel := str(p, "envId"), str(p, "selector")
		if envID == "" || sel == "" {
			return "", fmt.Errorf("envId and selector are required")
		}
		if err := mgr.ScrollIntoView(envID, str(p, "sessionId"), sel); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_drag":
		envID, src, tgt := str(p, "envId"), str(p, "sourceSelector"), str(p, "targetSelector")
		if envID == "" || src == "" || tgt == "" {
			return "", fmt.Errorf("envId, sourceSelector and targetSelector are required")
		}
		if err := mgr.Drag(envID, str(p, "sessionId"), src, tgt); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_upload_file":
		envID, sel, files := str(p, "envId"), str(p, "selector"), strSlice(p, "files")
		if envID == "" || sel == "" || len(files) == 0 {
			return "", fmt.Errorf("envId, selector and files are required")
		}
		if err := mgr.UploadFile(envID, str(p, "sessionId"), sel, files); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_screenshot":
		envID := str(p, "envId")
		if envID == "" {
			return "", fmt.Errorf("envId is required")
		}
		opts := brosdk.ScreenshotOptions{
			Path:     str(p, "path"),
			Dir:      str(p, "screenshotDir"),
			Format:   str(p, "format"),
			Annotate: boolVal(p, "annotate"),
			FullPage: boolVal(p, "fullPage"),
		}
		if v, ok := p["quality"].(float64); ok {
			opts.Quality = int(v)
		}
		outPath, err := mgr.Screenshot(envID, str(p, "sessionId"), opts)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"path":%q}`, outPath), nil

	case "browser_pdf":
		envID, path := str(p, "envId"), str(p, "path")
		if envID == "" || path == "" {
			return "", fmt.Errorf("envId and path are required")
		}
		outPath, err := mgr.PDF(envID, str(p, "sessionId"), path)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"path":%q}`, outPath), nil

	// ── Environment CRUD ──
	case "env_create":
		body := str(p, "body")
		if body == "" {
			body = buildEnvCreateBody(p)
		}
		resp, err := mgr.EnvCreate(body)
		if err != nil {
			return "", err
		}
		return resp.Response, nil

	case "env_page":
		body := str(p, "body")
		if body == "" {
			body = buildEnvPageBody(p)
		}
		resp, err := mgr.EnvPage(body)
		if err != nil {
			return "", err
		}
		return resp.Response, nil

	case "env_update":
		body := str(p, "body")
		if body == "" {
			envID := str(p, "envId")
			if envID == "" {
				return "", fmt.Errorf("envId is required")
			}
			body = `{"envId":"` + escJ(envID) + `"}`
		}
		resp, err := mgr.EnvUpdate(body)
		if err != nil {
			return "", err
		}
		return resp.Response, nil

	case "env_destroy":
		envID := str(p, "envId")
		if envID == "" {
			return "", fmt.Errorf("envId is required")
		}
		body := `{"envId":"` + escJ(envID) + `"}`
		resp, err := mgr.EnvDestroy(body)
		if err != nil {
			return "", err
		}
		return resp.Response, nil

	case "env_getinfo":
		envID := str(p, "envId")
		if envID == "" {
			return "", fmt.Errorf("envId is required")
		}
		body := `{"envId":"` + escJ(envID) + `","pageSize":1}`
		resp, err := mgr.EnvPage(body)
		if err != nil {
			return "", err
		}
		return resp.Response, nil

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

// ---------- param helpers ----------

func str(p map[string]any, key string) string {
	if v, ok := p[key].(string); ok {
		return v
	}
	return ""
}

func boolVal(p map[string]any, key string) bool {
	if v, ok := p[key].(bool); ok {
		return v
	}
	return false
}

func strSlice(p map[string]any, key string) []string {
	raw, ok := p[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func rawOrJSON(p map[string]any, field, jsonKey string) string {
	v := str(p, field)
	return `{"` + jsonKey + `":"` + escJ(v) + `"}`
}

func rawBodyOr(p map[string]any, def string) string {
	if b := str(p, "body"); b != "" {
		return b
	}
	return def
}

func buildEnvCreateBody(p map[string]any) string {
	body := `{`
	sep := ""
	if name := str(p, "name"); name != "" {
		body += sep + `"name":"` + escJ(name) + `"`
		sep = ","
	}
	if os := str(p, "os"); os != "" {
		body += sep + `"os":"` + escJ(os) + `"`
	}
	return body + `}`
}

func buildEnvPageBody(p map[string]any) string {
	page := 1
	pageSize := 20
	if v, ok := p["page"].(float64); ok && v >= 1 {
		page = int(v)
	}
	if v, ok := p["pageSize"].(float64); ok && v >= 1 {
		pageSize = int(v)
	}
	return fmt.Sprintf(`{"page":%d,"pageSize":%d}`, page, pageSize)
}

func escJ(s string) string {
	out := ""
	for _, r := range s {
		switch r {
		case '\\':
			out += `\\`
		case '"':
			out += `\"`
		case '\n':
			out += `\n`
		case '\r':
			out += `\r`
		case '\t':
			out += `\t`
		default:
			out += string(r)
		}
	}
	return out
}

// Package tools registers all BroSDK MCP tools and their handlers.
package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/browsersdk/brosdk-mcp-go/internal/brosdk"
	"github.com/browsersdk/brosdk-mcp-go/internal/mcp"
	"github.com/browsersdk/brosdk-mcp-go/internal/recorder"
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
				"required":["url"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"url":{"type":"string","description":"URL to navigate to"}
				}
			}`),
		},
		{
			Name:        "browser_snapshot",
			Description: "Capture the accessibility tree of the current page. Set interactiveOnly=true to return only interactive elements (buttons, inputs, links, etc.) — much smaller response for LLM agents.",
			InputSchema: schema(`{
				"type":"object",
				"required":[],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"},
					"interactiveOnly":{"type":"boolean","description":"Only return interactive elements (button/input/link/select etc). Reduces output 10-50x."}
				}
			}`),
		},
		{
			Name:        "browser_click",
			Description: "Click an element matching a CSS selector. Scrolls the element into view, focuses it, then clicks.",
			InputSchema: schema(`{
				"type":"object",
				"required":["selector"],
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
				"required":["ref"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"ref":{"type":"string","description":"Accessibility backendNodeId from snapshot"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_dblclick",
			Description: "Double-click an element matching a CSS selector.",
			InputSchema: schema(`{
				"type":"object",
				"required":["selector"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector for the element"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_focus",
			Description: "Focus an element matching a CSS selector.",
			InputSchema: schema(`{
				"type":"object",
				"required":["selector"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector for the element"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_focus_ref",
			Description: "Focus an element by its accessibility ref (backendNodeId from browser_snapshot).",
			InputSchema: schema(`{
				"type":"object",
				"required":["ref"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"ref":{"type":"string","description":"Accessibility backendNodeId from snapshot"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_type",
			Description: "Type text into an element matching a CSS selector (appends to existing value, fires input/change events).",
			InputSchema: schema(`{
				"type":"object",
				"required":["selector","text"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector for the input element"},
					"text":{"type":"string","description":"Text to type"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_type_ref",
			Description: "Type text into an element identified by its accessibility ref (backendNodeId from browser_snapshot).",
			InputSchema: schema(`{
				"type":"object",
				"required":["ref","text"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"ref":{"type":"string","description":"Accessibility backendNodeId from snapshot"},
					"text":{"type":"string","description":"Text to type"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_fill",
			Description: "Clear an input field and type new text (fires input + change events).",
			InputSchema: schema(`{
				"type":"object",
				"required":["selector","text"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector for the input element"},
					"text":{"type":"string","description":"Text to fill"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_fill_ref",
			Description: "Clear an input field (identified by ref) and type new text. Ref is the backendNodeId from browser_snapshot.",
			InputSchema: schema(`{
				"type":"object",
				"required":["ref","text"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"ref":{"type":"string","description":"Accessibility backendNodeId from snapshot"},
					"text":{"type":"string","description":"Text to fill"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_find_click_text",
			Description: "Find visible text on the page (XPath contains) and click the containing element.",
			InputSchema: schema(`{
				"type":"object",
				"required":["text"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"text":{"type":"string","description":"Text to search for on the page"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_press_key",
			Description: "Press a key (keyDown + keyUp). Use this for special keys like 'Enter', 'Escape', 'Tab', 'ArrowDown', etc.",
			InputSchema: schema(`{
				"type":"object",
				"required":["key"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"key":{"type":"string","description":"Key name, e.g. 'Enter', 'Escape', 'Tab', 'ArrowDown'"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_keyboard_type",
			Description: "Dispatch char-level key events for each character (use when a focused element needs character-by-character input).",
			InputSchema: schema(`{
				"type":"object",
				"required":["text"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"text":{"type":"string","description":"Text to type character by character"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_insert_text",
			Description: "Insert text via Input.insertText (preferred for text input into a focused field).",
			InputSchema: schema(`{
				"type":"object",
				"required":["text"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"text":{"type":"string","description":"Text to insert"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_key_down",
			Description: "Send a keyDown event for a specific key. Pair with browser_key_up for modifiers.",
			InputSchema: schema(`{
				"type":"object",
				"required":["key"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"key":{"type":"string","description":"Key name, e.g. 'Shift', 'Control', 'Alt', 'a'"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_key_up",
			Description: "Send a keyUp event for a specific key.",
			InputSchema: schema(`{
				"type":"object",
				"required":["key"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"key":{"type":"string","description":"Key name"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_evaluate",
			Description: "Execute a JavaScript expression in the current page and return the result as JSON. Useful for reading page state after interactions.",
			InputSchema: schema(`{
				"type":"object",
				"required":["expression"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"expression":{"type":"string","description":"JavaScript expression, e.g. 'document.title' or 'document.querySelector(\"#status\").textContent'"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_hover",
			Description: "Hover over an element matching a CSS selector (dispatches mouseover + mouseenter).",
			InputSchema: schema(`{
				"type":"object",
				"required":["selector"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector for the element"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_hover_ref",
			Description: "Hover over an element by its accessibility ref (backendNodeId from browser_snapshot).",
			InputSchema: schema(`{
				"type":"object",
				"required":["ref"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"ref":{"type":"string","description":"Accessibility backendNodeId from snapshot"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_select_option",
			Description: "Set the value of a <select> element and fire change event.",
			InputSchema: schema(`{
				"type":"object",
				"required":["selector","value"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector for the <select> element"},
					"value":{"type":"string","description":"Option value to select"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_select_option_ref",
			Description: "Set the value of a <select> element by its accessibility ref (backendNodeId from browser_snapshot).",
			InputSchema: schema(`{
				"type":"object",
				"required":["ref","value"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"ref":{"type":"string","description":"Accessibility backendNodeId from snapshot"},
					"value":{"type":"string","description":"Option value to select"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_check",
			Description: "Check a checkbox or radio input (sets checked=true and fires change event).",
			InputSchema: schema(`{
				"type":"object",
				"required":["selector"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector for the checkbox/radio"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_check_ref",
			Description: "Check a checkbox/radio by its accessibility ref (backendNodeId from browser_snapshot). Reads .checked status first and clicks only if unchecked.",
			InputSchema: schema(`{
				"type":"object",
				"required":["ref"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"ref":{"type":"string","description":"Accessibility backendNodeId from snapshot"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_uncheck",
			Description: "Uncheck a checkbox (sets checked=false and fires change event).",
			InputSchema: schema(`{
				"type":"object",
				"required":["selector"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector for the checkbox"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_scroll",
			Description: "Scroll the page or a specific element. direction: 'up'|'down'|'left'|'right'. px defaults to 300 if omitted.",
			InputSchema: schema(`{
				"type":"object",
				"required":["direction"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"direction":{"type":"string","description":"Scroll direction: up, down, left, right"},
					"px":{"type":"integer","description":"Pixels to scroll (default 300)"},
					"selector":{"type":"string","description":"CSS selector of scrollable container (optional; scrolls window if omitted)"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_scroll_into_view",
			Description: "Scroll an element into view (aligns to center).",
			InputSchema: schema(`{
				"type":"object",
				"required":["selector"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector for the element"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_drag",
			Description: "Drag a source element onto a target element (simulates dragstart → dragover → drop → dragend).",
			InputSchema: schema(`{
				"type":"object",
				"required":["sourceSelector","targetSelector"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"sourceSelector":{"type":"string","description":"CSS selector for the draggable source element"},
					"targetSelector":{"type":"string","description":"CSS selector for the drop target element"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_upload_file",
			Description: "Set files on a file input element (DOM.setFileInputFiles). Paths are resolved to absolute paths.",
			InputSchema: schema(`{
				"type":"object",
				"required":["selector","files"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector for the file input element"},
					"files":{"type":"array","items":{"type":"string"},"description":"Array of file paths to upload"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_screenshot",
			Description: "Take a screenshot of the current page. Saves to workDir/screenshots/ (auto-created). Returns absolute path for agent reading.",
			InputSchema: schema(`{
				"type":"object",
				"required":[],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"path":{"type":"string","description":"Output file path (auto-generated under workDir/screenshots/ if omitted)"},
					"dir":{"type":"string","description":"Directory for auto-named screenshots (default: workDir/screenshots/)"},
					"format":{"type":"string","description":"Image format: 'png' (default) or 'jpeg'"},
					"quality":{"type":"integer","description":"JPEG quality 0-100 (jpeg only)"},
					"fullPage":{"type":"boolean","description":"Capture the full scrollable page"},
					"annotate":{"type":"boolean","description":"Annotate interactive elements (not yet implemented)"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_pdf",
			Description: "Generate a PDF of the current page. Saves to workDir/pdfs/ (auto-created). Returns absolute path for agent reading.",
			InputSchema: schema(`{
				"type":"object",
				"required":[],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"path":{"type":"string","description":"Output PDF file path (default: workDir/pdfs/output.pdf)"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_reload",
			Description: "Reload the current page.",
			InputSchema: schema(`{
				"type":"object",
				"required":[],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_back",
			Description: "Navigate back in browser history.",
			InputSchema: schema(`{
				"type":"object",
				"required":[],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_forward",
			Description: "Navigate forward in browser history.",
			InputSchema: schema(`{
				"type":"object",
				"required":[],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_get_text",
			Description: "Return the visible text content of an element matched by a CSS selector.",
			InputSchema: schema(`{
				"type":"object",
				"required":["selector"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector to locate the element"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_get_value",
			Description: "Return the value attribute of an input element matched by a CSS selector.",
			InputSchema: schema(`{
				"type":"object",
				"required":["selector"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"selector":{"type":"string","description":"CSS selector to locate the input element"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_uncheck_ref",
			Description: "Uncheck a checkbox or radio button identified by a snapshot backendDOMNodeId reference (see browser_snapshot).",
			InputSchema: schema(`{
				"type":"object",
				"required":["ref"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"ref":{"type":"string","description":"BackendDOMNodeId from browser_snapshot response"},
					"sessionId":{"type":"string","description":"CDP session ID (optional; uses active session if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_get_html",
			Description: "Return the full HTML source of the current page. Use this when browser_snapshot doesn't capture content you need (e.g. raw data attributes, comments, inline scripts). The output can be very large — prefer browser_snapshot or browser_get_text for element-level queries.",
			InputSchema: schema(`{
				"type":"object",
				"required":[],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"}
				}
			}`),
		},
		{
			Name:        "browser_new_tab",
			Description: "Create a new blank tab in the active browser environment. The new tab becomes the active tab. The previous active tab is kept open in the background. Returns {tabId} for later reference.",
			InputSchema: schema(`{
				"type":"object",
				"required":[],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"}
				}
			}`),
		},
		{
			Name:        "browser_close_tab",
			Description: "Close a specific tab by its tabId. If you close the active tab, the most recent remaining tab becomes active. Use browser_list_tabs to find tab IDs.",
			InputSchema: schema(`{
				"type":"object",
				"required":["tabId"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"tabId":{"type":"string","description":"Tab ID from browser_list_tabs or browser_new_tab (use \"__active__\" for the currently active tab)"}
				}
			}`),
		},
		{
			Name:        "browser_list_tabs",
			Description: "List all open tabs in the active browser environment. Returns [{tabId, title, url, isActive}]. The active tab has tabId=\"__active__\".",
			InputSchema: schema(`{
				"type":"object",
				"required":[],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"}
				}
			}`),
		},
		{
			Name:        "browser_get_cookies",
			Description: "Get cookies from the current page. If urls are provided, returns cookies for those URLs only. Otherwise returns all cookies in the browser. Returns an array of cookie objects.",
			InputSchema: schema(`{
				"type":"object",
				"required":[],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"urls":{"type":"array","items":{"type":"string"},"description":"URLs to filter cookies by (optional; returns all cookies if omitted)"}
				}
			}`),
		},
		{
			Name:        "browser_set_cookies",
			Description: "Set one or more cookies in the browser. Each cookie object requires 'name' and 'value', and optionally 'url', 'domain', 'path', 'secure', 'httpOnly'.",
			InputSchema: schema(`{
				"type":"object",
				"required":["cookies"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"cookies":{"type":"array","items":{"type":"object"},"description":"Array of cookie objects e.g. [{\"name\":\"session\",\"value\":\"abc123\",\"url\":\"https://example.com\"}]"}
				}
			}`),
		},

		// ── Agent-Friendly Tools (Tier 1 & 2) ─────────────────────────────────
		{
			Name:        "browser_find_ref",
			Description: "Search the accessibility tree for elements matching role/name/value and return their refs. Much cheaper than parsing the full browser_snapshot output. All filter fields are optional — omit to match all, provide multiple to narrow results.",
			InputSchema: schema(`{
				"type":"object",
				"required":[],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"role":{"type":"string","description":"ARIA role to match, e.g. 'button', 'textbox', 'link' (optional)"},
					"name":{"type":"string","description":"Accessible name to match (optional)"},
					"value":{"type":"string","description":"Current value to match, e.g. input field content (optional)"},
					"limit":{"type":"integer","description":"Max results to return (default 10)"}
				}
			}`),
		},
		{
			Name:        "browser_wait",
			Description: "Wait for a condition on the page. Supports multiple wait modes via the 'waitFor' parameter: 'navigation' (page load complete), 'selector' (CSS selector appears), 'text' (text substring appears in accessibility tree), 'time' (fixed delay). For backward compatibility, if waitFor is omitted, it auto-detects from which params you provide (text/role+name → 'text' mode; selector → 'selector' mode).",
			InputSchema: schema(`{
				"type":"object",
				"required":[],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"waitFor":{"type":"string","description":"Wait mode: 'navigation', 'selector', 'text', or 'time'. Auto-detected if omitted."},
					"text":{"type":"string","description":"Text substring to wait for (used with waitFor=text or auto-detect)"},
					"role":{"type":"string","description":"ARIA role (paired with name, used with waitFor=text)"},
					"name":{"type":"string","description":"Accessible name (paired with role, used with waitFor=text)"},
					"selector":{"type":"string","description":"CSS selector to wait for (used with waitFor=selector or auto-detect)"},
					"timeout":{"type":"integer","description":"Timeout in ms (default 5000 for text/selector, 30000 for navigation)"},
					"delay":{"type":"integer","description":"Fixed delay in ms (used with waitFor=time)"}
				}
			}`),
		},
		{
			Name:        "browser_page_state",
			Description: "Quick page check: returns {title, url, readyState}. Use instead of browser_evaluate for common page metadata queries.",
			InputSchema: schema(`{
				"type":"object",
				"required":[],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"}
				}
			}`),
		},
		{
			Name:        "browser_exists",
			Description: "Quick boolean check whether an element (by role and name) exists on the current page. Returns {exists: true/false}. Faster and cheaper than parsing the full snapshot.",
			InputSchema: schema(`{
				"type":"object",
				"required":["role","name"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"role":{"type":"string","description":"ARIA role, e.g. 'button', 'dialog'"},
					"name":{"type":"string","description":"Accessible name, e.g. 'Submit', 'Close'"},
					"value":{"type":"string","description":"Optional additional value to match"}
				}
			}`),
		},
		{
			Name:        "browser_dialog",
			Description: "Handle a JavaScript dialog (alert/confirm/prompt). Use 'accept' to dismiss with OK, 'dismiss' to cancel.",
			InputSchema: schema(`{
				"type":"object",
				"required":["action"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"action":{"type":"string","description":"'accept' (click OK) or 'dismiss' (click Cancel)"}
				}
			}`),
		},
		{
			Name:        "browser_fill_form",
			Description: "Fill multiple form fields at once. Accepts a map of CSS selectors to values. If submitSelector is provided, clicks it after filling all fields. Much more efficient than calling browser_fill/browser_type multiple times.",
			InputSchema: schema(`{
				"type":"object",
				"required":["fields"],
				"properties":{
					"envId":{"type":"string","description":"Target environment ID"},
					"fields":{"type":"object","description":"Map of CSS selectors to values, e.g. {\"#name\": \"John\", \"#email\": \"john@test.com\"}"},
					"submitSelector":{"type":"string","description":"CSS selector for submit button to click after filling (optional)"}
				}
			}`),
		},

		{
			Name:        "browser_select",
			Description: "Set the active browser environment. All subsequent Browser Action tools will use this envId by default when their own envId parameter is omitted.",
			InputSchema: schema(`{
				"type":"object",
				"required":["envId"],
				"properties":{
					"envId":{"type":"string","description":"Environment ID to activate"}
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

		// ── Record & Replay ─────────────────────────────────────────────────
		{
			Name:        "record_start",
			Description: "Start recording browser actions. All subsequent tool calls (except recorder tools) will be captured as steps. Caller is responsible for staying on one browser environment.",
			InputSchema: schema(`{"type":"object","properties":{}}`),
		},
		{
			Name:        "record_stop",
			Description: "Stop recording and auto-save the captured scene to scenes/{name}.json. No need to call scene_save separately.",
			InputSchema: schema(`{
				"type":"object",
				"required":["name"],
				"properties":{
					"name":{"type":"string","description":"Scene file name (without extension, e.g. 'login_flow')"},
					"description":{"type":"string","description":"Optional human-readable description for the scene"}
				}
			}`),
		},
		{
			Name:        "record_status",
			Description: "Check whether a recording is currently active and how many steps have been captured.",
			InputSchema: schema(`{"type":"object","properties":{}}`),
		},
		{
			Name:        "scene_save",
			Description: "Save a recorded scene to disk. Rejects if a scene with the same name already exists.",
			InputSchema: schema(`{
				"type":"object",
				"required":["name","scene"],
				"properties":{
					"name":{"type":"string","description":"Scene file name (without extension, e.g. 'login_flow')"},
					"scene":{"type":"object","description":"The scene object (from record_stop) with optional description field"}
				}
			}`),
		},
		{
			Name:        "scene_list",
			Description: "List all saved scenes with summary info (name, step count, description, createdAt).",
			InputSchema: schema(`{"type":"object","properties":{}}`),
		},
		{
			Name:        "scene_get",
			Description: "Get a single scene by name, including all steps and params.",
			InputSchema: schema(`{
				"type":"object",
				"required":["name"],
				"properties":{
					"name":{"type":"string","description":"Scene file name (without extension)"}
				}
			}`),
		},
		{
			Name:        "scene_update",
			Description: "Overwrite an existing scene with a new scene object. Useful for editing steps or variables.",
			InputSchema: schema(`{
				"type":"object",
				"required":["name","scene"],
				"properties":{
					"name":{"type":"string","description":"Scene file name (without extension)"},
					"scene":{"type":"object","description":"The full scene object to save"}
				}
			}`),
		},
		{
			Name:        "scene_delete",
			Description: "Delete a saved scene by name.",
			InputSchema: schema(`{
				"type":"object",
				"required":["name"],
				"properties":{
					"name":{"type":"string","description":"Scene file name (without extension)"}
				}
			}`),
		},
		{
			Name:        "scene_replay",
			Description: "Replay a saved scene against a browser environment. Uses WaitFor guards (readyState/exists) to ensure each step completes before the next begins. Supports {{variable}} substitution in step params. Returns per-step results.",
			InputSchema: schema(`{
				"type":"object",
			"required":["name"],
			"properties":{
				"name":{"type":"string","description":"Scene file name (without extension)"},
				"envId":{"type":"string","description":"Target browser environment ID. If omitted, uses the active environment set by browser_select."},
					"variables":{"type":"object","description":"Key-value pairs for {{variable}} substitution in step params"},
					"stopOnError":{"type":"boolean","description":"Stop replay on first error (default false)"},
					"stepDelay":{"type":"number","description":"Delay between steps in milliseconds (default 500)"},
					"applyHumanDelay":{"type":"boolean","description":"Insert recorded human pauses between steps (default true). Disable for fast headless replay."}
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
// The dispatch function is exported as Dispatch for reuse by the recorder.Player.
func Handler(mgr *brosdk.Manager) mcp.Handler {
	return func(name string, params json.RawMessage) (string, bool) {
		// 1. unmarshal params once — shared by recorder hook and dispatch
		var p map[string]any
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return err.Error(), true
			}
		}
		if p == nil {
			p = map[string]any{}
		}

		// 2. Recorder hook — capture every tool call except recorder tools themselves
		if rec := recorder.Get(); rec.IsRecording() && !isRecorderTool(name) {
			rec.Capture(name, recorder.SanitizeParams(p))
		}

		// 3. dispatch
		result, err := Dispatch(mgr, name, p)

		// 4. Cache browser_snapshot result for _ref fingerprint extraction.
		if name == "browser_snapshot" && err == nil {
			if rec := recorder.Get(); rec.IsRecording() {
				rec.SetLastSnapshot([]byte(result))
			}
		}

		if err != nil {
			return err.Error(), true
		}
		return result, false
	}
}

// recorderToolNames lists tools managed by the recorder itself.
// These are never captured during recording.
var recorderToolNames = map[string]bool{
	"record_start":  true,
	"record_stop":   true,
	"record_status": true,
	"scene_save":    true,
	"scene_list":    true,
	"scene_get":     true,
	"scene_update":  true,
	"scene_delete":  true,
	"scene_replay":  true,
}

func isRecorderTool(name string) bool { return recorderToolNames[name] }

// Dispatch routes a tool call to the appropriate Manager method.
// Exported so recorder.Player can reuse the same dispatch logic.
func Dispatch(mgr *brosdk.Manager, name string, p map[string]any) (string, error) {
	switch name {
	// ── SDK ──
	case "sdk_info":
		resp, err := mgr.SDKInfo()
		if err != nil {
			return "", err
		}
		return resp.Response, nil

	case "sdk_token_update":
		reqID, err := mgr.TokenUpdate(jsonFromParams(p))
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
		reqID, err := mgr.BrowserInstall(jsonFromParams(p))
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
		// Tear down chromedp + CDP resources immediately.
		// The SDK close is async; stale allocators would otherwise
		// cause panics if browser_open + scene_replay follow.
		mgr.CloseBrowser(envID)
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

	// ── Active env selection ──
	case "browser_select":
		envID := str(p, "envId")
		if envID == "" {
			return "", fmt.Errorf("envId is required")
		}
		mgr.SetActiveEnv(envID)
		return fmt.Sprintf(`{"ok":true,"activeEnvId":%q}`, envID), nil

	// ── Browser Actions ──
	case "browser_navigate":
		envID := resolveEnvID(mgr, p)
		url := str(p, "url")
		if envID == "" || url == "" {
			return "", fmt.Errorf("envId (or browser_select) and url are required")
		}
		result, err := mgr.Navigate(envID, url)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(result)
		return string(b), nil

	case "browser_snapshot":
		envID := resolveEnvID(mgr, p)
		if envID == "" {
			return "", fmt.Errorf("envId (or browser_select) is required")
		}
		sid := str(p, "sessionId")
		interactiveOnly := boolVal(p, "interactiveOnly")
		if interactiveOnly {
			raw, err := mgr.SnapshotInteractive(envID)
			if err != nil {
				return "", err
			}
			return string(raw), nil
		}
		raw, err := mgr.Snapshot(envID, sid)
		if err != nil {
			return "", err
		}
		return string(raw), nil

	case "browser_click":
		envID, sel := resolveEnvID(mgr, p), str(p, "selector")
		if envID == "" || sel == "" {
			return "", fmt.Errorf("envId (or browser_select) and selector are required")
		}
		if err := mgr.Click(envID, str(p, "sessionId"), sel); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_click_ref":
		envID, ref := resolveEnvID(mgr, p), str(p, "ref")
		if envID == "" || ref == "" {
			return "", fmt.Errorf("envId (or browser_select) and ref are required")
		}
		if err := mgr.ClickRef(envID, str(p, "sessionId"), ref); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_dblclick":
		envID, sel := resolveEnvID(mgr, p), str(p, "selector")
		if envID == "" || sel == "" {
			return "", fmt.Errorf("envId (or browser_select) and selector are required")
		}
		if err := mgr.DblClick(envID, str(p, "sessionId"), sel); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_focus":
		envID, sel := resolveEnvID(mgr, p), str(p, "selector")
		if envID == "" || sel == "" {
			return "", fmt.Errorf("envId (or browser_select) and selector are required")
		}
		if err := mgr.Focus(envID, str(p, "sessionId"), sel); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_focus_ref":
		envID, ref := resolveEnvID(mgr, p), str(p, "ref")
		if envID == "" || ref == "" {
			return "", fmt.Errorf("envId (or browser_select) and ref are required")
		}
		if err := mgr.FocusRef(envID, str(p, "sessionId"), ref); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_type":
		envID, sel, text := resolveEnvID(mgr, p), str(p, "selector"), str(p, "text")
		if envID == "" || sel == "" || text == "" {
			return "", fmt.Errorf("envId (or browser_select), selector and text are required")
		}
		if err := mgr.Type(envID, str(p, "sessionId"), sel, text); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_type_ref":
		envID, ref, text := resolveEnvID(mgr, p), str(p, "ref"), str(p, "text")
		if envID == "" || ref == "" || text == "" {
			return "", fmt.Errorf("envId (or browser_select), ref and text are required")
		}
		if err := mgr.TypeRef(envID, str(p, "sessionId"), ref, text); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_fill":
		envID, sel, text := resolveEnvID(mgr, p), str(p, "selector"), str(p, "text")
		if envID == "" || sel == "" || text == "" {
			return "", fmt.Errorf("envId (or browser_select), selector and text are required")
		}
		if err := mgr.Fill(envID, str(p, "sessionId"), sel, text); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_fill_ref":
		envID, ref, text := resolveEnvID(mgr, p), str(p, "ref"), str(p, "text")
		if envID == "" || ref == "" || text == "" {
			return "", fmt.Errorf("envId (or browser_select), ref and text are required")
		}
		if err := mgr.FillRef(envID, str(p, "sessionId"), ref, text); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_find_click_text":
		envID, text := resolveEnvID(mgr, p), str(p, "text")
		if envID == "" || text == "" {
			return "", fmt.Errorf("envId (or browser_select) and text are required")
		}
		if err := mgr.FindClickText(envID, str(p, "sessionId"), text); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_press_key":
		envID, key := resolveEnvID(mgr, p), str(p, "key")
		if envID == "" || key == "" {
			return "", fmt.Errorf("envId (or browser_select) and key are required")
		}
		if err := mgr.PressKey(envID, str(p, "sessionId"), key); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_keyboard_type":
		envID, text := resolveEnvID(mgr, p), str(p, "text")
		if envID == "" || text == "" {
			return "", fmt.Errorf("envId (or browser_select) and text are required")
		}
		if err := mgr.KeyboardType(envID, str(p, "sessionId"), text); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_insert_text":
		envID, text := resolveEnvID(mgr, p), str(p, "text")
		if envID == "" || text == "" {
			return "", fmt.Errorf("envId (or browser_select) and text are required")
		}
		if err := mgr.KeyboardInsertText(envID, str(p, "sessionId"), text); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_key_down":
		envID, key := resolveEnvID(mgr, p), str(p, "key")
		if envID == "" || key == "" {
			return "", fmt.Errorf("envId (or browser_select) and key are required")
		}
		if err := mgr.KeyDown(envID, str(p, "sessionId"), key); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_key_up":
		envID, key := resolveEnvID(mgr, p), str(p, "key")
		if envID == "" || key == "" {
			return "", fmt.Errorf("envId (or browser_select) and key are required")
		}
		if err := mgr.KeyUp(envID, str(p, "sessionId"), key); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_hover":
		envID, sel := resolveEnvID(mgr, p), str(p, "selector")
		if envID == "" || sel == "" {
			return "", fmt.Errorf("envId (or browser_select) and selector are required")
		}
		if err := mgr.Hover(envID, str(p, "sessionId"), sel); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_hover_ref":
		envID, ref := resolveEnvID(mgr, p), str(p, "ref")
		if envID == "" || ref == "" {
			return "", fmt.Errorf("envId (or browser_select) and ref are required")
		}
		if err := mgr.HoverRef(envID, str(p, "sessionId"), ref); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_evaluate":
		envID, expr := resolveEnvID(mgr, p), str(p, "expression")
		if envID == "" || expr == "" {
			return "", fmt.Errorf("envId (or browser_select) and expression are required")
		}
		var result any
		if err := mgr.Evaluate(envID, str(p, "sessionId"), expr, &result); err != nil {
			return "", err
		}
		b, _ := json.Marshal(result)
		return string(b), nil

	case "browser_select_option":
		envID, sel, val := resolveEnvID(mgr, p), str(p, "selector"), str(p, "value")
		if envID == "" || sel == "" || val == "" {
			return "", fmt.Errorf("envId (or browser_select), selector and value are required")
		}
		if err := mgr.SelectOption(envID, str(p, "sessionId"), sel, val); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_select_option_ref":
		envID, ref, val := resolveEnvID(mgr, p), str(p, "ref"), str(p, "value")
		if envID == "" || ref == "" || val == "" {
			return "", fmt.Errorf("envId (or browser_select), ref and value are required")
		}
		if err := mgr.SelectOptionRef(envID, str(p, "sessionId"), ref, val); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_check":
		envID, sel := resolveEnvID(mgr, p), str(p, "selector")
		if envID == "" || sel == "" {
			return "", fmt.Errorf("envId (or browser_select) and selector are required")
		}
		if err := mgr.Check(envID, str(p, "sessionId"), sel); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_check_ref":
		envID, ref := resolveEnvID(mgr, p), str(p, "ref")
		if envID == "" || ref == "" {
			return "", fmt.Errorf("envId (or browser_select) and ref are required")
		}
		if err := mgr.CheckRef(envID, str(p, "sessionId"), ref); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_uncheck":
		envID, sel := resolveEnvID(mgr, p), str(p, "selector")
		if envID == "" || sel == "" {
			return "", fmt.Errorf("envId (or browser_select) and selector are required")
		}
		if err := mgr.Uncheck(envID, str(p, "sessionId"), sel); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_scroll":
		envID, dir := resolveEnvID(mgr, p), str(p, "direction")
		if envID == "" || dir == "" {
			return "", fmt.Errorf("envId (or browser_select) and direction are required")
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
		envID, sel := resolveEnvID(mgr, p), str(p, "selector")
		if envID == "" || sel == "" {
			return "", fmt.Errorf("envId (or browser_select) and selector are required")
		}
		if err := mgr.ScrollIntoView(envID, str(p, "sessionId"), sel); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_drag":
		envID, src, tgt := resolveEnvID(mgr, p), str(p, "sourceSelector"), str(p, "targetSelector")
		if envID == "" || src == "" || tgt == "" {
			return "", fmt.Errorf("envId (or browser_select), sourceSelector and targetSelector are required")
		}
		if err := mgr.Drag(envID, str(p, "sessionId"), src, tgt); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_upload_file":
		envID, sel, files := resolveEnvID(mgr, p), str(p, "selector"), strSlice(p, "files")
		if envID == "" || sel == "" || len(files) == 0 {
			return "", fmt.Errorf("envId (or browser_select), selector and files are required")
		}
		if err := mgr.UploadFile(envID, str(p, "sessionId"), sel, files); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_screenshot":
		envID := resolveEnvID(mgr, p)
		if envID == "" {
			return "", fmt.Errorf("envId (or browser_select) is required")
		}
		opts := brosdk.ScreenshotOptions{
			Path:     str(p, "path"),
			Dir:      str(p, "dir"),
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
		envID := resolveEnvID(mgr, p)
		if envID == "" {
			return "", fmt.Errorf("envId (or browser_select) is required")
		}
		outPath, err := mgr.PDF(envID, str(p, "sessionId"), str(p, "path"))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"path":%q}`, outPath), nil

	case "browser_reload":
		envID := resolveEnvID(mgr, p)
		if envID == "" {
			return "", fmt.Errorf("envId (or browser_select) is required")
		}
		if err := mgr.Reload(envID, str(p, "sessionId")); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_back":
		envID := resolveEnvID(mgr, p)
		if envID == "" {
			return "", fmt.Errorf("envId (or browser_select) is required")
		}
		if err := mgr.Back(envID, str(p, "sessionId")); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_forward":
		envID := resolveEnvID(mgr, p)
		if envID == "" {
			return "", fmt.Errorf("envId (or browser_select) is required")
		}
		if err := mgr.Forward(envID, str(p, "sessionId")); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_get_text":
		envID, sel := resolveEnvID(mgr, p), str(p, "selector")
		if envID == "" || sel == "" {
			return "", fmt.Errorf("envId (or browser_select) and selector are required")
		}
		text, err := mgr.GetText(envID, str(p, "sessionId"), sel)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"text":%q}`, text), nil

	case "browser_get_value":
		envID, sel := resolveEnvID(mgr, p), str(p, "selector")
		if envID == "" || sel == "" {
			return "", fmt.Errorf("envId (or browser_select) and selector are required")
		}
		value, err := mgr.GetValue(envID, str(p, "sessionId"), sel)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"value":%q}`, value), nil

	case "browser_uncheck_ref":
		envID, ref := resolveEnvID(mgr, p), str(p, "ref")
		if envID == "" || ref == "" {
			return "", fmt.Errorf("envId (or browser_select) and ref are required")
		}
		if err := mgr.UncheckRef(envID, str(p, "sessionId"), ref); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	// ── Tab & Cookie Management ──
	case "browser_get_html":
		envID := resolveEnvID(mgr, p)
		if envID == "" {
			return "", fmt.Errorf("envId (or browser_select) is required")
		}
		html, err := mgr.GetHTML(envID, str(p, "sessionId"))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"html":%q}`, html), nil

	case "browser_new_tab":
		envID := resolveEnvID(mgr, p)
		if envID == "" {
			return "", fmt.Errorf("envId (or browser_select) is required")
		}
		tabID, err := mgr.NewTab(envID)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"tabId":%q}`, tabID), nil

	case "browser_close_tab":
		envID, tabID := resolveEnvID(mgr, p), str(p, "tabId")
		if envID == "" || tabID == "" {
			return "", fmt.Errorf("envId (or browser_select) and tabId are required")
		}
		if err := mgr.CloseTab(envID, tabID); err != nil {
			return "", err
		}
		return `{"ok":true}`, nil

	case "browser_list_tabs":
		envID := resolveEnvID(mgr, p)
		if envID == "" {
			return "", fmt.Errorf("envId (or browser_select) is required")
		}
		tabs, err := mgr.ListTabs(envID)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(map[string]any{"tabs": tabs, "count": len(tabs)})
		return string(b), nil

	case "browser_get_cookies":
		envID := resolveEnvID(mgr, p)
		if envID == "" {
			return "", fmt.Errorf("envId (or browser_select) is required")
		}
		urls := strSlice(p, "urls")
		cookies, err := mgr.GetCookies(envID, urls)
		if err != nil {
			return "", err
		}
		return string(cookies), nil

	case "browser_set_cookies":
		envID := resolveEnvID(mgr, p)
		if envID == "" {
			return "", fmt.Errorf("envId (or browser_select) is required")
		}
		if raw, ok := p["cookies"].([]interface{}); ok {
			cl := make([]map[string]any, 0, len(raw))
			for _, v := range raw {
				if m, ok := v.(map[string]any); ok {
					cl = append(cl, m)
				}
			}
			if err := mgr.SetCookies(envID, cl); err != nil {
				return "", err
			}
			return `{"ok":true}`, nil
		}
		return "", fmt.Errorf("cookies must be an array of cookie objects")

	// ── Agent-Friendly Tools (Tier 1 & 2) ──
	case "browser_find_ref":
		envID := resolveEnvID(mgr, p)
		if envID == "" {
			return "", fmt.Errorf("envId (or browser_select) is required")
		}
		role := str(p, "role")
		name := str(p, "name")
		value := str(p, "value")
		limit := 10
		if v, ok := p["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}
		refs, err := mgr.FindRefs(envID, role, name, value, limit)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(map[string]any{"refs": refs, "count": len(refs)})
		return string(b), nil

	case "browser_wait":
		envID := resolveEnvID(mgr, p)
		if envID == "" {
			return "", fmt.Errorf("envId (or browser_select) is required")
		}
		waitFor := str(p, "waitFor")
		timeout := 5000
		if v, ok := p["timeout"].(float64); ok && v > 0 {
			timeout = int(v)
		}
		// Auto-detect mode from provided params (backward compat).
		if waitFor == "" {
			if str(p, "selector") != "" {
				waitFor = "selector"
			} else if str(p, "text") != "" || str(p, "role") != "" {
				waitFor = "text"
			} else if _, ok := p["delay"]; ok {
				waitFor = "time"
			}
		}
		switch waitFor {
		case "navigation":
			if timeout < 30000 {
				timeout = 30000
			}
			if err := mgr.WaitForNavigation(envID, timeout); err != nil {
				return "", err
			}
		case "selector":
			sel := str(p, "selector")
			if sel == "" {
				return "", fmt.Errorf("selector is required for waitFor=selector")
			}
			if err := mgr.WaitForSelector(envID, sel, timeout); err != nil {
				return "", err
			}
		case "text":
			text := str(p, "text")
			role := str(p, "role")
			name := str(p, "name")
			if err := mgr.Wait(envID, text, role, name, str(p, "selector"), timeout); err != nil {
				return "", err
			}
		case "time":
			delay := 500
			if v, ok := p["delay"].(float64); ok && v > 0 {
				delay = int(v)
			}
			time.Sleep(time.Duration(delay) * time.Millisecond)
		default:
			// Fallback: use the old Wait (backward compat).
			text := str(p, "text")
			role := str(p, "role")
			name := str(p, "name")
			selector := str(p, "selector")
			if err := mgr.Wait(envID, text, role, name, selector, timeout); err != nil {
				return "", err
			}
		}
		return `{"waited":true}`, nil

	case "browser_page_state":
		envID := resolveEnvID(mgr, p)
		if envID == "" {
			return "", fmt.Errorf("envId (or browser_select) is required")
		}
		ps, err := mgr.PageState(envID)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(ps)
		return string(b), nil

	case "browser_exists":
		envID, role, name := resolveEnvID(mgr, p), str(p, "role"), str(p, "name")
		if envID == "" || role == "" || name == "" {
			return "", fmt.Errorf("envId (or browser_select), role and name are required")
		}
		value := str(p, "value")
		exists, err := mgr.Exists(envID, role, name, value)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(`{"exists":%v}`, exists), nil

	case "browser_dialog":
		envID, action := resolveEnvID(mgr, p), str(p, "action")
		if envID == "" || action == "" {
			return "", fmt.Errorf("envId (or browser_select) and action are required")
		}
		if action != "accept" && action != "dismiss" {
			return "", fmt.Errorf("action must be 'accept' or 'dismiss'")
		}
		result, err := mgr.Dialog(envID, action)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(result)
		return string(b), nil

	case "browser_fill_form":
		envID := resolveEnvID(mgr, p)
		if envID == "" {
			return "", fmt.Errorf("envId (or browser_select) is required")
		}
		fieldsRaw, ok := p["fields"].(map[string]any)
		if !ok || len(fieldsRaw) == 0 {
			return "", fmt.Errorf("fields must be a non-empty object")
		}
		fields := make(map[string]string, len(fieldsRaw))
		for k, v := range fieldsRaw {
			if s, ok := v.(string); ok {
				fields[k] = s
			}
		}
		if len(fields) == 0 {
			return "", fmt.Errorf("fields object has no string values")
		}
		submitSel := str(p, "submitSelector")
		result, err := mgr.FillForm(envID, fields, submitSel)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(result)
		return string(b), nil

	// ── Environment CRUD ──
	case "env_create":
		resp, err := mgr.EnvCreate(jsonFromParams(p))
		if err != nil {
			return "", err
		}
		return resp.Response, nil

	case "env_page":
		resp, err := mgr.EnvPage(jsonFromParams(p))
		if err != nil {
			return "", err
		}
		return resp.Response, nil

	case "env_update":
		resp, err := mgr.EnvUpdate(jsonFromParams(p))
		if err != nil {
			return "", err
		}
		return resp.Response, nil

	case "env_destroy":
		resp, err := mgr.EnvDestroy(jsonFromParams(p))
		if err != nil {
			return "", err
		}
		return resp.Response, nil

	case "env_getinfo":
		resp, err := mgr.EnvPage(jsonFromParams(p))
		if err != nil {
			return "", err
		}
		return resp.Response, nil

	// ── Record & Replay ──
	case "record_start":
		if err := recorder.Get().Start(); err != nil {
			return "", err
		}
		return `{"recording":true}`, nil

	case "record_stop":
		name := str(p, "name")
		if name == "" {
			return "", fmt.Errorf("name is required")
		}
		scene, err := recorder.Get().Stop()
		if err != nil {
			return "", err
		}
		scene.Name = name
		if desc := str(p, "description"); desc != "" {
			scene.Description = desc
		}
		if err := validateScene(scene); err != nil {
			return "", err
		}
		path := filepath.Join(ScenesDir(), name+".json")
		data, err := json.MarshalIndent(scene, "", "  ")
		if err != nil {
			return "", fmt.Errorf("marshal scene: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return "", fmt.Errorf("create scenes dir: %w", err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			return "", fmt.Errorf("write scene: %w", err)
		}
		return fmt.Sprintf(`{"saved":true,"path":%q,"steps":%d}`, path, len(scene.Steps)), nil

	case "record_status":
		b, _ := json.Marshal(recorder.Get().Status())
		return string(b), nil

	case "scene_save":
		name := str(p, "name")
		if name == "" {
			return "", fmt.Errorf("name is required")
		}
		sceneRaw, ok := p["scene"]
		if !ok {
			return "", fmt.Errorf("scene is required")
		}
		sceneJSON, err := json.Marshal(sceneRaw)
		if err != nil {
			return "", fmt.Errorf("invalid scene: %w", err)
		}
		var scene recorder.Scene
		if err := json.Unmarshal(sceneJSON, &scene); err != nil {
			return "", fmt.Errorf("invalid scene: %w", err)
		}
		if err := validateScene(&scene); err != nil {
			return "", err
		}
		path := filepath.Join(ScenesDir(), name+".json")
		if _, err := os.Stat(path); err == nil {
			return "", fmt.Errorf("scene already exists: %s", name)
		}
		// ensure description is carried over
		if desc, ok := sceneRaw.(map[string]any)["description"].(string); ok {
			scene.Description = desc
		}
		scene.Name = name
		data, err := json.MarshalIndent(scene, "", "  ")
		if err != nil {
			return "", fmt.Errorf("marshal scene: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return "", fmt.Errorf("create scenes dir: %w", err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			return "", fmt.Errorf("write scene: %w", err)
		}
		return fmt.Sprintf(`{"saved":true,"path":%q}`, path), nil

	case "scene_list":
		dir := ScenesDir()
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return `{"scenes":[]}`, nil
			}
			return "", fmt.Errorf("read scenes dir: %w", err)
		}
		type summary struct {
			Name        string `json:"name"`
			StepCount   int    `json:"stepCount"`
			Description string `json:"description,omitempty"`
			CreatedAt   string `json:"createdAt"`
		}
		var scenes []summary
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			var s struct {
				Steps       []recorder.Step `json:"steps"`
				Description string          `json:"description"`
				CreatedAt   string          `json:"createdAt"`
			}
			if err := json.Unmarshal(data, &s); err != nil {
				continue
			}
			scenes = append(scenes, summary{
				Name:        strings.TrimSuffix(e.Name(), ".json"),
				StepCount:   len(s.Steps),
				Description: s.Description,
				CreatedAt:   s.CreatedAt,
			})
		}
		b, _ := json.Marshal(map[string]any{"scenes": scenes})
		return string(b), nil

	case "scene_get":
		name := str(p, "name")
		if name == "" {
			return "", fmt.Errorf("name is required")
		}
		path := filepath.Join(ScenesDir(), name+".json")
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("scene not found: %s", name)
		}
		// Return raw JSON directly.
		return string(data), nil

	case "scene_update":
		name := str(p, "name")
		if name == "" {
			return "", fmt.Errorf("name is required")
		}
		path := filepath.Join(ScenesDir(), name+".json")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return "", fmt.Errorf("scene not found: %s", name)
		}
		sceneRaw, ok := p["scene"]
		if !ok {
			return "", fmt.Errorf("scene is required")
		}
		sceneJSON, err := json.Marshal(sceneRaw)
		if err != nil {
			return "", fmt.Errorf("invalid scene: %w", err)
		}
		var scene recorder.Scene
		if err := json.Unmarshal(sceneJSON, &scene); err != nil {
			return "", fmt.Errorf("invalid scene: %w", err)
		}
		if err := validateScene(&scene); err != nil {
			return "", err
		}
		scene.Name = name
		if desc, ok := sceneRaw.(map[string]any)["description"].(string); ok {
			scene.Description = desc
		}
		data, err := json.MarshalIndent(scene, "", "  ")
		if err != nil {
			return "", fmt.Errorf("marshal scene: %w", err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			return "", fmt.Errorf("write scene: %w", err)
		}
		return fmt.Sprintf(`{"saved":true,"path":%q}`, path), nil

	case "scene_delete":
		name := str(p, "name")
		if name == "" {
			return "", fmt.Errorf("name is required")
		}
		path := filepath.Join(ScenesDir(), name+".json")
		if err := os.Remove(path); err != nil {
			return "", fmt.Errorf("delete scene: %w", err)
		}
		return `{"deleted":true}`, nil

	case "scene_replay":
		return replayScene(mgr, p)

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

// ---------- scene helpers ----------

var scenesDir string

// SetScenesDir sets the directory for scene file storage (called from main.go).
func SetScenesDir(dir string) { scenesDir = dir }

// ScenesDir returns the scenes storage directory.
func ScenesDir() string {
	if scenesDir == "" {
		scenesDir = "scenes"
	}
	return scenesDir
}

func validateScene(s *recorder.Scene) error {
	if s.Version != 1 {
		return fmt.Errorf("unsupported scene version: %d", s.Version)
	}
	if len(s.Steps) == 0 {
		return fmt.Errorf("scene has no steps")
	}
	if len(s.Steps) > recorder.MaxSteps {
		return fmt.Errorf("scene has %d steps, max is %d", len(s.Steps), recorder.MaxSteps)
	}
	return nil
}

// replayScene is the dispatch helper for scene_replay.
func replayScene(mgr *brosdk.Manager, p map[string]any) (string, error) {
	name := str(p, "name")
	envID := resolveEnvID(mgr, p)
	if name == "" || envID == "" {
		return "", fmt.Errorf("name is required and envId must be set (via envId param or browser_select)")
	}

	path := filepath.Join(ScenesDir(), name+".json")
	scene, err := recorder.LoadScene(path)
	if err != nil {
		return "", err
	}

	opts := recorder.ReplayOptions{
		EnvID:            envID,
		StopOnError:      boolVal(p, "stopOnError"),
		ApplyHumanDelay:  true, // default on for safer replay
	}

	if vars, ok := p["variables"].(map[string]any); ok {
		opts.Variables = make(map[string]string, len(vars))
		for k, v := range vars {
			if s, ok := v.(string); ok {
				opts.Variables[k] = s
			}
		}
	}
	if delay, ok := p["stepDelay"].(float64); ok && delay > 0 {
		opts.StepDelay = time.Duration(delay) * time.Millisecond
	}
	if v, ok := p["applyHumanDelay"]; ok {
		if b, ok := v.(bool); ok {
			opts.ApplyHumanDelay = b
		}
	}

	player := recorder.NewPlayer(mgr, Dispatch)
	result := player.Replay(scene, opts)
	b, _ := json.Marshal(result)
	return string(b), nil
}

// ---------- param helpers ----------

// jsonFromParams marshals p back to JSON. Used for pass-through tools
// where the dispatch layer does NOT know SDK parameter structure.
func jsonFromParams(p map[string]any) string {
	b, _ := json.Marshal(p)
	return string(b)
}

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

// resolveEnvID returns the envId from params, falling back to the
// active envId set by browser_select. Returns "" if neither is set.
func resolveEnvID(mgr *brosdk.Manager, p map[string]any) string {
	if v, ok := p["envId"].(string); ok && v != "" {
		return v
	}
	return mgr.GetActiveEnv()
}

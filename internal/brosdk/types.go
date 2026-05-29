package brosdk

import "encoding/json"

// Response is the synchronous return value from SDK calls.
type Response struct {
	Code     int32  `json:"code"`
	Response string `json:"response,omitempty"`
}

// Event is an asynchronous notification delivered via the result callback.
type Event struct {
	Code int32  `json:"code"`
	Data string `json:"data"`
}

// CookiesEvent carries raw cookie JSON from the SDK's cookie interception callback.
type CookiesEvent struct {
	Data string `json:"data"`
}

// InitOptions maps to the SDK init JSON request body.
// When UserSig is empty and ApiKey is set, the manager will call
// POST https://api.brosdk.com/api/v2/browser/getUserSig to obtain
// a userSig automatically before init.
type InitOptions struct {
	UserSig   string `json:"userSig"`
	ApiKey    string `json:"apiKey,omitempty"`
	WorkDir   string `json:"workDir,omitempty"`
	Port      int    `json:"port,omitempty"`
	SdkApiURL string `json:"sdkApiUrl,omitempty"`
	Debug     bool   `json:"debug"`
}

// BrowserOpenEnv is one entry in a browser-open request.
type BrowserOpenEnv struct {
	EnvID string   `json:"envId"`
	URLs  []string `json:"urls,omitempty"`
	Args  []string `json:"args,omitempty"`
}

// BrowserOpenOptions is the request body for BrowserOpen.
type BrowserOpenOptions struct {
	Envs []BrowserOpenEnv `json:"envs"`
}

// BrowserCloseOptions is the request body for BrowserClose.
type BrowserCloseOptions struct {
	Envs []string `json:"envs"`
}

// ---------- JSON builders (no encoding/json dependency) ----------

func initJSON(o InitOptions) string {
	body := `{"userSig":"` + escapeJSON(o.UserSig) + `"`
	if o.WorkDir != "" {
		body += `,"workDir":"` + escapeJSON(o.WorkDir) + `"`
	}
	if o.Port > 0 {
		body += `,"port":` + intStr(o.Port)
	}
	if o.SdkApiURL != "" {
		body += `,"sdkApiUrl":"` + escapeJSON(o.SdkApiURL) + `"`
	}
	if o.Debug {
		body += `,"debug":true`
	} else {
		body += `,"debug":false`
	}
	return body + `}`
}

func browserOpenJSON(envID string, urls []string, args []string) string {
	body := `{"envs":[{"envId":"` + escapeJSON(envID) + `"`
	if len(urls) > 0 {
		body += `,"urls":[`
		for i, u := range urls {
			if i > 0 {
				body += ","
			}
			body += `"` + escapeJSON(u) + `"`
		}
		body += `]`
	}

	// Always include --remote-debugging-port=0 so the browser picks a random
	// free port for CDP connections. Without this, the browser won't start a
	// DevTools debug server and browser_command / CDP proxy won't work.
	// If the caller already specified a debug port arg, we still prepend
	// --remote-debugging-port=0 — Chromium uses the last occurrence per flag
	// style, so the caller's value wins if they intended a specific port.
	merged := make([]string, 0, len(args)+1)
	merged = append(merged, "--remote-debugging-port=0")
	merged = append(merged, args...)

	body += `,"args":[`
	for i, a := range merged {
		if i > 0 {
			body += ","
		}
		body += `"` + escapeJSON(a) + `"`
	}
	body += `]`

	return body + `}]}`
}

func browserCloseJSON(envID string) string {
	return `{"envs":["` + escapeJSON(envID) + `"]}`
}

// ---------- CDP types (shared across all build tags) ----------

// CDPRequest is a single CDP command sent over WebSocket.
type CDPRequest struct {
	ID        int            `json:"id"`
	Method    string         `json:"method"`
	Params    map[string]any `json:"params,omitempty"`
	SessionID string         `json:"sessionId,omitempty"`
}

// CDPResponse is the raw response frame from the browser DevTools.
type CDPResponse struct {
	ID     int             `json:"id,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *CDPError       `json:"error,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
}

// CDPError is a CDP protocol error.
type CDPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func escapeJSON(s string) string {
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

func intStr(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	d := ""
	for v > 0 {
		d = string(rune('0'+v%10)) + d
		v /= 10
	}
	if neg {
		return "-" + d
	}
	return d
}

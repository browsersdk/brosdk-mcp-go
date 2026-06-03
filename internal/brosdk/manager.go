package brosdk

import (
	"encoding/json"
	"fmt"
	"sync"
)

// Manager wraps the native BroSDK library and exposes a clean Go API.
// It is safe to use from multiple goroutines.
type Manager struct {
	mu            sync.RWMutex
	lib           nativeLib
	listeners     []func(Event)
	debugPorts    map[string]int         // envId → remoteDebuggingPort (from browser-open-success)
	activeEnvID   string                // currently active browser environment (set by browser_select)
	browsers      map[string]*browserTab // envId → chromedp resources
	workDir       string                // base path for screenshots/PDFs output
}

// NewManager creates an empty Manager. Call Load before any SDK operations.
func NewManager() *Manager {
	return &Manager{
		debugPorts: make(map[string]int),
	}
}

// Load loads the native library from the given file path and registers
// the result callback. Supported on Windows and macOS.
func (m *Manager) Load(path string) error {
	lib, err := loadNative(path, m.emit)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.lib = lib
	m.mu.Unlock()
	return nil
}

// Loaded reports whether the native library has been loaded.
func (m *Manager) Loaded() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lib != nil
}

// OnEvent registers a listener for asynchronous SDK events.
func (m *Manager) OnEvent(fn func(Event)) {
	m.mu.Lock()
	m.listeners = append(m.listeners, fn)
	m.mu.Unlock()
}

// ---------- Lifecycle ----------

// Init performs synchronous SDK initialization.
// When UserSig is empty and ApiKey is provided, a userSig is
// automatically fetched from the BroSDK API before init.
func (m *Manager) Init(opts InitOptions) (*Response, error) {
	m.workDir = opts.WorkDir
	if opts.UserSig == "" && opts.ApiKey != "" {
		us, err := fetchUserSig(opts.ApiKey)
		if err != nil {
			return nil, fmt.Errorf("auto-fetch userSig: %w", err)
		}
		opts.UserSig = us
	}
	if opts.UserSig == "" {
		return nil, sdkError("userSig (or apiKey) is required for sdk_init")
	}
	return m.callSync("sdk_init", func(l nativeLib) (int32, string, error) {
		return l.init(initJSON(opts))
	})
}

// InitAsync starts async SDK initialization; result arrives via OnEvent.
func (m *Manager) InitAsync(opts InitOptions) (int32, error) {
	return m.callAsync("sdk_init_async", func(l nativeLib) (int32, error) {
		return l.initAsync(initJSON(opts))
	})
}

// SDKInfo returns current SDK runtime information.
func (m *Manager) SDKInfo() (*Response, error) {
	return m.callSync("sdk_info", func(l nativeLib) (int32, string, error) {
		return l.info()
	})
}

// Shutdown stops the SDK singleton synchronously.
func (m *Manager) Shutdown() error {
	_, err := m.callAsync("sdk_shutdown", func(l nativeLib) (int32, error) {
		return l.shutdown()
	})
	return err
}

// SetActiveEnv sets the currently active browser environment ID.
// All Browser Action tools will use this envId when their own envId is omitted.
func (m *Manager) SetActiveEnv(envID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeEnvID = envID
}

// GetActiveEnv returns the currently active browser environment ID.
// Returns empty string if no environment has been selected.
func (m *Manager) GetActiveEnv() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeEnvID
}

// TokenUpdate refreshes the userSig asynchronously.
func (m *Manager) TokenUpdate(jsonBody string) (int32, error) {
	return m.callAsync("sdk_token_update", func(l nativeLib) (int32, error) {
		return l.tokenUpdate(jsonBody)
	})
}

// ---------- Browser ----------

// BrowserInstall installs browser core resources asynchronously.
func (m *Manager) BrowserInstall(jsonBody string) (int32, error) {
	return m.callAsync("sdk_browser_install", func(l nativeLib) (int32, error) {
		return l.browserInstall(jsonBody)
	})
}

// BrowserInfo returns the list of currently running browsers.
func (m *Manager) BrowserInfo() (*Response, error) {
	return m.callSync("sdk_browser_info", func(l nativeLib) (int32, string, error) {
		return l.browserInfo()
	})
}

// BrowserOpen opens one browser environment asynchronously.
// Result event type "browser-open-success" signals CDP readiness.
func (m *Manager) BrowserOpen(envID string, urls []string, args []string) (int32, error) {
	return m.callAsync("sdk_browser_open", func(l nativeLib) (int32, error) {
		return l.browserOpen(browserOpenJSON(envID, urls, args))
	})
}

// BrowserClose closes one browser environment asynchronously.
func (m *Manager) BrowserClose(envID string) (int32, error) {
	return m.callAsync("sdk_browser_close", func(l nativeLib) (int32, error) {
		return l.browserClose(browserCloseJSON(envID))
	})
}

// BrowserCommand sends a CDP command to a running browser via WebSocket.
// Implemented MCP-side — connects to the browser's DevTools endpoint.
// See cdp.go for the implementation. (Windows only)

// GetUserSig retrieves a userSig via the BroSDK HTTP API.
// This function does NOT require the native library to be loaded.
func (m *Manager) GetUserSig(apiKey string) (string, error) {
	if apiKey == "" {
		return "", sdkError("apiKey is required")
	}
	return fetchUserSig(apiKey)
}

// ---------- Environment CRUD ----------

// EnvCreate creates a new environment (sync).
func (m *Manager) EnvCreate(jsonBody string) (*Response, error) {
	return m.callSync("sdk_env_create", func(l nativeLib) (int32, string, error) {
		return l.envCreate(jsonBody)
	})
}

// EnvPage queries environments (sync).
func (m *Manager) EnvPage(jsonBody string) (*Response, error) {
	return m.callSync("sdk_env_page", func(l nativeLib) (int32, string, error) {
		return l.envPage(jsonBody)
	})
}

// EnvUpdate updates an existing environment (sync).
func (m *Manager) EnvUpdate(jsonBody string) (*Response, error) {
	return m.callSync("sdk_env_update", func(l nativeLib) (int32, string, error) {
		return l.envUpdate(jsonBody)
	})
}

// EnvDestroy deletes an environment (sync).
func (m *Manager) EnvDestroy(jsonBody string) (*Response, error) {
	return m.callSync("sdk_env_destroy", func(l nativeLib) (int32, string, error) {
		return l.envDestroy(jsonBody)
	})
}

// ---------- internals ----------

func (m *Manager) callSync(api string, fn func(nativeLib) (int32, string, error)) (*Response, error) {
	m.mu.RLock()
	lib := m.lib
	m.mu.RUnlock()

	if lib == nil {
		return nil, sdkError("native library not loaded")
	}
	code, resp, err := fn(lib)
	if err != nil {
		return nil, err
	}
	if code < 0 {
		return &Response{Code: code, Response: resp}, sdkError(api + " returned error code " + intStr(int(code)))
	}
	return &Response{Code: code, Response: resp}, nil
}

func (m *Manager) callAsync(api string, fn func(nativeLib) (int32, error)) (int32, error) {
	m.mu.RLock()
	lib := m.lib
	m.mu.RUnlock()

	if lib == nil {
		return -1, sdkError("native library not loaded")
	}
	code, err := fn(lib)
	if err != nil {
		return code, err
	}
	if code < 0 {
		return code, sdkError(api + " returned error code " + intStr(int(code)))
	}
	return code, nil
}

func (m *Manager) emit(evt Event) {
	// Capture remoteDebuggingPort from browser-open-success events
	// for CDP proxy fallback (browser_info may not return running browsers).
	if evt.Data != "" {
		var payload struct {
			Type string `json:"type"`
			Data struct {
				EnvID               string `json:"envId"`
				RemoteDebuggingPort int    `json:"remoteDebuggingPort"`
			} `json:"data"`
		}
		if json.Unmarshal([]byte(evt.Data), &payload) == nil &&
			payload.Type == "browser-open-success" &&
			payload.Data.EnvID != "" &&
			payload.Data.RemoteDebuggingPort > 0 {
			m.mu.Lock()
			// Clean up any stale browserTab from a prior run for this envID
			// (defense-in-depth: covers browser_open without explicit close).
			if bt := m.browsers[payload.Data.EnvID]; bt != nil {
				if bt.tabCancel != nil {
					bt.tabCancel()
				}
				if bt.allocCancel != nil {
					bt.allocCancel()
				}
				delete(m.browsers, payload.Data.EnvID)
			}
			m.debugPorts[payload.Data.EnvID] = payload.Data.RemoteDebuggingPort
			m.mu.Unlock()
		}
	}

	m.mu.RLock()
	ls := append([]func(Event){}, m.listeners...)
	m.mu.RUnlock()
	for _, fn := range ls {
		fn(evt)
	}
}

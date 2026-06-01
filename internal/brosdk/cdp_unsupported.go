//go:build !windows && !darwin

package brosdk

// BrowserCommand — stub for unsupported platforms.
func (m *Manager) BrowserCommand(envID, method string, params map[string]any, sessionID string) (*CDPResponse, error) {
	return nil, sdkError("browser_command is not supported on this platform")
}

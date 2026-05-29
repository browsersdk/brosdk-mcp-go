//go:build !windows

package brosdk

// BrowserCommand — stub for non-Windows builds.
func (m *Manager) BrowserCommand(envID, method string, params map[string]any, sessionID string) (*CDPResponse, error) {
	return nil, sdkError("browser_command is only supported on Windows")
}

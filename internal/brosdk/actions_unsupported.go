//go:build !windows

package brosdk

import "encoding/json"

// Stubs for non-Windows builds.

func (m *Manager) CloseTab(envID string)            {}
func (m *Manager) CloseBrowser(envID string)         {}
func (m *Manager) CloseAllBrowsers()                 {}
func (m *Manager) SetActiveSession(envID, sid string) {}
func (m *Manager) GetActiveSession(envID string) string { return "" }

func (m *Manager) Navigate(envID, url string) (*NavigateResult, error) {
	return nil, sdkError("browser_navigate is only supported on Windows")
}

func (m *Manager) Snapshot(envID, sessionID string) (json.RawMessage, error) {
	return nil, sdkError("browser_snapshot is only supported on Windows")
}

func (m *Manager) Click(envID, sessionID, selector string) error {
	return sdkError("browser_click is only supported on Windows")
}

func (m *Manager) ClickRef(envID, sessionID, ref string) error {
	return sdkError("browser_click_ref is only supported on Windows")
}

func (m *Manager) DblClick(envID, sessionID, selector string) error {
	return sdkError("browser_dblclick is only supported on Windows")
}

func (m *Manager) Focus(envID, sessionID, selector string) error {
	return sdkError("browser_focus is only supported on Windows")
}

func (m *Manager) Type(envID, sessionID, selector, text string) error {
	return sdkError("browser_type is only supported on Windows")
}

func (m *Manager) TypeRef(envID, sessionID, ref, text string) error {
	return sdkError("browser_type_ref is only supported on Windows")
}

func (m *Manager) Fill(envID, sessionID, selector, text string) error {
	return sdkError("browser_fill is only supported on Windows")
}

func (m *Manager) FillRef(envID, sessionID, ref, text string) error {
	return sdkError("browser_fill_ref is only supported on Windows")
}

func (m *Manager) FindClickText(envID, sessionID, text string) error {
	return sdkError("browser_find_click_text is only supported on Windows")
}

func (m *Manager) PressKey(envID, sessionID, key string) error {
	return sdkError("browser_press_key is only supported on Windows")
}

func (m *Manager) KeyboardType(envID, sessionID, text string) error {
	return sdkError("browser_keyboard_type is only supported on Windows")
}

func (m *Manager) KeyboardInsertText(envID, sessionID, text string) error {
	return sdkError("browser_keyboard_insert_text is only supported on Windows")
}

func (m *Manager) KeyDown(envID, sessionID, key string) error {
	return sdkError("browser_key_down is only supported on Windows")
}

func (m *Manager) KeyUp(envID, sessionID, key string) error {
	return sdkError("browser_key_up is only supported on Windows")
}

func (m *Manager) Hover(envID, sessionID, selector string) error {
	return sdkError("browser_hover is only supported on Windows")
}

func (m *Manager) SelectOption(envID, sessionID, selector, value string) error {
	return sdkError("browser_select_option is only supported on Windows")
}

func (m *Manager) Check(envID, sessionID, selector string) error {
	return sdkError("browser_check is only supported on Windows")
}

func (m *Manager) Uncheck(envID, sessionID, selector string) error {
	return sdkError("browser_uncheck is only supported on Windows")
}

func (m *Manager) Scroll(envID, sessionID, direction string, px int, selector string) error {
	return sdkError("browser_scroll is only supported on Windows")
}

func (m *Manager) ScrollIntoView(envID, sessionID, selector string) error {
	return sdkError("browser_scroll_into_view is only supported on Windows")
}

func (m *Manager) Drag(envID, sessionID, sourceSel, targetSel string) error {
	return sdkError("browser_drag is only supported on Windows")
}

func (m *Manager) Evaluate(envID, sessionID, expression string, result any) error {
	return sdkError("browser_evaluate is only supported on Windows")
}

func (m *Manager) UploadFile(envID, sessionID, selector string, files []string) error {
	return sdkError("browser_upload_file is only supported on Windows")
}

func (m *Manager) Screenshot(envID, sessionID string, opts ScreenshotOptions) (string, error) {
	return "", sdkError("browser_screenshot is only supported on Windows")
}

func (m *Manager) PDF(envID, sessionID, path string) (string, error) {
	return "", sdkError("browser_pdf is only supported on Windows")
}

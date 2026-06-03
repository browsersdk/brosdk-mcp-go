//go:build !windows && !darwin

package brosdk

import "encoding/json"

// Stubs for non-Windows builds.

func (m *Manager) CloseTab(envID string)            {}
func (m *Manager) CloseBrowser(envID string)         {}
func (m *Manager) CloseAllBrowsers()                 {}
func (m *Manager) SetActiveSession(envID, sid string) {}
func (m *Manager) GetActiveSession(envID string) string { return "" }

func (m *Manager) NewTab(envID string) (string, error) {
	return "", sdkError("browser_new_tab is only supported on Windows/macOS")
}
func (m *Manager) ListTabs(envID string) ([]tabInfo, error) {
	return nil, sdkError("browser_list_tabs is only supported on Windows/macOS")
}
func (m *Manager) GetCookies(envID string, urls []string) (json.RawMessage, error) {
	return nil, sdkError("browser_get_cookies is only supported on Windows/macOS")
}
func (m *Manager) SetCookies(envID string, cookieList []map[string]any) error {
	return sdkError("browser_set_cookies is only supported on Windows/macOS")
}
func (m *Manager) GetHTML(envID, sessionID string) (string, error) {
	return "", sdkError("browser_get_html is only supported on Windows/macOS")
}

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

func (m *Manager) Reload(envID, sessionID string) error {
	return sdkError("browser_reload is only supported on Windows")
}

func (m *Manager) Back(envID, sessionID string) error {
	return sdkError("browser_back is only supported on Windows")
}

func (m *Manager) Forward(envID, sessionID string) error {
	return sdkError("browser_forward is only supported on Windows")
}

func (m *Manager) GetText(envID, sessionID, selector string) (string, error) {
	return "", sdkError("browser_get_text is only supported on Windows")
}

func (m *Manager) GetValue(envID, sessionID, selector string) (string, error) {
	return "", sdkError("browser_get_value is only supported on Windows")
}

func (m *Manager) UncheckRef(envID, sessionID, ref string) error {
	return sdkError("browser_uncheck_ref is only supported on Windows")
}

func (m *Manager) FocusRef(envID, sessionID, ref string) error {
	return sdkError("browser_focus_ref is only supported on Windows")
}

func (m *Manager) HoverRef(envID, sessionID, ref string) error {
	return sdkError("browser_hover_ref is only supported on Windows")
}

func (m *Manager) SelectOptionRef(envID, sessionID, ref, value string) error {
	return sdkError("browser_select_option_ref is only supported on Windows")
}

func (m *Manager) CheckRef(envID, sessionID, ref string) error {
	return sdkError("browser_check_ref is only supported on Windows")
}

func (m *Manager) FindRefs(envID, role, name, value string, limit int) ([]RefResult, error) {
	return nil, sdkError("browser_find_ref is only supported on Windows/macOS")
}
func (m *Manager) Wait(envID, text, role, name, selector string, timeoutMs int) error {
	return sdkError("browser_wait is only supported on Windows/macOS")
}
func (m *Manager) WaitForNavigation(envID string, timeoutMs int) error {
	return sdkError("browser_wait (navigation) is only supported on Windows/macOS")
}
func (m *Manager) WaitForSelector(envID, selector string, timeoutMs int) error {
	return sdkError("browser_wait (selector) is only supported on Windows/macOS")
}
func (m *Manager) PageState(envID string) (*PageState, error) {
	return nil, sdkError("browser_page_state is only supported on Windows/macOS")
}
func (m *Manager) Exists(envID, role, name, value string) (bool, error) {
	return false, sdkError("browser_exists is only supported on Windows/macOS")
}
func (m *Manager) Dialog(envID, action string) (*DialogResult, error) {
	return nil, sdkError("browser_dialog is only supported on Windows/macOS")
}
func (m *Manager) FillForm(envID string, fields map[string]string, submitSel string) (*FillFormResult, error) {
	return nil, sdkError("browser_fill_form is only supported on Windows/macOS")
}

//go:build windows || darwin

package brosdk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	cdpdom "github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// ---------- chromedp context management ----------

const actionTimeout = 10 * time.Second

func runAction(ctx context.Context, actions ...chromedp.Action) error {
	actionCtx, cancel := context.WithTimeout(ctx, actionTimeout)
	defer cancel()
	return chromedp.Run(actionCtx, actions...)
}

// browserTab tracks chromedp resources for one browser environment.
// tabCtx/tabCancel hold the active tab. Additional non-active tabs are
// stored in tabs/tabCancels maps keyed by internal tab ID (e.g. "tab-1").
type browserTab struct {
	allocCtx    context.Context               // remote allocator context (shared by all tabs)
	allocCancel context.CancelFunc            // cancels allocator (and all tabs)
	tabCtx      context.Context               // active tab context
	tabCancel   context.CancelFunc            // active tab cancel
	tabs        map[string]context.Context    // non-active tabs: tabID → tab context
	tabCancels  map[string]context.CancelFunc // non-active tabs: tabID → cancel func
	nextTabID   int                           // monotonically increasing tab counter
}

// ensureBrowser returns the browserTab for envID, creating one if needed.
// If the cached allocator is stale (context cancelled / connection lost),
// it is torn down and a fresh one is created.
func (m *Manager) ensureBrowser(envID string) (*browserTab, error) {
	m.mu.RLock()
	bt := m.browsers[envID]
	m.mu.RUnlock()

	// Check cached entry: if the allocator context is already done, discard it.
	if bt != nil && bt.allocCtx.Err() != nil {
		m.closeAllTabsLocked(bt)
		if bt.allocCancel != nil {
			bt.allocCancel()
		}
		m.mu.Lock()
		delete(m.browsers, envID)
		m.mu.Unlock()
		bt = nil
	}

	if bt != nil {
		return bt, nil
	}

	wsURL, err := m.findDebugURL(envID)
	if err != nil {
		return nil, fmt.Errorf("resolve debug URL for %s: %w", envID, err)
	}

	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), wsURL)

	bt = &browserTab{
		allocCtx:    allocCtx,
		allocCancel: allocCancel,
		tabs:        make(map[string]context.Context),
		tabCancels:  make(map[string]context.CancelFunc),
		nextTabID:   1,
	}

	m.mu.Lock()
	if m.browsers == nil {
		m.browsers = make(map[string]*browserTab)
	}
	m.browsers[envID] = bt
	m.mu.Unlock()

	return bt, nil
}

// ensureTab returns the active tab context for envID, creating one if needed.
func (m *Manager) ensureTab(envID string) (context.Context, error) {
	bt, err := m.ensureBrowser(envID)
	if err != nil {
		return nil, err
	}
	if bt.tabCtx != nil {
		return bt.tabCtx, nil
	}
	bt.tabCtx, bt.tabCancel = chromedp.NewContext(bt.allocCtx)
	return bt.tabCtx, nil
}

// newTab creates a fresh chromedp tab context under the given allocator.
func newTab(allocCtx context.Context) (context.Context, context.CancelFunc) {
	return chromedp.NewContext(allocCtx)
}

// CloseTab tears down a specific tab by its internal tab ID.
// If the tab is the active one, the most recent remaining tab becomes active.
// Returns an error if the tab doesn't exist.
func (m *Manager) CloseTab(envID, tabID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	bt := m.browsers[envID]
	if bt == nil {
		return fmt.Errorf("no browser for env %q", envID)
	}
	// Close a non-active tab.
	if tt, ok := bt.tabs[tabID]; ok {
		if cancel := bt.tabCancels[tabID]; cancel != nil {
			cancel()
		}
		_ = tt // prevent unused
		delete(bt.tabs, tabID)
		delete(bt.tabCancels, tabID)
		return nil
	}
	if tabID != "" && tabID != activeTabSentinel {
		return fmt.Errorf("tab %q not found", tabID)
	}
	// Close the active tab.
	if bt.tabCancel != nil {
		bt.tabCancel()
	}
	bt.tabCtx = nil
	bt.tabCancel = nil

	// Promote a background tab to become the new active tab.
	if len(bt.tabs) > 0 {
		for id, ctx := range bt.tabs {
			bt.tabCtx = ctx
			bt.tabCancel = bt.tabCancels[id]
			delete(bt.tabs, id)
			delete(bt.tabCancels, id)
			break
		}
	}

	return nil
}

const activeTabSentinel = "__active__"

// closeActiveTab closes the active tab without error checking.
func (m *Manager) closeActiveTab(bt *browserTab) {
	if bt.tabCancel != nil {
		bt.tabCancel()
	}
	bt.tabCtx = nil
	bt.tabCancel = nil
}

// newTabID generates a unique internal tab ID.
func (bt *browserTab) newTabID() string {
	id := bt.nextTabID
	bt.nextTabID++
	return fmt.Sprintf("tab-%d", id)
}

// NewTab creates a new blank tab in the browser environment.
// The new tab becomes the active tab. The previous active tab is
// saved as a background tab. Returns the new tab's internal ID.
func (m *Manager) NewTab(envID string) (string, error) {
	bt, err := m.ensureBrowser(envID)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	// Save current active tab as a background tab.
	if bt.tabCtx != nil {
		oldID := bt.newTabID()
		bt.tabs[oldID] = bt.tabCtx
		bt.tabCancels[oldID] = bt.tabCancel
	}

	// Create a new tab as the active one.
	bt.tabCtx, bt.tabCancel = newTab(bt.allocCtx)
	newID := bt.newTabID()
	return newID, nil
}

// tabInfo holds metadata about an open tab.
type tabInfo struct {
	TabID    string `json:"tabId"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	IsActive bool   `json:"isActive"`
}

// ListTabs returns information about all open tabs in the browser environment.
func (m *Manager) ListTabs(envID string) ([]tabInfo, error) {
	bt, err := m.ensureBrowser(envID)
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []tabInfo

	// Collect active tab info.
	if bt.tabCtx != nil {
		var title, url string
		// Best-effort fetch — don't fail if tab is navigating.
		_ = chromedp.Run(bt.tabCtx,
			chromedp.Title(&title),
			chromedp.Location(&url),
		)
		result = append(result, tabInfo{
			TabID:    activeTabSentinel,
			Title:    title,
			URL:      url,
			IsActive: true,
		})
	}

	// Collect background tab infos.
	for id, ctx := range bt.tabs {
		var title, url string
		_ = chromedp.Run(ctx,
			chromedp.Title(&title),
			chromedp.Location(&url),
		)
		result = append(result, tabInfo{
			TabID:    id,
			Title:    title,
			URL:      url,
			IsActive: false,
		})
	}

	return result, nil
}

// GetCookies returns cookies for the active tab (or all cookies if url is empty).
func (m *Manager) GetCookies(envID string, urls []string) (json.RawMessage, error) {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return nil, err
	}
	var result json.RawMessage
	err = chromedp.Run(tabCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			cmd := network.GetCookies()
			if len(urls) > 0 {
				cmd = cmd.WithURLs(urls)
			}
			cookies, fetchErr := cmd.Do(ctx)
			if fetchErr != nil {
				return fetchErr
			}
			b, err := json.Marshal(cookies)
			if err != nil {
				return err
			}
			result = json.RawMessage(b)
			return nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("get cookies: %w", err)
	}
	return result, nil
}

// SetCookies sets one or more cookies on the active tab.
func (m *Manager) SetCookies(envID string, cookieList []map[string]any) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	return chromedp.Run(tabCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			for _, c := range cookieList {
				name, _ := c["name"].(string)
				value, _ := c["value"].(string)
				if name == "" {
					continue
				}
				cmd := network.SetCookie(name, value)
				if url, ok := c["url"].(string); ok {
					cmd = cmd.WithURL(url)
				}
				if domain, ok := c["domain"].(string); ok {
					cmd = cmd.WithDomain(domain)
				}
				if path, ok := c["path"].(string); ok {
					cmd = cmd.WithPath(path)
				}
				if secure, ok := c["secure"].(bool); ok && secure {
					cmd = cmd.WithSecure(true)
				}
				if httpOnly, ok := c["httpOnly"].(bool); ok && httpOnly {
					cmd = cmd.WithHTTPOnly(true)
				}
				if err := cmd.Do(ctx); err != nil {
					return err
				}
			}
			return nil
		}),
	)
}

// CloseBrowser tears down all chromedp resources for envID, including all tabs.
func (m *Manager) CloseBrowser(envID string) {
	m.mu.Lock()
	bt := m.browsers[envID]
	if bt != nil {
		m.closeAllTabsLocked(bt)
		if bt.allocCancel != nil {
			bt.allocCancel()
		}
		delete(m.browsers, envID)
	}
	delete(m.debugPorts, envID)
	m.mu.Unlock()
	RemoveCDPConn(envID)
}

// closeAllTabsLocked closes all tabs (active + background). Caller must hold m.mu.
func (m *Manager) closeAllTabsLocked(bt *browserTab) {
	if bt.tabCancel != nil {
		bt.tabCancel()
		bt.tabCtx = nil
		bt.tabCancel = nil
	}
	for _, cancel := range bt.tabCancels {
		if cancel != nil {
			cancel()
		}
	}
	bt.tabs = make(map[string]context.Context)
	bt.tabCancels = make(map[string]context.CancelFunc)
}

// ResetEnv tears down chromedp resources for envID when the browser is
// being re-opened (e.g. browser_open after a prior close or crash).
// Unlike CloseBrowser this does NOT close CDP conns — the new browser
// will get a fresh WebSocket. It tears down allocator + all tabs.
func (m *Manager) ResetEnv(envID string) {
	m.mu.Lock()
	bt := m.browsers[envID]
	if bt != nil {
		m.closeAllTabsLocked(bt)
		if bt.allocCancel != nil {
			bt.allocCancel()
		}
		delete(m.browsers, envID)
	}
	m.mu.Unlock()
}

// CloseAllBrowsers shuts down all chromedp connections across all envs.
func (m *Manager) CloseAllBrowsers() {
	m.mu.Lock()
	for envID, bt := range m.browsers {
		m.closeAllTabsLocked(bt)
		if bt.allocCancel != nil {
			bt.allocCancel()
		}
		delete(m.browsers, envID)
	}
	m.mu.Unlock()
}

// ---------- helpers ----------

// resolveNodeID resolves a backendNodeId to a cdp.NodeID.
func resolveNodeID(ctx context.Context, backendID cdp.BackendNodeID) (cdp.NodeID, error) {
	obj, err := cdpdom.ResolveNode().WithBackendNodeID(backendID).Do(ctx)
	if err != nil {
		return 0, fmt.Errorf("DOM.resolveNode: %w", err)
	}
	nodeID, err := cdpdom.RequestNode(runtime.RemoteObjectID(obj.ObjectID)).Do(ctx)
	if err != nil {
		return 0, fmt.Errorf("DOM.requestNode: %w", err)
	}
	return nodeID, nil
}

func toBackendID(ref string) (cdp.BackendNodeID, error) {
	n, err := strconv.Atoi(ref)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid ref: %s", ref)
	}
	return cdp.BackendNodeID(n), nil
}

// ---------- Browser Actions ----------

// NavigateResult is returned by Navigate.
type NavigateResult struct {
	TargetID  string `json:"targetId"`
	SessionID string `json:"sessionId"`
}

// Navigate opens a URL in a new page tab.
func (m *Manager) Navigate(envID, url string) (*NavigateResult, error) {
	bt, err := m.ensureBrowser(envID)
	if err != nil {
		return nil, err
	}

	// Close old tab if any
	if bt.tabCancel != nil {
		bt.tabCancel()
	}
	bt.tabCtx, bt.tabCancel = chromedp.NewContext(bt.allocCtx)

	var targetID string
	err = chromedp.Run(bt.tabCtx,
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			if c := chromedp.FromContext(ctx); c != nil && c.Target != nil {
				targetID = string(c.Target.TargetID)
			}
			return nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("navigate: %w", err)
	}

	return &NavigateResult{TargetID: targetID, SessionID: targetID}, nil
}

// Snapshot returns the full accessibility tree.
func (m *Manager) Snapshot(envID, sessionID string) (json.RawMessage, error) {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return nil, err
	}

	var raw json.RawMessage
	err = chromedp.Run(tabCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			tree, err := accessibility.GetFullAXTree().Do(ctx)
			if err != nil {
				return err
			}
			b, err := json.Marshal(map[string]any{"nodes": tree})
			if err != nil {
				return err
			}
			raw = b
			return nil
		}),
	)
	return raw, err
}

// Click clicks the element matching a CSS selector.
func (m *Manager) Click(envID, sessionID, selector string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	return runAction(tabCtx,
		chromedp.ScrollIntoView(selector),
		chromedp.Click(selector),
	)
}

// ClickRef clicks an element by its accessibility backendNodeId.
func (m *Manager) ClickRef(envID, sessionID, ref string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	backendID, err := toBackendID(ref)
	if err != nil {
		return err
	}

	return chromedp.Run(tabCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			nodeID, err := resolveNodeID(ctx, backendID)
			if err != nil {
				return err
			}
			nid := cdp.NodeID(nodeID)
			if err := chromedp.ScrollIntoView([]cdp.NodeID{nid}, chromedp.ByNodeID).Do(ctx); err != nil {
				return err
			}
			return chromedp.Click([]cdp.NodeID{nid}, chromedp.ByNodeID).Do(ctx)
		}),
	)
}

// DblClick double-clicks the element matching a CSS selector.
func (m *Manager) DblClick(envID, sessionID, selector string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	return runAction(tabCtx,
		chromedp.ScrollIntoView(selector),
		chromedp.DoubleClick(selector),
	)
}

// Focus focuses the element matching a CSS selector.
func (m *Manager) Focus(envID, sessionID, selector string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	return runAction(tabCtx,
		chromedp.ScrollIntoView(selector),
		chromedp.Focus(selector),
	)
}

// FocusRef focuses an element by its accessibility ref.
func (m *Manager) FocusRef(envID, sessionID, ref string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	backendID, err := toBackendID(ref)
	if err != nil {
		return err
	}

	return chromedp.Run(tabCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			nodeID, err := resolveNodeID(ctx, backendID)
			if err != nil {
				return err
			}
			nid := cdp.NodeID(nodeID)
			if err := chromedp.ScrollIntoView([]cdp.NodeID{nid}, chromedp.ByNodeID).Do(ctx); err != nil {
				return err
			}
			return chromedp.Focus([]cdp.NodeID{nid}, chromedp.ByNodeID).Do(ctx)
		}),
	)
}

// Hover moves the mouse over an element matching a CSS selector.
func (m *Manager) Hover(envID, sessionID, selector string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	return chromedp.Run(tabCtx,
		chromedp.ScrollIntoView(selector),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var nodeIDs []cdp.NodeID
			if err := chromedp.NodeIDs(selector, &nodeIDs, chromedp.ByQuery).Do(ctx); err != nil {
				return fmt.Errorf("query node: %w", err)
			}
			if len(nodeIDs) == 0 {
				return fmt.Errorf("selector not found: %s", selector)
			}
			box, err := cdpdom.GetBoxModel().WithNodeID(nodeIDs[0]).Do(ctx)
			if err != nil {
				return fmt.Errorf("get box model: %w", err)
			}
			// Content quad: [x1,y1, x2,y2, x3,y3, x4,y4]
			x := (box.Content[0] + box.Content[2]) / 2
			y := (box.Content[1] + box.Content[5]) / 2
			return input.DispatchMouseEvent(input.MouseMoved, x, y).Do(ctx)
		}),
	)
}

// HoverRef hovers over an element by its accessibility ref.
func (m *Manager) HoverRef(envID, sessionID, ref string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	backendID, err := toBackendID(ref)
	if err != nil {
		return err
	}

	return chromedp.Run(tabCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			nodeID, err := resolveNodeID(ctx, backendID)
			if err != nil {
				return err
			}
			nid := cdp.NodeID(nodeID)
			if err := chromedp.ScrollIntoView([]cdp.NodeID{nid}, chromedp.ByNodeID).Do(ctx); err != nil {
				return err
			}
			box, err := cdpdom.GetBoxModel().WithNodeID(nid).Do(ctx)
			if err != nil {
				return fmt.Errorf("get box model: %w", err)
			}
			x := (box.Content[0] + box.Content[2]) / 2
			y := (box.Content[1] + box.Content[5]) / 2
			return input.DispatchMouseEvent(input.MouseMoved, x, y).Do(ctx)
		}),
	)
}

// Type types text into an element (appends to existing value).
func (m *Manager) Type(envID, sessionID, selector, text string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	return runAction(tabCtx,
		chromedp.ScrollIntoView(selector),
		chromedp.Focus(selector),
		chromedp.SendKeys(selector, text),
	)
}

// TypeRef types text into an element by its accessibility ref.
func (m *Manager) TypeRef(envID, sessionID, ref, text string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	backendID, err := toBackendID(ref)
	if err != nil {
		return err
	}

	return chromedp.Run(tabCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			nodeID, err := resolveNodeID(ctx, backendID)
			if err != nil {
				return err
			}
			nid := cdp.NodeID(nodeID)
			if err := chromedp.ScrollIntoView([]cdp.NodeID{nid}, chromedp.ByNodeID).Do(ctx); err != nil {
				return err
			}
			if err := chromedp.Focus([]cdp.NodeID{nid}, chromedp.ByNodeID).Do(ctx); err != nil {
				return err
			}
			return chromedp.SendKeys([]cdp.NodeID{nid}, text, chromedp.ByNodeID).Do(ctx)
		}),
	)
}

// Fill clears the input and types new text.
func (m *Manager) Fill(envID, sessionID, selector, text string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	return runAction(tabCtx,
		chromedp.ScrollIntoView(selector),
		chromedp.SetValue(selector, text),
	)
}

// FillRef clears the input (by ref) and types new text.
func (m *Manager) FillRef(envID, sessionID, ref, text string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	backendID, err := toBackendID(ref)
	if err != nil {
		return err
	}

	return chromedp.Run(tabCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			nodeID, err := resolveNodeID(ctx, backendID)
			if err != nil {
				return err
			}
			nid := cdp.NodeID(nodeID)
			if err := chromedp.ScrollIntoView([]cdp.NodeID{nid}, chromedp.ByNodeID).Do(ctx); err != nil {
				return err
			}
			return chromedp.SetValue([]cdp.NodeID{nid}, text, chromedp.ByNodeID).Do(ctx)
		}),
	)
}

// FindClickText finds visible text on the page and clicks the containing element.
func (m *Manager) FindClickText(envID, sessionID, text string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}

	// Escape double quotes in text for XPath
	escaped := strings.ReplaceAll(text, `"`, `&quot;`)

	return chromedp.Run(tabCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			query := fmt.Sprintf(`//*[contains(text(),"%s")]`, escaped)
			searchID, resultCount, err := cdpdom.PerformSearch(query).Do(ctx)
			if err != nil {
				return fmt.Errorf("DOM.performSearch: %w", err)
			}
			if resultCount == 0 {
				return fmt.Errorf("text not found: %s", text)
			}
			nodeIDs, err := cdpdom.GetSearchResults(searchID, 0, 1).Do(ctx)
			if err != nil {
				return fmt.Errorf("DOM.getSearchResults: %w", err)
			}
			_ = cdpdom.DiscardSearchResults(searchID).Do(ctx)

			if len(nodeIDs) == 0 {
				return fmt.Errorf("text not found: %s", text)
			}
			nid := cdp.NodeID(nodeIDs[0])
			if err := chromedp.ScrollIntoView([]cdp.NodeID{nid}, chromedp.ByNodeID).Do(ctx); err != nil {
				return err
			}
			return chromedp.Click([]cdp.NodeID{nid}, chromedp.ByNodeID).Do(ctx)
		}),
	)
}

// PressKey presses a key (keyDown + keyUp).
func (m *Manager) PressKey(envID, sessionID, key string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	return chromedp.Run(tabCtx, chromedp.KeyEvent(key))
}

// KeyboardType dispatches char events for each character.
func (m *Manager) KeyboardType(envID, sessionID, text string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	actions := make([]chromedp.Action, len(text))
	for i, ch := range text {
		c := ch
		actions[i] = chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchKeyEvent(input.KeyChar).
				WithText(string(c)).
				Do(ctx)
		})
	}
	return chromedp.Run(tabCtx, actions...)
}

// KeyboardInsertText uses Input.insertText (preferred for focused field text input).
func (m *Manager) KeyboardInsertText(envID, sessionID, text string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	return chromedp.Run(tabCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return input.InsertText(text).Do(ctx)
		}),
	)
}

// KeyDown sends a keyDown event.
func (m *Manager) KeyDown(envID, sessionID, key string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	return chromedp.Run(tabCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchKeyEvent(input.KeyDown).
				WithKey(key).
				Do(ctx)
		}),
	)
}

// KeyUp sends a keyUp event.
func (m *Manager) KeyUp(envID, sessionID, key string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	return chromedp.Run(tabCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchKeyEvent(input.KeyUp).
				WithKey(key).
				Do(ctx)
		}),
	)
}

// SelectOption sets the value of a <select> element.
func (m *Manager) SelectOption(envID, sessionID, selector, value string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	return chromedp.Run(tabCtx,
		chromedp.ScrollIntoView(selector),
		chromedp.SetValue(selector, value),
	)
}

// SelectOptionRef sets the value of a <select> element by its accessibility ref.
// Uses CDP ResolveNode + CallFunctionOn to set .value and dispatch change event,
// since SetValue by NodeID does not reliably fire the change event for <select>.
func (m *Manager) SelectOptionRef(envID, sessionID, ref, value string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	backendID, err := toBackendID(ref)
	if err != nil {
		return err
	}

	return chromedp.Run(tabCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			nodeID, err := resolveNodeID(ctx, backendID)
			if err != nil {
				return err
			}
			nid := cdp.NodeID(nodeID)
			if err := chromedp.ScrollIntoView([]cdp.NodeID{nid}, chromedp.ByNodeID).Do(ctx); err != nil {
				return err
			}
			obj, err := cdpdom.ResolveNode().WithBackendNodeID(backendID).Do(ctx)
			if err != nil {
				return err
			}
			_, _, err = runtime.CallFunctionOn(
				fmt.Sprintf("function(){this.value=%q;this.dispatchEvent(new Event('change',{bubbles:true}))}", value),
			).WithObjectID(obj.ObjectID).Do(ctx)
			return err
		}),
	)
}

// Check checks a checkbox or radio input.
func (m *Manager) Check(envID, sessionID, selector string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	// Click only if not already checked
	var checked bool
	err = chromedp.Run(tabCtx,
		chromedp.ScrollIntoView(selector),
		chromedp.Evaluate(
			fmt.Sprintf(`document.querySelector(%q)?.checked`, selector),
			&checked,
		),
	)
	if err != nil {
		return err
	}
	if !checked {
		return chromedp.Run(tabCtx, chromedp.Click(selector))
	}
	return nil
}

// CheckRef checks a checkbox/radio by its accessibility ref.
// Uses CDP ResolveNode + CallFunctionOn to read the .checked property,
// then clicks only if the element is not already checked.
func (m *Manager) CheckRef(envID, sessionID, ref string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	backendID, err := toBackendID(ref)
	if err != nil {
		return err
	}

	return chromedp.Run(tabCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			nodeID, err := resolveNodeID(ctx, backendID)
			if err != nil {
				return err
			}
			nid := cdp.NodeID(nodeID)

			// Check .checked property via ResolveNode → CallFunctionOn
			obj, err := cdpdom.ResolveNode().WithBackendNodeID(backendID).Do(ctx)
			if err != nil {
				return err
			}
			result, _, err := runtime.CallFunctionOn("function(){return this.checked}").
				WithObjectID(obj.ObjectID).
				Do(ctx)
			if err != nil {
				return err
			}
			var checked bool
			if result != nil && len(result.Value) > 0 {
				json.Unmarshal(result.Value, &checked)
			}

			if checked {
				return nil
			}

			if err := chromedp.ScrollIntoView([]cdp.NodeID{nid}, chromedp.ByNodeID).Do(ctx); err != nil {
				return err
			}
			return chromedp.Click([]cdp.NodeID{nid}, chromedp.ByNodeID).Do(ctx)
		}),
	)
}

// Uncheck unchecks a checkbox.
func (m *Manager) Uncheck(envID, sessionID, selector string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	var checked bool
	err = chromedp.Run(tabCtx,
		chromedp.ScrollIntoView(selector),
		chromedp.Evaluate(
			fmt.Sprintf(`document.querySelector(%q)?.checked`, selector),
			&checked,
		),
	)
	if err != nil {
		return err
	}
	if checked {
		return chromedp.Run(tabCtx, chromedp.Click(selector))
	}
	return nil
}

// Scroll scrolls the page or a specific element.
func (m *Manager) Scroll(envID, sessionID, direction string, px int, selector string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	if px <= 0 {
		px = 300
	}

	var x, y int
	switch direction {
	case "up":
		y = -px
	case "down":
		y = px
	case "left":
		x = -px
	case "right":
		x = px
	default:
		return fmt.Errorf("invalid scroll direction: %s (use up/down/left/right)", direction)
	}

	if selector != "" {
		return chromedp.Run(tabCtx,
			chromedp.Evaluate(
				fmt.Sprintf(`document.querySelector(%q)?.scrollBy(%d,%d)`, selector, x, y),
				nil,
			),
		)
	}
	return chromedp.Run(tabCtx,
		chromedp.Evaluate(fmt.Sprintf(`window.scrollBy(%d,%d)`, x, y), nil),
	)
}

// Evaluate executes a JavaScript expression and returns the result.
func (m *Manager) Evaluate(envID, sessionID, expression string, result any) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	return chromedp.Run(tabCtx, chromedp.Evaluate(expression, result))
}

// ScrollIntoView scrolls an element into view.
func (m *Manager) ScrollIntoView(envID, sessionID, selector string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	return chromedp.Run(tabCtx, chromedp.ScrollIntoView(selector))
}

// Drag drags sourceSelector onto targetSelector.
// Uses minimal Evaluate for reliable DataTransfer-based drag simulation.
func (m *Manager) Drag(envID, sessionID, sourceSel, targetSel string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	return chromedp.Run(tabCtx,
		chromedp.Evaluate(
			fmt.Sprintf(
				`((s,t)=>{var d=new DataTransfer();var e=document.querySelector(s);var f=document.querySelector(t);if(!e||!f)throw new Error("drag: element not found");e.scrollIntoView({block:"center"});e.dispatchEvent(new DragEvent("dragstart",{bubbles:true,dataTransfer:d}));f.scrollIntoView({block:"center"});f.dispatchEvent(new DragEvent("dragover",{bubbles:true,dataTransfer:d}));f.dispatchEvent(new DragEvent("drop",{bubbles:true,dataTransfer:d}));e.dispatchEvent(new DragEvent("dragend",{bubbles:true,dataTransfer:d}))})(%q,%q)`,
				sourceSel, targetSel,
			),
			nil,
		),
	)
}

// UploadFile sets files on a file input element.
func (m *Manager) UploadFile(envID, sessionID, selector string, files []string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	absFiles := make([]string, len(files))
	for i, f := range files {
		abs, err := filepath.Abs(f)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", f, err)
		}
		absFiles[i] = abs
	}
	return chromedp.Run(tabCtx,
		chromedp.SetUploadFiles(selector, absFiles),
	)
}

// ScreenshotOptions configures the screenshot.
type ScreenshotOptions struct {
	Path     string `json:"path,omitempty"`
	Dir      string `json:"screenshotDir,omitempty"`
	Format   string `json:"format,omitempty"`
	Quality  int    `json:"quality,omitempty"`
	Annotate bool   `json:"annotate,omitempty"`
	FullPage bool   `json:"fullPage,omitempty"`
	Clip     *struct {
		X, Y, Width, Height float64
	} `json:"clip,omitempty"`
}

// Screenshot captures a screenshot and saves it to disk.
func (m *Manager) Screenshot(envID, sessionID string, opts ScreenshotOptions) (string, error) {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return "", err
	}

	format := opts.Format
	if format == "" {
		format = "png"
	}

	var buf []byte

	if opts.FullPage && opts.Clip == nil {
		quality := opts.Quality
		if quality <= 0 {
			quality = 90
		}
		err = chromedp.Run(tabCtx, chromedp.FullScreenshot(&buf, quality))
	} else if opts.Clip != nil {
		err = chromedp.Run(tabCtx,
			chromedp.ActionFunc(func(ctx context.Context) error {
				pfmt := page.CaptureScreenshotFormatPng
				if format == "jpeg" {
					pfmt = page.CaptureScreenshotFormatJpeg
				}
				params := page.CaptureScreenshot().
					WithFormat(pfmt).
					WithClip(&page.Viewport{
						X:      opts.Clip.X,
						Y:      opts.Clip.Y,
						Width:  opts.Clip.Width,
						Height: opts.Clip.Height,
						Scale:  1,
					})
				if format == "jpeg" && opts.Quality > 0 {
					params = params.WithQuality(int64(opts.Quality))
				}
				b, err := params.Do(ctx)
				if err != nil {
					return err
				}
				buf = b
				return nil
			}),
		)
	} else {
		if format == "jpeg" {
			err = chromedp.Run(tabCtx,
				chromedp.ActionFunc(func(ctx context.Context) error {
					b, err := page.CaptureScreenshot().
						WithFormat(page.CaptureScreenshotFormatJpeg).
						WithQuality(int64(opts.Quality)).
						Do(ctx)
					if err != nil {
						return err
					}
					buf = b
					return nil
				}),
			)
		} else {
			err = chromedp.Run(tabCtx, chromedp.CaptureScreenshot(&buf))
		}
	}
	if err != nil {
		return "", fmt.Errorf("capture screenshot: %w", err)
	}

	// Determine output path
	outPath := opts.Path
	if outPath == "" {
		dir := opts.Dir
		if dir == "" {
			dir = filepath.Join(m.workDir, "screenshots")
		}
		os.MkdirAll(dir, 0755)
		outPath = filepath.Join(dir, fmt.Sprintf("screenshot.%s", format))
		for i := 1; fileExists(outPath); i++ {
			outPath = filepath.Join(dir, fmt.Sprintf("screenshot_%d.%s", i, format))
		}
	}

	if err := os.WriteFile(outPath, buf, 0644); err != nil {
		return "", fmt.Errorf("write screenshot: %w", err)
	}

	absPath, _ := filepath.Abs(outPath)
	return absPath, nil
}

// PDF generates a PDF of the current page.
func (m *Manager) PDF(envID, sessionID, path string) (string, error) {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return "", err
	}

	var buf []byte
	err = chromedp.Run(tabCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			b, _, err := page.PrintToPDF().
				WithPrintBackground(true).
				WithPreferCSSPageSize(true).
				Do(ctx)
			if err != nil {
				return err
			}
			buf = b
			return nil
		}),
	)
	if err != nil {
		return "", fmt.Errorf("print to PDF: %w", err)
	}

	outPath := path
	if outPath == "" {
		outPath = filepath.Join(m.workDir, "pdfs", "output.pdf")
	}
	if !strings.HasSuffix(outPath, ".pdf") {
		outPath += ".pdf"
	}
	os.MkdirAll(filepath.Dir(outPath), 0755)

	if err := os.WriteFile(outPath, buf, 0644); err != nil {
		return "", fmt.Errorf("write pdf: %w", err)
	}

	absPath, _ := filepath.Abs(outPath)
	return absPath, nil
}

// ---------- navigation helpers ----------

// Reload reloads the current page.
func (m *Manager) Reload(envID, sessionID string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	return chromedp.Run(tabCtx, chromedp.Reload())
}

// Back navigates back in browser history.
func (m *Manager) Back(envID, sessionID string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	return chromedp.Run(tabCtx, chromedp.NavigateBack())
}

// Forward navigates forward in browser history.
func (m *Manager) Forward(envID, sessionID string) error {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	return chromedp.Run(tabCtx, chromedp.NavigateForward())
}

// GetText returns the visible text content of the element matched by selector.
func (m *Manager) GetText(envID, sessionID, selector string) (string, error) {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return "", err
	}
	var text string
	err = chromedp.Run(tabCtx, chromedp.TextContent(selector, &text, chromedp.ByQuery))
	if err != nil {
		return "", fmt.Errorf("get text: %w", err)
	}
	return text, nil
}

// GetValue returns the value attribute of the element matched by selector
// (input, textarea, select, etc.).
func (m *Manager) GetValue(envID, sessionID, selector string) (string, error) {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return "", err
	}
	var value string
	err = chromedp.Run(tabCtx, chromedp.Value(selector, &value, chromedp.ByQuery))
	if err != nil {
		return "", fmt.Errorf("get value: %w", err)
	}
	return value, nil
}

// GetHTML returns the full HTML source of the current page.
func (m *Manager) GetHTML(envID, sessionID string) (string, error) {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return "", err
	}
	var html string
	err = chromedp.Run(tabCtx,
		chromedp.Evaluate(`document.documentElement.outerHTML`, &html),
	)
	if err != nil {
		return "", fmt.Errorf("get html: %w", err)
	}
	return html, nil
}

// UncheckRef unchecks a checkbox or radio button identified by a
// snapshot backendDOMNodeId reference.
func (m *Manager) UncheckRef(envID, sessionID, ref string) error {
	if envID == "" {
		return fmt.Errorf("envId is required")
	}
	if ref == "" {
		return fmt.Errorf("ref is required")
	}

	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return err
	}
	backendID, err := toBackendID(ref)
	if err != nil {
		return err
	}

	return chromedp.Run(tabCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			nodeID, err := resolveNodeID(ctx, backendID)
			if err != nil {
				return err
			}
			nid := cdp.NodeID(nodeID)

			// Check .checked property via ResolveNode → CallFunctionOn
			obj, err := cdpdom.ResolveNode().WithBackendNodeID(backendID).Do(ctx)
			if err != nil {
				return err
			}
			result, _, err := runtime.CallFunctionOn("function(){return this.checked}").
				WithObjectID(obj.ObjectID).
				Do(ctx)
			if err != nil {
				return err
			}
			var checked bool
			if result != nil && len(result.Value) > 0 {
				json.Unmarshal(result.Value, &checked)
			}

			if !checked {
				return nil // already unchecked
			}

			if err := chromedp.ScrollIntoView([]cdp.NodeID{nid}, chromedp.ByNodeID).Do(ctx); err != nil {
				return err
			}
			return chromedp.Click([]cdp.NodeID{nid}, chromedp.ByNodeID).Do(ctx)
		}),
	)
}

// ---------- agent-friendly tools (Tier 1 & 2) ----------

// RefResult is a matched element from a snapshot-based search.
type RefResult struct {
	Ref   int    `json:"ref"`
	Role  string `json:"role"`
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

// FindRefs takes a snapshot, searches by role/name/value, and returns matching refs.
func (m *Manager) FindRefs(envID, role, name, value string, limit int) ([]RefResult, error) {
	raw, err := m.Snapshot(envID, "")
	if err != nil {
		return nil, err
	}
	return filterRefs(raw, role, name, value, limit)
}

// SnapshotInteractive returns only interactive elements from the AX tree.
func (m *Manager) SnapshotInteractive(envID string) (json.RawMessage, error) {
	raw, err := m.Snapshot(envID, "")
	if err != nil {
		return nil, err
	}
	return filterInteractiveNodes(raw)
}

// PageState holds quick page metadata.
type PageState struct {
	Title      string `json:"title"`
	URL        string `json:"url"`
	ReadyState string `json:"readyState"`
}

// PageState returns the current page title, URL, and readyState.
func (m *Manager) PageState(envID string) (*PageState, error) {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return nil, err
	}
	var ps PageState
	err = chromedp.Run(tabCtx,
		chromedp.Evaluate(`document.title`, &ps.Title),
		chromedp.Evaluate(`location.href`, &ps.URL),
		chromedp.Evaluate(`document.readyState`, &ps.ReadyState),
	)
	if err != nil {
		return nil, fmt.Errorf("page state: %w", err)
	}
	return &ps, nil
}

// Exists checks whether an element matching the given role/name/value exists on the page.
func (m *Manager) Exists(envID, role, name, value string) (bool, error) {
	raw, err := m.Snapshot(envID, "")
	if err != nil {
		return false, err
	}
	_, err = findNodeByFingerprintJSON(raw, role, name, value)
	if err != nil {
		return false, nil // not found → exists=false (no error)
	}
	return true, nil
}

// Wait polls until an element (by text, role+name, or selector) appears.
// Returns error if timeout is exceeded.
func (m *Manager) Wait(envID, text, role, name, selector string, timeoutMs int) error {
	if timeoutMs <= 0 {
		timeoutMs = 5000
	}
	if text == "" && (role == "" || name == "") && selector == "" {
		return fmt.Errorf("at least one of text, role+name, or selector is required")
	}

	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)

	for time.Now().Before(deadline) {
		// Try snapshot-based matching for text or role+name
		if text != "" || (role != "" && name != "") {
			raw, err := m.Snapshot(envID, "")
			if err == nil {
				// Text search
				if text != "" {
					if refs, _ := filterRefs(raw, "", "", "", 0); len(refs) > 0 {
						for _, r := range refs {
							if strings.Contains(strings.ToLower(r.Name), strings.ToLower(text)) ||
								strings.Contains(strings.ToLower(r.Value), strings.ToLower(text)) {
								return nil
							}
						}
					}
				}
				// Role+name search
				if role != "" && name != "" {
					if _, err := findNodeByFingerprintJSON(raw, role, name, ""); err == nil {
						return nil
					}
				}
			}
		}

		// Try selector-based matching
		if selector != "" {
			tabCtx, err := m.ensureTab(envID)
			if err == nil {
				var nodeIDs []cdp.NodeID
				err := chromedp.Run(tabCtx,
					chromedp.ActionFunc(func(ctx context.Context) error {
						return chromedp.NodeIDs(selector, &nodeIDs, chromedp.ByQuery).Do(ctx)
					}),
				)
				if err == nil && len(nodeIDs) > 0 {
					return nil
				}
			}
		}

		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("wait timed out after %dms", timeoutMs)
}

// WaitForNavigation polls until the active tab's readyState is "complete".
func (m *Manager) WaitForNavigation(envID string, timeoutMs int) error {
	if timeoutMs <= 0 {
		timeoutMs = 30000
	}
	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	for time.Now().Before(deadline) {
		ps, err := m.PageState(envID)
		if err == nil && ps != nil && ps.ReadyState == "complete" {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("WaitForNavigation timed out after %dms", timeoutMs)
}

// WaitForSelector polls until at least one element matching selector exists.
func (m *Manager) WaitForSelector(envID, selector string, timeoutMs int) error {
	if selector == "" {
		return fmt.Errorf("selector is required")
	}
	if timeoutMs <= 0 {
		timeoutMs = 5000
	}
	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	for time.Now().Before(deadline) {
		tabCtx, err := m.ensureTab(envID)
		if err == nil {
			var nodeIDs []cdp.NodeID
			err := chromedp.Run(tabCtx,
				chromedp.ActionFunc(func(ctx context.Context) error {
					return chromedp.NodeIDs(selector, &nodeIDs, chromedp.ByQuery).Do(ctx)
				}),
			)
			if err == nil && len(nodeIDs) > 0 {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("WaitForSelector(%q) timed out after %dms", selector, timeoutMs)
}

// DialogResult holds the result of a dialog interaction.
type DialogResult struct {
	Action  string `json:"action"`
	Message string `json:"message,omitempty"`
}

// Dialog handles a JavaScript dialog (alert/confirm/prompt).
// action: "accept" or "dismiss".
func (m *Manager) Dialog(envID, action string) (*DialogResult, error) {
	tabCtx, err := m.ensureTab(envID)
	if err != nil {
		return nil, err
	}

	result := &DialogResult{Action: action}
	accept := action == "accept"

	// Read dialog message from page-side capture.
	// The test page (or custom page) should override alert/confirm/prompt
	// to set window.__wbDialogMsg and window.__wbDialogType before calling
	// the browser_dialog tool. This avoids CDP event-timing issues.
	var msg string
	var dlgType string
	_ = chromedp.Run(tabCtx,
		chromedp.Evaluate(`window.__wbDialogMsg||''`, &msg),
		chromedp.Evaluate(`window.__wbDialogType||''`, &dlgType),
	)
	if msg != "" {
		// Clear captured message after reading.
		chromedp.Run(tabCtx, chromedp.Evaluate(`window.__wbDialogMsg=null;window.__wbDialogType=null`, nil))
		result.Message = msg
		return result, nil
	}

	// Fallback: try native CDP dialog handling (real alert/confirm/prompt).
	err = chromedp.Run(tabCtx, page.HandleJavaScriptDialog(accept))
	if err == nil {
		return result, nil
	}

	return nil, fmt.Errorf("dialog handle: no dialog detected")
}

// FillFormResult holds per-field results.
type FillFormResult struct {
	Filled map[string]bool `json:"filled"`
	Errors []string        `json:"errors,omitempty"`
}

// FillForm fills multiple form fields at once using CSS selectors.
// If submitSelector is non-empty, clicks it after filling.
func (m *Manager) FillForm(envID string, fields map[string]string, submitSelector string) (*FillFormResult, error) {
	result := &FillFormResult{Filled: make(map[string]bool)}

	for sel, text := range fields {
		if err := m.Fill(envID, "", sel, text); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", sel, err))
			continue
		}
		result.Filled[sel] = true
	}

	if submitSelector != "" {
		if err := m.Click(envID, "", submitSelector); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("submit: %v", err))
		}
	}

	return result, nil
}

// ---------- AX tree helpers (used by FindRefs, Exists, Wait) ----------

// axNode mirrors a single node in the CDP Accessibility.getFullAXTree response.
type axNode struct {
	NodeID           string   `json:"nodeId"`
	BackendDOMNodeID int      `json:"backendDOMNodeId"`
	Ignored          bool     `json:"ignored"`
	Role             axValue  `json:"role"`
	Name             axValue  `json:"name"`
	Value            axValue  `json:"value"`
	ChildIDs         []string `json:"childIds"`
}

type axValue struct {
	Value any    `json:"value"`
	Type  string `json:"type"`
}

type axTree struct {
	Nodes []axNode `json:"nodes"`
}

func axStr(v axValue) string {
	if s, ok := v.Value.(string); ok {
		return s
	}
	return ""
}

// filterRefs parses a snapshot JSON and returns matching refs by role/name/value.
func filterRefs(snapshot json.RawMessage, role, name, value string, limit int) ([]RefResult, error) {
	var tree axTree
	if err := json.Unmarshal(snapshot, &tree); err != nil {
		return nil, fmt.Errorf("snapshot parse: %w", err)
	}

	var out []RefResult
	for i := range tree.Nodes {
		n := &tree.Nodes[i]
		if n.Ignored {
			continue
		}
		r := axStr(n.Role)
		nm := axStr(n.Name)
		v := axStr(n.Value)

		if role != "" && r != role {
			continue
		}
		if name != "" && nm != name {
			continue
		}
		if value != "" && v != value {
			continue
		}

		out = append(out, RefResult{
			Ref:   n.BackendDOMNodeID,
			Role:  r,
			Name:  nm,
			Value: v,
		})

		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// findNodeByFingerprintJSON searches a snapshot JSON for a node matching role+name+value.
func findNodeByFingerprintJSON(snapshot json.RawMessage, role, name, value string) (*axNode, error) {
	var tree axTree
	if err := json.Unmarshal(snapshot, &tree); err != nil {
		return nil, err
	}
	for i := range tree.Nodes {
		n := &tree.Nodes[i]
		if n.Ignored {
			continue
		}
		if axStr(n.Role) != role || axStr(n.Name) != name {
			continue
		}
		if value != "" && axStr(n.Value) != value {
			continue
		}
		return n, nil
	}
	return nil, fmt.Errorf("not found: role=%q name=%q", role, name)
}

// filterInteractiveNodes returns only interactive nodes from the AX tree.
func filterInteractiveNodes(snapshot json.RawMessage) (json.RawMessage, error) {
	var tree axTree
	if err := json.Unmarshal(snapshot, &tree); err != nil {
		return nil, fmt.Errorf("snapshot parse: %w", err)
	}

	interactiveRoles := map[string]bool{
		"button": true, "link": true, "textbox": true, "searchbox": true,
		"combobox": true, "listbox": true, "checkbox": true, "radio": true,
		"menuitem": true, "tab": true, "switch": true, "slider": true,
		"spinbutton": true, "heading": true, "image": true,
	}

	filtered := make([]axNode, 0, len(tree.Nodes)/3)
	for i := range tree.Nodes {
		n := &tree.Nodes[i]
		if n.Ignored {
			continue
		}
		if interactiveRoles[axStr(n.Role)] {
			filtered = append(filtered, *n)
		}
	}

	out, err := json.Marshal(axTree{Nodes: filtered})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ---------- helpers ----------

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

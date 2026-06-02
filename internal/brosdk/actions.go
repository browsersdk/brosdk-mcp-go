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

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	cdpdom "github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// ---------- chromedp context management ----------

// browserTab tracks chromedp resources for one browser environment.
type browserTab struct {
	allocCtx    context.Context    // remote allocator context (for creating tabs)
	allocCancel context.CancelFunc // cancels allocator
	tabCtx      context.Context    // current active tab
	tabCancel   context.CancelFunc // cancels active tab
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
		if bt.allocCancel != nil {
			bt.allocCancel()
		}
		if bt.tabCancel != nil {
			bt.tabCancel()
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
	}

	m.mu.Lock()
	if m.browsers == nil {
		m.browsers = make(map[string]*browserTab)
	}
	m.browsers[envID] = bt
	m.mu.Unlock()

	return bt, nil
}

// ensureTab returns the active tab context for envID.
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

// CloseTab tears down the active tab for envID (keeps browser allocator).
func (m *Manager) CloseTab(envID string) {
	m.mu.Lock()
	bt := m.browsers[envID]
	if bt != nil {
		if bt.tabCancel != nil {
			bt.tabCancel()
		}
		bt.tabCtx = nil
		bt.tabCancel = nil
	}
	m.mu.Unlock()
}

// CloseBrowser tears down all chromedp resources for envID.
func (m *Manager) CloseBrowser(envID string) {
	m.mu.Lock()
	bt := m.browsers[envID]
	if bt != nil {
		if bt.tabCancel != nil {
			bt.tabCancel()
		}
		if bt.allocCancel != nil {
			bt.allocCancel()
		}
		delete(m.browsers, envID)
	}
	delete(m.debugPorts, envID)
	m.mu.Unlock()

	// Also close any pooled CDP WebSocket connection.
	RemoveCDPConn(envID)
}

// ResetEnv tears down chromedp resources for envID when the browser is
// being re-opened (e.g. browser_open after a prior close or crash).
// Unlike CloseBrowser this does NOT close CDP conns — the new browser
// will get a fresh WebSocket. It only tears down the allocator/tab.
func (m *Manager) ResetEnv(envID string) {
	m.mu.Lock()
	bt := m.browsers[envID]
	if bt != nil {
		if bt.tabCancel != nil {
			bt.tabCancel()
		}
		if bt.allocCancel != nil {
			bt.allocCancel()
		}
		delete(m.browsers, envID)
	}
	m.mu.Unlock()
}

// CloseAllBrowsers shuts down all chromedp connections.
func (m *Manager) CloseAllBrowsers() {
	m.mu.Lock()
	for envID, bt := range m.browsers {
		if bt.tabCancel != nil {
			bt.tabCancel()
		}
		if bt.allocCancel != nil {
			bt.allocCancel()
		}
		delete(m.browsers, envID)
	}
	m.mu.Unlock()
}

// ---------- Session tracking (kept for backward compat) ----------

// SetActiveSession is a no-op stub — chromedp manages sessions internally.
func (m *Manager) SetActiveSession(envID, sessionID string) {}

// GetActiveSession returns empty string — chromedp manages sessions internally.
func (m *Manager) GetActiveSession(envID string) string { return "" }

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
			b, err := json.Marshal(tree)
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
	return chromedp.Run(tabCtx,
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
	return chromedp.Run(tabCtx,
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
	return chromedp.Run(tabCtx,
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
	return chromedp.Run(tabCtx,
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
	return chromedp.Run(tabCtx,
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
	Path      string `json:"path,omitempty"`
	Dir       string `json:"screenshotDir,omitempty"`
	Format    string `json:"format,omitempty"`
	Quality   int    `json:"quality,omitempty"`
	Annotate  bool   `json:"annotate,omitempty"`
	FullPage  bool   `json:"fullPage,omitempty"`
	Clip      *struct {
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
			dir = "."
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

	return outPath, nil
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
		outPath = "output.pdf"
	}
	if !strings.HasSuffix(outPath, ".pdf") {
		outPath += ".pdf"
	}

	if err := os.WriteFile(outPath, buf, 0644); err != nil {
		return "", fmt.Errorf("write pdf: %w", err)
	}

	return outPath, nil
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

// ---------- helpers ----------

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

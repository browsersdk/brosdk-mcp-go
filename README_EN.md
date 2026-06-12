# brosdk-mcp-go

English | [简体中文](README.md)

Wrap the BroSDK fingerprint-browser SDK as an [MCP (Model Context Protocol)](https://modelcontextprotocol.io/) SSE service,
so AI Agents (Claude, CodeBuddy, etc.) can directly control fingerprint browser environments.

## Doc Navigation

> AI Agent entry point: start here. Quick reference to all project docs.

| Document | Purpose | When to Read |
|----------|---------|-------------|
| **README.md** (this file) | Project overview, API reference, config, usage | First time |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Architecture, tech stack, design decisions | Understanding internals / contributing |
| [docs/tools-reference.md](docs/tools-reference.md) | Full 72-tool API reference | Looking up specific tool params/returns |

## Feature Overview

- Expose all BroSDK capabilities through 72 MCP Tools: SDK lifecycle, browser control, high-level browser actions, environment CRUD, **browser record-replay**
- **Cookie Storage Callback**: SDK-level cookie interception via `mgr.OnCookies()` — cookie changes broadcast as SSE `cookies-event`
- High-level browser operations powered by [chromedp](https://github.com/chromedp/chromedp) — click, type, fill, screenshot, PDF, no raw CDP required
- Retain `browser_command` transparent CDP proxy for advanced/custom DevTools scenarios
- Async tools deliver results via SSE events, with support for long-wait operations
- Dual config files (`config.json` / `config.local.json`) + environment variable overrides, auto-init SDK at startup
- Cross-platform: Windows / macOS native support, auto-download library on first run

## Architecture

```
Agent / MCP Client
        │  GET /sse   (SSE stream)
        │  POST /message  (JSON-RPC 2.0)
        ▼
┌────────────────────────────────────┐
│   MCP SSE Server (net/http)       │
│   internal/mcp/server.go          │
└───────────────┬───────────────────┘
                │  tools.Handler()
        ┌───────▼──────────┐
        │  Tool Layer      │
        │  internal/tools/ │
        └───────┬──────────┘
                │  brosdk.Manager
        ┌───────▼──────────────────┐
        │  Browser Actions Layer   │
        │  actions.go (chromedp)   │
        │  - Click, Type, Fill     │
        │  - Snapshot, Screenshot  │
        │  - 37 high-level ops     │
        └───────┬──────────────────┘
                │  WebSocket (CDP)
        ┌───────▼──────────┐
        │  Native Layer    │
        │  syscall / CGo   │
        │  brosdk.dll/.dylib│
        └───────┬──────────┘
                │  WebSocket (browser control)
        ┌───────▼──────────┐
        │  CDP Proxy       │
        │  cdp.go          │
        │  (DevTools raw)  │
        └──────────────────┘
```

## Prerequisites

- **Windows** x64 / **macOS** arm64
- Go 1.26+
- brosdk native library — **auto-downloaded from GitHub Releases** on first run

## Build

```bash
# Windows / macOS — production build
go build -o brosdk-mcp .
```

Build constraints: `//go:build windows` / `//go:build darwin` / `//go:build !windows && !darwin`.

## Configuration

On startup, the server reads `config.local.json` → `config.json` in order (first match wins).
If a config file is found, `sdk_init` is called automatically.

### Config File Format

```json
{
    "apiKey":    "your-customer-id",
    "userSig":   "optional-precomputed-sig",
    "workDir":   "D:/brosdk-data",
    "port":      5811,
    "sdkApiUrl": "https://api.brosdk.com",
    "debug":     false
}
```

| Field      | Required | Default                     | Description                                         |
|------------|----------|-----------------------------|-----------------------------------------------------|
| `apiKey`   | Yes\*    | —                           | Customer ID; auto-fetches `userSig` if absent       |
| `userSig`  | No       | —                           | Pre-computed signature (takes precedence over apiKey)|
| `workDir`  | No       | `./brosdk`                  | SDK working directory (auto-created)                |
| `port`     | No       | `5811`                      | SDK browser control port                            |
| `debug`    | No       | `false`                     | Enable SDK debug logging                            |

\* Either `apiKey` or `userSig` must be present.

### Environment Variable Overrides

All config fields can be overridden via `BROSDK_*` environment variables. Priority order:

```
CLI flag > Environment variable > Config file > Default
```

| Environment Variable   | Maps to       | Example                         |
|-----------------------|---------------|---------------------------------|
| `BROSDK_API_KEY`      | `apiKey`      | `BROSDK_API_KEY=your-id`        |
| `BROSDK_USER_SIG`     | `userSig`     | `BROSDK_USER_SIG=sig-value`     |
| `BROSDK_WORK_DIR`     | `workDir`     | `BROSDK_WORK_DIR=/data/sdk`     |
| `BROSDK_PORT`         | `port`        | `BROSDK_PORT=5811`              |
| `BROSDK_SDK_API_URL`  | `sdkApiUrl`   | `BROSDK_SDK_API_URL=https://`   |
| `BROSDK_DEBUG`        | `debug`       | `BROSDK_DEBUG=true`             |
| `BROSDK_LIB`          | `-lib` flag   | `BROSDK_LIB=./libs/sdk.dll`     |
| `BROSDK_ADDR`         | `-addr` flag  | `BROSDK_ADDR=:9000`             |

You can run without a config file using only environment variables:

```bash
BROSDK_API_KEY=your-id BROSDK_ADDR=:8765 brosdk-mcp
```

### Local Dev Override

Copy `config.json` → `config.local.json` with different credentials; the
local file is never checked into version control and takes precedence.

## Run

```bash
# First run — auto-download platform library from GitHub Releases
brosdk-mcp -addr :8765

# Explicit library path
brosdk-mcp -lib ./libs/darwin-arm64/libbrosdk.dylib -addr :8765
```

### CLI Flags

| Flag   | Default                   | Description                    |
|--------|---------------------------|--------------------------------|
| `-lib` | (auto-download)           | Path to brosdk native library  |
| `-addr`| `:8765`                   | HTTP listen address            |

### Library Auto-Download

When `-lib` is omitted, the lookup order is:
1. `./brosdk.dll` (Windows) or `./libbrosdk.dylib` (macOS)
2. `libs/<platform>/` local copy
3. Auto-download from [GitHub Releases](https://github.com/browsersdk/brosdk/releases) (latest version)

Real-time progress is printed to console during download.

## Endpoints

| Endpoint       | Method          | Description                         |
|----------------|-----------------|-------------------------------------|
| `/inspector`   | GET             | Built-in MCP Inspector Web UI       |
| `/mcp`         | POST/GET/DELETE | Streamable HTTP transport (MCP 2025-03-26) |
| `/sse`         | GET             | SSE stream (MCP 2024-11-05, legacy) |
| `/message`     | POST            | JSON-RPC 2.0 request endpoint (legacy) |
| `/health`      | GET             | Health check (returns `200 OK`)     |

## MCP Tools (72)

### SDK Info (3)

| Tool               | Sync/Async | Parameters                                 | Description                                     |
|--------------------|-----------|--------------------------------------------|-------------------------------------------------|
| `sdk_info`         | Sync      | —                                          | Get SDK runtime information (version, status, config) |
| `sdk_token_update` | Async     | `userSig` (required)                       | Refresh userSig; result via `sdk-event` SSE     |
| `sdk_get_user_sig` | Sync      | `apiKey` (required), `duration`            | Obtain userSig via apiKey (can call before sdk_init) |

### Browser Control (6)

| Tool              | Sync/Async | Parameters                                  | Description                                     |
|-------------------|-----------|---------------------------------------------|-------------------------------------------------|
| `browser_install` | Async     | `channel`                                   | Install/update browser core; progress via `sdk-event` SSE |
| `browser_info`    | Sync      | —                                           | List currently running browser environments     |
| `browser_open`    | Async     | `envId` (required), `urls`, `args`          | Open browser environment; `browser-open-success` = CDP ready; auto-sets as active env |
| `browser_close`   | Async     | `envId` (required)                          | Close browser environment                       |
| `browser_select`   | Sync      | `envId` (required)                          | **Set active environment**; subsequent Browser Actions use it when `envId` is omitted |
| `browser_command` | Sync      | `envId`, `method` (required), `params`, `sessionId` | Send raw CDP command to browser          |

### Browser Actions (50)

> **`envId` parameter**: Now **optional** for all Browser Actions.
> Use `browser_select` to set the active environment; subsequent actions can omit `envId`.
> Explicit `envId` still works and overrides the active environment.

#### Page Navigation

| Tool              | Parameters                                   | Description                                      |
|-------------------|----------------------------------------------|--------------------------------------------------|
| `browser_navigate`| `envId`, `url` (required)                    | Open URL; returns `{targetId, sessionId}`        |
| `browser_reload`  | `envId`, `sessionId`               | Reload the current page                           |
| `browser_back`    | `envId`, `sessionId`               | Navigate back in browser history                  |
| `browser_forward` | `envId`, `sessionId`               | Navigate forward in browser history               |
| `browser_snapshot`| `envId`, `sessionId`              | Capture accessibility tree with `backendDOMNodeId` refs |

#### Tab Management

| Tool                 | Parameters                                   | Description                                      |
|----------------------|----------------------------------------------|--------------------------------------------------|
| `browser_new_tab`    | `envId`                                      | Create a new blank tab; returns `{tabId}`. New tab becomes active. |
| `browser_close_tab`  | `envId`, `tabId` (required)                  | Close a tab by ID (`"__active__"` for current active tab) |
| `browser_list_tabs`  | `envId`                                      | List all open tabs: `[{tabId, title, url, isActive}]` |

#### Page Content

| Tool                 | Parameters                                   | Description                                      |
|----------------------|----------------------------------------------|--------------------------------------------------|
| `browser_get_html`   | `envId`                                      | Return full HTML source of the current page      |

#### Cookie Management

| Tool                    | Parameters                                   | Description                                      |
|-------------------------|----------------------------------------------|--------------------------------------------------|
| `browser_get_cookies`   | `envId`, `urls`                              | Get cookies for current tab; optionally filter by URL |
| `browser_set_cookies`   | `envId`, `cookies` (required)                | Set cookies; each supports `{name, value, url, domain, path, secure, httpOnly}` |

#### Mouse Actions

| Tool               | Parameters                                   | Description                 |
|--------------------|----------------------------------------------|-----------------------------|
| `browser_click`    | `envId`, `selector` (required), `sessionId`   | Click by CSS selector       |
| `browser_click_ref`| `envId`, `ref` (required), `sessionId`        | Click by snapshot ref       |
| `browser_dblclick` | `envId`, `selector` (required), `sessionId`   | Double-click by CSS selector |
| `browser_hover`    | `envId`, `selector` (required), `sessionId`   | Hover by CSS selector       |
| `browser_hover_ref`| `envId`, `ref` (required), `sessionId`        | Hover by snapshot ref       |

#### Keyboard / Input

| Tool                        | Parameters                                       | Description                       |
|-----------------------------|--------------------------------------------------|-----------------------------------|
| `browser_type`              | `envId`, `selector`, `text` (required), `sessionId` | Type by CSS selector (append)    |
| `browser_type_ref`          | `envId`, `ref`, `text` (required), `sessionId`      | Type by snapshot ref (append)    |
| `browser_fill`              | `envId`, `selector`, `text` (required), `sessionId` | Clear + type by CSS selector     |
| `browser_fill_ref`          | `envId`, `ref`, `text` (required), `sessionId`      | Clear + type by snapshot ref     |
| `browser_press_key`         | `envId`, `key` (required), `sessionId`              | Press key (Enter, Escape, Tab…)  |
| `browser_keyboard_type`     | `envId`, `text` (required), `sessionId`             | Type char by char                |
| `browser_insert_text` | `envId`, `text` (required), `sessionId`          | Insert text via Input.insertText |
| `browser_key_down`          | `envId`, `key` (required), `sessionId`              | keyDown event                    |
| `browser_key_up`            | `envId`, `key` (required), `sessionId`              | keyUp event                      |

#### Focus / Selection

| Tool                   | Parameters                                       | Description                 |
|------------------------|--------------------------------------------------|-----------------------------|
| `browser_focus`        | `envId`, `selector` (required), `sessionId`       | Focus by CSS selector       |
| `browser_focus_ref`    | `envId`, `ref` (required), `sessionId`            | Focus by snapshot ref       |
| `browser_select_option`| `envId`, `selector`, `value` (required), `sessionId` | Set `<select>` value    |
| `browser_select_option_ref`| `envId`, `ref`, `value` (required), `sessionId` | Set select value by snapshot ref |
| `browser_check`        | `envId`, `selector` (required), `sessionId`       | Check checkbox/radio        |
| `browser_check_ref`    | `envId`, `ref` (required), `sessionId`            | Check by snapshot ref       |
| `browser_uncheck`      | `envId`, `selector` (required), `sessionId`       | Uncheck checkbox            |
| `browser_uncheck_ref`  | `envId`, `ref` (required), `sessionId`            | Uncheck by snapshot ref    |

#### Scroll / Drag

| Tool                    | Parameters                                                  | Description            |
|-------------------------|-------------------------------------------------------------|------------------------|
| `browser_scroll`        | `envId`, `direction` (required), `px`, `selector`, `sessionId` | Scroll page or element|
| `browser_scroll_into_view` | `envId`, `selector` (required), `sessionId`            | Scroll element into view|
| `browser_drag`          | `envId`, `sourceSelector`, `targetSelector` (required), `sessionId` | Drag operation    |

#### File / Screenshot / PDF

| Tool                | Parameters                                                          | Description                  |
|---------------------|--------------------------------------------------------------------|------------------------------|
| `browser_upload_file`| `envId`, `selector`, `files` (required), `sessionId`              | Upload files to file input   |
| `browser_screenshot` | `envId`, `path`, `dir`, `screenshotDir`, `format`, `quality`, `fullPage`, `sessionId` | Screenshot, defaults to workDir/screenshots/, returns absolute path. `screenshotDir` is a backward-compatible alias for `dir` |
| `browser_pdf`       | `envId`, `path`, `sessionId`                             | Generate PDF, defaults to workDir/pdfs/output.pdf, returns absolute path |

#### Text Finding / Scripting

| Tool                    | Parameters                                       | Description                               |
|-------------------------|--------------------------------------------------|-------------------------------------------|
| `browser_find_click_text`| `envId`, `text` (required), `sessionId`         | Find visible text and click               |
| `browser_get_text`    | `envId`, `selector` (required), `sessionId`       | Return visible text content of element      |
| `browser_get_value`   | `envId`, `selector` (required), `sessionId`       | Return value attribute of input element     |
| `browser_evaluate`      | `envId`, `expression` (required), `sessionId`    | Execute JavaScript and return result      |

#### Agent-Friendly Tools

| Tool                    | Parameters                                       | Description                               |
|-------------------------|--------------------------------------------------|-------------------------------------------|
| `browser_find_ref`      | `envId`, `role`, `name`, `value`, `limit` | Search AX tree for elements and return refs (80% smaller than full snapshot) |
| `browser_wait`          | `envId`, `text`, `role`, `name`, `selector`, `timeout`, `waitFor`, `delay` | Wait for a condition; `waitFor` supports `navigation` (readyState), `selector` (CSS appeared), `text` (text visible), `time` (fixed ms) |
| `browser_page_state`    | `envId`, `sessionId`                   | Return `{readyState, title, url}` to check page load state |
| `browser_exists`        | `envId`, `role`, `name`, `value`, `selector`, `sessionId` | Quick boolean check if element exists |
| `browser_dialog`        | `envId`, `action`, `sessionId` | Read/accept/dismiss dialogs (alert/confirm/prompt) |
| `browser_fill_form`     | `envId`, `fields` (required), `submitSelector` | Batch fill form fields; `fields` is an object mapping CSS selectors to values |

### Environment Management (5)

| Tool          | Sync/Async | Parameters                                  | Description                        |
|---------------|-----------|---------------------------------------------|------------------------------------|
| `env_create`  | Sync      | `name`, `os`, `body`                        | Create fingerprint browser environment |
| `env_page`    | Sync      | `page`, `pageSize`, `body`                  | Query/list environments (paginated) |
| `env_update`  | Sync      | `envId` (required), `body`                  | Update environment config          |
| `env_destroy` | Sync      | `envId` (required)                          | Permanently delete environment     |
| `env_getinfo` | Sync      | `envId` (required)                          | Get single environment detail      |

### Record & Replay (9)

| Tool           | Sync/Async | Parameters                                       | Description                            |
|----------------|----------|--------------------------------------------|---------------------------------|
| `record_start` | Sync     | —                                          | Start recording; subsequent operations are auto-captured |
| `record_stop`  | Sync     | `name` (required), `description`              | Stop recording and auto-save to `scenes/{name}.json`; existing scenes with the same name are overwritten |
| `record_status`| Sync     | —                                          | Check current recording status |
| `scene_list`   | Sync     | —                                          | List all saved scenes |
| `scene_get`    | Sync     | `name` (required)                              | View scene details (step JSON) |
| `scene_update` | Sync     | `name` (required), `scene` (required)             | Edit scene steps and metadata |
| `scene_delete` | Sync     | `name` (required)                              | Delete a scene file |
| `scene_replay` | Sync     | `name` (required), `envId`, `variables`, `stopOnError`, `stepDelay`, `applyHumanDelay` | Load scene and replay step by step. WaitFor guards ensure each step completes before next. Supports `{{variable}}` substitution |
| `scene_save`   | Sync     | `name` (required), `scene` (required)             | Manually save a complete scene object (usually auto-saved by `record_stop`) |

#### Recording Replay Workflow

```
1. record_start()                                    → Start recording
2. browser_navigate/browser_click/browser_fill...    → Operations auto-captured (WaitFor guards auto-inferred)
3. record_stop({name:"login_flow"})                  → Auto-save to workDir/scenes/login_flow.json

# Method A: explicit envId (backward compatible)
4a. scene_replay({name:"login_flow", envId:"env-2",
      variables:{"username":"alice","password":"s3cret"}})

# Method B: set active env once, omit envId after
4b. browser_select({envId:"env-2"})                → Set as active environment
    scene_replay({name:"login_flow",                → envId omitted, uses active env
      variables:{"username":"alice","password":"s3cret"}})

# Fast replay (skip human delay)
5. scene_replay({name:"login_flow",
      applyHumanDelay: false, stepDelay: 100})
```

- `envId`/`sessionId` are auto-stripped during recording; injected by caller at replay
- Steps support `{{variable}}` placeholders for dynamic substitution
- Max 200 steps; default 500ms inter-step delay
- Scene names may contain only letters, numbers, `.`, `_`, and `-`; `record_stop` overwrites existing scenes with the same name so agents can iterate recordings easily
- Scene files are JSON, human-readable, git-friendly
- **WaitFor guard**: navigate/click steps auto-infer completion condition (`readyState:complete`), replay blocks instead of blind wait
- Failed WaitFor guards are returned as a structured `guardWarn` field on the step result without marking the successful tool step as failed
- **HumanDelay**: inter-step human pauses captured during recording (capped at 3s), replay enables by default (`applyHumanDelay: true`), can disable for speed

## Ref-Based Targeting

Some browser actions support two targeting modes: CSS selector (`browser_click`) and accessibility ref (`browser_click_ref`).

**Workflow**:

```
1. browser_snapshot  →  Capture page accessibility tree (AX Tree)
   Returns: {"nodes": [{"name": {"value": "Target Button"}, "backendDOMNodeId": 42, ...}]}

2. LLM selects target element's backendDOMNodeId from snapshot

3. browser_click_ref(envId, ref="42")  →  Operate on DOM node directly
```

**Use Cases**:
- 🔴 Complex DOM structures — CSS selectors are long and error-prone
- 🟡 Multiple operations on the same page — snapshot cost is amortized
- 🟢 AI needs to understand page structure — snapshot itself provides context

**Tools with `_ref` variants** (8):

| Base Tool             | Ref Variant               | Description         |
|-----------------------|--------------------------|---------------------|
| `browser_click`       | `browser_click_ref`       | Click               |
| `browser_type`        | `browser_type_ref`        | Type (append)       |
| `browser_fill`        | `browser_fill_ref`        | Clear + type        |
| `browser_hover`       | `browser_hover_ref`       | Hover               |
| `browser_focus`       | `browser_focus_ref`       | Focus               |
| `browser_select_option`| `browser_select_option_ref`| Set select value   |
| `browser_check`       | `browser_check_ref`       | Check checkbox/radio|
| `browser_uncheck`     | `browser_uncheck_ref`     | Uncheck             |

## browser_command — CDP Command Reference

`browser_command` is a transparent CDP proxy over WebSocket. **Prefer the high-level `browser_*` tools above for everyday operations.**

### How It Works

```
browser_open → browser-open-success → extract remoteDebuggingPort
    → /json/version → browser WebSocket URL
    → WebSocket connection pool → CDP request/response forwarding
```

| Step | Description |
|------|-------------|
| 1 | `browser_open` injects `--remote-debugging-port=0`; browser picks a random free port |
| 2 | SSE `browser-open-success` event carries `remoteDebuggingPort` |
| 3 | CDP proxy fetches `http://127.0.0.1:<port>/json/version` to get WebSocket URL |
| 4 | Establishes a WebSocket connection pool (cached per envId) |
| 5 | Subsequent `browser_command` calls reuse the established connection |

### Parameters

| Parameter   | Required | Type   | Description                                                       |
|-------------|----------|--------|-------------------------------------------------------------------|
| `envId`     | Yes      | string | Target browser environment ID                                     |
| `method`    | Yes      | string | CDP method name, e.g. `Page.navigate`                             |
| `params`    | No       | object | CDP command parameters                                            |
| `sessionId` | No       | string | CDP session ID                                                    |
| `body`      | No       | string | Raw JSON body; overrides other fields when provided               |

`body` example: `{"method":"Runtime.evaluate","params":{"expression":"document.title"},"sessionId":"..."}`.

### High-Level Tools vs CDP Commands

| Scenario            | Recommended                                      |
|---------------------|--------------------------------------------------|
| Navigate to URL     | `browser_navigate`                               |
| Click element       | `browser_click` / `browser_click_ref`            |
| Fill input          | `browser_fill` / `browser_fill_ref`              |
| Take screenshot     | `browser_screenshot`                             |
| Custom CDP command  | `browser_command`                                |
| Execute JS          | `browser_evaluate`                               |

## Async Pattern

Async tools (`browser_open`, `browser_close`, `browser_install`, `sdk_token_update`)
return a `reqId` immediately:

```json
{"reqId": 42, "envId": "abc123"}
```

The final result is delivered as an SSE event:

```
event: sdk-event
data: {"code":100,"data":"{\"type\":\"browser-open-success\",\"reqId\":42,...}"}
```

### SSE Event Types

| Event Type                | code | Meaning                    |
|---------------------------|------|----------------------------|
| `browser-open-success`    | 0    | Browser opened, CDP ready  |
| `browser-close-success`   | 0    | Browser closed             |
| `token-update-success`    | 0    | Token refreshed            |
| `install-success`         | 0    | Core installation complete |
| `cookies-event`           | 0    | SDK intercepted cookie storage/update |
| Other                     | ≠0   | Error or warning           |

SSE connections receive a `: ping` heartbeat every 15 seconds.

## Claude Desktop / CodeBuddy Config Example

```json
{
  "mcpServers": {
    "brosdk": {
      "url": "http://localhost:8765/sse"
    }
  }
}
```

## E2E Tests

Test files are split into 11 files by feature area for easy focused runs:

| File | Test Function | Coverage |
|------|-------------|----------|
| `e2e_test.go` | — | Shared test infrastructure (JSON-RPC client, SSE reader, fixture) |
| `e2e_basic_test.go` | `TestE2E_AllTools` | SDK init → env mgmt → browser open/close → CDP commands |
| | `TestE2E_CDPFormInteraction` | navigate → fill → click → evaluate full form flow |
| `e2e_snapshot_test.go` | `TestE2E_SnapshotClickRef` | snapshot → extract ref → click_ref → evaluate verify |
| `e2e_form_test.go` | `TestE2E_FormElements` | 9 form element ops: focus/type/fill/select/check + ref variants |
| `e2e_keyboard_test.go` | `TestE2E_KeyboardInteraction` | press_key/keyboard_type/insert_text/key_down/key_up |
| `e2e_mouse_test.go` | `TestE2E_MouseInteraction` | dblclick/hover/hover_ref/find_click_text |
| `e2e_scroll_test.go` | `TestE2E_ScrollAndScreenshot` | scroll/scroll_into_view/screenshot/PDF |
| `e2e_drag_test.go` | `TestE2E_DragAndUpload` | drag/upload_file |
| `e2e_agent_test.go` | `TestE2E_AgentFriendlyTools` | 7 agent-friendly tools (find_ref, wait, page_state, exists, dialog, fill_form, snapshot:interactiveOnly) |
| `e2e_cookie_test.go` | `TestE2E_CookieCallback` | Cookie storage callback: built-in HTTP server sets cookies → close browser → verify callback data |
| `e2e_record_test.go` | `TestE2E_RecordReplay_FullFlow` | record_start → ops → record_stop → scene_replay |
| | `TestE2E_RecordReplay_VariableSubstitution` | Variable substitution: `{{username}}`/`{{password}}` |
| | `TestE2E_RecordReplay_StopOnError` | stopOnError behaviour |

```bash
# Run all e2e tests (requires Windows + DLL + valid apiKey)
go test -v -run TestE2E -timeout 600s

# Run individual test files
go test -v -run TestE2E_FormElements -timeout 300s .
go test -v -run TestE2E_KeyboardInteraction -timeout 300s .
go test -v -run TestE2E_SnapshotClickRef -timeout 300s .
```

72 of 72 MCP tools covered; only `browser_install` (async, long-running) is not covered.

## Project Layout

```
brosdk-mcp-go/
├── main.go                        # Entry point, CLI flags, graceful shutdown
├── go.mod
├── README.md
├── README_EN.md
├── docs/
│   ├── ARCHITECTURE.md             # Architecture & design decisions
│   └── tools-reference.md          # 72 MCP Tool API reference
├── e2e_test.go                    # E2E shared infrastructure (types, fixture, helpers)
├── e2e_basic_test.go              # E2E: SDK basics + CDP form tests
├── e2e_snapshot_test.go           # E2E: snapshot + click_ref workflow
├── e2e_form_test.go               # E2E: form element interactions
├── e2e_keyboard_test.go           # E2E: keyboard interactions
├── e2e_mouse_test.go              # E2E: mouse interactions
├── e2e_scroll_test.go             # E2E: scroll + screenshot + PDF
├── e2e_drag_test.go               # E2E: drag + file upload
├── e2e_agent_test.go              # E2E: agent-friendly tools
├── e2e_cookie_test.go             # E2E: cookie storage callback
├── e2e_record_test.go             # E2E: record-replay full flow
├── e2e_tab_test.go                # E2E: tab management + get_html
├── libs/
│   ├── brosdk.h                   # C header (reference)
│   ├── windows-x64/
│   │   └── brosdk.dll             # Windows x64 native library
│   └── darwin-arm64/
│       └── libbrosdk.dylib        # macOS arm64 native library (auto-downloaded)
└── internal/
    ├── brosdk/                    # Native SDK bindings + chromedp browser actions
    │   ├── native.go              # nativeLib interface
    │   ├── native_windows.go      # Windows syscall.LazyDLL impl
    │   ├── native_darwin.go       # macOS CGo dlopen/dlsym impl
    │   ├── native_unsupported.go  # Stub for unsupported platforms
    │   ├── download.go            # GitHub Releases auto-downloader
    │   ├── http.go                # HTTP client: fetchUserSig via BroSDK API
    │   ├── manager.go             # High-level Go API (Manager singleton + event publishing)
    │   ├── types.go               # Data types + JSON builders + CDP types
    │   ├── errors.go              # Error helper
    │   ├── cdp.go                 # CDP WebSocket proxy (Windows / macOS)
    │   ├── cdp_unsupported.go     # CDP stub (other platforms)
    │   ├── actions.go             # chromedp browser actions (Windows / macOS)
    │   └── actions_unsupported.go # actions stub (other platforms)
    ├── config/
    │   └── config.go              # Startup config loader (config.local.json → config.json + BROSDK_* env vars)
    ├── mcp/
    │   ├── server.go              # MCP server (JSON-RPC 2.0 + SSE/Streamable HTTP + broadcast)
    │   ├── streamable.go          # Streamable HTTP transport (MCP 2025-03-26)
    │   ├── inspector.go           # go:embed loader
    │   └── inspector.html         # Built-in MCP Inspector Web UI (standalone HTML)
    └── tools/
        └── tools.go               # 72 tool definitions + handler dispatch + Recorder hook
    └── recorder/
        ├── recorder.go            # Record singleton (start/stop/capture/sanitize)
        ├── player.go              # Replay engine (step execution + variable substitution)
        └── guard.go               # WaitFor guard (readyState/exists completion detection)
```

## Key Design Decisions

| Design Point               | Notes                                                        |
|----------------------------|--------------------------------------------------------------|
| **No CGo**                 | Pure `syscall.LazyDLL` dynamic loading, following `brosdk-go` |
| **Pure GOOS Constraints**  | `//go:build windows` / `//go:build !windows`, no build tags   |
| **SDK Singleton**          | Managed by `Manager` + `sync.RWMutex`                         |
| **Async Callback Bridge**  | C `result_callback` → Go `emit()` → SSE Broadcast `sdk-event` |
| **Cookie Callback Bridge** | C `cookies_storage_cb` → Go `emitCookie()` → SSE Broadcast `cookies-event` |
| **chromedp Actions**       | Powered by `github.com/chromedp/chromedp` (v0.15.1), native Action types |
| **AX Tree Ref Targeting**  | `browser_snapshot` → `backendDOMNodeId` → `browser_*_ref` precise targeting |
| **CDP Connection Pool**    | WebSocket connections cached per `envId`, auto-reconnect on failure (one retry) |
| **Auto userSig**           | When `apiKey` is provided, `Init()` auto-fetches userSig via BroSDK HTTP API |
| **Auto Debug Port**        | `browser_open` always injects `--remote-debugging-port=0`    |
| **Record-Replay Hook**     | Handler layer auto-captures all browser actions; Dispatch & Player share same switch |
| **WaitFor Guard**          | Steps auto-infer completion conditions (readyState/exists); replay blocks instead of blind sleep |
| **HumanDelay**             | Inter-step human pauses captured during recording (capped 3s); replay toggle via `applyHumanDelay` |

## Dependencies

| Dependency                              | Purpose                              |
|-----------------------------------------|--------------------------------------|
| `github.com/chromedp/chromedp` v0.15.1 | High-level browser actions (click/type/fill, etc.) |
| `github.com/chromedp/cdproto`          | CDP protocol types + low-level ops   |
| `github.com/gorilla/websocket`         | CDP WebSocket connection             |

## Related Projects

| Repo                                                    | Description            |
|--------------------------------------------------------|------------------------|
| [brosdk](https://github.com/browsersdk/brosdk)         | Native C/C++ SDK      |
| [brosdk-go](https://github.com/browsersdk/brosdk-go)   | Go language bindings   |
| [brosdk-mcp](https://github.com/browsersdk/brosdk-mcp) | TypeScript MCP Server |
| [brosdk-core](https://github.com/browsersdk/brosdk-core) | Browser cores + versions |

## License

MIT

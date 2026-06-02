# MCP Tools Reference

> 65 tools, 4 categories + recorder/scene (9 tools). All async tools return `reqId` immediately; results delivered via SSE `sdk-event`.

## 1. SDK Info (3 tools)

| Tool | Type | Parameters | Returns | Description |
|------|------|-----------|---------|-------------|
| `sdk_info` | sync | — | `{version, status, config}` | Get SDK runtime info |
| `sdk_token_update` | async | `userSig` (required) | `reqId` | Refresh userSig |
| `sdk_get_user_sig` | sync | `apiKey` (required), `duration` | `{userSig}` | Obtain userSig via API |

## 2. Browser Control (5 tools)

| Tool | Type | Parameters | Returns | Description |
|------|------|-----------|---------|-------------|
| `browser_install` | async | `channel` | `reqId` | Install/update browser core |
| `browser_info` | sync | — | `{envs[]}` | List running environments |
| `browser_open` | async | `envId` (required), `urls[]`, `args[]` | `reqId` | Open browser; wait for `browser-open-success` |
| `browser_close` | async | `envId` (required) | `reqId` | Close browser |
| `browser_command` | sync | `envId`, `method` (required), `params`, `sessionId` | CDP response | Raw CDP command |

## 3. Browser Actions (43 tools)

### 3.1 Page Navigation

| Tool | Parameters | Description |
|------|-----------|-------------|
| `browser_navigate` | `envId`, `url` (required) | Navigate to URL; returns `{targetId, sessionId}` |
| `browser_reload` | `envId` (required), `sessionId` | Reload the current page |
| `browser_back` | `envId` (required), `sessionId` | Navigate back in browser history |
| `browser_forward` | `envId` (required), `sessionId` | Navigate forward in browser history |
| `browser_snapshot` | `envId` (required), `sessionId`, `interactiveOnly` | Capture accessibility tree (AX Tree). `interactiveOnly=true` filters to interactive nodes only, reducing output 10-50x |

### 3.2 Mouse Actions

| Tool | Parameters | Description |
|------|-----------|-------------|
| `browser_click` | `envId`, `selector` (required), `sessionId` | Click by CSS selector |
| `browser_click_ref` | `envId`, `ref` (required), `sessionId` | Click by snapshot ref (backendDOMNodeId) |
| `browser_dblclick` | `envId`, `selector` (required), `sessionId` | Double-click by CSS selector |
| `browser_hover` | `envId`, `selector` (required), `sessionId` | Hover by CSS selector |
| `browser_hover_ref` | `envId`, `ref` (required), `sessionId` | Hover by snapshot ref |

### 3.3 Keyboard / Input

| Tool | Parameters | Description |
|------|-----------|-------------|
| `browser_type` | `envId`, `selector`, `text` (required), `sessionId` | Type by CSS selector (append) |
| `browser_type_ref` | `envId`, `ref`, `text` (required), `sessionId` | Type by snapshot ref (append) |
| `browser_fill` | `envId`, `selector`, `text` (required), `sessionId` | Clear + type by CSS selector |
| `browser_fill_ref` | `envId`, `ref`, `text` (required), `sessionId` | Clear + type by snapshot ref |
| `browser_press_key` | `envId`, `key` (required), `sessionId` | Press key (Enter, Escape, Tab...) |
| `browser_keyboard_type` | `envId`, `text` (required), `sessionId` | Type char by char |
| `browser_insert_text` | `envId`, `text` (required), `sessionId` | Insert text via Input.insertText |
| `browser_key_down` | `envId`, `key` (required), `sessionId` | KeyDown event |
| `browser_key_up` | `envId`, `key` (required), `sessionId` | KeyUp event |

### 3.4 Focus / Selection

| Tool | Parameters | Description |
|------|-----------|-------------|
| `browser_focus` | `envId`, `selector` (required), `sessionId` | Focus by CSS selector |
| `browser_focus_ref` | `envId`, `ref` (required), `sessionId` | Focus by snapshot ref |
| `browser_select_option` | `envId`, `selector`, `value` (required), `sessionId` | Set `<select>` value |
| `browser_select_option_ref` | `envId`, `ref`, `value` (required), `sessionId` | Set select by snapshot ref |
| `browser_check` | `envId`, `selector` (required), `sessionId` | Check checkbox/radio (idempotent) |
| `browser_check_ref` | `envId`, `ref` (required), `sessionId` | Check by snapshot ref (idempotent) |
| `browser_uncheck` | `envId`, `selector` (required), `sessionId` | Uncheck checkbox |
| `browser_uncheck_ref` | `envId`, `ref` (required), `sessionId` | Uncheck by snapshot ref |

### 3.5 Scroll / Drag

| Tool | Parameters | Description |
|------|-----------|-------------|
| `browser_scroll` | `envId`, `direction` (required), `px`, `selector`, `sessionId` | Scroll page or element |
| `browser_scroll_into_view` | `envId`, `selector` (required), `sessionId` | Scroll element into view |
| `browser_drag` | `envId`, `sourceSelector`, `targetSelector` (required), `sessionId` | Drag from source to target |

### 3.6 File / Screenshot / PDF

| Tool | Parameters | Description |
|------|-----------|-------------|
| `browser_upload_file` | `envId`, `selector`, `files[]` (required), `sessionId` | Upload files to file input |
| `browser_screenshot` | `envId` (required), `path`, `format`, `quality`, `fullPage`, `sessionId` | Take screenshot |
| `browser_pdf` | `envId`, `path` (required), `sessionId` | Generate PDF |

### 3.7 Text Finding / Scripting

| Tool | Parameters | Description |
|------|-----------|-------------|
| `browser_find_click_text` | `envId`, `text` (required), `sessionId` | Find visible text and click |
| `browser_get_text` | `envId`, `selector` (required), `sessionId` | Return visible text content of element |
| `browser_get_value` | `envId`, `selector` (required), `sessionId` | Return value attribute of input element |
| `browser_evaluate` | `envId`, `expression` (required), `sessionId` | Execute JavaScript and return result |

### 3.8 Agent-Friendly Tools (NEW)

Designed for AI agent workflows — reduce token usage, simplify element targeting, and handle async page state.

| Tool | Parameters | Returns | Description |
|------|-----------|---------|-------------|
| `browser_find_ref` | `envId` (required), `role`, `name`, `value` | `{refs[{ref,role,name,value}], count}` | Search AX tree by role/name/value; returns ref list for `_ref` tools |
| `browser_wait` | `envId` (required), `text`, `role`, `name`, `timeoutMs` (default 5000) | `{found,elapsedMs}` | Poll snapshot until matching element appears |
| `browser_page_state` | `envId` (required) | `{title, url, readyState}` | Fast page metadata — no snapshot overhead |
| `browser_exists` | `envId` (required), `role`, `name` | `{exists}` | Boolean element existence check |
| `browser_dialog` | `envId` (required), `action` (accept/dismiss) | `{action, message}` | Handle JS alert/confirm/prompt via page-side capture |
| `browser_fill_form` | `envId` (required), `fields[{ref,value}]`, `submitRef` | `{filled, submitted}` | Batch fill form fields + optional submit |

## 4. Environment Management (5 tools)

| Tool | Type | Parameters | Description |
|------|------|-----------|-------------|
| `env_create` | sync | `name`, `os`, `body` | Create fingerprint browser environment |
| `env_page` | sync | `page`, `pageSize`, `body` | Query/list environments (paginated) |
| `env_update` | sync | `envId` (required), `body` | Update environment config |
| `env_destroy` | sync | `envId` (required) | Permanently delete environment |
| `env_getinfo` | sync | `envId` (required) | Get single environment detail |

## 5. Recorder & Scene (9 tools)

| Tool | Parameters | Description |
|------|-----------|-------------|
| `record_start` | `name` | Start recording browser actions |
| `record_stop` | — | Stop recording, auto-save to `scenes/{name}.json` |
| `record_status` | — | Get recording status |
| `scene_list` | — | List all saved scenes |
| `scene_get` | `name` (required) | Get scene details + steps |
| `scene_update` | `name` (required), `steps`, ... | Update scene (steps, variables, etc.) |
| `scene_delete` | `name` (required) | Delete a scene |
| `scene_replay` | `name` (required), `envId`, `variables`, `stopOnError`, `stepDelay`, `applyHumanDelay` | Replay scene with **WaitFor guards** + optional human delay |
| `scene_save` | `name` (required), `steps` | Manually save scene (rare; `record_stop` auto-saves) |

**Replay features (NEW):**
- **WaitFor guard**: Steps auto-infer post-conditions (e.g. `readyState:complete` after navigate/click). Replay blocks until satisfied — no blind sleep.
- **HumanDelay**: Recorded inter-step human pauses captured as `HumanDelayMs`. Replay optionally applies them (`applyHumanDelay=true`, cap 3s).

---

## Ref-Based Targeting Workflow

```
browser_snapshot → AX Tree with backendDOMNodeId → browser_*_ref(envId, ref=NN)
```

**8 `_ref` variants**: `click_ref`, `type_ref`, `fill_ref`, `hover_ref`, `focus_ref`, `select_option_ref`, `check_ref`, `uncheck_ref`

**Key rules:**
- `findBackendDOMNodeID` does **exact match** on AX `name.value`
- Always search by the target element's accessible name (not child text)
- Use `aria-label` on elements inside labels to ensure correct AX matching

# Changelog

All notable changes to brosdk-mcp-go are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/).

---

## [Unreleased] — v0.1.0

### Added

- **MCP Streamable HTTP transport (2025-03-26)**: new `/mcp` endpoint supporting POST (JSON-RPC requests with content negotiation), GET (SSE stream for server-initiated events), and DELETE (session termination). Session management via `Mcp-Session-Id` header. Protocol version bumped to `2025-03-26`. Legacy `/sse` + `/message` endpoints preserved for backward compatibility.
- **Environment variable configuration overrides**: all config fields now support `BROSDK_*` env var overrides (`BROSDK_API_KEY`, `BROSDK_USER_SIG`, `BROSDK_WORK_DIR`, `BROSDK_PORT`, `BROSDK_SDK_API_URL`, `BROSDK_DEBUG`). CLI flags `-lib` and `-addr` fall back to `BROSDK_LIB` / `BROSDK_ADDR`. Priority: CLI flag > env var > config file > default. Server can start with env vars alone, no config file needed.
- **Inspector Streamable HTTP support**: Inspector web UI auto-detects Streamable HTTP transport and uses it when available, falling back to legacy SSE.
- 6 new tools: `browser_new_tab`, `browser_close_tab`, `browser_list_tabs`, `browser_get_html`, `browser_get_cookies`, `browser_set_cookies`.
- `browser_select` tool: set an active browser environment so subsequent Browser Actions can omit `envId`.
- `browser_wait` now supports four modes: `navigation`, `selector`, `text`, `time`.
- `browser_select_option_ref`, `browser_check_ref`, `browser_uncheck_ref`, `browser_focus_ref`, `browser_hover_ref` — ref-based variants for form interaction tools.
- Screenshot and PDF tools now save to `workDir/screenshots/` and `workDir/pdfs/` subdirectories and return absolute file paths.

### Fixed

- **Concurrency races in tab lifecycle**: `Navigate()`, `ensureTab()`, `ensureBrowser()` now use mutex protection for tab context swaps. Dead code `closeActiveTab` removed.
- **CDP connection pool race**: `getOrDial()` uses a signal-channel dedup pattern to prevent concurrent dials from leaking orphaned WebSocket connections.
- **Screenshot/PDF timeout**: added 30s `captureTimeout` context; fixed ignored `os.MkdirAll` errors.
- **Form event dispatching**: `Fill()`, `FillRef()`, `SelectOption()`, `SelectOptionRef()` now dispatch `input` + `change` events after `SetValue`, making programmatic changes detectable by React/Vue/Angular.
- **Shutdown flow**: reordered to HTTP → chromedp → CDP → SDK dependency chain. Fixed `CloseCDP()` nil map panic when `cdpConns` is nil.
- **Broadcast logging**: SSE broadcast now logs dropped events (channel full) with event type and client ID.
- **`json.Marshal` error handling**: `browser_evaluate` returns explicit error when JS result contains non-JSON-safe values (NaN/Infinity) instead of silently returning empty string. `BrowserCommand` returns error on marshal failure before sending nil body over CDP WebSocket, preventing protocol corruption.
- **`CloseTab` active tab promotion**: closing the active tab now correctly promotes a background tab instead of leaving no active tab.
- **E2E test**: fixed wrong tool name `browser_keyboard_insert_text` → `browser_insert_text` in keyboard tests and unsupported platform stubs.
- **Broadcast deadlock prevention**: `Broadcast()` uses copy-then-send pattern, collecting all channel references under RLock then sending without holding any lock.
- **Inspector dead code**: removed unused `inspectorToolsJSON()` function and its `encoding/json` import.

### Changed

- **Inspector go:embed migration**: extracted 660-line HTML/CSS/JS from Go raw string constant into standalone `inspector.html` file. `inspector.go` now uses `//go:embed` for compile-time embedding (713 → 33 lines).
- **Tool descriptions**: added cross-reference guidance to 10 tools (type ↔ fill, keyboard tools, content retrieval tools) to reduce Agent confusion.
- `envId` parameter is now optional for all Browser Actions when an active environment is set via `browser_select`.
- `browser_wait` replaces the former flat `timeout` parameter with a structured `waitFor` object.

### Documentation

- Updated README and README_EN: environment variable section, Streamable HTTP endpoint, project structure reflecting new files.
- Added analysis report (`docs/analysis-mcp-design.md`) with 12 items all resolved: concurrency races, shutdown flow, timeouts, CDP races, form events, tool granularity evaluation, broadcast logging, tool descriptions, json.Marshal, Inspector go:embed, env var config, Streamable HTTP.

---

## [v0.0.3] — 2025-05-28

### Added

- **Browser record-replay system**: `record_start`, `record_stop` tools capture browser action sequences as "scenes". Scene CRUD via `scene_list`, `scene_get`, `scene_delete`. `scene_replay` replays recorded steps with variable substitution (`{{key}}`) and optional `applyHumanDelay`.
- **WaitFor guard**: replay steps now wait for completion conditions before executing the next step. Supports `navigation` (page readyState), `selector` (CSS appearance), `text` (text presence), and `time` (fixed delay).
- **HumanDelay**: optional randomized delays between replay steps to mimic human interaction patterns.
- **7 Agent-friendly tools**: `browser_find_ref`, `browser_wait`, `browser_page_state`, `browser_exists`, `browser_dialog`, `browser_fill_form`, plus `interactiveOnly` mode for `browser_snapshot` (returns only interactive elements, ~10% of full snapshot size).
- **Inspector parameter documentation table**: shows parameter name, type, required/optional tag, and description above the JSON editor when a tool is selected.
- `browser_select_option_ref`: ref-based variant for select_option.

### Fixed

- Browser lifecycle cleanup on environment close.
- `_ref` replay via snapshot fingerprint matching for reliable element identification across sessions.
- `Snapshot()` output format for consistent AX tree rendering.
- Dialog JS injection conflict resolved.
- E2E test timing fixes for stable CI runs.
- Exposed `applyHumanDelay` to `scene_replay` tool parameters.

### Documentation

- Added tools-reference and ARCHITECTURE docs for agent-friendly tools and WaitFor guard.

---

## [v0.0.2] — 2025-05-15

### Added

- **macOS support**: `libbrosdk.dylib` binding via CGo alongside Windows DLL via syscall.
- **Auto-download**: brosdk native library auto-downloaded from GitHub Releases on first run when `-lib` flag is not specified. Uses HTTP redirect for release discovery.
- **MCP Inspector**: embedded web UI at `GET /inspector` — self-contained single-page app for browsing tools, calling them interactively, monitoring SSE events, and managing scenes.
- Inspector features: tool search/filter, JSON parameter editor, call history, SSE event log with expandable detail, scene management tab, replay dialog.
- 6 new tools added (40 → 50): `browser_scroll`, `browser_scroll_into_view`, `browser_drag`, `browser_upload_file`, `browser_evaluate`, `browser_find_click_text`.
- History clear button and SSE event detail expansion in Inspector.

### Changed

- Tool parameter unification: renamed tools and standardized parameter names for consistency.
- Pass-through dispatch: marshal params as-is for SDK tools, reducing boilerplate in the tool layer.

### Fixed

- Graceful shutdown: replaced `os.Exit(0)` with `httpSrv.Shutdown()` + 10s timeout.

### Documentation

- Added `docs/` directory with ARCHITECTURE.md.
- Split E2E tests into separate files by category.

---

## [v0.0.1] — 2025-05-01

Initial open-source release.

### Core Features

- MCP SSE server implementing JSON-RPC 2.0 over HTTP+SSE (MCP 2024-11-05 spec).
- 40 browser automation tools: navigation, clicking, typing, filling, screenshots, PDF generation, cookies, JavaScript evaluation.
- CSS Selector + Ref dual-positioning: every interaction tool has both `selector` and `_ref` variants for flexible element targeting.
- `browser_snapshot` with accessibility tree output for Agent-driven element discovery.
- CDP WebSocket connection pool with per-`envId` caching and automatic reconnection.
- Four-layer architecture: MCP Server → Tool Layer → Browser Actions → Native SDK binding.
- Windows-only native SDK binding via `syscall`.
- Config file support: `config.local.json` (local override) → `config.json`.
- Health check endpoint at `GET /health`.

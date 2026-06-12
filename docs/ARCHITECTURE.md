# brosdk-mcp-go 架构设计

> 基于 BroSDK 指纹浏览器 SDK 的 MCP SSE 服务，Go 实现。
> 本文档记录**真实实现**，不是计划。

## 技术栈

| 组件 | 选型 | 说明 |
|------|------|------|
| HTTP Server | `net/http` 标准库 | SSE + JSON-RPC，不引入框架 |
| MCP Transport | SSE + Streamable HTTP（`POST/GET/DELETE /mcp`） | SSE 符合 spec 2024-11-05；Streamable HTTP 符合 2025-03-26 |
| FFI | **Windows**: `syscall.LazyDLL`（无 CGo）<br>**macOS**: CGo + `dlopen`/`dlsym` | 平台原生动态库加载 |
| 浏览器操作 | `chromedp` v0.15.1 | 高层 API：click/type/screenshot 等 |
| CDP 代理 | `gorilla/websocket` | 透明转发到浏览器 DevTools |
| 序列化 | `encoding/json` | 标准库 |
| 日志 | `log/slog` | 结构化日志 |

## 目录结构（实际）

```
brosdk-mcp-go/
├── main.go                     # 入口：SSE server 启动 + SDK 自动初始化 + 优雅关闭
├── go.mod
├── README.md / README_EN.md    # 项目说明 + API 参考
├── docs/
│   ├── tools-reference.md      # 72 MCP Tool API 参考
│   └── ARCHITECTURE.md         # 本文档
├── e2e_test.go                 # E2E 共享基础设施
├── e2e_*_test.go               # 11 个按功能拆分的 E2E 测试文件（含 agent 工具、cookie 回调）
├── libs/
│   ├── brosdk.h                       # C 头文件（参考）
│   ├── windows-x64/brosdk.dll         # Windows x64 原生库
│   └── darwin-arm64/libbrosdk.dylib   # macOS arm64 原生库（自动下载）
├── internal/
│   ├── brosdk/                 # Native 绑定 + chromedp + CDP 代理
│   │   ├── native.go           # nativeLib 接口定义
│   │   ├── native_windows.go   # Windows: syscall.LazyDLL（//go:build windows）
│   │   ├── native_darwin.go    # macOS: CGo dlopen/dlsym（//go:build darwin）
│   │   ├── native_unsupported.go # 其他平台桩（//go:build !windows && !darwin）
│   │   ├── download.go         # GitHub Releases 自动下载（平台无关）
│   │   ├── manager.go          # SDK 单例管理 + 事件发布
│   │   ├── types.go            # 数据类型 + JSON 构造器
│   │   ├── errors.go           # 错误辅助
│   │   ├── http.go             # HTTP 客户端（fetchUserSig API）
│   │   ├── cdp.go              # CDP WebSocket 代理（windows || darwin）
│   │   ├── cdp_unsupported.go  # CDP 桩（其他平台）
│   │   ├── actions.go          # chromedp 高层浏览器操作（49 个 action + 6 agent-friendly 工具）
│   │   └── actions_unsupported.go # actions 桩
│   ├── config/
│   │   └── config.go           # config.local.json → config.json 加载
│   ├── mcp/
│   │   ├── server.go           # SSE server + session 管理 + JSON-RPC dispatch
│   │   ├── streamable.go       # Streamable HTTP transport（MCP 2025-03-26）
│   │   ├── inspector.go        # go:embed 加载器
│   │   └── inspector.html      # 内嵌 MCP Inspector Web UI（独立 HTML 文件）
│   └── tools/
│       └── tools.go            # 72 Tool 定义 + handler dispatch + Recorder hook
│   └── recorder/
│       ├── recorder.go         # 录制单例：start/stop/capture/sanitize + WaitFor 推断 + HumanDelay 捕获
│       ├── player.go           # 回放引擎：步骤执行 + WaitFor 守卫 + 变量替换 + human delay
│       ├── guard.go            # WaitGuard 类型 + 4 种守卫实现 (readyState/exists/wait/none)
│       ├── refconv.go          # 录制指纹提取（_ref 工具 ↔ AX tree 匹配）
│       ├── recorder_test.go    # 录制单元测试
│       ├── player_test.go      # 回放单元测试
│       └── guard_test.go       # WaitFor/HumanDelay 单元测试
```

## MCP Transport：SSE + Streamable HTTP

### SSE（传统，spec 2024-11-05）

```
GET  /inspector  → 内嵌 MCP Inspector Web UI（工具浏览 + 调用 + SSE 事件）
GET  /sse        → 建立 SSE 长连接，推送 endpoint event
POST /message    → 客户端发送 JSON-RPC 请求
```

流程：
1. 客户端 `GET /sse` → 收到 `event: endpoint\ndata: /message?sessionId=<id>`
2. 客户端向 `/message?sessionId=<id>` POST JSON-RPC 请求
3. 服务端处理后通过 SSE 推送 `event: message\ndata: {...}`
4. SDK 异步回调通过 SSE `sdk-event` 推送给所有 session
5. SSE 每 15s 发送 `: ping` 心跳

### Streamable HTTP（spec 2025-03-26）

```
POST /mcp  → JSON-RPC 请求（Content-Type 协商：application/json 或 text/event-stream）
GET  /mcp  → SSE 流（服务端主动推送事件）
DELETE /mcp → 终止会话
```

通过 `Mcp-Session-Id` header 管理会话。传统 `/sse` + `/message` 端点保留向后兼容。

## 异步回调桥接

```
Native result_callback (C)
        ↓ syscall.NewCallback
  Go channel (chan SdkEvent)
        ↓ Manager.emit()
  SSE Broadcast → 所有连接的 MCP Client
```

关键约束（与 brosdk-rust 对齐）：
- `sdk_malloc` / `sdk_free`：所有 SDK 分配的 `char*` 必须用 `sdk_free` 释放
- 异步操作（`browser_open`、`browser_close`、`token_update`）返回 `reqId`，最终结果通过 `result_callback` 推送
- 单例：`Manager` + `sync.RWMutex` 管理

## Cookie Storage 回调

```
SDK cookies_storage_cb (C)
        ↓ RegisterCookiesStorageCB
  Go channel (chan CookiesEvent)
        ↓ Manager.emitCookie()
  SSE Broadcast "cookies-event" → 所有连接的 MCP Client
```

浏览器存储或修改 cookie 时，SDK 通过 `sdk_cookies_storage_cb_t` 回调通知。
回调数据包含 cookie 数组（`name`、`value`、`domain`、`path`、`expirationDate` 等）。
通常在浏览器关闭时触发。

## Native 绑定方案

**Windows**（`native_windows.go`，`//go:build windows`）：

- 动态加载：`syscall.LazyDLL`（无 CGo）
- 回调注册：`syscall.NewCallback(sdkResultCallback)` → `sdk_register_result_cb`
- sync 调用：`callSyncJSON` / `callSyncNoArgs`，out buffer 用 `sdk_free` 释放
- async 调用：`callAsyncJSON`，返回 `reqId`（int32 > 0）
- `sdk_init`：`withHandle=true`（第一个参数是 `sdk_handle_t*`）
- `uintptr → unsafe.Pointer`：`go vet` false positive，`go build` 通过

**macOS**（`native_darwin.go`，`//go:build darwin`）：

- 动态加载：CGo + `dlopen`/`dlsym`
- 回调注册：`//export goSdkResultCallback` → C function pointer → `sdk_register_result_cb`
- sync/async 调用：CGo inline helper 函数（类型安全的函数指针调用）
- out buffer 释放：`sdk_free` via dlsym

**其他平台**（`native_unsupported.go`，`//go:build !windows && !darwin`）：返回错误桩。

**自动下载**（`download.go`，平台无关）：
- 首次运行无本地库时，通过 HTTP redirect（`/releases/latest` → `/releases/tag/vX.Y.Z`）解析版本号，按命名约定构造下载 URL — 无需 GitHub API、无需 token、无速率限制
- 下载时控制台输出实时进度（百分比 + 大小）
- 解压到 `libs/<platform>/` 目录
- 平台标签：`windows-x64` / `darwin-arm64`

## Browser Actions 设计（chromedp）

- **核心库**：`github.com/chromedp/chromedp` v0.15.1
- **连接管理**：`NewRemoteAllocator(ctx, wsURL)` → `chromedp.NewContext(allocCtx)`，每个 envID 独立 tab
- **Tab 存储**：`Manager.browsers map[string]*browserTab`
- **Active session**：`browser_navigate` 自动创建 target + attach + 存储 sessionId，后续工具默认使用
- **Chromedp 原生 Action**：Click, DoubleClick, Focus, SendKeys, Clear, SetValue, ScrollIntoView, SetUploadFiles, CaptureScreenshot, FullScreenshot, KeyEvent, Navigate, WaitReady
- **cdproto 直接调用**：Snapshot (`accessibility.GetFullAXTree`), Hover (`input.DispatchMouseEvent`), 键盘 (`input.DispatchKeyEvent`), PDF (`page.PrintToPDF`), FindClickText (`dom.PerformSearch`)
- **最小 Evaluate**：Scroll (`window.scrollBy`), Drag (DataTransfer 事件链), Check/Uncheck (checked 状态检查) — 单行 JS
- **Ref 定位**：`cdpdom.ResolveNode` → `cdpdom.RequestNode` → `chromedp.ByNodeID`，8 个 `_ref` 变体工具

### Agent-Friendly 工具（6 个新增）

为 AI Agent 工作流优化，减少 token 消耗、简化元素定位、处理异步页面状态：

| 工具 | 功能 | 关键设计 |
|------|------|------|
| `browser_find_ref` | AX tree 按 role/name/value 搜索 | 返回 `ref` 列表供 `_ref` 工具使用，避免 LLM 手写 selector |
| `browser_wait` | 轮询等待元素出现 | 200ms 间隔，内部复用 `Snapshot(interactiveOnly)` 降开销 |
| `browser_page_state` | 快速 `{title, url, readyState}` | 无 snapshot 开销，供 guard 和 agent 判断页面状态 |
| `browser_exists` | 布尔检查元素存在 | 轻量，不返回树结构 |
| `browser_dialog` | 处理 JS alert/confirm/prompt | 页面侧 override 捕获 → `__wbDialogMsg` → 读取后清除。避免 CDP 事件时序问题 |
| `browser_fill_form` | 批量填表 + 可选提交 | 单次调用完成多字段填充，减少 tool call 往返 |

`browser_snapshot` 增强：`interactiveOnly=true` 仅返回可交互节点，输出缩减 10-50x。内部由 `filterInteractiveNodes()` 递归过滤。

## browser_command CDP 代理

- WebSocket 连接池按 `envID` 缓存，30s 超时
- **debug port 来源**：`Manager.emit()` 解析 `browser-open-success` 事件，提取 `remoteDebuggingPort`
- 当 chromedp 有活跃 tab 时，优先通过 chromedp 路由 CDP 命令（待实现）

## 浏览器录制回放

### 架构

```
Agent calls record_start
        ↓
Handler() → Recorder hook (capture every browser action)
        ↓                      ↓
  Dispatch(mgr, name, params)  Recorder.Capture(name, sanitized)
        ↓                           ↓
Agent calls record_stop → auto-save to scenes/{name}.json
        ↓
Agent calls scene_replay → Player.LoadScene → Player.Replay
        ↓
  For each step: resolve ref → dispatch → WaitFor guard → HumanDelay → next step
```

### 步骤完成判断：WaitFor 守卫（NEW）

回放每步不再依赖固定 sleep。`Step.WaitFor` 声明该步完成后期望的页面状态，Player 在 dispatch 返回后阻塞等待条件满足。

4 种守卫类型：

| Type | 实现 | 默认超时 | 场景 |
|------|------|:--:|------|
| `readyState` | 轮询 `PageState()` 直到 `readyState` 匹配 | 10s | navigate → 等页面加载完成 |
| `exists` | 轮询 `Exists()` 直到元素出现 | 5s | click → 等结果元素渲染 |
| `wait` | 调用 `Wait()` 内部轮询 | 5s | 通用等待（text/role/name） |
| `none` | 不等待 | — | 无副作用的工具 |

录制时自动推断（`InferWaitFor`）：
- navigate/open → `readyState:complete` (10s)
- click/back/forward/reload → `readyState:complete` (5s)
- 其他 → `none`

Guard 失败处理：注入 `guardWarn` 注解（软错误），不中断回放。

### HumanDelay 机制（NEW）

录制时 `Capture()` 记录 `time.Since(lastCaptureTime)` → `Step.HumanDelayMs`。
回放时若 `ReplayOptions.ApplyHumanDelay=true`（默认），在 WaitFor 满足后插入 humanDelay（上限 3s）。
关闭后全速运行，适合回归测试。

### 关键设计

- **Hook 模式**：Handler 在 Dispatch 之前自动捕获所有非录制类 tool call，零侵入
- **共享 Dispatch**：Player 直接调用 `tools.Dispatch()`，重用相同的 switch 分支，零代码重复
- **`record_stop` 自动保存**：停止录制时直接写入 `workDir/scenes/{name}.json`，不需要 Agent 手动调 `scene_save`
- **变量替换**：步骤中的 `{{variable}}` 占位符在回放时被 `scene_replay({variables:{...}})` 替换
- **envId 注入**：录制时自动去除 `envId`，回放时由调用方注入
- **场景文件**：JSON 格式，可读、可 git 管理、可手写编辑
- **WaitFor 守卫**：回放可靠性核心 — 条件驱动等待替代固定 sleep，自动推断 + 可手动编辑
- **HumanDelay 模拟**：录制时捕获人类操作间隔，回放时可选启用（cap 3s），平衡真实感与效率

### 工具分类（9 个）

| 类别 | 工具 | 说明 |
|------|------|------|
| 录制控制 | `record_start`, `record_stop`, `record_status` | 开始/停止/查看录制 |
| 场景管理 | `scene_list`, `scene_get`, `scene_update`, `scene_delete` | 列出/查看/编辑/删除场景 |
| 回放 | `scene_replay` | 加载场景并逐步回放 |

## 关键设计决策

| 决策 | 理由 |
|------|------|
| **Windows: syscall / macOS: CGo** | Windows 用无 CGo 方案避免交叉编译复杂性；macOS CGo 是 dlopen 的标准选择 |
| **自动下载动态库** | 首次运行从 GitHub Releases 获取最新版本，简化部署和跨平台体验 |
| **手写 MCP，不引入 mcp-go** | SSE transport 协议简单，可控性更高 |
| **SDK 自动初始化** | `main.go` 启动时加载 config.json 自动 init，用户无需调用 sdk_init |
| **双配置文件** | `config.local.json` > `config.json`，本地凭据不入 git |
| **异步工具返回 reqId** | `browser_open` 等立即返回，结果通过 SSE 事件推送 |
| **chromedp 高层 API** | 减少 LLM 手写 CDP 命令，覆盖率 97.7% 的 E2E 测试 |
| **selector + ref 双定位** | 分离工具而非合并参数，工作流清晰，无破坏性 |
| **SSE + Streamable HTTP transport** | SSE 向后兼容；Streamable HTTP 支持 MCP 2025-03-26，AI Agent 通过 HTTP 连接更灵活 |
| **录制回放 Hook 模式** | Handler 层自动捕获，Dispatch 与 Player 共享同一 switch，零代码重复 |
| **record_stop 自动保存** | 简化 Agent 工作流，减少 tool call 次数，避免步骤数组在网络间传输 |
| **WaitFor 守卫代替盲等** | 条件驱动等待（readyState/exists/wait）替代固定 sleep，解决 AJAX/SPA 可靠性问题 |
| **HumanDelay 可选模拟** | 录制时捕获人类节奏，回放时可开关（cap 3s），回归测试 vs 演示场景灵活切换 |
| **Dialog 页面侧捕获** | JS override alert/confirm/prompt → `__wbDialogMsg`，避免 CDP 事件时序竞争 |
| **Cookie Storage 回调** | `sdk_cookies_storage_cb_t` 全链路桥接，浏览器 cookie 变更通过 SSE `cookies-event` 实时推送 |

# brosdk-mcp-go 架构设计

> 基于 BroSDK 指纹浏览器 SDK 的 MCP SSE 服务，Go 实现。
> 本文档记录**真实实现**，不是计划。

## 技术栈

| 组件 | 选型 | 说明 |
|------|------|------|
| HTTP Server | `net/http` 标准库 | SSE + JSON-RPC，不引入框架 |
| MCP Transport | 仅 SSE（`GET /sse` + `POST /message`） | 符合 spec 2024-11-05 |
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
│   ├── tools-reference.md      # 50 MCP Tool API 参考
│   └── ARCHITECTURE.md         # 本文档
├── e2e_test.go                 # E2E 共享基础设施
├── e2e_*_test.go               # 7 个按功能拆分的 E2E 测试文件
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
│   │   ├── actions.go          # chromedp 高层浏览器操作（31 个 action）
│   │   └── actions_unsupported.go # actions 桩
│   ├── config/
│   │   └── config.go           # config.local.json → config.json 加载
│   ├── mcp/
│   │   ├── server.go           # SSE server + session 管理 + JSON-RPC dispatch
│   │   └── inspector.go        # 内嵌 MCP Inspector Web UI（自包含 HTML）
│   └── tools/
│       └── tools.go            # 50 Tool 定义 + handler dispatch
```

## MCP Transport：仅 SSE

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
- **Ref 定位**：`cdpdom.ResolveNode` → `cdpdom.RequestNode` → `chromedp.ByNodeID`，7 个 `_ref` 变体工具

## browser_command CDP 代理

- WebSocket 连接池按 `envID` 缓存，30s 超时
- **debug port 来源**：`Manager.emit()` 解析 `browser-open-success` 事件，提取 `remoteDebuggingPort`
- 当 chromedp 有活跃 tab 时，优先通过 chromedp 路由 CDP 命令（待实现）

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
| **仅 SSE transport** | 不需要 stdio，AI Agent 通过 HTTP 连接更灵活 |

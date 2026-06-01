# brosdk-mcp-go

[English](README_EN.md) | 简体中文

将 BroSDK 指纹浏览器 SDK 封装为 [MCP (Model Context Protocol)](https://modelcontextprotocol.io/) SSE 服务，
让 AI Agent（Claude、CodeBuddy 等）可以直接操控指纹浏览器环境。

## 功能概览

- 通过 44 个 MCP Tool 暴露 BroSDK 全部能力：SDK 生命周期、浏览器控制、浏览器高级操作、环境 CRUD
- 基于 [chromedp](https://github.com/chromedp/chromedp) 的高层浏览器操作——点击、输入、截图、PDF 等，无需手写 CDP 命令
- 保留 `browser_command` 透明 CDP 代理，支持所有 DevTools 命令的高级/自定义场景
- 异步工具通过 SSE 事件回传结果，支持长时间等待
- 双配置文件（`config.json` / `config.local.json`），启动时自动初始化 SDK
- 跨平台编译（Windows 生产 / 其他平台桩编译）

## 架构

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
        │  - 30+ high-level ops    │
        └───────┬──────────────────┘
                │  WebSocket (CDP)
        ┌───────▼──────────┐
        │  Native Layer    │
        │  syscall DLL     │
        │  brosdk.dll      │
        └───────┬──────────┘
                │  WebSocket (browser control)
        ┌───────▼──────────┐
        │  CDP Proxy       │
        │  cdp.go          │
        │  (DevTools raw)  │
        └──────────────────┘
```

## 前置要求

- Windows x64（运行；编译可在任何 OS 上，非 Windows 生成桩实现）
- Go 1.26+
- `brosdk.dll` — 位于 `libs/windows-x64/brosdk.dll`（仓库已包含）

## 编译

```bash
# Windows — 生产编译
go build -o brosdk-mcp.exe .

# Linux/macOS — 桩编译（无 DLL，tool 调用返回错误）
go build .
```

无 build tag 依赖，纯 `GOOS` 约束（`//go:build windows` / `//go:build !windows`）。

## 配置

启动时服务器依次读取 `config.local.json` → `config.json`（前者优先级更高）。
找到配置文件后自动调用 `sdk_init`。

### 配置文件格式

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

| 字段       | 必填   | 默认值         | 说明                                        |
|-----------|--------|----------------|---------------------------------------------|
| `apiKey`  | 是\*  | —              | 客户 ID；未提供 `userSig` 时自动通过 API 获取 |
| `userSig` | 否     | —              | 预计算的签名（优先级高于 apiKey）             |
| `workDir` | 否     | `./brosdk`     | SDK 工作目录（自动创建）                      |
| `port`    | 否     | `5811`         | SDK 浏览器控制端口                            |
| `sdkApiUrl`| 否    | `https://api.brosdk.com` | SDK API 地址                   |
| `debug`   | 否     | `false`        | 是否启用 SDK 调试日志                        |

\* `apiKey` 或 `userSig` 二选一必须提供。

### 本地开发覆盖

复制 `config.json` → `config.local.json`，填入不同凭据；
本地文件不纳入版本控制且优先于 `config.json`。

## 运行

```bash
brosdk-mcp.exe -lib libs/windows-x64/brosdk.dll -addr :8765
```

### 命令行参数

| 参数    | 默认值                           | 说明                        |
|---------|---------------------------------|------------------------------|
| `-lib`  | `libs/windows-x64/brosdk.dll`   | brosdk 原生库路径             |
| `-addr` | `:8765`                         | HTTP 监听地址                |

### DLL 自动发现

`-lib` 未指定时，按顺序查找：`./brosdk.dll` → `libs/windows-x64/brosdk.dll`。

## 端点

| 端点        | 方法  | 说明                          |
|------------|------|--------------------------------|
| `/sse`     | GET  | SSE 流（MCP transport）        |
| `/message` | POST | JSON-RPC 2.0 请求端点         |
| `/health`  | GET  | 健康检查（返回 `200 OK`）      |

## MCP Tools（44 个）

### SDK 信息（3 个）

| Tool               | 同步/异步 | 参数                                      | 说明                                            |
|--------------------|----------|-------------------------------------------|------------------------------------------------|
| `sdk_info`         | Sync     | —                                         | 获取 SDK 运行时信息（版本、状态、配置）          |
| `sdk_token_update` | Async    | `userSig` (必填)                           | 刷新 userSig，结果通过 `sdk-event` SSE 返回    |
| `sdk_get_user_sig` | Sync     | `apiKey` (必填), `duration`               | 通过 apiKey 获取 userSig（可在 sdk_init 前调用）|

### 浏览器控制（5 个）

| Tool              | 同步/异步 | 参数                                       | 说明                                           |
|-------------------|----------|--------------------------------------------|-----------------------------------------------|
| `browser_install` | Async    | `channel`                                  | 安装/更新浏览器内核；进度通过 `sdk-event` SSE 推送 |
| `browser_info`    | Sync     | —                                          | 列出当前运行的浏览器环境                         |
| `browser_open`    | Async    | `envId` (必填), `urls`, `args`             | 打开浏览器环境；`browser-open-success` 事件表示 CDP 就绪 |
| `browser_close`   | Async    | `envId` (必填)                             | 关闭浏览器环境                                   |
| `browser_command` | Sync     | `envId`, `method` (必填), `params`, `sessionId` | 发送原始 CDP 命令到浏览器                       |

### 浏览器高级操作（31 个）

#### 页面导航

| Tool              | 参数                                         | 说明                                              |
|-------------------|---------------------------------------------|---------------------------------------------------|
| `browser_navigate`| `envId`, `url` (必填)                        | 打开 URL，返回 `{targetId, sessionId}`             |
| `browser_snapshot`| `envId` (必填), `sessionId`                  | 获取页面无障碍树，含 `backendDOMNodeId`，供 `_ref` 工具使用 |

#### 鼠标操作

| Tool               | 参数                                         | 说明                        |
|--------------------|---------------------------------------------|-----------------------------|
| `browser_click`    | `envId`, `selector` (必填), `sessionId`      | CSS 选择器点击              |
| `browser_click_ref`| `envId`, `ref` (必填), `sessionId`           | 通过 snapshot ref 点击       |
| `browser_dblclick` | `envId`, `selector` (必填), `sessionId`      | CSS 选择器双击              |
| `browser_hover`    | `envId`, `selector` (必填), `sessionId`      | CSS 选择器悬停              |
| `browser_hover_ref`| `envId`, `ref` (必填), `sessionId`           | 通过 snapshot ref 悬停      |

#### 键盘/输入操作

| Tool                        | 参数                                              | 说明                              |
|-----------------------------|--------------------------------------------------|-----------------------------------|
| `browser_type`              | `envId`, `selector`, `text` (必填), `sessionId`   | CSS 选择器输入（追加）             |
| `browser_type_ref`          | `envId`, `ref`, `text` (必填), `sessionId`        | 通过 snapshot ref 输入（追加）     |
| `browser_fill`              | `envId`, `selector`, `text` (必填), `sessionId`   | CSS 选择器清除后输入               |
| `browser_fill_ref`          | `envId`, `ref`, `text` (必填), `sessionId`        | 通过 snapshot ref 清除后输入       |
| `browser_press_key`         | `envId`, `key` (必填), `sessionId`                | 按键（Enter、Escape、Tab 等）      |
| `browser_keyboard_type`     | `envId`, `text` (必填), `sessionId`               | 逐字符键入                         |
| `browser_keyboard_insert_text` | `envId`, `text` (必填), `sessionId`            | Input.insertText 插入文本          |
| `browser_key_down`          | `envId`, `key` (必填), `sessionId`                | keyDown 事件                      |
| `browser_key_up`            | `envId`, `key` (必填), `sessionId`                | keyUp 事件                        |

#### 焦点/选择

| Tool                   | 参数                                              | 说明                      |
|------------------------|--------------------------------------------------|---------------------------|
| `browser_focus`        | `envId`, `selector` (必填), `sessionId`           | CSS 选择器聚焦            |
| `browser_focus_ref`    | `envId`, `ref` (必填), `sessionId`                | 通过 snapshot ref 聚焦    |
| `browser_select_option`| `envId`, `selector`, `value` (必填), `sessionId`  | `<select>` 设置值         |
| `browser_select_option_ref`| `envId`, `ref`, `value` (必填), `sessionId`   | 通过 snapshot ref 设置 select 值 |
| `browser_check`        | `envId`, `selector` (必填), `sessionId`           | 勾选 checkbox/radio       |
| `browser_check_ref`    | `envId`, `ref` (必填), `sessionId`                | 通过 snapshot ref 勾选    |
| `browser_uncheck`      | `envId`, `selector` (必填), `sessionId`           | 取消勾选 checkbox         |

#### 滚动/拖拽

| Tool                    | 参数                                                    | 说明                      |
|-------------------------|--------------------------------------------------------|---------------------------|
| `browser_scroll`        | `envId`, `direction` (必填), `px`, `selector`, `sessionId` | 滚动页面或元素            |
| `browser_scroll_into_view` | `envId`, `selector` (必填), `sessionId`             | 滚动元素到视口            |
| `browser_drag`          | `envId`, `sourceSelector`, `targetSelector` (必填), `sessionId` | 拖拽操作     |

#### 文件/截图/PDF

| Tool                | 参数                                                             | 说明                      |
|---------------------|-----------------------------------------------------------------|---------------------------|
| `browser_upload_file`| `envId`, `selector`, `files` (必填), `sessionId`                | 上传文件                  |
| `browser_screenshot` | `envId` (必填), `path`, `format`, `quality`, `fullPage`, `sessionId` | 截图，支持全页/JPEG      |
| `browser_pdf`       | `envId`, `path` (必填), `sessionId`                              | 生成 PDF                  |

#### 文本查找/脚本

| Tool                    | 参数                                              | 说明                                     |
|-------------------------|--------------------------------------------------|------------------------------------------|
| `browser_find_click_text`| `envId`, `text` (必填), `sessionId`             | 按可见文本查找并点击                      |
| `browser_evaluate`      | `envId`, `expression` (必填), `sessionId`        | 执行 JavaScript 表达式并返回结果           |

### 环境管理（5 个）

| Tool          | 同步/异步 | 参数                                       | 说明                            |
|---------------|----------|--------------------------------------------|---------------------------------|
| `env_create`  | Sync     | `name`, `os`, `body`                       | 创建指纹浏览器环境               |
| `env_page`    | Sync     | `page`, `pageSize`, `body`                 | 查询/列出环境（支持分页）        |
| `env_update`  | Sync     | `envId` (必填), `body`                     | 更新环境配置                    |
| `env_destroy` | Sync     | `envId` (必填)                             | 永久删除环境及所有数据          |
| `env_getinfo` | Sync     | `envId` (必填)                             | 获取单个环境详情                 |

## Ref 定位机制

部分浏览器操作提供两种定位方式：CSS 选择器（`browser_click`）和 accessibility ref（`browser_click_ref`）。

**工作流**：

```
1. browser_snapshot  →  获取页面无障碍树 (AX Tree)
   返回: {"nodes": [{"name": {"value": "Target Button"}, "backendDOMNodeId": 42, ...}]}

2. LLM 从 snapshot 中选择目标元素的 backendDOMNodeId

3. browser_click_ref(envId, ref="42")  →  直接操作 DOM 节点
```

**适用场景**：
- 🔴 复杂 DOM 结构——CSS 选择器长且易出错
- 🟡 多次操作同一页面——snapshot 成本摊薄
- 🟢 AI 需要理解页面结构——snapshot 本身提供上下文

**已实现 `_ref` 变体的工具**（7 个）：

| 基础工具                | ref 变体                  | 说明               |
|------------------------|--------------------------|--------------------|
| `browser_click`        | `browser_click_ref`       | 点击               |
| `browser_type`         | `browser_type_ref`        | 输入（追加）        |
| `browser_fill`         | `browser_fill_ref`        | 清除后输入          |
| `browser_hover`        | `browser_hover_ref`       | 悬停               |
| `browser_focus`        | `browser_focus_ref`       | 聚焦               |
| `browser_select_option`| `browser_select_option_ref`| 设置 select 值     |
| `browser_check`        | `browser_check_ref`       | 勾选 checkbox/radio|

## browser_command — CDP 命令参考

`browser_command` 是透明 CDP 代理，通过 WebSocket 连接浏览器 DevTools 调试端口，
直接转发 CDP 请求/响应。**日常操作推荐使用上方的 `browser_*` 高层工具**。

### 工作原理

```
browser_open → browser-open-success → 提取 remoteDebuggingPort
    → /json/version → browser WebSocket URL
    → WebSocket 连接池 → CDP 请求/响应转发
```

| 步骤 | 说明 |
|------|------|
| 1 | `browser_open` 自动注入 `--remote-debugging-port=0`，浏览器随机分配端口 |
| 2 | SSE `browser-open-success` 事件携带 `remoteDebuggingPort` |
| 3 | CDP 代理访问 `http://127.0.0.1:<port>/json/version` 获取 WebSocket URL |
| 4 | 建立 WebSocket 连接池（按 envId 缓存复用） |
| 5 | 后续 `browser_command` 复用已建立的连接 |

### 参数

| 参数        | 必填   | 类型   | 说明                                                         |
|------------|--------|--------|--------------------------------------------------------------|
| `envId`    | 是     | string | 目标浏览器环境 ID                                             |
| `method`   | 是     | string | CDP 方法名，如 `Page.navigate`、`Runtime.evaluate`            |
| `params`   | 否     | object | CDP 命令参数                                                  |
| `sessionId`| 否     | string | CDP session ID（用于 session 域命令）                         |
| `body`     | 否     | string | 原始 JSON body（提供后覆盖 method/params/sessionId）          |

### 高层工具 vs CDP 命令对照

| 场景           | 推荐方式                                           |
|---------------|---------------------------------------------------|
| 导航到 URL     | `browser_navigate`                                |
| 点击元素       | `browser_click` / `browser_click_ref`             |
| 填写输入框     | `browser_fill` / `browser_fill_ref`               |
| 截图           | `browser_screenshot`                               |
| 自定义 CDP 命令 | `browser_command`                                  |
| 执行 JS        | `browser_evaluate`                                 |

## 异步模式

异步工具（`browser_open`、`browser_close`、`browser_install`、`sdk_token_update`）
立即返回 `reqId`：

```json
{"reqId": 42, "envId": "abc123"}
```

最终结果以 SSE 事件推送：

```
event: sdk-event
data: {"code":100,"data":"{\"type\":\"browser-open-success\",\"reqId\":42,...}"}
```

### SSE 事件类型

| 事件类型                  | code | 含义              |
|---------------------------|------|-------------------|
| `browser-open-success`    | 0    | 浏览器已打开，CDP 就绪 |
| `browser-close-success`   | 0    | 浏览器已关闭        |
| `token-update-success`    | 0    | Token 刷新成功      |
| `install-success`         | 0    | 内核安装完成        |
| 其他                      | 非 0 | 错误或警告          |

SSE 连接每 15 秒发送 `: ping` 心跳保持连接。

## Claude Desktop / CodeBuddy 配置示例

```json
{
  "mcpServers": {
    "brosdk": {
      "url": "http://localhost:8765/sse"
    }
  }
}
```

## E2E 测试

测试文件按功能拆分为 8 个文件，方便单独运行：

| 文件 | 测试函数 | 覆盖内容 |
|------|---------|---------|
| `e2e_test.go` | — | 共享测试基础设施（JSON-RPC 客户端、SSE reader、fixture） |
| `e2e_basic_test.go` | `TestE2E_AllTools` | SDK 初始化 → 环境管理 → 浏览器开闭 → CDP 命令 |
| | `TestE2E_CDPFormInteraction` | navigate → fill → click → evaluate 完整表单交互 |
| `e2e_snapshot_test.go` | `TestE2E_SnapshotClickRef` | snapshot → 提取 ref → click_ref → evaluate 验证 |
| `e2e_form_test.go` | `TestE2E_FormElements` | 9 个表单元素操作：focus/type/fill/select/check + ref 变体 |
| `e2e_keyboard_test.go` | `TestE2E_KeyboardInteraction` | press_key/keyboard_type/insert_text/key_down/key_up |
| `e2e_mouse_test.go` | `TestE2E_MouseInteraction` | dblclick/hover/hover_ref/find_click_text |
| `e2e_scroll_test.go` | `TestE2E_ScrollAndScreenshot` | scroll/scroll_into_view/screenshot/PDF |
| `e2e_drag_test.go` | `TestE2E_DragAndUpload` | drag/upload_file |

```bash
# 运行全部 e2e 测试（需 Windows + DLL + 有效 apiKey）
go test -v -run TestE2E -timeout 600s

# 单独运行某个测试文件
go test -v -run TestE2E_FormElements -timeout 300s .
go test -v -run TestE2E_KeyboardInteraction -timeout 300s .
go test -v -run TestE2E_SnapshotClickRef -timeout 300s .
```

测试覆盖 44 个 MCP tools 中的 43 个（97.7%），仅 `browser_install`（纯异步、耗时过长）未覆盖。

## 目录结构

```
brosdk-mcp-go/
├── main.go                        # 入口、CLI 参数、优雅关闭
├── go.mod
├── README.md
├── README_EN.md
├── docs/
│   └── tools-reference.md         # 44 个 MCP Tool API 参考
├── e2e_test.go                    # E2E 共享基础设施（类型、fixture、helper）
├── e2e_basic_test.go              # E2E: SDK 基础 + CDP 表单测试
├── e2e_snapshot_test.go           # E2E: snapshot + click_ref 工作流
├── e2e_form_test.go               # E2E: 表单元素交互
├── e2e_keyboard_test.go           # E2E: 键盘交互
├── e2e_mouse_test.go              # E2E: 鼠标交互
├── e2e_scroll_test.go             # E2E: 滚动 + 截图 + PDF
├── e2e_drag_test.go               # E2E: 拖拽 + 文件上传
├── libs/
│   ├── brosdk.h                   # C 头文件（参考）
│   └── windows-x64/
│       └── brosdk.dll             # 原生 SDK 库
└── internal/
    ├── brosdk/                    # 原生 SDK 绑定 + chromedp 浏览器操作
    │   ├── native.go              # nativeLib 接口定义
    │   ├── native_windows.go      # Windows syscall.LazyDLL 实现
    │   ├── native_unsupported.go  # 非 Windows 桩实现
    │   ├── http.go                # HTTP 客户端：fetchUserSig
    │   ├── manager.go             # 高级 Go API（Manager 单例 + 事件发布）
    │   ├── types.go               # 数据类型 + JSON 构造器 + CDP 类型
    │   ├── errors.go              # 错误辅助函数
    │   ├── cdp.go                 # CDP WebSocket 代理（仅 Windows）
    │   ├── cdp_unsupported.go     # CDP 桩（非 Windows）
    │   ├── actions.go             # chromedp 高层浏览器操作（仅 Windows）
    │   └── actions_unsupported.go # actions 桩（非 Windows）
    ├── config/
    │   └── config.go              # 启动配置加载（config.local.json → config.json）
    ├── mcp/
    │   └── server.go              # MCP SSE 服务器（JSON-RPC 2.0 + 广播）
    └── tools/
        └── tools.go               # 44 个 Tool 定义 + handler dispatch
```

## 关键设计

| 设计点              | 说明                                                             |
|---------------------|------------------------------------------------------------------|
| **无 CGo**          | 纯 `syscall.LazyDLL` 动态加载，参考 `brosdk-go`                   |
| **纯 GOOS 约束**    | `//go:build windows` / `//go:build !windows`，无 build tag       |
| **SDK 单例**        | `Manager` + `sync.RWMutex` 管理                                  |
| **异步回调桥接**    | C `result_callback` → Go `emit()` → SSE Broadcast `sdk-event`    |
| **chromedp 操作**   | 基于 `github.com/chromedp/chromedp`（v0.15.1），使用原生 Action 类型 |
| **AX Tree ref 定位**| `browser_snapshot` → `backendDOMNodeId` → `browser_*_ref` 精准定位 |
| **CDP 连接池**      | 按 `envId` 缓存 WebSocket 连接，断线自动重连（最多一次）          |
| **自动 userSig**    | `apiKey` 传入时，`Init()` 自动调用 BroSDK HTTP API 获取 userSig   |
| **自动调试端口**    | `browser_open` 始终注入 `--remote-debugging-port=0`              |

## 依赖

| 依赖                        | 用途                      |
|-----------------------------|---------------------------|
| `github.com/chromedp/chromedp` v0.15.1 | 高层浏览器操作（click/type/fill 等） |
| `github.com/chromedp/cdproto`          | CDP 协议类型 + 低级操作     |
| `github.com/gorilla/websocket`         | CDP WebSocket 连接         |

## 相关项目

| 仓库                                                   | 说明                   |
|--------------------------------------------------------|------------------------|
| [brosdk](https://github.com/browsersdk/brosdk)         | 原生 C/C++ SDK        |
| [brosdk-go](https://github.com/browsersdk/brosdk-go)   | Go 语言原生绑定         |
| [brosdk-mcp](https://github.com/browsersdk/brosdk-mcp) | TypeScript MCP Server |
| [brosdk-core](https://github.com/browsersdk/brosdk-core) | 浏览器内核 + 版本     |

## License

MIT

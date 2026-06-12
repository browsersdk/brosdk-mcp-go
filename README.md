# brosdk-mcp-go

[English](README_EN.md) | 简体中文

将 BroSDK 指纹浏览器 SDK 封装为 [MCP (Model Context Protocol)](https://modelcontextprotocol.io/) SSE 服务，
让 AI Agent（Claude、CodeBuddy 等）可以直接操控指纹浏览器环境。

## 文档导航

> AI Agent 入口：从本文件开始。以下是各文档的用途速查。

| 文档 | 用途 | 何时读 |
|------|------|--------|
| **README.md**（本文件） | 项目总览、API 速查、配置、运行 | 首次了解项目 |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | 架构设计、技术栈、设计决策 | 理解实现原理 / 贡献代码 |
| [docs/tools-reference.md](docs/tools-reference.md) | 72 个 MCP Tool 完整 API 参考 | 查找特定 tool 的参数/返回值 |

## 功能概览

- 通过 72 个 MCP Tool 暴露 BroSDK 全部能力：SDK 生命周期、浏览器控制、浏览器高级操作、环境 CRUD、**浏览器录制回放**
- 基于 [chromedp](https://github.com/chromedp/chromedp) 的高层浏览器操作——点击、输入、截图、PDF 等，无需手写 CDP 命令
- 保留 `browser_command` 透明 CDP 代理，支持所有 DevTools 命令的高级/自定义场景
- **录制回放**：`record_start` → 操作（自动捕获）→ `record_stop` 自动保存场景，`scene_replay` 一键回放，支持 `{{变量}}` 替换
- 异步工具通过 SSE 事件回传结果，支持长时间等待
- 双配置文件（`config.json` / `config.local.json`）+ 环境变量覆盖，启动时**必须提供 apiKey**
- 跨平台：Windows / macOS 原生支持，首次运行自动下载对应动态库

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

## 前置要求

- **Windows** x64 / **macOS** arm64
- Go 1.26+
- brosdk 原生库 — 首次运行**自动从 GitHub Releases 下载**，无需手动准备

## 编译

```bash
# Windows / macOS — 生产编译
go build -o brosdk-mcp .
```

build constraint：`//go:build windows` / `//go:build darwin` / `//go:build !windows && !darwin`。

## 配置

启动时服务器依次读取 `config.local.json` → `config.json`（前者优先级更高）。
**必须提供有效的 `apiKey`**，缺失则启动失败。

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
| `apiKey`  | **是**  | —              | 客户 ID（**必须提供**）；未提供 `userSig` 时自动通过 API 获取 |
| `userSig` | 否     | —              | 预计算的签名（优先级高于 apiKey）             |
| `workDir` | 否     | `./brosdk`     | SDK 工作目录（自动创建）                      |
| `port`    | 否     | `5811`         | SDK 浏览器控制端口                            |
| `debug`   | 否     | `false`        | 是否启用 SDK 调试日志                        |

\* `apiKey` 或 `userSig` 二选一必须提供。

### 环境变量覆盖

所有配置字段均可通过 `BROSDK_*` 环境变量覆盖，优先级为：

```
CLI flag > 环境变量 > 配置文件 > 默认值
```

| 环境变量            | 对应字段       | 示例                          |
|--------------------|----------------|-------------------------------|
| `BROSDK_API_KEY`   | `apiKey`       | `BROSDK_API_KEY=your-id`      |
| `BROSDK_USER_SIG`  | `userSig`      | `BROSDK_USER_SIG=sig-value`   |
| `BROSDK_WORK_DIR`  | `workDir`      | `BROSDK_WORK_DIR=/data/sdk`   |
| `BROSDK_PORT`      | `port`         | `BROSDK_PORT=5811`            |
| `BROSDK_SDK_API_URL`| `sdkApiUrl`   | `BROSDK_SDK_API_URL=https://` |
| `BROSDK_DEBUG`     | `debug`        | `BROSDK_DEBUG=true`           |
| `BROSDK_LIB`       | `-lib` flag    | `BROSDK_LIB=./libs/sdk.dll`   |
| `BROSDK_ADDR`      | `-addr` flag   | `BROSDK_ADDR=:9000`           |

无配置文件时也可纯通过环境变量启动，例如：

```bash
BROSDK_API_KEY=your-id BROSDK_ADDR=:8765 brosdk-mcp
```

### 本地开发覆盖

复制 `config.json` → `config.local.json`，填入不同凭据；
本地文件不纳入版本控制且优先于 `config.json`。

## 运行

```bash
# 首次运行 — 自动下载对应平台的 brosdk 动态库
brosdk-mcp -addr :8765

# 手动指定本地库路径
brosdk-mcp -lib ./libs/darwin-arm64/libbrosdk.dylib -addr :8765
```

### 命令行参数

| 参数    | 默认值                           | 说明                        |
|---------|---------------------------------|------------------------------|
| `-lib`  | （自动下载）                      | brosdk 原生库路径             |
| `-addr` | `:8765`                         | HTTP 监听地址                |

### 动态库自动下载

`-lib` 未指定时，按以下顺序查找：
1. `./brosdk.dll`（Windows）或 `./libbrosdk.dylib`（macOS）
2. `libs/<platform>/` 目录下的本地库
3. 自动从 [GitHub Releases](https://github.com/browsersdk/brosdk/releases) 下载最新版本

下载时控制台输出实时进度，下载完成后解压到 `libs/<platform>/` 目录。

## 端点

| 端点        | 方法            | 说明                          |
|------------|----------------|--------------------------------|
| `/inspector` | GET           | 内嵌 MCP Inspector Web UI     |
| `/mcp`     | POST/GET/DELETE | Streamable HTTP transport（MCP 2025-03-26）|
| `/sse`     | GET             | SSE 流（MCP 2024-11-05，旧版兼容）|
| `/message` | POST            | JSON-RPC 2.0 请求端点（旧版兼容）|
| `/health`  | GET             | 健康检查（返回 `200 OK`）      |

## MCP Tools（72 个）

### SDK 信息（3 个）

| Tool               | 同步/异步 | 参数                                      | 说明                                            |
|--------------------|----------|-------------------------------------------|------------------------------------------------|
| `sdk_info`         | Sync     | —                                         | 获取 SDK 运行时信息（版本、状态、配置）          |
| `sdk_token_update` | Async    | `userSig` (必填)                           | 刷新 userSig，结果通过 `sdk-event` SSE 返回    |
| `sdk_get_user_sig` | Sync     | `apiKey` (必填), `duration`               | 通过 apiKey 获取 userSig（可在 sdk_init 前调用）|

### 浏览器控制（6 个）

| Tool              | 同步/异步 | 参数                                       | 说明                                           |
|-------------------|----------|--------------------------------------------|-----------------------------------------------|
| `browser_install` | Async    | `channel`                                  | 安装/更新浏览器内核；进度通过 `sdk-event` SSE 推送 |
| `browser_info`    | Sync     | —                                          | 列出当前运行的浏览器环境                         |
| `browser_open`    | Async    | `envId` (必填), `urls`, `args`             | 打开浏览器环境；`browser-open-success` 事件表示 CDP 就绪；成功后自动设为激活环境 |
| `browser_close`   | Async    | `envId` (必填)                             | 关闭浏览器环境                                   |
| `browser_select`   | Sync     | `envId` (必填)                             | **设置激活环境**；后续 Browser Actions 未指定 `envId` 时自动使用此环境 |
| `browser_command` | Sync     | `envId`, `method` (必填), `params`, `sessionId` | 发送原始 CDP 命令到浏览器                       |

### 浏览器高级操作（50 个）

> **`envId` 参数说明**：所有 Browser Actions 的 `envId` 参数现改为**可选**。
> 先用 `browser_select` 设置激活环境，后续操作可省略 `envId`。
> 显式传入 `envId` 仍受支持，会覆盖激活环境。

#### 页面导航

| Tool              | 参数                                         | 说明                                              |
|-------------------|---------------------------------------------|---------------------------------------------------|
| `browser_navigate`| `envId`, `url` (必填)                        | 打开 URL，返回 `{targetId, sessionId}`             |
| `browser_reload`  | `envId`, `sessionId`                        | 重新加载当前页面（envId 可省略，使用激活环境）    |
| `browser_back`    | `envId`, `sessionId`                        | 浏览器后退（envId 可省略，使用激活环境）          |
| `browser_forward` | `envId`, `sessionId`                        | 浏览器前进（envId 可省略，使用激活环境）          |
| `browser_snapshot`| `envId`, `sessionId`                        | 获取页面无障碍树，含 `backendDOMNodeId`，供 `_ref` 工具使用（envId 可省略） |

#### Tab 管理

| Tool                 | 参数                                         | 说明                                              |
|----------------------|---------------------------------------------|---------------------------------------------------|
| `browser_new_tab`    | `envId`                                      | 创建新空白 Tab，返回 `{tabId}`；新 tab 自动成为活跃 tab |
| `browser_close_tab`  | `envId`, `tabId` (必填)                      | 关闭指定 tab（`"__active__"` 表示当前活跃 tab）        |
| `browser_list_tabs`  | `envId`                                      | 列出所有打开的 tab，返回 `[{tabId, title, url, isActive}]` |

#### 页面内容

| Tool                 | 参数                                         | 说明                                              |
|----------------------|---------------------------------------------|---------------------------------------------------|
| `browser_get_html`   | `envId`                                      | 获取当前页面完整 HTML 源码；内容可能很大，优先用 `browser_snapshot` 或 `browser_get_text` |

#### Cookie 管理

| Tool                    | 参数                                         | 说明                                              |
|-------------------------|---------------------------------------------|---------------------------------------------------|
| `browser_get_cookies`   | `envId`, `urls`                              | 获取当前 tab 的 cookies；可选按 URL 过滤              |
| `browser_set_cookies`   | `envId`, `cookies` (必填)                    | 设置 cookies，每项支持 `{name, value, url, domain, path, secure, httpOnly}` |

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
| `browser_insert_text` | `envId`, `text` (必填), `sessionId`            | Input.insertText 插入文本          |
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
| `browser_uncheck_ref`  | `envId`, `ref` (必填), `sessionId`                | 通过 snapshot ref 取消勾选 |

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
| `browser_screenshot` | `envId`, `path`, `dir`, `screenshotDir`, `format`, `quality`, `fullPage`, `sessionId` | 截图，默认保存至 workDir/screenshots/，返回绝对路径；`screenshotDir` 是 `dir` 的兼容别名 |
| `browser_pdf`       | `envId`, `path`, `sessionId`                              | 生成 PDF，默认保存至 workDir/pdfs/output.pdf，返回绝对路径（envId 可省略，使用激活环境） |

#### 文本查找/脚本

| Tool                    | 参数                                              | 说明                                     |
|-------------------------|--------------------------------------------------|------------------------------------------|
| `browser_find_click_text`| `envId`, `text` (必填), `sessionId`             | 按可见文本查找并点击                      |
| `browser_get_text`    | `envId`, `selector` (必填), `sessionId`          | 获取元素可见文本内容                        |
| `browser_get_value`   | `envId`, `selector` (必填), `sessionId`          | 获取 input 元素的 value 属性               |
| `browser_evaluate`      | `envId`, `expression` (必填), `sessionId`        | 执行 JavaScript 表达式并返回结果           |

#### Agent 辅助工具

| Tool                    | 参数                                              | 说明                                     |
|-------------------------|--------------------------------------------------|------------------------------------------|
| `browser_find_ref`      | `envId`, `role`, `name`, `value`, `limit`   | 搜索 AX 树元素并返回 ref 列表（比完整 snapshot 小 80%） |
| `browser_wait`          | `envId`, `text`, `role`, `name`, `selector`, `timeout`, `waitFor`, `delay` | 等待条件满足；`waitFor` 支持 `navigation`（页面 readyState）、`selector`（CSS 出现）、`text`（文本出现）、`time`（固定毫秒） |
| `browser_page_state`    | `envId`, `sessionId`                        | 返回 `{readyState, title, url}` 三元组，判断页面加载状态 |
| `browser_exists`        | `envId`, `role`, `name`, `value`, `selector`, `sessionId` | 快速检查元素是否存在，返回 boolean |
| `browser_dialog`        | `envId`, `action`, `sessionId` | 读取/接受/取消浏览器弹窗（alert/confirm/prompt） |
| `browser_fill_form`     | `envId`, `fields` (必填), `submitSelector`   | 批量填充多字段表单；`fields` 为 CSS selector 到 value 的对象 |

### 环境管理（5 个）

| Tool          | 同步/异步 | 参数                                       | 说明                            |
|---------------|----------|--------------------------------------------|---------------------------------|
| `env_create`  | Sync     | `name`, `os`, `body`                       | 创建指纹浏览器环境               |
| `env_page`    | Sync     | `page`, `pageSize`, `body`                 | 查询/列出环境（支持分页）        |
| `env_update`  | Sync     | `envId` (必填), `body`                     | 更新环境配置                    |
| `env_destroy` | Sync     | `envId` (必填)                             | 永久删除环境及所有数据          |
| `env_getinfo` | Sync     | `envId` (必填)                             | 获取单个环境详情                 |

### 录制回放（9 个）

| Tool           | 同步/异步 | 参数                                       | 说明                            |
|----------------|----------|--------------------------------------------|---------------------------------|
| `record_start` | Sync     | —                                          | 开始录制，后续操作自动捕获       |
| `record_stop`  | Sync     | `name` (必填), `description`               | 停止录制并自动保存为 `scenes/{name}.json`；同名场景会直接覆盖 |
| `record_status`| Sync     | —                                          | 查看当前录制状态                 |
| `scene_list`   | Sync     | —                                          | 列出所有已保存场景               |
| `scene_get`    | Sync     | `name` (必填)                              | 查看场景详情（步骤 JSON）        |
| `scene_update` | Sync     | `name` (必填), `scene` (必填)              | 编辑已保存场景的步骤和元数据     |
| `scene_delete` | Sync     | `name` (必填)                              | 删除场景文件                     |
| `scene_replay` | Sync     | `name` (必填), `envId`, `variables`, `stopOnError`, `stepDelay`, `applyHumanDelay` | 加载场景并逐步回放。WaitFor 守卫确保每步完成后再执行下一步，支持 `{{变量}}` 替换 |
| `scene_save`   | Sync     | `name` (必填), `scene` (必填)              | 手动保存完整场景对象（通常由 `record_stop` 自动保存，极少直接使用） |

#### 录制回放工作流

```
1. record_start()                                    → 开始录制
2. browser_navigate/browser_click/browser_fill...    → 操作自动捕获（自动推断 WaitFor 守卫）
3. record_stop({name:"login_flow"})                  → 自动保存到 workDir/scenes/login_flow.json

// 方式 A：显式传 envId（向后兼容）
4a. scene_replay({name:"login_flow", envId:"env-2",
      variables:{"username":"alice","password":"s3cret"}})

// 方式 B：先选环境，后续省略 envId
4b. browser_select({envId:"env-2"})                → 设为激活环境
    scene_replay({name:"login_flow",                → envId 省略，使用激活环境
      variables:{"username":"alice","password":"s3cret"}})

// 快速回放（跳过 human delay）
5. scene_replay({name:"login_flow",
      applyHumanDelay: false, stepDelay: 100})
```

- 录制时自动去除 `envId`/`sessionId`，回放时由调用方注入
- 步骤中可使用 `{{变量名}}` 占位符，回放时替换为实际值
- 最大 200 步，步骤间默认 500ms 延迟
- 场景名称仅支持字母、数字、`.`、`_`、`-`，避免写入 scenes 目录之外；`record_stop` 会直接覆盖同名场景，方便 Agent 反复录制迭代
- 场景文件为 JSON 格式，可读可 git 管理
- **WaitFor 守卫**：navigate/click 等步骤自动推断完成条件（`readyState:complete`），回放阻塞等待而非盲等
- WaitFor 守卫失败不会中断成功步骤，会在回放结果的 `guardWarn` 字段中返回警告
- **HumanDelay**：录制时自动捕获操作之间的人类停顿（上限 3s），回放默认启用（`applyHumanDelay: true`），可关闭加速

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

**已实现 `_ref` 变体的工具**（8 个）：

| 基础工具                | ref 变体                  | 说明               |
|------------------------|--------------------------|--------------------|
| `browser_click`        | `browser_click_ref`       | 点击               |
| `browser_type`         | `browser_type_ref`        | 输入（追加）        |
| `browser_fill`         | `browser_fill_ref`        | 清除后输入          |
| `browser_hover`        | `browser_hover_ref`       | 悬停               |
| `browser_focus`        | `browser_focus_ref`       | 聚焦               |
| `browser_select_option`| `browser_select_option_ref`| 设置 select 值     |
| `browser_check`        | `browser_check_ref`       | 勾选 checkbox/radio|
| `browser_uncheck`      | `browser_uncheck_ref`     | 取消勾选            |

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

`body` 示例：`{"method":"Runtime.evaluate","params":{"expression":"document.title"},"sessionId":"..."}`。

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

测试文件按功能拆分为 9 个文件，方便单独运行：

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
| `e2e_tab_test.go` | `TestE2E_TabManagement` | new_tab/list_tabs/close_tab/get_html |
| `e2e_record_test.go` | `TestE2E_RecordReplay_FullFlow` | record_start → 操作 → record_stop → scene_replay |
| | `TestE2E_RecordReplay_VariableSubstitution` | 变量替换：`{{username}}`/`{{password}}` |
| | `TestE2E_RecordReplay_StopOnError` | stopOnError 行为 |

```bash
# 运行全部 e2e 测试（需 Windows + DLL + 有效 apiKey）
go test -v -run TestE2E -timeout 600s

# 单独运行某个测试文件
go test -v -run TestE2E_FormElements -timeout 300s .
go test -v -run TestE2E_KeyboardInteraction -timeout 300s .
go test -v -run TestE2E_SnapshotClickRef -timeout 300s .
go test -v -run TestE2E_TabManagement -timeout 300s .
```

测试覆盖 72 个 MCP tools。仅 `browser_install`（纯异步、耗时过长）未纳入 E2E。

## 目录结构

```
brosdk-mcp-go/
├── main.go                        # 入口、CLI 参数、优雅关闭
├── go.mod
├── README.md
├── README_EN.md
├── docs/
│   ├── ARCHITECTURE.md             # 架构设计 + 技术决策
│   └── tools-reference.md          # 72 个 MCP Tool API 参考
├── e2e_test.go                    # E2E 共享基础设施（类型、fixture、helper）
├── e2e_record_test.go            # E2E: 录制回放完整流程
├── e2e_tab_test.go               # E2E: Tab 管理 + get_html
├── libs/
│   ├── brosdk.h                    # C 头文件（参考）
│   ├── windows-x64/
│   │   └── brosdk.dll              # Windows x64 原生库
│   └── darwin-arm64/
│       └── libbrosdk.dylib         # macOS arm64 原生库（自动下载）
└── internal/
    ├── brosdk/                    # 原生 SDK 绑定 + chromedp 浏览器操作
    │   ├── native.go              # nativeLib 接口定义
    │   ├── native_windows.go      # Windows syscall.LazyDLL 实现
    │   ├── native_darwin.go       # macOS CGo dlopen/dlsym 实现
    │   ├── native_unsupported.go  # 其他平台桩实现
    │   ├── download.go            # GitHub Releases 自动下载
    │   ├── http.go                # HTTP 客户端：fetchUserSig
    │   ├── manager.go             # 高级 Go API（Manager 单例 + 事件发布）
    │   ├── types.go               # 数据类型 + JSON 构造器 + CDP 类型
    │   ├── errors.go              # 错误辅助函数
    │   ├── cdp.go                 # CDP WebSocket 代理（Windows / macOS）
    │   ├── cdp_unsupported.go     # CDP 桩（其他平台）
    │   ├── actions.go             # chromedp 高层浏览器操作（Windows / macOS）
    │   └── actions_unsupported.go # actions 桩（其他平台）
    ├── config/
    │   └── config.go              # 启动配置加载（config.local.json → config.json + BROSDK_* 环境变量）
    ├── mcp/
    │   ├── server.go              # MCP 服务器（JSON-RPC 2.0 + SSE/Streamable HTTP + 广播）
    │   ├── streamable.go          # Streamable HTTP transport（MCP 2025-03-26）
    │   ├── inspector.go           # go:embed 加载器
    │   └── inspector.html         # 内嵌 MCP Inspector Web UI（独立 HTML 文件）
    └── tools/
        └── tools.go               # 72 个 Tool 定义 + handler dispatch + Recorder hook
    └── recorder/
        ├── recorder.go            # 录制单例（start/stop/capture/sanitize）
        ├── player.go              # 回放引擎（步骤执行 + 变量替换）
        └── guard.go               # WaitFor 守卫（readyState/exists 完成检测）
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
| **录制回放 Hook 模式**| Handler 层自动捕获所有 browser action，Dispatch 与 Player 共享同一 switch |
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

# brosdk-mcp-go 项目规划

> 基于 BroSDK 指纹浏览器环境的 MCP 服务，仅支持 SSE transport。

## 目标

将 BroSDK 原生库（`brosdk.dll` / `brosdk.dylib`）的能力通过标准 MCP 协议暴露为 SSE 服务，让 Claude、Agent IDE 等 MCP 客户端可直接控制指纹浏览器环境。

---

## 目录结构

```
brosdk-mcp-go/
├── main.go                  # 入口：SSE server 启动
├── go.mod
├── go.sum
│
├── internal/
│   ├── sdk/                 # CGo FFI 封装层
│   │   ├── sdk.go           # SDK 生命周期（load/init/shutdown）
│   │   ├── browser.go       # browser_open / browser_close / browser_info / browser_command
│   │   ├── env.go           # env_create / env_page / env_update / env_destroy / env_getinfo
│   │   └── callback.go      # result_callback / cookies_storage_callback 桥接到 Go channel
│   │
│   ├── mcp/                 # MCP 协议层
│   │   ├── server.go        # SSE server（/sse + /message endpoints）
│   │   ├── handler.go       # JSON-RPC 请求分发
│   │   ├── tools.go         # 所有 Tool 定义（schema / description）
│   │   └── session.go       # SSE session 管理（client 连接 / 断开）
│   │
│   └── config/
│       └── config.go        # 配置加载（env / flags）
│
├── libs/                    # 原生库（与 brosdk-rust 同结构）
│   ├── windows-x64/brosdk.dll
│   └── macos-arm64/brosdk.dylib
│
└── docs/
    └── tools.md             # MCP Tool 文档
```

---

## 技术栈

| 组件 | 选型 | 说明 |
|------|------|------|
| HTTP Server | `net/http` 标准库 | 足够，不引入框架 |
| MCP 协议 | 手写 JSON-RPC 2.0 | SSE transport spec (2024-11-05) |
| FFI | CGo + `dlopen`/`LoadLibrary` | 动态加载 brosdk.dll |
| 序列化 | `encoding/json` | 标准库 |
| 日志 | `log/slog` | Go 1.21+ 结构化日志 |
| 配置 | `flag` + 环境变量 | 无额外依赖 |

---

## MCP Transport：仅 SSE

按照 MCP spec SSE transport 规范：

```
GET  /sse      → 建立 SSE 长连接，服务端推送 endpoint event
POST /message  → 客户端发送 JSON-RPC 请求
```

流程：
1. 客户端 `GET /sse` 建立连接，收到 `event: endpoint\ndata: /message?sessionId=<id>`
2. 客户端向 `/message?sessionId=<id>` POST JSON-RPC 请求
3. 服务端处理后通过 SSE 推送响应 `event: message\ndata: {...}`
4. SDK 异步回调（`result_callback`）同样通过 SSE 推送给对应 session

---

## MCP Tools 清单

### SDK 生命周期

| Tool | 对应 SDK 函数 | 描述 |
|------|--------------|------|
| `sdk_init` | `sdk_init` | 初始化 SDK（userSig / workDir / port / sdkApiUrl） |
| `sdk_info` | `sdk_info` | 查询 SDK 运行时信息（版本、状态） |
| `sdk_shutdown` | `sdk_shutdown` | 关闭 SDK |
| `sdk_token_update` | `sdk_token_update` | 异步刷新 userSig |
| `sdk_get_user_sig` | `sdk_get_user_sig` | 通过 apiKey 获取 userSig |

### 浏览器控制

| Tool | 对应 SDK 函数 | 描述 |
|------|--------------|------|
| `browser_open` | `sdk_browser_open` | 打开一个或多个指纹浏览器环境（异步） |
| `browser_close` | `sdk_browser_close` | 关闭浏览器环境（异步） |
| `browser_info` | `sdk_browser_info` | 查询当前运行中的浏览器列表 |
| `browser_install` | `sdk_browser_install` | 安装浏览器核心资源（异步） |
| `browser_command` | MCP 侧 CDP WebSocket 代理 | 向运行中的浏览器发送 CDP 命令（WebSocket 直连 DevTools） |

### 环境管理

| Tool | 对应 SDK 函数 | 描述 |
|------|--------------|------|
| `env_create` | `sdk_env_create` | 创建新的浏览器指纹环境 |
| `env_page` | `sdk_env_page` | 分页查询环境列表 |
| `env_update` | `sdk_env_update` | 更新环境配置 |
| `env_destroy` | `sdk_env_destroy` | 删除环境 |
| `env_getinfo` | `sdk_env_getinfo` | 查询单个环境详细信息 |

**共 15 个 Tool**。

---

## CGo FFI 设计

参考 `brosdk-rust/libs/brosdk.h`，在 Go 中通过 CGo 动态加载：

```go
// internal/sdk/sdk.go
/*
#include "brosdk.h"
#cgo LDFLAGS: -ldl
*/
import "C"
import "unsafe"
```

关键约束（与 Rust 实现对齐）：
- `sdk_malloc` / `sdk_free`：所有 SDK 分配的 `char*` 必须用 `sdk_free` 释放
- 异步操作（`browser_open`、`browser_close`、`token_update`）返回 `reqId`，最终结果通过 `result_callback` 推送
- `cookies_storage_callback` 替换 buffer 时必须用 `sdk_malloc` 分配新 buffer
- 单例：全局 `sync.Once` 保证 SDK 只加载一次

### 回调桥接方案

```
Native result_callback
        ↓
  Go channel (chan SdkEvent)
        ↓
  SSE session 广播
```

由于 CGo 不允许在 C 回调中直接调用 Go 函数，通过 `//export` 导出 Go 函数作为回调，内部向 channel 写入数据，SSE goroutine 负责消费并推送。

---

## 配置参数

| 参数 | 环境变量 | 默认值 | 说明 |
|------|---------|--------|------|
| `--port` | `MCP_PORT` | `8080` | SSE server 监听端口 |
| `--lib` | `BROSDK_LIB` | 自动探测 | brosdk 原生库路径 |
| `--log-level` | `LOG_LEVEL` | `info` | 日志级别 |

---

## 开发阶段规划

### Phase 1 — CGo FFI 骨架（~1天）
- [ ] `go.mod` 初始化，CGo brosdk.h 绑定
- [ ] `sdk.go`：load / init / shutdown / sdk_info
- [ ] 单元测试：用 mock dll 验证 FFI 调用链

### Phase 2 — SSE MCP Server（~1天）
- [ ] `mcp/server.go`：`GET /sse` + `POST /message`
- [ ] `mcp/session.go`：session map + 广播机制
- [ ] `mcp/handler.go`：`initialize` / `tools/list` / `tools/call`

### Phase 3 — Tool 实现（~2天）
- [ ] SDK 生命周期 tools（5个）
- [ ] 浏览器控制 tools（5个）
- [ ] 环境管理 tools（5个）
- [ ] 异步回调 → SSE 事件桥接

### Phase 4 — 集成测试与文档（~1天）
- [ ] 本地 brosdk.dll 联调
- [ ] `docs/tools.md` 工具文档
- [ ] `README.md` 快速开始

---

## 关键设计决策

1. **不使用 mcp-go 框架**：SSE transport 协议简单，手写可控，避免引入不熟悉的依赖。
2. **CGo 动态加载**：与 Rust 实现对齐，运行时指定 dll 路径，不依赖系统 PATH。
3. **单 SDK 实例**：brosdk 设计为单例（`sdk_shutdown` 后才能重新 init），Go 侧用 `sync.Once` 保证。
4. **回调 → channel → SSE**：隔离 C 回调上下文，避免 CGo 限制，同时支持多 SSE 客户端广播。
5. **所有异步 Tool 返回 reqId**：`browser_open` 等工具立即返回 reqId，后续事件通过 SSE 推送，客户端订阅即可获得完整生命周期通知。

---

## 参考

- BroSDK C API：`brosdk-rust/libs/brosdk.h`
- Rust 实现参考：`brosdk-rust/src/brosdk/`
- MCP SSE Transport Spec：https://spec.modelcontextprotocol.io/specification/2024-11-05/basic/transports/

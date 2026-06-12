## brosdk-mcp-go MCP 封装合理性分析

> 前提：本项目是一个本地运行、单用户的 MCP 服务，面向 AI Agent（Claude、CodeBuddy 等）操控指纹浏览器。以下分析基于这一上下文。

---

### 一、整体架构

四层分离，职责边界清晰：

```
MCP SSE Server (mcp/server.go)      ← 协议层：SSE + JSON-RPC
    ↓
Tool Layer (tools/tools.go)          ← 路由层：72 个工具定义 + dispatch
    ↓
Browser Actions (brosdk/actions.go)  ← 操作层：chromedp 高层封装
    ↓
Native Layer (brosdk/native_*.go)    ← 绑定层：DLL/dylib syscall/CGo
```

MCP 协议层不包含业务逻辑，Tool 层只做参数解析和路由，Actions 层专注浏览器操作，Native 层隔离平台差异。这个分层对本地 MCP 服务来说是恰当的——没有过度设计，也没有职责混乱。

---

### 二、MCP 协议实现

server.go 约 285 行，手写实现了一个最小 MCP SSE Server（spec 2024-11-05）。

**做得好的部分：**

手写而非引入 mcp-go 框架是合理的。SSE transport 协议本身非常简单（`GET /sse` 建连 + `POST /message` 发请求），引入框架反而增加依赖。JSON-RPC 2.0 dispatch 正确处理了 `initialize`、`tools/list`、`tools/call`、`ping` 四个标准方法，`notifications/initialized` 作为 no-op 也符合规范。15 秒心跳保活、CORS 头设置、Flush 机制都是正确的。

**值得注意的问题：**

1. **双通道响应**：`handleMessage` 在 HTTP 响应和 SSE Broadcast 中同时推送结果。对于单用户场景这不会造成数据泄露，但 Agent 可能收到两份相同结果——HTTP 同步响应 + SSE `message` 事件。MCP 客户端通常只取其一，所以实际无害，但设计上不够干净。理想做法是只对异步事件走 SSE，同步响应走 HTTP。

2. ~~**Broadcast 丢消息无感知**~~ **[已修复]**：`select + default` 的非阻塞发送在 channel 满时会静默丢弃消息。本地单用户场景下 SSE 消费通常很及时（64 条缓冲足够），但如果 Agent 在处理一个耗时操作时恰好有密集事件到达，可能丢失异步通知。~~建议至少在丢消息时打一条日志。~~ 现已添加 `log.Printf` 记录丢消息的 event 类型和 client ID。

3. ~~**仅支持 SSE transport**~~ **[已修复]**：已新增 MCP 2025-03-26 Streamable HTTP transport（`/mcp` 端点），支持 POST 请求/响应、GET 服务端事件流、DELETE 会话终止。旧版 `/sse` + `/message` 保留向后兼容。协议版本已升级到 `2025-03-26`。

---

### 三、工具设计（核心评估）

这是整个项目最值得讨论的部分。72 个工具的设计直接影响 Agent 的使用体验和 token 效率。

#### 3.1 CSS Selector + Ref 双定位 — 最大亮点

8 对工具（click/type/fill/hover/focus/select_option/check/uncheck）同时提供 CSS selector 和 ref 两个版本。这配合 `browser_snapshot`（尤其是 `interactiveOnly=true` 模式）构成了一个完整的 Agent 工作流：

```
snapshot(interactiveOnly) → 获取 AX tree + ref → 用 _ref 工具精确操作
```

这避免了让 LLM 手写 CSS selector 的脆弱性，是当前 browser-use Agent 的最佳实践。`interactiveOnly` 过滤将输出缩减 10-50 倍，显著降低 token 消耗。

#### 3.2 Agent-Friendly 工具 — 设计精巧

6 个新增工具针对 Agent 场景做了深入思考：

`browser_find_ref` 让 Agent 按 role/name/value 搜索 AX tree，不用手写 selector。`browser_page_state` 提供轻量级的 `{title, url, readyState}` 查询，无需 snapshot 开销。`browser_exists` 做布尔存在检查，不返回完整树。`browser_dialog` 用页面侧 JS override 方案捕获 alert/confirm/prompt，避开了 CDP 事件时序竞争这个经典难题。`browser_fill_form` 单次调用完成多字段表单填写。`browser_wait` 支持四种等待模式。

这些工具的共同特点是：**减少了 Agent 的 tool call 往返次数和每次返回的 token 量。**

#### 3.3 工具数量与 Token 成本

72 个工具的 schema 在 `tools/list` 响应中一次性发送给 Agent，估算消耗 6000-8000 tokens。对于当前主流 LLM 的 context window 来说不是大问题，但确实增加了每轮对话的基础开销。

更实质的问题是：~~**部分工具的粒度偏细，增加了 Agent 的选择负担。**~~ **经评估，当前粒度是合理的**（详见下文分析）。

键盘操作拆成了 5 个工具（`press_key`、`keyboard_type`、`insert_text`、`key_down`、`key_up`），表面上看粒度偏细，但深入实现后发现它们对应 CDP 层面不同的操作语义：`press_key` 调用 `chromedp.KeyEvent` 模拟单次完整按键（keyDown+keyUp）；`keyboard_type` 逐字符派发 `input.KeyChar` 事件，触发完整的 keydown/keypress/input/keyup 链路，对有 per-key 事件监听的站点（autocomplete、input mask）是必需的；`insert_text` 使用 `input.InsertText` 一次性插入文本，不触发逐字符 key 事件；`key_down`/`key_up` 是 modifier 键的状态控制（按下/释放），与 `press_key` 的单次触发语义完全不同。~~合并这些工具为带 mode 参数的单一工具，Agent 仍然需要理解底层差异才能正确选择参数，只是把"选哪个工具"变成了"填什么参数"，并未真正降低认知负担。~~ **经评估：当前粒度合理，不宜合并。**

类似地，`get_text` / `get_value` / `get_html` 中 `get_text`（textContent）和 `get_value`（input value）操作语义不同（前者取元素文本，后者取表单值），`get_html` 甚至不接受 selector 参数（返回整页 outerHTML），~~三者合并为 `get_content` 会让 schema 更复杂而非更简单。~~ **经评估：不宜合并。**

另一个方向上，`browser_wait` 集成了 4 种等待模式（navigation/selector/text/time），参数空间较大，Agent 容易传错参数。这个工具反而粒度偏粗了。

**建议**：考虑将 `browser_wait` 拆分为 `wait_for_element` 和 `wait_for_navigation` 两个更语义明确的工具。工具总数维持 72 个不变——当前数量对主流 LLM 的 context window 不构成问题，各工具的语义区分度也是必要的。

#### 3.4 工具描述质量

~~工具描述整体质量不错：每个 description 用 1-2 句话清晰说明了功能和参数含义，`browser_snapshot` 的描述还特别提示了 `interactiveOnly` 的 token 节省效果。`browser_command` 标注了 `[Advanced]` 前缀引导 Agent 优先使用高层工具。~~

~~不过有些描述可以更 Agent-friendly。比如 `browser_type` 说 "appends to existing value, fires input/change events"，而 `browser_fill` 说 "Clear an input field and type new text"——这两个的区别对 Agent 来说不够直觉，建议在 `browser_type` 的描述中加一句 "Use browser_fill to replace existing content"。~~

**[已修复]** 已为 10 个工具添加了交叉引用引导：type↔fill 互引、keyboard_type 提示优先用 type/fill、insert_text 提示使用场景、key_down/key_up 互引、get_text/get_value 互引。Agent 现在能从工具描述中直接了解到相似工具的差异和选择建议。

#### 3.5 Dispatch 机制

单一 switch-case（约 1000 行、72 个 case）做路由。

对本地单用户 MCP 服务来说，**功能上完全没问题**——简单、性能好、编译时检查。Dispatch 导出给 Player 回放复用也是好的设计。

可维护性是唯一短板：新增工具需要同时在 `All()` 和 `Dispatch` 两处修改。但这对于一个相对稳定的工具集来说是可以接受的 tradeoff。如果未来工具数量继续增长，可以考虑注册表模式。

---

### 四、录制回放系统

这是项目最有特色的功能模块，值得单独展开分析。

#### 4.1 架构设计

```
record_start → Agent 操作（Handler 自动捕获每一步）→ record_stop（自动保存 Scene JSON）
                                                                    ↓
scene_replay → Player 逐步加载 Scene → 解析 ref → dispatch → WaitFor guard → HumanDelay → 下一步
```

三个核心设计决策都很出色：

**Hook 模式**——Handler 闭包中在 Dispatch 之前自动捕获，录制工具和业务工具完全解耦，零侵入。`isRecorderTool()` 排除录制工具自身，避免递归。

**共享 Dispatch**——Player 回放时直接调用 `tools.Dispatch()`，录制和回放走同一套代码路径，零重复。这意味着新增工具后录制回放自动支持，不需要修改 Player。

**Fingerprint 机制**（refconv.go）——录制 `_ref` 工具时，从缓存的 snapshot 中提取元素的 `{role, name, value}` 指纹。回放时用新鲜 snapshot 按指纹重新匹配元素，解决了 backendNodeId 跨 session 不稳定的问题。两级匹配（先精确匹配 role+name+value，再降级到 role+name）考虑周到。

#### 4.2 WaitFor 守卫

这是回放可靠性的核心。每步操作后不再依赖固定 sleep，而是条件驱动等待：

- navigate/open → 等 `readyState:complete`（10s）
- click/back/forward/reload → 等 `readyState:complete`（5s）
- 其他 → 不等待

Guard 失败注入 `guardWarn` 注解但不中断回放，这是合理的——软错误让 Agent 自己决定是否重试。自动推断 `InferWaitFor` 覆盖了最常见的场景，同时 Scene JSON 可手动编辑覆盖。

#### 4.3 HumanDelay

录制时记录步骤间的时间差（`time.Since(lastCaptureTime)`），回放时在 WaitFor 满足后插入人类节奏（上限 3s）。`ApplyHumanDelay` 默认开启，关闭后全速运行适合回归测试。这个设计在真实感和效率之间取得了好的平衡。

#### 4.4 变量替换

`{{variable}}` 占位符在回放时由 `scene_replay({variables:{...}})` 注入。`substituteValue` 递归处理了 string/map/array 三种类型，覆盖了参数中各种嵌套情况。

**录制回放整体评价：这是整个项目设计最成熟的部分。** Hook + 共享 Dispatch + Fingerprint + WaitFor + HumanDelay 形成了一个完整且可靠的自动化方案。

---

### 五、可靠性

对于本地单用户场景，Agent 可能通过 MCP 客户端发出并发 tool call（比如同时查 snapshot 和执行点击），以下问题值得关注：

#### 5.1 并发安全

~~`Navigate()` 在获取 `browserTab` 后直接修改 `tabCtx` 和 `tabCancel`，没有持锁。~~ **[已修复]** 三处竞态均已加锁保护：`ensureBrowser` 的 stale cleanup 段在 `m.mu.Lock()` 内执行并加了 double-check；`ensureTab` 的 tabCtx 读写加锁；`Navigate` 的 tab context 交换加锁（cancel + 创建 + 拷贝到局部变量后 Unlock，后续 chromedp.Run 不持锁）。同时删除了无调用方的死代码 `closeActiveTab`。

~~CDP 连接池 `getOrDial()` 的 check-then-act 不是原子的。~~ **[已修复]** 引入 `cdpDialing` 信号量 map（`map[string]chan struct{}`），保证每个 envID 只有一个 goroutine 执行 dial，其他并发调用者在信号 channel 上等待并复用结果。`CloseCDP` 同步清理所有 pending 信号 channel 防止 shutdown 期间 goroutine 泄漏。

Native 层的包级全局变量 `activeEventSink` 对单用户单实例场景不是问题。

#### 5.2 超时保护

~~`runAction()` 给大多数交互操作提供了 10 秒超时，但 Screenshot 和 PDF 没有超时保护。~~ **[已修复]** 新增 `captureTimeout = 30s` 常量，`Screenshot` 和 `PDF` 在 `ensureTab` 后立即创建 `context.WithTimeout(tabCtx, captureTimeout)` 子 context，所有 `chromedp.Run` 分支均使用该 ctx。页面卡住时 30 秒后返回明确的 `context deadline exceeded` 错误。同时修复了两处 `os.MkdirAll` 忽略 error 的问题。

#### 5.3 关闭流程

~~`main.go` 的 shutdown 只调用了 `mgr.Shutdown()`（SDK 层），没有清理 chromedp 连接和 CDP WebSocket 连接。~~ **[已修复]** 关闭流程重写为四步有序关闭：(1) `httpSrv.Shutdown` 异步启动停止接收新请求；(2) `mgr.CloseAllBrowsers()` 取消所有 chromedp tab/allocator context；(3) `brosdk.CloseCDP()` 关闭所有 CDP proxy WebSocket；(4) `mgr.Shutdown()` 关闭 native SDK。同时修复了 `CloseCDP()` 将 `cdpConns` 设为 `nil` 导致并发写入 nil map panic 的隐患，改为 `make(map[string]*cdpConn)`。

#### 5.4 表单事件派发

~~`Fill()` 和 `SelectOption()` 使用 `SetValue` 直接修改 DOM property，不触发 `input`/`change` 事件，导致 React/Vue/Angular 检测不到变化。~~ **[已修复]** `Fill`（CSS selector 版）在 `SetValue` 后通过 `chromedp.Evaluate` 派发 `input` + `change` 冒泡事件；`FillRef`（ref 版）通过 `cdpdom.ResolveNode` + `runtime.CallFunctionOn` 派发同样事件，与 `SelectOptionRef` 模式一致。`SelectOption`（CSS selector 版）同样追加了 `change` 事件派发。`FillForm` 内部调用 `Fill`，自动受益于修复。

#### 5.5 错误处理

工具调用失败时，error 信息通过 `ToolResult.IsError=true` 返回给 Agent。错误消息质量参差不齐：`envId is required` 很清晰，但有些地方直接返回了 Go 的内部错误（如 `chromedp` 的原始错误），Agent 可能难以理解。

~~`jsonFromParams` 和散落在各处的 `json.Marshal` 忽略了 error。~~ **[已修复]** 审计了全部 14 处 `json.Marshal` 调用，其中 12 处序列化的是纯基本类型 struct（string/int/bool），`json.Marshal` 不可能失败，保持原样；2 处有实际风险的已修复：`browser_evaluate` 的 JS 返回值（`any` 类型，可能含 NaN/Infinity）现在返回明确错误；`BrowserCommand` 的 CDP 请求序列化失败现在提前返回 error 而非向 WebSocket 发送 nil body。

---

### 六、Inspector

嵌入 Web UI 调试工具（660 行 HTML/CSS/JS），提供工具浏览、调用、SSE 事件监控、场景管理。

**对本地单用户场景来说是恰到好处的设计。** 零依赖部署，启动后直接访问 `/inspector` 就能交互测试。与 Go 社区嵌入 pprof/Swagger 的传统一致。

~~维护性上建议将前端代码迁移为独立文件 + `//go:embed`~~ **[已修复]**：已将 660 行 HTML/CSS/JS 从 Go 字符串常量提取为独立的 `inspector.html` 文件，`inspector.go` 改用 `//go:embed` 编译时嵌入（713 行 → 33 行），同时删除了未使用的 `inspectorToolsJSON()` 死代码。现在可以用 IDE 正常编辑和高亮前端代码。

---

### 七、综合评价

#### 设计亮点（按重要程度）

1. **CSS Selector + Ref 双定位** — 对 AI Agent 操控浏览器来说是最核心的设计，snapshot → ref → 操作的工作流是当前最佳实践
2. **录制回放的完整方案** — Hook + 共享 Dispatch + Fingerprint + WaitFor + HumanDelay，设计成熟度很高
3. **Agent-Friendly 工具集** — `find_ref`、`page_state`、`fill_form`、`dialog` 等工具显著降低了 Agent 的 token 消耗和操作复杂度
4. **手写 MCP + 四层分离** — 没有过度依赖框架，各层职责清晰
5. **Native 绑定的平台抽象** — `nativeLib` 接口干净，Windows/macOS 差异封装彻底，内存管理正确

#### 建议改进（按优先级）

**已修复：**

1. ~~`Navigate()` 修改 tabCtx/tabCancel 的竞态条件~~ — `ensureBrowser`/`ensureTab`/`Navigate` 三处加锁，删除死代码 `closeActiveTab`
2. ~~关闭流程补充 `CloseAllBrowsers()` + `CloseCDP()`~~ — 重写为四步有序关闭，修复 `CloseCDP` nil map 隐患
3. ~~Screenshot/PDF 操作添加超时保护~~ — `captureTimeout = 30s`，修复 `MkdirAll` 忽略 error
4. ~~CDP `getOrDial()` 并发 dial 竞态~~ — `cdpDialing` 信号量保证单 envID 单次 dial
5. ~~`Fill()`/`FillRef()`/`SelectOption()` 缺少 input/change 事件~~ — `SetValue` 后追加事件派发，React/Vue 兼容
6. ~~合并细粒度工具（键盘 5→2、内容获取 3→1）~~ — **经评估不宜合并**：5 个键盘工具对应 CDP 层面不同的操作语义（KeyEvent vs KeyChar vs InsertText vs KeyDown/KeyUp），合并为带 mode 参数的单一工具并未降低认知负担；内容获取三个工具的参数签名和功能语义差异过大，强行合并只会让 schema 更复杂。当前粒度合理。
7. ~~Broadcast 丢消息时至少打日志~~ — 已在 `server.go` 的 `select+default` 分支添加 `log.Printf` 记录 event 类型和 client ID
8. ~~工具描述增加使用引导~~ — 已为 10 个工具添加交叉引用引导（type↔fill、键盘工具、内容获取工具）
9. ~~`json.Marshal` 错误处理~~ — 审计全部 14 处调用，修复 2 处有实际风险的（`browser_evaluate` JS 返回值可能含 NaN/Infinity、`BrowserCommand` CDP 请求序列化），其余 12 处序列化的都是纯基本类型 struct，保持原样
10. ~~Inspector 前端代码迁移为 `//go:embed` 独立文件~~ — 提取 `inspector.html`，`inspector.go` 改用 `go:embed`，删除 `inspectorToolsJSON()` 死代码
11. ~~配置文件支持环境变量覆盖~~ — `config.Load()` 现在支持 `BROSDK_*` 环境变量覆盖所有字段（`BROSDK_API_KEY`、`BROSDK_USER_SIG`、`BROSDK_WORK_DIR`、`BROSDK_PORT`、`BROSDK_SDK_API_URL`、`BROSDK_DEBUG`），优先级为 CLI flag > 环境变量 > 配置文件 > 默认值；无配置文件时也可纯通过环境变量启动
12. ~~关注 MCP Streamable HTTP transport 演进~~ — 已实现 MCP 2025-03-26 Streamable HTTP transport（`/mcp` 端点），支持 POST/GET/DELETE 三种方法、会话管理（`Mcp-Session-Id`）、内容协商（JSON/SSE）；保留旧版 `/sse` + `/message` 向后兼容；Inspector 前端自动检测并使用 Streamable HTTP（失败时回退 SSE）

#### 结论

brosdk 的 MCP 封装整体设计合理，在 AI Agent 操控浏览器这个场景下做了很多有针对性的优化（双定位、Agent-Friendly 工具、interactiveOnly snapshot），录制回放系统的设计成熟度尤其突出。原报告中标记的 12 个应修复/建议改进项已全部处理完毕（竞态保护、关闭流程、超时保护、CDP 连接池竞态、表单事件派发、工具粒度评估、Broadcast 日志、工具描述引导、json.Marshal 错误处理、Inspector go:embed 迁移、环境变量配置覆盖、MCP Streamable HTTP transport）。当前实现质量可以稳定支撑本地单用户的日常使用，同时已具备面向未来 MCP 协议演进的兼容性。

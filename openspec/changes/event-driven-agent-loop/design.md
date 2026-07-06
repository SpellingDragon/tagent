## Context

tagent 当前使用 trpc-agent-go 的 `runner.Run()` + `LLMAgent.flow.Run()` + `graph` 作为执行模型。核心问题：React Loop 期间工具调用是 graph 内部 inline 同步执行的，外部事件积压在 mailbox 中必须等整轮 Run 结束才能被消费。

Framework 的 `agent.Agent` 接口只要求 `Run(ctx, *Invocation) → <-chan *event.Event`，这给 tagent 留下了多态空间：`TagentAgent` 可以实现自己的 `Run`，内部跑事件驱动的 agent loop，而不使用 `LLMAgent`/`flow`/`graph`。

现有的 `event/types.go` 已定义 7 种事件类型（`external_input`/`agent_output`/`action_command`/`thinking_plan`/`thinking_recall`/`thinking_knowledge`/`context_compress`），为事件驱动模型提供了语义基础。

## Goals / Non-Goals

**Goals:**
- 用 EventBus + AgentLoop 替代 framework 的 graph/flow 同步执行模型
- 事件管线中的事件按类型自然流转：tool_use 触发异步工具执行，结果以 external_input 回写
- 最大化复用 trpc-agent-go：model.Model、tool.Tool、session.Session、plugin.Plugin、event.Event、agent.Invocation
- Preprocessor 承担全部业务判断（事件筛选、shouldCallModel、压缩），AgentLoop 保持纯引擎
- 所有 shell 命令统一走 tmux async，子 agent 走 goroutine 异步
- agent_output 不进 bus，直接输出 + 写 session

**Non-Goals:**
- 不修改 trpc-agent-go framework 源码
- 不引入 Task/Backend 抽象层（按最简方案，shell 走 tmux，子 agent 走 goroutine，if-else 分发）
- 不实现多消费者事件总线（agent loop 是唯一顺序消费者）
- 不保证事件先后顺序（由 model 在 react 模式下自行判断执行条件）
- 不改变 MemoryPlugin/SummaryPlugin 的 OnEvent 持久化机制

## 三层数据表示模型（核心设计基础）

事件驱动架构中存在三层数据表示，必须明确区分并正确流转：

### 层 1: EventBus AgentEvent（触发器，临时）

```
AgentEvent{
    Type: "external_input" | "tool_use"
    Source: "user" | "tmux" | "meditation" | "subagent" | "tool_result"
    Message: *model.Message  // external_input 载荷
    ToolCall: *model.ToolCall // tool_use 载荷
}
```

- **生命周期**: 从 Publish 到 AgentLoop.Pull 消费，然后丢弃
- **用途**: 触发 AgentLoop 执行，区分事件类型
- **关键**: tool_use 事件只触发 dispatch，不进 LLM context

### 层 2: Session Events（工作内存，完整未压缩）

```
session.Session{
    Events: []event.Event{  // 由 onEvent 回调追加
        Event{
            Response: *model.Response  // 包含 Choices[0].Message
            StateDelta: {
                "event_key": "12345",      // Snowflake ID
                "event_type": "external_input",
                "partition_id": "0",
            }
        },
        ...
    },
    State: StateMap{ "12345": <data> }
}
```

- **生命周期**: session 存活期间（StartLoop 到 StopLoop）
- **用途**: Preprocessor 的唯一历史来源 — 从这里构建 LLM Context
- **写入方式**: AgentLoop 通过 onEvent callback → MemoryPlugin.OnEvent → sessionSvc.AppendEvent
- **关键**: 这是唯一完整未压缩的对话历史，AgentLoop 不维护自己的 history

### 层 3: MemoryStore FullEvent（长期记忆，持久化）

```
FullEvent{
    EventKey: 12345,          // Snowflake ID (编码 PartitionID)
    PartitionID: 0,           // 从 AgentName FNV-1a hash
    EventType: "external_input",
    EventSummary: "用户说你好",
    Content: "你好",           // 完整内容
    Response: *model.Response,
    Parent: 12344,            // 因果链 (via RelationStore)
}
```

- **生命周期**: 永久（文件/DB 存储）
- **用途**: recall 工具跨 session/agent 查询
- **写入方式**: MemoryPlugin.OnEvent 同步写入（与 Session Events 同一个回调）
- **关键**: 与 Session Events 在同一个 onEvent 调用中写入，保证一致性

### 层间关系

```
EventBus AgentEvent (临时)
    │
    ├──▶ onEvent callback (AgentLoop 调用)
    │       ├──▶ MemoryPlugin.OnEvent
    │       │       ├──▶ MemoryStore.StoreEvent (层3: FullEvent)
    │       │       └── evt.StateDelta["event_key"] = "K1"
    │       └──▶ sessionSvc.AppendEvent (层2: Session.Events)
    │
    └──▶ Preprocessor.Process
            └── 读 session.Events → 构建 []model.Message (层1: LLM Context)
                    ├── event_key 前缀注入 ([evt_K1|external_input] 内容)
                    ├── token 预算检查 (完整 messages)
                    └── SmartCompress (完整 messages，含历史)
```

## Decisions

### 1. EventBus：per-agent 单消费者事件队列

**决策**：每个 TagentAgent 实例拥有一个 EventBus。Bus 是一个有序的事件队列，agent loop 是唯一的消费者。

**接口**：
```
type AgentEvent struct {
    ID        string
    Type      string          // external_input | tool_use
    Source    string          // user | tmux | meditation | subagent | agent_loop
    Timestamp time.Time
    Message   *model.Message  // external_input 的载荷
    ToolCall  *model.ToolCall // tool_use 的载荷
    Metadata  map[string]any  // event_key, partition_id 等
}

type EventBus struct {
    Publish(event *AgentEvent)
    Pull(ctx context.Context) ([]*AgentEvent, error) // 阻塞直到至少一个事件
}
```

**替代方案**：直接复用 `chan model.Message`（现 mailbox）。否决：无法区分事件类型，无法携带 tool_use/tool_call 载荷。

### 2. AgentLoop：纯引擎，无业务语义

**决策**：AgentLoop 是一个简单的循环，不包含任何业务判断逻辑：

```
loop:
  1. Pull batch of unconsumed events from bus
  2. For each external_input event:
     a. Wrap as event.Event
     b. Call onEvent callback → MemoryPlugin.OnEvent + sessionSvc.AppendEvent
        (写入 Session.Events 和 MemoryStore)
  3. messages, shouldCallModel = Preprocessor.Process(events, session)
     (Preprocessor reads session.Events — NOT just the new batch)
  4. if !shouldCallModel → continue
  5. if shouldCallModel → model.GenerateContent(messages)
  6. parse response:
     - has tool_calls → dispatch tool_use (async, 不等待)
     - final response → wrap as event.Event → onEvent callback
                       → emit to outputCh (NOT to bus)
  7. continue
```

**关键变更（vs 第一版实现）**：
- **删除 `history []model.Message` 字段** — Session.Events 是唯一历史来源
- **AgentLoop 在调用 Preprocessor 前，先把 bus 事件通过 onEvent 写入 session** — 保证 Preprocessor 能读到
- **Model response 也通过 onEvent 写入 session** — 保证因果链和持久化
- **Preprocessor 从 session.Events 构建完整 messages** — 不再只处理新 batch

**替代方案**：把判断逻辑放 agent loop 内。否决：agent loop 变成业务引擎，难以测试和复用。

### 3. Preprocessor：从 Session.Events 构建完整 LLM Context

**决策**：Preprocessor 不再接收 `[]*AgentEvent` 作为输入，而是从 `session.Session.Events` 构建完整的 LLM Context。

**职责**：
1. shouldCallModel 判断：检查 bus batch 中是否有 external_input（有 → true；只有 tool_use → false）
2. 从 session.Events 构建 `[]model.Message`：遍历 Events，提取 `Choices[0].Message`
3. event_key 前缀注入：从 session.Events[i].StateDelta["event_key"] 读取，按位置匹配 message
4. Token 预算检查（对完整 messages，不只是新 batch）
5. SmartCompress 触发（超限时压缩旧消息，与原 ContextIntervention 行为一致）

**输出**：`ProcessResult{Messages []model.Message, ShouldCallModel bool}`

**输入变更**：
```go
// 旧（第一版实现，错误）：
func (p *Preprocessor) Process(ctx context.Context, events []*AgentEvent) ProcessResult
    // 只处理新 batch 事件，压缩范围错误

// 新（修正后）：
func (p *Preprocessor) Process(ctx context.Context, batch []*AgentEvent, sess *session.Session) ProcessResult
    // shouldCallModel 从 batch 判断
    // messages 从 sess.Events 构建（完整历史）
    // 压缩作用于完整 messages
```

**替代方案**：保留 BeforeModel callback。否决：callback 时机隐式，且无法表达 shouldCallModel 判断。

### 4. agent_output 不进 bus

**决策**：agent loop 解析出最终响应时：
1. 包装为 `event.Event`，调用 onEvent callback
   - MemoryPlugin.OnEvent → 写入 MemoryStore（FullEvent + 因果链）
   - sessionSvc.AppendEvent → 写入 session.Events（含 StateDelta: event_key/type）
2. 直接 emit 到 outputCh（供调用方/用户/A2A 接收）
3. 不 publish 到 bus

**理由**：避免 agent loop 被自己的输出触发死循环。bus 上作为触发事件的只有 `external_input` 和 `tool_use`。

### 5. onEvent 回调：接通 Plugin 和 Session 持久化

**决策**：AgentLoop 持有 `onEvent func(evt *event.Event)` 回调，在以下时机调用：
- **bus 事件写入前**：每个 external_input 事件 → 包装为 event.Event → onEvent
- **model response 解析后**：assistant response（含 tool_calls 或 final）→ 包装为 event.Event → onEvent

**onEvent 回调实现**（由 TagentAgent 设置）：
```go
func(evt *event.Event) {
    // 1. MemoryPlugin.OnEvent → StoreEvent + SetParent + StateDelta
    memPlugin.OnEvent(ctx, inv, evt)
    // 2. sessionSvc.AppendEvent → sess.Events = append(...)
    sessionSvc.AppendEvent(ctx, sess, evt)
}
```

**关键**：onEvent 必须在 Preprocessor.Process 之前调用，确保 session.Events 已包含最新事件。

### 6. 异步 Tool Dispatch：最简方案

**决策**：agent loop 发现 tool_use 后，按工具类型 if-else 分发：

**Shell 类（action/skill_run）**：
- goroutine 调用 `tool.Call()` 
- 如果返回 `tmuxAsyncResult`（IsTmuxAsync()==true），不 publish — TmuxMonitor 回调会 publish
- 否则（同步工具如 duckduckgo_search），结果直接 publish 为 external_input

**子 agent 类（knowledge/recall）**：
- goroutine 调用 `wrapper.Call(ctx, args)`
- `wrapper.Call` 内部调用 `子agent.Run(ctx, inv)`，消费 eventCh 直到 final response
- 结果 publish 为 external_input 到父 bus

**子 agent 超时保护**：dispatch goroutine 使用 `context.WithTimeout(ctx, 10*time.Minute)`。

**不引入 Task/Backend 抽象层**：两种路径的差异只在"怎么知道完成了"，回写事件的方式统一。

### 7. 全 tmux 化：shell 命令统一异步

**决策**：action tool 的 `Call` 不再有 exec/tmux_exec 两种模式，统一走 tmux async。

**影响**：
- `tool.Call()` 返回 `TmuxExecResponse{SessionID, Status:"running"}`
- TmuxMonitor 异步感知状态变化，通过 `InjectMessage` → `bus.Publish` 回写 external_input
- model 在下一轮看到命令执行结果（exit code / stdout / stderr / stable 状态）
- tool description 需更新：告知 model 结果稍后以事件形式送达
- tmux 不可用时 fallback 到 sync exec（degraded mode）

### 8. 与 framework 的衔接

**决策**：保留 `runner.Runner` 作为外壳用于 session/plugin 生命周期管理，但 AgentLoop 绕过 runner 的执行路径。

```
TagentAgent (implements agent.Agent)
  ├── NewTagentAgent:
  │     ├── 创建 MemoryPlugin + SummaryPlugin
  │     ├── 创建 SessionService (inmemory)
  │     ├── 创建 EventBus + Preprocessor + AgentLoop
  │     ├── 创建 Runner (持有 plugins + sessionSvc)
  │     └── 设置 AgentLoop.onEvent callback
  │           → memPlugin.OnEvent + sessionSvc.AppendEvent
  │
  ├── StartLoop (持久循环):
  │     ├── 创建/获取 session
  │     ├── 设置 Preprocessor.session
  │     └── go agentLoop.Run(loopCtx)
  │
  └── Run (sub-agent 调用路径):
        ├── 创建独立 EventBus + SmartCompressor + AgentLoop
        ├── 发布 inv.Message 为 external_input
        ├── go agentLoop.Run(ctx)
        └── 转发 outputCh → eventCh (final response 后停止)
```

**复用**：session.Service、MemoryPlugin.OnEvent、SummaryPlugin、model.Model、tool.Tool、event.Event。

**不使用**：LLMAgent、llmflow、graph、runner.Run()（runner 仅用于 plugin 注册和 session service 创建）。

### 9. 子 agent 的事件管线隔离

**决策**：子 agent 有自己的 EventBus、AgentLoop、Preprocessor，与父 agent 完全隔离。

- 子 agent 的 SmartCompressor 独立创建（不与父 agent 共享，防止并发竞态）
- 子 agent 的 session 独立（不与父 agent 共享 session.Events）
- 父 agent 只看到 dispatch goroutine 返回的最终 external_input

## 数据流时序图

### 完整多轮对话（含工具调用）

```
t0: User sends "你好"
    Bus: [AgentEvent{type:external_input, msg:{user:"你好"}}]

t1: AgentLoop.Pull → batch=[evt_user]
    ├── onEvent(wrap(evt_user))           ← 写入 session + MemoryStore
    │   ├── MemoryPlugin: StoreEvent(K1, {type:external_input, content:"你好"})
    │   │                 SetParent(K1, 0)
    │   │                 evt.StateDelta["event_key"]="K1"
    │   └── sessionSvc: sess.Events=[evt_K1]
    │
    ├── Preprocessor.Process(batch, sess)
    │   ├── shouldCallModel=true (有 external_input)
    │   ├── 读 sess.Events → [msg:{user:"[evt_K1|external_input] 你好"}]
    │   ├── token check → OK
    │   └── return (messages, true)
    │
t2: AgentLoop.callModel(messages)
    Model returns: {choices:[{msg:{asst:"你好！有什么可以帮你？"}}]}  (no tool_calls)

t3: AgentLoop.handleResponse
    ├── onEvent(wrap(asst_response))       ← 写入 session + MemoryStore
    │   ├── MemoryPlugin: StoreEvent(K2, {type:agent_output, content:"你好！..."})
    │   │                 SetParent(K2, K1)  ← 因果链
    │   │                 evt.StateDelta["event_key"]="K2"
    │   └── sessionSvc: sess.Events=[evt_K1, evt_K2]
    │
    ├── emitEvent(asst_evt) → outputCh → 调用方收到响应
    └── (final → 回到 idle)

    ─── 一段时间后 ───

t4: User sends "帮我搜索 Go 教程"
    Bus: [AgentEvent{type:external_input, msg:{user:"帮我搜索..."}}]

t5: AgentLoop.Pull → batch=[evt_user2]
    ├── onEvent(wrap(evt_user2))
    │   ├── MemoryPlugin: StoreEvent(K3, ...), SetParent(K3, K2)
    │   └── sessionSvc: sess.Events=[evt_K1, evt_K2, evt_K3]
    │
    ├── Preprocessor.Process(batch, sess)
    │   ├── 读 sess.Events → [
    │   │     {user:"[evt_K1|external_input] 你好"},
    │   │     {asst:"[evt_K2|agent_output] 你好！..."},
    │   │     {user:"[evt_K3|external_input] 帮我搜索..."}
    │   ]
    │   └── return (messages, true)

t6: AgentLoop.callModel → Model returns tool_calls:[knowledge("搜索 Go")]
    ├── onEvent(wrap(asst_with_toolcalls))   ← assistant with tool_calls
    │   ├── MemoryPlugin: StoreEvent(K4, {type:thinking_plan, ...})
    │   └── sessionSvc: sess.Events=[..., evt_K4]
    │
    ├── emitEvent(toolcall_evt) → outputCh (调用方看到过程)
    └── dispatch tool_use → goroutine 调用 knowledge agent

    ─── knowledge agent 异步执行 ───

t7: Knowledge agent returns "找到以下教程:..."
    Bus: [AgentEvent{type:external_input, msg:{tool:"找到以下教程:..."}}]

t8: AgentLoop.Pull → batch=[evt_tool_result]
    ├── onEvent(wrap(tool_result))
    │   ├── MemoryPlugin: StoreEvent(K5, {type:tool_result, ...})
    │   │                 SetParent(K5, K4)
    │   └── sessionSvc: sess.Events=[..., evt_K5]
    │
    ├── Preprocessor.Process(batch, sess)
    │   ├── 读 sess.Events → 完整对话历史 (5 条 + 新的 tool_result)
    │   └── return (messages, true)

t9: AgentLoop.callModel → Model returns final response (no tool_calls)
    ├── onEvent(wrap(final_response))
    │   ├── MemoryPlugin: StoreEvent(K6, {type:agent_output, ...})
    │   └── sessionSvc: sess.Events=[..., evt_K6]
    │
    ├── emitEvent(final_evt) → outputCh → 最终响应
    └── (final → 回到 idle)
```

### 压缩发生时的数据流

```
Session.Events (完整历史，永不压缩):
  [K1|user] 你好
  [K2|asst] 你好！...
  [K3|user] 帮我搜索 A
  [K4|asst] tool_calls:[knowledge("A")]
  [K5|tool] {"results": [...A...]}
  [K6|asst] 基于 A 的结果...
  [K7|user] 帮我搜索 B      ← 第 2 个 task
  ... (K8-K10)
  [K11|user] 帮我搜索 D      ← 新 task (触发压缩)

Preprocessor 从 sess.Events 构建 messages (完整 15+ 条):
  token check: 16000 > 6400 (阈值) → 触发 SmartCompress

SmartCompress:
  ├── splitByTaskBoundary → 4 segments
  ├── KeepRecentTasks=2 → 保留最后 2 个
  ├── collectCompressedKeys → [K1..K10]  (从 [evt_K|type] 前缀解析)
  ├── (如有 summaryModel) → LLM 摘要旧 segments
  └── 重建 messages (压缩后)

压缩后 LLM Context (model 实际看到的):
  [system] You are tagent...
  [system] [context_compress] 压缩了 2 个片段, keys: [K1,K2,...,K10]
  [system] [摘要批次 1/1] 用户搜索了 A 和 B...
  [asst] [evt_K11|thinking_plan] tool_calls:[knowledge("C")]
  ... (保留的 recent messages)

关键: Session.Events 不变，MemoryStore 不变，只有 LLM Context 被压缩
```

## Risks / Trade-offs

**[Risk] 全 tmux 化对短命令的开销** → tmux session pool 复用 + 快速 completed 检测。短命令的 tmux 启动开销在 ~50ms 级别，可接受。

**[Risk] runner plugin 链断裂** → 第一版实现中 onEvent 未接通，导致 session.Events 和 MemoryStore 为空。修正方案：AgentLoop.onEvent callback 直接调用 MemoryPlugin.OnEvent + sessionSvc.AppendEvent。

**[Risk] A2A server 路径** → `a2a_server.go` 通过 `agent.Run()` 调用。新架构下 A2A 请求转为 bus 的首个 external_input，agent loop 接管。A2A 的流式输出通过 outputCh 转发。

**[Risk] 子 agent goroutine 泄漏** → 使用 context.WithTimeout(ctx, 10*time.Minute) 控制，超时后回写 error external_input。

**[Risk] 测试重写量大** → 依赖 onEvent 的测试需要 mock plugin + sessionSvc。分阶段迁移：先建 AgentLoop 骨架 + 新测试，再逐步迁移旧测试。

**[Trade-off] 事件顺序不保证** → 用户明确接受。model 在 react 模式下自行判断工具执行条件是否满足。

**[Trade-off] agent_output 不进 bus** → 意味着 agent 无法被自己的输出触发后续动作。如果需要 chain-of-thought，需要通过 tool_use 实现。
## Context

tagent 当前使用 trpc-agent-go 的 `runner.Run()` + `LLMAgent.flow.Run()` + `graph` 作为执行模型。核心问题：React Loop 期间工具调用是 graph 内部 inline 同步执行的，外部事件积压在 mailbox 中必须等整轮 Run 结束才能被消费。

Framework 的 `agent.Agent` 接口只要求 `Run(ctx, *Invocation) → <-chan *event.Event`，这给 tagent 留下了多态空间：`TagentAgent` 可以实现自己的 `Run`，内部跑事件驱动的 agent loop，而不使用 `LLMAgent`/`flow`/`graph`。

现有的 `event/types.go` 已定义 7 种事件类型（`external_input`/`agent_output`/`action_command`/`thinking_plan`/`thinking_recall`/`thinking_knowledge`/`context_compress`），为事件驱动模型提供了语义基础。

## Goals / Non-Goals

**Goals:**
- 用 EventBus + AgentLoop 替代 framework 的 graph/flow 同步执行模型
- 事件管线中的事件按类型自然流转：tool_use 触发异步工具执行，结果以 external_input 回写
- 最大化复用 trpc-agent-go：model.Model、tool.Tool、session.Session、plugin.Plugin、event.Event、agent.Invocation
- Preprocessor 承担全部业务判断（事件筛选、shouldCallModel、压缩），AgentLoop 保持纯引擎
- 所有 shell 命令统一走 tmux async，子 agent 走 goroutine 异步
- agent_output 不进 bus，直接输出 + 写 session

**Non-Goals:**
- 不修改 trpc-agent-go framework 源码
- 不引入 Task/Backend 抽象层（按最简方案，shell 走 tmux，子 agent 走 goroutine，if-else 分发）
- 不实现多消费者事件总线（agent loop 是唯一顺序消费者）
- 不保证事件先后顺序（由 model 在 react 模式下自行判断执行条件）
- 不改变 MemoryPlugin/SummaryPlugin 的 OnEvent 持久化机制

## Decisions

### 1. EventBus：per-agent 单消费者事件队列

**决策**：每个 TagentAgent 实例拥有一个 EventBus。Bus 是一个有序的事件队列，agent loop 是唯一的消费者。

**接口**：
```
type AgentEvent struct {
    ID        string
    Type      string          // external_input | tool_use
    Source    string          // user | tmux | meditation | subagent | agent_loop
    Timestamp time.Time
    Message   *model.Message  // external_input 的载荷
    ToolCall  *model.ToolCall // tool_use 的载荷
    Metadata  map[string]any  // event_key, partition_id 等
}

type EventBus interface {
    Publish(event *AgentEvent)
    Pull(ctx context.Context) ([]*AgentEvent, error) // 阻塞直到至少一个事件
}
```

**替代方案**：直接复用 `chan model.Message`（现 mailbox）。否决：无法区分事件类型，无法携带 tool_use/tool_call 载荷。

### 2. AgentLoop：纯引擎，无业务语义

**决策**：AgentLoop 是一个简单的循环，不包含任何业务判断逻辑：

```
loop:
  1. Pull batch of unconsumed events from bus
  2. messages, shouldCallModel = Preprocessor.Process(events)
  3. if !shouldCallModel → dispatch tool_use if any, continue
  4. if shouldCallModel → model.GenerateContent(messages)
  5. parse response:
     - has tool_calls → publish tool_use to bus
     - final response → emit to outputCh + write session (不进 bus)
  6. dispatch tool_use if any (异步，不等待)
  7. continue
```

**关键点**：步骤 3 和 6 中的 tool_use 分发是异步的——agent loop 发布 tool_use 后立刻结束本轮，不等待工具执行完成。

**替代方案**：把判断逻辑放 agent loop 内。否决：agent loop 变成业务引擎，难以测试和复用。

### 3. Preprocessor：显式预处理阶段

**决策**：从 `ContextIntervention.BeforeModel`（model callback）升级为显式的 `Preprocessor`，在 agent loop 调用 model 之前被显式调用。

**职责**：
1. 事件筛选：external_input → 进 LLM context；tool_use → 不进（已分发）
2. shouldCallModel 判断：有 external_input → true；只有 tool_use → false
3. 构造 `[]model.Message`：external_input → model.Message，附带 event_key hints
4. Token 预算检查
5. SmartCompress 触发（超限时压缩旧消息）

**输出**：`(messages []model.Message, shouldCallModel bool)`

**替代方案**：保留 BeforeModel callback。否决：callback 时机隐式，且无法表达 shouldCallModel 判断。

### 4. agent_output 不进 bus

**决策**：agent loop 解析出最终响应时：
1. 直接 emit 到 outputCh（供调用方/用户/A2A 接收）
2. 写入 session（通过 MemoryPlugin.OnEvent 自然持久化）
3. 不 publish 到 bus

**理由**：避免 agent loop 被自己的输出触发死循环。bus 上作为触发事件的只有 `external_input` 和 `tool_use`。

### 5. 异步 Tool Dispatch：最简方案

**决策**：agent loop 发现 tool_use 后，按工具类型 if-else 分发：

**Shell 类（action/skill_run）**：
- 调用 `tool.Call()` 立刻返回 session_id + status:running
- TmuxMonitor 异步感知状态变化
- 完成后回写 external_input（含 stdout/exit_code/stable 等状态信息）到 bus
- **BREAKING**：移除 exec 同步路径，全部走 tmux async

**子 agent 类（knowledge/recall）**：
- 启动 goroutine 调用 `子agent.Run(ctx, inv)`
- goroutine 等待 eventCh 关闭，收集最终输出
- 回写 external_input（子 agent 最终结果）到 bus

**不引入 Task/Backend 抽象层**：两种路径的差异只在"怎么知道完成了"，回写事件的方式统一。代码里直接 if-else 分发。

**替代方案**：引入统一 Task interface + 多 Backend 实现。否决：增加复杂度，当前两种 backend 足够简单，if-else 更直接。

### 6. 全 tmux 化：shell 命令统一异步

**决策**：action tool 的 `Call` 不再有 exec/tmux_exec 两种模式，统一走 tmux async。

**影响**：
- `tool.Call()` 返回 `session_id + status:running`
- model 在下一轮看到命令执行结果（exit code / stdout / stderr / stable 状态）
- 状态事件信息足够 model 决策下一步
- tool description 需更新：告知 model 结果稍后以事件形式送达

**替代方案**：保留 exec 同步路径。否决：两套路径增加复杂度，且同步 exec 会阻塞 agent loop。

### 7. 与 framework 的衔接

**决策**：保留 `runner.Runner` 作为外壳，`TagentAgent` 仍实现 `agent.Agent`。

```
runner.Run(ctx, userID, sessionID, msg)
  → 创建/获取 session
  → 调用 agent.RunWithPlugins(ctx, inv, ta)  // 复用 plugin/session/trace
  → ta.Run(ctx, inv)  // TagentAgent.Run
    → 将 inv.Message 作为首个 external_input 发布到 bus
    → 启动 AgentLoop
    → AgentLoop 产出的事件通过 eventCh 返回
  → runner.processAgentEvents 持久化 + 转发到 outputCh
```

**复用**：session.Service、plugin.PluginManager、MemoryPlugin.OnEvent、SummaryPlugin、trace/span、错误处理。

**不使用**：LLMAgent、llmflow、graph（tagent 内不再引用）。

### 8. 子 agent 的事件管线隔离

**决策**：子 agent 有自己的 EventBus、AgentLoop、Preprocessor，与父 agent 完全隔离。父 agent 只看到 Tool Dispatch goroutine 返回的最终 external_input。

## Risks / Trade-offs

**[Risk] 全 tmux 化对短命令的开销** → tmux session pool 复用 + 快速 completed 检测（进程退出即完成）。短命令的 tmux 启动开销在 ~50ms 级别，可接受。

**[Risk] runner.Run() 隐式功能丢失** → runner 承担了 session 管理、plugin 调用、trace、错误处理、流式输出。保留 runner 作为外壳可复用大部分功能，但需要在 AgentLoop 里补充事件发射机制（通过 eventCh 返回给 runner 的 processAgentEvents）。

**[Risk] A2A server 路径** → `a2a_server.go` 现在通过 `agent.Run()` 调用。新架构下 A2A 请求转为 bus 的首个 external_input，agent loop 接管。A2A 的流式输出通过 outputCh 转发。

**[Risk] 子 agent goroutine 泄漏** → 子 agent.Run 如果永不结束（如 LLM 挂起），goroutine 泄漏。→ 使用 context.WithTimeout 控制，超时后回写 error external_input。

**[Risk] 测试重写量大** → 现有大量测试依赖 runner.Run() 和 mockRunner。→ 分阶段迁移：先建 AgentLoop 骨架 + 新测试，再逐步迁移旧测试。

**[Trade-off] 事件顺序不保证** → 用户明确接受。model 在 react 模式下自行判断工具执行条件是否满足，不依赖事件先后顺序。

**[Trade-off] agent_output 不进 bus** → 意味着 agent 无法被自己的输出触发后续动作。如果需要 chain-of-thought（如 agent 先输出 plan，再执行），需要通过 tool_use 实现，而非 agent_output。

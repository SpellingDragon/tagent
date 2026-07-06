## Context

tagent 的核心设计哲学是**三层上下文分离**：

```
LLM 世界（只能看到 event_key 数字）
  ↓
Wrapper 工程层（解析 event_key → 还原完整事件 → 投递上下文）
  ↓
子 Agent 世界（收到已还原的上下文，无需感知 key 机制）
```

当前 Wrapper 的工程层通过 `IngestExternalEvents`（struct 字段赋值）+ `RunSimple`（直接方法调用）完成投递。这两个操作都是进程内的，当子 agent 部署为远程 tagent 服务时，上下文传递链路断裂。

trpc-agent-go 框架已提供完整的 A2A 通信能力：
- `a2aagent.A2AAgent` 实现了 `agent.Agent` 接口，通过 `WithTransferStateKey` 将 `Invocation.RunOptions.RuntimeState` 中的指定 key 传递到 A2A message metadata
- `server/a2a` 接收 A2A 请求时自动执行 `agent.WithRuntimeState(message.Metadata)`（server.go:377），将 metadata 映射回 RuntimeState

tagent 的 `TagentAgent` 已实现 `agent.Agent` 接口（含 Run 方法），具备直接接入 trpc A2A 通信层的基础。

## Goals / Non-Goals

**Goals:**
- AgentToolWrapper 统一本地和远程子 agent 调用路径，通过 `agent.Agent` 接口消除 `*TagentAgent` 紧耦合
- 上下文传递改用 `RuntimeState["external_context"]`，使远程子 agent 能收到 wrapper 工程化还原的事件上下文
- TagentAgent 能作为 A2A server 暴露，接收远程调用并自动获取 external_context
- tagent 配置聚焦 agent 定义（模型、超参、prompt），通信配置（A2A URL）作为 ToolRef 的声明式字段
- 通信完全通过 trpc 框架实现：本地 = `agent.Run()` 直接调用，远程 = `a2aagent.A2AAgent` → trpc-a2a-go HTTP

**Non-Goals:**
- 不实现远程 MemoryStore proxy（远程子 agent 使用自身 MemoryStore，external_context 提供最小够用的上下文）
- 不实现 ReadPartitionIDs 的远程化（跨进程的分区查询属于未来扩展）
- 不实现跨进程因果链链接（分布式系统的固有限制，external_context 提供上下文链接）
- 不使用 trpc_go.yaml 配置文件（trpc-agent-go 不使用 trpc 服务框架配置，所有配置通过 Go options）

## Decisions

### D1: 上下文传递载体 — RuntimeState 替代 struct 字段

**选择**：`Invocation.RunOptions.RuntimeState["external_context"]`

**理由**：
1. trpc-agent-go 的 A2AAgent 通过 `WithTransferStateKey` 自动将 RuntimeState key 传递到 A2A metadata
2. A2A Server 通过 `agent.WithRuntimeState(message.Metadata)` 自动将 metadata 映射回 RuntimeState
3. 整个远程链路零额外代码——RuntimeState 是 trpc 框架原生的上下文传递机制

**备选方案**：
- `IngestExternalEvents` struct 字段：进程内有效，无法跨越 A2A 边界
- ProcessMessageHook 手动提取 metadata：多余代码，server.go:377 已自动完成

**设计哲学贡献**：强化"LLM 选 key，wrapper 工程化还原"的三层分离。远程化后 wrapper 仍负责 event_key 解析和上下文投递，只是投递载体从 struct 字段变为 RuntimeState。LLM 和子 agent 的世界观不变——LLM 仍只输出数字 key，子 agent 仍只收到已还原的上下文。

### D2: 序列化格式 — 精简 ExternalContextEntry

**选择**：序列化为 `[]ExternalContextEntry`（仅 EventKey、EventType、EventSummary），不序列化完整 Content

```go
type ExternalContextEntry struct {
    EventKey     int64  `json:"event_key"`
    EventType    string `json:"event_type"`
    EventSummary string `json:"event_summary"`
}
```

**理由**：
1. `injectExternalContext` 当前只用 `EventSummary`，不用 `Content`
2. A2A message metadata 有大小限制，完整 Content 可能很大
3. 远程子 agent 如需完整事件，可通过自身 MemoryStore 查询

**备选方案**：
- 完整 `FullEvent` 序列化：信息完整但体积大，可能超出 A2A metadata 限制
- 仅传 EventKey：子 agent 需要访问父 MemoryStore 查询，远程不可行

### D3: AgentToolWrapper 泛化 — agent.Agent 接口

**选择**：`agent *TagentAgent` → `agent agent.Agent`

**理由**：
1. TagentAgent 已实现 agent.Agent（含 Run 方法）
2. A2AAgent 也实现 agent.Agent
3. Wrapper 只需调用 `agent.Run(ctx, inv)`，不关心是本地还是远程
4. Declaration 中的 name 从 `w.agent.name` 改为 `w.agent.Info().Name`

**Call 方法流程变化**：
```
现在:                                    改后:
1. 解析 event_keys                       1. 解析 event_keys              (不变)
2. parentStore.GetEvent → FullEvents     2. parentStore.GetEvent         (不变)
3. agent.IngestExternalEvents(events)    3. 序列化 → ExternalContextEntry[]
4. agent.RunSimple(ctx, user, sess, msg) 4. 构造 Invocation{RuntimeState, Message}
                                         5. agent.Run(ctx, inv)          ← 统一接口
5. 收集 event.Event 输出                  6. 收集 event.Event 输出         (不变)
```

### D4: TagentAgent.Run 双路径读取

**选择**：Run 方法同时支持 RuntimeState 和 pendingExternalEvents

```go
func (ta *TagentAgent) Run(ctx, inv) {
    // 新增: 从 RuntimeState 读取 (远程/wrapper 路径)
    if ec, ok := inv.RunOptions.RuntimeState["external_context"]; ok {
        events := deserializeExternalContext(ec)
        ta.IngestExternalEvents(events)
    }
    // 保留: struct 字段路径 (直接 API 调用)
    message := inv.Message
    if len(ta.pendingExternalEvents) > 0 {
        message = ta.injectExternalContext(message)
    }
    return ta.runner.Run(...)
}
```

**理由**：向后兼容直接 API 调用（IngestExternalEvents + Run），同时支持远程 RuntimeState 路径。项目孵化期不保留无用的兼容路径——IngestExternalEvents 作为 public API 保留是因为直接调用场景（如测试、嵌入式使用）仍需要它。

### D5: 配置分层 — tagent 聚焦 agent 定义，通信配置声明式

**tagent YAML 配置（agent 定义层）**：
```yaml
agents:
  tagent:
    model: glm-4
    system_prompt: "resources/prompts/bootstrap"
    max_tokens: 8000
    temperature: 0.7
    tools:
      - kind: agent
        id: knowledge
        model: glm-4
        prompt: { file: "knowledge_agent" }
        max_tool_iterations: 10
        event_params: ["event_key"]
        # 通信配置: 声明式，不在此定义通信细节
        remote:
          url: "http://knowledge-service:8088"
      - kind: agent
        id: recall
        model: glm-4
        prompt: { file: "recall_agent" }
        # 无 remote = 本地 agent
```

**trpc A2A 通信层（代码层配置，通过 Go options）**：
```go
// tagent.go 内部，根据 ToolRef.Remote 创建 A2AAgent
a2aAgent, _ := a2aagent.New(
    a2aagent.WithAgentCardURL(ref.Remote.URL),       // 连接地址
    a2aagent.WithTransferStateKey("external_context"), // 上下文传递
    a2aagent.WithEnableStreaming(true),               // 流式
)
```

**配置关系**：
- tagent YAML：定义"使用哪些 agent、agent 用什么模型、超参、prompt"——这是应用语义
- `ToolRef.Remote.URL`：声明"这个 agent 在哪里"——这是连接信息
- trpc Go options：定义"如何连接"（A2A 协议、metadata 传递、流式）——这是通信细节

**为什么不用 trpc_go.yaml？**
trpc-agent-go 不是 trpc 服务框架，不使用 trpc_go.yaml。所有 A2A 通信配置通过 Go options 完成，这些 options 在 tagent 内部根据 ToolRef.Remote 自动生成。用户只需在 tagent YAML 中声明 `remote.url`，通信细节由 tagent 工程化处理。

### D6: A2A Server — 直接使用 TagentAgent

**选择**：`server/a2a.New(WithAgent(tagentAgent), WithHost(host))`

**理由**：
1. TagentAgent 已实现 agent.Agent，无需 adapter
2. server.go:377 自动将 A2A metadata 映射到 RuntimeState
3. TagentAgent.Run 从 RuntimeState 读取 external_context
4. 无需 ProcessMessageHook——metadata → RuntimeState 是自动的

**Server 启动**：
```go
func NewA2AServer(ta *TagentAgent, host string) (*a2a.A2AServer, error) {
    return a2a.New(a2a.WithAgent(ta, true), a2a.WithHost(host))
}
```

### D7: Factory 返回类型 — 保持 *TagentAgent

**选择**：`ToolAgentFactory` 保持返回 `*TagentAgent`，远程 agent 不经过 factory

**理由**：
1. Factory 创建的是本地 TagentAgent（需要 model、prompt、sub-tools 等配置）
2. 远程 agent 是 A2AAgent（只需 URL + transferStateKey），创建逻辑完全不同
3. 在 `buildAgentToolRef` 中根据 `ref.Remote` 分流：
   - `ref.Remote != nil` → 创建 A2AAgent → 包装为 AgentToolWrapper
   - `ref.Remote == nil` → 调用 factory 创建 TagentAgent → 包装为 AgentToolWrapper
4. 两种路径产出的都是 `agent.Agent`，AgentToolWrapper 统一处理

## Risks / Trade-offs

- **[ReadPartitionIDs 远程不可用]** → 远程子 agent 使用自身 MemoryStore。external_context（EventSummary）提供最小够用的上下文。如需查询父 agent 历史事件，属于未来远程 MemoryStore proxy 的扩展范围。

- **[A2A metadata 大小限制]** → 使用精简序列化（ExternalContextEntry 仅含 EventKey + EventType + EventSummary），不传完整 Content。单个事件摘要通常 < 200 字符。

- **[AgentToolWrapper 签名变化]** → 项目孵化期，不考虑兼容。所有调用方在 tagent.go 和 builtin.go 中，改动可控。

- **[远程 agent 健康检查]** → 当前不实现。A2AAgent.Run 失败时返回错误，wrapper 将错误传递给父 agent 的 LLM。未来可增加重试或 fallback 机制。

- **[Persistent Loop 与 A2A Server 共存]** → A2A Server 模式使用 one-shot Run（每次请求独立），不使用 Persistent Loop。两种模式是不同使用场景，不冲突。

## Open Questions

- RemoteConfig 是否需要 `Timeout`、`Headers` 等字段？建议先最小化（仅 URL），后续按需添加。
- A2A Server 是否需要在 tagent Config 中配置，还是通过独立 API 启动？建议独立 API（`agent.NewA2AServer`），保持 Config 聚焦 agent 定义。

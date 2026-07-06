## Why

tagent 的 AgentToolWrapper 当前紧耦合 `*TagentAgent`，通过 `IngestExternalEvents`（struct 字段赋值）和 `RunSimple`（直接方法调用）传递上下文。这两个操作都是进程内的，无法跨越 A2A 协议边界。当子 agent 部署为远程 tagent 服务时，父 agent 精心挑选的 event_key 所还原的完整事件上下文无法投递到远程子 agent，导致上下文传递断裂。

同时，tagent 与 trpc-agent-go 框架的配置边界缺乏明确划分：tagent 应聚焦 agent 本身的定义（使用哪些 agent、模型、超参），而 trpc 侧应聚焦通信层定义（特定 agent 的连接方式、通信配置等）。

## What Changes

- **AgentToolWrapper 泛化**：`agent *TagentAgent` → `agent agent.Agent`，统一本地和远程子 agent 的调用路径
- **上下文传递改用 RuntimeState**：EventKey 解析后的外部事件序列化为 JSON，通过 `Invocation.RunOptions.RuntimeState["external_context"]` 传递，替代 `IngestExternalEvents` struct 字段赋值
- **TagentAgent.Run 增加 RuntimeState 读取**：从 `inv.RunOptions.RuntimeState` 反序列化外部事件，复用现有 `injectExternalContext` 机制注入消息
- **远程子 agent 支持**：`ToolRef.Remote` 配置声明 A2A 连接 URL，通过 `a2aagent.New(WithAgentCardURL, WithTransferStateKey)` 创建远程 agent
- **A2A Server 模式**：tagent 可作为 A2A server 暴露，`server/a2a` 自动将 A2A metadata 映射到 RuntimeState，远程请求方传递的 external_context 自动到达 TagentAgent.Run
- **配置分层**：tagent YAML 聚焦 agent 定义（模型、超参、prompt），通信配置（A2A URL）作为 ToolRef 的 Remote 字段声明
- **BREAKING**：`AgentToolWrapper` 构造函数参数类型从 `*TagentAgent` 改为 `agent.Agent`；`NewAgentToolWrapper` 签名变化

## Capabilities

### New Capabilities
- `remote-agent-communication`: 远程子 agent 的 A2A 通信能力，包括 AgentToolWrapper 泛化、RuntimeState 上下文传递、A2A server 模式

### Modified Capabilities
（无现有 spec 的需求变更）

## Impact

- **代码**：`agent/tool_agent.go`（AgentToolWrapper 核心改造）、`agent/tagent_agent.go`（Run 增加 RuntimeState 读取）、`config.go`（ToolRef.Remote）、`tagent.go`（buildAgentToolRef 支持 remote）、新建 `agent/a2a_server.go`
- **依赖**：`trpc-a2a-go` 从 indirect 提升为 direct 依赖
- **API**：`NewAgentToolWrapper` 签名变化（`*TagentAgent` → `agent.Agent`）
- **文档**：agent-architecture.md、tool-architecture.md 需同步修订
- **设计哲学**：强化"LLM 选 key，wrapper 工程化还原"的三层分离——远程化后 wrapper 仍负责 event_key 解析和上下文投递，只是投递载体从 struct 字段变为 RuntimeState

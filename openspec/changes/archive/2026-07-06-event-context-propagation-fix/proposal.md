## Why

在实际运行中发现三个问题：

1. **event_keys 传递链路断裂**：LLM 调用子 Agent（knowledge/recall/action）时不传递 `event_keys`，导致子 Agent 收不到父 Agent 的事件上下文。根因是：(a) 系统 prompt 中完全没有提到 event_keys 的使用方法；(b) 即使 LLM 知道参数存在，也没有自动兜底机制。

2. **子 Agent 尾部事件被丢弃**：`TagentAgent.Run()` 的 wrappedCh goroutine 在收到最终响应后立即 `return`，触发 `defer runCancel()` 取消 context。但框架 Runner 此时可能还有尾部事件（如 MemoryPlugin 持久化）正在通过 `EmitEventWithTimeout` 发送，context 被取消导致事件丢弃，事件链不完整。

3. **资源清理不完整**：(a) `TrajectoryRecorder` 有 `Close()` 方法但 `TagentAgent.Close()` 未调用它，writeLoop goroutine 和文件句柄泄漏；(b) 每次 `Run()` 创建的临时 `ContextManager`（含 Runner）未被 `Close()`，Runner 内部 goroutine pool 和 session 缓存泄漏。

## What Changes

- **自动注入 event_keys（方案 B+C 混合）**：当 LLM 未传 event_keys 且 `AgentToolWrapper` 配置了 `event_params` 时，自动从父 Agent 的 SessionProjection 中选择最近 N 个事件（默认 5）的 EventKey 注入。同时更新 prompt 引导 LLM 主动选择相关 event_keys。
- **子 Agent 最终响应后 drain 尾部事件**：`TagentAgent.Run()` 的 wrappedCh goroutine 收到最终响应后不立即 return，而是进入 drain 模式（500ms 超时），转发剩余尾部事件，确保 MemoryPlugin 完成持久化后再关闭 channel 和取消 context。
- **子 Agent ContextManager 清理**：`Run()` 的 runEventLoop goroutine 退出后调用 `invCM.Close()`，释放临时 Runner 资源。
- **TrajectoryRecorder 清理**：`TagentAgent.Close()` 中在 `contextManager.Close()` 后调用 `trajectoryRecorder.Close()`（如果已设置），确保 writeLoop flush + 文件关闭。
- **更新 prompt 文件**：在 TOOLS.md 中增加 event_keys 使用指南，在 knowledge/recall tool description 中说明 event_keys 参数的作用。

## Capabilities

### New Capabilities

- `auto-event-key-injection`: AgentToolWrapper 在 LLM 未传 event_keys 时自动从父 Agent 投影注入最近 N 个事件
- `subagent-event-drain`: 子 Agent 最终响应后 drain 尾部事件，防止 MemoryPlugin 持久化事件丢失
- `resource-lifecycle-cleanup`: TrajectoryRecorder + 子 Agent ContextManager 的资源清理

### Modified Capabilities

- `persistent-event-loop`: 更新 Run() wrappedCh goroutine 的最终响应处理逻辑（drain 模式）+ invCM.Close()
- `remote-agent-communication`: 更新 AgentToolWrapper.Call 的自动注入逻辑
- `trajectory-recording`: 更新 Close() 时序，TagentAgent.Close() 调用 TrajectoryRecorder.Close()

## Impact

**代码变更范围**：
- `agent/tool_agent.go` — `AgentToolWrapper` 新增 `parentProjection` 字段 + auto-inject 逻辑
- `agent/tagent_agent.go` — `Run()` drain 模式 + invCM.Close() + `Close()` 增加 TrajectoryRecorder 关闭
- `tagent.go` — `buildAgentToolRef` 传入 parentProjection
- `examples/wechat-bot/resources/prompts/TOOLS.md` — 增加 event_keys 使用指南
- `examples/wechat-bot/resources/prompts/knowledge_tool_desc.md` — 说明 event_keys
- `examples/wechat-bot/resources/prompts/recall_tool_desc.md` — 说明 event_keys

**不涉及**：
- InjectEventKeys BeforeModel 回调逻辑不变
- MemoryPlugin/SummaryPlugin 不变
- prototype/agent.go 不变
## Why

在实际运行中发现两个问题：

1. **event_keys 传递链路断裂**：LLM 调用子 Agent（knowledge/recall/action）时不传递 `event_keys`，导致子 Agent 收不到父 Agent 的事件上下文。根因是：(a) 系统 prompt 中完全没有提到 event_keys 的使用方法；(b) 即使 LLM 知道参数存在，也没有自动兜底机制。

2. **子 Agent 尾部事件被丢弃**：`TagentAgent.Run()` 的 wrappedCh goroutine 在收到最终响应后立即 `return`，触发 `defer runCancel()` 取消 context。但框架 Runner 此时可能还有尾部事件（如 MemoryPlugin 持久化）正在通过 `EmitEventWithTimeout` 发送，context 被取消导致事件丢弃，事件链不完整。

## What Changes

- **自动注入 event_keys（方案 B+C 混合）**：当 LLM 未传 event_keys 且 `AgentToolWrapper` 配置了 `event_params` 时，自动从父 Agent 的 SessionProjection 中选择最近 N 个事件（默认 5）的 EventKey 注入。同时更新 prompt 引导 LLM 主动选择相关 event_keys。
- **子 Agent 最终响应后 drain 尾部事件**：`TagentAgent.Run()` 的 wrappedCh goroutine 收到最终响应后不立即 return，而是进入 drain 模式（500ms 超时），转发剩余尾部事件，确保 MemoryPlugin 完成持久化后再关闭 channel 和取消 context。
- **更新 prompt 文件**：在 TOOLS.md 中增加 event_keys 使用指南，在 knowledge/recall tool description 中说明 event_keys 参数的作用。

## Capabilities

### New Capabilities

- `auto-event-key-injection`: AgentToolWrapper 在 LLM 未传 event_keys 时自动从父 Agent 投影注入最近 N 个事件
- `subagent-event-drain`: 子 Agent 最终响应后 drain 尾部事件，防止 MemoryPlugin 持久化事件丢失

### Modified Capabilities

- `persistent-event-loop`: 更新 Run() wrappedCh goroutine 的最终响应处理逻辑（drain 模式）
- `remote-agent-communication`: 更新 AgentToolWrapper.Call 的自动注入逻辑

## Impact

**代码变更范围**：
- `agent/tool_agent.go` — `AgentToolWrapper.Call` 增加 auto-inject 逻辑
- `agent/tagent_agent.go` — `Run()` wrappedCh goroutine 增加 drain 模式
- `examples/wechat-bot/resources/prompts/TOOLS.md` — 增加 event_keys 使用指南
- `examples/wechat-bot/resources/prompts/knowledge_tool_desc.md` — 说明 event_keys
- `examples/wechat-bot/resources/prompts/recall_tool_desc.md` — 说明 event_keys

**不涉及**：
- InjectEventKeys BeforeModel 回调逻辑不变
- MemoryPlugin/SummaryPlugin 不变
- prototype/agent.go 不变

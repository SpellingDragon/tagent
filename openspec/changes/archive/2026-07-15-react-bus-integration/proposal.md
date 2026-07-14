## Why

tagent 的核心设计哲学是"事件驱动、随时 InjectMessage"。但当前实现中，`runEventLoop` 在 `RunFlow` 执行期间（可能持续数分钟，包括 SmartCompress + 多轮 ReAct + 子 Agent 调用）无法 Pull 新事件。用户在 Agent 执行工具期间发送的消息要等到整个 RunFlow 结束才被处理。

框架 trpc-agent-go 的 ReAct 循环每次 LLM 调用前都从 session 重新构建 messages，但 session 中没有新的用户消息——它们在 EventBus 中等待。

**期望的事件流**：
```
user → think → tool → think → user(新消息插入) → tool → think → output
```

## What Changes

- **EventBus 新增 `TryPull` 方法**：非阻塞读取所有 pending 事件（与 `Pull` 的批量 drain 逻辑一致，但不阻塞等待第一个事件）。
- **BeforeModel 新增 `InjectBusInputs` 回调**：在每次 LLM 调用前，TryPull EventBus 中的新用户消息，追加到 `args.Request.Messages`。LLM 在当前 ReAct 循环中就能看到新用户消息。
- **TryPull 到的用户消息手动触发 onEvent**：确保持久化到 MemoryStore + 追加到 SessionProjection，与正常 `runEventLoop` Pull 的事件走相同的处理路径。

## Capabilities

### New Capabilities

- `react-bus-integration`: ReAct 循环期间通过 BeforeModel 回调注入 EventBus 中的新用户消息

### Modified Capabilities

- `persistent-event-loop`: runEventLoop 的 RunFlow 期间，新用户消息通过 BeforeModel 回调注入而非等待 RunFlow 结束

## Impact

**代码变更范围**：
- `agent/event_bus.go` — 新增 `TryPull` 方法
- `agent/context_manager.go` — 新增 `InjectBusInputs` BeforeModel 回调

**不涉及**：
- 不修改框架 trpc-agent-go 代码
- 不修改 runEventLoop 主循环逻辑
- 不修改 SmartCompressor/Compactor
- 不修改 MemoryPlugin/SummaryPlugin

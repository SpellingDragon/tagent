## Why

当前 `tagent` 在 `agent/agent_loop.go` 中自建了完整的 ReAct 循环（Pull → dispatch → callModel → handleResponse），与 `trpc-agent-go` 框架的 `Flow.Run`、`ContentRequestProcessor`、`FunctionCallResponseProcessor` 能力高度重叠。这不仅导致约 1000 行重复代码，还使我们无法自动获得框架提供的 tracing/telemetry、标准化回调（BeforeModel/AfterModel）以及上游 bugfix。

阶段一/二已经让 `SessionProjection`、`Compact` 和事件语义就位，`SmartCompressor` 与 `Compactor` 已经在 `Preprocessor.Process` 中按"先压缩 messages、再 compact projection"的顺序协同工作。但两者各自实现了独立的任务分段逻辑（`splitByTaskBoundary` 操作 `[]model.Message` vs `splitTasks` 操作 `[]EventReference`），存在边界定义不一致的隐患。

现在应该把执行引擎对齐到框架，降低维护成本并提升可观测性，同时归一化任务分段逻辑。

## What Changes

- 新增 `FrameworkFlowAdapter`，把 `TagentAgent.StartLoop` 的持久事件流转换为对 `trpc-agent-go` `LLMAgent.Run` / `Flow.Run` 的单条调用。
- 将 `AgentLoop.Run` 的自定义 ReAct 循环替换为框架 `Flow.Run`；保留 `EventBus` 作为外部事件入口和工具结果回写通道。
- 把 `SmartCompressor` 注册为框架 `BeforeModel` 回调，在 `ContentRequestProcessor` 构建完 messages 后、调用模型前执行 SmartCompress。
- 把 `Compactor` 注册为独立的 `BeforeModel` 回调，在 SmartCompress 仍超 budget 时收缩 `SessionProjection`。
- **抽取公共 `TaskSegmenter`**，统一 `SmartCompressor` 和 `Compactor` 的任务边界定义，消除 `splitByTaskBoundary`（操作 messages）与 `splitTasks`（操作 EventReference）之间的不一致风险。
- 移除或降级 `AgentLoop` 中自建的 `callModel`、`handleResponse`、`dispatchToolUse` 逻辑；仅保留 `EventBus` 消费/生产、Tmux 异步注入和子 agent `Run` 包装。
- **BREAKING**: `AgentLoop` 的对外接口不变（`StartLoop/InjectMessage/StopLoop` 仍由 `TagentAgent` 提供），但内部执行路径从自定义循环切换为框架循环，日志、span 属性、错误码将与框架对齐。

## Capabilities

### New Capabilities
- `framework-flow-adapter`: 将 tagent 事件总线接入 `trpc-agent-go` `Flow.Run`，负责 `AgentEvent` → `agent.Invocation` 转换、`BeforeModel` 回调注册（含 SmartCompressor + Compactor）、工具结果回写到 bus。

### Modified Capabilities
- `persistent-event-loop`: 执行引擎从自建 ReAct 循环改为框架 `Flow.Run` 包装；保留 mailbox/drain/mergeBatch/outputCh 语义，但 `runner.Run()` 由 `LLMAgent.Run` / `Flow.Run` 替代。
- `event-compaction`: SmartCompressor 与 Compactor 的任务分段逻辑归一为公共 `TaskSegmenter`，Compact 触发点从 `Preprocessor.Process` 迁移到 `BeforeModel` 回调。

## Impact

- `agent/agent_loop.go`、`agent/tagent_agent.go` 重构最大。
- `agent/smart_compress.go`、`agent/compact.go`、`agent/preprocessor.go` 需适配 `TaskSegmenter` 抽取和 `BeforeModel` 注册。
- 依赖 `trpc-agent-go` 的 `agent.Invocation`、`Flow`、`BeforeModel`、`ContentRequestProcessor`、`FunctionCallResponseProcessor` 接口。
- `EventBus`、`SessionProjection`、`SmartCompressor`、`Compactor` 继续保留，但挂载点从 `Preprocessor.Process` 内联逻辑改为框架 `BeforeModel` 回调。
- 测试需补充框架 mock（`MockLLMAgent` / `MockFlow`），并验证 tmux 异步命令完成后仍能触发下一轮处理。
## Why

当前 `tagent` 在 `agent/agent_loop.go` 中自建了完整的 ReAct 循环（Pull → dispatch → callModel → handleResponse），与 `trpc-agent-go` 框架的 `Flow.Run`、`ContentRequestProcessor`、`FunctionCallResponseProcessor` 能力高度重叠。这不仅导致约 1000 行重复代码，还使我们无法自动获得框架提供的 tracing/telemetry、标准化回调（BeforeModel/AfterModel）以及上游 bugfix。

阶段一/二已经让 `SessionProjection`、`Compact` 和事件语义就位，`SmartCompressor` 与 `Compactor` 已经在 `Preprocessor.Process` 中按"先压缩 messages、再 compact projection"的顺序协同工作。但两者各自实现了独立的任务分段逻辑（`splitByTaskBoundary` 操作 `[]model.Message` vs `splitTasks` 操作 `[]EventReference`），存在边界定义不一致的隐患。

现在应该把执行引擎对齐到框架，降低维护成本并提升可观测性，同时归一化任务分段逻辑。

## What Changes

- 新增 `FrameworkFlowAdapter`，把 `TagentAgent.StartLoop` 的持久事件流转换为对 `trpc-agent-go` `LLMAgent.Run` / `Flow.Run` 的单条调用。
- 将 `AgentLoop.Run` 的自定义 ReAct 循环替换为框架 `Flow.Run`；保留 `EventBus` 作为外部事件入口和工具结果回写通道。
- 把 `SmartCompressor` 注册为框架 `BeforeModel` 回调，在 `ContentRequestProcessor` 构建完 messages 后、调用模型前执行 SmartCompress。
- 把 `Compactor` 注册为独立的 `BeforeModel` 回调，在 SmartCompress 仍超 budget 时收缩 `SessionProjection`。
- **抽取公共 `TaskSegmenter`**，统一 `SmartCompressor` 和 `Compactor` 的任务边界定义，消除 `splitByTaskBoundary`（操作 messages）与 `splitTasks`（操作 EventReference）之间的不一致风险。
- 移除或降级 `AgentLoop` 中自建的 `callModel`、`handleResponse`、`dispatchToolUse` 逻辑；仅保留 `EventBus` 消费/生产、Tmux 异步注入和子 agent `Run` 包装。
- **BREAKING**: `AgentLoop` 的对外接口不变（`StartLoop/InjectMessage/StopLoop` 仍由 `TagentAgent` 提供），但内部执行路径从自定义循环切换为框架循环，日志、span 属性、错误码将与框架对齐。

## Capabilities

### New Capabilities
- `framework-flow-adapter`: 将 tagent 事件总线接入 `trpc-agent-go` `Flow.Run`，负责 `AgentEvent` → `agent.Invocation` 转换、`BeforeModel` 回调注册（含 SmartCompressor + Compactor）、工具结果回写到 bus。

### Modified Capabilities
- `persistent-event-loop`: 执行引擎从自建 ReAct 循环改为框架 `Flow.Run` 包装；保留 mailbox/drain/mergeBatch/outputCh 语义，但 `runner.Run()` 由 `LLMAgent.Run` / `Flow.Run` 替代。
- `event-compaction`: SmartCompressor 与 Compactor 的任务分段逻辑归一为公共 `TaskSegmenter`，Compact 触发点从 `Preprocessor.Process` 迁移到 `BeforeModel` 回调。

## Impact

- `agent/agent_loop.go`、`agent/tagent_agent.go` 重构最大。
- `agent/smart_compress.go`、`agent/compact.go`、`agent/preprocessor.go` 需适配 `TaskSegmenter` 抽取和 `BeforeModel` 注册。
- 依赖 `trpc-agent-go` 的 `agent.Invocation`、`Flow`、`BeforeModel`、`ContentRequestProcessor`、`FunctionCallResponseProcessor` 接口。
- `EventBus`、`SessionProjection`、`SmartCompressor`、`Compactor` 继续保留，但挂载点从 `Preprocessor.Process` 内联逻辑改为框架 `BeforeModel` 回调。
- 测试需补充框架 mock（`MockLLMAgent` / `MockFlow`），并验证 tmux 异步命令完成后仍能触发下一轮处理。
## Why

当前 `tagent` 在 `agent/agent_loop.go` 中自建了完整的 ReAct 循环（Pull → dispatch → callModel → handleResponse），与 `trpc-agent-go` 框架的 `Flow.Run`、`ContentRequestProcessor`、`FunctionCallResponseProcessor` 能力高度重叠。这不仅导致约 1000 行重复代码，还使我们无法自动获得框架提供的 tracing/telemetry、标准化回调（BeforeModel/AfterModel）以及上游 bugfix。阶段一/二已经让 `SessionProjection`、`Compact` 和事件语义就位，现在应该把执行引擎对齐到框架，降低维护成本并提升可观测性。

## What Changes

- 新增 `FrameworkFlowAdapter`，把 `TagentAgent.StartLoop` 的持久事件流转换为对 `trpc-agent-go` `LLMAgent.Run` / `Flow.Run` 的单条调用。
- 将 `AgentLoop.Run` 的自定义 ReAct 循环替换为框架 `Flow.Run`；保留 `EventBus` 作为外部事件入口和工具结果回写通道。
- 把 `SmartCompressor` 注册为框架 `BeforeModel` 回调，在 `ContentRequestProcessor` 构建完 messages 后、调用模型前执行 SmartCompress。
- 把 `Compact` 注册为独立的 `BeforeModel` 回调或内嵌在压缩回调中，在模型调用前维护 `SessionProjection` 有界。
- 移除或降级 `AgentLoop` 中自建的 `callModel`、`handleResponse`、`dispatchToolUse` 逻辑；仅保留 `EventBus` 消费/生产、Tmux 异步注入和子 agent `Run` 包装。
- **BREAKING**: `AgentLoop` 的对外接口不变（`StartLoop/InjectMessage/StopLoop` 仍由 `TagentAgent` 提供），但内部执行路径从自定义循环切换为框架循环，日志、span 属性、错误码将与框架对齐。

## Capabilities

### New Capabilities
- `framework-flow-adapter`: 将 tagent 事件总线接入 `trpc-agent-go` `Flow.Run`，负责 `AgentEvent` → `agent.Invocation` 转换、`BeforeModel` 回调注册、工具结果回写到 bus。

### Modified Capabilities
- `persistent-event-loop`: 执行引擎从自建 ReAct 循环改为框架 `Flow.Run` 包装；保留 mailbox/drain/mergeBatch/outputCh 语义，但 `runner.Run()` 由 `LLMAgent.Run` / `Flow.Run` 替代。

## Impact

- `agent/agent_loop.go`、`agent/tagent_agent.go` 重构最大。
- 依赖 `trpc-agent-go` 的 `agent.Invocation`、`Flow`、`BeforeModel`、`ContentRequestProcessor`、`FunctionCallResponseProcessor` 接口。
- `EventBus`、`SessionProjection`、`SmartCompressor`、`Compactor` 继续保留，但挂载点从 `AgentLoop` 回调改为框架回调。
- 测试需补充框架 mock（`MockLLMAgent` / `MockFlow`），并验证 tmux 异步命令完成后仍能触发下一轮处理。

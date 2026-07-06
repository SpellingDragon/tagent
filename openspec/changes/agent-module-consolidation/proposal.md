## Why

当前 `agent/` 包有 17 个源文件、9288 行代码。经过三个阶段演进后，存在以下问题：

1. **`Preprocessor` 与 `FrameworkFlowAdapter` 职责重叠**：框架路径下 `Preprocessor.Process()` 是死代码，`FrameworkFlowAdapter` 的 Compactor 回调又临时 `NewPreprocessor` 来复用工具函数。
2. **`MemoryPlugin.OnEvent` 被双重调用**：框架 Runner 内部通过 Plugin 机制自动调用 `MemoryPlugin.OnEvent`，`makeOnEventCallback` 又直接调用一次，导致 MemoryStore 重复写入。
3. **`AgentLoop` 持有大量死字段**：`m`、`tools`、`toolMap`、`toolIterations`、`maxToolIters`、`systemPrompt`、`temperature`、`preprocessor` 在框架路径下全部未使用。
4. **子 agent `Run()` 路径走 legacy**：`Run()` 没有创建 `FrameworkFlowAdapter`，走的是已待删除的 `callModel`/`handleResponse` 路径。
5. **5 个小文件碎片化**：`compact.go`（97 行）、`session_projection.go`（56 行）、`event_reference_builder.go`（54 行）、`token_counter.go`（42 行）、`a2a_server.go`（56 行）各有独立文件但职责内聚。

原型 `prototype/agent.go` 展示了目标架构的简洁性：`BaseTAgent` 只有 7 个字段。生产代码应该同样清晰。

## What Changes

- **合并 `Preprocessor` + `FrameworkFlowAdapter` → `ContextManager`**：统一消息构建、压缩编排、Compact、Flow 执行。消除死代码和临时构造。
- **统一 Runner**：`ContextManager` 创建唯一的 Runner，同时注册 LLMAgent + MemoryPlugin + SummaryPlugin + SessionService。
- **修复 `MemoryPlugin.OnEvent` 双重调用**：`makeOnEventCallback` 不再直接调用 `memPlugin.OnEvent`，仅做 `sessionSvc.AppendEvent` + `projection.Append`。
- **合并 5 个碎片化小文件**。
- **删除 legacy 兼容路径和 `UseFrameworkFlow` flag**。
- **修复子 agent `Run()` 路径**：创建 `ContextManager` 代替 legacy 路径。
- **修复 `SmartCompressor` 使用传入的 `TokenCounter`**。

## Capabilities

### New Capabilities
- `context-manager`: 统一的消息构建、压缩编排、Compact 触发和 Flow 执行入口，含统一 Runner。

### Modified Capabilities
- `persistent-event-loop`: AgentLoop 从双路径变为单一路径，删除 `UseFrameworkFlow` flag。`makeOnEventCallback` 不再调用 `memPlugin.OnEvent`。
- `event-compaction`: 压缩/Compact 触发点统一到 `ContextManager` 的 `BeforeModel` 回调。

## Impact

- `agent/preprocessor.go` → 删除，合并到 `agent/context_manager.go`。
- `agent/framework_flow_adapter.go` → 删除，合并到 `agent/context_manager.go`。
- `agent/compact.go` → 删除，合并到 `agent/task_segmenter.go`。
- `agent/session_projection.go` + `agent/event_reference_builder.go` → 合并为 `agent/projection.go`。
- `agent/token_counter.go` → 删除，内嵌到 `agent/context_manager.go`。
- `agent/a2a_server.go` → 删除，移到 `agent/tagent_agent.go`。
- `agent/agent_loop.go` → 大幅瘦身。
- `agent/smart_compress.go` → 直接调用 `SegmentMessages`，使用传入的 `TokenCounter`。
- `agent/tagent_agent.go` → 适配 `ContextManager`，修复 `makeOnEventCallback`，删除 feature flag，修复子 agent `Run()` 路径。
- `plugin/memory_plugin.go` → 无代码变更，但调用来源从 `makeOnEventCallback` 改为仅框架 Plugin。
## Why

当前 `agent/` 包有 17 个源文件、9288 行代码。经过三个阶段演进后，存在以下问题：

1. **`Preprocessor` 与 `FrameworkFlowAdapter` 职责重叠**：框架路径下 `Preprocessor.Process()` 是死代码，`FrameworkFlowAdapter` 的 Compactor 回调又临时 `NewPreprocessor` 来复用工具函数。
2. **`MemoryPlugin.OnEvent` 被双重调用**：框架 Runner 内部通过 Plugin 机制自动调用 `MemoryPlugin.OnEvent`，`makeOnEventCallback` 又直接调用一次，导致 MemoryStore 重复写入。
3. **`AgentLoop` 持有大量死字段**：`m`、`tools`、`toolMap`、`toolIterations`、`maxToolIters`、`systemPrompt`、`temperature`、`preprocessor` 在框架路径下全部未使用。
4. **子 agent `Run()` 路径走 legacy**：`Run()` 没有创建 `FrameworkFlowAdapter`，走的是已待删除的 `callModel`/`handleResponse` 路径。
5. **5 个小文件碎片化**：`compact.go`（97 行）、`session_projection.go`（56 行）、`event_reference_builder.go`（54 行）、`token_counter.go`（42 行）、`a2a_server.go`（56 行）各有独立文件但职责内聚。

原型 `prototype/agent.go` 展示了目标架构的简洁性：`BaseTAgent` 只有 7 个字段。生产代码应该同样清晰。

## What Changes

- **合并 `Preprocessor` + `FrameworkFlowAdapter` → `ContextManager`**：统一消息构建、压缩编排、Compact、Flow 执行。消除死代码和临时构造。
- **修复 `MemoryPlugin.OnEvent` 双重调用**：`makeOnEventCallback` 不再直接调用 `memPlugin.OnEvent`，仅做 `sessionSvc.AppendEvent` + `projection.Append`。MemoryStore 写入由框架 Plugin 机制处理。
- **合并 `compact.go` + `task_segmenter.go` → `task_segmenter.go`**。
- **合并 `session_projection.go` + `event_reference_builder.go` → `projection.go`**。
- **合并 `token_counter.go` → `context_manager.go`**。
- **合并 `a2a_server.go` → `tagent_agent.go`**。
- **删除 legacy 路径**：移除 `callModel`/`handleResponse`/`dispatchToolUse`/`UseFrameworkFlow` flag。`AgentLoop` 退化为事件循环薄包装。
- **修复子 agent `Run()` 路径**：创建 `ContextManager` 代替 legacy 路径。
- **修复 `SmartCompressor` 内部 `NewDefaultTokenCounter`**：使用 `ContextManager` 传入的 `TokenCounter`。

## Capabilities

### New Capabilities
- `context-manager`: 统一的消息构建、压缩编排、Compact 触发和 Flow 执行入口。

### Modified Capabilities
- `persistent-event-loop`: AgentLoop 从双路径变为单一路径，删除 `UseFrameworkFlow` flag。`makeOnEventCallback` 不再调用 `memPlugin.OnEvent`（由框架 Plugin 机制处理）。
- `event-compaction`: 压缩/Compact 触发点统一到 `ContextManager` 的 `BeforeModel` 回调。

## Impact

- `agent/preprocessor.go` → 删除，合并到 `agent/context_manager.go`。
- `agent/framework_flow_adapter.go` → 删除，合并到 `agent/context_manager.go`。
- `agent/compact.go` → 删除，合并到 `agent/task_segmenter.go`。
- `agent/session_projection.go` + `agent/event_reference_builder.go` → 合并为 `agent/projection.go`。
- `agent/token_counter.go` → 删除，内嵌到 `agent/context_manager.go`。
- `agent/a2a_server.go` → 删除，移到 `agent/tagent_agent.go`。
- `agent/agent_loop.go` → 大幅瘦身。
- `agent/smart_compress.go` → 直接调用 `SegmentMessages`，使用传入的 `TokenCounter`。
- `agent/tagent_agent.go` → 适配 `ContextManager`，修复 `makeOnEventCallback`，删除 feature flag，修复子 agent `Run()` 路径。
- `plugin/memory_plugin.go` → 无代码变更，但调用来源从 `makeOnEventCallback` 改为仅框架 Plugin。
## Why

当前 `agent/` 包有 17 个源文件、9288 行代码。阶段三引入 `FrameworkFlowAdapter` 后，`Preprocessor` 和 `FrameworkFlowAdapter` 之间存在严重职责重叠：框架路径下 `Preprocessor.Process()` 是死代码，`FrameworkFlowAdapter` 又临时 `NewPreprocessor` 来复用工具函数。同时 5 个小文件（`compact.go`、`session_projection.go`、`event_reference_builder.go`、`token_counter.go`、`a2a_server.go`）各有 40-97 行，职责碎片化。legacy 兼容路径（`callModel`/`handleResponse`/`dispatchToolUse`）已无存在必要，应该删除。

原型 `prototype/agent.go` 展示了目标架构的简洁性：`BaseTAgent` 只有 `eventBus`、`inputs`、`model`、`tools`、`Run`、`OnEvents`、`Compact` 七个字段。生产代码应该同样清晰。

## What Changes

- **合并 `Preprocessor` + `FrameworkFlowAdapter` → `ContextManager`**：统一消息构建、压缩编排、Compact、Flow 执行为一个组件。消除 `Preprocessor.Process` 死代码和临时 `NewPreprocessor` 调用。
- **合并 `compact.go` + `task_segmenter.go` → `task_segmenter.go`**：Compact 本质是分段后保留最近 N 段，放在分段工具文件中。
- **合并 `session_projection.go` + `event_reference_builder.go` → `projection.go`**：EventReference 的构建和存储在同一个文件中。
- **合并 `token_counter.go` → `ContextManager`**：42 行的接口+实现内嵌到主要消费者中。
- **合并 `a2a_server.go` → `tagent_agent.go`**：56 行的便捷构造器放在顶层装配文件底部。
- **删除 legacy 路径**：移除 `AgentLoop` 中 `callModel`、`handleResponse`、`dispatchToolUse` 及 `UseFrameworkFlow` feature flag。`AgentLoop` 退化为 80 行的事件循环薄包装。
- **删除 `Preprocessor.Process`**：框架路径下不再需要内联压缩编排。
- **删除 `splitByTaskBoundary` 委托函数**：`SmartCompressor` 直接调用 `SegmentMessages`。
- **删除 `splitTasks` 委托函数**：`Compactor` 直接调用 `SegmentReferences`。

## Capabilities

### New Capabilities
- `context-manager`: 统一的消息构建、压缩编排、Compact 触发和 Flow 执行入口。

### Modified Capabilities
- `persistent-event-loop`: AgentLoop 从双路径（框架/legacy）变为单一路径（框架），删除 `UseFrameworkFlow` flag。
- `event-compaction`: 压缩/Compact 触发点从 `Preprocessor.Process` 内联逻辑统一到 `ContextManager` 管理的 `BeforeModel` 回调，不再有 legacy 内联路径。

## Impact

- `agent/preprocessor.go` → 删除，逻辑合并到 `agent/context_manager.go`。
- `agent/framework_flow_adapter.go` → 删除，逻辑合并到 `agent/context_manager.go`。
- `agent/compact.go` → 删除，逻辑合并到 `agent/task_segmenter.go`。
- `agent/session_projection.go` + `agent/event_reference_builder.go` → 合并为 `agent/projection.go`。
- `agent/token_counter.go` → 删除，内嵌到 `agent/context_manager.go`。
- `agent/a2a_server.go` → 删除，函数移到 `agent/tagent_agent.go`。
- `agent/agent_loop.go` → 大幅瘦身，删除 legacy 代码。
- `agent/smart_compress.go` → 直接调用 `SegmentMessages`，删除委托函数。
- `agent/tagent_agent.go` → 适配 `ContextManager`，删除 feature flag。
- 所有引用 `Preprocessor`/`FrameworkFlowAdapter` 的测试文件更新。

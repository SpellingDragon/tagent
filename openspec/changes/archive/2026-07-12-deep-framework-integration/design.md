## Context

阶段一/二已经让 `tagent` 具备了三层模型：

- **EventBus**：临时事件流，承载外部输入和工具结果。
- **SessionProjection**：有界的 `EventReference[]`，由 `onEvent` 回调维护。
- **MemoryStore**：持久化完整事件，支持按 `EventKey` 召回。

`Preprocessor` 从 `SessionProjection` 构建 messages，`SmartCompressor` 与 `Compactor` 负责上下文窗口管理，`AgentLoop` 负责 ReAct 循环。但 `AgentLoop` 自建了完整的 `Pull → dispatch → callModel → handleResponse` 流程，与 `trpc-agent-go` 框架的 `LLMAgent.Run` / `Flow.Run` 重复。

框架提供的能力包括：

- `ContentRequestProcessor`：从 `session.Events` 构建 messages。
- `FunctionCallResponseProcessor`：执行工具并控制迭代。
- `BeforeModel` / `AfterModel` 回调：在模型调用前后介入。
- 内置 tracing/telemetry 和 span 属性。

当前 `tagent` 已经通过 `runner.NewRunner` + `identityOnlyAgent` 保留了 Runner 的 plugin 生命周期，但实际执行绕过了框架。

### 当前压缩/Compact 架构现状

阶段一/二落地了以下组件：

| 组件 | 位置 | 职责 | 任务分段实现 |
|------|------|------|-------------|
| `SmartCompressor` | `agent/smart_compress.go` | 压缩 LLM 看到的 `[]model.Message` 视图 | `splitByTaskBoundary(messages)` — 按 `RoleAssistant && len(ToolCalls)==0` 切分 |
| `Compactor` | `agent/compact.go` | 收缩 `SessionProjection` 中的 `[]EventReference` | `splitTasks(refs)` — 按 `EventType == agent_output` 切分 |
| `Preprocessor` | `agent/preprocessor.go` | 编排：先 SmartCompress，后 Compact | 在 `Process()` 中内联调用两者 |

两者操作不同的数据类型（messages vs references），但"任务"的概念是相同的：以 `agent_output` 为边界的对话片段。当前存在两套独立的分段实现，边界判定规则略有差异（`splitByTaskBoundary` 基于 message role + tool_calls 长度，`splitTasks` 基于 `EventReference.EventType`），长期维护有不一致风险。

## Goals / Non-Goals

**Goals:**

1. 让 `TagentAgent.StartLoop` 的持久事件循环复用 `trpc-agent-go` 的 `LLMAgent.Run` / `Flow.Run`，替代自建的 ReAct 循环。
2. 将 `SmartCompressor` 和 `Compactor` 注册为 `BeforeModel` 回调，在框架构建 messages 后、调用模型前执行。
3. 保留 `EventBus` 作为外部事件入口和 tmux/异步工具结果回写通道。
4. 保持 `StartLoop/InjectMessage/StopLoop` 和子 agent `Run()` 的对外语义不变。
5. 获得框架的 tracing/telemetry 能力，减少约 1000 行自建循环代码。
6. **抽取公共 `TaskSegmenter`，统一 `SmartCompressor` 和 `Compactor` 的任务边界定义。**

**Non-Goals:**

1. 不替换 `MemoryPlugin` 的 `OnEvent` 持久化路径。
2. 不改写 `MemoryStore` 的存储格式或事件类型体系。
3. 不重构 `UnifiedToolRegistry` 或 `buildAgent` 的 agent 组装逻辑。
4. 不一次性迁移 TmuxMonitor 的异步语义到框架内部；仍通过 `InjectMessage` 回写。
5. **不合并 `SmartCompressor` 和 `Compactor` 为同一个组件** — 两者职责不同（messages 视图 vs projection 有界），触发条件不同，成本不同（前者可能调 LLM summary，后者只是 bookkeeping）。归一的是"任务分段逻辑"，不是组件本身。

## Decisions

### Decision 1: 在 `TagentAgent` 内新建 `FrameworkFlowAdapter`，而不是直接修改 `AgentLoop`

- **Rationale**: `AgentLoop` 当前承担"事件总线消费者 + ReAct 引擎"双重角色。阶段三需要把"ReAct 引擎"替换为框架，但"事件总线消费者"仍由 tagent 自己维护（因为 tmux 异步注入依赖 `InjectMessage`）。新建 `FrameworkFlowAdapter` 可以清晰划分边界：
  - `AgentLoop` 退化为事件总线消费者和 `Invocation` 构造器。
  - `FrameworkFlowAdapter` 负责把一批 `AgentEvent` 转成 `agent.Invocation`，调用 `LLMAgent.Run` / `Flow.Run`，并把输出事件转发回 `outputCh` 和 `EventBus`。
- **Alternatives considered**: 直接让 `AgentLoop` 实现 `agent.Agent` 接口并交给 `runner.Run`。该方案会让 Runner 接管事件循环，难以保留 `EventBus` 的异步注入能力。

### Decision 2: `SmartCompressor` 和 `Compactor` 作为两个独立的 `BeforeModel` 回调，按顺序注册

- **Rationale**: 框架的 `ContentRequestProcessor` 会在 `BeforeModel` 之前从 `session.Events` 构建 messages。我们需要在 messages 构建完成后、模型调用前压缩/Compact。
  - **回调 1 (SmartCompressor)**: 直接修改 `args.Request.Messages`。如果配置了 `summaryModel`，可能调用 LLM 生成旧任务摘要。这是"messages 视图压缩"，不修改 projection。
  - **回调 2 (Compactor)**: 检查 SmartCompressor 输出后仍超 `maxTokens` 时，修改 `SessionProjection`（`Replace` 为 compacted refs），并重新从 compacted projection 构建 messages 替换 `args.Request.Messages`。这是"projection 有界化"，修改持久化视图。
- **顺序保证**: 框架的 `BeforeModel` 回调按注册顺序执行。先注册 SmartCompressor，后注册 Compactor。
- **Alternatives considered**:
  - 合并为一个回调：职责混淆，测试和回滚困难。
  - 把 Compact 放在 `AfterModel` 中：本次模型调用已经看到未 Compact 的上下文，不能真正解决窗口超限问题。

### Decision 3: 抽取 `TaskSegmenter` 作为公共任务分段工具

- **Rationale**: `SmartCompressor.splitByTaskBoundary` 和 `Compactor.splitTasks` 都在按 `agent_output` 边界切分，但操作不同的数据类型。抽取 `TaskSegmenter` 确保：
  - 边界判定规则唯一：`agent_output` event type 或等价条件。
  - `SmartCompressor` 和 `Compactor` 使用同一套分段逻辑，不会出现"messages 层面 3 个任务但 projection 层面 4 个任务"的不一致。
- **实现方式**: 新建 `agent/task_segmenter.go`，提供：
  ```go
  // SegmentMessages splits messages into task segments.
  func SegmentMessages(messages []model.Message) []*TaskSegment
  // SegmentReferences splits references into task segments.
  func SegmentReferences(refs []memory.EventReference) [][]memory.EventReference
  ```
  两者共享同一个 `isTaskBoundary` 判定函数，分别适配 messages 和 references 的数据结构。
- **迁移路径**: `SmartCompressor.splitByTaskBoundary` → `TaskSegmenter.SegmentMessages`；`Compactor.splitTasks` → `TaskSegmenter.SegmentReferences`。旧函数标记为 deprecated 或直接删除。
- **Alternatives considered**: 统一为只操作 `EventReference` 的分段函数，messages 层面先转换为 references 再分段。这会增加一层转换，且 SmartCompressor 目前需要 `TaskSegment.IsComplete` 等额外元信息，不如直接在 messages 上操作高效。

### Decision 4: 继续由 `onEvent` 回调维护 `SessionProjection`

- **Rationale**: `MemoryPlugin.OnEvent` 已经把事件写入 `MemoryStore` 并填充 `StateDelta`。`BuildEventReference` 从 `StateDelta` 构造 `EventReference`，这与框架 `OnEvent` 插件机制兼容。框架执行过程中产生的事件同样会触发 Runner 上注册的 `MemoryPlugin`，因此 projection 维护逻辑无需重写。
- **Alternatives considered**: 让 `ContentRequestProcessor` 直接读取 `SessionProjection`。但框架 processor 默认读 `session.Events`，改动框架成本高；通过 `BeforeModel` 回调把 `session.Events` 映射为 messages 更轻量。

### Decision 5: 子 agent `Run()` 复用同一个 `FrameworkFlowAdapter`，但使用临时 `EventBus` 和 `SessionProjection`

- **Rationale**: 子 agent 已经是通过 `Run()` 创建临时 bus。阶段三保持这一模式：每次 `Run()` 创建独立的 `Invocation` 和临时 bus，调用 `LLMAgent.Run` 完成单轮 ReAct，第一个非 tool_call 响应即结束。
- **Alternatives considered**: 让子 agent 也走持久循环。这不符合当前工具-agent 的"单轮调用"语义。

### Decision 6: 保留 `EventBus` 的 `TypeToolUse` 用于 tmux 异步场景

- **Rationale**: 框架的 `FunctionCallResponseProcessor` 是同步执行工具的。对于 `ActionTool.TmuxMonitor` 这类异步结果，仍需通过 `InjectMessage` 发布 `external_input` 到 bus，触发下一轮 `Flow.Run`。
- **Alternatives considered**: 把 TmuxMonitor 改为框架 callback。侵入性太大，且与现有 `tool/action` 包耦合深。

### Decision 7: `Preprocessor.Process` 在框架模式下退化为薄包装

- **Rationale**: 迁移到 `BeforeModel` 后，`Preprocessor.Process` 中内联的 SmartCompress + Compact 逻辑将移入回调。但 `Preprocessor` 仍保留：
  - `shouldCallModel` 判断（基于 bus batch 中是否有非 agent_output 的 external_input）。
  - messages 初始构建（从 projection 拉取 + event_key 前缀注入）。
  - `BeforeModel` 回调注册（通过 `FrameworkFlowAdapter` 传给 LLMAgent）。
- **迁移策略**: 添加 `UseFrameworkFlow` feature flag。flag=true 时走 `BeforeModel` 回调路径；flag=false 时走旧的 `Process` 内联路径。两条路径共享 `SmartCompressor` 和 `Compactor` 实例。

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| 框架 `Flow.Run` 的事件类型/时序与 tagent 事件模型不匹配 | 先实现 `FrameworkFlowAdapter` PoC，验证 `runner.Run` 一次完整对话；保留旧 `AgentLoop` 作为 feature flag 回滚。 |
| `BeforeModel` 回调修改 `Request.Messages` 后，框架后续 processor 可能再次修改 | 将 `SmartCompressor` 注册为最后一个 `BeforeModel`，并验证 `ContentRequestProcessor` 不再后续变更。 |
| `SessionProjection` 与 `session.Events` 状态不一致 | `onEvent` 仍为唯一写入口；框架产生的事件同样触发 `MemoryPlugin.OnEvent`，projection 自然同步。 |
| Tmux 异步命令完成后，下一轮 `Flow.Run` 的 context 缺少前序 tool_call | `SessionProjection` 已包含前序 tool_call 和 tool_result 引用；`ContentRequestProcessor` 从 session 重建 messages。 |
| 测试需要大量框架 mock | 提供 `MockLLMAgent` / `MockFlow` 包装，并在 adapter 层做单元测试，减少对框架内部实现细节的依赖。 |
| 上游框架升级导致行为变化 | 通过 adapter 隔离框架接口；升级时只需调整 adapter 内部映射。 |
| `TaskSegmenter` 抽取后 SmartCompressor 的 `TaskSegment.IsComplete` 等元信息丢失 | `TaskSegmenter.SegmentMessages` 返回 `[]*TaskSegment`（含 `IsComplete`），保持与当前 `splitByTaskBoundary` 兼容。 |
| Compact 在 `BeforeModel` 中修改 projection 后，框架 `ContentRequestProcessor` 不会自动重新读取 | Compactor 回调不仅修改 projection，还直接替换 `args.Request.Messages` 为从 compacted projection 重建的 messages。 |

## Migration Plan

1. **TaskSegmenter 抽取**（可与框架迁移解耦，先行落地）
   - 新建 `agent/task_segmenter.go`，提供 `SegmentMessages` 和 `SegmentReferences`。
   - `SmartCompressor.splitByTaskBoundary` 改为调用 `TaskSegmenter.SegmentMessages`。
   - `Compactor.splitTasks` 改为调用 `TaskSegmenter.SegmentReferences`。
   - 运行 `go test ./agent/...` 验证无回归。
2. **PoC 验证**（独立分支）
   - 在 `agent/poc_test.go` 风格下，验证 `LLMAgent.Run` + `BeforeModel` 回调可以修改 messages 并完成 tool_call → final response 的完整循环。
   - 验证 `BeforeModel` 修改 `Request.Messages` 后框架不会再次覆盖。
3. **Adapter 实现**
   - 新增 `agent/framework_flow_adapter.go`，实现 `FrameworkFlowAdapter`。
   - 在 `TagentAgent.StartLoop` 中，用 adapter 替换 `AgentLoop.Run` 的调用；`AgentLoop` 保留为事件总线消费者。
4. **回调注册**
   - 将 `SmartCompressor` 和 `Compactor` 包装为 `BeforeModel` 回调，通过 `model.NewCallbacks` 注册到 `LLMAgent`。
   - 验证回调顺序：SmartCompressor 先执行，Compactor 后执行。
5. **子 agent 路径**
   - `TagentAgent.Run` 同样使用 adapter，但传入临时 bus 和临时 projection。
6. **回归测试**
   - 运行 `go test ./agent/...`、长会话集成测试、tmux 异步集成测试、A2A 远程子 agent 测试。
7. **灰度/回滚**
   - 通过配置 `UseFrameworkFlow`（默认 true）保留旧 `AgentLoop` 路径作为 fallback。
   - 观察 1-2 周生产日志后，移除 fallback 和 `Preprocessor.Process` 中的内联压缩逻辑。

## Open Questions

1. `trpc-agent-go` 的 `Flow.Run` 是否暴露与 `LLMAgent.Run` 相同的 `BeforeModel` 回调语义？需要阅读框架源码确认。
2. `ContentRequestProcessor` 默认是否会将 tool_result 事件以 `RoleTool` 加入 messages？如果是，是否与 tagent 当前将 tool_result 作为 `external_input` 的处理方式冲突？
3. 框架的 `FunctionCallResponseProcessor` 是否会自动发布 assistant tool_call 事件到 Runner 的 event channel？tagent 是否还需要手动调用 `emitEvent`？
4. 在 `BeforeModel` 中执行 `Compactor` 并修改 `SessionProjection` 后，如何让 `ContentRequestProcessor` 重新读取？——当前方案：Compactor 回调直接替换 `args.Request.Messages`，不依赖框架重新执行 processor。
5. `TaskSegmenter` 是否需要支持自定义边界事件类型（如未来引入 `task_complete` 事件）？——当前方案：边界判定函数可配置，但默认使用 `agent_output`。
## Context

阶段一/二已经让 `tagent` 具备了三层模型：

- **EventBus**：临时事件流，承载外部输入和工具结果。
- **SessionProjection**：有界的 `EventReference[]`，由 `onEvent` 回调维护。
- **MemoryStore**：持久化完整事件，支持按 `EventKey` 召回。

`Preprocessor` 从 `SessionProjection` 构建 messages，`SmartCompressor` 与 `Compactor` 负责上下文窗口管理，`AgentLoop` 负责 ReAct 循环。但 `AgentLoop` 自建了完整的 `Pull → dispatch → callModel → handleResponse` 流程，与 `trpc-agent-go` 框架的 `LLMAgent.Run` / `Flow.Run` 重复。

框架提供的能力包括：

- `ContentRequestProcessor`：从 `session.Events` 构建 messages。
- `FunctionCallResponseProcessor`：执行工具并控制迭代。
- `BeforeModel` / `AfterModel` 回调：在模型调用前后介入。
- 内置 tracing/telemetry 和 span 属性。

当前 `tagent` 已经通过 `runner.NewRunner` + `identityOnlyAgent` 保留了 Runner 的 plugin 生命周期，但实际执行绕过了框架。

## Goals / Non-Goals

**Goals:**

1. 让 `TagentAgent.StartLoop` 的持久事件循环复用 `trpc-agent-go` 的 `LLMAgent.Run` / `Flow.Run`，替代自建的 ReAct 循环。
2. 将 `SmartCompressor` 和 `Compactor` 注册为 `BeforeModel` 回调，在框架构建 messages 后、调用模型前执行。
3. 保留 `EventBus` 作为外部事件入口和 tmux/异步工具结果回写通道。
4. 保持 `StartLoop/InjectMessage/StopLoop` 和子 agent `Run()` 的对外语义不变。
5. 获得框架的 tracing/telemetry 能力，减少约 1000 行自建循环代码。

**Non-Goals:**

1. 不替换 `MemoryPlugin` 的 `OnEvent` 持久化路径。
2. 不改写 `MemoryStore` 的存储格式或事件类型体系。
3. 不重构 `UnifiedToolRegistry` 或 `buildAgent` 的 agent 组装逻辑。
4. 不一次性迁移 TmuxMonitor 的异步语义到框架内部；仍通过 `InjectMessage` 回写。

## Decisions

### Decision 1: 在 `TagentAgent` 内新建 `FrameworkFlowAdapter`，而不是直接修改 `AgentLoop`

- **Rationale**: `AgentLoop` 当前承担“事件总线消费者 + ReAct 引擎”双重角色。阶段三需要把“ReAct 引擎”替换为框架，但“事件总线消费者”仍由 tagent 自己维护（因为 tmux 异步注入依赖 `InjectMessage`）。新建 `FrameworkFlowAdapter` 可以清晰划分边界：
  - `AgentLoop` 退化为事件总线消费者和 `Invocation` 构造器。
  - `FrameworkFlowAdapter` 负责把一批 `AgentEvent` 转成 `agent.Invocation`，调用 `LLMAgent.Run` / `Flow.Run`，并把输出事件转发回 `outputCh` 和 `EventBus`。
- **Alternatives considered**: 直接让 `AgentLoop` 实现 `agent.Agent` 接口并交给 `runner.Run`。该方案会让 Runner 接管事件循环，难以保留 `EventBus` 的异步注入能力。

### Decision 2: `SmartCompressor` 作为 `BeforeModel` 回调，`Compactor` 作为独立的 `BeforeModel` 回调或内嵌在压缩回调中

- **Rationale**: 框架的 `ContentRequestProcessor` 会在 `BeforeModel` 之前从 `session.Events` 构建 messages。我们需要在 messages 构建完成后、模型调用前压缩/Compact。
  - `SmartCompressor` 直接修改 `args.Request.Messages`。
  - `Compactor` 修改 `SessionProjection` 并触发 `ContentRequestProcessor` 重新构建 messages（通过让 `Compactor` 先清空/压缩 projection，然后框架重新执行 processor 链），或作为第二个 `BeforeModel` 回调在压缩后再次修正 messages。
- **Alternatives considered**: 把 Compact 放在 `AfterModel` 中。这会导致本次模型调用已经看到未 Compact 的上下文，不能真正解决窗口超限问题。

### Decision 3: 继续由 `onEvent` 回调维护 `SessionProjection`

- **Rationale**: `MemoryPlugin.OnEvent` 已经把事件写入 `MemoryStore` 并填充 `StateDelta`。`BuildEventReference` 从 `StateDelta` 构造 `EventReference`，这与框架 `OnEvent` 插件机制兼容。框架执行过程中产生的事件同样会触发 Runner 上注册的 `MemoryPlugin`，因此 projection 维护逻辑无需重写。
- **Alternatives considered**: 让 `ContentRequestProcessor` 直接读取 `SessionProjection`。但框架 processor 默认读 `session.Events`，改动框架成本高；通过 `BeforeModel` 回调把 `session.Events` 映射为 messages 更轻量。

### Decision 4: 子 agent `Run()` 复用同一个 `FrameworkFlowAdapter`，但使用临时 `EventBus` 和 `SessionProjection`

- **Rationale**: 子 agent 已经是通过 `Run()` 创建临时 bus。阶段三保持这一模式：每次 `Run()` 创建独立的 `Invocation` 和临时 bus，调用 `LLMAgent.Run` 完成单轮 ReAct，第一个非 tool_call 响应即结束。
- **Alternatives considered**: 让子 agent 也走持久循环。这不符合当前工具-agent 的“单轮调用”语义。

### Decision 5: 保留 `EventBus` 的 `TypeToolUse` 用于 tmux 异步场景

- **Rationale**: 框架的 `FunctionCallResponseProcessor` 是同步执行工具的。对于 `ActionTool.TmuxMonitor` 这类异步结果，仍需通过 `InjectMessage` 发布 `external_input` 到 bus，触发下一轮 `Flow.Run`。
- **Alternatives considered**: 把 TmuxMonitor 改为框架 callback。侵入性太大，且与现有 `tool/action` 包耦合深。

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| 框架 `Flow.Run` 的事件类型/时序与 tagent 事件模型不匹配 | 先实现 `FrameworkFlowAdapter` PoC，验证 `runner.Run` 一次完整对话；保留旧 `AgentLoop` 作为 feature flag 回滚。 |
| `BeforeModel` 回调修改 `Request.Messages` 后，框架后续 processor 可能再次修改 | 将 `SmartCompressor` 注册为最后一个 `BeforeModel`，并验证 `ContentRequestProcessor` 不再后续变更。 |
| `SessionProjection` 与 `session.Events` 状态不一致 | `onEvent` 仍为唯一写入口；框架产生的事件同样触发 `MemoryPlugin.OnEvent`，projection 自然同步。 |
| Tmux 异步命令完成后，下一轮 `Flow.Run` 的 context 缺少前序 tool_call | `SessionProjection` 已包含前序 tool_call 和 tool_result 引用；`ContentRequestProcessor` 从 session 重建 messages。 |
| 测试需要大量框架 mock | 提供 `MockLLMAgent` / `MockFlow` 包装，并在 adapter 层做单元测试，减少对框架内部实现细节的依赖。 |
| 上游框架升级导致行为变化 | 通过 adapter 隔离框架接口；升级时只需调整 adapter 内部映射。 |

## Migration Plan

1. **PoC 验证**（独立分支）
   - 在 `agent/poc_test.go` 风格下，验证 `LLMAgent.Run` + `BeforeModel` 回调可以修改 messages 并完成 tool_call → final response 的完整循环。
2. **Adapter 实现**
   - 新增 `agent/framework_flow_adapter.go`，实现 `FrameworkFlowAdapter`。
   - 在 `TagentAgent.StartLoop` 中，用 adapter 替换 `AgentLoop.Run` 的调用；`AgentLoop` 保留为事件总线消费者。
3. **回调注册**
   - 将 `SmartCompressor` 和 `Compactor` 包装为 `BeforeModel` 回调，通过 `model.NewCallbacks` 注册到 `LLMAgent`。
4. **子 agent 路径**
   - `TagentAgent.Run` 同样使用 adapter，但传入临时 bus 和临时 projection。
5. **回归测试**
   - 运行 `go test ./agent/...`、长会话集成测试、tmux 异步集成测试、A2A 远程子 agent 测试。
6. **灰度/回滚**
   - 通过配置 `UseFrameworkFlow`（默认 true）保留旧 `AgentLoop` 路径作为 fallback。
   - 观察 1-2 周生产日志后，移除 fallback。

## Open Questions

1. `trpc-agent-go` 的 `Flow.Run` 是否暴露与 `LLMAgent.Run` 相同的 `BeforeModel` 回调语义？需要阅读框架源码确认。
2. `ContentRequestProcessor` 默认是否会将 tool_result 事件以 `RoleTool` 加入 messages？如果是，是否与 tagent 当前将 tool_result 作为 `external_input` 的处理方式冲突？
3. 框架的 `FunctionCallResponseProcessor` 是否会自动发布 assistant tool_call 事件到 Runner 的 event channel？ tagent 是否还需要手动调用 `emitEvent`？
4. 在 `BeforeModel` 中执行 `Compactor` 并修改 `SessionProjection` 后，如何让 `ContentRequestProcessor` 重新读取？是否需要将 `Compactor` 放在 `ContentRequestProcessor` 之前作为一个自定义 processor？

## Context

原型 `prototype/agent.go` 用 126 行展示了 tagent 的核心设计哲学：

- `BaseTAgent` 只有 7 个字段：`eventBus`、`inputs`、`model`、`tools`、`Run`、`OnEvents`、`Compact`
- `OnEvents` 统一处理事件追加 + 调用模型
- `Compact` 直接清理 `inputs` 投影
- 没有独立的 "preprocessor" 或 "flow adapter"
- **`Run` 是 `BaseTAgent` 自己的方法，不是独立组件**

原型到生产的映射关系：

| 原型 | 生产（目标架构） | 说明 |
|------|-----------------|------|
| `eventBus chan Event` | `EventBus` | 事件流，Publish/Pull 后丢弃 |
| `inputs []string` | `SessionProjection (EventReference[])` | 有界投影，onEvent 追加、ContextManager 读取、Compactor 清理 |
| `model *Model` | 框架 `model.Model` (通过 LLMAgent) | 框架处理 ReAct 循环 |
| `tools map[string]func` | `AgentToolWrapper` 实现 `CallableTool` | 子 agent 作为工具 |
| `Run func()` | `TagentAgent.runEventLoop(ctx)` | Pull → BuildInvocation → RunFlow，是 TagentAgent 自己的方法 |
| `OnEvents func([]Event) Event` | `ContextManager.BuildInvocation` + `RunFlow` | 合并消息 + 执行 Flow |
| `Compact func()` | `Compactor.Compact` (BeforeModel 回调) | 投影有界化 |
| `ModelCompletion func(inputs) string` | 框架 `model.GenerateContent` | model 独立因框架接口差异 |

### 框架 Runner 内部行为分析

通过阅读 `trpc-agent-go` 框架源码（`runner.go`），确认框架 Runner 在 `runner.Run` 内部完成以下工作：

1. **创建/获取 session**（L348-358）
2. **追加用户消息到 session**（L430-443）— `sessionService.AppendEvent`
3. **注册 appender**（L453-458）— 后续 Flow 产生的每个 event 都通过 appender 追加到 session
4. **处理每个 agent event**（`processSingleAgentEvent` L756-794）：
   - `applyEventPlugins`（L764）→ Plugin.OnEvent（MemoryPlugin 写 MemoryStore + StateDelta）
   - `handleEventPersistence`（L769）→ `sessionService.AppendEvent`（框架自己追加到 session）
   - `EmitEvent`（L789）→ 发到 event channel

**关键结论**：框架 Runner 已经完成了 `sessionService.AppendEvent` 和 `MemoryPlugin.OnEvent`。tagent 的 `makeOnEventCallback` 不需要重复这些操作。

当前生产代码存在以下问题：

1. **Preprocessor 与 FrameworkFlowAdapter 职责重叠**：框架路径下 `Preprocessor.Process()` 是死代码。
2. **MemoryPlugin.OnEvent 双重调用**：框架 Runner 通过 Plugin 机制自动调用，`makeOnEventCallback` 又直接调用一次。
3. **sessionSvc.AppendEvent 双重调用**：框架 Runner 在 `handleEventPersistence` 中自己调 `sessionService.AppendEvent`，`makeOnEventCallback` 又调一次。
4. **AgentLoop 是不必要的抽象层**：`AgentLoop.Run` 的核心逻辑只有 3 行（Pull → BuildInvocation → RunFlow），是纯传递层。原型中 `Run` 是 `BaseTAgent` 自己的方法，不是独立组件。
5. **AgentLoop 持有未使用字段**：`onEvent` 字段在删除 Step 1 后不再被 `Run` 使用。
6. **双 Runner 问题**：已通过 D2 统一 Runner 修复。

## Goals / Non-Goals

**Goals:**

1. 将 `Preprocessor` + `FrameworkFlowAdapter` 合并为单一 `ContextManager`。
2. 统一 Runner：`ContextManager` 创建唯一的 Runner。
3. `makeOnEventCallback` 仅做 `projection.Append`。
4. **内联 `AgentLoop` 到 `TagentAgent`**——`Run` 是 agent 自己的方法，不是独立组件。
5. 合并 5 个碎片化小文件。
6. 删除 legacy 兼容路径和 `UseFrameworkFlow` flag。
7. 修复子 agent `Run()` 路径。
8. 修复 `SmartCompressor` 使用传入的 `TokenCounter`。
9. 清洗 README 和 wiki 文档。

**Non-Goals:**

1. 不修改 `smart_compress.go` 的压缩算法逻辑。
2. 不修改 `tool_agent.go` 的 `AgentToolWrapper` 实现。
3. 不修改 `event_bus.go`、`http_api.go`、`meditation.go`、`trajectory_recorder.go`、`output_limit_tool.go`。
4. 不修改 `memory/` 或 `plugin/` 包的代码。

## Decisions

### D1: `ContextManager` 统一 Preprocessor + FrameworkFlowAdapter

`ContextManager` 持有压缩器、Compactor、TokenCounter、MemStore 等依赖，提供 `BuildMessages`、`InjectEventKeys`、`ShouldCallModel`、`BuildInvocation`、`RunFlow`、`NewContextManager`。BeforeModel 回调链直接闭包引用自身字段。

### D2: 统一 Runner

`ContextManager` 创建唯一的 Runner，同时注册 LLMAgent + MemoryPlugin + SummaryPlugin + SessionService。框架 Runner 内部完成 `sessionService.AppendEvent` 和 `Plugin.OnEvent`。

### D3: makeOnEventCallback 仅做 projection.Append

框架 Runner 已完成 `sessionService.AppendEvent`（L769/L944）和 `MemoryPlugin.OnEvent`（L764/L818）。`makeOnEventCallback` 不再重复这些操作，仅做 `projection.Append`。

### D4: 删除 AgentLoop.Run Step 1

框架 `runner.Run` 内部处理所有持久化。Step 1 的 `onEvent` 手动构造的 frameworkEvt 没有 StateDelta，`BuildEventReference` 会失败。删除 Step 1 后，框架通过 `RunFlow` 内部的 `onEvent` 完成 `projection.Append`。

### D5: RunFlow 内 onEvent 调用

`RunFlow` 内调用 `onEvent`（仅做 `projection.Append`）。框架 Runner 在 emit event 之前已完成 Plugin.OnEvent 和 sessionService.AppendEvent，所以 `onEvent` 被调用时 StateDelta 已被填充，`BuildEventReference` 会成功。

### D6: 合并小文件

- `compact.go` → 合并到 `task_segmenter.go`
- `session_projection.go` + `event_reference_builder.go` → 合并为 `projection.go`
- `token_counter.go` → 内嵌到 `context_manager.go`
- `a2a_server.go` → 移到 `tagent_agent.go` 底部

### D7: 删除 legacy 路径

`UseFrameworkFlow` 字段从 `TagentConfig` 删除。

### D8: 修复子 agent Run() 路径

`TagentAgent.Run` 创建临时 `ContextManager`（含临时 Runner + MemoryPlugin + SessionService）。

### D9: 修复 SmartCompressor TokenCounter

`SmartCompressor` 新增 `tokenCounter` 字段和 `WithTokenCounter` option。

### D10: 删除委托函数

`splitByTaskBoundary` 和 `splitTasks` 删除，直接调用 `SegmentMessages` / `SegmentReferences`。

### D11: 文档清洗

README.md 和 wiki 文档删除所有兼容/过渡描述，只描述目标架构。

### D12: TagentAgent 结构体精简

`TagentAgent` 删除 `preprocessor` 和 `runner` 字段，新增 `contextManager` 字段。`projection` 字段保留。`memPlugin` 字段保留。`sessionSvc` 字段保留。

### D13: 内联 AgentLoop 到 TagentAgent

**Rationale**: 原型中 `Run` 是 `BaseTAgent` 自己的方法，不是独立组件。`AgentLoop.Run` 的核心逻辑只有 3 行（Pull → BuildInvocation → RunFlow），是纯传递层。`AgentLoop` 结构体的 `onEvent` 字段在删除 Step 1 后不再被 `Run` 使用。`AgentLoopConfig` 是纯传递结构体。

**方案**：

1. 将 `AgentLoop.Run` 的逻辑内联为 `TagentAgent.runEventLoop(ctx)` 私有方法：

```go
func (ta *TagentAgent) runEventLoop(ctx context.Context) {
    defer func() {
        if r := recover(); r != nil {
            log.Errorf("[runEventLoop:%s] panic recovered: %v", ta.name, r)
        }
    }()
    for {
        if err := ctx.Err(); err != nil { return }
        events, err := ta.persistentBus.Pull(ctx)
        if err != nil { ... return }
        msg := ta.contextManager.BuildInvocation(events)
        if msg.Content == "" { continue }
        ta.contextManager.RunFlow(ctx, msg)
    }
}
```

2. `StartLoop` 调用 `go ta.runEventLoop(ta.loopCtx)`。
3. `Run()` 创建临时 `ContextManager` 后调用 `go func() { ... invCM.RunFlow ... }()`（或内联 runEventLoop 逻辑）。
4. 删除 `AgentLoop`、`AgentLoopConfig`、`NewAgentLoop`、`SetOnEvent`。
5. `TagentAgent.agentLoop` 字段删除。
6. `summarizeEvents` 移到 `tagent_agent.go`。
7. 删除 `agent_loop.go` 文件。

**测试适配**：
- `agent_loop_test.go` → 测试改为构造 `TagentAgent` + `StartLoop`，或直接测试 `ContextManager`。
- `agent_loop_edge_test.go` → 同上。
- `http_api_test.go` 中 `ta.agentLoop.Run(ta.loopCtx)` → 删除（`startTestLoop` 直接调 `runEventLoop` 或 `StartLoop`）。
- `tagent_agent_test.go` 中 `assert.NotNil(t, ta.agentLoop)` → 删除。
- `test_helpers_test.go` 中 `newTestAgentLoop` → 删除或改为 `newTestTagentAgent`。

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| 统一 Runner 后 MemoryPlugin 可能不触发 | 框架 Runner 在 `runner.go:818` 确认会调用 `Plugins.OnEvent` |
| 框架 Runner 自己调 sessionService.AppendEvent 导致重复 | D3 修复：makeOnEventCallback 不再调 AppendEvent |
| 删除 Step 1 后 projection 缺少用户输入引用 | 框架 runner.Run 内部产生 user message event 并触发 onEvent → projection.Append |
| 内联 AgentLoop 后测试需要重构 | 测试改为构造 TagentAgent + StartLoop，或直接测试 ContextManager |
| `ContextManager` 文件较大（~400 行） | 使用清晰的分节注释 |

## 精简后的文件结构

| 文件 | 行数（估） | 职责 | 原型对应 |
|------|-----------|------|---------|
| `tagent_agent.go` | ~900 | 顶层装配 + runEventLoop + A2A + makeOnEventCallback | `BaseTAgent.New()` + `Run` |
| `context_manager.go` | ~426 | 消息构建 + 压缩编排 + Compact + Flow 执行 + TokenCounter + 统一 Runner | `OnEvents` + `ModelCompletion` |
| `smart_compress.go` | ~484 | 两阶段压缩 | 无（生产扩展） |
| `task_segmenter.go` | ~141 | 任务分段 + Compactor | `Compact` |
| `event_bus.go` | ~153 | EventBus | `eventBus chan Event` |
| `tool_agent.go` | ~459 | AgentToolWrapper | `tools map` + `RegisterTool` |
| `projection.go` | ~81 | SessionProjection + EventReferenceBuilder | `inputs []string` |
| `trajectory_recorder.go` | ~325 | 轨迹记录 | 无（生产扩展） |
| `http_api.go` | ~153 | HTTP API | 无（生产扩展） |
| `meditation.go` | ~143 | 冥想心跳 | 无（生产扩展） |
| `output_limit_tool.go` | ~73 | 工具输出截断 | 无（生产扩展） |
| **总计** | ~3538 | **11 个文件** | |

## Migration Plan

1. 创建 `context_manager.go`（含 TokenCounter + 统一 Runner 构造）。✅
2. 合并 `compact.go` → `task_segmenter.go`，合并 `session_projection.go` + `event_reference_builder.go` → `projection.go`。✅
3. 修复 `smart_compress.go`：TokenCounter + 删除委托。✅
4. 删除旧文件。✅
5. 改造 `agent_loop.go`：删除 legacy、瘦身结构体。✅
6. 改造 `tagent_agent.go`：ContextManager 集成、makeOnEventCallback 修复、子 agent Run() 修复、NewA2AServer 迁移。✅
7. 更新所有测试。✅
8. 删除 Step 1 的 onEvent 调用。✅
9. makeOnEventCallback 删除 sessionSvc.AppendEvent。✅
10. 清洗 README.md。✅
11. **待做**：内联 AgentLoop 到 TagentAgent（D13）。
12. **待做**：清洗 wiki 文档。
13. 运行 `go test ./agent/...` 验证。

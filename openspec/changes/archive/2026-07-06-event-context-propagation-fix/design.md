## Context

tagent 的事件上下文传递机制在实际运行中存在三个问题：

1. **event_keys 传递断裂**：InjectEventKeys BeforeModel 回调已正确注入 `[evt_KEY|type]` 前缀到 LLM messages，但 LLM 调用子 Agent 工具时没有传 event_keys。原因是 prompt 缺乏引导 + 没有自动兜底机制。

2. **尾部事件丢弃**：子 Agent `Run()` 的 wrappedCh goroutine 在收到最终响应后立即 `return` + `defer runCancel()`，取消了框架 Runner 的 context。框架此时可能有尾部事件（如 RequiresCompletion 信号、MemoryPlugin 持久化）正在通过 `EmitEventWithTimeout` 发送，context 取消导致事件丢失。

3. **资源清理不完整**：(a) `TrajectoryRecorder` 有 `Close()` 方法（flush writeLoop + 关闭文件），但 `TagentAgent.Close()` 未调用它，导致 writeLoop goroutine 和文件句柄泄漏；(b) 每次 `Run()` 创建的临时 `ContextManager`（含 Runner + LLMAgent + Plugins）未被 `Close()`，Runner 内部资源泄漏。

约束：
- 不修改 InjectEventKeys BeforeModel 回调逻辑（已正确工作）
- 不修改 MemoryPlugin/SummaryPlugin
- 保持子 Agent 单轮语义（收到第一个 agent_output 后停止）
- 不引入新的外部依赖

## Goals / Non-Goals

**Goals:**
- 保证子 Agent 总能收到父 Agent 的事件上下文（即使 LLM 不主动传 event_keys）
- 保证框架 Runner 的尾部事件不被丢弃（MemoryPlugin 有时间完成持久化）
- 通过 prompt 引导 LLM 主动选择相关 event_keys（而非完全依赖自动注入）
- 保证所有资源（TrajectoryRecorder、子 Agent ContextManager）在生命周期结束时被正确清理

**Non-Goals:**
- 不实现 event_keys 的语义相关性匹配（如 embedding 搜索）
- 不修改框架 Runner 的 EmitEventWithTimeout 实现
- 不改变子 Agent 单轮语义
- 不解决 MemoryPlugin 共享的潜在并发问题（当前同步调用无并发风险）

## Decisions

### D1: 自动注入 — 从父 Agent SessionProjection 取最近 N 个事件

**决策**：`AgentToolWrapper.Call` 在解析到 `event_keys` 为空且配置了 `event_params` 时，从父 Agent 的 SessionProjection 中取最近 N 个（默认 5）EventKey 自动注入。

**理由**：
- SessionProjection 是轻量引用列表，GetAll() 是 O(1) copy
- 最近 N 个事件覆盖了当前对话轮次的上下文
- N=5 是经验值：覆盖最近 1-2 轮 user+assistant 交互

**注意**：AgentToolWrapper 当前不持有父 Agent 的 SessionProjection 引用。需要通过 `AgentToolWrapper` 新增 `parentProjection *SessionProjection` 字段，在 `NewAgentToolWrapper` 或 `buildAgentToolRef` 中注入。

**备选**：
- 从 MemoryStore 查询最近 N 个事件 → 需要 PartitionID，且返回 EventReference（已有）
- 从父 Agent 的 `MemStore()` 查询 → 需要 PartitionID，更复杂
- 从 parentStore 直接查询 → 已有 `parentStore`，但需要知道 PartitionID

最终选择：直接持有 `parentProjection` 引用，因为它是最轻量的数据源（已在内存中）。

### D2: Drain 模式 — 500ms 超时转发尾部事件

**决策**：`TagentAgent.Run()` 的 wrappedCh goroutine 收到最终响应后，进入 drain 模式：
1. 设置 500ms 超时
2. 继续从 `invOutputCh` 读取事件并转发到 `wrappedCh`
3. 超时或 `invOutputCh` 关闭后退出

**理由**：
- 框架 Runner 在最终响应后的尾部事件通常在毫秒级产出
- 500ms 足够覆盖 MemoryPlugin.OnEvent + EmitEvent 的延迟
- 超时后强制退出，避免 goroutine 泄漏（runEventLoop 会阻塞在 bus.Pull 上）

**备选**：
- 不 cancel，让 runEventLoop 自然退出 → runEventLoop 在 RunFlow 后会 bus.Pull 阻塞，不会自然退出
- 等待 invOutputCh 关闭 → 不会关闭，因为 runEventLoop 还在运行
- 0ms drain（直接 cancel）→ 当前行为，导致事件丢弃

### D3: Prompt 引导 — TOOLS.md 增加 event_keys 说明

**决策**：在 `examples/wechat-bot/resources/prompts/TOOLS.md` 中增加 event_keys 使用指南段落。在 knowledge 和 recall 的 tool description 中增加 event_keys 参数说明。

**理由**：
- prompt 引导让 LLM 在需要历史上下文时主动选择相关 event_keys
- 自动注入作为兜底，覆盖 LLM 不传的情况
- 两者互补：prompt 提升精度，auto-inject 保证可用性

### D4: 子 Agent ContextManager 清理 — runEventLoop 退出后 Close

**决策**：在 `Run()` 的 goroutine 1（runEventLoop goroutine）中，`runEventLoop` 退出后调用 `invCM.Close()`。`invCM.Close()` 会关闭 Runner（如果 Runner 实现了 Close 方法）。

```go
go func() {
    defer close(invOutputCh)
    defer invCM.Close()  // 新增：释放临时 Runner 资源
    ta.runEventLoop(runCtx, invBus, invCM)
}()
```

**理由**：
- `invCM` 内部创建了 Runner + LLMAgent + Plugins + SessionService
- Runner 内部可能持有 goroutine pool、session 缓存等资源
- `invCM.Close()` 在 `runEventLoop` 退出后调用，确保不会中断正在执行的 Flow

**备选**：
- 不关闭，依赖 GC → Runner 的 goroutine 不会自动停止，资源泄漏
- 在 wrappedCh goroutine 中关闭 → 时序更复杂，且 invCM 与 runEventLoop 在同一个 goroutine 生命周期内

**风险**：`invCM.Close()` 可能关闭共享的 `ta.memPlugin` 或 `ta.sessionSvc`。需要检查 `ContextManager.Close()` 的实现——它只关闭 Runner，不关闭 Plugin 和 SessionService（因为它们是外部传入的）。

### D5: TrajectoryRecorder 清理 — Close() 中显式调用

**决策**：在 `TagentAgent.Close()` 中，在 `contextManager.Close()` 之后调用 `trajectoryRecorder.Close()`（如果已设置）。

```go
// Close() 新增：
if ta.trajectoryRecorder != nil {
    if err := ta.trajectoryRecorder.Close(); err != nil {
        errs = append(errs, fmt.Errorf("close trajectory recorder: %w", err))
    }
}
```

**理由**：
- TrajectoryRecorder 的 writeLoop goroutine 和文件句柄必须被显式关闭
- 在 `contextManager.Close()` 之后调用，确保 Runner 先停止（不再产生新的 LLM 调用）
- 在 `memStore.Close()` 之前调用，确保轨迹数据先落盘

**备选**：
- 通过 `RegisterCloser` 注册 → 但 TrajectoryRecorder 通过 `SetTrajectoryRecorder` 设置，时机不同
- 在 `closers` 循环中处理 → 需要将 TrajectoryRecorder 包装为 Closer 接口，多余

## Risks / Trade-offs

- **[自动注入不相关上下文]** → 只注入最近 N 个事件，不注入全部历史。最近 N 个通常与当前任务相关。LLM 可以通过 prompt 引导更精确地选择。
- **[500ms drain 增加延迟]** → 子 Agent 调用方会多等最多 500ms。在实际场景中（LLM 调用本身需数秒），500ms 可接受。
- **[parentProjection 引用增加耦合]** → AgentToolWrapper 需要持有 SessionProjection 引用。但 AgentToolWrapper 已经持有 parentStore（MemoryStore），增加 projection 引用不违反依赖方向。
- **[drain 期间新事件混入]** → drain 模式可能收到 framework 的非尾部事件（如下一轮 ReAct 的事件）。但子 Agent 单轮语义保证：收到最终响应后不再有 ReAct 循环，drain 期间的事件都是框架尾部事件。
- **[invCM.Close() 关闭共享资源]** → `ContextManager.Close()` 只关闭 Runner（如果 Runner 实现 Close），不关闭外部传入的 Plugin/SessionService。需要验证 Runner.Close() 不会影响共享的 memPlugin。
## Context

tagent 的事件上下文传递机制在实际运行中存在两个断裂点：

1. **event_keys 传递断裂**：InjectEventKeys BeforeModel 回调已正确注入 `[evt_KEY|type]` 前缀到 LLM messages，但 LLM 调用子 Agent 工具时没有传 event_keys。原因是 prompt 缺乏引导 + 没有自动兜底机制。

2. **尾部事件丢弃**：子 Agent `Run()` 的 wrappedCh goroutine 在收到最终响应后立即 `return` + `defer runCancel()`，取消了框架 Runner 的 context。框架此时可能有尾部事件（如 RequiresCompletion 信号、MemoryPlugin 持久化）正在通过 `EmitEventWithTimeout` 发送，context 取消导致事件丢失。

约束：
- 不修改 InjectEventKeys BeforeModel 回调逻辑（已正确工作）
- 不修改 MemoryPlugin/SummaryPlugin
- 保持子 Agent 单轮语义（收到第一个 agent_output 后停止）
- 不引入新的外部依赖

## Goals / Non-Goals

**Goals:**
- 保证子 Agent 总能收到父 Agent 的事件上下文（即使 LLM 不主动传 event_keys）
- 保证框架 Runner 的尾部事件不被丢弃（MemoryPlugin 有时间完成持久化）
- 通过 prompt 引导 LLM 主动选择相关 event_keys（而非完全依赖自动注入）

**Non-Goals:**
- 不实现 event_keys 的语义相关性匹配（如 embedding 搜索）
- 不修改框架 Runner 的 EmitEventWithTimeout 实现
- 不改变子 Agent 单轮语义

## Decisions

### D1: 自动注入 — 从父 Agent SessionProjection 取最近 N 个事件

**决策**：`AgentToolWrapper.Call` 在解析到 `event_keys` 为空且配置了 `event_params` 时，从父 Agent 的 SessionProjection 中取最近 N 个（默认 5）EventKey 自动注入。

**理由**：
- SessionProjection 是轻量引用列表，GetAll() 是 O(1) copy
- 最近 N 个事件覆盖了当前对话轮次的上下文
- N=5 是经验值：覆盖最近 1-2 轮 user+assistant 交互

**注意**：AgentToolWrapper 当前不持有父 Agent 的 SessionProjection 引用。需要通过 `AgentToolWrapper` 新增 `parentProjection *SessionProjection` 字段，在 `NewAgentToolWrapper` 或 `buildAgentToolRef` 中注入。

**备选**：
- 从 MemoryStore 查询最近 N 个事件 → 需要 PartitionID，且返回 EventReference（已有）
- 从父 Agent 的 `MemStore()` 查询 → 需要 PartitionID，更复杂
- 从 parentStore 直接查询 → 已有 `parentStore`，但需要知道 PartitionID

最终选择：直接持有 `parentProjection` 引用，因为它是最轻量的数据源（已在内存中）。

### D2: Drain 模式 — 500ms 超时转发尾部事件

**决策**：`TagentAgent.Run()` 的 wrappedCh goroutine 收到最终响应后，进入 drain 模式：
1. 设置 500ms 超时
2. 继续从 `invOutputCh` 读取事件并转发到 `wrappedCh`
3. 超时或 `invOutputCh` 关闭后退出

**理由**：
- 框架 Runner 在最终响应后的尾部事件通常在毫秒级产出
- 500ms 足够覆盖 MemoryPlugin.OnEvent + EmitEvent 的延迟
- 超时后强制退出，避免 goroutine 泄漏（runEventLoop 会阻塞在 bus.Pull 上）

**备选**：
- 不 cancel，让 runEventLoop 自然退出 → runEventLoop 在 RunFlow 后会 bus.Pull 阻塞，不会自然退出
- 等待 invOutputCh 关闭 → 不会关闭，因为 runEventLoop 还在运行
- 0ms drain（直接 cancel）→ 当前行为，导致事件丢弃

### D3: Prompt 引导 — TOOLS.md 增加 event_keys 说明

**决策**：在 `examples/wechat-bot/resources/prompts/TOOLS.md` 中增加 event_keys 使用指南段落。在 knowledge 和 recall 的 tool description 中增加 event_keys 参数说明。

**理由**：
- prompt 引导让 LLM 在需要历史上下文时主动选择相关 event_keys
- 自动注入作为兜底，覆盖 LLM 不传的情况
- 两者互补：prompt 提升精度，auto-inject 保证可用性

## Risks / Trade-offs

- **[自动注入不相关上下文]** → 只注入最近 N 个事件，不注入全部历史。最近 N 个通常与当前任务相关。LLM 可以通过 prompt 引导更精确地选择。
- **[500ms drain 增加延迟]** → 子 Agent 调用方会多等最多 500ms。在实际场景中（LLM 调用本身需数秒），500ms 可接受。
- **[parentProjection 引用增加耦合]** → AgentToolWrapper 需要持有 SessionProjection 引用。但 AgentToolWrapper 已经持有 parentStore（MemoryStore），增加 projection 引用不违反依赖方向。
- **[drain 期间新事件混入]** → drain 模式可能收到 framework 的非尾部事件（如下一轮 ReAct 的事件）。但子 Agent 单轮语义保证：收到最终响应后不再有 ReAct 循环，drain 期间的事件都是框架尾部事件。

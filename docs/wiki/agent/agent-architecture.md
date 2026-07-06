# tagent/agent 模块架构文档

## 一、模块定位

`tagent/agent` 是 tagent 项目的**事件驱动执行引擎**。核心设计思想源于 [prototype/agent.go](../../../prototype/agent.go)（126 行抽象实现），原型用可替换的函数字段定义了一个可扩展的框架骨架。

### 原型到生产的映射

| 原型 | 生产 | 说明 |
|------|------|------|
| `eventBus chan Event` | `EventBus` | 事件流，Publish/Pull 后丢弃 |
| `inputs []string` | `SessionProjection (EventReference[])` | 有界投影，onEvent 追加、ContextManager 读取、Compactor 清理 |
| `model *Model` | 框架 `model.Model` (通过 LLMAgent) | 框架处理 ReAct 循环 |
| `tools map[string]func` | `AgentToolWrapper` 实现 `CallableTool` | 子 agent 作为工具 |
| `Run func()` | `TagentAgent.runEventLoop(ctx, bus, cm)` | Pull → BuildInvocation → RunFlow |
| `OnEvents func([]Event) Event` | `ContextManager.BuildInvocation` + `RunFlow` | 合并消息 + 执行 Flow |
| `Compact func()` | `Compactor.Compact` (BeforeModel 回调) | 投影有界化 |

### 三个不变量

- **不变量 1**：inputs 是投影（有界，读写同一份数据）→ SessionProjection = EventReference[]，框架 Runner 的 Plugin.OnEvent 填充 StateDelta，onEvent 从 StateDelta 构建 EventReference 追加到 SessionProjection
- **不变量 2**：Compact 修改投影不修改事件流 → Compactor 清理 SessionProjection，不碰 MemoryStore
- **不变量 3**：工具结果回写 bus 不直接操作 inputs → 框架 Runner 内部执行工具，结果自动追加到 session

### 框架 Runner 内部行为

trpc-agent-go 的 Runner 在 `runner.Run` 内部完成：

1. 创建/获取 session
2. 追加用户消息到 session（`sessionService.AppendEvent`）
3. 触发 Plugin.OnEvent（SummaryPlugin 先注入 Tag；MemoryPlugin 后写 MemoryStore + StateDelta，含 `event_summary`）
4. 构建 messages（ContentRequestProcessor 从 session.Events 提取 Response.Choices[0].Message）
5. BeforeModel 回调链（按注册顺序）：
   - **Callback 0: InjectEventKeys** — 从 SessionProjection 读取 refs，为非 system/tool 的 message 注入 `[evt_KEY|type]` 前缀（每次 LLM 调用都执行）
   - **Callback 1: SmartCompressor** — 如 token 超限，按任务边界压缩 messages（仅修改 `[]model.Message`，不修改 projection）
   - **Callback 2: Compactor** — 如压缩后仍超限，从 projection 重建 messages（旧引用用 EventSummary，最近 N 条从 MemoryStore 还原）
6. 调用 model.GenerateContent
7. 工具执行（FunctionCallResponseProcessor）
8. 追加 response event 到 session（appender → `sessionService.AppendEvent`）
9. Emit event 到 channel

**关键结论**：
- 框架 Runner 完成 `sessionService.AppendEvent` 和 `MemoryPlugin.OnEvent`
- tagent 的 `makeOnEventCallback` 仅做 `projection.Append`（从 StateDelta 构建 EventReference，含 MemoryPlugin 生成的 `event_summary`）
- LLM 在每次调用时都看到带 `[evt_KEY|type]` 前缀的 messages（由 Callback 0 统一注入）

## 二、核心组件

### 2.1 TagentAgent（组合根）

**文件**：`tagent_agent.go`（866 行）
**原型对应**：`BaseTAgent.New()` + `Run`

`TagentAgent` 是 tagent 的顶层装配点。它创建 EventBus、ContextManager、SessionProjection，并提供对外 API：

- `NewTagentAgent(cfg)` — 构造，创建 ContextManager（含统一 Runner）
- `StartLoop(userID, sessionID)` — 启动持久事件循环，返回 outputCh
- `Run(ctx, inv)` — 子 agent 单轮调用，创建临时 ContextManager + 临时 EventBus
- `InjectMessage(msg)` — 向 activeBus 发布 external_input
- `StopLoop()` — 停止持久循环
- `Close()` — 关闭 ContextManager（释放 Runner）

### 2.2 runEventLoop（事件循环）

**原型对应**：`DefaultRun`

```go
func (ta *TagentAgent) runEventLoop(ctx context.Context, bus *EventBus, cm *ContextManager) {
    const maxRetries = 3
    retryDelays := []time.Duration{100ms, 200ms, 400ms}

    for {
        events, err := bus.Pull(ctx)          // ① 拉取事件（批量）
        msg := cm.BuildInvocation(events)     // ② 合并为一条 user message
        if msg.Content == "" { continue }

        // ③ RunFlow with exponential backoff retry
        for attempt := 0; attempt <= maxRetries; attempt++ {
            if err := cm.RunFlow(ctx, msg); err != nil {
                if attempt < maxRetries {
                    time.Sleep(retryDelays[attempt])  // 退避等待
                    continue
                }
                ta.publishErrorEvent(bus, err)  // 重试耗尽→发布错误事件
            }
            break
        }
    }
}
```

**错误处理**：RunFlow 失败后指数退避重试（100ms → 200ms → 400ms，最多 3 次）。重试耗尽后发布 `AgentEvent{Type: "external_input", Source: "error"}` 到 EventBus。`BuildInvocation` 跳过 `Source="error"` 事件，不触发模型调用，但外部监听器可感知。

`StartLoop` 在 goroutine 中调用 `runEventLoop`（使用 persistentBus + ContextManager）。
`Run()`（子 Agent 调用路径）创建临时 EventBus + ContextManager 后，同样在 goroutine 中调用 `runEventLoop`，并在第一个 `agent_output` 到达后关闭 channel。

### 2.3 ContextManager（消息构建 + Flow 执行）

**文件**：`context_manager.go`（425 行）
**原型对应**：`OnEvents` + `ModelCompletion`

`ContextManager` 创建唯一的 Runner（LLMAgent + MemoryPlugin + SummaryPlugin + SessionService），注册 BeforeModel 回调（SmartCompressor + Compactor），并提供：

- `BuildMessages(refs)` — 从 EventReference 构建 messages（按需从 MemoryStore 拉取完整 Content）
- `InjectEventKeys(messages, refs)` — 注入 `[evt_KEY|type]` 前缀
- `BuildInvocation(events)` — 合并 bus 事件为一条消息
- `RunFlow(ctx, msg)` — 调用 `runner.Run`，转发事件到 outputCh + bus，onEvent 做 projection.Append
- `SetUserIDSessionID(userID, sessionID)` — 设置 runner.Run 的 session 上下文

### 2.4 makeOnEventCallback

仅做 `projection.Append`（从 event.StateDelta 构建 EventReference）。框架 Runner 已完成 `sessionService.AppendEvent` 和 `MemoryPlugin.OnEvent`。

### 2.5 SmartCompressor

**文件**：`smart_compress.go`（483 行）

两阶段压缩，注册为 BeforeModel 回调：
- Stage 1：按任务边界（agent_output）丢弃旧任务段
- Stage 2：对丢弃的段生成 LLM 摘要（可选）

使用注入的 `TokenCounter`，不自行创建。

### 2.6 Compactor

**文件**：`task_segmenter.go`（141 行，与 TaskSegmenter 合并）

投影有界化，注册为第二个 BeforeModel 回调。当 SmartCompressor 不足以压缩时，从 SessionProjection 层面清理旧引用替换为 summary reference。

### 2.7 SessionProjection + BuildEventReference

**文件**：`projection.go`（82 行）
**原型对应**：`inputs []string`

`SessionProjection` 是有界的 `EventReference[]`，线程安全。`BuildEventReference` 从框架 event.Event 的 StateDelta 构建 EventReference。

### 2.8 EventBus

**文件**：`event_bus.go`（154 行）
**原型对应**：`eventBus chan Event`

per-agent 有序事件队列。Publish 非阻塞，Pull 阻塞直到有事件。

### 2.9 AgentToolWrapper

**文件**：`tool_agent.go`（458 行）
**原型对应**：`tools map` + `RegisterTool`

将子 agent 包装为 `CallableTool`，处理 event_key 参数解析和外部上下文注入。

## 三、文件结构

| 文件 | 行数 | 职责 | 原型对应 |
|------|------|------|---------|
| `tagent_agent.go` | 866 | 顶层装配 + runEventLoop + A2A + makeOnEventCallback | `BaseTAgent.New()` + `Run` |
| `context_manager.go` | 425 | 消息构建 + 压缩编排 + Compact + Flow 执行 + TokenCounter + 统一 Runner | `OnEvents` + `ModelCompletion` |
| `smart_compress.go` | 483 | 两阶段压缩 | 无（生产扩展） |
| `tool_agent.go` | 458 | AgentToolWrapper + PlainToolFactory/ToolAgentFactory 注册接口 | `tools map` + `RegisterTool` |
| `trajectory_recorder.go` | 325 | LLM 调用轨迹记录 | 无（生产扩展） |
| `event_bus.go` | 154 | EventBus | `eventBus chan Event` |
| `http_api.go` | 153 | HTTP API（RL/AReaL 集成） | 无（生产扩展） |
| `meditation.go` | 143 | 冥想心跳 | 无（生产扩展） |
| `task_segmenter.go` | 141 | 任务分段 + Compactor | `Compact` |
| `projection.go` | 82 | SessionProjection + BuildEventReference | `inputs []string` |
| `output_limit_tool.go` | 73 | 工具输出截断 | 无（生产扩展） |
| **总计** | **~7600** | **10 个文件** | |

## 四、数据流

```
用户调用 InjectMessage / 外部事件到达
    │
    ▼
EventBus.Publish(AgentEvent{external_input})
    │
    ▼
TagentAgent.runEventLoop:
  ① bus.Pull(ctx) → 批量取出事件
  ② cm.BuildInvocation(events) → 合并为一条 model.Message
  ③ cm.RunFlow(ctx, msg)
       │
       ├─ runner.Run(ctx, userID, sessionID, msg)
       │    ├─ 创建/获取 session
       │    ├─ 追加用户消息到 session (sessionService.AppendEvent)
       │    ├─ Plugin.OnEvent (SummaryPlugin 先注入 Tag，MemoryPlugin 后持久化 + StateDelta + event_summary)
       │    ├─ ContentRequestProcessor 从 session.Events 构建 messages
       │    ├─ BeforeModel 回调 0: InjectEventKeys（从 projection 读取 refs，注入 [evt_KEY|type] 前缀）
       │    ├─ BeforeModel 回调 1: SmartCompressor.Compress（如超限，修改 Request.Messages）
       │    ├─ BeforeModel 回调 2: Compactor.Compact（如仍超限则清理 SessionProjection 并重建 messages）
       │    ├─ model.GenerateContent
       │    ├─ FunctionCallResponseProcessor (工具执行 + 迭代控制)
       │    ├─ handleEventPersistence (sessionService.AppendEvent)
       │    └─ EmitEvent → event channel
       │
       └─ for fwEvt := range eventCh:
            ├─ onEvent(fwEvt) → projection.Append (仅此一项)
            ├─ outputCh <- fwEvt
            └─ if final: bus.Publish(agent_output echo)
                │
                ▼
  ④ 回到 bus.Pull — 下一轮事件
```

## 五、tagent 与 trpc-agent-go 的边界

**tagent 独有**：
- `EventBus` + `runEventLoop`：持久事件循环 + 异步事件注入
- `SessionProjection` + `Compactor`：有界投影 + 投影清理
- `SmartCompressor`：两阶段上下文压缩
- `MemoryStore` + `MemoryPlugin`：结构化事件存储 + 因果链
- `MeditationManager`：定时冥想
- `TrajectoryRecorder`：LLM 调用轨迹记录

**框架已有（tagent 复用）**：
- `runner.Run` (Flow.Run)：ReAct 循环、工具执行、迭代控制
- `ContentRequestProcessor`：从 session 构建 messages
- `FunctionCallResponseProcessor`：工具执行
- `BeforeModel` / `AfterModel` 回调
- `session.Service`：session 管理 + AppendEvent
- `model.Model`：LLM 调用
- `event.Event`：事件结构
- `tool.Tool` / `CallableTool`：工具接口

## 六、上下文管理

### 6.1 压缩（SmartCompressor）

SmartCompressor 作为 BeforeModel 回调，在框架构建 messages 后、调用 model 前执行：
- Stage 1：按任务边界（`agent_output`）丢弃旧任务段
- Stage 2：对丢弃的段生成 LLM 摘要（如果配置了 `summaryModel`）
- **仅修改 `args.Request.Messages`，不修改 SessionProjection**

### 6.2 Compact（Compactor）

Compactor 作为第二个 BeforeModel 回调，当 SmartCompressor 不足以压缩时触发：
- 从 SessionProjection 读取所有 EventReference
- 按任务分段，保留最近 N 个任务，旧任务折叠为 summary reference
- `projection.Replace(compacted)` + 从 compacted projection 重建 messages
- **修改 SessionProjection（投影），不修改 MemoryStore**

### 6.3 MaxToolIterations

- 主 agent：`DefaultMaxToolIterations = 50`
- 子 agent：`DefaultSubAgentMaxToolIterations = 10`（如父配置更低则取父配置）

通过 `llmagent.WithMaxToolIterations` 注册到框架 LLMAgent。

## 七、子 Agent 调用

`TagentAgent.Run(ctx, inv)` 是子 agent 单轮调用路径：
1. 从 `inv.RunOptions.RuntimeState["external_context"]` 读取父 Agent 传入的上下文（A2A 兼容路径）
2. 创建临时 EventBus + SessionProjection + ContextManager（含临时 Runner）
3. 将外部上下文注入到首条用户消息
4. 发布初始消息到临时 bus
5. goroutine 中调用 `runEventLoop`
6. 消费 outputCh 直到第一个 final response（无 tool_calls），关闭 channel
7. 恢复 activeBus 到 persistentBus

子 agent 的 MaxToolIterations 取 `min(父配置, 10)`，默认不超过 10。

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
4. 构建 messages（ContentRequestProcessor 从 session.Events 提取，session limit=2）
5. **BeforeModel 统一回调**（Projection-first 设计）：
   - Step 1: **TryPull + 即时持久化** — 从 EventBus 非阻塞拉取新事件，立即 StoreEvent + Projection.Append
   - Step 2: **ContextCompressor** — 从 Projection 解析全部 refs 为带 `[evt_KEY|type]` 前缀的消息，超预算时触发 SmartCompressor
   - Step 3: **提取当前轮次** — 从 args.Request.Messages 中提取无前缀的 assistant/tool 消息（当前 ReAct 迭代产物）
   - Step 4: **消息重建** — `[system] + 历史(from Projection) + 当前轮次`
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
- `Run(ctx, inv)` — 子 agent 单轮调用，创建临时 ContextManager + 临时 EventBus，直调一次 `RunFlow`
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

`StartLoop` 在 goroutine 中调用 `runEventLoop`（使用 persistentBus + ContextManager），持续 `for { Pull; RunFlow }` 直到 `StopLoop`。

`Run()`（子 Agent 调用路径）**不使用 `runEventLoop`**：它创建临时 EventBus + ContextManager 后，先将驱动请求 `persistBusEvent` 写入临时 `SessionProjection`（保证请求位于时间线首条），再在 goroutine 中**直接调用一次 `RunFlow`**。Turn 边界即 `RunFlow` 的自然返回——`RunFlow` 内部由框架跑完完整 ReAct 工具循环（多轮）直到最终 assistant 响应才返回，随后 `close(invOutputCh)` 通知调用方结束。子 Agent 与持久循环**共享同一个 turn 原语 `RunFlow`**，区别仅在于是否包裹 `for { Pull }` 守护循环；不再依赖「事件流探测 + drain 定时器 + 强制 cancel」判断 turn 结束。

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

将子 agent 包装为 `CallableTool`，处理 event_key 参数解析和外部上下文注入。子 agent 调用**默认异步**：经上下文注入的 `TaskSpawner` 纳入任务层执行，dense 阶段内返回则内联、越窗则 ack（`asyncDisabled` 可回退为同步）。每次子 agent Task 仍持有独立 EventBus / SessionProjection / ContextManager 的并发隔离契约。

### 2.10 TaskManager（异步任务层）

**文件**：`task_manager.go`、`task_board.go`、`event_bus.go`；探测器 `tool/action/settle.go`、`poll_schedule.go`；任务工具 `tool/task/`

确定性（非 LLM）的任务注册表 + 调度器，让 `action`（tmux）等长耗时工具与子 agent 不阻塞事件循环：

- **spawn + settle-or-detach**：`Spawn` 在 `select{settle, detach}` 上等待——探测器在自适应轮询的 **dense→sparse 边界**发出 `Detached()`，即"同步→异步"的 ack 点（已退役独立 `sync_wait` 旋钮）。dense 内 settle → 内联；detach 先到 → ack + 后台跟踪；越界后到的 settle 走 `OnSettle`（不丢失）。
- **自适应轮询**：`TmuxMonitor` 按任务年龄逐会话调度——dense 密集探测、几何退避至 `max_interval`；`stable` 服务型任务钉在最稀档（alive-detached）。参数经 `MonitorConfig` 配置。
- **settle 三档**：`completed` / `stable` / `suspect`，探测器只做确定性分类，语义判断交给 LLM。
- **task_settled 回收 turn**：后台任务结算发一条自包含事件到 EventBus；持久循环空闲则唤醒、进行中则排队。
- **看板 + 工具**：`BeforeModel` 每 turn 从 registry 重渲染 live 看板（不参与压缩，置于 recency 锚点）；`list_tasks` / `get_task_result` / `cancel` / `relaunch` 为即时同步工具。
- **会话回收**：进程真死（completed/error）时回收 tmux 会话，避免长程运行累积死会话。

对应能力规格：`async-task-execution`、`task-registry-and-board`、`adaptive-poll-scheduling`。

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
       │    ├─ ContentRequestProcessor 从 session.Events 构建 messages (session limit=2)
       │    ├─ BeforeModel 统一回调 (Projection-first):
       │    │    ├─ TryPull + persistBusEvent（新事件即时入 Projection）
       │    │    ├─ ContextCompressor.Compress(refs)（解析 + 压缩）
       │    │    ├─ extractCurrentTurnMessages（提取当前 ReAct 轮次）
       │    │    └─ 消息重建: [system] + history + currentTurn
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
- Stage 1：按任务边界（`agent_output`）丢弃旧任务段；`protectPendingAsyncSegments` 保护含 `{status:running}` 的未完成异步工具结果
- Stage 2：对丢弃的段生成 LLM 摘要（如果配置了 `summaryModel`）
- `buildCompressEvent`：从 oldSegments 消息的 `[evt_KEY|type]` 前缀提取每个被压缩事件的 key + type + summary，生成可读清单供 LLM 按需 recall
- `extractExecutionState`：提取工具调用/结果精简行（截断参数可配置，扩展支持 `[system] tmux` 异步结果）
- **仅修改 `args.Request.Messages`，不修改 SessionProjection 或 MemoryStore**（纯视图变换，遵守不变量 2）

截断参数通过 `SmartCompressorOption` 配置（`WithMaxExecStateChars`、`WithMaxToolResultChars`、`WithMaxToolArgsChars`、`WithChunkSize`、`WithChunkSummaryLen`），也可通过 YAML `compress` 段配置。

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

### 6.4 配置化

压缩参数通过 YAML `compress` 段配置：
```yaml
agents:
  - name: tagent
    compress:
      max_tool_result_chars: 500
      max_exec_state_chars: 2000
      chunk_size: 1000
      chunk_summary_len: 150
```

TmuxMonitor 参数通过 ActionProperties `monitor` 段配置：
```yaml
tools:
  - kind: tool
    id: action
    properties:
      monitor:
        interval: 10s
        stable_duration: 30s
```

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

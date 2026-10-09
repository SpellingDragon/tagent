# tagent/agent 模块架构文档

<a id="module-position"></a>
## 一、模块定位

`tagent/agent` 是 tagent 项目的**事件驱动执行引擎**。核心设计思想源于 [prototype/agent.go](../../../prototype/agent.go) 的抽象实现，原型用可替换的函数字段定义了一个可扩展的框架骨架。

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

### 四个不变量

- **不变量 1**：inputs 是投影（有界，LLM 输入的唯一装配源）→ SessionProjection = EventReference[]（现居 `agent/compress` 包），`assembleRequest = [system] + render(投影)`，永不读回框架消息尾部
- **不变量 2**：写入统一——事件被存储 ⇔ 被投影，恰好一次，同点原子（MemoryPlugin.OnEvent 存储后经 ProjectionSink 同点追加）
- **不变量 3**：时序是构造保证——BeforeModel 时投影必完整，非时序碰巧
- **不变量 4**：Compact 只修改投影，不修改事件流也不修改存储层

### 框架 Runner 内部行为

trpc-agent-go 的 Runner 在 `runner.Run` 内部完成：

1. 创建/获取 session
2. 追加用户消息到 session（`sessionService.AppendEvent`）
3. 触发 Plugin.OnEvent（SummaryPlugin 先注入 Tag 与 `event_summary` 元数据——**非内容总结，原文视图**，内容级总结收归压缩固化时刻；MemoryPlugin 后写 MemoryStore + StateDelta）
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

<a id="core-components"></a>
## 二、核心组件

### 2.1 TagentAgent（组合根）

**文件**：`agent.go`
**原型对应**：`BaseTAgent.New()` + `Run`

`TagentAgent` 是 tagent 的顶层装配点。它创建 EventBus、ContextManager、SessionProjection，并提供对外 API：

- `NewTagentAgent(cfg)` — 构造，创建 ContextManager（含统一 Runner）
- `StartLoop(userID, sessionID)` — 启动持久事件循环，返回 outputCh
- `Run(ctx, inv)` — 被调方执行入口：为本次调用运行有界事件环（调用作用域总线 + 请求级隔离的 CM/投影），输出交本次调用的通道（见 §七）
- `InjectMessage(msg)` — 向 activeBus 发布 external_input
- `StopLoop()` — 停止持久循环并**终结实例**（输出通道恰关一次；二次 `StartLoop` 显式报错（重启语义=新建 agent 实例））
- `Close()` — 关闭 ContextManager（释放 Runner）

### 2.2 runEventLoop（事件循环）

**原型对应**：`DefaultRun`

```go
func (ta *TagentAgent) runEventLoop(ctx context.Context, bus *EventBus, cm *ContextManager) {
    const maxRetries = 3
    retryDelays := []time.Duration{100ms, 200ms, 400ms}

    for {
        events, err := bus.Pull(ctx)          // ① 拉取事件（批量；混合批先丢弃冥想谱系事件，投递同谱系故同受此条）
        msg := cm.BuildInvocation(events)     // ② 合并为一条 user message
        if msg.Content == "" { continue }

        cm.SetTriggerSource(extractTriggerSource(events))     // 消费侧确定性派发
        cm.SetInvocationMetadata(extractRootMetadata(events)) // chat_id/user_name 等经 meta_* 传播

        spanCtx, turnSpan := startTurnSpan(ctx, turnSpanAttrs{   // ③ turn root span（noop 安全）
            AgentName: ta.name, TriggerSource: source, ChatID: chatID,
            BatchSize: len(events), EventSources: eventSources(events),
            LinkTraceID: meta[trace_id], LinkSpanID: meta[span_id], // task_settled 回流 → span link
        })
        spanCtx = governance.WithTriggerSource(spanCtx, source) // ctx 盖章 trigger source（goal 门消费）

        retried, retriedDegenerate := false, false
        for attempt := 0; attempt <= maxRetries; attempt++ {
            if attempt > 0 {                    // 重试前查 ctx + 退避等待（select 可被 ctx 打断）
                if ctx.Err() != nil { endTurnSpan(turnSpan, retriedDegenerate); return }
                select {
                case <-time.After(retryDelays[attempt-1]):
                case <-ctx.Done(): endTurnSpan(turnSpan, retriedDegenerate); return
                }
            }
            if err := cm.RunFlow(spanCtx, msg); err != nil {
                if attempt < maxRetries { retried = true; continue }
                // 重试耗尽：仅记日志 + degradation.ReportFailure(DepModel)——每 turn 至多一次，
                // ctx 取消（关机）不计退化；RunFlow 只返回传输层错误（model-API 错误经 outputCh
                // 流出），故不发布错误事件。
                if ta.degradation != nil && ctx.Err() == nil {
                    ta.degradation.ReportFailure(DepModel, err)
                }
            } else {
                if ta.degradation != nil { ta.degradation.ReportSuccess(DepModel) } // 恢复路径
                // 退化 turn（无工具调用且空 final）额外重试一次——判定在**成功分支内**
                if cm.LastTurnDegenerate() && !retriedDegenerate && attempt < maxRetries {
                    retriedDegenerate = true; continue
                }
                break
            }
        }
        endTurnSpan(turnSpan, retriedDegenerate) // 退化重试记为属性，同一 turn 不另开 root span
        if ta.meditationMgr != nil {
            ta.meditationMgr.UpdateLastTurnEnd(time.Now()) // 空闲闸门锚点：任意 turn 结束都算忙
        }
    }
}
```

**错误处理**：RunFlow 失败后指数退避重试（100ms → 200ms → 400ms，最多 3 次；ctx 取消不计）。重试耗尽后**仅记日志并上报 DegradationManager 的 model 依赖失败**（每 turn 至多一次，成功时经 `ReportSuccess` 走 degraded→recovering→normal 恢复路径；RunFlow 返回的是传输层错误，model-API 错误经 outputCh 流出，故不再发布错误事件）。退化 turn（无工具调用且空 final）在**成功分支内**判定并额外重试一次（同一 turn span，`degenerate_retry` 属性标记，不另开 span）。`BuildInvocation` 只要求 `Type=external_input` 且 `Message` 非空，不区分 Source。

`StartLoop` 在 goroutine 中调用 `runEventLoop`（使用 persistentBus + ContextManager），持续 `for { Pull; RunFlow }` 直到 `StopLoop`（终结态，不可重启——输出通道在循环退出时恰好关闭一次，二次关闭会 panic，故二次 Start 显式拒绝）。

`Run()`（被调方路径）**复用同一共享壳**：入口的 `runEventLoop` 与被调方的 `runAgentLoop` 是同一条管线在各自总线上的消费者——同一 turn 原语（`processTurn` 内完成一次完整 RunFlow ReAct 循环）、同一重试预算；不存在绕过事件消费的直调快路径，也不依赖「事件流探测 + drain 定时器 + 强制 cancel」判断 turn 结束（终止＝投递对账，见 §七）。并发调用互不串投影：隔离由调用作用域总线与请求级投影承担。

### 2.3 ContextManager（粘合层：消息构建 + Flow 执行）

**文件**：`context_manager.go`（引擎侧唯一与压缩域双向衔接的文件）
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

**文件**：`compress/smart_compress.go`（agent/compress 子包）

骨架模型确定性压缩（task-skeleton-compression，唯一压缩管线；定级/丢弃纯工程，L3 折叠为双层结构）：

| 级别 | 触发（age = 段在新→旧序列中的位置，指数边界） | 段内保留 |
|------|------|---------|
| L0 | 进行中段 或 `age < keepRecent` | 全部消息 |
| L1 | `age < keepRecent*2` | 骨架 + `thinking_plan`（丢 `action_command`） |
| L2 | `age ≥ keepRecent*2`（老化封顶档） | 仅骨架（`external_input` + `agent_output`） |
| L3 | 仅预算升级（骨架化后仍超预算，自最老段起逐段升档，达标即停；段龄不触发归档） | 整段移出时间线 → 滚动 summary 归档（票据层 + 可选综述层） |

- 段 = 以 `agent_output` 为界的完整任务回合；基础定级为段龄纯函数（`deterministicLevel`，零 LLM，封顶 L2——L3 仅由预算升级抵达，段数不是触发器）
- 预算升级 O(n)：预计算每段四级成本后 O(1) 增量升档
- 使用注入的 `TokenCounter`，不自行创建
- L3 折叠双层：工程票据层（卡片行 + `[evt_key]` 召回票据）恒在；`summary_model` 配置时叠加 LLM 滚动综述 `synthesizeRollingNarrative`（旧综述 + 新折叠段骨架**原文**增量合成为单行 `〔历史综述〕`，编译期常量限长，失败/无模型降级纯工程，纯携带轮零调用）
- 卡片浓缩（`condenseCardLines`）：卡片超 `card_max_chars` 时浓缩较旧一半，保留 `[evt_key]` 票据

### 2.6 Compactor（滚动卡片序列）

**文件**：`compress/context_compressor.go` + `compress/task_segmenter.go`

投影有界化：旧引用替换为**滚动** summary reference（携带卡片序列，跨轮吸收——计数累计/卡片继承/时间下界继承）。详见 [memory 架构文档「记忆策展」章](../memory/memory-architecture.md)。

### 2.7 SessionProjection + ProjectionSink

**文件**：`compress/projection.go`（随压缩域——"Compact 只修改投影"，投影即压缩域对象）+ `projection_sink.go`（plugin 包）
**原型对应**：`inputs []string`

`SessionProjection` 是有界的 `EventReference[]`，线程安全、EventKey 幂等去重。写入统一在事件插件管线：RunFlow 用 `plugin.WithProjectionSink` 把当前 invocation 的投影绑到 ctx，MemoryPlugin 在 store 成功后同点 `Append`。

### 2.8 EventBus

**文件**：`event_bus.go`
**原型对应**：`eventBus chan Event`

per-agent 有序事件队列。Publish 非阻塞，Pull 阻塞直到有事件。构造经 `NewReliableEventBus(spillDir)`：配置 `reliability.bus_spill_dir` 时启用 **durable inbox（v2）**——每个输入在回执之前先落盘，channel 只承载唤醒脉冲（不是「满才溢」的二级路径），at-least-once；空则回退纯 channel（默认，零行为变化）。存在 `*.spill` 残留或未排空的 v1 树时**拒绝升级**并给出迁移指引。语义详见 [持久投递与依赖退化](../reliability/durable-delivery.md)。文件亦含 task_settled 事件构建（自包含 + Origin trace 锚回填）。

### 2.9 AgentToolWrapper

**文件**：`tool_agent.go`
**原型对应**：`tools map` + `RegisterTool`

将子 agent 包装为 `CallableTool`，处理 event_key 参数解析和外部上下文注入。子 agent 调用**默认异步**：经上下文注入的 `TaskSpawner` 纳入任务层执行，dense 阶段内返回则内联、越窗则 ack（`asyncDisabled` 可回退为同步）。并发调用不串投影/接收者：隔离由调用作用域总线与请求级隔离投影承担（同一管线，见 §七）；本地目标借用唯一常驻 owner 实例，其执行配置随组织代统一推进。

### 2.10 TaskManager（异步任务层）

**文件**：`task/task_manager.go`、`task/task_board.go`（agent/task 子包，零引擎依赖的叶子包）、`event_bus.go`；探测器 `tool/action/settle.go`、`poll_schedule.go`；任务工具 `tool/task/`

确定性（非 LLM）的任务注册表 + 调度器，让 `action`（tmux）等长耗时工具与子 agent 不阻塞事件循环：

- **spawn + settle-or-detach**：`Spawn` 在 `select{settle, detach}` 上等待——探测器在自适应轮询的 **dense→sparse 边界**发出 `Detached()`，即"同步→异步"的 ack 点（已退役独立 `sync_wait` 旋钮）。dense 内 settle → 内联；detach 先到 → ack + 后台跟踪；越界后到的 settle 走 `OnSettle`（不丢失）。
- **自适应轮询**：`TmuxMonitor` 按任务年龄逐会话调度——dense 密集探测、几何退避至 `max_interval`；`stable` 服务型任务钉在最稀档（alive-detached）。参数经 `MonitorConfig` 配置。
- **settle 五档 + 唯一终态入口**：`completed` / `stable` / `suspect` / `watch`（命中通知，非终态）/ `failed`（reconcile 回收），探测器只做确定性分类；终态转换**只经 `finalize`**（内存状态、信号 Kind、WAL settle_status、反馈极性四端一致——失败必带失败语义，未知 Kind 映射 unknown 告警不落 completed）；终态后迟到信号被 fencing 丢弃（不得复活/重复结算）。
- **task_settled 回收 turn**：后台任务结算发一条自包含事件到 EventBus；持久循环空闲则唤醒、进行中则排队。
- **Origin 信使行李**：TaskSpec.Origin 携带 spawn turn 的调用元数据（chat_id 等 + trace_id/span_id 锚点），任务层只透传不解读；task_settled 回流的新 turn 据锚点建 OTel span link，连接 spawn/settle 两棵 trace（跨 turn 闭环）。
- **registry = 事实链 fold**：spawn 伴随 `task_spawned` 一等事件（载 Declarative 声明式投影）；**inline settle 也补发终态记录**（`task_inline_record` 标记）+ 后台 settle 携结构化 `task_id/settle_status` Metadata——重启经 `RebuildTaskRegistry` 纯全量回放重建 active 态（running→suspect 交存活探测裁决，终端不重建），闭包按 Kind 承诺表由工厂重建（command 全套；subagent 仅 Relaunch、跨重启 Resume 返回引导；generic 展示）。TaskManager 归属每个 agent 自身——一个 tagent 生命周期内唯一，换执行器代不丢任务板；父的委派任务与子的内部任务分属各自任务域，不合并为全局表。
- **看板 + 工具**：`BeforeModel` 每次调用从 registry 重渲染 live 看板（不参与压缩，**追加在消息列表末尾**——看板字节逐次变化，置于尾部使前缀缓存仅损失看板自身，等待指引行同时是模型读到的最后内容）；`list_tasks` / `cancel` / `relaunch` / `resume_task` 为即时同步工具（结果消费不走专用工具：小结果随 settle 通知内联，大结果转储文件经 read_file 分页）。
- **resume_task 重入**：合法源状态 {alive-detached, stable, completed, failed}；tmux 经 detector `Rearm`（绑会话非轮次，零换绑），subagent 经新 Run + 任务链还原器。详见 [tool 架构文档「任务重入」章](../tool/tool-architecture.md)。
- **会话回收闭环（ADOPT-FIRST）**：运行时 completed/error 即回收；优雅退出 `Close()` 收编存活会话；启动**先收养后清扫**——每个重挂/已跟踪会话刷新 `LastAdoptedAt`（收养即 freshness 锚），Sweep 只收割「无人收养且距最后收养超 TTL」的孤儿，**spawn 年龄不构成清理依据**（长驻会话不因 24h 生日被误杀）。探测三态化：`SessionAlive3`（list-sessions 单源：在列表=活/不在=死/命令不可辨=unknown），monitor 连续 N 次（默认 3）unknown 才按 dead 处理（fail-dead 加闸，tmux 抖动不再误杀常驻会话）。
- **统一 TTL 回收（假活治理）**：命令在 spawn 时确定有限 `ttl`（模型入参；否则取 `task_default_ttl`，缺省 10min），到期由**唯一** reaper 经 owner `detector.Cancel` 真实终止 → failed 终态 → reap → 移出看板。准入覆盖全部 active 态（running/stable/alive_detached/suspect）与全部寿命类（含常驻/交互服务，**无按 mode 豁免、无禁用哨兵**），堵死旧双墙「gate 在 alive_detached/stale + detachedAt、漏掉未 detach 的 suspect」的盲区。`op=send`/`resume_task` 续命重置锚点，`op=peek` 只读不续；restored 任务经声明式投影恢复其 `ttl`。年龄回收仅 TTL 一条路径，无按 mode 豁免、无禁用哨兵。detachedAt 仍入事实链供观测，但不再作为终止判据。
- **世系跨重启保真**：`Spec.Origin` 深拷贝入 `task_spawned`，恢复时身份字段以持久层为准（恢复闭包只补执行能力，不得整体覆盖）；无世系历史恢复为 `unknown`，宿主对 unknown 与内部来源同等扣留——内部任务（如冥想派生）跨重启不再退化为可投递来源。`settle_status/task_id/lineage_absent/detached_at_ms` 为框架控制键，不进后续任务的 Origin baggage。
- **跨重启连续**：冷启动序 = 投影重建 → registry 重建 → 常驻会话重挂（`residentReattachOnce` 唯一挂载点，多 agent 仅首实例）→ TaskID 桥（重挂跟踪的会话将其 suspect 任务提升回 running）。常驻会话生命周期入事实链（`resident_session` spawn 全参/终态事件），meta 目录可配（`resident_meta_dir`）。
  - 投影重建是**事实链的纯回放**：取最新的压缩快照，再把尾部事件重放出来，目标是逐字节复原换代前的上下文（同样的字节才谈得上复用前缀缓存）。只在启动期做一次、进的是空投影，并且排在兜底落盘的重放接线之前。事实链里没有带代际标记的压缩事件时（首次启动、或从未折叠）整步 no-op，维持现状行为；热重建的执行壳跳过这一步——投影属常驻实例，丢弃壳上重建是空跑。
  - **进程重启 vs 整机重启（证据边界）**：常驻重挂以 **live `tmux list` 为存活真源**对账磁盘 `ResidentMeta`——(a) **进程重启**（tagent 崩溃/升级，tmux server 存活）：tmux 会话仍在列表 → 重挂成功 → suspect 任务经 TaskID 桥提升回 running，执行现场连续；(b) **整机重启**（tmux server 随之消亡）：`ResidentMeta` 磁盘持久仍在，但 `tmux list` 为空 → 无存活可挂 → 在飞任务**不复活**（registry fold 后 running→suspect，探测判死）。两态下**事实链均不受影响**（正 key 事件 + settle_fold 票据原样在链，recall 仍可取回原文）——即「耐久真相源恒存，易失执行现场仅进程重启可续」。

- **遥测通道与消费降级**：结算通知是机器遥测而非对话输入——其保留由**消费状态**决定（确定性推导：回收 turn 的产出与 outputCh 投递记录），不由相邻关系或段龄决定。compaction act 时：已消费且外显的通知降级为票据卡（settle_fold 单条折叠，原文 recall 可达；failed 卡片行带 ★ 进反思通道）；内部性（冥想派生/退役结算/无世系）保一行摘要 keepRecent 轮后降级；**未消费通知保持完整且被 L3 豁免**（至少一次在通道层的延伸）。看板是任务状态的唯一常驻呈现，通知只承载"事件到达"。行为审计（self-telemetry-audit）滚动统计自管遥测占比，L2 拒绝自管来源的新 spawn、L3 冻结非保护类（保护类由构造声明豁免，磁盘闸仍适用）——与 disk block spawn 同闸不同源。

**一个 tmux 命令的一生**（把上面的零件串成一条线）：

```mermaid
sequenceDiagram
    participant LLM
    participant Tool as ActionTool
    participant TM as TaskManager
    participant Mon as TmuxMonitor(自适应轮询)
    participant Bus as EventBus/持久循环

    LLM->>Tool: Call(command)
    Tool->>TM: Spawn(spec, TmuxSettleDetector)
    TM->>Mon: 启动会话 + 按年龄调度(dense→backoff)
    alt dense 阶段内结算
        Mon-->>TM: settle(completed/stable)
        TM-->>Tool: 内联结果
        Tool-->>LLM: 最终结果(体验如常)
    else 越过 dense(detach)
        TM-->>Tool: ack(task_id, running)
        Tool-->>LLM: "已在后台运行"
        Note over Mon: 稀疏轮询直至结算
        Mon-->>TM: settle(后台)
        TM->>Bus: task_settled 自包含事件
        Bus->>LLM: 触发回收 turn(空闲唤醒/进行中排队)
    end
```

对应能力规格：`async-task-execution`、`task-registry-and-board`、`adaptive-poll-scheduling`。

子任务登记在**提供 spawner 的那个 agent 自己的** TaskManager 看板上，而不是入口的看板：`TaskSpawner` 由所属 agent 注入，看板与 TaskManager 一一对应。因此"任务落在哪一级"由抬起它的那一级决定，跨级重放与退役判定都按这一归属解析。

### 2.11 治理与可靠性接线（本模块落点）

buildAgent 对**所有 agent** 的非 wrapper leaf 工具经 GovernanceTool 过闸（包裹链 `OutputLimitTool(GovernanceTool(raw))`，per-agent 独立 BudgetManager + 共享 Ledger/Classifier/Approval/Goals；refine 工具仅 entry、先于治理包裹追加）；event_loop 上报 model 依赖退化、turn ctx 盖章 trigger source（goal 门消费）；ReliableBus/AnchorStore 为 opt-in（目录配置非空启用）。详见 [platform 篇](../platform/platform-subsystems.md)。

**逐 agent 的模型解析**：调用方预解析好的覆盖实例优先（入口的 `SwappableModel` 就走这条）；该 agent 未声明模型时继承父模型；否则按它的 provider 与模型名从 provider 池解析；解析失败回落父模型并告警——一个次要 agent 的模型配错不该让整个构建失败。

### 2.12 turn-as-trace 可观测

每 turn 一棵 trace（`tagent.turn` root span，属性含 trigger_source/chat_id/event_sources；退化重试记为属性不另开 span；ctx 早退补 End；task_settled 据 Origin 锚建 span link）——事件 Metadata / RL 轨迹 / OTel span 三投影由 trace_id 互链，noop provider 零开销。详见 [platform 篇](../platform/platform-subsystems.md)。

### 2.13 执行代与发布（executor generations）

ContextManager 的 runner 是**可换代缝**，换代由「构造 → 纳管 → 激活」三段与一条唯一线性化承载：

- **构造与发布分离**：`NewExecutorCandidate(face)` 只装配新 runner 与执行面（模型/工具/装饰器；装配是纯函数——冷启动与换代共用同一路径，防双路径漂移）。单 owner 的发布入口是 `PublishExecutor(candidate, face)`；组织路径经 `StageExecutor`（候选私有 overlay + 有序责任表，纳管期完成声明持有与工具接线）→ `ActivateExecutor`（安装纳管好的绑定）。两条入口共用**同一条线性化** `publishActiveLocked`：记录执行面 → 装代 → 换 runner → 在锁外交出待退役绑定（慢 Close 不占执行锁）。同一 runner 重发不产生第二代（一个 runner 对象恰好一次 Close）；执行面记录推进、绑定保持其建成时快照。
- **turn 取代点**：`BeginTurnLease()` 是业务 turn 取得执行绑定的唯一位置——先触发非阻塞懒检查，再钉定当时已发布的 effective；lease 可发布进调用链上下文，派生执行（嵌套委派、传输重试、ACK 后后台续写）在**同一代**上累加引用，不因新发布改路由。`BeginTurn` 是其便利形态。
- **drain-free 换代**：进行中 turn 持旧代引用跑完；退役判定 = 已退役 ∧ 在途引用归零 ∧ 声明持有归零（一个代为其面内声明的子 owner 持有使用权，声明方存活期间被声明代不回收）。
- **热参数读取**：五个数值热参（压缩阈值/预算/保留数/任务 TTL 两值）不随换代推送——各 owner 从唯一已提交应用记录在**消费边界现读**（压缩器经注入的热参源拉取、任务 spawn 经 TTL 源读取），结构代与数值轴分离，无第二份可独立修改的真值。
- **懒检查**：`SetOrgReloader` 闭包在业务 turn 起点触发（单次 stat，未变更零成本）；结构变更经指纹对比触发 candidate-then-publish（fail-closed + 双槽回滚环）。详见 [platform 篇 §六·A](../platform/platform-subsystems.md)。

<a id="meditation-curator"></a>
### 2.14 反思与策展：一个机制，两种观察面

冥想只有一套机制：门控到点后，`MeditationManager` 的动作恒为**向本 agent 常驻循环的 session 注入一个冥想输入事件**（`source=meditation`），由这个 agent 的一个正常 turn 完成反思——不跨 agent 注入，也不借投递缝绕路。**"自察还是策展"不是两套形态，只是观察面配置的差别**：`meditation.observed_namespaces` 未声明即缺省 **[自身分区]**——入口 agent 配冥想就是回看自己，反思事件落进它正在跑的那条业务 session，与业务回合共享会话上下文（自体维护的语义因此原样保留，零配置零迁移）；显式列出他人分区，就是 `agents:` 下的一个同构 agent（无新 agent 类型、无新运行时机制，先例是 recall agent）做跨域策展；自身与他人混列同样合法，同一趟扫描、同一条判据一次覆盖。观察面只改变"读谁"与"反思落在谁的 session"，不改变动作本身。**使用形态的默认取向是独立策展线**：反思声明给专门的策展 agent、固定跑在保留 session 上——不占业务 session 的上下文预算，内部叙述也不混进用户可见的对话历史（wechat-bot 示例即此形态，业务 agent 的 meditation 块处于关闭）。把冥想直接配在业务 agent 上是合法的进阶形态（该 agent 的反思会落进它的业务 session、与其共享上下文），留给"复盘方必须当面看到当前业务上下文"的需求。

- **授权边界**：显式声明的观察面必须 ⊆ `memory.read_namespaces` ∪ {自身分区}（自身恒合法、他人须授权），未授权分区**具名拒绝启动**，绝不静默剔除。缺省**不回落** `read_namespaces`——"能读谁"不等于"反思谁"，回落等于悄悄扩观察面。查询层本身 default-nothing，越界只能靠装配强制，见 [记忆篇 13.1](../memory/memory-architecture.md#read-paths)。
- **门控三件（唯一形态定义）**：interval 自查节奏；自身空闲门 `lastTurnEnd`（任意谱系的回合结束都算忙，含投递触发的回合与失败回合）；novelty 门——观察面内存在 `Timestamp > lastMeditation` 且**非自管谱系**的事件（经 `NoveltyReader` 读事实链上入库时盖章的持久归因 `Metadata[trigger_source]`）。锚点是**两锚结构**（`lastTurnEnd`/`lastMeditation`），后者兼作水位：有效触发即推进并自锁，无需额外重置；历史三锚文件里的多余键在 Load 时被忽略（自然兼容，无迁移）。
- **判据唯一，且结构上无法自持**：自管与否一律经 `event.SelfManagedLineage` 单源派生（判据处零清单副本，冥想产出与巩固建议天然不计入新鲜度）；未盖章 `trigger_source` 的事件按未知谱系处理、不计入（宁可少反思，不可误判新鲜，判定过程落 debug 日志）；查询失败或没接上读缝时**门保持关闭**（fail-closed：读不通的事实链既不是"没新东西"也不是"有新东西"，也没有任何备用判据可回落）。`task`/`system_alert` 等非自管输入计入新鲜度是**有意为之**：观察他人时那是被观察 agent 的真实后台活动，正是跨域理解的素材；观察自己时它就是"自己的新输入"。反思主体自己的产出只落自身分区，喂不到自己。
- **早停水合**：`EventReference` 不带 Metadata，所以判据先把降序引用页（上界 `noveltyScanPageLimit`）按分区计数，再逐条 `GetEvent` 水合读谱系，**命中即停**；一次判据只扫一遍，digest 复用同一份证据。
- **digest 单一覆盖面**：以**观察面概况**为主——各观察分区自水位以来的分谱系计数（非自管／自管与未知）、引用页数与水合样本数、最近一条非自管活动（带可解析事件键 `[hex]` 与 `trigger_source`）；观察面只有自身时，这份概况就是"自体近况"。**自身任务层降为可选段**（挂有任务层才渲染，没有则省略、不产空壳），空闲时长恒含。
- **session 安排**：注入目标恒为本 agent `StartLoop` 起的那条循环 session——所以"反思落在哪"完全由"冥想配在哪个 agent"决定。业务线沿用宿主路由（wechat-bot：`TAGENT_SESSION_ID`，缺省 `wechat-session`）；反思线的默认形态是独立策展 agent + **保留名**（推荐 `curation`）、固定单 session，与业务线物理同 store、逻辑隔线——每次换新 session 等于冷启动重建投影并丢掉反思连续性，而它的增长由该 agent 自己的 `compress_threshold` 在同一 session 内折叠（单压缩权自管），永不与用户路由撞名。
- **产出落反思主体自己的分区**：经验卡片/综述是写进**自己**分区事实链的普通事件，不写 compaction 事件、不改任何被观察分区的状态；策展卡片要回到业务线只经 `deliver_to` 白名单回流，目标侧的上下文瘦身仍由它自己的阈值折叠承担——压缩权不可转移。三类产物（脚本/skill/prompt）与 `refine register` 登记义务对任何反思主体同样适用，不论它在观察谁。
- **投递缝与热更归属**：产出回流别起第二通道，裁决表见 [持久投递·谱系可见性](../reliability/durable-delivery.md#lineage-visibility) 内的投递缝一节；`observed_namespaces`/`deliver_to` 在构造期读取，随 meditation 块整体参与组织指纹，改即**换代**，见 [组织热更](../platform/org-hot-reload.md#fingerprint)。

<a id="package-layout"></a>
## 三、包与文件结构（分包后）

```mermaid
graph TB
    subgraph agent["agent/ 引擎本体"]
        AG["agent.go 组合根"]
        EL["event_loop.go + event_bus.go"]
        TR["trace.go turn span"]
        CM["context_manager.go 粘合层"]
        SE["session.go + inject.go + lifecycle.go"]
        TW["tool_agent.go 子Agent封装"]
        MD["meditation*.go 冥想"]
    end
    subgraph compress["agent/compress 压缩域"]
        SC["smart_compress.go L0-L3"]
        CC["context_compressor.go 卡片序列"]
        PJ["projection.go + task_segmenter.go"]
        TK["token_counter.go + defaults.go"]
    end
    subgraph task["agent/task 任务域（叶子包）"]
        TM["task_manager.go 生命周期+resume"]
        TB["task_board.go 看板"]
    end
    subgraph governance["agent/governance 治理闸（默认关）"]
        GT["gate.go + tool.go 决策管线+装饰器"]
        GB["budget/ledger/approval/classifier"]
    end
    subgraph reliability["agent/reliability 常驻可靠性（默认关）"]
        DG["degradation.go 五依赖状态机"]
        SP["spill.go + anchor.go"]
    end
    agent --> compress
    agent --> task
    agent --> governance
    agent --> reliability
```

| 包/文件 | 职责 | 原型对应 |
|------|------|---------|
| `agent.go` | 顶层装配 + TagentConfig；OutputLimitTool 包裹全部工具（封顶 `toolOutputCapChars`=60K，与 MaxTokens 解耦——防长 budget 下 MaxTokens/2×4 派生形同虚设；超限全量存 `<workspace>/tool-output`，返回带路径摘要） | `BaseTAgent.New()` |
| `event_loop.go` / `event_bus.go` | 持久循环（turn span + model 退化上报 + 退化重试）+ 事件队列（可选 ReliableBus 溢出） | `DefaultRun` / `eventBus chan` |
| `trace.go` | turn root span（`tagent.turn`）开/关与属性（trigger_source/chat_id/event_sources）；task_settled span link | 无（生产扩展） |
| `context_manager.go` | 粘合层：消息构建 + 压缩编排 + Flow 执行 + 统一 Runner + Attribution/OriginSpawner 绑定 | `OnEvents` + `ModelCompletion` |
| `tool_agent.go` | AgentToolWrapper + 任务链还原器 + 工具注册接口 | `tools map` + `RegisterTool` |
| `meditation.go` / `meditation_digest.go` | 冥想心跳 + 自我状态 digest（PromptSource 为 prompt.Getter）；一个 manager、一条判据，观察面（缺省＝自身）决定是自察还是策展（§2.14） | 无（生产扩展） |
| `governance/` | GovernanceGate 决策管线（classify→critical 批准→goal→budget→记账）、GovernanceTool leaf 装饰器、BudgetManager、ApprovalManager、DenialLedger、RiskClassifier | 无（生产扩展，默认关） |
| `reliability/` | DegradationManager（memory/disk/rustviking/model/mcp 五依赖退化-恢复）、Inbox（durable inbox-v2，受理前落盘；前代 SpillStore 已停用，仅余格式识别与受管重置）、AnchorStore（冥想锚点跨重启） | 无（生产扩展，默认关） |
| `compress/` | SmartCompressor、卡片序列 Compactor、SessionProjection、TokenCounter、压缩默认常量单源 | `Compact` + `inputs` |
| `task/` | TaskManager、settle 探测契约、看板、resume、跨包测试基建（fixture.go）；Origin 携带 trace 锚 | 无（生产扩展） |
| `rl/`（独立顶级包） | TrajectoryRecorder（含 trace 关联字段）+ HTTPAPI（**token 认证 + loopback fail-closed**：`TAGENT_RL_AUTH_TOKEN`/`ValidateListenAddr`，实施加固 3.x）+ SwappableModel（retired model 延迟回收，5.2） | 无（生产扩展） |

依赖方向由编译器执法：`agent → compress`、`agent → task`、`agent → governance`、`agent → reliability`，子包零反向依赖，新代码直接 import 子包。

agent 包内 50 个文件按职责分五组，子域已独立成包（`task/` 任务生命周期、`compress/` 压缩域、`governance/` 治理闸、`reliability/` 退化追踪，各有独立篇）：

| 组 | 文件与职责 |
|---|---|
| 事件循环（引擎主干） | `agent.go` 聚合根与 AgentConfig；`event_loop.go` runEventLoop 主循环（Pull 批处理、退避重试、降级 backoff）；`event_bus.go` EventBus + AgentEvent + durable inbox 受理；`inject.go` InjectMessageWithSource 渗透入口；`trace.go` turn span |
| 上下文管理（LLM 视图） | `context_manager.go` 粘合层（投影/持久化/settle 反馈/bundle 章盖章）；`output_overflow.go` outputCh 宽限与溢出票据；`helpers.go`、`lifecycle.go` 辅助与生命周期；`session.go` 子 agent 调用路径 |
| 子 Agent | `tool_agent.go`（最大文件）AgentToolWrapper：本地与 A2A 统一封装、重入、交接；`a2a.go` 远程协议 |
| 冥想 | `meditation.go` 门控触发（血统无关的空闲闸门 + 观察面 novelty 闸门，判据唯一）与观察面的分谱系扫描；`meditation_digest.go` digest 组装（观察面概况为主、自身任务板可选）——机制判据见 §2.14 |
| 可选注入（经 TagentAgent setter） | 退化与可靠性注入经 `agent/reliability`；治理经 `govGate`；自进化经根包 |

顶层模块的层与归属（大扫除第二阶段后）：

| 模块 | 职责 | 依赖方向 |
|---|---|---|
| 根包 `tagent`（组合根，11 文件终形） | 装配（build_agent/wiring/builtin/registry/partition_collision 注册表 + config_alias/org_alias 别名层 + prompts_embed/testing）+ **编排发布权**（`org_hotreload.go` 的 orgCoordinator：换入/发布/告警）+ 面向消费者的入口 | root → config, agent, tool, … |
| `config/`（配置模型层） | 编排声明的类型实体（`Config`/`AgentConfig`/`ToolRef` 族）与 `LoadConfig`、严格校验、生命周期投影、纯查询（记忆段指纹、可达拓扑、仅远端声明判定）；根包以**别名再导出**保持 `tagent.*` 公共 API 源码级不变 | config → agent, prompt, tool, workspace；**MUST NOT 回指 root** |
| `agent/`（引擎本体） | 事件循环、上下文管理、子 Agent 封装、冥想；主体不依赖 config（模型经 root 的别名与注入面进入装配） | agent → plugin → memory；不 import config |
| `agent/org`（世代机制，已落地） | 退役账本、候选事务簿记、换壳 overlay——机制在此；**发布动作留根包**，两侧只经注入契约（壳构造回调、注册表接口、resident 句柄）协作 | agent/org → config, agent；MUST NOT import root |
| `agent/resources`（资源租约治理） | 共享存储/引擎的最后引用清理、目录写锁（flock）、毒化封闭与重开判定；组合根 wiring 经 `resources.DefaultResources.Acquire` 接线，装配级测试留根包、深白盒用例随包 | agent/resources → config, memory；MUST NOT import root/agent |

已迁出的机制件为 `Ledger`（退役账本）、`Txn`（候选责任表）、`Overlay`+`BuildOwners`+`ShellBuilder`+`Deps`（私有构造域）、`ComputeOrgFingerprint`/`ExtractOrgSubset`/`CanonicalAgentSubset`（世代簿记，agent/org/fingerprint.go）、`OrgStatus` 等 5 状态类型（agent/org/status.go，根以 org_alias.go 同名再导出），根侧唯一接缝 `runtimeConfig.orgDeps(loader)`。漂移审计器（evolution/asset_drift.go）与整理提示（memory/consolidation_hint.go）同期归域。方向由编译器与 `TestArch_LayeredDependencyDirection` 双重执法。发布动作为何必须留在根包：`architecture-guardrails` 的「唯一编排发布权」规定组合根独占执行绑定的构造与发布，内部包不得触及编排内部状态——把 orgCoordinator 下放即违反该条，故世代**机制**可迁、**特权**留根。

<a id="data-flow"></a>
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
  ③ startTurnSpan(tagent.turn) → spanCtx
  ④ cm.RunFlow(spanCtx, msg)
       │
       ├─ runner.Run(ctx, userID, sessionID, msg)
       │    ├─ 创建/获取 session
       │    ├─ 追加用户消息到 session (sessionService.AppendEvent)
       │    ├─ Plugin.OnEvent (SummaryPlugin 先注入 Tag，MemoryPlugin 后持久化 + StateDelta + event_summary)
       │    ├─ ContentRequestProcessor 从 session.Events 构建 messages (session limit=2)
       │    ├─ BeforeModel 统一回调 (Projection-first):
       │    │    ├─ TryPull + persistBusEvent（新事件即时入 Projection）
       │    │    ├─ ContextCompressor.Compress(refs)（原生时间线渲染 + 压缩）
       │    │    └─ 消息重建: [system] + render(投影)（单行化，永不读回框架消息尾部）
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
  ⑤ 回到 bus.Pull — 下一轮事件
```

<a id="framework-boundary"></a>
## 五、tagent 与 trpc-agent-go 的边界

跨这条边界的数据流是**严格单向**的：送给模型的最终消息列表只由投影装配（`[system] + render(projection)`，任务面板由后续回调注入），绝不从框架的 message 尾部读回任何东西。每一个事件（用户输入、工具调用、工具结果、终局、总线注入）都经事件插件管线或 `persistBusEvent` 进入投影，因此"模型可见"与"已被投影"是同一件事——曾经存在过的"可见但未投影"状态就是顺序缺陷的根源。

**tagent 独有**：
- `EventBus` + `runEventLoop`：持久事件循环 + 异步事件注入
- `SessionProjection` + `Compactor`：有界投影 + 投影清理
- `SmartCompressor`：两阶段上下文压缩
- `MemoryStore` + `MemoryPlugin`：结构化事件存储 + 因果链
- `MeditationManager`：冥想心跳（双闸门触发：血统无关的空闲闸门 `lastTurnEnd` + 新颖性闸门——观察面内水位之后的非自管谱系事件，观察面缺省＝自身分区；见 §2.14）
- `DeliverToAgent`（根包 `delivery.go`）：进程内跨 agent 投递缝——白名单+盲投+未知目标+未运行四道具名拒绝，收口在目标既有的 `InjectMessageWithSource` 入口
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

<a id="request-budget"></a>
### 5.1 完整请求预算：固定的一半由装配面交进去

历史折叠只能付得起"可压内容"那一半。一次装配的**固定开销**——装配好的 system、本回合将注入的动态看板与恢复通告、冻结的工具声明、协议信封——由装配面（`agent/context_manager.go:assembleRequest`）打包成 `compress.RequestBudgetContext` 交给压缩器，因此"能不能发"这件事只有一个裁决者。

| 判据 | 内容 | 佐证 |
|---|---|---|
| 单一估源 | 固定侧用**发出请求时同一把尺**定价（`modelutil.RequestSnapshot.EstimateBudget`），压缩器与预算永不可能各算各的 | `RequestBudgetContext.FixedOverhead` |
| 常数单源 | 估算用的三个数（每字符比、每条消息余量、每次工具调用余量）只在 `modelutil` 定义一次，压缩器的默认计数器**引用同一组常数**，因此两侧不可能各抄一份而漂移 | `modelutil.CharsPerToken`、`agent/compress/token_counter.go` |
| 快照是深拷贝 | S1 声明快照与请求快照由**纯函数**产出冻结值：不持生命周期、存储或调度器引用，拷贝辅助全为非导出，公开面只有快照本身 | `copyMessages`、`ToolDeclarationSnapshot` |
| 声明序全序 | 快照里的工具声明按 `RegistryKey` 排序——每个 map 键唯一，因此它是**全序**；`Name` 只作防御性决胜，结果与 map 迭代序无关 | `sort.Slice`（`request_snapshot.go`） |
| 参数全量计量 | 工具调用的原始 JSON 参数字节**全量计入、绝不截断**：截断会让预算低估真实出站体积 | `EstimateBudget` |
| 空请求零信封 | 没有消息、没有工具、也没有结构化输出 schema 时**不加信封余量**，与计数器对空输入返回 0 同调——预算绝不把空快照误读成"有内容" | `protocolOverhead` |
| fixed 输入集 | system ＋ notices ＋ 冻结声明 ＋ 信封余量四类进固定侧；历史由压缩器自己的计数器定价，落在另一个桶 | `FixedOverhead` |
| Unknown 申报 | 调用方量不出的部件（如无可用元数据的媒体）进 `ExtraUnknown`：**只申报、不估价**，因此量化总额是**下界**而不是"精确总数"。未知既不当 0 也不冒充测得 | `RequestBudgetContext.ExtraUnknown` |
| 缺省 ≠ 真零 | 不传参数＝"调用方对固定部分一无所知"，触发线与内层目标照旧取热组值；传了但字段全空＝**权威零**（空 system、无工具真的不花钱），可压内容预算作为显式值下发，于是一个零预算不会被降级回默认值 | `RequestBudgetContext` 文档 |
| 值快照 | 传的是**这一次请求携带的东西**（与出站 SDK 请求同一份冻结声明），并发的 schema 热更换不掉球门 | `assembleRequest` |
| 看板只渲染一次 | 装配面为定价固定开销而渲染本回合看板，并把**同一份文本**贴进真正出站的消息尾部：看板字节随任务年龄变化，二次渲染会发出预算从未计过账的内容；本轮没有交接时才走原本那次渲染 | `assembleRequest` `RequestBudgetContext.NoticesText` |
| 先切 system 再定价 | system 的切分发生在定价之前，因此固定侧量的就是这一回合即将发出去的那段文本，而不是配置里的陈旧副本 | `assembleRequest` |
| 具名拒发 | 固定部分已 ≥ 输入上限时，压缩器返回 `budget_exceeded`（`compress.BudgetExceededReason`）并**原样交回完整时间线**：不删 system、不删工具声明、不做第二次压缩"腾地方"——`ContextCompressor` 始终是唯一的压缩权威 | `context_compressor.go` `BudgetExceededReason` |
| 门禁只搬运 | 最终门禁（`executionGateModel`）**不发明第二种裁决**，只把压缩器的裁决转成发送前的一次拒绝（`agent.ErrBudgetExceeded` 包装同一常数） | `execution_gate_model.go:ErrBudgetExceeded` |
| 拒发先于消费 | 拒绝发生在**一次性恢复通告附着之前**：这一轮一个字节都没发出去，所以通告仍然欠着（它只在真正发起调用时消费），时间线原样保留 | `GenerateContent` `withRecoveryNotice` |
| 两条出路同一个判决 | 记录在**每次装配**时设置或清除，因此一次拒发不会活过产生它的那一轮；通道与迭代两条出路读到同一份裁决。迭代路径必须以**带 Error 的 Response** 呈现，绝不返回空流——零输出流会被归约成"回合完成"，从而把已认领的耐久输入 ack 掉 | `setBudgetRefusal` `takeBudgetRefusal` `GenerateContentIter` |
| 数字同源 | 拒绝里报出的上限从**压缩器取数用的同一个热参数源**读出（`liveInputLimit`），无源时回落构造期值／预算线：门禁报的数就是压缩器用过的数，不是镜像猜测 | `liveInputLimit` |

<a id="context-management"></a>
## 六、上下文管理

### 6.1 压缩（SmartCompressor）

SmartCompressor 由 ContextCompressor 在 BeforeModel 装配回调中调用，在投影解析为消息后、调用 model 前执行（骨架模型，见 §2.5 定级表与 [event-flow.md §六](event-flow.md)）：
- 以 `agent_output` 为界切分完整任务回合（`SegmentMessages`），段龄纯函数定级，按 `tool > assistant` 序丢弃中间事件，零 LLM 不失败不降级
- L1 丢弃 `action_command` 结果时同步剥离对应 tool_calls（单轮产物自洽合法）
- L3 整段不进入产物，由 `buildRetainedRefs` 收编进滚动 summary（`external_input`/`agent_output` 成卡片行，recall 可溯源）
- 保留消息原样携带 `[evt_KEY|type]` 前缀，衔接存活 ref 判定
- **仅修改发给 LLM 的消息视图，不修改 SessionProjection 或 MemoryStore**（纯视图变换，遵守不变量 2；投影替换由 ContextCompressor 的 RetainedRefs 返回值驱动）

压缩参数通过 YAML `compress` 段配置（`summary_model`/`card_max_chars`/`compact_keys_listed`/`recent_full_count`/`summary_max_tokens`）。骨架管线为唯一压缩路径，其中 LLM 文摘恰有两处低频叠加层——L3 滚动综述 `synthesizeRollingNarrative` 与卡片浓缩 `condenseCardLines`，均无模型时降级为纯工程形态。

### 6.1.1 时间线渲染红线（外部分析常见误判点）

压缩产物**永远不进 system**。渲染规则的三条铁律：

| 铁律 | 含义 |
|------|------|
| system 恒单条恒首位 | 只装指令（system prompt）；任何机制产物不得追加进 system |
| 压缩摘要 = user 级〔历史归档〕注记 | `context_compress` 渲染为带"非用户发言勿模仿"标注的 user 消息——观察类信息归 user |
| assistant 恒等于 LLM 真实产出 | 系统永不代 assistant 说话，也永不在 assistant 历史中生成文本化调用语法（防模仿伪调用） |

> 之所以单列：曾有外部分析（基于可见 API 契约的合理外推）误判为"摘要 system 内联"——这是常见框架做法，但 tagent 恰恰以不这样做为设计红线。凡对模型可感知的渲染行为，本文档显式声明，不留猜测空间。

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
  tagent:
    compress:
      summary_model: deepseek-v4-flash    # 压缩专用模型(可用廉价模型)
      card_max_chars: 6000                # 卡片序列上限
      compact_keys_listed: 32             # 滚动摘要 recent keys 上限
```

TmuxMonitor 参数通过 ActionProperties `monitor` 段配置：
```yaml
tools:
  - kind: tool
    id: exec
    properties:
      monitor:
        dense_interval: 1s      # dense 阶段探测间隔
        dense_duration: 10s     # dense 阶段时长(=同步→异步 ack 点)
        backoff_factor: 2       # 几何退避因子
        max_interval: 60s       # 稀疏轮询上限
```

<a id="subagent-loop"></a>
## 七、子 Agent 调用（同构调用环）

`TagentAgent.Run(ctx, inv)` 是被调方的执行入口。**被调方与入口是同一种 tagent**——同一共享壳、同一 turn 原语、同一重试预算，自有事件总线与任务域；差别只在输出交给谁：

1. 输入（含 `RuntimeState["external_context"]` 的事件上下文与关联信息；resume 时由任务链还原器自动注入本任务前序轮次）转换为初始事件，**发布进本次调用的作用域总线**；
2. `invID→bus` 绑定表在入口注册——本次输入衍生的任务 settle 与 agent 输出的目的地在**输入时即确定**，运行期只查绑定、不猜；
3. 共享壳 `runAgentLoop` 消费该总线：每次迭代是一次完整 turn（`processTurn`，与入口同一原语）；请求级隔离的 CM/投影接本 agent 自己的服务（含任务控制器）；
4. 本 agent 任务域发起的后台任务，其 `task_settled` 按绑定路由回**本次调用的总线**——环持续消费，晚到结算触发续写轮（首答与续写同环、同一重试预算）；
5. 输出恒发往本次调用的通道：同步关联结果按原语义作为 tool result 返回发起 turn；父 turn 已结束的越窗输出按通知形态送达绑定接收者，不对同一 tool_call 重复结算；
6. 终止＝**投递对账**：绑定总线上无待达结算且无更多注入时环排空退出（调用方 ctx 为硬上限），不靠首条 assistant 消息、drain 定时器或文本探测关闭 agent。

被调用不构成第二套架构：本地目标借用唯一常驻 owner 实例（身份/存储/治理随组织代统一推进，不因角色变化缺能力）；远程 A2A 目标走同一 `agent.Agent` 接口。取消/超时只终结该调用环与通道，不 Close 被 agent、不级联其无关任务。子 agent 的 MaxToolIterations 取 `min(父配置, 10)`；运行参数只在其自身 `agents.<name>` 定义处配置（ToolRef 只声明引用关系）。

**委派目标的解析代**。委派目标只从**已发布代**的声明面解析，不读可变全局：编辑既有配置（不引入新语法）即改变下一个请求真正可调的子 agent，而入口运行时——存储、会话服务、常驻绑定表——在一次真实发布前后按**指针**保持身份（"换成内容相同的新实例"正是必须拦下的形态）。在途委派的代际边界是 turn：持租约的那一代把它服务完、答案恰好回给发起回合一次，新目标不得在其返回之前抢跑；换代的影响只对返回之后的请求生效。被移除的目标此后不再获得新调用，却保留其常驻属主，使同名再入复用原存储属主。

**重入的目标解析同用一条规则**：已存储任务的重投递，有发起调用绑定就用那次调用的绑定，否则用**当前有效执行面**。重投递走的是常驻构建，绝不能把此刻的包装器表冻成快照传下去——热更换代后若仍照快照解析，已被移除的目标会被旧代绑定静默复活。这与普通委派同源，不另立第二套解析规则。
句柄指向的必须是**常驻属主**的上下文管理器：候选壳在其运行器被发布后就丢弃，绑到壳上等于冻住一张死面，重入会永远从它路由出去。取不到常驻属主时按拒绝处理并说明理由，不猜一个面顶上。

**逐层与逐形态**。每一层委派带它**自己那代**的声明（入口→[b]、嵌套层 b→[c]、叶无工具），最深结果逐层回流到直接父、再回流到入口；`async` 取默认时委派被任务层收养（spawn 记录为 `agent:request` 形状）且结果仍 inline 交回请求回合；`kind:tool` 工厂产物在其所建之代被声明、被真实执行、返回值回到父回合——服务调用的正是已发布面构建时持有的那个实例。读全局 agent 表而非本层绑定，会表现为缺失或错误的工具声明、或结果永不抵达。

**远程 A2A 的线上传输契约**。除走同一 `agent.Agent` 接口外：只有远程引用、无任何本地定义的配置必须能加载，在真实模型请求里暴露该委派工具，确实落到声明的 URL，并把远端答案作为 tool result 回到父 turn（父请求原文随委派送出）；父存储解析出的 `event_key` 上下文以 transferred state 跨线送达远程，供给来自父绑定而非新绑定。声明为远程却缺 endpoint 必须在加载期拒绝，绝不静默按本地构建——那会运行一个与配置所述不同的运行时。传输重试固定**同一声明端点与同一委派载荷**（继承发起调用的租约），不在重试路径重解析目标；「重试过程中发布」把同名 agent 改指不同端点时，判别是因果的：父拿到答案之前后继端点一次都不得被联系，而原端点每次尝试都带同一载荷。本地回合不做这种传输重试。


---

<a id="test-support"></a>
## 八、测试替身住在包内的非 _test 文件里

包内测试共享的替身与构造器集中在 `agent/testsupport.go`（不是 `_test.go`）。原因是一条构建事实：内部测试（`package agent`）无法导入一个反向依赖 `agent` 的支撑包——Go 明确禁止测试里的导入环；而把那些内部测试改成外部测试包，又会牵出大量包内私有引用，属更大范围的重构。

关于真实框架行为的判据（例如 MemoryPlugin 的用户回声 `OnEvent` 是否**早于**进入模型、基座模型实现 `model.IterModel` 时框架是否真的走 `GenerateContentIter` 而非 channel 回退）一律不许猜：两者都决定执行凭据校验门能否安全地放在真实模型入口，猜错就是每回合误阻断。这类问题由一次性的真机探针harness取证，结论落进测试注释与本页，而不是靠推测写断言。

单元测试的 memory store 根必须**显式挪出仓库工作树**，并按单个测试用例隔离。原因是一条分派顺序事实：`resources.acquire` 在按 memory type 分派**之前**就无条件 `os.MkdirAll(path)` 并取目录写锁（写 `.tagent-writer.lock`），`type: localfile` 还会另建 `relations.journal`——所以"用 `type: memory` 配一个逻辑路径"并不等于不落盘，相对路径会在仓库根造出目录（历史提交里被跟踪的 lock 与 journal 即明证）。

根目录由 `t.TempDir()`／`b.TempDir()` 按用例唯一：同一用例内同名 store 返回同一绝对路径（热更多代"存储段字节不变"与重启模拟所依赖的身份前提由此成立）；不同用例（含 `-count` 重复，每次是全新 `*testing.T`）落在不同根，互不串存储；根不得跨用例共享，否则两个用例的存储接在同一条链上。

因此这些替身以非 `_test` 文件形态存在：文件名不受"测试文件须声明职责"这条判据约束，替身本身仍保持包内私有、只被测试引用。代价是它们会随库一起编译（不参与运行时行为）；若要消掉这一点，就得承担外部化改造的规模。

## 已知缺口与演进方向

> 本章主动声明当前设计尚未闭合的环——供使用者评估适用边界，也供外部分析引用（缺口以工程事实陈述，含现有防线与候选方向）。

| 缺口 | 现状与防线 | 候选方向 |
|------|-----------|---------|
| **子 Agent handoff 无结构化 schema** | 跨 Agent 传递依赖 `request` 自然语言 + `event_keys` 票据（票据本身是结构化 hex 契约，有真实 LLM 契约测试守护）；但"意图/约束/权限/未决决策"没有结构化载体 | 定义 handoff envelope（intent/constraints/grants 字段）随 external_context 传递 |
| **迭代上限无收尾轮** | 撞 `max_tool_iterations` 时进行中的工具调用直接丢弃（实机：plan 子 Agent 3m52s 的文档工作被掐断，靠模型自恢复换路完成） | 预算剩 1 轮时注入收尾提示，让模型保存半成品再终止 |
| **runEventLoop 单 session** | 一个 TagentAgent 实例绑定一个 (user, session) 循环；多会话需多实例 | 会话路由层（多循环共享引擎与存储） |
| **冥想无内容价值判据** | 双闸门已解决自触发永动机：触发需 `now - lastTurnEnd ≥ MinGap`（任意 turn 结束算忙）**且**新颖性门打开。新颖性门只回答"有没有新东西"，不回答"新东西值不值得反思"——它看的是"水位之后观察面里有没有非自管新事件"，一句无关闲聊或一条低价值任务事件同样解锁下一轮 | 未消化事件量/★ 卡片密度作为内容价值第三判据（单一取数面，不分形态） |

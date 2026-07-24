## Context

当前 tool 执行同步阻塞:ActionTool `Call()` 阻塞到 tmux 稳定态才返回,事件循环被一阶段命令卡死。曾试过纯异步(占位符 + 稳定态回写),因请求/响应分离导致 LLM 冗余响应/重复 tool use/空回复,退回同步。

已核实的关键前置事实:

- **TmuxMonitor 已有成熟的 settle 探测器**(`tool/action/tmux_monitor.go` `detectSessionState`):周期 poll + 输出 MD5 稳定计时(`StableSince`)+ 进程退出/pane dead 检测 + 静默超 `fake_dead_duration`(默认 150s)后**主动 heartbeat 探针**区分 FakeAlive(探针 ok=进程卡死但响应)/ FakeDead(无响应)。这正是"如何判断自然稳定"的现成机器。
- **框架支持并行工具执行**:`internal/flow/processor/functioncall.go` 的 `enableParallelTools` → `executeToolCallsInParallel`(WaitGroup + goroutine)。并行 sync-wait 依赖开启此项。
- **持久循环 Pull 天然可被新事件唤醒**:`task_settled` 只是又一种事件。
- **ref 回写机制已存在**:`event_key` + projection ref(大结果按 ref 回写复用之)。

## Goals / Non-Goals

**Goals:**
- 统一 spawn + sync-wait 窗口执行:窗口内 settle 内联、超窗 ack + 后台跟踪。
- 并行等窗口(不串行累加阻塞)。
- 按类型的 settle 探测器 + 三档分类(monitor 探测,LLM 解读)。
- 服务型 alive-detached。
- TaskManager(确定性)+ live 看板(recency 锚点、不压缩、age-out)+ LLM 任务工具。
- `task_settled` 触发回收 turn。
- 子 agent 异步分阶段、可回退。

**Non-Goals:**
- 不做状态持久化/跨重启恢复(纯 ephemeral;死任务 relaunch 仅进程内)。
- 不做周期/cron 任务。
- 不改 tmux 状态检测算法本身(复用)。
- 不改事件管线核心不变量(单一事件流 / RunFlow 作为 turn 原语)。

## Decisions

### D1: 统一 spawn + sync-wait 窗口

`Call()` = `spawn(task)` → `select { case settle := <-task.settled: inline(settle) ; case <-after(sync_wait): ack(id) + track }`。ack 与 inline 都诚实闭合,无占位符谎言。

**调优(已实施)**:短命令的结算走 tmux **completed(pane dead)** 检测,其时延取决于 monitor **poll interval**(非 `stable_duration`)。故把 `DefaultMonitorConfig.Interval` 从 30s 降到 **3s**,并取 `sync_wait = 10s`:结果 = 命令在几秒内完成 → 窗口内 settle → **内联返回**(响应式);>10s 的长命令 → ack + 异步。服务型任务靠 `stable_duration`(60s)判稳,天然 > `sync_wait` → 异步。即 `sync_wait` 只需 ≳ poll interval(+余量),无需 ≳ `stable_duration`。

### D2: 并行窗口

依赖开启框架 `enableParallelTools`,使一个 turn 内多个 tool_call 各自 goroutine 执行、各自等各自的窗口。单个任务对事件循环的阻塞 = `min(sync_wait, 真实响应时间)`——窗口内 settle 则在真实响应时刻提前返回(内联),不必等满 `sync_wait`。并行 N 个任务时,事件循环阻塞时长 ≈ `max_i min(sync_wait, real_response_i)` ≤ `sync_wait`(取并行中最慢者,**而非累加**)。若 `enableParallelTools` 未开,则退化为串行累加 `Σ min(sync_wait, real_response_i)`(需在集成时确认开启)。

### D3: settle 探测器抽象 + 三档

定义 `SettleDetector` 接口,按 Task 类型注入:
- tmux 命令 → 包装现有 `TmuxMonitor`(其 `StateChangeCallback` 映射为 settle 信号)。
- 子 agent → `RunFlow` 返回。
- 通用 → goroutine 返回。

settle `kind` ∈ {`completed`(退出), `stable`(输出稳定存活), `suspect`(fake_dead/timeout)}。**探测器只做确定性探测+分类,不判"是否真完成"**;LLM 在回收 turn 读 output 语义解读(服务就绪 vs 挂死)。这把判不准的语义交给 LLM,把可确定的探测留给 monitor。

### D4: 服务型 alive-detached

首次 `stable`(存活)→ 发一次"就绪"通知 → 转 `alive-detached`:看板紧凑常驻显示、后续输出变化不再触发回收 turn、不自动完成;结束靠 `cancel` 或进程死亡。避免服务型任务无限刷屏 / 永占看板 running。

### D5: TaskManager 为确定性组件(非 agent)

调度/注册/回写这类不能出错的事交给确定性组件,避免再引入一层可能空回复/幻觉的 LLM。LLM 仅通过 `list_tasks`/`get_task_result`/`cancel`/`relaunch` + 自动 spawn 使用它。registry 纯内存;spawn 幂等去重防重复发起。

### D6: 看板 = live 投影,不参与压缩

`BeforeModel` 每 turn 从 registry 重渲染看板,置于**最后 agent_output 之后、当前输入之前**。它是仪表盘(live 状态)而非日志(历史),因此不进压缩块;settled 且已处理的任务短 TTL age-out 保持有界。原始 spawn 的 tool_call 仍在可压缩历史里(可被压掉)——无妨,因为看板 + 自包含 `task_settled` 事件覆盖了关联。

### D7: task_settled 触发回收 turn

`task_settled` 经 EventBus 进入持久循环,像外部输入一样触发新 turn;空闲时唤醒 Pull;turn 进行中则排队到下一轮(不打断)。

### D8: 子 agent 异步分阶段、可回退

子 agent 涉及独立事件流/projection 交织与最初的 LLM 友好性风险,故置于**后置阶段**,前置(tmux 任务)验证通过后经开关启用,保留同步回退。并发隔离契约不变。

### D9: 大结果按 ref

settle 事件/看板仅放摘要 + `event_key` ref,LLM 用 `get_task_result(id)` 拉全量,避免上下文膨胀。

### D10: ActionTool 无状态 + 任务 spawner 经 RuntimeState 注入(call-time hook)

ActionTool SHALL NOT 持有 TaskManager 或任何任务生命周期状态。任务派发能力经**调用时注入**:

- **注入通道**:tagent 在启动 RunFlow 前,把一个 `TaskSpawner`(小接口 `Spawn(TaskSpec, SettleDetector) SpawnResult`,由 `*TaskManager` 实现)放入 invocation 的 `RunOptions.RuntimeState["task_spawner"]`——框架原生、随 ctx 传播、与 tagent 现有 `external_context` 同机制。工具经 `agent.InvocationFromContext(ctx)` 取出。
- **ActionTool.Call**:启动 tmux 会话 → 建 `TmuxSettleDetector` → 若拿到 spawner 则 `Spawn(spec, detector)`(得到 inline 或 ack);若无 spawner(独立使用)则**同步回退**——从 detector 读第一个 settle,保留当前阻塞语义。
- **监听路由(关键)**:`TmuxMonitor` 仅一个全局回调,今由 ActionTool 的 `waiters` map 多路复用(状态)。为让 ActionTool 真正无状态,改 monitor 支持**按会话回调**:`AddSession(session, onStateChange)` 把回调随会话记录存储,ActionTool 无需自持 map。(备选:保留 ActionTool 内瞬态 session→detector map,较不干净。)
- **write-back 分离**:异步 `task_settled` 由 TaskManager 的 `OnSettle` 发出(tagent 装配),ActionTool 从不发事件——它只负责 spawn。

理由:ActionTool 回归无状态纯工具;任务生命周期/看板/事件回写全在 agent 侧;工具与 agent 经窄接口 + RuntimeState 解耦,tmux 工具仍可独立使用(同步回退)。

## Risks / Trade-offs

- **[R1] sync_wait 期间有界阻塞**:窗口内事件循环仍被阻塞(有上界)。缓解:`sync_wait` 取较小值(覆盖快命令稳定即可);并行窗口避免累加。
- **[R2] enableParallelTools 未开则并行窗口退化**:集成时必须确认开启;否则多长任务串行阻塞。列为前置检查项。
- **[R3] suspect 误判**:静默的合法长任务(长时间无输出的计算)可能被判 suspect。缓解:suspect 只是"提示 LLM 关注",非强制终止;LLM 可选择继续等待;`fake_dead_duration` 可调。
- **[R4] 看板 token 成本**:每 turn 注入看板。缓解:紧凑渲染(id/短 desc/status/age),仅活跃 + 近期,age-out。
- **[R5] 子 agent 异步的交织复杂度**:最高风险项。缓解:分阶段 + 开关 + 独立测试 + 回退。
- **[R6] ephemeral 重启丢任务**:tmux session 不随进程重启存活;registry 内存丢失。接受(Non-Goal);仅进程内 relaunch。

## Migration Plan

分阶段(见 tasks.md):Phase 0 TaskManager+探测器抽象(确定性可测)→ Phase 1 tmux/ActionTool 接入 + 看板 + task_settled + LLM 工具 → Phase 2 alive-detached → Phase 3(gated)子 agent 异步 → Phase 4 真实 LLM 端到端 + 文档。每阶段独立可验证、可回退。ActionTool 同步阻塞语义在 Phase 1 被 sync-wait 取代(BREAKING,有测试护栏)。

## Open Questions

- `sync_wait` 默认值与 `stable_duration`/`poll interval` 的确切关系(实现时以真实 tmux 时序标定)。
- 幂等 spawn 的"语义等价"判定粒度(命令字符串完全相等?归一化后?)——先用保守的精确匹配,后续按需放宽。

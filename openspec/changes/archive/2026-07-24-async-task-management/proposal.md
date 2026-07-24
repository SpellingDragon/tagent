## Why

tagent 的定位是**长程运行 agent**——自由接收各类输入事件、决策,并在执行复杂任务时异步派发处理。但当前 tool 执行是**同步阻塞**的:ActionTool 的 `Call()` 一直阻塞到 tmux 稳定态才返回,整条事件循环被一阶段命令卡住。后果:

- 无法并行执行多个长任务;
- 无法运行常驻/长程任务(如起服务、长构建);
- 长命令执行期间新输入只能排队,违背"自由接收各类输入事件"的定位。

**历史教训**:曾试过 ActionTool 纯异步(`Call()` 立即返回占位符 + 稳定态 `InjectMessage` 回写),但因**请求与响应分离**导致 LLM 产生"我已启动,请稍候"的冗余响应、在结果回收前收到新输入而重复 tool use、甚至空回复——于是退回同步。

根本约束:**LLM tool-use 协议是同步请求-响应**,每个 `tool_call` 必须在下一条消息里有配对结果。要既异步又 LLM 友好,必须把"异步"拆成两个各自完整的同步事件:**① 派发(诚实的同步 ack)+ ② 回收(作为新 turn 的 settle 事件)**,并用一个始终可见的**任务看板**让 LLM 永不失忆。

（本 change 承接并深化 `unified-event-consumer-and-async-tool` 中未竟的 async-tool 思路,将其从"仅 ActionTool"提升为"统一任务层"。）

## What Changes

### 统一异步执行模型（sync-wait 窗口）

- 每个 tool / 子 agent 调用都 spawn 一个 **Task**。`Call()` 同步等待可配置的 `sync_wait` 窗口,等**第一个 settle 信号**:
  - 窗口内 settle → **内联返回结果**(快命令手感同 sync);
  - 超窗未 settle → 返回 **ack**(`任务 T 已启动 (running)，完成后通知`)+ 转后台由 TaskManager 跟踪。
- **并行等窗口**:一个 turn 内多个长任务各自并行等各自的窗口(不串行累加),依赖启用框架的 `enableParallelTools`。
- `sync_wait` 需 ≳ `stable_duration`,否则快命令还没被判稳定就被踢去异步。

### settle 探测器（按任务类型）+ 分档

- **tmux 命令**:复用现有 `TmuxMonitor`(周期 poll + 输出 MD5 稳定计时 + 主动 heartbeat 探针区分假死/假活 + 进程退出)。
- **子 agent**:settle 信号 = `RunFlow` 返回(干净的 done)。
- **通用**:goroutine 返回。
- settle 分三档:`completed`(进程退出,确定完成)/ `stable`(输出稳定,可用但可能在等输入)/ `suspect`(静默超 `fake_dead_duration`,疑似挂死)。**Monitor 只做确定性探测+分类,LLM 在被触发的 turn 读 output 做语义解读**(判断"服务已就绪" vs "疑似挂死")。

### 服务型任务 → alive-detached

- 输出稳定但进程不退(如 `listening on :8080`)的服务型任务:settle 一次(发"服务已就绪"通知)后转 **`alive-detached`** 态——看板紧凑显示、不再因后续输出变化刷屏、不自动完成;结束靠显式 `cancel` 或进程死亡。

### TaskManager + 任务看板

- **TaskManager**(确定性组件,非 LLM agent):内存 registry(ephemeral)、幂等 spawn 去重、settle → 发 `task_settled` 事件 → 触发新 turn。
- **任务看板**:每个 turn 由 `BeforeModel` 从 registry 重新渲染,放在**最后 agent_output 之后、当前输入之前**(recency 锚点);**不参与上下文压缩**(它是 live 状态快照,非历史);已被 LLM 处理的 settled 任务 **age-out**。
- **LLM 接口**:`list_tasks` / `get_task_result(id)` / `cancel(id)` / `relaunch(id)`;大结果按 `event_key` ref 回写,LLM 按需拉全量。
- `task_settled` 事件**自包含**(带 task_id + desc + 结果摘要/ref),与压缩解耦。

### 子 agent 异步（分阶段、谨慎）

- 将 `AgentToolWrapper` 调用纳入 task 层(settle = `RunFlow` 返回)。因涉及子 agent 独立事件流/projection 交织与最初担心的 LLM 友好性,**作为独立的后置阶段实施**,前置阶段验证通过后才启用,保留回退。

### 简化边界（Non-Goals）

- 不做状态恢复/持久化:registry 纯内存,重启即忘;"从死任务重发"仅在进程内(死/挂的任务保留 spec,可 `relaunch`)。
- 不做周期/cron 任务(但稳定感知本身是周期 poll)。

## Capabilities

### New Capabilities

- `async-task-execution`: 统一的 spawn + sync-wait 窗口执行模型、并行窗口、按类型的 settle 探测器与分档、服务型 alive-detached。
- `task-registry-and-board`: TaskManager 内存 registry、幂等去重、live 任务看板投影(放置/不压缩/age-out)、LLM 任务工具、自包含 settle 事件。

### Modified Capabilities

- `subagent-turn-execution`: 子 agent 调用纳入 task 层,可异步执行(settle = RunFlow 返回);分阶段启用。
- `persistent-event-loop`: `task_settled` 成为一等事件,空闲时也能唤醒 loop 触发回收 turn。

## Impact

- **新增**:`agent/task_manager.go`(TaskManager + registry)、settle-detector 抽象;`ContextManager.BeforeModel` 注入看板;task 相关 LLM 工具。
- **改造**:`tool/action`(ActionTool 接入 task 层,替换当前阻塞等待)、`agent/tool_agent.go`(AgentToolWrapper 异步,Phase 后置)、`agent/context_manager.go`(RunFlow / 看板 / task_settled)、`agent/event_loop.go`(task_settled 唤醒)。
- **配置**:`sync_wait`(全局/按工具)、看板开关。
- **BREAKING**:ActionTool 的同步阻塞语义改为 sync-wait 窗口;`unified-event-consumer-and-async-tool` 中的 async-tool 部分被本模型取代。
- **测试**:确定性单测(sync-wait 内联 vs 超窗、并行窗口、settle 分档、看板渲染/age-out、task_settled 触发 turn)+ 真实 LLM 端到端(长命令异步 + 回收 turn LLM 友好)。

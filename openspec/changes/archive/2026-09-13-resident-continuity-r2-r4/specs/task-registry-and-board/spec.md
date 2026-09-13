## MODIFIED Requirements

### Requirement: TaskManager 内存 registry 与幂等 spawn

系统 SHALL 提供一个确定性的 TaskManager 组件(非 LLM agent),维护 registry,记录每个 Task 的 `{id, desc, status, spawn-spec, resultRef, startedAt, declarative}`。status SHALL 覆盖 `running` / `stable` / `alive-detached` / `completed` / `failed` / `suspect` / `dead` / `cancelled`。

spawn SHALL 幂等:当一个语义等价的任务已在运行时,再次 spawn SHALL 返回既有 Task 句柄而非新建,以防止 LLM 重复发起。

**registry=事实链 fold（本变更反转原「纯内存禁持久化」）**：spawn SHALL 伴随一条 `task_spawned` 正 key 事件入事实链（载声明式 spec：Kind/Desc/Key/Command/AgentName/MessageBody/EventKeys/Origin/TaskID/Params/StartedAtUnixMilli）；**窗口内 inline settle（最常见形态）SHALL 也发 settle 终态记录**（既有 external_input+subtype 形态，不新增 settle 事件类型）；settle 事件 SHALL 携带结构化 `Metadata[task_id]`（全量 UUID）与 `settle_status`（不解析正文）。运行期 registry 为内存维护；重启 SHALL经 `RebuildTaskRegistry` 纯全量回放重建 active 态（无 compaction snapshot）。事件写入 best-effort（失败仅 ERROR 日志——**残余缺口**：spawn 落库失败则重启丢该任务）。

**TaskSpec 声明式化（按 Kind 拆分承诺）**：`RebuildClosures(declarative)` 工厂承诺表——command：Relaunch/Resume/Alive 全可用（Resume 跨重启由 resident-session-continuity 重挂供能）；subagent：Relaunch 可用（AgentName+MessageBody+EventKeys 重投递）、**跨重启 Resume SHALL 返回「请 relaunch」引导文案**（rounds 轮次链为无事件源的运行期态，不承诺）；generic：仅展示。Params SHALL 枚举 ActionArgs 全 spawn 字段集（含 Timeout/ProbeFailures；未知字段拒绝）。

**TaskManager org 级单例**：TaskManager SHALL 在 build 路径构造一次（org 级、跨执行器代共享），注入各 cm/taskController/meditationMgr/ActionTool；SHALL NOT 随执行器换代重建（状态⊥执行器不变量）。

#### Scenario: 幂等 spawn 去重

- **WHEN** 一个语义等价的任务已处于 running,LLM 再次发起同一任务
- **THEN** TaskManager SHALL 返回既有 Task,SHALL NOT 新建重复任务

#### Scenario: 死任务保留 spec 以便进程内 relaunch

- **WHEN** 一个任务转为 `dead` / `suspect`
- **THEN** 其记录 SHALL 保留 spawn-spec
- **AND** LLM SHALL 可通过 `relaunch(id)` 用原 spec 在**同一进程内**重新发起

#### Scenario: spawn 伴随事实链事件（含 StartedAt）

- **WHEN** 一个任务 spawn（含跨重启重建后的新 spawn）
- **THEN** SHALL StoreEvent 一条 `task_spawned` 正 key 事件，载完整 Declarative（含 StartedAtUnixMilli）
- **AND** 运行期内存 registry 照旧即时可用（best-effort，写失败仅 ERROR 日志——重启丢该任务为已文档化残余缺口）

#### Scenario: inline settle 也有终态事件

- **GIVEN** 一个任务在 dense 窗口内（默认 10s）完成结算
- **WHEN** Spawn 返回内联结果
- **THEN** SHALL 也发一条 settle 终态记录（既有 external_input+subtype 形态，含 task_id/settle_status 结构化 Metadata；不新增 settle 事件类型）→ 重启回放不重建为幽灵 suspect

#### Scenario: subagent 跨重启 resume 引导 relaunch

- **GIVEN** 跨重启重建的 subagent 任务，LLM 调用 resume
- **THEN** SHALL 返回「请 relaunch」引导文案（rounds 无事件源不承诺），进程内 resume 不受影响

#### Scenario: 重启回放重建 active 态

- **GIVEN** 重启前 registry 有 2 个 running、1 个 alive-detached、1 个 completed（含 inline 结算）任务，事实链含对应 spawned/settled 事件
- **WHEN** 冷启动 `RebuildTaskRegistry`
- **THEN** 重建后 active 任务=3（running×2→suspect+alive-detached×1），completed（含 inline-settled）不重建

#### Scenario: running 任务跨重启重建为 suspect（tmux 存活待裁决）

- **GIVEN** 一个 running 任务的 tmux 会话跨重启存活状态未知
- **WHEN** registry 重建
- **THEN** SHALL 重建为 `suspect`（进程内 watch goroutine 不可恢复），由存活探测（resident-session-continuity 能力）裁决：会话活→恢复跟踪，会话死→走 settle 补偿
- **AND** 看板对 suspect 的 ⚠ 提示语义不变

#### Scenario: 换执行器不丢任务板

- **WHEN** R4（swappable-executor）结构热更换入新执行器
- **THEN** TaskManager 及其 registry SHALL 原封不动（持有者在执行器之外），新执行器内的工具经注入接口继续操作同一 registry

## ADDED Requirements

### Requirement: 看板渲染数据源跨重启连续

任务看板（每 turn 重渲染的 live 快照，不持久不压缩——语义不变）SHALL 在重启后从重建的 registry 渲染出同样的 active 任务集（id/desc/status/年龄语义一致），使 LLM 跨重启感知进行中后台任务不间断。

#### Scenario: 重启后看板连续

- **GIVEN** 重启前看板显示 2 个 running 任务
- **WHEN** 重启完成后的首个 BeforeModel
- **THEN** 看板 SHALL 渲染出同 2 个任务（status 经存活探测裁决后为 running 或 suspect），SHALL NOT 为空

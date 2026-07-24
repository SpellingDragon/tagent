## ADDED Requirements

### Requirement: TaskManager 内存 registry 与幂等 spawn

系统 SHALL 提供一个确定性的 TaskManager 组件(非 LLM agent),维护一个**内存 registry**,记录每个 Task 的 `{id, desc, status, spawn-spec, resultRef, startedAt}`。status SHALL 覆盖 `running` / `stable` / `alive-detached` / `completed` / `failed` / `suspect` / `dead` / `cancelled`。

spawn SHALL 幂等:当一个语义等价的任务已在运行时,再次 spawn SHALL 返回既有 Task 句柄而非新建,以防止 LLM 重复发起。

registry SHALL 为纯内存(ephemeral),SHALL NOT 跨进程重启持久化。

#### Scenario: 幂等 spawn 去重

- **WHEN** 一个语义等价的任务已处于 running,LLM 再次发起同一任务
- **THEN** TaskManager SHALL 返回既有 Task,SHALL NOT 新建重复任务

#### Scenario: 死任务保留 spec 以便进程内 relaunch

- **WHEN** 一个任务转为 `dead` / `suspect`
- **THEN** 其记录 SHALL 保留 spawn-spec
- **AND** LLM SHALL 可通过 `relaunch(id)` 用原 spec 在**同一进程内**重新发起

### Requirement: live 任务看板投影

系统 SHALL 在每个 turn 的 `BeforeModel` 阶段从 TaskManager registry **重新渲染**一个紧凑的任务看板注入上下文。看板 SHALL 放置在**最后一个 agent_output 之后、当前输入之前**(recency 锚点)。

任务看板 SHALL NOT 参与上下文压缩——它是 live 状态快照而非历史事件。已被 LLM 处理(acknowledged)的 settled 任务 SHALL 在短 TTL 后从看板 age-out,以保持看板有界。

#### Scenario: 看板每 turn 重新渲染且不被压缩

- **WHEN** 上下文历史被压缩
- **THEN** 任务看板 SHALL 仍由 registry 重新渲染出当前活跃任务
- **AND** 看板内容 SHALL NOT 被压缩器改写或丢弃

#### Scenario: 看板位于 recency 锚点

- **WHEN** 构建发给 LLM 的消息序列
- **THEN** 任务看板 SHALL 位于最后一个 agent_output 之后、当前输入(或 settle 事件)之前

#### Scenario: 已处理任务 age-out

- **WHEN** 一个 settled 任务已被 LLM 在回收 turn 中处理并超过 age-out TTL
- **THEN** 该任务 SHALL 从活跃看板移除

### Requirement: 自包含的 task_settled 事件

当一个后台 Task settle 时,TaskManager SHALL 发出一个**自包含**的 `task_settled` 事件,携带 `task_id`、任务 `desc`、settle `kind` 与结果摘要或 `event_key` ref。该事件 SHALL 不依赖原始 spawn 的 tool_call 仍存在于历史中即可被 LLM 正确关联(与压缩解耦)。

大结果 SHALL 按 `event_key` ref 回写,看板/事件仅放摘要,LLM SHALL 可按需拉取全量。

#### Scenario: 压缩后仍可关联结果

- **WHEN** 原始 spawn 的 tool_call/thinking 已被压缩丢弃,而任务随后 settle
- **THEN** `task_settled` 事件 SHALL 凭自身携带的 desc + 结果使 LLM 正确关联,无需原始 tool_call 存在

### Requirement: LLM 任务操作工具

系统 SHALL 向 agent 暴露任务操作工具:`list_tasks`(列出活跃/近期任务)、`get_task_result(id)`(拉取全量结果)、`cancel(id)`、`relaunch(id)`。这些工具 SHALL 是即时返回的同步工具(不进入 sync-wait 窗口)。

#### Scenario: LLM 拉取大结果全量

- **WHEN** 一个 settle 事件只携带结果摘要 + ref
- **THEN** LLM SHALL 可通过 `get_task_result(id)` 获取全量结果

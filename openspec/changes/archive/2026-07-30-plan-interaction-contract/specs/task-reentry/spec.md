# task-reentry Delta

## ADDED Requirements

### Requirement: plan 计划生命周期经 resume_task 续行

顶层 agent 与 plan 的计划生命周期交互 SHALL 遵循"一个 change ≙ 一个 task id"：`create` 以新工具调用发起并记住返回的 task id 与计划名；create settle 之后的 `update` / `progress` / `archive` SHALL 经 `resume_task(task_id, ...)` 续行（任务链还原器自动注入前序轮次上下文），SHALL NOT 为同一计划的后续操作发起新的并发工具调用。create 尚未 settle（任务 running）期间，同一计划的后续操作 SHALL 被任务层"轮次在飞"错误拒绝——拿到 ACK 不等于计划已建立。`update` 报账 SHALL 携带产物证据（路径或结论摘要），供归档审计核对。

超出终态保留期（`task_terminal_ttl`）后任务被 prune、resume 不可达时，调用方 SHALL 改为携带 `name` 的新调用继续该计划的生命周期。

#### Scenario: create settle 后经 resume 续行

- **WHEN** plan 的 create 任务 settle 返回计划（name + task id）后，父 agent 需要更新进度
- **THEN** 父 agent SHALL 调用 `resume_task(task_id, "update name=X: 步骤N完成, 产物=...")`
- **AND** 新 Run 的任务链上下文 SHALL 含上轮计划结论，plan 无需重新定位 change

#### Scenario: create 未 settle 时 update 被拒

- **WHEN** create 任务仍在 running（仅拿到 ACK）时父 agent 尝试对其 resume
- **THEN** 任务层 SHALL 返回"轮次在飞"错误并引导等待 settle 后重试

#### Scenario: 重入窗口过期后按 name 续行

- **WHEN** 计划任务已被终态 prune（超出 task_terminal_ttl）
- **THEN** `resume_task` SHALL 返回任务不存在的明确错误
- **AND** 父 agent SHALL 以携带 `name` 的新 plan 调用继续该计划的生命周期

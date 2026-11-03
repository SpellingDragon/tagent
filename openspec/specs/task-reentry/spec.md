# task-reentry Specification

## Purpose

对 task 的重入经唯一通信工具 `send{target:{task}, content}` 表达；settled 可重激活（修改状态、新 Round 续行），failed/cancelled 具名拒绝。

## Requirements
### Requirement: 特异出入口

resume 的执行 SHALL 按任务类型分派:

- tmux:detector 绑定会话而非轮次——resume 仅 Rearm（记录 capture 基线+重开 dense 窗口）并 `SendKeys` 注入存活会话,监控回调与任务 watch SHALL NOT 换手;IsTUI 会话 SHALL 拒绝 resume;同会话并发 resume 由任务层占坑单胜拒绝后到者（提示重试）
- subagent:新 Run + 任务链还原器自动注入 external_context;子 agent 保持单 turn 原语,无进程复活

#### Scenario: TUI 会话拒绝注入

- **WHEN** 对 IsTUI 的会话 resume
- **THEN** SHALL 返回错误（避免 send-keys 破坏画面）,不改变任务状态

#### Scenario: subagent 重入引用上次结论

- **WHEN** plan 任务 settle 后被 resume 追加指令
- **THEN** 新 Run 的 external_context SHALL 含上次 settle 结果,使其能直接引用上次结论继续工作

#### Scenario: 并发 resume 单胜

- **WHEN** 并行工具执行中对同一任务并发 resume
- **THEN** 恰好一个 resume 获得轮次（ResumeFn 恰执行一次）,后到者 SHALL 获 running 态错误提示重试

#### Scenario: 陈旧信号不串轮

- **WHEN** resume 已换绑新 detector 后,旧 detector 的存量信号到达
- **THEN** 旧信号 SHALL NOT 影响新轮次的状态与结算（旧 watch 已退役）

### Requirement: plan 计划生命周期经重入续行

顶层 agent 与 plan 的计划生命周期交互 SHALL 遵循"一个 change ≙ 一个 task id"：`create` 以新工具调用发起并记住返回的 task id 与计划名；create settle 之后的 `update` / `progress` / `archive` SHALL 经 `send{target:{task}, content}` 续行（任务链还原器自动注入前序轮次上下文），SHALL NOT 为同一计划的后续操作发起新的并发工具调用。create 尚未 settle（任务 running）期间，同一计划的后续操作 SHALL 被任务层"轮次在飞"错误拒绝——拿到 ACK 不等于计划已建立。`update` 报账 SHALL 携带产物证据（路径或结论摘要），供归档审计核对。

超出终态保留期（`task_terminal_ttl`）后任务被 prune、重入不可达时，调用方 SHALL 改为携带 `name` 的新调用继续该计划的生命周期。

#### Scenario: create settle 后经 resume 续行

- **WHEN** plan 的 create 任务 settle 返回计划（name + task id）后，父 agent 需要更新进度
- **THEN** 父 agent SHALL 调用 `send{target:{task: <task_id>}, content: "update name=X: 步骤N完成, 产物=..."}`
- **AND** 新 Run 的任务链上下文 SHALL 含上轮计划结论，plan 无需重新定位 change

#### Scenario: create 未 settle 时 update 被拒

- **WHEN** create 任务仍在 running（仅拿到 ACK）时父 agent 尝试对其 resume
- **THEN** 任务层 SHALL 返回"轮次在飞"错误并引导等待 settle 后重试

#### Scenario: 重入窗口过期后按 name 续行

- **WHEN** 计划任务已被终态 prune（超出 task_terminal_ttl）
- **THEN** `send` SHALL 返回任务不存在的明确错误
- **AND** 父 agent SHALL 以携带 `name` 的新 plan 调用继续该计划的生命周期

### Requirement: 重建任务的 resume nil 安全

TaskManager 对经 RestoreTask 重建（detector/watchDone/firstSettle 未初始化）的任务执行 Resume 时 SHALL NOT panic：重建任务 SHALL 在注册时即持有可用的 watchDone 与 firstSettle 通道；Resume 的 watch 换代判定 SHALL 将 nil detector 视为「必换新 watch」而非依赖 close 旧通道；Spawn 与 Resume 的 select 分支对 nil detector SHALL 只等待 firstSettle（纯同步语义）。修复 MUST 附 fail-before 回归测试（重建任务 + resume 先证 panic 再证修复）。

#### Scenario: 冷启动重建后的任务被 resume

- **WHEN** 进程重启后 RestoreTask 重建历史任务（detector=nil）且模型经 `send{target:{task}}` 触发 Resume
- **THEN** Resume 正常换装新 watch 并返回 SpawnResult，进程不出现 nil channel close 或 nil 接口调用的 panic

### Requirement: nil 接口模式级审计留痕

对全库「close(通道) / 接口方法调用」中依赖调用方非 nil 口头契约的点 SHALL 完成一次模式级审计，清单（含 file:line 与处置）SHALL 记录于 LEDGER 台账；审计发现的同型裂缝 MUST 逐个修复或显式豁免（豁免须写明理由）。

#### Scenario: 审计发现同型裸调点

- **WHEN** 审计在某调用点发现对可能为 nil 的 detector/channel 的裸操作
- **THEN** 该点获得守卫修复并附回归测试，或以注释与 LEDGER 条目显式豁免（含不可达论证）

### Requirement: 重入经 send 表达

task 重入 SHALL 经 `send{target:{task}, content}` 提供；`resume_task` MUST NOT 出现在模型工具面。重入产生的 ACK 与 settle 事件 SHALL 携带同一 task id。

#### Scenario: 工具面无 resume_task

- **WHEN** 读取模型工具面
- **THEN** 无 `resume_task`；重入语义由 `send` 承载

### Requirement: settled 重激活

settled task SHALL 可被重激活：状态 settled→活跃（修改状态），以新 Round 续行，可再次 settle。重激活 SHALL 留审计事实（settled→reactivated）。新 Round 的 settle 通知 SHALL 为新身份（TaskID+Round+Kind+Seq 不与旧 Round 合并），SHALL 恰好一次驱动发起方。

#### Scenario: settled 被重激活续行

- **WHEN** 对 settled 的 subagent task 调 `send{target:{task}}`
- **THEN** 状态改为活跃、新 Round 执行注入输入，完成后再次 settle 且通知为新 Round 身份

#### Scenario: 翻转可追溯

- **WHEN** 任一重激活发生
- **THEN** 持久事实含 settled→reactivated 审计记录

### Requirement: 合法源状态收敛为仅 settled

存活类（轮次不在飞）SHALL 接受注入续行；settled SHALL 重激活；**failed/cancelled SHALL 具名拒绝**（`[send_denied]`，引导 relaunch 或新调用）；running/suspect（轮次在飞）SHALL 拒绝并引导。并发重入 SHALL 占坑单胜；陈旧信号 MUST NOT 串入新轮次。

#### Scenario: failed 拒绝重入

- **WHEN** 对 failed task 调 `send{target:{task}}`
- **THEN** `[send_denied]` 具名拒绝，状态不变，引导另起新 task

#### Scenario: 并发重入单胜

- **WHEN** 并发对同一 task 重入
- **THEN** 恰好一个获得轮次，后到者获"轮次在飞"错误

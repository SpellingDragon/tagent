## ADDED Requirements

### Requirement: resume_task 重入原语

任务层 SHALL 提供 `resume_task(id, input)` 工具:对可重入任务继续输入,生命周期完全复用 spawn 的 dense→内联/ACK→settle;resume 产生的 ACK 与 settle 事件 SHALL 携带同一 task id。合法源状态:存活类（alive-detached / stable,tmux 会话仍活）与完成态（completed / failed,round 型执行器如 subagent 以新 Run 续行）。running / suspect（轮次在飞）与 cancelled（会话已杀）的 resume SHALL 返回明确错误并引导（等待重试 / relaunch 或新调用）。并发 resume SHALL 单胜（占坑式状态转移,后到者获 running 态错误）。

#### Scenario: 服务会话重入取增量输出

- **WHEN** 对 alive-detached 的 tmux 服务任务 resume 一条命令
- **THEN** dense 窗口内结算则内联返回本次命令的增量输出;超窗则返回 ACK,结算后以 task_settled 通知回写
- **AND** 输出 SHALL 为 resume 时刻基线之后的增量（非全屏历史）

#### Scenario: 非法状态 resume 被拒绝

- **WHEN** 对 running 或 terminal 状态的任务 resume
- **THEN** SHALL 返回明确错误说明当前状态与可行动作

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

#### Scenario: 完成态 subagent 任务可重入

- **WHEN** subagent 任务处于 completed/failed 终态（其 detector 只发 completed,不会到达 alive-detached）
- **THEN** resume SHALL 允许并以新 Run + 任务链还原器续行（终态对 round 型执行器是自然续行点,而非拒绝理由）

#### Scenario: 并发 resume 单胜

- **WHEN** 并行工具执行中对同一任务并发 resume
- **THEN** 恰好一个 resume 获得轮次（ResumeFn 恰执行一次）,后到者 SHALL 获 running 态错误提示重试

#### Scenario: 陈旧信号不串轮

- **WHEN** resume 已换绑新 detector 后,旧 detector 的存量信号到达
- **THEN** 旧信号 SHALL NOT 影响新轮次的状态与结算（旧 watch 已退役）

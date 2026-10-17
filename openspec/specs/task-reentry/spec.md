# task-reentry Specification

## Purpose

本规范定义 resume_task 重入原语:统一生命周期复用 spawn 的 dense→ACK→settle,tmux/subagent 特异出入口,并发占坑单胜,陈旧信号不串轮。
## Requirements
### Requirement: resume_task 重入原语

resume_task SHALL 在同task id下认领新intent并复用dense→ACK→结果路径。alive_detached/stable/completed/failed可尝试续行；running/suspect及并发认领者 SHALL 被拒绝，cancelled SHALL 引导relaunch。完成态仅表示可尝试新轮，不保证已死native资源可重新输入；执行器失败 SHALL 明确记录而不伪造成功。

#### Scenario: 已完成subagent显式续行
- **WHEN** subagent完成且仍在terminalTTL重入窗口内
- **THEN** Resume以新intent和持久历史创建新Run，保持task id

#### Scenario: cancelled不能重入
- **WHEN** task已cancelled
- **THEN** 返回明确错误并引导新资源relaunch，不调用ResumeFn

### Requirement: 特异出入口

命令 Resume SHALL 在原资源identity上准备输入，冻结精确字节输出基线、认领新的单调intent round，再在资源绑定提交后注入输入；TUI仍 SHALL 拒绝注入。旧/new detector wrapper 可共享底层resource，但观察者 SHALL 按producer round换代，MUST NOT 以wrapper不同为由杀死复用资源。发送失败 SHALL 记round_aborted并保留旧actual producer。

subagent Resume SHALL 新建一次显式委派Run，在可见执行面解析目标，并从持久task_round begin/finish历史恢复前序输入、结果、错误和正文票据；MUST NOT 重跑未完成逻辑目标或靠运行期map保存跨重启上下文。

#### Scenario: 原末行prompt不冒充新输入
- **WHEN** 同一行交互会话被resume
- **THEN** 只有发送之后的字节增量可满足input pattern，旧末行不能使新轮提前就绪

#### Scenario: 子任务跨重启显式续行
- **WHEN** 已完成subagent任务经显式resume追加输入
- **THEN** 新Run获得持久轮次历史，沿选定执行面解析目标并保留同task id

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

### Requirement: 重建任务的 resume nil 安全

TaskManager 对经 RestoreTask 重建（detector/watchDone/firstSettle 未初始化）的任务执行 Resume 时 SHALL NOT panic：重建任务 SHALL 在注册时即持有可用的 watchDone 与 firstSettle 通道；Resume 的 watch 换代判定 SHALL 将 nil detector 视为「必换新 watch」而非依赖 close 旧通道；Spawn 与 Resume 的 select 分支对 nil detector SHALL 只等待 firstSettle（纯同步语义）。修复 MUST 附 fail-before 回归测试（重建任务 + resume 先证 panic 再证修复）。

#### Scenario: 冷启动重建后的任务被 resume

- **WHEN** 进程重启后 RestoreTask 重建历史任务（detector=nil）且模型调用 resume_task 触发 Resume
- **THEN** Resume 正常换装新 watch 并返回 SpawnResult，进程不出现 nil channel close 或 nil 接口调用的 panic

### Requirement: nil 接口模式级审计留痕

对全库「close(通道) / 接口方法调用」中依赖调用方非 nil 口头契约的点 SHALL 完成一次模式级审计，清单（含 file:line 与处置）SHALL 记录于 LEDGER 台账；审计发现的同型裂缝 MUST 逐个修复或显式豁免（豁免须写明理由）。

#### Scenario: 审计发现同型裸调点

- **WHEN** 审计在某调用点发现对可能为 nil 的 detector/channel 的裸操作
- **THEN** 该点获得守卫修复并附回归测试，或以注释与 LEDGER 条目显式豁免（含不可达论证）

### Requirement: 意图轮与实际 producer 轮分离

每次被受理的Resume尝试 SHALL 消耗一个新intent round并保存本次owner。actual producer round/owner/seq与TTL SHALL 独立保存；失败意图 MUST NOT 复用编号、覆盖旧producer、清掉其等待或续命。round_aborted SHALL 作为内部事实提交而不伪装为旧producer终局。新生产绑定和候选TTL SHALL 先于实际Start持久化，拒绝时回滚候选资源。

#### Scenario: 失败后再次resume
- **WHEN** round 2受理后发送失败，而round 1仍在原会话中
- **THEN** 实际producer保持round 1；下一次尝试使用round 3，不复用round 2

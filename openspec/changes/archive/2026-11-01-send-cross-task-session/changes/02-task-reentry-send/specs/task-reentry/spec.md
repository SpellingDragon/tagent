# task-reentry Specification（delta：重入原语归并进 send + 合法源状态收敛）

> 本文件为 MODIFIED delta。原条款 `resume_task(id, input)` 工具与"完成态（completed/**failed**）可重入"由本 delta 翻转（用户裁定 2026-11-01，design D7）。

## Purpose

对 task 的重入经唯一通信工具 `send{target:{task}, content}` 表达；settled 可重激活（修改状态、新 Round 续行），failed/cancelled 具名拒绝。

## MODIFIED Requirements

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

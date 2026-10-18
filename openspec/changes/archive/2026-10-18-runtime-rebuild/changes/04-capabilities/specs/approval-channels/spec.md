# approval-channels Specification

## Purpose

本 delta 在既有审批消息流上明确区分**等待（Hold）**与最终 **Denied**：审批等待是状态，不是拒绝；拒绝才不执行工具并产生原生配对具名结果。

## ADDED Requirements

### Requirement: Hold 与 Denied 分离

异步审批的**等待（Hold）** SHALL 建模为"尚未执行的任务 + 明确等待状态"，MUST NOT 伪装为最终 **Denied**；仅当明确拒绝（Denied）时，系统 SHALL 不执行工具、并产生一条原生配对的**具名结果**供模型自纠。审批请求 SHALL 仍经 ApprovalManager 落盘（digest 绑定不变），通道投递失败 MUST NOT 阻塞审批门（闸不是墙）。

#### Scenario: Hold 记录为等待而非拒绝

- **WHEN** 一次工具调用进入审批等待
- **THEN** 返回明确等待状态并登记为尚未执行的任务
- **AND** 结果中不出现最终 Denied 语义

#### Scenario: Denied 产生原生配对具名结果（失败）

- **WHEN** 审批被明确拒绝
- **THEN** 工具零执行，返回一条原生配对的具名结果
- **AND** 模型下一轮可据此自纠

#### Scenario: 重复回应幂等（并发）

- **WHEN** 同一 digest 被重复回应 approve/reject
- **THEN** 首次生效（responded→consumed），后续为幂等无副作用

#### Scenario: 通道投递失败不阻塞门（失败）

- **WHEN** ApprovalChannel 投递失败
- **THEN** 审批门不被阻塞（闸不是墙），请求仍可经 CLI/微信其它入口回应

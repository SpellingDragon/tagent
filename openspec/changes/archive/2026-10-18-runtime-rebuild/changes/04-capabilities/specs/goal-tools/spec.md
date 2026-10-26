# goal-tools Specification

## Purpose

本 delta 要求 goal 状态语义**先核实后沿用**：状态词与旧语义以现场保留业务语义为准，不因重建而偷改；goal 声明激活治理门并写治理事件，重启经回放重建。

## ADDED Requirements

### Requirement: goal 旧状态语义核实后沿用

goal 工具族（`goal_declare`/`goal_list`/`goal_resolve`/`denial_query`/`approval_list`，仅 entry agent 可挂载）SHALL 保持其**当前保留的业务语义与状态词**；重建 MUST NOT 未经核实改动状态词或语义。goal 声明 SHALL 激活治理 `goal` 门并写 governance 事件；GoalRegistry 启动时 SHALL 从 governance 事件回放重建。

#### Scenario: 声明激活 goal 门

- **WHEN** entry agent 调 `goal_declare(text)`
- **THEN** GoalRegistry 登记并写 governance 事件（subtype=goal_declared）
- **AND** 此后 `GoalRequiredFor` 关键工具被拒时渗透当前 goal 上下文

#### Scenario: 语义核实前不落地（失败）

- **WHEN** 旧状态词/语义尚未经现场核实
- **THEN** 相关任务保持 `design-blocked`，MUST NOT 以新命名替换旧状态词

#### Scenario: 重启不丢 goal（重启）

- **WHEN** 声明 goal 后进程重启
- **THEN** goal 门上下文经 governance 事件回放恢复
- **AND** denial 渗透仍携带 goal

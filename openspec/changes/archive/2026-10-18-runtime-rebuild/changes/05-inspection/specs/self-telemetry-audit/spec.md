# self-telemetry-audit Specification

## Purpose

本 delta 明确自管遥测审计**只在观测面**：占比判定与分级动作不改运行状态、不拥有状态；冻结动作经既有 spawnGate 判定链。

## ADDED Requirements

### Requirement: 审计只在观测面、不改状态

自管遥测占比判定与分级动作（L1 告警 / L2 收敛 / L3 冻结）SHALL 为确定性工程计算（零 LLM），仅在观测面生效；其指标与动作写入事实链供 Query 审计；MUST NOT 因审计而改写业务状态或回滚事实。

#### Scenario: 占比指标可查询

- **WHEN** 滚动窗口内自管遥测占比超过阈值
- **THEN** 先触发 L1 告警与 L2 收敛；Query 可读到该指标与动作记录
- **AND** 已提交业务事实不受影响

#### Scenario: L3 复用 spawnGate（失败）

- **WHEN** 收敛无效后触发 L3 冻结
- **THEN** 复用与 disk block spawn 同一判定链、进行中任务不受影响、新 spawn 被具名拒绝
- **AND** 占比回落至阈值一半以下自动解除

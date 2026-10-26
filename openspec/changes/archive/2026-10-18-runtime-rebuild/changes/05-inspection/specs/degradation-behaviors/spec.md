# degradation-behaviors Specification

## Purpose

本 delta 要求退化/阻塞在 Query 读面上**可解释**：退化状态与其对执行的影响可查询，且给出下一推进者。

## ADDED Requirements

### Requirement: 退化状态与阻塞可解释

退化行为（model 退避 / mcp 熔断半开 / disk 禁 spawn）SHALL 在 Query 读面以具名状态可查询；退化造成的拒收 SHALL 产生可查询记录，并给出**下一推进者**（恢复探测/退避结束等）；每项退化默认关闭（零行为变化）。

#### Scenario: 退器具名可查

- **WHEN** DepMCP degraded 且 mcp_call 被熔断快失败
- **THEN** Query 可读到该退化状态与熔断拒收记录
- **AND** 给出下一推进者（半开探测）

#### Scenario: 恢复自动回正常（重启）

- **WHEN** 依赖恢复上报
- **THEN** 退化状态清除，Query 反映正常路径
- **AND** 不残留拒收记录为当前状态

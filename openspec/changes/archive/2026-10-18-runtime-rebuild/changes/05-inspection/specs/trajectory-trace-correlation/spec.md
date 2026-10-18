# trajectory-trace-correlation Specification

## Purpose

本 delta 要求轨迹与 trace 的双向关联在 Query 读面上**可解释**：从运行总图可定位轨迹/trace，且 noop 时字段省略、向后兼容。

## ADDED Requirements

### Requirement: 关联字段在 Query 读面可解释

轨迹记录携带的 `trace_id`/`span_id` 与 turn span 携带的轨迹定位属性 SHALL 在 Query 只读面上可查询与解释；OTel 未启用（noop）时关联字段 SHALL 省略且不影响 Query 结果。

#### Scenario: 启用导出时可跳转

- **WHEN** OTLP 启用且轨迹记录开启
- **THEN** 轨迹记录含非空 `trace_id`/`span_id`
- **AND** Query 可据此解释该次调用与 span 的关系

#### Scenario: noop 时兼容（失败）

- **WHEN** 未设 OTLP endpoint
- **THEN** 轨迹记录不含 trace 字段（omitempty 省略）
- **AND** Query 结果与无观测时一致

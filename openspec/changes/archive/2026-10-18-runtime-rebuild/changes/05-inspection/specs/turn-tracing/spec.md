# turn-tracing Specification

## Purpose

本 delta 明确：OTel 为**旁路观测**，关闭 OTel 时 Query 能力不减；观测埋点不触碰工具声明、不拥有状态。

## MODIFIED Requirements

### Requirement: noop 降级零影响

未配置 OTLP 导出（未设 `OTEL_EXPORTER_OTLP_ENDPOINT`）时，全部 span/metric 操作 MUST 为 noop：零导出、零可观测开销、事件循环/工具执行/轨迹记录行为与无观测时一致。span 埋点 MUST NOT 触碰任何工具的 Declaration。**关闭 OTel MUST NOT 减少 Query（只读运行总图）的可用维度或语义**——Query 的可见性不依赖观测导出是否开启。

#### Scenario: 默认态零行为变化

- **WHEN** 未设 OTLP endpoint 运行完整集成测试套件
- **THEN** 全部测试通过且与埋点前行为一致（既有断言零修改）

#### Scenario: 声明区零变化

- **WHEN** 对比埋点前后所有内置工具的 Declaration JSON
- **THEN** 完全一致（prefix-cache 不变量）

#### Scenario: 关闭 OTel 仍可查询

- **WHEN** 未设 OTLP endpoint 且执行 Query
- **THEN** 只读运行总图、快照、分页、订阅与阻塞解释均可用
- **AND** 结果与开启 OTel 时相同（观测不参与 Query 语义）

## ADDED Requirements

### Requirement: 观测旁路不拥有状态

观测（span/metric/审计）SHALL 在**事实提交之后**接入；观测者失败 SHALL 记为缺口，MUST NOT 回滚事实或重跑工具。

#### Scenario: 观测失败不回滚

- **WHEN** 某观测导出失败
- **THEN** 已提交事实保持
- **AND** 记录观测缺口，不影响业务结果

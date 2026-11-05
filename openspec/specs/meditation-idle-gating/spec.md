# meditation-idle-gating Specification

## Purpose

已退役能力：冥想触发门控由 `meditation-autonomy` 的状态机规则承担，本规格归档后仅保留退役记录，不再包含或复活旧的独立双闸门条款；旧四条款的迁移去向见归档 delta。
## Requirements
### Requirement: 独立门控能力保持退役

本能力 SHALL 保持退役：冥想触发门控 MUST 由 `meditation-autonomy` 的状态机规则承担；任何实现 MUST NOT 复活本能力的旧四条款或新增对独立门控能力的消费。本规格归档后仅作退役记录，不含活动实现条款。

#### Scenario: 新消费只走状态机

- **WHEN** 任何实现需要判定冥想触发
- **THEN** 它 MUST 使用 `meditation-autonomy` 的状态机规则，MUST NOT 引用本能力已退役的旧四条款


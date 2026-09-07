# consolidation-triggers Specification

## Purpose

记忆巩固触发能力:以建议式(容量路与冥想 hint 路)而非自动执行的方式提示巩固,执行权保留在 LLM 与 memory_consolidate 工具;BuildConsolidationEvent 前置硬门控校验实际源事件数,SNOOZED 状态持久化于 vmeta KV 键,零值配置即禁用、默认行为不变。

## Requirements

### Requirement: 建议式触发（两路）

系统 MUST 提供配置 `memory.engine.consolidation { capacity_threshold, min_source_events, snooze }`（零值=禁用，默认零值行为不变）。容量路：写入旁路计数超阈值→经 EventBus 发 consolidation_hint 消息；冥想 hint 路：meditation digest 附可巩固候选清单。两路均为建议——执行权仍在 LLM + memory_consolidate 工具。

#### Scenario: 容量触发建议
- **WHEN** 分区未巩固事件计数超 capacity_threshold 且未被 SNOOZE
- **THEN** 收到一条 consolidation_hint 渗透消息（非自动执行）；建议被拒绝后进入 snooze 窗口不重复打扰

### Requirement: 硬门控

系统 MUST 在 BuildConsolidationEvent 前置校验 min_source_events（实际取回的源事件数不足即拒绝），SNOOZED 状态记于 `0:vmeta:consolidation_state` KV 键。

#### Scenario: 源不足拒绝
- **WHEN** memory_consolidate 调用时实际取回源事件数 < min_source_events
- **THEN** 返回显式拒绝（不产 consolidation 事件）

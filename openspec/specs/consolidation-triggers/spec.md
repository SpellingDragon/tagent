# consolidation-triggers Specification

> **复核注记（2026-11-04 逐模块核对）**：本规范对已退役符号（`TagentAgent` / `EventBus` / `SessionProjection` / `PlainTool` / `SwappableModel` 等，全仓 Go 0 命中）的引用均为 MUST NOT 禁止项、历史实现注记或迁移期条款，**无现行 SHALL/MUST 建立在已退役符号上**；现行契约以 `assembly` 组合根 + 每地址常驻消费循环 + 装配期按声明分派为准。逐条映射见 `openspec/changes/archive/2026-11-03-runtime-architecture-convergence/evidence/10-doc-gap-register.md`。

## Purpose

记忆巩固触发能力:以建议式(容量路与冥想 hint 路)而非自动执行的方式提示巩固,执行权保留在 LLM 与 memory_consolidate 工具;BuildConsolidationEvent 前置硬门控校验实际源事件数,SNOOZED 状态持久化于 vmeta KV 键,零值配置即禁用、默认行为不变。
## Requirements
### Requirement: 建议式触发（两路）

系统 MUST 提供配置 `memory.engine.consolidation { capacity_threshold, min_source_events, snooze }`（零值=禁用，默认零值行为不变）。容量路：写入旁路计数超阈值→经 EventBus 发 consolidation_hint 消息；冥想 hint 路：meditation digest 附可巩固候选清单。两路均为建议——执行权仍在 LLM + memory_consolidate 工具。容量路注入的 `consolidation_hint` 谱系 SHALL 在 event 包 lineage 单源**显式登记**为自管且非投递（常量 `LineageConsolidationHint`；登记 MUST NOT 仅依赖未登记时 unknown fail-closed 的隐式默认，防止后人将其加入投递白名单而静默破坏 novelty 排除）；该谱系 SHALL 被跨分区 novelty 判据排除（见 meditation-agent-partition——自管谱系不计入新鲜度）。装配侧注入 MUST 引用同一常量（不再复写字面量）。两路防抖语义不变（同窗口至多一次建议）。

#### Scenario: 容量触发建议

- **WHEN** 分区未巩固事件计数超 capacity_threshold 且未被 SNOOZE
- **THEN** 收到一条 consolidation_hint 渗透消息（非自动执行）；建议被拒绝后进入 snooze 窗口不重复打扰

#### Scenario: 显式登记锁死两层语义

- **WHEN** 审查 lineage 单源对 consolidation_hint 的登记
- **THEN** `SelfManagedLineage("consolidation_hint")` 恒真、`DeliverableLineage("consolidation_hint")` 恒假（独立于 unknown fail-closed 默认），且注入侧与判据侧引用同一常量

#### Scenario: 不计入跨分区新鲜度

- **WHEN** 被观察分区内仅有 consolidation_hint 事件（无非自管新事件）
- **THEN** 外部冥想 agent 的 novelty 门保持关闭

### Requirement: 硬门控

系统 MUST 在 BuildConsolidationEvent 前置校验 min_source_events（实际取回的源事件数不足即拒绝），SNOOZED 状态记于 `0:vmeta:consolidation_state` KV 键。

#### Scenario: 源不足拒绝
- **WHEN** memory_consolidate 调用时实际取回源事件数 < min_source_events
- **THEN** 返回显式拒绝（不产 consolidation 事件）


# consolidation-triggers Specification（MODIFIED delta）

## MODIFIED Requirements

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

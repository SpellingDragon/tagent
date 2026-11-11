# meditation-idle-gating Specification（delta，REMOVED 整个能力）

> 用户裁决 r12（ADR 0003）：门＝状态机内部转移规则，不再作为独立主规格对外暴露；对外只保留冥想能力契约的不变量。本 delta 退役该能力的全部四条 requirement，迁移去向＝`meditation-autonomy`（五不变量）＋`features/evolution/meditation` 实现文档（机制条款）。**OpenSpec 不允许空 spec（归档实证 `Spec must have at least one requirement`），故附一条退役记录（ADDED）保证归档可表达；旧四条款仍全部 REMOVED。**

## REMOVED Requirements

### Requirement: 冥想触发采用双闸门判定

**Reason**: 双闸门是状态机的内部转移规则（状态积累＋激活检查），把它冻结为对外契约会让每加一种激活源都变成规格手术；r12 裁决将触发语义整体内化，对外只留"激活即真实事件带因果"与"状态只由外部事件推进"。

**Migration**: 对外的可观察行为改由 `meditation-autonomy` 的「激活即真实事件并携带因果」「状态积累只由外部事件推进」「冥想对外不变量」承接；novelty/节奏/抑制判据的实现细节移入 `features/evolution/meditation` 设计文档与状态机测试。

### Requirement: 门控不依赖输出侧血统追踪

**Reason**: 该条款的历史动机是防"冥想→派生任务→结算→再冥想"的自触发；状态机以"自管产出不计入状态积累、激活即复位"在状态层表达同一防线，不再依赖输出侧血统追踪这一实现级约束。

**Migration**: 防自持由 `meditation-autonomy` 的「自管产出不计入状态积累」承接；血统仅保留为 send 投递侧既有语义（`DeliverableLineage`），不再充当触发门控依据。

### Requirement: 混合批次中丢弃冥想事件

**Reason**: 批次合并是消费循环的通用让路机制，不是冥想独立能力；把它留在此能力会与"冥想是普通回合"的模型冲突。

**Migration**: 让路仍由既有批次合并规则承担（消费循环通用行为，不改）；`meditation-autonomy` 只保留"用户让路"的不变量表述。

### Requirement: 水位执行语义与让位补位

**Reason**: 水位是状态机的内部持久字段；r11/r12 后状态由 `AnchorStore` 扩展保存并可审计，水位不再是对外契约。

**Migration**: 状态持久与迁移记录改由 `features/evolution/meditation` 的状态机实现承担；`meditation-autonomy` 保留"锚点重启安全"的不变量，不暴露水位字段。

## ADDED Requirements

### Requirement: 独立门控能力保持退役

本能力 SHALL 保持退役：冥想触发门控 MUST 由 `meditation-autonomy` 的状态机规则承担；任何实现 MUST NOT 复活本能力的旧四条款或新增对独立门控能力的消费。本规格归档后仅作退役记录，不含活动实现条款。

#### Scenario: 新消费只走状态机

- **WHEN** 任何实现需要判定冥想触发
- **THEN** 它 MUST 使用 `meditation-autonomy` 的状态机规则，MUST NOT 引用本能力已退役的旧四条款

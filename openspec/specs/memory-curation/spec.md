# memory-curation Specification

> **勘误（2026-11-19 契约同步核验）**：本规范原「卡片序列（历史表示的唯一对象）」一条条款及其**四条**场景要求的滚动卡片序列、Compact 时刻工程化提取、可配卡片字符上限与总结模型都已随分层压实与 Compact 世界退役——那批对象在 Go 树零定义，本能力不再以卡片序列承载历史收敛；原条款引用该序列的剩余条款已同批改写为「原文过期按召回窗口判定、不物理销毁」与「冥想自述摘要确定性沉淀」。历史收敛的现行要求不在本能力：段切分、三带折叠与带事件键票据的折叠产物见 `task-skeleton-compression`，按事件键取回原文由 recall 工具承接。

## Purpose

本规范定义记忆策展能力:记忆三原语（store/compress=总结+遗忘/recall）、固化级联与素材律、原文过期的召回窗口判定与固化物随链长存、冥想自述摘要的确定性沉淀。

## Requirements
### Requirement: 记忆三原语与固化级联

内容级总结 SHALL 收归压缩固化时刻,遵循素材律:每一层总结的素材恒为其下层固化物——最底一层直接取段内事件原文,其上依次是段摘要的折叠产物与历史综述。SummaryPlugin SHALL 退位为元数据标注（`event_summary`="原文截断视图",键名与消费端不变）。原文过期 SHALL 由原文仓按其召回窗口判定,窗口外的读取 SHALL 具名拒绝,MUST NOT 以物理销毁达成;固化物（段摘要/历史综述）SHALL 随事件链长存,MUST NOT 被淘汰、覆盖或改写。

#### Scenario: 压缩成本与历史总量无关

- **WHEN** 长会话第 N 次压缩折叠（N>1）
- **THEN** 总结调用的输入规模 SHALL 只与本轮新增素材成正比

#### Scenario: 原文过期不等于原文被销毁

- **WHEN** 一条原文的全部引用都落在其召回窗口之外
- **THEN** 读取 SHALL 具名拒绝（`RecallExpired`／`ErrOriginalExpired`），原文 MUST NOT 被物理销毁
- **AND** 其段摘要与历史综述 SHALL 仍可按事件键取回

### Requirement: 冥想总结以有界自述摘要沉淀

冥想 turn 的总结 SHALL 由同一水位上的确定性有界渲染产出——同一观察面在同一水位渲染出同一摘要——并随该反思事实提交进入事件链;摘要 MUST NOT 依赖滚动卡片序列或超限浓缩整理, MUST NOT 因折叠被改写。

#### Scenario: 冥想结论沉淀

- **WHEN** 冥想总结产出后发生一次压缩折叠
- **THEN** 该反思事实 SHALL 按事件键仍可 recall 取回，摘要正文与提交时逐字一致
- **AND** 折叠 SHALL 只改变其可见性，MUST NOT 改写其字节

### Requirement: 整理触发不改写原始事实

价值评估与整理触发 SHALL 以**因果关联**（引用/父键）关联既有事实；整理 MUST NOT 改写、覆写或迁移原始事件字节与其 owner/format 归属；整理失败 SHALL 工程沉底，不破坏 recall 链。

#### Scenario: 整理不改写原文

- **WHEN** 触发一次记忆整理
- **THEN** 原始事件字节与引用元数据保持
- **AND** 整理产出以新引用/固化物形式存在，可回溯原文

#### Scenario: 折叠后 recall 链不断

- **WHEN** 中带段被折叠为带事件键票据的产物
- **THEN** 该产物 SHALL 携带原事件键，recall SHALL 仍可按该键回补原文

#### Scenario: 整理失败降级不塌（失败）

- **WHEN** 整理调用失败
- **THEN** 主流程不受影响，行为等于沉底
- **AND** 原始事实未被改写

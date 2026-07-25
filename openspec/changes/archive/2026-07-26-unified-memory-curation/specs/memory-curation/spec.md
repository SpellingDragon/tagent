## ADDED Requirements

### Requirement: 记忆三原语与固化级联

内容级总结 SHALL 收归压缩固化时刻,遵循素材律:第 N 层素材恒为第 N-1 层固化物（事件原文 →L3 段摘要 →工程提取卡片行 →超限时浓缩卡片）,唯一例外是第 0→1 层（段内事件原文）。SummaryPlugin SHALL 退位为元数据标注（`event_summary`="原文截断视图",键名与消费端不变）。固化物 TTL 分层:原文按既有 lifecycle TTL 自然遗忘,固化物（段摘要/浓缩卡片）SHALL 豁免 TTL。

#### Scenario: 压缩成本与历史总量无关

- **WHEN** 长会话第 N 次 Compact（N>1）
- **THEN** 总结调用的输入规模 SHALL 只与本轮新增素材成正比

#### Scenario: 原文可忘固化物长存

- **WHEN** lifecycle TTL 清理运行
- **THEN** 过期原文可被清理,段摘要与浓缩卡片 SHALL 保留（索引卡指向恒可达）

### Requirement: 卡片序列（历史表示的唯一对象）

滚动 summaryRef 的内容 SHALL 为:累计计数 + 卡片行序列 + 近期 key 清单（现状有界规则）。卡片行 SHALL 在 Compact 时刻工程化提取（零 LLM）:有 SummaryModel 时取段摘要首行,无 SummaryModel 时取段边界事件摘要首行+任务层元数据（settle 状态/task desc）。序列超过 `card_max_chars`（可配,0=包默认）时 SHALL 触发 LLM 整理（素材=旧卡片行,产出=浓缩卡片,SHALL 保留任务骨架与关键 key 引用）;整理失败或无 SummaryModel 时 SHALL 工程沉底（"更早 n 项(recall 可查)"）。投影 SHALL NOT 引入任何特殊 ref 或例外规则。

#### Scenario: 卡片行零漂移积累

- **WHEN** 多轮 Compact 依次发生且序列未超限
- **THEN** 已有卡片行 SHALL 原样保留（可与 MemoryStore 逐条对账）,新行机械追加

#### Scenario: 超限触发整理且 recall 链不断

- **WHEN** 卡片序列超过 card_max_chars 且 SummaryModel 可用
- **THEN** 旧卡片行 SHALL 被 LLM 浓缩,浓缩卡片 SHALL 含关键固化物 key 引用,recall 仍可按 key 回补

#### Scenario: 降级不塌

- **WHEN** 无 SummaryModel 或整理调用失败
- **THEN** 序列 SHALL 工程沉底,Compact 主流程不受影响,行为等于现状滚动机制

#### Scenario: 早期事实跨多轮压缩可发现

- **WHEN** 会话经历多轮 Compact,首轮含任务 A 完成事实
- **THEN** 模型所见的卡片序列（或浓缩卡片）SHALL 仍含任务 A 的存在性线索与可回补 key

### Requirement: 冥想总结以高亮卡片行沉淀

冥想 turn 的总结 SHALL 在固化时以高亮卡片行（★ 前缀）写入卡片序列（零 LLM 成本）;超限整理时其要点 SHALL 被浓缩保留。

#### Scenario: 冥想结论沉淀

- **WHEN** 冥想总结产出后发生 Compact
- **THEN** 卡片序列 SHALL 含该冥想的高亮行;原冥想事件照常存储/投影

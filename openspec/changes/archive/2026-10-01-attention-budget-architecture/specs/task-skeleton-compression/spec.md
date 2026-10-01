# task-skeleton-compression Delta

## ADDED Requirements

### Requirement: L3 预算升级的未消费遥测豁免

compaction 的 L3 升档路径（buildRetainedRefs 预算升级）MUST 跳过含未消费遥测通知的段——未读事件不得被预算判决静默归档；豁免导致无法达标时 SHALL 如实报告而非强行归档。对话通道的定级表（deterministicLevel）、骨架红线与滚动综述机制保持不变。

#### Scenario: 混合段的豁免判定

- **WHEN** 一个候选 L3 段同时含对话消息与一条未消费 settle
- **THEN** 该段被跳过，压缩继续评估其他段；全部候选被豁免时本轮 compaction 报告未达标原因

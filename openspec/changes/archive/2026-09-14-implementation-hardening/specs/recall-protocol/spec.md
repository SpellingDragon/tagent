## ADDED Requirements

### Requirement: items 批量水合有界

recall 的 items 路径（票据批量精确回补）SHALL 施加条数上限（默认与 engine 路径钳制同源，超限显式截断并在返回中说明丢弃数量）；模型传入超量票据 MUST NOT 无界放大水合成本。

#### Scenario: 模型传入超量票据

- **WHEN** recall(items=[...]) 携带超过上限的票据列表
- **THEN** 仅水合前 N 条（N=上限），返回携带截断说明（含被丢弃计数与建议），不静默

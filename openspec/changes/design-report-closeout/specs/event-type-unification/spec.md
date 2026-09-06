# event-type-unification 规格增量

## ADDED Requirements

### Requirement: feedback 类型注册
系统 MUST 经 EventTypeSpec 注册 `feedback`（见 feedback-binding spec），类型元数据单点声明。

#### Scenario: 注册即生效
- **WHEN** `feedback` 在注册表 init() 注册
- **THEN** 类型元数据全链路一致生效

## MODIFIED Requirements

### Requirement: governance 事件非骨架
governance 治理事件的 EventTypeSpec `Skeleton` 标记 MUST 由 true 改为 **false**——治理记录（否决/批准/goal/退化）不参与骨架压缩，全文保留供审计与 goal 重建回放。

#### Scenario: 治理事件不被骨架化
- **WHEN** 压缩定级处理含 governance 事件的段
- **THEN** governance 事件内容完整保留（不折叠为骨架行），历史审计可全文追溯

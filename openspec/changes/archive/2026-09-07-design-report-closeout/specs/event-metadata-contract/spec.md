# event-metadata-contract 规格增量

## ADDED Requirements

### Requirement: bundle_id 归因键
Attribution 载体 MUST 补 BundleID；RunFlow 组装时读取当前 active bundle（evolution 未启用为空串、不写键）；MemoryPlugin 构造期与 persistBusEvent 双路径盖章；evolution Evidence 归因优先 bundle_id 精确 join，缺章回退 ActivationLog 时间窗。

#### Scenario: 自进化产出可归因到版本
- **WHEN** 某 bundle active 期间产生事件
- **THEN** 事件 Metadata 携带该 bundle_id；guardrail/feedback 聚合可按版本精确分组

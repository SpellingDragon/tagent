## MODIFIED Requirements

### Requirement: Compress 方法签名包含 inv 参数
文档中 Compress 方法的所有引用必须反映当前的三参数签名。

#### Scenario: BeforeModel 调用 Compress
- **WHEN** 读者阅读 §5 ContextIntervention 的 BeforeModel 流程
- **THEN** Compress 调用为 `ci.compressor.Compress(ctx, args.Request.Messages, inv)` 而非两参数版本
- **AND** inv 参数来源于 `inv *agent.Invocation`

### Requirement: Snowflake EventKey 标注为已实现
§12.5 中 Snowflake EventKey 的描述必须反映当前实现状态。

#### Scenario: 状态描述
- **WHEN** 读者阅读 §12.5.4 EventKey Snowflake 设计
- **THEN** 不出现"当前...不含分区信息"或"未来设计"等过时语气
- **AND** 明确标注 Snowflake EventKey 已全面实现
- **AND** 包含位布局图和核心优势说明

### Requirement: Phase 1 事件视图转换有文档描述
文档必须覆盖 ContextIntervention 的 Phase 1 事件视图转换逻辑。

#### Scenario: 描述两个阶段
- **WHEN** 读者阅读 ContextIntervention 章节
- **THEN** 文档区分 Phase 1（事件视图转换: applyEventView/extractEventInfo）和 Phase 2（token 预算 + 多轮压缩）
- **AND** 描述 [evt_xxx|event_type] 前缀的生成逻辑

### Requirement: 文件清单行数准确
文档中所有文件行数引用必须与实际代码一致。

#### Scenario: context_intervention.go 行数
- **WHEN** 读者阅读文件清单表
- **THEN** `context_intervention.go` 行数反映实际值（非 84 行）

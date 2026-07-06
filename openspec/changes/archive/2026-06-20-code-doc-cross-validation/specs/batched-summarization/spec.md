## ADDED Requirements

### Requirement: wiki 记录批量摘要内部函数

wiki 文档 SHALL 记录 SmartCompressor 的以下内部函数及其行为：
- `batchSegmentsByTokenBudget(segments []*TaskSegment, maxTokens int) [][]*TaskSegment`：按 token 预算将 segments 分成多批
- `summarizeBatches(ctx, batches) ([]model.Message, bool)`：并行调用 LLM 生成多批摘要
- `collectCompressedKeys(oldSegments []*TaskSegment) []int64`：收集被压缩事件的 Snowflake key
- `parseEventKeyFromPrefix(content string) int64`：从消息前缀解析 event key
- `findPendingUserMessage(segments []*TaskSegment) *model.Message`：查找未完成的 user 消息

#### Scenario: batchSegmentsByTokenBudget 文档

- **WHEN** 阅读 wiki 中 batchSegmentsByTokenBudget 函数文档
- **THEN** 文档说明输入参数（segments 和 maxTokens）
- **AND** 文档说明输出（二维切片，每个子切片是一批 segments）
- **AND** 文档说明 token 估算逻辑（字符数 / 4）

#### Scenario: summarizeBatches 文档

- **WHEN** 阅读 wiki 中 summarizeBatches 函数文档
- **THEN** 文档说明并行调用 LLM 的行为
- **AND** 文档说明单批失败容错（跳过失败批次，继续其他批次）

#### Scenario: collectCompressedKeys 文档

- **WHEN** 阅读 wiki 中 collectCompressedKeys 函数文档
- **THEN** 文档说明从 oldSegments 中提取 event key 的逻辑
- **AND** 文档说明 key 用于后续压缩事件查询

### Requirement: wiki 记录 WithMaxTokens option

wiki 文档 SHALL 记录 `WithMaxTokens(n int)` option，说明它设置 SmartCompressor 的 maxTokens 字段，影响 token 预算计算（maxInputTokens = maxTokens / 2）。

#### Scenario: WithMaxTokens option 文档

- **WHEN** 阅读 wiki 中 SmartCompressor 选项文档
- **THEN** 文档列出 `WithMaxTokens(n int)` option
- **AND** 文档说明默认值和最小值约束

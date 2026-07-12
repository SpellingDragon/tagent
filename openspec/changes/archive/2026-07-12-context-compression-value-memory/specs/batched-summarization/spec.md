## MODIFIED Requirements

### Requirement: 按 token 预算分批摘要且无预设批次上限

SmartCompressor 的 Stage 2 LLM 摘要 SHALL 按 token 预算分批处理 oldSegments。每批的 token 估算值 SHALL 不超过 maxInputTokens（maxTokens / 2，最小 1000）。每个事件 SHALL 完整保留在批次中（不截断）。系统 SHALL 不预设批次数量上限；批次数量由输入规模和 token 预算自然决定，并受配置的超时/预算控制收敛。

#### Scenario: 多个事件分批摘要

- **WHEN** oldSegments 包含 50 个事件，每个约 200 tokens，maxTokens=8000（maxInputTokens=4000）
- **THEN** 事件被分成约 3 批（每批约 20 个事件，约 4000 tokens）
- **AND** 每批独立调用 LLM 生成摘要
- **AND** 生成 3 条独立的摘要事件

#### Scenario: 单批不超 token 预算

- **WHEN** oldSegments 包含 5 个事件，总 token 约 1000，maxInputTokens=4000
- **THEN** 所有事件放入 1 批
- **AND** 生成 1 条摘要事件

#### Scenario: 事件不截断

- **WHEN** 某个事件的描述文本超过 maxInputTokens
- **THEN** 该事件独占一个批次
- **AND** 事件内容完整保留（不截断）

#### Scenario: 大量事件不被人为上限截断

- **WHEN** oldSegments 包含 200 个事件，maxTokens=8000
- **THEN** 系统 SHALL 生成所有必要批次（例如约 10 批）
- **AND** SHALL NOT 因为固定上限丢弃后续批次
- **AND** 总处理时间 SHALL 受配置超时约束

## ADDED Requirements

### Requirement: 批量评估与摘要合并为单次 LLM 调用

当启用 `value_driven` 时，`SmartCompressor` SHALL 将同一批次内的事件评估（value_score, processing, key_facts）和摘要生成合并为单次 LLM 调用。该调用返回的 JSON/text SHALL 被解析为 `[]EventValue` 和 batch summary 两部分。

#### Scenario: 单次调用同时返回评估和摘要

- **WHEN** 一批包含 5 个 segment 且 `value_driven=true`
- **THEN** LLM SHALL 被调用 1 次
- **AND** 返回结果 SHALL 包含 5 个 `EventValue`
- **AND** 返回结果 SHALL 包含 1 个 batch summary 字符串

#### Scenario: 无 summaryModel 时跳过合并调用

- **WHEN** `summaryModel` 为 nil
- **THEN** SHALL 不执行 LLM 评估/摘要调用
- **AND** SHALL 降级为 rule-based 压缩

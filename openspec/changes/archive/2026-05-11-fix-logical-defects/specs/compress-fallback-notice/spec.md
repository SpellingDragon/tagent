## ADDED Requirements

### Requirement: SmartCompress 在 Stage 2 LLM 失败时提供 fallback notice
当 Stage 2 LLM 摘要模型调用失败或返回空结果时，SmartCompress SHALL 生成一个结构化的 fallback notice 而非空字符串，确保 LLM 知晓有历史对话被压缩省略。

#### Scenario: Stage 2 LLM 调用失败
- **WHEN** `generateSummary` 调用 summaryModel.GenerateContent 返回 error
- **THEN** 返回 `[Compressed: N earlier tasks omitted. Full context available via recall agent.]`
- **AND** N 为被压缩的 task segment 数量

#### Scenario: Stage 2 LLM 返回空摘要
- **WHEN** `generateSummary` 调用 summaryModel.GenerateContent 成功但摘要内容为空
- **THEN** 返回 `[Compressed: N earlier tasks omitted.]`
- **AND** 不包含 recall agent 提示（无提示在空摘要场景下更简洁）

#### Scenario: fallback notice 格式一致性
- **WHEN** 生成的 fallback notice 被注入到消息列表
- **THEN** 格式为 `[Compressed: ...]` 前缀，与现有事件前缀 `[System]`、`[evt_xxx|type]` 风格一致

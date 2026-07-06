## ADDED Requirements

### Requirement: 按 token 预算分批摘要

SmartCompressor 的 Stage 2 LLM 摘要 SHALL 按 token 预算分批处理 oldSegments。每批的 token 估算值 SHALL 不超过 maxInputTokens（maxTokens / 2，最小 1000）。每个事件 SHALL 完整保留在批次中（不截断）。

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

### Requirement: 多条摘要事件替换原始片段

SmartCompressor SHALL 生成多条摘要事件（每批一条），使用 System role，替换原始 oldSegments。摘要事件的内容 SHALL 包含批次编号（如 "批次 1/3"）以便 LLM 理解上下文结构。

#### Scenario: 多条摘要替换原始事件

- **WHEN** oldSegments 被分成 3 批，生成 3 条摘要
- **THEN** 压缩后的消息列表包含 3 条 System role 摘要消息
- **AND** 每条摘要内容包含 "批次 N/3" 标识
- **AND** 原始 oldSegments 的消息不再出现在压缩后的列表中

#### Scenario: 摘要后接 recent segments

- **WHEN** 压缩后有 3 条摘要 + 2 个 recent segments
- **THEN** 消息顺序为 [System(摘要1), System(摘要2), System(摘要3), ...recent messages]
- **AND** recent segments 的消息保持原始顺序

### Requirement: 单批失败容错

当某批次的 LLM 摘要调用失败时，SmartCompressor SHALL 跳过该批次（log warning），继续处理其他批次。失败的批次 SHALL 不出现在压缩后的消息列表中，但其他批次的摘要仍保留。

#### Scenario: 单批失败不影响其他批次

- **WHEN** 3 批摘要中第 2 批 LLM 调用失败
- **THEN** 第 1 批和第 3 批的摘要正常生成
- **AND** 压缩后的消息列表包含摘要 1 和摘要 3（不含摘要 2）
- **AND** 记录 warning 日志 "batch 2 summary failed, skipped"

#### Scenario: 全部批次失败时降级

- **WHEN** 所有批次的 LLM 调用都失败
- **THEN** 压缩后的消息列表不包含摘要事件
- **AND** 保留 compressEvent（context_compress 标记事件），内容标注 "摘要生成失败"
- **AND** recent segments 仍保留

### Requirement: 无 summaryModel 时跳过分批摘要

当 SmartCompressor 未配置 summaryModel 时，SHALL 跳过 Stage 2（不分批、不调用 LLM），仅保留 Stage 1 的 compressEvent 标记。

#### Scenario: 无 summaryModel 时不调用 LLM

- **WHEN** SmartCompressor 的 summaryModel 为 nil
- **THEN** 不执行分批摘要
- **AND** 压缩后的消息列表仅包含 compressEvent 标记 + recent segments
- **AND** 不发起任何 LLM 调用

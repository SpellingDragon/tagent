## Why

`harden-event-storage-for-scale` change 为 L3 归档预留了 `SummaryGenerator` hook（接口 + `PassthroughSummarizer` 默认实现，见 `l3-archive-summarization` capability）。本 change 替换默认实现为 **LLM 驱动的摘要生成器**，真正在 L2→L3 压缩时对低价值事件（`assistant_response`、`tool_result` 等）生成语义摘要，大幅压缩 L3 存储体积同时保留语义可检索性。

本 change 的范围边界非常窄：**只替换 `SummaryGenerator` 实现**，不改变调用 hook 点、L3 归档策略配置、事件模型或 Compaction 流水线。这是典型的"先放 hook、后替换实现"的解耦收益兑现。

## What Changes

### 新增能力

- **`LLMSummarizer` 实现**：基于 `trpc-agent-go/model` 调用 LLM，prompt 针对事件类型定制（如 `assistant_response` 提取核心决策、`tool_result` 提取关键数据）
- **批处理优化**：L2→L3 合并时按事件类型分桶，同类型事件批量送入 LLM（减少 round-trip）
- **失败降级**：LLM 调用失败时降级为 `PassthroughSummarizer` 行为（content 截断），记录降级 metric
- **摘要质量门控**：生成摘要长度、语言一致性（中/英）、敏感信息过滤的最小校验

### 配置变更

- `partition_defaults.archive_summary_types[type]` 新增 `llm_model` 字段（覆盖默认模型）
- `archive_summarizer.model`（全局默认模型，默认 `gpt-4o-mini` 或等效）
- `archive_summarizer.max_batch_size`（默认 8）
- `archive_summarizer.timeout_ms`（默认 30000）

### Non-Goals

- 不修改 `SummaryGenerator` 接口签名
- 不改变 L0-L2 事件流（LLM 调用仅发生在 L2→L3 压缩时的后台 goroutine）
- 不做向量嵌入 / 语义搜索（归 future change `event-vector-search`）

## Impact

- `memory/summarizer.go`：新增 `LLMSummarizer` 类型 + 构造器
- `memory/summarizer_prompt.go`（新增）：事件类型 → prompt 模板映射
- `tagent.go`：`resolveMemoryStore("file")` 中根据配置切换 `PassthroughSummarizer` 或 `LLMSummarizer`
- `memory/compaction.go`：接入批处理调用（按类型聚合后交给 summarizer）

### 依赖

- 上游：`harden-event-storage-for-scale` 已合并（提供 `SummaryGenerator` 接口 + `EventSummary` 字段 + `archive_summary_types` 配置加载）
- 横向：`trpc-agent-go/model`（LLM 调用客户端）

## Status

**PLACEHOLDER** — 尚未启动实施。等待 `harden-event-storage-for-scale` 合并后评估 L3 归档真实数据量与 LLM 预算，再决定本 change 的优先级与 scope 细化。

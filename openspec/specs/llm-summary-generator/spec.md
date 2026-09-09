# llm-summary-generator Specification

## Purpose

本规范定义 llm-summary-generator 能力。`LLMSummarizer` SHALL implement the `SummaryGenerator` interface defined by the upstream `l3-archive-summarization` capability.

> **状态核验（2026-09-09）**：本规范描述的逐事件 LLM 语义摘要管线（`LLMSummarizer`/`PassthroughSummarizer`/`GenerateBatch`/`Config.ArchiveSummarizer.Enabled`，及上游 `l3-archive-summarization` 能力）**已随 legacy 压缩管线移除**。L3 摘要现由双层折叠承载：工程票据层（卡片行 + `[evt_key]` 召回票据，纯工程零 LLM）恒在，`summary_model` 配置时叠加单行滚动综述 `synthesizeRollingNarrative`（增量合成、编译期常量限长、失败降级纯工程），卡片超限由 `condenseCardLines` 浓缩。本文 SHALL 条款不再约束当前实现，保留作为设计决策记录；现状契约见 [task-skeleton-compression](../task-skeleton-compression/spec.md)。

## Requirements

### Requirement: LLMSummarizer implements SummaryGenerator interface

`LLMSummarizer` SHALL implement the `SummaryGenerator` interface defined by the upstream `l3-archive-summarization` capability. Its `Generate(event FullEvent) (string, error)` method SHALL invoke an LLM to produce a semantic summary of the event content, respecting the `archive_summary_types[event.type]` strategy (only invoked for `summary` strategy events).

#### Scenario: LLMSummarizer generates semantic summary

- **WHEN** `LLMSummarizer.Generate` is called with an `assistant_response` event whose content is a 5 KB response
- **THEN** the returned summary SHALL be a concise string capturing the core decision / action / conclusion, substantially shorter than the original content

#### Scenario: LLMSummarizer falls back on failure

- **WHEN** the LLM call fails (timeout, error, rate limit)
- **THEN** `Generate` SHALL return the result of the configured fallback (default `PassthroughSummarizer`), NOT an error
- **AND** a `summarizer_fallback_total{reason}` metric SHALL be incremented

### Requirement: Batched LLM calls reduce round-trip overhead

`LLMSummarizer` SHALL support a `GenerateBatch([]FullEvent) ([]string, error)` method. The Compactor's L2→L3 merge path SHALL bucket events by type and invoke `GenerateBatch` per bucket, respecting `max_batch_size` configuration.

#### Scenario: Batched call for same-type events

- **WHEN** an L2→L3 compaction batch contains 20 `assistant_response` events and `max_batch_size=8`
- **THEN** `GenerateBatch` SHALL be called 3 times (8 + 8 + 4 events) rather than 20 single `Generate` calls

### Requirement: Configuration gates LLM summarization

`Config.ArchiveSummarizer.Enabled` SHALL control whether `tagent.go` constructs `LLMSummarizer` or `PassthroughSummarizer` at wiring time. When disabled (default), the behavior SHALL be identical to `harden-event-storage-for-scale` baseline.

#### Scenario: Disabled config uses pass-through

- **WHEN** `Config.ArchiveSummarizer.Enabled = false`
- **THEN** `resolveMemoryStore` SHALL wire `PassthroughSummarizer` into the Compactor; no LLM client SHALL be constructed

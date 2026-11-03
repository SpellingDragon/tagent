# llm-summary-generator Specification

## Purpose

> **superseded（2026-11-03 归档核验）**：本规范描述的 `LLMSummarizer` 逐事件语义摘要、批量摘要与配置门已随压缩实现退役（`LLMSummarizer` 与 `SummaryGenerator` 在 Go 树 0 定义）。现行折叠零 LLM：段折叠与历史综述都是确定性工程产物，见 `task-skeleton-compression`。本文件只作历史记录，不再承载现行规范命令。

## Requirements

### Requirement: 历史条款已退役

本文件保留已退役的 LLM 摘要管线历史记录。原条款中的规范化关键词（SHALLs/MUSTs）已全部迁出或退役；现行要求见 `task-skeleton-compression` 与 `compression-pipeline`，公共行为锚见 `memory/replay/age_segments_test.go` 的 `TestAgeFoldFoldsTheOlderBandsIntoOneHistorySummary`。

#### Scenario: 查现行折叠要求

- **WHEN** 需要查询折叠是否调用 LLM 或如何生成正文的现行要求
- **THEN** 读 `task-skeleton-compression` 及其公共行为锚，而不是本历史文件

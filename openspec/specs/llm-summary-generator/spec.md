# llm-summary-generator Specification

## Purpose

> **superseded（2026-11-03 归档核验）**：本规范描述的 `LLMSummarizer` 逐事件语义摘要、批量摘要与配置门已随压缩实现退役（`LLMSummarizer` 与 `SummaryGenerator` 在 Go 树 0 定义）。见 `task-skeleton-compression`。本文件只作历史记录，不再承载现行规范命令。
>
> **勘误（2026-11-15 契约同步核验）**：上方退役注记原有一句断言「折叠零 LLM：段折叠与历史综述都是确定性工程产物」，该断言关于历史综述的一半已被现行实现推翻——历史综述正文在会话 agent 持有模型时由压缩消费插件产出有界语义综述，机械有界读法只作兜底；段折叠仍为确定性工程产物。退役事实部分不变（两符号仍零定义），现行要求见 `task-skeleton-compression` 的老带综述条款与 `compression-pipeline`。

## Requirements

### Requirement: 历史条款已退役

本文件保留已退役的 LLM 摘要管线历史记录。原条款中的规范化关键词（SHALLs/MUSTs）已全部迁出或退役；现行要求见 `task-skeleton-compression` 与 `compression-pipeline`，公共行为锚见 `memory/replay/age_segments_test.go` 的 `TestAgeFoldFoldsTheOlderBandsIntoOneHistorySummary`。

#### Scenario: 查现行折叠要求

- **WHEN** 需要查询折叠是否调用 LLM 或如何生成正文的现行要求
- **THEN** 读 `task-skeleton-compression` 及其公共行为锚，而不是本历史文件

# compress-event-enrichment Specification

## Purpose

> **superseded（2026-11-03 归档核验）**：本规范描述的 `buildCompressEvent` 价值分数与处理策略富集注记已随压缩实现退役（`SmartCompressor`、`buildCompressEvent` 与 `EventValuator` 在 Go 树 0 定义）。现行折叠产出的是工程条目：段折叠产物或历史综述，正文为原条目类型与摘要的一行读法，不含分数/策略元数据。本文件只作历史记录，不再承载现行规范命令。

## Requirements

### Requirement: 历史条款已退役

本文件保留已退役的压缩事件富集注记历史记录。原条款中的规范化关键词（SHALLs/MUSTs）已全部迁出或退役；现行要求见 `task-skeleton-compression` 的段折叠与历史综述条款，公共行为锚见 `memory/replay/age_segments_test.go` 的 `TestAgeFoldFoldsTheNewBandIntoOneProduct` 与 `TestAgeFoldFoldsTheOlderBandsIntoOneHistorySummary`。

#### Scenario: 查现行折叠正文要求

- **WHEN** 需要查询折叠产物的正文由什么组成的现行要求
- **THEN** 读 `task-skeleton-compression` 及其公共行为锚，而不是本历史文件

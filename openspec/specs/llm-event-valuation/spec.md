# llm-event-valuation Specification

## Purpose

> **superseded（2026-11-03 归档核验）**：本规范描述的 `EventValuator` LLM 事件估值、批量合并与类型下限已随压缩实现退役（`EventValuator` 在 Go 树 0 定义）。现行段龄定级是确定性纯函数 `LevelOfAge`/`BandOfLevel`，见 `deterministic-compress-level` 与 `task-skeleton-compression`。本文件只作历史记录，不再承载现行规范命令。

## Requirements

### Requirement: 历史条款已退役

本文件保留已退役的 LLM 事件估值历史记录。原条款中的规范化关键词（SHALLs/MUSTs）已全部迁出或退役；现行要求见 `deterministic-compress-level` 与 `task-skeleton-compression`，公共行为锚见 `memory/replay/age_segments_test.go` 的 `TestAgeLevelReadsTheSharedRule`。

#### Scenario: 查现行定级要求

- **WHEN** 需要查询事件或段龄如何定级的现行要求
- **THEN** 读 `deterministic-compress-level`、`task-skeleton-compression` 及其公共行为锚，而不是本历史文件

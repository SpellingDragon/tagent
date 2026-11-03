# event-compaction Specification

## Purpose

> **superseded（2026-11-03 归档核验）**：本规范描述的 `TaskSegmenter`/`Compactor`/`SmartCompressor` 统一分段与 BeforeModel 压缩回调链已随实现退役（三符号在 Go 树 0 定义）。现行压缩是判定与执行各成事实的一条链：`compress.request` → `compress.product`（见 `compression-pipeline`）；段切分与三带折叠由 `memory/replay` 的 `SegmentsOf`/`PlanByAge`/`FoldByAge` 承载（见 `task-skeleton-compression`）。本文件只作历史记录，不再承载现行规范命令。

## Requirements

### Requirement: 历史条款已退役

本文件保留已退役的统一分段与 BeforeModel 压缩回调链的历史记录。原条款中的规范化关键词（SHALLs/MUSTs）已全部迁出或退役；现行要求见 `compression-pipeline` 与 `task-skeleton-compression`，公共行为锚见 `tests/integration/host/compress_pipeline_test.go` 与 `memory/replay/age_segments_test.go`。

#### Scenario: 查现行压缩要求

- **WHEN** 需要查询分段、判定、执行或恢复的现行要求
- **THEN** 读 `compression-pipeline`、`task-skeleton-compression` 及其公共行为锚，而不是本历史文件

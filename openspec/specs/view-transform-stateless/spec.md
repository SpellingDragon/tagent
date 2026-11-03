# view-transform-stateless Specification

## Purpose

> **superseded（2026-11-03 归档核验）**：本规范描述的 `SmartCompressor` 无段早退与 `protectPendingAsyncSegments` 移除断言已随压缩实现退役（两符号在 Go 树 0 定义）。现行早退是容量门：`PlanByAge`/`FoldByAge` 在投影渲染容量未超阈时直接返回且不触碰投影，见 `task-skeleton-compression` 与 `compression-pipeline`。本文件只作历史记录，不再承载现行规范命令。

## Requirements

### Requirement: 历史条款已退役

本文件保留已退役的无段早退与 pending 段保护移除的历史记录。原条款中的规范化关键词（SHALLs/MUSTs）已全部迁出或退役；现行要求见 `task-skeleton-compression` 与 `compression-pipeline`，公共行为锚见 `memory/replay/age_segments_test.go` 的 `TestAgeFoldWaitsForTheVolumeThreshold`。

#### Scenario: 查现行早退要求

- **WHEN** 需要查询容量内早退或无候选 no-op 的现行要求
- **THEN** 读 `task-skeleton-compression` 与 `compression-pipeline` 及其公共行为锚，而不是本历史文件

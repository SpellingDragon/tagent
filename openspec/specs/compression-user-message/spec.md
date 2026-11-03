# compression-user-message Specification

## Purpose

> **superseded（2026-11-03 归档核验）**：本规范描述的 `SmartCompressor` 替换旧段后保留 pending user、`ensureUserPrompt` 引导消息与摘要 System role 已随压缩实现退役（`SmartCompressor` 与 `ensureUserPrompt` 在 Go 树 0 定义）。现行投影把无 `agent_output` 闭合的进行中段整段保留，见 `task-skeleton-compression`。本文件只作历史记录，不再承载现行规范命令。

## Requirements

### Requirement: 历史条款已退役

本文件保留已退役的压缩后 pending user 保留与摘要 System role 历史记录。原条款中的规范化关键词（SHALLs/MUSTs）已全部迁出或退役；现行要求见 `task-skeleton-compression` 的「进行中段与近期窗口保持原样」，公共行为锚见 `memory/replay/age_segments_test.go` 的 `TestAgeFoldKeepsTheSegmentInProgressWhole`。

#### Scenario: 查现行进行中段要求

- **WHEN** 需要查询进行中段是否被压缩或改写的现行要求
- **THEN** 读 `task-skeleton-compression` 及其公共行为锚，而不是本历史文件

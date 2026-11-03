# summary-memory-rag Specification

## Purpose

> **superseded（2026-11-03 归档核验）**：本规范描述的压缩摘要写入 `MemoryStore`、用引用消息替换原文、按摘要 key 召回与摘要去重已随压缩实现退役（`SmartCompressor` 与 `context_compress_summary` 在 Go 树 0 定义）。现行折叠不产生摘要事件：被折原文留在事实链，由 `compress.product` 记录产物，召回走 `recall-protocol` 的票据协议。本文件只作历史记录，不再承载现行规范命令。

## Requirements

### Requirement: 历史条款已退役

本文件保留已退役的压缩摘要持久化与召回的历史记录。原条款中的规范化关键词（SHALLs/MUSTs）已全部迁出或退役；现行要求见 `compression-pipeline`（产物事实）与 `recall-protocol`（票据召回），公共行为锚见 `tests/integration/host/compress_pipeline_test.go` 与 `recall-protocol` 的既有 Scenario。

#### Scenario: 查现行恢复与召回要求

- **WHEN** 需要查询压缩后原文恢复或按票据召回的现行要求
- **THEN** 读 `compression-pipeline`、`recall-protocol` 及其公共行为锚，而不是本历史文件

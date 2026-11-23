# production-wiring-fix Specification

## Purpose

> **superseded（2026-11-16 契约同步核验，2026-11-19 复核补记）**：本规范描述的生产入口存储生命周期装配已随退位机制与三器脚手架删除而整体退役——它要求创建的墓碑集、遗忘管理器、压实器与生产入口名在 Go 树零定义，退化扫描、容量淘汰与物理回收不是现行路径（现行只允拒绝销毁，见 `storage-lifecycle-completion`）。其覆盖的接线事实由 `production-wiring` 单一承载：存储装配经一个开档入口派生原文仓与事件段仓，关闭是一次提交屏障后结束视图。本文件只作历史记录，不再承载现行规范命令。

## Requirements

### Requirement: 历史条款已退役

本文件保留已退役的生产入口生命周期装配历史记录。原条款中的规范化关键词（SHALLs/MUSTs）已全部迁出或退役；现行要求见 `production-wiring` 的存储接线条款与 `storage-lifecycle-completion` 的保留保护条款，公共行为锚见 `memory/ledger` 的开档与段存储测试。

#### Scenario: 查现行存储装配要求

- **WHEN** 需要查询生产入口如何装配存储、以及哪些生命周期组件存在
- **THEN** 读 `production-wiring` 与 `storage-lifecycle-completion` 的现行条款，而不是本历史文件

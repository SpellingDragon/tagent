# goal-tools Specification

## Purpose

> **superseded（2026-11-04 逐模块核对）**：本规范描述的「`goal_declare` / `goal_list` / `goal_resolve` / `denial_query` / `approval_list` 五个 `PlainTool` 仅 entry agent 可挂载」已退役。现 goal 声明激活治理 goal 门并写 governance 事件（`features/evolution/governance/goal.go:53/55` 的 `goal_declared`/`goal_resolved`），`GoalRegistry` 启动时经事件回放重建；本代无 `goal_*` 工具面，`PlainTool` 类型不存在。

本文件此后只作 superseded 记录，不再承载现行规范命令；阅读现行要求时不得以本文件为依据，也不得把已退役的机制形态重新写入现行规范。

## Requirements

### Requirement: 历史条款已退役

原 Requirements 中的规范化关键词（SHALLs/MUSTs）及其描述的机制已随实现退役而撤销；有效义务由对应现行能力主规格与公共行为锚承接。本条只登记历史事实，不设现行命令。

#### Scenario: 查现行要求

- **WHEN** 需要查询该域的现行要求
- **THEN** 读对应现行能力的主规格及其公共行为锚，而不是本历史文件

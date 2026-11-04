# tool-lifecycle-management Specification

## Purpose

> **superseded（2026-11-04 逐模块核对）**：本规范描述的「`tagent.New()` 调 `RegisterBuiltinTools()` 注册到 `ToolRegistry`、`RegisterPlainTool(id, factory)`」已退役。现装配入口是 `assembly.Boot`（`assembly/host.go:350`）/ `assembly.Assemble`（`assembly/run.go:368`），工具按声明逐名解析（`assembly/host_reload.go:446-463`、`assembly/host_tools.go:36`），无全局注册表、无模块根包 `tagent`。

本文件此后只作 superseded 记录，不再承载现行规范命令；阅读现行要求时不得以本文件为依据，也不得把已退役的机制形态重新写入现行规范。

## Requirements

### Requirement: 历史条款已退役

原 Requirements 中的规范化关键词（SHALLs/MUSTs）及其描述的机制已随实现退役而撤销；有效义务由对应现行能力主规格与公共行为锚承接。本条只登记历史事实，不设现行命令。

#### Scenario: 查现行要求

- **WHEN** 需要查询该域的现行要求
- **THEN** 读对应现行能力的主规格及其公共行为锚，而不是本历史文件

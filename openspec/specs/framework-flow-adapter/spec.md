# framework-flow-adapter Specification

## Purpose

> **superseded（2026-11-04 逐模块核对）**：本规范描述的 `FrameworkFlowAdapter`（接收 `[]*AgentEvent` / `*session.Session` / `*SessionProjection`、新建 `EventBus`、单次 `Flow.Run`）已退役。现实现是每地址一个常驻消费实例（`runtime/agent.Loop` + `Consumer`），输入与通知是会话事件链上的事实，由 `WaitAnswer` 交回闭合段答案；现行契约见 `consumer-loop-convergence`、`async-task-execution`、`persistent-event-loop`（已退役注记）。`FrameworkFlowAdapter`/`AgentEvent`/`SessionProjection`/`EventBus` 全仓 0 命中。

本文件此后只作 superseded 记录，不再承载现行规范命令；阅读现行要求时不得以本文件为依据，也不得把已退役的机制形态重新写入现行规范。

## Requirements

### Requirement: 历史条款已退役

原 Requirements 中的规范化关键词（SHALLs/MUSTs）及其描述的机制已随实现退役而撤销；有效义务由对应现行能力主规格与公共行为锚承接。本条只登记历史事实，不设现行命令。

#### Scenario: 查现行要求

- **WHEN** 需要查询该域的现行要求
- **THEN** 读对应现行能力的主规格及其公共行为锚，而不是本历史文件

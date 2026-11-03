# persistent-event-loop Specification

## Purpose

> **superseded（2026-11-02 核验；2026-11-03 迁移）**：本规范描述的「事件邮箱 drain + `FrameworkFlowAdapter.RunFlow`」常驻循环已退役。现实现是每地址一个常驻消费实例（`runtime/agent.Loop` + `Consumer`）：输入与通知是会话事件链上的事实，实例拉取驱动事实、闭合消费段、由 `WaitAnswer` 把该段的答案交回等待者。原 Requirements 中仍有效的条款已逐条映射到唯一现行能力与公共行为锚；旧循环机制（mailbox、`RunFlow`、`StopLoop`、durable inbox、`turnEcho`）的条款随实现退役而撤销。逐条映射与红绿证据见 `openspec/changes/runtime-architecture-convergence/changes/04-domain-contracts/tasks.md` 4.2。本文件此后只作 superseded 记录，不再承载任何现行规范命令；现行契约见 `consumer-loop-convergence`、`async-task-execution`、`unified-task-semantics`、`event-metadata-contract`、`failure-polarity`、`quiet-receipts`、`execution-resource-lifecycle` 等主规格。

## Requirements

### Requirement: 历史条款已迁移

本文件只保留被取代的旧常驻事件循环的历史记录。原条款中的有效义务已由上列现行能力主规格与公共行为锚承接；旧机制条款已随对应实现退役而撤销。原条款中的规范化关键词（SHALLs/MUSTs）已全部迁出或退役；阅读现行要求时不得以本文件为依据，也不得把已退役的 `mailbox`／`RunFlow`／`StopLoop`／durable inbox 形态重新写入现行规范。

#### Scenario: 查现行要求

- **WHEN** 需要查询地址消费、通知、关闭、可靠输入或模型入口失败的现行要求
- **THEN** 读对应现行能力的主规格及其公共行为锚，而不是本历史文件

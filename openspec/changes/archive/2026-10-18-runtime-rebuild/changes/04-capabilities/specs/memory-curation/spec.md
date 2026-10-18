# memory-curation Specification

## Purpose

本 delta 要求价值评估与整理触发与固化物的**因果关联**不改写原始事实：整理产生新的固化物引用，原文与其 owner/format 隔离保持。

## ADDED Requirements

### Requirement: 整理触发不改写原始事实

价值评估与整理触发 SHALL 以**因果关联**（引用/父键）关联既有事实；整理 MUST NOT 改写、覆写或迁移原始事件字节与其 owner/format 归属；整理失败 SHALL 工程沉底，不破坏 recall 链。

#### Scenario: 整理不改写原文

- **WHEN** 触发一次记忆整理
- **THEN** 原始事件字节与引用元数据保持
- **AND** 整理产出以新引用/固化物形式存在，可回溯原文

#### Scenario: 超限整理且 recall 链不断

- **WHEN** 卡片序列超限且整理可用
- **THEN** 浓缩结果含关键固化物 key 引用，recall 仍可按 key 回补

#### Scenario: 整理失败降级不塌（失败）

- **WHEN** 整理调用失败
- **THEN** 主流程不受影响，行为等于沉底
- **AND** 原始事实未被改写

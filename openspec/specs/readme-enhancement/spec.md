# readme-enhancement Specification

## Purpose

规范 README.md 项目文档的增强要求：提供模块架构概览、端到端数据流描述、快速启动指引与关键设计决策说明，使新读者能快速理解 tagent 的结构与运行方式。

## Requirements
### Requirement: README 包含项目架构概览

README.md SHALL 包含 tagent 项目的架构概览，包括：核心模块列表（agent、memory、tool、plugin、event、prompt、config）、模块职责一句话描述、模块间依赖关系。

#### Scenario: README 包含模块列表

- **WHEN** 阅读 README.md 的架构概览部分
- **THEN** 列出 agent 包及其职责（TagentAgent 组合根、拦截器、压缩引擎）
- **AND** 列出 memory 包及其职责（FullEvent 类型、MemoryStore 接口、FileSegmentStore、RelationStore）
- **AND** 列出 tool 包及其职责（AgentToolWrapper、MemoryStoreAccessor）
- **AND** 列出 plugin 包及其职责（MemoryPlugin、SummaryPlugin）

#### Scenario: README 包含依赖关系说明

- **WHEN** 阅读 README.md 的模块关系部分
- **THEN** 说明 tagent.go 是组装入口（New 工厂函数）
- **AND** 说明依赖方向：tagent → agent/plugin/memory/tool/prompt/event

### Requirement: README 包含核心数据流说明

README.md SHALL 包含一次请求的数据流描述，帮助读者理解端到端执行路径。

#### Scenario: README 数据流描述

- **WHEN** 阅读 README.md 的数据流部分
- **THEN** 描述用户请求 → TagentAgent.Call → Runner → LLM → BeforeModel 拦截器 → Plugin OnEvent → Session 存储 → MemoryStore 持久化的流程
- **AND** 描述 AgentToolWrapper 作为子 Agent 入口的执行路径

### Requirement: README 包含快速启动指引

README.md SHALL 包含基于 config.go 声明式配置的快速启动指引，展示如何通过 YAML/JSON 配置文件启动 tagent。

#### Scenario: README 快速启动

- **WHEN** 阅读 README.md 的快速启动部分
- **THEN** 包含一个最小配置示例（YAML 或 JSON 格式）
- **AND** 包含 `tagent.New(cfg)` 调用示例

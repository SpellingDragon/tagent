# tool-lifecycle-management Specification

## Purpose

本规范定义 tool-lifecycle-management 能力。tagent SHALL 在 `tagent.New()` 中调用 `RegisterBuiltinTools()`，将所有内置工具（exec + knowledge/recall sub-tools）注册到 ToolRegistry。注册通过 `RegisterPlainTool(id, factory)` 完成。

## Requirements

### Requirement: 内置工具通过 RegisterBuiltinTools 统一注册

tagent SHALL 在 `tagent.New()` 中调用 `RegisterBuiltinTools()`，将所有内置工具（exec + knowledge/recall sub-tools）注册到 ToolRegistry。注册通过 `RegisterPlainTool(id, factory)` 完成。knowledge/recall 不再通过 `RegisterToolAgent` 注册工厂 — 它们变为 config-driven agent，与其他 agent（action/read/write/speak/draw）一致。

#### Scenario: tagent.New 调用 RegisterBuiltinTools

- **WHEN** 调用 `tagent.New(cfg, opts...)`
- **THEN** `RegisterBuiltinTools()` 被调用
- **AND** ToolRegistry 中注册了 exec + 10 个 sub-tool factory
- **AND** ToolRegistry 中不包含 "knowledge" 和 "recall" 的 tool agent factory

#### Scenario: knowledge 不再通过 RegisterToolAgent 注册

- **WHEN** `RegisterBuiltinTools()` 执行完毕
- **THEN** `GetToolAgentFactory("knowledge")` 返回 false
- **AND** `GetToolAgentFactory("recall")` 返回 false
- **AND** knowledge/recall 的 sub-tools 通过 `GetPlainToolFactory("skill_search")` 等可查询

### Requirement: buildAgent 对所有内置 agent 走 config-driven 路径

`buildAgent` SHALL 对所有 agent 统一走 config-driven 路径：从 `AgentConfig.Tools` 列表构建工具。`buildAgent` 中的 `GetToolAgentFactory` 检查 SHALL 仅用于外部注册的自定义 factory，内置 agent（knowledge、recall、action、read、write、speak、draw）SHALL 不命中 factory 路径。

#### Scenario: buildAgent 构建 knowledge agent 走 config 路径

- **WHEN** `buildAgent` 构建 "knowledge" agent
- **THEN** `GetToolAgentFactory("knowledge")` 返回 false（未注册）
- **AND** 从 `AgentConfig.Tools` 列表构建 6 个 plain tool
- **AND** 每个 plain tool 从 ToolRegistry 的 `GetPlainToolFactory` 创建

#### Scenario: buildAgent 构建 recall agent 走 config 路径

- **WHEN** `buildAgent` 构建 "recall" agent
- **THEN** `GetToolAgentFactory("recall")` 返回 false（未注册）
- **AND** 从 `AgentConfig.Tools` 列表构建 4 个 plain tool

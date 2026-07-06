## ADDED Requirements

### Requirement: 内置 agent name 禁止 ToolAgentFactory 覆盖

`buildAgent` SHALL 对内置 agent name（`knowledge`、`recall`、`action`、`read`、`write`、`speak`、`draw`）跳过 `GetToolAgentFactory` 检查，强制走 config-driven 路径。即使有人注册了对应的 ToolAgentFactory，`buildAgent` 也 SHALL 忽略它。

#### Scenario: 内置 agent name 忽略 ToolAgentFactory

- **WHEN** 有人通过 `RegisterToolAgent("knowledge", factory)` 注册了 knowledge 的 ToolAgentFactory
- **AND** `buildAgent` 构建 "knowledge" agent
- **THEN** `buildAgent` 不调用该 factory
- **AND** agent 通过 config-driven 路径构建（从 `AgentConfig.Tools` 列表创建 plain tool）

#### Scenario: 非内置 agent name 可走 ToolAgentFactory 路径

- **WHEN** 有人通过 `RegisterToolAgent("custom_agent", factory)` 注册了自定义 agent 的 ToolAgentFactory
- **AND** `buildAgent` 构建 "custom_agent" agent
- **THEN** `buildAgent` 调用该 factory，走 ToolAgentFactory 路径

#### Scenario: 内置 agent name 集合完整

- **WHEN** 检查 `buildAgent` 中的内置 agent name 保护列表
- **THEN** 列表 SHALL 包含以下 7 个 name：`knowledge`、`recall`、`action`、`read`、`write`、`speak`、`draw`

### Requirement: 移除死代码 decodeProperties

`builtin.go` SHALL 不包含 `decodeProperties[T]` 函数。该函数未被使用且签名与 `ToolRef.Properties` 类型不匹配。

#### Scenario: builtin.go 无 decodeProperties

- **WHEN** 检查 `builtin.go` 的函数列表
- **THEN** 不存在名为 `decodeProperties` 的函数

### Requirement: read/write/speak/draw 不暴露 nil parentMemStore 的 NewTool

read、write、speak、draw 包 SHALL 不暴露 `NewTool` 便利函数，因为该函数传入 `nil` 作为 `parentMemStore`，在有 `event_keys` 参数时会导致 panic。这些 agent 的创建 SHALL 通过 config-driven 路径（`buildAgent` → `buildAgentToolRef`），由 `buildAgentToolRef` 正确传入 `parentMemStore`。

#### Scenario: read/write/speak/draw 包无 NewTool 函数

- **WHEN** 检查 `tool/read`、`tool/write`、`tool/speak`、`tool/draw` 包的导出函数
- **THEN** 不存在名为 `NewTool` 的函数

#### Scenario: config-driven 路径正确创建 read agent

- **WHEN** `buildAgent` 构建 "read" agent（config-driven 路径）
- **AND** read agent 被 "action" agent 引用为 `kind: agent`
- **THEN** `buildAgentToolRef` 创建 read agent 并包装为 CallableTool
- **AND** `AgentToolWrapper` 的 `parentMemStore` 被设置为 "action" agent 的 MemoryStore

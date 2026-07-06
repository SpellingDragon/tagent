## ADDED Requirements

### Requirement: ToolRegistry 统一工具注册中心

tagent SHALL 维护一个 `ToolRegistry` 实例，作为所有内置工具（plain tool 和 tool agent factory）的统一注册中心。`GetRegistry()` SHALL 返回全局单例。`RegisterBuiltinTools()` SHALL 在 `tagent.New()` 中被调用，注册所有内置工具。

#### Scenario: RegisterBuiltinTools 注册所有内置 plain tool

- **WHEN** `RegisterBuiltinTools()` 被调用
- **THEN** ToolRegistry 中注册了以下 plain tool factory：`exec`、`skill_search`、`skill_load`、`mcp_discover`、`web_search`、`duckduckgo_search`、`memory_query`、`recall_query`、`recall_get`、`recall_recent`、`recall_trace`
- **AND** 每个 factory 可通过 `GetPlainToolFactory(id)` 查询到

#### Scenario: GetRegistry 返回单例

- **WHEN** 多次调用 `GetRegistry()`
- **THEN** 返回同一个 `*ToolRegistry` 实例

#### Scenario: 重复注册同一 tool id 时 panic

- **WHEN** 对已注册的 tool id 再次调用 `RegisterPlainTool`
- **THEN** panic with message containing the duplicate id

### Requirement: PlainToolFactoryConfig 携带运行时依赖

`PlainToolFactoryConfig` SHALL 包含以下可选运行时依赖字段：`MemStore`（memory.MemoryStore）、`SkillRepo`（tool.SkillRepository）、`MCPToolSets`（[]trpctool.ToolSet）、`ReadPartitionIDs`（[]int）。这些字段由 `buildAgent` 在构建 plain tool 时注入。不依赖这些字段的 plain tool（如 exec）SHALL 忽略它们。

#### Scenario: skill_search 工厂接收 SkillRepo

- **WHEN** `buildAgent` 构建包含 `kind: tool, id: skill_search` 的 agent，且 `runtimeConfig.skillRepo` 非 nil
- **THEN** `PlainToolFactoryConfig.SkillRepo` 被设置为 `runtimeConfig.skillRepo` 的值
- **AND** skill_search 工厂创建的 tool 可正常搜索 skill

#### Scenario: skill_search 工厂在 SkillRepo 缺失时返回 error

- **WHEN** `buildAgent` 构建包含 `kind: tool, id: skill_search` 的 agent，但 `runtimeConfig.skillRepo` 为 nil
- **THEN** skill_search 工厂返回 error: "skill_search requires SkillRepo"

#### Scenario: recall_query 工厂接收 MemStore 和 ReadPartitionIDs

- **WHEN** `buildAgent` 构建包含 `kind: tool, id: recall_query` 的 agent
- **THEN** `PlainToolFactoryConfig.MemStore` 被设置为该 agent 的 MemoryStore
- **AND** `PlainToolFactoryConfig.ReadPartitionIDs` 被设置为该 agent 的 ReadPartitionIDs（从 MemoryConfig.ReadNamespaces 解析）

#### Scenario: exec 工厂忽略运行时依赖字段

- **WHEN** `buildAgent` 构建包含 `kind: tool, id: exec` 的 agent
- **THEN** exec 工厂仅使用 `Properties` 中的 work_dir 和 session_name
- **AND** 不访问 MemStore、SkillRepo 等字段

### Requirement: 内置 sub-tool 通过 RegisterSubTools 注册

knowledge 包 SHALL 导出 `RegisterSubTools(registry *agent.ToolRegistry)` 函数，注册 skill_search、skill_load、mcp_discover、web_search、duckduckgo_search、memory_query 共 6 个 plain tool factory。recall 包 SHALL 导出 `RegisterSubTools(registry *agent.ToolRegistry)` 函数，注册 recall_query、recall_get、recall_recent、recall_trace 共 4 个 plain tool factory。

#### Scenario: knowledge.RegisterSubTools 注册 6 个工具

- **WHEN** 调用 `knowledge.RegisterSubTools(registry)`
- **THEN** registry 中新增 6 个 plain tool factory
- **AND** id 分别为 "skill_search"、"skill_load"、"mcp_discover"、"web_search"、"duckduckgo_search"、"memory_query"

#### Scenario: recall.RegisterSubTools 注册 4 个工具

- **WHEN** 调用 `recall.RegisterSubTools(registry)`
- **THEN** registry 中新增 4 个 plain tool factory
- **AND** id 分别为 "recall_query"、"recall_get"、"recall_recent"、"recall_trace"

### Requirement: knowledge/recall 为 config-driven agent

knowledge 和 recall agent SHALL 通过 Config.Agents map 中的 tools 列表声明其使用的 sub-tools。`buildAgent` SHALL 不再为 knowledge/recall 走 factory 路径（`GetToolAgentFactory` 查不到）。`knowledge.NewAgent` 和 `recall.NewAgent` SHALL 接收外部传入的 `Tools []tool.Tool`，不再内部调用 `BuildSubTools` / `buildRecallSubTools`。

#### Scenario: DefaultConfig 中 knowledge 声明 6 个 sub-tool

- **WHEN** 调用 `DefaultConfig()`
- **THEN** `Agents["knowledge"].Tools` 包含 6 个 ToolRef，kind=tool，id 分别为 skill_search、skill_load、mcp_discover、web_search、duckduckgo_search、memory_query

#### Scenario: DefaultConfig 中 recall 声明 4 个 sub-tool

- **WHEN** 调用 `DefaultConfig()`
- **THEN** `Agents["recall"].Tools` 包含 4 个 ToolRef，kind=tool，id 分别为 recall_query、recall_get、recall_recent、recall_trace

#### Scenario: buildAgent 对 knowledge 不走 factory 路径

- **WHEN** `buildAgent` 构建 "knowledge" agent
- **THEN** `GetToolAgentFactory("knowledge")` 返回 false
- **AND** agent 通过 config tools 列表构建，tools 从 registry 中查找 plain tool factory 创建

### Requirement: ValidateToolAccess 验证 config 引用的 tool 已注册

`ValidateToolAccess(cfg *Config)` SHALL 遍历所有 agent 的 tools 列表，对 `kind: tool` 的 ToolRef 检查其 `id` 是否在 ToolRegistry 中已注册。未注册时 SHALL 返回 error。

#### Scenario: config 引用未注册的 tool id

- **WHEN** config 中某 agent 声明了 `kind: tool, id: non_existent`，且 registry 中未注册 "non_existent"
- **THEN** `ValidateToolAccess` 返回 error: `agent "xxx" tool "non_existent" is not registered`

#### Scenario: config 引用已注册的 tool id

- **WHEN** `RegisterBuiltinTools()` 已执行，config 中某 agent 声明了 `kind: tool, id: exec`
- **THEN** `ValidateToolAccess` 返回 nil（无错误）

### Requirement: 三阶段工具生命周期

tagent 工具体系 SHALL 遵循三阶段生命周期：(1) 实现层指定 — tool 包提供工厂函数和 `RegisterSubTools`；(2) init 注册 — `RegisterBuiltinTools()` 在 `tagent.New()` 中调用，通过 `RegisterPlainTool` 声明所有可用工具；(3) config 组织 — `buildAgent` 按 config 的 tools 列表从 registry 查找 factory 并组装 agent。

#### Scenario: 三阶段完整执行

- **WHEN** 调用 `tagent.New(cfg, opts...)`
- **THEN** 第一阶段：`RegisterBuiltinTools()` 将所有内置 tool factory 注册到 ToolRegistry
- **AND** 第二阶段：`ValidateToolAccess(&cfg)` 验证 config 引用的 tool 已注册
- **AND** 第三阶段：`buildAgent` 按 config tools 列表从 registry 查找 factory、注入运行时依赖、组装 agent

#### Scenario: 未调用 RegisterBuiltinTools 时 ValidateToolAccess 失败

- **WHEN** 未调用 `RegisterBuiltinTools()`，直接调用 `ValidateToolAccess(&cfg)`，且 config 引用了 `kind: tool, id: exec`
- **THEN** 返回 error: `agent "xxx" tool "exec" is not registered`

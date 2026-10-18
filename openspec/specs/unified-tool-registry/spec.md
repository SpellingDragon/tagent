# unified-tool-registry Specification

## Purpose

本规范定义 unified-tool-registry 能力。tagent SHALL 维护一个 `ToolRegistry` 实例，作为所有内置工具（plain tool 和 tool agent factory）的统一注册中心。`GetRegistry()` SHALL 返回全局单例。

## Requirements

### Requirement: ToolRegistry 统一工具注册中心

tagent SHALL 在**运行时装配期**（组合根 `tagent.New()`）以**工厂装配**构造一个**运行时局部**的工具 catalog，注册所有内置工具；catalog MUST NOT 被规定为进程级全局单例，MUST NOT 依赖 `GetRegistry()` 式全局访问点，也 MUST NOT 以反射/DI 容器承载装配。`RegisterBuiltinTools()` SHALL 在组合根构造（`tagent.New()`）中被调用，注册所有内置工具。注册只声明**原生工具**（`CallableTool`/`ToolSet`/`FunctionTool` 适配）；注册表 MUST NOT 承载第二工具迭代循环（不得引入或复用 `LLMAgent` 式再循环）。工具的**生效实例**由 state 在请求边界按代借出，catalog 本身不持有跨代可变执行状态。

原生工具列表 SHALL 按**可见 scope** 在当前请求装配时固定为**请求快照**（进入 `Request.Tools`）：请求进行中的运行时增删 MUST NOT 改变该在途快照，但**合法配置换代**（组织发布新代）SHALL 按新代产生**新的**请求快照——「运行时增删不影响在途快照」MUST NOT 被泛化为「快照永不更新」。

#### Scenario: RegisterBuiltinTools 注册所有内置 plain tool

- **WHEN** `RegisterBuiltinTools()` 被调用
- **THEN** 该运行时 catalog 中注册了以下 plain tool factory：`exec`、`skill_search`、`skill_load`、`mcp_discover`、`web_search`、`duckduckgo_search`、`memory_query`、`recall_query`、`recall_get`、`recall_recent`、`recall_trace`
- **AND** 每个 factory 可通过该运行时 catalog 的查询面（如 `GetPlainToolFactory(id)`）查到

#### Scenario: 运行时局部 catalog（不强制全局单例）

- **WHEN** 同一进程内装配两个相互独立的运行时实例
- **THEN** 每个实例经自身装配入口持有独立 catalog，互不共享进程级可变注册表
- **AND** MUST NOT 依赖 `GetRegistry()` 全局单例，MUST NOT 为跨实例共享而引入 DI 容器或新的注册框架

#### Scenario: 同一 catalog 内重复 tool id 为具名装配错误

- **WHEN** 构造某运行时 catalog 时对同一 tool id 注册两次
- **THEN** 装配期返回**具名错误**（含重复 id）并使本次构造失败
- **AND** MUST NOT 以进程级 panic 终止——错误限定在该次装配，不污染其它运行时实例

#### Scenario: 原生工具列表按可见 scope 固定为请求快照

- **WHEN** 一次请求按 config 声明的可见 scope 组装
- **THEN** 该请求的 `Request.Tools` 为该 scope 下的固定快照，请求进行中不被运行时增删改变
- **AND** 合法配置**换代**时新请求 SHALL 依据新代产生**新的**快照（不沿用旧代声明）

#### Scenario: 无第二迭代循环

- **WHEN** 检查 catalog 与执行路径
- **THEN** 一次工具调用只经**一次**原生调用归约，不存在第二次 `LLMAgent` 循环
- **AND** `CallReceipt` 仅表示**受理**（任务已被接收），MUST NOT 被当作任务**终局**归约；任务终局语义由 02 域承载，本域不另定归约口径
- **AND** 后台结果进入**下一次**实际模型视图，不冒充同 CallID 的第二次同步 response

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
- **AND** `PlainToolFactoryConfig.ReadPartitionIDs` 被设置为该 agent 的读分区列表：自身命名空间分区（`PartitionIDFromName(agentName)`，恒在首位）+ `MemoryConfig.ReadNamespaces` 解析出的跨命名空间分区（去重）——agent 无 read_namespaces 时也至少能查询自身时间线

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

### Requirement: 原生工具经 state 绑定注册并真实执行

系统 SHALL 使注册的原生工具进入实际模型请求的 `Request.Tools` 并**被模型第二轮真实调用执行**；工具活绑定（file/skill/FunctionTool/MCP ToolSet 等）SHALL 由 state 持有、请求边界借出同代，旧请求继续持有旧代绑定。工具注册 MUST NOT 使同一 native 接口被镜像为同形影子结构。

#### Scenario: 原生工具进入第二轮真实执行

- **WHEN** entry agent 的一次请求完成首个工具调用
- **THEN** 该工具的声明 SHALL 出现在 `Request.Tools`
- **AND** 模型第二轮 SHALL 收到该工具**真实正文结果**（非固定 final 替身）

#### Scenario: 同代绑定借出、旧请求不拼接新代

- **WHEN** 一次请求跨越一次组织发布（新代生成）
- **THEN** 在途请求继续持有其开始时取得的同代工具绑定
- **AND** MUST NOT 在请求中途拼接新代工具实例

#### Scenario: 失败——未注册的 native 工具被拒

- **WHEN** config 引用未注册的 `id`
- **THEN** `ValidateToolAccess` 返回具名 error，构造失败，不静默降级为空工具

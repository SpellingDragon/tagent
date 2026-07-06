## Why

knowledge/recall 的 sub-tools（skill_search、skill_load、mcp_discover、web_search、memory_query、recall_query、recall_get、recall_recent、recall_trace）当前硬编码在工厂内部的 `BuildSubTools()` / `buildRecallSubTools()` 中，无法通过配置灵活组合或被其他 agent 复用。这与 exec（已注册为 plain tool、通过 config 声明使用）的模式不一致。同时，上一轮架构重构（action 升级为执行中枢 agent、read/write/speak/draw config-driven）留下了未完成的编译错误，需要统一收尾。

## What Changes

- **BREAKING**: 移除 `knowledgeFactory`、`recallFactory` — knowledge/recall 不再通过工厂创建，变为纯 config-driven agent（与 action/read/write/speak/draw 一致）
- 将 knowledge 的 6 个 sub-tools（skill_search、skill_load、mcp_discover、web_search、duckduckgo_search、memory_query）注册为 plain tools，通过 `RegisterPlainTool()` 在 `init()` 中注册
- 将 recall 的 4 个 sub-tools（recall_query、recall_get、recall_recent、recall_trace）注册为 plain tools
- 扩展 `PlainToolFactoryConfig`，新增 `MemStore`、`SkillRepo`、`MCPToolSets`、`ReadPartitionIDs` 字段，使 plain tool 工厂能接收运行时依赖
- `buildAgent` 在构建 plain tool 时注入 agent 级别的运行时依赖（MemStore、SkillRepo 等）
- `DefaultConfig` 中 knowledge/recall agent 显式声明 tools 列表（`kind: tool, id: skill_search` 等）
- 修复当前编译错误：补全 `RegisterBuiltinTools()`/`GetRegistry()`/`ValidateToolAccess()`、`AgentConfig.Meditation`/`KeepRecentTasks` 字段、`action.NewActionTool` 签名适配
- 建立 **三阶段工具生命周期**：① 实现层指定（tool 包提供工厂函数）→ ② `init()` 注册（`RegisterPlainTool` 声明可用工具）→ ③ config 组织（`buildAgent` 按 config tools 列表组装 agent）

## Capabilities

### New Capabilities
- `unified-tool-registry`: 统一内置工具注册机制 — 所有内置工具（exec + knowledge/recall sub-tools）通过 `RegisterPlainTool` 注册为 plain tool，agent 通过 config tools 列表声明使用，工厂仅用于需要复杂初始化的场景

### Modified Capabilities
- `tool-lifecycle-management`: 工具注册生命周期从"工厂创建 agent"变为"init 注册 plain tool → config 声明 → buildAgent 组装"

## Impact

- **builtin.go**: 移除 knowledgeFactory/recallFactory，新增 10 个 sub-tool 工厂注册 + `RegisterBuiltinTools()`/`GetRegistry()`/`ValidateToolAccess()` 实现
- **agent/tool_agent.go**: `PlainToolFactoryConfig` 扩展运行时依赖字段
- **tagent.go**: `buildAgent`/`buildPlainToolRef` 传递运行时依赖给 plain tool 工厂
- **config.go**: `DefaultConfig` 中 knowledge/recall 声明 tools 列表；补全 `Meditation`/`KeepRecentTasks` 字段
- **tool/knowledge/knowledge_subtools.go**: `BuildSubTools` 拆分为独立 `RegisterSubTools()` 函数，每个 sub-tool 提供独立工厂
- **tool/recall/recall_subtools.go**: `buildRecallSubTools` 拆分为独立工厂函数
- **tool/knowledge/knowledge_agent.go**: `NewAgent` 不再内部调用 `BuildSubTools`，改为接收外部传入的 tools
- **tool/recall/recall_agent.go**: `NewAgent` 不再内部调用 `buildRecallSubTools`
- **examples/wechat-bot/tagent.yaml + tagent.rl.yaml**: knowledge/recall agent 配置补充 tools 声明

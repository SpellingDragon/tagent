## Why

tagent 面对复杂多步骤任务时，LLM 在 ReAct 循环中逐步执行工具调用，没有持久化的工作计划。上下文压缩后 LLM 可能丢失执行意图，用户无法看到进度。项目已有 OpenSpec 机制（proposal → design → tasks → apply → archive），但目前只在人工开发流程中使用，tagent 运行时无法利用它做计划管理。

## What Changes

- 新增 `plan` 子 agent：作为 tagent 的工具被调用，负责工作计划的创建、更新、查询和归档。tagent 告诉 plan 要做什么，plan 通过 openspec CLI 和文件操作生成 proposal + tasks.md，在 tagent 需要时检查执行情况并反馈进度
- plan agent 拥有 `exec`、`read_file`、`save_file` 工具，通过 `exec` 调用 `openspec new change` / `openspec archive`，通过 `save_file` 更新 tasks.md checkbox
- 新增 `PlanProgressTracker` BeforeModel 回调：读取当前活跃 openspec change 的 tasks.md，注入进度摘要到 tagent 的 LLM 上下文
- FrameworkPrompt 中增加 openspec 计划机制说明，告知 tagent LLM 何时调用 plan agent 创建计划
- 在 `examples/wechat-bot/tagent.yaml` 中注册 plan 子 agent

## Capabilities

### New Capabilities
- `openspec-integration`: plan 子 agent 集成 openspec CLI 做工作计划管理。包括 plan agent 配置、PlanProgressTracker 进度注入回调、FrameworkPrompt 说明

### Modified Capabilities
（无——不修改现有 spec 的需求）

## Impact

- 新增 `examples/wechat-bot/resources/prompts/plan_agent.md`：plan agent 的 system prompt
- 新增 `examples/wechat-bot/resources/prompts/plan_tool_desc.md`：tagent 调用 plan agent 的工具描述
- 新增 `agent/plan_progress_tracker.go`：PlanProgressTracker BeforeModel 回调组件
- 修改 `agent/tagent_agent.go`：FrameworkPrompt 增加 openspec 计划说明；newContextManagerFromConfig 注册 PlanProgressTracker
- 修改 `agent/context_manager.go`：回调链中注册 PlanProgressTracker
- 修改 `config.go`：AgentConfig 新增 `OpenSpecDir` 字段
- 修改 `examples/wechat-bot/tagent.yaml`：注册 plan 子 agent，配置 openspec_dir
- 无新增工具注册——plan agent 复用已有 exec/read_file/save_file 工具

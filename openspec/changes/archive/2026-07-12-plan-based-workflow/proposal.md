## Why

tagent 面对复杂多步骤任务时，LLM 在 ReAct 循环中逐步执行工具调用，没有持久化的工作计划。上下文压缩后 LLM 可能丢失执行意图，用户无法看到进度。项目已有 OpenSpec 机制（proposal → tasks → archive），但目前只在人工开发流程中使用，tagent 运行时无法利用它做计划管理。

## What Changes

- 新增 `plan` 子 agent：作为 tagent 的工具被调用，负责工作计划的完整生命周期管理（创建、更新、查询、归档）。tagent 告诉 plan 要做什么，plan 通过 openspec CLI 和文件操作完成
- plan agent 区分两类操作：**需要过 model 的**（创建计划、更新进度、归档计划——需要 LLM 推理生成内容）和**直接工程逻辑的**（查询进度——直接读文件返回，不过 model）
- plan agent 的 `plan_tool_desc.md` 明确说明调用方式和两种模式，让 tagent LLM 自行判断何时调用
- **删除 PlanProgressTracker BeforeModel 回调**——不需要自动注入进度。tagent 需要时主动调用 `tool_call(plan, {request: "当前进度"})` 询问
- **删除 OpenSpecDir 配置**——openspec 目录由 plan agent 的 workspace 配置决定，不需要在 tagent 层面额外配置
- FrameworkPrompt 中增加工作计划机制说明（不硬编码工具名，描述框架机制）

## Capabilities

### New Capabilities
- `plan-agent`: plan 子 agent 的配置、双模式操作（model/工程直读）、工具描述

### Modified Capabilities
（无——不修改现有 spec 的需求）

## Impact

- 更新 `examples/wechat-bot/resources/prompts/plan_agent.md`：增加双模式说明
- 更新 `examples/wechat-bot/resources/prompts/plan_tool_desc.md`：明确调用方式
- 删除 `agent/plan_progress_tracker.go` 和 `agent/plan_progress_tracker_test.go`
- 删除 `agent/context_manager.go` 中 PlanProgressTracker 回调注册
- 删除 `config.go` 中 `OpenSpecDir` 字段
- 删除 `agent/tagent_agent.go` 中 `OpenSpecDir` 字段和 `FrameworkPrompt` 中的计划段落
- 删除 `tagent.go` 中 `OpenSpecDir` 传递
- 删除 `examples/wechat-bot/tagent.yaml` 中 `openspec_dir` 配置
- 更新 `agent/tagent_agent.go` FrameworkPrompt：描述框架注入机制，不硬编码工具名
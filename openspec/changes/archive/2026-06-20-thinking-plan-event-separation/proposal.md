## Why

当前 `ExtractEventType` 将 `RoleAssistant + ToolCalls` 统一映射为 `action_command`，丢失了 trpcclaw 设计哲学中的"思考层"语义。顶层 tagent 在调用 tool 之前的规划/决策属于思考过程（`thinking_plan`），应独立于工具的实际执行（`action_command`）。这种语义混淆导致：(1) LLM 无法区分 Agent 的决策行为和执行行为；(2) 因果链丢失了规划→执行的层次关系；(3) 压缩时思考类事件和工具执行类事件被同等对待，降低压缩质量。

## What Changes

- **新增** `thinking_plan` 事件类型的实际分类逻辑：`RoleAssistant + ToolCalls → thinking_plan`
- **调整** `ExtractEventType` 推断规则：将 assistant tool_calls 消息从 `action_command` 重新分类为 `thinking_plan`
- **调整** `action_command` 语义范围：仅限 `RoleTool`（工具执行结果），不再包含 assistant 的 tool_calls
- **调整** `IsSpecialEventType` 判断：`thinking_plan` 归类为思考类特殊事件（使用原文全文作为摘要）
- **更新** MemoryPlugin、SummaryPlugin、SmartCompressor 中的事件类型处理逻辑
- **更新** wiki 文档中事件类型推断规则表和所有不一致的描述

## Capabilities

### New Capabilities

- `thinking-plan-event-classification`: 将顶层 agent 的工具调用决策（`RoleAssistant + ToolCalls`）独立分类为 `thinking_plan` 事件类型，与 `action_command`（`RoleTool` 执行结果）分离

### Modified Capabilities

<!-- 无现有 spec -->

## Impact

- `tagent/event/types.go`: `ExtractEventType` 推断规则、`IsSpecialEventType` 判断逻辑
- `tagent/plugin/memory_plugin.go`: MemoryPlugin 使用 `ExtractEventType`，自动继承新分类
- `tagent/plugin/summary_plugin.go`: SummaryPlugin 使用 `ExtractEventType`，自动继承新分类
- `tagent/agent/context_intervention.go`: 事件视图转换中的类型前缀展示
- `tagent/agent/smart_compress.go`: 压缩时摘要生成逻辑中事件类型引用
- `tagent/docs/wiki/agent/agent-architecture.md`: 事件类型推断表
- `tagent/docs/wiki/event/event-architecture.md`: 类型常量与推断规则表
- `tagent/docs/wiki/plugin/plugin-architecture.md`: 事件类型推断规则

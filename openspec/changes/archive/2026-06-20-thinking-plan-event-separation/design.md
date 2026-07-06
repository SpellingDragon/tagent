## Context

当前 `tagent/event/types.go` 中的 `ExtractEventType` 函数将消息按 Role 分类：

```
当前规则:
RoleUser        → external_input
RoleAssistant + ToolCalls → action_command  ← 问题所在
RoleAssistant   → agent_output
RoleTool        → action_command
RoleSystem      → external_input
```

这与 trpcclaw concept.md 的设计哲学不一致。trpcclaw 将事件分为四层：
- **外部事件**：external_input（用户输入）、agent_output（Agent 输出）
- **行为类事件**：action_command（命令执行）
- **思考类事件**：thinking_plan（规划）、thinking_recall（回忆）、thinking_knowledge（知识获取）

关键洞察：**RoleAssistant + ToolCalls 是 Agent 的"思考决策"——Agent 正在考虑调用哪些工具，而非实际执行**。实际的工具执行由 RoleTool 消息承载。当前实现将两者混为一谈，丢失了因果链中的规划→执行层次。

`IsSpecialEventType` 当前仅将 `external_input` 和 `agent_output` 视为特殊事件（使用原文全文）。`thinking_plan` 也应该享受相同待遇——LLM 需要看到 Agent 完整的思考过程来理解上下文脉络。

### 代码变更范围

核心改动点极少——`ExtractEventType` 一行推断规则 + `IsSpecialEventType` 一个 case。其余模块（MemoryPlugin、SummaryPlugin、SmartCompressor）均通过调用这两个函数自动继承新行为。

## Goals / Non-Goals

**Goals:**
- 将 `RoleAssistant + ToolCalls` 的分类从 `action_command` 改为 `thinking_plan`
- `action_command` 语义收窄为仅 `RoleTool`（工具执行结果）
- `thinking_plan` 作为特殊事件类型，摘要使用原文全文
- 所有依赖 `ExtractEventType` / `IsSpecialEventType` 的模块自动适配

**Non-Goals:**
- 不新增 `thinking_recall` / `thinking_knowledge` 的自动推断（需额外语义信息）
- 不改变 `RoleTool` 消息的处理方式
- 不改变 MemoryStore / FullEvent 数据结构
- 不修改 Summarizer 机制

## Decisions

### Decision 1: 仅修改 ExtractEventType 一行

**方案 A（选型）**: 在 `ExtractEventType` 中将 `RoleAssistant + ToolCalls` 返回 `TypeThinkingPlan`

```go
case model.RoleAssistant:
    if len(msg.ToolCalls) > 0 {
        return TypeThinkingPlan  // 变更点：从 TypeActionCommand 改为 TypeThinkingPlan
    }
    return TypeAgentOutput
```

**方案 B**: 在 `ExtractEventType` 中保留 `action_command`，在调用处（MemoryPlugin）做二次判断

**选择 A 的理由**: `ExtractEventType` 是统一的事件分类入口，所有调用方（MemoryPlugin、SummaryPlugin、ContextIntervention）通过它获取一致的事件类型。在入口处修正语义最干净，避免每个调用方重复判断。

### Decision 2: thinking_plan 加入 IsSpecialEventType

`thinking_plan` 描述的是 Agent 的完整思考过程，压缩时应作为特殊事件使用原文全文保留。

```go
func IsSpecialEventType(eventType string) bool {
    switch eventType {
    case TypeExternalInput, TypeAgentOutput, TypeThinkingPlan:
        return true
    default:
        return false
    }
}
```

**理由**: (1) Agent 的工具调用决策包含关键上下文（调用哪个 tool、传了什么参数），截断或摘要化会导致 LLM 丢失决策脉络；(2) 符合 trpcclaw "思考类事件保留完整信息" 的设计哲学。

### Decision 3: 不改变 GenerateEventSummary 行为

`GenerateEventSummary` 中对 `action_command` 的特殊处理（formatToolCallSummary）仅对 `RoleTool` 消息生效。`thinking_plan` 消息（RoleAssistant）因 `IsSpecialEventType` 返回 true，已走原文全文路径。无需修改。

## Risks / Trade-offs

- **[Risk] Token 消耗略微增加**: `thinking_plan` 原文全文比工具调用摘要格式更长
  → Mitigation: `thinking_plan` 事件数量通常远少于 `action_command`（一个 tool_call 对应一个 thinking_plan，但可能对应多个 tool result），且 SmartCompress 在多轮循环中会压缩旧事件。Token 增幅可控。

- **[Risk] 已有数据兼容性**: 历史事件中 `EventType = "action_command"` 的 assistant tool_calls 消息不会被修改
  → Mitigation: 这是不可变的日志数据，语义上仍是"那一刻 Agent 调用了工具"。新分类仅影响新产生的事件。RecallAgent 查询时按 `event_type` 过滤需同时查两种类型。

- **[Trade-off] 类型数量膨胀**: 新增一个活跃使用的类型值
  → Accept: 这是值得的，因为它表达了真实的语义层次。`thinking_plan` 常量已定义只是未使用。

## Migration Plan

1. 修改 `ExtractEventType` 一行（`TypeActionCommand` → `TypeThinkingPlan`）
2. 修改 `IsSpecialEventType` 添加 `TypeThinkingPlan` case
3. 更新 MemoryPlugin 中可能存在的硬编码类型引用（实际上通过 `inferEventInfo` → `ExtractEventType` 自动继承）
4. 运行现有单元测试确认无回归
5. 更新 wiki 文档
6. 无需数据迁移——历史数据不可变，新分类仅对新事件生效

## Open Questions

- 是否需要在 ContextIntervention 的 Phase 1 事件视图中为 `[evt_xxx|thinking_plan]` 提供特殊的视觉标记（如不同的前缀颜色）？当前均使用统一格式 `[evt_xxx|event_type]`，无需变更。

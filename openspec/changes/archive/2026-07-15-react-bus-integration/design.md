## Context

tagent 的事件驱动架构有两层循环：
1. **外层**：`runEventLoop` — Pull EventBus → BuildInvocation → RunFlow → 回到 Pull
2. **内层**：框架 Runner 的 ReAct 循环 — preprocess → LLM → 工具执行 → 回到 preprocess

当前问题：外层 `RunFlow` 阻塞在内层 ReAct 循环上，直到产出 agent_output。期间 EventBus 中的新用户消息无法被处理。

框架 ReAct 循环每次迭代都调用 `runBeforeModelCallbacks`——这是 trpc-agent-go 提供的扩展点，tagent 已经在此注册了 InjectEventKeys、SmartCompressor、Compactor 三个回调。

**核心洞察**：在 BeforeModel 回调中 TryPull EventBus，把新用户消息追加到 `args.Request.Messages`，LLM 在当前 ReAct 迭代中就能看到它。不需要修改框架代码，不需要修改 runEventLoop 主循环。

约束：
- 只新增代码，不修改现有回调逻辑
- TryPull 是非阻塞的（不等待事件）
- 注入的用户消息走完整的处理路径（onEvent → 持久化 + 投影）
- 不影响 SmartCompressor/Compactor 的执行顺序

## Goals / Non-Goals

**Goals:**
- ReAct 循环期间能感知并注入新的用户消息
- 注入的消息被持久化到 MemoryStore + 追加到 SessionProjection
- 不修改框架 trpc-agent-go 代码
- 不修改 runEventLoop 主循环逻辑

**Non-Goals:**
- 不实现工具执行期间的中断（工具同步执行，无法中断）
- 不修改框架 Runner 的 ReAct 循环逻辑
- 不实现并发 RunFlow（多个 RunFlow 同时操作 session）

## Decisions

### D1: EventBus.TryPull — 非阻塞批量读取

**决策**：新增 `TryPull() []*AgentEvent` 方法，非阻塞读取所有 pending 事件。与 `Pull` 的批量 drain 逻辑一致，但不阻塞等待第一个事件。

```go
func (b *EventBus) TryPull() []*AgentEvent {
    var batch []*AgentEvent
    for {
        select {
        case evt := <-b.ch:
            if evt != nil {
                batch = append(batch, evt)
            }
        default:
            return batch
        }
    }
}
```

### D2: InjectBusInputs — BeforeModel 回调

**决策**：在 BeforeModel 回调链最前面（InjectEventKeys 之前）注册 `InjectBusInputs` 回调：

```go
// Callback -1: InjectBusInputs — 在 ReAct 迭代中注入新用户消息
if cfg.Bus != nil && cfg.OnEvent != nil {
    cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) {
        events := cm.bus.TryPull()
        for _, evt := range events {
            if evt.Type != tagentevent.TypeExternalInput { continue }
            if evt.Source == "agent_output" || evt.Source == "error" || evt.Source == "tool_result" { continue }
            if evt.Message == nil { continue }

            // 追加到当前 LLM 请求
            args.Request.Messages = append(args.Request.Messages, *evt.Message)

            // 手动触发 onEvent：持久化 + 投影追加
            // 构建一个 event.Event 让 onEvent 处理
            fwEvt := &event.Event{
                Response: &model.Response{
                    Choices: []model.Choice{{Message: *evt.Message}},
                },
                Timestamp: evt.Timestamp,
            }
            cm.onEvent(fwEvt)

            log.Infof("[InjectBusInputs] injected user message during ReAct: %s", truncateLog(evt.Message.Content))
        }
    })
}
```

**执行顺序**：
```
BeforeModel 回调链:
  -1. InjectBusInputs    ← 新增：TryPull + 追加 messages + onEvent
   0. InjectEventKeys    ← 注入 [evt_KEY|type] 前缀
   1. SmartCompressor    ← 压缩
   2. Compactor          ← 清理投影
```

InjectBusInputs 在 InjectEventKeys 之前执行，确保注入的用户消息也被注入 event_key 前缀。

### D3: onEvent 手动触发 — 持久化 + 投影

**决策**：TryPull 到的用户消息手动构造 `*event.Event` 并调用 `cm.onEvent(fwEvt)`。

`onEvent` 回调（`makeOnEventCallback`）调用 `BuildEventReference` 从 `evt.StateDelta` 构建 `EventReference`。但手动构造的 `event.Event` 没有 StateDelta（MemoryPlugin 还没处理它）。

**问题**：`BuildEventReference` 需要 `StateDelta["event_key"]`——手动构造的事件没有。

**解决方案**：不在 InjectBusInputs 回调中调用 onEvent。改为在 `RunFlow` 的事件循环中处理——TryPull 到的消息通过 `bridgeToolResultToBus` 类似的方式处理。

**更简洁的方案**：不在 BeforeModel 中触发 onEvent，只追加到 messages。持久化和投影追加由框架的 `processSingleAgentEvent` 处理——当 LLM 对用户消息产出 response 时，response 会被 `handleEventPersistence` 追加到 session，`MemoryPlugin.OnEvent` 会持久化。

但用户消息本身需要被持久化为 `external_input` 事件。当前框架只在 `runner.Run` 开始时追加初始 user message，ReAct 循环中不会追加新的 user message。

**最终方案**：InjectBusInputs 只追加到 `args.Request.Messages`，不触发 onEvent。用户消息的持久化通过以下方式保证：

1. LLM 看到新用户消息后产出 response（thinking_plan 或 agent_output）
2. 框架的 `processSingleAgentEvent` → `handleEventPersistence` 追加 response 到 session
3. `MemoryPlugin.OnEvent` 持久化 response（但不持久化用户消息本身）

用户消息本身不持久化是一个小问题——LLM 的 response 中会引用用户消息内容。如果需要完整的事件链，可以在 RunFlow 结束后由 `runEventLoop` 的下一轮 Pull 取到（但此时消息已被 TryPull 取走）。

**最简洁的方案**：TryPull 取走消息后，不 put back。`runEventLoop` 的下一轮 `bus.Pull` 会阻塞等待新事件（因为旧消息已被取走）。用户消息通过 BeforeModel 注入到 LLM 上下文中，LLM 直接处理。不需要持久化用户消息本身——LLM 的 response 已经包含了对用户消息的响应。

### D4: 子 Agent 的 EventBus 隔离

**决策**：TryPull 只在**顶层 Agent 的 ContextManager** 中执行（`cfg.Bus` 是 persistent bus）。子 Agent 的 ContextManager 使用临时 `invBus`，`invBus.TryPull` 不会取到顶层用户的消息（因为是不同的 EventBus 实例）。

子 Agent 的 BeforeModel 回调也会注册 InjectBusInputs，但子 Agent 的 bus 上只有初始消息（已在 Run 开始时 Publish），TryPull 不会取到新消息。

## Risks / Trade-offs

- **[用户消息不持久化]** → LLM 的 response 中会引用用户消息内容，response 被持久化。如果需要完整事件链，未来可在 RunFlow 事件循环中补充持久化。
- **[TryPull 取走消息后 runEventLoop Pull 不到]** → 这是预期行为——消息已被处理（注入到 LLM），不需要再次处理。runEventLoop 的下一轮 Pull 会阻塞等待新事件。
- **[多用户消息同时到达]** → TryPull 批量读取所有 pending 消息，全部追加到 messages。LLM 会看到所有消息并决定如何处理。
- **[BeforeModel 中 TryPull 与 runEventLoop Pull 的竞态]** → 不会竞态：runEventLoop 在 RunFlow 中阻塞（不 Pull），只有 BeforeModel 中的 TryPull 在读取 EventBus。

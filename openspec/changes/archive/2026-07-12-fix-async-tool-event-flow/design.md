## Context

### 异步工具事件的完整流转路径

```
LLM → tool_call(exec, "curl ...")
  │
  ▼ ActionTool.Call() → executeAsync()
  │  返回 TmuxExecResponse{session_id, status:"waiting_async_response"}
  │
  ▼ 框架 FunctionCallResponseProcessor:
  │  创建 tool result event: Role=tool, Content="{session_id, status:...}"
  │  → MemoryPlugin: StoreEvent(action_command, evt_KEY_A)
  │  → onEvent: projection.Append(EventReference{Key:A, Type:action_command, Role:tool})
  │  → LLM 下一轮看到: [..., assistant(tool_call), tool("{waiting_async_response}")]
  │
  ▼ ...tmux 执行完成...
  │
  ▼ TmuxMonitor → handleStateChange()
  │  → InjectMessage(Role: RoleSystem, Content: "[action_tool_result] ...")
  │  → EventBus.Publish(external_input, source="inject")
  │
  ▼ 两条路径之一:
  │
  ├─ 路径 A: ReAct 循环仍在运行
  │  → InjectBusInputs (Callback -1) TryPull 到事件
  │  → evt.Type == external_input → 追加到 args.Request.Messages 末尾
  │  → 问题: Role=system 被原样追加，LLM 视角中是外部指令
  │  → 修复: RoleSystem → RoleUser，让 LLM 正确理解为外部输入
  │
  └─ 路径 B: ReAct 已结束，runEventLoop 下一轮
     → BuildInvocation 合并事件为 user message（RoleUser）
     → 正常流程
```

### 压缩切分边界问题

**当前**：`SegmentMessages` 以 `agent_output`（assistant 无 tool_calls）为边界。

```
messages:
  [system, user("帮我看看这个"), assistant(tool_call=exec), tool(result),
   assistant("我来帮你看看"), assistant(tool_call=read_file), tool(result),
   assistant("内容如下..."), user("继续"), assistant("好的...")]

当前切分 (agent_output 边界):
  segment 0: [system, user("帮我看看这个"), assistant(exec), tool, assistant("我来帮你看看"), assistant(read_file), tool, assistant("内容如下...")]  ← 完成
  segment 1: [user("继续"), assistant("好的...")]  ← 完成
```

问题：一次用户输入 "帮我看看这个" 触发了多轮 tool_call 和多个 agent_output，被切成了多个段。用户可能只发了一条消息但产生了 3 个 segment。

**修正**：以 `RoleUser`（用户输入）为边界。

```
修正后切分 (user input 边界):
  segment 0: [system]  ← 系统消息单独或归入第一段
  segment 1: [user("帮我看看这个"), assistant(exec), tool, assistant("我来帮你看看"), assistant(read_file), tool, assistant("内容如下...")]  ← 一个对话轮次
  segment 2: [user("继续"), assistant("好的...")]  ← 一个对话轮次
```

`KeepRecentTasks=2` 保留最近 2 个用户对话轮次。

### fake_alive 检测分析

Heartbeat 机制：发送 `echo tmux_heartbeat` → 检查输出是否包含 "tmux_heartbeat"。

```
非交互式命令 (curl, ls, find):
  curl https://example.com → 5s 完成
  → tmux pane 中 shell 还活着
  → 输出稳定 (无新输出)
  → heartbeat: echo 被 shell 处理 → "ok"
  → 但命令早就完成了!
  → heartbeat 只证明 shell 活着, 不证明命令在执行

交互式/TUI 命令 (vim, less, top):
  vim file.txt → 用户编辑中
  → 输出稳定 (用户没在打字)
  → heartbeat 会向 vim 发送 echo, 破坏 vim 状态
  → 当前已正确处理: TUI 跳过 heartbeat, 直接 TimedOut
```

**正确的检测逻辑**：
- 非交互式: stable → completed（不进入 fake 检测）
- 交互式/TUI: stable → (更长超时) → timedout（已有的 TUI 逻辑）

## Goals / Non-Goals

**Goals:**
- `InjectBusInputs` 将 `RoleSystem` 转为 `RoleUser`，让异步工具结果被正确理解
- 压缩以 user input 为切分边界，保留最近 N 个对话轮次
- 非交互式命令 stable 后直接完成

**Non-Goals:**
- 不修改 `BuildInvocation` 的合并逻辑
- 不修改 TUI 命令的监控逻辑
- 不修改 heartbeat 机制本身（交互式命令仍需要）

## Decisions

### Decision 1: InjectBusInputs 中 RoleSystem → RoleUser

**选择**: 在 `InjectBusInputs` 追加消息到 `args.Request.Messages` 前，检查 `evt.Message.Role`。如果是 `model.RoleSystem`，创建一个副本并将 Role 改为 `model.RoleUser`。

**理由**: `handleStateChange` 用 `RoleSystem` 注入是因为框架的 ReAct 循环期望 `RoleTool` 有 `ToolCallID` 配对，无法直接用 `RoleTool`。但 `RoleSystem` 在 LLM 上下文中被理解为外部指令/系统消息，不如 `RoleUser` 合适。`[action_tool_result]` 前缀让 LLM 从内容理解这是工具结果。

**实现**: 在 `context_manager.go` 的 InjectBusInputs 回调中：
```go
msg := *evt.Message  // 拷贝避免修改原始事件
if msg.Role == model.RoleSystem {
    msg.Role = model.RoleUser
}
args.Request.Messages = append(args.Request.Messages, msg)
```

### Decision 2: SegmentMessages 以 RoleUser 为边界

**选择**: 修改 `isMessageTaskBoundary` 从检查 `RoleAssistant && no ToolCalls` 改为检查 `RoleUser`。同步修改 `isReferenceTaskBoundary` 从 `TypeAgentOutput` 改为 `TypeExternalInput`。

**理由**: 用户输入是自然的对话轮次边界。每个用户输入关联的 tool calls、results、agent responses 构成一个完整的对话轮次。压缩保留最近 N 个轮次，比保留 N 个 agent_output 更符合用户的直觉。

**实现**: 
```go
// SegmentMessages
func isMessageTaskBoundary(msg *model.Message) bool {
    return msg.Role == model.RoleUser
}

// SegmentReferences
func isReferenceTaskBoundary(ref memory.EventReference) bool {
    return ref.EventType == tagentevent.TypeExternalInput
}
```

注意：system 消息（如 system prompt、摘要批次）不是边界，它们属于当前轮次的一部分。第一个 user 消息之前的 system 消息归入第一个轮次。

### Decision 3: 非交互式命令 stable 后直接完成

**选择**: `detectSessionState` 中，当 `stableDuration >= threshold` 且 `!session.IsInteractive && !session.IsTUI` 时，直接返回 `SessionCompleted`。

**理由**: 非交互式命令输出稳定 = 命令已完成。heartbeat 只证明 shell 活着，不证明命令在执行。继续等待 fakeDeadDuration(150s) 没有意义。

**实现**: 在 `stableDuration >= threshold` 判断后：
```go
if stableDuration >= threshold {
    if !session.IsInteractive && !session.IsTUI {
        // Non-interactive: stable = completed
        session.LastOutput = currentOutput
        session.LastOutputMD5 = currentMD5
        return SessionCompleted
    }
    return SessionStable
}
```

## Risks / Trade-offs

| 风险 | 缓解 |
|------|------|
| RoleSystem→RoleUser 可能影响冥想等 RoleSystem 注入 | 冥想消息也用 RoleSystem 注入，转为 RoleUser 后 LLM 更自然地当作新输入处理，符合预期 |
| 以 user input 为边界可能导致段更长 | `KeepRecentTasks` 默认 2 个轮次，如果单个轮次很长，SmartCompressor 的摘要会处理 |
| 非交互式命令可能输出稳定但实际还在运行 | stable_duration（默认 30s）已足够长；真正需要长时间运行的命令应使用 IsInteractive=true |
## Context

### 异步工具事件的完整流转路径

```
LLM → tool_call(exec, "curl ...")
  │
  ▼ ActionTool.Call() → executeAsync()
  │  返回 TmuxExecResponse{session_id, status:"waiting_async_response"}
  │
  ▼ 框架 FunctionCallResponseProcessor:
  │  创建 tool result event: Role=tool, Content="{session_id, status:...}"
  │  → MemoryPlugin: StoreEvent(action_command, evt_KEY_A)
  │  → onEvent: projection.Append(EventReference{Key:A, Type:action_command, Role:tool})
  │  → LLM 下一轮看到: [..., assistant(tool_call), tool("{waiting_async_response}")]
  │
  ▼ ...tmux 执行完成...
  │
  ▼ TmuxMonitor → handleStateChange()
  │  → InjectMessage(Role: RoleSystem, Content: "[action_tool_result] ...")
  │  → EventBus.Publish(external_input, source="inject")
  │
  ▼ 两条路径之一:
  │
  ├─ 路径 A: ReAct 循环仍在运行
  │  → InjectBusInputs (Callback -1) TryPull 到事件
  │  → evt.Type == external_input → 追加到 args.Request.Messages 末尾
  │  → 问题: Role=system 被当作新 user 消息追加
  │  → LLM 看到: [..., tool("{waiting_async_response}"), system("[action_tool_result]...")]
  │  → system 消息在 LLM 视角中是外部指令，不是工具结果
  │
  └─ 路径 B: ReAct 已结束，runEventLoop 下一轮
     → BuildInvocation 合并事件为 user message
     → 问题: RoleSystem 被当作 external_input → BuildInvocation 合并为 user
     → LLM 看到: [摘要..., user("[action_tool_result]...")]
     → LLM 误以为是用户发来的新消息
```

### 问题根因

**根因 1: Role 不匹配**

`handleStateChange` 用 `RoleSystem` 注入，但 `[action_tool_result]` 本质是工具执行的异步结果——应该是 `RoleTool`。

`InjectBusInputs` 只过滤 `Source == "agent_output"` 和 `Source == "error"`，不过滤 `external_input` 事件。所以 `RoleSystem` 的 `[action_tool_result]` 会被追加到 messages 末尾，被 LLM 当作外部指令而非工具结果。

如果改为 `RoleTool`，`ExtractEventType` 会分类为 `action_command`，`InjectBusInputs` 仍然会拉取它（因为 `Type == external_input` 是 EventBus 的类型，不是 EventType）。但 `InjectBusInputs` 追加到 messages 时，`RoleTool` 的消息会被框架正确处理为工具结果。

**等等——这里需要仔细看 InjectBusInputs 的逻辑**：

```go
// InjectBusInputs 只检查 evt.Type == tagentevent.TypeExternalInput
// evt.Type 是 EventBus 的 AgentEvent.Type，始终是 "external_input"（NewExternalInputEvent 创建）
// evt.Source 是 "inject"
// evt.Message.Role 是注入时的 Role
```

`InjectBusInputs` 把 `evt.Message` 追加到 `args.Request.Messages`。如果 `evt.Message.Role == RoleTool`，框架的 ReAct 循环会把它当作工具结果处理。

但问题是：框架的 ReAct 循环期望 `RoleTool` 消息有对应的 `ToolCallID`（关联到之前的 `tool_call`）。`handleStateChange` 注入的 `RoleTool` 消息没有 `ToolCallID`，框架可能无法正确关联。

**更好的方案**：`handleStateChange` 注入的消息用 `RoleUser`，但内容明确说明是工具结果。这样 `InjectBusInputs` 正确将其作为新的外部输入追加到 messages，LLM 从内容 `[action_tool_result]` 理解这是工具的异步完成通知。

**根因 2: 引导消息误导**

```go
// 当 findPendingUserMessage 返回 nil 时:
result = append(result, model.Message{
    Role:    model.RoleUser,
    Content: "（以上是对话历史摘要。如果有新任务，请告诉我。）",
})
```

"如果有新任务" 暗示之前的任务已结束。应改为不追加任何引导消息——让 LLM 基于摘要和最近段自行判断下一步。

**根因 3: 非交互式命令的 fake_alive 误判**

```
detectSessionState:
  stable (30s 无新输出) → 触发事件
  继续监控...
  fakeDeadDuration (150s) → heartbeat 检测
  heartbeat ok → FakeAlive → 重启命令
```

非交互式命令（`curl`, `ls`, `find`）在输出稳定后应该直接完成。能响应 heartbeat 说明 shell 还活着但命令可能已经执行完了（只是 tmux session 没退出）。

## Goals / Non-Goals

**Goals:**
- `handleStateChange` 注入的事件被 LLM 正确理解为工具结果
- 压缩后不误导 LLM 认为任务已结束
- 非交互式命令在 stable 后直接完成，不进入 fake 检测

**Non-Goals:**
- 不修改 `InjectBusInputs` 的过滤逻辑
- 不修改 `BuildInvocation` 的合并逻辑
- 不修改 TUI 命令的监控逻辑

## Decisions

### Decision 1: handleStateChange 保持 RoleSystem，但调整 InjectBusInputs 行为

**选择**: `handleStateChange` 继续用 `RoleSystem`（因为框架 ReAct 循环期望 `RoleTool` 有 `ToolCallID`）。但 `InjectBusInputs` 在追加 `RoleSystem` 消息时，将其 Role 改为 `RoleUser`，让框架将其作为新的外部输入处理。

**理由**: `[action_tool_result]` 对 LLM 来说是一个新的外部通知（"你的工具调用完成了，这是输出"），不是 ReAct 循环中 tool_call → tool_result 的配对。用 `RoleUser` 让 LLM 将其作为新的输入处理，从内容中的 `[action_tool_result]` 前缀理解这是异步工具结果。

**实现**: 在 `InjectBusInputs` 中，当 `evt.Message.Role == model.RoleSystem` 时，追加到 messages 时改为 `RoleUser`。

### Decision 2: 压缩后不追加引导消息

**选择**: 当 `findPendingUserMessage` 返回 nil 时，不追加任何引导消息。LLM 基于摘要和 recentSegments 自行判断下一步。

**理由**: 追加引导消息（无论内容如何）都是人为干预 LLM 的决策。如果 LLM 看到摘要 + 最近的任务段，它应该能自行判断是否需要继续或等待。不追加比追加错误的引导更好。

### Decision 3: 非交互式命令 stable 后直接完成

**选择**: `detectSessionState` 中，当 `!session.IsInteractive && !session.IsTUI && stableDuration >= threshold` 时，直接返回 `SessionCompleted`，不继续等待 `fakeDeadDuration`。

**理由**: 非交互式命令（curl, ls, find 等）输出稳定意味着命令已执行完。继续等待 150s 再检测 heartbeat 没有意义——能响应 heartbeat 只说明 shell 还在，不代表命令还在执行。

## Risks / Trade-offs

| 风险 | 缓解 |
|------|------|
| RoleSystem→RoleUser 转换可能影响其他 RoleSystem 消息 | 仅在 InjectBusInputs 中转换，且仅影响 EventBus 注入的 external_input 事件；system prompt 不走 EventBus |
| 不追加引导消息可能导致 LLM 不响应 | recentSegments 中如果有未完成的任务，LLM 会看到上下文并继续；如果没有未完成任务，LLM 自然等待新输入 |
| 非交互式命令可能输出稳定但实际还在运行 | stable_duration（30s）已足够长；如果命令真的需要更长时间，应该用 IsInteractive=true |

## Context

本设计整合两个层面的架构修正：**事件消费统一化** + **异步工具事件回写清理**。两者共同回归 `prototype/agent.go` 的核心不变量——单一事件管线、单一消费者、所有输出经事件流。

### 当前问题的具体位置

**wechat-bot 主逻辑（`examples/wechat-bot/main.go`）**：
- L252：`responseCh := make(chan string, 1)` — buffered chan 反模式
- L253-259：`typingActive` / `replyTarget` / `lastUser` — 全局状态承载路由信息
- L307-343：Consumer 中 `switch triggerSource` 混用 `responseCh` 和 `bot.SendTextToUser` 两条路径
- L432-453：Handler 阻塞 `<-responseCh`

**ActionTool（`tool/action/action_tool.go`）**：
- L210：`Call()` 根据 `args.Async` 分派两条路径
- L289-292：async 分支立即返回 `TmuxExecResponse` — 这个响应被框架当作 tool result 送入事件流
- L299-383：`handleStateChange` 每次状态变化都注入事件

**TmuxMonitor（`tool/action/tmux_monitor.go`）**：
- L440-450：`Running → Stable` / `Stable → Running` 都通过 `StateChangeCallback` 触发

## Goals / Non-Goals

**Goals:**

- 消除 wechat-bot 中 `responseCh` / `replyTarget` / `lastUser` 三个"隐式路由通道"
- 事件级 `chat_id` 元数据通过 `event.StateDelta` 沿因果链传播
- Handler 立即返回（不阻塞），Consumer 是唯一发送响应的地方
- ActionTool 恢复纯异步模式，删除 `async` 参数与 sync 分支
- 命令启动时**不产生独立的 tool result 事件**（避免 LLM 产生"我已启动 XX，请稍等"这类冗余响应）
- 仅在 tmux session 达到最终稳定态时触发一次 `InjectMessage`

**Non-Goals:**

- 不改变 tagent 内部的事件管线核心（EventBus / SessionProjection / MemoryStore）
- 不改变 tmux 状态检测算法（monitor 的 detectSessionState 逻辑保持不变）
- 不移除 `IsTmuxAsync()` 标记机制（它是 Runner 过滤 tool result 的关键）
- 不实现多语言/多渠道路由（本 change 只处理 wechat-bot，其他 example 后续单独适配）

## Decisions

### D1: 事件元数据传播链路

**问题**：Handler 在投递用户消息时如何让 Consumer 知道"响应应该发给哪个 chat_id"？

**方案**：利用已有的 `AgentEvent.Metadata` 字段（`event_bus.go` 中已定义），通过 MemoryPlugin 传播到 `event.StateDelta`。因果链上派生事件（thinking_plan / action_command / agent_output）继承源事件的 metadata。

```
InjectMessageWithMetadata(source, msg, {chat_id: "user123"})
  ↓
AgentEvent{Type: "external_input", Metadata: {chat_id: "user123"}}
  ↓ EventBus.Publish
  ↓ runEventLoop.Pull → ContextManager.BuildInvocation
  ↓
Framework Runner 处理 → 生成 event.Event
  ↓ MemoryPlugin.OnEvent 五步协同 (增加第6步)
  ↓ 从当前 batch 的源 AgentEvent 复制 Metadata 到 event.StateDelta
  ↓
Consumer 从 evt.StateDelta["chat_id"] 提取路由信息
```

**关键约束**：metadata 只在 `external_input` 类型的 AgentEvent 中携带用户身份；派生事件从 SessionProjection 或 factCausalChain 继承。为简化实现，MemoryPlugin 在同一次 RunFlow 期间将根事件的 metadata 复制到所有派生事件。

### D2: MemoryPlugin 传播 metadata 的时机

**问题**：`MemoryPlugin.onEvent` 是每个事件独立触发的，如何知道"根源 AgentEvent"是谁？

**方案**：在 `TagentAgent.runEventLoop` 中，取出批量事件后计算根 metadata：

```go
// runEventLoop
events, _ := bus.Pull(ctx)
rootMetadata := extractRootMetadata(events)  // 从 external_input 事件中提取

// 通过 ContextManager 传递到 Runner 的 Invocation
cm.SetInvocationMetadata(rootMetadata)  // 新增字段

cm.RunFlow(ctx, msg)
  ↓ 内部创建 Invocation 时携带 metadata
  ↓ MemoryPlugin.OnEvent 读取 inv.RunOptions.RuntimeState["metadata"] 
  ↓ 复制到 evt.StateDelta（键前缀 "meta_" 避免冲突）
```

**替代方案（更简单）**：由于当前实现中 `AgentEvent.Metadata` 在 EventBus 中已丢失（runEventLoop 消费后不再传递），改为 ContextManager 内维护一个 `currentInvocationMetadata map[string]string`，每次 RunFlow 开始时设置，onEvent 时读取。

选用**简单方案**：ContextManager 层维护 `currentMetadata`，避免大改 Runner 接口。

### D3: Consumer 单一决策逻辑

```go
// wechat-bot/main.go 消费者简化后:
for evt := range outputCh {
    if !evt.IsFinalResponse() { continue }  // 非终响应仅记日志（可选）
    
    content := extractContent(evt)
    triggerSource := extractStateDelta(evt, "trigger_source")
    chatID := extractStateDelta(evt, "meta_chat_id")
    
    // 静默类型：不发送
    if triggerSource == "meditation" || triggerSource == "error" {
        log.Infof("[Consumer] silent trigger=%s content=%s", triggerSource, truncate(content))
        continue
    }
    
    // 必须有 chat_id 才能发送
    if chatID == "" {
        log.Warnf("[Consumer] no chat_id in event metadata, skipping delivery")
        continue
    }
    
    _ = bot.SendTextToUser(ctx, chatID, content)
}
```

**关键**：Handler 完全不再等待响应。用户体验层面：
- 用户发消息 → wechat 展示"正在输入"（typing indicator 由 handler 单独控制）
- Consumer 处理 agent_output 事件时通过 `bot.SendTextToUser` 主动推送
- Handler 早已返回，wechat SDK 层没有阻塞

### D4: ActionTool 纯异步的独立事件抑制

**问题**：`Call()` 返回 `TmuxExecResponse` 后，框架 Runner 会把它当作 tool result 追加到 session，生成一次 LLM 响应"我已启动..."

**方案**：利用已有的 `IsTmuxAsync() bool` 方法。在 tagent 层（`agent/context_manager.go` 的 RunFlow）中：

```go
// RunFlow 中处理 tool result 事件时:
for fwEvt := range eventCh {
    if isToolResult(fwEvt) {
        if hasIsTmuxAsyncMarker(fwEvt) {
            // 抑制事件传播 — 不追加到 Projection、不 emit 到 outputCh
            log.Debugf("[RunFlow] suppressing tmux async placeholder event")
            continue
        }
    }
    // 正常处理...
}
```

**如何标记事件为 tmux async**：`Call()` 返回的对象类型断言为 `interface { IsTmuxAsync() bool }`，如果是 tmux async 响应，其序列化后的 content 中包含 `"status":"waiting_async_response"`。tagent 在 tool response 处理路径识别此模式并抑制。

**替代方案**：让 `Call()` 直接返回一个特殊错误 `ErrTmuxAsyncPending`，framework 层已有此类错误的抑制机制。但会破坏"tool 一定返回结果"的约定，弃用。

选用 **content pattern 抑制**：在事件写入 outputCh / Projection 前，判断 content 是否为已知的 tmux async 占位符（`{"status":"waiting_async_response","session_id":"..."}`），是则跳过。

### D5: TmuxMonitor 状态过滤

**核心规则**：只有以下 4 种状态被认为是"稳定态"，触发 `handleStateChange`：

| 状态 | 含义 | 触发注入？ |
|------|------|-----------|
| `SessionRunning` | 命令正在运行且输出有变化 | 否（中间态） |
| `SessionStable` | 输出稳定（无变化超过 stable_duration） | **是** |
| `SessionCompleted` | 命令退出 | **是** |
| `SessionError` | tmux session 出错 | **是** |
| `SessionTimedOut` | TUI 超时未变化 | **是** |
| `SessionFakeDead` | 假死中间态（output 未变） | 否 |
| `SessionFakeAlive` | 假死恢复中间态 | 否 |

**实现**：在 `tmux_monitor.go` 的 `checkSession` 中，只有当 `newStatus` 是稳定态**且** `oldStatus != newStatus` 时，才调用 `StateChangeCallback`：

```go
func isStableState(s SessionStatus) bool {
    return s == SessionStable ||
           s == SessionCompleted ||
           s == SessionError ||
           s == SessionTimedOut
}

// 状态变化时
if newStatus != oldStatus && isStableState(newStatus) {
    tm.stateChangeCallback(sessionID, oldStatus, newStatus, output)
}
```

**特殊情况**：从 Stable 回到 Running（假死恢复输出变化）不注入事件。从 Stable 再次进入 Stable（同状态）不重复注入。

## Risks / Trade-offs

- **[R1] metadata 键名冲突**：`event.StateDelta` 中已有 `event_key` / `partition_id` / `event_type` / `event_summary`。metadata 键名需加前缀 `meta_` 避免冲突。
- **[R2] 事件传播路径变化**：Consumer 直接调 `bot.SendTextToUser` 而非通过 responseCh，可能影响 typing indicator 的时序。缓解：typing indicator 由 handler 在 InjectMessage 后立即启动，consumer 发送响应时通过独立信号（如 `evt.RequiresCompletion`）通知 handler 停止。
- **[R3] ActionTool 纯异步破坏简单命令的即时响应**：`ls /tmp` 这类快速命令也走 tmux + 稳定态检测，可能延迟 30s（tmux stable_duration 默认）。缓解：ActionTool 检测到命令快速退出（Completed 而非 Stable）时立即触发注入，延迟由 `interval`（默认 3s）决定。
- **[R4] tmux 未安装的部署环境**：删除 sync 分支后，无 tmux 环境完全无法执行命令。缓解：在 tagent.New 启动时检测 tmux 可用性，未安装时报错并给出安装建议（`brew install tmux`）。
- **[R5] `IsTmuxAsync()` 过滤依赖 content pattern**：如果 tmux 返回值格式变化，过滤失效。缓解：定义常量 `TmuxAsyncPlaceholderStatus = "waiting_async_response"`，所有代码引用此常量而非硬编码字符串。

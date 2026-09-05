## 能力: stable-state-tool-notification

ActionTool 恢复纯异步模式；命令启动时不产生独立事件；只在 tmux session 达到稳定态时通过 InjectMessage 回写事件。

## 需求

### ActionArgs 简化

```go
// 简化后的 ActionArgs（删除 Async 字段）
type ActionArgs struct {
    Command string            `json:"command"`
    Timeout int               `json:"timeout,omitempty"`
    WorkDir string            `json:"work_dir,omitempty"`
    Env     map[string]string `json:"env,omitempty"`
    IsTUI   bool              `json:"is_tui,omitempty"`
}
```

### Call() 行为

```go
func (ct *ActionTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
    args, err := parseArgs(jsonArgs)
    if err != nil { return nil, err }
    
    // 直接走异步路径 — 不再有 sync 分支
    if ct.tmuxExecutor == nil {
        return nil, fmt.Errorf("action: tmux not available (install tmux: `brew install tmux`)")
    }
    
    return ct.executeAsync(ctx, args)
}
```

### executeAsync 返回值

保持返回 `TmuxExecResponse{SessionID, Status: "waiting_async_response"}`。此返回值被 tagent 层识别为 tmux async 占位符并**从事件流中抑制**：

```go
// TmuxAsyncPlaceholderStatus is the well-known status string used to
// identify tmux async placeholder responses. Events with this status
// in their content are suppressed from outputCh and Projection.
const TmuxAsyncPlaceholderStatus = "waiting_async_response"
```

### tagent 层的事件抑制

在 `agent/context_manager.go` 的 `RunFlow` 中，处理 tool result 事件时判断：

```go
func isTmuxAsyncPlaceholder(evt *event.Event) bool {
    if evt == nil || evt.Response == nil { return false }
    for _, choice := range evt.Response.Choices {
        if choice.Message.Role == model.RoleTool {
            if strings.Contains(choice.Message.Content, TmuxAsyncPlaceholderStatus) {
                return true
            }
        }
    }
    return false
}

// 在 for fwEvt := range eventCh 循环中:
if isTmuxAsyncPlaceholder(fwEvt) {
    log.Debugf("[RunFlow] suppressing tmux async placeholder")
    continue  // 不追加到 Projection，不 emit 到 outputCh，不 publish 到 bus
}
```

### TmuxMonitor 状态过滤

`tmux_monitor.go` 的 `checkSession` 中：

```go
// isStableState returns true for terminal or stable states that warrant
// event injection. Intermediate states (Running, FakeDead, FakeAlive) are
// suppressed to avoid event noise.
func isStableState(s SessionStatus) bool {
    switch s {
    case SessionStable, SessionCompleted, SessionError, SessionTimedOut:
        return true
    default:
        return false
    }
}

// 状态变化时的回调触发条件:
if newStatus != oldStatus && isStableState(newStatus) && tm.stateChangeCallback != nil {
    tm.stateChangeCallback(sessionID, oldStatus, newStatus, output)
}
```

### handleStateChange 简化

`action_tool.go` 的 `handleStateChange` 保持不变（继续构建 `[action_tool_result]` 消息 + 调 `InjectMessageWithSource("async_result", ...)`），但由于 Monitor 已过滤，此函数只会在稳定态被调用。

### 快速命令的即时响应

- 如果命令快速退出（<3s，早于 stable_duration），Monitor 会先检测到 `SessionCompleted` 状态并触发注入
- 用户体验：`ls /tmp` 类命令在 3-5s 内返回结果
- 长命令（如 `tail -f`）在 stable_duration（默认 30s）后触发 Stable 注入

### tool description 更新

`resources/prompts/action_tool_desc.md` 移除所有 async 参数相关说明，改为强调：

```
所有命令通过 tmux 异步执行。调用后立即返回，结果在命令稳定或完成后通过
[action_tool_result] 事件到达。不要连续调用相同命令等待结果——事件到达
时会自动进入你的上下文。
```

### 约束

- 无 tmux 环境时 Call() 返回明确错误（不再 fallback 到 sync exec）
- 中间态（Running / FakeDead / FakeAlive）永不触发 InjectMessage
- 同一 session 同一稳定态不重复触发（去重）
- `[action_tool_result]` 消息内容包含足够信息供 LLM 判断（命令、状态转换、输出摘要）


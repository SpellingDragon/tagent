## Why

异步工具执行结果的事件流和监控机制存在三个问题，导致 LLM 上下文质量退化、任务丢失、命令被误重启：

1. **`handleStateChange` 注入的 `[action_tool_result]` 事件 Role 不正确**：当前用 `RoleSystem` 注入，被 `InjectBusInputs` 当作新的用户消息追加到 messages 末尾。应该在 `InjectBusInputs` 中将 `RoleSystem` 转为 `RoleUser`，让 LLM 从内容前缀理解这是异步工具结果。

2. **压缩应以 user input 为切分边界，保留最近 N 个对话轮次**：当前 `SegmentMessages` 以 `agent_output`（assistant 无 tool_calls 的 final response）为边界，导致一次用户输入被切成多个段。应以 `RoleUser` 消息为边界——每次用户发一条消息就是一个"对话轮次"，压缩保留最近 N 个完整轮次。

3. **非交互式命令的 fake_alive 检测逻辑错误**：当前所有命令都走 `stable → fakeDeadDuration(150s) → heartbeat` 检测。但非交互式命令（curl, ls, find）输出稳定意味着命令已完成。Heartbeat 只证明 shell 还活着（`echo tmux_heartbeat` 被 shell 处理），不证明命令还在执行。非交互式命令 stable 后应直接完成，不应进入 fake 检测。

## What Changes

- **`InjectBusInputs` 转换 RoleSystem → RoleUser**：追加消息前检查 Role，`RoleSystem` 转为 `RoleUser`，让 LLM 正确理解异步工具结果
- **压缩以 user input 为切分边界**：`SegmentMessages` 和 `SegmentReferences` 改为以 `RoleUser` 消息/`external_input` 事件为边界，`KeepRecentTasks` 改为保留最近 N 个用户对话轮次
- **非交互式命令 stable 后直接完成**：`detectSessionState` 中非交互式非 TUI 命令在 stable 后返回 `SessionCompleted`，不进入 fake 检测

## Capabilities

### New Capabilities
- `async-tool-event-fix`: 修复异步工具结果 Role、压缩切分边界、非交互式命令完成判定

### Modified Capabilities
（无）

## Impact

- 修改 `agent/context_manager.go`：`InjectBusInputs` 中 `RoleSystem` → `RoleUser` 转换
- 修改 `agent/task_segmenter.go`：`SegmentMessages` 以 `RoleUser` 为边界
- 修改 `tool/action/tmux_monitor.go`：非交互式命令 stable 后直接 `SessionCompleted`

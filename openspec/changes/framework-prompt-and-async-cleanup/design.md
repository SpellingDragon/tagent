## Context

tagent 的事件驱动架构中，异步工具（如 exec/tmux）的执行流程应为：

1. LLM 调用工具 → 工具创建 tmux session → 返回 tool result（命令已提交）
2. LLM 看到 tool result，正常思考（可继续调其他工具或产生回复）
3. tmux 完成 → TmuxMonitor 检测到状态变更 → `handleStateChange` → `InjectMessage` → EventBus
4. tagent 主 agent 的 `runEventLoop` 拉取事件 → `RunFlow` → LLM 看到 `[action_tool_result]`

当前问题：
- `AsyncTaskChecker` 让 action 子 agent 的 `Run()` 阻塞等待 tmux 完成——这与非阻塞设计意图不符
- `handleStateChange` 的 `InjectMessage` 在 `Run()` 期间注入到 `invBus`（临时 bus），action 子 agent 退出后 invBus 被丢弃——完成事件可能丢失
- LLM 不理解 tagent 的事件机制，看到中间状态后退化循环

## Goals / Non-Goals

**Goals:**
- 在 system prompt 前面注入框架运行时说明，让 LLM 理解事件机制
- 删除 AsyncTaskChecker，Run() 恢复非阻塞语义
- `handleStateChange` 注入到 tagent 主 agent 的 persistentBus（而非 action 子 agent 的 invBus）
- 简化 `waiting_async_response` 返回值

**Non-Goals:**
- 不改变 TmuxMonitor 的轮询检测机制
- 不改变 tmux session 的状态检测逻辑（stable/completed/timedOut）
- 不改变框架 ContentRequestProcessor 的消息构建逻辑
- 不改变 `InjectBusInputs` 的 TryPull 机制

## Decisions

### Decision 1: 框架 system prompt 注入

**选择**: 在 `ContextManager` 构建 system prompt 时，在用户配置的 prompt 之前插入框架说明。

**理由**: LLM 需要理解 tagent 的异步工具机制、事件标识格式、上下文压缩等，才能正确处理中间状态和完成事件。这段说明是框架层面的，不应由用户手动配置。

**注入位置**: `ContextManagerConfig.SystemPrompt` 在传给框架 LLMAgent 之前，前置框架说明。

**框架说明内容**:
```
# tagent 运行时说明

你运行在 tagent 事件驱动框架中。以下机制影响你的工具调用和上下文管理:

## 异步工具

某些工具（如 exec）异步执行。调用后返回 session_id 和状态标识，
不代表执行完成。命令完成时，框架会发送 [action_tool_result] 事件
到你的上下文中，包含完整输出。收到结果前，不要重复调用同一命令。

## 事件标识

每条消息前的 [evt_KEY|type] 标记是事件追踪标识。
使用 recall 工具可通过 key 检索完整事件内容。

## 上下文压缩

当上下文接近 token 上限时，框架自动压缩旧对话段。
压缩后的摘要以 system 消息形式呈现，被压缩的事件 key 列表在摘要中列出。
完整内容可通过 recall 工具检索对应 key 获取。
```

### Decision 2: 删除 AsyncTaskChecker

**选择**: 删除 `AsyncTaskChecker` 接口、`RegisterAsyncTaskChecker`、`hasPendingAsyncTasks`，以及 `Run()` 中的 `hasPendingAsyncTasks()` 检查。

**理由**: 设计意图是非阻塞的——action 子 agent 正常返回，tmux 完成事件注入到 tagent 主 agent。`AsyncTaskChecker` 让 `Run()` 阻塞，与设计意图矛盾，且导致了 `persistentBus` 重定向等复杂问题。

**删除内容**:
- `AsyncTaskChecker` 接口
- `TagentAgent.asyncTaskCheckers` 字段
- `RegisterAsyncTaskChecker` 方法
- `hasPendingAsyncTasks` 方法
- `Run()` 中的 `if ta.hasPendingAsyncTasks()` 检查
- `Run()` 中的 `persistentBus` 临时重定向逻辑
- `ActionTool.HasPendingAsyncTasks` 方法
- `tagent.go` 中的 `RegisterAsyncTaskChecker` 调用

### Decision 3: handleStateChange 注入到 tagent 主 agent

**选择**: `ActionTool.SetMessageInjector(ta)` 中 `ta` 改为 tagent 主 agent（entry agent），而非 action 子 agent。

**实现**: 在 `tagent.go` 的 `New()` 中，entry agent 构建完成后，遍历所有 agent 的 ActionTool，重新设置 injector 为 entry agent。

**理由**: action 子 agent 通过 `Run()` 调用，退出后 invBus 被丢弃。tmux 完成事件应注入到 tagent 主 agent 的 persistentBus，由主 agent 的 `runEventLoop` 消费。

**事件流（修复后）**:
```
tagent 主 agent → tool_call(action) → action 子 agent Run()
  → action 子 agent 内部: LLM → exec → {session_id, status}
  → action 子 agent 返回 finalOutput → tagent 主 agent 看到 tool result
  → tagent 主 agent 继续 ReAct (可以调其他工具或回复用户)

  ...tmux 完成...
  → handleStateChange → InjectMessage(tagent主agent)
  → tagent 主 agent 的 persistentBus
  → runEventLoop 的 bus.Pull 拉取
  → RunFlow → InjectBusInputs → LLM 看到 [action_tool_result]
  → LLM 基于结果继续思考
```

### Decision 4: 简化 waiting_async_response

**选择**: `TmuxExecResponse` 的 `Message` 字段简化为简短英文标识。

**理由**: 框架 system prompt 已经说明了异步机制，tool result 不需要重复说明。简短标识足够 LLM 识别。

**修改**:
```go
return &TmuxExecResponse{
    SessionID: session.ID,
    Status:    "waiting_async_response",
}, nil
```

## Risks / Trade-offs

- [action 子 agent 返回后 tmux 完成事件注入到 tagent 主 agent] → tagent 主 agent 的 `InjectBusInputs` 在下一轮 LLM 调用前拉取 → 缓解：这是设计预期，`InjectBusInputs` 已经能拉取 bus 中的新事件
- [删除 AsyncTaskChecker 后 action 子 agent 立即返回] → tagent 主 agent 看到 "命令已提交" → 缓解：框架 system prompt 告知 LLM 等待 `[action_tool_result]` 事件
- [多个 tmux session 同时完成] → 多个 `[action_tool_result]` 事件同时到达 → 缓解：`InjectBusInputs` 批量拉取，LLM 一次看到所有完成结果

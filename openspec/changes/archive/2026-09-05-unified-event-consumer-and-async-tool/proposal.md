## Why

当前 tagent 与其示例应用（wechat-bot）之间的事件消费机制违背了原型的"单一事件管线"设计思想，同时 ActionTool 的 sync/async 双模式引入了不必要的复杂性和事件噪音：

### 问题一：wechat-bot 的双通道反模式

- `outputCh`（tagent 事件管线）与 `responseCh`（Handler-Consumer 缓冲区）并存，形成两条决策路径
- Handler 阻塞等待 `responseCh`，违背事件驱动语义
- `responseCh` 是 `cap=1` buffered channel + `select default` 组合——在没有 Handler 等待时，`case responseCh <- content:` 仍会成功（因为 buffer 未满），fallback 分支永不触发。这是本次日志中"async_result 消息未发送给用户"的根因
- `replyTarget` / `lastUser` 用全局 atomic pointer 记录"当前该发给谁"，多用户场景下必然串线

### 问题二：ActionTool sync/async 双模式带来的架构负担

- 当前 `Call()` 根据 `async` 参数走两条路径：sync 阻塞 60s，async 走 tmux
- Async 模式下 `Call()` 立即返回 `TmuxExecResponse{status:"waiting_async_response"}`，这个响应被框架当作 tool result 触发 LLM 生成"我已经启动异步任务，稍等"的中间响应，产生冗余事件
- 每次 tmux 状态变化都通过 `handleStateChange` 注入 `[action_tool_result]` 事件——包括中间态（Running→Stable、Stable→Running），造成事件噪音

### 修复方向

1. **架构层面**：让 wechat-bot 只从 `outputCh` 消费事件，Handler 只做投递不阻塞。事件级 chat_id 元数据通过 `event.StateDelta` 因果链传播，Consumer 作为唯一决策点。
2. **工具层面**：ActionTool 恢复为纯异步模式，`Call()` 立即返回但**不产生独立响应事件**（避免 LLM 中间响应），只在 tmux session 达到**最终稳定状态**（Stable / Completed / Error / TimedOut）时才通过 `InjectMessage` 注入 `[action_tool_result]` 到事件管线。

两项变更共同回归原型的"单一事件管线"设计思想。

## What Changes

### 变更 A：事件元数据传播 + 统一消费者

- **tagent 新增 API**：`InjectMessageWithMetadata(source string, msg model.Message, metadata map[string]string)`。metadata 写入 `AgentEvent.Metadata`
- **MemoryPlugin 传播 metadata**：`onEvent` 时将 `AgentEvent.Metadata` 复制到 `event.StateDelta`（保持因果链）
- **AppendEventHook 保留 metadata**：Framework Session 追加事件时保留 metadata
- **wechat-bot 重构**：
  - 删除 `responseCh`、`replyTarget`、`lastUser`
  - Handler 只做 `InjectMessageWithMetadata(source, msg, {chat_id, user_name})` 后立即返回
  - Consumer 从 `evt.StateDelta["chat_id"]` 提取 chat_id，通过 `bot.SendTextToUser(chatID, content)` 主动发送
  - Consumer 单一决策：`meditation` → 静默；其他 trigger → 发送给对应 chat_id

### 变更 B：ActionTool 纯异步模式

- **删除 `async` 参数**：从 `ActionArgs` 中移除，从 tool description 中移除相关说明
- **删除 `executeSync` 分支**：所有命令统一走 tmux
- **`Call()` 返回值改为空**：不返回 `TmuxExecResponse{session_id, ...}`，而是返回一个通知性字符串（如 `"已在 tmux 会话中启动，结果稍后通过 [action_tool_result] 事件返回"`），并且这个字符串**不作为 tool result 触发 LLM 响应**——通过 tagent 层的机制过滤（例如 `Call()` 返回值的 `IsTmuxAsync()` 已存在，需增强其在 Runner 中的过滤效果）
- **TmuxMonitor 状态过滤**：`handleStateChange` 只在**最终稳定态**触发注入：
  - Stable（输出稳定，命令还在运行但无输出变化）
  - Completed（命令退出）
  - Error（tmux session 出错）
  - TimedOut（TUI 假死超时）
- **过滤中间态**：Running→Stable、Stable→Running（假死超时后恢复）等中间状态不触发注入

### 变更影响

- **BREAKING**：`ActionArgs.Async` 字段删除；`ta.InjectMessageWithSource` 保留但增加 `InjectMessageWithMetadata`
- **BREAKING**（wechat-bot）：`responseCh` 相关代码全部重写
- 现有集成测试需要适配（`tool/action/*_test.go` 中 async=true 的用例改为默认行为）

## Capabilities

### New Capabilities

- `event-metadata-propagation`: 事件级元数据传播机制。InjectMessage 时携带的 metadata 通过 EventBus → AgentEvent → MemoryPlugin → StateDelta 完整传播到因果链上的所有派生事件
- `unified-output-consumer`: wechat-bot 单一消费者架构。删除 responseCh，Consumer 作为唯一决策点从事件 metadata 中提取路由信息主动发送响应
- `stable-state-tool-notification`: ActionTool 只在稳定状态回写事件。中间态（Running/Stable-Running 切换）不产生事件，仅最终稳定态（Stable/Completed/Error/TimedOut）触发 InjectMessage

### Modified Capabilities

- `action-tool-config`: 删除 `async` 参数，ActionTool 恢复为纯异步模式
- `async-tool-event-fix`: `Call()` 返回值不再作为 tool result 触发 LLM 响应（通过 IsTmuxAsync 标记 + Runner 过滤）
- `persistent-event-loop`: `InjectMessageWithSource` 增加同类 `InjectMessageWithMetadata` API

## Impact

### 代码变更

- **tagent 层**：
  - `agent/event_bus.go`：`AgentEvent.Metadata` 已存在，无需修改
  - `agent/tagent_agent.go`：新增 `InjectMessageWithMetadata` 方法
  - `plugin/memory_plugin.go`：在 `onEvent` 中将 `AgentEvent.Metadata` 写入 `event.StateDelta`（去重与 EventKey/EventType 等已有字段冲突）
  - `agent/tool_agent.go`（如需）：AgentToolWrapper 传递 metadata 到子 agent 的 Invocation

- **ActionTool 层**：
  - `tool/action/action_tool.go`：
    - 删除 `ActionArgs.Async` 字段
    - 删除 `executeSync` 方法
    - `Call()` 直接调用 `executeAsync`
    - `Call()` 返回值改为 `TmuxExecResponse`，但通知性文案更明确（"tmux session {ID} 已启动，等待稳定后返回结果"）
    - 修改 tool description prompt（移除 async 相关说明）
  - `tool/action/tmux_monitor.go`：`handleStateChange` 增加状态过滤逻辑

- **wechat-bot 层**：
  - `examples/wechat-bot/main.go`：
    - Handler 简化为投递 metadata + 立即返回
    - Consumer 从事件提取 chat_id 直接发送
    - 删除 `responseCh` / `replyTarget` / `lastUser`

### 测试

- 更新 `tool/action/*_test.go`：所有传 `async: true` 的用例改为默认行为（因为已是唯一模式）
- 新增 `tests/event_metadata_test.go`：验证 metadata 在事件因果链上的传播
- 新增 `tests/multi_user_dispatch_test.go`（wechat-bot）：验证多用户并发消息各自路由到正确 chat_id

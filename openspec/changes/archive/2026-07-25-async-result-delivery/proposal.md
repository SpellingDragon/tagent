## Why

两个已核实的缺陷让异步委托体验事实上是断的：

1. **退化空 turn 被当正式输出投递**。一个以 `assistant{content="", tool_calls=0}` 结束的 turn 仍被判为 final response 并 echo 成 `agent_output`，消费端再兜成 `"(empty response)"` 发给用户——纯噪声，且迫使用户手动"继续"。
2. **后台任务结算后结果送不回用户**。当 `task_settled` 事件驱动一个回收 turn 时，该 turn 丢失了发起轮的路由身份（`chat_id`），且消费端把 `task` 当"未知触发源"只 log 不下发。于是"交给后台异步处理"的最终答复被静默丢弃。

两者共同的根源是**持久循环的"turn 输出契约"不完整**：既没规定"退化空响应不产出"，也没规定"后台结果如何回到发起会话"。

## What Changes

- **修复"任务看板从未注入"的构造顺序 bug**（根因级）：看板 BeforeModel 回调此前被**构造期守卫** `if cm.taskController != nil` 挡掉（`taskController` 在 ContextManager 构造后才 wiring），导致 LLM **从未看到后台任务看板**——空/困惑响应与重复 spawn 的一个根因诱因。改为**无条件注册回调、运行时判 nil**，恢复"每轮注入活跃任务看板"（本被 spec 要求却从未生效）的行为。
- **退出任务清理 + 会话资源回收**（补齐 async 机制的最后一环）：`TaskManager.tasks` 此前从不清理已退出任务——常驻进程内存/**会话资源**无界泄漏 + 每轮 `List()`/去重 O(N)。改为**状态驱动清理**：稳态只保留存活任务（running/stable/alive_detached/suspect），已退出任务（completed/failed/cancelled/dead）被惰性清除；清除时 SHALL 先 `detector.Cancel()` **回收底层资源**（goroutine/context/tmux 会话）再删 map，非仅删引用。短 grace TTL 仅保回收 turn 内 `get_task_result` 可用 + 兜底（不止靠 TTL）。结果已随 `task_settled` 入历史，清理不丢信息。
- **抑制退化空 turn**：final assistant 响应当 `content` 为空**且**无 `tool_calls` 时，SHALL NOT echo `agent_output`。持久循环随后在 `Pull` 上自然阻塞，等下一个外部/monitor 事件（如 `task_settled`）恢复——这正是"空+挂起"场景所需的语义。**不**为"空+空闲（真卡住）"引入框架兜底 nudge（保持纯粹，视为模型/prompt 质量问题）。
- **spawn 时快照发起轮来源上下文（不透明 baggage）**：在 RunFlow 注入 task spawner 的边界处捕获当轮 `GetInvocationMetadata()`（含 `chat_id` 等）的副本，作为**不透明 baggage** 存入 Task；任务层只忠实透传、不读取/解释（不知道 `chat_id`/路由），工具无感（继续裸调 `Spawn`）。
- **结算事件携带路由 metadata**：`task_settled` 事件及其驱动的回收 turn 的所有事件 SHALL 携带发起轮的路由 metadata，使 `meta_chat_id` 一路存活到输出事件。
- **`task` 成为一等可路由触发源**：消费端 SHALL 将 `task` 触发源的最终输出投递回被捕获的原始会话（wechat-bot example 新增 `task` 分支）。
- 消费端不再下发 `"(empty response)"`。

## Capabilities

### New Capabilities
- `async-result-delivery`: 后台任务结算结果回递到发起会话——覆盖 spawn 时路由 metadata 捕获、随 settle 透传至回收 turn、`task` 作为可路由触发源、消费端投递语义。

### Modified Capabilities
- `persistent-event-loop`: 收紧 turn 输出契约——退化空 final 响应不产出 `agent_output` echo，循环依赖下一个 monitor/外部事件恢复（既有"`task_settled` 排队至下一轮"语义不变）。
- `task-registry-and-board`: (1) 补回归保证——看板注入 SHALL 不受"`taskController` 在 ContextManager 构造后才 wiring"的顺序影响（修构造期守卫 bug，使既有"每轮注入看板"要求真正生效）；(2) 新增退出任务的状态驱动清理 + 资源回收（清理前 `Cancel` 底层 goroutine/context/会话），根治 registry 与会话资源无界泄漏。

## Impact

- **框架**：`agent/context_manager.go`（任务看板回调改为无条件注册、运行时判 `taskController`；RunFlow echo 守卫收紧 + `isFinalResponse` 退化判定；spawner 注入处包 wrapper 快照 origin baggage）、`agent/task_manager.go`（`TaskSpec`/`Task` 承载不透明来源 baggage `Origin`；wrapper 捕获；退出任务状态驱动清理 + `detector.Cancel()` 资源回收）、`agent/event_bus.go`（`newTaskSettledEvent` 逐键拷入 metadata）。
- **示例**：`examples/wechat-bot/main.go`（新增 `task` 触发源投递分支；移除 `"(empty response)"` 下发）。
- **兼容性**：无公共 API 破坏；`TaskSpec` 新增一个可选字段 `Origin`（零值即旧行为）。
- **测试**：RunFlow 空响应抑制、spawn→settle metadata 透传、消费端 `task` 路由。
> 注：本变更方案已被 unified-event-projection（2026-07-25 归档）吸收取代——异步结果作为通知类 input 事件的设计在该变更中完整落地。

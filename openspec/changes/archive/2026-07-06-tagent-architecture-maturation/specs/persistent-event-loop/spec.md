## MODIFIED Requirements

### Requirement: Loop goroutine 持续运行不退出

Loop goroutine SHALL 循环执行：`bus.Pull(ctx)`（阻塞等第一个事件 + non-blocking drain 剩余）→ `ContextManager.BuildInvocation` 合并为一条 model.Message → `ContextManager.RunFlow` 调用框架 Runner → 转发事件到 outputCh + bus → 回到 Pull。Loop SHALL NOT 在 RunFlow 结束（event channel 关闭）时退出。Loop SHALL 仅在 ctx 被取消（StopLoop）时退出。

Loop SHALL NOT 执行任何 trajectory 采集、reward 计算或 trajectory 存储逻辑。Loop 仅负责事件转发、日志记录和 OTLP span 属性设置。

#### Scenario: RunFlow 结束后继续 Pull

- **WHEN** ContextManager.RunFlow 返回的 event channel 关闭（Flow 在 IsFinalResponse 时 break）
- **THEN** Loop 不退出
- **AND** Loop 回到 bus.Pull 等待下一批事件

#### Scenario: StopLoop 终止 Loop

- **WHEN** 调用 StopLoop()
- **THEN** loopCtx 被取消
- **AND** Loop goroutine 退出
- **AND** outputCh 被关闭

#### Scenario: Loop 不采集 trajectory

- **WHEN** Loop 处理完一个 batch 的事件
- **THEN** 不创建 Trajectory 记录
- **AND** 不调用任何 RewardFunc
- **AND** 不调用任何 TrajectoryStore.Add
- **AND** 仅记录日志（batch 完成、duration、events、tokens）和 OTLP span 属性

### Requirement: 批量 drain EventBus

Loop SHALL 阻塞等待 EventBus 的第一个事件，然后 non-blocking drain 所有后续 pending 事件。`bus.Pull(ctx)` 返回 `[]*AgentEvent`，保证至少包含 1 个事件（或 ctx 取消时返回 error）。

#### Scenario: 单事件 drain

- **WHEN** EventBus 中只有 1 个 pending 事件
- **THEN** Pull 返回 `[]*AgentEvent{evt1}`
- **AND** EventBus 为空

#### Scenario: 批量 drain 多事件

- **WHEN** Pull 时 EventBus 中有 evt1、evt2、evt3
- **THEN** Pull 返回 `[evt1, evt2, evt3]`
- **AND** EventBus 为空

#### Scenario: ctx 取消

- **WHEN** ctx 被取消且 EventBus 为空
- **THEN** Pull 返回 nil 和 ctx.Err()

### Requirement: 工具结果 Publish 到 EventBus

ContextManager.RunFlow 在转发框架 Runner 事件流时，对于 EventType 为 `action_command` 的事件，SHALL 额外 `bus.Publish` 一个 `AgentEvent{Type: "external_input", Source: "tool_result"}`。该桥接使外部监听器可消费工具结果事件。其他事件类型（`thinking_plan`、`agent_output`）不桥接到 EventBus。

#### Scenario: action_command 事件桥接到 EventBus

- **WHEN** RunFlow 转发事件流中出现 EventType == "action_command" 的事件
- **THEN** bus.Publish 发布 AgentEvent{Type: "external_input", Source: "tool_result", Message: {Content: 事件摘要}}
- **AND** 事件同时追加到 SessionProjection（经 onEvent 回调）
- **AND** 事件同时转发到 outputCh

#### Scenario: thinking_plan 事件不桥接

- **WHEN** RunFlow 转发事件流中出现 EventType == "thinking_plan" 的事件
- **THEN** 不发布到 EventBus
- **AND** 事件仅追加到 SessionProjection 和转发到 outputCh

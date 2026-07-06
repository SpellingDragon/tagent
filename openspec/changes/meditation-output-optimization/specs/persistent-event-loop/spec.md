## MODIFIED Requirements

### Requirement: Loop goroutine 持续运行不退出

Loop goroutine SHALL 循环执行：`bus.Pull(ctx)` → `ContextManager.BuildInvocation` → `ContextManager.RunFlow` → 回到 Pull。Loop SHALL NOT 在 RunFlow 结束时退出。Loop SHALL 仅在 ctx 被取消时退出。

**RunFlow 中的 outputCh 写入策略**：所有事件阻塞写入（`select` + `ctx.Done()`），不丢弃任何事件。消费者持续消费，按事件类型分发。

**子 Agent Run() 的 wrappedCh goroutine**：在收到最终响应后进入 500ms drain 模式，转发剩余尾部事件。

#### Scenario: RunFlow 结束后继续 Pull

- **WHEN** ContextManager.RunFlow 返回
- **THEN** Loop 不退出
- **AND** Loop 回到 bus.Pull 等待下一批事件

#### Scenario: StopLoop 终止 Loop

- **WHEN** 调用 StopLoop()
- **THEN** loopCtx 被取消
- **AND** Loop goroutine 退出
- **AND** outputCh 被关闭

#### Scenario: 所有事件阻塞写入 outputCh

- **WHEN** RunFlow 产出任何事件（thinking_plan, action_command, agent_output）
- **THEN** 阻塞写入 outputCh
- **AND** 不丢弃
- **AND** ctx 取消时放弃写入并退出

#### Scenario: 工具结果 Publish 到 EventBus

- **WHEN** RunFlow 转发事件流中出现 EventType == "action_command" 的事件
- **THEN** bus.Publish 发布 AgentEvent{Type: "external_input", Source: "tool_result"}
- **AND** 事件同时追加到 SessionProjection 和阻塞写入 outputCh

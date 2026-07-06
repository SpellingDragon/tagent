## MODIFIED Requirements

### Requirement: Loop goroutine 持续运行不退出

Loop goroutine SHALL 循环执行：`bus.Pull(ctx)` → `ContextManager.BuildInvocation` → `ContextManager.RunFlow` → 回到 Pull。Loop SHALL NOT 在 RunFlow 结束时退出。Loop SHALL 仅在 ctx 被取消时退出。

**RunFlow 期间的新用户消息处理**：通过 BeforeModel 回调 `InjectBusInputs` 在 ReAct 循环中注入。TryPull 取走的消息不会在 runEventLoop 的下一轮 Pull 中重复出现。

**RunFlow 中的 outputCh 写入策略**：所有事件阻塞写入（`select` + `ctx.Done()`），不丢弃任何事件。消费者持续消费，按事件类型分发。

#### Scenario: RunFlow 结束后继续 Pull

- **WHEN** ContextManager.RunFlow 返回
- **THEN** Loop 不退出
- **AND** Loop 回到 bus.Pull 等待下一批事件

#### Scenario: RunFlow 期间用户消息被 BeforeModel 注入

- **WHEN** RunFlow 执行中，用户发送新消息到 EventBus
- **AND** 框架 ReAct 循环在下一次 LLM 调用前触发 BeforeModel
- **THEN** InjectBusInputs TryPull 到用户消息并追加到 LLM 请求
- **AND** LLM 在当前 ReAct 迭代中处理用户消息
- **AND** runEventLoop 的下一轮 Pull 不会重复取到该消息

#### Scenario: 所有事件阻塞写入 outputCh

- **WHEN** RunFlow 产出任何事件
- **THEN** 阻塞写入 outputCh
- **AND** 不丢弃
- **AND** ctx 取消时放弃写入并退出

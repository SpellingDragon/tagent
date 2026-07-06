## MODIFIED Requirements

### Requirement: Loop goroutine 持续运行不退出

Loop goroutine SHALL 循环执行：`bus.Pull(ctx)`（阻塞等第一个事件 + non-blocking drain 剩余）→ `ContextManager.BuildInvocation` 合并为一条 model.Message → `ContextManager.RunFlow` 调用框架 Runner → 转发事件到 outputCh + bus → 回到 Pull。Loop SHALL NOT 在 RunFlow 结束（event channel 关闭）时退出。Loop SHALL 仅在 ctx 被取消（StopLoop）时退出。

Loop SHALL NOT 执行任何 trajectory 采集、reward 计算或 trajectory 存储逻辑。Loop 仅负责事件转发、日志记录和 OTLP span 属性设置。

**子 Agent Run() 的 wrappedCh goroutine**：在收到最终响应后 SHALL 进入 500ms drain 模式，转发剩余尾部事件，然后退出。这确保框架 Runner 的尾部事件（如 MemoryPlugin 持久化）不被 context 取消丢弃。

#### Scenario: RunFlow 结束后继续 Pull

- **WHEN** ContextManager.RunFlow 返回的 event channel 关闭
- **THEN** Loop 不退出
- **AND** Loop 回到 bus.Pull 等待下一批事件

#### Scenario: StopLoop 终止 Loop

- **WHEN** 调用 StopLoop()
- **THEN** loopCtx 被取消
- **AND** Loop goroutine 退出
- **AND** outputCh 被关闭

#### Scenario: 子 Agent 最终响应后 drain 尾部事件

- **WHEN** 子 Agent Run() 的 wrappedCh goroutine 收到最终响应
- **THEN** 进入 500ms drain 模式
- **AND** 转发剩余尾部事件到 wrappedCh
- **AND** 500ms 后或 invOutputCh 关闭后退出
- **AND** 触发 runCancel() 取消 context

#### Scenario: 工具结果 Publish 到 EventBus

- **WHEN** RunFlow 转发事件流中出现 EventType == "action_command" 的事件
- **THEN** bus.Publish 发布 AgentEvent{Type: "external_input", Source: "tool_result"}
- **AND** 事件同时追加到 SessionProjection 和转发到 outputCh

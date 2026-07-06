## ADDED Requirements

### Requirement: 子 Agent 最终响应后 drain 尾部事件

TagentAgent.Run() 的 wrappedCh goroutine 在收到最终响应（无 tool_calls 的 assistant message）后 SHALL 进入 drain 模式：
1. 设置 500ms 超时
2. 继续从 `invOutputCh` 读取事件并转发到 `wrappedCh`
3. 超时后退出 goroutine，触发 `defer runCancel()` 和 `defer close(wrappedCh)`

drain 模式确保框架 Runner 的尾部事件（如 MemoryPlugin 持久化、RequiresCompletion 信号）有时间完成发送和持久化，不会被 context 取消丢弃。

#### Scenario: 最终响应后有尾部事件

- **WHEN** 子 Agent 产出最终响应，框架 Runner 随后产出 1-2 个尾部事件
- **THEN** wrappedCh goroutine 在 500ms 内转发尾部事件
- **AND** 尾部事件被 AgentToolWrapper.Call 消费
- **AND** MemoryPlugin 有时间完成 OnEvent 持久化

#### Scenario: 最终响应后无尾部事件

- **WHEN** 子 Agent 产出最终响应，500ms 内无新事件
- **THEN** drain 超时后退出 goroutine
- **AND** runCancel() 取消 context
- **AND** wrappedCh 关闭

#### Scenario: drain 期间 invOutputCh 关闭

- **WHEN** drain 模式期间 `invOutputCh` 被关闭（runEventLoop 退出）
- **THEN** 立即退出 drain 模式
- **AND** 不等待 500ms 超时

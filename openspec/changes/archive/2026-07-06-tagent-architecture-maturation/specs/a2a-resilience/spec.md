## ADDED Requirements

### Requirement: AgentToolWrapper.Call 设置超时

AgentToolWrapper.Call SHALL 使用 `context.WithTimeout(ctx, 120s)` 包装子 Agent 调用。超时后 SHALL 返回 error，不无限等待。超时时间 SHALL 可通过 `AgentToolWrapper` 的配置覆盖。

#### Scenario: 子 Agent 正常完成

- **WHEN** 子 Agent 在 10s 内完成
- **THEN** Call 正常返回结果
- **AND** context 未超时

#### Scenario: 子 Agent 超时

- **WHEN** 子 Agent 运行超过 120s
- **THEN** context 超时
- **AND** Call 返回 "agent tool %q: timeout after 120s" 错误
- **AND** 子 Agent 的 eventCh 被关闭（runEventLoop 的 ctx 取消）

### Requirement: 远程 A2A 调用失败后重试 1 次

AgentToolWrapper.Call 在远程 A2A 调用（agent.Agent 类型为 `*a2aagent.A2AAgent`）失败时，SHALL 重试 1 次。本地调用（`*TagentAgent`）失败不重试。重试前 SHALL 等待 500ms。重试时 SHALL 使用新的 context（重置超时计时器）。

#### Scenario: 远程调用首次失败重试成功

- **WHEN** A2AAgent.Run 首次返回网络错误
- **THEN** 等待 500ms 后重试
- **AND** 第二次调用成功
- **AND** Call 正常返回结果

#### Scenario: 远程调用重试后仍失败

- **WHEN** A2AAgent.Run 重试后仍返回错误
- **THEN** 不再重试
- **AND** Call 返回最后一次的错误

#### Scenario: 本地调用失败不重试

- **WHEN** TagentAgent.Run 返回错误
- **THEN** 不重试
- **AND** Call 直接返回错误

### Requirement: BuildInvocation 跳过 error 和 tool_result 事件

ContextManager.BuildInvocation SHALL 只合并 `Type == "external_input"` 且 `Source != "error"` 且 `Source != "tool_result"` 的事件。`Source == "error"` 和 `Source == "tool_result"` 的事件不触发模型调用。

#### Scenario: 批量事件包含 error 事件

- **WHEN** Pull 返回 [external_input(user), external_input(error), external_input(tool_result)]
- **THEN** BuildInvocation 只合并 user 事件
- **AND** error 和 tool_result 事件被跳过
- **AND** 生成的 message.Content 只包含 user 事件内容

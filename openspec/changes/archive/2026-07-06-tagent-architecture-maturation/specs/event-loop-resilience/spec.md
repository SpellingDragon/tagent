## ADDED Requirements

### Requirement: RunFlow 失败后指数退避重试

runEventLoop 在 `cm.RunFlow(ctx, msg)` 返回 error 时，SHALL 使用指数退避策略重试（100ms → 200ms → 400ms），最多重试 3 次。每次重试前 SHALL 检查 ctx 是否已取消。如果重试期间 `outputCh` 已收到 final response，SHALL 不再重试。

#### Scenario: RunFlow 首次失败后重试成功

- **WHEN** RunFlow 第一次调用返回 error
- **THEN** 等待 100ms 后重试 RunFlow
- **AND** 第二次调用成功
- **AND** 继续进入下一轮 bus.Pull

#### Scenario: RunFlow 连续失败 3 次后放弃

- **WHEN** RunFlow 连续 3 次返回 error
- **THEN** 不再重试
- **AND** 将失败信息封装为 AgentEvent 发布到 EventBus
- **AND** continue 到下一轮 bus.Pull

#### Scenario: 重试期间 ctx 被取消

- **WHEN** 重试等待期间 loopCtx 被取消（StopLoop 调用）
- **THEN** 立即退出 runEventLoop
- **AND** 不发布错误事件

### Requirement: RunFlow 失败后发布错误事件到 EventBus

当 RunFlow 重试耗尽后，runEventLoop SHALL 将失败信息封装为 `AgentEvent{Type: "external_input", Source: "error"}` 发布到 EventBus。Message.Content SHALL 包含错误摘要。BuildInvocation SHALL 跳过 `Source == "error"` 的事件（不合并到 user message）。

#### Scenario: 错误事件发布到 EventBus

- **WHEN** RunFlow 重试 3 次后仍失败，错误为 "model timeout"
- **THEN** 发布 AgentEvent{Type: "external_input", Source: "error", Message: {Content: "[error] RunFlow failed after 3 retries: model timeout"}}
- **AND** 下一轮 Pull 取到该错误事件
- **AND** BuildInvocation 跳过该事件（Source == "error"）

#### Scenario: 外部监听器感知错误事件

- **WHEN** EventBus 中存在 Source == "error" 的事件
- **THEN** 外部监听器（如 HTTPAPI、outputCh 消费者）可以通过事件流感知到错误
- **AND** 错误事件不会被合并为 user message 触发模型调用

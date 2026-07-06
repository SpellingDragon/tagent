## ADDED Requirements

### Requirement: 持续消费 outputCh 并按事件类型分发

应用侧 SHALL 在 StartLoop 后启动持续消费 goroutine，持续读取 outputCh 直到 channel 关闭。消费者 SHALL 按事件类型分发处理：
- agent_output（IsFinalResponse）：如果当前有等待中的用户消息 → 回复用户；否则 → 记录日志
- 非 final 事件：可选展示（日志、打字指示等）

#### Scenario: 用户消息的 agent_output 回复用户

- **WHEN** 用户发送消息 → InjectMessage → 持续消费者收到 agent_output
- **AND** 当前有等待中的用户消息
- **THEN** agent_output 内容回复给用户
- **AND** 清除等待状态

#### Scenario: 冥想的 agent_output 记录日志

- **WHEN** 冥想事件触发 → 持续消费者收到 agent_output
- **AND** 当前没有等待中的用户消息
- **THEN** agent_output 内容记录到日志
- **AND** 不发送给用户

#### Scenario: 中间事件不影响用户响应

- **WHEN** RunFlow 产出 thinking_plan 或 action_command 事件
- **THEN** 持续消费者接收并记录
- **AND** 不回复用户
- **AND** 继续打字指示（如适用）

#### Scenario: 持续消费者在 StopLoop 后退出

- **WHEN** StopLoop 被调用 → outputCh 关闭
- **THEN** 持续消费者 goroutine 的 `for range outputCh` 退出
- **AND** goroutine 正常结束

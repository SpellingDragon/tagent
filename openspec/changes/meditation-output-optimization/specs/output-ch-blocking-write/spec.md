## ADDED Requirements

### Requirement: outputCh 阻塞写入所有事件

ContextManager.RunFlow 在写入 outputCh 时 SHALL 对所有事件使用阻塞写入：`select` + `ctx.Done()`。不再有 `default` 丢弃分支。阻塞写入在 ctx 取消时退出。

#### Scenario: 消费者正常消费时阻塞立即通过

- **WHEN** 消费者正在消费 outputCh，RunFlow 产出事件
- **THEN** 事件被写入 outputCh
- **AND** 消费者收到事件

#### Scenario: 消费者短暂忙碌时阻塞等待

- **WHEN** 消费者正在处理前一个事件，RunFlow 产出新事件
- **THEN** 阻塞等待消费者读取
- **AND** 消费者处理完前一个事件后读取新事件

#### Scenario: ctx 取消时放弃写入

- **WHEN** 阻塞写入期间 ctx 被取消（StopLoop 或重试超时）
- **THEN** 放弃写入
- **AND** RunFlow 退出

#### Scenario: outputCh 不再有丢弃日志

- **WHEN** outputCh 中的事件被消费
- **THEN** 不出现 "outputCh full, dropping event" 日志

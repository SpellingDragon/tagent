# persistent-event-loop（ADDED delta）

## ADDED Requirements

### Requirement: 在途 turn 的外部输入在 decision 边界消费

外部输入（`TypeExternalInput`；`trigger_source == "user"` 或 owner 为宿主用户地址）到达 SHALL 先提交为事实并返回受理回执（`ErrAcceptedInFlight` 语义）；同一 session 有在途 turn 时，该输入 SHALL 在该 turn 的下一 decision 边界被聚合进输入并消费，SHALL NOT 排队至 turn 结束另起 turn；执行中的 decision SHALL NOT 被打断。自管谱系通知（task_settled 等）的既有排队条款不受本要求影响。

#### Scenario: 在途消息被下一决策消费

- **WHEN** 慢工具轮 turn 进行中同 session 到达用户消息
- **THEN** 该消息 SHALL 提交为事实，且同 turn 下一 decision 的输入 SHALL 含该消息

#### Scenario: 无在途时起 turn

- **WHEN** 无在途 turn 时到达用户消息
- **THEN** SHALL 落链并起新 turn 消费

#### Scenario: 忙碌不丢

- **WHEN** 在途 turn 的 decision 进行中到达外部输入
- **THEN** 输入 SHALL 保留于链上待下一决策，MUST NOT 丢弃或覆盖

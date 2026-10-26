## ADDED Requirements

### Requirement: 发送动作身份与未确认结果

每一次外部发送 SHALL 以其动作身份记录确认状态，动作身份沿用 `event-metadata-contract`「通知身份、事实引用与消费身份分层」的发送轴，MUST NOT 以正文、时间戳或另一个动作的身份代替。已确认成功的动作 MUST NOT 因恢复、重放、重入或后续 turn 再次执行。结果未知（确认丢失、超时、错误不可辨）MUST 保留动作身份、目标、错误与未确认状态，MUST NOT 自动重发、MUST NOT 伪报成功。显式重试 SHALL 经既有正式入口或人工处置，并保留与原动作的关联。不同动作身份 MUST NOT 合并；同一动作身份但目标或正文不同 MUST 被判为冲突并具名拒绝。

#### Scenario: 已确认动作重入不重发

- **WHEN** 一个已确认成功的显式发送动作因恢复或重入再次被处理
- **THEN** 系统 MUST NOT 再次调用发送端口
- **AND** SHALL 复用原动作的确认结果

#### Scenario: 结果未知不自动重发

- **WHEN** 发送动作返回超时或确认不可辨
- **THEN** 该动作 SHALL 保持未确认状态并保留目标与错误
- **AND** MUST NOT 自动重发或声明已送达

#### Scenario: 不同动作身份不合并

- **WHEN** 同一 turn 既执行显式 `send` 又产生最终答复
- **THEN** 二者 SHALL 各自记录动作身份与确认状态
- **AND** MUST NOT 以一个动作的成功覆盖另一个动作的未确认

#### Scenario: 显式重试保留关联

- **WHEN** 一个未确认动作经显式重试再次发送
- **THEN** 新动作 SHALL 关联原动作身份与重试原因
- **AND** MUST NOT 被记作一次无来源的全新发送

# delivery-dispatch-receipts Specification

## Purpose
TBD - created by archiving change delivery-intent-and-receipts. Update Purpose after archive.
## Requirements

### Requirement: 预期外静默终态必回执

宿主分发层 SHALL 对以下终态注入回执事件：发送失败（ERROR 级）、未知/未声明血统消化（WARN）、冥想扣留且内容含交付特征（WARN）、error 分支扣留（WARN）、无目标扣留（WARN）。已送达 SHALL 不回执；冥想扣留且无交付特征（纯叙事）SHALL 维持契约内静默、不回执。

#### Scenario: 冥想血统任务结算被扣留即回执

- **WHEN** trigger_source=meditation 的最终响应含 `[task settled]` 或 `delivery/` 特征并被 case "meditation" 扣留
- **THEN** 注入 WARN 回执事件，正文含终态原因、血统、内容截断与投递目标（chat_id 或最近活跃会话兜底）

#### Scenario: 冥想纯叙事维持静默

- **WHEN** trigger_source=meditation 的最终响应不含交付特征
- **THEN** 不注入回执（契约内静默，不给 agent 刷屏）

#### Scenario: 发送失败必回执

- **WHEN** SendText 返回错误
- **THEN** 注入 ERROR 回执事件——用户在场却没收到是最重裂缝

#### Scenario: 已送达不回执

- **WHEN** SendText 与 DeliverFiles 均成功
- **THEN** 不注入任何回执（agent 自证可观察）

### Requirement: 回执走持久总线且血统内部

回执 SHALL 经 `InjectMessageWithSource("delivery_receipt", …)` 注入持久总线（转生/换装后仍在账上）；`delivery_receipt` 血统 SHALL 保持非投递（回执轮自身输出静默、不武装冥想新颖门）；回执与用户消息同批时 user 一票否决，该轮产出正常投递（agent 可当场补投）。

#### Scenario: 回执轮自身输出不外发

- **WHEN** 仅由回执事件触发的轮次产出最终响应
- **THEN** 该响应按非投递血统静默消化，用户不可见

#### Scenario: 回执与用户消息混批

- **WHEN** 同批含用户消息与回执事件
- **THEN** 批血统判 user，该轮产出正常投递

### Requirement: 回执不递归

lineage 为 `delivery_receipt` 的最终响应被消化时 SHALL 不再产生回执（防自激红线）；交付特征检测 SHALL 仅用于回执分级，SHALL NOT 参与投递裁决。

#### Scenario: 回执链不自激

- **WHEN** 回执轮自身输出被静默消化
- **THEN** 不出现第二条回执

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

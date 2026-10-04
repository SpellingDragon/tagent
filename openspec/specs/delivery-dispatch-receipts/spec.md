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


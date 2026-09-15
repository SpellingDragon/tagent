## Purpose

让机器人回复文本中的 @ 提及在 QQ 客户端呈现为官方真提及（蓝字可点）而非纯文本假 @，确保提及目标能收到提醒。

## ADDED Requirements

### Requirement: 出站提及转真提及形态

机器人群聊回复发送前，文本中的 `@显示名` SHALL 按以下优先级反查 openid：(1) 同窗口帧 username↔openid 关联；(2) 既有 profile 注册表。命中则 MUST 替换为 `<@member_openid>` 形态发送；未命中时 SHALL 保留原文 `@显示名` 发送，MUST NOT 丢弃或改写该提及之外的任何文本。

#### Scenario: 显示名可反查 openid
- **WHEN** 模型回复文本含 `@G411`，且窗口帧/profile 注册表中 G411 对应 openid u_abc123
- **THEN** 实际发出的消息中该片段为 `<@u_abc123>`，QQ 客户端呈现为真提及

#### Scenario: 显示名反查未命中
- **WHEN** 模型回复文本含 `@NANA`，且窗口帧与 profile 注册表均查无该名
- **THEN** 消息按原文 `@NANA` 发出，其余文本不受影响

### Requirement: 替换不改写提及外文本

出站替换 SHALL 仅作用于 @ 提及片段本身，MUST NOT 影响消息其余部分的字符内容（含换行、标点、emoji）。

#### Scenario: 其余文本逐字保留
- **WHEN** 模型回复为 `哈哈，@G411 这图哪来的？我看你收藏夹要爆炸了`
- **THEN** 替换后发出的消息为 `哈哈，<@u_abc123> 这图哪来的？我看你收藏夹要爆炸了`，除提及片段外逐字一致

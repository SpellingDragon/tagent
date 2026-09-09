## Purpose

约束机器人在群内的一切主动发言（@回复、昵称/别名触发、低频激活），以群为单位可开可关；防止机器人骚扰未授权的群。

## ADDED Requirements

### Requirement: 群级发言白名单门控

机器人一切群内主动发言（@回复、昵称触发、低频激活）MUST 要求目标群在发言白名单中；白名单为空时 MUST 采用 allow-all 并在启动时输出一次性警告（沿用既有两态语义）。

#### Scenario: 空白名单 allow-all + 启动警告
- **WHEN** `group_speech_whitelist` 为空（未配置任何群）
- **THEN** 所有群的主动发言均被放行，且进程启动时输出一次警告日志说明 allow-all 模式与配置方法

#### Scenario: 非白名单群拒绝发言
- **WHEN** 群 G 不在白名单中，且用户在 G 内 @bot、昵称提及、或满足低频激活条件
- **THEN** 机器人不在 G 内产生任何主动发言（不发起 LLM 调用，不发送消息）

#### Scenario: LLM 工具发言同受约束
- **WHEN** LLM 工具（mute/profile 工具组）在群 G 内产生发言动作
- **THEN** 该发言同样要求 G 在白名单中，否则被拒绝

### Requirement: 群主切换白名单命令

群主命令"开启白名单/关闭白名单"MUST 切换本群的白名单状态，并持久化到 `data/group_whitelist.json`（tmp+rename 原子写）。

#### Scenario: 群主开启白名单
- **WHEN** 群 G 的群主发送"开启白名单"
- **THEN** G 加入 `group_speech_whitelist`，持久化生效，且审计日志记录切换动作（时间、群、动作、操作者）

#### Scenario: 群主关闭白名单
- **WHEN** 群 G 的群主发送"关闭白名单"
- **THEN** G 从白名单移除，持久化生效，机器人此后的主动发言被拒，审计日志记录该动作

#### Scenario: 非群主切换被拒
- **WHEN** 非群主用户发送"开启白名单/关闭白名单"
- **THEN** 状态不改变，机器人可提示无权限，审计日志记录拒绝

#### Scenario: 持久化跨重启
- **WHEN** 白名单切换后进程重启
- **THEN** 白名单状态从 `data/group_whitelist.json` 恢复，与重启前一致

### Requirement: 群主身份判定

群主身份判定 MUST 支持画像 role==owner 与 WS 帧 member_role==owner 两路来源，任一命中即为群主。

#### Scenario: 画像学得 owner
- **WHEN** 用户画像记录该用户 role==owner，且 WS 帧未标记 owner
- **THEN** 该用户被判定为群主，白名单命令生效

#### Scenario: WS 帧标记 owner
- **WHEN** WS 帧 author.member_role==owner，且画像未记录 role
- **THEN** 该用户被判定为群主，白名单命令生效

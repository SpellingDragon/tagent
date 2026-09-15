## Purpose

QQ 侧消息白名单由隐式 allow-all 改为显式配置驱动：配置了列表即严格校验，未配置保持兼容但启动醒目告警。

## ADDED Requirements

### Requirement: 配置驱动的白名单
机器人 SHALL 从 config 读取 `white_list` 列表作为 QQ 侧消息授权依据。列表非空时 MUST 严格校验（仅列表内身份可触发指令）；列表为空或缺失时 MUST 保持 allow-all 兼容行为，且在启动日志输出醒目 Warn。

#### Scenario: 配置了白名单（严格态）
- **WHEN** `white_list` 列表非空且收到来自列表外身份的指令消息
- **THEN** 机器人拒绝该指令（不执行、不回复结果），并记录一条拒绝日志

#### Scenario: 配置了白名单（命中）
- **WHEN** 收到来自列表内身份的指令消息
- **THEN** 机器人正常执行指令

#### Scenario: 未配置白名单（兼容态）
- **WHEN** `white_list` 缺失或为空
- **THEN** 机器人保持 allow-all，启动日志输出醒目 Warn（提示显式配置白名单）

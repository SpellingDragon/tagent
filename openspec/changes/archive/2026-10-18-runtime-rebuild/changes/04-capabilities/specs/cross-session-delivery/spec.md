# cross-session-delivery Specification

## Purpose

本 delta 在保留既有 fail-closed 投递语义下，要求投递使用**显式目标**并把拒绝落成**具名拒绝账本**，供 05-inspection 解释阻塞。

## ADDED Requirements

### Requirement: 显式目标投递与具名拒绝账本

进程内跨 session/agent 投递 SHALL 使用**显式目标**；白名单外、目标未运行、目标名不存在等拒绝 SHALL 返回具名错误，并 SHALL 产生一条可查询的**拒绝账本**记录（含目标名、拒绝类别、时间）；MUST NOT 静默丢弃。

#### Scenario: 白名单外目标具名拒绝

- **WHEN** 投递方投递到不在其 `deliver_to` 内的目标
- **THEN** 返回具名白名单拒绝错误，目标零感知
- **AND** 产生一条拒绝账本记录

#### Scenario: 未配置白名单 fail-closed（失败）

- **WHEN** `deliver_to` 未配置
- **THEN** 任何投递都被具名拒绝，且记入拒绝账本

#### Scenario: 目标 loop 未运行具名拒绝（失败）

- **WHEN** 目标 agent 在常驻表但其 loop 未启动
- **THEN** 返回具名"目标未运行"错误，投递方可观测并自决
- **AND** 不产生任何新 Run/新 goroutine

#### Scenario: 成功语义为"进入 mailbox"（并发）

- **WHEN** 投递消息与 `source=user` 输入同批进入目标事件循环
- **THEN** 用户优先，投递让位是合法结局
- **AND** 投递 API 成功语义为"已进入 mailbox"而非"已触发 turn"

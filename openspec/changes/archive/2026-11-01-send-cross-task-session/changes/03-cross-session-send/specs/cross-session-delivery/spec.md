# cross-session-delivery Specification（delta：消费面扩为 send 对外形态）

> 本文件为 MODIFIED delta。四道安全门条款语义**不变**；挂载点从"deliver_to 缝直调"扩为"`send{target:{agent,session}}` 的对外分派"，并新增"入链+驱动一轮"送达条款（用户裁定 2026-11-01）。

## Purpose

同级会话间的事件投递经唯一通信工具 `send` 表达；安全门与送达语义（入链+驱动一轮）在此定义。

## MODIFIED Requirements

### Requirement: 四道安全门语义不变

`send{target:{agent,session}}` SHALL 复用既有投递缝四道门：deliver_to 白名单 fail-closed（空表拒绝一切）、盲投拒绝、未知目标拒绝、目标 loop 未运行具名拒绝；一切拒绝 SHALL 具名回执，MUST NOT 静默丢弃、MUST NOT 触发 one-shot 回退。

#### Scenario: 白名单外被拒

- **WHEN** send 指向不在 deliver_to 白名单的目标
- **THEN** `[send_denied]` 具名文本，目标零感知

### Requirement: 送达=入链+驱动一轮

四门通过的投递 SHALL 写事件入目标会话链并驱动目标会话执行一轮（空闲目标被唤醒）；忙碌目标 SHALL 在 one-slot gate 边界读取，MUST NOT 死锁发送方。重复投递 SHALL 按投递身份幂等（不重复有效处理）。

#### Scenario: 空闲目标被驱动

- **WHEN** send 指向空闲的目标会话
- **THEN** 事件入链且目标会话跑完一轮

#### Scenario: 重复投递幂等

- **WHEN** 同一投递身份重投
- **THEN** 目标会话不产生第二条有效处理

### Requirement: 治理可挂起

risk_rules SHALL 可对 send 跨会话形态配 critical：hold 时回执 `[send_held]`（不得报为送达），批准后恰补投一次，拒绝则丢弃并留审计。

#### Scenario: hold 批准补投

- **WHEN** 一次 send 被 hold 后批准
- **THEN** 正文恰补投一次并驱动目标一轮

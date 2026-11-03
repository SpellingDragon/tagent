# cross-session-delivery Delta

## MODIFIED Requirements

### Requirement: 送达=入链+驱动一轮

四门通过的投递 SHALL 将目标输入事实以稳定身份写入目标会话链，并唤醒目标地址的常驻 Consumer。send 的 `ok` SHALL 仅表示该事实已持久提交，不表示目标已跑完一轮或已成功回答；目标后续 failed/cancelled MUST NOT 反向改变该回执。目标忙闲 MUST NOT 改变驱动者或回执时点。重复投递 SHALL 按投递身份幂等。

#### Scenario: 空闲目标被驱动
- **WHEN** send 指向空闲的目标会话
- **THEN** 输入事实落链并唤醒该地址 Consumer，send 在事实提交后返回 ok，不等待目标回答

#### Scenario: 目标回答失败
- **WHEN** 输入事实已提交且 send 已返回 ok，随后目标回合 failed
- **THEN** 原回执保持 ok，目标失败由其回合结果与通知表达，不得追加事后 denied 或回滚已提交输入

#### Scenario: 重复投递幂等
- **WHEN** 同一投递身份重投
- **THEN** 目标会话不产生第二条有效处理，也不另开 Round

# persistent-event-loop Specification（delta：循环停止=静止收束）

> 本文件为 MODIFIED delta。原"循环停止为终结态"条款中的"轮询真实停止确认"改为"静止收束（quiesce）"：事件唤醒的收敛判定，禁止轮询。

## MODIFIED Requirements

### Requirement: 循环停止为终结态

循环关闭 SHALL 以**真实义务收束**为准：先拒绝新输入（intake 关）、静止到不动点（工作收敛 ∧ 消费收敛 ∧ 写入者停止）；MUST NOT 把 `cancel` 调用或队列取空当作完成。非合作 producer 返回的 `false` MUST NOT 被当作已结束；截止期限到达 SHALL 返回未收束原因。

判定 SHALL 由事件唤醒驱动（新事实提交/执行体收尾信号），MUST NOT 以定时轮询实现；判定读权威账（`Unsettled()` 同源函数），不得读进程内影子账本。

#### Scenario: 非合作 producer false 不伪结束

- **WHEN** producer 的停止确认返回 false
- **THEN** 关闭不得报告已收束

#### Scenario: 截止报告未收束原因

- **WHEN** 到达截止期限而条件未满足
- **THEN** 返回未收束项清单（事实键与原因），且为 `Cut` 正常返回（非错误）

#### Scenario: 先拒新再结算

- **WHEN** 关闭开始
- **THEN** 先拒绝新的 intake，再静止收束；derived（关闭期由已有义务派生的回合）仍可被受理

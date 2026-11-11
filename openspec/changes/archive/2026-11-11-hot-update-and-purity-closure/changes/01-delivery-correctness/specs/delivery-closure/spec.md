# delivery-closure Specification（MODIFIED delta）

## Purpose

收紧交付闭环：owed 活动集跨驻留窗口与崩溃仍恰好补投一次，重试跨重启有界，读错具名不降级，消除漏投与无界重试。

## MODIFIED Requirements

### Requirement: owed 活动集驻留

owed 但未结算的交付义务 SHALL 不随上下文窗口 trim 而丢失；崩溃重启后 SHALL 补投且不丢（至少一次）。

#### Scenario: 链超窗口后重启补投

- WHEN 一条 owed 未送达且链增长超过驻留窗口后进程崩溃重启
- THEN 重启后该义务补投且不丢（至少一次；重投由回执销账约束）（锚 B）

#### Scenario: sent 不重发

- WHEN 义务已 sent 销账后重启
- THEN 不重发

### Requirement: 重试跨重启有界

重试 attempt SHALL 落链；重启 SHALL 读链续计；超界 SHALL 具名拒绝。

#### Scenario: 重启续计

- WHEN 重试若干次后崩溃重启
- THEN 计数从链续计而非归零

#### Scenario: 超界拒绝

- WHEN attempt 达上限
- THEN 具名拒绝不再 schedule

### Requirement: 交付读错具名

结算/耐久链读错误 SHALL 具名上报，SHALL NOT 降级为 absence 触发重发或静默部分视图。

#### Scenario: 结算读错不重发

- WHEN 结算事实底层读错
- THEN 返回具名 error 且不重发

#### Scenario: 耐久读错 surfaced

- WHEN 交付事实耐久链读错
- THEN surfaced 具名失败而非 WARN 部分视图
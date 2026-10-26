## MODIFIED Requirements

### Requirement: 消费状态决定遥测退出时点

遥测的退出单位 SHALL 为消费状态（客观可推导，零 LLM 参与）：已逻辑消费确认且已外显（回收 turn 有 agent_output，且经 outputCh 投递宿主）→ turn 收尾即时降级为票据行；已逻辑消费确认但内部性 → 保留一行摘要 N 轮后降级，N MUST 与 keepRecent 值同源（消费边界现读，无独立配置项）；未逻辑消费确认 → 保持完整直到被消费（MUST NOT 被丢弃或静默降级）。在遥测退出判定中，运输层接收与 claim MUST NOT 被当作逻辑消费确认，MUST NOT 据此降级。降级操作 SHALL 仅修改投影（事实链不可变红线不变）。连跑折叠（settle_fold）与消费降级 SHALL 幂等共存。

#### Scenario: 已外显通知即时退出

- **WHEN** 一条 settle 的回收 turn 产出 agent_output 并经 outputCh 投递宿主
- **THEN** 该 turn 收尾时通知降级为票据行，下一轮装配的常驻 messages 不含其全文

#### Scenario: 内部性通知的 N 轮提醒

- **WHEN** 一条内部巡检类 settle 被回收 turn 消费但输出未外显，keepRecent=2
- **THEN** 其一行摘要保留 2 个完整段后降级；热更 keepRecent 后 N 自动跟随

#### Scenario: 未消费通知不可丢

- **WHEN** 停机期间错过消费的 settle 在重启后进入投影
- **THEN** 其以完整形态保持直到被某回收 turn 消费；积压有界性由任务层 TTL reaper 与批量退役 N→1 汇总在源头保证，上下文层不设数量/体积上限

#### Scenario: 运输接收不触发降级

- **WHEN** 通知已进入邮箱或被 claim，但尚无逻辑消费确认
- **THEN** 该通知 SHALL 保持完整形态
- **AND** MUST NOT 被降级为票据行


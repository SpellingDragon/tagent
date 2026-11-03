## ADDED Requirements

### Requirement: 消息语义审查以逻辑消费事实为准

消息语义审查 SHALL 区分受理、事实落链、运输投递、逻辑消费与答复，并 SHALL 从正式公共入口验证通知、委派、拒收和重启路径。运输成功或内部 helper 测试 MUST NOT 冒充逻辑消费确认。

#### Scenario: 回执与通知共用同一事实解释

- **WHEN** 审查 send 回执、任务通知或委派结果
- **THEN** 报告 SHALL 指出每个状态对应的权威事实及生产消费者，并验证不存在第二个相互矛盾的结清判据

#### Scenario: 负向路径经公共入口

- **WHEN** 验证丢通知、错配答复或重复消费
- **THEN** 负锚 SHALL 经正式入口驱动并具名记录，内部 helper 拒绝 SHALL NOT 单独构成通过证据

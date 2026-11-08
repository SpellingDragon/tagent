## ADDED Requirements

### Requirement: 采集损失可查询
运行查询 SHALL 返回采集损失账目：被准入拒绝、写入失败与封账不完整的记录数及其原因。损失 MUST NOT 以零值或空结果呈现。

#### Scenario: 队列满导致损失
- **WHEN** 采集队列达到上限而拒绝一条记录
- **THEN** 查询 MUST 返回该损失计数与原因
- **AND** MUST NOT 报告为样本完整

### Requirement: 待处理义务与年龄可查询
运行查询 SHALL 返回未结算义务的数量与最老等待时长，并指出下一推进者。

#### Scenario: 待发送义务积压
- **WHEN** 存在尚未确认的外发义务
- **THEN** 查询 MUST 返回其数量与最老年龄
- **AND** MUST 说明由哪个动作推进

### Requirement: 观测关闭不缩减查询维度
关闭 OTLP 导出时，查询 MUST 保持与开启时相同的维度与语义。

#### Scenario: 关闭观测后查询一致
- **WHEN** 未配置 OTLP 端点并执行查询
- **THEN** 返回维度 MUST 与开启时一致
- **AND** MUST NOT 因观测关闭而缺少采集或义务读数

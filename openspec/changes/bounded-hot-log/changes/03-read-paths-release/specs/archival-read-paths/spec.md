## ADDED Requirements

### Requirement: 消费面共用一条身份定位读路径

`recall_get`/`recall_trace`、历史查询、训练导出与全史回读 SHALL 共用“热索引→manifest→段索引→段”的身份定位读；MUST NOT 为跨档读另立第二读路径或把归档正文复制进热卷形成第二真相。读缓存（若有）SHALL 可丢且按身份校验后服务。

#### Scenario: 召回老键

- **WHEN** 目标事实已归档
- **THEN** `recall_get` SHALL 经定位读返回原字节
- **AND** 归档缺失/损坏 SHALL 具名上返

### Requirement: 平台锚与回退零差异入验收

有界验收 SHALL 包含：S1 多代平台锚（满窗后热字节落带、老键可读）、S2 三态损坏负锚、无归档配置时的零差异探针。回滚本变更 SHALL 使 S1 复红（证据两次入档）。

#### Scenario: 回滚即红

- **WHEN** 完整回滚轮转/归档实现
- **THEN** S1 SHALL 红
- **AND** 修复后 SHALL 绿，两次记录同档

### Requirement: 三类成本读数分离

验收 SHALL 分别记录热卷字节（平台）、归档卷字节（随历史）、跨档读延迟（p50/p90）；MUST NOT 以任一读数替代其他结论。

#### Scenario: 声明分离

- **WHEN** 汇报放行
- **THEN** 三类读数 SHALL 同时在场
- **AND** 归档增长 SHALL 被标为已声明成本而非热卷达标

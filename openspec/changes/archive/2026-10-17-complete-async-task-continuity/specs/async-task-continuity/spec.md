## ADDED Requirements

### Requirement: 逻辑调用的持久恢复与原地址结果回流

本地逻辑调用 SHALL 以continuation_state记录原invocation、初始goal、私有投影边界、已消费输入、等待与stream游标、结果和父task原producer地址。恢复 SHALL 先重建checkpoint、输入事实及处置，再绑定所有receiver和实际driver，最后放行pending交付。已消费初始goal MUST NOT 自动重跑；缺失工具结果的中断 SHALL 明确报告reconciliation，而非盲目重复副作用。

#### Scenario: 等待期间重启
- **WHEN** 子调用已派长命令并在等待时重启
- **THEN** 原私有投影和等待恢复；原命令结果回同一child invocation并结算原parent task round

#### Scenario: 结果已完成但父未ACK
- **WHEN** child结果已保存而父结果受理尚未确认即崩溃
- **THEN** 恢复重发缓存结果，父ACK前保留材料，不重跑goal

### Requirement: 就绪服务的明确交接与恢复保护

逻辑goal完成时，仍存活的ready服务 SHALL 明确交接至父scope或所属持久owner；交接链 SHALL 防环，原stream游标与owner保留，MUST NOT 用原子调用退出作为丢弃服务终局的理由。未完成checkpoint、raw结果、预留receipt和工具正文 SHALL 受既有RetentionGuard保护，完整装配后才放行破坏性清理。

#### Scenario: 原调用完成后服务稍后退出
- **WHEN** ready服务已满足goal而producer继续存活
- **THEN** 服务被交接；其后终局仍到明确receiver，不落入已退休调用的死信

#### Scenario: 恢复清点失败
- **WHEN** checkpoint或保护材料枚举失败
- **THEN** 恢复显式失败并保持保留屏障，不把残缺视图当作无材料

# compression-recovery-ticket Specification

## Purpose
本规范定义 compression-recovery-ticket 能力：重启作为事件的两步恢复消费逻辑——先装载未消费事件，再装载票据快照或重放内存状态，两步完成才开放服务；快照由链上票据事实按内容寻址引用，任何失败退回全量事件流且义务不丢。
## Requirements
### Requirement: compression-recovery-ticket 契约

重启恢复 SHALL 是重启事件的消费逻辑，两步完成才开放服务：①装载未消费的事件；②装载或重放内存状态。内存状态 SHALL 由链上票据事实 recovery.ticket 按内容寻址引用唯一一份可覆盖的快照 blob；票据 SHALL 带完整性信息与游标；MUST NOT 以链外独立状态作为可写第二真相。快照 SHALL 只保留一份（固定键，新覆盖旧）。任何票据/blob 缺失、截断、校验失败或版本不认识 SHALL 退回全量事件流且结果不丢义务；全量重放与快照+增量 SHALL 逐位一致。

#### Scenario: 主路径

- **WHEN** 恢复时票据存在且校验通过
- **THEN** 加载唯一一份 blob 并补放其后增量，结果与全量重放逐位一致

#### Scenario: 校验失败退回全量

- **WHEN** blob 被截断、指纹不符或版本不认识
- **THEN** 读失败并入日志，改走全量事件流，义务不丢

#### Scenario: 单份与完成点

- **WHEN** 连续两次启动各写一次快照
- **THEN** 日志中 snapshot 前缀下恰一份 blob；票据在两步完成、服务开放前写出


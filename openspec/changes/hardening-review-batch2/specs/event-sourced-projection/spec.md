# event-sourced-projection Delta

## ADDED Requirements

### Requirement: 无锚恢复不静默截断
无 compaction anchor 的冷启动回放 MUST NOT 静默丢弃历史：回放 MUST 全量分页读取后过滤非投影事件；若因内存护栏必须截断，rebuild 结果 MUST 显式标记 partial（truncated_events 计数 + Error 级日志 + diagnostics 字段），MUST NOT 以截断结果冒充完整恢复。

#### Scenario: 超护栏长链冷启动
- **WHEN** 事实链无 compaction anchor 且事件数超过回放护栏
- **THEN** rebuild MUST 输出 partial 标记与截断计数，调用方与日志可辨「恢复不完整」，MUST NOT 无标记地返回截断投影

### Requirement: 恢复观测覆盖全误差面
投影重建的观测 MUST 覆盖全部误差来源：快照 lost keys、tail 分页失败（pages_failed）、批量读取错误（batch_errors）、payload 解析失败（payload_errors）各自计数并进入汇总日志行；单次零丢失 MUST NOT 被解读为恢复完整性证明。

#### Scenario: tail 分页部分失败
- **WHEN** 恢复期间某 tail 分页查询返回错误
- **THEN** 汇总日志 MUST 含 pages_failed≥1 且 rebuild 结果标记 partial，MUST NOT 静默跳页后报告成功

# recovery-partition-cursor Specification

## ADDED Requirements

### Requirement: recovery-partition-cursor 契约

段存储 Init SHALL 经活跃分区位图与 per-partition 游标恢复，MUST NOT 依赖全前缀扫描；位图与游标 SHALL 与事件同批原子写入；缺失时 SHALL 等价回退全扫描并具名告警。

#### Scenario: 主路径

- **WHEN** 1000 个活跃分区的冷启动
- **THEN** 位图一次 get + 并发游标点读，结果与全扫描逐位一致

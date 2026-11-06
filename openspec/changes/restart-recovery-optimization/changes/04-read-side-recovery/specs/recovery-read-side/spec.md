# recovery-read-side Specification

## ADDED Requirements

### Requirement: recovery-read-side 契约

开机要重建的内存态——观察表（`memory/store_query.go`）、`landedSeq`、回放投影（`memory/replay`）——SHALL 在完成点写进唯一一份快照，并在恢复第二步以「装载快照 + 从 `landedSeq` 之后消费增量」恢复；一次恢复消费的事件数 MUST NOT 与历史事件数成正比。任一状态缺失、版本不符或 `landedSeq` 不可信时 SHALL 退回全量重建并具名告警，恢复结果 MUST 与全量重建逐面一致。

#### Scenario: 增量恢复

- **WHEN** 历史 200 条事件、上次完成点后又追加 5 条，进程重启
- **THEN** 恢复只消费这 5 条增量，装载出的观察表与回放投影与全量重建逐字段一致

#### Scenario: 状态缺失退回

- **WHEN** 快照里缺观察表或 `landedSeq` 不可信
- **THEN** 恢复退回全量重建，结果与增量路径逐字段一致，并记录具名告警

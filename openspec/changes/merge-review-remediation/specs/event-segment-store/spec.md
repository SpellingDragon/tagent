## REMOVED Requirements

### Requirement: WAL 中间坏行容错
**Reason**: localfile 最小化裁决删除 WAL 追加机制（`replayWAL`/`wal_quarantined` 不复存在），损坏容错语义由快照 fail-fast（读失败即报错、不静默）与信封层 quarantine 四态归类承接（persistent-event-loop 能力准入与直接核对条款）。
**Migration**: 无迁移动作；主 spec 本条为归档 delta（2026-09-30-complete-resident-reliability-protocol）已声明 REMOVED 而未执行清理的残留，本次补执行。诊断面 `wal_quarantined` 兼容残留字段随下一轮清理移除。

### Requirement: LocalFileKV 写路径 fsync 耐久
**Reason**: fsync 可配置两档与掉电耐久宣称随机制移除（`WithFSync` accepted-and-ignored）。屏障语义收敛为：`Sync()` = 全量快照 atomic tmp+rename，成功后新进程可读回；生产级 fsync 耐久认证推迟至 rustviking 阶段。提交前必须过屏障、失败不得报 durable 成功的契约由本 delta ADDED 条款与 persistent-event-loop 能力准入条款承接。
**Migration**: 无迁移动作；主 spec 本条为归档 delta 已声明 REMOVED 而未执行清理的残留，本次补执行。`memory.fsync` 配置键与 `WithFSync` 选项一并删除（pre-release 定位，无兼容负担；配置面不留恒为 no-op 的死旋钮）。

## ADDED Requirements

### Requirement: LocalFileKV Sync 为原子快照屏障

LocalFileKV 的 `Sync()` SHALL 将全部键值整序列化并以临时文件写入加原子 rename 落盘；快照 SHALL 按分区分片（每分区独立文件），`Sync()` 只重写自上次屏障以来变更过的分区——单次提交的屏障成本 SHALL 与其触碰的分区成正比，MUST NOT 与全库键数成正比。`Sync()` 返回成功后，独立新进程 SHALL 能读回该快照代表的全部键值。快照文件本体损坏 SHALL 启动失败（fail-fast）。FileSegmentStore.StoreEvent 的提交屏障契约不变：evt/idx/必需 meta 写完且 `Sync()` 成功后才发布缓存、计数与成功结果；屏障未完成 MUST NOT 报告 durable 成功，调用方 MUST 在成功后才投影。

#### Scenario: 屏障后跨进程读回

- **WHEN** StoreEvent 提交且 `Sync()` 返回成功，进程随即终止而不经 Close
- **THEN** 独立新进程可经 EventKey 取回原文及索引

#### Scenario: 屏障失败不报 durable 成功

- **WHEN** 事件提交遇快照写入或 rename 失败
- **THEN** 返回非 nil error、保留故障证据，投影不追加，消费者不收到 durable 成功

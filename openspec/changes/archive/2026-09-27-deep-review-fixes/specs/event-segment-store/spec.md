# event-segment-store Delta

## ADDED Requirements

### Requirement: compaction 源窗口读失败必须中止本轮

Compactor 在读取任一源窗口事件（mergeEvents 的 KVScan）遇到错误时，SHALL 使本次压缩以错误终止，MUST NOT 跳过该窗口继续迁移——一次不完整的读取不能证明窗口为空，据此删除源段等于静默销毁事实。

#### Scenario: 源窗口扫描瞬时失败

- **WHEN** L1→L2（或 L2→L3）compaction 读取源窗口 A 时后端 KVScan 返回错误
- **THEN** 本次 CompactL1ToL2/CompactL2ToL3 返回错误，源窗口 A 与其余源段的 evt 槽与 meta 全部保持原样（未被 deleteSegments 删除），目标层不写入不完整合并段
- **THEN** 后续 scheduler 轮次重试同一 compaction，读恢复后正常完成迁移

#### Scenario: 冷缓存读不因压缩丢失事实

- **WHEN** 任一 compaction 曾因读失败中止且未重试成功
- **THEN** 用同一 KV 后端构造的新 store 实例（冷缓存）GetEvent 仍能取回全部源窗口事实

### Requirement: TTL 扫描读失败必须可观测

LifecycleManager 的 TTL 扫描对某窗口 KVScan 失败时 MAY 跳过该窗口本轮扫描（方向安全：跳过即推迟遗忘），但 MUST 记录告警日志，MUST NOT 静默无声。

#### Scenario: TTL 扫描后端读失败

- **WHEN** checkTTL 扫描某分区窗口时 KVScan 返回错误
- **THEN** 该窗口本轮不产生墓碑（无数据销毁），日志中出现含分区与窗口标识的告警，下一轮扫描重试

### Requirement: ReplayEvent 窗口重入与 StoreEvent 同做 sealed 降级

FileSegmentStore 的 ReplayEvent 在窗口重入（seqCounter 从零恢复）时 SHALL 与 StoreEvent 执行同一 sealed 窗口降级（Sealed→false，回到 memtable 语义恒扫描不被剪枝），两处 MUST 共用同一实现，杜绝行为发散。

#### Scenario: 重放事实落入 sealed 包络之外的窗口

- **WHEN** 一个 sealed 窗口的 MinTime/MaxTime 包络未覆盖某恢复重放事实的事件时间戳，ReplayEvent 将该事实 fresh-commit 进该窗口
- **THEN** 窗口 meta 的 Sealed 被降级为 false，随后的时间范围查询（覆盖该事实时间戳）能在结果中看到该事实
- **THEN** GetEvent 对该事实保持可达（idx 直达不受剪枝影响）

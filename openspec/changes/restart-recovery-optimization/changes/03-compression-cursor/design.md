# 03-compression-cursor 设计

## 用户裁定（r17/r19，优雅优先）

「重启」是一个事件，恢复就是它的消费逻辑，**两步**：①装载未消费的事件；②装载或重放内存状态。两步完成才开放服务。快照**只留一份**：每次启动写新的覆盖旧的，过时状态无意义。由此删掉「压缩时写快照」——运行中途写才有并发，恢复完成点写则服务未开放、单线程，竞争结构性消失。

## 技术要点

票据 recovery.ticket（族 12）字段：schema_version、digest（blob 的 sha256）、length、cursor（覆盖到的最后 fact key）。身份=schema‖cursor‖digest 内容寻址。blob 固定键 tagent/task/v1/snapshot:current，Put 覆盖、只有一份。schema 现为 2（v1 无 mutations 表，按版本拒绝而非猜）。

**两步落点**：① Host.restoreUnconsumedEvents（host_delivery.go）= restorePendingDeliveries + restoreOwed；② Run.restoreInMemoryState（run.go）= restoreChains + fileBootRecoveryTicket。第二步完成点写票，主机尚未 newHostFace，服务未开放。

不变式：全量重放 ≡ 快照 + 增量；失败一律退回全量；八表排序后确定性序列化；快照八表（含 mutations）往返、回执/拒绝在增量合并时清 owed、recommit 幂等。

## 被否方案

- 0 拷贝内存映射：可变 Go map 与只读页不兼容、需 unsafe、瓶颈不在拷贝。
- 压缩时写票（r13 旧案）：运行中写 → 与并发 Commit 竞争（审者实证 fatal map race），已删。
- 每票据一份 blob（r13 旧案）：越攒越多、过时状态无意义，改固定键单份覆盖。

## 诚实边界（r16/r19）

- cursor 是字典序、只用于选票与观测；mergeDelta 仍 Decode 全部事实、按 per-task ExpectedRevision 过滤 fold，不宣称省 I/O；I/O 点读归阶段 4。
- 本后端 Put 即 rename、Sync 为 no-op、无 fsync；两段写入只有 rename 序，失败安全（孤立 blob 无人引用 / 票据指向缺失 blob → 退回全量），不宣称 fsync 屏障。
- 恢复完成点在服务开放前，锁是防御性冗余；TestSnapshotBesideConcurrentCommits 覆盖开放期调用。

## 风险与回退

每单以复跑命令为准，失败即回退本单并回写计划。

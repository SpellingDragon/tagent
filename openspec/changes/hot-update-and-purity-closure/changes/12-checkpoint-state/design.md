# Design: 12-checkpoint-state

## D1 统一 checkpoint 事实族（载体裁决）

新增框架级事实 `event.TypeCheckpoint`（api/event/meta.go 常量 + Meta 键约定），载荷携带全部可恢复状态切片：投影幸存者、宿主全局送达/结算账（含开账 body/目标/lineage/attempt/hold）、邮箱欠账、任务终态。一个事实族、一个 offset 语义。压缩事实（compress.request/compress.product）回归纯投影职责，其 checkpoint 字段不再承载账本状态，仅作 checkpoint 触发源之一。RecoveryTicket 独立快照路线退役（用户裁决：统一而非两条腿）。

## D2 框架状态接缝（Flink 算子类比，只借不变量）

借三条不变量：① 一致性切面落在单一偏移；② checkpoint 节奏框架所有、与单消费者触发解耦；③ 恢复靠回放经过活路径，而非 bespoke 恢复读者。

- 接缝（capability seam：Definition/Provider/Consumer）：消费者注册 `Snapshot() slice` / `Restore(slice)`，框架协调者聚合。状态持有者：投影（per-session）、送达账（宿主全局，含子会话）、邮箱（per-address）、任务注册表（per-task）。协调者从各持有字段读快照（实现期 grep 定位 h.deliveryStates / r.store / r.registry 等）。
- 节奏：有界周期/提交计数 + 折叠触发 + close/quiesce；大窗口（32768）极少折叠也有载体（框架补发）。
- 一致性切面：提交锁下、分区 Seq（landedSeq, run.go:324）偏移处聚合；快照状态对应 ≤offset 的事实，晚于 offset 的结算靠回放覆盖。需「结算与 checkpoint 并发」测试钉死。

## D3 紧凑已结集合（水位）

去重需知「身份送过没有」。身份 `delivery-fresh-<seq>-<digest>` 的 factSeq 进程局部+随机（facts.go:1045），不可作全局水位；改用分区 Seq 水位（「Seq ≤ K 的输出除开账清单外全部已结」）+ 开账清单 = 完整紧凑账本态，O(开账数)。

## D4 恢复重写

boot = 读最近 TypeCheckpoint 事实 → Restore 各切片 → 回放 Seq>offset 的已提交事实经现有 observe/commit 管线（commitFactKey 的 After pass / recordMapped / attachDeclaredReplay）。删除三条全史读者：deliveryRecoveryFacts/eachCommittedEvent 全扫（host_delivery.go:1608/run.go:2170）、Unsettled 逐站点 durable 扫（invoke.go:435/471，二次挂死点）、RestoreOwed 全量重投（memory.go:427）。Unsettled 改读恢复后内存态。义务无限期语义不变：开账随 checkpoint 存活，折叠不得吃未清承诺（连带消解 8.1）。

## D5 门与格式

durable 载荷格式变更过门：golden/runtime/durable_truth.json（增 checkpoint surface、删 RecoveryTicket 条目 :211/:376）、event_integrity_scenarios.json、fold_contract、FACT-VOCABULARY、gen_godoc、anchored_boot_parity；api/event 新类型过协议 manifest/schemas（若属协议面）。回退：单域回滚。

## 审读坐标（实现起点，已核）

- 挂死链：host.go:780 Unsettled → invoke.go:435 Unanswered（每 ActiveSession）→ invoke.go:471 unsettledSessionFacts → run.go:2203 EachSessionFact → run.go:2170 eachCommittedEvent（收全索引后逐 key GetEvent，session 过滤在 visit 内 run.go:2229，不剪读取）。
- 单次 GetEvent 已 O(value)（journal.go:570 帧索引）；1MiB 双 LRU（segment_store.go:191/kv_resident.go:17）全量读抖动，-race 下数十 µs×10⁵⁻⁶=分钟级。
- checkpoint 现状：api/event/compress_product.go:46-59 opaque checkpoint 字段，仅批末产物携带投影幸存者 []anchorEntry（compress_consumer.go:217/249），Meta survivors 键落链(:441)读回校验(:462)。
- 边界两套：assembly 锚用数值分区 Seq（run.go:2615/2515）；memory/replay CompactionFact.FullBoundary 用事件键字典序（projection.go:433），读者 RebuildProjectionFromWAL 无生产调用方。统一用分区 Seq。
- RecoveryTicket：run.go:948 → DurableCommit.WriteRecoveryTicket（recovery.go:157），blob+ticket KV（recovery.go:24-32），每 boot 全量快照追加 journal 致膨胀。
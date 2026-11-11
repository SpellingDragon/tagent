# 12-checkpoint-state 设计（契约冻结，派发据此实施）

> 用户裁决：统一 checkpoint 事实族（Flink 框架状态管理）。本设计冻结事实形状与恢复语义，实施不得擅改签名；偏离上报编排者。

## D1 取代与继承

- **取代** domain 06 D3 的「D（快照装全状态）否决维持」一条：用户显式裁决框架级 checkpoint 承载全部可恢复状态。06 D3 其余（无损早停不存在、C 外部存活位索引否决）不变。
- **继承** 06 D4 折叠锚定理：折叠瞬间是每会话同步一致性安全的检查点时刻；本域把一致性切面从投影扩展到宿主全局状态，折叠成为 checkpoint 的触发源之一。

## D2 checkpoint 事实族（冻结形状）

- 事件类型 `event.TypeCheckpoint = "checkpoint"`（api/event/meta.go），登记 FACT-VOCABULARY。
- 事实键 `checkpoint:<partitionSeq>`（确定性、按偏移单调）；经 `commitFactChecked` 落链（assembly/facts.go:68）。恢复取「最新 checkpoint」= 索引 newest-first 过滤 type=checkpoint 的首条（O(1) 摊销，不随历史增长）。
- 载荷 `CheckpointPayload`（JSON）：`Offset`（一致性切面的分区 Seq 水位 `landedSeq`，run.go:324；**不得**用进程局部 factSeq）+ 注册式命名切片聚合（D3 接缝）。切片集（r47 实施裁定）：
  - `projection`：每会话投影幸存者（复用 anchorEntry）+ 各自 seq 边界。**投影恢复以继承的折叠锚为主路径**（domain 06 D4 定理，折叠锚即投影的 checkpoint）；checkpoint 的 projection 切片承载锚载荷，全局 Offset 不裁剪投影链（投影按各自锚边界回放），避免锚落进全局覆盖区。
  - `delivery`：开账清单（identity/state/body/target/lineage/heldAt/attempt）+ 已结水位（「landingSeq ≤ Offset 的输出除开账清单外全部已结」）。
  - `mailbox`：欠账通知清单（task.OutboxNotice）。
  - `inputs`：未应答输入（checkpoint 覆盖历史后待驱动轮次的存续，正确性必需）。
  - `tasks`：任务终态集合 + 水位（r47 用户裁决纳入：有界恢复不变量须覆盖任务态，registry 全史读 restoreChains 降为兜底，见 D4）。
- 一身份一不可变事实：checkpoint 不覆盖旧事实，每次新增；旧 checkpoint 可被后续折叠清理（非本次范围）。

## D3 状态接缝与协调者

- 接缝（capability seam）：`Stateful`{ StateName() string; SnapshotState(ctx) ([]byte, error); RestoreState(ctx, []byte) error }，消费者经 `Run.RegisterStateSlice` 注册；框架协调者按名聚合各切片为 CheckpointPayload。切片先于持有者到达则暂存、挂载即恢复，Boot 注册后校验 PendingRestore 为空（否则具名拒 boot）。
- 状态持有者：送达/结算账（Host，宿主全局含子会话）、邮箱（r.store MemoryStore）、未应答输入（Host）、任务注册表（r.registry，r47 纳入）、投影（per-session，以折叠锚实现）。
- 节奏（框架所有，与单消费者触发解耦）：① 每 N 条提交事实（N 机制常量，登记 window_bounds）；② 折叠产物落链时；③ close/quiesce 收敛时。三者皆请求一次切面，切面在**安静点无锁**取（见 D3b）。
- **D3b 安静点无锁切面（r48 勘误，改写 D3 原「提交锁下取切面」与 D6 条件 1 的「同一读点」）**
  - 旧模型（已删）：协调者持提交锁 `cutMu`（后为 `landing`/`checkpointMu` 分离锁创可贴）跨持有者快照，持锁期间调用 `SnapshotState`；持有者 flush/commit 重入该锁 → 消费者等 `r.mu`、协调者持锁等持有者，LongRun gen36 死锁（证据 `/tmp/hupc-stall/gen36-stacks.txt`：facts.go:69 等 cutMu，checkpoint.go:167 持 cutMu 在 tasksStateSlice 快照内）。
  - 新模型：**checkpoint 请求进入单一写者事件管线，优先在安静点被消费**。① 先读水位 `landedSeq`（`checkpointWatermark`，仅取 journal 分区序列锁）；② 排水到安静点：`commitFence` 计「事实落链 → 该事实对持有者派生状态落定」这一整段在飞单元，协调者等其归零（等而非持锁关闭提交门；起点晚于水位的提交落链必在切面之上，由回放覆盖且按身份幂等）；③ 在**不持任何 run 锁**下逐个调用各 `Stateful.SnapshotState`（纯读契约，见 D3c）；④ 聚合经 `commitFactNoCadence` 一次普通追加落链（短暂分区序列锁）；⑤ 恢复其他处理——协调者全程未阻塞任何提交，无需显式 resume。`checkpointMu` 仅序列化「一次只取一个切面」（两次切面同一 slot 会产生同键异载荷），不参与提交路径；`landing` 回归 journal 追加本职。
  - 一致性论证（取代「同一读点」）：事实的落链与其状态应用同属一个在飞单元 ⇒ 安静点上无半应用；`Offset` 先于排水读取 ⇒ 先于 Offset 落定的事实其状态必已在快照内；晚于 Offset 落链的事实由回放按身份重新应用（幂等，最多重复一次、绝不漏跳）。
- **D3c 接缝纯读契约**：`SnapshotState` 只陈述持有者已派生的内存态，禁 flush durable、禁 commit、禁等待自身提交所需的锁（事实已由 journal 持久化，链是真相，快照是派生态读）。状态跟随自身已提交事实的持有者，须在该提交的在飞单元内应用该状态（`writeDeliverySettlement` 按此把结算落链与账户状态标记为一个 `commitFence` 单元）。五个持有者核查结论：projection/inputs（`assembly/checkpoint.go`、`checkpoint_inputs.go`）、delivery（`host_delivery_checkpoint.go`）、mailbox（`runtime/state/mailbox_checkpoint.go`）、tasks（`runtime/task/checkpoint.go`）的 `SnapshotState` 均为纯读，无 flush/commit/重入锁，无需改写。

## D4 恢复重写（冻结语义）

- Boot：读最新 checkpoint 事实 → 各 Stateful.RestoreState 载入切片 → 回放 landingSeq > Offset 的已提交事实经 **`seedFact` 隔离重建**（run.go:2846，只写内存账本，**不调 commitFactChecked、不跑 After pass**，故 deliveryConsumer/settle/meditation 回放期不触发外部副作用）重新派生偏移后状态。投影按各自折叠锚边界回放（不被全局 Offset 裁剪）。**r48 勘误：原「经现有 observe/commit 管线（After pass/recordMapped）」为伪断言**——活路径 After 才带副作用，恢复回放必须走 seedFact 隔离路径，否则会重发/重驱动＋noteCheckpointCommit 风暴。at-least-once 窗口（Send 成功回执未落链会重发、turn 在飞崩溃会重驱动）是文档接受的语义，由八键精确读/durable attempt terminal 守卫收敛。
- **删除/降级** bespoke 全史读者：`deliveryRecoveryFacts`/`eachCommittedEvent` 全扫（host_delivery.go:1608/run.go:2170，改 eachFactPastCut 按切片套切面）、`Unsettled`/`Unanswered`/`unsettledSessionFacts` 逐站点 durable 扫（invoke.go:348-490，二次挂死点，改读恢复后欠账输入+驻留窗口）、`RestoreOwed` 全量重投（memory.go:427，邮箱改由 checkpoint 切片恢复）。**`restoreChains` 任务全史读降为「无 checkpoint 年轻链」兜底**（r47：任务终态进 checkpoint 后，主路径不再 O(历史任务数)）。`Unsettled` 改读恢复后内存态。
- 无 checkpoint（年轻会话/首次 boot）：退化为该会话链全量回放（链小，有界）；坏 checkpoint 具名拒 boot，不退全量（checkpoint 是状态真相载体，坏即拒）。

## D5 退役 RecoveryTicket

- 删 `fileBootRecoveryTicket`（run.go:948）→ `DurableCommit.WriteRecoveryTicket`（recovery.go:157）→ `recoveryTicketWriter` 接口与实现、blob+ticket KV 键（recovery.go:24-32）。
- durable_truth.json 钉它的条目（:211/:376 附近）移除，新增 checkpoint surface。

## D6 正确性三条件（测试钉死）

1. 边界一致性：快照状态取自 Offset 之后的安静点（D3b：先读 Offset、排水到无在飞单元、再无锁纯读），「结算与 checkpoint 并发」测试——晚于 Offset 的结算靠回放覆盖，早于 Offset 的必在快照。
2. 开账嵌入：折叠时刻未清义务（含 body）原样进 checkpoint；8.1（owed 输出被折叠收走致应答丢失）由构造消解。
3. 水位压缩：已结集合用分区 Seq 高水位 + 开账清单表达，O(开账数)，不随历史增长。

## D7 门清单

durable_truth.json（加 checkpoint surface、删 RecoveryTicket）、event_integrity_scenarios.json（LongRun 六分片）、fold_contract.json、FACT-VOCABULARY（TypeCheckpoint）、gen_godoc、anchored_boot_parity、window_bounds.json（节奏常量 N）。合并门：LongRun 全绿 + 每代 boot 成本平坦。
# 06 退位分层 · 设计

> ## ⚠️ 勘误（2026-11-16，用户裁定反转）：本文件描述的是**已整体删除**的机制
> 本域由「建退位」改为「删退位」（用户裁定原话与依据见 [tasks.md](tasks.md) 头部与一级 [design.md](../../design.md) D6.13 勘误）。故下文「冷层载体／搬迁三步／身份读三态／排他安静点／journal-only 能力／脚手架接线／水位制判据」**全部是已删机制的设计记录**，保留为历史证据（5.8：历史不可变，推翻结论记在本节与 tasks.md 头部），**不得据以实施**。
> **现行机制**（本域反转后的事实）：热层只追加、不搬迁、无冷层载体，其字节增长是显式声明、单独拥有的关切（MUST NOT 被声称有界）；开档读入量＝上次索引陈述水位以来的窗口，由可寻址索引承担（06 域）；既有冷档数据由外部脚本 `scripts/fold-cold-root` 折入新根，框架零分支。本域现行有效叶＝**08.R3（删退位机制）／08.R4（删切面族＋外部折回脚本）**。
> 第 3 行引用的 D6.1／D6.2／D6.7 已在一级 design.md 同名条目上标作废；D6.3 存活权威仍成立但定位为性能层（D6.13 前半）。

> 静态决策见一级 [design.md](../../design.md) D6.1（取向乙）、D6.2（搬迁非删除）、D6.3（存活键集权威单一）、D6.7（引擎能力 journal-only＋排他安静点）、§五（共享面冻结签名）、D7（风险与回退）、§六 S1。本域只列实施要点与实证触碰集，不改冻结签名。

## 一、冷层载体与搬迁三步

**冷层＝同引擎第二 journal 根**：`OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: <root>/cold})`（config.go:136-147），落盘 `<root>/cold/kv/journal.log`（journal.go:42 `journalFileName`）。同帧格式、同 CRC32C 校验（journal.go:62-64）、同 32MiB 帧上限（journal.go:56）。冷层只追加，不重写；不新增文件类型、不新增布局词汇。冷层**跨趟累积**：根非空不是拒搬理由（区别于一次性迁移的 `ErrMigrateTargetNotEmpty`，migrate.go:37/91-93）；已持同一身份时字节相同＝幂等跳过复制，字节不同＝具名拒绝，MUST NOT 盖写（一身份一不可变事实）。

搬迁对象＝存活键集的补集（§五冻结）：`¬live`，其中 `live = checkpoint.Survivors ∪ {ref : ref.seq > Offset} ∪ obligationLeases ∪ chainReachableBlobs(live)`。三步顺序不可换：

1. **复制**：`¬live` 的键值对按 256 键/批写入冷层（借 `migrateBatchKeys`，migrate.go:52）。一个事实的段三键（`{pid}:idx:*`／`{pid}:evt:{window}:{seq}`／窗口 meta）与其 `ref:`／`blob:` 同批搬，保持「先写什么后写什么」的既有顺序不变量（memory-architecture.md:44 第 8 条）。
2. **校验**：新开冷层视图逐键读回、逐字节比对（借 `verifyMigratedRoot` 的复制＋校验半段，migrate.go:121-175）；键数不等或字节不等＝`ErrMigrateMismatch` 具名（migrate.go:48）；`ref:` 无独立 `blob:`＝`ErrSnapshotCorrupt` 具名（migrate.go:150-160，与 kvOriginalStore.load 同一拒绝）。**校验不过：热层原样不动，冷层留档不读**（源只读纪律，migrate.go:68-71）。
3. **热层重写＋替换（swap）**：把存活键集＋尾按**逆帧序去重**反写为新 journal（每键只留最新记录），新文件读回校验后替换 `journal.log`，热层视图（index/order/resident）随之重建。替换成功之后，热层旧文件不再被读。

**反事实检验（热层旧文件移除，谁真的坏？）**：无人坏——被移除的每个键都已在校验步逐字节落在冷层，`GetEvent` 透明回冷层返回原字节（§五 identity-read 不变量），读全史的消费者（host_tools.go:926 `eachDeliveryFact`、scripts/recover-kv/main.go:140）仍按身份读到正文。反之，若校验未过就移除，坏的是身份读——所以校验是 swap 的**前置条件**，不是后置检查。

**冷层厚度是接受边界（显式声明，非静默未达成）**：冷层字节随已结事实累积，本域不治；S1 量的是**热层 `journal.log` 字节**有界于存活键集＋尾。冷层治理（若将来需要）另立裁定，不在本域偷偷加机制。

## 二、identity-read 不变量（本域第一红线）

`FileSegmentStore.GetEvent`（segment_store.go:527-557）今天是两态：index 命中→读正文；未命中→`(false, nil)`。搬迁后「热层未命中」不再等价于「事实不存在」，必须改**三态**：

| 情形 | 答复 | 依据 |
|---|---|---|
| 热层 index 命中 | 原路径（cache→readBodyLocked） | segment_store.go:533-547 |
| 热层未命中 ∧ 冷层持有 | 冷层原字节＋**原 indexPosition**（partition/window/seq 不变，身份不变） | §五 identity-read |
| 两层皆未持有 | `(false, nil)`（真不存在，如被拒提交的键） | 既有语义保留 |
| 冷层持有但正文读不出／校验失败 | **具名拒绝** `ErrSegmentBodyMissing`（segment_store.go:205/546），MUST NOT 降级为 `(false, nil)` | D7 静默降级 |

- **冷层懒开**【机制已删】：冷层视图只在第一次冷读时开，boot 路径不开（02 的派生索引加载不得被冷层拉回 O(全史)）。冷读触发后冷层位置索引按冷层键数计费（journal.go:99-100 每键一条小条目）——这是**显式全史读消费者自身的成本**（`session-checkpoint-material` 的 Requirement「派生态有界且不删除已提交事实」点名「恢复期读全史的消费者」），不是 boot 成本；S2 锚量 boot，不被本域拉红。此边界写入 spec 场景，不藏。
- **身份读消费者逐一过**（✅ grep `.GetEvent(`）：assembly/read.go:118/174/202/236/328（查询面）、run.go:2281（`CommittedEvent`）/2311（`eachCommittedEvent`）/2640/2723/2761（boot seeding 与 restoreSurvivors）/2854/2888、checkpoint.go:781/901/999、facts.go:370/401（重复提交检查：搬迁事实再提交必须仍判重复，不得因热层未命中而二次落链）、host_tools.go:931（全史扫）、scripts/recover-kv/main.go:140。
- **原文 store 同责**：`kvOriginalStore`（kv_original_store.go:47）的 ref/blob 身份读同样透明回冷；`Guard` 红线「MUST NOT treat a released hold as an unreadable reference」（guard.go:17-21）在搬迁后仍成立——释放租约不等于原文不可读。
- **枚举面**：`EachEvent`（segment_store.go:595）/`EachIndexNewestFirst`（:709）/`Events`（:750）是读全史消费者的入口，搬迁后 SHALL 仍遍历到冷层事实（热层存活键集＋冷层已搬迁），且保持提交序。

## 三、排他安静点（安静点不是 fence）

安静点只保证「无半应用」：`commitFence` 是计数等待，**不排斥提交**（assembly/checkpoint.go:23/:102-107「The fence never excludes a commit」）。在途歧义（originals-before-segments，孤儿 vs 在途不可区分，D7）要求搬迁期间**没有新提交落地**，故三段闸：

1. **Run 停写闸**：提交入口（assembly/facts.go:84 `commitFactNoCadence`，其 `flight.enterSession`:88）前置闸标志。闸关时新提交**等待**，不拒绝落链（拒绝＝丢事实，违 P1）；等待有上限，超时＝具名错误上报，不静默丢。闸开后按原序落链。
2. **drain 归零**：复用既有 drain（checkpoint.go:99-121），等 `inFlight` 归零——已落地的提交其状态已应用，存活键集判定才稳定。
3. **swap 持引擎锁**：`journalKV.mu`（journal_kv.go:105）只在热层重写替换与视图重建期间持；复制＋校验阶段由停写闸保证无新写入，不长持引擎锁阻塞读。

纪律（不可违反，来自既有构造）：

- **闸内不持 Run 锁读切片**：安静点的构造是「单写者流水线的排序，不是覆盖它的锁」（checkpoint.go:18-26），持有者在自身提交锁内不得等待向它要状态的闸——否则死锁。停写闸只挡**新提交进入**，不挡已在飞的工作完成。
- **读不挡**：搬迁期间热层原样可读，直到 swap 瞬间；swap 后热层视图重建，冷读接管已搬迁身份。
- **同根第二视图无锁**（config.go:174）：本域不引入跨进程锁；第二实例由生产 open 路径拒绝（既有纪律）。冷层根与热层根是不同目录，两个写者句柄互不交错半帧（config.go:173）。
- **闸外不判定**：任何 `¬live` 判定只在闸内做；闸外扫描只读不搬（避免用在途视图决定搬迁）。

## 四、存活键集权威与脚手架接线

- **oracle 改链可达**：`Lifecycle.Scan`（lifecycle.go:217-255）现判据＝租约（:230 `Held`）＋龄期（:238-245 `cfg.Window`/`recordedAt`）＋容量（:251 `evictOverCapacity`:260-295）。改为：租约检查保留 ＋ **存活键集补集判定**；龄期与容量 MUST NOT 单独触发搬迁。`Reclaim`（lifecycle.go:321-343）在退位路径＝第一节三步搬迁，不是物理销毁事实。
- **红线扩写**：lifecycle.go:64-68 的「MUST keep an original readable until compaction once it is tombstoned」在本域扩为「**搬迁后仍按身份可读（冷层）**」；`Tombstone` 语义＝待搬迁标记，`ReclaimTombstoned` 实现＝复制＋校验＋热层移除。既有四条错误保留并继续生效：`ErrCapabilityIncomplete`（:85，持久档缺能力 fail-closed）、`ErrRecoveryIncomplete`（:89，恢复清点未完成不放行）、`ErrEnumerationIncomplete`（:92，枚举失败不返回部分列表）、`ErrProtected`（:95，受保护只具名拒绝）。
- **义务租约的生产载体**：`Retention`（retention.go:60-65，登记＋原时间戳） over `MemoryGuard`（guard_memory.go:23-34，引用计数＋租约代次）。持有者＝**共享资源 owner**（未结投递义务、恢复资源），不是任一 agent（retention.go:5-11 / guard.go:18）。
  - 未结→`Hold`：`noteDeliveryState`（host_delivery.go:671-690）在 `deliveryStateUnsettled` 分支置 pin 处同时登记租约。
  - 结清→`Release`：同函数 else 分支删 pin 处释放，与 `Run.unpinFact`（run.go:2210-2217）同一时机；`keepPinnedLocked`（run.go:2247）的热窗豁免与租约同源，不另立第二套保护。
  - 恢复侧：`restoreObligation`（host_delivery.go:2069-2073）对已结态早退（dropHeld＋noteDeliveryState），故恢复后已结义务不持租约——租约可行性的既有证据。
  - 持久态：结清事实落链＋checkpoint 的 Pins/Owed[].State 切片，重启后租约按链重建，不靠进程内存。
- **headless-retired 审计 pin**：`retireHeadlessDelivery`（host_delivery.go:860-873）结清义务但**保留审计正文**。本域按独立审计租约（holder＝审计）保留该正文于存活键集，默认 keep（用户无偏好）。MUST NOT 因「已结清」把它搬走。
- **journal-only 能力诚实**：`journalKV`/`FileSegmentStore`/`kvOriginalStore` 今天对 `EnumerateKeys`/`Tombstone`/`ReclaimTombstoned` **零实现**（✅ grep：唯一实现者是非持久的 `MemoryLifecycleStore`，lifecycle.go:381/412/431/444，只供能力分档与测试，不承载搬迁）；本域实现于 journal 档。rustviking file 档（rustviking_kv.go）无跨键原子（D6.7）→不实现，按既有分档处置显式声明能力缺失（storage-lifecycle-completion 主 spec:66），不伪造保护已生效；memory 档不持久→不搬迁。
- **配置面**：`StorageConfig`（config.go:46-63）增退位开关与冷层根；`hostStorage`（host_document.go:76-79，今只有 type/root）增对应 yaml 键，经 host.go:635 接线。缺省**关闭**＝热层只追加（回退态与基线零 diff）；启用由部署声明。新键过 schema_allowlist 门，主 spec/golden 同步交 05 域；不触 agent 配置面。

## 五、保序与帧上限

- `order` 按帧记（journal.go:115-121）：一键被后续记录重写则在每条记录处各点名一次，逆走先遇最新。热层重写 MUST **逆帧序去重**（每键只留最新记录）后反写，否则最新 checkpoint 被旧 checkpoint 盖过（D7 保序陷阱）。
- checkpoint 键族 `checkpoint:<partition>:<seq>`（api/event/checkpoint.go:75-83）：重写后最新 checkpoint 仍是 order 中最新的 checkpoint 键；分区各自编号，不得跨分区重排。
- 单帧 ≤32MiB（journal.go:56）：重写分批，超限具名拒绝，不静默截断。
- 既有保序锚保持绿：`TestJournalStatesItsCommitOrderNewestFirst`（journal_kv_test.go:72）、`TestEachIndexNewestFirstStreamsTheCommitOrder`（segment_store_test.go:508）。

## 六、实证触碰集（✅ grep／读码，不照搬一级清单）

- `memory/ledger/`：journal.go（journalLog:103、order:121、journalReplay:433、openJournalLog:471、append:562、read:611、帧上限:56）、journal_kv.go（journalKV:20、openJournalKV:33、Commit/mu:101-105、valueLocked:133、eachCommittedNewestFirst:150）、segment_store.go（commitOrder:224、Load:281、StoreEvent:455、GetEvent:527、readBodyLocked:571、EachEvent:595、EachIndexNewestFirst:709、ErrSegmentBodyMissing:205）、kv_original_store.go（kvOriginalStore:47、ErrSnapshotCorrupt:27）、lifecycle.go（红线:64-68、LifecycleStore:69-79、错误:85-98、Scan:217、evictOverCapacity:260、Reclaim:321）、retention.go（Retention:60）、guard.go（Guard:22）、guard_memory.go（MemoryGuard:23、Hold:43、Release:74、Protected:103）、config.go（StorageConfig:46、OpenAtomicStore:136-147、OpenTier:178、第二实例纪律:174）、migrate.go（借复制＋校验:76/121、migrateBatchKeys:52、ErrMigrateMismatch:48）、kv_resident.go:17（正文常驻预算，冷读复用）
- `assembly/`：checkpoint.go（commitFence:99、安静点纪律:18-26）、facts.go（commitFactNoCadence:84、重复提交检查:370/401）、run.go（pinnedFacts:280、unpinFact:2210、keepPinnedLocked:2247、CommittedEvent:2267、eachCommittedEvent:2304）、host_delivery.go（noteDeliveryState:671、retireHeadlessDelivery:860、restoreObligation:2069）、host.go:635（存储配置接线）、host_document.go:76（hostStorage）、read.go:118/174/202/236/328、host_tools.go:926（eachDeliveryFact）
- `scripts/recover-kv/main.go:140`（正式修复入口，身份读消费者）
- 测试：memory/ledger/{journal_kv_test.go:72, segment_store_test.go:508}、tests/integration/history/{recovery_before_reclamation_test.go:33, storage_capabilities_test.go:25/381 corruptLastRecord 损坏注入先例}、assembly/{host_delivery_test.go:930 TestSettlementFactsStayHotWhileUnsettled, checkpoint_test.go:550 TestCheckpointWaitsForTheQuietPointOfThePipeline}、examples/wechat-bot/smoke_test.go:3566（TestEventIntegrityLongRun，真实宿主 121 世代 SIGKILL 重启）
- **不改**：rustviking_kv.go（file 档不做，D6.7）、`MemoryLifecycleStore`（lifecycle.go:381，非持久档唯一实现者，不承载搬迁）、memory/replay（投影纯回放不写链）、api/config agent 配置面（config-surface-normalization 禁区）、examples/wechat-bot/.wechat-config/data（禁读）、冻结归档

## 七、子步序（每步 go build 保持可编译）

6.1 引擎能力（三方法＋冷层根＋复制校验＋热层重写保序）→ 6.2 排他安静点 → 6.3 搬迁 ¬live ＋ GetEvent 三态透明回冷 → 6.4 脚手架接线（oracle 改链可达＋义务租约＋审计 pin）→ 6.5 S1 锚转绿＋负锚＋真实宿主消费者锚。

6.1–6.3 属引擎与读路径，6.4 属生命周期与宿主接线；6.5 是验收面，依赖前四步全部落地。S1 在 6.1–6.4 期间保持红（能力未接线＝未生效），6.5 转绿；回滚（关闭退位）必红。

## 八、风险与回退（承 D7）

- **冷层损坏**：帧校验失败／键缺失／ref 无 blob → 具名拒绝（`ErrSegmentBodyMissing`／`ErrSnapshotCorrupt`／`ErrMigrateMismatch`），MUST NOT 静默丢事实或答 miss。损坏注入用既有 `corruptLastRecord` 手法（storage_capabilities_test.go:381）。
- **identity-read 破口**：任何消费者把「热层未命中」当「不存在」即违红线——重复提交检查（facts.go:370/401）是最隐蔽的一处（会二次落链同一身份）。三态判定＋消费者逐一过锚。
- **在途歧义**：只在排他安静点内搬迁；闸外只读。停写闸等待超时＝具名上报，不拒绝落链。
- **保序陷阱**：逆帧序去重反写；两条既有保序锚＋最新 checkpoint 认定锚保持绿。
- **死锁**：闸内不持 Run 锁读切片；持有者自身提交不得等待向它要状态的闸（checkpoint.go:18-26 纪律）。
- **有界恢复被拉红**：冷层懒开，boot 不开冷层；S2 四量（journalBytes/indexEntries/durableLoadCount/wallClock，§五）由 04 定义，本域消费不得使其回红。
- **审计正文误搬**：headless-retired 独立审计租约保留（默认 keep）。
- **回退**：退位是增量能力，关闭开关＝热层只追加（与基线零 diff），**不删已搬迁事实**（冷层恒可读，身份读不变量继续成立）。完整回滚（撤掉本域代码）→ S1 必红，作 F3 证据。core（01–05）回退＝恢复全史读，与本域回退互不掩盖。

## 九、重整：与整事实提交的接缝（08.R1）

### 九·一 现状的单位不一致（✅ 读码）

- 提交侧一条事实是**两笔**：原文族（`kvOriginalStore.Put`，assembly/facts.go:377）与事件三键（`StoreEvent`，facts.go:395）。两笔之间可以崩溃，留下「原文在、事件不在」的孤儿（memory-architecture.md:260 已承认）。
- 搬迁侧却按「一个事实的段三键＋其 `ref:`/`blob:` 同批」对待（6.1 的 `keepKeysLocked` 判据扩到原文族），并要求族拆被拷贝校验以 `ErrSnapshotCorrupt` 具名拒绝（6.3 消费者锚的红腿）。
- 于是**孤儿原文没有可对齐的事件身份**：它既不属于任何「被结清的事实」，也不属于任何 checkpoint 存活者。今天它要么留在热层（占 S1 字节），要么被搬走（`ref:`/`blob:` 按身份仍可读，但没有任何事件能证明它该存在）。这条分支当前没有被具名规定，属**不明残余**。

### 九·二 11 闭合后的形状

- 提交单位＝搬迁单位＝一条事实的完整键集（原文族＋事件三键）。此时「族拆」不再是可构造状态，孤儿原文只能来自**既往数据**（11 落地前的历史根）或**外部写者**（`recover-kv --restore`）。
- 08.R1 必须对孤儿原文给一条具名处置，二选一且写进 spec 场景：
  - **甲 留热并计入自然成本**：孤儿原文留热，S1 判据只对「有事件身份的事实」有界；须在 S1 报告里单列孤儿字节，MUST NOT 混入存活键集字节。
  - **乙 搬冷并保持可读**：孤儿原文按 `ref:`/`blob:` 身份搬冷，冷层读仍答复原字节；须在报告中单列「无事件身份的原文族」计数。
- 禁止第三态：既留在热层又在报告里被算作已搬迁。

### 九·三 回退与身份读的整合验收（08.R1 交付面）

- 关闭退位（`relocatableStorage` 不再开启，assembly/run.go:506-515）后：热层只追加、已搬事实仍按身份可读、重复提交仍判已存在（依赖 11 的提交语义）。
- 身份读三态（热命中／冷持有／皆无）在冷层损坏时具名拒绝（`ErrSegmentBodyMissing`/`ErrJournalCorrupt`），MUST NOT 答「不存在」。
- 搬迁复制的校验单位与提交单位一致：一次整事实提交的键集要么整批在冷层校验通过，要么热层原样不动。

## 十、重整：租约/脚手架与 D6.13 的裁定门（08.R2）

### 十·一 D6.13 的既有裁定（非本轮新裁）

D6.13 原文（[design.md](../../design.md)）：「正确性＝identity-read＋全局提交序；存活权威是性能层非正确性门；退位判据简化为水位制（水位前已结搬冷、尾留热）」。据此：

- 退位判据 SHALL 是**水位制 keep 集**（实现即 `keptHotKeys`：尾＋每链最新 checkpoint＋framework checkpoint），而不是更早的 D6.3 存活权威并集。
- 冷层按身份可读时，「键不在keep」不等于消费者受损。租约或保护能力是否保留，取决于独立的业务保证是否由现有状态与存储完整承担，以及搬迁后的真实读取/交付/恢复成本；不预先限定只能通过扩大keep实现。

### 十·二 三候选与判定测试（08.R2 只做判定，不实施）

| 候选 | 形态 | 前提 |
|---|---|---|
| 保留并接线独立能力 | 复用已有保护能力表达经证据确认的业务保证，不另造同义账 | 搬迁后的真实消费者行为或已批准成本目标需要该能力，且现有机制未承担 |
| 删除冗余脚手架 | 删除零调用且职责已被现有机制完整承担的结构及伴生引用 | 逐项证明原有保护、交付、恢复、原文可读仍成立，不仅看keep成员关系 |
| 调整既有职责 | 将有效部分并入既有存储/义务状态接口，删除重复部分 | 存在真实职责但当前结构边界错误；以全局复杂度与实际消费者验证取舍 |

**判别测试（拟新增，尚未建立）**：`TestRelocatedObligationMaterialStillServesItsConsumer`。构造未完义务引用的事实离开热层，再经正式重启恢复、原文读取与补交付路径验证内容、归属和未完义务不丢；同时测读取成本。另构造冷层缺失/损坏的反例，确认具名失败而非假成功。keep成员关系只解释存储位置，不能决定能力是否必要；即使行为正确，也要按G3目标核成本。

### 十·三 不许留第三态

无论取甲、乙、丙，本域 MUST NOT 留下「存在但零生产调用、语义与水位制判据冲突」的脚手架。选乙时须同时移除 `skeleton_assert.go:150` 的引用与相关测试，并在 spec 的 MODIFIED 条款里删除「脚手架 SHALL 接线生产」的硬要求；选甲/丙时须把接线点写进 spec 场景与消费者锚（`grep -rn "NewLifecycle\|NewRetention(" assembly memory runtime --include=*.go` 命中生产文件）。

## 十一、重整后的子步序与回退

- 08.R2（判定，只读＋拟新增判别测试）→ 08.R1（整合，依 06.R2＋11.R2）。R2 先定留删，避免 R1 在两种形态上各写一遍。
- **回退**：R1 的回退＝关闭退位开关（热层只追加，已搬事实冷层恒可读）；R2 选乙后的回退＝恢复脚手架文件与引用（回退后必须回到零调用原状，不得顺手留下半接线）。两者都不删已提交事实，也不触发迁移。
## 十二、08.R5：外部转换的两条异常前置（源离线、失败目标不切换）

**裁定在先**：本叶把「源必须离线」从运维约定升成脚本的**可判定前置**，并把「目标半成品」的可观察行为写死。既有契约（`docs/wiki/memory/memory-architecture.md` §六）已写「每个日志只复制，拷贝期间长度或修改时间变动即 `ledger.ErrMigrateSourceUnchanged`」；本节不改契约文本，只把实现与证据补到能过反事实检验的粒度。

### 十二·一 整源盖戳而非逐日志自校验

- **原实现的反例（实测）**：`readTier` 只在**各自**日志读前读后比较 size/mtime。热档读到一半时冷档被追加，冷档自己的「前」戳已在新状态 ⇒ 两次读取被折成一份「热旧冷新」的混合快照，且脚本会照常报 ready。这正是 requirements 里「不把多次源读取包装成在线一致快照」所禁的形态（负控实测：去掉 `holdSource` 后同一用例把变动后的源折成计划并列出 `Tiers:[hot cold]`）。
- **修法**：`stampSource` 在读到任何一字节之前对每个存在的日志取「长度＋修改时间」；`readSource` 读完全部日志后 `holdSource` 一次；`-restore` 在 `verifyFold` 之后、`printReport` 之前再陈述一次。三点位置各有职责：第一处挡「读到了半新半旧」，第二处挡「报告了一个已在上电的源被折完」，第三处使 `-restore` 的整段执行都在「源未动」的声明之下。
- **为什么不是「再读一遍比对键集」**（`migrate.go` 的 `ErrMigrateSourceUnchanged` 形态）：折回要的是**字节与提交序**保真，二次读键集只能证明键集没变，不能证明字节没被重写；盖戳比较的是日志文件本身，代价是两次 `stat`。
- **为什么不是 flock 独占源根**：`acquireRootWriteLock` 会在源根里建 `.write-lock` 文件，与「源根一字节不动」和干跑零写的既有红线直接冲突。故本条只做**事后拒绝**，不假装能拦住不合作写者。

### 十二·二 确定性同步点（受控注入，具名披露）

「写入者恰在读窗口内改源」无法从外部调度：并发 + 睡眠碰撞会把这条锚变成 flaky，而按时间窗重试等于用非确定性证明确定性。故 `readTier` 读完一个日志后调用包级 `journalReadHook(dir)`（命令为 `nil`），用例在该点移动源，拒绝仍由命令自己的 `holdSource` 作出。

- **这条结构的性质**：生产零消费者的测试注入点，不满足「第二使用方独立存在」，故**不据它主张任何生产语义**；它只换取「同一读窗口内的变动」这一无法从外部复现的输入。
- **反面记录**：拒绝逻辑本身没有测试专用分支——用例走的是 `foldRoot` 的同一条 `holdSource`，故「守卫被接线」与「守卫判得对」是同一次测量；但「CLI 子进程内源变化被拒」**未**取证（注入点必须同进程），已在 tasks 记录为未验证面。

### 十二·三 目标失败与半成品

- **已判据**：目标已有键 → `ledger.ErrMigrateTargetNotEmpty`；目标被第二个写者持有 → `ledger.ErrRootWriteLocked`（既有锚保留）。
- **本次写死的行为**：批次被日志帧上界拒（`ErrJournalFrameTooLarge`）或回读不等时，`foldRoot` 返回错误 ⇒ `printReport` 不执行 ⇒ stdout 无 `read back by identity`（ready 声明只出现在成功路径）。已落的事实留在目标根，**不删、不覆盖**：再跑一次以 `ErrMigrateTargetNotEmpty` 拒绝，须由操作方先移除该根。**不自动清理半成品根**：清理是删除事实的动作，脚本不持有「哪一半是垃圾」的授权；把删除留在运维手里，与「源不切换」同一取向。
- **证据取材**：半成品用「原文批次越过 32 MiB 帧上界」构造（2×17 MiB），因为它是**内容驱动**的确定性失败，不依赖注入、不依赖盘空间；其它写入失败形态（盘满、权限撤销、进程被杀）共用同一错误出口，未逐个取证。

### 十二·四 与 R3／R4 的验收关系

本叶只补两条异常前置的证据与实现，不复活任何退位机制，也不改 `main.go` 之外的框架面：`08.R3/R4` 的删除与折回能力仍按各自锚验收，本叶不据「三锚绿」外推整域完成，也不触发 F 任务或归档。

### 十二·五 运维前置的落点（08.R4 交付③）

08.R4 交付③「升级说明记录该运维前置与 `file` 档 `ErrBatchNotAtomic` 后果」落在 **`docs/OPERATION.md` §F「存储面升级前置：带冷档的根必须先折回新根」**（`:246-250`，随 08.R5 同批落盘），不在本文件重复一遍：

- `:248` 干跑 → `-restore` 的两步命令与「默认干跑零写」；
- `:249` 源根只读且必须离线（整源盖戳，任一戳变动即 `ErrMigrateSourceUnchanged`）；
- `:250` `file` 档对一条事实的三键批次答 `ErrBatchNotAtomic`，故它从未产生可折键集，脚本按 `ErrSourceUnstated` 具名拒绝；目标根须空、一根一写者（flock）、半成品不报 ready；
- `:161` 是升级章节对该前置的入口指针（「带 `cold/kv/journal.log` 的根会缺一部分历史且缺得静默」）。

选 `docs/OPERATION.md` 而非本节的理由：运维前置的读者是操作方而非实现者，OPERATION 的升级章已承载同一条红线；`check_doc_budget.py` 的正文体量棘轮余量为 0（`slack.lines: 0`），故本节只做指针、不搬运正文，两处内容不各写一遍。

## 十三、6.1–6.5 旧实施行的 superseded 登记（08.R3 落地后）

`tasks.md` 的 **6.1／6.2／6.3／6.4／6.5** 描述的是**已整体删除**的退位分层机制（建 `RetireFacts`、冷层根、排他安静点到 S1 字节有界），其正文与当年的锚表**原样保留为历史证据**（5.8：历史不可变，推翻结论记在后段，不改写历史原文）；本节的唯一作用是给出**勘误指针**：这五行**不得**按原口径复验、**不得**据其绿数宣称达成、**不得**勾为完成。

取代关系（谁淘汰了谁）：

| 旧行 | 原主张 | 现行处置 | 指针 |
|---|---|---|---|
| 6.1 | 建 `RetireFacts` 单能力＋冷层根＋原文族同批搬 | 机制整体删除，无冷层载体 | `tasks.md` 头部反转说明；[08.R3](tasks.md) 零残留 grep；一级 design D6.13 勘误 |
| 6.2 | 排他安静点（引擎独占＋装配侧停写准入） | 无搬迁即无安静点；`settleGate` 与 `ErrColdArchiveNotQuiet` 零 Go 定义 | 06 design:661；`tasks.md` 08 锚对账段 |
| 6.3 | `GetEvent` 三态透明回冷＋冷层具名拒绝 | 读路径回到两态（命中/不存在），`ErrSegmentBodyMissing` 仍管正文读不出 | 08.R3 零残留 grep；主 spec `storage-lifecycle-completion` |
| 6.4 | lifecycle/retention/guard 接线（存活键集 oracle） | 未实施且不再派发（无搬迁可协调）；`Retention.Retire` 等不同义项保留 | 本文件 §十·三「不许留第三态」 |
| 6.5 | S1 症状锚转绿＋回滚负锚 | 该锚已随机制删除退役；现行成本锚为 `TestSymptomAnchorPerGenerationCostFlatOverALongRun` | 一级 ORCHESTRATION R291 |

已落地的取代交付＝**08.R3（删退位机制及其测试）／08.R4（删切面机制＋外部折回脚本）／08.R5（转换的离线源与失败目标前置）**；既有冷档数据的一次性搬运由框架外 `scripts/fold-cold-root` 承担，框架零分支。故本域验收面**不是** 6.x，而是 08.R3–R5 各自的三锚；6.x 的 `- [ ]` 保持未勾，勾选与否不再表达进度（永久 superseded 标记）。

## 十四、08.R1／08.R2 交付项逐条取代判定（08.R4 落地后，2026-11-19）

退位机制与切面族已整删（08.R3／08.R4），故 08.R1／08.R2 的多数交付项**失去了对象**。本节逐条登记取代关系与依据，两叶正文原文保留不动（5.8：历史不可变，推翻结论记在后段）；**未被取代者具名列在表下，两叶均不勾**。

取代依据（本单复跑）：`RetireFacts`／`relocatableStorage`／`WithRetirement`／`placeIndexCut`／`cutOf`／`appendJournalIndexCut`／`writeJournalIndexCut`／`readJournalIndexCut`／`journalIndexCutVersion`／`journalIndexCutFileName` 在 `*.go` 全树 **0 命中**；`memory/ledger/journal.go` 现存方法表中 `rewrite`／`reopen`／`relocate`／`seedIndexCut` 均不存在。

| 叶·交付项 | 判定 | 取代者／依据 |
|---|---|---|
| 08.R1① 搬迁单位对齐整事实提交后的键集（族拆不再是可构造状态） | **已取代** | 无搬迁即无搬迁单位（`RetireFacts` 0 命中）。族拆的现行防线＝`fold-cold-root` 的 `TestFoldRootRefusesAReferenceWithNoBody`；整事实提交形状归 11 域 |
| 08.R1② 孤儿原文具名处置（甲留热／乙搬冷，禁第三态） | **已取代** | 甲乙两条分支的落点都是退位报告，报告不存在；孤儿原文的现行处置入口＝`scripts/recover-kv`（`TestRecoverOrphanOriginals*` 六锚）与上述折回拒绝 |
| 08.R1③ 回退整合（关闭退位后热层只追加、已搬事实按身份可读、重复提交判已存在） | **已取代** | 开关本身已删（`relocatableStorage`／`WithRetirement` 0 命中），「关闭」与「删除」合一；已搬事实的现行读法＝外部脚本折回新根后按身份读（`TestOfflineConversionPreservesEveryFact`）。「重复提交判已存在」归 11.R4 提交语义 |
| 08.R1④ 身份读三态在冷层损坏时具名拒绝 | **已取代** | 冷层已删，读路径回两态（命中／不存在）；`ErrSegmentBodyMissing` 仍管正文读不出。见 §十三 6.3 行 |
| 08.R1⑤ spec 与代码同向（三方法条款与闸条款按单能力／阻塞语义重写） | **部分已取代** | 三方法条款与闸条款已不在本域 delta；「无损搬迁」→「压实复制」已由 08.R3 改。**残余＝下表脚手架判据**，故本项不单独勾 |
| 08.R2 以真实消费者判定保护能力的留删 | **未取代，仍开** | 见下 |

**未取代的残余（08.R2 主体，本单不勾）**：本域 delta 的 `storage-lifecycle-completion` MODIFIED 条款仍硬写「`NewLifecycle`／`NewRetention`／`NewMemoryGuard` 中任一构造 SHALL 要么有生产调用方并说明它并入的那一条判据，要么被移除；……MUST NOT 停留在只被测试与 `skeleton_assert.go:150` 引用的状态」，并带 Scenario「脚手架不零调用滞留」。本单复跑 `\b(NewLifecycle|NewRetention\(|NewMemoryGuard)\b` 于 `*.go` 全树：**生产命中 0**，命中面＝`memory/ledger/skeleton_assert.go:149` ＋ `memory/partition_test.go` ＋ `tests/integration/history/{recovery_before_reclamation,original_reference_isolation}_test.go`——正是该条款点名的第三态。10 域已把未接线脚手架的所有权转给 08（[10/tasks.md](../10-contract-sync-dead-code/tasks.md):72「未接线脚手架转 08 拥有者」），故本域是唯一归属方。

- **原两条判别测试的锚已失效**：`TestRelocatedObligationMaterialStillServesItsConsumer`／`TestRetirementRejectsUnreadableObligationMaterial` 的构造前提是「搬冷后的未完义务材料」，随退位删除不可构造（两者本就「拟新增，尚未建立」）。本单**只登记其锚须由拥有者重定，不宣布该验收面不再需要**——按「验收面不追认缩水」，该叶重开与否由编排者裁定。
- **待裁二选一**（两者都改 spec 条款，须另单承接）：**留**＝把 `Retention`／`MemoryGuard`／`Lifecycle` 接到某条现行判据（TTL／容量／租约）并具名说明它并入哪一条；**删**＝连同 `skeleton_assert.go:149` 与相关测试一并移除，同步删 spec 的「脚手架 SHALL 接线生产」硬要求。禁第三态。
- **一处指针漂移（不属本单白名单）**：上述条款写 `skeleton_assert.go:150`，实际定义在 `:149`（`lifecycle.go:143`／`retention.go:70`／`guard_memory.go:32` 三处准确）。改该条款的叶须一并更正，本单不改。

## 十五、08.R2 落地：甲案移除与两跨域锚判定（R307 裁定，方向不再取证）

### 十五·一 裁定与依据（引用不复核）

R307 已裁甲案（移除）。生产消费者实证：`NewLifecycle`／`NewRetention`／`NewMemoryGuard` 三构造全树零生产调用方，`Guard`／`MemoryGuard`／`LifecycleStore`／`LifecycleCapabilities`／`LifecycleConfig`／`StoredKey`／`RetentionRecord`／`HolderID`／`ProtectionLease`／`ScanReport` 生产引用全 0；两条保护语义各有活载体——**原文过期**由 `StorageConfig.RetentionWindows`→`kvOriginalStore`→`ErrOriginalExpired`／`RecallExpired` 承接，**未结义务**由热窗 pin（`assembly/run.go` `pinFact`／`keepPinnedLocked`／`deliveryPendingFor`，结算于 `host_delivery.go`）承接。再接一层 Lifecycle 即造 spec 明禁的销毁路径（`spec:7` 禁销毁已提交事实），Guard／Retention 则为第二真相。故不留「在册未接线」第三态。

### 十五·二 删除清单（实测）

| 落点 | 处置 | 依据 |
|---|---|---|
| `memory/ledger/{lifecycle,retention,guard_memory,lifecycle_config}.go` | 整文件删 | 三构造零生产调用方 |
| `memory/ledger/guard.go` | 删 `Guard`／`HolderID`／`ProtectionLease`／`ErrLeaseMismatch`；`RecallOutcome`／`RecallResult` 迁 `reference.go`，随后整文件删 | 保留面属 `reference.go` 生产面（`kv_original_store.go`、`tool/recall/get.go`、`assembly/tools.go` 消费） |
| `memory/ledger/skeleton_assert.go` | 删四类符号断言＋retention 块＋lifecycle 块（原 122-161），连带 `"time"` import | 被断言符号已不存在 |
| `memory/ledger/doc.go` | 删保护/生命周期散文 | 「随代码走」 |
| `memory/partition_test.go` | 删 `TestCapacityEvictionStaysInsideOnePartition`、`TestLeaseSegmentsProtectOnlyTheirOwnKeys` 与 helper `newLifecycleUnderTest`／`storedKeys`／`holdsKey` | 构造前提为被删对象 |
| `tests/integration/history/recovery_before_reclamation_test.go` | 整文件删（见十五·四） | 六子测全打被删对象 |
| `tests/integration/history/original_reference_isolation_test.go` | 删子测 `protection holds outlive one holder`、helper `assertProtected`、头注一条 | 见十五·四 |
| `examples/contract/memory_ledger/{Guard,LifecycleStore}_example.go` | 删；包 `doc.go` 同步 | 例证对象不存在 |

### 十五·三 保留项复核（逐项，不默认）

- **`Retention.Retire`**：`Retention` 类型唯一构造为 `NewRetention`，生产调用 0、无其他生产嵌入点 ⇒ **类型整体无生产实例**，`Retire` 随类型删。不是「构造链不明」，是构造链已证断（与 R306 要求的「按类型消费者枚举」一致）。
- **generation retirement**：`assembly/run.go` `generationRetireKey`／`recordGenerationRetirement`／`convergeGenerationRetirements` 属代际退位，与本文移除对象不同义项，**未动**。
- **`migrate.go`、`ErrJournalLegacyLayout`**（`memory/ledger/config.go:122-146`）：**未动**，仍在生产路径。
- **`RecallOutcome`／`RecallResult`**：保留，迁入 `reference.go`（唯一生产存续面）。

### 十五·四 两跨域锚逐条判定（先读断言本体）

**锚一 `protection holds outlive one holder`**（原 `original_reference_isolation_test.go:306-360`）：13 条断言点全部打 `Guard.Hold`／`Guard.Release`／`assertProtected`——无名 holder 拒绝、同 holder 重入幂等（`leaseA` 相等）、释放后不可保护、再持有换新 lease、陈旧 lease 报 `ErrLeaseMismatch` 且保护保留、双 holder 互不干扰、外来 lease 拒绝、重复释放幂等、无关键恒不保护。**零条**触及义务存在性、义务材料可读性或事实字节。判定：**测已删 `ProtectionLease` 机制**，非测活义务语义 ⇒ superseded。其承载的「多持有者引用计数保护」在现行设计无对应形状——pin 按事实单一属主（`pinFact`／`unpinFact`），结算即解除，不存在按 holder 计数。承接锚（均 exit 0）：`TestPinnedDeliveryFactsSpendTheWindowNotTheFoldProducts`、`TestCrossSessionDeliveryObligationSurvivesTheTargetSessionCut`、`TestPinnedFactsReseeded`。引用方为冻结 archive `2026-10-18-runtime-rebuild/changes/03-history/tasks.md`(03.1)，**不改写历史**，勘误登记于本节。

**锚二 `TestRecoveryBeforeReclamation`**（原整文件 6 子测）：全打 `Lifecycle`／`Retention`／`MemoryLifecycleStore`／`StoredKey`。逐条判定：

| 子测 | 断言本体 | 判定 | 承接 / 登记 |
|---|---|---|---|
| G2.1 重启登记先于淘汰 | 破坏性扫描先登记再执行 | superseded：扫描对象不存在 | 无活等价（登记，不宣布验收面不需） |
| G2.2 未确认材料越 TTL 仍保留；释放后按原时间戳归类 | 前半＝pin 使结算事实留在热窗；后半＝原文过期具名拒绝 | **迁移** | `TestPinnedDeliveryFactsSpendTheWindowNotTheFoldProducts`／`TestOriginalReadEntrypointsPreserveBytesAndScope/a_reference_past_its_window_reads_as_expired` |
| G2.3 显式删除撞保护 `ErrProtected` | 显式销毁路径 | superseded：无销毁路径；上层「已提交事实不销毁」仍活 | `TestOfflineConversionPreservesEveryFact`（源字节零改动＋按身份读回） |
| G2.4 单 agent 关闭不释放共享保护 | 多 holder 形状 | superseded（同锚一） | 无活等价（形状被单一属主取代） |
| G2.5 枚举失败不核对残缺视图 | 生命周期对账循环 | superseded：循环已删 | 活判据＝`TierFacts.Scan` 失败答 `nil`＋`err`（`tier_facts.go:36-40`） |
| G2.6 持久档缺能力 fail-closed | `StoreCapabilities.Durable` 诚实自报 | **迁移** | `TestStorageCapabilities`（memory 档不声明恢复） |

引用方为冻结 archive `2026-10-18-runtime-rebuild/changes/03-history/tasks.md`(03.5) 与 `expectations.md`(O2-5)，不改写。判定：文件整体 superseded；其上四条活语义已逐条指向具名活锚并实跑绿，失踪的两条（G2.1、G2.5 的对账循环面）随对象消失并在此具名登记——**不以「对象已删」为由抹掉仍成立的验收面**。

### 十五·五 级联落点

- `scripts/gates/product_surface_baseline.json`：清 `NewMemoryGuard`／`NewMemoryLifecycleStore` 两陈旧冻结项（37→35），门 0 error／0 warning。
- `docs/wiki/memory/memory-architecture.md` 四处伪断言改真：`:48` 硬契约第 10 行 LifecycleStore 红线→`TierFacts.Scan`；`:94`「Retention 在册未接线」→原文过期唯一执行面＋删除说明；`:149`「仍在 lifecycle.go 在册」→已整体删除；`:254` 缺口行改为「已裁为删」。doc-budget 22149 行 / slack 0 未变（逐行原地改写）。
- `assembly/host_delivery.go`：`deliverySettlementType` 注释删去对 `lifecycle_config.go` 的指引（TTL 豁免无对象），「两处拼写一致」契约保留。
- `docs/api/`：重新生成。本单引起 `memory__ledger`、`examples__contract__memory_ledger` 两文件；其余 6 文件为工作树既有陈旧（已核 0 处 lifecycle 相关 hunk），非本单产生。
- 08 delta spec：`REMOVED`「TTL check uses actual event timestamp」（执行面已删，时间真相由主规格 `event-segment-store`「时间真相源单一」承接）＋`MODIFIED`「保留保护与回收协调」重写为现行判据（无销毁路径、按身份恒可读、两条保护各一活载体、能力诚实 fail-closed），三器硬要求不留死条款。

### 十五·六 验证（原始 exit）

`go build ./...`=0；`go vet ./...`=0；`go test ./memory/ledger/ -count=1`=0；`go test ./assembly/ -count=1`=0；`go test ./tests/... -count=1`=0（13 包全 ok）。负锚：31 个移除符号词边界 grep 全树零残留，唯 `\bLifecycle\b` 命中宿主 `DiagnosticLifecycle` 字段（`assembly/host_diagnostics.go:245,246,271` 等，不同义项，R307 已注明）。门：`comment_policy` 两聚合均 0；`check_concept_budget.py --json`=0；`check_product_surface.py` 0/0/35；`check_dead_producers.py`=0；`check_doc_budget.py`=0；`check_doc_links.py`=0；`gen_godoc.sh --check`=0；`openspec validate runtime-recovery-ideal --strict` valid。基线未抬（反而收紧 2 项）、无活谓词删减凑绿。

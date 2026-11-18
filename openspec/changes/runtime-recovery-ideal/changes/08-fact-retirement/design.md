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
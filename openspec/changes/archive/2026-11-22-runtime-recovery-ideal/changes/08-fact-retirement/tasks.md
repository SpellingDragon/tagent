> **历史输入（2026-11-20）**：本目录不再作为当前派发或自动规格同步输入；下文旧勾选、方案与限制按原时点保留，不授权后续实施。当前只按[编排入口](../../ORCHESTRATION.md)派发三个语义单元，全部旧要求由[承接表](../../CARRYOVER.md)逐项接收，不因转历史而取消。

# 08 删除退位与外部转换 · 孙任务

> **当前承接（R265，2026-11-19 勘误；同日后补 08.R4 核销）**：运行时退位删除与外部折回工具已有实现，**08.R3 已核销（2026-11-19 接续核销，证据见文末 `## 08.R3 执行记录` §六）**，**08.R4 已核销（2026-11-19，证据见文末 `## 08.R4 执行记录`）**；**08.R5 已核销（2026-11-18，证据见文末 `## 08.R5 执行记录`）**——源离线、源变化拒绝与目标失败不切换三面已取得独立证据（含三条负控）。外部工具依赖06/11现有写权和只读开档能力，不能把脚本单独套到旧HEAD便宣告可交付。历史搬迁方案不再派发，历史绿数不等于当前转换验证通过。**08.R1／08.R2 交付项逐条取代判定见 [design.md](design.md) §十四**：多数交付项随退位删除失去对象（已登记 superseded），08.R2 的脚手架留删裁定**仍未取代**，两叶不勾。

> ## ⚠️ 本域性质已反转（2026-11-16，用户裁定；阶段5自审 A2 回写）
> **用户裁定原话**：「采纳可寻址索引，退位退役」，随后纠正「**准确的说不是"退役"，我们的所有变更都要0遗留0残留，不考虑框架兼容，最多考虑外部脚本辅助**」。D6.13 后半（水位制退位）已反转，见一级 [design.md](../../design.md) D6.13 勘误与 ORCHESTRATION R50/R51。
> **故本域由「建退位」改为「删退位」**。下方 **08.R1／08.R2 及其原 6.x 全部作废**（它们描述的是建退位），**不得派发**；保留原文仅作历史证据（5.8：历史不可变，推翻结论记在后段）。当前有效叶＝**08.R3（S3）／08.R4（S4）**，见本文件末。
> **依据**：退位三条核心性判据全否——失败不阻断（`checkpoint.go:377-379` 只 `assemblyError`，检查点照样成功）、关掉只变成本不变功能、不在三挂点拓展轨道上（不生产事件、不是插件、不为呈现读链）。它是性能层补偿，且掩盖了「索引必须全量物化」的结构缺陷。G5/G6/S1c 实测：可寻址索引使开档/点读/提交跨 64× 历史全平，退位的成本买点被取代；冷热同盘（`hotDev=coldDev`）故无分层节省；枚举两边都访问全部条目故退位从未界定枚举。
> **零残留形状**：框架内**不留任何兼容分支**——无 legacy 冷读路径、无冷档检测谓词、无 legacy 具名错误、无退位配置开关。既有冷档数据由**外部脚本** `scripts/fold-cold-root` 折入**新根**（真实事实的发布前运维前置），框架零分支。
> **下方原「红线贯穿每一行：搬迁非删除」仍然成立且更强**：删除的是**机制**，不是**已提交事实**；任何步骤不得使已提交事实不可读。

## 当前补差（R265）

- [x] 08.R5 外部转换的源变化与失败目标拒绝。产出：证明转换要求源离线；执行中源变化或目标半成品时不报告可切换，不把多次源读取包装成在线一致快照。前置：现有08.R4工具与06/11写权、只读开档能力；写面`scripts/fold-cold-root/main.go`、`scripts/fold-cold-root/main_test.go`、`docs/OPERATION.md`及本域计划，不读真实用户根。三锚已建立并绿（2026-11-18，命令与实测见文末 `## 08.R5 执行记录`）：`go test ./scripts/fold-cold-root -run '^(TestOfflineConversionPreservesEveryFact|TestSourceChangeRefusesConversion|TestPartialTargetIsNotReportedReady)$' -count=10 -v`，源变化使用确定性同步而非睡眠碰撞；正式消费者锚在同包启动工具CLI，干跑源零写、转换后独立开档逐身份/字节/提交序核验。保留已有写者拒绝测试；独立审确认失败后没有切换源或覆盖非空目标。该叶与08.R3/R4原验证共同验收，不把历史49键通过外推为全部异常已测。

## 当前有效叶（S3／S4，承载 G6 冻结次序）

- [x] 08.R3 **S3：删除退位机制及其测试（原子，不可拆）**：交付＝按 G6 移除清单删除退位面——`RetireFacts`／`relocate`／`keepKeysLocked`／`keptHotKeys`／`relocateSettledFacts`／`coldRoot`/`WithColdRoot`／`WithRetirement`/`cutMaintenance`／`coldEventLocked`/`coldViewLocked`/`coldIndex`／`ColdReadCost`/`ColdWriteCost`／`RelocateReport`／`coldDirName`／`coldRetirement`／`retireSettled`／`copyToCold`／`verifyColdCopy`／`segmentSettledKey`/`segmentSettledCommitKey`／`ErrColdArchiveUnavailable`，及 `planFactLocked` 的 `settled` 标志级联；**3 个 ledger 测试文件整删（约 1,500 行）＋1 history 文件＋7 测试＋helper `judgeOpenIsBounded`＋一条交错腿**；`cold_archive_gate_test.go` **保留其两枚 flock 锚但须改名**（它已承载写权用例）。**明确不删（不同义项/机制）**：generation retirement、`Retention.Retire`、`headless-retired`、cache-cold 测试、`migrate.go` 与 shard-layout 拒绝。`settleGate` 与 `ErrColdArchiveNotQuiet` **零 Go 定义＝纯文档残留**，一并清。 —— 验证：正锚 `go build ./...` exit 0 ＋ `go test ./memory/ledger/ ./assembly/ -count=1` exit 0；负锚 **零残留 grep**：上述符号在非测试生产代码零命中（附所用正则与逐面命中数，N10 要求）；消费者锚 `go test ./tests/... -count=1`（**全树不得抽样**）＋ `go test ./runtime/... ./features/... ./tool/... -count=1` ＋ 两注释门 0 findings。**已提交事实可读性不得下降**：删前取一份含已搬事实的根，删后经外部脚本折入新根并按身份读回原字节（该脚本属 08.R4 交付）。
  - **文件白名单**：G6 移除清单所列 7 个生产文件＋3 个 ledger 测试文件＋1 history 文件＋`cold_archive_gate_test.go`（改名）＋本域四件套。**清单以 `06/design.md §十七` 的移除 inventory 为准，须 grep 复核完整性，不得凭清单遗漏级联**。
  - **前置**：**09.R4 症状锚红证据已取得**（否则反转会打断症状锚）＋ S1c 已落地 ＋ **06.R4（S2）已完成**（store 去物化后删退位才不留悬空读路径）。**冲突**：与 06.R4 同触 `memory/ledger/segment_store.go` → **串行**。**原子性**：机制与其测试必须同批删，不得分批（分批会留「测试引用已删符号」的破碎态，本轮已实证该形态）。

- [x] 08.R4 **S4：删除切面机制 ＋ 外部折回脚本**：交付＝①删 `placeIndexCut`／`cutOf`／`appendJournalIndexCut`／`writeJournalIndexCut`／`readJournalIndexCut`／`journalIndexCutVersion`／`journalIndexCutFileName` 及 `journal_index_cut_test.go` 中随之失效者；②**外部脚本** `scripts/fold-cold-root`（`recover-kv` 形态：默认干跑、`WriteRights: restore`、经新引擎校验），把既有冷档事实按 `segmentIndexCarried.Commit` ＋ `settled:commit` 可恢复的**原提交序**折入**新根**；③升级说明记录该运维前置与 `file` 档 `ErrBatchNotAtomic` 后果。 —— 验证：正锚 `go test ./memory/ledger/ -count=1` exit 0 ＋ 脚本干跑对含冷档的夹具根输出待折键数且**零写入**；负锚 **脚本对已有写者的根具名拒绝**（复用 flock）＋ 折回后按身份读回原字节逐字符相等、提交序不变；消费者锚 `go test ./scripts/recover-kv/ -count=1` ＋ `go test ./tests/... -count=1` ＋ 两注释门 0。**已核销 2026-11-19，命令与逐面命中数见文末 `## 08.R4 执行记录`。**
  - **前置**：**08.R3 完成**——`placeIndexCut` 的**最后一个调用者是 `relocate`**，故 S4 必须在 S3 之后，否则删切面会断退位。**此序不可颠倒**。
  - **上报项**：若移除清单有符号在删后仍被引用（级联遗漏），须具名上报并补删，不得留「存在但零调用」的第三态。

> verify 三要素随任务行冻结（R29）：正锚点名测试＋可复跑命令、负锚具名错误路径、消费者锚正式入口实测。测试名与 `-run` 正则逐字符一致。
> **波次 W3**：前置＝W1（01+02+04 合单）＋W2（03/05）复跑凭据绿——存活键集权威（§五 `checkpoint.Survivors`）与派生索引加载就绪后方可派发。
> **红线贯穿每一行**：搬迁非删除；`GetEvent(key)` 对已搬迁事实恒返回原字节；任何负锚不得以「删事实」换取有界。
> build/test 与 commit 永不串联；闸与 swap 属并发敏感面，正锚重复运行 ≥10 次并开竞态检测。
>
> **W4 回归扫尾（本轮实测，2026-11-15）**：①**真缺陷已修**——退位趟原按「relocate 时 `l.index` 快照」枚举待搬键，而 `keepKeysLocked` 的 keep 快照取自更早一次 `Keys`，两者之间的窗口里落地的键（含 `tagent/task/v1/spec:*` 任务准入材料）会被本趟搬走；该类族在 boot 只空 `Scan` 热层（`state.DurableCommit` 的 `specPrefix`/`factPrefix` 扫描无冷读），故 `TestSoakContinuity/崩溃恢复` 读回 `ErrSpecMissing`（真机取证：kill -9 现场 `root/cold/kv/journal.log` 第 6 帧 17 键含 `tagent/task/v1/spec:task-11571be7…`，热层 32 个任务里 31 个 spec 仍在）。修法＝「一趟只搬它快照读到的键」：`retireSettled` 收 `settle []string`（消费方快照），`journalLog.relocate` 由 `l.index∖settle` 自推 keep，窗口内落地的键留热、下一趟自理。锚：`TestRetirementSettlesOnlyTheKeysItsSnapshotRead`（负锚＝旧语义下同一注入用例 settled=4 红）、`TestTaskAdmissionMaterialStaysHotAcrossARetirementPass`。②**审批 boot 回归已修**——`RestoreBoot` 读不出请求目录时原样上抛，`assembly.Boot` 因此整体失败（`TestGateFailsClosedWithoutAttention/an_unwritable_request_directory_keeps_the_wait_standing` 红）；改为「书空＋`DeliveryGap` 记名、boot 不因一行请求文件死」，等待在调用再次 park 时重立（写入失败由 `Request` 具名返回）。③宿主三锚现树复跑全绿（见 6.5）。
>
> **08 锚对账（本轮，编排者派发授权）**：原冻结面写的 `EnumerateKeys`/`Tombstone`/`ReclaimTombstoned` 三方法未实现，实现以**单一 `RetireFacts(ctx, keepKeys)` 一步原子**替代（复制→校验→重写，`memory/ledger/segment_store.go:889`；`keepKeysLocked` 由消费方 keep 快照自推 settle 集），无「已标记未搬」中间态（编排者 R3 认可）。对账后本域验收面＝**RetireFacts 单能力 ＋ identity-read ＋ 排他安静点**：三方法不作为能力形状要求；`ErrColdArchiveNotQuiet`、停写超时等「闸拒绝」路径不建——闸的语义是**阻塞**（写者等锁，不返回值），不是拒绝，故其为不可实现形态而非未完成项。配置声明面（`hostStorage` yaml 开关）不入本对账面（理由见 6.1）。逐锚凭据见 `run-notes.md` §08。

## 本轮状态回退声明（重整后，先读）

一级 proposal/design 已撤销旧整体完成结论。6.1／6.2／6.3／6.5 的旧 `[x]` 反映的是**旧证据时点**（2026-11-15 W3/W4：memory/ledger 包级 21.515s 绿、S1 锚 exit 0、run-notes.md §08 七行锚表），现一律回退为 `[ ]` 待复验。依据（✅ 读码，见 [proposal.md](proposal.md)「本轮重整」表）：

- 契约仍写三方法而实现是 `RetireFacts` 单能力（P5 不成立）；
- 判据两套并存（spec 的存活权威并集 vs 实现的 `keptHotKeys` 水位制 keep），D6.13 只承认后者；
- 搬迁单位（段三键＋原文族）与提交单位（两笔）不一致，孤儿原文处置未具名；
- 6.2 的闸形态与实现不同（引擎独占＋只搬快照键），旧负锚不可实现。

7.1／7.2 保留 `[x]`（真缺陷修复，各有负锚：`TestRewriteKeepsTheHandleOnTheLogItLeft`、`TestRetirementDropsTheSettledKeysFromTheResidentSet`），但须在 **11.R2 落地后复跑**（提交路径改变会影响 rewrite 与驻留集两侧假设）；7.3–7.6 为证伪不改，保留原结论。

- [ ] 6.1 【待复验】 **journal-only 退位引擎单能力（`RetireFacts`）＋全局提交序＋原文族同批搬**：`journalLog.relocate`（复制→校验→重写三步，`memory/ledger/journal.go`）、`journalKV.retireSettled`、`coldRetirement` 能力、冷层根 `<root>/cold`、`FileSegmentStore.RetireFacts`＋`keepKeysLocked`、`segmentSettledKey`。**全序提交序**：`{pid}:idx:` 值携 `segmentIndexCarried.Commit`（store 级单调提交序号，`StoreEvent` 分配、`Load` 由已发布条目与计数取高水位），`eachIndexEntryNewestFirst` 按序号归并热冷两序，`settled:commit` 记已搬最大序号使 boot 在热层头已越过该界时不打开冷层。**原文族随事实同批搬**：`keepKeysLocked` 判据扩到原文族（`originalFamilyEntry`，`ref:`/`blob:` 随其事实同批搬，事实仍被存活键集点名或本店不 state 者原样留热；族拆被拷贝校验以 `ErrSnapshotCorrupt` 具名拒绝，见 6.3 消费者锚红腿）；`relocate` 在复制/校验/重写三个步界检查 ctx。**一趟只搬快照读到的键**（W4 扫尾）：`retireSettled(settle []string)`，`relocate` 自推 `keep=l.index∖settle`。 —— 验证（本轮复跑，全 exit 0）：正锚 `go test ./memory/ledger/ -run 'TestColdArchiveRelocatesSettledKeys|TestColdArchiveRewriteKeepsCommitOrder|TestColdArchiveRanksTheSettledCommitsBesideTheResidentOnes|TestRelocatedFactReadsByIdentity|TestColdArchiveRefusesCorruptColdTier|TestRetirementSettlesOnlyTheKeysItsSnapshotRead|TestTaskAdmissionMaterialStaysHotAcrossARetirementPass' -count=1`；消费者锚 `go test ./memory/ledger/ -run 'TestJournalStatesItsCommitOrderNewestFirst|TestEachIndexNewestFirstStreamsTheCommitOrder' -count=1`；包级 `go test ./memory/ledger/ -count=1` exit 0（21.515s）＋ `go build ./...` exit 0。**对账记录**：①三方法（`EnumerateKeys`/`Tombstone`/`ReclaimTombstoned`）由单一原子能力替代，不建；②`ErrEnumerationIncomplete`、单帧 >32MiB 等「枚举/分页」负锚随三者一并退出验收面（单能力不分页，无部分列表可返回）；③**配置开关偏离已申报**：`assembly/run.go:512 relocatableStorage` 对持久 journal/localfile 根**强制**开启退位（注释自陈「默认即搬迁」），与本域早期设想「缺省关闭、由部署 yaml 声明」不同；`ledger.StorageConfig.RetireFacts` 零值仍关闭（ledger 场景「未声明只追加」保留），`hostStorage` yaml 开关未做，理由是它不在本派发单白名单且装配层默认开启已使退位在部署侧可达（S1 因此可在生产复绿）。该开关若要做，须在装配/配置面另单承接，并同步 `host_document.go`。

- [ ] 6.2 【待复验】 **排他安静点（引擎独占＋装配侧停写准入）**：P4-cont 在 `assembly/{facts,checkpoint}.go` 落 `relocateSettledFacts`（`flight.await` 归零→`keptHotKeys`→`RetireFacts`）。**引擎侧独占**＝`RetireFacts` 持 `FileSegmentStore.mu` 贯穿复制/校验/重写，`StoreEvent` 同锁，故重写期内无新写入、重写只写其快照键（写者窗口内落地的键留热）。**停写准入**改读 run 的 boot 上下文（`run.go` `bootContext`）：boot 已结束的 run 不起搬迁、已在途者改用 boot ctx 传入 `RetireFacts`，由 `relocate` 步界检查自行停下（`TestCompressProductRestoresAfterRestart -count=12` 由 8/10 绿→12/12 绿）。未采用 `sync.RWMutex` 闸：`commitFactNoCadence`→`reduceCommitted`→`compress_consumer.go:512` 在同一提交单元内再提交（重入），闸在等待写者时自锁（实测死锁）。 —— 验证（本轮复跑，全 exit 0）：正锚 `go test ./memory/ledger/ -run '^TestColdArchiveHoldsTheExclusiveGate$' -count=1`（1.89s）＋ `-race -count=10`（24.17s）：写者 200 次已确认提交与读者并行穿越一趟退位（report.Settled>0），搬迁后与重启后逐键按身份读回原字节、EventCount 陈述全量历史、读者在搬迁中从不读到 missing；**非空转负腿**（副本回滚：退位后删除冷层字节＝以删除代替搬迁）→ exit 1，`the write gate-writer-0 answers missing after the reopen, want the bytes the store acknowledged: the pass erased a write that landed inside its window`。消费者锚 `go test ./assembly/ -run '^TestCheckpointWaitsForTheQuietPointOfThePipeline$' -count=1` exit 0（本轮复跑，checkpoint_test.go:550，安静点仍是 drain 不是锁；包级 `go test ./assembly/ -count=1` exit 0（295.066s）与 `go test ./tests/integration/host/ -count=1` exit 0（213.245s）为 R14 复跑证据，本轮未重跑）。**对账记录**：`ErrColdArchiveNotQuiet` 与停写超时负锚按上文「闸是阻塞非拒绝」退出验收面（不可实现形态，非未完成项）；「drain 未完成→不进入 swap」由 `flight.await` 先于 `RetireFacts` 的顺序保证，无独立负锚。

- [ ] 6.3 【待复验】 **搬迁 ¬live ＋ GetEvent 按身份透明回冷 ＋ 冷层具名拒绝 ＋ 修复入口消费者锚**：`GetEvent`/`eventByKey`/`StoreEvent` 三态已实现——热层命中→原路径；热层未命中→`coldEventLocked` 冷层懒开（boot 不开）答复原字节＋原位置；两层皆无→`(false,nil)`；冷层索引点名而正文读不出→`ErrSegmentBodyMissing`；重复提交对已搬迁身份仍判重复（不二次落链）；`EachEvent`/`Events` 已遍历到冷层。`kvOriginalStore` 同责——`entryLocked`→`coldEntryLocked`（`ref:`／`blob:` 按身份回冷，冷层 `ref:` 无 `blob:`→`ErrSnapshotCorrupt`），`Get`/`ReadSegment`/`References` 同一入口，`Put` 对已搬身份判重复且不把正文写回热层。身份单一化：`EachEvent` 与 `eachIndexEntryNewestFirst` 对同一身份只陈述一次（热层点名者跳过冷层副本）。 —— 验证（本轮复跑，全 exit 0）：正锚 `go test ./memory/ledger/ -run '^TestRelocatedFactReadsByIdentity$' -count=1`（热层不再持已搬事实的三族键＋进程内与重启后均读到原字节原位置＋原文读到原字节＋重复提交判重复且不二次落链＋异字节冲突具名）；负锚 `go test ./memory/ledger/ -run '^TestColdArchiveRefusesCorruptColdTier$' -count=1`（冷层帧损坏→`ErrJournalCorrupt`，事实与原文两次读均具名拒绝，不答 `(false,nil)`）；消费者锚 **`go test ./tests/integration/history/ -run '^TestRecoverKVReadsARelocatedRoot$' -count=1`**（0.83s，正式修复入口：`go build ./scripts/recover-kv` 后以 `-root <搬迁后夹具根>` 干跑，exit 0 且输出 `nothing to recover, every reference names its independent body`；夹具自证已真搬迁＝热层不持已搬事实的 `ref:`/`blob:`、report.Settled>0、冷层按身份读回原文）。**非空转负腿**（副本回滚：原文族拆开搬，`blob:` 留热而 `ref:` 搬冷）→ exit 1，`relocate the settled fact: memory: stored record is corrupt: original "recover-kv-settled-fact" has a reference but no independent body`——引擎在拷贝校验处以具名拒绝挡住族拆，故命令永不面对「引用在热、正文不在」的孤儿根。**对账记录**：原 `assembly` 侧 `TestRelocatedFactRefusesDuplicateCommit` 与 store 级 `TestRelocatedFactReadsByIdentity` 的重复/冲突腿同断言，合并为后者一处（避免同断言双锚漂移）；`¬live` 判据的 `obligationLeases`／`chainReachableBlobs` 并入属 6.4，未接线前本域不主张该并集。

- [ ] 6.4 **lifecycle/retention/guard 接线（oracle 改链可达＋义务租约）**（**状态：本轮仍未动，未勾选**）：`Lifecycle.Scan`（lifecycle.go:217-255）判据由租约＋龄期＋容量改为租约＋存活键集补集（龄期/容量 MUST NOT 单独触发搬迁），`Reclaim`（:321）在退位路径＝三步搬迁；生产接线 `NewRetention(NewMemoryGuard())`＋`NewLifecycle`（今零生产调用，仅 tests 与 skeleton_assert.go:150），持有者＝共享资源 owner；义务租约 Hold/Release 接 `noteDeliveryState`（host_delivery.go:671-690）未结/结清两分支，与 `Run.unpinFact`（run.go:2210）同时机，恢复侧 `restoreObligation` 已结早退（host_delivery.go:2069）；headless-retired 审计正文（host_delivery.go:860-873）按独立审计租约保留（默认 keep）；file 档不实现→显式能力缺失声明。**本轮判断（未变）**：真实接线需要 `LifecycleStore` 三方法落到 journal 档并把装配侧 `keptHotKeys` 的存活键集（含义务租约项）交回 `Lifecycle.Scan` 作唯一 oracle；仓促接一半会造出与 `keptHotKeys` 并存的第二 oracle（违 D6.3／§五）。故本轮不动，另单一次做完。 —— 验证：正锚 `go test ./tests/integration/history/ -run 'TestColdArchiveWiresTheLifecycle|TestRecoveryBeforeReclamation' -count=1` ＋ `go test ./assembly/ -run TestObligationLeaseHoldsUntilSettled -race -count=10`（未结持租约、结清释放、重启按链重建）；负锚 受租约保护的键被搬→`ErrProtected`（lifecycle.go:95）具名拒绝、持久档缺能力→`ErrCapabilityIncomplete`（:85）fail-closed、恢复清点未完成→`ErrRecoveryIncomplete`（:89）、龄期到期但仍在存活键集→不搬、headless-retired 审计正文被搬→红；消费者锚 `grep -rn "NewLifecycle\|NewRetention(" assembly memory runtime --include=*.go` 命中生产文件（非仅 tests/skeleton_assert.go:150）＋ `go test ./tests/integration/history/ -run TestStorageCapabilities -count=1`（能力诚实，storage_capabilities_test.go:25）。**W4 回归扫尾再补（真缺陷归属）**：任务耐久族（`tagent/task/v1/{spec,fact,consume,attempt,reject}`）**不走冷读**——`state.DurableCommit` 的 `Scan(specPrefix)`/`readTaskLocked` 只扫热层，故「热层未命中」对该族等于「不存在」；本轮把该族从搬迁面彻底摘出（一趟只搬快照读到的键），不再靠「冷层按身份可读」代偿。真机现场：kill -9 后 `root/cold/kv/journal.log` 末帧 17 键内含一个 `tagent/task/v1/spec:*`，热层 31/32 仍在。锚 `go test ./tests/integration/host/ -run '^TestSoakContinuity$' -count=3` exit 0。

- [ ] 6.5 【待复验】 **S1 症状锚转绿＋回滚负锚（F3 红转绿两次）＋真实宿主消费者锚**：S1（一级 §六）＝持续写入 N 事实＋折叠＋投递结清后 `journal.log` **真字节**有界于存活键集＋尾（多代＋多任务夹具，量 `os.Stat` 字节非正文计数，D6.6）。 —— 验证（本轮复跑）：**绿** `go test ./tests/integration/history/ -run '^TestJournalBytesBoundedByLiveSet$' -count=1` exit 0（主树 0.989s；隔离副本 0.976s），转绿成因＝原文族随事实同批搬冷（6.1）＋原文按身份回冷（6.3）。**回滚必红（F3 两次证据）**：同一命令在 `/tmp/recovery-f3/tree` 隔离副本上、将 `RetireFacts` 早退空转（退位能力缺席）→ **exit 1**，`journal.log holds 356558 real bytes (os.Stat) after 5 generations beside the 71326 bytes the live key set plus tail account for … committed=[71254 142580 213906 285232 356558] retained=[71254 142580 213906 285232 356558] appended=[71254 71326 71326 71326 71326]`；副本复原（md5 与主树逐字节相等）后复跑 exit 0。证据表与 md5/隔离核验见 `run-notes.md` §F3（S1/S2/S3 三锚同法：当前树绿→回滚红→复原绿）。**宿主侧（R14 复跑证据，本轮未重跑）**：`go test ./tests/integration/host/ -run '^TestCompressProductRestoresAfterRestart$' -count=12` 全绿（重启不再重铸产物：热冷双份去重＋已搬原文不被写回热层）、`go test ./tests/integration/host/ -run '^TestSoakContinuity$' -count=1` exit 0。**未跑**：`examples/wechat-bot` 真实宿主长跑（`TestEventIntegrityLongRun`）——禁区含 `.wechat-config/data`，本轮未触。

## 7. 复审发现缺陷修复（新增，本轮）

> 立案来源＝代码复审对 6.1/6.3 的复查。纪律：逐条读码证实后才改，证伪者不改并留证（避免为假缺陷加防御逻辑）。白名单＝`memory/ledger/*.go`＋`*_test.go`。**（历史口径：本轮重整后新叶 08.R1/08.R2 的文件白名单一律精确到文件，不使用通配。）**

- [x] 7.1 **重写后句柄悬空（`journalLog.rewrite`）**：原序 `remove cut → rename → writeJournalIndexCut → reopen(cut)`，`rename` 已把 `l.path` 换成新 inode，而 `l.file` 仍指向被 unlink 的旧 inode；若 cut 写入失败即直接返回，句柄悬空，其后每次 `commit` 都写进不可读的旧 inode（事实静默丢失）。修法＝把 `reopen(cut)` 提到 cut 写入之前（`reopen` 不读 cut 文件，只吃内存 cut 结构），失败路径因此落在「有效句柄＋整读日志（无 cut）」上，保住文档不变量「绝不出现描述不了日志的 cut」。 —— 验证：锚 `go test ./memory/ledger/ -run '^TestRewriteKeepsTheHandleOnTheLogItLeft$' -count=1` exit 0；**负锚**（旧序回滚）同命令 exit 1：`the commit filed after the failed cut reads [] … a handle on the unlinked inode writes where no open reads`；复原后复绿。
- [x] 7.2 **退位后驻留集不随日志收缩（`journalKV.retireSettled`）**：重写已把已搬迁键移出热日志，但驻留集仍留着它们的正文，于是同一进程按 key 读答复「有」，而下一进程开日志答复「无」——两进程视图不一致（且驻留预算被已搬正文占用）。修法＝搬迁成功后按 `settle` 逐个 `resident.remove`。 —— 验证：锚 `go test ./memory/ledger/ -run '^TestRetirementDropsTheSettledKeysFromTheResidentSet$' -count=1` exit 0；**负锚**（回滚该 purge）exit 1：`the settled key 0:evt:a reads 1 pair(s) through this view and 0 through a view that opened the log …`。
- [x] 7.3 **原店冷视图陈旧（`kvOriginalStore.cold`）＝证伪，不改**：`coldViewLocked` 确实缓存、且 `retireSettled` 的冷写入走另开的 `journalLog`，故缓存视图看不见后续 appends；但可达性不成立——`load` 把热层所有 `blob:`／`ref:` 全量载入 `byKey`（`kv_original_store.go:110-120`），`entryLocked` 先查 `byKey`（:258），`Put` 亦入 `byKey`（:345），`byKey` 从不清减（:445 `BlobCount`）⇒ 待读键只要「本趟可搬」，必在 `byKey` 内，冷视图不为它服务。实测（临时探针，已删）：一趟搬走后 `hot blob keys=0 byKey=1 coldOpened=false outcome=Found body="original-a"`。跨 Tier 实例模型下也不成立：热层 `settled:commit` 本身是该实例 open 时的快照（探针：reader 读回 `settled:commit "1"`，writer 已写 "2"），故按边界重开冷视图亦无效——该模型对「后到写入」整体不可见，属另一面（超出本域）。因此不加「按 settled 界重开」结构。
- [x] 7.4 **`commitSeq` 播种未含 `coldCommit`＝证伪，不改**：提交序号与事件计数同路径递增（`segment_store.go:604/632/633`），重复提交不占号、失败提交不入号，故 `count == 最大提交号`（密集 1..n），而 `coldCommit` 只取**已发布**被搬条目的最大号 ⇒ 恒有 `coldCommit ≤ count`，`s.commitSeq = int64(s.count)` 不会落在冷层序号之下；`coldCommit` 超过计数的唯一形态是「窗口 meta 缺失致条目未发布」，那已是既有面（:421-429 用已发布位置的号补高水位），本次不为病理态加半截补丁。
- [x] 7.5 **`EachEvent` 冷热非全局提交序＝证伪，不改**：该函数契约即「索引序（partition→window→seq），已搬条目随后」（`segment_store.go:1051-1055`），全局最新优先归并在 `eachIndexEntryNewestFirst`（:1320-1400，按 `commit` 归并热冷两头），两者职责不同。消费者面：`Records`/`Refusals` 自排序（`assembly/host_tools.go:781/869` 后接 `sort.Slice`），`delegationDeclaration`（:323）取首个携带声明的条目且站点唯一，均不依赖 `EachEvent` 的全局提交序。
- [x] 7.6 **退位不重置 `windows`／`EventCount`／读缓存＝证伪，不改**：`EventCount` 语义即「全量已提交历史」，`count = published + retired` 由设计保持（`segment_store.go:309/875/1031`，`cold_archive_test.go:110-112` 已钉），窗口 meta 的 `EventCount` 字段无任何读者（`grep -rn "EventCount" memory assembly` 仅写入与 store 级计数），读缓存是 1 MiB 有界加速器且命中的是不可变字节。无观察面者不改。

> 门禁（本轮）：`go test ./memory/... ./assembly/` 全 exit 0（assembly 281.1s）；`go build ./...` exit 0；`go vet ./memory/ledger/` exit 0；S1/S2/S3 三锚 `-count=1` 全 exit 0（S2 取证 `journalBytes=[0 110 110 110 110]`）；`scripts/gates/check_feature_boundary.py`、`check_durable_truth_registry.py`、`check_runtime_architecture.py` exit 0；`check_runtime_structure.py` exit 1 为**既有**行数超限（`tool/exec/exec_test.go`、`tool/subagent/tool.go`＋本变更早已超限的 `memory/ledger/{journal,kv_original_store,segment_store}.go`），非本单引入。

## 本轮重整叶（08.R2 → 08.R1）

> 叶级依赖：**08.R2（判定）→ 08.R1（整合）**；08.R1 前置＝**06.R2 ＋ 11.R2 ＋ 08.R2**。改判据形态的 R2 先做，避免 R1 在两种形态上各写一遍。
> 三锚纪律同前；**新增测试一律标「拟新增，尚未建立」**，不得引用为已跑证据。命令 exit 0 但零匹配不是凭据。

- [x] 08.R2 **以真实消费者判定保护能力的留删（调查/原型，不实施生产）**：前置09.R1；对齐D6.13与旧租约条款，分别证明搬冷后的原文读取、恢复未完义务、补交付及成本，不用keep成员关系判断结构必要性。交付＝每条原保护承诺的真实消费者、现有承担者、缺失行为或已重复的证据，以及经批准后交08.R1/10.R4实施的保留/合并/删除清单。正锚：拟新增 `TestRelocatedObligationMaterialStillServesItsConsumer`；负锚：拟新增 `TestRetirementRejectsUnreadableObligationMaterial`，两者尚未建立，建立后 `go test ./tests/integration/history -run '^(TestRelocatedObligationMaterialStillServesItsConsumer|TestRetirementRejectsUnreadableObligationMaterial)$' -count=10 -v`；消费者锚经正式恢复/召回/补交付入口验证内容、身份、义务，不以源码grep作消费者成立证明。原始独立能力尚未被证明替代时不删要求，发现现状已有保障则记录反证而不强造红。**08.R4 后逐条取代判定见 [design.md](design.md) §十四：本叶未被取代，脚手架「留或删」裁定仍开（本单不勾）。**
  - **调查期写入白名单**：拟新增 `tests/integration/history/retirement_obligation_test.go` 与本域design/tasks证据；生产文件全部只读。与06/11测试读取闭包相交时串行或隔离，不预先宣布可并发。
  - **实施交接**：必要的保护接线交08.R1，已证明冗余的代码及对应条款清理交10.R4；本叶只产出处置与证据，不先完成后续代码来满足本叶前置。

- [x] 08.R1 **整合退位与回退／身份读（依 06.R2＋11.R2＋08.R2）**：交付＝①搬迁单位对齐整事实提交后的键集（原文族＋事件三键同批；族拆不再是可构造状态）；②**孤儿原文（无事件身份的原文族）具名处置**：取甲（留热并计入自然成本，S1 报告单列孤儿字节）或乙（搬冷并保持按身份可读，报告单列无事件身份原文族计数），禁止「留热算已搬迁」的第三态；③回退整合：关闭退位后热层只追加、已搬事实仍按身份可读、重复提交仍判已存在（依 11 的提交语义）；④身份读三态在冷层损坏时具名拒绝；⑤spec 与代码同向（本域 spec 的三方法条款与闸条款按单能力/阻塞语义重写，不推给 10 域）。 —— 验证：正锚 `go test ./memory/ledger/ -run '^TestARetirementPassMovesAWholeFactOrNothing$' -count=1`（**拟新增，尚未建立**：一趟退位后一条事实的原文族与事件三键要么全在冷层、要么全在热层，重开档逐键按身份读到原字节）＋ `go test ./memory/ledger/ -run '^TestRelocatedFactReadsByIdentity$' -count=1`（既有，复验）；负锚 `go test ./memory/ledger/ -run '^TestColdArchiveRefusesCorruptColdTier$' -count=1`（既有，复验：冷层帧损坏→具名拒绝，不答不存在）＋ `go test ./memory/ledger/ -run '^TestARelocatedFactStaysReadableAfterTheSwitchIsOff$' -count=1`（**拟新增，尚未建立**：关闭退位开关后，已搬事实仍按身份读到原字节）＋ `go test ./memory/ledger/ -run '^TestAnOrphanOriginalIsReportedApartFromTheLiveSet$' -count=1`（**拟新增，尚未建立**：无事件身份的原文族在报告里单列，MUST NOT 混入存活键集字节）；消费者锚 `go test ./tests/integration/history/ -run 'TestJournalBytesBoundedByLiveSet|TestRecoverKVReadsARelocatedRoot|TestStorageCapabilities' -count=1` ＋ `go test ./memory/ledger/ -run '^TestColdArchiveHoldsTheExclusiveGate$' -race -count=10`（并发敏感面重复运行）＋ `go test ./assembly/ -run '^TestSymptomAnchorBootCostFlatAcrossGenerations$' -count=1`（不准为转绿放宽断言）。**08.R4 后逐条取代判定见 [design.md](design.md) §十四：①②③④ 已随退位删除失去对象（superseded），⑤ 残余＝脚手架判据随 08.R2（本单不勾）。**
  - **文件白名单（精确）**：`memory/ledger/segment_store.go`（`RetireFacts`/`keepKeysLocked`/`GetEvent` 三态）、`memory/ledger/kv_original_store.go`（原文族与冷读）、`memory/ledger/journal.go`（`relocate` 步界与 rewrite）、`assembly/run.go`（`relocatableStorage` 与 `keptHotKeys` 调用）、`assembly/checkpoint.go`（`relocateSettledFacts`）、`memory/ledger/cold_archive_test.go`、`memory/ledger/retirement_snapshot_test.go`、新增测试 `memory/ledger/retirement_integration_test.go`、`tests/integration/history/symptom_anchor_journal_test.go`（S1 报告字段）。
  - **前置**：06.R2（索引维护点稳定）、11.R2（整事实提交形状）、08.R2（判据形态已定）。
  - **冲突**：与 06.R2、11.R2 同触 `memory/ledger/segment_store.go`／`journal.go` → 串行或合单，不得并发派发。

## 08.R5 执行记录（2026-11-18，openspec-apply 单）

**实现（写面＝`scripts/fold-cold-root/main.go`、`scripts/fold-cold-root/main_test.go`、`docs/OPERATION.md`、本域 tasks/design）**：

1. **源离线＝整源盖戳而非逐日志自校验**：新增 `journalStamp`／`stampSource`／`holdSource`。`stampSource` 在读到任何一字节之前对**每个存在的日志**（热＋可选冷）取「长度＋修改时间」；`readSource` 读完全部日志后再 `holdSource` 一次，`-restore` 在自校验（`verifyFold`）之后、报告可切换之前再陈述一次。任一戳变动即 `ledger.ErrMigrateSourceUnchanged` 并点名移动的日志文件。原实现只在**各自日志**读取前后比较，热档读取期间冷档变动可被折成「热旧冷新」的混合快照，本次补的正是这条缝（负控 C 之外另见下「负控 A」：去掉 `holdSource` 后同一用例把变动后的源照折成计划）。
2. **确定性同步点**：`readTier` 读完一个日志后调用包级 `journalReadHook(dir)`（命令为 nil）。理由是「写入者在读期间恰好改源」无法从外部调度，睡眠碰撞会变成 flaky；注入点让用例在**同一读窗口内**移动源，而拒绝用的仍是命令自己的 `holdSource`（非另建测试专用校验）。该结构为测试注入用、生产零消费者，已在 design 与本记录具名披露，不声称它证明超出此面的东西。
3. **失败目标不报可切换**：`commitFold` 已拒非空目标与第二写者（既有锚保留），本次把「失败/半成品」的行为写清并实测：批次被日志帧上界（`ErrJournalFrameTooLarge`）拒或回读不等时返回错误 → `printReport` 不执行 → stdout 无 `read back by identity`；已落的事实留在目标根，**不删、不覆盖**，再跑即以 `ErrMigrateTargetNotEmpty` 拒绝；源根全程零写。

**三锚（正／负／消费者，`-count=10 -v` 实测）**：

- **正锚＋消费者锚** `TestOfflineConversionPreservesEveryFact`：经**同包构建的 CLI**（`buildFoldColdRoot` → `runFoldColdRoot`）跑干跑与 `-restore`。干跑 exit 0 且源根 `snapshotRoot` 逐文件 size/modTime/SHA-256 不变、目标根不存在；`-restore` exit 0 且 stdout 出现 `read back by identity`；随后**独立开档**（`ledger.OpenAtomicStore` 重读 + `ledger.OpenTier`）把折后根与「源被拆前的整史根」逐键比对：键集相等、逐键字节相等、`Segments.GetEvent` 每身份原字节、`Originals.Get` 每原文原字节、`EventCount` 相等、`EachIndexNewestFirst` 提交序与整史根逐项相等。
- **负锚** `TestSourceChangeRefusesConversion`：`journalReadHook` 在读窗口内向源热日志提交一个键，折回必须 `errors.Is(err, ledger.ErrMigrateSourceUnchanged)` 且点名 `kv/journal.log`；同时断言未产出计划、`foldReport{}`、目标根不存在、源根条目集与冷日志字节不变。
- **负锚** `TestPartialTargetIsNotReportedReady`：源多两条 17 MiB 原文使折回的原文批次越过日志 32 MiB 帧上界 → CLI exit≠0、stderr 含 `frame bound` 与首个被拒的 `blob:` 键、stdout 无 `read back by identity`／`committed`；目标根**半成品**（事件已落、`EventCount` 与源相等、零 `blob:`/`ref:` 键）；再跑一次被 `already holds data`＋目标路径拒绝，且半成品的键与字节逐一不变；源根两次 `assertUnchanged` 全绿。

**负控（证明三锚非空转，逐个改代码后复原）**：

- A 去掉 `foldRoot` 的 `holdSource` → `TestSourceChangeRefusesConversion` **红**：`the refused run stated the plan {Events:[…] Tiers:[hot cold]}`——变动后的源被当成一致快照折成计划，正是本条要求禁止的形态。
- B 把大原文夹具从 2×17 MiB 降到 1×17 MiB（批次不再越界）→ `TestPartialTargetIsNotReportedReady` **红**：`the restore … exited 0 with stdout "… committed, read back by identity …"`。
- C 在 `commitFold` 跳过原文提交 → `TestOfflineConversionPreservesEveryFact` **红**：`the restore exited 1 with stderr "the folded root holds other bytes for \"blob:fact-0-1\""`（CLI 直接拒报 ready）。
- 三次改动均已复原；复原后 `gofmt -l` 空、`go build ./scripts/fold-cold-root` exit 0、`go vet ./scripts/fold-cold-root` exit 0。

**未验证面（不得外推）**：

1. 源变化只在**函数入口**（`foldRoot`）取证，未在 CLI 子进程内取证：注入点必须同进程，故 CLI 层的证据面是「成功报告行」与「失败不报 ready」两条，不是「源变化在子进程里被拒」。
2. 只测了 `journal` 档源根与目标根；`file` 档按既有 `ErrSourceUnstated`／`ErrBatchNotAtomic` 口径未重测。
3. 半成品形态取自帧上界拒绝；盘满、写权限撤销、进程被杀等其它写入失败形态未逐个取证（同一错误出口，未另设用例）。
4. 真实用户数据根（`examples/wechat-bot/.wechat-config/data`、用户 `--source`）全程未读，夹具均为临时合成根。
5. 该叶与 08.R3／08.R4 原验证共同验收；本记录不宣告 08 域完成，也不替代 F 任务与归档。

## 08.R3 执行记录（2026-11-19，openspec-apply 单；删除面已核销，消费者锚未绿故不勾）

> **勘误（2026-11-19 同日接续）**：本段「未勾」结论已被推翻——两 fuse 红经 12.R2.6 于其域内修复，本轮重跑全树消费者锚 exit 0，**08.R3 已勾**。以下 §一–§五 原文保留为当时证据（5.8：历史不可变），现行结论以 **§六 接续核销** 为准。

**本单性质＝核销验证，零 .go 写入**（退位删除已于 0f84b2d1 落账：`cold_archive_test.go`／`journal_index_cut_test.go`／`retirement_snapshot_test.go`／`cold_archive_gate_test.go`／`tests/integration/history/recover_kv_relocated_root_test.go` 均不在索引与盘上）。证据：`find . -name '*.go' -newermt '2026-11-19 02:39'` = **0**（本单开工后无任何 Go 文件改动；02:03 的 8 个 `assembly/compress_*` 改动属 03.R5 在飞单，早于本单）。

### 一、负锚：零残留 grep（逐面命中数）

正则（`-w` 逐符号；面＝相对于仓库根的目录）：

```
RetireFacts|relocate|keepKeysLocked|keptHotKeys|relocateSettledFacts|coldRoot|WithColdRoot|
WithRetirement|cutMaintenance|coldEventLocked|coldViewLocked|coldIndex|ColdReadCost|ColdWriteCost|
RelocateReport|coldDirName|coldRetirement|retireSettled|copyToCold|verifyColdCopy|
segmentSettledKey|segmentSettledCommitKey|ErrColdArchiveUnavailable
```

| 面 | 全命中 | 非测试命中 |
|---|---|---|
| memory（含 `memory/ledger`） | 0 | 0 |
| assembly | 1（`symptom_anchor_longrun_test.go:492` 散文字样 `a checkpoint relocates it`，见下「待裁」） | 0 |
| tests / runtime / features / tool / api / internal / cli / examples / bench / codetools | 0 | 0 |
| scripts | 10（**全部在 `scripts/fold-cold-root/`**：`main.go:41/43/93` 定义「源根的冷日志目录名」＋`main_test.go` 7 处构造夹具路径） | 3 |

- **23 个符号逐个计数：22 个全树 0 命中**；唯一非零项 `coldDirName` 是**外部折回脚本对旧布局的命名**（源根 = `<root>/kv/journal.log` ＋ `<root>/cold/kv/journal.log`），属 08.R4 交付且语义与引擎退位机制不同义（脚本只读旧根、框架零分支），故**不删**。
- `relocat*` 前缀全树仅 1 命中＝上述 06 域锚测试里的散文字样（非生产、非断言）。
- `settleGate`／`ErrColdArchiveNotQuiet`：**Go 全树 0 命中**（`grep -rn --include='*.go'`），纯文档残留仍在 3 个 openspec 文件（本就只在文档面，非代码面）。
- 保留项复核（**明确不删**，各自命中且在位）：`Retention.Retire`（retention.go:111）、`ErrJournalLegacyLayout`＋`migrate.go`（shard 旧布局拒绝）、`journal.go:881`「rewrite 后未重建索引」叙述、`kv_resident.go`/`journal_kv.go` 的 cold-value 缓存词义。
- 级联确认：`judgeOpenIsBounded`／`IndexEntries`（非 `IndexEntriesRead`）／`placeIndexCut`／`appendJournalIndexCut`／`journalIndexCutVersion` 全树 0 命中；`factOriginalPlan`（kv_original_store.go:244）**无 `settled` 字段**。
- 两枚 flock 锚**已改名保留**（`git log -S "second write-rights open"` 证明二者随 0f84b2d1 同批引入）：`journal_kv_test.go` `TestJournalCommitThroughAReadFaceIsRefusedByName`（:213 `ErrRootWriteLocked`）＋`config_test.go` `TestWriteRightsAdmissionReportsTheRootByItsPath`（:135）／`TestSecondProcessOnOneRootIsRefusedWriteRights`（:37）。

### 二、正锚与消费者锚（原始 exit）

| 命令 | exit | 输出 |
|---|---|---|
| `go build ./...` | **0** | — |
| `go test ./memory/ledger/ -count=1` | **0** | `ok … 85.135s` |
| `go test ./assembly/ -count=1` | **0** | `ok … 332.211s` |
| `go run ./scripts/comment_policy . examples/wechat-bot examples/cli-host` | **0** | `0 finding(s); 0 beyond baseline` |
| `go run ./scripts/comment_policy -no-baseline assembly api runtime memory features tool internal cli` | **0** | `0 finding(s)` |
| `go test ./runtime/... ./features/... ./tool/... -count=1` | **0** | 17 包全 ok |
| `go test ./scripts/fold-cold-root -count=1` | **0** | `ok … 6.653s` |
| **`go test ./tests/... -count=1`（全树 13 包，未抽样）** | **1** | 12 包 ok；`tests/integration/execution` **FAIL 53.383s** |

**红项逐条归属（`tests/integration/execution`，非本叶引入）**：

1. `TestResponseCallCapFuse/approval_admits_the_whole_batch`（`response_call_cap_fuse_test.go:88`）：`chain increment = 1 actions beside 179 result facts`（期望每被准调用一条结果）；`TestStormPropagationFuse/an_approved_storm_leaves_a_bounded_chain_increment`（`storm_propagation_fuse_test.go:143`）：同断言 453 条。两者**单包单测复跑仍红**（`-run` 两枚 → exit 1，9.551s），**非负载抖动**。
2. `TestNativeConversationRoundTrip/…`（`native_conversation_roundtrip_test.go:93`，10s 未进模型请求）：**独占单跑 exit 0（0.392s）** ⇒ 13 包并行下 wall-clock 抖动，与 11 域 R58／ORCHESTRATION-HISTORY:1258 既有归因一致。
3. **归属取证（第 1 条）**：`git archive HEAD` 物化到 `/tmp/08r3-head` 后同命令 **exit 0**（9.225s）⇒ 红来自**未提交的在工作树改动**，不来自 HEAD、也不来自 08.R3；该测试文件 mtime 仍是 11-04（本变更未改它）；把工作树 `assembly/{facts.go,run.go}` 叠到 HEAD 树即 **build failed**（`facts.go:1002 r.loop.Backflow(...) (no value) used as value`）⇒ 在飞改动跨 `Backflow` 签名与消费路径（12 域 sink 乙案一簇），非单文件可归。

### 三、已提交事实可读性（承 08.R4 脚本，本叶只核存在与实跑）

`scripts/fold-cold-root` 在位且被索引；三锚 `-count=1 -v` 全 PASS（`TestOfflineConversionPreservesEveryFact` 1.17s／`TestSourceChangeRefusesConversion` 0.23s／`TestPartialTargetIsNotReportedReady` 1.53s，exit 0）。该锚内含：干跑零写＋`-restore` 后独立开档逐身份/字节/提交序核验（详见 `## 08.R5 执行记录`），故「含冷档的根折入新根后按身份读回原字节」这一面**已实跑**，本叶不重做 08.R4 的实现与证据。

### 四、为何不勾

叶验证面写死「消费者锚 `go test ./tests/... -count=1`」而现树为 **exit 1**；红项虽已具名归属到别的在飞单（HEAD 树同用例绿），但**验收面不追认缩水**，故 `08.R3` 保持 `- [ ]`。待两条 fuse 测试所在的在飞单收口（或由编排者裁定该红归属并明示除外）后，重跑 `go test ./tests/... -count=1` 得 exit 0 即可勾选。6.x 旧实施行的 superseded 口径已登记在 [design.md](design.md) §十三（不改写历史原文，仅附勘误指针）。

### 五、待裁与披露（本单未自决的部分）

1. **`assembly/symptom_anchor_longrun_test.go:492` 散文字样**「a commit reads the journal a growing history left and a checkpoint **relocates** it」——退位删除后无搬迁机制，此句为陈旧断言（§〇元规则一）。该文件属 06 域已核销锚（R291 用其 mtime 作零改动凭据），本单白名单未含 06 面，故**只登记不改**；建议随 06 域文档/注释小修清一句。
2. **08 域 `specs/storage-lifecycle-completion/spec.md:5`** 仍写「只拒销毁，**无损搬迁**（压实复制原文到高层段）允许」——括号内限定为同根压实复制（既有 age-tier 机制），非已删的冷层搬迁；措辞可被误读为退位，建议后续 spec 叶改为直写「压实复制」以消歧义。本单白名单未含 spec 面，未改。
3. **删前「含已搬事实的根」实证未单独重做**：该形态的构造与读回已由 08.R4 脚本的 `TestOfflineConversionPreservesEveryFact` 承担（本叶实跑绿）；若要「删前/删后」两点对照，需旧 HEAD 上仍可构造冷档，属 08.R4 面，本叶不外推。§五 第 1／2 条的待裁小项本轮已在白名单内处理，见 §六·4。

### 六、接续核销（2026-11-19，openspec-apply 单；fuse 红经 12.R2.6 修复后重跑，全树绿⇒已勾）

**本单性质＝核销验证＋两处措辞/散文小修；零生产 .go 写入**（`.go` 唯一改动是 `assembly/symptom_anchor_longrun_test.go:492` 一行注释：`git diff --numstat` = `1 1`）。

**1. 上轮唯一阻断的红因已在其域内消解（不属本叶除外裁定）**：`tests/integration/execution` 两 fuse 的根因＝`assembly/facts.go` `foldMapped` 的 `newestOfChainLocked != evt.Key` 顺序守卫把「非本链最新」误当「重试既存」——200 条结果事实并发提交时，登记后折叠前同链可再落地一条，合法提交被整条投影丢弃并跳过 `endedAt`/`rangeOpen` 折叠，故 cap 记 184≠200、storm 边界停在 403≠460。修复（**12.R2.6**，见一级 ORCHESTRATION `R304`）＝删该守卫与 `newestOfChainLocked`，新增 typed 可选面 `repeatReducer{ReduceRepeat}`，提交侧 carried/retry 走 `ReduceRepeat`。本轮不复述其实现，只据其修复后重跑本叶验收面。

**2. 本轮命令与原始 exit（最终树，含下述小修）**：

| 命令 | exit | 输出 |
|---|---|---|
| `go build ./...` | **0** | — |
| `go test ./memory/ledger/ -count=1` | **0** | `ok … 84.872s` |
| `go test ./assembly/ -count=1` | **0** | `ok … 319.751s`（终树复跑；含 `TestSymptomAnchorPerGenerationCostFlatOverALongRun` 时序锚） |
| **`go test ./tests/... -count=1`（全树 13 包，未抽样）** | **0** | 13/13 ok；`tests/integration/execution` **ok 44.692s**（上轮 FAIL 处）、`tests/integration/host` ok 205.104s |
| `go test ./runtime/... ./features/... ./tool/... -count=1` | **0** | 17 包全 ok |
| `go test ./tests/integration/execution/ -run '^(TestResponseCallCapFuse\|TestStormPropagationFuse\|TestNativeConversationRoundTrip)$' -count=1` | **0** | `ok … 9.216s`；`approval_admits_the_whole_batch`／`an_approved_storm_leaves_a_bounded_chain_increment` 两子用例具名 PASS |
| `go run ./scripts/comment_policy . examples/wechat-bot examples/cli-host` | **0** | `0 finding(s); 0 beyond baseline` |
| `go run ./scripts/comment_policy -no-baseline assembly api runtime memory features tool internal cli` | **0** | `0 finding(s)` |
| `npx --no-install openspec validate runtime-recovery-ideal --strict` | **0** | `Change 'runtime-recovery-ideal' is valid` |

`TestNativeConversationRoundTrip` 本轮 13 包并行下即绿，未触发独占复跑（上轮归因「并行 wall-clock 抖动」未被推翻，只是本轮未发生）。

**3. 负锚：零残留 grep（本轮复跑，正则同 §一）**：

- 23 符号**非测试全树 3 命中，全部在 `scripts/fold-cold-root/main.go:41/43/93`**（`coldDirName`＝外部折回脚本对**旧布局**的命名，属 08.R4 交付，与已删引擎退位机制不同义）；**非测试命中中落在 `scripts/fold-cold-root` 之外＝0**。
- 含测试全树 10 命中＝上述 3 ＋ `scripts/fold-cold-root/main_test.go` 7 处夹具路径；`memory`／`assembly`／`tests`／`runtime`／`features`／`tool` 等面 **0**。
- `relocat*` **全树 Go 0 命中**（上轮唯一 1 命中 `assembly/symptom_anchor_longrun_test.go:492` 散文本轮已改，见 4）。
- `settleGate|ErrColdArchiveNotQuiet` Go **0 命中**。
- 保留项复核不变：`Retention.Retire`、`ErrJournalLegacyLayout`＋`migrate.go`、`headless-retired`、cold-value 缓存词义均在位且不同义。

**4. 措辞与散文小修（两处，均在白名单内）**：

- `specs/storage-lifecycle-completion/spec.md`：`:5` 与 `:18` 的「**无损搬迁**（压实复制原文到高层段）」→ 直写「**压实复制原文到高层段**」。括号本就限定同根压实复制（既有 age-tier 机制），但「搬迁」一词在退位删除后可被读成已删的冷层搬迁；直写后无歧义。`:18` 是同一 Requirement 的 Scenario 句、同一词、同一歧义，只改 `:5` 会留下文件内自相矛盾，故同批改（申报见交接，非扩大验收面）。主 spec `openspec/specs/storage-lifecycle-completion/spec.md:62/71` 同词，属归档时由本 delta 的 MODIFIED Requirement 覆盖，不在本单白名单。
- `assembly/symptom_anchor_longrun_test.go:492`：「…and a checkpoint **relocates** it」→「…and a checkpoint **states its account over it**」。依据：`checkpointLocked`（`assembly/checkpoint.go:343`）只读水位＋快照状态并 file 一条事实，退位删除后无任何搬迁路径；原句是伪断言。**仅注释**，未动断言/夹具。
- **如实登记一次自造红**：该散文首版写「…states its account over that history without moving it」，触发 `comment_policy` 的 `test-doc-line-too-long`（`testDocMaxLineRunes=160`，首版 176）→ `CP_A_EXIT=1`。改短至 153 runes 后两口径复归 `0 finding(s)`（exit 0）。教训：该锚 bullet 行有形状上限，改散文须先量长。

**5. 为何本轮可勾**：叶验证面六项（`go build`／`memory/ledger`＋`assembly`／`tests/...` 全树不抽样／`runtime+features+tool`／两注释门／零残留 grep）本轮在**同一最终树**上全部 exit 0；上轮唯一红项已由 12.R2.6 在其属域修复并复跑证绿，非以「除外」缩水验收面。本记录不宣告 08 域完成（08.R4 未核销），不改一级编排文件，不触发 F 任务与归档，未提交。

## 08.R4 执行记录（2026-11-19，openspec-apply 单；删除面＋脚本＋升级说明齐绿⇒已勾）

**本单性质＝核销验证；零生产 `.go` 写入**。证据：`find . -name '*.go' -newermt '2026-11-19 03:40' -not -path './.git/*'` = **0**（本单开工后无任何 Go 文件改动）。切面族与 `journal_index_cut_test.go` 的删除随 `0f84b2d1` 落账（`git ls-files | grep index_cut` 空、盘上无该文件、`memory/ledger/` 下无 `*cut*` 文件）。

### 一、负锚：切面符号零残留（逐符号 × 逐面命中数）

正则（`\b` 词边界，`glob=*.go`）：

```
\b(placeIndexCut|cutOf|appendJournalIndexCut|writeJournalIndexCut|readJournalIndexCut|journalIndexCutVersion|journalIndexCutFileName)\b
```

| 面 | 命中 |
|---|---|
| memory（含 `memory/ledger`） | 0 |
| assembly | 0 |
| runtime | 0 |
| features | 0 |
| tests（全树） | 0 |
| scripts（`fold-cold-root`／`recover-kv`／`migrate-kv` 全部） | 0 |
| examples | 0 |
| tool ／ api ／ internal ／ cli | 0 ／ 0 ／ 0 ／ 0 |
| **`*.go` 全树（不限面）** | **0** |

- **`cutOf` 的子串陷阱已排除**：不设词边界时 `assembly/checkpoint_test.go:366/399/400` 会因 `cutOffset` 命中——那是 checkpoint 的 `cut`（`CheckpointFiled()` 返回的偏移），与已删切面不同义项，加 `\b` 后为 0，故**不删、不改**。
- **06 design §S4 移除 inventory 的其余 delete 项也全部为 0**：`\b(journalRewriteFileName|rewriteInto|journalLog\.rewrite|cutFixture|judgeOpenIsBounded)\b` = 0；`journalIndexCut`／`journalCutValue`／`journalCutOrder`／`cutStatement`／`IndexCutNotice`／`seedIndexCut`／`maintainsCut`／`statesCutMaintenance`／`noteTail`／`tailKeys`／`cutNotice` 亦为 0。`memory/ledger/journal.go` 现存方法表已无 `rewrite`／`reopen`／`relocate`／`seedIndexCut`（`grep -n 'func \(l \*journalLog\)'` 逐行列）。
- **文档面命中不删**：`ORCHESTRATION-HISTORY.md`／`06-bounded-open-index/{proposal,design,tasks}.md`／本域 `tasks.md`／`ORCHESTRATION.md` 的切面字样是历史证据（5.8：历史不可变），非代码残留。
- **`scripts/fold-cold-root/main.go:41/43/93` 的 `coldDirName` 保留**（08.R3 §三已判定：脚本对**旧布局**的命名，与已删引擎退位机制不同义）。

### 二、正锚与消费者锚（原始 exit）

| 命令 | exit | 输出 |
|---|---|---|
| `go build ./...` | **0** | — |
| `go test ./memory/ledger/ -count=1` | **0** | `ok … 88.919s` |
| `go test ./scripts/fold-cold-root/ -count=1` | **0** | `ok … 6.094s` |
| `go test ./scripts/recover-kv/ -count=1` | **0** | `ok … 2.319s` |
| `go test ./tests/... -count=1`（全树 13 包，未抽样） | **0** | 13/13 ok；`execution` ok 42.692s、`host` ok 206.576s、`history` ok 19.825s |
| `go run ./scripts/comment_policy . examples/wechat-bot examples/cli-host` | **0** | `0 finding(s); 0 beyond baseline; 0 ratchet slot(s) can be lowered` |
| `go run ./scripts/comment_policy -no-baseline assembly api runtime memory features tool internal cli` | **0** | `0 finding(s) over [api assembly cli features internal memory runtime tool]` |
| `npx --no-install openspec validate runtime-recovery-ideal --strict` | **0** | `Change 'runtime-recovery-ideal' is valid` |

两注释门各带一行非阻断提示（`1 git-ignored Go file(s) are outside the ratchet: examples/wechat-bot/knowledge_base`），非 finding、exit 0；该文件在 git 忽略面，本单未触。

### 三、脚本三锚＋干跑／拒绝／原序（08.R5 复跑，`-count=1 -v`）

| 锚 | 结果 | 收紧的验收面 |
|---|---|---|
| `TestOfflineConversionPreservesEveryFact` | **PASS** 1.16s | 干跑 exit 0 且 stdout 含 `dry run`（`printReport` 先无条件打印 `%d event(s), %d original key(s), %d key(s) to state`＝待折键数，见 `main.go:768-773`）；源根 `assertUnchanged`（逐文件 size/modTime/SHA-256）＋目标根不存在 ⇒ **零写入**；`-restore` 后 stdout 含 `read back by identity`；折后根与整史根：键集相等、逐键 **`bytes.Equal`** 原字节、`Segments.GetEvent` 每身份原字节、`Originals.Get` 每原文原字节、`EventCount` 相等、`indexOrder`（提交序）**逐项相等** |
| `TestSourceChangeRefusesConversion` | **PASS** 0.37s | 读窗口内源变动 ⇒ `ledger.ErrMigrateSourceUnchanged` 具名拒绝并点名日志；未产出计划、目标根不存在、源不变 |
| `TestPartialTargetIsNotReportedReady` | **PASS** 1.66s | 批次越帧上界 ⇒ CLI exit≠0、stdout 无 `read back by identity`／`committed`；半成品键与字节不变；源根两次 `assertUnchanged` 绿 |
| `TestFoldRootRefusesARootAnotherWriterHolds` | **PASS** 0.31s | **负锚：写者拒绝**。目标根被第二写者持有时以 `ledger.ErrRootWriteLocked` 具名拒绝（内核 flock） |

非空转负控 A/B/C 已由 08.R5 记录（去 `holdSource`→源变化锚红；缩大原文夹具→半成品锚红；跳原文提交→保真锚红），本单未重放，据其记录同批有效。

### 四、升级说明落点（交付③）

落在 **`docs/OPERATION.md` §F（`:246-250`）＋升级章入口 `:161`**，随 08.R5 同批落盘：`:248` 干跑→`-restore` 两步与「默认干跑零写」、`:249` 源根只读且必须离线、`:250` `file` 档 `ErrBatchNotAtomic` 后果＋`ErrSourceUnstated` 具名拒绝＋目标根须空／一根一写者／半成品不报 ready、`:161` 升级入口指针。本单**未改** `docs/OPERATION.md`：`python3 scripts/gates/check_doc_budget.py` 报 `slack.files: 0`／`slack.lines: 0`（正文体量棘轮无余量），内容已齐，搬运即净增。指针与理由已登记在 [design.md](design.md) §十二·五。

### 五、08.R1／08.R2 状态评估（不勾）

逐条判定见 [design.md](design.md) §十四（本轮新增）：

- **08.R1** ①②③④ **已取代**——搬迁单位、孤儿原文处置、回退开关、冷层三态四处交付对象的机制已整删（`RetireFacts`／`relocatableStorage`／`WithRetirement` 全树 Go 0 命中）；⑤ 部分已取代（三方法／闸条款已不在 delta，「无损搬迁」→「压实复制」由 08.R3 改），残余＝脚手架判据。
- **08.R2 未被取代，仍开**：本域 delta 的 `storage-lifecycle-completion` MODIFIED 条款硬写「`NewLifecycle`／`NewRetention`／`NewMemoryGuard` 任一构造 SHALL 要么有生产调用方，要么被移除；MUST NOT 停留在只被测试与 `skeleton_assert.go:150` 引用的状态」，而复跑 `\b(NewLifecycle|NewRetention\(|NewMemoryGuard)\b` 于 `*.go` 全树仍是**生产 0 命中**（仅 `skeleton_assert.go:149`＋`memory/partition_test.go`＋`tests/integration/history/{recovery_before_reclamation,original_reference_isolation}_test.go`）——正是该条款点名的第三态。10 域已把未接线脚手架所有权转给 08（`10/tasks.md:72`），故本域是唯一归属方。
- **剩余项（待派，未勾）**：①脚手架「留（接线到某条现行判据并具名说明）或删（连同 `skeleton_assert.go:149` 与相关测试、spec 硬要求一并移除）」二选一；②08.R2 原两条判别测试（`TestRelocatedObligationMaterialStillServesItsConsumer`／`TestRetirementRejectsUnreadableObligationMaterial`）的构造前提随退位删除失效，其锚须由拥有者**重定**——本单只登记失效，**不宣布该验收面不再需要**（验收面不追认缩水）；③该条款内 `skeleton_assert.go:150` 应为 `:149` 的指针漂移随①同批更正。

### 六、为何本轮可勾、勾了什么

叶验证面四项（正锚 `build`＋`memory/ledger`＋两脚本包；负锚 7 符号逐面 0 残留＋写者拒绝＋干跑零写＋原字节原序；消费者锚 `tests/...` 全树不抽样＋两注释门；交付③升级说明）在**同一最终树**上全部 exit 0，且交付①②③三项均有落点与凭据 ⇒ `08.R4` 勾 `[x]`。

**未勾**：08.R1／08.R2（§五 列出剩余项）。本记录不宣告 08 域完成，不改一级编排文件，不触发 F 任务与归档，未提交。

## 08.R2 执行记录（2026-11-20，openspec-apply 单；甲案移除落地＋全绿⇒已勾）

> 本记录**取代**上节末句「未勾：08.R1／08.R2」中关于 08.R2 的部分（历史原文不改写，按 5.8 勘误体例在此追加）。方向由 ORCHESTRATION R307 裁定为甲案（移除），本单不重开方向取证。

**前置核对（不重复质疑，只复核落点）**：`NewLifecycle`／`NewRetention`／`NewMemoryGuard` 生产调用方 0；`Guard`／`MemoryGuard`／`LifecycleStore`／`LifecycleCapabilities`／`LifecycleConfig`／`StoredKey`／`RetentionRecord`／`HolderID`／`ProtectionLease`／`ScanReport` 生产引用 0。两条保护语义各有唯一活载体：原文过期（`RetentionWindows`→`kvOriginalStore`→`ErrOriginalExpired`／`RecallExpired`→`tool/recall/get.go:225`）、未结义务（热窗 pin：`assembly/run.go` `pinFact`／`unpinFact`／`keepPinnedLocked`／`deliveryPendingFor`，结算写于 `assembly/host_delivery.go`）。故本叶交付从「调查证据」转为「移除落地＋活载体指向」，逐条见 design §十五。

**交付与判定**：

1. **删除面**（design §十五·二）：四个整文件（`lifecycle.go`／`retention.go`／`guard_memory.go`／`lifecycle_config.go`）＋`guard.go`（`RecallOutcome`／`RecallResult` 迁 `reference.go` 后整文件删）＋`skeleton_assert.go` 四类断言与两区块＋`doc.go` 散文＋级联测试（`memory/partition_test.go` 两测三 helper、`tests/integration/history/recovery_before_reclamation_test.go` 整文件、`original_reference_isolation_test.go` 子测与 `assertProtected`）＋两 `examples/contract` 例。**无一条活谓词被删**：删的每条都是被删对象的构造前提。
2. **保留项复核**（§十五·三）：`Retention.Retire` 随类型删——`Retention` 唯一构造 `NewRetention` 生产 0 调用且无其他生产嵌入，属「构造链已证断」而非「不明」；generation retirement（`run.go` 代际退位三函数）、`migrate.go`、`ErrJournalLegacyLayout`（`config.go:122-146`）未动。
3. **两跨域锚判定**（§十五·四，先读断言本体）：锚一 13 条断言点全打 `ProtectionLease` 机制、零条触及义务语义 ⇒ superseded，其多持有者语义被「每事实单一属主 pin」形状取代，承接锚三支；锚二 6 子测逐条判：G2.2／G2.6 迁移至具名活锚，G2.3 上层语义由 `TestOfflineConversionPreservesEveryFact` 承接，G2.1／G2.4／G2.5 无活等价**具名登记**（不宣布验收面不需，亦不追认缩水）。引用方为冻结 archive `2026-10-18-runtime-rebuild` 的 03.1／03.5 与 O2-5，历史文件未改写，勘误登记于本域 design。
4. **级联落点**（§十五·五）：`product_surface_baseline.json` 清两陈旧项（37→35，基线收紧非抬高）；wiki 四处伪断言逐行原地改真（doc-budget 22149 行 / slack 0 未变）；`host_delivery.go` 注释去 lifecycle 指引；`docs/api/` 重生成（本单两文件，余 6 为既有陈旧）；08 delta spec 改 REMOVED（TTL check，时间真相归主规格 `event-segment-store`「时间真相源单一」）＋MODIFIED（重写为无销毁路径／按身份恒可读／两保护各一活载体／能力诚实 fail-closed 四条真判据），**不留死条款**。

**验证（同一最终树，原始 exit）**：`go build ./...`=0；`go vet ./...`=0；`go test ./memory/ledger/ -count=1`=0；`go test ./assembly/ -count=1`=0；`go test ./tests/... -count=1`=0（13 包）。负锚：31 个移除符号词边界全树 grep 零残留，唯 `\bLifecycle\b` 命中宿主 `DiagnosticLifecycle` 字段（不同义项）。门：`comment_policy` ratchet 0 越基线＋product 0 finding；`check_concept_budget.py --json`=0；`check_product_surface.py` 0 error／0 warning／35 hooks；`check_dead_producers.py`=0；`check_doc_budget.py`=0（slack 0/0）；`check_doc_links.py`=0；`gen_godoc.sh --check`=0；`openspec validate runtime-recovery-ideal --strict` valid。活载体锚实跑绿：`TestOfflineConversionPreservesEveryFact`、`TestPinnedDeliveryFactsSpendTheWindowNotTheFoldProducts`、`TestCrossSessionDeliveryObligationSurvivesTheTargetSessionCut`、`TestPinnedFactsReseeded`、`TestOriginalReadEntrypointsPreserveBytesAndScope/a_reference_past_its_window_reads_as_expired`、`TestStorageCapabilities`、`TestOriginalReferenceIsolation`。

**已知界外红（如实登记）**：`bash scripts/lint.sh --only policy` exit 1，唯一失败 `scripts/gates/test_check_runtime_symptoms.py::test_registry_matches_the_module`——该组门文件属他域在飞变更（未跟踪），在 ORCHESTRATION「不触碰」清单内，本单 0 行改动；故 comment_policy 两聚合单独跑，均 0。

**勾选依据**：正锚／负锚／消费者锚与全部门在同一最终树 exit 0；两跨域锚已判并或迁移或具名登记；无预算超支、无停线项触发。**本记录只勾 08.R2**（08.R1 仍未勾），不改一级编排文件，未提交。

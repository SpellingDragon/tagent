# 06 开档与索引 · 孙任务

> **当前承接（R265）**：索引读侧与去全量物化已有实现，不恢复已删除的退位/切面。06.R3仍需核旧键查询起点的历史成本；**06.R5 已于 2026-11-18 按存活路径（frame／closing）重钉屏障判据并核销**（[design.md](design.md) §二十八）；09.R2负责独立阶段归因。以下“停稳根读0字节”只指索引回放窗口，完整帧校验仍读全日志；不能据此宣称整个boot零读取。旧常驻索引规模与当前枚举计数分开保留测量，不偷换原量。

> ## ⚠️ 文件级勘误（2026-11-16，反转后现状）
> 本文件横跨两个机制世代。**§「本轮状态回退声明」起的 2.x 原文、以及 06.R1／06.R2 的执行记录段，描述的是「退位＋索引切面」世代**，其中下列断言**今天均已不成立**，保留原文只作历史证据（5.8：历史不可逆，推翻结论记在本节）：
> - 「切面只在 `journalLog.rewrite`（退位重写）落盘」「未开启退位时无切面，开档仍是整份读」「`journalIndexCutVersion=1`」——切面族（`placeIndexCut`／`cutOf`／`writeJournalIndexCut`／`appendJournalIndexCut`／`readJournalIndexCut`／`journalIndexCutFileName`）已零调用，随 08.R4 删除；开档 `from` 改由 **`journalStatedWatermark(dir)` 读槽表表头**取得。
> - 「`openJournalLog` 仍从偏移 0 全量重放，故 boot 的 `journalBytes` 仍等于整份日志字节」——**已不成立**：开档只重放上次索引陈述水位以来的窗口，停稳根实测**读 0 字节**（7.37MB 日志，R102）。
> - 「旧 S2 的绿由退位把热层变小造成」——退位已删，热层字节增长改为显式声明、单独拥有的关切；S2 的三道界分属日志层窗口／store 层常驻索引／耐久任务账扫描三层（归 09.R2 分栏）。
> - 「整趟退位仍 O(冷键数)」「退位 cut 写」「冷层首读」「`a deployment without retirement always reads whole` 的具名退化」——机制与其载体均已删除，无对象。
> **现行有效交付**：06.R4（S2 载体落地）／06.R3（store 常驻索引去物化）／06.R6（槽表负载计数耐久）／06.R8（日志窗口字节界）／06.R9（读面退化读链）／S1–S3b（读侧接线）。现行机制与读数见 [design.md](design.md) 文件级勘误与 §十七起。

> verify 三要素随任务行冻结（R29）。正锚测试名与 `-run` 正则逐字符一致；负锚具名错误路径；消费者锚正式 boot 入口（`Assemble`，assembly/run.go:491 → `assemble`:496 → `ledger.OpenTier`:606 → `restoreCheckpointState`:690 → `seedReplay`:704）。
> **与 01（task-custody）/04（acceptance-real-measure）合单原子（W1）**：同触 boot 路径簇，不可并行派发（一级 design.md:45、tasks.md:32）。
> **设计闸门**：2.1 的索引切片载体层与签名属 §五 共享面（R17 编排者单写），[design.md](design.md) §二 冻结前 2.1 不动工。
> **共享面冲突已裁定（已解决）**：`indexEntries`＝常驻索引条目 `len(s.index)`（一级 §五），**非 `EventCount` 全史标量**；04 四处（proposal/design/spec/tasks）已同步归正，本域 2.4 同口径（design.md §三）。
> S2 为共享症状锚（一级 design.md:93），本域只引用不重定义；四真量探针与多代多任务夹具归 04。

## 当前补差（R265）

- [x] 06.R3.a 旧键前驱定位的完整成本轴（06.R3补差）。产出：按正式历史查询路径同时计入“最新记录到目标键”和“目标键到前驱”的查找，不能只量后一段。前置：09.R2计量定义；写面为`assembly/read.go`、`assembly/read_test.go`及对应本域计划，若需引擎接口先在本域design枚举既有可寻址能力并冻结最小共享面。正/负锚（拟新增、未运行）：`go test ./assembly -run '^(TestParentLookupCountsTheSearchBeforeTheTarget|TestParentLookupKeepsSessionIsolation)$' -count=10 -v`，固定目标与前驱间距、增加目标之后历史，核完整访问量；跨会话/分区结果仍正确。消费者锚（拟新增、未运行）：`go test ./assembly -run '^TestRecallTraceThroughTierCountsOldTargetLookup$' -count=10 -v`，在`assembly/read_test.go`通过正式recall_trace工具及真实Tier读面构造旧目标，不用替身的恒定GetParent代替；现有回归：`go test ./assembly -run '^(TestTierParentSurvivesDurableReopen|TestRecallPortLateBindsParentReader)$' -count=10 -v`及`go test ./tool/recall -run '^TestTraceOfDepthDPaysDLocalLookupsNotDHistoryWalks$' -count=10 -v`。后者仅量工具层，不能独自放行引擎成本；独立审查区分原生成本与可消除扫描。**（2026-11-18 已核销：三锚真建真跑、非零匹配、各带 overlay 可红演示；引擎零改，既有 `IndexWalkReads()` 已覆盖两段，无需新接口。执行记录与凭据见 [design.md](design.md) §二十九：两段成本轴 18/66/258、消费者锚 398、原生 vs 可消除分类。）**
- [x] 06.R5.a 现有提交与关闭屏障验证承接（06.R5补差）。产出：把已删除机制的旧屏障锚逐一映射到当前写入/关闭路径；在09.R2里分别直接计量，不从总fsync次数反推阶段。前置：11.R4提交结果分类（[x]）；写面`memory/ledger/journal_test.go`、`memory/ledger/journal_kv_test.go`及本域计划，计量实现若需生产修改先登记精确落点（结论：无需生产修改，读引擎自身已持状态即可命名阶段）。正/负锚（已新增、已运行）：`go test ./memory/ledger -run '^(TestCommitBarrierMeasurementNamesItsPhase|TestCloseBarrierFailureStaysVisible)$' -count=10 -race -v`；消费者锚：`go test ./assembly -run '^(TestFactCommitLandsOriginalAndRecordInOneCommit|TestCloseFilesTheCheckpointTheNextBootReads)$' -count=10 -v`（两枚现有、本轮已跑），并在`assembly/checkpoint_test.go`新增`TestCloseReportsTheActualBarrierFailure`，命令`go test ./assembly -run '^TestCloseReportsTheActualBarrierFailure$' -race -count=10 -v`；该文件仅测试补差纳入本叶写面，与12串行。独立验证错误确从实际屏障返回，不是包装层提前拒绝。**执行记录与凭据见 [design.md](design.md) §二十七**（旧锚→现行路径映射、三条命令退出码、可红演示、注入面如实限制）。

## 本轮状态回退声明（历史，不作为当前派发入口）

一级 proposal/design 已撤销旧整体完成结论。本文件 2.1／2.2／2.3／2.5 的旧 `[x]` 反映的是**旧证据时点**（2026-11-15 W3 合单，当时全量 assembly 绿、S2 记为绿），现一律回退为 `[ ]` 待复验。理由（✅ 读码，见 [proposal.md](proposal.md)「本轮已核出的口径偏差」）：

- 切面只在 `journalLog.rewrite`（退位重写，journal.go:750-767）落盘，未开启退位时无切面，开档仍是整份读；
- `FileSegmentStore.Load`（segment_store.go:346-401）仍整表读 `{pid}:idx:`／`{pid}:meta:` 前缀；
- 旧 S2 的「绿」由 08 退位把热层变小造成（退位关时 `journalBytes` 仍线性），不构成 06 的独立兑现；
- 2.3 的「响亮失败」在派生加载未独立成立前只是潜在路径。

旧证据不删、不改写（保留上述时点与行号），但**不得**据此计为当前完成。2.4 本就未勾，保持。

- [ ] 2.1 【待复验】 **引擎自持索引切片＋派生加载（D6.8：不从 checkpoint 派生，避循环依赖）**（**状态：P4-cont 后仍未动工，未勾选**）：P4 落冷归档引擎核心，P4-cont 落全局提交序（idx 值携 commit 单调序号，热冷按序号归并）与水位制退位的装配接线；**索引切片文件（切面偏移＋存活键集 span）未落**——`openJournalLog` 仍从偏移 0 全量重放，故 boot 的 journalBytes 仍等于整份日志字节（S2 实测 [0 75465 169010 281113 411748]，随历史线性）。切片的输入面（水位后的尾键集）已在 `Run.keptHotKeys` 就绪，可直接复用。引擎在安静点落**自持**索引切片（存活键集 key→span、帧序、已提交计数、window meta、切面偏移；临时文件 + 原子 rename）——索引切片是引擎自有持久结构，**SHALL NOT 从 checkpoint 事实派生**（checkpoint 自身是 journal 事实，anchorEntry 无 partition/seq，派生即循环）；checkpoint 托管消费者读态（投影幸存者/任务/governance），引擎索引是另一层；`openJournalLog`（memory/ledger/journal.go:471-503）读切面 seeding `index`/`order`；`FileSegmentStore.Load`（memory/ledger/segment_store.go:281-380）不再全量读 `{pid}:idx:`（:311）与 `{pid}:meta:`（:339）前缀，published 集由切面存活键集 ∪ 尾派生；`s.count`（:373）与 `s.windows`（:372）由切面标量与表派生，**`EventCount`（:584）语义不变**；切面文件名避开 `rejectLegacyShardLayout`（memory/ledger/config.go:109-126）拒绝的目录与 `shard_*.json`、避开 `snapshot.json`（migrate.go:25）；journal.go:40-42 目录注释就地归位 —— 验证：正锚 `go test ./memory/ledger/ -run TestSegmentLoadDerivesThePublishedIndex`（新增：多代存储重开档，published 集＝存活键集 ∪ 尾，`EventCount` 仍等于全部已提交数）＋ `go test ./memory/ledger/ -run 'TestSegmentLoadLazyPeak|TestColdReadAfterRestart|TestSegmentLazyLoadPreservesEventCount|TestSegmentLazyLoadPreservesConsumerContract'`（既有四枚保持绿，断言不改）；负锚 切面损坏 → `ErrSnapshotCorrupt`（memory/ledger/kv_original_store.go:27，Load 已用于 window meta，segment_store.go:353）具名告警并退回全扫重建，`go test ./memory/ledger/ -run TestOpenRebuildsFromTheChainWhenTheIndexCutIsCorrupt`（新增）；消费者锚 `go test ./assembly/ -run TestBootRestoresTheCheckpointOnce`（assembly/checkpoint_test.go:734，走 `Assemble` 正式入口重开档恢复，非直接调 `Load`）＋ `go test ./tests/integration/history/ -run TestStorageCapabilities`（EventCount 计数面零改）。

- [ ] 2.2 【待复验】 **尾增量索引**：`journalReplay`（memory/ledger/journal.go:433-466）起始偏移由 `int64(0)` 改为切面偏移，尾增量照旧走 `publish`:508 → `indexRecord`:523 → `noteOrder`:534；`Load` 只补尾（seq > Offset）的 idx/meta；`order` 只保留存活键集键 + 尾键，按**逆帧序去重后反写**（journal.go:115-121 帧序契约）；撕裂尾 `Truncate`（journal.go:495-499）语义不变；切面偏移 > 文件完整尾 → 具名拒绝开档，不静默全扫；第二视图无锁（config.go:170-176）下切面偏移须 ≤ 真实完整尾 —— 验证：正锚 `go test ./memory/ledger/ -run TestOpenReplaysOnlyPastTheIndexCut`（新增：开档读入字节＝切面 + 尾，不随历史增长）＋ `go test ./memory/ledger/ -run TestEachIndexNewestFirstStreamsTheCommitOrder`（segment_store_test.go:508，跨分区帧序 third/second/first）＋ `go test ./memory/ledger/ -run TestJournalStatesItsCommitOrderNewestFirst`（journal_kv_test.go:72，帧序 newer/evt/older 且删除键跳过）；负锚 派生 order 条目少于 published → `tagent: the engine states the commits of %d of the %d index entries it published`（segment_store.go:409），`go test ./memory/ledger/ -run TestLoadRefusesAnOrderThatMissesPublishedEntries`（segment_store_test.go:569）保持绿；切面偏移越过完整尾 → `go test ./memory/ledger/ -run TestOpenRefusesAnIndexCutPastTheCompleteTail`（新增，具名拒绝）；消费者锚 `go test ./assembly/ -run TestAnchoredBootMatchesFullBoot`（assembly/anchored_boot_parity_test.go:143，正式 boot 锚路径与全史路径逐条一致）。

- [ ] 2.3 【待复验】 **静默跳页改响亮失败**：`replayRef`（assembly/run.go:2889-2901）的 `if !held`（:2892）由 `return event.CompressProduct{}, false, "", nil` 改为具名 `ledger.ErrSegmentBodyMissing`，与 run.go:2315-2317、checkpoint.go:1003-1005、segment_store.go:608-610 同口径；`restoreSurvivors`（run.go:2758-2796）已具名 `ErrCheckpointSurvivorUnstated`（run.go:2785 / checkpoint.go:95）且折叠产物凭 payload 正文合法重建，**不改** —— 验证：正锚 `go test ./assembly/ -run TestReplayRefNamesAMissingBody`（新增：索引点名而正文读不到 → 具名错误，不静默少事实）；负锚 错误路径 `ErrSegmentBodyMissing`（memory/ledger/segment_store.go:205）须携带键名，`go test ./assembly/ -run TestReplayRefNamesAMissingBody` 断言 `errors.Is(err, ledger.ErrSegmentBodyMissing)` 且错误串含该键；缺页时 boot MUST NOT 返回 nil error 后投影少条目（违 `session-checkpoint-material` 的 Scenario「恢复与运行期派生一致」）；消费者锚 `go test ./assembly/ -run 'TestDefaultBootRebuildsFromTheAnchor|TestTheRestoredProjectionReadsOnlyPastItsSurvivors'`（anchored_boot_parity_test.go:296/363，正式 boot 的活兜底逐槽路径 run.go:2685-2706 真被走到）。

- [ ] 2.4 **负锚（索引规模随历史回归→红）+ 消费者锚（多代 boot 索引条目有界）**（**状态：2.4 的规模量已实测转绿，但本行点名的锚 `TestBootIndexEntriesBoundedAcrossGenerations` 未新建，故不勾选**）：P4-cont 实测 `TestSymptomAnchorBootCostFlatAcrossGenerations` 的 `indexEntries` 由 [0 43 94 145 196]（退位关闭）转 [0 2 2 2 2]（退位开启），红→绿两次输出已入 ORCHESTRATION 台账 R6；`journalBytes`/`durableLoadCount` 两量仍线性，阻塞项与归属见 06·6.5 状态。原验收面：完整回退（`journalReplay` 起始偏移回 0 + `Load` 回全量前缀扫）必红，两次证据入 run-notes 供一级 F3 —— 验证：正锚 `go test ./assembly/ -run TestBootIndexEntriesBoundedAcrossGenerations`（新增：末代常驻索引条目 ≤ 存活键集 + 尾，不随代数线性增长；分母断言链确实增长，沿用 checkpoint_test.go:1256-1259 口径）；负锚 把 2.1/2.2 回退为全量重建后同一命令**必红**（`go test ./assembly/ -run TestBootIndexEntriesBoundedAcrossGenerations` exit≠0），红项归属须先在干净基线复验同用例；消费者锚 `go test ./assembly/ -run 'TestSymptomAnchorBootCostFlatAcrossGenerations|TestAnchoredSurvivorsFollowTheResidentBound|TestBootRefusesAnUnreadableCheckpoint'`（symptom_anchor_recovery_test.go S2 症状锚 / anchored_boot_parity_test.go:64 / checkpoint_test.go:432；既有 checkpoint_test.go:1231 Session 锚保留不动）＋ 04 的 S2 四量锚中 `journalBytes`/`indexEntries` 两量由本域转绿（探针签名归 04，本域只消费）＋ `go test ./memory/... ./assembly/... ./tests/integration/history/...` 全绿。

## 收尾（合单内，一级 F 直管）

- [ ] 2.5 【待复验】 **级联检查与全量 grep**：`Load`/`openJournalLog`/`EachIndexNewestFirst`/`EventCount`/`replayRef` 的全部调用方逐一审视（design.md §八 触碰集）；`grep -rn "journalReplay\|commitOrderOf\|ErrSegmentBodyMissing" --include=*.go` 枚举消费者零漏；保序两钉 + 计数断言零改实证；build/test 与 commit **永不串联**，验证独立语句分号分隔。
## 实施记录（W3 05+06+07 合单，2026-11-15）

- **2.1/2.2 落地形状**：索引切片落 `memory/ledger/journal.index`（帧形 `journalIndexCut`：Version/Offset/Size/Order/Index），由 `rewrite` 在**排他安静点**（退位重写）写；`openJournalLog` 读切面 seeding `index`/`order`，`journalReplay` 起始偏移由 0 改为切面偏移，`openBytes` 量改成 `size - cut.Offset`。切面在日志替换**前**删除、替换**后**写入，中途崩溃留下「有日志无切面」→ 整份读（安全回退）。`journal.index.tmp` + rename 原子落盘。
- **偏离（记入战报 ④）**：`FileSegmentStore.Load` 的 idx/meta 前缀读**未改**（2.1 原口径）。其实测已平坦（`indexEntries=[0 2 2 2 2]`）：kv 索引由切面 placement 提供，`Load` 的前缀读规模随存活键集而非历史。改造 `Load` 会动 `commitOrderOf` 保序与 6 处计数断言面，收益为零残余风险为正，故按「边界画在语义单元上」停在引擎层。
- **2.4 未勾**：点名的 `TestBootIndexEntriesBoundedAcrossGenerations` 未新建；规模量已由 S2 实测转绿（`journalBytes=[0 110 110 110 110] indexEntries=[0 2 2 2 2]`，分母 chain indexed 逐代增长）。新立的三锚覆盖切面本身：`TestOpenReplaysOnlyPastTheIndexCut`（正）/`TestOpenRefusesToTrustAnIndexCutItCannotPlace`（负：切面越过尾、坏切面、异版本）/`TestOpenReadsTheLogWholeBesideNoIndexCut`（负：无切面整份读 + TailKeys==nil）。

## 本轮重整叶（06.R1 / 06.R2）

> **验收勘误（2026-11-18，R262）**：`TestCostPhasesAreMeasuredWithoutOverlap` 与其 `g3*` 辅助量已删除。旧名选择器会零匹配，不再是验证命令；历史 06.R1 勾选与原始读数保留，但其中“不重叠已实证”的结论撤销。当前计数器与夹具有效性仍有其他测试，不能据此证明分阶段成本完整归因；该工作继续由 **09.R2** 承担，完成前不得核销 G3 的分阶段验收。

> 叶级依赖：**06.R1 → 06.R2**；06.R2 与 **11.R2** 同触 `memory/ledger/segment_store.go` → 同文件簇串行或合单；**08.R1 依赖 06.R2**。旧 2.1–2.5 不作前置（其状态待复验），本域当前完成面只看 R1/R2。
> 三锚纪律同前：正锚点名测试＋可复跑命令（名与 `-run` 逐字符一致）；负锚具名错误路径；消费者锚正式 boot 入口（`Assemble`：assembly/run.go:491→`assemble`:518→`ledger.OpenTier`:622→`restoreCheckpointState`）。**新增测试一律标「拟新增，尚未建立」。**

- [x] 06.R1 **成本与引擎能力设计门（G3，只调查/原型；不通过不得派 06.R2）**：交付＝①分阶段计量面（[design.md](design.md) §十一·一 七行表，含新增必记量 `prefixPairsRead`：`{pid}:idx:`／`{pid}:meta:` 实读键数）；②五问结论（切面由谁维护／索引前缀读取舍／哪些操作不承诺常数／格式与迁移边界／冷热只改布局）；③引擎实现候选剖面（目录保留 / 增量维护 / 按需定位），逐候选给 boot 量、提交代价与点读代价读数；④「具名操作成本表」草案（每项声明成本随历史／存活键集／尾／常数哪一项增长）。 —— 验证：原正锚 `TestCostPhasesAreMeasuredWithoutOverlap` **已删除，不再执行**（该测试的无重叠判据为恒等式；独立阶段计量改由未完成的 09.R2 补足，当前其他计数锚不等价承接此承诺）＋ `go test ./memory/ledger/ -run '^TestOpenWithoutAnIndexCutPaysTheWholeLog$' -count=1`（**拟新增，尚未建立**：无切面时读入字节＝整份日志，且该事实被具名报告而非算作有界）；负锚 `go test ./assembly/ -run '^TestCostAttributionSeparatesBootFromRelocation$' -count=1`（**拟新增，尚未建立**：同一夹具退位开／关两态下分列 boot 量与冷读量；若把退位带来的字节减少记进索引账，本用例须红）；消费者锚 既有锚复跑并录两态读数 `go test ./assembly/ -run '^TestSymptomAnchorBootCostFlatAcrossGenerations$' -count=1` ＋ `go test ./tests/integration/history/ -run '^TestJournalBytesBoundedByLiveSet$' -count=1` ＋ `go build ./...`。
  - **文件白名单（R1 只读；允许追加用例与证据）**：`assembly/symptom_anchor_recovery_test.go`、`memory/ledger/journal_index_cut_test.go`、`memory/ledger/segment_store_test.go`（仅追加用例）、`openspec/changes/runtime-recovery-ideal/changes/06-bounded-open-index/{proposal,design,tasks}.md`（证据与结论回写）。**禁改生产代码**（`memory/ledger/journal.go`、`memory/ledger/segment_store.go`、`assembly/run.go` 在 R1 内只读）。
  - **前置**：09.R1失败清单固定。只读取证可与11.R1并行；两叶都可能写segment_store_test.go且读取ledger构建闭包，新增用例/采样须串行或合单，未有实测闭包证据不声明可并发。

- [x] 06.R2 **按引擎职责实施索引与冷热（依 06.R1 的选择；不承诺所有操作 O(1)）**：交付＝①索引维护下沉为引擎职责——切面的创建/更新/失效/重建在 `memory/ledger` 内，**SHALL NOT 依赖退位重写作为唯一触发**（today 唯一落盘点 `journalLog.rewrite` journal.go:750-767）；②按 G3 选定的取舍处理 `FileSegmentStore.Load`（segment_store.go:346-401）的前缀读（改增量 / 保留整表并如实计量，二选一以 G3 结论为准）；③交付「具名操作成本表」：逐项声明成本随何增长，未列入者视为未承诺；④冷热界限只改布局，不删任何事实。 —— 验证：正锚 `go test ./memory/ledger/ -run '^TestOpenCostIsBoundedByTheCutItKeeps$' -count=1`（**已建立**：多代存储重开档，读入字节＝切面＋尾，且切面在**未开启退位**的路径上也被维护或具名声明退化）＋ `go test ./memory/ledger/ -run '^TestIndexMaintenanceRunsOutsideTheRelocation$' -count=1`（**已建立**：不开退位时切面存在且可判读，或引擎具名报告「本档不维护切面」）；负锚 `go test ./memory/ledger/ -run '^TestOpenReadsTheLogWholeBesideNoIndexCut$' -count=1`（既有，`journal_index_cut_test.go`，保持绿：无切面时整份读并具名）+ `go test ./memory/ledger/ -run '^TestDerivedIndexNeverSkipsAKeyItPublished$' -count=1`（**已建立**：派生索引条目少于发布条目即具名失败，沿用 `commitOrderOf` 口径 segment_store.go:408-410）；消费者锚 `go test ./assembly/ -run 'TestBootRestoresTheCheckpointOnce|TestAnchoredBootMatchesFullBoot|TestSymptomAnchorBootCostFlatAcrossGenerations' -count=1` ＋ `go test ./tests/integration/history/ -run '^TestStorageCapabilities$' -count=1`（计数面零改）＋ `go test ./memory/ledger/ -count=1`。
  - **文件白名单（精确）**：`memory/ledger/journal.go`（切面维护点与偏移）、`memory/ledger/journal_kv.go`（锁与安静点）、`memory/ledger/segment_store.go`（`Load` 按 G3 取舍）、`memory/ledger/config.go`（仅切面/布局相关注释与既有键，不新增部署可变开关）、`memory/ledger/journal_index_cut_test.go`、`memory/ledger/segment_store_test.go`。
  - **前置**：06.R1 通过（G3 七问/五问结论＋剖面读数）。**冲突**：与 11.R2 同触 `memory/ledger/segment_store.go` → 串行或合单，不得并发。
  - **上报项**：若 G3 结论为「保留整表前缀读」，则本域不得宣称索引规模有界于存活键集以外的更强性质，须在 spec 的具名操作表里写明；若结论为「切面仅在退位时维护」，则须把「无退位的部署永远整份读」写成已声明的退化路径并让 09 的 S2 归因据此分栏。

- [x] 06.R3 **点查询与尾查询不得随历史增长（N24，编排者 R72 追路径所得；无此叶则「恢复成本有界」是假终点）**：交付＝①**给走查 API 一个真正的早停出口**——今 `EachIndexNewestFirst` 公开回调只返回 `error`（segment_store.go:1527-1534），包装层恒「继续」，唯一停法是返回哨兵 `errStopIndexWalk` 而只有 `keptHotKeys` 用了它；**只改引擎不改出口，消费者仍会写成全走查**；②`GetParent`（assembly/read.go:280）改从该键位置反向走到第一条同链条目即停，其回退 `parentFromBodies`（read.go:317）**不得再收集全分区 refs 并逐条读正文**；③`eachFactPastCut`（assembly/checkpoint.go:1126）从水位起走、到尾即停（**它只想要尾却走查全史再过滤**）；④`newestSessionCheckpointRef`(:970)、`restoreSessionCheckpoints`(:1006)、`seedAnchoredReplay`(assembly/run.go:2617) 走到最新框架检查点即停；⑤`QueryEvents`（read.go:114）按命名空间粒度收窄，**若不能收窄则在成本表如实声明 O(匹配集)**，不得默认算作已解；⑥具名操作成本表**把「本征 O(历史)」与「可修项」分列**——按序导出全链、跨分区全量查询属本征；座位损坏重推与无切面整读属**一次性具名降级**，不是常态。 —— 验证：正锚 `go test ./assembly/ -run '^TestParentLookupCostsItsOwnChainNotTheHistory$' -count=1`（**拟新增，尚未建立**：同一链形态在 400/1600/6400 三深度下 `GetParent` 成本比 ≤4×，且**须有随历史增长的控制案证明该界可失败**）＋ `go test ./assembly/ -run '^TestTailWalkStopsAtTheWatermarkAndTheTailEnd$' -count=1`（**拟新增，尚未建立**：`eachFactPastCut` 访问条目数＝尾条目数，非全史条目数）；负锚 `go test ./assembly/ -run '^TestAPointQueryThatWalksTheWholeHistoryIsRed$' -count=1`（**拟新增，尚未建立**：把早停出口去掉即红，且失败文案点名被走查的条目数与历史深度）＋ `go test ./tool/recall/ -run '^TestTraceOfDepthDPaysDLocalLookupsNotDHistoryWalks$' -count=1`（**拟新增，尚未建立**：深度 D 的 trace 成本随 D 线性而**不随历史深度**增长）；消费者锚 **走真实工具入口**：`go test ./tests/integration/capabilities/ -run '^TestRecallToolSurface$' -count=1` ＋ `go test ./assembly/ -run 'TestBootRestoresTheCheckpointOnce|TestAnchoredBootMatchesFullBoot|TestSymptomAnchorBootCostFlatAcrossGenerations' -count=1` ＋ `go test ./tests/... -count=1`（**全树，不得抽样**）。
  - **文件白名单（精确）**：`memory/ledger/segment_store.go`（走查 API 出口）、`assembly/read.go`、`assembly/checkpoint.go`、`assembly/run.go`、`tool/recall/trace.go`、`tool/recall/get.go`（仅在其调用形态须随出口调整时）、新增测试 `assembly/point_query_cost_test.go`、`tool/recall/trace_cost_test.go`。
  - **前置**：S1c 载体定形并落地 ＋ S2 store 去物化（可寻址 order 可用）。**冲突**：与 S1c/S2/S3 同触 `memory/ledger/segment_store.go` → **串行**，不得并发。
  - **上报项**：`QueryEvents` 若无法收窄须具名上报，不得静默计入已解；`recall_trace` 的 D 次局部查找若局部密度本身随历史增长（多会话交错），须量出该密度并如实声明其轴，不得宣称 O(1)。

- [x] 06.R4 **S2：store 去物化 ＋ 生产接线 ＋ 托管 boot 可观测（A3/A6 回写；承载 S0 冻结形状与 N6 所需日志行）**：交付＝①按 **S0 冻结形状**（§十九）实施——`count`/`commitSeq` 搭**链内座位键** `store:seat`，**与事件族同帧写入**、开档**一次点读**；`windows` **不新增载体**、按需读既有 `{pid}:meta:{window}` 链键、**常驻表删除**；座位不可用（缺失/损坏/`commitSeq<count`）⇒ 统一从 idx 条目重推并**仅在持写权时**以一帧重述；②**启用 S1c 生产接线**（`statesAddressMaintenance(true)`）——S1c 已在隔离树证明接线后四枚装配锚 PASS（1.61/13.47/14.08/14.27s），**但须在主树复跑全树确认，不得以隔离树代替**；③**新增一条「托管 boot 按身份读回什么」的日志行**（N6 所需，13.R2 的 attestation 重写依赖它）——**其消费者是 CI attestation，独立于它存在，故不违 N2 自证禁令**；④`namespaces()` 语义按编排者裁定改为**只答持有键的命名空间**（今日答「曾写过」，属 tombstone 留下的空命名空间＝历史残留），**但须先 grep 枚举全部消费者证明无人依赖旧语义**，有人依赖则具名上报。 —— 验证：正锚 `go test ./memory/ledger/ -run '^TestStoreSeatIsReadWithOnePointReadAtOpen$' -count=1`（**拟新增，尚未建立**：多深度开档 `storeLoad` 平坦，**须配 materializingLoad 增长控制案**，S0 实测 223ms→782ms→3.12s）＋ `go test ./assembly/ -run 'TestBootRestoresTheCheckpointOnce|TestAnchoredBootMatchesFullBoot' -count=1`；负锚 `go test ./memory/ledger/ -run '^TestACorruptStoreSeatRederivesFromTheChain$' -count=1`（**拟新增**：座位损坏⇒重推且结果等于今日 Load，**不得静默答零**）＋ 座位 `commitSeq<count` 同须具名重推；消费者锚 **`go test ./tests/... -count=1` 全树** ＋ `go test ./assembly/ -count=1` ＋ `go test ./runtime/... ./features/... ./tool/... -count=1` ＋ 两注释门 0 findings。**提交成本不得回归**：须给对今日基线（4.144ms，S1c 同机复现 4.887/4.167/4.058ms）的比值，S1c 已达 0.98×/1.09×/1.02×，接线后须复量。
  - **文件白名单（精确）**：`memory/ledger/segment_store.go`、`memory/ledger/config.go`（接线开关与能力注释，**不新增部署可变开关**）、`memory/ledger/atomic_store.go`（仅接线点）、`memory/ledger/segment_store_test.go`、`assembly/run.go` 或 `assembly/checkpoint.go`（仅新增该日志行）、本域四件套。
  - **前置**：**09.R4 红证据已取得** ＋ S1c 已落地。**冲突**：与 09.R4／05.R3／assembly 锚合批单同触 `assembly/` → **串行**；与 08.R3 同触 `segment_store.go` → **06.R4 必须先于 08.R3**。
  - **上报项**：`namespaces()` 若有消费者依赖「曾写过」语义，须具名上报待裁，不得静默改语义；生长提交非 O(触碰键)（S1c 实测一次步长加宽 18.067ms，摊还常数）须在成本表如实声明。

> **06.R2 勘误指针（A4，5.8）**：06.R2 已勾且**保持勾选**——它确实交付过（切面维护下沉、`Load` 取舍、13 行成本表、冷热只改布局）。但其交付**已被 D6.13 反转取代**：切面机制将随 08.R4 删除、冷热分层将随 08.R3 消失。**取代后的交付由 06.R4（S2）与 06.R3（N24）承载**。按 5.8「历史文件不可变，推翻结论记录在后段汇总文件，双方证据均保留」，**不取消勾选、不改写其原文**，仅在此立指针。其原「具名操作成本表」中 5 行冷/退位行随之作废，新表由 06.R4 重出。

- [x] 06.R5 **屏障预算须覆盖检查点与关闭路径（R78 查出的真回归；症状锚转绿的必要条件）**（**2026-11-18 按存活路径重钉屏障判据后核销**：正锚五跑全绿、绝对读数低于退位世代两端；判据重钉与突变凭据见 [design.md](design.md) §二十八）【R78 回写】：**为什么必须做**——06.R4 收口时同负载三方对照证明：本树（接线＋座位）首/末窗 **223.93／379.96ms**，改动前形状 **139.59／264.16ms**，接线**无**座位 **212.98／360.01ms** ⇒ **座位≈0 成本，成本在接线**，落在 **`frame`×2.00 与 `closing`×2.19**，而 `commits` 无回归。**故 S1c 的 0.98–1.09× 只在「一次提交」成立，不延伸到「一代」；一代绝对成本 ~1.53–1.60×**。**比值 1.89→1.70 不算改善。****删退位不会消掉此回归**——frame/closing 在删退位后仍在，故本叶必须在 08.R3 之前。交付＝①**直接量** frame/closing 两阶段的屏障道数与耗时（R78 的归因是按 fsync≈5ms **推断**，须确证，不得沿用推断当结论）；②把 S1c 的「按载体字节预算屏障」规则**扩展到检查点落盘与关闭路径**（今只覆盖提交路径）；③若确证不是屏障道数而是别的（如地址维护在 frame/closing 重复全量重述），按实测重设计，**不得先认定机理再凑读数**。 —— 验证（**2026-11-18 按存活路径重钉**）：正锚 `go test ./assembly/ -run '^TestSymptomAnchorPerGenerationCostFlatOverALongRun$' -count=1 -v` —— 五跑 **exit 0**（24.96–26.48s），首窗 75.00–86.77ms／末窗 85.82–94.93ms（跑间中位 81.85／89.04），逐阶段 frame 4.97–5.11→3.82–5.08ms、closing 13.15–15.88→12.12–13.99ms **全平**；§二十四 记的「改前带 132–140／243–264」属**退位世代**，08.R3 删除退位后无对应机制，现读数**低于该带两端**（这是绝对读数比较，不是比值改善）；屏障判据按存活路径重出＝**06.R5.a 两锚**（`go test ./memory/ledger -run '^(TestCommitBarrierMeasurementNamesItsPhase|TestCloseBarrierFailureStaysVisible)$' -count=1 -v` exit 0：提交相位命名 `log`／`log+index`、空账关档 nil 且两索引文件屏障各具名）＋ **overlay 探针逐相位计数**（`go test -overlay … ./assembly -run '^TestProbeR5CloseFrameBarriers$'` exit 0：frame **1 道**＝log、closing **3 道**＝log+order+slots、`reset=grow=0` 逐代恒定，即无整表重述）；**「每载体一道」的增长控制案**（界可失败）＝把字节预算判定退回（`-overlay` 去 `flush` 的 `journalIndexSyncBytes`/`journalLogSyncBytes` 判定）⇒ 同一 ledger 命令 **exit 1**、红文 `the commit covered the "log+index" barrier, want the log's alone: the barrier over the index files waits for its budget`，同突变下探针 frame 道数 **1→3**、耗时 5.0→21.0–35.1ms；复原同命令 **exit 0**；负锚即上述突变，失败文案点名 frame 的载体屏障道数；消费者锚 `go test ./assembly/ -count=1`（**exit 0**，317.5s，零具名红）＋ `go test ./memory/ledger/ -count=1`（**exit 0**，80.1s）。
  - **文件白名单（精确）**：`memory/ledger/journal.go`、`memory/ledger/journal_kv.go`、`memory/ledger/segment_store.go`（仅屏障预算相关）、`memory/ledger/journal_test.go`、`memory/ledger/segment_store_test.go`、本域四件套。
  - **前置**：06.R4 已勾。**冲突**：与 08.R3 同触 `memory/ledger` → **本叶必须先于 08.R3**。**与 05.R3 可并行**（05.R3 触 assembly/runtime-state，本叶触 memory/ledger；但两者验证都跑 assembly，故**验证阶段须串行**）。
  - **上报项**：若绝对成本回不到改动前量级，须具名上报差额与归因，**不得以比值改善冒充达标**；若机理与假设不符，按实测重设计并回写 design。

## 06.R5 执行记录（06.R5 未勾，2026-11-16，主树工作树计时）

**判决：交付①②③完成，但验收判据只兑现一半 ⇒ 本叶不勾。** 全文证据见 [design.md](design.md) §二十四。

- **①直接测量（成立）**：overlay 探针逐相位计数。改前 frame **7 道屏障**（log=2 order=1 slots=1 rewrite=1 table=2）／57.7–76.7ms、closing **9 道**（log=2 order=2 slots=2 rewrite=1 table=2）／67.0–89.1ms；改前形状（接线关）3／3 道。R78 的 ×2.00／×2.19 **归因成立但机理要改写**：不是「每载体一道」，而是**每次日志重写都走 `rebuild()`＝整表重述＋全量回放新日志＋长任务键触发的步长重放**。
- **②落地（成立）**：`rewriteInto` 答出它写下的语句 → `reopened(statements)` → `restate`，重述**零回放**、一次定宽定容（与增量增长同形）、**不取自己的屏障**（交给预算）；`flush(force)` 在 `staged==0` 时不再为零字节取屏障对。改后 frame **3 道**（回到改前形状）、closing **5 道**。
- **③机理假设部分成立、部分被实测改写**：屏障道数确是主项，但**剩下 2 道是「整份重述必付的一次屏障对」**（两载体两份 fsync，无法合并）；三案（不写表头／原地补丁／开档从 cut 重述）逐条排除，见 §二十四·四。
- **⚠️ 判据未兑现（跑次间 3%–23% 摆动，按不达标处置）**：末窗 total **271.05／304.79／323.87ms**（另一次 584.76 为负载尖峰）对改前 **262.36ms** ＝ **+3.3%／+16.2%／+23.5%**；改前同形状为 **356.09**，故相对改前 −9%…−24%。首窗 108.92／174.64／177.94 对 147.80（区间已覆盖，方差 > 差额）。frame 已回到改前量级（68.35–85.07 对 72.57），**差额全在 closing**（95.62–116.80 对 77.89），归因＝存活键集重述工作 × 2 次重写/代。**不以比值改善冒充达标**：改后末/首 = 1.55／1.82／2.80，改前 = 1.54／1.99。
- **锚**：新锚 `TestCheckpointAndClosePayOneIndexBarrier` 正锚三深度绿（`-count=10` ok）、增长控制以「one barrier per index file」具名拒绝、负锚（overlay 退回重写自取强制屏障）exit 1 且原文点名 `read 1530 byte(s) of the log it wrote, want none`。
  - **勘误（08.R3 执行）**：该锚已随退位机制删除——其夹具的搬迁步是 `log.relocate`，生产面删 `relocate` 后锚钉空机制。同批删除其 helper `judgeOneIndexBarrier`／`oneIndexBarrierReading`／`controlReadingLine`。本行正锚因此失效，**06.R5 的屏障面须按存活路径（frame／closing）重出判据**，不得留一枚钉着已删路径的绿锚。
- **套件**：`go build ./...` 0；`go vet ./memory/ledger/` 0；`./memory/ledger/` **0（37.1s，81 顶层＝基线 80＋新锚）**；两注释门 0／0；`./assembly/` 见战报。
- **差额的唯一降法不在本叶白名单内**：要么少一次重写（08 域退位删除），要么少一个载体（§二十一 冻结载体表）。**故本叶的剩余项须由编排者裁定归属，不由本叶自行扩白名单。**

## 06.R5 接续记录（08.R3 退位删除后重测；06.R5 已勾，2026-11-18）

**判决：判据兑现，本叶核销。** 上面 2026-11-16 记录原文保留（历史不可变），其「判据只兑现一半」的时点在 08.R3 删除退位机制后不再成立。全文证据见 [design.md](design.md) §二十八。

- **① 正锚重测（转绿，五跑）**：252 代／窗口 5／界 13 tenths 未改，`-count=1 -v` 五跑 **exit 0**（24.96／25.36／25.84／26.48／26.09s）。首窗 total **75.00–86.77ms**（中位 81.85）、末窗 **85.82–94.93ms**（中位 89.04），末/首 1.026–1.174；**逐阶段全平**：frame 4.97–5.11→3.82–5.08ms、closing 13.15–15.88→12.12–13.99ms。§二十四 的「改前带 132–140／243–264」是**退位世代**形状（内含 `relocate→rewrite` 重述），退位删除后该带无对应机制；现读数**低于该带两端**，故 §二十四 记的 +3.3%…+23.5% 有害方向消失。**判据是绝对读数与逐阶段窗口，比值只并列。**
- **② 屏障道数重测（08.R3 后，存活路径）**：overlay 计数探针 8 代逐代恒定 —— frame **1 道**（log=1 order=0 slots=0）／4.68–6.09ms；closing **3 道**（log=1 order=1 slots=1）／12.68–15.92ms；`reset=grow=0` 逐代 ⇒ **无重建、无整表重述**。对照 §二十四：改前 frame 7／closing 9、改后 frame 3／closing 5。差额全消，仅剩 closing 的 order+slots 一对（每载体一道的本征价，§二十四·四 三案已排除合并与搬迁）。
- **③ 归属判定**：§二十四 具名的两条降法——**①少一次重写＝08 域退位删除，已在树**（`rewriteInto`／`reopened(`／`restate(`／`keptHotKeys`／`RetireFacts`／`retireSettled`／`.relocate(` 全树零命中）；②少一个载体（§二十一 冻结载体表）**不需动用**。故本叶**无白名单外剩余项**，停报条件不成立。
- **④ 屏障判据按存活路径重出**：复用 06.R5.a 两锚（提交相位命名 `log`／`log+index`＋空账关档 nil、两索引文件屏障各自具名）**不重复造**——检查点落盘与提交共用 `journalKV.Commit`→`journalLog.sync`→`addr.confirm`→`flush` 同一路径（`assembly/checkpoint.go` 无独立屏障点）；补上原锚缺失的**增长控制案**，实测见⑤。
- **⑤ 突变→红→复原→绿（界可失败）**：`-overlay` 去掉 `flush` 的字节预算判定（＝预算规则不再覆盖检查点路径）⇒ `go test -overlay … ./memory/ledger -run '^TestCommitBarrierMeasurementNamesItsPhase$'` **exit 1**，红文 `journal_test.go:3105: the commit covered the "log+index" barrier, want the log's alone: the barrier over the index files waits for its budget`；同突变下探针 frame 道数 **1→3**、耗时 5.0→21.0–35.1ms。复原（不带 overlay）同命令 **exit 0**（`ok … 0.551s`）。
- **⑥ 套件与门**：`go test ./memory/ledger/ -count=1` **exit 0**（80.136s）；`go test ./assembly/ -count=1` **exit 0**（317.545s，**零具名红**，比旧记录的两枚预期红更干净）；`go build ./...` 0；`go vet ./memory/ledger/` 0；`gofmt -l`（本叶白名单五文件）空；`codetools tracked-hygiene` 0。**`comment_policy -no-baseline <产品包>` 今日报 63 finding(s)**（memory 17），全部落在 `kv_original_store_test.go`／`segment_store_test.go` 等其他叶写面——本叶**零 `.go` 写**，基线未动，故为既存门红，逐条未处置。
- **⑦ 本轮零生产改动**：白名单内未发现可消除的屏障浪费（`flush` 是唯一索引屏障点、零字节短路在树、关档不重述）。
- **未做／未证**：`go test ./tests/...` 全树未跑；`-race` 未重跑；探针样本 8 代；掉电窗口未测。

## 06.R1 执行记录（G3，2026-11-16，主树工作树计时）

**判决：PASS**（四项交付齐、五锚全绿）。**「恢复成本随历史有界」未达成**：退位 cut 写实测 O(冷键数)，具名无界，归 06.R2。全文证据见 [design.md](design.md) §十四。

**源树绑定**：主树工作树（HEAD `9547faca` ＋已验证计量修正）。**工作树计时**，与绑 HEAD 的 CI 记录分列。生产代码零写；探针经 `-overlay` 注入 `/tmp/recovery-g3/probes/`。

**四项交付**：①七行计量面＋`prefixPairsRead`（design §十四·一，区间不重叠实证 `engineOpen+rebind==bootWall` 逐字相等，Σ145.665ms≤总1.0013s，其他855.645ms）；②五问结论（§十四·二）；③三候选剖面（§十四·三）；④具名操作成本表草案（§十四·四）。

**五问要点**：①切面今日只在退位落盘，退位关＝无切面＝整份读（`[436 1308 2616]`/`[4 12 24]` 提交，109B/提交），选丙＋甲并具名退化；②前缀读**保留整表**（退位开 `prefixPairsRead=4` 恒定，退位关 `[0 22 52]` 线性），不得称 O(1) 或按需；③不承诺常数的五项已具名（cut 写／全序走查／未折叠长链全扫／Namespaces／无切面开档）；④`journalIndexCutVersion=1`，切面是可丢弃派生载体，本轮不迁移；⑤冷热只改布局✅（`EventCount` 退位后仍 45），冷读缓存首读 3.293ms vs 次读 4.92µs（670×），**MUST NOT 写 O(1)**。

**走查形状裁定（R32 输入）：推荐甲（开局锁内快照合并序）**。缺陷主树复现 10/10 不一致、65 次重复、0 漏。甲增量 4–26µs，而今日 `coldIndex` 已付 2.4–4.7ms（**小 100–1000×**）；甲堆 24,728B vs 乙 49,192B（**省 2.0×**）；快照实测 52 条目/52 身份/0 重复/0 漏，**不需去重集不需新锁**。丙（闸）被否：重入死锁已复现（写者待时新读者阻塞），且走查持闸 18.223ms 与 `RetireFacts` 全程持 `s.mu` 互锁。

**退位 cut 写＝O(冷键数) 确证**：66.0 字节/键；100→6400 键（64×）→ JSON 65×、建表 62×、序列化 61.5×，三者严格线性。6400 键时 cut 428,871B、序列化 1.889ms、合计 3.451ms。**R19 ①最大风险兑现**，06.R2 必须承接。

**N13 区间重表达**：一趟 25.437ms ＝ 开档 6.111ms ＋ 其余 19.289ms（和 25.400≤span）；另 miss 读 2.057ms 在趟外。`ColdReadCost`＝miss∪开档，`ColdWriteCost`＝趟−开档，**非两半**。

**两态读数（布局不冒充机制）**：S2 四量全部借退位——退位关 `journalBytes=[0 134975 327739 559874 831230]`、`indexEntries=[0 43 94 145 196]`、**`durableLoadCount=[0 12 24 36 48]`（新发现：亦随退位翻转）**、`coldRead=[0s×5]` 致冷读守卫先触发故该态无法判定。S1 保留字节退位开平坦 ~71k、退位关线性至 356,558。

**冻结锚（三枚本轮新建，两枚既有复跑）**：

| 锚 | 包 | 状态 | RUN/PASS | exit |
|---|---|---|---|---|
| `TestCostPhasesAreMeasuredWithoutOverlap` | assembly | 本轮新建 | 1/1 | 0 |
| `TestCostAttributionSeparatesBootFromRelocation` | assembly | 本轮新建 | 1/1 | 0 |
| `TestOpenWithoutAnIndexCutPaysTheWholeLog` | memory/ledger | 本轮新建 | 1/1 | 0 |
| `TestSymptomAnchorBootCostFlatAcrossGenerations` | assembly | 既有复跑 | 1/1 | 0 |
| `TestJournalBytesBoundedByLiveSet` | tests/integration/history | 既有复跑 | 1/1 | 0 |

合并实跑 `go test ./assembly/ -run '^TestCostPhasesAreMeasuredWithoutOverlap$|^TestCostAttributionSeparatesBootFromRelocation$|^TestSymptomAnchorBootCostFlatAcrossGenerations$' -count=1 -v` → **exit 0，3 RUN／3 PASS／0 FAIL**。`go build ./...` → **exit 0**。注释门 → **2 findings 均 03.R1 既存（`recovery_judgment_test.go:102/:124`），本域新增 0**。2× 阈值字面未动。

**写集**：`assembly/symptom_anchor_recovery_test.go`（追加两锚＋helper）、`memory/ledger/segment_store_test.go`（追加一锚＋judge）、本域 `design.md`/`tasks.md`。生产代码零写，`state.FactLog.Keys`／`LoadStats.Families`／`enumerateFamiliesLocked` 未复活。

**未量项**：①增量维护候选的尾-only 前缀读是推导非原型；②cut 单段精确归因需 `RelocateReport` 加字段（白名单外）；③`coldIndexLocked` 枚举读未计成本；④失败退位两半都不计。**G1/G2/G4 未由本叶判定，产品不可发布，06.R2 未派。**

## 06.R2 执行记录（2026-11-16，主树工作树）

**判决：交付①②③④齐、四锚全绿、全树绿。但「恢复成本随历史有界」仍**未**达成**：退位 cut 写已收为 O(本趟搬迁键数)，**整趟退位仍 O(冷键数)**（两次冷开档）。成本表与红线—绿对照见 [design.md](design.md) §十五。

**①切面维护面**：**不变**（唯一落盘点仍是退位重写与退位趟的冷切面）。按 G3 的丙＋甲取**具名退化**：`a deployment without retirement always reads whole`，引擎逐字报告 `no index cut beside <dir> and this tier maintains no cut, so the open read the whole log`（`journalLog.cutStatement()` → `IndexCutNotice()` → assembly WARN）。声明由存储层交引擎（`FileSegmentStore.WithRetirement` → `cutMaintenance`），故它是配置事实而非引擎猜测；维护切面但尚未落过盘的首次开档不报警。

**②`Load` 前缀读**：**保留整表读**（G3 乙），成本表标「随存活键集、每键整表读」；**未改一行**（改它会动 `commitOrderOf` 保序与 6 处计数断言面）。

**③具名操作成本表**：design.md §十五·一，13 行逐项给增长轴与实测。**未列入者视为未承诺**。

**④冷热只改布局**：本轮不触碰退位与 `EventCount`；`TestStorageCapabilities` 计数面零改（复跑 exit 0）。

**退位 cut 写有界化（本轮新增的实质改动）**：`journal.index` 改**一行一语句**（JSON Lines）；退位趟的冷切面走 `appendJournalIndexCut`，只陈述**本趟切面之后追加的键**（`journalLog.tail`），开档 `readJournalIndexCut` 逐行归并且 `journalCutOrder` 复算同形序；热日志 `rewrite` 仍整份替换。`journalIndexCutVersion` 仍 1，旧单份切面天然可读，**无迁移**。

**红线—绿对照**（`TestColdCutStatesOnlyTheKeysItsPassMoved`，五趟×40 键，冷史 5,140→25,700 B）：改前每趟写下 `[2190 4334 6514 8694 10874]` → **exit 1**；改后 `[2190 2203 2241 2241 2241]` 平坦 → **exit 0**；整份切面仍 `[2190 4334 6514 8694 10874]`（随冷键数线性，只出现在重建与热 rewrite）。

**冻结锚实跑（逐条原始退出码）**：
| 锚 | 包 | 状态 | RUN/PASS | exit |
|---|---|---|---|---|
| `TestOpenCostIsBoundedByTheCutItKeeps` | memory/ledger | 本轮新建 | 1/1（`log bytes [417 834 1251 1668]`／`replayed [89 89 89 89]`） | 0 |
| `TestIndexMaintenanceRunsOutsideTheRelocation` | memory/ledger | 本轮新建 | 1/1 | 0 |
| `TestColdCutStatesOnlyTheKeysItsPassMoved` | memory/ledger | 本轮新建 | 1/1（含红线 exit 1） | 0 |
| `TestDerivedIndexNeverSkipsAKeyItPublished` | memory/ledger | 本轮新建（负锚：`the engine states the commits of 2 of the 3 index entries it published`） | 1/1 | 0 |
| `TestOpenReadsTheLogWholeBesideNoIndexCut` | memory/ledger | 既有，断言未改 | 1/1（并入 `-race -count=10` 组） | 0 |

**全树实跑**：`go build ./...` 0；`go vet ./memory/ledger/ ./assembly/` 0；`./memory/ledger/` 0（23.411s）；`./assembly/` **0（231.607s）**；`./tests/...` **0（13 包，0 FAIL）**；`./scripts/recover-kv/` 0；`./runtime/... ./features/... ./tool/...` 0（17 包）；本案四锚＋保序三锚＋`TestOpenReadsTheLogWholeBesideNoIndexCut` `-race -count=10` 0（0 DATA RACE）；`TestColdArchiveHoldsTheExclusiveGate`／`TestSecondProcessOnOneRootIsRefusedWriteRights` `-race -count=10` 0；注释门两 scope 均 **0 findings**。

**残留（见 design.md §十五·五）**：整趟退位仍 O(冷键数)（两次冷开档，收它须寻址式冷索引，另一设计门）；`cold_archive_test.go:537/:559` 文案仍写旧形状（不在本叶白名单）。

## S1 执行记录（序列步，非计划叶；不勾任何框）

载体与引擎已落，**共存且树绿**；生产接线**未启用**，因为提交成本实测为今日 5–6 倍并已使四枚装配测试转红（因果以「同命令、关接线重跑」隔离）。全部数据、归因与三选一待裁项见 [design.md](design.md) §十八。本文件既有 2.x 复验状态**不变**：S1 不兑现 2.1/2.2/2.4 的任何一条，S2 仍待 S0 裁定。

## S1b 执行记录（R67 派单的载体形态测量单；非计划叶，不勾任何框）

**交付＝读数与裁定建议，不是代码。** 主树零 `.go` 写；隔离树 `/tmp/recovery-carrier/tree/`（HEAD `9547faca`＋工作树，`journal.go` `601fcc49…`／`journal_kv.go` `b8a0f83a…`／`config.go` `126c6e86…`／`segment_store.go` `9a4ae9f7…`，与 S1／S0 记载逐字节同）。

**四案读数（提交／点读@6,400／枚举@32,000 键）**：S1 一键一文件 **25.1ms（6.1×）／0.53ms 恒定／28.7ms**；选项2 `keys.log` **13.4ms（3.2×）／11.5ms 随距离／36.8ms**；选项3 惰性物化 **9.06ms（2.2×）／物化前 10.8ms、靠 15.6s 首枚举突发换回常数／15.6s 突发**；选项4 只 order **9.05ms（2.2×）／10.4ms 随历史线性／25.9ms**。

**成本分解（实测）**：一次 fsync≈5ms ⇒ 今日 4.1–5.0ms＝一道屏障；**任何自带耐久点的派生载体＝两道屏障＝2.2×**（选项3／4 已删尽每键文件仍停在 2.2×）；S1 的额外 15–21ms/5 键＝每键文件创建；就地 40B 写 5 键 **0.007ms**。故「改载体形状」只能消掉文件创建那一半，消不掉屏障那一半；去掉 order 屏障即 **4.80/4.87ms（1.15×）**。

**裁定**：四案**无一可直接落地**。选项2／3 是同一持有键集的第二份表示（纯度不过，选项2 还须自带「完整行」规则，否则撕裂尾线使开档拒绝）；选项4 的**枚举半成立**（`Keys`/`Namespaces` 可由 order 派生，不劣于目录），**点读半不成立**（删 `idx` ⇒ 点读 10.4ms 且随历史线性，absent 键同一读数 ⇒ S2 的每提交判重点读退化为整份走查）；S1 成本不过。**主树保持 S1 原状（共存、绿、未接线）**。

**建议（须设计门，未实现）**：三职各归一位——order 陈述水位／提交序／全序枚举；**容量有界槽表**做点地址（就地定长记录，无每键文件）；**按载体字节预算屏障**（界＝一个 chunk）替代每提交屏障。推算提交 ≈1.0–1.2×、开档 ≤ root＋一个 chunk、点读一次小读。**推翻 §十七·二 载体表并新增两条承重规则，非实施单可裁**，见 [design.md](design.md) §二十·七。

**验证实跑（隔离树，形状由 env 选）**：四形状各跑同一套件 **73 顶层＋37 子、0 FAIL、exit 0**；`-race -count=10`（选项4，八锚＋两枚 flock 锚）**100 PASS／0 DATA RACE**；撕裂＋孤儿语句注入四形状皆绿（`keys.log` 须补「完整行」规则后转绿，首轮红原文 `keys.log states no key line: unexpected end of JSON input`）。**未跑**：`./assembly/`、`./tests/...`（无接线即无消费面；且接线点 `atomic_store.go`／`segment_store.go` 不在本单白名单）。**未测**：掉电、槽表原型。

## S1c 执行记录（载体形态冻结单：槽表＋字节预算屏障；非计划叶，不勾任何框）

**交付＝一个形状、量它、冻它、落地（绿、未接线）。** 主树写面：`journal.go`（`idx/` 一键一文件与 `root.json` 换成 `order.log`＋`slots` 一个文件的定长槽表；新增记录级 crc、水位守卫、两条生长轴、预算屏障）、`config.go`（收回 `rejectLegacyShardLayout` 对自有 `idx/` 的唯一例外）、`journal_test.go`（两锚随载体改名、崩溃锚夹具放宽、新增三锚）。`atomic_store.go`／`segment_store.go` **未触**。全部数据、证明与偏离见 [design.md](design.md) §二十一。

**冻结形状**：`journal.log` 唯一真相（未动）｜`order.log` 水位＋提交序＋全序枚举｜`slots` 64B 表头＋N×96B 槽记录＝点地址＋持有键集＋命名空间｜屏障按**载体字节**（64 KiB＝一个 chunk）而非每提交一道。

**成本实测（同机，400／1,600／6,400）**：提交 **median 0.98×／1.09×／1.02×**、mean 1.00×／1.06×／1.16×（后者含一次表翻倍，最慢 87.0ms），对照 S1 **6.1×**、四案下沿 **2.2–3.2×**；点读 newest/mid/oldest/absent **0.0018–0.0071ms 四点平**（absent 与 present 同阶），控制 order 走查 absent **0.011→13.577ms**；开档地址层 **64B 恒定**，控制 **260,000→4,160,000B**；枚举 keys **0.65→6.18ms**、newest-first **1.95→14.12ms**（**O(历史)，不得称有界**）。

**崩溃一致性**：新写序五注入点落四可区分态，五子测试全绿（全有或全无＋水位收敛）。**撕裂槽记录**＝记录级 crc32c ＋**整表从 order 重述**（键不可知，无法只修一槽）；**孤儿槽记录**＝`span` 水位守卫，点读／列举／命名空间三处同判。与 `order.log`（**逐行语句校验和，失败即从日志重述**）、`keys.log`（须自写「完整行」）**三个载体的完整性规则各不相同，已实测分列**。掉电未测。

**S1 八变异复跑**：八枚**全部转红**（含 `1 of 5 key(s) landed after the stop`、`the open of the cut log = <nil>`、`the walk read 2 value(s), want none`），复原后 `^TestAddressableIndex` 全绿。两锚随载体改名、崩溃锚夹具放宽（**断言一字未改**）；M4 的首个红点位移到「order 未截回水位」，已如实登记。

**接线判定（只报不落）**：隔离树接上 writer 面（`config.go` 一处）后四枚装配锚 **1.61／13.47／14.08／14.27s 全绿**（S1 为 18.89s FAIL／120.49s／120.22s／90.47s）；全量 `./assembly/` 246.0s 唯一红 `TestGovernanceSliceWiredInProduction`，**接线关同树同命令仍红** ⇒ 并发叶归属；`./tests/...` 唯一红因 rsync 排除 `examples/`（含真实用户数据根，按令不读不复制）。**主树未落接线：接生产提交面属共享面，待裁。**

**实跑凭据（主树）**：`go build ./...` **0**；`go vet ./memory/ledger/` **0**；`go test ./memory/ledger/ -count=1` **0（42.752s），76 顶层＋53 子，0 FAIL**；`-race -count=10`（11 锚＋3 枚 flock 锚）**0（105.725s），0 DATA RACE**；两注释门 **0／0**（`comment_policy . examples/wechat-bot examples/cli-host` 与 `-no-baseline assembly api runtime memory features tool internal cli`）。

**未做／未证**：生产接线（待裁）；读路径切换（S2）；掉电；主树 `./assembly/`／`./tests/...`（无接线即无消费面）；`namespaces()` 只答持有键的命名空间（与今日「曾写过」有位移，**登记未裁**）；生长提交单次非 O(触碰键)（实测 18.067ms）。**G2/G4 未判定，反转未完成，产品不可发布。**

## 06.R4 执行记录（2026-11-16，主树；**2026-11-16 06.R4b 补记：框已勾，见下**）

**判决：交付②③④ 达标，交付①只达「座位与窗口去物化」；冻结验收里的「storeLoad 平坦」未兑现，故不勾。** 数据与论证全文见 [design.md](design.md) §二十二。

**①座位（S0 冻结形状）**：`store:seat = {count,commitSeq}` 与事件族**同帧**落盘（`planEventLocked` 的同一个 Batch），开档 **1 次单键点读**；`windows` 常驻表**删除**，`nextWindowMeta` 按 `{pid}:meta:{window}` 点读一次/提交；座位不可用四态（缺失／解不出／`count<0`／`commitSeq<count`）统一落到 `count=len(published)+retired`、`commitSeq=max(count,maxPublishedCommit)`；**开档零写入**，重述由下一次提交的那一帧承担（只有持写权者能提交）。退位趟不搬座位（四族判定都不认它），且不改 `count`／`commitSeq`，故 `count=published+retired` 在搬迁前后守恒。

**⚠️ 未兑现项（`s.index`／`s.order` 未删，三条硬约束，均实证）**：①按身份点读**缺分区**——分区只在常驻索引里（`PartitionIDFromName`），S0 原型从无身份读，冻结形状未覆盖；②`IndexEntries()` 的消费者在 `assembly/symptom_anchor_recovery_test.go` 并有「零即失败」守卫，删表即令该锚转红，而该文件不在本叶写集、其重表达归 09（S5）；③引擎开档仍整份重放，面在 `journal.go`（读路径切换），不在写集。**故 store 开档实测仍随历史增长：845µs→2.984ms→10.69ms（400/1600/6400），座位自身恒 1 次单键读。§十九·三 的 storeLoad 平坦未复现，不得声称复现。**

**②接线已启用（主树）**：`OpenAtomicStore` 的 journal 面按 `cfg.WriteRights` 调 `statesAddressMaintenance`；读视图声明 false（不维护载体，读在写者旁）。

**③N6 日志行（逐字）**：`tagent: boot seated the task account tasks=%d tail=%s reads=%d keys=%d`（`assembly/checkpoint.go` 的 `stateSeatedAccount`）。陈述按身份读回的账户与读量，`tail=none` 区分整族回退读。

**④`namespaces()` 消费者枚举**：五处生产消费者全为**分区发现**或**迁移副本比对**，**无一处依赖「曾写过」语义**（`memKV`／`rustVikingKV` 本就答持有键）；但改动点 `journalLog.namespaces()` 与切换点 `journalKV.Namespaces` **均在写集外**，故只交枚举与裁定，未动语义。

**三锚（新建，均绿；四条变异各自红）**：`TestStoreSeatIsReadWithOnePointReadAtOpen`（含两条控制）、`TestACorruptStoreSeatRederivesFromTheChain`、`TestASeatBelowItsCountRederivesTheRank`。变异：座位恒不可用／去 `commitSeq<count`／坏座位答零／座位读两次 → 四者各自具名转红。

**提交成本（同机 A/B 三形状）**：A（无座位无接线）median 4.056–4.956／4.082–4.711／4.456–4.757ms；B（只接线）4.922／4.853／4.927；C（现状）4.87–5.00／4.85–4.92／4.22–4.93ms。**座位自身≈0**（B≈C），接线的那一份≈1.1×；对派单基线 4.144ms 之比 ≈**1.18×**。本机最慢提交 115–136ms（并发负载），只报中位数。

**写集**：`memory/ledger/segment_store.go`、`memory/ledger/config.go`（接线一处＋字段注释）、`memory/ledger/segment_store_test.go`、`memory/ledger/cold_archive_test.go`（一行：热链键数 5→6）、`memory/ledger/fact_commit_batch_test.go`（一行：提交键集加 `store:seat`）、`assembly/checkpoint.go`（日志行）、本域 design/tasks。`journal.go`／`journal_kv.go`／`atomic_store.go` 未触。

## 06.R4b 收尾记录（2026-11-16，主树；06.R4 的四项未了）＝R77 派单

**判决：派单四项全部完成；`06.R4` 框据此勾。但第④项判出「是改动不是负载」的一处成本回归，明列于此不得视为改善。** 全文见 [design.md](design.md) §二十三。

**①（交付④）`namespaces()` 已改为只答持有键**：`journalLog.namespaces()` 从 `namespaceSeen`（曾写过集）改为按 `l.index` 的持有键（跳过 tombstone）当场求集合与排序；`namespaceSeen`／`namespaceOrder`／`noteNamespace` 三结构删除，**不保排序缓存**（集合可因 tombstone 缩小，且无调用方在一次写入序列里重复枚举）。消费者枚举先行：`Load`／`coldEventLocked`／`coldIndexLocked`／G3 探针／`migrate_test`／装饰器夹具**全为分区发现或迁移副本比对**，**无一处依赖「曾写过」**；`memKV`／`rustVikingKV` 本就答持有键；`scripts/recover-kv` 不读 `Namespaces`（grep 0）。旧语义的残留点确证为退位重写（`rewrite` 重建 `l.index` 却从不重置 `namespaceSeen`）。新锚 `TestJournalNamespacesAnswerOnlyTheHeldKeys`（tombstone 后同视图与重开档两向断言），去掉 `value.deleted` 跳过即红（`= [0 ref], want 0`）。

**②（提交成本＋零新增耐久点）**：三形状同机 A/B/C，200 次提交/深度——C／A＝**0.99×／1.02×／1.05×**（400／1600／6400；对冻结基线 4.144ms 为 1.17／1.01／1.03×）。**`Sync()` 计数**：A 200/200＝1.0000、B 205–207、C **207/200＝1.0350**；`append()` 三形状皆 **200＝1.00/提交**。⇒ 座位与事件族同帧，**零新增耐久点**；接线那份是载体字节预算的 0.025–0.035 道/提交。

**③（增长控制案）**：座位锚新增「删掉座位后开档（重推）」控制 **291.8µs→823µs→2.827ms**（16× 深度＝9.7×，界≥2×）；坏座位锚新增「答案恒等于链、读到的条目 50→200→800」控制。变异两枚各自具名转红：座位读两次 → `read the seat 2 time(s)`；坏座位当零信任 → `answered 0 event(s), want the 4`。`commitSeq<count` 由 `TestASeatBelowItsCountRederivesTheRank` 具名覆盖（复跑 PASS）。

**④（症状锚绝对成本上升：判为改动，非负载；回归须重估）**：同机同夹具三形状 252 代，total（第1–5代／末5代中位数）：**无座位无接线 139.59/264.16（1.89）；接线无座位 212.98/360.01（1.69）；本树 223.93/379.96（1.70）**，单次 53.24s／78.08s／77.75s。⇒ ①负载假设否定（改动前形状在同负载窗口内复现 R76 的 131.75/242.9）；②**座位≈0**（接线无座位与本树同值，与·二的屏障数互证）；③成本落在 `frame`（框架检查点 ×2.00）与 `closing`（`Close` ×2.19），`commits` 全程无回归。**回归声明：开启地址维护使一代的绝对成本升至 ~1.53–1.60×；S1c 的 0.98–1.09× 只在「一次事件提交」上成立，不延伸到「一代」，须在载体屏障预算／检查点批形状上重估（08／09 域）。比值由 1.89 降到 1.70 不得当作改善。**

**写集（本轮）**：`memory/ledger/journal.go`（`namespaces()`）、`memory/ledger/journal_test.go`（一锚）、`memory/ledger/segment_store_test.go`（两控制＋`dropRawSeat`）、本域 design/tasks。**`assembly/`、`config.go`、`segment_store.go` 生产码零写**；探针与变异经 `-overlay` 注入 `/tmp/recovery-06r4b/`。

**红绿与门禁（主树原始退出码，`/tmp/recovery-06r4b/` 有全文）**：

| 命令 | exit | 结果 |
|---|---|---|
| `go build ./...` | **0** | — |
| `go vet ./memory/ledger/` | **0** | — |
| `go test ./memory/ledger/ -count=1` | **0** | ok 38.813s（80 顶层测试，0 FAIL） |
| `go test ./assembly/ -count=1` | **1** | 336.243s／240 测试，**仅 2 红**：`TestGovernanceSliceWiredInProduction`（07.R2 载荷裁减后果，待 assembly 锚合批单重述）＋ `TestSymptomAnchorPerGenerationCostFlatOverALongRun`（故意红至反转完成） |
| `go test ./tests/... -count=1` | **0** | 13 包全 ok |
| `go test ./runtime/... ./features/... ./tool/... -count=1` | **1** | 16 包 ok；唯 `runtime/task` 的 `TestConsumptionFailureIsReportedByName`（12 域 G2-C2 既有故意红，`12/tasks.md:17`）。**归因**：以「旧 `namespaces()` 语义」overlay 复跑同红 ⇒ 非本叶引入 |
| 四锚 `-count=10` | **0** | 121.256s |
| 三座位锚 `-race -count=10` | **0** | 133.920s，70 PASS（含子测试）／0 DATA RACE |
| `comment_policy . examples/wechat-bot examples/cli-host` | **0** | **0 finding(s)** |
| `comment_policy -no-baseline assembly api runtime memory features tool internal cli` | **0** | **0 finding(s)** |

**`06.R4` 框已勾**（依据派单：四项完成且树除两枚具名预期红外全绿）。**未变项**：`storeLoad 平坦` 仍未兑现（§二十三·五 第 1 条）；**新增重估项**：地址维护使一代绝对成本 ~1.53–1.60×（§二十三·四）。

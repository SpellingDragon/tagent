# B 历史读取—派生索引 · 六叶执行计划

> 只从 [ORCHESTRATION](../../ORCHESTRATION.md) 派发；共同合同不在此重复。以下候选写面及命令均属未来执行，本轮未实施、未运行。
> 根：`/Users/pengweiye/Documents/codes/tagent`；候选文件均相对该根。设计门前生产只读，测试/原型另经精确签发；候选列表不授予整簇修改权。
> 状态：六叶全部未完成。既有测试已用 Grep 核定义；五个拟新增名称均零命中，尚未建立、未执行。命中数指每轮顶层测试，`-count=10` 预期各执行十次，子测试另核非空与终局。

## 06.101 真实读路径与最小反证

- [x] **06.101 枚举全部索引角色、真实读取与成本，验证最小反例。**（2026-11-21 完成：D8 记录 8 小节；H08 证实——129 同桶合法键第 129 个被误判损坏且根不可重开（新锚 10/10 红，编排者独立复跑同签名）；H09 证实（前驱查询含目标后全史、scoped 首读全族）；H07 部分证伪；H10/H11 未证。产品面仅 journal_test.go 探针。）
- **取证记录：[design D8](design.md)。** 结论：H08 证实（新锚 exit 1，10/10 逐字同构，合法 129 键同桶在 12.5% 负载被 `ErrJournalCorrupt` 拒绝且 root 无法重开）、H09 证实、H07 部分证伪、H10/H11 未证；角色清单 19 行、六个角色分列、成本读数与未验面见 D8.2–D8.7。本锚为 06.103 转绿的硬门，全包回归在它红时不可能通过。本叶未勾选。
- 阶段：取证；前置：**13.101**。
- 候选读取：`memory/ledger/journal.go`、`memory/ledger/journal_kv.go`、`memory/ledger/segment_store.go`、`memory/ledger/kv_original_store.go`、`memory/ledger/reference.go`、`memory/ledger/original_store.go`、`memory/ledger/config.go`、`memory/ledger/tier_facts.go`。
- 候选读取：`assembly/read.go`、`assembly/run.go`、`assembly/compress_consumer.go`、`api/event/compress_product.go`、`scripts/fold-cold-root/main.go`；按符号枚举全部生产、接口满足、示例和测试消费者。
- 候选测试：`memory/ledger/journal_test.go`、`memory/ledger/kv_original_store_test.go`、`assembly/read_test.go`、`assembly/symptom_anchor_recovery_test.go`、`assembly/symptom_anchor_longrun_test.go`；仅最小反证，不改产品。
- 产出：身份/缺失/墓碑/校验/前驱/原文 scope/压缩产物/转换清单；每项列来源、真实入口、返回语义、角色、成本轴、正常与失败路径。
- 产出：先列槽、序、发布元数据、窗口、校验及指标自身的角色；固定目标与前驱间距，增加目标后历史；量 scoped 首读/热读和采样时间、分配、档案保留增长。
- 正锚（既有，1 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./assembly -run '^TestParentLookupCountsTheSearchBeforeTheTarget$' -count=10 -v`。
- 正锚（既有，1 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^TestOriginalReadEntrypointsPreserveBytesAndScope$' -count=10 -v`；六入口及第七条接口绑定子案逐一命中。
- 负锚（拟新增，当前 0、建立后 1 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^TestLegalClusteredKeysSurviveCommitAndReopen$' -count=10 -v`。
- 该负锚通过真实 Commit/Read/重开构造 129 同桶合法键，含 absent/墓碑；算术造键不等于存储实跑，合法碰撞被报损坏才构成产品反证，不强造红。
- 安全负锚（既有，2 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^(TestJournalReplayStatesAZeroFillTailAsTornAndRefusesAFillItResumesPast|TestJournalReadViewLeavesTheTornTailOfTheWriter)$' -count=10 -v`。
- 指标对照（既有，2 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./assembly -run '^(TestBootStageSamplesHaveIndependentEndpoints|TestBootStageSamplesRejectDoubleChargeAndMissingSpan)$' -count=10 -v`；不能以通过代替指标自身成本取证。
- 消费者锚（既有，1 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./assembly -run '^TestRecallTraceThroughTierCountsOldTargetLookup$' -count=10 -v`；真实 Tier/工具成本，宿主装配另由 06.105 覆盖。
- 存在状态与预期：上述既有命令当前各有对应定义；旧前驱测试的 H+2/整条扫描数是现状观测，不是未来必须保持的实现。
- 停止条件：入口不可达、空夹具/零匹配、源码身份不明或计量只覆盖后半查找则反证无效；疑点被驳回记证据，角色/目标未承接仍不关闭。
- 交付去向：06.102 与 12.102 消费同一取证，13.102 接收能力/验证缺口；不以历史记录替当前运行证据。

## 06.102 比较设计并精确签发

- [x] **06.102 比较保留、最小改、删并或替代，冻结实施选择。**（2026-11-21 完成：七案裁定＋D6 十三字段＋D9 八节；H08 修复原型使新锚 3/3 绿。独立设计审确认七案实质成立，2 阻断已修——D8.5 引文失真经溯源（06.101 原文即"每 pass ＋1"，0 处"每次提交"）改为澄清记录；行5④ scoped 作用域收窄列为 06.103 派发前置待 A 侧会签。审者移交四问全部源码级核验：满表走满回唯一修复规则、偏移文件并入既有重建、作用域收窄不削弱安全、被否四案理由成立。）
- **方案比较记录：[design D6 签发区](design.md)＋[design D9](design.md)。** 七案选择：H08 探测终止条件＝表本身（`used+1` 上界，走满回唯一修复规则，删“链长即损坏”判词；129 同桶键原型 3/3 转绿，邻域 20 项全绿）；H09 前驱＝逐提交 order 行偏移索引（8 B/提交，第一段随目标之后历史消失，链距保留）＋scoped 首读按身份解析（48 对→0，2 次点读）；CRC 保留全帧扫描（替代案已找到反例：`value()` 不验帧 CRC）；Content 保留（真实理由改为恢复自带正文副本）；`passRecords` 保留并更正 D8.5 的增长轴（维护 pass，非每次提交）；`#2/#5/#6` 三面均保留（行界语义与 `ErrScopeRequired` 无替代）；不新增档位/机制。D6 十三字段已填，第 10 行“独立安全等价审”仍为待审稿；本叶未勾选，等独立设计审通过由编排者勾。
- 阶段：设计；前置：**06.101、12.101、13.102**。
- 候选读取：06.101 的逐文件清单及 `memory/ledger/atomic_store.go`、`memory/ledger/rustviking_kv.go`、`memory/ledger/atomic_store_test.go`、`memory/ledger/segment_store_test.go`、`memory/ledger/journal_kv_test.go`。
- 候选读取：`assembly/checkpoint.go`、`assembly/anchored_boot_parity_test.go`、`assembly/read_test.go`、`tests/integration/history/storage_capabilities_test.go`；原型限签发的隔离副本。
- 候选计划写入：本单元 `design.md` 签发区及 `tasks.md` 的已选择验证面；生产不写，测试候选仅 `memory/ledger/journal_test.go` 的等价反证。
- 产出：design D2 每角色的独立必要性、删掉谁坏、保留/最小改/删并/替代比较，以及全局维护、存储、启动/读/写/关闭总成本。
- 产出：身份索引、有序索引、发布元数据、窗口、校验分别判定；meta 的存在性与值、seat 分区/计数、缓存失效都纳入，不按“多索引”直接判多权威。
- 外部选型：允许评估成熟库但不预选；先取得一手文档/版本/保证，再同数据、故障点、只读权限和冷热条件原型比较，文档承诺与实测分列。
- 正锚（既有，3 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^(TestJournalStatesItsCommitOrderNewestFirst|TestEachIndexNewestFirstStreamsTheCommitOrder|TestACorruptStoreSeatRederivesFromTheChain)$' -count=10 -v`。
- 当前基线负锚（既有，1 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^TestJournalOpenRefusesCorruptionBehindAStatedWatermark$' -count=10 -v`；取证保留原断言：水位覆盖的帧损坏使写 open 与只读 open 均当场报 `ErrJournalCorrupt`（`journal_test.go:2972–2975`），不以候选时机重写基线。
- 当前基线成本锚（既有，1 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^TestOpenIntegrityScanIsTheWholeLogAndGrowsWithIt$' -count=10 -v`；全日志校验字节、零回放窗口及 200/2000 提交的线性读数是现状断言，不冻结未来实现。
- 条件负锚（拟新增，当前 0、选替代后 1 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^TestRecoveryReadIntegrityMatchesFullVerification$' -count=10 -v`；候选文件 `memory/ledger/journal_test.go`。
- 若拟替代全 CRC，该锚逐项覆盖损坏、present/absent/tombstone、枚举遗漏、前驱、原文、未访问坏帧和只读安全；未访问不是豁免。独立安全等价审未证所选保证即保留 CRC。选择保留时不建可选锚，后续跑原安全/成本锚；选择替代时先签发并完成旧时机断言及完整成本锚的等价迁移，再跑替代锚，不额外强制旧 open 当场失败；全包回归仍需通过。
- 算法/读接口对照（既有，2 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./assembly -run '^(TestAnchoredBootMatchesFullBoot|TestReadCorruptionNamedError)$' -count=10 -v`；前者手调 `seedFullReplay`/`seedAnchoredReplay`，只证明算法对照，不是实际 Boot 覆盖；后者核读接口错误传播。
- 真实装配消费者锚（既有，1 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./assembly -run '^TestYoungLogBootRestoresTheOwedMailboxThroughTheSlice$' -count=10 -v`；经公开 Assemble 核年轻链非空欠账及冻结路由。完整三分支消费验证由 12.105 建立、06.105/106 签收，不以算法对照抵销。
- 存在状态与预期：既有名均已核；各原型用相同输入、等价性质并记录真实执行数；迁移前后锚逐项对应，不把 helper 通过当 Boot/召回通过。
- 签发：填写 design D6 所有字段，含精确生产/测试/文档/门文件、每文件所有权、所有消费者、完整命令和预期命中；逐条列被迁旧时机/成本断言、替代对应性质和时点、旧/新文件及测试全名/完整 argv/命中数、独立安全等价审。未填或未审不授权，原 1.3×/boot/total 不缩。
- 签发：接口/持久字段/格式/外部依赖变动冻结后才实施；共享提交位置/发布/快照语义与 A 同一记录，06.103 另等待 12.102。
- 停止条件：选型前提未证、元数据角色漏项、安全等价欠账、原型不同尺、写面仍是目录通配或独立设计审未通过则不签发。
- 交付去向：06.103/06.104 只取得已选文件子集；设计保留现状也要处理已证缺陷及成本残项，不以“未改”关闭目标。

## 06.103 实施所选读取与完整性语义

- [x] **06.103 按签发实施索引、读取与完整性，保持原子结果和原序。**（2026-11-21 完成：七案落地。H08 聚簇锚 10/10 红→绿；前驱 16/64/256→2/2/2、recall 398→5；scoped 全族装载 48→0；CRC 双锚原样绿；变异三组全咬。编排者抽验聚簇＋前驱双锚绿。偏移文件"按槽键控"偏离经编排者追认入 D6（理由：槽布局冻结无 seq、保持增长不变量；06.106 复核）。旧计数锚按 D6 第9行迁移改名，历史命令不改写。相邻回归唯一红＝12.104 探针，已预登记。）
- **执行记录：[design D10](design.md)。** 2026-11-21 一次实施会话：H08 转绿（`-race -count=10` 10/10，改前 raw exit 1）、H09 前驱第一段消失（目标之后历史 16/64/256 → 2/2/2，无偏移文件控制面仍 18/66/258）、scoped 首读降为 2 次身份点读（`wholeSetLoads=0`）。三案各有一组变异红＋控制绿；偏移文件删档重开由写视图重写后同一查询仍答同一前驱。**结构性偏离一处已申报**：偏移文件按槽键控（`capacity×8`，字＝该槽键最近一次提交的 order 行尾）而非按提交，理由与后果见 D10.6 第 1 条，须 06.102/06.106 复核。旧计数锚 `TestParentLookupCountsTheSearchBeforeTheTarget` 已按 D6 第 9 行②迁为 `TestPredecessorQueryBoundsTheWalkToTheChain`（06.101 行记录的旧命令因此零匹配，历史记录不改）。本叶未勾选，未 commit/push/archive。
- 阶段：实施；前置：**06.102、12.102**。
- 候选生产：`memory/ledger/journal.go`、`memory/ledger/journal_kv.go`、`memory/ledger/segment_store.go`、`memory/ledger/kv_original_store.go`、`memory/ledger/reference.go`、`memory/ledger/original_store.go`、`memory/ledger/config.go`、`memory/ledger/tier_facts.go`、`assembly/read.go`。
- 候选镜像测试：`memory/ledger/journal_test.go`、`memory/ledger/journal_kv_test.go`、`memory/ledger/segment_store_test.go`、`memory/ledger/kv_original_store_test.go`、`memory/ledger/config_test.go`、`memory/ledger/atomic_store_test.go`、`assembly/read_test.go`。
- 转换候选：`scripts/fold-cold-root/main.go`、`scripts/fold-cold-root/main_test.go`；仅所选格式确需适配时签发。`atomic_store.go`/`rustviking_kv.go` 若需变动回 06.102，不能自动扩写。
- 产出：从候选压缩的实际所有权清单、完整调用方变更及读写/重启行为；身份同内容不重复、冲突保原字节、未知提交不冒成功。
- 产出：合法聚集不误报损坏；missing 与损坏分开；墓碑不复活；只读不截断；已发布缺正文具名失败；年轻链/坏派生的安全降级保持原序与计数。
- 产出：前驱固定目标、原文 scope 首读、尾读和查询的已选成本目标；若减少扫描，先迁旧计数锚到实际全成本轴，保留历史扫描负控而非固定旧线性次数。
- 正锚（既有，2 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^(TestFactCommitAnswersNewExistingConflictUncertain|TestEachIndexNewestFirstStreamsTheCommitOrder)$' -count=10 -race -v`。
- 正锚（拟新增，当前 0、建立后 1 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./assembly -run '^TestHistoricalReadsAvoidUnrelatedHistory$' -count=10 -v`；落 `assembly/read_test.go`，固定答案/授权，独立增长目标后历史和 scope 外原文，包含首读与热读。
- 负锚（既有，3 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^(TestJournalReplayStatesAZeroFillTailAsTornAndRefusesAFillItResumesPast|TestJournalReadViewWritesNothingBesideTheWriter|TestUncertainFactCommitRequiresAuthoritativeRead)$' -count=10 -race -v`。
- 负锚（06.101 拟新增，完成该前置后应存在，1 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^TestLegalClusteredKeysSurviveCommitAndReopen$' -count=10 -race -v`。
- 消费者锚（既有，3 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./assembly -run '^(TestParentLookupKeepsSessionIsolation|TestTierQueryHonorsScopeBeforeDecoding|TestReadCorruptionNamedError)$' -count=10 -race -v`。
- 原文消费者（既有，1 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^TestOriginalReadEntrypointsPreserveBytesAndScope$' -count=10 -race -v`；缺失、过期、损坏子案均保留。
- 校验保留方案：执行原安全锚 `go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^TestJournalOpenRefusesCorruptionBehindAStatedWatermark$' -count=10 -race -v`，及原成本锚 `go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^TestOpenIntegrityScanIsTheWholeLogAndGrowsWithIt$' -count=10 -v`；各 1 名、各 10 次。
- 校验替代方案：先落实 06.102/D6 已签旧 open 时机断言与完整成本锚的等价迁移，再执行 `go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^TestRecoveryReadIntegrityMatchesFullVerification$' -count=10 -race -v`（拟新增，迁移后 1 名、10 次），并逐条执行已签完整成本 argv/命中清单。覆盖损坏、present/absent/tombstone、枚举遗漏、前驱、原文、未访问历史、只读安全；不再叠加已获等价迁移的旧 open 时机要求，所选保证未证仍保留 CRC。
- 存在状态与预期：新增锚按各行及 06.102 条件核存在；既有名已核。替代路径先冻结逐断言等价迁移及完整命中清单，不以删测、skip、宽断言或零匹配放行。
- 相邻回归（两方案均执行）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger ./assembly ./scripts/fold-cold-root -count=1`；每包全选集核实际非零执行，替代方案包含迁移后的旧测试及新锚，不以 focused 或排除旧时机测试代全包。
- 停止条件：签发外字段/格式/依赖、消费者遗漏、所选完整性保证未证却删除 CRC、索引改变提交/原文/scope 语义，或把查询增长标成自然成本而无论证。
- 交付去向：06.104 与 12 单元收同候选存储结果；后续验证需修本叶时重开，不把安全或成本缺口转出后勾选。

## 06.104 定位产物并删减观测冗余

- [x] **06.104 收口产物定位、原文入口与默认生产观测负担。**（2026-11-21 完成并勾：锚1 九生产者/恢复者全覆盖＋四具名负例、锚2 关断态独立量判据；负变异三案全咬；注释单行修正（comment-only 门过）；相邻 7 红二分归因 A 单元 `loop.go`＋`goal.go`（全部生产回退 0 红），A 残项单在飞；06.106 复核后勾。）
- **执行记录：[design D11](design.md)。** 2026-11-21 一次实施会话：两锚建立且各 10/10 绿（`TestCompressionAnchorLocatesEveryProducerWithoutRerunningModel` 覆盖九个生产者/恢复者角色＋四类具名负例；`TestRecoveryCostDetectsGrowthWithInstrumentationDisabled` 在引擎计量全置零的等价关断态下由边界实读计数咬住重复读取，并拒绝漏阶段读数）；`anchorEntry.Content` 保留理由改写为“恢复自带正文副本”，字段/载荷/行为零变化；`#2`/`#5`/`#6`、`passRecords`、`memory/ledger/journal.go` 零改动。三组 `-overlay` 变异各红、控制绿。相邻回归 7 红（history 2、capabilities 1、host 4）经逐面回退 HEAD 归因为 `runtime/agent/loop.go` 与 `features/evolution/governance/goal.go` 的未提交改动，非本叶写面，未修未屏蔽。本叶未勾选，未 commit/push/archive。
- 阶段：实施与删并；前置：**06.103、12.103**。
- 候选生产：`assembly/compress_consumer.go`、`assembly/compress_checkpoint.go`、`assembly/run.go`、`memory/replay/age_segments.go`、`memory/replay/age_plan.go`、`api/event/compress_product.go`、`memory/ledger/journal.go`、`memory/ledger/journal_kv.go`。
- 原文候选：`memory/ledger/reference.go`、`memory/ledger/original_store.go`、`memory/ledger/kv_original_store.go`、`memory/ledger/skeleton_assert.go`、`assembly/tools.go`、`assembly/host_document.go`；实际修改先按 06.102 所有权释放，非全部可写。
- 候选测试：`assembly/compress_consumer_test.go`、`assembly/compress_checkpoint_test.go`、`assembly/summary_gist_test.go`、`assembly/anchored_boot_parity_test.go`、`assembly/symptom_anchor_recovery_test.go`、`assembly/symptom_anchor_longrun_test.go`、`memory/replay/age_segments_test.go`、`memory/replay/age_plan_test.go`、`api/event/compress_product_test.go`、`memory/ledger/journal_test.go`、`memory/ledger/kv_original_store_test.go`。
- 迁锚候选：`examples/contract/memory_ledger/OriginalStore_example.go`、`examples/contract/memory_ledger/ScopedOriginalStore_example.go`、`tests/integration/history/original_reference_isolation_test.go`、`tests/integration/history/storage_capabilities_test.go`、`tests/integration/capabilities/search_real_store_test.go`、`tests/integration/capabilities/recall_tools_test.go`、`tests/integration/host/compress_pipeline_test.go`。
- 产出：遍历所有产物生产/恢复者后决定 Content 删留；ID 不含 text 的源码反证不能替代可寻址证明；保 Text 原字节、Covered 顺序、预算和原始来源，恢复不重调模型。
- 产出：阶段档案/时钟/计数引用与自身开销；只有 test 读取的档案比较移出默认生产，保留真实增长检测，不把关指标后无读数说成零成本。
- 产出：逐项核旧 #2 `OriginalStore.ReadSegment`、#5/#6 `ScopedReader` 删留，先迁等价锚；#1 裸原文转换、GetScoped 接口满足、ReadSegmentScoped/RecallPort/Run.RecallReads 活链保全，不按同名批删。
- 正锚（既有，2 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./assembly -run '^(TestCompressionSummaryReplaysTheRecordedGist|TestCompressionSummaryCutsToTheDeclaredBudget)$' -count=10 -v`。
- 正锚（拟新增，当前 0、建立后 1 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./assembly -run '^TestCompressionAnchorLocatesEveryProducerWithoutRerunningModel$' -count=10 -v`；落 `assembly/compress_checkpoint_test.go`，覆盖所有已枚举生产者及定位缺失/冲突负例。
- 负锚（既有，1 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^TestOriginalReadEntrypointsPreserveBytesAndScope$' -count=10 -v`；接口迁移后保 scope、过期、损坏与分段原字节判据。
- 负锚（拟新增，当前 0、建立后 1 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./assembly -run '^TestRecoveryCostDetectsGrowthWithInstrumentationDisabled$' -count=10 -v`；落 `assembly/symptom_anchor_longrun_test.go`，指标关断仍由独立量捕获真实读取/重复工作增长，保持所选已证完整性保证；保证未证仍保留 CRC，去 CRC 不作性能负控。
- 消费者锚（既有，1 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./tests/integration/host -run '^TestCompressProductRestoresAfterRestart$' -count=10 -v`。
- 消费者锚（既有，2 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./tests/integration/capabilities -run '^(TestRecallToolSurface|TestSearchRecallsFromRealStore)$' -count=10 -v`。
- 存在状态与预期：两新锚当前不存在；其余均已核定义。契约示例 `go -C "/Users/pengweiye/Documents/codes/tagent" test ./examples/contract/memory_ledger -count=1` 仅编译，不能充作运行命中。
- 相邻回归：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./api/event ./memory/replay ./memory/ledger ./assembly ./tests/integration/history ./tests/integration/capabilities ./tests/integration/host -count=1`。
- 停止条件：任何生产者仅剩 anchor 正文、消费者 scope/接口满足失联、等价锚未迁、观测撤掉后失去退化敏感性或新增默认生产计量层未经设计比较。
- 交付去向：13.104 接公开能力，13.105 后续对账；删除所需文档/baseline 连带面由 06.102/13.102 签发并在本窗口单写完成，不反向等待 13.105 关闭。未签发则保留接口，清理目标欠账仍不勾。

## 06.105 安全等价与全成本回归

- [x] **06.105 验证真实入口安全等价及冷热、深浅、完整成本。**（2026-11-22 勾：两阻断解除——①boot O(历史)走查经 D13 重开＋06.103R 探针修复，O(历史)→O(log C) 实证（restoreChains 9.498ms→539µs，计数锚 10/10）；②rustviking sibling 只读构建＋PATH 注入，TestStorageCapabilities 80/80 零 skip。1.3× 判决维持；A1-cold 判环境窗 1/32 未复现；localfile 别名缺口非阻断（config 接缝已验）；flake 归 F2 清单；total 判决面归 F2 矩阵。）
- **执行记录：[design D12](design.md)。** 2026-11-21/22 一次验证会话，全部命令串行：16 组锚、310 次顶层执行，15 组 exit 0（309 次绿）。安全等价成立——A6 三安全负锚 `-race` 30/30、A5 全帧 CRC 10/10（30400 B@200、304000 B@2000、回放窗口 0/0，原样承接未缩）、A7a/A7b/A7c/A8/A10 消费者与转换全绿、B1 前驱 2/2/2（原 18/66/258 控制面仍在）与 trace 398→5。档位：`memory`/`journal` 绿，`localfile` 别名经 `memory/ledger` 引擎矩阵七子案 ×10 绿（`TestStorageCapabilities` 内无别名子案，缺口记录、未自建），`file` 真 rustviking **具名 skip ×10、未验**。**未闭合**：`A1-cold` 第 1 轮五阶段同时越 1.3×（1.58–1.68×）而计数面全绿，同配置复跑 10/10 绿、31 次执行中仅 1 次，未复现；1.3× 含 boot/total 未闭合（boot 墙钟 depth 252 实测 2.0–4.7× 参照深度，即保留的全日志 CRC 未消项），界未放宽、失败轮保留。冷热分列：冷编译 9525 ms，冷态代际墙钟约热态 1.35×、计数面一致。相邻回归 12 包 0 FAIL、无 `cached`。既有 flake `TestNativeConversationRoundTrip` 串行 2/5 红，签名同 12 域 D13.6-①，非本候选引入。本叶未勾选，未 commit/push/archive。
- 阶段：验证；前置：**06.104、12.105**；真实恢复分支锚由 12.105 交付，不用候选名字代替结果。
- 候选只读生产：06.103/06.104 已签发清单及 `memory/ledger/atomic_store.go`、`memory/ledger/rustviking_kv.go`、`scripts/fold-cold-root/main.go`；发现修复回所属叶。
- 候选测试：`assembly/read_test.go`、`assembly/symptom_anchor_recovery_test.go`、`assembly/symptom_anchor_longrun_test.go`、`memory/ledger/journal_test.go`、`memory/ledger/atomic_store_test.go`、`tests/integration/history/storage_capabilities_test.go`、`tests/integration/history/symptom_anchor_midband_test.go`、`tests/integration/capabilities/recall_tools_test.go`、`scripts/fold-cold-root/main_test.go`；新增验收面仍先签发。
- 产出：真实 Tier/recall_trace/recall_get/查询的原字节、原序、scope、缺失/损坏及增长轴；外部转换干跑/恢复后独立重开全键全事实比较，源变/部分目标拒绝。
- 档位：`memory`、`journal`、`localfile` 别名、`file` 真 rustviking 逐项运行；扩充既有 TestStorageCapabilities 的缺档，不以 AtomicStore 替身当真实 CLI。无资源/skip 保持未完成。
- 成本：冷编译、冷开档、热态分列；浅/深固定答案和业务工作量，记录首读/热读、负查询、枚举、开档、索引、提交、checkpoint、rebind、关闭及总墙钟、分配/保留字节和 I/O。
- 成本：现状/保留方案单列完整帧 CRC O(日志字节)；替代方案同轴计入移至各操作的全部校验成本，不用索引窗口零值或推迟至首次读掩盖。所选保证未证仍保留 CRC；原 1.3× 判决含 boot/total，全部失败轮保留，不把历史低末首比值当常数复杂度。
- 正锚（既有，2 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./assembly -run '^(TestSymptomAnchorBootCostFlatAcrossGenerations|TestSymptomAnchorPerGenerationCostFlatOverALongRun)$' -count=10 -v`；冷条件用独立缓存/合成根执行同命令，不删共享缓存。
- 阶段正/负锚（既有，3 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./assembly -run '^(TestBootStageSamplesHaveIndependentEndpoints|TestBootStageSamplesRejectDoubleChargeAndMissingSpan|TestBootWallClockRejectsUnattributedGrowth)$' -count=10 -v`；如涉及旧阶段计量实现，先按 D6 逐断言等价迁移并执行已签完整 argv，端点、双计/漏计及未知增长拒绝性质不缩。
- 增长负锚（06.104 拟新增，届时 1 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./assembly -run '^TestRecoveryCostDetectsGrowthWithInstrumentationDisabled$' -count=10 -v`；时长与计数同轴，重复/漏阶段控制各命中，保持所选已证完整性保证；去 CRC 不作为性能负控。
- 共同安全负锚（既有，2 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^(TestJournalReadViewLeavesTheTornTailOfTheWriter|TestJournalReplayStatesAZeroFillTailAsTornAndRefusesAFillItResumesPast)$' -count=10 -race -v`。
- 校验保留方案：执行 `go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^TestJournalOpenRefusesCorruptionBehindAStatedWatermark$' -count=10 -race -v` 及 `go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^TestOpenIntegrityScanIsTheWholeLogAndGrowsWithIt$' -count=10 -v`；原安全/成本锚各 1 名、10 次。
- 校验替代方案：先核 06.102/D6 旧 open 时机断言与完整成本锚的等价迁移已完成及独立安全等价审，再执行 `go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^TestRecoveryReadIntegrityMatchesFullVerification$' -count=10 -race -v`（拟新增，届时 1 名、10 次），及迁移清单全部成本锚的已签完整 argv/命中数；不另强制旧 open 当场失败。损坏、present/absent/tombstone、枚举遗漏、前驱、原文、未访问历史、只读安全和原 1.3×/boot/total 逐项核验。
- 消费者锚（既有，2 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./tests/integration/history -run '^(TestStorageCapabilities|TestSymptomAnchorMidbandProcessRecalledByTicket)$' -count=10 -v`。
- 消费者锚（既有，1 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./tests/integration/capabilities -run '^TestRecallToolSurface$' -count=10 -v`；同轮另执行 06.103 的 TestHistoricalReadsAvoidUnrelatedHistory 完整命令。
- 转换正/负/消费者锚（既有，3 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./scripts/fold-cold-root -run '^(TestOfflineConversionPreservesEveryFact|TestSourceChangeRefusesConversion|TestPartialTargetIsNotReportedReady)$' -count=10 -v`；真实 CLI，不直接 helper 替代。
- 存在状态与预期：既有组均已核；TestStorageCapabilities 现含真实 rustviking 的 skip 分支，顶层 PASS 不代表该子案执行。按四档和各负例核非零命中及终局。
- 真实恢复消费者交接（12.105 拟新增，当前未建立未执行；届时 1 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./assembly -run '^TestRecoveryEntriesPreserveStateAndObligations$' -count=10 -race -v`；由 A 拥有 `assembly/checkpoint_test.go`，B 签收同候选结果。经真实 Boot/Assemble 触发无可用锚、有效锚、允许降级三类生产分支及 checkpoint 子分支，按 A design D3 各核非空状态/义务与拒绝负例；缺证据不关闭，不借此扩 B 写面。
- race 单列：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./assembly -run '^(TestParentLookupKeepsSessionIsolation|TestAnchoredBootMatchesFullBoot)$' -count=10 -race -v`（既有 2 项）；后者仅为手调 seed 算法对照，不替代上一行实际装配恢复证据；race 数据不混入正式时长比较。
- 相邻回归（保留/替代均执行）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/... ./assembly ./tool/recall ./tests/integration/history ./tests/integration/capabilities ./tests/integration/host ./scripts/fold-cold-root -count=1`；全选集真实命中，替代方案包含等价迁移后的旧测试和新锚，不以 skip/删测/排除旧断言代全包通过。
- 停止条件：未知 boot/total 或查询增长、缺档/跳过、源/目标不等价、独立负控不敏感、只有合成判定器控制而无真实退化路径，均不得勾选。
- 交付去向：同候选原始证据交 06.106、13.105/13.106；B 未归因项不能转 F 或下游后伪闭合，最终十短/六长仍归顶层 F2。
- **归因记录：[design D12.9](design.md)（06.106 判决①回叶，2026-11-22）。** boot 增长的构成已定位：全帧 CRC 占 boot 增量 **15.7%**（补测①；30 轮逐轮 5.6–21.7%、中位 17.3%），**79.7% 落在 `restoreCheckpointState`+`restoreInMemoryState` 块内**，块内由 `adoptRecordedPublication → configChain.latest → loadAddressedLocked`（`assembly/commit.go:277`）的**提交序 key 走查**（`memory/ledger/journal_kv.go:353`，每提交一次 `pread`、≈3.5 µs/提交，随历史线性）主导；该走查无任何计数面，由候选提交 `0f84b2d1` 以它替换候选前的 prefix 读引入——判 **结论(b) 未声明的新增长＝缺陷候选**（不修，转设计门；`assembly/commit.go` 现不在任何叶写面内）。fold、checkpoint 载入入表、seedReplay、站点建立、converge 均平。total 有读数面无判决面（30 轮窗比中位 1.18×，工作段占绝对多数且平）→ 记 **F2 矩阵补面**。**1.3× 维持未闭合**且理由重述：不再是"已声明 CRC 轴"，而是未声明走查。本叶仍不勾选，未 commit/push/archive。
- **重开裁定：[design D13](design.md)（设计门，2026-11-22；06.106 判决①按 D12.9 结论(b) 转回）。** boot 期"最新配置事实"的发现由**提交序 key 走查**改为**按名探针**（倍增定界＋二分＋命中值进装载）：成本＝2⌈log₂C⌉+1 次点读＋C 次值读，与提交数无关；零新键、零新格式、零写侧改动，只退役 `journalKV.EachCommittedKeyNewestFirst` 与 `TierFacts.CommitOrder` 两条被移除唯一消费者后的面。**否决**：回退 prefix 读（槽表一遍 `capacity×stride`，与 `longrun_test.go:351` 的零面互斥，且轴是活键集＝只换常数不换轴，并勘误 D12.9 该方法"随配置事实数"的括号）、head/seat 位置键（一条持久派生键＋同批写＋崩溃窗校验＋旧根回退，换每 boot 十余次点读）、B 新增窄接口（单消费者，且零面下无更廉价实现）、声明为已声明轴（D12.9 已驳）。实施窗口新签 **06.103R**（`assembly/commit.go` 首次入签发面，拥有者＝06 域）；三锚（正锚两枚原样绿／新负锚 `TestConfigChainDiscoveryCostsTheChainNotTheHistory` 先红后绿／四条真实入口消费者锚）、四组变异、回归与回滚触发见 D13.5。**本叶仍不勾选**：新负锚与 `TAGENT_BOOT_TRACE` 归因复核尚未执行。
- **执行记录：[design D13.8](design.md)（06.103R 实施窗口，2026-11-22）。** boot 期"最新配置事实"的发现已由提交序 key 走查改为**按名探针**（倍增定界＋二分定位 C，命中值直接进装载、不重读）：`assembly/commit.go` 的 `loadAddressedLocked` 重写、`addressedChainLog` 缩为 `Get` 单口，`journalKV.EachCommittedKeyNewestFirst` 与 `TierFacts.CommitOrder` 退役（grep 零消费者；`check_dead_producers.py` exit 0 且不再点名）。新负锚 `TestConfigChainDiscoveryCostsTheChainNotTheHistory` **先红**（`walked 17 commit key(s)`）**后绿**（`-count=10` RUN 10/PASS 10；16 vs 512 无关提交 `point reads 2/2`、`prefix reads 0/0`、`walked 0/0`；C=1/64 `point reads 2/71`）；两正锚与四消费者锚逐字节未动（20/20、40/40）。五组变异逐组入 D13.8.5（4 与 4b 为签发组 4 的两半），**组 3 不咬**已申报（resident 集吸收重读，`ValueReads` 不动）。boot 成本 `-count=10` 得 19/20：唯一红轮为负载 7.85 下的墙钟阶段判，基线 A/B（实施前代码 10/10）与负载回落后复跑（10/10）已归因、最终字节复跑 20/20，计数面 30 轮全绿。`TAGENT_BOOT_TRACE=1` 复核 `restoreCheckpointState` 块 **661.5 µs→636.5 µs（最终字节复跑 779.0→348.0 µs，均深度无关）**，D12.9 held 为 9.498 ms。rustviking `TestStorageCapabilities` 80/80、0 SKIP（`file` 子案 20/20）补记 06.105 的具名 skip；`file` 档配置链发现仍未验。**本叶仍不勾选**：等 06.106 复审 D13.8.3 三处偏离、D13.8.5 变异与 `file` 档未验后由编排者勾。**06.103R 验证完毕（接续单 2026-11-22，全串行、逐块记负载；证据 `/tmp/rr-06-103r-f/30–50`）。** 新锚 `-count=10` raw exit 0（RUN/PASS 10/10，逐轮读数一致 `point reads 2/2`·`prefix 0/0`·`walked 0/0`，C=1/64 `2/71`）；正锚 20/20、消费者 40/40；boot 两锚 20/20（起测 load 7.03，计数面十轮全绿，无阶段墙钟红）、`TAGENT_BOOT_TRACE` 354 次 boot 的 `storage→restoreChains` 块 **714.0→539.0 µs**（D12.9 held 9.498 ms；O(历史)→O(log C)，末 10 不高于首 10）；rustviking `TestStorageCapabilities` 80/80、0 SKIP（`file` 子案 20/20）；退役 grep 零命中（唯测试注入缝）、`check_dead_producers.py` exit 0；build／gofmt／vet／`./memory/ledger` 124.223 s＋`./assembly` 416.311 s／`tests/integration/history` 11.173 s 全 exit 0、0 FAIL；`check_concept_budget.py` `RESULT: ok`。变异五组沿用前单存档（`/tmp/rr-06-103r/mut/`＋08–09 日志）复核咬合，未复做。**另立勘误**：D13.8.6／D13.8.11 的 comment_policy 行不成立——`-v` 实测 06.103R 净增 3 条 finding（新锚 doc 两规则＋新锚内 free-standing 注释），`lint.sh --only policy` 在当前工作树 exit 1（77 条／空基线；HEAD 树 0 finding）——明细入 **D13.8.12**，交 06.106 裁（本单禁改测试，未修）。

## 06.106 独立审与结构/成本关闭

- [x] **06.106 独立审全部角色、消费者和成本，关闭 B 单元。**（2026-11-22 两轮：初轮两阻断（boot 残差归因→D13→06.103R 探针 O(log C) 实证；rustviking sibling 构建 80/80 零 skip）；关闭轮核验形状/复跑/变异/CARRYOVER 35 行终判（兑现34/部分1）；B frontier 空。登记项非阻断：R4 补锚、file 档发现端到端归 F2、docs/api 两退役面重生成。）
- 阶段：独立审；前置：**06.105**。
- 候选只读：06.102 签发的每个精确文件、所有调用方及同机制相邻未改路径；本域四件套、对应历史原件、06.101–06.105 原始证据。审者不改产品。
- 候选写面：无；审者可在自有隔离区定向复跑或构造反例，但不改仓库。结论由编排者复核后回填本域 `design.md`/`tasks.md`；新增修复回 owning 叶，受影响结果重新验证。
- 产出：完整断言面/结构清单逐项过反事实、事实溯源与语义对齐；身份/序/发布/窗口/校验、Content、#2/#5/#6、指标各有保留或删并理由。
- 算法/摘要对照复核（既有，2 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./assembly -run '^(TestAnchoredBootMatchesFullBoot|TestCompressionSummaryReplaysTheRecordedGist)$' -count=10 -v`；前者仍仅为手调 seed 的算法对照，不是实际 Boot 覆盖。
- 负锚复核（当前拟新增，届时各 1 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^TestLegalClusteredKeysSurviveCommitAndReopen$' -count=10 -v`；`go -C "/Users/pengweiye/Documents/codes/tagent" test ./assembly -run '^TestRecoveryCostDetectsGrowthWithInstrumentationDisabled$' -count=10 -v`。
- 消费者复核（既有，1 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./scripts/fold-cold-root -run '^TestOfflineConversionPreservesEveryFact$' -count=10 -v`。
- 消费者复核（既有，1 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./tests/integration/capabilities -run '^TestRecallToolSurface$' -count=10 -v`。
- 真实恢复消费者复核（12.105 拟新增，届时 1 项）：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./assembly -run '^TestRecoveryEntriesPreserveStateAndObligations$' -count=10 -race -v`；核 Boot/Assemble 三类分支及 checkpoint 子分支的前提、非空状态/义务、拒绝负例和同候选交接，不用 full seed 对照替代。
- 校验保留方案复核：`go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^TestJournalOpenRefusesCorruptionBehindAStatedWatermark$' -count=10 -race -v`；`go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^TestOpenIntegrityScanIsTheWholeLogAndGrowsWithIt$' -count=10 -v`；原安全/成本锚各 1 名、10 次，核两种 open 拒绝与完整计量。
- 校验替代方案复核：先逐断言核 D6 旧时机/完整成本锚的迁移签发、独立安全等价审及实际迁移，再复核 `go -C "/Users/pengweiye/Documents/codes/tagent" test ./memory/ledger -run '^TestRecoveryReadIntegrityMatchesFullVerification$' -count=10 -race -v`（拟新增，届时 1 名、10 次）与全部已签成本 argv/命中数；不强制已迁旧 open 时机。损坏、present/absent/tombstone、枚举遗漏、前驱、原文、未访问历史及只读安全逐项不缩；保证未证即保留 CRC 并拒绝关闭。
- 存在状态与预期：既有定义已核；拟新增由前置交付后重新核存在。两方案均核 06.103/06.105 全包回归原始证据；替代方案包含等价迁移后的旧测试，不能 skip/删测/排除来过全包。审者核全部选集、原始退出码、同候选身份与无 skip，不用本节抽样代替 06.105 全矩阵。
- 成本关闭：原 1.3×/boot/total 不变；保留方案判 CRC 线性项，替代方案同轴判移至各操作的全部校验成本；未知增长留未完成，所选保证未证仍保留 CRC，删指标或挪校验时机不等于删成本，去 CRC 不作性能负控。
- 停止条件：审查分母不全、未查面未列、角色自证、需求漏承接、正负/消费者不等价或范围内 frontier 非空，均退回对应叶。
- 交付去向：列扫描范围、未查面、成立/驳回证据及逐目标结论，独立审无阻断后才向 13.106 提交关闭结果；不继承历史已勾与局部绿色。

# 证据台账：注释面收敛与测试面收敛

> 本文件是过程记录，不参与代码注释政策的适用范围（政策管 `*.go` 里的注释）。读法：§0 现状 → §1 批次台账（每批一条，含门与退出码）→ §2 测试映射表 → §3 承接去向表 → §4 豁免台账。

## 0. 基线（D0 完成时实测，后续批次以此为对照起点）

| 项 | 值 |
|---|---|
| 起点提交 | `4f7c723`（dev，文档重写完成态） |
| Go 文件 | 557（生产 156／测试 401） |
| 注释行 | 22,501（生产 12,507／测试 9,462） |
| 测试·基准函数 | 1,570 |
| 编号测试文件名 | 28 |
| 编号测试函数名 | 49 |
| **comment_policy 违规基线** | 初测 **12,499** 系重复计数（`.` 与子目录同时传入，同文件扫两遍），已修正：**两模块单遍 7,220 发现／9 个规则槽**（2026-09-27 E 段），入库为 `scripts/comment_policy/baseline.json`（按规则总量计，对文件搬移不变）；D1 T 半段后降至 **7,138** |
| 包总数（两模块） | 37 |
| 门禁（D0 时点） | `go build ./...`=0、`go vet ./...`=0、`gofmt -l scripts/`=空、`go test ./scripts/...` 双包 ok。注意：`gofmt` 自查历轮只覆盖 `*.go`／`agent/*.go`，`tests/` 下有一处已提交错位由 lint 首次抓出并修 |

`free-standing` 占绝大多数的原因已由实测确认（非工具误判）：章节横幅（`// ----- 区域名 -----`）与被空行隔开的说明块**不属任何 doc 槽位**，`go doc` 根本不渲染它们——这类注释既不是 API 文档也不是索引，正是本变更要清除的主体。该形态已写入 spec（`code-documentation`「章节横幅不属于任何声明」Scenario）。

## 1. 批次台账

### D0（任务 1.1–1.7）工具与机器门 —— 已完成

**交付**：`scripts/codetools`（`strip`/`decls`/`comment-check`/`merge-check`）、`scripts/comment_policy`（含 testdata 夹具与自测）、`scripts/check_comment_only.sh`、`scripts/check_test_merge.sh`、`scripts/gen_godoc.sh`、CI 两个报告模式步骤。

**测门本身（门未被证过就等于没有门）**：
- `codetools` 自测 `TestCommentCheckIgnoresDocsButNotCodeOrDirectives`（5 例）＋`TestMergeCheckGuardsTheTestSurface`（9 例）＋`TestMergeCheckComparesCodeNotComments`；
- `comment_policy` 自测含**零误报断言**（合规夹具必须 0 发现）与 7 条规则逐一触发断言；
- 两包在 root 模块内，故 `go test ./...` 天然执行它们（无需另接 CI）。

**过程中被工具自身抓出的四处缺陷（全部先红后修，非纸面推演）**：
1. `strip` 起初把「删注释留下的空行」算作差异 → 仅注释改动会被误判为代码改动；改为剥离后丢弃空行（排版由 gofmt 门单独管）；
2. `strip` 起初连 `//go:build`/`//go:noinline`/`//go:embed` 一并剥除 → 「只动注释」的门会放过**改变构建语义**的指令增删（实测 H2：删 `//go:noinline` 当时未被抓）；改为指令保留在比对流中；
3. `merge-check` 的哈希起初含**函数名** → 与「改名须经映射表声明」自相矛盾（合法重命名被判 body-changed）；改为遮蔽被声明标识符、比签名与体（实测用例 5 由红转绿）；
4. 文件头被编辑器自动注入 `package codetools` 造成语法错误后，我第一次「按 linter 提示修」反而引入损坏——IDE 报告在本环境会滞后/自作主张，**以编译器为准**已再次验证。

**一次无效自测的诚实记录**：首轮自测用 `go run` 且未校验构建，构建失败时两侧输出皆空，`diff` 比的是两个空集 ⇒ 同时「通过」了判等与必抓两向断言（假绿）。此后所有工具自测改为：先 `go build` 取退出码、再断言输出非空。这条已属本变更第 N 次「观测手段先于结论出错」，故写在这里而非只写结论。

**设计取舍（对任务 1.5 字面的偏离，显式记账）**：`gen_godoc.sh` 原写「index 含覆盖率摘要」，实测 `go doc -all` 在无文档包上的输出**未经证实**（临时模块因 toolchain 下载失败无法建立对照），且覆盖率本已由 `comment_policy` 的 `missing-package-doc`/`missing-symbol-doc` 单点判定——在生成器里另算一次会造出第二个可能漂移的见证。故删除该计数，index 只列包清单并指向扫描器。相应地 9.6 的覆盖判据改为「扫描器两规则计数为 0」。

**门的真实数据复验**：`check_comment_only.sh HEAD~2` 在真实含行为改动的提交上报 **58 处 CODE-CHANGED**（该提交确改行为，证明门在真实数据上咬得住，不是只在夹具上有效）；`check_test_merge.sh HEAD agent tool/action` 报 `2 package(s) intact`（109 测文件扫描 1.9s，成本可忽略）。

**基线数字见 §0。**

### D1（根组合包）· T 半段第一批：8 组

**已执行组（25 个源文件 → 13 个目标；根包测文件 70 → 58）**：org_diagnostics(3→1)、resources(4→1)、prompts(2→1)、consolidation_hint(2→1)、partition_collision(2→1)、modelref(2→1)、registry(3→1)、guardrails(2→1)。映射表见 §2（全 17 组已定稿，任务 2.1 完成）。

**门对本批的裁决**：`check_test_merge.sh HEAD .` → **intact**，且规模经独立核实为**非空比对**：base 与 head 各 **504** 条顶层声明逐名比哈希（不是"两边都没数据"的假绿）。可咬性用植入违规正面验证：从某测试体内删掉一条 `require.` 断言 ⇒ 门立刻报 `{"kind":"body-changed", ...}` 并指名函数；还原后恢复 intact。`go build ./...`/`go vet ./...`=0；`go test . -short`=ok 59.875s；`go test . -race -short`=ok 73.696s，**0 DATA RACE**。

**两处我自己的过程错误（记录以免被"结果绿"掩盖）**：
1. 第一版合并脚本用 `^package` 行定位后**丢弃了 `package` 之前的文件级注释**，且 import 前有注释时提取失败（编译报 "imports must appear before other declarations" 暴露之）。改为逐行扫描：注释一律保留、只摘除 import 声明本身。**代价**：3 个"既是目标又是源"的文件（`org_diagnostics_test.go`/`consolidation_hint_test.go`/`partition_collision_test.go`）已被破损版覆盖，我用 `git show HEAD:<f>` 取回原件后重跑——若无版本库可取回，这一步就是不可逆的内容丢失，合并脚本必须先跑在只读干跑模式上。
2. 我一度以为存在 4 组"同名测试冲突"（`TestMeditationDigest_IncludesCandidates`、`TestBuildAgent_ReadPartitionsIncludeOwnNamespace` 等），实为**我还没删源文件**导致包里同时有合并版与原件；对照 HEAD 后确认每个名字只定义一次，不存在真冲突。教训：报"重复/缺失"前先看基线，否则会把中间态当语义问题。

**全仓门禁实况（本轮 `go test ./... -short` = RC 1，如实记录、不并入总体绿）**：
- `agent` 包 2 红：`TestReview_CountingSpawnerDedupLeaksBarrier`、`TestReview_CountingSpawnerBlockedLeaksBarrier`。归属查明：**与我的合并无关**——它们在 `agent` 包（我这次只改根包），且 `agent` 单独整包也红；来源是**未跟踪**文件 `agent/zz_review_evidence_test.go`（`git status` 为 `??`，HEAD 里不存在，非我创建）。
- 该文件是**正确的缺陷证据**，不是坏测：`agent/settle_routing.go:218-224` 的 `countingSpawner.Spawn` 先无条件 `noteSpawn`，仅当 `res.Settled` 才 `voidSpawn`——`Deduped`（命中同键活跃任务）与 `Blocked`（磁盘降级拒绝收养）两形态都没有注册任务，永无 settle 路由，`awaiting()` 恒真，调用环不能自行静默（只能靠 ctx 上限退出）。**属行为缺陷，超出本变更范围（本变更不改行为）**，已停下上报待裁决。
- 1 红为负载敏感：`TestI1ConcurrentDelegationsEndToEnd` 单跑 ok（0.277s）、`agent` 整包 `-short` 亦 ok，仅在 `./...` 跨包并行下红——与 Monitor33 同族（那一族本轮已把断言改到有界重取；I1 我**未动**，避免在同一变更里混杂未审的测改动）。
- 排除 `TestReview_` 后 `agent` 整包 ok（41.362s）；根包 `-short` 与 `-race -short` 均 ok（59.875s／73.696s，0 DATA RACE）；`go build ./...`/`go vet ./...` = 0/0。

**T 半段余项（任务 2.2 保持未勾）**：org_hotreload(11→1)、org_candidate(11→1)、owner_retirement(9→1)、cross_generation(8→1)、tagent(6→1)、delegation(2→1)、build_agent(1→改名)、config(1→改名)、teststores(1→改名) 共 9 组；随后 2.3 去编号（本域现有 `TestD51_*`/`TestD53_*`/`TestSC_*`/`TestSE_*`/`TestM34_*`/`TestL3_*`/`TestWAL42_*`/`TestMonitor33_*`/`TestD52_*` 等，需一张 `--map` 表随批走）与 2.4 的整域门。

### D1（根组合包）· T 半段完成：70 → 17 个职责文件

**余下 6 组已合并**（org_hotreload 11→1、org_candidate 11→1、owner_retirement 9→1、cross_generation 8→1、tagent 6→1、delegation 2→1；45 个源文件删除）。**合并前先对原始（未合并）内容做同名预检**：6 组共 327 个顶层声明、**零冲突** ⇒ 本域无需 D15 helper 裁决（上一轮我曾把"未删源文件导致的中间态重名"误判为语义冲突，这次先算清再动手）。

**门与验证**：
- `merge-check`（绕过 shell 的生产检查、直接比根包）→ **intact**：504 条声明的无注释体逐名相等、断言数与 `t.Parallel()` 数不变。
- `go test . -count=1` ok **57.672s**；`go test . -race` ok **73.999s**、**0 DATA RACE**、0 FAIL；根包 `go vet` = 0；测文件数核对为 **17**，与映射表目标数吻合。
- 棘轮：`0 beyond baseline`，且总量 **7,220 → 7,138**（合并后 `missing-test-responsibility` 槽数随文件数下降）——证明"按规则总量计量"对文件搬移不变，这正是我从 `(文件,规则)` 改过来的目的；若仍按文件计，这次合并会凭空产生上百条"新违规"并逼人在 CI 上放水。
- `check_comment_only.sh HEAD` 报 5 处 `CODE-CHANGED` 落在我的合并目标文件上：**这是预期的门错配**，T 批改变的是"代码在哪个文件"，其正确门是 merge-check；另有 8 处落在 `agent/*`、`memory/*`、`rl/*`，属用户并行变更，不代为处置。**结论：每类变换必须各用自己的门**（T→merge-check，G→comment-check），已在 §2 步骤里写死。

**我这次改动自己造成的破坏，已就地修复**：删除 62 个文件后活文档出现失效引用——`README.md`（两处 `model_contract_matrix_test.go` → `modelref_test.go`）、`docs/upgrade-rollback-drill.md`（`resources_lock_test.go` → `resources_test.go` 两处、`reset_managed_drill_test.go` → `owner_retirement_test.go`）。复核后活文档（README／docs/wiki／docs/*.md／openspec/specs／tests/README／example README）**引用残留 0**；`docs/.dev/**` 是时点归档，按 Non-Goals 不改写。

**D1 余项**：2.3 标识去编号（`TestD51_*`/`TestD52_*`/`TestD53_*`/`TestSC_*`/`TestSE_*`/`TestM34_*`/`TestL3_*`/`TestWAL42_*`/`TestMonitor33_*`/`TestReentry42_*` 等，需旧→新映射表随批 `--map` 走，并同步活文档对测试名的引用），随后 2.4 复跑域门；再进 G 半段（先补文档 → 注释重写 → comment-check 等价门）。

### D1 · 2.3 标识去编号（26 个测试名＋两族 helper），以及三个**门自身的缺陷**

**改名**：根包 26 个编号测试名去掉任务号前缀、保留契约语义（`TestD51_ReceiptIsBackedByRealConsumers`→`TestReceiptIsBackedByRealConsumers`、`TestL3_FullConfigAndRollback`→`TestFullConfigAndRollback`、`TestWAL42_RelaunchAfterRestart…`→`TestRelaunchAfterRestartResolvesOnTheCurrentFace`、`TestMonitor33_LiveSession…`→`TestLiveSessionStaysWatchedAcrossToolGeneration` 等）；helper 两族 `mon33*`→`sessionWatch*`、`wal42*`→`walReentry*`（tmux 会话名、模型名等测试内字符串值一并跟随，保持一致）。外部引用面实测为 0（`.github`/`scripts`/`docs` 无一引用）；改后 xproc 锚实跑 PASS（12.3s／4.3s）。

**顺带抓出的真空门（真隐患）**：这些锚靠子进程 `-test.run=<全名>$` 驱动，而 `runBootChild` 只看退出码——过滤器失配时 `go test` 打印 `no tests to run` 并**退 0**，整族断言会静默空跑。本次改名恰好动过这些过滤器，故给 `runBootChild` 补反虚设断言。**先红验证**：过滤器指向不存在的名字 ⇒ 报 `boot child filter … matched no test — the gate would pass vacuously`；还原 ⇒ 绿。第一次红验本身是空的：我用单空格匹配被 gofmt 对齐成 `X   = "…"` 的常量，植入从未生效——"以为改了"≠"改了"，此后植入类操作一律先断言替换命中数。其余 7 处同类 xproc 站点（agent×4、memory×1、tests×1）属后续域，登记未动。

**三个门自身缺陷（真实使用暴露，均已修并加测）**：
1. `merge-check` 的改名归一化第一版漏赋 `raw` ⇒ 两侧哈希都被覆写为 `hash("")`，**体比对静默失效、门恒判 intact**。线索是"唯一违例只剩 assert-count"。修法：正确赋值 `raw`、空 `raw` 不得覆写哈希，并新增 `TestMergeCheckRenameNormalizationStaysBlindToNothing`（声明改名不得为丢失断言开路；纯改名必须通过）。
2. 棘轮只在 +1 时打印示例 ⇒ +19 时只给总数、不给规则与位置，不可行动。改为按规则输出 `baseline N -> M (+k)` ＋一条样本。
3. 断言数比对改为**单调**（只准增不准减，与棘轮同向）；`t.Parallel()` 仍要求相等——调度是语义不是排版。

**根包复验**：`go vet .`=0、`gofmt -l`=空、`go test . -short` ok **56.4s**、策略门对根包 **0 beyond baseline**（8 槽可下调）；全仓 `free-standing` +19 全部落在 `agent/*`（并行编辑，不代为处置，待 10.8 重定基线）。`merge-check` 登记 10 条派生差异后 `1 package(s) intact`，且**无 missing-test/extra-test**（测试面零增删）。

**D1 豁免登记（10 条派生差异，逐条可审）**：`mon33Filter`／`mon33Command`／`mon33PhaseEnv`／`mon33YamlEnv`／`mon33SvcName`／`mon33Child`／`mon33Model.Info`／`wal42Child`／`wal42Model.Info` —— 声明改名的派生（体内标识符与测试内字符串值随之变，语义不变，映射表已登记）；`runBootChild` —— 主动加固新增断言（1→2），方向虽为"只增"，但该体确非纯搬移，故逐条登记而非默默放行。

**第二次普查（同一任务内的自我纠正）**：首轮我只匹配「前缀式」编号名（`TestD51_*`），勾完 2.3 才发现还有**数字嵌在名字中间**的 10 个（`TestRollback24_*`、`TestFactory33_*`、`TestHotAdd34_*`）与 17 个 helper/类型（`d51YAML`、`g24Gate`、`di64`、`m34YAML` 等）。全部改语义名后归零；**保留 `TestTencentProvider_Hy3Model`**——`hy3` 是真实模型名，判据是「离开已归档变更能否读懂」，不是「含不含数字」。

**改名与门的交互**：helper 改名必须一并写进映射表（累计 70 条），否则门会把「已声明的改名」报成一缺一多——首次跑出 16 条违例正是这个原因；补全映射后剩 10 条，全部是**字符串值跟随改名**（tmux 会话名、模型名），归一化只重写标识符、不该重写值，故逐条登记为派生差异而非放行。

**2.4 退回未勾**：改名后的根包 `-race` 未真跑通——`rl/swappable_model.go: undefined: log`（用户并行编辑，非本域文件）挡住编译。「结果绿不能替代证据」，该项保持未勾并计入 10.8 集成复验清单。

### D1 标识映射表（任务 2.3 的可审记录：测试名 35 条、helper/类型 35 条）

| 旧名 | 新名 |
|---|---|
| `TestD51_DrainingReceiptTracksHeldConsumer` | `TestDrainingReceiptTracksHeldConsumer` |
| `TestD51_ReceiptIsBackedByRealConsumers` | `TestReceiptIsBackedByRealConsumers` |
| `TestD52_FoldIsIdempotentUnderRepeat` | `TestFingerprintFoldIsIdempotentUnderRepeat` |
| `TestD52_FoldedFieldIsLiveInTheFingerprint` | `TestFingerprintFoldedFieldIsLive` |
| `TestD52_HotAddedOwnerPullsTheRecordAfterNumericOnly` | `TestHotAddedOwnerPullsTheRecordAfterNumericOnly` |
| `TestD52_ModelRefAliasesFoldToStableFingerprint` | `TestModelRefAliasesFoldToStableFingerprint` |
| `TestD52_RemoteOnlyAliasSpellingStillPublishes` | `TestRemoteOnlyAliasSpellingStillPublishes` |
| `TestD52_RuntimeObjectAliasIsNotAStructuralChange` | `TestRuntimeObjectAliasIsNotAStructuralChange` |
| `TestD53_CloseInitiatedIsDistinguishableFromResourcesExited` | `TestCloseInitiatedIsDistinguishableFromResourcesExited` |
| `TestD53_HotAddedOwnerReceiptMatchesRealConsumption` | `TestHotAddedOwnerReceiptMatchesRealConsumption` |
| `TestD53_PerPublishObjectLifespan` | `TestPerPublishObjectLifespan` |
| `TestFactory33_CommittedBehavior` | `TestFactoryCommittedBehavior` |
| `TestFactory33_ConfigBuiltReleasesItsStoreLease` | `TestFactoryConfigBuiltReleasesItsStoreLease` |
| `TestFactory33_FactoryBuiltReleasesItsStoreLease` | `TestFactoryBuiltReleasesItsStoreLease` |
| `TestFactory33_FactoryConfigChangeReachesDelegations` | `TestFactoryConfigChangeReachesDelegations` |
| `TestFactory33_ReloadConstructsNoOrphanAgents` | `TestFactoryReloadConstructsNoOrphanAgents` |
| `TestHotAdd34_DataLandsInItsOwnStoreWithHostReturn` | `TestHotAddDataLandsInItsOwnStoreWithHostReturn` |
| `TestL3_CoordinatorHotApplyRevision` | `TestCoordinatorHotApplyRevision` |
| `TestL3_FullConfigAndRollback` | `TestFullConfigAndRollback` |
| `TestL3_RejectedCandidateKeepsBothAxes` | `TestRejectedCandidateKeepsBothAxes` |
| `TestL3_RollbackHookSurvivesNumericOnlyFirstUpdate` | `TestRollbackHookSurvivesNumericOnlyFirstUpdate` |
| `TestM34_SnapshotRotatesWithReloaderCommitPoint` | `TestHotParamSnapshotRotatesAtCommitPoint` |
| `TestMonitor33_LiveSessionStaysWatchedAcrossToolGeneration` | `TestLiveSessionStaysWatchedAcrossToolGeneration` |
| `TestReentry42_ChangedTargetResolvesOnTheNewGeneration` | `TestChangedTargetResolvesOnTheNewGeneration` |
| `TestReentry42_GenerationThatRemovedTargetRefusesWithoutRerouting` | `TestGenerationThatRemovedTargetRefusesWithoutRerouting` |
| `TestReentry42_PostSilenceRelaunchUsesResidentOwnerFace` | `TestPostSilenceRelaunchUsesResidentOwnerFace` |
| `TestReentry42_WithinLoopInitiatorResolvesOnBsOwnFace` | `TestWithinLoopInitiatorResolvesOnBsOwnFace` |
| `TestRollback24_HotAddNumericAndInFlightRollback` | `TestRollbackOfHotAddNumericWithInFlightTurn` |
| `TestRollback24_LateStageFailureLeavesNoOwnerPublished` | `TestLateStageFailureLeavesNoOwnerPublished` |
| `TestRollback24_RemovedParentRollbackKeepsSharedChildSingleOwner` | `TestRemovedParentRollbackKeepsSharedChildSingleOwner` |
| `TestSC_HotAddedAgentNumericOnlySeedsNextCall` | `TestHotAddedAgentNumericOnlySeedsNextCall` |
| `TestSC_RecordCommitsAtomicallyWithVersion` | `TestAppliedRecordCommitsAtomicallyWithVersion` |
| `TestSE_RecordReadIsLockFree` | `TestAppliedRecordReadIsLockFree` |
| `TestSE_SpawnerTTLReachesRealSpawnSpec` | `TestSpawnerTTLReachesRealSpawnSpec` |
| `TestWAL42_RelaunchAfterRestartResolvesOnTheCurrentFace` | `TestRelaunchAfterRestartResolvesOnTheCurrentFace` |

helper/类型（含 `di64`→`diagInt64` 这类）：

- `d51Receipts` → `diagnosticsReceipts`
- `d51YAML` → `hotParamsYAML`
- `d51YAMLNoSub` → `hotParamsYAMLNoSub`
- `d52RemoteYAML` → `remoteSpellingYAML`
- `d52Write` → `writeAliasConfig`
- `d52YAML` → `aliasSpellingYAML`
- `d53Boot` → `bootForDiagnostics`
- `d53Write` → `writeRoutedConfig`
- `d53YAML` → `routedSub2YAML`
- `di64` → `diagInt64`
- `g24Arm` → `armStageGate`
- `g24BYAML` → `ownerBYAML`
- `g24DiamondYAML` → `diamondYAML`
- `g24Gate` → `stageGate`
- `g24LeafYAML` → `leafYAML`
- `g24Write` → `writeGateConfig`
- `m34YAML` → `snapshotRotationYAML`
- `mon33Boot` → `sessionWatchBoot`
- `mon33Child` → `sessionWatchChild`
- `mon33Command` → `sessionWatchCommand`
- `mon33Filter` → `sessionWatchFilter`
- `mon33Model` → `sessionWatchModel`
- `mon33PhaseEnv` → `sessionWatchPhaseEnv`
- `mon33SvcName` → `sessionWatchSvcName`
- `mon33YAML` → `sessionWatchYAML`
- `mon33YAMLAfterPublish` → `sessionWatchYAMLAfterPublish`
- `mon33YAMLWithExtra` → `sessionWatchYAMLWithExtra`
- `mon33YamlEnv` → `sessionWatchYamlEnv`
- `wal42Boot` → `walReentryBoot`
- `wal42Child` → `walReentryChild`
- `wal42Filter` → `walReentryFilter`
- `wal42Model` → `walReentryModel`
- `wal42PhaseEnv` → `walReentryPhaseEnv`
- `wal42YAML` → `walReentryYAML`
- `wal42YamlEnv` → `walReentryYamlEnv`

### W1（agent 域 T）第一批：session 与 settle_routing 两组（33 源 → 2 目标）

**执行**：映射表 `scripts/consolidate_domain.py` 把 agent 的 111 个测文件划分为 18 个职责目标，`check` 子命令做**全覆盖断言**（未归位文件即失败）＋**同名冲突预检**；hold 清单（你正在编辑的 3 个测文件）本轮不动。本批完成 2 组：`session_test.go`（20 源）与 `settle_routing_test.go`（13 源），删 33 个源文件。

**合并器被真实使用纠正的两处**：
1. 预检只看函数名，漏了**两个不同路径的包同名**（本仓 `tagent/event` 与上游 `.../agent-go/event` 都是 `event`）与**同一路径在不同源用不同别名**（`upagent`/`agent`/`trpcagent`）——vet 连着报了 `event redeclared`、`undefined: tagentevent`、`undefined: agent`。我先手工逐个消歧（慢且易错），随后把正确算法落进工具：`scripts/consolidate_lib.py` **在拼接前按每个源文件自己的导入表**把限定符归一到主别名（文件内别名→包是无歧义的，拼接后就不可能分辨），并按仓内惯例合成 `trpcXxx`/`tagentXxx`；
2. 门用**全局映射表**跑 agent 域，把 root 的改名与别名归一误用到 agent 基线体上，凭空造出 169 条 `body-changed`。改为**按域分表**后违例降到 32 条，且逐条可归因：其中 `missing-test = 0`（我的合并没丢任何测试），7 条 `extra-test` 正是你未跟踪新文件里的测试，其余是你已改动的测文件与新增 helper。映射表按域入库（`rename-map-root.tsv`、`rename-map-agent-alias.tsv`）。

**门与验证**：`go vet ./agent`=0、`gofmt` 净、`go test ./agent -count=1` **ok 42.703s（后台波，与门校验并行）**。根包（D1）此前的 `-race` 余项仍挂在 10.8。

**W1 剩余**：agent 域另有 16 个目标待执行（映射已定，脚本一次一组端到端）；agent 域标识去编号（`d*`/`w1*`/`m3*`/`TestI1*`/`TestD42*`/`restart30` 等）尚未开始，须与结构合并分开成两次可证明变换。

### W1（agent 域 T）整域完成：111 → 22 个测试文件，并修掉驱动/门的三处缺陷

**结构**：19 个职责目标全部生成（18 组＋`poc_test.go` 独占），agent 测文件 **111 → 22**（19 目标＋3 hold）。`go vet ./agent`=0、`gofmt` 净；结构门 `missing-test = 0`（无测试丢失），余 18 条违例逐条归属：17 条来自你未跟踪的新测试文件（7 `extra-test`＋10 `extra-helper`），1 条 `body-changed` 在你已修改的 `reliability_matrix_test.go`。

**三处被真实使用纠正的缺陷**（都是"我以为对"被证伪）：
1. **构建约束被误并**：`poc_test.go` 带 `//go:build poc`，并入后该约束被套到整个 `agent_test.go` 头上 ⇒ 共享 helper（`loopMockModel`/`waitForFinalResponse`/`newTestTagentAgent`/`pinEchoTool`）随文件一起被排除，编译报一片 `undefined`。我先怀疑符号丢失，实际是**单文件约束不可随合并扩散**。修法：`poc_test.go` 保持独占，驱动加硬规则——带构建约束的源若被并入多文件组即报错退出。
2. **重跑不幂等**：驱动第二轮从磁盘读源，而"目标同时也是源"时读到的是**已合并内容**，与 HEAD 原文再拼一次 ⇒ `busCap redeclared`、101 条假 `extra-test`。修法：`read_source` 一律**基线优先**（HEAD 是源之真身，工作树只作兜底），并加注释说明为何不能先读磁盘。
3. **门的全局限定符重写自相矛盾**：别名归一是**每文件**决定，我用一张全局 map 施于全包，同一拼写在两组里被映射成两个名字互相打架 ⇒ 303 条假 `body-changed`。修法：门改为**限定符盲**（`x.Foo` → `Q.Foo`，选择器名仍逐字比较，故调用语义仍可比），并用后实测降到 18 条全可归因；另修 `_` 空标识符被当身份键造成的假缺/多违例。

**遗留问题（不吞）——`TestI1ConcurrentDelegationsEndToEnd`**：单跑 0.375s 绿、整包跑必红且恰好 **6.00s**（内部预算耗尽）。这不是并行噪声，而是**合并把重锚挤到相邻位置后放大的负载敏感**：整包并发度上升使该锚的等待窗口不够。已登记为显式待办（任务 11.4），处置需在 T 之外做一次测加固（同 D1 里 Monitor33 的有界重取修法），不与本批混提交。

**全量套件现状**：`go test ./agent` 整包 RC=1，唯一失败即上述 I1 锚（用户并行编辑的 3 个新测文件未参与失败）。三门与 `-race` 仍按指示留到 W4 集成波统一跑。

### W1 · 11.3 agent 标识去编号（39 测试名＋20 helper），并修我两处操作失误

**改名**：39 个测试/基准名与 20 个 helper/类型改为契约语义名（`TestI1ConcurrentDelegationsEndToEnd`→`TestConcurrentDelegationsEndToEnd`、`TestW1_ConcurrentCallsIsolateTheirProjections`→`TestConcurrentCallsIsolateTheirProjections`、`TestS3mA_DeliverTaskSettled_Decision`→`TestDeliverTaskSettledDecision`、`w1Gate`→`enteredGate`、`rb2Key`→`sinkEventKey`、`m34FinalResp`→`scriptedFinalResp` 等）。判据仍是「离开已归档变更能否读懂」：`g1Refs`/`publishG2` 的 g1/g2 是**代际领域词汇**、`mustI64`/`TestToInt64Key_*` 的 64 是类型形状、`e2e*` 是端到端缩写——**全部保留**，不是"见数字就改"。映射表入库 `rename-map-agent.tsv`。

**避让纪律暴露的第二个后果**：`r30Stack` 被我改名后，你 hold 的 `deep_review_regressions_test.go` 引用旧名 ⇒ 包编译断。规则补入：**被 hold 文件引用的标识本波不改名**（已回退该 pair，故映射表比脚本表少一条）。

**我的两处失误**：
1. 回退脚本里 `for new, old in clash.items()` 方向弄反（clash 是 {旧:新}），于是"回退"实际是再替换一次、什么都没改，vet 仍红；发现方式是直接 grep 两个名字的现存位置而不是继续猜。
2. 我在 11.4 上过早下结论：先据"整包必红"断言是**共置放大**，随后安静整包跑 `ok 43.314s` 通过 ⇒ 那两次红与我的并发编译/门同窗，属**并发负载间歇超时**。教训：把"我没控制住的并发活动"计入被测系统性质之前，必须先在受控负载下复跑。定量复现（`./agent`＋`./memory`＋`./tool/action` 三包并发 × 3 轮）：**1/3 轮失败**，失败耗时恒为 **6.00s**（内部预算耗尽的确定值，非随机时长）⇒ 定性为「间歇性负载超时＋固定偏小的预算」。11.4 的修法据此定为**有界重取**（List/绑定后重读，预算到 30s 量级），而不是把 6s 调大掩盖；该改动属测加固，必须与 T/G 分开成批。

**门读数**：改名前后完全一致——`missing-test = 0`、18 条违例且归属不变（17 条你的未跟踪新测文件、1 条你已改的 `reliability_matrix_test.go`）；`go vet ./agent` = 0、`gofmt` 净、`go test ./agent` RC=0。

### 11.4 完成：并发委派锚的预算按用途重定（测加固，单独成批）

**判据不是"把超时调大"**，而是先问每个预算在断言什么：
- `context.WithTimeout(runCtx, ...)` 是**泄漏守卫**（路由坏掉时不留下 goroutine），不是速度断言 => 6s -> 90s；
- 捕获两个后台探测器、等待通道关闭是**观测预算** => 5s->30s、7s->60s；真挂死（永不关闭）仍必报，只是不被 CPU 饥饿误报。
- 断言文本与注释里 8 处 `I1:` 前缀（指向已归档变更的不变量编号）改为自解释文字：失败信息必须让不读归档的人也能看懂（`grep -c I1` = 0）。

**前后对照（同一负载波：`./agent` + `./memory` + `./tool/action` 三包并发）**：加固前 **1/3 轮失败，耗时恒 6.00s**；加固后 **4/4 轮 0 失败**。`go vet ./agent`=0、`gofmt` 净。

**方法学记账**：这一项先前被我误判为"合并共置放大"，实为并发负载间歇超时；纠正方式是受控复现（同条件多轮）而非推理。同时记录：本轮验证期间不再对同一包做结构改动，避免自我污染读数（此前一次误报即源于我并发的编译/门活动）。

### W2 · agent 子包四域结构收敛（59 → 11 个测试文件），四域门「空映射即 intact」

| 域 | 测文件 | 目标 | 依据 |
|---|---|---|---|
| `agent/task` | 20 → 2 | `task_manager_test.go`(19 源)、`task_board_test.go` | 生产脊柱只有 TaskManager/TaskBoard |
| `agent/compress` | 18 → 2 | `context_compressor_test.go`(15)、`session_projection_test.go`(3) | 压缩器与投影/切分是两条职责 |
| `agent/governance` | 9 → 4 | `gate_test.go`(5)、`approval_test.go`(2)、`goal_test.go`、`ledger_test.go` | 四块各自成职责 |
| `agent/reliability` | 12 → 3 | `inbox_test.go`(9)、`degradation_test.go`(2)、`anchor_test.go` | 信箱是单一职责，降级/锚各自独立 |

删 52 个源文件。门读数：`gofmt` 净、四包 `go vet` = 0、`go test` 四包 **RC=0**；结构门在**空映射**下即报 `1 package(s) intact`（零违例）——本批只搬文件与统一别名，没有任何测试体改变，是 T 批能给出的最强证据。

**预检工具的一处过严被纠正**：首版把 `Cancel`/`GenerateContent`/`Info`/`count` 判为跨源冲突，实为**不同接收者上的方法**（Go 里不冲突）。修正为「方法按 `接收者类型.方法名` 键控，包级名才比对」，随后四域预检全部零冲突通过。若不修正，会把合法合并误判为必须改名，制造无意义 churn。

**避让核查**：本波开始前重取 `git status`，确认四域内无你正在编辑的测试文件（你在 `agent/reliability/inbox.go` 有生产改动，T 批不触生产文件，故无冲突）。

### 12.4 · 一个被三门同时放行的静默失效（本变更迄今最有价值的门缺陷）

一次性改名脚本按 `n[len('TestM3_'):]` 切片去编号，把 **`Test` 前缀也切掉了**：`TestM3_TerminalTTLReachesLiveConsumer` → `TerminalRecordSurvivesConsumerRestart`。结果两个测试成为永不执行的死码，而三重关卡全部绿灯：

| 关 | 读数 | 为什么看不见 |
|---|---|---|
| `go vet` | 0 | 未使用的包级函数不是 vet 的错误 |
| `go test` | `ok ... [no tests to run]` | 非 `Test*` 函数压根不被登记，包级仍报成功 |
| `merge-check` | `4 package(s) intact` | 改名是**声明过的**，体哈希也一致——门允许声明式改名，却没检查改名后是否**仍是测试** |

**修法（不是补测，是补门的语义）**：`diffDecls` 增加 `test-name-mangled` 判定——基线名以 `Test`/`Benchmark` 开头者，其映射后的头文件对应声明必须仍以 `Test`/`Benchmark` 开头。自测 `TestMergeCheckRejectsRenameThatStopsBeingATest` 以「中和规则即 FAIL、恢复即 PASS、`cmp` 判 IDENTICAL、`grep` 残留 0」四步证明有效。

**波及面回扫**：三张既有映射表（root 73／agent 56／alias 8 条）按同一判据扫描，**无同类缺陷**——此前的批次都显式写了 `TestXxx` 全名；缺陷限于这次"用切片自动生成名字"的写法。再对全仓 216 个测文件扫描带 `*testing.T` 形参却非 Test 名且无人调用的函数：**0**。

**教训**：自动化生成的改名比手写更易吃掉语义前缀。凡"批量改标识符"的脚本，都必须让门校验**改名后的语义类别不变**（测试仍是测试、导出不轻易降为不导出），而不是只校验体不变。四域复验：强化后的门仍报 `4 package(s) intact`，四包测试 `ok`（task 1.8s／compress 0.4s／governance 0.5s／reliability 12.1s），两个被测锚 `-v` 下 `--- PASS` 可见。

### W2 · 其余七包结构收敛（100 → 47 个测试文件），并修掉切分器的块注释缺陷

| 包 | 测文件 | 说明 |
|---|---|---|
| `memory` | 34 → 18 | `segment_store` 一族按子职责三分（超 1500 行上限）；`package memory_test` 三文件独占；hold 1 个（你在写的 `compaction_safety_test.go`） |
| `tool/action` | 31 → 12 | `tmux_monitor` 一族二分；两个 `//go:build integration` 文件独占 |
| `event` / `plugin` / `tool/recall` | 7→2 / 6→3 / 7→2 | 按注册表与类型、memory 插件与投影、召回与记忆召回分组 |
| `rl` | 13 → 6 | 含死文件 `http_api_test.go` 原样保留（见下） |
| `evolution` | 6 → 4 | eval 一族合并，judge/gitrefine/switch_combo 独立 |

全仓测试文件 **401 → 159**。

**本批挖出的切分器缺陷（性质比前几批更严重：它会把"停用文件"变成"编译错误"）**：`rl/http_api_test.go` 在 HEAD 中把整文件体（含 import 块）包在一个 `/* … */` 里，首行是 `// TODO: Rewrite tests …`——即有意停用、待重写。`split_imports` 逐行扫描时把**块注释内部的 `import (`** 当成真 import 声明提取，并在 `render()` 里提升为文件头的活 import；于是活 import 对应的使用代码仍在注释内，编译器报 `"io" / "assert" / "agent" imported and not used`。

排查过程中我先后误判为"某源正文丢失"（用 `func` 计数 42＝42 否证）与"重复 import"（用单一 import 块否证），最后用 `grep -n '^\s*/\*\|^\s*\*/'` 直接看块注释开闭位置才定位——**教训：文本级工具必须先问编译器看到什么，而不是猜结构**。

修法三件：（1）`split_imports` 增加块注释感知（含文件头之前的块注释起算），实测该死文件现在提取 0 个 import；（2）该文件列入 `SKIP`，永不参与重写，还原后与 HEAD `cmp` 判 byte-identical；（3）预检新增**混包作用域**与**测试名同名**两项，另加 `verify` 子命令专查"源已并但未删"（本批我漏删 `rl/trajectory_trace_test.go` 造成 `TestTraceIDsFromCtx_Noop redeclared`，正是它该被机器拦住而非靠编译器）。

**门的最终读数**：七包 `gofmt` 净、`go vet`=0；空映射下 6 包 `intact`、`memory` 5 条全部来自你在写的 `compaction_safety_test.go`（`TestMergeScanError*`、`TestReplayEventSealedWindowDemotion`、`scanFailOnceKV*`），`missing-test = 0`；七包**顺序**测试全 `ok`（0.6～35.3s）。并发跑时 `tool/action/TestCommandParsing` 出现一次红，但在 HEAD 快照与当前态单跑均 `ok`（7.3～7.5s）⇒ 与 11.4 同族的环境敏感，不改被测代码、不放宽预算，留观察。

### 13.8 残留去编号（10 条），以及一次自我拆穿：root 的 explain 清单没入库

**改名 10 条**：`d52PullModel`→`plainTextPullModel`；`TestReconcileZombies_Channel2NilProbeOrphan`→`TestReconcileZombies_DeadSessionTrackerRetiresNilProbeOrphan`（连同其断言消息里的 `"channel 2"` 分支编号——兄弟测 `NilProbeSkipped` 才是真正的语义区分点）；`plugin` 4 条 `TestI1_*` 去不变量编号；`wp4Loop`→`envelopeInjectingLoop`、`wp4Post`→`postEnvelopeBody`；`TestActionTool33_*`→`TestMonitorsArePerGeneration`。保留项经逐条核为领域词汇（层级 `L1/L2/L3`、盘上版本 `V1/V2`、`MD5`、HTTP `401`、`Int64`、`e2e`、模型名 `Hy3`、场景实体 `sub2/sub3`、`30Round`、`80Percent`、`round3`）；`r30Stack` 仍被 hold 文件引用 4 次，按"被 hold 引用不改名"规则不动。五包 `go vet`=0、`go test` 全 `ok`（root 58.6s／task 1.9s／plugin 1.7s／rl 0.9s／action 37.3s），策略棘轮 0 beyond baseline。

**我拆自己的台**：用**空 explain** 复跑 root 时冒出 91 条 `body-changed`，而我先前报过 root intact。为免再次"凭猜"，我按判决性实验排查：
1. 先疑切分器新加的 `qualifierBlind` 回归 ⇒ 加环境变量中和该变换重编译，实测**关掉后违例更多（158）**，故非回归（探针用后 `cp` 还原、`grep` 残留 0）。
2. 再疑改名表级联（顺序替换把先写入的新名二次改写）⇒ 机器查 `new ∈ old 集合` 与"新名内含其它旧名整词"，**均为 0**，排除。
3. 最后落到真因：当初 root 读数的 `--explain` 清单**只写在临时路径、从未入库**，而我这次传的是空表——所以差异项正是那批"helper 归一/字符串值随改名"的已声明差异；但**这等于我先前那次读数不可复现**。

**处置（不掩盖）**：新增第 14 组任务——逐条重建并入库 `explain-root.tsv`（每条出具可核归类，真实差异必须修，不许以「已知」混入），并立规：**任何 `merge-check` 读数必须由"入库的映射表＋explain 表"二元组完整复现**才算证据；在重建完成前 D1/W1 不放行归档与提交。这比多改几个名字重要：门的证明力来自工件可复现，而不是我说过 intact。

### 14 组补证：root 的 91 条假差异，根因是我把 agent 的别名表 cp 进了 root 表

追查链（每步以实验否证/证实，而非推测）：

| 假设 | 实验 | 结论 |
|---|---|---|
| 新加的 `qualifierBlind` 回归 | 环境变量中和后重编译计数 | 关掉反而更多（158）⇒ 否 |
| 改名表级联（新名被后规则二次改写） | 机器查 `new ∈ old` 与「新名含其它旧名整词」 | 均 0 ⇒ 否 |
| **表被跨域污染** | `grep -nE '^(agent\|event\|upagent)\t' rename-map-root.tsv` | **命中 3 行** ⇒ 真因 |
| 差异实质 | 新增 `--diff-out` 导出两侧归一文本逐条读 | base 侧 YAML `kind: agent` 被改写成 `kind: trpcagent` |

**两层错误叠在一起**：（1）我把累计的临时映射表整体 `cp` 成 root 表，带入 3 条 agent 域别名项；（2）更糟的是当初我没有追这 91 条的来源，而是**写了一份 explain 清单把它们登记放行** —— explain 被用在了它最不该用的地方（掩盖工具输入错误），而那份清单还没入库，于是读数不可复现。两者均已记入 14.1/14.2。

**重建后的 `explain-root.tsv`（13 条，逐条行内理由）**：9 条为子进程 `-run` 过滤器字符串必须跟随测试改名（留旧名则子进程匹配不到测试、虚设通过，正是 `runBootChild` 那条断言要防的）；1 条为固件取值随 helper 改名；1 条为有意加固（断言只增）；2 条为接收者类型改名带来的方法体文本差异。**复现**：`merge-check --map rename-map-root.tsv --explain explain-root.tsv .` → **`1 package(s) intact`**。

**顺带修掉的两处工具真实缺陷**：（1）`applyRenames` 是裸文本替换，会改写字符串/raw/字符字面量内容 ⇒ 改为字面量感知（新增 `splitOutsideLiterals`），先红后绿（两条新测在旧实现下双红）；（2）`--diff-out` 首版查头侧对应 decl 时误传空映射，10 条只导出 3 条 ⇒ 修参数后逐条可读。

**其余批次复验（同一工具、入库表）**：agent 四子包 `intact`、W2 六包 `intact`、root `intact`；`agent` 包剩 21 条已逐项归属（3 条 = 11.4 声明式测体改动、1 条 = 你在改的 `TestReliableBus_FixedSlotsNotCompacted`、17 条 = 你未跟踪新测文件；`missing-test = 0`）。由此暴露的方法问题记为 14.5：**门刻意不许 explain 免除测试体差异**（防以解释代修改），故每批须独立提交、门对该批父提交跑；未提交期间的跨批差异只能记台账。

正向不变量确认：root 包内**已无任何含编号的测试名字符串**残留（过滤器全部跟随改名）。

### 16 组收口：explain 的充分最小性，以及一次被自己规则拦下的污染输入

**门证据定义的一次正确性收敛**：`merge-check` 的体哈希原先是"打印文本"，而**改名本身会改变排版**（标识符变长 ⇒ gofmt 把单行函数体折成多行、const 块对齐列宽变化）。这类差异与语义无关，却计入 `body-changed`（实测 root 10 条、rl 1 条）。改为对**词元流**判等（`foldLayout`）后这些自动消解。三项先红后绿：`TestMergeCheckIgnoresRenameInducedReflow`（旧实现红）、`TestApplyRenamesIgnoresStringLiterals`/`...RawAndRuneLiterals`（字面量感知，旧实现双红）、`TestMergeCheckFollowsReceiverTypeRename`（守卫既有行为）。

**`map-lint` 当场抓到我自己的新错**：把 residual 整表并进 root 表，带入 8 行别包标识（`TestI1_*`、`wp4*`、`TestActionTool33_*`、`TestReconcileZombies_*`）。判据形状有两类：`STRAY`（该名字在包内基线根本不出现）与 `ALIAS?`（old 恰为某导入包名）。已按包拆表，root 回到 71 行自有项，七张表全过 lint。

**explain 最小性的三连验（全部用入库输入）**：

| 输入 | 读数 |
|---|---|
| 真正空的 explain（新建零字节文件） | 13 条 `body-changed` |
| 入库 `explain-root.tsv`（13 条，逐条行内理由） | **`1 package(s) intact`** |
| 抽掉任意一条 | 回弹 **1** 条 |

三个读数单调自洽 ⇒ 13 条既充分又必要。

**自纠记录**：我一度据"空 explain 只报 3 条"把清单精简到 3 条，随后精简表反而报 10 条——违反单调性才暴露：我当作"空"的 `/tmp/ex_empty.txt` 里**残留着早先命令写入的 10 个名字**。若没有"explain 必须单调抑制违例"这条常识性检验，我会把一份被污染输入导出的**弱化清单**留下，那正是能掩盖未来回归的东西。新规补进 14.3/16.3：门的对照输入必须现场新建，能入库的输入一律从仓库取，不复用临时路径。

**全域最终读数**：`.` 与 9 个包 `intact`；`agent` 21、`agent/task` 5、`memory` 5 全部逐项归属（11.4 声明式测体改动／你在改的文件／你未跟踪的新测文件），**`missing-test = 0` 全域成立**；`go test .` ok（59.2s），`go build ./...` = 0。

### W2 尾批：最后 5 个包（40→27 个测试文件），四包真空表即 `intact`

| 包 | 测文件 | 目标依据 |
|---|---|---|
| `tests` | 22 → 13 | 异步结果、计划 agent、真实模型契约、集成、常驻 e2e 各一族；`soak_test.go`（`//go:build soak`）独占 |
| `memory/engine` | 7 → 5 | bridge 一族三分（含 idempotency、review-fixes）；contract/inmemory/persist/diagnostics 各自 1:1 |
| `prompt` | 3 → 2 | loader 与 fallback 同职责；source 独立 |
| `tool/mcp` | 3 → 2 | 调用与熔断同职责；registry 独立 |
| `examples/wechat-bot` | 6 → 5 | main 的两份装配测并入 `main_test.go` |

已 1:1 的包（`memory/embedder`、`memory/kv`、`tool/knowledge`、`tool/memoryx`）**明确不动**——合并它们没有单职责收益，只是 churn。

**门读数**：四包以 **真空映射＋真空 explain**（现场新建零字节文件，不用任何复用路径）报 `intact`；`go vet` 与 `gofmt` 干净；`memory/engine` 2.7s、`prompt` 2.0s、`tool/mcp` 0.9s、`wechat-bot` 0.9s 测试全 `ok`。全仓跟踪测试文件 **401 → 140**。

**检查器自纠一处**：预检把同一文件内的 Test 名在两条规则里各计一次，误报 `dup-decl`（`seen[n] == s_`，与自身相撞）。修规则后五包预检全清。这类"假冲突"若不清，会诱导对合法合并做无意义改名。

**两条 FAIL 的归属（先判性质再谈因果）**：`tests` 包报 `TestPlanAgentCreateBehavior_RealPrompt`、`TestRealLLM_PlanReentry_ClarificationLoop` 失败。在 HEAD 快照上单独复跑：前者同样 FAIL、后者在基线 FAIL 而在工作树 PASS ⇒ 二者打真实端点（`glm-4.7` @ bigmodel.cn），属**非确定性真实 API 测试**，与本批改动无因果关系，也不堪任门信号。据此提两项待你裁决的范围（17.5、17.6）：真实 API 测试是否收进显式构建标签；以及 `examples/wechat-bot/.gitignore` 以 `!xxx_test.go` 白名单决定哪些 example 测试入库——本批涉及的 `main_gate_test.go`/`main_delivery_target_test.go`/新 `main_test.go` **均未被跟踪**，故该包不在 CI 可见范围、门也无从证明（内容未丢，全在合并后的文件里）。

### G 首批 · `prompt`：一处真实缺陷、一次文档补写、一次门盲区自纠

**逐文件通读的直接产出是一个产品缺陷**（这类发现正是"必须读码"而非"只跑工具"的理由）：`prompt/source.go` 的 `checkModTimes` 里

```go
lastMod := s.modTime          // 上次成功载入的时刻（读出来了）
...
if mt.After(latestMod) {      // 却用 latestMod 作比较基准
        changed = true        // latestMod 是具名返回值，初值为零时刻
}
```

`latestMod` 初值为零 ⇒ 任何真实文件 mtime 都"晚于"它 ⇒ `changed` 对非空文件列表恒真 ⇒ **`Source` 的缓存永不命中**，而 `Get()` 位于每回合 `BeforeModel` 热路径上。构造两向边界测确认（文件比上次载入更旧仍报 `changed=true`，`Get()` 返回重读结果而非缓存），修基准为 `lastMod` 后两测转绿；`TestSource_HotReload` 仍 PASS，说明变更检测未被削弱；依赖方 `./agent` 全量 ok 44.7s。

**顺序：先补文档，再删注释。** `docs/wiki/prompt/prompt-architecture.md` 新增「缓存与降级契约」六情形表、把"为何比较基准必须是上次载入时刻"写成防回归说明、补「Getter 缝的存在理由」及其例外；顺带清掉文档里我自己的迭代标记（`TC0`、`C6 遗产`）。之后 `getter.go`/`source.go` 的注释才收敛为两形态（契约 go doc ＋ `// 契约:` 索引）。验收看最终产物而非过程：`go doc prompt Getter`、`go doc prompt Source.Get` 输出已是纯契约文本。

**门等价性**：`comment-check`（基线取修复批后的快照）报 `2 file(s), code identical under comment strip`；`gofmt`/`go vet`/`go test ./prompt` 全清；策略计数 95 → 85（`loader.go` 与两测文件留待下批，未读不改）。

**门盲区自纠（又一次先红后绿）**：这次是 G 门自己。字段与常量的**行尾注释**存在 `Field.Comment`/`ValueSpec.Comment` 槽，而 `clearComments` 只清 `Doc` ⇒ 我改一条字段注记被判 `CODE-CHANGED`。补 `TestCommentCheckStripsFieldComments`（旧实现 FAIL、补槽后 PASS）。若无这条测，我很可能把正确修改当成"代码被改"回退掉，或干脆把注释重新塞回去骗过门——两者都会让门的证明力失真。

**方法学**：本批再次说明"注释只保留两种形态"的收益不是美观——被删掉的那类注释（设计史、步骤旁白、任务编号）恰好掩盖了上面那个热路径缺陷：`changed` 的判定既无契约说明也无不变量约束，写错的比较基准便无人察觉。

### G 首批续 · `prompt/loader.go`：删掉的是"改动履历"，留下的是契约

通读 404 行后的处置分三类：

| 被删形态 | 例子 | 去处 |
|---|---|---|
| 外项目血统标注 | `Aligned with nanobot's BOOTSTRAP_FILES pattern` | 不进文档也不留代码：序列本身已由 `BootstrapLoadOrder` 承担真源 |
| **改动过程叙述** | `LoadFiles` 文档 8 行："previously LoadFiles hard-failed … could not start from a clean checkout" | 文档「`LoadFiles` 为什么容忍缺文件」段（保留**动机**，删掉**历史**） |
| 步骤旁白／行内"为什么" | `// Collect .md files`、`// Sort for deterministic order`、`// 1. Inline prompt`、5 行 optional-file 说明 | 契约要点上移到方法 doc 或文档表；纯旁白直删 |

**顺序仍是先补文档**：`docs/wiki/prompt/prompt-architecture.md` 增「加载契约」表（`LoadFromFile`/`LoadFiles`/`LoadFromDir`/`LoadComposite`/`LoadBootstrap` 逐入口）＋缺文件宽恕的动机段（含"info 日志使拼错必需文件名不被静默吞掉"这条容易被忽略的取舍）＋`BootstrapLoadOrder` 为序列唯一真源。

**门与读数**：`comment-check --base-root <修复批快照> --head-root . prompt/{getter,source,loader}.go` ⇒ **`3 file(s), code identical under comment strip`**；`go vet ./prompt`=0、`go test ./prompt` ok 1.685s；策略发现 95 → **57**（余量在两个测试文件，18.6 处理）、0 beyond baseline、8 槽可下调；`go doc prompt Loader` 已输出纯契约文本。

**我本轮自己写坏一次并当场抓住**：替换文本里 `\t` 多转义一层，Go 源码出现字面反斜杠，`go vet` 立刻报 `illegal character U+005C`（不是我肉眼看到就放行）。修回后 gofmt/vet/test 全清。

**通读中另记一处可简化**（18.8，不改行为故不入 G 批）：`LoadBootstrap` 在 `errors.Is` 之后仍用 `strings.Contains(err.Error(), "no such file")` 匹配错误文本；`LoadFromFile` 以 `%w` 包装 `*PathError`，`errors.Is` 已覆盖，字符串分支是冗余兜底。删除属代码变更，须先以测证明两向等价。

### `prompt` 包 G 完成：策略发现 **95 → 0**（首个生产＋测试全清的包）

**测试文件的处置**（读完 534＋194 行之后）：删 47 处体内旁白（`// Test loading file`、`// Third read — should detect change and re-read`、`// Verify order (alphabetical)` 一类——即测试名与断言已说明的东西）。两条信息**换形态保留**而非丢弃：文件系统 mtime 粒度通常 1 秒（`TestSource_HotReload` 为何要等待），「清单全部缺失返回空内容且不报错」写进该测的 doc。我自己早先写的 `Fail-before: …` 履历整段删除——它记录的是改动过程，不是契约。

**两处由规则倒逼出来的正确结构**：
1. **`prompt/doc.go` 新建**承载包契约（此前整个包没有包注释）：一句话说清：从磁盘读、按固定顺序拼接、磁盘未命中回退内嵌、文件是唯一真源。
2. **`// 契约:` 索引行不在豁免清单内** ⇒ 职责声明必须挂在 doc 槽上，于是两个测试文件各自主测的 doc 变成"契约陈述＋索引行"，而不是随手贴一条游离注释。这条规则按其设计意图生效了。

**又抓到工具（不是我代码）的一处规则错**：`missing-package-doc` 按文件判定，导致有 `doc.go` 的包把其余每个文件都报一遍；照它改就得写重复包注释（go 工具本身不允许）。红测 `TestPackageDocIsPerPackageNotPerFile`（先引用尚不存在的函数 ⇒ 编译失败即红）→ 实现 `dropRedundantPackageDocs` → 工具自测全绿。全仓计数 6546 → 6362。

**读数**：`prompt` 策略发现 **0**（95 → 57 → 10 → 7 → 2 → 0，每一步都由门重新列举剩余项驱动，不是我按感觉清理）；`comment-check` 对 5 个文件报 **`code identical under comment strip`**；`go vet ./prompt`=0、`go test ./prompt` ok 1.80s；`gofmt` 净；全仓 `0 beyond baseline`。

**我本轮的操作失误也记在这**：批量删注释的脚本用"行必须整行是注释"做断言，遇到 `source.go` 的**行尾注释**时按预期中止（没有整行删掉代码），我把该处改为只剥注释文本；另一次因抄写 `old` 文本漏了"目录内其余"而断言失败——门与断言都在替我把关，不是靠我细心。

### G · `event` 首批：先量文档覆盖度，再决定搬什么

**方法**：动注释前先用关键词逐项核对现态文档 `docs/wiki/event/event-architecture.md` 对**每条待迁移事实**的覆盖（12 项探测），发现 7 类缺口，而不是笼统"把注释搬进文档"。据此新增「十二、类型注册表与投影、保留契约」5 小节：注册表为静态属性唯一权威源（含字段零值语义表与非投影单一入口）、时间线前缀写读同包、内部记录类型的保留不得被缩短（`wf.*` 引擎已撤回、`TTLDays` 必须为 0，否则静默缩短历史保留；`inbox_receipt` 的 TTL 就是 30 天去重窗口）、`source_snapshot` 为何集中一个保留键。

**完成范围**：`timeline.go`、`wf_facts.go`、`inbox_receipt.go` —— 删 12 处任务/审计编号（D2、D4、D6、F5、R05、§5.5 等）与英文旁白，改契约陈述＋`// 契约:` 索引；三件在策略下**归零**。`event` 计数 88 → 74，余量集中在 `types.go`（118 注释行）、`registry.go`（100）、`metadata.go`（64）与两个测文件，留 20.5 按同一办法做，**不为降数字而删**。

**门**：`comment-check --base-root <git archive HEAD 的 event> --head-root .` ⇒ **`3 file(s), code identical under comment strip`**；`go vet ./event`=0、`go test ./event` ok、`go build ./...`=0。

**一条规则学来的东西**：`mechanism-narrative` 连着两次判定「先成为事实，再产生效果」是步骤旁白（我先在行注释写、后挪到块 doc 仍被拦）。它没错——那确实是过程描述。正确处置不是换同义措辞骗过它，而是**把这条不变量写进文档**（十二节的 `wf.*` 条目），代码里只留"事实链是唯一真源"这类性质陈述。规则在替我守住"过程 vs 契约"的界线。

**我自己的两处操作问题**：（1）新增小节时把既有 `## 已知缺口与演进方向` 标题顶掉了，读文件时发现并当即补回；（2）批量替换里一条 `old` 未命中被脚本如实报出（未静默跳过），核对后发现是同一条注释的另一换行形态，已在结果文件里确认无残留英文旁白。

### G · `event` 生产六件全部完成（88 → 34），并修掉 `comment-check` 的布局敏感

**做法**：对 `types.go`/`registry.go`/`metadata.go` 采用**机械剥离＋锚点回插**——先剥掉全部注释（295 行），再把该说的契约逐条插回。剥离前先做安全探测（确认无字符串内含 `//`、无构建指令），插入时每个锚点都 `assert count==1`，因此不存在"顺手改到代码"的余地。

**先补的文档**（十二节新增三小节）：12.6 元数据键的归属与注入点（含"跨包复制字面量会静默漂移，漂移使取证侧拒绝计数归零、快道回滚防线失效"）、12.7 每个内置类型的存在理由（表）、12.8 摘要与命名澄清（`GenerateEventSummary` 不是内容摘要、工具结果 JSON 提取字段以防嵌套转义等）。另发现 HEAD 里 `types.go` 与 `metadata.go` **各带一份包注释**（重复包注释），统一收敛到新建的 `event/doc.go`。

**门的一处真实缺陷（与 merge 门同源）**：剥离注释后 `registry.go`/`metadata.go` 被判 `CODE-CHANGED`。我用工具取实证而非猜测：差异是 `Meta	map[string]string` 与 `Meta		map[string]string` —— **字段前导注释被删会使相邻对齐组合并，printer 遂多插一个 padding tab**，词元完全相同。`comment-check` 原先比较打印文本，即布局敏感；修法与 merge 门一致：比较前 `foldLayout` 折叠空白。

**真实文件双向验证**（关键，而不是造夹具）：

| 方向 | 做法 | 结果 |
|---|---|---|
| 正向 | 6 个 `event` 生产文件 vs `git archive HEAD` | `6 file(s), code identical under comment strip` |
| 反向 | 在 `registry.go` 真改一个词元（加 `&& name != ""`） | `CODE-CHANGED`，RC=1 |
| 还原 | 恢复原文件再测 | `1 file(s), code identical`（探针无残留） |

**我把一条虚设测删了**：为这个缺陷我先写了合成夹具测，三次改写（空格对齐 → 制表符 → 结构体字段注释）**每次都通过**，说明它从未复现失败、不构成证据。已删除，换成对 `foldLayout` 的直接单测，并显式断言「原始文本必须真的不同」与「真实词元变化必须存活折叠」，防止空断言。真实覆盖以上表的双向验证为凭——**这比留一条看起来在测、实际恒过的测更有价值**。

**读数**：`event` 88 → 34（余 `registry_test.go` 24、`types_test.go` 10）；`go vet ./event`=0、`go test ./event` ok、工具自测 ok、0 beyond baseline。`go build ./...` 当前失败在 `agent/meditation.go`（`m.auditLine` 未定义，你的在飞编辑），按既定指示不代改、留待你落地后复验。

### `event` 整包归零（88 → 0），并记下一次"用错基线"的误报

**测试文件的收敛**：删掉表格列注（`special bool // IsSpecialEventType` 一类，其信息由紧邻断言与错误文本承载）、步骤编号旁白、以及带归档坐标的叙述（`§5.5`、`R05/6.2`、`T-D/T-G/D1/R2/R3/RRP`、`fail-before：修复前 TTLDays==30`、`archived 7.6`）。被删的内容里属于**不变量**的都先落进文档十二节（含 fuzz 深挖命令的去处），代码里只留"钉住什么契约"的陈述并加 `// 契约:` 职责索引。

**一次声明式改名**：`TestRegistryDerivedSetsMatchLegacy` → `...MatchDeclaredTable`。`Legacy` 触发 `audit-marker`（策略语义是"离开归档就读不懂的编号/履历"），而改名属代码变更，故按声明式走：映射入库 `rename-map-event.tsv`、`map-lint` 通过、`merge-check` 对 HEAD 仍 `1 package(s) intact`（`missing-test = 0`）。

**本批最有价值的自我纠正——基线用错**：我给两个测试文件跑 G 门时用了 `git archive HEAD` 作基线，得到 2 处 `CODE-CHANGED`。若按"降低标准"处理就会把真问题混过去；实际取证发现差异是**上一批 T 的内容并入**（HEAD 的 `registry_test.go` 还没有 `require` 导入与另外几个测试函数），与本批注释改动无关。改用"T 批之后的快照"作基线后立刻 `code identical under comment strip`。

规则由此明确并入库（20.10）：**未提交期间每批开工前必须固化该批基线快照，跨批不得复用旧快照**；一旦分批提交，就一律改用"该批父提交"作基线。这正是 14.5 的按批设基线在实操中的具体形态。

**最终读数**：`event` 策略发现 **0**（起点 88）；`go vet ./event`=0、`go test ./event` ok 0.33s；9 个棘轮槽可下调（留待 W4 与你的并行改动一起定基线，避免吸收你 `agent/*` 的 +42）。

### G · `plugin` 生产四件归零（83 → 40），新基线纪律第一次就跑通了

**纪律生效**：上一批我在测试文件上用错基线（拿 HEAD 当 G 基线，误报 2 处 `CODE-CHANGED`），并据此写下 20.10「每批开工前固化该批基线快照」。本批第一步就固化 `/tmp/gb_plugin/plugin`，随后剥离 200 行注释、回插契约，`comment-check` 对四个生产文件一次报 **`code identical under comment strip`** —— 规则的实际收益，不是套话。

**文档先行**：读码后核出现态 `plugin` 篇缺 5 类契约（精确回显凭据、四道跳过闸、归因章双持久化路径、因果链上界与复活语义、伪造 `[evt_...]` 前缀的存储边界），新增「十五、存储管线的契约」把它们写成表与条目——包括两处容易被忽略的**取舍正当性**：仅图无文的输入内容相等也会匹配空 user 事件（不跳就会双写循环已提交的合并图像输入，多跳者本身无信息），以及绑定身份只能是根调用 id（框架 `event.Event` 不携带逐事件 id）。

**一条被采信的工具提醒**：剥完后 `missing-symbol-doc` 报 `NewMemoryPlugin` 无注释。该规则对导出函数强制（导出常量不强制），我补回函数文档而不是放宽规则——避免"清理注释"变成"删掉本该存在的契约"。

**读数**：`plugin` 83 → 40（**生产归零**，余三测试文件：`memory_plugin.go` 系 29 条、`projection_sink_test.go` 6、`attribution_test.go` 5，内含 `§4.4/§4.5/§4.7/D8/H1/L1/R2/TC0/I2` 等归档坐标）；`go vet ./plugin`=0、`go test ./plugin` ok 0.67s、`go build ./...`=0；`go doc ./plugin EchoCredential` 已是纯契约渲染。

**两包归零的累计形态**：`prompt` 95 → 0、`event` 88 → 0、`plugin` 生产 83 → 40（余测试）。方法已固化为：先读本批全部文件 → 用关键词核文档覆盖 → 补缺口 → 机械剥离＋锚点回插 → 用本批基线快照过等价门 → 策略计数与棘轮复核。

### `plugin` 整包归零（83 → 0），以及一次"混批"被门当场抓住

三测试文件读完（567＋158＋49 行）后按同一方法收敛：删章节横幅、步骤编号、体内 `SAFETY:` 叙述与全部归档坐标（`§4.3/§4.4/§4.5/§4.7`、`D1/D8/H1/I2`、`implementation-hardening 5.3`、`resident-remaining-hardening 3.6, archived 7.6`、`unified-event-projection D1/D4/D8`）。其中"淘汰只能把链降级为无父、绝不错接前驱"这条**承重契约**先补进文档十五节，再从测试注释里删除；新节序号也补成「十五、…」以延续文档编号。

**最有价值的一次自我纠正**：我在清理注释时顺手改了 2 处**断言消息**与 1 处**日志文本**里的 `§4.5/§4.7/§4.4`。断言与日志里的字符串是**代码**，`comment-check` 立刻 `CODE-CHANGED plugin/memory_plugin{,_test}.go`。

处置没有走"放宽比较"或"塞进 explain"，而是**把两类变换真正拆开**：

1. 先把 2 处消息改回原样 ⇒ G 批恢复纯净：`comment-check` 对其余 5 个文件报 **`5 file(s), code identical under comment strip`**；
2. 再把"用户可见文本去归档"作为**独立变换**重做（含 `log.Debugf` 里那处），并用门自己的 `decls` 视图证明**每个函数的断言条数不变**（`断言数变化的函数: 无`）——覆盖强度未动，只是失败/日志文本变得可独立读懂；
3. 最终读数分列陈述，不合并成一个"全绿"：纯注释部分 code-identical；文本部分为声明式改动，其 `intact` 需等按批提交（14.5）后才成立。

由此立规（21.9）：**「面向读者的文本携带归档坐标」是第三类变换**（改代码不改行为），与 G（注释）、T（结构）并列；三类必须能被门分别承认，否则我们会用"注释批"的名义夹带代码改动。

**读数**：`plugin` 策略发现 **0**（83 → 0），`go vet ./plugin`=0、`go test ./plugin` ok 0.64s，`plugin` 目录内 `§`/`TC0`/`R05`/`fail-before`/`archived` 残留 **0 个文件**。三包累计：`prompt` 95→0、`event` 88→0、`plugin` 83→0。

### G · `tool/recall` 前两件：把"防线"从注释搬成文档条目

读 `memory_recall.go` 全文后判断：这里的注释**不是废话**，而是三条安全/正确性防线的唯一记录处——入参上界（`maxRecallLimit`/`maxRecallItems`）、引擎超取补偿（`limit*2` 后裁回，防死键占 topK 静默少返回）、**水合前按 `EventKey` 高位过滤分区**（持久化/重建链路一旦缺 pid，只靠存储侧过滤会跨命名空间泄漏）。因此顺序是：先把它们写成文档「十六、`memory_recall` 的检索、降级与诚实回报契约」（含每条防线的**理由**，以及"零结果不等于没有历史""miss 必须逐条标注""orchestrate 未接线不得静默降级"这些模型可见语义），再删代码注释、只留契约陈述与 `// 契约:` 索引。

顺带去掉的坐标类：`T-A`、`T-B 5.1`、`implementation-hardening 3.4`、`审查 M2/S4/Nit10`、`D7`、`（observed in production）` 的过程口吻。

**门读数**：`comment-check`（本批基线快照）⇒ **`2 file(s), code identical under comment strip`**；`go vet ./tool/recall`=0、`go test ./tool/recall` ok 0.36s；`tool/recall` 69 → 58（余 `recall_agent.go` 18、`recall_subtools.go` 16、两测试文件 24），0 beyond baseline。

**一次被断言拦住的批处理**：首轮脚本把某段注释结尾的「）。」误写成「.」，`assert count==1` 当场失败 ⇒ **文件未被写入**（计数仍 69 即证据）。改成"先干跑校验所有锚点命中数、再一次性写入"后成功。这是第 N 次印证：批量文本修改的安全性来自**断言前置**，而不是我抄写准确。

### `tool/recall` 整包归零（69 → 0）：改用"门自己的清单"驱动清理，并修掉一条误判规则

**方法升级**：上一批我手抄锚点，两次因一字之差被断言拦停（文件未被写入）。这一批改用**门输出的 (文件, 行, 文本) 清单**驱动，并按两类语义处置——
- **字段尾注**（`Model model.Model // Required: ...`、`// Default: 5`）：信息属于契约，不能一删了之 ⇒ **原位转成字段 doc 槽**（`recall_agent.go` 9 处、`recall_subtools.go`/`memory_recall_test.go`/`recall_test.go` 若干），go doc 照常渲染，规则也不再判为游离；
- **体内旁白**：直接删；多行注释的**续行**残留由迭代轮次收敛（三轮分别处理 15/3/2 处，直到零命中）。

**文档先行**：新增「召回子工具的读回语义」，把只存在于注释里的两条硬语义写成规范——**断链即止**（回溯遇缺即停、保留已取部分，只有首事件取不到才报错；绝不跨断点猜父链）与**整轮重建以 `external_input` 为界、输出反转为时间正序**（否则会把跨轮事件拼成一轮）；另补「子 agent 注册时记忆存储必须同源，否则刚写入的事件召不回」。`truncationHint` 文档去掉 `2026-07-31` 事故日期，保留推理本身。

**修掉一条工具误判（红→绿）**：`docPathRef` 的 `[\w/.]+\.md` 会把运行时提示词资源名 `recall_agent.md` 当成文档引用报 `unindexed-path-ref`。红测 `TestDocPathRefIgnoresRuntimeAssetNames`（裸 `.md` 不命中、`docs/…md` 必须命中）→ 判据收紧为「须含路径分隔符」→ 工具自测全绿。同一批里另外两处命中（事件日期、测试缺职责索引）经核**合理**，是照实改文本而不是放宽规则——分不清这两者，棘轮就会退化成摆设。

**门读数**：`comment-check` 对本包全部 6 个文件 ⇒ **`6 file(s), code identical under comment strip`**（含尾注转 doc 的位移，仍是纯注释变更）；`go vet ./tool/recall`=0、`go test ./tool/recall` ok 0.28s；`tool/recall` 策略发现 **0**。四包累计 `prompt`/`event`/`plugin`/`tool/recall` 全 0。

**本轮两处非绿我都不背、也不代改**：全仓 `REGRESSION missing-symbol-doc 138→139` 经定位是**你未跟踪的新文件** `agent/telemetry_audit.go:50`（`SelfTelemetryAuditor` 无 doc）；`lint.sh` 的 gofmt 失败同样落在 `agent/telemetry_audit*.go`（`??` 状态）。另有一处既存问题 `agent/compress/context_compressor.go:272` 的 `MarkMeditationKey`——说明文字错挂在别的声明上，属该包 G 批处理，不跨包顺手改。

### `memory/engine`：三件生产归零，以及一次"未读先改"的回退

**文档增量**（十七节）把只能存在于注释里的设计约束升为规范：装饰器**顺序**契约与递归透传能力清单（漏透传＝外层能力静默变 nil）、"已提交"回放不得二次计数、自带向量路径**不经引擎索引**的一致性理由、向量随 KV 存**模型指纹**以跳旧、诊断**只读实时状态**（平行计数器必然与真实分叉）、`SearchByEmbedding` 的**全库检索风险**、`Close` 只是委托腿不持共享释放权；另单列进程内引擎的四条退化规则与 **RRF 关键词腿偏向新近**这一 MVP 取舍、重建不入等待组以保 `Close` 有界、排空必须用不继承取消的独立 ctx。

**我犯了一次流程错并回退**：机械清理脚本的 TARGETS 误含**我没读过**的 `engine_inmemory.go`，扫掉 24 行注释。处置不是"大概没问题"，而是：

1. `diff <(git show HEAD:…) <文件>` 把**被删注释逐条**取出核对；
2. 确认其中有真实不变量（Close 有界、排空 ctx、RRF 偏向新近、topK 下限、KV 失败不传染）→ 全部补进十七节；
3. 把该文件**还原到本批基线**（`cmp` 判 byte-identical），留待读完后的下一批按正规流程重做；
4. 立规（24.6）：机械pass只允许作用于**本批已通读**的文件，批前断言文件在读清单内，每轮打印被删行供复核——机制不应依赖我事后想起去 diff。

**门读数**：`comment-check`（本批基线快照）⇒ **`3 file(s), code identical under comment strip`**；`go vet ./memory/engine`=0、`go test ./memory/engine` ok、`go build ./...`=0；包计数 119 → 84（其中 38 属已还原的 `engine_inmemory.go`、46 属五个测试文件），0 beyond baseline。

顺带清掉文档里一处迭代标记残留：`## 十五、记忆策展（unified-memory-curation）` → 去掉归档变更名；新节按序编号为十七。

### `memory/engine` 生产四件归零：一次"未读先改"的完整回退，与一处过期注释的清除

**按新流程重做 `engine_inmemory.go`**：先通读 653 行，再让机械pass只接受**已通读文件清单**，并**每轮打印全部被删注释行**（本轮 24 行逐条核对，确认每条内容都已在十七节落地），最后一次性清除编号与横幅。

**读到一处与实现矛盾的过期注释**：横幅声称"MVP 局限：向量索引内存态、不持久化，重启后旧事件向量丢失"，而同文件的 `EngineConfig.KV` 与 `engine_persist.go` 已实现"KV 持久化＋启动重建"。这类注释比没有更坏——它会误导下一次改动（例如据此认为无需处理旧向量指纹）。处置：内容以准确形式并入文档（持久性取决于是否配置 KV），注释删除。

**信息没有丢**：结构体字段的尾注（默认值、可空语义、指标含义）**原位转成字段 doc**，`go doc` 仍能看到；被删的只是重复代码语义的步骤旁白与归档编号。

**顺带一个工具性发现**：`go run ./scripts/comment_policy` 的输出在管道缓冲区边界**截断多字节 UTF-8 字符**，令按文本模式读取的子进程抛 `UnicodeDecodeError` 中断（文件未被破坏——`gofmt`/`vet`/`test`/门随后全通过）。已记入 24.6：批处理脚本必须按字节捕获、整体解码。

**门与读数**：`memory/engine` 生产四件（＋新建 `doc.go`）策略发现**逐个归零**；包计数 119 → 46（46 全在五个测试文件）；`comment-check` ⇒ **`4 file(s), code identical under comment strip`**；`go vet ./memory/engine`=0、`go test ./memory/engine` ok 2.54s、`go build ./...`=0。

### `memory/engine` 整包归零（119 → 0）：三类变换分开举证

先通读五个测试文件（882 行）再动手，落实上批立下的 24.6（机械pass只接受已通读文件、并打印全部被删行）。**T/G/第三类**在本包分别留下独立证据：

- **G（纯注释）**：删体内旁白 45 行、尾注转字段/常量 doc 10 处、五个文件各补一条 `// 契约:` 职责索引，并清除 doc 槽里的 `§2.7①/②`、`C6`、`T-A`、`审查 M1/M2/M3`、Snowflake 纪元日期字面量。门 ⇒ **`5 file(s), code identical under comment strip`**。被删行全部打印并逐条复核：其中两处契约级表述（「未接线行为逐字节不变」「未就绪退化为关键词而非报错」）确认已被十七节覆盖，不是丢信息。
- **第三类（读者可见文本，改代码不改行为）**：`engine_bridge_test.go` 有 **6 处断言消息**携带 `§2.7①/②`、`M1:`、`M2:`、`M3:` 坐标——字符串属代码，纯注释批的门不允许它混进来，所以单独一步做，并把「修复前会丢」这种履历口吻也去掉，改成能自解释的断言语义。证据：该文件**断言计数 29 → 29 不变**、全包读者可见文本坐标残留 0、`go test` ok。
- **文档增量来自读码**：核对时发现「**索引按 EventKey 幂等**」（同一 key 重复投递只覆盖那条向量、不产生第二个逻辑条目，检索因此不会返回重复票据）被测试钉住却没进文档 ⇒ 先补进十七节，再清理相关注释。

**读数**：`memory/engine` 策略发现 **0**（生产四件＋`doc.go`＋五个测试文件全部归零）；包计数 119 → 0；`go vet`=0、`go test ./memory/engine` ok、`go build ./...`=0、全仓 0 beyond baseline。至此已完成 G 的包：`prompt`、`event`、`plugin`、`tool/recall`、`memory/engine`。

### `memory/kv` 整包归零（61 → 0）：一次手删越界被棘轮精确指出，外加两条真实缺口

**方法**：先通读 4 个文件（1127 行）再动，机械pass只接受已通读清单并打印全部被删行（55 行逐条核对，内容均已在十八节）。删横幅残段与 `VectorInsert` 履历改用**行号＋前缀断言**，因为旧全文已被上一轮删成残段、拿旧文本再锚定必然失败（该次断言失败也确实拦住了整批写入，文件未被破坏）。

**十八节（文档）**承担了这类后端最容易被误读的东西：`LocalFileKV` 是**故意的临时模型**——无 fsync，因此挺得过进程重启、挺不过操作系统掉电，`KVPut` 返回 nil 不是持久保证；`WithFSync` 接受但忽略；分区发现靠"键命名空间里存在任意键即证明分区存在"，不另建清单；rustviking 的 null value 必须翻译成**类型化未命中**（否则进程启动失败会被读成"键不存在"）；`KVRange` 无公共前缀直接报错，绝不退化成隐式全库扫描；真实向量命令是 `index *`（`vector *`/`embed` 是历史虚构契约，正是检索长期退化为 stub 的根因）。

**我自己的一次越界**：删游离横幅块时，把紧邻的 `VectorResult` doc 一起吞了 ⇒ `missing-symbol-doc` 精确指到那一行，补回归零。规则并入 24.6：**人工删块与机械pass同等对待，必须打印被删行**——不能因为"这是我手写的判断"就省掉复核。

**读码挖出的两条真实缺口（都不在注释层，也未擅自修）**：

1. `WalQuarantined`：`grep 'func .*) WalQuarantined'` 只有 `ErrorTrackingStore`／`FileSegmentStore`／`engineBridge` 三个**转发者**，`memory/kv` 里没有任何实现产生该计数 ⇒ 诊断字段 `wal_quarantined` **恒为 0**。旧注释称其为「F3 可观测闭环」不成立；我按事实写进十八节末节（未接通管道），补实现还是删整链请你裁决。顺带：测试里那句"小写入量合法地只存在于 `kv.wal.jsonl`"也指向一个并不存在的 WAL 布局，已随旁白一并清除。
2. `findRustVikingBinary` 硬编码个人绝对路径，其他人机器上集成测试**永远静默 skip**——验证通路没有被看见。属测试代码改动（PATH 查找＋env 覆盖），已登记不混进注释批。

**读数**：`comment-check` ⇒ **`4 file(s), code identical under comment strip`**；`go vet`=0、`go test ./memory/kv` ok 0.85s、`go build ./...`=0；包策略发现 **0**、全仓 0 beyond baseline。

### `tool/knowledge` 整包归零（71 → 0）：一处"规则没报但判据要改"的注释

通读 6 个文件（1283 行）后机械pass：删 53 行体内旁白、28 处尾注转 doc，全部被删行打印并逐条核对（内容均已进文档新增的「知识获取子 agent 的契约」）。清掉的坐标：`S-2（四审，信号倒置）`、`mcp-discovery-execution-loop`、`existing-defect cleanup, 2026-08-26`、`S1040 removed the redundant same-type assertion`。

**关键一类**：`websearch.go` 的包注释**不在任何规则的命中范围内**（它好好地待在 doc 槽里、也没有编号），但它写的是「Compared with the **former** multi-engine HTML-scraping implementation…」。这正是本变更的判据要处理的情形——**离开已归档的变更历史就读不懂**（读者无从知道 former 指哪一版）。改写为不依赖历史的固有理由：抓取引擎 HTML 会因对端改版而**无声失效**，结构化 API 才是主用后端；并把降级语义整表交给文档。棘轮工具抓不到这类，只能靠人读——这也是为什么本变更不能只跑工具就交差。

文档承担的最容易被误读的契约：`memory_query` 在分区隔离存储上**空分区列表＝什么都不扫**（故必须构造期注入可读分区）；存储查询失败**必须返回显式 `query_error` 项**，静默空集会诱导 agent 做冗余搜索并让故障不可观测；MCP 发现器读**活注册表**且只在调用时读，所以运行期注册无需重建 agent；发现结果**不得给假 exec 调用路径**；`web_search` 不限制读取大小**不是遗漏**，而是框架的输出限额工具会把超大返回转储成文件。

**读数**：`comment-check` ⇒ **`6 file(s), code identical under comment strip`**；`go vet`=0、`go test ./tool/knowledge` ok 0.28s、`go build ./...`=0；包策略 **0**；全仓 5821、0 beyond baseline。另登记一处代码遗留（`callable := searchTool`，历史冗余断言删除后未清理的局部变量），不混进注释批。

### `evolution` 生产五件归零：一个包**根本没有文档**，以及一次被门抓住的代码位移

**最要紧的发现不是注释，而是文档为零**：`docs/wiki/evolution/` 目录此前不存在。这个包里全是需要跨模块遵守的硬约定（窗口起点必须锚激活时刻、事件查询必须服务端过滤＋倒序否则后验评估永久静默失效、`commit --only` 防卷入用户暂存内容、judge 的 `score` 用指针接收防"字段缺失=严重劣化"、四态结论不得让 `insufficient` 冒充 `healthy`）。若直接跑删除旁白的机械流程，这些知识会随注释一起消失。所以本批的实际顺序是：**先建 `evolution-architecture.md`（八节）并登记进 wiki 索引，再动代码**。

**修了一处 `go doc` 正确性缺陷**：`evolve.go` 里三句文档注释整体错位——`NewGitEvolution` 的说明挂在 `SetGovernanceSignalsAvailable` 上、`BindRuntime` 的说明挂在 `NewGitEvolution` 上，而 `SetGovernanceSignalsAvailable` 自己没有 doc。这与之前 `MarkMeditationKey` 同族：`go doc` 会输出**张冠李戴**的描述。复位后 `go doc ./evolution` 渲染正确。

**门抓住我自己的位移**：复位错位 doc 需要把 `SetGovernanceSignalsAvailable` 挪回正确位置 ⇒ `comment-check` 立刻报 `CODE-CHANGED evolution/evolve.go`。**没有放宽门**，改为另证等价：

- `decls` 的声明名集合与基线**无增无缺**；
- **剥注释后的记号多重集差异 = 0** ⇒ 代码内容一字不差，只是顺序与 gofmt 对齐变了（`WorkDir        string` → `WorkDir string` 这类对齐变化源自字段注释被删，属预期）；
- 其余四件 ⇒ `4 file(s), code identical under comment strip`。

**规则对我一视同仁**：我在**新写的** `Stop` doc 里用了「不再等满」，被 `audit-marker` 当场命中（"不再"是变更叙述词）⇒ 改成「无需等满」。写注释的人和清注释的人得吃同一套规则，否则棘轮只是装饰品。

**读数**：生产五件 `0 finding`（逐件验证）；包计数 136 → 60（60 全在四个测试文件）；`go vet`=0、`go test ./evolution` ok 1.54s、`go build ./...`=0、0 beyond baseline。`rl` 整包因你在 `swappable_model.go` 上有在飞改动而推迟。

### `evolution` 整包归零（136 → 0）：三条"测试绿但语义没验证"的事实进了文档

四个测试文件（722 行）通读后按同一流程收敛：pass 删 63 行体内旁白、16 处尾注转 doc（被删行全部打印并逐条核对），补四个职责索引，清掉 `W4（§8.3）`/`D1-B（design-report-closeout）`/`C1（backlog-final-closeout）`/`2.4/2.2/4.2/4.4`/`N4`/`K3/K7`/`M1 独立评审`/`resident-remaining-hardening 3.4, archived 7.4`/`fail-before` 等坐标。门 ⇒ **`4 file(s), code identical under comment strip`**。

**读测试代码读到三条容易再次踩的接线事实**，它们各自的缺失都会让测试"过了断言但什么都没验证"，因此从注释升为文档第九节：

1. 种进存储的事件**必须用真实 Snowflake key**：批量读回时分区是从 key 自身推导的（`PartitionIDFromEventKey`），裸整数 key 会让读回静默丢事件；
2. `git status --porcelain` 对未跟踪内容**取目录级展示**（列 `workspace/` 而非 `workspace/x.txt`）：判断"无关改动没被卷进改进 commit"的断言必须按目录前缀写，否则会在正确的实现上误判失败；
3. `encoding/json` 字段匹配**默认大小写不敏感**：`{"Score":0.9}` 是正常命中、**不算缺失**，不触发保守分支——"缺失即保守"的回归只该覆盖真缺失与显式 `null`。

同时把评估结论的两条 fail-closed 不变量与**反向对照的用意**（证明"信号不可用"注解来自开关而非常量字符串）写进文档——那才是这组测试存在的理由，原来只在一段游离注释里。

**又一次被自己的断言拦住**：脚本里误留了一行空替换对（`('func TestEvidence_RatesAndGuards', 同一个串)`）却用在另一个文件上 ⇒ `count==1` 当场失败、该文件未被写入。这已经是本变更里第 N 次证明：批量文本修改的可靠性来自**断言前置**，不是我抄写准确。

**读数**：`evolution` 包策略发现 **0**；`go vet`=0、`go test ./evolution` ok 1.50s、`go build ./...`=0；全仓 5685（本变更累计降 **861**）、0 beyond baseline。已完成 G 的包：`prompt`/`event`/`plugin`/`tool/recall`/`memory/engine`/`memory/kv`/`tool/knowledge`/`evolution`。

### 规范符合性审计：我的"归零"是窄口径，两条 SHALL 工具根本没检查

用户质询后回读 `specs/code-documentation/spec.md` 逐条对表，发现两处**规范有明确 SHALL 而扫描器无对应检查**：

1. 「doc 注释 SHALL 以所依附标识符名开头」；
2. 「测试文件 doc 槽 SHALL 只包含一行测试意图 ＋ 文档索引，MUST NOT 包含机制叙述、踩坑记录或用例论证」。

这两条恰好被我自己的机械流程系统性违反：**把字段尾注整体上移成前导注释**只满足"在 doc 槽内"，不满足"以名字开头"；**测试职责索引**被我写成 3–4 行带"没有这条就形同虚设"的论证段，正是 spec 明令禁止的形态（期望该进断言消息，论证该进 wiki）。补齐检查后，八个"已完成"包暴露 255 处违规（159＋96）。

**做法是先把门补上，再改码**：新增 `doc-not-name-prefixed`（含结构体字段与接口方法；组声明允许多名之一；`_`、索引行、指令豁免）与 `test-doc-not-one-line`。两条红测先失败 → 实现 → `go test ./scripts/comment_policy` 全绿。随后八包收敛到**完整规则集下 0 发现**，`comment-check` ⇒ `60 file(s), code identical under comment strip`，`go build`=0、8 个包 `go test` 全 ok，并用 `-update-baseline` 把未清扫包的既有欠账登记为棘轮起点（此后只准降）。

**我在修复过程中写坏了 147 行注释，被自己的复检抓个正着**：首版前缀器**不幂等**（缩进与 `// ` 重复拼接，且去重判断读不到刚写入的前缀），连跑三轮每轮都报"修复 42 处"而违规数不降——**"跑成功"不等于"有效"**。代码未受损（改的都是注释行，build/test 全程通过）。处置顺序：回滚到修复前快照 → 先写**规范化器**折叠 `// // X // // X` 与重复名字 → 再写幂等前缀器（输出形式固定、只在确不合规时写入）。验收条件也相应改写：**机械写入器必须"重跑一轮违规下降、第三轮恒为 0"**。

**结论与后续判据**：此前各批的读数按其时的规则集是真实的，我没有虚报数字；但"0 发现"曾被当作"符合规范"，这是错的——**门读数是规范的一个子集**。已立 28.7：spec 每条 SHALL/MUST NOT 必须映射到具体规则名成表，无对应检查者即为门缺口，先补规则再清包。

**口径提醒**：全仓数从 5685 涨到 6818，不是退步，而是新纳入检查带出的既有欠账（1131 条），已登记为基线并只准下降。

### 双向审计：51 条索引**没有一条带锚点**，指向的却是 1354 行的长文

用户指出清理不是单向的。回读自己的产物，量化后确实站不住：8 个"完成"包共 51 条 `// 契约:`，**带锚点 0 条**；被指向的文档 568–1354 行（`memory-architecture.md` 1354 行被引 11 次）。索引只回答了"去哪本书"，没回答"翻到哪一页"——这正是"文档里确有对应描述"这一前提没被验证的表现。

再用关键词反查落点，暴露两类更硬的问题：

- **概念在文档中根本不存在**：`MetaKeyEventKey`、"摘要命名"在 `event-architecture.md` 里 **0 命中**；
- **落点错位**：`EventTypeSpec`、`FormatEventPrefix`、`装饰器`、`幂等`、`Getter` 的关键词最密集处竟是《二、文件清单》《一、模块定位》这类泛章节——契约段即使存在，索引也没把读者带过去。

按 28.7 的教训**先补门**：新增 `index-anchor-required`（目标文档 >200 行必须带 `#锚点`）与 `index-anchor-unknown`（锚点须能解析到显式 `<a id>` 或含该词的标题）。红测 `TestIndexMustLandOnASpecificSection` 第一次失败其实是**我断言取错字段**（索引类发现按约定 `Text` 是违规行本身，不是符号名）——改正期望值而不是放宽规则。实现后工具 11 条规则自测全绿。

**以 `prompt` 做完整闭环作为范式**：文档侧补 `Getter` 专段（这层运行期抽象存在的三个理由：消费方不知来源／降级路径单一／可测；外加 `Get()` 的两条 MUST NOT），6 个章节加锚点；代码侧 8 条索引全部落到具体章节（`#getter`、`#source-hotreload`、`#load-composite`、`#load-files`、`#loader-methods`、`#file-layout`）。`go test ./prompt` ok，`prompt` 在**含新规则的完整规则集**下 0 发现。

**没有机器证明的那一步，我没有掩盖**：`prompt` 的索引改写缺机器证明——它的 G 批快照 `/tmp/gb_prompt` 早已被清理。当前依据只有 `go build`=0 ＋ "改写脚本的搜索串以 `// 契约:` 开头、作用域天然限于注释行"的构造性论证。因此另 7 包的锚点迁移被硬性要求**先落快照、后改写、逐包交 `comment-check` 的 `code identical`**（29.6/29.7）。

余量与口径：`index-anchor-*` 已登记为基线，剩余 **36 条**全在那 7 个包；`event` 有 2 处必须**先写清描述**再加锚点（加锚点不能替代把内容写清楚）。另立 29.8：spec 目前只约束锚点语法与目标根，未要求"指向存在的章节"——规则先于规范跑了一步，需回写 spec 使二者一致。

### 索引从"去哪本书"变成"翻到哪一页"：51/51 带锚点，并把这条回写进 spec

承接双向审计，本轮按 `prompt` 范式做完余下七包，分三层——缺一层都还是假的：

1. **文档侧**加 37 个 `<a id>` 锚点（`event` 12.1/12.3/12.4/12.6/12.7/12.8、`plugin` 五/六节与十五节四个子契约、`tool` 十六节与知识获取五节、`memory` 十七/十八节九个小节、`evolution` 三/四/六/九节）。
2. **可检索性**：`event` 那两处"文档缺失"经复查是**用词不一致**——12.6 确有元数据键归属表、12.8 确有摘要与命名澄清，但表里只写字符串值不写常量名。于是把常量点名写进表（`event_key`（`MetaKeyEventKey`）等四个）。加锚点不能替代把描述写清楚；反过来，内容对但搜不到，索引照样落空。
3. **代码侧** 51 条索引全部带锚点（51/51），逐条按其契约句选落点（`metadata.go`→`#metadata-keys`、`projection_sink.go`→`#skip-set`、`engine_persist_test.go`→`#vector-persist`、`websearch_test.go`→`#websearch-backend`）。

**机器证明补齐**：本轮先落快照 `/tmp/gb_an` 再动手，迁移后 ⇒ **`60 file(s), code identical under comment strip`**。仍存一个缺口并如实记录：`prompt` 那一步（上轮）快照已被清理，只有构造性论证。八包 `comment_policy` 均 0 发现；全仓 `index-anchor-*` 归零并写入基线 ⇒ **今后任何裸路径或悬空锚点会直接阻断**；`go test` 8/8 ok、`go build`=0。

**规范回写**：spec 原先只约束锚点语法与目标根，是我这轮的门跑到了规范前面。现补 Requirement「索引必须落到具体章节」＋两个 Scenario，并把反向承诺写进规范：被索引指向的契约描述 MUST 真实存在、且以与代码一致的用词可被检索；描述缺失时 MUST 先写清再加锚点。

**又一次被自己的脚本拦住**：heredoc 里残留一行写坏的表元素，修正用的子串没匹配上 ⇒ `ast.parse` SyntaxError、**一个文件都没动**（各包计数未变即为证）。执行前先解析脚本、失败时用"计数是否未变"判断是否半途写入——第三次救场。

### 追溯表：把"规范有 SHALL 而门没检查"变成可见的三态

用户上一问已证明"门读数 ≠ 规范"。这一轮把它做成结构件：`traceability.md` 把 23 条规范逐条映射到检查名，只有三种状态——**机器 ✓／人工（附为何不可自动化）／门缺口**。这样再新增规范条款时，写不出检查名就必须显式承认它是人工项或缺口，而不是悄悄留着当装饰。

表照出三个缺口，关掉可自动化的那个：

- **G-3 标识编号无持续门** → 新增 `codetools name-check`，并入 `scripts/lint.sh`。红测写了三轮才立住，每轮失败都是一个真问题：`(^|_)` 漏掉紧贴 `Test` 的 `I1`；`\b` 在数字后接大写字母时不成立；改前瞻写法时 **RE2 不支持 `(?=)` 直接 panic**。最终按片段切分判定"大写字母＋数字且数字结束该片段"，并把领域词汇显式白名单（`Int64`、`L1L2L3`、`V2`、`MD5`、`HTTP401`、`Round30`、`Sub2`、`P99`）——**检查若误报领域名，就会被关掉，宁窄勿宽**。全仓实测 0 命中，与 13.8 的清理结论互相印证
- **G-2 `docs/api/` 整条无检查** → 唯一尚未落地的规范性要求，归 W4（30.7）
- **G-1 字段是否算"导出符号"** → **范围口径问题，交给你裁决**（30.6）。我不擅自扩（会把数百条既有欠账砸进门里），也不静默按窄口径解释

**又被 lint 抓到我的作用域错误**：`lint.sh` 扫 `.` ＋ `examples/wechat-bot`，而我上一轮 `-update-baseline` 只按 `.` 写 ⇒ 206 条假「新增违规」。按门实际作用域重登基线后：7035 条、**0 beyond baseline**、退出码 0。规则化：**基线必须与强制作用域同集**。

`go build ./...`=0、`go test ./scripts/codetools ./scripts/comment_policy` 全绿。

### E（任务 10.1–10.7）lint 棘轮与 CI 接线

**为什么是棘轮而不是直接阻断**：存量 7,214 发现若一步阻断，CI 从第一天就红，门会在一天内被关掉——那等于没有门。棘轮记录 `(文件, 规则)` 计数：**只准降不准升**，新文件或新规则从 0 计起，因此"后续变更引入新叙述注释"这一件事是必然失败的；同时批次每清完一域就下调基线，方向被机械锁死。终态（D7/D8）用 `--strict` 要求基线清零。

**门的自测（含门自己的可咬性）**：
- 单元层 `compareRatchet` 四向断言（同级过／增即回归／降级可见／未登记槽位从 0 起）＋基线 JSON **往返测**——我是手写生成器，不可解析的基线会让每次 CI 变硬错误或退化为空基线（静默放行），必须测；
- 端到端：在 `agent/face.go` 函数体内植入一条注释 ⇒ `scripts/lint.sh` 报 `REGRESSION agent/face.go:41: free-standing` 且退出码 **1**；`git checkout` 撤除后该条消失。
- 扫描目录集收敛到 `scripts/lint.sh` 的 `POLICY_DIRS` 单点：基线由它生成、检查由它执行，避免"用 A 作用域的基线判 B 作用域的结果"这种假降级（我第一次跑 `.` 单目录时就出现过 40 个伪"可下调槽位"）。

**修正本台账的一处错误数字**：D0 记的「基线 12,499 违规」是**重复计数**——当时同时把 `.` 与 `agent event plugin ...` 传给扫描器，同一文件被扫两遍。正确单遍数（两模块、跳过 testdata）为 **7,214**，已入库为棘轮基线（1,697 槽）。这是"观测作用域未核对"的同族错误，与 §5.x 的门范围漂移一类。

**lint 上线即抓到的两处（都不是我这次改动引入的）**：
1. `tests/offline_bench/offline_bench_test.go` 有一处**已提交**的 gofmt 错位——历轮我自查只跑到 `*.go`／`agent/*.go`，从未覆盖 `tests/`；本轮 gofmt -w 修正（纯格式）。
2. `memory/segment_store.go` 的 `§5.8`/`§2.8` 注释被判 `audit-marker` 回归——属用户并行变更在飞的编辑，**不由本变更改写**；集成时按任务 10.8 统一重定基线。

**接线**：`.github/workflows/ci.yml` 的报告模式步骤换成阻断的 `bash scripts/lint.sh`（gofmt/build/vet/棘轮一次跑完）；`scripts/hooks/pre-commit` 在 staged 含 `.go` 时调同一脚本（本地与 CI 对"lint 过了"的含义同源；钩子仍是 opt-in，`git config core.hooksPath` 属用户决定，我不代改）。`gen_godoc --check` 暂留报告模式（`continue-on-error`），D7/D8 转阻断。

**并行变更下的纪律（用户令）**：本轮起不追逐在飞编辑引起的红，验证统一留到集成点；该豁免仅限噪声，不覆盖本变更自身造成的失败。

## 2. 根组合包测试文件映射表（任务 2.1；70 → 17，以生产文件为骨）

| 目标文件（职责） | 所辖生产文件 | 并入的源文件 |
|---|---|---|
| `tagent_test.go` | `tagent.go` 组合根与子系统接线 | barrier_composition, governance_wire, mcp_wiring, memory_engine_wiring, tagent_evolution_wire, working_dir |
| `org_hotreload_test.go` | `org_hotreload.go`＋`tagent.go` 热更编排（门序、懒检查、唯一记录、热参消费） | org_hotreload, org_hotreload_e2e, org_hotreload_bench, hotreload_multiagent, org_entry_identity, org_exec_gate_closed, org_config_alias_folding, org_d3_scheduling, org_sc_record, org_m34_hotthread_root, org_se_spawner_ttl |
| `org_candidate_test.go` | `org_candidate_overlay.go`/`org_candidate_txn.go`（候选构造、责任表、回滚、热增、工厂门） | org_candidate, org_candidate_txn, org_candidate_txn_order, org_rollback_txn, org_rollback_l3, org_close_candidate, org_hotadd, org_hotadd_data, org_factory_trunk, factory_gate, resident_shell_lease |
| `owner_retirement_test.go` | `owner_retirement.go`（退役判据、关闭、排空、去壳） | org_retirement, org_retire_diamond, org_retire_poisoned_acquire, org_owner_close, org_close_shared, org_sd_usage_hold, org_sd_drain_poke, org_deshell, reset_managed_drill |
| `cross_generation_test.go` | 跨发布版本继承与重入（执行代租约＋settle 路由的根级贯穿锚） | org_cross_publish_vertical, org_cross_alias, org_cross_config_scenario, org_reentry_multilevel, org_wal_restart_reentry, org_monitor_reattach, org_nested_hop, reentry_task_action |
| `org_diagnostics_test.go` | 诊断面（`OrgDiagnostics`/引用债/关闭相位） | org_diagnostics, org_diagnostics_consumers, org_diagnostics_legs |
| `delegation_test.go` | 委派与 A2A 子调用 | org_delegation, a2a_delegation |
| `resources_test.go` | `resources.go` 资源所有权与回收 | resources_ownership, resources_build_reclaim, resources_lock, resources_poisoned |
| `build_agent_test.go` | `build_agent.go` 装配 | build_cycle |
| `registry_test.go` | `registry.go`/`builtin.go` | build_plain_tool_ref, builtin, builtin_agent_protection |
| `partition_collision_test.go` | `partition_collision.go` | partition_collision, build_agent_partitions |
| `modelref_test.go` | `modelref.go` 与 provider 协议矩阵 | model_resolution, model_contract_matrix |
| `prompts_test.go` | `prompts_embed.go` | prompts_embed, prompt_contract |
| `consolidation_hint_test.go` | `consolidation_hint.go` | consolidation_hint, consolidation_candidates |
| `config_test.go` | `config.go` 严格解析 | config |
| `guardrails_test.go` | 架构护栏断言（依赖方向、无第二表示、已消除项） | arch_layers, elimination_latest_path |
| `teststores_test.go` | `testing.go` 共享测试基建（夹具，非职责测） | test_stores |

**端到端链落点（合并不得拆散）**：`org_hotreload_e2e`、`hotreload_multiagent`、`org_monitor_reattach`（三进程真 tmux）、`org_wal_restart_reentry`（子进程崩溃形态）、`cross_generation` 族的跨发布回流锚、`reset_managed_drill`（启动子进程演练）——这些链各自保持单一整链落点，仅改变所在文件，不降级为分面断言。

**本轮已执行的组（8 组，20 文件 → 8 目标）**：org_diagnostics／resources／prompts／consolidation_hint／partition_collision／modelref／registry／guardrails。其余 9 组（含 org_hotreload 12→1、org_candidate 11→1、owner_retirement 9→1、cross_generation 8→1、tagent 6→1、delegation 2→1、build_agent、config、teststores）保持 `[ ]`，映射表已定，后续按组过门执行。

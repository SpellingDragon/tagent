# 05 契约同步 + 死码清理 · 设计

> 静态决策见一级 [design.md](../../design.md) D1 **P5 契约一致**／**P6 无死代码**、D4「05 独立但后置」、§七 消费者枚举「契约」行。
> 本域不改恢复语义。所有「已退役」判定以**当前树 grep 实测**为据，不照抄归档清单计数。

## 一、现状核验：继承清单与当前树的差

`archive/2026-11-03-runtime-architecture-convergence/evidence/06-compression-legacy-authority.md` §三 点名 16 个主 spec／628 SHALL／181 legacy 条款，并在 §八.1 明写「归档阶段执行，本清单为输入」。该归档波**已执行了一部分**：当前树实测 8 个 spec 已带 superseded 备注且 Requirements 只剩「历史条款已退役」（体例见 `openspec/specs/compress-event-enrichment/spec.md:5-17`）。

**归档清单的行号已漂移，本域一律以当前树重新实测**（✅）：清单 §一 的 `assembly/run.go:1401`（RestoreProducts）现为 `run.go:2562` `seedAnchoredReplay`（:2633/:2676 调 RestoreProducts）＋ `seedFullReplay:2470`（:2524）；清单 §3.3 的 `age_plan.go:118`（ApplyProducts 幂等）现为 `:134`，`:183`（RestoreProducts）现为 `:208`；清单 §3.3 引的 `age_plan_test.go:15` 现为 import 行，该文件现行测试是 `:24 TestPlanAndApplyAnswerTheFold`／`:163 TestFoldCapacityAndCandidateRules`／`:440 TestFoldCapacityEarlyExitDecision`／`:621 TestFoldPlanGolden`，**其中无幂等专锚**——幂等判据的现行锚在 `age_segments_test.go:506` 与 `replay_budget_test.go:402`（见 §四）。

| 分类 | 主 spec | 实测判据 |
|---|---|---|
| 已 superseded（8，不重写） | compress-event-enrichment、compression-user-message、event-compaction、llm-event-valuation、llm-summary-generator、summary-memory-rag、view-transform-stateless、framework-flow-adapter | 各 `spec.md:5` 带 `> **superseded（2026-11-03 归档核验）**`；legacy 符号只出现在该历史备注内 |
| 活条款反向（8，须改写/退役） | task-skeleton-compression:74、event-sourced-projection:47-51/:156、compression-recovery-ticket:8、compaction-scheduler:11/:33、production-wiring（23 处）、production-wiring-fix（17 处）、storage-lifecycle-completion（6 处）、event-segment-store:227/:233/:269/:293、architecture-review-standards:60/:102/:153 | 无 superseded 标记且 legacy 符号在 `### Requirement:` 正文 |
| 已转正（3，只核） | deterministic-compress-level（`LevelOfAge`/`BandOfLevel` 线性三带）、compress-quality-fix（`Append` 幂等）、recall-protocol | legacy 符号 0 命中 |
| 审计新发现（1） | memory-curation:24/:33/:38 | `SummaryModel`/`card_max_chars`/`Compact` Go 树 0 命中（`summaryRef` 仍 3 命中，属现行） |
| 本域不触（2） | config-hot-reload、request-budget-accounting | 前者工作树已被 config-surface-normalization 修改；后者由其 01/03 delta 归档时同步 |

**两处必须精确、不得扩大打击面**：

- `task-skeleton-compression:51`（`segment_fold` 产物 MUST NOT 是 LLM 生成文本）**仍成立**：`assembly/compress_consumer.go:276` 的类型闸 `product.Entry.Type != replay.ProjectionTypeHistorySummary` 直接 return，只有历史综述被改写。反向的只有 :74。
- `FileSegmentStore`（`memory/ledger/segment_store.go:244`）是**现行** journal 档位实现，与已退役的 `memory/localfile` 包（`ls memory/` 无该目录）不是一回事。`event-segment-store:227` 的 Close 条款只去 Compactor/LifecycleManager/TombstoneSet 分句，主体保留。

## 二、5.1 逐 spec 处置手法

三种手法，按条款是否仍有现行 owner 选择，**不整文件删**（归档清单 §三 计数栏同此纪律）：

1. **MODIFIED（有现行 owner，语义须改写）**
   - `task-skeleton-compression:74`：改为「历史综述正文以机械一行读法为底；会话 agent 持有模型时 SHALL 以有界语义综述取代该正文，无模型／请求失败／答空 SHALL 降级回机械正文并告警，综述 SHALL NOT 为空、SHALL NOT 阻塞折叠、SHALL NOT 超过既定字节界」。依据 `compress_consumer.go:275-291` + `assembly/summary_gist.go:40-43`（2048 字节界）+ `run.go:578`。锚：`tests/integration/host/compress_pipeline_test.go`。
   - `event-sourced-projection:156`：去 `RebuildProjectionFromWAL` 分句，保留 `FoldByAge`/`ApplyProducts`/`RestoreProducts` 幂等（现行 owner `memory/replay/age_plan.go:134`（ApplyProducts）/`:208`（RestoreProducts）、`age_segments.go:468`（FoldByAge））。
   - `production-wiring`／`production-wiring-fix`：`resolveMemoryStore` 的现行接线条款转正为 journal 档位＋现行生命周期；两 spec 重复的 wiring 条款收口到单一 owner，另一份标 superseded 指回（避免双重立法，`openspec/AGENTS.md`「同一事实单处」）。
   - `event-segment-store:227/:233/:269/:293`、`architecture-review-standards:60/:102/:153`、`memory-curation:24/:33/:38`：审查项/配置面对象改写为现行权威。
2. **REMOVED（条款整体无现行 owner）**
   - `event-sourced-projection:47-51`「历史 compaction 事实的冷启动只读重建」整条 REMOVED——与 5.3 删 `RebuildProjectionFromWAL` **同批原子**，否则主 spec 立刻 SHALL 一个不存在的函数。该条还 names `MinEventKey`（Go 树 0 命中），佐证其已整体退场。
3. **superseded（整能力已退役）**
   - `compression-recovery-ticket`（`recovery.ticket` 票据快照，12.6 退役）、`compaction-scheduler`（L1→L2→L3 压实）：照 `compress-event-enrichment/spec.md:5-17` 体例——`## Purpose` 下加 superseded blockquote 说明退役事实与现行 owner，`## Requirements` 只留一条「历史条款已退役」＋一个指向现行 spec 的 Scenario，**不留现行 SHALL/MUST**（`scripts/gates/check_concept_budget.py:815-839` `superseded_spec_findings` 正是这条判据）。
4. **只附勘误（历史备注内的现行断言被推翻）**
   - `llm-summary-generator:5`「现行折叠零 LLM：段折叠与历史综述都是确定性工程产物」→ 附勘误：历史综述在会话持模型时为有界语义综述（`run.go:578`），段折叠仍确定性。备注的**退役事实部分不改写**（`LLMSummarizer`/`SummaryGenerator` 仍 0 定义）。

**结构硬约束**（`openspec/AGENTS.md`）：主 spec 保 `# <capability> Specification` + `## Purpose`（≥50 字符）+ `## Requirements`；每 `### Requirement:` 含 SHALL/MUST 且至少一个 `#### Scenario:`；主 spec **MUST NOT** 出现 `## ADDED/MODIFIED/REMOVED Requirements`（那是本域 delta 的语法）。本阶段 delta 写在本目录 `specs/`，主 spec 同步在实施/归档由 `openspec archive`（不 `--skip-specs`）执行。

## 三、5.2 golden prose 同步

- `golden/runtime/durable_truth.json:300`：reason 内 `checkpoint:<partitionSeq>` → `checkpoint:<partition>:<partitionSeq>`，与 `api/event/checkpoint.go:68-77` 的 doc 与 `CheckpointKey` 实现同向（分区＋槽位双段，理由是「两分区可站在同一槽位，只写槽位会把一个事实命名两次」）。`prefixes[].value` 仍是 `checkpoint:`（`checkpointKeyPrefix`，checkpoint.go:82），**不动**——门 `check_durable_truth_registry.py:460` 比的正是这个字面量。
- `golden/runtime/architecture.json:6`：`concept_ratchet.note` 是棘轮抬升的历史台账，已有「勘误（W8-VOCAB）」先例 → 5.4 结论以**同体例追加勘误句**，不改写 `new_max 44->47` 等历史数值。若 5.4 只退役未导出的 `compatMaterialOf`，导出面计数不变，两个棘轮数**不动**。
- **门不拦 prose**：`check_durable_truth_registry.py:450-467` 只校 `value`，故 5.2 的正锚不能只写「门绿」（改前改后都绿）。负锚用 grep：`checkpoint:<partitionSeq>` 在 `golden/` 零残留。

## 四、5.3 死生产者删 + 级联 + 测试锚迁移

**删的对象**（生产调用方 0，实测）：`ReplayProjection.Fold`（`memory/replay/fold.go:49`）、`RebuildProjectionFromWAL`（`memory/replay/projection.go:342`）、`skeleton_assert.go:32`（Fold 断言）与 `:101`（Rebuild 断言）。

**级联检查（全局纪律 §四.1：被删抽象的伴生引用降级为单使用方须回卷）**——实测下列符号的生产引用只落在被删函数自身文件＋skeleton_assert，须同批退役：

| 符号 | 生产引用面 | 处置 |
|---|---|---|
| `RebuildOptions`／`RebuildReport` | projection.go + skeleton_assert.go | 随 Rebuild 退役 |
| `ErrRebuildUnwired`／`ErrRebuildPartial`／`ErrCompactionCorrupt` | 同上 | 随 Rebuild 退役 |
| `ErrChainScan` | projection.go:365/:420（均在 Rebuild 体内） | 随 Rebuild 退役 |
| `RetainRefType` | fold.go:83（Fold 体内）+ fold.go:131 定义 | 随 Fold 退役 |
| **`ErrChainPageLimit`** | projection.go:200（`ScanFrom` 分页，**现行**）+ `examples/contract/memory_replay/EventChain_example.go:19/:61` | **KEEP**——属 EventChain 分页契约，不属重建路径 |
| **`FileSegmentStore`／`MigrateShardedRoot`** | segment_store.go:244／migrate.go:76 + `scripts/migrate-kv/main.go:24` | **KEEP**（前者现行档位，后者活操作员工具） |

**测试锚迁移（12 个测试文件，逐文件定性，MUST NOT 整片删覆盖）**：

| 锚文件:行 | 现行覆盖的是什么 | 处置 |
|---|---|---|
| `memory/replay/fold_test.go:42/:70/:82/:94`（TestFoldIsIdempotentOverTheSameMaterial，:21） | compaction 生产者幂等 | 随机制退役；**现行孪生锚已存在**——`memory/replay/age_segments_test.go:506` `TestAgeFoldMintsTheSameProductsForTheSameMaterial`（同材料同产物键＋二次折叠不产第二份）。`fold_test.go:118` `TestFoldDepthMeasurementExists` 是测量存在性锚，**不得随文件整删** |
| `memory/replay/replay_budget_test.go:343`（TestFoldDropsTheMaterialOfTheEntriesItFolded，:330） | 折叠后材料离开投影 | 随机制退役；**现行孪生锚已存在**——同文件 `:402` `TestApplyProductsDropsTheMaterialOfTheCoveredEntries` 在 `PlanByAge`/`ApplyProducts` 生产面覆盖同判据＋二次 apply 幂等。死锚独有的 **retained-ref 保留原文**子断言随 `RetainRefType` 退役，MUST NOT 声称判据全等价 |
| `tests/integration/host/request_boundary_test.go:368` | 请求边界内的折叠 | 迁移到 `FoldByAge` |
| `tests/integration/history/projection_replay_and_budget_test.go:148/:196/:234/:295/:334/:353/:371`（7 处） | 重建分页／fallback 界／partial 报告／空库确认 | 逐条定性：属 compaction 载荷恢复的随机制退役；属**现行链分页/预算**的迁到 `ScanFrom`＋`AssembleBudgetPrefix` |
| `memory/partition_test.go:289/:329/:346/:352/:363/:373/:382`（7 处） | 分区隔离＋`ErrCompactionCorrupt` 具名拒绝 | 分区隔离判据迁到现行恢复入口；corrupt 拒绝随符号退役 |
| `tests/integration/state/notice_projection_test.go:67/:127/:142/:145` | 通知投影重建幂等 | 迁移到 `RestoreProducts` |
| `tests/integration/execution/cross_session_scoping_test.go:243` | 跨会话不串链 | 迁移到现行恢复入口（判据是链隔离，非重建函数） |
| `tests/integration/history/age_band_request_test.go:74` | 龄段带请求装配 | 迁移到 `FoldByAge`＋`AssembleBudgetPrefix` |
| `tests/integration/history/event_integrity_test.go:1807/:2026` | 事件完整性：rebuild 不重折 | 迁移到 `RestoreProducts`（现行入口 `run.go:2562` `seedAnchoredReplay`，:2633/:2676） |
| `tests/integration/host/age_fold_test.go:237`（**TestRebuildRefoldsIdempotent**） | 内容寻址键幂等 | **迁移，不删**：归档清单 §5.4 把该测试列为「必须保留的现行锚」，而它正是靠 Rebuild 建投影 → 改用现行恢复路径建投影，保留「同链同窗口折出同组产物键」断言 |
| `tests/integration/acceptance/scenario_age_fixtures_test.go:121` | 验收场景夹具 | 迁移到现行恢复入口 |
| `examples/wechat-bot/smoke_test.go:2331` | 远端 smoke 重建 | 迁移；**远端独立模块**，须 `go test -C examples/wechat-bot -tags smoke` 单独验 |

**触碰集冲突（✅ 实测 `git status`）**：上表 4 个文件当前已被 config-surface-normalization 未提交变更修改——`examples/wechat-bot/smoke_test.go`、`tests/integration/acceptance/scenario_age_fixtures_test.go`、`tests/integration/history/event_integrity_test.go`、`tests/integration/host/age_fold_test.go`（diff 内容为 `MaxTokens`→`ReplayWindow`/`ContextWindow` 键名归位）。这 4 个文件的锚迁移**须待该变更落地后追加**，只在其终态上改，MUST NOT 覆盖或回滚其未提交改动。其余 8 个文件可先行。

## 五、5.4 六端口兼容读法：先审计后裁

**已实测的三分消费者面**：

- **六端口常量仍活**：`api/plugin/material.go:25-45`（PortCallTransform／PortCallRuling／PortRequestEnvironment／PortFactObservation／PortFactView／PortFactCompression）；`validatePort:57` 闭表校验、`NewFlow:254` 取 `instance.Port()` 入 declaration、`api/plugin/phases.go:210/:214/:256` 按 port 分派并校验 `port == material.Reads`。→ **端口本身 MUST NOT 退役**，`architecture.json:6`「六端口保留为兼容读法」这句的**主语须辨明**：活的是分派键，候选死的是 material 的兼容派生。
- **`compatMaterialOf`（material.go:203）生产零消费**：9 个生产 face 全部同时实现 `Port()` 与 `Material()`——`assembly/compress_consumer.go:133/:139`、`assembly/settle_turn.go:297/:304`、`assembly/age_fold.go:130/:137`、`assembly/facts.go:1073/:1080`、`assembly/host_tools.go:1411/:1416`、`features/evolution/meditation/driver.go:102/:110`、`features/evolution/governance/policy.go:97/:104`、`features/rl/lease_observer.go:76/:83`、`runtime/agent/final_round_planner.go:92/:97`。故 `materialOf:190-195` 的兼容分支在生产路径永不进入。
- **剩余消费方两类**：①`examples/contract/api_plugin/*_example.go` 14 个只声明 `Port()` 的 face（受 `scripts/gates/check_concept_budget.py:77` `EXAMPLE_ROOT` 的 canonical example 门约束，是 `Instance` 公共面的契约示例）；②测试 face（`api/plugin/material_test.go:25`、`phases_test.go:27`、`features/evolution/governance/policy_test.go:462`、`tests/integration/**`）。

**裁定判据（全局纪律 §〇二／§五）**：测试不纳入衡量；契约示例是否算独立第二使用方，取决于它演示的是**公共 API 的可选声明路径**还是**仅为兼容分支自证**。两条候选处置：

- **甲：退役 `compatMaterialOf`＋`materialDeclarer` 可选性**——`Material()` 升为 `Instance` 的必选方法，14 个契约示例与测试 face 同批补声明，`admitted` 表（material.go:177-184）只留 `validateMaterial:221` 的校验用途，`dutyOfObserved`（material.go:210 → `observe_duty.go:60`）的兼容分支随之回卷。导出面变化：`Instance` 增一方法 → 须核 `check_concept_budget.py` 的 `exported_interfaces_max`（当前 83）与 canonical example 门。
- **乙：保留兼容读法**——若契约示例确为公共 API 的合法可选声明形态（外部插件只声明 Port 即可挂载），则 `compatMaterialOf` 有独立第二使用方，保留；只把 `architecture.json:6` 的勘误写清「兼容读法的消费方是公共契约示例，生产 face 全部已声明 Material」。

**这是设计级分叉**：5.4 先出审计结论（消费者三分表＋独立性判定），甲/乙的选择上报编排者裁定后再动手，MUST NOT 在审计未结时先删。

## 六、文件清单

**契约面（本阶段只写 delta，主 spec 实施/归档时同步）**
- 本域 `specs/code-documentation/spec.md`（ADDED 五条）
- 主 spec：`task-skeleton-compression`、`event-sourced-projection`、`compression-recovery-ticket`、`compaction-scheduler`、`production-wiring`、`production-wiring-fix`、`storage-lifecycle-completion`、`event-segment-store`、`architecture-review-standards`、`memory-curation`、`llm-summary-generator`（勘误）
- 只核不改：`deterministic-compress-level`、`compress-quality-fix`、`recall-protocol`、已 superseded 的另 7 个

**golden**
- `golden/runtime/durable_truth.json:300`（reason 键形）
- `golden/runtime/architecture.json:6`（追加勘误，数值不动）
- `golden/runtime/spec_hygiene.json`（仅在 reviewed tightening 后 `--write-baseline` 清 stale，见风险）

**代码（5.3）**
- `memory/replay/fold.go:49`（Fold）＋`:83/:127-131`（RetainRefType 级联）
- `memory/replay/projection.go:342`（RebuildProjectionFromWAL）＋`:87-91`（ErrChainScan）＋RebuildOptions/RebuildReport/ErrRebuild* 定义处
- `memory/replay/skeleton_assert.go:32/:101/:103/:107-108`（对应断言；`:107` ErrChainPageLimit **保留**）
- `memory/replay/replay_budget.go:67`（doc 中对 RebuildProjectionFromWAL 的引用）
- 12 个测试锚文件（见 §四表；4 个待 config-surface 落地）

**代码（5.4，裁定后）**
- `api/plugin/material.go:157-215`（materialDeclarer／materialOf／compatMaterialOf／admitted）、`api/plugin/observe_duty.go:60`
- `examples/contract/api_plugin/*_example.go`（14 face）、`api/plugin/{material,phases}_test.go`

**不动**：`memory/ledger/migrate.go:76`、`scripts/migrate-kv/main.go`、`memory/ledger/segment_store.go`、boot 路径簇、`openspec/changes/archive/**`、config-surface-normalization 全部未提交文件。

## 七、风险

1. **spec_hygiene 门反噬（最高风险）**：`scripts/gates/check_spec_hygiene.py:1-45` 禁主 spec **requirement 正文**出现 `.go` 文件名、函数调用形态、数字常量，既有违例 grandfathered 于 `golden/runtime/spec_hygiene.json`。本域改写若把现行权威写成 `LevelOfAge(age, keepRecent)`／`compress_consumer.go:275`／`2048 字节` 落进 requirement 正文，即**新增违例判红**。手法：机制细节写进 `#### Scenario:`（该门明写 Scenario 不受此规则）或 `docs/wiki`，requirement 正文只写不变量；符号名以裸标识符（不带调用括号/不带 `.go`）出现。退役 legacy 条款会留下 stale 基线条目（门报 stale 不报错），须 reviewed tightening 后 `--write-baseline`，MUST NOT 为消红先改基线。
2. **concept_budget 的 superseded 规则**：标 superseded 后 Requirements 仍含现行 SHALL/MUST 即红（`check_concept_budget.py:815-839`）；且 superseded spec 不得再作 spec anchor（`:926-927`）→ 退役 `compression-recovery-ticket`/`compaction-scheduler` 前须 grep 全仓（含 `docs/wiki`、golden、代码 `契约:` 索引）确认无锚指向它们，有则先改绑现行 owner。
3. **删覆盖**：12 个测试锚中 `TestRebuildRefoldsIdempotent` 被归档清单 §5.4 列为必须保留的现行锚，却依赖被删函数 → 必须先迁移建投影手法再删函数，且迁移后断言判据（内容寻址键幂等）逐字保留。任何「先删函数后补测试」的次序都会产生一段无覆盖窗口。`fold_test.go` 与 `replay_budget_test.go` 的死锚虽已有现行孪生覆盖（§四表），仍须逐条核对判据后再删，MUST NOT 以「有孪生锚」为由整文件删（`fold_test.go:118` 的测量存在性锚与死锚同文件）。
4. **级联漏项**：`ErrChainPageLimit` 与 `ErrChainScan` 名字同族但归属不同（前者 EventChain 分页现行、后者 Rebuild 专属）→ 按语义单元划界，不按名字形态批量删（全局纪律 §〇三）。
5. **远端模块**：`examples/wechat-bot` 是独立 go.mod，主模块 `go build ./...` 不覆盖它 → 5.3 须单独跑远端 build/smoke。
6. **spec 转正早于代码稳定**：01/02/03 若改 checkpoint 键形/切片名/恢复入口，先写的 spec 与 golden 会二次漂移 → 草案先写、转正在 W1+W2 代码绿后（一级 D4）。
7. **5.4 隐藏消费者**：兼容分支可能有非 face 的间接消费（如 `assemble` 内按 material 分派）→ 审计须覆盖 `api/plugin/phases.go` 全部分派点，不止 `Port()` 的直接调用方。
8. **校验入口的两处实测限制（✅，须上报编排者，本域不代改父级）**：①`openspec validate` 只寻址顶层 change——`openspec validate --changes --strict --json` 实测只发现 8 个顶层项，`openspec validate 05-contract-sync-dead-code` 报 `Unknown item`；本域 delta 的 strict 校验在编排者合入父 change `specs/` 或归档同步时生效，域内正锚改用 `bash scripts/gates/check-openspec.sh` + `openspec validate --specs --strict --no-interactive`（主 spec 面）＋ delta 结构自检。②父 change 当前 strict 红：`openspec/changes/runtime-recovery-ideal/specs/orchestration/spec.md` 用主 spec 结构（`# orchestration Specification`/`## Purpose`/`## Requirements`）而非 delta 语法，报「No delta sections found」；对照 `config-surface-normalization/specs/orchestration/spec.md:1` 用 `## ADDED Requirements` 且 validate 绿。此项属编排者写集，本域只上报。
9. **本域 delta 自身须归档后可过门**：delta 在归档时成为主 spec requirement 正文，故本域 `specs/code-documentation/spec.md` 的 requirement 正文已按 spec_hygiene 判据写——不含源文件名、调用括号形态与数字常量，全部 `file:line` 与数字落在 `#### Scenario:`（该门明写 Scenario 不受此规则）。

## 八、回退

- **契约面**：主 spec 与 golden 均为文本，回退＝`git revert` 对应提交；验收 `openspec validate --specs --strict --no-interactive` 与 `scripts/gates/check-openspec.sh` 绿。回退后 legacy 条款回到反向状态（P5 红），如实汇报，不伪称已同步。
- **死生产者**：删除是纯减法且生产调用方 0，回退＝恢复函数＋断言＋测试锚原状；因无生产消费者，回退不改变任何运行期行为（可用「回退前后 `go test ./assembly/... ./memory/...` 同绿」作证）。
- **5.4**：甲案若抬导出面或改公共 `Instance` 契约，回退成本高于乙案 → 裁定前须把该成本写进上报，不默认选甲。
- **不下线旧机制的并存纪律**（全局纪律 §四.3）：本域删的是**已无生产调用方**的死码与**已退役机制**的条款，不属「下线仍在用的旧机制」；对仍有消费者的兼容读法（5.4 乙案）保留并存，不强行迁移。
# 10 契约同步 + 死码清理 · 设计

> **当前执行见 §十三与 tasks 的 R5–R9。** 下文旧轮次实测与方案原文保留，不把旧“今日”、零调用计数或门绿作为现树结论。历史 R/G 及 ORCHESTRATION 引文均指 [ORCHESTRATION-HISTORY.md](../../ORCHESTRATION-HISTORY.md)。本轮仅修本域两份计划，未实施、未复跑。

> 静态决策见一级 [design.md](../../design.md) D1 **P5 契约一致**／**P6 无死代码**、D4「05 独立但后置」、§七 消费者枚举「契约」行。
> 本域不改恢复语义。所有「已退役」判定以**当前树 grep 实测**为据，不照抄归档清单计数。

## 一、现状核验：继承清单与当前树的差

`archive/2026-11-03-runtime-architecture-convergence/evidence/06-compression-legacy-authority.md` §三 点名 16 个主 spec／628 SHALL／181 legacy 条款，并在 §八.1 明写「归档阶段执行，本清单为输入」。该归档波**已执行了一部分**：当前树实测 8 个 spec 已带 superseded 备注且 Requirements 只剩「历史条款已退役」（体例见 `openspec/specs/compress-event-enrichment/spec.md` 的 Purpose 下 superseded 备注与「历史条款已退役」条款）。

**归档清单的行号已漂移，本域一律以当前树重新实测**（✅）：清单 §一 的 `assembly/run.go:1401`（RestoreProducts）现为 `run.go:2562` `seedAnchoredReplay`（:2633/:2676 调 RestoreProducts）＋ `seedFullReplay:2470`（:2524）；清单 §3.3 的 `age_plan.go:118`（ApplyProducts 幂等）现为 `:134`，`:183`（RestoreProducts）现为 `:208`；清单 §3.3 引的 `age_plan_test.go:15` 现为 import 行，该文件现行测试是 `:24 TestPlanAndApplyAnswerTheFold`／`:163 TestFoldCapacityAndCandidateRules`／`:440 TestFoldCapacityEarlyExitDecision`／`:621 TestFoldPlanGolden`，**其中无幂等专锚**——幂等判据的现行锚在 `age_segments_test.go:506` 与 `replay_budget_test.go:402`（见 §四）。

| 分类 | 主 spec | 实测判据 |
|---|---|---|
| 已 superseded（8，不重写） | compress-event-enrichment、compression-user-message、event-compaction、llm-event-valuation、llm-summary-generator、summary-memory-rag、view-transform-stateless、framework-flow-adapter | 各文件 Purpose 下的 superseded 备注带 `> **superseded（2026-11-03 归档核验）**`；legacy 符号只出现在该历史备注内 |
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
   - `compression-recovery-ticket`（`recovery.ticket` 票据快照，12.6 退役）、`compaction-scheduler`（L1→L2→L3 压实）：照 `compress-event-enrichment/spec.md`（Purpose 下 superseded 备注＋「历史条款已退役」）体例——`## Purpose` 下加 superseded blockquote 说明退役事实与现行 owner，`## Requirements` 只留一条「历史条款已退役」＋一个指向现行 spec 的 Scenario，**不留现行 SHALL/MUST**（`scripts/gates/check_concept_budget.py:815-839` `superseded_spec_findings` 正是这条判据）。
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
8. **校验入口的两处实测限制（✅，须上报编排者，本域不代改父级）**：①`openspec validate` 只寻址顶层 change——`openspec validate --changes --strict --json` 实测只发现 8 个顶层项，`openspec validate 05-contract-sync-dead-code` 报 `Unknown item`；本域 delta 的 strict 校验在编排者合入父 change `specs/` 或归档同步时生效，域内正锚改用 `bash scripts/gates/check-openspec.sh` + `openspec validate --specs --strict --no-interactive`（主 spec 面）＋ delta 结构自检。②父 change 当前 strict 红：`openspec/changes/runtime-recovery-ideal/specs/orchestration/spec.md` 用主 spec 结构（`# orchestration Specification`/`## Purpose`/`## Requirements`）而非 delta 语法，报「No delta sections found」；对照 `config-surface-normalization/specs/orchestration/spec.md` 以 `## ADDED Requirements` 起头且 validate 绿。此项属编排者写集，本域只上报。
9. **本域 delta 自身须归档后可过门**：delta 在归档时成为主 spec requirement 正文，故本域 `specs/code-documentation/spec.md` 的 requirement 正文已按 spec_hygiene 判据写——不含源文件名、调用括号形态与数字常量，全部 `file:line` 与数字落在 `#### Scenario:`（该门明写 Scenario 不受此规则）。

## 八、回退

- **契约面**：主 spec 与 golden 均为文本，回退＝`git revert` 对应提交；验收 `openspec validate --specs --strict --no-interactive` 与 `scripts/gates/check-openspec.sh` 绿。回退后 legacy 条款回到反向状态（P5 红），如实汇报，不伪称已同步。
- **死生产者**：删除是纯减法且生产调用方 0，回退＝恢复函数＋断言＋测试锚原状；因无生产消费者，回退不改变任何运行期行为（可用「回退前后 `go test ./assembly/... ./memory/...` 同绿」作证）。
- **5.4**：甲案若抬导出面或改公共 `Instance` 契约，回退成本高于乙案 → 裁定前须把该成本写进上报，不默认选甲。
- **不下线旧机制的并存纪律**（全局纪律 §四.3）：本域删的是**已无生产调用方**的死码与**已退役机制**的条款，不属「下线仍在用的旧机制」；对仍有消费者的兼容读法（5.4 乙案）保留并存，不强行迁移。

## 九、R1 P0–P8 与完成账双向对账

### 今日实测（只读命令与原始结果）

```sh
python3 scripts/gates/check_dod_coverage.py            # exit 1，19 gap
python3 scripts/gates/check_concept_budget.py          # RESULT: FAILED（S1 iface-methods runtime/state/durable.go:FactLog 5 方法 > baseline 4）
python3 scripts/gates/check_spec_hygiene.py            # ok（specs=151 requirements=781 violations=279 baseline=286 stale=7）
grep -rn "func <锚名>(" --include=*.go .                # P0/P1/P2/P3/P4/P7/P8 的锚各 1 处命中；P5/P6 无锚
```

### 双向对账表（对齐 design D1 的每条子要求）

| 判据 | design D1 子要求（要点） | 登记表绑定 | 锚测试是否存在 | 实际运行它的 CI 作业 | 当前状态 |
|---|---|---|---|---|---|
| P0 | 特性经三挂点/注册表动态挂载；装配根不认特性名；`plugins:` 有生产消费 | `mountedBuiltinFeatures`；`TestBuiltinCatalogMountsEveryFeature`；`MUT-DOD-P0-concrete-mount`；`ci_gate=test` | 1 处命中 | `test`（`go test ./... -short`） | 交付在，**变异未登记、无判杀记录** |
| P1 | 链唯一真相；一身份一不可变事实 | `committedIdentityFacts`；`TestColdArchiveRefusesToOverwriteAnIdentity`；`ci_gate=test` | 1 | `test` | 交付在，**变异未登记**。**勘误 2026-11-16**：原「退位搬迁非删除」半句随退位删除作废；该锚名中的 `ColdArchive` 现钉的是写权（flock）面，见 08.R3b 的 `config_test.go` 改名 |
| P2 | 全部恢复经同一 reducer，无全史读旁路 | `bootFoldedFacts`；`TestSeedFactReusesTheReducer`；`ci_gate=test` | 1 | `test` | 交付在，**变异未登记** |
| P3 | 所有可恢复态是 Stateful 切片 | `durablePrefixScansOfASeatedAccount`；`TestDurableLoadReadsTheTaskSlice`；`ci_gate=test` | 1 | `test` | 交付在，**变异未登记** |
| P4 | boot 解码/索引/durable load/墙钟不随历史线性增长（真量） | `measure=wallClock`，`bound{factor:2, vs:the second boot}`；`TestSymptomAnchorBootCostFlatAcrossGenerations`；`ci_gate=event-integrity-long` | 1 | 锚在 `assembly` 包 → 由 `test` 作业跑；`event-integrity-long` 跑的是 `examples/wechat-bot` 的 `TestEventIntegrityLongRun` | **三处真冲突**：①`ci_gate` 指错作业（门只比作业名是否存在，故不拦）；②`measure` 只写 `wallClock` 而子要求是四量；③`bound.factor=2` 与候选变异记录自述的 8x 矛盾，且变异判 `not_killed` |
| P5 | 主 spec 与代码同向；legacy 清零；golden 同步 | `anchor.test` 为空，登记表自述「unguarded until one exists」；`ci_gate=openspec` | 无 | `openspec` 作业跑 `check_spec_hygiene.py` 等 | **要求未达且无锚**：不得标「已退役」，须补锚或改绑 |
| P6 | 死生产者删；未接线脚手架接线或删；废弃 spec 退役 | `anchor.test` 为空，自述「no anchor binds that reading」；`ci_gate=test` | 无 | `test` | **要求未达且无锚**：与 R2 的脚手架处置同源 |
| P7 | 中段过程呈现为可召回票据，模型自主取全文 | `eventKeyTicketsInThePresentedRequest`；`TestSymptomAnchorMidbandProcessRecalledByTicket` | 1 | `test` | 交付在，**变异未登记** |
| P8 | 摘要预算可配置；摘要经租约＋声明采样 | `summaryTextBytesUnderTheDeclaredBudget`；`TestHistorySummaryObeysThePlanBudget` | 1 | `test` | 交付在，**变异未登记** |

**对账判据（修真实冲突，不静默退役）**：

1. 每条 P 的每条子要求 SHALL 有承载叶与可运行锚；无锚者（今 P5/P6）SHALL 补锚或改绑到既有检查器，MUST NOT 以「登记表自述 unguarded」充当已达成。
2. `ci_gate` SHALL 指实际执行该锚的作业；门只比作业名存在即不足（今 P4 即反例），须把「作业是否执行该包/该选择器」纳入判据。
3. 变异记录 SHALL 由运行产物产生、且杀死它的是判据点名的断言；今 P4 由 journal-bytes 先响而登记断言是 wallClock，属**未证**，不得计入达成。
4. 未达项 SHALL 保留为未达并登记到叶；MUST NOT 以删除要求、改判据或改宽阈值换取门绿。
5. concept_budget 今日 FAILED（`FactLog` 接口方法超棘轮）：00/05 域的托管改造抬高了导出面而未登记，属真冲突，须按实测处置（登记理由或回卷），MUST NOT 改 baseline 掩盖。

## 十、R2 计划余项逐条处置台账

| # | 余项 | 现状证据（今日实测） | 处置 | 前置/阻塞 |
|---|---|---|---|---|
| 1 | 死生产者 `ReplayProjection.Fold`／`RebuildProjectionFromWAL` 家族 | 生产调用方 0（非测试引用只剩定义、`skeleton_assert.go` 断言与 `replay_budget.go` doc）；12 个测试锚待迁移 | 删＋级联回卷＋锚先迁后删（原 5.3.1–5.3.4） | 4 个测试文件被 config-surface 占用 → blocked |
| 2 | 六端口兼容读法 | `compatMaterialOf` 仅有 `materialOf` 一处调用，9 条生产 face 均自带 `Material()`；剩余消费方是契约示例与测试 | 审计表 → 甲/乙上报裁定（原 5.4） | 契约示例是否算独立使用方须裁定 |
| 3 | `compression-recovery-ticket` 退役 | `golden/runtime/architecture.json` 把 `Stateful`/`StateRegistry` 锚绑在该 spec | 先改绑 `session-checkpoint-material` 再退役 | golden 属 01/03 共用写面，须单写者 |
| 4 | legacy 条款退役（`production-wiring`／`-fix`／`storage-lifecycle-completion`／`event-segment-store`／`architecture-review-standards`／`memory-curation`） | legacy 符号 0；面 23+17+9+7+3 处 | 逐条改写/退役，不新增 spec_hygiene 违例 | 与 06 域脚手架接线边界对齐 |
| 5 | 未接线脚手架 `memory/ledger/{lifecycle,retention,guard}.go` | 非测试引用只在包内互引；`NewRetention`/`NewLifecycle` 仅测试构造（`memory/partition_test.go:124/412`、`tests/integration/history/recovery_before_reclamation_test.go:39/203`），装配面 0 处 | 接线（退位水位制用它）或删；**不得**以 skeleton_assert 断言充当消费者 | 归 06 域的退位接线；本域只出处置与残留零证 |
| 6 | 历史词汇与 golden prose | `durable_truth.json` reason 仍 `checkpoint:<partitionSeq>`；词汇禁用词表见原 5.1.8 | 词汇核对＋golden prose 同步（原 5.1.8／5.2） | 无 |
| 7 | 派生文档残留 | `docs/api/github_com__SpellingDragon__tagent__features__evolution__governance.md` 仍含 `BindStore` | `scripts/gates/gen_godoc.sh` 单写者统一重生成 | 与既有 6 包失同步同批 |
| 8 | 嵌套 change 不被 CLI 寻址 | `openspec validate --changes --strict --json` 只列顶层；本域 delta 的 strict 在父级合入/归档时生效 | 上报编排者（原 5.6），本域不改父级文件 | 编排者写集 |

**处置纪律**：每行给出「删/接线/改写/退役/上报」其一；「删除」行须同时给出同族 KEEP 符号的命中 >0（`ErrChainPageLimit`、`FileSegmentStore`、`MigrateShardedRoot`、`BenchmarkFoldDepthCost`）；「接线」行须给出非测试、非 `skeleton_assert` 的生产调用方。「blocked」行不得计入完成。

## 十一、R3 交接面与归档前验证

### 1. no-prefix 交接（唯一 owner）

`9547faca` 的提交说明是「票据注入测试断言归正（spec 正确，测试冻结了无前缀裸正文）」。落点是测试侧仍按裸前缀取正文：`smokeSettleNoticeOf`（examples/wechat-bot/smoke_test.go:535-544）要求 `HasPrefix(content, "tagent: task")`，而生产材料已统一为 `[evt_KEY|type] ` 前缀。本域只登记归属与判据，不修改 `no-prefix-criterion` 的文件：

- 交接项：呈现侧票据前缀的单一拥有者；测试侧裸前缀匹配器的归属（04 域消费者修复 or no-prefix）。
- 判据：同一失配不得同时被两处认领，也不得两处都不认（今日即两处都不认的状态）。
- 若当前验收与后置启动条件构成循环，设计门先定归属再动手。

### 2. config-surface 耦合

两笔合提（本变更＋`config-surface-normalization`）；4 个测试文件被其未提交改动占用。规则：只在其终态上追加；不覆盖、不回滚；两个时点的测试结论不得混用；已有候选不称完成。

### 3. 源码与主 spec 同步、归档前验证

- 主 spec 同步在实施/归档经 `openspec archive`（不 `--skip-specs`、不 `--no-validate`）；delta 保 `## ADDED/MODIFIED Requirements` 语法，归档前不得改回主 spec 结构。
- **归档前必须显式消费完成判据**：`openspec archive -y` 只跳过确认提示，不检查 DoD（`openspec archive --help` 无 DoD 相关选项）。故归档前须独立运行 `check_dod_coverage.py` 与 13 的放行证据门，任一非零即不得归档。
- 归档后复跑 `bash scripts/gates/check-openspec.sh` 与 `openspec validate --specs --strict --no-interactive`。

## 十二、10.R8 持久面端口收口：语义归属推导与冻结迁移序列

### 1. 今日实测读数

```sh
go build ./...                                                                       # exit 0
go vet ./runtime/state/ ./runtime/task/approval/ ./features/evolution/governance/    # exit 0
python3 scripts/gates/check_concept_budget.py --json
# errors: S4 contract-surface runtime/task/approval/attention_log.go:AttentionFactLog: no 红线 marker in Godoc (not baselined)
#         S1 iface-methods  runtime/task/approval/attention_log.go:AttentionFactLog: 3 methods > 2 (polymorphic port; 2 implementer(s)) (not baselined)
# counts: exported_interfaces=85 interfaces=121 domain_surface.new=55 removed=5 relocations=1 bindings=56 baseline_entries=78 findings.iface-methods=16
```

另以该检查器自身的解析函数（`collect_sources`／`build_index`／`implementer_count`）在树上直接求值：`FactLog{Put,Get,Scan,Sync}`、`GovernanceEventLog{Put,Scan,Sync}`、`AttentionFactLog{Put,Scan,Sync}` 三者的实现者数都是 2，且实现者都是同一对 `TierFacts`、`memoryFactLog`（`Put+Get+Scan+Sync` 与 `Put+Scan+Sync` 两套方法名的接收者集合相同，无第三个类型）。用 `exported_type_inventory` 对基线提交与当前树求差并模拟统一后的清单：

| 计数 | 今日 | 统一后 | 依据（检查器口径） |
|---|---|---|---|
| `exported_interfaces` | 85 | **83** | 登记包内导出接口**声明**数（名字首字母大写）；两个名字各减一，`FactLog` 搬迁净 0 |
| `domain_surface.new` | 55 | **54** | 基线与当前按**名字集合**求差；`GovernanceEventLog` 在基线里故落 removed，`AttentionFactLog` 不在基线里故从 new 落 removed |
| `domain_surface.removed` | 5 | 6 | 同上 |
| `domain_surface.relocations` | 1（`FactCommit`） | 2（`+FactLog`） | 同名、文件集不同 |
| `domain_surface.bindings` | 56 | 55 | 删 `AttentionFactLog` 绑定；`FactLog` 名在基线已有，为其加绑定会被判「不需要的绑定」warn |

⇒ 「净减 2（85→83）」逐名成立。**归因勘误（R172）**：用户裁定的是「共享叶子统一端口」这一方向（ORCHESTRATION R85）；**净减 2 这个数是编排者按三端口收口为一端口推导的执行结果，不是用户原话裁定的数字**，原写「用户裁定的净减 2」扩大了裁决面。同一批实测把 `new_max` 的实测值降到 54（G3 的 `84→85` 与 `54→56` 两处抬升一并撤回）。

### 2. 落点判定：既有包的三方交集只剩 api/event 与 api/plugin

冻结 `edges` 决定可选面：三个消费者 `runtime/state`（edges 474）、`features/evolution/governance`（670）、`runtime/task/approval`（689）的 `allow` 三元交集＝`{api/event, api/plugin}`。`memory` 与 `runtime/state` 同在后两方的 `forbidden`；`runtime/task` 只在 state 与 approval 的 allow 内，governance 未列。

| 候选 | allow 依据 | 语义是否匹配 | 取舍 |
|---|---|---|---|
| `api/event` | 三方均 allow；其 edge 为 `allow: []`（395） | **不匹配**：登记 reason 是「事件身份与事实载体；纯叶子」，其唯一接口 `Fact`（fact.go:18）只问答身份与冻结类型，无 `context`、无 `error`；`Put/Scan/Sync` 是存储缝的义务，不是事件词汇 | 零 edges 改动，但要先把该包的登记 reason 改宽才装得下它——边界跟着实现走 |
| `api/plugin` | 三方均 allow | 不匹配：三个拓展点的阶段化插件协议（Governor/RiskLevel/Decision） | 排除 |
| `api/<新叶>`，推荐 `api/durable` | 新建，`allow: []` 即纯叶 | **匹配**：包名说的是耐久轴，类型名仍是 `FactLog` ⇒ `durable.FactLog` | 需在 `packages`＋`edges`＋根 allow 各登记一条，并给三方 allow 各加一项；理由写进 architecture.json |
| 放宽 governance／approval 的 allow 收 `runtime/state` | 需改两条冻结边 | 不匹配语义单元：两域的 reason 明写「不拥有状态、不依赖执行器」 | 比新建叶更坏，否决 |

**推荐 `api/durable`，类型名保持 `FactLog`**（不造第三个名字）。推导三条：

1. 三个端口各自写着**同一句解释**：签名只用内置类型，故后端结构上满足而两向无需包边。三个词讲一件事＝同一契约被命名两次；统一后这句话只在一处陈述，另两处由 `durable.FactLog` 的类型名承担。
2. `api/event` 的 `Fact` 是「事实是什么」的词汇契约，本端口是「事实如何耐久」的行为契约。把后者塞进纯词汇叶，等于让事件叶承担存储缝。
3. 名字用既有词：`durable` 已在 `DurableCommit`／`durable_truth`／`durable-delivery` 上，`FactLog` 已在这三个端口上。备选逐个否掉——`api/fact` 与 `api/event.Fact` 同名异义；`api/factlog` 与类型名叠字；`api/store` 与 `runtime/state.Store` 及 `atomic_store` 的引擎义冲突，而本端口刻意不是引擎（无删除、无键序）。

### 3. `Get` 归属：统一端口取 `Put`／`Scan`／`Sync` 三方法

`Get` 的调用方枚举（`grep -rIn --include=*.go -E "\.Get\("` 逐文件计数后再看接收者类型）：

| 面 | 命中 |
|---|---|
| 生产 `.go`——接触该端口的九个文件 `assembly/{commit,host,host_delivery}.go`、`features/evolution/governance/checkpoint.go`、`runtime/state/{durable,mailbox,mailbox_checkpoint,skeleton_assert}.go`、`memory/ledger/tier_facts.go` | **0** |
| 端口实现 | `memory/ledger/tier_facts.go:35`、`assembly/commit.go:331` |
| 测试与样例 | `memory/ledger/journal_test.go:312,316`（经 `TierFactLog(...).Get` 断言）；`runtime/state/durable_test.go:1189`、`assembly/host_delivery_test.go:1157`、`tests/integration/state/delegation_lineage_record_test.go:154`（三者是转发或实现）；`examples/contract/runtime_state/FactLog_example.go:27,68` |

三个生产消费者的实际用法：`DurableCommit` 只用 `Scan`（durable.go:691）与五对 `Put`+`Sync`（:741/:746、:797/:802、:840/:845、:937/:942、:971/:975）；`configChain` 只用 `Scan`（assembly/commit.go:220）与一对 `Put`+`Sync`（:262/:265）；`Host` 的迁移只 `Scan`（host_delivery.go:2165）。生产的点读走 `Run.CommittedEvent`（host_delivery.go:592），不经该端口。

⇒ **取三方法**。理由：端口该陈述消费者真正要求的（governance 与 approval 从不点读）；S1 债务由 4 降 3；红线预算只容 4 行（§十二·4）。代价三项且都落在测试与一处公开方法：删 `TierFacts.Get`／`memoryFactLog.Get`（唯二调用方是测试与样例）；`journal_test.go:312,316` 两条断言改经该包自己的读面 `kv.Read(ctx, Fetch{Key: ...})` 复述，判据逐字保留（经 tier 提交的事实重开后按身份读回，且空体算持有）；`runtime/state/skeleton_assert.go:228` 的 `_ FactLog = (FactLog)(nil)` 随搬迁成为自证断言，删而非改写。
取四方法的代价（若改裁）：每个后端被要求实现一个无生产消费者的方法，且第七条红线无处安放（见下条预算）。

### 4. S1 与 S4 的迁移形态

**S1**（`scripts/gates/check_concept_budget.py:142`）：等额一条随端口走——键改路径、observed 由 4 降 3、注释合并原有两条理由：

```python
# 事实的耐久写读面 3m：同一份事实账上的三种动作（写入、前缀回放、Sync 屏障），
# 三本账（任务事实、治理事件、审批注意力）由同一对实现者承担（TierFacts／memoryFactLog）；
# 拆窄口会让前缀回放绕开屏障另立一条读路，与本域「一条链一本账」的裁决冲突。
("iface-methods", "api/durable/fact_log.go:FactLog", 3),
```

`:128-131` 的 `GovernanceEventLog` 条目连同四行注释删除；`AttentionFactLog` 无基线条目，不新增。结果：`baseline_entries` 78→77、`findings.iface-methods` 16→15、该面两个 error 清零（3>2 仍成立，走既有豁免 warn）。

**S4 红线与样例**。`doc_block()` 只读紧贴 `type X interface {` 上方、中间不空行的 `//` 块，故红线清单必须写进类型自己的 doc 块（文件头被 `package` 行隔断，`attention_log.go` 今日的 S4 error 即此）。预算口径实测（`check_concept_budget.py:669-671`）：`REDLINE_MUST_RE` 连 `// MUST NOT` 行也计一次，故 `FactLog` 现有四行＝`must 4 + must_not 2 = 6`，**恰等于 `REDLINE_LIMIT`**，统一端口一行都不得加。

| 候选红线 | 判定 |
|---|---|
| `durable.go:29-32` 四条（暂存不声称持久／屏障之后才报成功／不得把暂存当已存／前缀扫描不得给子集） | 全部是存储面义务，**整块沿用原样**，即已用满的 6 点 |
| 治理侧「按本族序号键」「不得改写或丢弃已存事件」 | 生产者义务，留在 `DenialLedger`／`writeGovernanceEvent` 的 doc，不进端口 |
| 审批侧「被拒写须具名」 | 与「不得把暂存当已存」同义，由端口第三条承担 |
| 审批侧「正文陈述他人身份即拒」「折叠回链须与在场裁决一致」 | 折叠义务，留在 `attention_log.go` 文件头与折叠函数上 |

**样例**：路径由 `example_path()` 机械算出＝`examples/contract/<包路径的 / 换 _>/<Name>_example.go` ⇒ `examples/contract/api_durable/FactLog_example.go`，正文须含 `func ExampleFactLog()`。手法＝`git mv` 现有样例，改 `package stateexample`→`package durableexample`、import 换 `api/durable`、断言换 `var _ durable.FactLog = (*stagedLog)(nil)`、删 `Get` 方法及其调用；同目录补 `doc.go`（`examples/contract/` 下 22 个 slug 目录均有 `doc.go`，仅 `assembly`／`bench` 例外）。`examples/` 在 `scope.exclude_dir_names` 内且免包登记，新建目录不改 golden。
`examples/contract/features_evolution_governance/` 删样例后只剩 `doc.go`、其主题随之消失 ⇒ 整目录删除。`examples/contract/runtime_task_approval/` 仍有 `ApprovalChannel_example.go`，只删 `AttentionFactLog_example.go`。`examples/contract/runtime_state/doc.go` 的「the durable and migration seams」删去 durable 一项。

### 5. 波及面

**生产**
- `runtime/state/durable.go:20-43`（doc 块＋接口，整块移走；`:23` 的「put, get, prefix scan」随三方法改写）、`:168`、`:182`、`:245`、`:261`、`:268`
- `runtime/state/skeleton_assert.go:228`（删）、`:230`（形参类型）
- `features/evolution/governance/denial.go:19-39`（删）、`:108`、`:132`、`:298`、`:323`、`:457`、`:483`、`:518`
- `features/evolution/governance/goal.go:114`、`:135`、`:350`、`:411`；`checkpoint.go:175`；`skeleton_assert.go:134`（删）
- `runtime/task/approval/attention_log.go:1-15`（文件头红线清单改归属）、`:30-41`（删）、`:75`、`:95`、`:129`、`:156`、`:166`、`:184`
- `runtime/task/approval/attention_store.go:81`、`approval_store.go:93`、`:148`
- `assembly/commit.go:189`、`:195`、`:312-352`（`memoryFactLog`，删 `Get`）；`host.go:232`；`host_delivery.go:559`；`governance_checkpoint.go:60`
- `memory/ledger/tier_facts.go:7-10`（doc 改指新包）、`:31-42`（删 `Get`）

**测试**
- `runtime/state/durable_test.go:831`、`:841`、`:864`、`:915`、`:1179`、`:1189`、`:1242`、`:1305`、`:1353`；`durable_seat_test.go:194`、`:517`
- `assembly/host_delivery_test.go:1142`、`:1157`、`:1383`；`assembly/governance_checkpoint_test.go:143`
- `runtime/task/approval/approval_store_test.go:103`
- `tests/integration/history/event_integrity_test.go:231`、`:628`、`:642`、`:652`、`:2907`、`:2920`、`:2921`、`:2955`、`:2961`
- `tests/integration/state/delegation_lineage_record_test.go:154`；`tests/integration/capabilities/governance_recovery_test.go:418`
- `memory/ledger/journal_test.go:312`、`:316`

**golden 与门**
- `golden/runtime/architecture.json:2-6`（`exported_interfaces_max` 85→83、`new_max` 56→54、note 追加撤回与逐名理由）
- 同文件 `packages`（`api/durable` 一条：`pattern`／`role: "durable-facts"`／`required: true`／reason）、`edges`（新条 `allow: []`＋reason；`assembly`:330、`runtime/state`:474、`features/evolution/governance`:670、`runtime/task/approval`:689 四方 allow 各加一项）、`:168`（governance 的 reason 删去 `GovernanceEventLog` 一名）、`:1478`（`concept_bindings.bindings.AttentionFactLog` 整条删）
- `scripts/gates/check_concept_budget.py:128-131`（删）、`:142`（键改＋observed 改 3＋注释合并）
- `scripts/gates/product_surface_baseline.json` 不动：该门只核导出**函数**，`TierFacts.Get` 是方法

**spec 与文档**
- 主 spec 对这 4 个名字**零命中**（`grep -rn "GovernanceEventLog\|FactLog\|AttentionFactLog" openspec/specs/` 无输出），故无「以端口名命名的能力条款」要改。`openspec/specs/session-checkpoint-material/spec.md` 的「插件 Material 声明以时机与失败语义为策略」条款里的「六端口」指 `api/plugin` 的插件面端口，与本三处持久面端口无关，MUST NOT 误改。
- `docs/api/**` 是生成物（`scripts/gates/lint.sh:242` 校验新鲜度），末步 `bash scripts/gates/gen_godoc.sh` 重生成；`docs/wiki/reliability/durable-delivery.md:57` 那句「`FactLog`：`Put`/`Get`/`Scan`/`Sync`，四条红线」改为三方法并指向新包（与 10.R6 的文档面合单，避免同文件两写者）。

### 6. 冻结迁移序列（每步结束可编译）

原子性判定：三个接口各由一个包声明、消费者跨包，故不能「先删声明再改消费者」。序列为「先建新家 → 消费者改指 → 再删声明」：

| 步 | 动作 | 结束态 |
|---|---|---|
| 1 | 新建 `api/durable/fact_log.go`（`package durable`＋doc 块＋三方法接口）；architecture.json 登记 `packages`／`edges`／根 allow＋三方 allow | 可编译；`exported_interfaces` 临时 86（超 85 上限），登记为预期中间态，不得当 gate 绿 |
| 2 | 全部消费者改指 `durable.FactLog`：state 6 处、governance 12 处、approval 9 处、assembly 5 处，测试与样例按上表 | 可编译；旧三接口零引用但仍在声明 |
| 3 | 删三接口与各自 doc 块、两处自证断言行、`memoryFactLog.Get`、`TierFacts.Get`；`journal_test.go` 两条断言改经 `kv.Read` 复述 | 可编译；旧名零残留 |
| 4 | 删 `examples/contract/features_evolution_governance/` 整目录与 `AttentionFactLog_example.go`；`git mv` FactLog 样例到 `api_durable/` 并补 `doc.go` | 可编译；S4 样例路径与接口同步 |
| 5 | golden 与门：ratchet 两数下调、删 `AttentionFactLog` 绑定、改 governance reason、`check_concept_budget.py` 两条目 | 门绿；`exported_interfaces=83` |
| 6 | `bash scripts/gates/gen_godoc.sh` 重生成 `docs/api/**`；wiki 一句改写（与 10.R6 合单） | `lint.sh` 的 docs/api 新鲜度段绿 |

**净减 2 逐名核对**

| 名 | 今日位置 | 变更 | 计数 |
|---|---|---|---|
| `FactLog` | `runtime/state/durable.go:33` | 声明移到 `api/durable/fact_log.go` | 1→1（净 0） |
| `GovernanceEventLog` | `features/evolution/governance/denial.go:31` | 删 | 1→0 |
| `AttentionFactLog` | `runtime/task/approval/attention_log.go:35` | 删 | 1→0 |
| 合计 | | | 85→**83** |

**实现者不随接口搬家而变**：`TierFacts`、`memoryFactLog` 都已有 `Put/Scan/Sync`，结构满足，两向都不需要 import 新叶；`memory/ledger` 不新增 edge 项，`TierFacts` 也不加 `var _ durable.FactLog` 断言——那会为一处声明新增第二条陈述，而结构满足正是该端口只用内置类型的目的。

### 7. 风险、判别测试与回退

1. **漏改消费者**（最高风险，编译期可判）：`go build ./...`＋`go vet ./runtime/state/ ./runtime/task/approval/ ./features/evolution/governance/ ./assembly/ ./tests/...`。判别测试＝在步 3 之前删掉新叶的一次编译必红，且红点落在改指处而非别处。
2. **`Get` 删除的连带**：`go test ./memory/ledger/ ./runtime/state/ ./assembly/`。判别测试＝`journal_test.go` 两条断言在删 `Get` 前红、改经 `kv.Read` 后绿，且改后仍断言「空体也算持有」——削弱这两条换取编译通过即违约。`memory/ledger` 归 11 域的原子改造写面，步 3 的 `TierFacts.Get` 删除须等该写面释放；未释放时只做其余部分并如实登记未完成的删除项，不伪称零残留。
3. **红线条数越预算**：`FactLog` 的 doc 块实测恰用满 6 点，任何新增红线行即 S4 红。判别测试＝在 doc 块加一行 `// MUST NOT …` 后门必红，删回即绿。
4. **新叶未登记**：`check_runtime_architecture.py` 对未登记包与缺 edge 条各判红。判别测试＝故意漏掉根 `assembly` 的 allow 项，门报 `composition root does not list package 'api/durable'`。
5. **棘轮方向**：`exported_interfaces_max` 与 `new_max` 只降不升。若实施时实测不是 83／54，以门输出为准并在报告附复跑命令，不得照抄本节数字。
6. **回退**：纯声明搬迁、零行为变更、零持久格式变更 ⇒ 单提交 `git revert` 即回退，无需数据迁移。回退验收＝`go build ./...` 0 且 `check_concept_budget.py` 回到今日两条 `AttentionFactLog` error 的现状，如实登记为红，不伪称绿。
7. **与相邻叶的写面冲突**：10.R7 删 `NewDurableCommit`（`durable.go:239-245`）与本叶同文件 ⇒ 串行；10.R5／R6 触 spec 与文档 ⇒ 步 6 的 wiki 改写合单。

### 8. 须实施单实测的开口项

1. 统一后五项计数（`exported_interfaces`／`new`／`removed`／`relocations`／`bindings`）的实跑值：本节来自对当前树与基线提交的离线求差，实施单须用同一门复跑，尤其 `relocations` 由 1 变 2 是否只作计数而无判据。
2. `api/durable` 登记后 `check_runtime_architecture.py --implementation` 的 `policy_packages`／`policy_edges` 实读数。
3. `gen_godoc.sh --check` 在删两个接口后的生成物差异范围（`docs/api/index.md` 是否列包）。
4. `journal_test.go` 两条断言改经 `kv.Read` 后的实跑结果，以及 `memory/ledger` 写面是否已释放（决定步 3 能否一次做完）。
5. `REDLINE_MUST_RE` 对 `// MUST NOT` 行双计的实测：本节按正则读得，须以「加一行即红」的变异证明一次。

## 十三、当前承接与归档边界（计划勘误，未执行）

**R1–R4 被拆分替代，不等于旧清理完成。** 当前映射以 tasks 的转交段为准：R5 管主规格，R6 管现状/派生文档，R7 管 golden/门与插件 Material 兼容读法，R8 管持久端口；新增 R9a 枚举消费者、R9b 迁 replay 锚及裁删、R9c 核原文六入口。旧 5.5/5.6 的域级验证与同步限制交 13.R4/编排者，R1 的要求对账仍由 R7/13 消费，不另造完成账。

| 旧项/现树依据 | 当前承接与完成条件 |
|---|---|
| 5.1.2、5.3.1–5.3.4：`memory/replay/projection.go:342` 仍定义 Rebuild，`fold.go:49` 仍定义 Fold；断言/doc/测试仍引用 | R9a 复核全部直接/间接生产使用方；R9b 按 §四十二文件逐判据迁移后再裁删，与 R5 的 event-sourced-projection 同批。家族互引、断言/注释不算生产消费者，但测试迁移义务不消失；KEEP 清单与 retained-ref 独有断言分开处理。 |
| 原文六读入口：OriginalStore、ScopedOriginalStore、ScopedReader 各两条读法；`reference.go:223-264` 的绑定读及 scoped 读仍可达，后者调用 ensureLoaded | R9c 独立枚举六入口到真实 recall 消费者，活的保留；全量加载是成本问题，不是死码证据。不能与 §五插件六端口或 R8 三个耐久接口混算。 |
| 5.1.3：`golden/runtime/architecture.json:1429-1437` 仍将 Stateful/StateRegistry 绑 compression-recovery-ticket | R7 先迁两锚到对应现行恢复条款，concept 门通过后 R5 才 superseded；不能先退役再把门红解释成文档债。 |
| 5.1.4/5.1.5：production-wiring(:11/:46) 与 -fix(:10/:44) 仍要求 Compactor，其他 legacy 条款见 §二 | R5 逐 Requirement 对生产接线定 owner/改写/退役；保留 FileSegmentStore、关闭/迁移等现行语义，不按名称整片删除。 |
| 5.1.3/5.1.8：compaction-scheduler:5 的 superseded 历史段仍称“物理有界走退位” | R5 在原段旁附限定勘误：退位机制已删、磁盘历史增长已接受；不改历史原文、不恢复旧机制、不改 GLOSSARY 本义。R6 只清现状文档的同义残留。 |
| 5.2/5.4：checkpoint reason 与插件六端口兼容读法 | R7 保留前缀值、核历史乙案的真实消费者后更正文案；不把端口常量当死码。5.7 已做项只提供历史证据，仍在对应规格终验中核现树。 |

**死码门边界**：`scripts/gates/check_dead_producers.py:11-20` 将测试/示例与同名接口方法纳入可达性；其 PASS 只能说明该算法没发现无消费者，不能推出“零生产死码”。R9a 的生产调用图与迁锚清单是独立证据，不能为迎合该门删测试或改 baseline。

**注释门归因勘误**：不传 scope 时 `scripts/comment_policy/main.go:160-162` 默认 `[.]`，正式 `lint.sh:36/:86` 是三模块；`:271-274` 可在总 findings 汇总前因 scan-set 不符退出，且未开 `-v` 本就不逐条打印。因此撤回该次失败“来自外来文件”与“零 finding”的归因，真实数值未核。R7 用正式三模块及产品测量两组完整命令分别取证；不修改 baseline，不把测量模式 exit 0 等同零 finding。

**能否先 commit/归档已确定部分**：可讨论独立提交已核部分以保存进度，但先给精确文件/依赖闭包、现树证据及未完成清单，明确“阶段提交≠完成”；有耦合的 spec/golden/代码/锚一起交付，不能按 checkbox 挑文件。整个 runtime-recovery-ideal 暂不具备完成归档证据；不能用局部提交替代 09 校准/矩阵、13 现树绑定/正式接线与 F3/R35/F4。若要拆成可独立归档能力，另行确定切面和同步方案，不由本六文件改写授权。本轮无 git 写、无 archive，历史全文存档也不等于 OpenSpec 完成归档。

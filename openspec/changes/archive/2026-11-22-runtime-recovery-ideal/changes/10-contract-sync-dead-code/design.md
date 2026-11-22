> **历史输入（2026-11-20）**：本目录不再作为当前派发或自动规格同步输入；下文旧勾选、方案与限制按原时点保留，不授权后续实施。当前只按[编排入口](../../ORCHESTRATION.md)派发三个语义单元，全部旧要求由[承接表](../../CARRYOVER.md)逐项接收，不因转历史而取消。

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

> **已被 §二十三 取代（事实修正版，2026-11-21）**：本节六行「变异未登记」、P1 锚名、P4 的 `ci_gate`／`measure`／阈值、P5/P6「无锚」四处与今日登记表不符，按 10.R1 负锚①「表与 design D1 不一致即红」就地留档不改写，读数以 §二十三 为准。

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

> **已被 §二十四 取代（逐行处置终表，2026-11-21）**：本节是计划期台账（8 个归组），10.R2 要求的是**旧 5.x／R1–R4 每行恰一处置**，故按旧行号重排为 §二十四；本节原文留作核销索引。

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

### 9. 10.R8 终态复核与现树重计（执行凭据，2026-11-19）

**本节取代 §十二·1 的两列预测值，重计口径照门实跑，不抬棘轮。** §十二·1 的 `85→83`／`55→54`／`removed 5→6`／`relocations 1→2`／`bindings 56→55` 是推导期对当时代码的离线求差；此后多波命名面（`CommitOrder`／`IndexEntry`／`ChainSource`、`SettleAccount` 等）与三份端口声明删除同向作用，现树读数与之不同，实施单不得照抄预测数字。

| 计数 | §十二·1 预测 | 现树实测（`check_concept_budget.py`） |
|---|---|---|
| `exported_interfaces` | 83 | **79** |
| `domain_surface.new` | 54 | **57**（棘轮 `new_max=57`，恰在上限） |
| `domain_surface.removed` | 6 | **32**（`removed_min=5`） |
| `domain_surface.relocations` | 2 | **5** |
| `domain_surface.bindings` | 55 | **57** |
| `baseline_entries` | 77 | **77**（exempted 77、stale 0） |

棘轮三数 `removed_min=5`／`new_max=57`／`exported_interfaces_max=85` **未按步 5 下调**（预测的 83/54 已过期，实测 79/57 全在限内）；收紧属 reviewed tightening，归 golden 写面另裁，本叶只保证「不抬」。S1 条目 `("iface-methods", "api/durable/fact_log.go:FactLog", 3)` 按名在位，走已登记 exempt warn；`GovernanceEventLog` 条目已删、`AttentionFactLog` 无新增条目。

**终态三事实（核验结论）**：①端口唯一——全仓 interface 方法含 `Put+Scan` 或 `Put+Sync` 者仅 `api/durable.FactLog`，无第三个同族端口；②旧名零残留仅限**声明/别名/引用**（`.go` 内 0 命中，`state.FactLog` 0，无 `type X = durable.FactLog`），三处散文残留属历史账（`architecture.json` 棘轮 note、`check_concept_budget.py:129` 注释、`doc_budget.json` 下调说明）；③`TierFacts.Get` **保留**——其活消费者是 `assembly/commit.go:231 addressedChainLog`（`:298`/`:308`），删则按地址装链退回整前缀扫描，故 `Get` 降级为该 tier 的引擎读能力，不回流端口、不建别名；`memoryFactLog.Get` 已删（回退扫描即设计预期）。

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

## 十四、10.R9a 消费者冻结清单（只读盘点产物，供 R5/R6/R7/R9b/R9c 直接消费）

**性质**：本节是**盘点不是清理**——本轮零产品代码改动、零删除、零 git 写。表中每个符号给出：定义位置 → 逐类消费者（生产调用／接口或注册间接调用／生产定义／契约示例／测试／编译断言／注释/doc／主spec条款／golden·门baseline）→ 处置（KEEP／迁移／裁删）→ 保留项／迁移断言／精确文件 → 承载叶。**消费本节的下游叶按此表施工，不得按符号名猜测删除。**

### 14.1 取证基线与可复跑命令

```bash
rg -n 'RebuildProjectionFromWAL|RebuildOptions|RebuildReport|RetainRefType|compatMaterialOf|GetScoped|ReadSegmentScoped|Compactor|compression-recovery-ticket' memory api assembly features runtime tool tests examples openspec/specs golden/runtime
```

原始结果：**132 行命中**，逐行归属已冻结（逐行清单见本节表 1–表 5；分类计数随表给出）。归属口径与两条拒绝规则由盘点脚本执行：

```text
R9a attribution: hits=132 attributed=132 uncovered=0 test-as-prod=0
  生产调用 1        接口/注册间接调用 9      生产定义(被删/保留判定面) 28   契约示例 9
  测试 35          编译断言 6               注释/doc 1                   主spec条款 32
  golden/门baseline 11
```

三条负面锚（同一脚本的变异模式，均已实测拒绝）：

| 负锚 | 变异 | 结果 |
|---|---|---|
| N1 漏一项 | 删掉 `tool/recall/get.go` 归类规则 | `N1 FAIL (漏项): tool/recall/get.go:153` exit 1 |
| N2 把测试当生产 | 把 `_test.go`/`tests/`/`examples/` 判成生产调用 | `N2 FAIL (把测试/示例当生产)` 44 条 exit 1 |
| N3 按名误删 KEEP | 把 KEEP 符号声明为裁删 | `按名误删 KEEP 被拒: MigrateShardedRoot 在删除集外仍有活引用 ['memory/ledger/config.go','memory/ledger/migrate.go','memory/ledger/migrate_test.go','scripts/gates/product_surface_baseline.json']` exit 1；`ReadSegmentScoped` 同法被拒（`assembly/tools.go`、`tool/recall/get.go`） |

KEEP 台账正锚：`R9a KEEP ledger: symbols=8 mode=positive violations=0` exit 0（8 个 KEEP 符号各自的承载面与名断言全部解析）。盘点脚本临存 `/tmp/r9a_attr.py`、`/tmp/r9a_keep.py`（非仓内产物，不入库；判定规则已在本节表格固化，复跑按上表重建即可）。

**并列取证（红项照录，不参与判定）**：

| 门 | 命令 | 本次原始结果 |
|---|---|---|
| 死生产者 | `python3 scripts/gates/check_dead_producers.py` | **exit 0**，`every one of 3505 production function(s) in 32 package(s) has a consumer`。该门把测试/示例调用算消费者，故 **PASS ≠ 零生产死码**；本节的“生产调用 0”是独立结论，不得反过来为迎合该门删测试或改 baseline。 |
| 概念预算 | `python3 scripts/gates/check_concept_budget.py` | **exit 1**，`ERROR: S1 iface-methods assembly/compress_consumer.go:compressSessions: 4 methods > 3 (consumer-side narrow port; 1 implementer(s)) (not baselined)`。归因：`git show HEAD:assembly/compress_consumer.go` 该接口 3 方法，工作树新增 `HoldsCompressProduct` 成 4 —— 这是 **03.R2 在飞瞬态**，不是本节结论，也不得据此改基线（design §九/5.1.6 记的旧红 `runtime/state/durable.go:FactLog: 5 methods > 4` 现已消失）。 |
| 产品面 | `python3 scripts/gates/check_product_surface.py` | exit 0，`0 error(s), 0 warning(s), 37 frozen test-only hook(s)`；`NewScopedReader` 在冻结的 `test_only_exported_funcs` 内（`scripts/gates/product_surface_baseline.json:18`），是“无产品调用者”的**门侧旁证**。 |

### 14.2 表 1 · Rebuild 家族与 `ReplayProjection.Fold`（承载：R9b）

| 符号 | 生产定义 | 生产调用 | 接口/注册间接 | 契约示例 | 测试 | 编译断言 | 注释 | 处置 |
|---|---|---|---|---|---|---|---|---|
| `RebuildProjectionFromWAL` | `memory/replay/projection.go:342` | **0** | 0 | **0** | 9 文件（见 14.3） | `skeleton_assert.go:101` | `replay_budget.go:67`（“what RebuildProjectionFromWAL replays into”） | **裁删（R9b）** |
| `RebuildOptions` | `projection.go:279` | **0** | 0 | 0 | 同上各调用点 | `skeleton_assert.go:91` | — | **裁删** |
| `RebuildReport` | `projection.go:292` | **0** | 0 | 0 | 同上各调用点（读 `.Entries/.Skipped/…`） | `skeleton_assert.go:92` | — | **裁删**；删后 `check_concept_budget.py:183` 的 `struct-fields memory/replay/projection.go:RebuildReport 9` 降为 **stale 警告**（非错误，须在 R7 一并清条目并记原因） |
| `ReplayProjection.Fold` | `memory/replay/fold.go:49` | **0** | 0 | 0 | `fold_test.go:42/:70/:82/:94`、`replay_budget_test.go:343`、`projection_replay_and_budget_test.go:152`、`request_boundary_test.go:369` | `skeleton_assert.go:32` | — | **裁删**；活继承者是年龄折路径 `FoldByAge/PlanByAge/ApplyProducts`（生产调用 `assembly/age_fold.go:184/:195`、`assembly/compress_consumer.go:290/:333`），迁移断言的对照面即此三方法 |
| `RetainRefType` | `fold.go:131`（doc `:127`） | **0 独立**；唯一生产引用 `fold.go:83` 在 `Fold` 体内 | 0 | 0 | `fold_test.go` 经 `Fold` 间接 | `skeleton_assert.go:103` | — | **随 `Fold` 同裁**：`Fold` 一死，`RetainRefType` 调用面归零；保留的判定语义（哪些事件类型留存）须由年龄折的现行实现承接，不得只删函数不落语义 |
| `ErrRebuildUnwired` / `ErrChainScan` / `ErrCompactionCorrupt` / `ErrRebuildPartial` | `projection.go:86/:91/:94/:98` | **0** | 0 | 0 | 家族测试断言 | — | — | **随家族同裁**（体内错误返回全部属 `RebuildProjectionFromWAL`/`ScanFrom` 的死分支，逐条核） |
| `ErrChainKeyRequired` | `projection.go:83` | **0** | 0 | 0 | `projection_replay_and_budget_test.go:361-362`、`partition_test.go:374-375/:386`、`notice_projection_test.go:146` | — | — | **随家族同裁，断言迁移单列**：这 3 处断言是“空 chain key 具名拒绝”的现存覆盖，迁移目标须在现行恢复入口上重钉，不得以“断言随符号消失”了账 |
| `RebuildSkipNotEmpty` | `projection.go:103`（`projection.go:350` 赋值） | **0** | 0 | 0 | `partition_test.go:329-386` 区间断言 | — | — | **随家族同裁**；`projection not empty` 的跳过语义若在现行恢复路径无对应，须在 R5 的 spec 改写里显式声明“该跳过语义已随重建入口退役”，不留悬空语义 |
| `ErrChainPageLimit` | `projection.go:88` | **1 活生产**：`projection.go:200`（`EventChain.ScanFrom` 拒绝零页） | 0 | **1**：`examples/contract/memory_replay/EventChain_example.go:19/:61` | `projection_replay_and_budget_test.go:49` | `skeleton_assert.go:107` | `projection.go:87/:152` | **KEEP**（`ScanFrom` 是活入口，与本家族无关；按名整片删除会误伤） |

### 14.3 「12 文件」复核（现树）

`rg -l 'RebuildProjectionFromWAL|RebuildOptions|RebuildReport|\.Fold\(|RetainRefType' -g '*_test.go'` 得**恰好 12 个**文件，与 design §四的十二文件表一致（逐个命中数）：

| # | 文件 | 命中 | 现树 git 状态 |
|---|---|---|---|
| 1 | `tests/integration/history/projection_replay_and_budget_test.go` | 13 | CLEAN |
| 2 | `memory/partition_test.go` | 9 | CLEAN |
| 3 | `memory/replay/fold_test.go` | 4 | CLEAN |
| 4 | `tests/integration/state/notice_projection_test.go` | 4 | CLEAN |
| 5 | `tests/integration/history/event_integrity_test.go` | 3 | CLEAN |
| 6 | `tests/integration/history/age_band_request_test.go` | 2 | CLEAN |
| 7 | `examples/wechat-bot/smoke_test.go` | 1 | **M（脏）** |
| 8 | `memory/replay/replay_budget_test.go` | 1 | CLEAN |
| 9 | `tests/integration/acceptance/scenario_age_fixtures_test.go` | 1 | CLEAN |
| 10 | `tests/integration/execution/cross_session_scoping_test.go` | 1 | CLEAN |
| 11 | `tests/integration/host/age_fold_test.go` | 1 | CLEAN |
| 12 | `tests/integration/host/request_boundary_test.go` | 1 | CLEAN |

**「4 冲突」现树复核结论：锚行冲突 = 0**（历史数字不再成立）。config-surface 已提交（`ec038779`、`0f84b2d1`），12 文件中 11 个 CLEAN；唯一脏的 `examples/wechat-bot/smoke_test.go` 的三个 hunk（`@@ -545,4 +545,2 @@`、`@@ -550 +547,0 @@`、`@@ -554,0 +552,19 @@`）全在 `smokeSettleNoticeOf` 体内（no-prefix 交接项），`git diff` 在该文件**零 `Rebuild|Fold` 命中**，重建锚 `:2384` 原样在位。R9b 可在该文件上直接改锚，不需等交接。

### 14.4 表 2 · 原文读入口六条 ＋ 第七条间接入口（承载：R9c）

| # | 入口 | 实现 | 生产调用 | 接口/注册间接调用 | 契约示例 | 测试 | 编译断言 | 处置 |
|---|---|---|---|---|---|---|---|---|
| 1 | `OriginalStore.Get` | `kv_original_store.go:354`（`entryLocked` 点读） | **1**：`scripts/fold-cold-root/main.go:743`（运维工具 `tier.Originals.Get`） | — | `examples/contract/memory_ledger/OriginalStore_example.go` | `kv_original_store_test.go` 等 | `skeleton_assert.go:74` | **KEEP**：消费者是仓内产品路径（`scripts/fold-cold-root` 是操作员入口，非测试替身）；opt：若该工具改用 scoped 读，须先给出等价字节证据再迁移 |
| 2 | `OriginalStore.ReadSegment` | `kv_original_store.go:390`（`entryLocked` 点读，**不复用** `ReadSegmentScoped`） | **0** | 0 | `OriginalStore_example.go:76` | `kv_original_store_test.go` | `skeleton_assert.go:76` | **裁删首选**：零生产调用、零接口转发；唯一“活”引用是契约示例与编译断言。契约示例与断言须随删同步（示例文件若整份失效，按 5.4 的示例豁免判据处置） |
| 3 | `ScopedOriginalStore.GetScoped` | `reference.go:244`（`ensureLoaded` 全量） | **0 直连**；仅两条转发：`ScopedReader.Get`（`reference.go:224`）、`RecallPort.GetScoped`（`assembly/tools.go:912`，体内 `:919` 转 `originals.GetScoped`） | **2**（上述两转发者） | `ScopedOriginalStore_example.go:25/:27/:43/:78/:86` | `kv_original_store_test.go:497`、`cross_session_scoping_test.go` | `skeleton_assert.go:78` | **迁移/裁删待 R9c 定**：两条转发者都无生产调用者（见 #5/#7 判据），故本条实为**零可达生产**；但它是 `scopedRecall` 的唯一装配口，删前须确认“按 scope 的精确召回”在新入口上仍可测 |
| 4 | `ScopedOriginalStore.ReadSegmentScoped` | `reference.go:258`（`ensureLoaded`） | **1 活生产链**：`tool/recall/get.go:153` → `assembly/tools.go:924`（`RecallPort.ReadSegmentScoped`，体内 `:931` 转 `originals.ReadSegmentScoped`）→ `tier.Originals` | **1**（`RecallPort`） | `ScopedOriginalStore_example.go:40/:42/:81/:89` | `search_real_store_test.go`、`cross_session_scoping_test.go` | `skeleton_assert.go:80` | **KEEP（本族唯一实活入口）**；其装配链 `assembly/host_document.go:778-779`（`reads.Originals = recallPort`，`recall.New(reads, …)`）是消费者锚，R9c 的正式入口实测须走此链，不得用测试直连替代 |
| 5 | `ScopedReader.Get` | `reference.go:223`（转 `r.store.GetScoped`） | **0 构造** | 0 | — | `original_reference_isolation_test.go`、`storage_capabilities_test.go`、`search_real_store_test.go` 经 `NewScopedReader` | `skeleton_assert.go:82` | **裁删**：`NewScopedReader` 全部 8 处调用都在 `tests/integration/{history,capabilities}`，产品零构造；`NewScopedReader` 亦在 `product_surface_baseline.json:18` 的 test-only 集合。删除后该 baseline 条目转 stale，R7 清条目并记因 |
| 6 | `ScopedReader.ReadSegment` | `reference.go:236`（转 `r.store.ReadSegmentScoped`） | **0 构造** | 0 | — | 同 #5 | `skeleton_assert.go:84` | **裁删**（与 #5 同一 `ScopedReader` 类型，同批） |
| **7** | **`RecallPort.GetScoped`／`RecallPort.ReadSegmentScoped`（第七条间接入口）** | `assembly/tools.go:839`（`RecallPort` 定义；`:849 var _ ledger.ScopedOriginalStore = (*RecallPort)(nil)`） | `ReadSegmentScoped`：1 活生产（`tool/recall/get.go:153`）；`GetScoped`：0 调用者 | **是**：`assembly/host_document.go:767-780` 晚绑定 `reads.Originals = recallPort` 后由 `recall.New` 生成召回工具 | — | `assembly` 侧测试 | — | **KEEP**：这正是六入口得以可达的**唯一生产通路**；R9c 若只数“六个方法”而不认这一条，就会把活入口判成死码并把整条召回链删掉 |

**第七条的次级间接入口**（一并列出，不以“六”截断）：`Run.RecallReads()`（`assembly/tools.go:159`，`:168-169` 以类型断言把 `ledger.ScopedOriginalStore` 绑到 `reads.Originals`）。另记两条**非入口**的 concretemethods，避免按名混淆：`BlobCount`（`kv_original_store.go:428`，`ensureLoaded`）与 `References`（`:440`）是仅测试使用的观察面，其全量成本属性能议题，**不是**入口死亡证据（design §十三已裁）。

**成本归属（不计入死码判定）**：`Get`/`ReadSegment` 走 `entryLocked` 两点读；`GetScoped`/`ReadSegmentScoped`/`BlobCount` 走 `ensureLoaded` 的整族前缀遍历。故“`ensureLoaded` 贵”只经 scoped 面与 `BlobCount` 抵达，**与 #1/#2 的存活无关**；性能改造归 06，不得把成本当裁删理由。

### 14.5 表 3 · `compatMaterialOf`（承载：R7）

| 项 | 事实 | 处置 |
|---|---|---|
| 定义 | `api/plugin/material.go:203`（doc `:197`） | — |
| 调用 | 唯一生产调用 `material.go:194`，位于 `materialOf` 体内；`materialOf` 由 `NewFlow`（`material.go:244`，`:250`）触发 | — |
| 生产面声明 `Material()` | **9 个面全部声明**：`assembly/host_tools.go:1416`、`assembly/age_fold.go:141`、`assembly/settle_turn.go:304`、`assembly/facts.go:1147`、`assembly/compress_consumer.go:149`、`runtime/agent/final_round_planner.go:97`、`features/rl/lease_observer.go:83`、`features/evolution/governance/policy.go:104`、`features/evolution/meditation/driver.go:110` | 生产路径上 `materialDeclarer` 断言恒真 ⇒ **`compatMaterialOf` 生产可达 = 0** |
| 兼容路径真实消费者 | **15 个契约示例面，分布在 11 个文件**（`examples/contract/api_plugin/` 的 `Instance_example.go` 2、`EnvironmentInject_example.go` 2、`CompressTrigger_example.go` 2、`TransformCall_refusal_example.go` 2、`RiskAssessor_example.go`、`FinalPresenter_example.go`、`RiskPolicy_example.go`、`Presentation_example.go`、`PostCommitObserver_example.go`、`Governor_example.go`、`TransformCall_example.go`），**零个声明 `Material()`**；另有 `api/plugin/{phases,material,observe_duty}_test.go` 的测试面 | design §五“14 个”**更正为 15 面/11 文件**（现树实测） |
| 甲案（示例补 `Material()`，删 `compatMaterialOf`）成本 | `Instance` 增至 3 方法 ⇒ 越 `POLYMORPHIC_METHOD_LIMIT=2`，且 `api/plugin:Instance` **未在** `check_concept_budget.py:120-156` 基线 ⇒ 须新增具名 S1 基线条目；另改 15 示例面 ＋ N 测试面 | 高，且抬基线 |
| 乙案（保留兼容读，只更正文案） | 只需设计 `admitted` 映射的 golden 文案勘误 | 低 |
| 冻结倾向 | **乙案**：`compatMaterialOf` 是**现役兼容读法**（有 15 个活契约示例消费者），按 R7 原文“不把端口常量当死码、先核真实消费者后更正文案”处置；**不裁删**。`admitted` 映射的语义 owner 留 `api/plugin`（`material.go:171-178`） | — |

### 14.6 表 4 · `Compactor`（承载：R5）

| 项 | 事实 |
|---|---|
| 代码面 | `rg 'Compactor\|checkAndCompact\|CompactL1ToL2\|CompactL2ToL3\|filterTombstoned\|LifecycleManager\|TombstoneSet\|SetCompactor'` 在 `*.go` 全仓 **0 命中**（含测试） |
| 仍要求创建它的主 spec | `production-wiring/spec.md:11/:31/:34/:35/:37/:41/:46/:53/:55/:62`、`production-wiring-fix/spec.md:10/:17/:23/:44/:49/:89`、`storage-lifecycle-completion/spec.md:5/:11/:25/:34/:43`、`event-segment-store/spec.md:227/:233/:293` |
| golden 陈旧预览 | `golden/runtime/spec_hygiene.json:382/:390/:398/:406/:1198/:1262/:1270/:1278/:1326`（改 spec 后转 stale，须随 R7 重生） |
| 处置 | R5 逐 Requirement 定现行 owner：**改写为现行存储接线**而非“补齐 Compactor”；活 owner 是 `ledger.OpenTier`（`memory/ledger/config.go:282`）与 `ledger.OpenAtomicStore`（`:224`），由 `assembly/run.go:609` 消费。`FileSegmentStore`（`memory/ledger/segment_store.go:276`，`config.go:100` 字段、`:295` 构造）是**现役**分段落盘实现，**KEEP** |
| 计划勘误 | 原“`resolveMemoryStore` 现行接线条款转正”**不可执行**：`resolveMemoryStore` 全树 0 命中（非改名可解，是符号从未存在或已彻底消失）。R5 改写条款时须指名 `OpenTier`/`OpenAtomicStore`，不得沿用该名 |

### 14.7 表 5 · `compression-recovery-ticket` 退役与 `Stateful`/`StateRegistry` 迁锚（承载：R7 → R5）

| 项 | 事实 | 处置 |
|---|---|---|
| 代码面 | `RecoveryTicket\|WriteRecoveryTicket\|recovery.ticket` 在 `.go` **0 命中** | 无代码债 |
| 残留引用 | `openspec/specs/compression-recovery-ticket/spec.md:1/:4/:6`（能力本体）、`golden/runtime/architecture.json:6`（历史 ratchet 注记 `RecoveryTicket/LoadScan/LoadStats`）、`docs/wiki/platform/platform-subsystems.md:107`（退役陈述）、archive 历史全文 | R5 REMOVED／R6 文档面；**archive 不改** |
| **阻塞源（实证活）** | `golden/runtime/architecture.json:1429 "Stateful"` → `:1432 "spec": "compression-recovery-ticket"`；`:1434 "StateRegistry"` → `:1437` 同值 | **R5 5.1.3 的 superseded 动作在 R7 迁锚前 = blocked** |
| 迁锚目标（已验证可落） | `session-checkpoint-material`：`check_concept_budget.py:926-927` 的 `binding_anchor_problem` 只要求锚定 spec 文件存在且未被 superseded；`openspec/specs/session-checkpoint-material/spec.md:22`「`Stateful` 收编消费者读态且不新增挂点」＋“接入一个新的可恢复消费者 SHALL 只注册一个切片，SHALL NOT 要求新增挂点、端口或第二份真相”是该绑定的语义归属面 | R7 先改两处 `spec` 值 → 跑 `check_concept_budget.py` 绿 → R5 才 superseded。先退役再解释门红＝禁止 |
| 顺序约束 | R7 迁锚 ⇒ 概念门绿 ⇒ R5 退役。三者不可调序，且 R7 与 R5 触及同一 golden/spec 面 ⇒ **串行或合单** | — |

### 14.8 KEEP 集合（冻结，负锚 N3 的判定面）

| KEEP 符号 | 承载证据 | 类型 |
|---|---|---|
| `ErrChainPageLimit` | `projection.go:200`（`ScanFrom` 活入口）＋ `EventChain_example.go:19/:61` | 生产＋契约示例 |
| `FileSegmentStore` | `memory/ledger/segment_store.go:276`、`config.go:100/:295` | 生产 |
| `MigrateShardedRoot` | `memory/ledger/migrate.go:69` ＋ `scripts/migrate-kv/main.go:24`（产品调用者）；`product_surface_baseline.json:45` 的 note 明确记录“自该调用起它已有产品调用者，不再是 test-only 钩子” | 生产 |
| `OriginalStore.Get` | `scripts/fold-cold-root/main.go:743` | 生产 |
| `ReadSegmentScoped`（含 `RecallPort` 转发） | `tool/recall/get.go:153` ← `assembly/tools.go:931` | 生产 |
| `BenchmarkFoldDepthCost` | `examples/wechat-bot/smoke_bench_test.go:461`，被 `memory/replay/fold_test.go:123` 按函数名断言 | 名断言 |
| `TestFoldDepthMeasurementExists` | `memory/replay/fold_test.go:118` | 名断言 |
| `TestRebuildRefoldsIdempotent` | `tests/integration/host/age_fold_test.go:166`，被 `memory/replay/fold_test.go:135` 按函数名断言 | 名断言 |

**跨文件名断言耦合一并冻结**（design §四**未列**，R9b 必须接）：`memory/replay/fold_test.go:108-150` 的 `TestFoldDepthMeasurementExists` 用 `strings.Contains` 读三个**别的文件**的源码文本并要求它们仍存在：`age_fold_test.go` 的 `func TestRebuildRefoldsIdempotent(`（`:135`）、`smoke_bench_test.go` 的 `func BenchmarkFoldDepthCost(` ＋ `[]int{256, 32768}`（`:123`）、`compress_pipeline_test.go` 的 `func TestCompressProductRestoresAfterRestart(`。折叠成本决策引用的“独立测量”由这条字符串断言维系——R9b 删 `Fold` 时若顺手删该测试或改锚名，折页成本决策的证据面即静默失效。

### 14.9 blocked 项（不得宣称零残留）

| # | 阻塞项 | 具体阻断 | 解阻条件 |
|---|---|---|---|
| B1 | 5.1.3 `compression-recovery-ticket` superseded | `architecture.json:1432`/`:1437` 仍绑 `Stateful`/`StateRegistry`（14.7 实证） | R7 完成两处迁锚至 `session-checkpoint-material` 且 `check_concept_budget.py` 绿 |
| B2 | 5.1.4/5.1.5 Compactor 条款改写 | 4 个主 spec 仍以 SHALL 要求创建 `Compactor`；`resolveMemoryStore` 名在树中不存在，原改写方案不可执行（14.6） | R5 逐 Requirement 定 owner 并给出 `OpenTier`/`OpenAtomicStore` 的确切替换文本；改后 `spec_hygiene.json` 9 处预览重生 |
| B3 | `ScopedOriginalStore.GetScoped`（入口 #3）最终处置 | 生产直连 0、转发者 `RecallPort.GetScoped` 亦 0 调用者；但它是 `scopedRecall` 唯一装配口，删与留的代价都需 R9c 以“按 scope 精确召回仍可测”的断言面裁决 | R9c 就位后按 14.4 判据定稿 |
| B4 | `compatMaterialOf` 甲案（删除） | 需为 `api/plugin:Instance` 新增具名 S1 基线条目（抬基线）并改 15 示例面 ＋ N 测试面 | 编排者裁定是否抬基线；未裁定前冻结为乙案（保留） |
| B5 | `RebuildSkipNotEmpty` / `ErrChainKeyRequired` 断言迁移目标 | 两符号的现存断言（`partition_test.go:329-386`、`projection_replay_and_budget_test.go:361-362`、`notice_projection_test.go:146`）检验的语义在新入口上是否有对应，本轮只读未定 | R9b 施工时先给出新入口的等价断言，再随家族裁删 |

### 14.10 未查清面（如实列出，不作结论）

1. **`ScopedReader` 是否在 5.4 插件六端口处被间接需要**：本节只核到 `ScopedReader` 零生产构造；与 `api/plugin` 六端口的 Material 兼容读、R8 三耐久方法的交叉影响**未核**（design §十三已明令不得混算）。
2. **`ensureLoaded` 的调用计数与成本**：本轮只静态确认三条调用路径（`GetScoped`/`ReadSegmentScoped`/`BlobCount`），**未实测**每次召回是否触发整族前缀遍历、遍历规模与延迟。性能结论须由 06 的基准给出，本节不写。
3. **`examples/wechat-bot` 远端模块的锚迁移后 smoke 是否仍完整**：该模块以 `-tags smoke` 独立编译（`go test -C examples/wechat-bot -tags smoke .`），本轮未运行任何测试（只读盘点，零执行），故其 1 处重建锚在删除后的实际行为**未验**。
4. **`docs/api/*.md` 生成物的删除面**：`docs/api/github_com__SpellingDragon__tagent__memory__replay.md`、`__memory__ledger.md` 仍含拟删符号的 godoc，须由 `scripts/gates/gen_godoc.sh` 重生成；本轮未执行生成，也未核生成器对“符号消失”的处理（是否留空节）。
5. **wiki 三处与实况不符的叙述**（R6 已识别的派生债，本轮只读取证，未改）：`docs/wiki/memory/memory-architecture.md:202` 称 scope 在装配处由 `NewScopedReader` 绑定——**假**（生产绑 `RecallPort`，scope 由 `reference.ReadScope()` 每次传入）；`docs/wiki/tool/tool-architecture.md:92` 称 `recall_get` 走 `ledger.ScopedReader`——**假**（走 `RecallPort.ReadSegmentScoped`）；`docs/wiki/plugin/plugin-architecture.md:23` 记录 `compatMaterialOf`（与 14.5 冻结一致，仅需与新计数对齐）。

### 14.11 承载叶映射（下游唯一施工依据）

| 承载叶 | 用本节哪几张表 | 该叶的施工前置 |
|---|---|---|
| **R5** 主 spec 同步 | 14.5（Material 文案）、14.6（Compactor 逐 Requirement）、14.7（ticket 退役）、14.2（`RebuildSkipNotEmpty` 语义声明） | B1 解阻（ticket 迁锚）、B2 定 owner；与 R7 同面串行 |
| **R6** 文档/词汇 | 14.5、14.10-5（wiki 三行勘误）、14.6（`spec_hygiene` 预览） | R5 定稿后改叙述；`gen_godoc.sh` 重生成 |
| **R7** golden/门 | 14.5（乙案正文案）、14.6（stale 条目）、14.7（迁锚）、14.4 #5（`product_surface_baseline.json:18` stale） | 迁锚先于 R5 退役；门是否抬基线须具名上报 |
| **R9b** replay 迁锚与裁删 | 14.2 全表、14.3（12 文件与 0 冲突）、14.8（KEEP 与跨文件名断言） | 12 文件按现名改锚（不扩大）；B5 的等价断言先就位 |
| **R9c** 原文入口收口 | 14.4 七条全表（含第七条与次级间接入口）、14.9-B3 | 正式入口实测须走 `assembly/host_document.go:778` 链；`TestOriginalReadEntrypointsPreserveBytesAndScope` 落 `kv_original_store_test.go` |

**冻结效力**：本表与 14.8 的 KEEP 清单是 R9b/R9c 的**判定面**——下游若要删表中标 KEEP 的符号，或要删表外未列的符号，须先回到本节改表并给出新证据（负锚 N3 即此纪律的机器实现）。本节**不构成任何删除授权**：删除动作属 R9b/R9c，本轮零改动。

## 十五、10.R9c 原文读入口收口定稿（六入口＋第七条间接入口）

**性质**：§十四 是 R9a 的只读盘点；本节是 R9c 的**处置定稿**——逐入口给出现树复核、处置与依据，并把 `14.9-B3` 的待定状态结掉。本轮只改 `memory/ledger` 三个写面文件（`reference.go`、`kv_original_store.go`、`kv_original_store_test.go`），零删除、零公开契约改动、零 git 写。§十四 原文按审计记录保全，本节只作增量与勘误。

### 15.1 处置表（六入口＋第七条，现树复核于本轮复跑）

| # | 入口 | 现树复核（本轮） | 处置 | 依据 |
|---|---|---|---|---|
| 1 | `OriginalStore.Get`（`kv_original_store.go:354`） | 生产 1：`scripts/fold-cold-root/main.go:743`（操作员入口 `tier.Originals.Get`） | **KEEP** | 仓内产品路径非测试替身；入口由 `TestOriginalReadEntrypointsPreserveBytesAndScope` 的正锚固定 |
| 2 | `OriginalStore.ReadSegment`（`:390`） | 生产 0；`StorageConfig.Originals`（`config.go:98`）是公开接口字段，任何实现方都必须实现它 | **KEEP（本轮不删，见 15.3）** | 零**直连**调用不等于可删：它是公开端口的必需方法面，删它要同批改接口声明、契约示例、skeleton 断言与三处集成锚 |
| 3 | `ScopedOriginalStore.GetScoped`（`reference.go:244`） | 生产直连 0；**但它是 `*kvOriginalStore` 满足 `ScopedOriginalStore` 的必需方法** | **KEEP，B3 结案** | 见 15.2-①：`Run.RecallReads` 的运行时断言依赖该方法的存续，删它会把整条召回链静默解绑 |
| 4 | `ScopedOriginalStore.ReadSegmentScoped`（`reference.go:258`） | 生产 1 活链：`tool/recall/get.go:153` → `assembly/tools.go:924`（体内 `:931`）→ `tier.Originals` | **KEEP** | 本族唯一实活入口；正式入口锚见 15.4 |
| 5 | `ScopedReader.Get`（`reference.go:223`） | 生产 0 构造；`NewScopedReader` 8 处调用全在 `tests/integration/{history,capabilities}`，且在 `product_surface_baseline.json:18` 的 test-only 冻结集内 | **本轮保留，裁删转派（见 15.3）** | 删除面越出本叶写面，且与 R7 baseline／R6 文档同批 |
| 6 | `ScopedReader.ReadSegment`（`reference.go:236`） | 同 #5（同一 `ScopedReader` 类型） | **本轮保留，裁删转派（见 15.3）** | 同 #5 |
| **7** | `RecallPort.GetScoped`／`RecallPort.ReadSegmentScoped`（`assembly/tools.go:839`） | `ReadSegmentScoped` 1 活生产（上表 #4）；`GetScoped`（`:912`）现树**0 调用者** | **KEEP** | 见 15.2-② |
| 7bis | `Run.RecallReads()`（`assembly/tools.go:159`，`:168-169` 类型断言绑 `reads.Originals`） | 由 `RecallPort.Bind`（`:858`）在装配期调用 | **KEEP**（读面唯一绑定点） | 见 15.2-① |

### 15.2 两条关键发现（本轮新增，R9a 未覆盖）

**① `GetScoped` 的 KEEP 依据是接口满足面，不是"转发者活着"**

`OpenTier` 恒以 `*kvOriginalStore` 填 `Tier.Originals`（`config.go:291-300`），而 `Run.RecallReads` 用**运行时断言**把它绑进召回读面：

```go
if scoped, ok := r.tier.Originals.(ledger.ScopedOriginalStore); ok { reads.Originals = scoped }   // tools.go:168-170
```

删掉 `kvOriginalStore.GetScoped`，该断言就静默转 `false`：`reads.Originals` 留 `nil`，`RecallPort.ReadSegmentScoped`（`:928`）与 `recall_get`（`get.go:116`）全部退化为 `ErrRecallUnwired`——**编译照过、召回全死**。因此 `GetScoped` 不是"零可达的死端口"，而是活链得以绑定的结构前提。实测负锚见 15.4-M3（删除后**编译失败**于绑定处，无静默窗口）。

> 这条同时说明 R9a 在 `14.9-B3` 里问的是"删与留的代价"是对的，而"生产直连 0 ⇒ 倾向裁删"的推论不成立：**接口满足面是可删性判据的一部分**，与调用点计数并列。

**② 第七条入口的 `GetScoped` 是接口面必需，不是死码**

`RecallPort.GetScoped`（`tools.go:912`）现树 0 调用者，但 `var _ ledger.ScopedOriginalStore = (*RecallPort)(nil)`（`:849`）与 `reads.Originals = recallPort`（`host_document.go:778`）要求它存在：删掉即失去 `ScopedOriginalStore` 满足，`:778` 的绑定与 `get.go` 的 scoped 读面一起断。判 KEEP，理由与 15.2-① 同构。

### 15.3 本轮不执行的删除与转派理由（写面边界，不是"忘了"）

R9a 冻结 #2 为"裁删首选"、#5/#6 为"裁删"。本轮**未执行**，理由三条，均属边界而非推断：

1. **写面**：本叶授权写面只到 `memory/ledger` 三文件。删 #2 需同批改 `memory/ledger/reference.go` 的接口声明（在写面内）**加** `memory/ledger/skeleton_assert.go:76`、`examples/contract/memory_ledger/OriginalStore_example.go:38/:76`、`tests/integration/history/{original_reference_isolation,storage_capabilities}_test.go`；删 #5/#6 需同批改 `reference.go`（写面内）**加** `skeleton_assert.go:82/:84`、`tests/integration/history/{original_reference_isolation,storage_capabilities}_test.go`、`tests/integration/capabilities/search_real_store_test.go`（8 处 `NewScopedReader`）。
2. **归属**：删除使 `product_surface_baseline.json:18` 的 `NewScopedReader` 条目转 stale——该 baseline 是 **R7 的写面**；`docs/api/*.md` 的 godoc 随之失效，归 **R6**。在本叶删除会把两个他叶的 stale 条目与文档重生拉进同一 diff，违反"账目单点归属"。
3. **判据面**：`ScopedReader` 的 8 处调用正是"按 scope 的精确召回仍可测"的现行表达；B3 的裁决刚刚依赖它。先删承载再讲结论，等于把裁决的证据面一起删掉。

⇒ 处置：本轮记**现状保留**，并把"删 #2/#5/#6"作为**独立派发单**上报编排者，写面须包含上述 6 个文件＋(R7) baseline＋(R6) godoc 重生，且须先给出等价断言落点。

### 15.4 证据（本轮实测，可复跑）

测试落点：`memory/ledger/kv_original_store_test.go` 的 `TestOriginalReadEntrypointsPreserveBytesAndScope`（七个子测试：六入口读回原字节／跨 scope／缺失／过期／损坏-篡改字节／损坏-不可读引用记录／tier 满足 scoped 端口）。

| 锚 | 命令 | 本轮结果 |
|---|---|---|
| 正锚 | `go test ./memory/ledger -run '^TestOriginalReadEntrypointsPreserveBytesAndScope$' -count=10 -v` | **10/10 PASS**（7 子测试全绿，含 `-v` 计数） |
| 正式入口 | `go test ./tool/recall ./tests/integration/capabilities ./tests/integration/history -count=1` | 三包 **ok**（0.311s／5.050s／20.973s） |
| 契约示例 | `go test ./examples/contract/memory_ledger -count=1` | exit 0，但该目录 **无测试文件**——`ExampleOriginalStore` 无 `// Output:` 注释，故这是**编译锚而非行为锚**，不得当作示例已执行 |
| 包回归 | `go test ./memory/ledger -count=1` | ok |
| 全仓编译 | `go build ./...`；`go vet ./memory/ledger` | exit 0 |

负锚（变异在副本上施加，逐条复原并以 `shasum` 复核；M3 为编译级）：

| # | 变异 | 命中 |
|---|---|---|
| M1 | `scopedReference` 忽略 scope，返回任一条引用 | `a_scope_that_registered_no_reference_reads_as_missing` 红：`GetScoped("0a01") Outcome:1`（Found）而期望 `2`（Missing）——scope 泄漏被抓 |
| M2 | `ReadSegment` 行界 off-by-one（`end = startLine + numLines`） | `every_entry_point_answers_the_committed_bytes` 红：拼回字节重复了第二行与第三行——行界漂移被抓 |
| M3 | 删 `kvOriginalStore.GetScoped`＋`var _ ScopedOriginalStore` 满足断言 | **编译失败**：`*kvOriginalStore does not implement ScopedOriginalStore (missing method GetScoped)`，命中绑定处 `kv_original_store_test.go:678`——`GetScoped` 的 KEEP 是编译期约束，无静默窗口 |
| M4 | `scopedSegment` 跳过 `blob.verify()` | `altered_bytes_read_as_an_integrity_mismatch` 红：`ReadSegmentScoped` 返回篡改后的字节而非 `ErrIntegrityMismatch` |

复原核验：三个文件在变异前后的 `shasum` 逐一致（`reference.go 3967619b…`、`kv_original_store.go 424cfad3…`、`kv_original_store_test.go aeb7f927…`），复原后 `go build ./...` 与 `gofmt -l memory/ledger` 均清净。

### 15.5 边界重申

- **成本归属不变**：`ensureLoaded` 的全量成本只经 scoped 面与 `BlobCount` 抵达，与 #1/#2 的存活无关；性能改造归 **06**，本节不写任何性能结论（沿用 §14.4 成本注与 §十三裁定）。
- **不与别族混算**：本节只涉原文读入口；5.4 的六个插件分派端口属 R7，`R8` 的三耐久方法属持久面，二者与本族不交叉（`ScopedReader` 与它们的交叉影响仍是 §14.10-1 的未查清面）。
- **未验证面**：`examples/contract/memory_ledger` 只编译不执行（见 15.4）；`docs/api` godoc 与 wiki 三行假叙述仍未改（归 R6）。

## 十六、R314 四裁定落地留痕（10.R5 第二接续）

**性质**：本节是 R5 契约面的实施留痕，四裁定见一级 `ORCHESTRATION.md` R314。本轮零产品 Go、零 git 写、零 archive；写面＝`openspec/specs/event-segment-store/spec.md`、`scripts/gates/doc_budget.json`、本域 tasks/design。

### 16.1 裁定①：doc_budget 只降锁步

`python3 scripts/gates/check_doc_budget.py --tighten` 实测 `baseline.lines 22149 -> 21979`（files 231 不变），`RESULT: ok`；复跑 `check_doc_budget.py` 得 `measured=baseline=21979`、`slack.lines=0`、exit 0。构成：前批主 spec 退位条款删除与 memory-curation/event-segment-store 现行权威改写净减 165 行（`git diff --numstat -- openspec/specs/` 合计 +132/−297），本轮 event-segment-store 租约条款改写再减 5 行。**只降不升**：未跑 `--update-baseline`、未抬任何指标；减少量按门提示锁进基线，manifest `note` 已具名记该次下调。裁定文中的「22149→21984」是裁定时刻读数；裁定②改写落在其后，故实际落点为 21979，下降方向与裁定一致。

### 16.2 裁定②：死租约要求改写为单属主 pin（不立新设计）

`openspec/specs/event-segment-store/spec.md:334` 原「未确认恢复材料的有限保留租约」整条改写为「未确认恢复材料的单属主 pin」；`:378` 的 spill 分句同改。语义全部取自活载体，逐条对应如下（口径：现树实测，`grep` 见 16.6）：

| 改后条款语义 | 活载体 | 实测判据 |
|---|---|---|
| 登记表以义务身份为键，一义务一条 pin | `assembly/host.go:214` `deliveryPins map[string]event.EventID` | 值类型是事实键而非计数——**无引用计数** |
| 未结即登记、结清即解除、换用新结算事实时旧 pin 同批解除 | `assembly/host_delivery.go:682-697` `noteDeliveryState` | `previous != key` 时 `run.unpinFact(previous)`；结清分支 `delete` 后不 pin |
| pin 集合挂在运行热窗上，唯一写者是投递账户 | `assembly/run.go:2265` `pinFact` / `:2279` `unpinFact` | 非测试调用方只有 `host_delivery.go` 与 `host_delivery_checkpoint.go` |
| 裁剪 MUST NOT 物理销毁被 pin 的事实 | `assembly/facts.go:229`、`:308` → `assembly/run.go:2317` `keepPinnedLocked` | cut 中 pin 命中项被剔除，`delete(r.facts, key)` 只作用于 cut 剩余项 |
| 启动时由既有 checkpoint 重建 | `assembly/host_delivery_checkpoint.go:208`（`wire.Pins`）→ `:350-352`（恢复后逐个 `pinFact`） | 每义务身份一条 `Pins` 条目 |
| 保护只覆盖裁剪这一条销毁路径 | `grep -rn 'pinnedFact(\|keepPinnedLocked' assembly/ --include='*.go'`（排除 `_test`） | 命中只在 `run.go` 定义与 `facts.go` 两处裁剪——TTL／容量／墓碑路径不读 pin |

删去的死语义：租约武装宽限 `armGrace` 与 `memory/lifecycle.go` 引用、引用计数递减与归零解除、共享内存守护集、遗忘扫描首轮破坏性动作等待租约、无损搬迁（压实复制到高层段）、显式删除受保护 key 的 protected 错误、单 agent 关闭的解绑语义。**这些对象在树中零定义**（`armGrace` 全树 `*.go` 0 命中、`Protect` 系全树 `*.go` 0 命中、`memory/lifecycle.go` 不在树），且与 `storage-lifecycle-completion:17`「MUST NOT 引入与这两条载体并行的保留租约账本、守卫计数或生命周期执行器」正面冲突，故按现行单 pin 改写而非保留过渡设计。

场景随之由 6 条改为 5 条：删「未确认材料越过普通 TTL」（TTL 豁免无载体）、「显式删除撞上受保护材料」（protected 错误无载体）、「共享存储下的单 agent 关闭」（共享多属主无载体）；「重启时登记先于淘汰」改写为「重启时 pin 先于首轮裁剪重建」；保留 fail-closed 与非耐久档 capability 两条。

**载体指路不进 spec**：`check_spec_hygiene` 的 R4 禁止 Requirement 正文出现 `.go` 文件名与函数调用形（本轮首稿因 `assembly/host.go`／`Sync()` 三处立红，`violations` 255→本改后 252）。故主 spec 只写不变量，载体坐标落在本表。裁定②要求「保留 MUST NOT 物理销毁／fail-closed／非耐久档 capability 缺失」三条均在位（16.6 逐条命中）。

### 16.3 裁定③：sync 两负例退役（对象不存在，不建假锚）

原拟新增 `scripts/gates/test_check_openspec_sync.py` 的两个负例（`test_conflicting_requirement_text_is_refused`／`test_missing_rename_source_is_refused`）**退役**：被测实现不存在——`ls scripts/gates/` 无 `check_openspec_sync.py`，真实 delta 合并由外部 `openspec` CLI 1.2.0 的 `archive` 承担，`scripts/gates/check-openspec.sh` 只跑 `validate --all --strict`。无被测实现即无锚，写「冲突／缺源」两负例只能对着自造实现打，属假锚，故不建。此为**对象不存在的计划勘误**，不是验收面缩水：该两负例自始未执行、无产物、未进入任何门。

### 16.4 裁定④：ticket 退役子项转 R7

`compression-recovery-ticket` 退役仍等 R7 把 `golden/runtime/architecture.json:1432`／`:1437` 的 `Stateful`／`StateRegistry` 迁锚到 `session-checkpoint-material` 并过概念门（14.7／14.9-B1 顺序约束）。本单零改动，**转 R7**；R5 的其余交付不因该子项等待。

### 16.5 delta 约定核定

- `openspec archive --help`（1.2.0）只提供 `-y`／`--skip-specs`／`--no-validate`，无 delta 相关选项；合并对象是 change 根 `specs/`。
- `openspec list` 实测只列一级 change（9 项），`changes/10-contract-sync-dead-code` 等子域目录**不在 CLI 清单**——子域非跟踪，已核实。
- 一级 change 的 `specs/` 只有 `orchestration/spec.md`；子域 `10/specs/` 只有 `code-documentation/spec.md`，**无 event-segment-store delta**。
- 结论：主 spec 直编＋本节留痕即完成同步；无 delta 可补，`archive` 也不会对本主 spec 施加 delta 应用，故**不补** REMOVED/MODIFIED 假 delta（补一份无人消费的 delta 只会造第二真相）。

### 16.6 本轮验证（原始 exit，命令可复跑）

| 命令 | 结果 |
|---|---|
| `openspec validate runtime-recovery-ideal --strict` | exit 0（`Change 'runtime-recovery-ideal' is valid`） |
| `bash scripts/gates/check-openspec.sh` | exit 0（158 passed / 2 failed＝`event-driven-consumption`、`post-training-data-loop` 两个 in-flight change，非门控；`OK: all main specs pass validate --strict`） |
| `python3 scripts/gates/check_doc_budget.py` | exit 0（`measured=baseline=21979`、`slack.lines=0`） |
| `python3 scripts/gates/check_spec_hygiene.py` | exit 0（`violations=252 baseline=286 stale=34 errors=0`） |
| `python3 scripts/gates/check_doc_links.py` | exit 0（42 文档 / 130 链接 / 0 error） |
| `python3 scripts/gates/check_concept_budget.py` | exit 0（`RESULT: ok`） |
| 死措辞零命中：`for w in armGrace 引用计数 守护集 无损搬迁 保留租约 memory/lifecycle.go "protected 错误" 遗忘扫描 宽限; do grep -c "$w" openspec/specs/event-segment-store/spec.md; done` | 逐词皆 0（命令逐词输出，均 0） |
| 在位命中：`grep -n "单属主 pin\|MUST NOT 物理销毁\|fail-closed\|capability 缺失" openspec/specs/event-segment-store/spec.md` | 命中 Requirement 标题、正文两条、fail-closed 场景与包装层条款 |
| 被移除 Requirement 标题负锚：`git diff -U0 -- openspec/specs/` 取 `^-### Requirement:` 得 20 条，逐条 `grep -F "### Requirement: <t>" openspec/specs/*/spec.md` | 17 条 0 命中；2 条为**同名改写后在位**（`存储失败不可冒充正常缺失`、`保留保护与回收协调`，diff 加号侧各 1 命中）；1 条 `冥想总结以高亮卡片行沉淀` 从 `memory-curation` 移除、存活于 `meditation-self-state-digest`（不同能力义项，非漏删） |

### 16.7 边界与未改项（如实留白）

- **未改**：`event-lifecycle:61` 仍以「`event-segment-store`『未确认恢复材料的有限保留租约』」与「`storage-lifecycle-completion`『保留保护与回收协调』」双名交叉引用，改后标题不再对应；且该要求整条围绕 `LifecycleManager`／`DefaultLifecycleConfig` 已退役对象（R313 记 event-lifecycle 冷引用 5 处）。**该 spec 不在本单写面**（design §二 未点名，本轮写面限 event-segment-store 与已改主 spec 自洽），故只报不改，交主 agent 裁定归属。
- **未改**：`golden/runtime/spec_hygiene.json`（stale 34 条不阻断，`--write-baseline` 是 baseline 面）；`architecture.json` 两处 ticket 锚（归 R7）；archive 与一级编排。
- **口径限制**：本节为静态读码与门禁结果，未跑产品测试；pin 语义的行为证据（裁剪跳过、重启重建）来自代码路径与既有测试引用，本轮未另跑 `go test`。

## 十七、R315 留白落地：event-lifecycle 整体 superseded（10.R5b）

**性质**：R315 把 `event-lifecycle` 主 spec 的留白派为独立叶 10.R5b。本节是该叶的实施留痕；一轮取证定方向、一次改写、四个门复跑，**不立新设计**。零产品 Go、零 git 写、零 archive、零一级编排改动；写面＝`openspec/specs/event-lifecycle/spec.md`、`scripts/gates/doc_budget.json`、本域 tasks/design。

### 17.1 方向判定：整能力退役（非部分活）

| 判据 | 实测 | 含义 |
|---|---|---|
| `LifecycleManager`／`NewLifecycle`／`DefaultLifecycleConfig`／`LifecycleConfig`／`checkTTL`／`evictOldest`（`*.go` 全树） | **0 命中**（生产与测试皆无） | 扫描器与策略配置在全树无定义、无引用 |
| 墓碑族 `IsTombstone`／`MarkTombstone`／`TombstoneSet`／`PartitionState`／`StartLifecycle`／`StartCompactor` | **0 命中** | 墓碑标记与容量计数同批死透 |
| `api/config` 的 `lifecycle`／`ttl`／`max_events` | 仅 `settle*` 词形误命中，**无配置字段** | 声明式注入面无承载 |
| 任意 `*.yaml`／`*.yml`／`*.md` 的 `global_ttl_days`／`max_events_per_partition`／`check_interval`（排除 archive 与 specs） | **0 命中** | 「YAML 注入还有消费者」不成立，属整能力退役 |
| `memory/ledger/lifecycle_config.go`／`memory/partition.go` 的 `eventCount`／`evict` | 文件不在树／无命中 | 配置与计数载体都已删 |
| `memory/ledger/journal.go` 的 tombstone（`:175/:186/:860`） | 命中 | **日志键删除记录**义项，与事件 TTL 墓碑非同一机制，不计活证据 |

结论：`event-lifecycle` 没有「部分活条款」，全章整体 superseded；不做「留骨架改措辞」的中间态。

### 17.2 改写形态（照 production-wiring-fix／compaction-scheduler 先例）

116 行 → 26 行。保留 `# event-lifecycle Specification`／`## Purpose`／`## Requirements` 三段骨架：

1. **Purpose 三段 banner**：①superseded 声明，列机制、符号、配置面三重零命中，并记「承载实现文件已不在树」；②「现行判据是只拒销毁」段，把活的实质承诺指到 `storage-lifecycle-completion`「保留保护与回收协调」与 `event-segment-store`「未确认恢复材料的单属主 pin」；③勘误段，正名原 :61 双名交叉引用——「有限保留租约」标题已随 R314 裁定②改写为「单属主 pin」。
2. **Requirements 只余一条**「历史条款已退役」＋2 场景（查现行条款／已退役扫描与淘汰不得复活），把原五条要求标题作为**历史清单**列出，不留任何现行 SHALL/MUST。

**两条硬约束的碰撞及处置**：`check_concept_budget` 的「标 superseded 的主 spec 在 `## Requirements` 里仍含 SHALL/MUST 即红」与「退役要求仍要写清承诺去向」正面相遇——首稿把两条 `MUST NOT` 写进退役正文、一条 `SHALL NOT` 写进场景，实测 `superseded_specs_with_requirements: 1` 立红。处置：现行关键词一律撤到 Purpose banner（不在 `requirements_block` 扫描面内），`## Requirements` 内改用「不得／不存在」的中文禁令，与 `production-wiring-fix`／`compaction-scheduler` 两先例逐字同构。

### 17.3 活承诺的承接（不重复立）

| 原条款的实质承诺 | 现行唯一载体 | 本轮动作 |
|---|---|---|
| 淘汰/墓碑 MUST NOT 物理销毁受保护原文 | `storage-lifecycle-completion`「保留保护与回收协调」（`MUST NOT 物理销毁已提交事实`、`只拒销毁`） | 原文未动，仅在新 banner 指路 |
| 未结义务的原文不被窗口裁掉 | `event-segment-store`「未确认恢复材料的单属主 pin」 | 原文未动，交叉引用由旧租约标题正名为单 pin |
| 原文窗口外须具名拒绝 | `storage-lifecycle-completion` 场景「原文窗口外具名拒绝」 | 原文未动，仅指路 |

**不立第二份**：不在 `event-lifecycle` 重述这三条，避免与 `storage-lifecycle-completion:17`「MUST NOT 立第二真相」冲突。

### 17.4 本轮验证（原始 exit，命令可复跑）

| 命令 | 结果 |
|---|---|
| `openspec validate runtime-recovery-ideal --strict` | exit 0（`Change 'runtime-recovery-ideal' is valid`） |
| `bash scripts/gates/check-openspec.sh` | exit 0（`OK: all main specs pass validate --strict`；2 failed 为 `event-driven-consumption`／`post-training-data-loop` 两个非门控 in-flight change，与基线同） |
| `python3 scripts/gates/check_spec_hygiene.py` | exit 0（`violations=250 baseline=286 stale=36 errors=0`；基线 252→250，退役的两条 grandfathered 违例行消失；stale 34→36 非错） |
| `python3 scripts/gates/check_doc_links.py` | exit 0（42 文档 / 130 链接 / 0 error） |
| `python3 scripts/gates/check_concept_budget.py` | exit 0（`superseded_specs_with_requirements: 0`、`findings.domain-surface: 0`、`RESULT: ok`；首稿该计数为 1，改后归零） |
| `python3 scripts/gates/check_doc_budget.py` | 首测 exit 1（`实测 21889 < 基线 21979（-90）`，双向锁步）→ `--tighten` exit 0（`baseline.lines lowered: 21979 -> 21889`）→ 复跑 exit 0 |
| 负锚：死符号 grep 计数 `for w in LifecycleManager checkTTL DefaultLifecycleConfig LifecycleConfig evictOldest MarkTombstone TombstoneSet global_ttl_days type_ttl max_events_per_partition; do grep -c "$w" openspec/specs/event-lifecycle/spec.md; done` | 逐词皆 **1**，且每条命中都在 banner 的「零命中」历史叙述或退役标题清单内，**无一条作现行命令** |
| 正锚在位：`grep -n "单属主 pin\|保留保护与回收协调\|MUST NOT 被读作\|superseded" openspec/specs/event-lifecycle/spec.md` | 命中 banner 三段与退役要求正文 |

### 17.5 级联发现（未改，超本叶写面）

- **变异锚漂移（13 域变异账）**：`scripts/gates/check_event_integrity_mutations.py:1144` 的 `MUT-DOD-P5-legacy-clause` 用 `old="for age calculation."` 打在本 spec 原「Time-based TTL expiration」要求正文，期望 `check_spec_hygiene` 报 `R4 go-file in requirement 'Time-based TTL expiration'`。该要求退役后**锚点与期望标题同时消失**，变异将 `not_applied`。`golden/runtime/dod_predicates.json` 的 `mutation_verdicts` 现为 `[]`（无已记 kill 判决），故未击穿任何当前绿门；正解是把该变异重指到另一条仍在位的主 spec 要求。
- **相邻 spec 的死判据引用**：`openspec/specs/event-segment-store/spec.md:338` 仍写「TTL、容量与墓碑各自的判据不因 pin 改变」。三分句的判据已随本叶整体退役，宜改为「不是现行销毁判据」；但该行是 R314 裁定② 刚定稿的要求正文（§16.2 逐字留痕），再改需新裁定，故只报不改。
- **未动**：`golden/runtime/spec_hygiene.json`（stale 36 条不阻断，`--write-baseline` 属 baseline 面且无裁定）；archive 与一级编排。
- **口径限制**：本节为静态读码与门禁结果，未跑产品测试（本叶为纯规格面，无产品 Go 改动）。

## 十八、R316 裁定①：单 pin 段死判据改写（10.R5c）

**性质**：§17.5 第二条把 `event-segment-store:338` 的 TTL／容量／墓碑三分句列为「再改需新裁定」，R316 裁定①据此派为独立叶 10.R5c。本节是该叶的实施留痕：一次取证定墓碑义项、一处改行、六个门复跑，**不立新设计**。零产品 Go、零 git 写、零 archive、零一级编排改动；写面＝`openspec/specs/event-segment-store/spec.md`（仅该段）、本域 tasks/design。

### 18.1 墓碑义项判定：三项同时零命中 ⇒ 该分句判死删除

| 取证对象 | 命令 | 实测 | 含义 |
|---|---|---|---|
| 存储墓碑族符号 | `grep -rn --include='*.go' -E "IsTombstone|MarkTombstone|TombstoneSet" . \| wc -l` | **0** | 标记／查询接口全树无定义，生产与测试皆无 |
| 墓碑键前缀写入 | `grep -rn --include='*.go' -F "tomb:" . \| wc -l` | **0** | `{pid}:tomb:{key}` 无任何写入方或读方 |
| 该分句的原载体 | `grep -rn --include='*.go' -E "ReplayEvent\|ErrEventForgotten\|EventForgotten" . \| wc -l` | **0** | 「重放入口查墓碑即拒写」的机制无定义 |
| 同名异义项 | `grep -rn --include='*.go' -w tombstone memory/ledger/journal.go \| wc -l` | **4**（`:175`／`:186`／`:860`） | **日志帧的键删除记录**：回答「该键是否仍被持有」，不是重放拒绝机制 |

**判定**：「合法墓碑 MUST 被拒绝重放」的原载体是 `ReplayEvent` 入口查墓碑后写前拒 `ErrEventForgotten`（唯一历史证据：`openspec/changes/archive/2026-09-30-complete-resident-reliability-protocol/evidence.md:347`）。该机制与存储墓碑集同批退役，故该分句**判死删除**，不留过渡措辞。`memory/ledger/journal.go` 的同名 tombstone 是**另一义项**（键删除记录），既非事件级墓碑也不拒绝重放，因此**不塞进该空缺**——那样只会造第二真相。判定方向明确（三项零命中＋义项可分），不触发「墓碑义项不明⇒停报」。

### 18.2 改写落点与活载体对应（仅 `:338` 这一段，等长行替换、总行数不变 386）

| 改后条款语义 | 活载体 | 实测判据 |
|---|---|---|
| 保护只覆盖热窗裁剪这一条销毁路径 | `grep -rn "keepPinnedLocked" assembly/` | 定义 `assembly/run.go:2317`，调用只有 `assembly/facts.go:229`／`:308` 两处裁剪 |
| 待删前缀由热窗边界给出（热窗投影仍在呈现的最旧原文之前的事实） | `assembly/facts.go:260` `liveWindowCutLocked` | 取会话各带窗投影的最旧非合成键位置为上界；无正窗口时落 `:323` `fallbackCutLocked` |
| pin 不改变该判据，只把命中项从待删集合摘出 | `assembly/run.go:2317` `keepPinnedLocked` | 对 cut 逐项判断，命中即 `continue` 跳过——**摘出**而非**改判据** |
| 义务结清后按原有时间戳恢复参与老化 | 同表 `unpinFact`（§16.2） | 解除后条目重新落到下一轮 cut |
| 恢复材料清除后的任意手工历史重放不属自动恢复保证，MUST NOT 新增永久去重库 | 同要求前段「MUST NOT 引入第二张持久保留表或全历史去重集合」 | 承诺同向，未新增第三条来源 |

删去的死语义：`TTL、容量与墓碑各自的判据不因 pin 改变`（三分句判据已随 10.R5b 退役）、`合法墓碑 MUST 被拒绝重放`（三零命中）、`恢复材料与墓碑皆已清除`中的墓碑分句（同族）。**保留不动**：`MUST NOT 物理销毁`、持久档 fail-closed、非耐久档 capability 缺失、`提交屏障与 pin 各自独立`——四条 R314 裁定②定稿正文，本叶不重开（实测 `grep -c "MUST NOT 物理销毁\|fail-closed\|capability 缺失\|单属主 pin"` 仍 8 处）。载体坐标仍不进 spec（`check_spec_hygiene` R4 禁 `.go` 文件名与函数调用形），故落上表。

### 18.3 本轮验证（原始 exit，命令可复跑）

| 命令 | 结果 |
|---|---|
| `openspec validate runtime-recovery-ideal --strict` | exit 0（`Change 'runtime-recovery-ideal' is valid`） |
| `bash scripts/gates/check-openspec.sh` | exit 0（158 passed / 2 failed＝`event-driven-consumption`、`post-training-data-loop` 两个非门控 in-flight change；`OK: all main specs pass validate --strict`） |
| `python3 scripts/gates/check_spec_hygiene.py` | exit 0（`specs=151 requirements=765 violations=250 baseline=286 stale=36 errors=0`，与改前同数 ⇒ 未新增 R4 违例） |
| `python3 scripts/gates/check_doc_links.py` | exit 0（42 文档 / 130 链接 / 56 锚 / 0 error） |
| `python3 scripts/gates/check_concept_budget.py` | exit 0（`RESULT: ok`） |
| `python3 scripts/gates/check_doc_budget.py` | exit 0（`measured=baseline=21889`、`slack.lines=0`）。该门**计行不计字符**：本改为同段等长行替换，`wc -l` 前后皆 386 ⇒ 无需 `--tighten`，`doc_budget.json` 本轮零改动，「净删才降」的授权条件未触发 |
| 负锚：`sed -n '338p' openspec/specs/event-segment-store/spec.md \| grep -c "TTL\|容量淘汰\|墓碑"` | **0**（grep exit 1），四字面改为逐字 grep 亦全 0 |
| 正锚在位：`grep -c "MUST NOT 物理销毁\|fail-closed\|capability 缺失\|单属主 pin"` | **8** 处 |
| 改动面：`diff -u <改前快照> openspec/specs/event-segment-store/spec.md` | 仅 `:338` 一行；其余 385 行零改动 |

**工具限制**：本树 `.git/HEAD` 为空且存在 `index.lock`，`git rev-parse --show-toplevel` 报 `not a git repository`（`GIT_INDEX_FILE=/tmp/r2d-index` 亦同），故改动面证据用改前快照 `diff -u` 与 `wc -l` 代替 `git diff`；全程零 git 写。

### 18.4 级联发现（未改，超本叶写面「仅该段」）

- **同一主 spec 另有三处死判据**：`:77`／`:80`／`:84-85`「去重存活计数＋墓碑容量淘汰」要求与场景（计数与容量淘汰载体已随 10.R5b 退役）、`:185`「排序、时间过滤、TTL 年龄均只依据 Timestamp」的 TTL 年龄分句、`:375`「容量提示…不作删除依据」。三者均在本叶写面之外，宜由后续叶统一处置。
- **`:324` 的 `ReplayEvent` 要求标题本身零定义**（同 §18.1 第三行证据）。整条改写需要先判「sealed 窗口降级是否仍有活载体」，属新裁定而非同步实况，故只报不改；`tombstone-partition-isolation` 主 spec 同属该族死能力，一并留待裁定。
- **未动**：`golden/runtime/spec_hygiene.json`（stale 36 条不阻断，`--write-baseline` 属 baseline 面）；`scripts/gates/doc_budget.json`（行数未变）；archive 与一级编排。
- **口径限制**：本节为静态读码与门禁结果，本叶纯规格面、无产品 Go 改动，未跑 `go test`。

## 十九、R317 余项：event-segment-store 余死条款逐条留删（10.R5d）

**性质**：§18.4 把该主 spec 的四处死条款列为「本叶写面之外，宜由后续叶统一处置」，R317 据此连同 `:324` 一起派为独立叶 10.R5d。本节是该叶的实施留痕：六项逐条取证定活否、一处改写三处删除、六个门复跑，**不立新设计**。零产品 Go、零 git 写、零 archive、零一级编排改动；写面＝`openspec/specs/event-segment-store/spec.md`、`scripts/gates/doc_budget.json`（净删只降）、本域 tasks/design。

### 19.1 逐条活否取证（现树实测；零命中即判死）

| 项（改前行号） | 原措辞要旨 | 取证命令 | 实测 | 判定 |
|---|---|---|---|---|
| `:77` 计数正文 | 重建「去重、排除墓碑」存活计数；物理清理墓碑与压实搬迁不掉计数；容量淘汰递减 | `grep -rn --include='*.go' -E "IsTombstone\|MarkTombstone\|TombstoneSet" . \| wc -l`；`grep -rn --include='*.go' -E "CompactL1ToL2\|CompactL2ToL3\|deleteSegments\|mergeEvents" . \| wc -l`；`grep -rn --include='*.go' -F "淘汰" . \| wc -l`；`grep -rni "scanner" --include='*.go' memory/ \| wc -l` | 依次 **0／0／0／0** | 死半删；活半留（见 19.2） |
| `:77` 计数活载体 | 计数如何来、何处增 | `grep -n "func (s \*FileSegmentStore) \(Load\|rederive\|EventCount\|publishedEntriesOf\|publishEventLocked\|seatOfCommitLocked\)" memory/ledger/segment_store.go` | `:330`／`:352`／`:1043`／`:412`／`:740`／`:695` 全在 | 活：链自述一次读出／索引枚举重建／提交后加一 |
| `:80` 场景 | 压实清理墓碑与搬迁同键时 live count 只计一次 | 同上三族 | **0** | 死，整场景删 |
| `:84-85` 场景 | 含 600 存活＋40 墓碑的存量，容量淘汰包括存量 | 墓碑族 0／`淘汰` 0；重启计数恢复有锚 | 死措辞 0＋四锚 PASS | 死措辞删，重启语义保留 |
| `:185` | 排序、时间过滤、**TTL 年龄**均只依据 `Timestamp` | `grep -rn --include='*.go' -E "checkTTL\|LifecycleManager\|DefaultLifecycleConfig" . \| wc -l`；`sed -n '286,290p' memory/query_events.go` | 前者 **0**（10.R5b 已退役）；后者按 `rec.Timestamp` 过滤 | TTL 措辞删；`Timestamp` 轴判活保留 |
| `:324` 整条 | `ReplayEvent` 窗口重入与 `StoreEvent` 同做 sealed 降级 | `grep -rn --include='*.go' -F "ReplayEvent" . \| wc -l`；`grep -rn --include='*.go' -E "seqCounter\|SeqCounter" . \| wc -l`；`grep -rn --include='*.go' -iE "sealed\|封口" memory/ \| wc -l`；`grep -rn --include='*.go' -E "\bLayer\b" memory/ \| wc -l` | 依次 **0／0／0／0** | 整条死：要求＋场景删，原位留退役注 |
| `:375` | 重放判「已修复／已提交」不递增，**容量提示**至多建议性观察 | `grep -rn --include='*.go' -E "ConsolidationHint\|checkCapacity\|consolidation_hint" . \| grep -v '_test.go' \| wc -l`；`grep -rn --include='*.go' -F "淘汰" . \| wc -l` | 依次 **0／0** | 容量提示分句删；「计数只在真实提交递增」判活 |

**同名异义项排除**：`grep -rn --include='*.go' -iE 'compactor|compaction' memory/ \| grep -v '_test.go'` 有 85 条命中，但逐条读后均为**投影折叠**（`memory/replay/fold.go` 的 compaction fact／`memory/replay/replay_budget.go:423` `Compaction()`／`ErrCompactionCorrupt`），不是事件段 L1→L2 搬迁，故**不承接**「压实搬迁不掉计数」的空缺（同 §18.1 对 journal tombstone 的处理：不塞第二真相）。`features/evolution/meditation/judgment_test.go` 的 `consolidation_hint` 是判断过滤夹具里的 stamp 字面，无生产生产者，故不承接容量提示。

### 19.2 改写落点与活载体对应（386→375 行，净 −11）

| 改后条款语义 | 活载体 | 实测判据 |
|---|---|---|
| 计数由链自述一次读出，不自述时由索引枚举重建 | `memory/ledger/segment_store.go:330` `Load`（`storeSeatKey = "store:seat"`，`:509`）、`:352` `rederive`、`:412` `publishedEntriesOf`（`filed` 集合过滤未到达窗口元数据的条目，`:442`） | `TestStoreSeatIsReadWithOnePointReadAtOpen`、`TestACorruptStoreSeatRederivesFromTheChain`、`TestASeatBelowItsCountRederivesTheRank` PASS |
| 每次成功提交只加一 | `:695` `seatOfCommitLocked`（`next := storeSeat{Count: s.count + 1, …}`，`:696`）、`:740` `publishEventLocked`（仅在 `:616` 屏障成功后于 `:619` 调用） | `TestSegmentLazyLoadPreservesEventCount` PASS（重开计数＝提交数） |
| 重复提交与写失败不增计数 | `:613` `plan.duplicate` 早返、`:838-839` `FactAlreadyPresent` 早返；屏障失败返回 `ErrSegmentBarrier` 且不调发布 | `TestSegmentDedupViaIndex` PASS（重复提交后计数不变＋异字节 `ErrContentConflict`） |
| 重启后计数＝已提交事件数，未提交／未发布条目不计 | 同第一行（座位或索引二选一） | 同上四锚 |
| 排序与时间过滤只依据 `FullEvent.Timestamp` | `memory/query_events.go:286-290`（`StartTime`／`EndTime` 与 `rec.Timestamp` 比较） | 静态读码；排序键 `(timestamp, EventKey)` 见同 spec「双实现查询语义一致」 |
| 判「已存在」的重复提交不递增 | `CommitFact` `:838-839`、`StoreEvent` `:613` | 同第三行 |

删去的死语义：`排除墓碑`／`物理清理已墓碑事件与压实搬迁 SHALL NOT 再递减`／`容量淘汰包括存量`／`扫描器启动前`／`去重`（结构性由「每条索引记录一条事件身份」承担，不写成重建步骤）／`TTL 年龄`／整条 `ReplayEvent` sealed 降级／`抬高淘汰压力，容量提示因此至多是建议性观察，不作删除依据`。载体坐标不进 spec（`check_spec_hygiene` R4 禁 `.go` 文件名与函数调用形），故落上表。

### 19.3 本轮验证（原始 exit，命令可复跑）

| 命令 | 结果 |
|---|---|
| `openspec validate runtime-recovery-ideal --strict` | exit 0（`Change 'runtime-recovery-ideal' is valid`） |
| `bash scripts/gates/check-openspec.sh` | exit 0（158 passed / 2 failed＝`event-driven-consumption`、`post-training-data-loop` 两个非门控 in-flight change；`OK: all main specs pass validate --strict`） |
| `python3 scripts/gates/check_spec_hygiene.py` | exit 0（`specs=151 requirements=764 violations=250 baseline=286 stale=36 errors=0`；requirements 765→764＝删掉 `ReplayEvent` 一条，violations／baseline 不变 ⇒ 未新增 R4 违例） |
| `python3 scripts/gates/check_doc_links.py` | exit 0（42 文档 / 130 链接 / 56 锚 / 0 error） |
| `python3 scripts/gates/check_concept_budget.py` | exit 0（`domain_surface.superseded_specs_with_requirements: 0`、`RESULT: ok`） |
| `python3 scripts/gates/check_doc_budget.py` | 首测 exit 1（`实测 21878 < 基线 21889（-11）`）→ `--tighten` exit 0（`baseline.lines lowered: 21889 -> 21878`，files 231 不变）→ 复跑 exit 0（`RESULT: ok`） |
| 负锚：`grep -cE "压实搬迁\|容量淘汰\|存储墓碑\|TTL\|ReplayEvent\|sealed\|live count\|扫描器\|淘汰" openspec/specs/event-segment-store/spec.md` | **4**：`:3` banner 退役叙述、`:321` 本叶退役注（两者不含现行 SHALL/MUST），`:25`／`:26` 为 19.4 级联项（`:26` 仍写 `sealed: true`／`layer: 1` 的现行 SHALL，本叶名单外，只报不改） |
| 正锚在位：`grep -c "单属主 pin\|MUST NOT 物理销毁\|fail-closed\|capability 缺失"` | **8** 处（与 10.R5c 后同数，未重开） |
| 实证补跑：`GOFLAGS=-buildvcs=false go test ./memory/ledger -run '^(TestSegmentLazyLoadPreservesEventCount\|TestSegmentDedupViaIndex\|TestACorruptStoreSeatRederivesFromTheChain\|TestASeatBelowItsCountRederivesTheRank)$' -count=1 -v` 与 `-run '^TestStoreSeatIsReadWithOnePointReadAtOpen$' -count=1 -v` | 前者 exit 0（`ok … memory/ledger 5.072s`），后者 exit 0（`ok … 25.523s`）；五锚全 PASS（本叶写的是计数契约，故用被测实现的反例面核一遍，不是门禁要求） |

**注**：本叶非纯静态——三条改后条款（计数来源、提交递增、重复不增）直接对应上表四锚，故补跑 `go test`；`go build`／`vet` 未跑（零产品 Go 改动）。

### 19.4 级联发现（未改，超本叶名单，交主 agent 裁归属）

- **`:18-26` 两条 sealed 场景同族未改**：`Active segment auto-seals on hour boundary`／`Seal produces segment metadata` 要求 meta 写 `sealed: true`、`layer: 1`。本叶已判 `sealed`／`Layer` 在 `memory/` 零定义（19.1 第 6 行证据），且现树窗口取事件自身毫秒（`assembly/facts.go:417` `window := evt.Timestamp.UnixMilli()`）而非小时桶 ⇒ 两场景同样无对象。整段改写要先裁定「窗口粒度」这条现行设计，非同步实况，故只报不改。
- **`:182` 句尾剪枝族待重新取证**：`剪枝与早停仅读封口段的事件时间边界（见“段边界可信性”）` 与 sealed 同族；且 `storedWindowMeta.MinTime/MaxTime` 在 `memory/` **只写不读**（零消费方），`FileSegmentStore` 无 `QueryEvents` 方法，召回排序／过滤跑在内存投影上（`memory/query_events.go:270`）⇒ 「窗口剪枝／早停／段边界可信性」三条要求的活载体待重新取证，属独立裁定（建议另立叶），本叶不动。
- **未动**：`golden/runtime/spec_hygiene.json`（stale 36 条不阻断，`--write-baseline` 属 baseline 面）、`scripts/gates/doc_budget.json` 的 `note`（只写 `--tighten` 产生的 −11 下调）、archive、一级编排、产品 Go。
- **无关红项（照录不归因本叶）**：`check_dod_coverage.py` exit 1（P0–P8 皆「no run records … as killed」，本树无变异 run 记录，属 13 域变异账）；`check_vocabulary_alignment.py` exit 1（4 条 `carrier` 违例全在 `features/evolution/governance/goal.go` 与 `docs/api` 生成物）。两者均不读本叶改动的 spec 文本，非本叶引入。

## 二十、R320 10.R7 收口：N10 叶计数形态、⑤ 对象已消失、⑥ 两假绿形态已修（2026-11-18）

### 20.1 N10 叶计数形态：现树已统一为裸编号，计数解析器补读粗体形态

**现树实测（命令见 20.4 第 1 行）**：13 域 `tasks.md` 共 147 个 checkbox，其中 `NN.R…` 形态的现行叶 **78** 个，把编号包在粗体里的 **0** 个——粗体一律落在编号之后的标题文字上（`- [x] 06.R1 **成本与引擎能力设计门…**`）。叶行原文 N10 记录的「05/07/12 用 `- [ ] **05.R1 …**`、其余十域裸格式」在现树已不存在，题面「统一为裸格式」这一支已由各叶改写完成。

**形态统一不等于形态通道关闭**：`scripts/gates/runtime_checklib.py` 的 `CHECKBOX_RE` 是树内唯一的叶 checkbox 解析器，消费者为 `check_runtime_plan.py` 两处（`collect_checkbox_ids` 旧十域路径、`check_event_checkboxes` event 路径）。改写前它只认裸形态，一支写成粗体的叶在两处的结局不是同一种：有 `task-graph` 时具名报「叶没有 checkbox」，**没有** graph 的手工计数里则只是少一个数、退出码照常为零——即 N10 指的「退出码零但零命中」同族。

**落点**：`CHECKBOX_RE` 增可选 `\*{0,2}` 前缀读两形；配套自测 `EventProfileTest.test_a_bold_leaf_checkbox_is_counted_too`（`scripts/gates/test_check_runtime_plan.py`）把一枚未勾叶（`03.11`）与一枚已勾叶（`03.1`）的编号改写成粗体，断言 `checkboxes`／`leaves_checked`／`leaves_unchecked` 三项与改前逐项相等、`unchecked_leaf_ids`／`checked_leaf_ids` 仍含该叶、且输出不出现 `has no checkbox`——标记与编号必须读自同一行，粗体行不能只被算成一条空记录。

**未扩编号族（如实留白）**：`TASK_ID_RE`／`CHECKBOX_RE` 仍只认 `NN.M` 与 `NN.P0.M`。本变更的叶编号是 `NN.R…`，且本变更**没有** `task-graph.json` ⇒ `scripts/gates/lint.sh` 的 plan 段对它 skip（R306 登记的「门孤儿」仍在），该门对本变更**没有判定对象**。这不属 N10 的形态问题：若日后为本变更建图，须先裁定编号族并把 `TASK_ID_RE` 与 `CHECKBOX_RE` 同批扩，不能只改一侧。

### 20.2 ⑤ `cold_archive_test.go` 旧切面文案：对象已随 08.R3 整删（勘误，零改动）

- `memory/ledger/cold_archive_test.go`（519 行）与 `memory/ledger/cold_archive_gate_test.go`（154 行）在 `0f84b2d1` 整删：`git show 0f84b2d1 --stat` 的 `grep cold` 得 `memory/ledger/cold_archive_gate_test.go | 154 -`、`memory/ledger/cold_archive_test.go | 519 -`；`git log --diff-filter=D -- "*cold_archive_test.go"` 唯一命中该提交。
- 现树与 HEAD 均无该文件（`ls` 无、`git ls-tree -r HEAD | grep cold_archive` 无），`cold_archive`／`ColdArchive`／`coldArchive` 在 `assembly runtime memory tests` 均 **0** 命中。
- 故两级 `:537/:559` 的旧切面文案随文件消失，**无需改字**；本项按勘误登记（对象不存在，不计作「已修」）。

### 20.3 ⑥ 13 报的两新假绿形态：已由 13.R2d／13.R2e 修（只登记指针，未重做）

| 假绿形态 | 现树载体 | 具名锚（20.4 复跑） |
|---|---|---|
| 控制树 race 豁免掩盖业务失败 | `check_event_integrity_mutations.py:1751 _race_waived`／`:1778 _failures_beside_the_race`／`:1842-1844` 分派：纯 race 判 `green_race_waived`，race 旁的普通断言失败判 `test_failed` | `test_control_asserting_beside_a_waived_race_is_red`、`test_control_with_an_asserting_case_beside_a_raced_case_is_red`、正对照 `test_control_failing_only_on_a_waived_race_stays_green` |
| 消费者不核 seed/window 实际 argv | `check_runtime_evidence.py:2327 check_event_matrix_args`（「能不能带」问生产者 `EVENT_CALIBRATION_FLAGS`→`event_calibration_bearer`，包清单由 `command_packages(argv)` 从 argv 反读）／`:2460` 逐 invocation 调用；定义侧在 `scripts/scenarios/run_runtime_scenarios.py` | `test_a_bearer_command_that_drops_the_matrix_pair_is_rejected`、`test_a_matrix_pair_broadcast_to_packages_that_cannot_parse_it_is_rejected`、`test_a_matrix_pair_the_argv_does_not_state_is_rejected`、`test_a_run_declaring_no_pair_cannot_carry_matrix_args`、`test_off_matrix_seed_window_is_rejected` |

两叶在 `13-verified-completion/tasks.md:28`／`:39` 已 `[x]`，台账 R296／R300 有编排者亲验记录 ⇒ 本叶不重做、不改 13 域任何文件。

### 20.4 本轮验证（原始 exit，命令可复跑）

| 命令 | 结果 |
|---|---|
| `grep -rEh '^\s*-\s*\[[ xX]\] [0-9]{2}\.R[0-9]' openspec/changes/runtime-recovery-ideal/changes/*/tasks.md \| wc -l` | **78**（现行叶全为裸编号） |
| `grep -rEh '^\s*-\s*\[[ xX]\] \*\*[0-9]{2}\.R[0-9]' openspec/changes/runtime-recovery-ideal/changes/*/tasks.md \| wc -l` | **0**（无粗体编号叶） |
| `grep -rEh '^\s*-\s*\[[ xX]\]' …changes/*/tasks.md \| wc -l` | **147**（含 5.x/6.x 等旧实施行） |
| `python3 scripts/gates/test_check_runtime_plan.py` | exit 0（`Ran 45 tests … OK`；含新增粗体叶用例） |
| 负锚（注入只认裸形态的 `CHECKBOX_RE` 到 `/tmp/n10-gates` 副本后跑同一条新用例） | exit 1：`AssertionError: 1 != 0` + `leaf 03.1 has no checkbox in any domain tasks.md`、`leaf 03.11 has no checkbox in any domain tasks.md` ⇒ 粗体一被漏读即成红 |
| 真实输入对照：`check_runtime_plan.py --change-dir openspec/changes/archive/2026-10-18-runtime-rebuild --json`（两形版 vs 裸形态版） | 两版 `counts.checkboxes=123`、`errors` 均 101 条且逐条同 ⇒ 改动对现树真实计划面零行为变化（该归档图自带 101 条与改前相同的错误：change 名与目录名不符、08 域已删测试的空转项等，非本叶引入） |
| `python3 -m unittest discover -s scripts/gates -p "test_check_runtime_*.py"` | exit 1：`Ran 316`，唯一红 `test_check_runtime_symptoms.test_registry_matches_the_module`（外来未跟踪面，见 20.5） |
| `python3 scripts/gates/test_check_event_integrity_mutations.py`／`-k waived_race -v` | exit 0（`Ran 60 … OK`）／exit 0（2 tests OK） |
| `python3 scripts/gates/test_check_runtime_evidence.py`／`-k matrix -v` | exit 0（`Ran 126 … OK`）／exit 0（6 tests OK） |
| `python3 scripts/gates/test_check_dod_coverage.py` | exit 0（`54/54 cases passed`） |
| `go run ./scripts/comment_policy -v . examples/wechat-bot examples/cli-host` | exit 0（`0 finding(s); 0 beyond baseline`） |
| `go run ./scripts/comment_policy -no-baseline -v assembly api runtime memory features tool internal cli` | exit 0（`0 finding(s)`） |
| `check_window_bounds.py`／`check_schema_allowlist.py`／`check_concept_budget.py`／`check_vocabulary_alignment.py` | 全 exit 0（`ok`／`errors=0`／`RESULT: ok`／`violations=0 carrier=0`） |
| `GOFLAGS=-buildvcs=false go test ./runtime/state/ -count=1` | exit 0（`ok …/runtime/state 0.647s`） |
| `python3 scripts/gates/check_dod_coverage.py`（无变异 run 产物） | exit 1：`9 gap(s)`，P0–P8 皆「no run records … as killed」（属 13 域变异账） |
| `bash scripts/lint.sh` | **exit 1**，红在 checker selftests 段的 `test_check_runtime_symptoms`（外来未跟踪，见 20.5）；自此段起未执行，DoD 段未跑到 ⇒ 本节不宣称 lint 全绿 |

### 20.5 未做与边界（如实留白）

- **`comment_policy/main.go` 的措辞缺陷未修**：两 scope 的输出都仍带 `1 git-ignored Go file(s) are outside the ratchet … examples/wechat-bot/knowledge_base`——它把**目录**条目称作 Go 文件（`13-verified-completion/tasks.md:11` 曾登记「归 10.R7 顺带修」）。该文件不在本叶写面（写面＝本域 tasks／design ＋ 计数门脚本与自测），本轮未改。
- **①golden 冷层条目／②N14 `NewDurableCommit`／③N15「整族枚举」不重做**：三项由编排者 R320 亲验完成，本叶仅抽样复核——`NewDurableCommit` 在 `runtime assembly memory api tool features internal cli golden docs openspec/specs` **0** 命中、`整族枚举` 同上 **0** 命中、`golden/runtime/architecture.json` 的 `compression-recovery-ticket` **0** 命中，与 R320 一致。
- **叶 ④ 的分布描述已失效**（现树 0 粗体编号，见 20.1）；不改验收面文字，按「让计数门认两形＋漏数即红自测」这一支落地。
- **lint 全绿不宣称**：红点对象 `scripts/gates/check_event_semantics_symptoms.py`／`scripts/gates/test_check_runtime_symptoms.py` 在 `git status` 中为未跟踪（`??`），属禁触外来面；其 `SYMPTOM_PARENTS` 少列 `TestLocalSmokeSymptomBootDeployed`（该测试在 `examples/wechat-bot/smoke_test.go:4690`）。R304／R308 已登记同一不一致，本轮零改动。

## 二十一、10.R9b 接续收口：迁移实况、KEEP 复核与红项归因（2026-11-19）

前置：裁删与迁锚已由前任落地并经编排者亲验（build 0、`memory/replay` 与 host 两锚 -count=1 绿、`RebuildProjectionFromWAL|RebuildOptions|RebuildReport` 于 `*.go` 0 命中）。本节只装**验证与记录**，不重开裁删/迁锚设计；唯一源码改动见 21.6。

### 21.1 正测全量（命令原文，落盘原始 exit）

| 命令 | 结果 |
|---|---|
| `go test ./memory/... ./tests/integration/history ./tests/integration/host ./tests/integration/state ./tests/integration/execution ./tests/integration/acceptance -count=1` | **exit 1**：唯一红 = `tests/integration/host` 的 `TestExecFaceWiredFromDocument`（`host_wiring_test.go:342`，`the release request carries no run result for tmux/…`）；其余 9 包 ok（`memory/ledger 100.803s`／`history 20.478s`／`state 1.837s`／`execution 54.064s`／`acceptance 6.132s`／`memory/replay 0.916s` 等） |

**红项归因（非本叶）**：①该文件 `git status` 为 CLEAN，`rg 'replay\.|Fold|Rebuild'` 零命中——不触本叶任何产物；②同包单独复跑 `go test ./tests/integration/host -count=1` → **exit 0**（`ok 203.444s`）；③单测复跑 `-run '^TestExecFaceWiredFromDocument$' -count=3` → **exit 0**（`ok 2.204s`，3/3 全绿）。⇒ 并发负载下的 tmux 呈现竞态，非本叶引入、非稳定红。

### 21.2 正式入口（三条逐条原始 exit）

| 命令 | 结果 |
|---|---|
| `go test ./tests/integration/host -run '^(TestCompressProductRestoresAfterRestart|TestRebuildRefoldsIdempotent)$' -count=10 -v` | **exit 0**（`ok 4.693s`；`TestCompressProductRestoresAfterRestart` 10 PASS、`TestRebuildRefoldsIdempotent` 10 PASS） |
| `go test ./tests/integration/history -run '^TestEventIntegrityProjection$' -count=10 -race -v` | **exit 0**（`ok 5.566s`，100 条 `--- PASS` 行含子测试） |
| `go test -C examples/wechat-bot -tags smoke . -count=1` | **exit 1**（`FAIL wechat-bot 142.860s`，8 项红；归因见 21.3）；单跑 `-run '^TestLocalSmoke$' -count=1` → **exit 1** 复现（`20.47s`） |
| `go build ./...`／`go vet ./...` | 均 **exit 0** |
| `go test ./examples/contract/... -count=1` | **exit 0**；23 包中 22 包 `[no test files]`、仅 `tool_recall` 有测试（`ok 0.281s`）⇒ 契约示例面为**编译锚**，不声称示例已执行 |
| `go run ./scripts/comment_policy -v . examples/wechat-bot examples/cli-host` | 修 21.6 后 **exit 0**（`0 finding(s); 0 beyond baseline`）；修前 exit 1（1 beyond baseline，见 21.6） |
| `go run ./scripts/comment_policy -no-baseline -v assembly api runtime memory features tool internal cli` | **exit 0**（`0 finding(s)`） |
| `python3 scripts/gates/check_concept_budget.py`（`GIT_DIR=/tmp/ri-gitdir`） | **exit 0**（`RESULT: ok`）；含新警告 `baseline entry S2 struct-fields:memory/replay/projection.go:RebuildReport matches no current violation (stale: remove it)`——与 14.2 预测一致，条目清理属 **R7 写面**，本轮不动基线 |
| `python3 scripts/gates/check_dead_producers.py` | **exit 0**（`3453 production function(s) in 32 package(s) has a consumer`；裁删前 3505 ⇒ −52），仅作并列取证，不作生产可达性证据 |

### 21.3 wechat-bot smoke 八项红逐条归因（不得以「模块整体红」一句了账）

| 红项 | 原始报文关键行 | 归属证据 |
|---|---|---|
| `TestLocalSmokeWave2Data/call_subagent…` | `receipt = "[evt_3-…\|action_command] ok", want the minimal ok admission confirmation` | 回执票据形态属 **no-prefix 变更**；`smoke_harness_test.go` 的 +123 行同为该叶（结算通知票据/裸正文双形态） |
| `TestLocalSmokeWave2Errors/refused_replacement…` | `host document declares no provider "smoke-errors-missing"` | 宿主文档/reload 面他叶 |
| `TestLocalSmokeWave2Recall` | `segment commit barrier failed: commit through a view that holds no write rights` | `memory/ledger/guard*.go`／`lifecycle*.go`／`retention.go` 已由他叶整删 |
| `TestLocalSmokeSymptomBootDeployed`／`NoSendTool` | `data directory holds the retired shard layout, open a new directory: .wechat-config/data/…` | 用户真实数据目录 + 分片退位叶；编排文档明令不读/不改该目录 ⇒ **环境不可达** |
| `TestEventIntegrityLongRun`／`…LongRunCalibration` | `the 03.11 long-run matrix must run under -race; rerun with -race` | 测试自述要求 `-race`，而任务行给出的命令不带 `-race` ⇒ **命令与测试要求不符** |
| `TestLocalSmoke` | `session smoke-chat-a folded no history summary carrying "EARLY-MARKER-smoke-chat-a" (products=1)` | 确定性复现；但本叶产物 diff **只删 0 生产调用者符号＋注释**（`projection.go` 全部新增行为注释、`replay_budget.go` 仅删 `seedConsumed` 与注释），活的折叠/恢复源 `memory/replay/age_{plan,segments}.go` 全仓未改，该断言读的是 `rig.host.Run().ReplayEntries` 的实时投影 ⇒ 归他叶 `assembly/run.go`（+248/−）的恢复重写或模块既有红，非本叶 |

**结论**：该正式入口在当前环境（用户数据目录不可触、命令缺 `-race`）与他叶红项下**不可达绿**；本叶不宣称该门已过，按停线处理（21.7）。

### 21.4 负锚（隔离副本 `/tmp/r9b-mut`，工作树零改动，逐条复原并 shasum 复核）

| 变异 | 施加后 | 复原后 |
|---|---|---|
| 控制组（未变异） | `memory/replay` 三锚 exit 0（`ok 0.445s`）、`assembly` 两锚 exit 0（`ok 0.477s`）、host `TestRebuildRefoldsIdempotent` exit 0（`ok 0.373s`） | — |
| **M1 产物幂等**：`ageKey` 掺入进程内 nonce（内容寻址身份不再只由内容决定） | host `TestRebuildRefoldsIdempotent` **exit 1**（`age_fold_test.go:217: the committed turns minted no fold product in the projection`）；`memory/replay` `TestPlanAndApplyAnswerTheFold` **exit 1**（`age_plan_test.go:61` 计划产物的 `-seg-` 键与直接折叠的键不同） | `age_segments.go` shasum 复原为 `fb53bf20…`，两处复绿（exit 0） |
| **M2 链路隔离**：父链遍历的 chain 比较改用待求 chain 自身的 session（丢 session 隔离） | `assembly` `TestParentLookupKeepsSessionIsolation` **exit 1**（`read_test.go:786: the body walk over an index carrying no session answered "bare-b1"/<nil>, want bare-a1`） | `read.go` shasum 复原为 `238cc403…`，复绿（exit 0） |
| **M2b 仅删分区守卫**（`part != partition`） | `assembly` 两锚 **exit 0**、`tests/integration/history` **exit 0**（`ok 15.844s`） | 同 M2 复原 |

**M2b 结论（如实登记，不计作已验）**：分区 ID 由 `memory.PartitionIDFromName` 从会话名稳定推导，同一会话链语义上只落一个分区，故该守卫在良构数据下行为等价，分区轴无法独立证伪；「跨分区隔离」的机器锚由 M2 的会话轴承担，不写成「分区与 session 双轴均已验」。

### 21.5 KEEP 复核（21.5 冻结面逐项在位）

- `ErrChainPageLimit`：定义 `memory/replay/projection.go:78`、编译断言 `skeleton_assert.go:97`、契约示例 `examples/contract/memory_replay/EventChain_example.go:19/:61`、分页页读 `tests/integration/state/notice_projection_test.go:115`。
- `FileSegmentStore`：`memory/ledger/segment_store.go` 系列 + `assembly/read.go:76/:82`。
- `MigrateShardedRoot`：`memory/ledger/migrate.go:69` + 产品调用者 `scripts/migrate-kv/main.go:24`。
- `BenchmarkFoldDepthCost`：`examples/wechat-bot/smoke_bench_test.go:461`（`//go:build smoke` + `[]int{256, 32768}` 两窗口断言均未被删）。
- `TestFoldDepthMeasurementExists`（`memory/replay/fold_test.go:25`）：三条跨文件名字断言（`BenchmarkFoldDepthCost(`／`TestCompressProductRestoresAfterRestart(`／`TestRebuildRefoldsIdempotent(`）逐条在位。
- `TestRebuildRefoldsIdempotent`（`tests/integration/host/age_fold_test.go:166`）：判据未删——「提交触发的折叠产物键」与「同链重放的折叠计划产物键」逐项相等（`sort` 后逐键比较）；M1 已证其可失败。

### 21.6 本轮唯一源码改动（裁删遗漏，仍在 R9b 冻结写面内）

`fold.go` 删除后 `memory/replay/fold_test.go` 不再镜像任何生产文件，`comment_policy` 报 **新引入** `responsibility-fragmentation: memory/replay/fold_test.go:1`（`baseline 0 -> 1 (+1)`，exit 1）。按该规则自述的三条出口取第 (3) 条**锚收敛**：把文件级契约锚从 `docs/FACT-VOCABULARY.md#families`（已随 `fold.go` 消失的族登记面）改为该文件唯一测试自述的 `docs/FACT-VOCABULARY.md#reduction-criteria`。改后 scope1 **exit 0 / 0 finding(s)**；该文件在 R9b 冻结的十二测试文件内，未扩大写面，零行为改动（纯注释行）。

### 21.7 未勾与停线理由

正测全量与他叶/环境红分开取证后可放行，但 **正式入口第 3 条（wechat-bot smoke）确定性红且当前环境不可达绿**（用户数据目录禁触 + 命令缺 `-race` + 五项他叶红），故本叶**不勾选**，按停线回报编排者裁定：或接受该门转由 examples 模块拥有者统一复验，或在授权写面内补 `-race`/独立数据目录后重跑。

### 21.8 非代码残留（交 R5/R6/R7，本叶未改）

- `openspec/specs/event-sourced-projection/spec.md:49/:156/:166` 仍以 SHALL 要求已删的 `RebuildProjectionFromWAL`（**R5 主 spec 同步**）。
- `docs/api/github_com__SpellingDragon__tagent__memory__replay.md` 仍含 7 处该符号 godoc（**R6 `gen_godoc.sh` 重生成**）。
- `scripts/gates/check_concept_budget.py:183` 的 `memory/replay/projection.go:RebuildReport` 基线转 stale（**R7 清条目并记原因**）。
⇒ 本叶不宣称「全仓零残留」，代码面 `*.go` 为 0 命中、上述三面按承载叶移交。

## 二十二、10.R6 接续收口：升级说明核真、godoc 重生与 doc_budget 归因（2026-11-19）

本节只记账，不改既有裁定。写面＝`docs/api` 生成物＋本域 tasks/design；**零产品 Go、零 git 写**；`docs/OPERATION.md` 与 `docs/wiki`、主 spec 一个字未动（前任已落定，本轮职责是核真伪而不是再改一遍）。

### 22.1 升级说明五项：俱在，逐条对代码锚

| 交付项 | 落点 | 代码锚（本轮实测） |
|---|---|---|
| `file` 档 `ErrBatchNotAtomic` 后果 | `docs/OPERATION.md` §F.3「整批零落地、一个键都不留」 | `memory/ledger/atomic_store.go:52/:98`；`rustviking_kv.go:152`；`config.go:89` `AtomicBatch` 同限度 |
| `recover-kv` 运行期具名失败 | §F.4「运行中的实例持有该根时…`recover-kv -restore` 以此名失败（干跑不取写权）」 | `scripts/recover-kv/main.go:111`（注释）`:116`（`WriteRights: restore`） |
| 既有 `cold/` 根先折回 | §F 标题＋§F.1 命令，§B 末句硬前置指向 `#storage-fold-prerequisite` | `scripts/fold-cold-root`（正锚三测 `-count=10` exit 0） |
| 磁盘增长显式声明、压实另立 | §F.5「不承诺有界、不承诺压实」 | 与 `memory-architecture.md:253` 同口径 |

⇒ 四项＋指向句齐全，**无缺可补**；净增一行即触 doc budget 棘轮（slack 0），故一字未加。

### 22.2 godoc 重生：一真红、一真故障、一处必须回避的空页

- 首测 `bash scripts/gates/gen_godoc.sh --check` **exit 1**，唯一差异 `memory__replay.md` ⇒ 前任那版生成物不反映现树。
- 根因**不是文档陈旧**：`memory/replay/fold_test.go` `stat=2063` 字节，而 `open().read()`、`cp`、`Read` 工具三者均得 **0 字节**（本机文件内容不可读）；`go doc -all` 因此报 `expected 'package', found 'EOF'`，生成器按空 body 落 `no documentation`。
- 处置：**不落空页**（745→1 行等于把整页 API 文档洗白）。改用只读镜像 `/tmp/r6view`（复制 `*.go`＋`go.mod/go.sum`，剔除那一个不可读测试文件），按生成器同款过滤（`/(契约|规格):/d`、docs 路径行清空、连续空行折叠、`$()` 尾换行语义）复跑并回写 **614 行**。
- 复验：镜像内逐包 `go doc -all` 生成 vs 现树 `docs/api` 比对 **43/43 包 0 diff**（正锚：内容＝源码）。现树 `--check` 仍红且只红该页，红因是源文件读故障；健康环境重跑即与该页相等。
- 顺带满足 §21.8 移交项：该页 `Rebuild` 符号 **0 命中**（原 7 处已随 fold 家族删除消失）。

### 22.3 doc_budget：本叶净 0，不 tighten；本地红 100% 属外来未跟踪

- tracked 面复算（checker 自身 `count_lines` ＋ 同一前缀排除，`git ls-files --cached`）＝**231 篇 / 21876 行**，与基线逐数相等 ⇒ 无净删、无需 `--tighten`，不上调。
- 裸跑红：**+12 篇 / +10561 行**。逐项溯源＝外来未跟踪 `examples/wechat-bot/skills/interview/agent-interview-helper/`（12 篇 md）；`231+12=243`、`21876+10561=32437` 与实测逐数相等，**无第三来源**。该目录在本变更内禁触，故不删、不排除规则化（`examples/wechat-bot/.gitignore:88` 的 `!skills/interview/**` 白名单优先级高于任何外部 exclude，改它即触外来面）。

### 22.4 词汇负锚与排除表（存储义冷引用在 docs/ 零命中）

正则：`冷档|冷归档|冷层|cold archive|cold tier|cold_archive|coldArchive|<root>/cold|cold/kv`；域：tracked `docs/` 125 篇。命中 4 处，**全部为「旧路径／工具输入」义**：`OPERATION.md:161/:246`、`memory-architecture.md:50/:163`（均「旧构建写在 `<root>/cold/` 下…由操作方显式跑 fold-cold-root」，无一处称冷层为现行机制）、`docs/api/…fold-cold-root.md:6`（该工具的 `-source` 输入路径）。

排除表（不同义项，不得过删）：`docs/GLOSSARY.md:14` 岗位退位本义（任务明令保留）、`docs/GLOSSARY.md:40` 压缩请求「消费后单次退位」、`docs/archive/wiki/PRE-REWRITE-MANIFEST.md:66` 冻结历史档；`generation retirement`／`headless-retired`／`Retention.Retire` 类命中属代际与交付终态义。`docs/.dev/**` 为 gitignore 本地物料，不入文档面。

### 22.5 本轮验证（原始 exit）与未达面

| 门 | exit | 读数 |
|---|---|---|
| `check_doc_links.py` | **0** | 42 文档／130 链接／56 锚／0 error |
| `check_spec_hygiene.py` | **0** | `violations=250 baseline=250 stale=0 errors=0` |
| `check_concept_budget.py` | **0** | `RESULT: ok` |
| `check_vocabulary_alignment.py` | **0** | `violations=0 carrier=0 retired-word=0` |
| `gen_godoc.sh --check` | **1** | 唯一差异 `memory__replay.md`；红因＝`memory/replay/fold_test.go` 内容不可读（见 22.2），镜像复验 43/43 0 diff |
| `check_doc_budget.py` | **1** | 红因＝外来未跟踪 12 篇（见 22.3）；tracked 面＝基线 |
| `bash scripts/lint.sh` | **1** | 首门 gofmt：`memory/replay/skeleton_assert.go` 两行未对齐（HEAD 版 `git show … | gofmt -l` 零输出 ⇒ 批内引入） |

**未达（不宣称文档段 0）**：lint.sh 在 `set -euo pipefail` 下首门即退，文档段不可达 ⇒ 以逐门直跑替代。两条他叶写面残留须单独派单：①`memory/replay/skeleton_assert.go` gofmt；②`memory/replay/fold_test.go` 可读性（读故障同时会让真树 `go doc`／`go vet ./...` 失败）。二者均不在本叶授权写面（产品 Go），本叶不越界。

## 二十三、10.R1 对账终表（事实修正版，取代 §九；零产品 Go、零 git 写、零 archive）

> §九 写于本轮取证之前，四类读数与今日登记表不符：六行「变异未登记」、P1 的锚名、P4 的 `ci_gate`／`measure`／阈值、P5/P6「无锚」。本节按**登记表与 CI 步骤命令的现树读数**重写，逐行附可复跑命令；未达项照实留未达，不以删要求、改判据或放宽阈值换门绿（本叶负锚①）。

### 23.1 口径（先立口径，再落读数）

- **锚侧真相**＝`golden/runtime/dod_predicates.json`（8 条谓词，`ci_gate` 全为 `test`）＋ `scripts/gates/check_event_integrity_mutations.py`（`MUT-DOD-P0..P8` 共九条，全 `enabled`，每条 `Replacement.old` 在现树**恰命中一处**）。
- **执行侧真相**＝CI 作业**步骤命令是否覆盖该锚选择器**，不是作业名存在。门的现状限制：`scripts/gates/check_dod_coverage.py:882` 只判 `entry["ci_gate"] not in jobs`（作业名∈jobs），**门绿 ≠ 选择器被执行**；本表按步骤命令逐条人工裁定并留凭据——这正是本叶负锚②的判据面。
- **判杀真相**＝`mutation_verdicts: []`（空）⇒ 九条变异**全部未证杀**，故 `check_dod_coverage.py` `exit 1`（9 gap：P0–P8 各一条「registered and unproven」）。该缺口属 13 域变异账，本表只作缺口输入，不要求本清单先绿。
- **承载叶未唯一者如实记**（P2），不硬塞一个叶凑一一对应。

复跑（全部只读）：

```sh
python3 -c "import json;[print(p['id'],p['ci_gate'],p['anchor']) for p in json.load(open('golden/runtime/dod_predicates.json'))['predicates']]"
sed -n '61,110p' .github/workflows/ci.yml                       # test 作业步骤命令
grep -n "test_check_spec_hygiene\|check_spec_hygiene\|test_check_dead_producers\|check_dead_producers\|test_check_dod_coverage" scripts/gates/lint.sh
for t in TestBuiltinCatalogMountsEveryFeature TestFactCommitAnswersNewExistingConflictUncertain TestSeedFactReusesTheReducer TestDurableLoadReadsTheTaskSlice TestSymptomAnchorBootCostFlatAcrossGenerations TestSymptomAnchorMidbandProcessRecalledByTicket TestHistorySummaryObeysThePlanBudget; do grep -rn --include='*.go' "func $t(" .; done
python3 - <<'PY'   # 九条变异 patch 锚文是否仍唯一命中所指文件
import importlib.util,sys
spec=importlib.util.spec_from_file_location("muts","scripts/gates/check_event_integrity_mutations.py")
m=importlib.util.module_from_spec(spec); sys.modules["muts"]=m; spec.loader.exec_module(m)
for d in m.MUTATIONS:
    if not d.id.startswith('MUT-DOD-'): continue
    r=d.replacement
    print(d.id, getattr(r,'file','(gate-anchor)'), open(r.file,encoding='utf-8').read().count(r.old) if r.file else '-', d.enabled)
PY
```

### 23.2 双向对账表（子要求 ↔ 承载叶 ↔ 锚全名 ↔ 执行该选择器的作业 ↔ 当前状态）

| 判据 | design D1 子要求（要点） | 承载叶 | 锚（全名 @ 现址） | `ci_gate` | 实际执行该选择器的作业·步骤 | 变异（登记／证杀） | 当前状态 |
|---|---|---|---|---|---|---|---|
| P0 | 特性经三挂点／插件注册表动态挂载；装配根不认特性名；`plugins:` 有生产消费 | 02-plugin-dynamic-mount/tasks.md:15（2.1）、:37；01-three-hooks-golden/tasks.md:8（01.R1a 守护） | `TestBuiltinCatalogMountsEveryFeature` @ `assembly/plugin_catalog_test.go:58`（must_assert `every_builtin_feature_mounts_through_the_catalog`） | `test` | `test#Test (short)` = `go test ./... -short -count=1`（ci.yml:63-64，根模块覆盖 `./assembly`）；该文件无 `testing.Short()`／`t.Skip`（grep 空） | `MUT-DOD-P0-concrete-mount`（`assembly/plugin_catalog.go`，patch 命中 1）／**未证杀** | 交付与锚在；判据未证（gap 之一） |
| P1 | 链唯一真相；一身份一不可变事实，SHALL NOT 删改已提交事实 | 11-atomic-fact-commit/tasks.md:50、:70（11.R2） | `TestFactCommitAnswersNewExistingConflictUncertain` @ `memory/ledger/segment_store_test.go:1431`（must_assert `identity_overwrite_refused_by_name`） | `test` | 同上覆盖 `./memory/ledger`；无 short-skip | `MUT-DOD-P1-identity-overwrite`（`memory/ledger/segment_store.go`，1）／未证杀 | 交付与锚在；判据未证 |
| P2 | 全部恢复经同一 reducer（seedFact），无全史读旁路 | **无单一承载叶**：05（任务恢复路径的 g(x)）＋06（年轻链兜底＝同一 g(x) 的退化输入，非旁路）＋07（governance 经同一 reducer 回放）＋12（消费者读态收编）；锚本身由 `59feb537` 引入，早于本变更 | `TestSeedFactReusesTheReducer` @ `assembly/checkpoint_test.go:2391`（must_assert `boot_folds_every_fact_through_the_live_reducer`） | `test` | 同上覆盖 `./assembly`；无 short-skip | `MUT-DOD-P2-reducer-bypass`（`assembly/run.go`，1）／未证杀 | 交付与锚在；**承载叶未唯一**（如实记缺口，交 13.R2 绑定） |
| P3 | 所有可恢复态是 Stateful 切片（任务＋治理收编） | 05-task-custody/tasks.md:8（1.2）、:65（消费者锚） | `TestDurableLoadReadsTheTaskSlice` @ `runtime/state/durable_test.go:1424`（must_assert `a_seated_account_pays_no_prefix_scan`） | `test` | 同上覆盖 `./runtime/state`；无 short-skip | `MUT-DOD-P3-slice-bypass`（`runtime/state/durable.go`，1）／未证杀 | 交付与锚在；判据未证 |
| P4 | boot 解码／索引读取与常驻规模／durable load／墙钟不随历史线性增长（真量） | 09-acceptance-real-measure/tasks.md:15（4.3，**未勾**）、:42（09.R4）；06-bounded-open-index/tasks.md:136（既有复跑表） | `TestSymptomAnchorBootCostFlatAcrossGenerations` @ `assembly/symptom_anchor_recovery_test.go:885`（must_assert `wallclock_flat_across_generations`；`bound{factor:2, vs:"the second boot of the same chain"}`；`note` 明写同一 2× 亦冻结 journal bytes／index entries／durable load，四量同判） | `test` | 同上覆盖 `./assembly`；**不是** `event-integrity-long`（该作业跑 `examples/wechat-bot` 的长跑锚） | `MUT-DOD-P4-window-unbounded`（`assembly/run.go`，丢弃「最新 checkpoint 的 cut 驱动的有界走查」）／未证杀；`MUT-DOD-P4-cold-unbounded` 已按 `check_event_integrity_mutations.py:1190-1198` **退役并写明理由**（其 patch 对象随退位删除） | 交付与锚在；四量判据在锚内逐条断言；**判据未证** |
| P5 | 主 spec 与代码同向；legacy 条款清零；golden 同步 | 10 域 R5／R5b／R5c／R5d／R6／R7（本文件 §十六–§二十二）＋13 证据绑定 | 门锚 `scripts/gates/check_spec_hygiene.py` ＋ 自测 `scripts/gates/test_check_spec_hygiene.py`（must_assert `main_spec_and_code_state_the_same_contract`） | `test` | `test#Lint` = `bash scripts/lint.sh`（ci.yml:108-109）→ lint.sh:125-126 跑自测与门；本机逐条直跑：自测 11 case PASS／门 exit 0 | `MUT-DOD-P5-legacy-clause`（打在 `storage-lifecycle-completion` 现文「保留保护与回收协调」，1）／未证杀 | **锚在、门绿**；**判据面未清零**（4 个 spec 仍以零定义符号写现行 SHALL，见 23.4③） |
| P6 | 死生产者删；未接线脚手架接线或删；废弃 spec 退役 | 10 域 R9b／R9c／R5–R7 ＋ 08.R3/R4（脚手架整删） | 门锚 `scripts/gates/check_dead_producers.py` ＋ 自测 `scripts/gates/test_check_dead_producers.py`（must_assert `no_producer_without_a_consumer`） | `test` | 同一 Lint 步骤 → lint.sh:164-165；本机直跑：自测 15 case PASS／门 exit 0（`3453 production function(s) in 32 package(s) has a consumer`） | `MUT-DOD-P6-dead-producer`（`tool/spec/spec.go`，1）／未证杀 | 锚在、门绿；**门把测试与示例算消费者 ⇒ 绿 ≠ 零生产死码**（R9a 已记，本表沿用） |
| P7 | 中段过程呈现为可召回票据，模型自主取全文 | 04-midband-ticket-recall/tasks.md:27（4.4）、:36、:44 | `TestSymptomAnchorMidbandProcessRecalledByTicket` @ `tests/integration/history/symptom_anchor_midband_test.go:141`（must_assert `midband_folded_entries_presented_as_recallable_tickets`） | `test` | 同上覆盖 `./tests/integration/history`；无 short-skip | `MUT-DOD-P7-midband-summary-feed`（`memory/replay/age_segments.go`，1）／未证杀 | 交付与锚在；判据未证 |
| P8 | 摘要预算可配置（非硬编码 2048）；摘要经租约＋声明采样 | 03-compression-configurable/tasks.md:27（3.2）、:36、:166 | `TestHistorySummaryObeysThePlanBudget` @ `memory/replay/age_segments_test.go:1544`（must_assert `summary_bounded_by_the_declared_plan_budget`） | `test` | 同上覆盖 `./memory/replay`；无 short-skip | `MUT-DOD-P8-fixed-budget`（`memory/replay/age_plan.go`，1）／未证杀 | 交付与锚在；判据未证 |

**选择器覆盖的实测凭据（P5/P6 特有）**：`lint.sh` 在 `set -euo pipefail` 下逐门 `run`，本机执行到 doc_budget 步即因 **git 对象不可读**（iCloud 驱逐：`git ls-tree -r HEAD` 仅 387 项、`git read-tree` 建不出索引）而中止，故 `lint.sh` 整体不可作为本机凭据；两门与两自测按 lint.sh 的同一命令**逐条直跑**得 exit 0（见 §二十五验证表）。CI 侧判据只能是「步骤命令覆盖该选择器」（负锚②的原文判据），本表按此裁定并记环境限制，不把本机不可达说成红或绿。

### 23.3 按事实修正的四处真实冲突（§九 → §二十三）

1. **「变异未登记」×6（P0/P1/P2/P3/P7/P8）→ 不成立**。九条 `MUT-DOD-*` 全 `enabled`，每条 `Replacement.old` 现树恰命中**一处**（上面 23.1 的 Python 复跑逐条打印）。真实状态是「已登记、**未证杀**」——`mutation_verdicts: []`，而非「未登记」。
2. **P1 锚名写错**：§九 记 `TestColdArchiveRefusesToOverwriteAnIdentity`，全仓 `func` 定义 **0 命中**；登记表实际绑定 `TestFactCommitAnswersNewExistingConflictUncertain`（`memory/ledger/segment_store_test.go:1431`，`must_assert identity_overwrite_refused_by_name`）。以登记表为准。
3. **P4 三处读数全废**：①`ci_gate` 现为 `test`（§九 记 `event-integrity-long`，那作业跑的是 `examples/wechat-bot` 长跑锚）；②`measure=wallClock` 但 `note` 明写同一 2× 也冻结 journal bytes／index entries／durable load，锚内对四量逐条断言（`symptom_anchor_recovery_test.go` 的 `indexEntries != 0`、`journalLogBytes` 递增前置、`integrityBytesScanned == journalLogBytes`、`slotTableBytesScanned == 0`、`addressedValueReads` 平坦等），故「`measure` 只写 wallClock ⇒ 与四量不符」不成立；③「`bound.factor=2` 与候选变异自述 8x 矛盾且判 `not_killed`」在今日登记表**无对应物**——8x 候选 `MUT-DOD-P4-cold-unbounded` 已退役并写明「patch 对象随退位删除、锚行在 `memory/ledger/journal.go` 不存在」；现行变异是 `MUT-DOD-P4-window-unbounded`（丢弃 cut 驱动的有界走查），仍未证杀。
4. **P5/P6「无锚」→ 不成立**：两条都是**门锚＋自测锚**（`gate`／`selftest` 双绑定），且由 `lint.sh:125-126`／`:164-165` 在 `test` 作业的 Lint 步内执行。§九 的「登记表自述 unguarded」是更早的登记文本，已被现登记表的 `note` 取代。

### 23.4 未达项（照实留未达，不退役、不改判据）

- ①**九条变异全部未证杀** ⇒ `check_dod_coverage.py` `exit 1`，P0–P8 无一可为「已达成」。缺口产出于 13 域变异账（运行产物 → `mutation_verdicts`），本叶不代做，**不改 baseline、不删要求**换 exit 0（本表复跑：`exit 1`，`9 gap(s)`）。
- ②**P2 无单一承载叶**：其子要求散在 05/06/07/12 四域，锚早于本变更。作缺口输入交 13.R2 绑定，不虚构叶号。
- ③**P5 判据面未清零**（门因 grandfathered baseline 未拦，属**假绿的一种**，须如实登记）：三族死符号在 Go 树 **0 定义**，却在 4 个主 spec 里仍是现行 SHALL／审查项——
  - `openspec/specs/architecture-review-standards/spec.md:60/:102/:144/:148/:153/:169`（`resolveMemoryStore`、`TombstoneSet`、`LifecycleManager`）
  - `openspec/specs/rustviking-config/spec.md:23`（`resolveMemoryStore` SHALL wire）
  - `openspec/specs/tombstone-partition-isolation/spec.md:5/:11/:20/:25/:26/:30/:35`（`TombstoneSet` 整能力）
  - `openspec/specs/error-handling-consistency/spec.md:22`（`TombstoneSet`）
  复跑：`grep -rn --include='*.go' -E "TombstoneSet|LifecycleManager|resolveMemoryStore" . | wc -l` → **0**。这 4 个文件不在本域 5.1.5 冻结写面（该行只点名 `storage-lifecycle-completion`／`event-segment-store`／`architecture-review-standards`／`memory-curation`，其中只有 `architecture-review-standards` 命中且**未改**），故只报不改，交 08/13 裁定归属。
- ④**P6 的判据严格性**：门把测试与示例算消费者，绿不等于零生产死码；生产可达性结论只认 R9a 的独立盘点与 R9b/R9c 的正式入口实测。

## 二十四、10.R2 旧项逐条处置终表（取代 §十）

> 一行恰一处置（保留／删除／接线／替代），附依据、实施叶、精确写面、现状。**接线行数＝0**：唯一候选（`memory/ledger/{lifecycle,retention,guard}.go`）已由 08.R3/R4 整删，`NewRetention`／`NewLifecycle`／`TombstoneSet`／`LifecycleManager`／`Compactor`／`resolveMemoryStore`／`BindStore`／`NewDurableCommit` 现树 **Go 命中全 0**（无「未接线」可留）。**旧 5.x 复选框本轮保持未勾**：每行原 verify 三锚未逐条复跑，R2 的交付是处置表本身（其文本亦明写「不要求本叶先把残留清零」），不以处置表冒充勾选。

| 旧行 | 现树实测（可复跑） | 处置 | 依据 | 实施叶 | 精确写面 | 现状 |
|---|---|---|---|---|---|---|
| 5.1.1 | `grep -c "MUST NOT 改写为 LLM 生成文本" openspec/specs/task-skeleton-compression/spec.md` → 0；`segment_fold` 仍 `MUST NOT 是模型生成文本` | 保留 | 交付已在位，判据未动摇 | 已勾（2026-11-15） | `openspec/specs/task-skeleton-compression/spec.md`（零改动） | 已落 |
| 5.1.2 | `grep -rn "RebuildProjectionFromWAL\|MinEventKey" openspec/specs/` → 仅 `event-sourced-projection/spec.md:55` 退役注（无现行 SHALL）；Go 树 0 | 删除 | 恢复入口与其语义随机制退役，现行恢复只认锚点 survivor ＋ `compress.product` | 10.R9b（＋10.R5 spec 侧） | `openspec/specs/event-sourced-projection/spec.md`；`memory/replay/{fold.go(D,−165),projection.go(+6/−232)}` | 已落 |
| 5.1.3 | `compaction-scheduler` 已 superseded；`compression-recovery-ticket/spec.md` **仍写现行 SHALL**（`recovery.ticket` 两步恢复），而 `RecoveryTicket|WriteRecoveryTicket|recovery.ticket` Go 命中 **0**；`architecture.json` 的 ticket 锚已 **0**（R7 迁锚完成，原 B1 阻塞解除） | 替代（superseded） | 零定义符号不得留现行命令；退役前提（概念锚改绑）已满足 | 10.R5（`compaction-scheduler` 已落）／**ticket 未落** | `openspec/specs/compression-recovery-ticket/spec.md` | **未达**（不得计完成） |
| 5.1.4 | 两 spec 均在工作树 `M`；`resolveMemoryStore` Go 0；`SetCompactor`／`SetLifecycleManager` 在 specs 0 | 替代（改写为现行权威） | 死机制退役、活入口（`ledger.OpenTier`／`OpenAtomicStore`）转正 | 10.R5 ① | `openspec/specs/production-wiring/spec.md`、`production-wiring-fix/spec.md` | 已落 |
| 5.1.5 | `storage-lifecycle-completion`／`event-segment-store`／`memory-curation` 均 `M`（R314/R315/R316/R317 已落）；**`architecture-review-standards` 未改**，仍含 `resolveMemoryStore`／`TombstoneSet`／`LifecycleManager` 现行审查项 | 替代（逐条改写／退役） | 死条款清零；改写不新增 spec_hygiene 违例（violations 250 = baseline） | 10.R5 ②／10.R5b／10.R5c／10.R5d | 上述三 spec；`architecture-review-standards/spec.md` 待派 | **部分落**（3/4） |
| 5.1.6 | `grep -rc "现行折叠零 LLM" openspec/specs/` 无命中；`check_concept_budget.py` → exit 0（原 `FactLog 5>4` 已消，撤回条件解除）；8 个 superseded 只核不重写 | 替代（勘误句，不重写退役事实） | 勘误对象是 Purpose 下的现行断言，退役事实不动 | 已勾（2026-11-15，撤回条件现已解除） | `openspec/specs/llm-summary-generator/spec.md` | 已落（可复验绿） |
| 5.1.7 | `check_spec_hygiene.py` exit 0（`violations=250 baseline=250 stale=0 errors=0`）；`^## (ADDED|MODIFIED|REMOVED|RENAMED) Requirements` 在主 spec 0 命中 | 保留 | 结构与门收口已在位 | 已勾 | `openspec/specs/*/spec.md`（零改动） | 已落 |
| 5.1.8 | `check_vocabulary_alignment.py` exit 0（`violations=0 carrier=0 retired-word=0`）；主 spec 内 `冷归档|索引切面|排他闸|cold-archive|存活集` 全 0；`liveness` 仅剩能力名 `config-liveness`（非退役义项） | 保留（词汇核对＋退位义项删除） | 死词零残留，`docs/GLOSSARY.md` 岗位本义保留 | 已勾 ＋ 10.R6（派生文档） | `docs/GLOSSARY.md`（零改动）／`docs/api/*`（R6） | 已落 |
| 5.2.1 | `golden/runtime/durable_truth.json:305` 仍 `checkpoint:<partitionSeq>`；实现与 doc 已是 `checkpoint:<partition>:<partitionSeq>`（`api/event/checkpoint.go:69/:75`） | 删除（旧键形散文） | 与 `CheckpointKey` 实现及 doc 同向；门只比 `value`，须 grep 判 | 未派（R7 未覆盖该行） | `golden/runtime/durable_truth.json:305`（仅 reason 文本） | **未达**（golden prose 陈旧） |
| 5.2.2 | `architecture.json` note 内有 `勘误（W8-VOCAB）` 与「六端口保留为兼容读法、不新增端口」；**无** 5.4 审计结论（候选死的是 material 兼容派生）句 | 替代（追加勘误句） | 5.4 乙案裁定须入 golden 说明，三个棘轮数不改写 | 10.R7／本表 | `golden/runtime/architecture.json:6` note（追加，不改数） | **部分落** |
| 5.3.1 | `memory/replay/fold.go` 已 D；`RebuildProjectionFromWAL` Go 0 | 删除 | 生产调用方 0 且锚先迁后删 | 10.R9b | `memory/replay/{fold.go,projection.go,skeleton_assert.go}` | 已落 |
| 5.3.2 | 级联族 Go 全 0：`RebuildOptions`/`RebuildReport`/`ErrRebuildUnwired`/`ErrRebuildPartial`/`ErrChainScan`/`RetainRefType` | 删除（KEEP 同族保留） | **KEEP>0 实证**：`ErrChainPageLimit`=7、`FileSegmentStore`=64、`MigrateShardedRoot`=15、`BenchmarkFoldDepthCost`=3、`TestFoldDepthMeasurementExists`=2、`TestRebuildRefoldsIdempotent`=3 | 10.R9b | `memory/replay/{projection.go,replay_budget.go,skeleton_assert.go}` | 已落 |
| 5.3.3 | 6 处以手工 `projection.Append` 替代重建入口；`TestRebuildRefoldsIdempotent` 改 `PlanByAge`（判据逐字保留）；退役断言单列（不称等价） | 替代（锚迁移） | 先迁后删，无覆盖窗口；`TestFoldDepthMeasurementExists` 未随 `fold_test.go` 删 | 10.R9b | `memory/replay/{fold_test.go,replay_budget_test.go}`、`tests/integration/state/notice_projection_test.go`、`tests/integration/history/{projection_replay_and_budget_test.go,age_band_request_test.go,original_reference_isolation_test.go,event_integrity_test.go}`、`tests/integration/acceptance/scenario_age_fixtures_test.go`、`examples/wechat-bot/smoke_test.go` | 已落 |
| 5.3.4 | 原「4 冲突文件」按现树复核＝**锚行冲突 0**：11 CLEAN＋`examples/wechat-bot/smoke_test.go` 唯一脏 hunk 属 no-prefix 交接项（`git diff` 对该文件零 `Rebuild|Fold` 命中） | 替代（同批迁锚） | 冲突前提消失，无需等待他变更终态 | 10.R9b | 同上（`tests/integration/host/age_fold_test.go`、`event_integrity_test.go`、`scenario_age_fixtures_test.go`、`smoke_test.go`） | 已落（阻塞解除） |
| 5.3.5 | `BindStore` Go 0（含派生文档外）；`BindStore` in `docs/api/` 1 处属 R6 派生文档 | 保留（`Bind` 生产路径保留，兼容面删除已落） | 删的是 0 调用兼容面，生产走 `ledger.Bind`/`goals.Bind` | 已勾 | `features/evolution/governance/{denial.go,goal.go,skeleton_assert.go}`（已落） | 已落 |
| 5.4.1 | 审计表落 §14.5：9 个生产 face 全同时声明 `Port()`+`Material()`；剩余消费方＝15 个只声明 `Port` 的契约示例面／11 文件＋测试 face | 替代（审计结论替代「先审计后裁」的过程行） | 生产兼容路径可达 0（9/9 成对） | 10.R9a | `design.md` §14.5 | 已落 |
| 5.4.2 | 乙案落：`api/plugin/` 在工作树**零改动**；`compatMaterialOf` 3 命中（定义 `material.go:203`、doc `:197`、生产调用 `:250`） | 替代（保留兼容读法，只落 5.2.2 勘误） | 契约示例是公共 API 的合法可选声明形态，构成第二使用方；六端口常量是活分派键，两案均不退役 | 10.R9a／10.R7 | `api/plugin/material.go`（零改动）＋`golden/runtime/architecture.json:6`（同 5.2.2） | 已落（勘误半落见 5.2.2） |
| 5.5 | 本表 §25.6：五门 exit 0；`go build`/`go vet` 由 lint.sh 前两步跑过（`all modules verified`／build／vet 无输出） | 替代（域级复跑由 10.R4 核销） | 复跑项按 R4 归属集中核销，不再单列一叶 | 10.R4 | 无写入（只读复跑） | 部分验证（`go test` 全量本轮未跑，见 25.7） |
| 5.6 | `openspec validate runtime-recovery-ideal --strict --no-interactive` → exit 0；`changes/05-contract-sync-dead-code` 不存在（旧编号已改 10） | 保留（上报项，本域不改父级） | 编排者已归位 delta 语法；本域保持 delta 形态 | 已勾 | 无（父级归编排者） | 已落 |
| 5.7.1–5.7.6 | 六条已勾，spec 侧改写均在位（`task-skeleton-compression` 带归属/GREP 判据、`event-key-visibility` 重写、`session-checkpoint-material` 三面、`compression-pipeline` 预算面、P6 复测） | 保留 | 交付与凭据在，无需再处置 | 已勾 | 上述 spec（零改动） | 已落 |
| R1 | 本文件 §二十三（§九 已标取代） | 替代（对账表重写） | 事实修正、不静默退役 | 10.R1 | `design.md` §二十三／§九 头注 | 已落 |
| R2 | 本节 | 替代（台账重排为逐行处置） | 一行恰一处置＋依据＋叶＋写面 | 10.R2 | `design.md` §二十四／§十 头注 | 已落 |
| R4 | §二十五 | 替代（清理核销＋合并唯一性） | 交付＝每行处置＋实际变更文件，不以清单结账 | 10.R4 | `design.md` §二十五 | 已落 |
| R3 | 需 13.R2/R3 ＋ 04.R0 的拒绝入口证据，本单未派 | **不处置**（另一叶承接） | 其正锚含 `check_dod_coverage.py` exit 0，今为 1；归档前拒绝入口未建 | 10.R3（另派） | `design.md` §十一 | 未达（不计完成） |

## 二十五、10.R4 实施清理核销：R5–R9 产物、逐行写面与 spec 合并唯一性

> 零产品 Go、零 git 写、零 archive。R4 的「实施」由 R5–R9 分叶完成，本节做**核销与唯一性审计**：每行给出实际变更文件（不写清单结账）、合并是否有唯一结果、失效重命名是否被执行。

### 25.1 清理产物（现树实测，`git status --porcelain` 与 `--numstat`）

- **整删（D）**：`memory/replay/fold.go`（−165）、`memory/ledger/guard.go`（−96）、`memory/ledger/guard_memory.go`、`memory/ledger/lifecycle.go`（−468）、`memory/ledger/lifecycle_config.go`、`memory/ledger/retention.go`（−184）、`tests/integration/history/recovery_before_reclamation_test.go`（脚手架专用测试）。
- **净删（M）**：`memory/replay/projection.go`（+6/−232，只剩退役注释）、`memory/replay/replay_budget.go`（+3/−13，删 `seedConsumed`）。
- **锚迁移（M）**：`memory/replay/{fold_test.go,replay_budget_test.go}`、`tests/integration/{state/notice_projection_test.go,history/{projection_replay_and_budget_test.go,age_band_request_test.go,original_reference_isolation_test.go,event_integrity_test.go},host/age_fold_test.go,acceptance/scenario_age_fixtures_test.go}`、`examples/wechat-bot/{smoke_test.go,smoke_harness_test.go}`。
- **契约同步（M，8 篇主 spec，0 新增／0 删除／0 改名）**：`event-lifecycle`、`event-segment-store`、`event-sourced-projection`、`memory-curation`、`production-wiring`、`production-wiring-fix`、`session-checkpoint-material`、`storage-lifecycle-completion`。
- **golden**：`architecture.json`（概念棘轮 note 追加，三数未改）、`dod_predicates.json`、`spec_hygiene.json`（baseline 面，非本域本轮所改）、`no_prefix_criterion.json`（`??` 未跟踪，属 04 域交接面，本域零触）。
- **零改动而有意保留**：`api/plugin/`（5.4 乙案）、`memory/ledger/reference.go` 的 `NewScopedReader`（R9c 记保留、8 处调用均在测试）、`memory/ledger/tier_facts.go` 的 `Get`（R8 记引擎读能力保留）。

### 25.2 旧行 ↔ 实际变更文件（与 §二十四 的写面列互证，无第三份账）

`5.1.2/5.3.1/5.3.2/5.3.3/5.3.4` → `memory/replay/*`＋上列测试文件＋`event-sourced-projection/spec.md`；`5.1.3/5.1.4/5.1.5` → 六篇主 spec（`compaction-scheduler` 在 HEAD 已退役，本工作树零改动）；`5.3.5` → `features/evolution/governance/*`（HEAD 已落）；`5.4.1/5.4.2` → `design.md §14.5`（零代码改动）；`5.1.8/5.3.5` 派生文档 → `docs/api/*`（R6）。

### 25.3 spec 合并唯一性（同一能力跨域重复修改时是否只有一个结果）

- 全变更 **13 域 21 个 delta spec**；同能力重复修改 **3 组**：`session-checkpoint-material` × 3（05／07／12）、`checkpoint-state` × 2（05／07）、`bounded-recovery` × 2（06／09）。
- **需求标题集合审计：0 条重名**（三组逐条列出 = `任务终态由 checkpoint 切片承载并有界恢复`／`governance 恢复读切片而非全流扫`／`派生态有界且不删除已提交事实`／`Stateful 收编消费者读态且不新增挂点`／`任务终态有界恢复`／`框架统一 checkpoint 事实承载可恢复状态`／`bounded-recovery` 的 6+8 条）⇒ 合并结果是**不相交条目的并集**，不存在「后一份覆盖前一份」的对象。
- **但发现 3 处目标缺失（按「冲突拒绝」判为未达，交归属，本域零改动）**：
  1. `changes/05-task-custody/specs/checkpoint-state/spec.md` 声明 `## MODIFIED Requirements: 任务终态有界恢复`，而 **`openspec/specs/` 下无 `checkpoint-state` 目录**（`ls openspec/specs | wc -l` = 151，无此名；该能力只存在于归档 `openspec/changes/archive/2026-11-11-hot-update-and-purity-closure/changes/12-checkpoint-state/`）；同标题实际住在 `openspec/specs/session-checkpoint-material/spec.md:36`。
  2. 同构：`changes/07-governance-custody/specs/checkpoint-state/spec.md` 的 `框架统一 checkpoint 事实承载可恢复状态` → 实际标题在 `session-checkpoint-material/spec.md`（其六条标题内无此名，逐条比对见 `grep -n "^### Requirement:" openspec/specs/session-checkpoint-material/spec.md`：`checkpoint 协调者按会话触发并写该会话的派生态`／`Stateful 收编…`／`任务终态由 checkpoint 切片承载并有界恢复`／`governance 恢复读切片而非全流扫`／`插件 Material 声明…`／`派生态有界且不删除已提交事实`）。
  3. `changes/08-fact-retirement/specs/storage-lifecycle-completion/spec.md` 的 `## REMOVED Requirements: TTL check uses actual event timestamp` 在主 spec **已无该标题**（`storage-lifecycle-completion/spec.md` 现只剩 1 条 `保留保护与回收协调`），属对已不存在要求发 REMOVED。
  三者的处置都不是「取最后一份」，而是**冲突拒绝**：归档时找不到目标即拒；本域写面不含他域 delta，故只登记归属（05／07／08），不改名、不复制标题、不代建 `checkpoint-state` 目录。
- 复跑：`python3` 扫描全 delta 的 `## (ADDED|MODIFIED|REMOVED) Requirements` × 标题，与 `openspec/specs/<cap>/spec.md` 的标题集合比对（本节 3 处即该扫描的全部输出），另 `ls openspec/specs | wc -l`＝151、`grep -n "^### Requirement:" openspec/specs/session-checkpoint-material/spec.md`。

### 25.4 已失效历史重命名不执行

- `changes/05-contract-sync-dead-code/` **不存在**（`ls -d` 报 No such file）：旧编号 05 的域已改 10，其余域编号（01–13）连续无重号。
- 旧 5.x 行号与「05 域」字面保留在 `tasks.md`（作为核销索引），**不追改历史编号**；域目录与 delta 能力名不改名（`05-*` 旧称只出现在历史引文）。
- 不新建 `openspec/specs/checkpoint-state/`：该能力的归属已并入 `session-checkpoint-material`，新建即造第二真相。

### 25.5 delta 合并负例的处置（勘误，不自造假锚）

R4 原文拟新增 `TestDeltaMergeRefusesConflictingRequirementText`／`TestDeltaMergeRejectsMissingRenameSource`；经 R314 裁定③与 §16.3 定为**退役不建**——被测实现不存在（`ls scripts/gates/` 无 `check_openspec_sync.py`；真实 delta 合并由外部 `openspec` CLI 1.2.0 `archive` 承担，`scripts/gates/check-openspec.sh` 只跑 `validate --all --strict`）。本节以 25.3 的**标题解析审计**作可复跑替代，不冒充实现级负例，也不把 strict／spec_hygiene 绿当作合并唯一性的证据。

### 25.6 本轮验证（原始 exit，命令可复跑；分号分隔，不与 commit 串联）

| 命令 | exit | 关键读数 |
|---|---|---|
| `python3 scripts/gates/check_dod_coverage.py` | **1** | `9 gap(s)`：P0–P8 皆「registered and unproven」（缺口输入，属 13 域变异账） |
| `python3 scripts/gates/test_check_dod_coverage.py` | **0** | `54/54 cases passed`（未改判据、未放宽） |
| `python3 scripts/gates/check_spec_hygiene.py` | **0** | `specs=151 requirements=764 violations=250 baseline=250 stale=0 errors=0` |
| `python3 scripts/gates/test_check_spec_hygiene.py` | **0** | `11 case(s) PASS` |
| `python3 scripts/gates/check_dead_producers.py` | **0** | `3453 production function(s) in 32 package(s) has a consumer` |
| `python3 scripts/gates/test_check_dead_producers.py` | **0** | `15/15 cases passed` |
| `python3 scripts/gates/check_concept_budget.py` | **0** | `RESULT: ok`（`superseded_specs_with_requirements: 0`） |
| `python3 scripts/gates/check_durable_truth_registry.py` | **0** | `go_files=280 persistence_files=18 prefixes=28 surfaces=16 errors=0` |
| `python3 scripts/gates/check_vocabulary_alignment.py` | **0** | `violations=0 carrier=0 retired-word=0` |
| `python3 scripts/gates/check_doc_links.py` | **0** | `42 document(s), 130 link(s), 56 anchor(s), 0 error(s)` |
| `openspec validate runtime-recovery-ideal --strict --no-interactive` | **0** | `Change 'runtime-recovery-ideal' is valid` |
| 变异 patch 锚唯一性（23.1 Python） | **0** | 九条 `MUT-DOD-*`：`old-count=1`、`enabled=True`（P5／P6 为 gate-anchor，打出 `storage-lifecycle-completion/spec.md`／`tool/spec/spec.go`） |
| `gofmt -l memory/replay/skeleton_assert.go assembly memory/replay api/plugin` | **0** | 无输出（§二十二 记的 gofmt 红已消） |
| `bash scripts/gates/lint.sh` | **1** | 中止于 doc_budget 步，红因＝**环境**：本机 git 对象 iCloud 驱逐（`git ls-tree -r HEAD` 仅 387 项、`git read-tree` 建不出索引 ⇒ `git ls-files` 空），非仓库缺陷；故不计红亦不计绿 |

### 25.7 未达与留白（不宣称全绿）

1. **九条变异未证杀** ⇒ DoD 判据不可宣称达成（23.4①）。
2. **P5 判据面在 4 个 spec 未清零**（23.4③）——门绿因 grandfathered baseline，属假绿面，须他域修。
3. **`compression-recovery-ticket` 未退役**（5.1.3）、**`durable_truth.json:305` 旧键形未改**（5.2.1）、**`architecture.json` note 缺 5.4 审计勘误句**（5.2.2）、**`architecture-review-standards` 未改**（5.1.5 的 1/4）。
4. **3 处 delta 目标缺失**（25.3），归档前必须由 05／07／08 修正能力名或标题。
5. **`go test` 全量本轮未跑**（5.5 的域级复跑只到 `go build`/`go vet`，由 lint.sh 前两步证），故不声称回归完成；R9b 记的 `examples/wechat-bot` smoke 8 红仍未在本环境消除（用户数据目录禁触＋命令缺 `-race`）。
6. **10.R3 未处置**：其正锚含 `check_dod_coverage.py` exit 0（今为 1）与 13 的放行证据门，另派。

## 二十六、R325 10.R5e 落地：P5 判据面四篇死 spec 逐条留删（2026-11-21）

**性质**：R325 把 §二十五 25.7② 的「P5 判据面在 4 个 spec 未清零」派为独立叶 10.R5e。本节是该叶的实施留痕：四篇逐篇取证定活否、一次改写、六个门复跑，**不立新设计**。零产品 Go、零 git 写、零 archive、零一级编排改动；写面＝`openspec/specs/{tombstone-partition-isolation,architecture-review-standards,error-handling-consistency,rustviking-config}/spec.md`、`scripts/gates/doc_budget.json`（净删只降）、本域 tasks/design。

### 26.1 逐篇活否取证（现树实测；零命中即判死，义项先分后判）

| 主 spec | 取证对象 | 命令（节录） | 实测 | 判定 |
|---|---|---|---|---|
| `tombstone-partition-isolation` | 墓碑族六符号 | `for w in TombstoneSet MarkTombstone IsTombstone getTombstoneSet RecoverFromKV PartitionState; do grep -rn --include='*.go' -F "$w" . | wc -l; done` | 逐词 **0** | **整能力退役** |
| 同上 | 墓碑键前缀 | `grep -rn --include='*.go' -F "tomb:" . | wc -l` | **0** | 无写入方也无读方 |
| 同上 | 同名异义项 | `grep -rn --include='*.go' -w tombstone memory/ledger/journal.go` | 命中（`:175`／`:186`／`:860`） | **日志帧键删除记录**，不构成事件级墓碑，**不承接**（同 10.R5c 处置） |
| `architecture-review-standards` | 原十一条要求的点名符号（`TagentAgent`／`AgentToolWrapper`／`EventBus`／`SessionProjection`／`PlainTool`／`ToolRegistry`／`RegisterBuiltinTools`／`SwappableModel`／`MemoryPlugin`／`SummaryPlugin`／`CommandTool`／`TmuxMonitor`／`InMemRelationStore`／`InMemoryStore`／`RelationStoreProvider`／`TombstoneSet`／`LifecycleManager`／`MockRustVikingClient`／`findAliveAncestor`／`ExtractEventType`／`IsSpecialEventType`／`ReadPartitionIDs`／`resolveMemoryStore`／`FullEvent`／`StateDelta`） | 同形 `for w in …; do grep -rn --include='*.go' -F "$w" . \| wc -l; done` | 逐词 **0**（`PartitionIDFromName` 77／`ReadNamespaces` 27／`AgeFoldPlugin` 29／`CompressionConsumer` 60 为活，但均属他能力条款面） | 十条退役、**仅⑦并发安全无替代承载而保留** |
| `architecture-review-standards` 的活替代承载体 | 待指路的现行 owner | `grep -n "^### Requirement:" openspec/specs/{compression-pipeline,causal-chain-isolation,event-segment-store,storage-lifecycle-completion,production-wiring,thinking-plan-event-classification,exec-integrity,execution-resource-lifecycle}/spec.md` | 逐篇在位 | 十条各自有唯一现行承载（见 26.3） |
| `error-handling-consistency` | 日志包统一 | `grep -rn --include='*.go' -F "trpc-agent-go/log" . \| wc -l`；`grep -rln --include='*.go' -E '^\s+"log"$' . \| wc -l` | **34**／**0** | 第一条**活**，原文不动 |
| 同上 | 第二条的检查面 | `for w in KVPut KVGet KVDelete KVScan RelationStore TombstoneSet; do …; done`；`grep -rn --include='*.go' -F "SetParent" . \| wc -l`；`grep -rn --include='*.go' -F "GetParent" . \| wc -l` | 前六词 **0**／SetParent **7**／GetParent **35** | 实质承诺活、**检查面正名为现行存储提交与关系读写**；`tool/recall/get.go:198` 与 `trace.go:118` 证明读失败具名不伪装 |
| 同上 | 第三条的迁移对象 | `grep -rn --include='*.go' -F "FullEvent" . \| wc -l`；`grep -rn --include='*.go' -iF "Phase 1 event view" . \| wc -l`；`find . -name memory_plugin.go -o -name smart_compress.go` | **0**／**0**／两文件均不在树 | 迁移已完成，**整条退役** |
| `rustviking-config` | 原载体名 | `grep -rn --include='*.go' -F "MemoryConfig" . \| wc -l`；`grep -rn --include='*.go' -F "resolveMemoryStore" . \| wc -l` | **0**／**0** | 原表述死 |
| 同上 | 现行载体 | `grep -rn --include='*.go' -F "RustVikingBinary" .`；`sed -n '250,266p' memory/ledger/config.go` | `config.go:53-55`（字段）、`:254-263`（空则回落 `rustviking`＋`LookPath`，失败 `ErrRustVikingMissing`）、测试 5 处 | 第一条**活**（载体为开档配置，非 `MemoryConfig`） |
| 同上 | 真实客户端与真实后端 | `grep -n "func newRustVikingKV" -A 3 memory/ledger/rustviking_kv.go`；`sed -n '325,367p' tests/integration/history/storage_capabilities_test.go` | `rustvikingKV` 每次操作驱动 CLI；用例在 CLI 缺失时 `t.Skipf` 并写明「no mock stands in for it」，重开档读回同一字节 | 第二条**活** |
| 同上 | 原「配置文件键可设」「列入资源门」两分句 | `grep -rn -F "rustviking_binary" --include='*.yaml' --include='*.yml' --include='*.json' .`；`grep -n "sectionFields(source, field, e, \"type\", \"path\", \"read_namespaces\", \"engine\")" api/config/decode.go`；`grep -rn "rustviking" docs/OPERATION.md .github/` | 配置文件 **0**；声明面只解码四键，无该键；`docs/OPERATION.md` §C 资源门清单与 CI 均**无** rustviking 行 | 两分句**无承载**，改写成现行可判场景（见 26.4 偏离登记） |

### 26.2 改写落点（四篇行数；载体坐标不进 spec 正文，全落本节）

| 主 spec | 改前→改后 | 形态 | 留/删要点 |
|---|---|---|---|
| `tombstone-partition-isolation` | 35→26 | 整能力退役，照 10.R5b／`production-wiring-fix` 先例 | Purpose 三段 banner（superseded／只拒销毁＋隔离承诺指路／同名异义项排除）；Requirements 只余一条「历史条款已退役」＋2 场景，现行关键词全部撤到 banner（`## Requirements` 内无 SHALL/MUST） |
| `architecture-review-standards` | 208→23 | 部分活：10 条退役＋活承诺指路，1 条（并发安全）保留 | banner 逐条给出 ①–⑪ 的现行唯一承载；⑦改写为「每条读写路径须能指出保护手段」，删去 5 个死符号举例；新增「并发面以竞态检测交付」场景（现行承载＝`scripts/gates/race_check.sh`） |
| `error-handling-consistency` | 51→37 | 部分活：2 条在位＋1 条退役 | 第一条原文不动、场景由死文件改为组合根后台循环；第二条检查面正名为存储提交／提交屏障／原文读写／关系父子链读写，并新增「原文读三类结果不折叠」场景（`ErrOriginalMissing`／`ErrOriginalExpired`／底层读失败三者可分）；第三条改为 banner 内的完成声明，不再作现行命令 |
| `rustviking-config` | 47→52 | 部分活：2 条在位、载体正名 | 第一条删 `MemoryConfig` 改「存储开档配置」，新增「可执行文件解析失败」场景（具名拒绝、不许替身或内存档顶替）；第二条删 `resolveMemoryStore` 改「开档入口」，删死类名括注，`-count=1`／资源门分句改为「CLI 缺失具名跳过、不以替身结项」（净增 5 行＝两条新场景） |

**硬约束碰撞（沿用 §17.2 结论）**：`check_concept_budget` 只对**标了 superseded 的**主 spec 在 `## Requirements` 内计 SHALL/MUST。故墓碑篇按先例把现行关键词全撤到 banner；另三篇属部分活，**不得**出现 superseded 字样的 blockquote（否则整篇剩余 SHALL 全数变红），本次以「同步注记／勘误」措辞落地，复跑 `superseded_specs_with_requirements: 0` 印证。

### 26.3 活承诺承接表（不重复立）

| 退役条款的实质承诺 | 现行唯一载体 | 本轮动作 |
|---|---|---|
| 分区隔离（墓碑不跨分区、查询不跨分区） | `event-segment-store`「resolvePartitions 默认隔离（无分区参数返回空）」＋「后端不可变与隔离一致性」 | 原文未动，仅 banner 指路 |
| 不得物理销毁已提交事实、窗口外具名拒绝 | `storage-lifecycle-completion`「保留保护与回收协调」 | 原文未动 |
| 未结义务事实留在热窗 | `event-segment-store`「未确认恢复材料的单属主 pin」 | 原文未动 |
| 角色到事件类型分类、原文全文不被截断 | `thinking-plan-event-classification`＋`event-type-unification`＋`event-segment-store`（原文按字节一致） | 原文未动 |
| 判定/执行分层、恢复不重折 | `compression-pipeline` 三段落条款 | 原文未动 |
| 因果链独立于事件字段 | `causal-chain-isolation` | 原文未动（其自身仍以 `MemoryPlugin.lastEventKeys` 表述，见 26.6③） |
| 存储装配与关闭次序 | `production-wiring`「存储装配经单一开档入口派生」 | 原文未动 |
| 日志包统一、错误不静默 | 本 spec 第一／二条（在位）＋`code-documentation` | 本叶即载体，未外移 |
| 执行回执完整性与资源释放 | `exec-integrity`＋`execution-resource-lifecycle` | 原文未动 |

**不立第二份**：四篇均不复述上述承诺，避免与 `storage-lifecycle-completion:17`「MUST NOT 立第二真相」冲突。

### 26.4 偏离记录（与任务行原文的差异，逐条具名）

1. **`architecture-review-standards` 判为「部分活」而非整能力退役**：⑦并发安全与⑧代码一致性的**断言句**不以任何死符号为主语（死符号只在其举例位）。删除断言会删掉无替代承载的活审查义务，故保留⑦、把⑧指到两个现行 owner，其余十条退役。此判定使该篇不能标 superseded，与 26.2 的硬约束一致。
2. **`rustviking-config` 净增 5 行**：原「`rustviking_binary` 在配置里设置」与「真实 CLI 用例列入资源门」两分句经 26.1 末行取证**无承载**（声明面只解码 `type`／`path`／`read_namespaces`／`engine`；资源门清单与 CI 无该行），故换成现行可判的两条场景。若裁定要求恢复「列入资源门」，须另改 `docs/OPERATION.md` §C 与 CI（本叶写面外）。
3. **`error-handling-consistency` 第三条整条退役**（不留改写正文）：迁移对象与点名的两个源文件均不在树，「注释已清洗」无剩余实例可判，留一条现行命令只会重造空转判据。

### 26.5 本轮验证（原始 exit，命令可复跑；分号分隔，不与 commit 串联）

| 命令 | exit | 关键读数 |
|---|---|---|
| `openspec validate runtime-recovery-ideal --strict` | **0** | `Change 'runtime-recovery-ideal' is valid` |
| `bash scripts/gates/check-openspec.sh` | **0** | `Totals: 157 passed, 3 failed (160 items)`（失败＝`config-surface-normalization`／`event-driven-consumption`／`post-training-data-loop` 三个非门控 in-flight change，与改前同数）；`OK: all main specs pass validate --strict` |
| `python3 scripts/gates/check_spec_hygiene.py` | **0** | `specs=151 requirements=752 violations=242 baseline=250 stale=8 errors=0`（requirements 764→752＝墓碑篇 −1、审查标准篇 −10、错误处理篇 −1；violations 250→242＝删掉 8 条具名违例行，未新增；stale 8 为被删行的基线残留，非错） |
| `python3 scripts/gates/check_doc_links.py` | **0** | `42 document(s), 130 link(s), 56 anchor(s), 0 error(s)` |
| `python3 scripts/gates/check_concept_budget.py`（`GIT_DIR=/tmp/ri-gitdir GIT_INDEX_FILE=/tmp/r2d-index`） | **0** | `counts.domain_surface.superseded_specs_with_requirements: 0`、`counts.findings.domain-surface: 0`、`RESULT: ok` |
| `python3 scripts/gates/check_doc_budget.py`（同上 ALT git） | 首测 **1**（`实测 21673 < 基线 21876（-203）`）→ `--tighten` **0**（`baseline.lines lowered: 21876 -> 21673`，files 231 不变）→ 复跑 **0** | `measured=baseline=21673`、`slack.lines=0` |
| 负锚：死符号在该 4 篇不作现行命令 | **0/命中仅在 banner** | `grep -nE "TombstoneSet\|MarkTombstone\|LifecycleManager\|MockRustVikingClient\|resolveMemoryStore\|MemoryConfig\|ExtractEventType\|InMemoryStore\|AgentToolWrapper\|KVPut\|TmuxMonitor\|FullEvent\|ParentKey"` 逐篇输出仅命中 banner 行（墓碑篇另命中退役要求的**历史标题清单**一行，无现行关键词） |
| 正锚：活承诺在位 | 命中 | `grep -c "MUST NOT 物理销毁\|fail-closed\|capability 缺失\|单属主 pin" openspec/specs/event-segment-store/spec.md` ＝ **8**（与 10.R5d 后同数，未重开）；`grep -n "^### Requirement:" openspec/specs/storage-lifecycle-completion/spec.md` ＝ 1 条「保留保护与回收协调」 |
| 改动面 | — | `diff -u /tmp/ri-r5e-before/<name>.md openspec/specs/<name>/spec.md` 四篇逐篇可复跑；`wc -l` 合计 341→138 |

**环境**：`.git/HEAD` 恒空读取故障沿用 ALT 目录（`GIT_DIR=/tmp/ri-gitdir`＋`GIT_INDEX_FILE=/tmp/r2d-index`）；`go` 未参与（零产品 Go 改动），故 `GOFLAGS=-buildvcs=false` 仅作环境约定、无调用点。

### 26.6 级联发现（未改，超本叶写面，交主 agent 裁归属）

1. **`tool-security-alignment` 同属 P5 判据面**：`runAsUser`／`runAsGroup`／`NewCommandTool`／`WithTmuxRunAsUser` 全树 **0** 命中，`tool/exec` 无 `sudo` 包装（`TmuxExecutor` 60 处仍在，但不再携带用户/工作目录选项）⇒ 该主 spec 的执行用户隔离条款同样无载体，建议另立叶。
2. **`concurrency-hardening` 四条要求全为死符号**（`TagentAgent`／`TmuxMonitor.running`／`InMemRelationStore.truncateJournal` 全 0）⇒ 与⑥同族，宜由后续叶统一处置；本叶保留的并发审查条**不以**它作承接（避免指到另一篇死 spec）。
3. **`causal-chain-isolation` 仍以死符号表述现行 SHALL**（`MemoryPlugin.lastEventKeys`／`RelationStore.SetParent`，`MemoryPlugin` 与 `RelationStore` 均 0 命中；现行是 `MemoryRelations` 的 `SetParent`／`GetParent`）⇒ 本叶把它作为④的承接目标，但其正文载体名待正名，属该篇自身的活否取证。
4. **`golden/runtime/spec_hygiene.json` 未动**：本叶删去的 8 条违例留下 stale 8，`--write-baseline` 属 baseline 面且无裁定，故只报不改。
5. **P5 门锚漂移延续**：`check_event_integrity_mutations.py` 的 `MUT-DOD-P5-legacy-clause` 原打 `event-lifecycle`（10.R5b 已记 `not_applied`）；本叶进一步删去四篇的 legacy 正文，若后续把该变异重指到本四篇之一，须避开本节已删条款——重指对象宜继续取 `storage-lifecycle-completion` 的禁入条款。
6. **未动**：`openspec/changes/**` 之外的 archive、一级编排、产品 Go、CI、`golden/runtime/*baseline` 面。
7. **口径限制**：本节为静态读码与门禁结果，本叶纯规格面、零产品 Go 改动，未跑 `go test`／`go build`。

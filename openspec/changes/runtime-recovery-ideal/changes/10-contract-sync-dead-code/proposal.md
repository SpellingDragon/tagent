# 05 契约同步 + 死码清理

> 一级依据：[design.md](../../design.md) D1 判据 **P5 契约一致**／**P6 无死代码**、D2 子域表 05、D4「05 独立但后置」、§七 消费者枚举「契约」行。
> 本域是契约/清理面：不改恢复语义、不改 boot 路径簇、不新增能力。

## Why

主 spec 是能力的事实来源（`openspec/AGENTS.md`），但当前有三类与代码反向的记载，且**继承清单已过时**——`archive/2026-11-03-runtime-architecture-convergence/evidence/06-compression-legacy-authority.md` 是归档改写**前**的输入清单，其点名的 16 个主 spec 中 8 个已在该波归档时标 superseded。本域按**当前树实测**重新定面，不照抄旧计数。

1. **主 spec 禁 LLM，代码写 LLM 综述（✅ 实测反向）**：`openspec/specs/task-skeleton-compression/spec.md` 的「老带段整段折叠为一条历史综述」原写「综述正文 … MUST NOT 改写为 LLM 生成文本」，而 `assembly/compress_consumer.go:275-291` `summarizeProduct` 对 `ProjectionTypeHistorySummary` 调 `c.gists.SummaryGist` 改写正文，实现是 `assembly/summary_gist.go:57` `bandSummaries.SummaryGist`（读会话 agent 的模型），生产接线 `assembly/run.go:578` `withSummaryGist(bandSummaries{run: r})`。
   **同 spec:51 不是反向**：`summarizeProduct` 的类型闸（compress_consumer.go:276）只放 `history_summary`，`segment_fold` 产物正文仍是机械一行读法，故 :51「MUST NOT 是 LLM 生成文本」仍成立，MUST NOT 一并改写。
   连带：`openspec/specs/llm-summary-generator/spec.md` 的 Purpose 下 superseded 备注断言「现行折叠零 LLM：段折叠与历史综述都是确定性工程产物」——该句已被上述接线推翻，是**主 spec 内的现行事实断言**（非归档历史），须附勘误。
2. **主 spec SHALL 已退役机制（✅ 实测代码零定义）**：`compression-recovery-ticket` 的 Requirement「compression-recovery-ticket 契约」仍 SHALL「链上票据事实 `recovery.ticket` … 唯一一份可覆盖的快照 blob」，而 `RecoveryTicket`/`WriteRecoveryTicket`/`recovery.ticket` 在 Go 树 0 命中（12.6 已退役）。同类：`compaction-scheduler/spec.md`（「历史条款已退役」，原 L1→L2/L2→L3 压实调度）、`production-wiring/spec.md`（「Production wiring creates and starts all lifecycle subsystems」「Subsystem references injected via setter methods」「Close follows strict shutdown order」）、`production-wiring-fix/spec.md`（「生产入口创建并启动生命周期组件」「优雅关闭流程」「wiki 记录 resolveMemoryStore 完整组装」）、`storage-lifecycle-completion/spec.md`（「L1 to L2 compaction is automatically scheduled」「L2 to L3 compaction is automatically scheduled」「filterTombstoned removes tombstoned events」）、`event-segment-store/spec.md`（「FileSegmentStore supports graceful shutdown via Close」「LocalFileKV Sync 为原子快照屏障」「compaction 源窗口读失败必须中止本轮」）、`architecture-review-standards/spec.md`（「审查 SHALL 验证因果关系完整性」「审查 SHALL 验证并发安全」「审查 SHALL 验证生产接线完整性」）仍 SHALL `Compactor`/`LifecycleManager`/`TombstoneSet`/`memory/localfile` 档位——四符号与 `memory/localfile` 包在树中 0 命中（`ls memory/` 无 localfile）。
3. **主 spec SHALL 死生产者（✅ 实测零生产调用方）**：`event-sourced-projection` 的 Requirement「历史 compaction 事实的冷启动只读重建」（旧行号引用 :47-51）「历史 compaction 事实的冷启动只读重建」SHALL 提供 `RebuildProjectionFromWAL`，`:156` 再次 SHALL 其幂等；而 `memory/replay/projection.go:342` `RebuildProjectionFromWAL` 与 `memory/replay/fold.go:49` `ReplayProjection.Fold` 只有 `memory/replay/skeleton_assert.go:32/:101` 编译期断言与 12 个测试文件引用，生产调用方 0。现行重启恢复是 `Run.seedAnchoredReplay`（`assembly/run.go:2562`，:2633/:2676 调 `RestoreProducts`）读 `compress.product`，全量兜底是 `seedFullReplay`（:2470，:2524）。**5.1 与 5.3 因此耦合**：删生产者必须同批退役这两条 SHALL，否则主 spec 立刻反向。
4. **golden prose 漂移（✅ 实测门不拦）**：`golden/runtime/durable_truth.json:300` reason 仍写 `checkpoint:<partitionSeq>`，代码 `api/event/checkpoint.go:75-76` `CheckpointKey` 已是 `checkpoint:<partition>:<partitionSeq>`（分区前缀修复）。门 `scripts/gates/check_durable_truth_registry.py:460` 只比 `entry["value"] != value`（前缀字面量 `checkpoint:`），prose 不在判据内 → 门绿而记载错。
5. **兼容读法未审计（⚠️ 候选死码，先审计后裁）**：`golden/runtime/architecture.json:6` 记「六端口保留为兼容读法」。实测六端口常量（`api/plugin/material.go:25-45`）**仍活**：`validatePort`（:57）、`NewFlow` 取 `instance.Port()`（:254）、`api/plugin/phases.go:214` 按 port 分派。真正候选死的是**兼容 material 派生**`compatMaterialOf`（material.go:203）——9 个生产 face 全部同时实现 `Port()` 与 `Material()`，`materialOf`（:190-195）的兼容分支生产零消费；剩余消费方是 `examples/contract/api_plugin/*_example.go`（14 个只声明 Port 的 face，受 `check_concept_budget.py`  canonical example 门约束）与测试。是否退役须先枚举消费者并判第二使用方独立性，不得凭「六端口」字样整体删。

**不是违背（接受边界，须分清）**：`MigrateShardedRoot`（`memory/ledger/migrate.go:76`）是**活的操作员工具**，正式入口 `scripts/migrate-kv/main.go:24`，一级 proposal 已点名退位可借其「复制+校验」半段 → **KEEP，不列入死码**。（**勘误 2026-11-16**：退位已整体删除，故「借其复制+校验半段」的用途作废；`MigrateShardedRoot` 的 KEEP 结论不变，依据只剩它是活的操作员工具与既有迁移面。）`FileSegmentStore`（`memory/ledger/segment_store.go:244`）是现行 journal 档位实现，**不是**已退役的 localfile 引擎 → 只退役 spec 中的 `memory/localfile` 路径与 `Compactor`/`LifecycleManager`/`TombstoneSet` 条款，MUST NOT 顺手删 `FileSegmentStore` 的现行 Close/Load 条款。`FoldWindow.MaxTokens`（`assembly/age_fold.go:26-27`）是内部字段名，config-surface-normalization 只改配置键（其 03 design 明写「FoldWindow.MaxTokens 保留」）→ `task-skeleton-compression/spec.md` 的「压缩窗口由三字段界定」不改。`window-recovery` 的「状态持久通道」条款「MUST NOT 通用注册钩子」是**归档独有**（`openspec/specs/window-recovery/` 不存在，权威文本在 `openspec/changes/archive/2026-11-11-hot-update-and-purity-closure/changes/06-window-recovery/specs/window-recovery/spec.md`），不是活主 spec 反向，不列入本域。

## What

- **5.1 主 spec 转正/退役**：按下方「逐 spec 处置面」改写活条款；已 superseded 的 8 个只核门绿 + 勘误其被推翻的现行断言，不重写历史。
- **5.2 golden 同步**：`durable_truth.json:300` reason 键形改 `checkpoint:<partition>:<partitionSeq>`；`architecture.json:6` 按既有勘误体例（该行已有「勘误（W8-VOCAB）」先例）**附勘误**说明兼容读法的审计结论，不改写棘轮历史数值。
- **5.3 死生产者删**：删 `ReplayProjection.Fold`（fold.go:49）、`RebuildProjectionFromWAL`（projection.go:342）及其 `skeleton_assert.go:32/:101` 断言；12 个测试锚按「迁移到现行路径 / 随机制退役」逐文件处置，MUST NOT 整片删覆盖。
- **5.4 六端口兼容读法审计→裁定**：枚举 `Port()`/`compatMaterialOf`/`materialDeclarer` 全部消费者（生产 / 契约示例 / 测试三分），按第二使用方独立性裁退役或保留，结论落 golden 勘误与 spec。

### 逐 spec 处置面（当前树实测，非继承计数）

| 主 spec | 当前状态 | 处置 |
|---|---|---|
| `task-skeleton-compression` | 活，:74 反向 | MODIFIED :74（历史综述正文＝机械读法为底，模型可得时为有界语义综述，失败/无模型/空答降级回机械）；:51 不动；:157 不动 |
| `llm-summary-generator` | 已 superseded | 只附勘误：:5「现行折叠零 LLM」被 run.go:578 推翻 |
| `event-sourced-projection` | 活，:47-51/:156 SHALL 死生产者 | REMOVED「历史 compaction 事实的冷启动只读重建」+ MODIFIED :156 去掉 `RebuildProjectionFromWAL` 分句（与 5.3 同批） |
| `compression-recovery-ticket` | 活，Requirement「compression-recovery-ticket 契约」（旧行号引用 :8）SHALL 已退役票据 | 整能力标 superseded（体例照 `compress-event-enrichment/spec.md` 的 Purpose 下 superseded 备注＋「历史条款已退役」），Requirements 不留现行 SHALL/MUST |
| `compaction-scheduler` | 活，:11/:33 Compactor | 整能力标 superseded（L1→L2→L3 压实已退役，现行是三龄段折叠） |
| `production-wiring` | 活，23 处 legacy | 逐条：Compactor/LifecycleManager/TombstoneSet 条款退役，`resolveMemoryStore` 现行接线条款转正 |
| `production-wiring-fix` | 活，17 处 legacy | 同上；与 `production-wiring` 重复的 wiring 条款收口到单一 owner，不双重立法 |
| `storage-lifecycle-completion` | 活，6 处 legacy | 逐条退役 Compactor 条款；**lifecycle/retention/guard 脚手架接线归 06 域**，本域只退役已死条款，MUST NOT 抢写 06 的存储 lifecycle 接线契约（**勘误 2026-11-16**：原写「06 的退位接线契约」——退位已删，该接线契约无对象；脚手架的留/删判定归 08 域） |
| `event-segment-store` | 活，7 处 legacy | :227/:233 Close 条款去 Compactor/LifecycleManager/TombstoneSet 分句（`FileSegmentStore.Close` 现行仍在）；:269 `memory/localfile/file` 路径改写为现行 journal 档位；:293 mergeEvents 条款退役 |
| `architecture-review-standards` | 活，:60/:102/:153 | 审查项对象改写为现行权威（RelationStore/因果链、现行并发面、现行可选依赖），去 TombstoneSet/LifecycleManager |
| `compress-event-enrichment`／`compression-user-message`／`event-compaction`／`llm-event-valuation`／`summary-memory-rag`／`view-transform-stateless`／`framework-flow-adapter` | 已 superseded（各自 :5 备注） | **不重写**：只核 `check_concept_budget.py` 的 superseded 规则绿（Requirements 无现行 SHALL/MUST）；备注中的现行断言若被推翻则附勘误 |
| `deterministic-compress-level`／`compress-quality-fix`／`recall-protocol` | 活，legacy 符号 0 | 已在归档波转正（`LevelOfAge`/`BandOfLevel` 线性三带、`Append` 幂等）；本域只核与代码同向，`recall-protocol:13/:90`「被骨架压缩丢弃」措辞核对为「折叠出时间线、原文仍在链」 |
| `memory-curation`（审计新发现） | 活，:24/:33/:38 SHALL `SummaryModel`/`card_max_chars`/`Compact` | ⚠️ 三符号 Go 树 0 命中（`summaryRef` 仍 3 命中）→ 同类 legacy，列入 5.1 逐条处置 |
| `config-hot-reload`／`request-budget-accounting` | 活，legacy 符号 0 | **本域不触**：前者工作树已被 config-surface-normalization 修改（`git status` M），后者由其 01/03 delta 在归档时同步 → 触碰集冲突，见「边界与依赖」 |

## Capabilities

- `code-documentation`（ADDED）：主 spec 与代码同向、legacy 条款退役、golden prose 同步、死生产者清零、兼容读法退役须先审计消费者——五条契约落 [specs/code-documentation/spec.md](specs/code-documentation/spec.md)。
- 多主 spec MODIFIED／REMOVED／superseded：上表列名的活条款面。**本阶段只写本域 delta，不触 `openspec/specs/*`**；主 spec 实际同步在实施/归档阶段由 `openspec archive`（不 `--skip-specs`）执行。

## 边界与依赖

- **波次**：W2 并行（一级 D3/D4）。触碰集 `openspec/specs/*` + `golden/runtime/{durable_truth,architecture}.json` + `memory/replay/{fold,projection,skeleton_assert}.go` + 死生产者测试锚，与 boot 路径簇（01+02+04）不相交。
- **前置**：W1 绿；**spec 转正待 01/02/03 代码稳定**（一级 D4「spec 草案先写，转正在代码绿后」）。01/02/03 若改动 checkpoint 键形、切片名或恢复入口，5.1/5.2 的对应条款须随其终态改写，不得先写死后返工。
- **与 config-surface-normalization 的触碰集冲突（✅ 实测 `git status`）**：该未提交变更已改 `examples/wechat-bot/smoke_test.go`、`tests/integration/acceptance/scenario_age_fixtures_test.go`、`tests/integration/history/event_integrity_test.go`、`tests/integration/host/age_fold_test.go`、`openspec/specs/config-hot-reload/spec.md`。前四个正是 5.3 的测试锚所在文件 → **5.3 对这些文件的改动须待 config-surface-normalization 落地后追加，MUST NOT 覆盖或回滚其未提交改动**（`openspec/AGENTS.md` 与全局纪律）。`config-hot-reload`/`request-budget-accounting` 两主 spec 本域不写。
- **与 06 域的边界**：`storage-lifecycle-completion` 的脚手架接线契约归 06；本域只退役已死条款。`MigrateShardedRoot` 归 06 借用，本域不动。
- **禁止事项**：
  - 不改写冻结归档（`openspec/changes/archive/**` 只附勘误）；旧证据清单 `06-compression-legacy-authority.md` 是归档件，其过时计数只在本域文档说明，不回写。
  - 不触碰外来未跟踪文件（`runtime-architecture-convergence/`、`scripts/gates/check_event_semantics_symptoms.py`、`runtime_budgets.py`、`test_check_runtime_symptoms.py`、`skills/interview/agent-interview-helper/`）。
  - 不触 config-surface-normalization 未提交变更；不读 `examples/wechat-bot/.wechat-config/data`。
  - 不触 boot 路径簇（`assembly/run.go` 的 `NewDurableCommit`/`restoreCheckpointState`/`seedAnchoredReplay`、`memory/ledger/journal.go` open、`segment_store.go` Load）——属 01/02/04。
  - **SHALL NOT 删除/覆盖/改写已提交事实**（一级 P1 红线）；本域删的是无生产调用方的代码与已退役条款，不删事实、不删现行覆盖。
  - 本文档阶段**只规划不实施**。

## 本轮重整：R1–R3（2026-11-15 追加）

> 原 5.1–5.4、5.7 的处置面与证据原文保留（历史时点结果不改写）。本节补三块此前未规划的工作，并把旧勾选中已不能复跑的项撤回待复验（见 [tasks.md](tasks.md) 逐行注）。

### R1 全原子域与完成账双向对账

`golden/runtime/dod_predicates.json` 把 P0–P8 各绑一条测量、一个锚、一个变异与一个 CI 作业；`scripts/gates/check_dod_coverage.py` 只核作业名存在（`ci_jobs` 集合成员）与登记表里的判词。今日实测：

```sh
python3 scripts/gates/check_dod_coverage.py   # exit 1，19 gap
```

- P0–P3、P5–P8 的变异「外并未登记／无运行记录其被判杀」；P4 的变异记录为 `not_killed`（候选自述 `2.53x the second boot, inside its 8x noise bound`，而锚的 journal-bytes 断言先响）；P5、P6 根本无锚测试。
- 因此 R1 的工作不是「把未达要求静默退役」，而是**双向对账**：design D1 的每条 P 与每条子要求 ↔ 承载叶 ↔ 锚测试全名 ↔ 实际运行该锚的 CI 作业 ↔ 当前 x 状态。真冲突按事实修（改绑定、补锚、补变异），不得改判据、不得删要求换取通过。

### R2 计划余项逐条处置

原计划仍有未处置面：死读路径（`ReplayProjection.Fold`／`RebuildProjectionFromWAL` 家族与 12 个测试锚）、未接线脚手架（`memory/ledger/{lifecycle,retention,guard}.go` 生产 0 消费）、历史词汇与 golden prose、六端口兼容读法审计、`compression-recovery-ticket` 退役受阻、legacy 条款退役（production-wiring 系列）、派生文档残留。R2 把这些**逐条**给出现状证据、处置、写集合、前置与残留零证，覆盖原计划全部未勾行；受外部占用（config-surface 未提交文件）的行标 blocked 而非「完成」。

### R3 交接面与归档前验证

- **no-prefix 交接**：票据前缀的呈现单一拥有者与裸前缀匹配器（如 `smokeSettleNoticeOf`）归属须一次说清并落到唯一 owner；`9547faca`「票据注入测试断言归正（spec 正确，测试冻结了无前缀裸正文）」正是这条交接的现场，不得两头都不认。
- **config-surface 耦合**：两笔合提的验收不得混用两个时点的测试结论；4 个被占用的测试文件只在终态上追加。
- **源码与主 spec 同步／归档前验证**：`openspec archive` 不 `--skip-specs`、不 `--no-validate`；归档前必须消费完成判据证据（`openspec archive -y` 只跳过确认，**不检查 DoD**），并复跑主 spec 门与结构门。

### 边界（本轮）

- 本轮只写 10 的四件套规划文档；不改主 spec、golden、代码、测试、CI 配置、分支保护、用户规则与 skill；不提交、不推送、不归档。
- 外部权限类动作（触发 CI、设必需检查、改工具权限）须明示授权；未配置时如实报告「未生效」，不声称已拦截。
- **禁读范围不扩大**：用户真实 `examples/wechat-bot/.wechat-config/data` 与 `--source` 禁读；该限制不禁止在独立临时根跑 wechat-bot 合成验收（远端 smoke 的锚迁移与核验须照跑）。
# 06/6.5 压缩 legacy 并行权威对照清单

> 范围：压缩域（06）在 `memory/replay`、`assembly`、`api/event` 与主 specs 中的新旧权威对照。
> 基线：`dfdfa72e70d2cdd91ce8b1db408d5543481dc499` + 6.2 已入树改动（`memory/replay/age_plan.go`、`age_segments.go`、`fold_capacity_rules_test.go`）。
> 纪律：本清单只读主 specs 与代码；**未改任何主 spec 文本**（归档阶段由编排者按本清单改写）。
> 写集：`memory/replay/**`、新测试文件、本清单。

## 一、唯一现行压缩权威（结论）

| 职责 | 唯一现行权威 | 代码锚 | 公共行为锚 |
|---|---|---|---|
| 判定（producer） | `AgeFoldPlugin.ObserveCommit` | `assembly/age_fold.go:136` | `tests/integration/host/age_fold_test.go:71`（TestAgeFoldFiresFromCommits） |
| 容量门 + 候选扫描 | `PlanByAge` / `planFold` | `memory/replay/age_plan.go:58`、`:76` | `memory/replay/fold_capacity_rules_test.go:108`；`memory/replay/fold_early_exit_test.go:92`（TestFoldCapacityEarlyExitDecision） |
| 执行（consumer） | `CompressionConsumer.Execute` → `FoldByAge` | `assembly/compress_consumer.go:169`、`memory/replay/age_segments.go:233` | `tests/integration/host/compress_pipeline_test.go:240`（TestCompressPipelineJudgesThenExecutes） |
| 事实因果 | `compress.request` → `compress.product` | `api/event/compress_request.go`、`api/event/compress_product.go`；主 spec `compression-pipeline` | `tests/integration/host/compress_request_causality_test.go:49`；`compress_pipeline_test.go:195/331` |
| 恢复不重折 | `Run.seedReplay` → `ReplayProjection.RestoreProducts` | `assembly/run.go:1350`、`:1401`；`memory/replay/age_plan.go:183` | `tests/integration/history/event_integrity_test.go:1829`（TestEventIntegrityProjection/rebuild_does_not_refold、restart_restores_fold_form） |
| 请求预算 | `AssembleBudgetPrefix` | `memory/replay/replay_budget.go:146` | `tests/integration/host/history_restore_budget_test.go:68` |
| 文档权威 | `docs/wiki/agent/event-flow.md#unified-compression` | — | `docs/wiki/memory/memory-architecture.md#event-shape` |

现行压缩只有一条链：**判定落 request → consumer 执行落 product → 重启按 product 恢复**。三龄段折叠的现行实现是 `memory/replay`（`LevelOfAge`/`BandOfLevel`/`SegmentsOf`/`PlanByAge`/`ApplyProducts`/`FoldByAge`/`RestoreProducts`），不是 `agent/compress`。

## 二、旧机制在代码中的退场证据（0 定义）

命令（仓根，写集外只读）：

```sh
for s in ContextCompressor buildRetainedRefs SmartCompressor RetainedRefs deterministicLevel \
         compressSkeleton curateCards Compactor TaskSegmenter Preprocessor LLMSummarizer \
         SummaryGenerator EventValuator GenerateEventSummary BuildEventReference \
         resolveReferenceToMessage synthesizeRollingNarrative condenseCardLines; do
  printf '%s ' "$s"; grep -rn "\b$s\b" --include='*.go' . | wc -l
done
```

结果：18 个符号全部 `0` 命中（含产品与测试）。另两条旧生产者链路也无产品调用方：

```sh
grep -rn 'RebuildProjectionFromWAL' --include='*.go' . | grep -v _test   # 只有 skeleton_assert 编译期断言
grep -rn '\.Fold(ctx' --include='*.go' . | grep -v _test                               # 0 命中
```

即：`ReplayProjection.Fold`（compaction 生产者）与 `RebuildProjectionFromWAL`（compaction 载荷恢复）当前均无产品调用方；现行重启恢复是 `RestoreProducts` 读 `compress.product`（`assembly/run.go:1401`）。它们只保留为旧链的只读兼容与测试面，不构成第二现行管线。

## 三、主 specs 逐条对照（并行权威清单）

### 3.1 task-skeleton-compression（22 条 Requirement，378 行）

| 行 | 旧条款 | 处置 | 现行唯一 owner / 替代 | 现行锚 |
|---|---|---|---|---|
| 7 | 以 agent_output 为段边界切分任务回合 | 保留概念，文本改写 | `SegmentsOf`（`memory/replay/age_segments.go:148`） | `memory/replay/age_segments_test.go:127` |
| 31 | 段内事件按类型二分骨架/中间 | 保留概念，文本改写 | `isProcessDetail`（`age_segments.go:318`） | `age_segments_test.go:189` |
| 45 | 中间事件按 tool > assistant 优先级丢弃 | 撤销（现行无优先级，过程细节一并折叠） | 三龄段折叠 | `age_segments_test.go:189`（不再区分 L1/L2 丢弃次序） |
| 63 | 骨架仍超预算时 buildRetainedRefs 多段归档 | 撤销 | request/product 折叠（无 L3 归档、无 LLM 综述） | `compress_pipeline_test.go:240` |
| 92 | 进行中段永不归档 | 保留 | `foldProcessDetail` 只折 complete 段 | `age_segments_test.go:351` |
| 102 | 保留消息携带 event key 前缀 | 撤销（投影按条目身份，无前缀协议） | `ProjectionEntry.Key` | `replay_budget_test.go:330` 等 |
| 113 | LLM 文摘可选叠加层（含 compaction 事件） | 撤销 | 现行折叠零 LLM；恢复走 product 事实 | `age_plan.go:183`；`event_integrity_test.go:1829` |
| 158 | 滚动摘要消息豁免 L3 | 撤销 | 无滚动摘要消息 | `age_segments_test.go:258`（history summary 条目） |
| 175 | 段定级指数边界且封顶 L2 | **文本与现行实现冲突**：现行 `LevelOfAge` 为线性 `{k,2k,3k}` 且 L3 为最老带 | `LevelOfAge`/`BandOfLevel` | `age_segments_test.go:82`（TestAgeLevelReadsTheSharedRule） |
| 194 | 压缩配置参数公式化默认值 | 部分保留：`max_tokens`/`compress_threshold`/`keep_recent_tasks` 仍是现行窗口字段；`card_max_chars`/`compact_keys_listed` 随卡片机制撤销 | `FoldWindow.VolumeThresholdBytes`（`assembly/age_fold.go:41`）、`AgeFoldPlan` | `tests/integration/host/reload_tiers_test.go:27` |
| 210 | 纯工具调用 thinking_plan 摘要 | 撤销（0 定义） | — | — |
| 220 | 老化工具运行折叠为 tool_chain | 撤销（0 定义） | — | — |
| 243 | 活跃前沿与近期配对不折叠 | 保留概念，文本改写 | 进行中段/最新 window 不折 | `age_segments_test.go:351` |
| 254 | 上下文维护五项不变量 | 部分保留（有界/稠密/锚定/无损/原生前沿由投影+预算承载）；工具链措辞撤销 | `memory/replay` + `AssembleBudgetPrefix` | `replay_budget_test.go:24` |
| 265 | 压缩触发器单维化（ContextCompressor/SmartCompressor） | 由 06 delta 替代；文本改写 | `PlanByAge` volume 门 + `FoldByAge` 候选门 | `fold_capacity_rules_test.go:108`；`fold_early_exit_test.go:92` |
| 287 | 整理间渲染冻结（buildRetainedRefs） | 撤销 | 投影 append 幂等 + 装配现算 | `replay_budget_test.go:202` |
| 305 | 浓缩卡片票据机器校验 | 撤销（0 定义） | — | — |
| 321 | 卡片预算与失去导航显式化 | 撤销（0 定义） | — | — |
| 333 | task settled 类外部输入票据化折叠 | 撤销压缩条款；结算通知的现行归属见 02 域 | `quiet-receipts`/通知语义 | `tests/integration/host/quiet_receipts_test.go` |
| 347 | 批量退役源头汇总单事件 | 非压缩职责；现行归属 runtime/task | `runtime/task` | `tests/integration/execution/delegation_completion_test.go`（通知/结算面） |
| 356 | 浓缩导航丢失可观测 | 撤销（0 定义） | — | — |
| 370 | L3 预算升级未消费遥测豁免 | 撤销（无 L3 预算升级） | — | — |

### 3.2 其它压缩专属 spec

| spec:行 | 旧条款要点 | 处置 | 现行 owner / 替代 |
|---|---|---|---|
| event-compaction:11 | TaskSegmenter 统一分段 | 撤销（0 定义）；分段概念由 `SegmentsOf` 承载 | `age_segments.go:148`；`age_segments_test.go:127` |
| event-compaction:37 | Compactor 在 BeforeModel 回调 | 撤销（0 定义） | — |
| event-compaction:53 | SmartCompressor 在 BeforeModel 回调 | 撤销（0 定义） | — |
| view-transform-stateless:11 | SmartCompressor 无段时返回原消息 | 撤销（0 定义） | 现行等价：容量内早退，见 `age_plan.go:58`；`fold_capacity_rules_test.go:108` |
| view-transform-stateless:22 | 删除 protectPendingAsyncSegments | 撤销（函数与机制均 0 定义） | — |
| summary-memory-rag:9/26/44/60 | 压缩摘要写 MemoryStore/引用消息/召回/去重 | 撤销（0 定义） | 原文仍在原文仓，票据可 recall：`recall-protocol` 现行条款 |
| compression-user-message:6/24/40 | 压缩后保留 pending user/ensureUserPrompt/摘要 System role | 撤销（0 定义） | — |
| compress-event-enrichment:9/39 | buildCompressEvent 富化卡片 | 撤销（0 定义） | — |
| framework-flow-adapter:51/75/157 | SmartCompressor/Compactor BeforeModel 注册 | 撤销（0 定义） | 现行执行核无 BeforeModel 压缩链，见 `docs/wiki/agent/agent-architecture.md:145` |
| deterministic-compress-level:9 | deterministicLevel 纯函数 0–3 | 保留概念，文本改写：函数名与 L1/L2 丢弃语义已退场；现行 `LevelOfAge` 线性三带 | `LevelOfAge`/`BandOfLevel`；`age_segments_test.go:82` |
| llm-summary-generator:11/26/35 | LLMSummarizer/Batched LLM/配置门 | 撤销（0 定义） | 现行折叠零 LLM |
| llm-event-valuation:11/27/48/64 | EventValuator/LLM 估值/合并调用/类型下限 | 撤销（0 定义） | — |
| compress-quality-fix:12/28/38/57 | resolveReferenceToMessage/BuildEventReference/Append 幂等/日志 | 前三项 0 定义撤销；`Append` 幂等仍现行 | `ReplayProjection.Append`（`replay_budget.go:105`）；`replay_budget_test.go:24` |

### 3.3 跨域主 spec 中的压缩条款

| spec:行 | 旧条款要点 | 处置 | 现行 owner |
|---|---|---|---|
| event-sourced-projection:45 | 折叠向链追加 compaction 事件（buildRetainedRefs/tool_chain） | **撤销生产者条款**（`Fold` 无产品调用方）；恢复兼容只保留读路径 | 现行 product 事实见 `compression-pipeline` |
| event-sourced-projection:62 | compaction 事件滚动 supersede | 撤销（0 产品调用方） | — |
| event-sourced-projection:132 | compaction 事件可召回并载综述正文 | 撤销生产者；历史事实读取兼容 | `memory/replay/projection.go:321` 只读恢复 |
| event-sourced-projection:140 | 移除游离快照补丁 | 保留（非压缩） | `memory/replay/projection.go` |
| event-sourced-projection:195 | 折叠幂等与不回流（tool_chain） | 文本改写为 product 幂等 | `age_plan.go:118`（ApplyProducts 幂等）；`age_plan_test.go:15` |
| recall-protocol:104 | buildRetainedRefs 统计丢弃步数 | 撤销压缩侧条款（0 定义） | 现行 recall 条款见该文件其它 Requirement |
| config-hot-reload:40 | ContextCompressor/SmartCompressor 同代快照 | 保留“同代”约束，对象改写为现行三字段窗口 | `FoldWindow`（`assembly/age_fold.go:25`）；`reload_tiers_test.go:27` |
| request-budget-accounting:24 | 历史压缩只由 ContextCompressor 执行 | 改写为唯一现行压缩链 | `age_fold.go:136`+`compress_consumer.go:169`；`compress_pipeline_test.go:240` |
| architecture-review-standards:23/37/79/130 | 审查项引用 SmartCompressor/Compactor/context_compress | 审查项改写为 request/product/三龄段折叠 | 同 3.1 现行锚 |

**计数**：上述 16 个主 spec 共 628 条 `SHALL/MUST`，其中含旧符号的条款 181 处（`ContextCompressor|buildRetainedRefs|SmartCompressor|RetainedRefs|compressSkeleton|curateCards|deterministicLevel|tool_chain|Compactor|TaskSegmenter|Preprocessor|LLMSummarizer|SummaryGenerator|EventValuator`）。**不得整文件删**：逐条按上表迁移/撤销。

## 四、并行 change spec（event-driven-consumption/03-turn-segment）

`openspec/changes/event-driven-consumption/changes/03-turn-segment/specs/task-skeleton-compression/spec.md` 是同一 capability 的第二份 delta：它声明“以 agent_output 为段边界”被“按实际消费关系闭合”取代。处置：

- 该 change 已被编排者暂停（ORCHESTRATION §2.2），其 spec 文本由编排者随 04 域退场；本单不写。
- 现行代码仍是 agent_output 边界（`SegmentsOf`，`memory/replay/age_segments.go:148`），测试锚是 `memory/replay/age_segments_test.go:127`（TestSegmentsCloseByConsumptionRelation/legacy_output_without_input_events）。**该测试锚是现行行为的锚，不是并行 spec 的锚**；删除它会移走现行覆盖，故本单未删。

## 五、测试锚处置（“删除并行 spec 引用的测试锚”核查）

### 5.1 结论

- 18 个旧符号在 `*_test.go` 中 **0 引用**（§二命令），因此“旧 ContextCompressor/buildRetainedRefs 的测试锚”在本树不存在，无可删除对象。
- 现存两类候选锚需要写集/owner 裁定：候选 A 在 `tests/integration/history`，候选 B 在既有 `memory/replay` 与 `tests/integration/host`/`history` 文件；两者都不在本单写集。

### 5.2 候选 A：forced 特例断言（必须改，非删文件）

`tests/integration/history/event_integrity_test.go` 的 `TestEventIntegrityProjection/fold_product_no_self_trigger`：

- `:2332-2334` 与 `:2356-2358` 断言 `VolumeThreshold:1` 的第二次/合成折叠仍 `Triggered=true`（forced 特例）。6.2 已把 `Triggered` 改为“确有产物”，6.3 任务要求删除 forced 特例，故该断言已与现行语义冲突。
- A/B 原始退出码：纯 HEAD 副本 `go test ./tests/integration/history -run '^TestEventIntegrityProjection/fold_product_no_self_trigger$' -count=1 -v` → **exit 0**；HEAD+6.2 副本同命令 → **exit 1**（`second fold report = {Volume:1969 Threshold:1 Triggered:false ...} want the forced fold to run`）。
- 最小改法：second 期望 `Triggered=false`；guarded synthetic fold 期望 `Triggered=false`、`NewFolded=0`、`Products=0`；保留“不二次折叠/不吞合成产物/键不变”断言。子场景名属 golden contract（02.3），不得改名。
- 该文件不在本单写集；已上报编排者裁定 owner。

### 5.3 候选 B：dead producer 的测试锚（可删/改，需授权）

`ReplayProjection.Fold` 与 `RebuildProjectionFromWAL` 无产品调用方，其锚为：

- `memory/replay/fold_test.go:20`（TestFoldIsIdempotentOverTheSameMaterial）
- `memory/replay/replay_budget_test.go:330`（TestFoldDropsTheMaterialOfTheEntriesItFolded）
- `tests/integration/host/request_boundary_test.go:341`
- `tests/integration/history/projection_replay_and_budget_test.go:147`

删除它们即删除 compaction 生产者/载荷恢复的唯一覆盖；是否随 04/4.4 删 `Fold`/`RebuildProjectionFromWAL` 一并处理，应由编排者裁定。本单写集不含这些既有文件，未动。

### 5.4 必须保留的现行锚

`TestFoldCapacityAndCandidateRules`、`TestFoldCapacityEarlyExitDecision`、`TestAgeFoldWaitsForTheVolumeThreshold`、`TestAgeFoldFoldsTheNewBandIntoOneProduct`、`TestAgeFoldFoldsTheOlderBandsIntoOneHistorySummary`、`TestAgeFoldKeepsTheSegmentInProgressWhole`、`TestCompressRequestIsFiledWithoutExecution`、`TestCompressPipelineJudgesThenExecutes`、`TestCompressProductRestoresAfterRestart`、`TestCompressionRequestCommitFailureKeepsFactChainConsistent`、`TestAgeFoldFiresFromCommits`、`TestUnderThresholdUntouched`、`TestRebuildRefoldsIdempotent`、`TestEventIntegrityProjection`（除候选 A 的两处断言）。

## 六、验证命令与原始退出码（本单实测）

| 命令 | 原始退出码 | 结果 |
|---|---|---|
| `openspec validate --specs --strict --no-interactive` | 0 | 141 passed / 0 failed |
| `openspec validate runtime-architecture-convergence --strict` | 0 | Change is valid |
| `python3 scripts/gates/test_check_runtime_workflow.py` | 0 | 50 tests OK（矩阵仍精确 [1,7,42]×[256,32768]） |
| §二 grep 18 符号 | 0（每条计数 0） | 无现行定义/测试引用 |
| `go test ./memory/replay -count=1` | 0 | ok |
| `go test ./memory/replay -count=1 -race` | 0 | ok |
| `go test ./memory/replay -run '^TestFoldDepthMeasurementExists$' -count=1` | 0 | PASS |
| `go test ./memory/replay -run '^TestFoldCapacityAndCandidateRules$' -count=1 -race -v` | 0 | 5 子锚 PASS |
| `go test ./memory/replay -run '^TestFoldCapacityEarlyExitDecision$' -count=1 -race -v` | 0 | 2 子锚 PASS |
| `go test ./assembly -short -count=1` | 0 | ok |
| host 压缩/龄段 8 锚（见 §5.4） | 0 | 全 PASS |
| `go test ./tests/integration/history -run '^(TestAgeBandsReachTheRequest|TestAgeFoldUnderThresholdLeavesTheRequestAsItStands)$'` | 0 | PASS |
| 候选 A 锚（HEAD+6.2） | **1** | 预存 forced 断言冲突，见 §5.2 |

**6.5 负锚现状**：`旧 ContextCompressor/buildRetainedRefs 不再作为现行 SHALL` 当前 **不成立**（文本仍在 16 个主 spec 中）。主 spec 结构 strict 校验不检查词面，概念门 D1 只拦 superseded spec；因此该负锚只能在归档改写后转绿。本单按纪律不提前改主 specs。

## 七、6.3 / 6.4 证据索引

### 6.3 容量内早退与扫描面

- 红：`go test ./memory/replay -run '^TestFoldCapacityEarlyExitDecision$' -count=1 -v` → **exit 1**，`PlanByAge inside capacity allocated 61.0 objects per run`。
- 绿：同命令 → **exit 0**；`-race` → 0；6.2 全表 `-race` → 0。
- 实现：`ReplayProjection` 维护 rendered volume（`Append` 累加；`ApplyProducts`/`RestoreProducts`/`Fold`/`FoldByAge` 重算），`PlanByAge`/`FoldByAge` 容量内 O(1) 早退，不复制条目、不 `RenderProjection`；超阈仍扫 `SegmentsOf`。
- A/B：`go test -C examples/wechat-bot -tags smoke . -run '^$' -bench '^BenchmarkFoldDepthCost$' -benchtime=300x -benchmem -count=3`
  - window-32768 基线 16.65/20.62/18.25 ms/op、7.92MB/op、11522/11526/11584 allocs/op；候选 14.97/21.21/26.30 ms/op、7.43-7.51MB/op、10974/10988/11042 allocs/op（allocs −4.8%，B/op −5.7%）。
  - window-256 基线 30.09/31.86/38.97 ms/op、14.4k allocs/op；候选 22.96/25.47/34.80 ms/op、14.4k allocs/op（每轮折叠，早退不适用）。
  - ns/op 噪声大（并行代理占机），不作墙钟断言；以 allocs/op 与 B/op 的稳定下降判定保留扫描优化。

### 6.4 4096 永久 shard 暂缓

- 决策：不向长测矩阵加入 4096 永久 shard；矩阵保持 `seed[1,7,42] × window[256,32768]`（`python3 scripts/gates/test_check_runtime_workflow.py` → exit 0）。
- 独立测量保留：`examples/wechat-bot/smoke_bench_test.go` 的 `BenchmarkFoldDepthCost`（`//go:build smoke`，windows 恰为 `{256, 32768}`，`-benchtime=300x` 可复跑）。
- 恢复证据保留：`TestCompressProductRestoresAfterRestart`（`tests/integration/host/compress_pipeline_test.go:331`）、`TestRebuildRefoldsIdempotent`（`tests/integration/host/age_fold_test.go:165`）。
- 红锚（测量存在性）：`memory/replay/fold_measurement_test.go` 的 `TestFoldDepthMeasurementExists`。该叶为暂缓叶，按 ORCHESTRATION“纯文档/暂缓叶不伪造红绿”以变异证红：在 `/tmp` 副本删 `BenchmarkFoldDepthCost` 名 → exit 1；恢复 → exit 0；工作树同锚 → exit 0。
- 重新开启条件：出现只属于 4096 窗口的恢复症状，且独立 spec delta、测量与校准齐备；否则不扩容。

## 八、遗留与交接

1. **主 spec 改写**（16 个 spec，628 条 SHALL/MUST）：按 §三逐条迁移/撤销；归档阶段执行，本清单为输入。`task-skeleton-compression:175`（指数/封顶 L2 vs 现行线性 L0–L3）与 `:265`（触发器）是两处必须重写的语义冲突。
2. **候选 A owner**：`TestEventIntegrityProjection/fold_product_no_self_trigger` 的 forced 断言（§5.2）当前使事件完整性合同锚为红；需编排者授权最小改断言（写集外，本单未动）。
3. **候选 B owner**：dead producer（`Fold`/`RebuildProjectionFromWAL`）的测试锚是否随 04/4.4 删除（§5.3），需裁定；本单未动。
4. **6.5 负锚**在归档改写前保持红（文本仍在），不伪称已绿。
5. **6.4 重开条件**见 §7.2；不删除 `BenchmarkFoldDepthCost`。

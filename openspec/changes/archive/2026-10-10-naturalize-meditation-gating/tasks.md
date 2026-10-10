# Tasks: naturalize-meditation-gating

- [x] 1.1 引擎重构：meditation.go 删 lastTurnEnd/UpdateLastTurnEnd/startedAt，新 pending 状态机 + NoteMeditationBatchOutcome(consumed) + 节奏门（lastMeditation 锚/首次直通）+ 水位执行语义 + 3×interval pending 防御 —— 验证：`go test ./agent -run '^TestMeditation' -count=1` 新用例绿。
- [x] 1.2 event_loop 接线改造：UpdateLastTurnEnd 回调点替换为冥想批结果通知（消费/让位两态）；dropMeditationFromMixedBatch 通知 deferred；纯冥想批消费通知 consumed —— 验证：同上 + `grep -c UpdateLastTurnEnd` =0。
- [x] 1.3 锚持久化：persistAnchors/LoadAnchors 收敛为单锚 lastMeditation（旧 LastTurnEnd 键忽略兼容用例钉住）—— 验证：`go test ./agent -run 'Anchor' -count=1`。
- [x] 2.1 测试矩阵重写：D4 七场景全展开（含冷启动两用例并入、让位后连续再来、pending 超时）；旧 TestMeditationGate_* 28 例清账（删/改清单入 tasks 注记）—— 验证：`go test ./agent -run '^TestMeditation' -count=1 -v` 全 PASS 且 `-count=5` 稳定。
- [x] 2.2 两形态 e2e：外部（远端 18h 剧本翻转）+ 自察（用户高频让位→走后即思覆盖全程）—— 验证：`go test ./tests -run '^TestExternalMeditation|MeditationE2E' -count=1`。
- [x] 3.1 文档：wiki §2.14 门控段重写（五不变量+mermaid 状态机）、README 界定句、observability 三态说明 —— 验证：lint=0、旧语义措辞（"空闲门=任意回合"）grep 清零。
- [x] F1 四查+非触碰：novelty 判据/观察面授权/注入动作 diff=0；远端三场景验收剧本对账。
- [x] F2 全门禁：short/race(agent 域 ×10)/lint(Go1.24)/check-openspec/bot 模块。
- [x] F3 真实模型复跑：TestRealModel_ExternalMeditation（门控行为实跑可见面，预算 3 calls 入账）。
- [x] F4 归档 + 部署知会（min_gap 语义变化写入发布说明与远端通知）。

## 注记 A：D4 七场景 × 用例名（agent 域 48 例中的门控部分）

| 场景 | 钉住它的用例 |
|---|---|
| 外部形态冷启动（远端 18h 案翻转） | `TestMeditationGate_ColdStartZeroWatermarkPassesThrough`（矩阵内，18h 存量事实 + min_gap=1h）、`TestMeditationGate_ColdStartFirstTickFires`（ticker 驱动）、`TestExternalMeditation_ColdStartFlipsBacklog`（tests 域 e2e） |
| 自察 + 用户高频 | `TestMeditationE2E_SelfObservingYieldCoversWindow`（tests 域：deferred→走后执行整窗覆盖） |
| 家务流常驻（自察） | `TestMeditationGate_HouseworkStreamLeavesGatesTransparent`、`TestOnEventLoop_NotifiesMeditationBatchOutcome/用户批不动任何账` |
| 冥想 turn 进行中 tick（pending 防重入） | `TestMeditationGate_PendingReentryGuardBlocksSecondFire`、`TestMeditationBatchOutcome_ConsumedAdvancesToInjectionMoment` |
| 让位后用户又来（连续让位，窗口无损） | `TestMeditationGate_ContinuousYieldKeepsWindow`、`TestMeditationBatchOutcome_DeferredIsPostponement` |
| 重启（锚恢复/锚缺失） | `TestMeditationGate_RestoredAnchorResumesRhythm`、`TestMeditationGate_MissingAnchorPassesThroughOnce`、`TestMeditationManager_AnchorStoreRestore/Persist`、`TestMeditationGate_RhythmGateMeasuresExecutions` |
| pending 卡死防御（3×interval + WARN） | `TestMeditationGate_PendingStaleResetsAndRefires`、`TestMeditationE2E_PendingStaleReinjects`（tests 域） |
| 节奏门（执行间下限） | `TestMeditationGate_RhythmGateMeasuresExecutions`、`TestExternalMeditation_MinGapResumesAfterFloor`（实测间隔 ≥ min_gap） |

## 注记 B：旧 28 例清账清单（基线 5334c10）

旧 `TestMeditationGate_ObservedSurfaceMatrix`（3 形态 × 8 子例 = 24 例）逐条处置：

- 「门齐则触发并推进水位」→ **改**为「触发只注入，消费才推进水位并自锁」：fire 不再推进水位（D1）。
- 「空闲不足只推迟不否决」→ **改**为「节奏不足只推迟不否决」：测量对象从"距上一回合"换成"距上一次执行"（D2）。
- 「从未有过回合结束则不开」→ **改**为「零水位直通：从未执行过也开门」：旧 idle 锚条款 REMOVED，冷启动结论反转（D2）。
- 「观察面上无非自管事件则不开」「未接事实链则门关且不猜」「读取失败则门关」「观察面外的事件不计入」「查询只带本组声明的分区」→ **保留**，断言逐字不变。

其余旧用例：

- `TestMeditationGate_InjectsUnderMeditationSource` → **保留**（注入血统=meditation 仍开门）。
- `TestMeditationDefault_ObservesOwnPartition` → **保留**（观察面解析到自身分区，D6）。
- `TestMeditationManager_WatermarkAdvancesOnFire` → **改名+改义** `TestMeditationManager_WatermarkAdvancesOnConsumed`。
- `TestMeditationManager_ScanConcurrentWithAnchorUpdates` → **改名** `TestMeditationManager_ScanConcurrentWithOutcomeNotes`（并发面是消费通知而非回合锚）。
- `TestMeditationManager_AnchorStoreRestore/Persist` → **保留**，`UpdateLastTurnEnd` 播锚改为 `NoteMeditationBatchOutcome`；新增 `TestMeditationManager_NoAnchorStoreInMemory`。
- 旧 `TestMeditationManager_UpdateAnchors`（双锚写入面）→ **随符号删除清账**，写入面由 `TestMeditationManager_AnchorStorePersist`（消费推进单锚）承接。旧 `TestOnEventCallback_*` 只测元数据透传、不含冥想接线，逐字保留。
- HEAD 里 20 处 `UpdateLastTurnEnd(...)` 播锚调用（散布于矩阵、`StartStop`、`SelfManagedOutputIsNotNovelty`、`ScanConcurrentWithAnchorUpdates`、`Retention` 前置）→ 全部删除或改为 `NoteMeditationBatchOutcome`；接线面新由 `TestOnEventLoop_NotifiesMeditationBatchOutcome` 三子例（纯批 consumed／混合批 deferred／家务批不通知）钉住——基线里 `event_loop.go:301` 的那个回调点此前没有任何接线用例。
- `agent/meditation_gate_coldstart_test.go`（98efb8f 引入的 2 例）→ 语义并入"冷启动直通"场景：`TestMeditationColdStart_FirstFireWithoutAnyTurn` 由 `TestMeditationGate_ColdStartFirstTickFires` 承接；`TestMeditationColdStart_StillWaitsMinGap`（旧结论"冷启动=刚活跃，首投仍等 min_gap"）随 D2 **反转**，由 `TestMeditationGate_ColdStartZeroWatermarkPassesThrough` + `TestMeditationGate_RestoredAnchorResumesRhythm`（非零锚才等下限）共同覆盖。

锚文件旧键兼容：`reliability.MeditationAnchors.LastTurnEnd` 字段与 JSON 键**按既有机制保留**（persist 写零、Load 后无人读取），因此 `grep -rn "UpdateLastTurnEnd\|lastTurnEnd" --include="*.go"` = 0 的清账口径不含该导出字段名；兼容面由 `TestMeditationManager_AnchorStorePersist`（`reloaded.LastTurnEnd` 恒零）钉住。

## 注记 C：2.2 落点调整

两形态 e2e 落在 `tests/meditation_e2e_test.go`（4 例：`TestExternalMeditation_ColdStartFlipsBacklog`、`TestExternalMeditation_MinGapResumesAfterFloor`、`TestMeditationE2E_SelfObservingYieldCoversWindow`、`TestMeditationE2E_PendingStaleReinjects`），未新建文件：comment_policy 的 responsibility-fragmentation 棘轮对「同 anchor、无生产镜像的两个测试文件」零容忍，而 `#meditation-curator` 已被 `tests/real_model_external_meditation_test.go` 占用；政策给出的三条出口里，改名（tests 域无生产镜像）与 docs 侧收敛（本 change 禁碰 docs/wiki）均不可用，故走 merge 出口。假注入面 `timedCards` 因此并入既有 `cardCollector`（新增注入时刻记录 `fireAt`/`latestText`）。

> 编排者四查+F 注记（2026-10-10 复跑核销）：勾选抽验全复跑（agent 48 例 ok 1.0s、tests e2e ok 3.36s、bot 面 grep 清零=0、SHORT=0）；F1 novelty 判据与 buildMeditationMessage 函数体 diff=0（唯一 -/+ 为调用点参数改名 idle→sinceExec，恰属门控语义更名）。**F3 真实模型首跑抓出设计缺陷并根治**：D1 原稿的 3×interval stale 时间复位在真实模型长回合（40s≫150ms 阈值）下误判注入丢失→重投踩踏 71 卡一批→模型混乱→证据链断+预算超。修正=长期未决仅 WARN 不复位（fail-safe 停摆优于风暴；通知缺位风险已由 consumed/deferred 路径用例闭合）——design D1/D4、delta ADDED 条款、agent/e2e 用例（PendingLongHoldKeepsSingleton/NeverStorms）、wiki 三态全部同步，修正后真实模型 PASS 58.65s。两波代理战报全部抽验复跑一致；偏离申报（e2e merge 入既有文件避共位门等）核准。

> 红项归属注记（F2 期间发现）：`TestModelOverride_ReentryIsolation` 子例在 `-race -count=20` 下本树与基线 `98efb8f` 复现率一致（38/20 行 ≈ 每轮必现）——既有缺陷非本批引入（本批 diff 与其零交集），CI count=1 从未命中。另行登记台账，不混入本 change。

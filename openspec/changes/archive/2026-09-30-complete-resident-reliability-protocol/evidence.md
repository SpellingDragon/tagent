# 实施证据台账

本文件随实施逐项追加。区分**已复现 / 源码推导待复现 / 当前已正确**；每勾一项须附命令与结果。工件 ready ≠ 代码 ready。

## 1. 基线与失效证据

### 1.1 实际基线快照（记录于实施开始，不凭记忆）

- **HEAD**：`a16fdce90fec4395131d5f1410ff3d23f0d912cb`　**分支**：`dev`
- **工具链**（模块内 `go env`）：go1.24.1 / darwin / arm64 / CGO_ENABLED=1
- **跟踪变更**：29 个源码/测试文件（`git diff --stat` 合计 +2429/−505）。重点规模：`agent/reliability/inbox.go`(+405)、`agent/context_manager.go`(239)、`resources.go`(199)、`wiring.go`(196)、`agent/event_bus.go`(189)、`memory/segment_store.go`(178)、`memory/storage_contract_test.go`(+159)。含本会话先前遗留的部分实现 4.0a/4.0b（`context_manager.go` buildBusFact 身份 stamp + persistBusEvent 走 ReplayEvent），**未验收**。
- **未跟踪源码**（2 个测试文件，计入基线）：
  - `agent/reliability_boundary_test.go`(406 行)：`requestCapturingModel`；用例含 `TestRunEventLoop_DurableBatch_ABStoredCDeferred`、`TestBeforeModel_DoesNotClaimMidTurnEvents`、`TestRunEventLoop_ABOneTurnCNextTurn`、`TestAssembleRequest_AppendsRecoveryNoticeAtTail`/`_FullRecovery_NoNotice`、`TestRecoveryNotice_{DiagnosticsStillReadable,SystemPromptUnchanged,NotInProjection}`。
  - `agent/reliability_matrix_test.go`(155 行)：`TestReliableBus_{FieldRoundTrip_Lossless,UnencodableEventRefused,FixedSlotsNotCompacted}`、`TestPersistBusEvent_SystemRoleNotMutatedInPlace`。
  - `openspec/changes/complete-resident-reliability-protocol/` 与 `fix-resident-reliability-boundaries/`（提案目录，不计入源码数）。
- **基线门命令与结果**（`GOFLAGS=-mod=mod`，`-count=1`）：
  - `go build ./...` → **rc=0**
  - `go vet ./agent/... ./memory/... ./event/... .` → **rc=0**
  - `go test -count=1 ./agent/... ./memory/... ./event/... .` → 全部 **ok**：agent 30.7s、agent/compress、agent/governance、agent/reliability 7.9s、agent/task、memory、memory/embedder、memory/engine、memory/kv、event、root 6.0s。
- **未决测试失败**：本次运行**未复现任何 FAIL**。prior 会话曾记录一次"全量 agent 包一次未知 FAIL、随后 exit=0"，当前三次采样（含本次）不复现，按未证实保留，不据一次成功宣称"历史全绿"、也不据一次失败宣称"存在回归"。

### 1.1 失效证据（据 09-17 远端快照，已取原包）

来源：Agent Mail `att-traj2h.gz`（83 条 / batch 187–269 / 2026-09-17 16:43–18:43，gitignore 于 `examples/wechat-bot/data/trajectories/`）。

- **失效任务空转（已复现于真实轨迹）**：看板逐回合重渲染 `[suspect] 56bf24c3`（`restart-tagent.sh` capfix 换装作业，`已运行 21h15m→23h11m44s`，标 `⚠ 长时间无输出，可能假死，需确认`）。模型逐回合十余次仅作文本裁决、不回收：batch 191/201/206/223/226/244 等，末条 226 明写"deadline 缺陷已立项，不处理"。
- **根因（源码推导，待 §10 复现 fail-before）**：`markStaleDetached`(task_manager.go:1089) 与 `enforceJobDeadline`(:1124) 均 gate `status∈{alive_detached,stale} && detachedAt≠0`；suspect（未 detach）不入任一墙 → 配的 8h deadline 对其不放行。
- **处置面割裂（源码推导）**：入口 agent `tagent.yaml` tools 未装 `cancel_task/list_tasks`；唯一回收原语 `exec op=stop`(action_tool.go:278) 需 `session_id`，看板只给 task 短 ID（task_board.go:53）。
- **结算死重（已复现于远端 24h 回执 + 本地聚合）**：`[task settled]` 合并 external_input 结构性不可回收；压缩回收率 27.8%→1–4.5%、floor→96–99%。

> 待 §1.3/1.4 建立故障注入与真实 runner 反例后，将"源码推导待复现"逐项转为"已复现"，再执行 1.6 基线门。

### 1.2 源文件 → 测试映射（据实际文件枚举，非套模板；旧证据有效范围逐项标注）

| 源码范围 | 现有测试（可复用） | 消费者/终点测试 | 旧证据范围 |
|---|---|---|---|
| `memory/segment_store.go`、`in_memory_store.go`、`error_tracking.go`、`mem_spill.go`（提交/重放/计数） | `storage_contract_test.go`、`segment_store_barrier_test.go`、`mem_spill_test.go`、`mem_spill_notify_test.go`、`cold_partition_test.go`、`lifecycle_test.go` | `memory_engine_wiring_test.go`、`engine_bridge_test.go`、`tests/offline_bench` | 覆盖普通写/去重/屏障，**未**按 input/receipt/read/write/sync 独立注入（待 1.3） |
| `agent/reliability/inbox.go`、`agent/event_bus.go`（接收/准备/固定槽） | `reliability/inbox_test.go`、`reliability/spill_test.go`、`reliability_matrix_test.go`(未跟踪) | `event_bus_spill_test.go`、`inbox_receipt_test.go` | 覆盖全序/固定槽/无损往返，**未**覆盖不确定写与精确大整数邻居（待 §3） |
| `agent/context_manager.go`、`event_loop.go`、`plugin/`（提交门/回显/批次） | `persist_bus_event_test.go`、`reliability_boundary_test.go`(未跟踪)、`projection_rebuild_test.go`、`projection_fallback_test.go` | `on_event_integration_test.go`、`tagent_agent_loop_test.go` | 4.0a/4.0b 仅分类未闭合；A+B/C 有 fixture，**未**在真实框架钉执行门（待 §4） |
| `agent/lifecycle.go`、`resources.go`、`wiring.go`（生命周期/所有权） | `lifecycle_test.go`、`resources_ownership_test.go`、`resources_lock_test.go` | `memory_engine_wiring_test.go`、`hotreload_multiagent_test.go`、`swap_executor_test.go` | 覆盖共享/租约，**未**覆盖停止未确认 poisoned/late attach（待 §6） |
| `completion/恢复/清理` | `inbox_receipt_test.go`、`reliability/fault_injection_test.go` | `tests/upgrade_rollback_drill_test.go` | 有底层原语，**未**接通 completion→固定回执→Ack 全链（待 §5） |
| **§10 `agent/task/*`、`tool/action`、结算事件** | `task_detached_wall_test.go`、`task_liveness_reconcile_test.go`、`task_zombie_reconcile_test.go`、`task_orphan_retire_test.go`、`task_alive_detached_test.go`、`finalize_retired_test.go`、`bind_detector_test.go`、`task_board_test.go`、`task_resume_test.go`、`task_settled_test.go`、`context_manager_recycle_test.go`、`memory/compaction_test.go` | `task_board_wiring_test.go`、`examples/wechat-bot/reincarnation_notice_test.go` | 现测试固化 stale/deadline 观测语义（本次将**删除/取代**），不可作 TTL 通过证据；§10 需新写到期强杀/重入刷新/票据折叠断言 |

> 映射仅供选择复用；每格"旧证据范围"表明不可继承。全绿仅代表当前命令通过，不代表覆盖新契约。

### 1.5 过时代码/兼容接口/旧接线淘汰清单 + 受管清理范围

**本次链路（可靠性 §1–§9）**

| 待淘汰面 | 位置 | 真实调用方 | 替代 |
|---|---|---|---|
| 在线 spill 回补的 `GetEvent` 弱回退 | `memory/mem_spill.go`、包装链 | spill/恢复路径 | 统一重放核心（任务 2.6） |
| volatile/durable 混合旁路、首信封/整轮 user 跳过标志 | `agent/event_loop.go`、`context_manager.go` | 事件循环 | 写入前 typed 身份识别（4.3/4.4） |
| 失败后插件另写合并事实的补偿路径 | `agent/`(plugin 写入) | 提交失败分支 | 删除，全 selected 成功才入模型（4.3） |
| 只凭 request ID/状态字符串确认的旧 `RecordReceipt` | `agent/reliability`、receipt 路径 | Ack | 合法 completion+核验凭据（5.4） |
| 旧 v1 inbox/spill 排空迁移与历史兼容读取 | `agent/reliability/inbox.go` 迁移分支 | 启动 | 只加载当前格式（3.7） |
| `InjectBusInputs` 中途认领（RoleSystem→RoleUser 就地改） | bus 中途消费点 | BeforeModel | 冻结批次准备时归一化（async-tool-event-fix） |
| 仅为旧快照/旧元数据存在的投影排除分支 | 投影非投影判定 | 三路径 | event 包统一判定（5.5） |

**§10 TTL/结算新增淘汰面**

| 待淘汰面 | 位置 | 调用方 | 替代 |
|---|---|---|---|
| `task_stale_after` 观测态（不杀） | `config.go:315`、`build_agent.go:469`、`task_manager.go:21`(`TaskStale`)、`:1081`(`markStaleDetached`)、`staleNoted` | 探测器/看板/恢复 | 删除，并入 TTL reaper |
| `task_job_deadline` 独立年龄墙 | `config.go:319`、`build_agent.go:478`、`task_manager.go:1116`(`enforceJobDeadline`) | reconcile | **复用其配置槽并更名**为默认 TTL（决策11），非并存 |
| `TaskMaxDetachedAge` 兼容重映射 | `config.go:320`、`build_agent.go:492` | 配置解析 | 直接删除（Decision 10 不留别名） |
| 看板 `⚠…需确认` 非终态邀请 | `task_board.go:54-56` | RenderBoard | 改为呈现 TTL/剩余寿命（决策12） |

**受管旧运行数据清理范围**：`.wechat-config/data`（FileSegmentStore：事件/索引/墓碑/meta）、`data/reliability/{bus,memspill,anchors}`（信箱/spill/锚点）、`data/governance`（budget/approvals）。按一致恢复单元重置，不要求排空；须先停 owner 取写权。**排除**：源码、`tagent*.yaml` 配置、凭据、`logs/`、`data/trajectories/`（含 `att-traj2h.gz` 等基准原始证据）。当前格式损坏/目录不可读/I/O 失败**不**触发清理。

**排除项**：正常 volatile 模式、当前版本 I/O 恢复、当前内部记录规则为产品能力，不列淘汰。4.0a/4.0b 属未完成实现（非旧版兼容），归 §4/§5 重做范畴而非"删除"。

### 1.3 故障替身与 spy（已交付，方案 A：test-only，不耦合 §2 生产）

新增 `memory/fault_double_test.go`（纯 test code）：

- `faultKV`（嵌入 `*mockKV`，保留 `Sync()`/`ListPartitionIDs()` 可选能力）：按 **(path, phase)** 二元组 arm 故障，phase ∈ input/read/write/receipt/sync；`classifyPut(key)` 谓词把特定 KVPut 归为 receipt 阶段；记录带路径前缀的有序 op spy。
- `faultStore`：双入口 `StoreEvent`(pathNormal)/`ReplayEvent`(pathReplay) 分别标路径后委派真实 `FileSegmentStore`；input 阶段在任何 inner/KV 调用前拒绝（spy 记 `*:input` 且零 KV op）。

自测（证明替身不空转、故障必被目标路径消耗）：`go test -count=1 -run TestFaultDouble_ ./memory/ -v` → 3/3 **PASS**。

| 自测 | 断言 | 结果 |
|---|---|---|
| `_PerPathWriteArming` | 只 arm (replay,write)：普通写成功、`ReplayEvent` 失败且 spy 见 `replay:write` 无 `store:*` | PASS |
| `_BarrierReachedByBothPaths` | **普通写与重放提交均触达 `Sync`**（spy 见 `store:sync`/`replay:sync`）；arm (replay,sync) 使重放失败而普通写不受影响 | PASS |
| `_InputStageRejectsBeforeIO` | arm (store,input) 拒事件且 KV 层零 op（区分校验失败与提交失败） | PASS |

> `_BarrierReachedByBothPaths` 通过 = harness 能识破"重走缓存不碰屏障"的假成功，正是 §2「移除缓存提交判定/重放仍过屏障」的 fail-before 探针基础。

回归：`go vet ./memory/`=0；`go test -race -count=1 ./memory/` → **ok 3.3s**（全量无回归，无命名冲突）。

### 1.4 真实夹具 + 当前反例（已运行，命令与完整结果）

复用现有 agent 夹具（`newTestContextManager`/`newTestTagentAgent`/`NewReliableEventBus`/`requestCapturingModel`）+ 新增 `agent/counterexample_test.go`（`phasedStore` 双入口替身）与 `memory/counterexample_test.go`（复用 1.3 `faultKV`）。

`go test -race -count=1 ./agent/ ./memory/` → 均 **ok**（反例经 `TestCounter` 选择运行）。

| 反例（对应 1.4 五项） | 命令观察 | 归类 | 处置 |
|---|---|---|---|
| 部分输入提交失败→不得吞作完成（§4.3） | 第2输入 `ReplayEvent failed → append gated → 返回 false`；第1正常 | **当前已正确** | 保留为回归锁 |
| 缓存淘汰后重放按 KV 内容判定（§2.4） | `StoreEvent`→`cache.Remove`→`ReplayEvent` 同内容 → 现为 `ReplayRepaired` + `eventCount++`（冷缓存以有界 LRU 为 committed-oracle，非 KV 事实链）→ 双计数 | **已复现 fail-before**（review #1） | 断言收紧至 `AlreadyCommitted`+`count==1`，`t.Skip("blocked-by §2.4")`；§2.4 移除缓存提交判定后解除 |
| 损坏准备载荷（§3.5） | 日志 `prepared_fact undecodable (rebuild may restamp)` 后仍 `persisted bus event`（context_manager.go:1097-1100）→ 断言 `stored=false` 失败 | **已复现 fail-before** | `t.Skip("blocked-by §3.5")`，§3 落地解除 |
| 并发关闭（§6.1） | 8×`ta.Close()` 并发 → 无 panic/无死锁、5s 内收敛（volatile 基线） | **当前已正确（浅）** | 保留；完整 poisoned/late-attach 见 §6.6 |
| 删除后 dirsync 失败（§2/§3） | 现由 `segment_store_barrier_test.go:TestBarrierFailure_FailsCommitAndSparesCountThroughDecorations` 覆盖屏障失败不递减 | **他处已覆盖** | 不重造弱变体，§3 冷启动 dirsync 另补 |

**附带发现（非阻塞）**：`persistBusEvent` 在 `stored=false`（追加已门控）时仍执行 L1203 `log.Infof("persisted bus event")`，日志与实际不符——§5/§4 重做时以结构化阶段结果取代布尔 + 该无条件日志。

### 1.6 基线门（逐项分类 + 断言来源核验）

**断言来源**：四个 `TestCounter` 目标（损坏须阻断、部分失败不得吞、重放按 KV 判、关闭须收敛）均取自 `async-task-lifetime`/`persistent-event-loop`/`event-sourced-projection`/`runtime-resource-ownership` 规格条款与 design 决策，非取实现状；fail-before 靠删除弱回退达成，未靠削弱断言。

**阶段 2–10 验收断言现状（已复现 / 源码推导待复现 / 当前已正确）**：

| 阶段 | 当前判定 | 依据/待办 |
|---|---|---|
| §2 存储提交/去缓存 | 部分已正确（淘汰后重放按 KV）；**同内容已存在重放仍过屏障**未证 | faultKV 可探 `replay:sync`；§2 写 pass-after |
| §3 接收/准备 | 损坏准备 **已复现 fail**；不确定写/精确大整数未覆盖 | §3.1/3.2/3.5 待实现 |
| §4 执行门 | persistBusEvent 级部分失败门 **已正确**；loop 级“全 selected 才进模型” **待复现** | 需 §4 runner 反例（真实 A+B/C） |
| §5 完成/恢复/清理 | 无 | receipt/Ack API 未建 → 待 §5；当前无可运行反例 |
| §6 生命周期 | volatile 并发关 **已正确（浅）**；poisoned/late-attach/共享 A/B **待 §6** | 6.6 完整生命夹具 |
| §7 呈现边界 | `reliability_boundary_test` 已锁恢复提示尾部/不入投影 | 基线已正确，§7 扩真实调用边界 |
| §8 消费者/§9 基准/§10 TTL | 无 | 各自阶段建；§10 已据 09-17 快照录反例（本 1.1） |

**准出**：基线快照、映射、淘汰清单、故障替身、当前反例均落定；阶段 2–10 断言源自规格已逐项确认；唯一已复现 fail-before（损坏准备）已 Skip 保树绿。→ **§1 门通过**，可进 §2。

## 2. 存储提交、恢复与派生计数

### 2.1 提交核心收敛：公共拒重 + 孤儿补齐专属 ReplayEvent（方案 A）

**改动**（`memory/segment_store.go` `StoreEvent`）：当 idx 已存在（该 EventKey 已有槽），公共 `StoreEvent` 不再调 `completeOrphanCommit` 幂等补齐，而是返回 typed `ErrDuplicateEventKey`——与 `InMemoryStore.StoreEvent`（已拒任意已存键）**公共契约一致**。孤儿/半孤儿补齐**唯一**归内部 `ReplayEvent`（L414）。

**依据（无生产依赖被破坏）**：mem_spill `ReplayWithNotify`（mem_spill.go:76-109）在 store 实现 EventReplayer 时优先走 `ReplayEvent`，仅能力缺失后端才回退 `StoreEvent`；durable 走 claim→`ReplayEvent`（persistBusEvent 4.0b）；volatile 用唯一新键。故无调用方依赖公共同内容幂等。

**同步测试到新契约**（非削弱，是移至正确入口 + 新增公共拒重断言）：
- `TestHalfOrphan_RepairWritesMissingEvtSlot`：改为断言公共 StoreEvent **拒**修复（evt 槽保持缺失）+ `ReplayEvent` 完成修复（`ReplayRepaired`、evt 可读）。
- `TestAlreadyCommitted_RetryDoesNotIncrementLiveCount`：公共重试 → `IsDuplicateEventKey` 且计数不变；F8 不重复计数改由 `ReplayEvent` 已存在路径验证（`ReplayAlreadyCommitted`）。
- `TestBarrierFailure_FailsCommitAndSparesCountThroughDecorations`：heal 步由 `decorated.StoreEvent` 改为 `decorated.ReplayEvent`（先断言公共重试被拒，再验 `ReplayRepaired` 完成）。

**回归**（`-race -count=1`）：memory/memory\_embedder/engine/kv、agent/compress/governance/reliability/task、event、root 均 **ok**；`examples/wechat-bot` build+test **ok**。`TestBackendParity_DuplicateRejected`、`TestReplayEvent_Classification`、`TestReplayEvent_HalfOrphanRepair` 保持绿。

### 2.2 锁面测绘（改锁前置，尚未动生产）

**当前锁模型**：`simpleLRU.mu`（cache 自洽）、`PartitionState.mu`（**短账目锁**，仅护 `currentWindow/seqCounter/eventCount`，**不跨 I/O**）、`partitions` 为 `sync.Map`。无跨「身份核对→原文/索引/meta→屏障→发布」的分区 mutation 锁。

**真实复合法竞态（2.2 目标，均零覆盖）**：
1. **提交发布非原子**：`finishCommit` 写 idx（527）→ Sync（552）→ cache.Add（561）→ 另起 `state.mu` 内 `eventCount++`（562-564）。idx 写与计数不在同一临界区。
2. **seal/demote 丢更新**：`SealCurrent`（1142）取 `state.mu` 读 window/seq 后释放 → 扫段 → 写 `Sealed` meta；并发 `StoreEvent` 的 demote-meta（314-324）与之读改写冲突。
3. **删除/修复 vs 异步压实**：`DeleteEvent`（物理删 evt+idx，`cache.Remove`+`decrementEventCount`，1047-1060）与 `completeOrphanCommit`（重填 evt+`cache.Add`+`eventCount++`）不共享锁 → 重写真被删事实、计数漂移；异步 `Compactor` finalize（lifecycle.go:191/284）+ `removeVector` 回调（1114-1121）亦在竞态面内。

**可复现性评估**：竞态为**逻辑性**（复合非原子），非裸内存 race——各共享字段已各持锁，`-race` 未必触发。经 review 纠正：`mockKV` **本身带 `mu sync.Mutex`**（testbase_test.go:20），故对 store 直接跑 `-race` 压力**不会**撞替身 map 噪声；但 `faultKV`/`faultStore` 的 per-path 标记（`setPath`+多次独立加解锁）**非并发安全**，只可单 goroutine 用——并发压力应用**普通 mockKV 后端的 store**，而非 faultStore。要确定性坐实逻辑竞态仍需：一个**阻塞交错 hook**（令一 goroutine 停在 idx-写后/计数-前，另一 goroutine 删同键）。

**锁序（spec 已钉死，非开放项，见 design 决策 13）**：新增分区 `mutationMu`（外层，跨 I/O 持有）；固定序 `mutationMu → (KV I/O) → state.mu(账目)`，永不反向；`vecRemover`/`notify`/tombstone 广播一律出 `mutationMu` 后延迟调用；`SealCurrent`/`Compactor` 对每分区最终发布同持该分区 `mutationMu`（spec L37）。**与 §2.3/§2.4 不可拆**：§2.4 去缓存后的 case C′（屏障成功但未计数）需“协调域内重扫该分区”对账，而协调域即本锁——故 **review #1 的正解是 2.2→2.3→2.4 一个提交核心簇**，非“先做 §2.4”。

## Code review 处置（2.1 落地后）

CodeReview 子代理对 §2.1/§1 及其全依赖面评审；我逐条亲验后处置：

| # | 发现 | 核验 | 处置 |
|---|---|---|---|
| 1 | `completeOrphanCommit` 以有界 LRU 为 committed-oracle→冷缓存/重启重放已提交事实误判 `ReplayRepaired`+双计数（F8）；且反例断言过弱（`NotEqual(ReplayNew)` 放行 Repair） | 属实（segment_store.go 缓存判定分支 + counterexample_test.go 旧断言） | **本轮**：反例收紧至 `AlreadyCommitted`+`count==1` 并 `t.Skip("blocked-by §2.4")`（诚实 fail-before）。**生产 cache-oracle = §2.4 既定靶**（“移除缓存提交判定”），非 §2.1 引入，待 §2.4 重设为持久可判事实 |
| 2 | `mem_spill.go` D4 注释与 §2.1 矛盾（称回退分支同内容由 `completeOrphanCommit` 处理） | 属实 | **已修**：幂等来自 `GetEvent` 预检，公共 `StoreEvent` 现一律拒 |
| 3 | `ErrorTrackingStore.StoreEvent(WithEmbedding)` 把公共 `ErrDuplicateEventKey` 误归为 memory 依赖故障+落 spill | 属实（error_tracking.go:146/157） | **已修**：`IsDuplicateEventKey` 短路，不上报、不落兜底（先例：`ErrVectorSearchNotSupported`） |
| 4 | `faultKV`/`faultStore` 非并发安全（per-path 标记多次独立加解锁）；mockKV 其实**有锁**（纠正本文先前前提） | 属实（testbase_test.go:20） | 已在上文 §2.2 纠正；§2.2 并发探针改用普通 mockKV 后端，不用 faultStore |
| 6 | parity 测试未覆盖“同内容”那一半双端 | 属实 | **已补**：`TestBackendParity_DuplicateRejected` 双端均断同内容公共重复拒 |

未采纳/缓办：#7 seq 烧号（基线既有、无害）、#9 canonical 文案细微不一致（次要，随后续收敛）。#8 §3.5 skip 已锁为阻塞前置。

## 10. 异步任务 TTL 回收与结算折叠

### 10.1 失效任务与结算死重的当前反例证据

**失效任务（主症，已坐实为代码级盲区）**：
- **根因（读码确证）**：唯一年龄终止 `enforceJobDeadline` 三重 gate——`deadline>0` ∧ `status∈{alive_detached,stale}` ∧ `detachedAt` 非零（L1124）∧ `LifetimeJob`；`reconcileDetached` 候选集也只含 alive_detached/stale（L928）。故 **job 型任务静默入 `suspect` 且从未 detach（detachedAt=0）时不受任何年龄墙约束**——正是生产 `56bf24c3`（restart-tagent.sh，挂 23h）根因。
- **可执行 fail-before**：`agent/task/task_ttl_counterexample_test.go` 两例（`TestCounter_SuspectNeverDetachedEscapesEveryAgeWall`、`TestCounter_SuspectBoardInvitesRepeatedArbitration`）。默认 SKIP 保树绿；`TAGENT_RUN_FAILBEFORE=1 go test -timeout 25s -run TestCounter_ ./agent/task/ -v` 实证**双双 FAIL**：前者报“suspect 30h 旧（超 8h 墙）仍 suspect”、后者报“看板含 ‘需确认’ 邀请”。→ 真 fail-before，非弱化断言。修复后同法验 pass-after。
- **新引入的可复用 honest-fail-before 模式**：`skipUnlessFailBefore(t, reason)`（env 门控），避免“名为 fail-before 实为已弱化/未必失败”（本轮 review 教训）。

**结算死重（不按旧数据断为当前缺陷）**：当前树**已实现** `foldSettleRuns`/`isSettleNoticeRef`/`settle_fold` 票据卡（`agent/compress/context_compressor.go:700-730+`）并有测试 `TestCompress_SettleStormFoldReclaims80Percent`、survival/lossless、idempotent 均绿。故 **不写“不可回收”fail-before**（与实测行为矛盾）；09-17 轨迹 floor 96–99% 早于该机制。**不得据旧轨迹冒充当前缺陷**。§10.7 残余范围＝以真实代码核验“覆盖全退役来源 + 每条结算以有界票据可召回”，而非假定已完备。

### 10.2 exec/action 模型侧 schema 增 ttl + 默认解析

**落地**（均 additive，无行为变更）：
- Declaration 新增 `ttl`（整型秒）属性，描述明说“绝对寿命、op=send/resume 刷新、op=peek 不刷新、无禁用、resident/interactive 不豁免、与 quiet_timeout 正交”。
- `ActionArgs.TTL int`；Call 校验 `ttl < 0` 拒（唯非法输入），`0/缺省`=取默认（无“无限”哨兵）。
- `TaskSpec.TTL time.Duration` 字段（供 §10.3 reaper 读的绝对寿命）；Call 异步路径与 relaunchClosure 两处均挂 `TTL: ct.resolveTTL(args)`。
- `ActionTool.resolveTTL`：显式>0秒 → 否则 `ct.defaultTTL` → 否则 10min floor；`defaultTaskTTL=10min`；`SetDefaultTaskTTL(<=0 忽略)`。
- build_agent late-bind 接 `SetDefaultTaskTTL(agentCfg.TaskJobDeadline)`：**复用运维 `task_job_deadline` 槽作默认 TTL**（空→setter 保留 10min）；旧 `enforceJobDeadline`（对 suspect 本即 no-op）待 §10.5 删除。

**测试**：`tool/action/ttl_arg_test.go`（`TestResolveTTL`、`TestDeclarationExposesTTL`）PASS，不依赖 tmux。**无 fail-before**（10.2 是新增能力非修旧缺）。

**验证备注**：首次 `go test ./tool/action/` 出现 `TestCommandParsing/simple` 报“failed to create tmux session: server exited unexpectedly”——真实 tmux 环境抖动（发生在建会话期、早于任何 TTL 逻辑），复跑两次均 `ok`；与本改动无关。**本步仅携带 anchor，reaper 尚未消费（§10.3）**。

### 10.3 单一 TTL reaper（reconcileTTL）——盲区反例转 pass-after

**实现**（`agent/task/task_manager.go`）：`reconcileTTL()` 作为 `reconcileDetached()` 首步（由 `List` 驱动），遍历**全部任务**：`effTTL = spec.TTL>0 ? spec.TTL : tm.defaultTTL`；**仅 `effTTL>0` 且非终态且 `now - ttlAnchor >= effTTL` 才回收**——覆盖全部 active 态（含 suspect/未 detach）与全部寿命类（resident/interactive 不豁免，因不再检 detachedAt）。到期锁外 `detector.Cancel`（杀底层）→ `finalizeRetired`（SettleFailed 单次，terminal fence 防重）→ 下回合看板不再含。

**过渡设计（保旧测不挂）**：`TaskManagerConfig.DefaultTTL` 缺省 **0=关**，NewTaskManager 不做 10min 默认——旧 `TestStaleDetached_*`/`TestJobDeadline_*` 直接 `tm.Spawn` 无 spec.TTL、manager 无 DefaultTTL → `effTTL<=0` → reaper OFF → 旧 detached-wall 行为完整保留（待 §10.5 统一翻为恒开+迁测试）。生产命令任务经 ActionTool 恒带 spec.TTL>0 → 无论 manager 默认均受界。

**接线**：agent.go `TaskManagerConfig{DefaultTTL: cfg.TaskJobDeadline}`（复用槽，作 restored/subagent 回退）；`SetDefaultTTL` 方法 + `task_record_sink.go` 热更（与 SetJobDeadline 同槽）。`Task.ttlRenewedAt` 字段 + `ttlAnchor()` 预留供 §10.4 刷新。

**验证**：10.1 的 `TestCounter_SuspectNeverDetachedEscapesEveryAgeWall` 去 skip、给 suspect 设 `TTL:10min`、始动 30h 前 → **PASS**（reaped→failed），旧 stale/deadline 全族仍 `ok`。`go test -race ./agent/... .` 全 **ok**；`tool/action -race` 单包 **ok**（全树并发曾现 `TestActionTool_TmuxExec` 真实 tmux 抖动，隔离 3× 均 ok，非改动）。

### 10.4 重入刷新（resume / op=send 重置锚点，peek 不刷）

**实现**：`Task.ttlRenewedAt`（10.3 已备）为写入型重入刷新锚点；`ttlAnchor()` = `ttlRenewedAt` 非零优先、否则 `StartedAt`。reconcileTTL 据此计时。
- **resume**：`TaskManager.Resume` 再臂段（会话确注入、`ResumeFn` 成功后）置 `ttlRenewedAt=tm.now()`。
- **op=send**：`TaskManager.RenewTTLBySession(sessionID)` 按 `Spec.Declarative.TaskID==sessionID` 找活跃任务刷新；`callSessionOp` 在 `opSend` 成功后经 `TaskControllerFromContext(ctx).RenewTTLBySession(target)` 调用。
- **op=peek**：不刷新（代码路径不接触 refresh）；**op=stop** 终态。一次性无会话命令无重入→锚点恒 spawn；reaper 已跳过终态→正常 settle 先行、到期不放行。
- 接口选择：`RenewTTLBySession` 加入 `TaskController` 宽接口（**非**另立可选窄接口）——`OriginSpawner` 内嵌 `TaskController` 接口会提升其全部方法，而内嵌接口不提升非接口方法，故宽接口才能同时满足 `*TaskManager` 与带 origin baggage 的 `*OriginSpawner`。需同步补测试替身 `fakeTaskController.RenewTTLBySession`（已补）。

**测试**：`agent/task/task_ttl_test.go`—`TestTTLReentrantRenewal` 三子例全 PASS：① renew 后按新锚点存活、过新锚点 ttl 后回收；② 未 renew 按 spawn 计时即回收（与 peek 不刷同构）；③ 未知/空 session no-op。`-race` 覆盖 agent 族 + root + tool/action 均 **ok**。

### 第 2 次 code review 处置（§10 TTL 链）

派发 CodeReview 子代理专评 §10.2–10.4（§2.1/§1 上轮已评并修）。承重结论本人已逐条读码复核，不盲信：

**① 重要（属实）— 默认配置下 reaper 对 restored/subagent 恒关，“恒开/无任务不受限”不变量未达成**。核验：`reconcileTTL` 仅 `effTTL>0` 才触发；`defaultTTL = cfg.TaskJobDeadline`，而 `task_job_deadline` **默认 0=禁用**；`SpecFromDeclarative`（[declarative.go#L137-L146](../../../tool/action/declarative.go)）重建 spec **不置 `TTL`**、`Declarative` 无 TTL 字段；subagent `SubagentSpecFromDeclarative` 同无。**后果**：默认环境所有 restored 任务（跨重启复活）与 subagent 对 reaper 不可见——而 `56bf24c3`（`restart-tagent.sh` 跨重启滞留）极可能就是 restored 任务→**本轮尚未真正修复该病案**。且我的 age-wall pass-after 反例**手动注入 `TTL:10min`**，恰好掩盖真实 restore 不给 TTL 的差异。**处置**：新增诚实 fail-before `TestCounter_RestoredTaskWithoutTTLBindingUnbounded`（默认 SKIP / `FAILBEFORE=1` 实测 FAIL：“24h restored 任务仍 suspect”），把缺口纳入追踪；修复归 §10.5（restore 派生/持久化 TTL + subagent TTL + manager 侧 10min 地板常开）。**不虚报 §10.3 为端到端已修复。**

**② 重要（属实）— 过渡期双墙早杀**：若运维配 `task_job_deadline=T` 且模型显式请求 `ttl>T`，旧 `enforceJobDeadline`（detached-gated）会先于新 TTL 把已 detach 的长寿命任务杀掉；command 任务（含 resident/interactive）不设 `Lifetime`→`LifetimeOf` 判 job→受旧墙约束。工具描述“pass a large ttl for long-lived services”在 §10.5 删旧墙前不严格成立。默认 T=0 时旧墙关→无冲突，故仅在运维配置时显现。**处置**：归 §10.5（删 `task_job_deadline` 后双墙合一）；根因同一槽复用过渡。

**③ 次要**：`finalizeRetired` 锁外改 `t.Spec.Origin`（与 `RetireOrphans/enforceJobDeadline` 同款，非本轮新引），但 reaper 现在每次 `List()`、对全 active 态触发，提高 lock-free 写暴露面→§10.5 一并收敛入 `t.mu`。

**④ 次要**：`quiet_timeout` 推荐 600s 与 `ttl` 默认 10min 同值，合法但 >10min 构建会被 TTL 强杀。属决策 11 接受的代价，但在 `quiet_timeout` 描述里未交叉提示。→待 §10.5/文档补交叉提醒。

**子代理肯定项（经复核）**：reaper 机制本身正确（覆盖全 active 态+全寿命类、`Cancel`+`finalizeRetired` 单次 failed、无重复终止/锁序死锁）；重入刷新语义正确；`op=send` 对命名会话经核验**不静默失配**（spawn 存 `n-<name>`、`resolveTarget(name)` 也返 `n-<name>`，与 `Declarative.TaskID` 一致）。总体：可作为 §10.5 之前的过渡增量，但**不得据此宣称 async-task-lifetime 已满足规格**（①②未收敛前）。

### 10.5 Phase-1（加法收口：review ①(a)/③(e)/④(f)，全绿；删除尾与 ②/地板属 Phase-2）

先落自洽、低风险、直接治愈真实病案的**加法项**，不动状态机/配置 schema（保证不留下不一致半成品）：

**(a) restore 恢复 TTL——治愈 `56bf24c3` 命令类主病案**。核验发现原投影 `Declarative.Params` **根本不含 `ttl`**（`DeclarativeFromArgs` 不写、`argsFromDeclarative` 不读且严格解码拒未知键）→ 仅补 `SpecFromDeclarative` 不够。本轮一次性打通往返：`declarative.go` 新增 `pTTL` const、`DeclarativeFromArgs` 持久化 `args.TTL>0`、`argsFromDeclarative` 严格解析（旧记录无该键→ `args.TTL=0`）、`SpecFromDeclarative` 两分支均挂 `TTL: ct.resolveTTL(args)`（显式值或 10min 地板）。经核 `build_agent.go:643` 重建主链确调用 `SpecFromDeclarative` → **生效于真实重启路径**（非死码）。旧记录向后兼容（回落地板）。

**(e) 锁修**：`finalizeRetired` 对 `t.Spec.Origin` 的写入现包于 `t.mu`（reaper 高频化后 lock-free 写 vs 投递侧读竞态，review ③）。

**(f) 文档**：`quiet_timeout` 描述交叉提示“仅探静默、不约总寿命，>ttl 的合法构建仍会被回收，长任务需同时加大 `ttl`”（review ④）。

**测试**：新增 `tool/action/declarative_ttl_test.go`—`TestSpecFromDeclarativeRestoresTTL` 三子例全 PASS（显式 ttl 往返 / 旧记录回落地板 / resident 7200s 经 marshal 往返精硬恢复 2h）。`go build ./...`、`gofmt`、`go vet` 净；**全量 `go test -race ./agent/... ./tool/... .` 17 包全 ok**（含 (a) 行为变更“restored 命令任务现受 TTL 约束”无回归；tmux 重的 tool/action 本轮无抖动）。

**未动（留 §10.5 Phase-2 专注迁移）**：(b) subagent TTL、(c) manager 地板恒开（需先删旧 stale/deadline 测试族）、(d) 删 `task_job_deadline` 后双墙合一（review ②）、`TaskStale` 枚举与三 config 槽删除 + 热更/指纹/重建状态过滤全链 + 迁 `task_detached_wall_test.go`。故 **10.5 保持未勾选**，① 的 task-layer fail-before（默认 manager 地板下 RestoreTask 无 TTL）仍需 (c) 才翻为 pass-after。

### 10.5 Phase-2（旧机制删除与状态机收口，已全绿）

一次性内聚迁移（删字段会级联多文件编译错，故源码全体同落地后统一编译）。前置事实核验：“stale” 仅在枚举定义处出现、**从不落盘**（`markStaleDetached` 是运行期内存态）→ 删 `TaskStale` 无需磁盘数据迁移；reconcileDetached 的 probe-gone→completed 是非年龄回收职责（保留）。

**状态机/回收收口（`task_manager.go`）**：删 `TaskStale` 枚举及其在 `isActive`/`isTerminalExpired`/`emitBackground` 回滚/`reconcileDetached`/`build_agent.go:667` 重建过滤的全部 case；删 `markStaleDetached`/`enforceJobDeadline`/`staleAfter`/`jobDeadline`/`staleNoted`/`defaultStaleAfter` 字段与 `SetStaleAfter`/`SetJobDeadline` setter；`reconcileDetached` 降为“探测会话消失→completed”单一职责（probe 存活→`continue`，年龄交由 reaper）。

**reaper 恒开**：新增 `defaultManagerTTL = 10min`；`NewTaskManager` 将 `cfg.DefaultTTL<=0` 回落地板；`SetDefaultTTL` 负值（禁用企图）→地板而非 0（无禁用哨兵）。**零锚点守卫**（新发现）：reconcileTTL 对 `ttlAnchor().IsZero()` 的任务跳过——无已知出生时刻不可定龄回收（生产 Spawn/RestoreTask 恒设 StartedAt，仅防不定龄伪任务被数千年假想年龄误杀）。

**(b) 简化**：subagent 无专用 TTL——reconcileTTL 不分 kind 遍历全任务，`spec.TTL=0` 即取 manager 地板→自动受界，无需跨包常量。

**配置层（`task_default_ttl` 单槽，不留别名）**：`config.go` AgentConfig 三旧字段→`TaskDefaultTTL`；`org_hotreload.go` agentSubset 去 `TaskMaxDetachedAge`；`build_agent.go` 三 parse 块→一 + `SetDefaultTaskTTL` 重指向；`tagent.go` 热更默认/解析/日志/注释；`context_manager.go` OrgHotParams；`agent.go` 字段+构造；`task_record_sink.go` 热更 setter。（e）(f) Phase-1 已落。

**测试**：删 `task_detached_wall_test.go` 全族（stale/deadline 语义，共 23 引用；probe-gone 回收已由 `task_liveness_reconcile_test.go` 覆盖）；age-wall 反例去 `StaleAfter/JobDeadline` 字面量；翻转 restored 反例→`TestCounter_RestoredTaskWithoutTTLBindingIsReaped` pass-after（无显式 TTL 仍被地板回收 + 断 failed）。

**地板恒开的行为回归（非削弱，测试隔离）**：`TestReconcileZombies_AliveProbeProtectsQuietRunner`/`_NilProbeSkipped`（用 2h/24h 真旧时间戳测 probe/zombie 路径）+ `TestPruneTerminal_NilDetectorDoesNotPanic`（零 StartedAt 手建）受地板影响——前两测属 **zombie/probe 路径**与 TTL 正交→给大 `DefaultTTL` 隔离；后者属零锚点→由新守卫自然修复（回退其大地板）。“无论多老都不回收”旧断言与 §10.5“无任务永生”相悖，按新不变量改写注释。“no task immortal” 真实断言移至 restored-floor pass-after 反例。

**文档**：`examples/wechat-bot/tagent.yaml`（strict-yaml KnownFields 拒旧键）两旧槽→`task_default_ttl: "8h"`；`docs/wiki/agent/agent-architecture.md` stale/deadline 条→统一 TTL reaper。

**验证**：`go build ./...`、全仓 `go vet ./...`、`gofmt` 净；全仓 `.go` 无悬挂被删符号（仅余三处历史注释，均已校）；**`go test -race ./agent/... ./tool/... .` 17 包全 ok**。review ①②（命令类不变量 + 双墙）至此真实闭环。

### 10.6 看板呈现剩余寿命 + 删"需确认"邀请

**实现**：`RenderBoard(tasks []*Task, defaultTTL time.Duration)` 携 manager 地板；行格式 `- [status] desc (id=…, 已运行 X[, 剩余 Y 后回收 | , 即将回收])`。`remainingLifetime(now, defaultTTL) (rem, ok)` = 与 `reconcileTTL` **同口径**：`ttl=spec.TTL>0?:defaultTTL`、`anchor=ttlAnchor()`（`ttlRenewedAt>0?:StartedAt`）、`rem=ttl-(now-anchor)`——保证看板数值恰是 reaper 将honored 的值。删除 suspect 的 `⚠ 长时间无输出，可能假死，需确认` 非终态邀请（年龄回收的 bounded 任务不需要模型逐回合重判）；剩余 <=0 显示"即将回收"。`isActive()` 已天然排除终态（到期/终态不渲染，reconcileTTL 已在 List 中回收）。

**接线**：`TaskController` 加 `DefaultTTL()`（`*TaskManager` 锁内读 `tm.defaultTTL`，`OriginSpawner` 经内嵌接口自动提升，`fakeTaskController` 补 no-op）；`context_manager.go:1415` 注入点改 `RenderBoard(cm.taskController.List(), cm.taskController.DefaultTTL())`。

**测试**：翻转 board 反例→`TestCounter_SuspectBoardShowsRemainingNotArbitration` pass-after（断无 `需确认` + 有 `剩余`）——注：原反例用 23h suspect，在 §10.5 地板下已被回收、不可能上榜，故改用 TTL 内的年轻 suspect（-1min、地板 10min → 剩余 9m）。新增 `TestRenderTaskBoard_ShowsRemainingLifetime` 锁精确数值（显式 30m TTL @10min→"剩余 20m"、无 TTL 回落 10m 地板 @1min→"剩余 9m"，验证 `spec.TTL` 优先于地板）；5 处 RenderBoard 调用点加 `defaultTTL` 参数。

**验证**：`go build ./...`、`go vet ./agent/... ./tool/... .`、`gofmt` 净；**全量 `go test -race ./agent/... ./tool/... .` 17 包全 ok**。至此 `56bf24c3`（suspect 挂看板逐回合空转裁决）端到端治愈：任务恒受 TTL 约束 → 看板只显"剩余 X 后回收"、不再邀请"需确认"→ 模型读一次即定。

### 10.7 结算折叠覆盖所有退役来源（TTL 波纳入 N→1）+ external_input 可回收核验

**核验既有**：投影侧已完备——`settleNoticePrefix="[task settled"` 按前缀（非退役原因）匹配，`isSettleNoticeRef` 令一切 settle 通知 external_input **不论段龄** fold-eligible；`buildSettleFoldRef` 产出有界票据卡（每行 `[evt_key]`+`settleFoldRowMaxChars`=80 字预览，全量经 evt_key 事实链可召回）；`settle_fold_test.go::TestCompress_SettleStormFoldReclaims80Percent` 等验收折叠回收（既存绿）。

**真实缺口（本轮修复）**：批量退役 N→1 折叠（`beginBatchRetire`/`onBatchRetire`→`newBatchRetiredSummaryEvent`）此前**仅 `reconcileZombies`/`RetireOrphans` 各自开批**，而顶层 `reconcileDetached`（含 §10.5 的 `reconcileTTL` 逐任务 `finalizeRetired` 与 probe-gone 回收）**未开批**——故 mass TTL 到期（重启后大量过期）会发 N 条独立 `[task settled]` 通知，重新抬高投影、违背"覆盖所有退役来源"。**修复**：`reconcileDetached` 首行 `finishBatch := tm.beginBatchRetire(); defer finishBatch()`，令 TTL reaper 波 + probe-gone + 嵌套 zombie/orphan 全部汇入同一收集器（nested-safe，内层 begin 复用外层）→ 单次 N→1 汇总；`newBatchRetiredSummaryEvent` 每行 `[task settled]` 且属 `NewExternalInputEvent` → 该汇总本身仍 fold-eligible（多轮多条汇总再被 `foldSettleRuns` 折叠）。

**测试**：`agent/task/task_batch_retire_ttl_test.go::TestBatchRetire_TTLReaperWaveCollapsed`——3 个超 1m TTL 且 probe 活的任务（唯 TTL 路径回收）经 `List()` → 断 `OnBatchRetire` 收 3、逐条 `OnSettle`=0、全部 failed。**修复前**该波走 per-task OnSettle（batch=0/perSettle=3）→ 测试红；**修复后** batch=3/perSettle=0 → 绿。既有 `TestBatchRetire_CollapsedNotification`（orphan 侧）仍绿。`agent/compress` + `agent` `-race` 全 ok。

### 10.8 quiet/TTL 正交核验

- **结构核验**：`reconcileTTL` 仅读 `spec.TTL`/`defaultTTL`/`ttlAnchor`/`isTerminalStatus`，**从不读任何 quiet/静默态**；`QuietTimeout` 不在 `TaskSpec`（仅 ActionArgs→detector 构造），两轴无共享状态。
- **fake-dead 语义**：`settle.go::StatusToSettle` 明示 `SessionTimedOut → SettleSuspect`（置疑、**不 kill**）；`KillSession` 仅在 detector 的 `Cancel` 回调（由 TTL reaper / 显式取消触发）。即"静默只改状态，唯 TTL 按年龄回收"。
- **既有覆盖**："长构建设大 quiet 不误杀" = `tool/action/quiet_timeout_test.go::TestQuietTimeout_SessionOverridePreventsKill`(4.1)（QuietTimeout=10m 使 200ms 静默不被判 fake-dead）+ `TestQuietTimeout_DefaultEquivalence`(4.2)。
- **新增锁定**：`agent/task/task_ttl_orthogonal_test.go::TestTTLReaperIndependentOfQuietState`——suspect（fake-dead 态）在 TTL 内**存活**（静默永不回收），超 TTL 必被回收（年龄轴与 quiet 无关）。

### 10.9 总门（TTL/回收/折叠 端到端闭环）

逐条 → 锁定测试：

| 10.9 断言 | 锁定 |
|---|---|
| 失效任务到期终止 + 从看板消失 | `TestCounter_SuspectNeverDetachedEscapesEveryAgeWall`(pass-after)、`TestCounter_RestoredTaskWithoutTTLBindingIsReaped`；看板经 `isActive()` 排除终态 |
| 模型不再重复裁决（看板无"需确认"、显剩余） | `TestCounter_SuspectBoardShowsRemainingNotArbitration`(10.6)、`TestRenderTaskBoard_ShowsRemainingLifetime` |
| 重入续命、peek 不续命 | `TestTTLReentrantRenewal`(10.4) 三子例 |
| `[task settled]` 可折叠、floor 不被死重抬高 | `TestCompress_SettleStormFoldReclaims80Percent` + `TestBatchRetire_TTLReaperWaveCollapsed`(10.7 源侧 N→1) |
| 无并存年龄墙、旧别名不读取 | 10.5 grep 净（`TaskStale`/`stale_after`/`job_deadline`/`TaskMaxDetachedAge` 全仓无悬挂） |
| quiet 与 TTL 不混淆 | `TestTTLReaperIndependentOfQuietState`(10.8) |

**回收率前后对照（同配置同输入，非清理旧数据）**：`TestCompress_SettleStormFoldReclaims80Percent`——50 条 settle storm external_input，`SmartCompress` 实测 `17406 → 2438 tokens`（**≈86% 回收**），恰 1 张票据卡，事实链每条 `GetEvent(evt_key)` 仍可召回。叠加 10.7 源侧修复（TTL 波 N→1），退役潮抵达投影前即压缩为 1 事件，floor 双重不抬高。

**全门 `-race`**：`go test -race ./agent/... ./tool/... ./event/... ./memory/... .` **全 22 包 ok**（agent/task/action/event/memory/projection/compress + root）。async-task-lifetime 能力（10.1–10.9）至此**全部闭环**。

## §2 存储提交核心（重启暂缓，按 decision 13 成簇推进）

### 2.2 完整分区 mutation 协调（写手↔压实↔删除↔封口 统一锁序 + 反向回调出锁）

**病根**：`state.mu` 仅短护 {currentWindow, seqCounter, eventCount}，提交全链（身份/碰撞探测→evt 写→idx 写→屏障→cache/计数发布）在**锁外**执行；且 `Compactor.CompactL1ToL2/L2ToL3` 的持久发布（merge→改写 idx→删源段→`finalizeTombstones`→`removeVector`）**全程不持任何分区锁**。故并发同键提交存在 probe→publish 非原子（torn 双提交/双计数），并发 writer↔compactor 存在压实删源段吞掉刚写事件/漏并新写（丢事件、dangling idx）。

**落地（M 锁序：`mutationMu`→`state.mu`，反向回调出锁）**：
- `PartitionState.mutationMu`（外层）：`StoreEvent`/`ReplayEvent`/`DeleteEvent`/`SealCurrent` 各入口 `state.mutationMu.Lock(); defer Unlock()` 端到端串行化整个提交；`state.mu` 降为内层计数/窗口锁（仅经 mutationMu 内或独立叶子访问）。
- Compactor：新增 nil-store 安全 `lockPartition(pid) func()`；`CompactL1ToL2`/`CompactL2ToL3` 把 merge→KVBatch 改写→`deleteSegments` 纳入闭包内的 mutationMu，**返回 `dead` 后出锁**，`finalizeTombstones`（→`c.store.removeVector` 引擎反向回调）在锁**外**执行（旁路回调不锁内反调）。
- 死锁审计：`checkHourlySeal` 于 `state.mu.Unlock()` 后才调 `SealCurrent`（无 `state.mu→mutationMu` 反转）；`simpleLRU` 无 eviction 回调（`cache.Add` 锁内安全）；各路径单 pid、无跨双分区、无同分区公有方法重入（`mergeEvents`/`GetEvent`/`deleteSegments` 仅触 KV，不取分区锁）。

**fail-before/pass-after（非空断言）**：`memory/segment_store_concurrency_test.go::TestSegmentStore_ConcurrentSameKeyAtomicCommit`——16 写手经 `close(gate)` 同刻涌入提交**同一 EventKey**，断恰 1 成功 + 15 `ErrDuplicateEventKey` + `TotalEvents==1` + 事件可召回。
- **有 mutationMu**：`-race -count=4` 全 PASS（确定性 1 成功）。
- **临时摘除 `StoreEvent` 的 mutationMu**（trap 保证还原）：`-count=20` 复现 **got 5 / 12 / 2 winners**（torn 双提交）→ **FAIL**。证明该锁真实消除身份核对↔发布非原子窗口。

**全门**：`go build ./...`、`go vet`、`go test -race ./agent/... ./tool/... ./memory/... ./event/... .` **22 包全 ok**；mutationMu 改动经 mem_spill/engine 消费链无回归。

### 2.3 缺口测绘（下轮直接落地；本轮未动恢复热路径，保持树绿）

规格 L59 钉死内部重放 MUST 核验并补齐三类，逐一比对现状：

| 类 | 规格要求 | 现状 | 缺口/落点 |
|---|---|---|---|
| **class1 idx有/evt缺** | 在原槽补写 evt | `completeOrphanCommit` F3 已实现（读 idx→写 evt 于原 `EventKeyStr(pid,w,seq)`）| 无（已覆盖，本轮 mutationMu 下更原子） |
| **class2 evt有/idx缺** | 在原放置窗 + 该分区已登记段定位同 key 原文并**复用**，不生成第二原文；双层同内容取高层优先 | **未实现**：`ReplayEvent` 仅探 idx；NotFound 即走新写（分配新 seq）→ 复制出第二份 evt、旧原文成孤儿 | 新增有界扫描 `locateOrphanEvtSlot(pid,hintWindow,key)`：候选窗=放置窗∪`ListSegments(pid)`，逐窗 `KVScan(SegmentEventPrefix)` 找 `EventKey==key` 的槽，按 `GetSegmentMeta.Layer` 取最高层，命中则 `finishCommit` 写 idx→该 (w:seq) + 复用计数，返回 `ReplayRepaired`；未命中才走新写。扫描限单 key 的放置窗+登记段（非全历史，合规 L7）|
| **class3 缺 meta** | **不论 seq 是否零**均修复，层级/封口不无条件重置 | 现仅 `if seq == 0` 内、且仅 meta 缺失时写（保守 Layer:1/Sealed:false）| 去掉 `seq==0` 外层守卫：`completeOrphanCommit` 末尾无条件探测窗 meta，缺失才补（保留"存在即不动"→不重置层级/封口）；seq>0 亦补 |
| **墓碑不复活** | 合法墓碑拒绝重放，返回明确 forgotten/conflict，不覆写/移除墓碑 | `completeOrphanCommit` 未查 `s.tombstones.IsTombstone` | 入口加守卫：`if s.tombstones!=nil && IsTombstone(key)` → 返回 forgotten（需确认/新增 `ReplayResult` 的 forgotten 变体 + 调用方 mem_spill 语义）|

**依赖**：class2/3/墓碑 均须在 §2.2 的 `mutationMu` 内执行（已具备）；class2 复用原槽依赖 class1 的"定位原 (w:seq)"能力。与 §2.4（去 cache-oracle、完整记录分类、C′ 走协调域重扫）同簇，宜连做。**准出**：三类各一 fail-before（注入 crash 点：仅写 evt 不写 idx / 仅写 evt 删 meta / 写后 MarkTombstone）→ 补齐后 pass-after + `-race`。

### 2.3 三类补齐（class2 复用原槽 / class3 meta 不论 seq 修复）

- **class1 idx有/evt缺**：`completeOrphanCommit` F3 原槽补写（既有，§2.2 mutationMu 下更原子）。
- **class2 evt有/idx缺（新增）**：`locateOrphanEvtSlot(pid,hintWindow,key)` 扫「放置窗 ∪ `ListSegments(pid)` 登记段」找 `EventKey==key` 的既有槽，双层命中取 `GetSegmentMeta.Layer` **最高层**（复用将存活至压实后的段），`KVScan` 失败 **fail-loud**（不完整扫描不能证明"不存在"，否则复制第二份）。`ReplayEvent` 在 idx 缺失→新写之前先定位：命中则内容比对（异内容→`ErrDuplicateEventKey` 拒，不发布 idx/不覆写）+ `ensureWindowMeta` + 仅 `KVPut(idx→原 w:seq)` + 屏障 + cache + 单次计数，返回 `ReplayRepaired`，**绝不写第二份 evt**。
- **class3 meta 缺（新增）**：`ensureWindowMeta` 仅当 meta 确缺才写保守 `{Layer:1,Sealed:false}`（未封口→恒被扫描、保守包络不藏事件），**既有 meta 一律不动**→ 不无条件重置已封口/L2 升层的层级/封口；替换 `completeOrphanCommit` 原 `if seq==0` 守卫，seq>0 亦修。
- **测试**（`memory/segment_store_orphan_test.go`，注入 crash 点直写 KV）：
  - `TestSegmentStore_ReplayReusesOrphanEvtNoSecondCopy`：seq0 有 evt 无 idx → 重放 → idx→原槽、窗内**恰 1 evt**、`TotalEvents=1`。**旧码**（无 class2 定位）会分配新 seq 写**第二份** → `Len(pairs)==1` FAIL。
  - `TestSegmentStore_ReplayOrphanEvtDifferentContentRefused`：同键异内容 → `ErrDuplicateEventKey`、idx 不发布、窗内仍 1 份。
  - `TestSegmentStore_ReplayRepairsMissingMetaAtSeqGT0`：idx+evt 于 seq3 齐但 meta 缺 → 重放后 `GetSegmentMeta` 成功且未封口。**旧码** `seq==0` 守卫 seq3→meta 仍缺 → `require.NoError(GetSegmentMeta)` FAIL。

**依赖**：class2/3 均在 §2.2 `mutationMu` 内执行。**留待 §2.4**：`completeOrphanCommit` 仍以 `s.cache.Get(key)`→`ReplayAlreadyCommitted` 作 LRU-oracle（C′ 少计隐患），去除该判定、改按完整记录分类 + 不确定走协调域重算 + 墓碑不复活，属 §2.4/§2.5 同簇。

### 2.4 去缓存提交 oracle（完整记录分类 + recompute 权威计数 + 墓碑不复活 + 分区不重盖）

- **删 `cache.Get`→Already LRU-oracle**（spec L7「缓存/replay 分类不能作计数真源」）。`completeOrphanCommit` 新语义：`wasComplete = evt 已存在(字节一致) && meta 已存在`（`ensureWindowMeta` 现返回是否修复）→ 未写任何物且屏障成功→`Already`；补写了 evt(class1) 或 meta(class3)→`Repaired`。**无论哪种，计数一律 `recomputePartition` 由事实链得出权威值，不再手工 ++**（故已存在净 +0、C′ durable-未计数被自动纳入）。同内容已存在重放仍实跑 `Sync` 屏障。
- **墓碑不复活**（spec L111）：`ReplayEvent` 入口 `if s.tombstones.IsTombstone(key)` → 写前拒 `ErrEventForgotten`（新增类型错，distinct from duplicate/IO）；消费者 `context_manager.persistBusEvent` 加 `case IsEventForgotten` 按冲突处置（持有 claim、不 ack、不重投影）。
- **不重盖身份**（spec L33/L59）：`ReplayEvent` 现校验 key 派生分区 vs fact 声明的非零分区，矛盾→`ErrDuplicateEventKey` 明确冲突，绝不静默以 key 覆写。
- **测（`segment_store_commit_oracle_test.go`）**：`TestSegmentStore_ReplayTombstonedEventRefused`（墓碑重放→forgotten、墓碑留存）、`TestSegmentStore_PartitionUnknownUntilRecompute`（§2.5，见下）、`TestSegmentStore_ReplayWrongPartitionRefused`（分区矛盾拒、零写入）。
- **规格驱动的既有测变更（非削断言，如实记）**：
  - `counterexample_test.go::TestCounter_ReplayAfterCacheEvictionClassifiesFromKV`——§2.4 的**官方 fail-before**（原 `t.Skip`，明写「§2.4 移除 oracle 后应 pass-after」）。本轮解除 skip，实测**通过**（冷缓存 `cache.Remove` 后重放→`Already`、计数不增）＝§2.4 真正落地的直接证据。
  - `storage_contract_test.go::TestBarrierFailure_FailsCommitAndSparesCountThroughDecorations`——其 `ReplayRepaired` 枚举断言系 §2.4 前 cache-oracle 遗留；barrier 失败的提交在 KV 已留下完整 idx+evt+meta，重放屏障成功即 durable-完整事实→依事实链判 `Already`（消费者仍由 recompute 得 `before+1`、`refs Len 1`，两项**未动且通过**）。仅枚举词 `Repaired`→`Already` 随 §2.4 语义更新，附详尽注释。

### 2.5 每分区 known/unknown 计数

- `PartitionState.countKnown bool`（`state.mu` 护）；全局 `countsKnown atomic.Bool` 语义**原样保留**（仅 `RebuildLiveCounts` 成功后 true），两门叠加＝belt-and-suspenders。
- `scanLiveKeys(pid)`（从 `RebuildLiveCounts` 抽出，完整记录去重+排墓碑，唯一权威真源）；`recomputePartition(pid)`（单分区重算→置 eventCount+known；扫描失败→unknown，绝不猜值）；`markPartitionUnknown`；`PartitionCountKnown`。
- 提交路径：正常新写 `finishCommit` 保持 ++（known 时准确）；idx/meta/Sync **任一写失败→`markPartitionUnknown`**（持久化不确定，spec L7「可能改变持久状态的失败 SHALL 置 count_known=false」）；修复(class1/2/3)/复用→`recomputePartition` **仅重算该分区**。
- `checkCapacity`：全局门后**循环内按分区** `if !state.countKnown { skip }`——某分区 unknown 只暂停**其自身**淘汰，无关 known 分区照常（spec「无关分区不被错误重算或淘汰」「unknown 不驱动容量淘汰」）。
- 测：`TestSegmentStore_PartitionUnknownUntilRecompute`——新进程仅 ++ 过 → `PartitionCountKnown=false`；`recomputePartition` 后 → true 且计数权威。既有 `storage_contract`(141/150/178) 全局 known 语义、`lifecycle_test`(328-333) unknown 整体暂停、`TestReplayEvent_Classification`、`TestBarrierFailure` 全绿。

**§2 提交核心簇（2.2+2.3+2.4，decision 13）连同 2.5 全部闭环。** 全门 `-race`：`./agent/... ./memory/... ./event/... .`（非-short）+ `./tool/...`（-short 跳真 tmux 环境抖动）全 ok，无跨包回归。

### 2.6 包装链递归透传 + 删 spill GetEvent 弱回退

- **包装链现状已合格**：`engine/engine_bridge.go` 与 `error_tracking.go` 均 `var _ EventReplayer = (*…)(nil)` 编译期断言 + `ReplayEvent` 递归 `inner.(EventReplayer)` 探测（内层非 replayer → 明确报错、不静默），且返回 inner 的 canonical `stored`（非传入副本）→ spec L89「递归透传 + canonical 为下游对象」满足。`ErrorTrackingStore.ReplayEvent` 失败**不 spill**（仅 `StoreEvent` 失败才 `spillEvent`）→ 可靠 inbox 重放错误不再进 mem_spill，无第二 owner（spec L95）。
- **真缺口（本轮落地）**：`MemSpill.ReplayWithNotify` 原有 W1 `GetEvent`+`StoreEvent` 弱回退分支（后端非 `EventReplayer` 时以「读得到」当「提交完成」）。§2.6/spec L99 删除之：改为入口 `store.(EventReplayer)` 失败即 `return 0, err`（能力检查失败、**保留全部 pending 原件**、不消费不重写）。既有 spill 测（`falseNegativeStore` 等内嵌 `*InMemoryStore`）本就是 replayer→走 canonical `Already`，删回退不破（memory `-race` 全绿佐证）。
- **测**：`mem_spill_capability_test.go::TestMemSpill_ReplayWithoutReplayerRefused`——非 replayer stub（转发 9 法不含 `ReplayEvent`）→ Replay 报错、`n==0`、`Len` 仍 1（原件未被弱回退吞掉）。

### 2.7 测绘（四子句，含一处待裁决设计分叉）

| 子句 | 现状 | §2.7 动作 |
|---|---|---|
| ① 引擎索引按 key 幂等、无重复逻辑项 | `vectors/vmeta map[int64]` 以 `EventKey` 赋值**覆盖**→天然幂等无重复项（L398）；`indexedCount` 是**累计健康比**计数（`Remove` 不递减，非 live size，仅喂 `IndexHealth`），非容量真源 | 补回归测锁定「重索引同 key→`Retrieve`/map 无重复逻辑项」 |
| ② repaired/already 不重复增量 | `engineBridge.ReplayEvent` 已对 `Already` **跳过** capacityHook+Index（L120-125）；`Repaired` 触发一次（§2.4 后仅真补写缺失件才判 Repaired，属首次 durable 计入，正确） | 补回归测锁定 Already 不二次触发 hook/索引 |
| ③ 容量观察取已知绝对计数 | `ConsolidationHintTracker.counts[pid]++` 是 **per-process 平行增量**（重启归零、仅算边界事件、纯建议） | **设计分叉——见下，需裁决** |
| ④ feedback 不是确认凭据 | durable inbox 的 ack 凭据是 `PublishReceipt.Durable`（`event_bus.go:415` 入队成功即 durable），**与 feedback 无关**；`writeSettleFeedback`/`BindFeedback` 仅作投影因果标注 | 现状已符合；补测锁定「ack 路径不依赖 feedback 成功」 |

**③ 设计分叉（上报待裁）**：spec L91「容量**提示**依据已知绝对计数」与 `ConsolidationHintTracker` 现「建议式、重启重积累、最多多提示一次可接受」（其注释 L18 明写）存在张力——把 tracker 改为读 store 绝对计数需：(A) store 暴露**每分区**绝对 live 计数（现 `GetStats().TotalEvents` 是全局，非分区），tracker 每次 `Track` 后以绝对值为准、增量仅作触发重读；或 (B) 维持建议式 delta 但显式声明其非计数真源、容量**淘汰**已由 §2.5 绝对 `eventCount` 驱动（tracker 只发提示、不拥有淘汰执行权）。**倾向 B**：淘汰执行权已在 §2.5 `checkCapacity`+`recomputePartition`（绝对、per-partition known），tracker 仅 LLM 建议，符合 D2「执行权在 LLM+工具」哲学；A 需扩 store 每分区绝对计数 API（更大改动，且 `QueryEvents` 按分区枚举受 §2 禁全历史扫描约束）。待下轮 `/opsx:apply` 确认取 B（补测锁定 delta 不驱动淘汰 + 淘汰读绝对）抑或 A（扩每分区绝对计数 API）。

### 2.7 引擎索引幂等 + 不重复增量 + 容量观察取绝对 + feedback 非凭据（用户裁 B）

用户选 **B**（不动 store API 结构，补测锁定 delta 不驱动淘汰 + 淘汰读绝对）。四子句落地：
- **① 索引按 key 幂等**：`InMemoryEngine.vectors/vmeta map[int64]` 以 `EventKey` 赋值**覆盖**（L398）→无重复逻辑项；`Stats().VectorCount=len(vectors)` 为 live size（`indexedCount` 是累计健康比、`Remove` 不递减，非容量真源）。**测** `engine_bridge_idempotency_test.go::TestEngineIndex_IdempotentByKey`：同 key 连索两次→异步 settle 后 `VectorCount==1`。
- **② repaired/already 不重复增量**：`engineBridge.ReplayEvent` 已对 `Already` 跳过 capacityHook+Index（L120-125）；`Repaired` 触发一次（§2.4 后仅真补写缺失件才判 Repaired，属首次 durable 计入）。**测** `TestEngineBridge_AlreadyReplayDoesNotDoubleIncrement`：`StoreEvent`→hook 1、`Already` 重放→仍 1。
- **③(B) 容量观察取绝对**：`ConsolidationHintTracker` 头注新增 §2.7③ 不变量声明——delta 仅建议、MUST NOT 驱动淘汰，淘汰唯一真源＝§2.5 绝对 per-partition eventCount（`checkCapacity`）；delta 重启归零且**提示即复位**（L97）→与绝对真源分叉不致误删。已由既有 `TestCapacityHint_TriggerAndSnooze`（提示即复位/非边界不计）+ §2.5 `TestSegmentStore_PartitionUnknownUntilRecompute`/unknown-skip 锁定，**无需新码**。
- **④ feedback 非确认凭据**：`agent/context_manager.go::writeSettleFeedback` 头注新增 §2.7④ 不变量——仅在 `stored` 已 true 且投影已 append 后执行，`BindFeedback` 失败不撤销提交、不影响 `stored` 返回值（F2 ack 依据）、不影响 inbox `receipt.Durable`（`event_bus.go:415`）；guardrail `negative_feedback_rate` 只作行为信号。

### 2.8 保留租约：强制核心已落，接线待裁（框未勾）

**已落地并测试（memory 层强制引擎，A–D）**：
- `memory/retention_lease.go` NEW：`RetentionLease`（`refs map[int64]int` 引用计数，多持有者＝共享 owner+spill owner；nil-safe）。`Protect/Release/IsProtected/Len`。Release 归零才解护。
- `FileSegmentStore`：`retention *RetentionLease` 字段 + `SetRetentionLease`（**必须在 recovery 重建期、producers 启动前调用**，注释已写死排序约束）+ `IsKeyProtected`/`ProtectKey`/`ReleaseKey`。`DeleteEvent` 于墓碑检查后、取 `mutationMu` 前加 `if s.retention.IsProtected(key) → ErrEventProtected`（不销毁）。
- `errors.go`：`ErrEventProtected` + `IsEventProtected`。
- `LifecycleManager.checkTTL`/`evictOldest`：`IsTombstone` 后 `if lm.store.IsKeyProtected(evt.EventKey){continue}`——TTL/容量不毁 protected 原文；**释放后同 original timestamp 恢复年龄淘汰**（不重盖）。
- `Compactor.finalizeTombstones`：物理销毁前过滤 `dead` 中 protected key（防御，防先已标墓后登记）。
- CompactL1ToL2/L2ToL3 段合并＝**无损搬迁允许**（L109），**不** gate（原文随合并存活）。
- **测** `retention_lease_test.go`：`TestRetentionLease_TTLProtectsUnackedOriginal`（protected 逐期不过期、可 GetEvent；释放后下趟按原时间过期）、`TestRetentionLease_DeleteRefusedWhileProtected`（双持有→删拒→原存；逐一释放→仍拒→全释→删成）。memory `-race` 全绿。**未接线时租约＝nil，所有 gate 惰性 no-op，无假保护。**

**已落地（E1 就绪门控，用户裁 B 的第一步，memory 包内自足）**：
- `RetentionLease` 新增就绪信号 `ready chan struct{}` + `MarkReady()`（幂等）+ `Ready() <-chan`（nil 租约返回已关闭通道）。`NewRetentionLease()` 初始**未就绪**。
- `LifecycleManager.scannerLoop` 首趟破坏性扫描前 `select { case <-store.RetentionLease().Ready(): case <-stopCh: return }`——挂租约的 store 阻塞至恢复 owner 登记完成才跑 `checkTTL`，彻底杜绝“先淘汰后登记”（spec L117-119）。**nil 租约即时放行**→所有非 durable store 与现有 lifecycle 测行为逐字节不变（已证：`TestLifecycleManager_StartStop`/`TestCheckTTL`/`TestProductionWiring_*` 全绿）。`SweepOnce`（harness）仍直调 checkTTL 故意旁路。
- **测** `retention_lease_test.go::TestRetentionLease_ScannerWaitsForLeaseReady`：挂未就绪租约+`Start()`→延迟 50ms→逐期事件**未被墓**（门控保证 checkTTL 未跑，无竞态）；`MarkReady()` 后轮询→按原 timestamp 过期。memory `-race` 全绿。

**待落地（E2 接线）——启动排序已解（B 就绪门控）**：
- **障碍**：store 扫描器在 `wiring.go::buildSharedResource(store,…,MemoryConfig)` 即 `lm.Start()` 并**同步跑首趟 `checkTTL()`**（`lifecycle.go:110`）；而 durable inbox 目录 `BusSpillDir/inbox-v2`（unacked envelope 文件）仅在 `agent.go:377 NewReliableEventBus(cfg.BusSpillDir)`（per-agent，更晚）才知，且 `BusSpillDir` 属 `AgentConfig` 非 `MemoryConfig`。→ **现下“先淘汰后登记”竞跑已真实存在**（首趟 checkTTL 可在 inbox 登记租约前 tombstone 逐期未确认原文）。
- **选项 A**：将 inbox 目录/spill 路径接入 `MemoryConfig`（或 shared-resource build 参），`buildSharedResource` 于 producers 启动前直接从磁盘 envelope+spill 文件枚举未确认 keys→填租约（保持 D5 排序，但需打通 config 且共享 store 跨多 agent 时 spillDir 归属歧义）。
- **选项 B**：推迟 store producers 首次破坏性扫描至租约首次 populate就绪（`lm.Start()` 不立即 `checkTTL`，等一个 lease-ready 信号/首次 populate 后才进循环）；bus/agent-open 时由恢复 owner 从现有 envelope+spill 注册租约后发就绪。保留 store/agent config 分离，但需新增就绪门控与异步 populate 协调。
- **倾向 B**：更符合“恢复 owner 拥有其材料”与 D5 生产者最后启动哲学；A 强行把 agent 级 BusSpillDir 塑进 store 级配置且共享 store 下歧义。**待下轮 `/opsx:apply` 确认 A/B 后一次完成 E（含 rebuild-from-material + protect-before-first-commit + release-after-ack-dirsync/spill-removal + 重启竞跑端到端测）。**

**E2 基础已落（本轮，全量 -race 绿零回归；用户已裁 B）**：“材料保留”为 spec L89 三恢复能力之一，需沿包装链递归透传：
- `memory.RetentionGuard` NEW（types.go：`ProtectKey/ReleaseKey/ArmRetention`，窄能力不扩主接口，符 proposal Non-Goal L44）；`FileSegmentStore.ArmRetention()`（→`retention.MarkReady()`）+ `var _ RetentionGuard`。
- 装饰链递归透传：`engineBridge`/`ErrorTrackingStore` 各加 3 法委派 inner（无则 no-op）+ 编译断言（与 §2.6 `EventReplayer` 透传同款），恢复 owner 可经最外层 `cm.memStore` 抵达内层租约。
- 重建源（只读现有未确认材料，禁第二持久表）：`reliability.Inbox.UnackedMaterial() ([]UnackedMaterial, error)`（按目录列未 ack envelope→slot `prepared_fact.event_key` int64 + `ReceiptKey` hex 原串；leaf 不 import memory/event）；`memory.MemSpill.PendingKeys() ([]int64, error)`。
- **尚待（E2 编排收尾，热路径/泄涌关键，下轮含 e2e 一次完成）**：(a) durable store 开时 `SetRetentionLease(NewRetentionLease())`（未就绪→E1 门控生效）；(b) agent-open 枚 UnackedMaterial(+ParseEventKey receipt)+spill PendingKeys → `ProtectKey` 全登后 `ArmRetention()`；(c) ack dirsync 后 `ReleaseKey`（防泄涌）+fresh prepare 首提交前 ProtectKey；(d) e2e 跨重启保护→ack 释放→按原时间过期。（~~§2.8 保持未勾~~ — 由下文「E2 接线已落地」取代。）

### 2.8 E2 接线已落地（承上文 (a)–(d)，端到端生效；生产码 + e2e 测实证）

上文 (a)–(d) 全部落地于生产码（非仅框内）：
- **(a) durable store 开挂未就绪租约**：`wiring.go:421` `store.SetRetentionLease(memory.NewRetentionLease())`（`buildSharedResource` store 分支）——未就绪租约使 E1 `scannerLoop` 首趟破坏扫前阻塞于 `Lease.Ready()`。
- **(b) agent-open 从 inbox 重建→ProtectKey→ArmRetention**：`agent/agent.go:395-397`（durable 分支）经最外层 `memStore.(memory.RetentionGuard)` `bus.SetRetentionGuard(g)`→`bus.ArmRetentionFromInbox()`（`event_bus.go:424`：枚 `inbox.UnackedMaterial()`→`materialKeys`（fact keys + `ParseEventKey(receiptKey)`）逐个 `ProtectKey`→`ArmRetention()`；枚举失败**不 arm**（绝不以偏全视图欠保护），由 lifecycle 60s 启动宽限兜底防饿死）。spill 带：`ErrorTrackingStore.SetMemSpill`→`spill.SetGuard(guard)`+`spill.ProtectAllPending()`（`mem_spill.go:54`：启动从 spill 文件 `PendingKeys` 重建 protect）；spill append 即 `guard.ProtectKey`（`mem_spill.go:102`）。
- **(c) ack dirsync 后 ReleaseKey（防泄涌）**：Pull 循环 `InboxStateReceipted` 分支（`event_bus.go:632-636`）与 `ConfirmDurable`（`:750-754`）均在 Ack **前** `MaterialOf(env)` 读材料、Ack 目录同步成功后 `releaseRetention(m)`（逐个 `ReleaseKey`）。**设计（tasks 2.8 记）**：未加 runtime-per-prepare protect——prepare 时事实尚未提交（无可护原文），且与 startup-arm 叠加会 double-protect/泄涌；pending 原文保护由 spill-append 承担。
- **(d) 端到端测**：`agent/retention_e2e_test.go::TestRetention_ArmFromInboxAndReleaseOnAck`——真实 localfile `FileSegmentStore`+真实 `reliability.Inbox`+真实 `NewReliableEventBus`：未 arm→`IsKeyProtected=false` 且 `lease.Ready()` 通道关闭（select default 证门未开）→`ArmRetentionFromInbox`→fact+receipt 双 `IsKeyProtected=true` 且门开（1s 内 `<-Ready()`）→`ConfirmDurable`（ack dir-synced）→双 key 释放（无泄涌）。跨重启保护→ack 释放→恢复原年龄处理全链可核对。

**§2.8 至此端到端生效并勾选。** 未接线 store（无 SetRetentionLease）→ 租约 nil→各 gate 惰性 no-op、`ArmRetention` 空转、scanner 即时放行——非 durable 路径逐字节不变（既有 memory/lifecycle/engine 全绿佐证）。

## 2.9 存储门（全关联测试 + 定向 race + 七项覆盖断言）

准出＝跑 §2 提交核心全消费面并锁定七项场景断言；本轮新增热路径无扫描测补上此前缺口的第 7 项，其余复用既有 §2.x 测。

**命令与结果**（`GOFLAGS=-mod=mod`，`-count=1`）：
- `go build ./...` → rc=0；`go vet ./memory/... ./agent/... .` → rc=0；`gofmt -l` 净。
- `go test -race -count=1 ./memory/... . ./agent/` → **6 包全 ok**：memory 3.46s、memory/embedder 9.29s、memory/engine 2.27s、memory/kv 1.71s、root 5.79s、agent 26.86s。无回归、无未知 FAIL。

**七项场景 → 锁定测（逐条实跑 PASS，`-v` 采证）**：

| 2.9 覆盖项 | 测（包） | 结果 |
|---|---|---|
| 冷/热缓存（重放以 KV 事实链判、非 LRU-oracle） | `TestCounter_ReplayAfterCacheEvictionClassifiesFromKV`(memory，§2.4 官方 fail-before→pass-after)、`TestFileSegmentStore_ColdPartitionDiscovery`(memory) | PASS |
| 同键并发原子提交（恰 1 成功） | `TestSegmentStore_ConcurrentSameKeyAtomicCommit`(memory，§2.2 mutationMu) | PASS |
| 压实/删除/封口交错（锁序 + 出锁回调） | `TestCompactor_L1ToL2`/`_L2ToL3`/`_DanglingRefRepair`/`_SealCurrent`、`TestCompaction_FinalizesTombstones`/`_DayAlignedSourceSurvives`/`_SkipsUnsealedSegments`(memory) | PASS |
| 墓碑不复活 + 保护期删拒 | `TestSegmentStore_ReplayTombstonedEventRefused`(memory，§2.4)、`TestRetentionLease_DeleteRefusedWhileProtected`(memory，§2.8)、`TestTombstoneSet_*`(memory) | PASS |
| unknown 恢复（枚举失败不猜值、仅暂停该分区） | `TestLiveCount_UnknownOnEnumerationFailure`、`TestSegmentStore_PartitionUnknownUntilRecompute`(memory，§2.5) | PASS |
| 无 Close 独立进程读回（Sync 屏障原子落盘） | `TestStoreEventDurableWithoutClose`(memory，子进程 `os.Exit(0)` 不 Close→父 fresh store 读回原文+idx) | PASS |
| **普通写无全库扫描断言（本轮新增）** | `TestStoreEvent_NormalWriteDoesNotScanHistory`、`TestStoreEvent_WindowSwitchScanIsWindowBounded`(memory/segment_store_hotpath_test.go NEW，faultKV spy) | PASS |

**新测要点（非空断言）**：`faultKV` 记 `KVScan` 为 `store:read:scan:<prefix>`；对已开窗内稳态新写断 `store:read:scan:`==0，正控 `store:sync`≥1 ∧ `store:write`≥2 ∧ `store:read:`≥1（证提交确实跑，非空转）；配套 `TestStoreEvent_WindowSwitchScanIsWindowBounded` 锁“首开窗 seq 恢复扫至多 1 窗前缀”（即便历史散于多小时/分区，新窗写不逐窗扫）——精确对应 design 决策3「公共新写正常路径不扫描全历史；较贵定位仅在内部恢复/窗首次/不确定修复」。

**范围注记**：`TestStoreEventDurableWithoutClose` 文件内注释仍写“fsync default ON / WAL flush threshold”（早于 localfile 最小化回退）；在 flush-only 语义（tmp→rename 原子快照 + `Sync()` 落盘屏障）下测试仍成立并通过——屏障真实原子落盘、新进程可读回，不宣称掉电/fsync 两模式（推迟 rustviking）。注释陈旧留待 §9 文档统一收敛，不影响本门正确性判定。

→ **§2.9 存储门通过**，§2 全组准出。

## 3. 接收、精确准备与不确定文件写入

### 3.1 严格接收校验 + 精确整数身份

规格真源：`persistent-event-loop`「可靠输入全序持久化」L68「JSON 不可编码、必要身份缺失、nil Message、非法状态在接收前拒绝。有效空输入与非法输入 SHALL 区分」+「大整数相邻身份不被合并」L82-84。三缺口逐一 fail-before→pass-after（`agent/reliability/inbox_validation_test.go`，均先实跑 FAIL 确认缺口真实、修后 PASS）：

| 缺口 | 症状（现状即缺陷） | 修复 | 锁定测 |
|---|---|---|---|
| **A 非法态被消费** | `readEnvelope` 仅查 `State==""`；`NewInbox`/`nextClaimable` `default:` 把任意态当 pending → 磁盘上 `state:"bogus"`（篡改/损坏）被静默认领执行 | `readEnvelope` 加白名单 `switch{pending,claimed,receipted}`，非法→错误（调用方 quarantineFile 隔离，不销毁） | `TestInbox_ReadEnvelopeRejectsIllegalState`（FAIL→PASS）+ `_AcceptsLegalStates` 正控 |
| **B 大整数幂等比较塌缩** | `jsonEqual` 以 `Unmarshal(&any)`→float64 归一后 Marshal 比较：2^53=9007199254740992 与 +1=9007199254740993 float64 同为 …992→判相等→相邻大整数 event_key 的“异 fact”被误认为幂等 no-op（不报冲突） | `decodeExactJSON`：`json.Decoder.UseNumber()`→`json.Number`（串背），重 Marshal 原样输出字面量→精硬保留 int64 身份 | `TestJsonEqual_BigIntAdjacentKeysDistinct`（FAIL→PASS，含 key-order/相同大整数正控）+ `TestInbox_PrepareFacts_BigIntAdjacentFactIsConflict` 端到端 |
| **C nil Message 入接** | `Enqueue`/`readEnvelope` 仅查 `len(SourceEvent)==0`；`json.RawMessage("null")`（len 4）/非法 JSON 经——nil Message 漏过接收 | 新增 `validateSourceEvent`（两处共用）：empty /  TrimSpace==`null` / `!json.Valid` 均拒 | `TestInbox_EnqueueRejectsNilOrMalformedSourceEvent`（FAIL→PASS） |

**区分合法空 vs 非文本有效（C 正控，未误伤）**：`TestInbox_EnqueueAcceptsValidEmptyAndNonTextMessages`——合法空文本 `{content:""}` 与仅图片 `{content:"",parts:[image_url]}` 均为合法 JSON 对象→照收（边界在 nil/坏损，非“空文本即无输入”）；“非文本载荷 MUST NOT 因驱动文本为空被判无输入”属 agent 层选择（§4），接收层在此仅保证不丢弃。

**回归**：`go build ./...`、`gofmt`、`go vet` 净；`go test -race -count=1 ./agent/reliability/ ./agent/ .` **3 包全 ok**（严化校验未破既有 envelope 处理；`srcEvent`/`env` 夹具均产合法 JSON）。本轮仅涉 `inbox.go` 接收/验证路径，未动写入三态（§3.2）与准备版本（§3.5）。

### 3.2 原子写三态结果 + 序号永不回退 + 不确定保留原件

规格真源：`persistent-event-loop`「可判定的接收结果」L50「接收结果不确定 SHALL 与确定未发布失败区分：rename 后 dir sync 失败时保留原件/序号/未决容量，暂停该信箱的新接收至核对完成…分配后的序号 SHALL 永不回退复用」+ design 决策2 L80。

**现状即缺陷（已复现 fail-before）**：`Enqueue` 对任何 `writeEnvelopeFile` 错误均 `in.seq.Add(-1)` 回退序号且不增 pending——但 rename 已落位时（dirsync 失败分支），磁盘上 **原件已存在**；序号回退使下一条 Enqueue 重用同 seq → 写到同 path → **覆写丢失不确定原件**（违反“永不回退”+“阻止覆写”）。`TestInbox_PublishUncertainRetainsOriginalAndNoOverwrite` 首跑 **FAIL 于 L31**（pending=0，未保留未决容量；后续 u2 将覆写 u1 原件）。

**落地（pass-after）**：
- `writeEnvelopeFile` 改返 `(writeOutcome, error)` 三态：marshal/创建/写/fsync/close/**rename 前**失败均 `outcomeNotPublished`（无最终文件）；**rename 落位后** dirsync 失败 = `outcomePublishUncertain`（原件已在盘）；全成 = `outcomeDurable`。区分点：rename 之后不再是“未发布”。
- `Enqueue`：**删 `in.seq.Add(-1)`**——序号一旦分配永不回退，失败留空洞（“允许空洞”，正因此阻止新输入复用旧 seq 覆写）。`outcomePublishUncertain`→`pending.Add(1)`+登记 path+返 `ErrReceiveUncertain`（新类型错，`%w` 包装）；`outcomeNotPublished`→返错但序号仍已消费（空洞）。
- claim/prepare/completion/receipt 四处改写点改 `if _, werr := writeEnvelopeFile(...)`（不改行为，仅适配签名）；不确定即报错，不谎报该阶段成功（屏障所欠→由 §3.3 同身份重试补齐）。
- 注入缝：`var syncDirFunc = syncDir`（默认真实现，行为逐字节不变），供测强制 post-rename 失败而无需真实掉电。
- 消费者（`event_bus.go:486`/`:554` `PublishContext`/`PublishEnvelopeContext`）：`receipt.Durable = true` 仅在 `Enqueue` 返回 err==nil 后执行→`ErrReceiveUncertain` 自然不报 accepted（无需改，已契约正确）。

**pass-after 断言**：首 Enqueue(dirsync fail)→err 非空、seq==0、**pending==1**（未决容量保留）、seq-1 原件 `readEnvelope` 仍在且 request_id==u1；次 Enqueue → seq==2（≠1，不复用）、seq-1 path 仍为 u1（未被覆写）、pending==2。

**回归**：`gofmt`、`go build ./...` 净；`go test -race -count=1 ./agent/reliability/ ./agent/ .` **3 包全 ok**（既有 Enqueue/claim/prepare/completion/receipt 全链测无回归）。`syncDirFunc` 仅测时暂改并即时恢复（单线程测，无并发污染）。

### 3.3 接收/关闭同锁协调 + 相同重试补齐屏障

规格真源：`persistent-event-loop`「可判定的接收结果」L50「关闭检查与接收发布 MUST 在同一生命周期协调内完成」+ scenario「接收与关闭并发…不出现关闭返回后新增未登记项」；「输入处理确认与幂等」/design 决策2 L80「相同 prepare/completion 重试也必须完成所欠屏障，不能因文件内容相同提前成功」。

**（a）不确定接收的同身份核对—设（用户裁）**：选 **A 不额外 suspend**。依据：§3.2 no-rollback 已保证落位的 publishUncertain 原件永不被后续输入覆写；该落位件以 `state=pending` 存盘→`ClaimNext` 正常认领、重开时 `NewInbox` 扫描计入→经正常消费/对账链自然收敛，无需常驻进程内硬暂停（硬暂停无清晰同进程恢复条件、会锤死长驻信箱）。“重试完成所欠屏障”由（c）机制承担。

**fail-before→pass-after**（`inbox_coordination_test.go`）：
- **(b) 接收/关闭同锁**：现状 `Close` 不取 `in.mu`、`Enqueue` 在取锁**前**查 dead → 典型 check-then-act。`TestInbox_EnqueueCoordinatesWithCompletedClose` 经 `testGateHook`（nil 默认、test-only）确定性令 Enqueue 过 fast-check 后、取锁前跑完一个 Close → 旧码仍写盘登记（pending=1、err==nil）→ **FAIL 于 L30**。**修**：`Inbox` 加 `closed bool`；`Close` 于 `in.mu` 内置 `closed=true`（再 `close(dead)`）；`Enqueue` 取锁后权威 `if in.closed → ErrInboxClosed`。二者共 `in.mu`→串行→“要么 Close 前完整登记、要么后来 Enqueue 见 closed 拒”，无“Close 返回后新增”→ **PASS**。
- **(c) 屏障重试**：现状 `RecordCompletion` 对已存相同 completion 直接 `return nil`，不重跑屏障——若首次 rename 落位但 dir-sync 失败（屏障未 fulfilled），重试因“内容相等”跳过→屏障永欠。`TestInbox_RecordCompletion_IdempotentRetryRerunsBarrier`：首次 completion 注 dir-sync 失败（返错、completion 已在盘）→恢复 syncDir、计数置 0→相同载荷重试→**FAIL 于 L74**（syncCalls==0）。**修**：`RecordCompletion` 改为“非冲突（不存在或内容相等）则无论相等与否都 `writeEnvelopeFile` 重跑屏障”（不同载荷仍 `ErrCompletionConflict`）→重试 syncCalls≥1、err==nil→ **PASS**。`PrepareFacts` 本无早退、恒重跑（无需改）；`RecordReceipt` 已 receipted 早退无害（后续 Ack 的 dir-sync 包容该屏障）。`TestInbox_RecordCompletion_DifferentPayloadStillConflicts` 守不同载荷仍冲突（屏障修复未软化冲突判定）。

**注入缝**：`syncDirFunc` 计数 spy（§3.2 已建）+ `testGateHook`（新，nil 默认、生产零开销）；均为本计划 task 1.3 确立的“可注入故障/确定性交错”测试模式同类，不改生产行为。

**回归**：`gofmt`、`go build ./...` 净；`go test -race -count=1 ./agent/reliability/ ./agent/ .` **3 包全 ok**——Close 取锁无死锁（Close 从不在持 mu 内被调；Enqueue 钩在 mu 外；确认链 `confirmDurableAt` 串行调 RecordReceipt/Ack 各自取放锁）。


## 范围变更：localfile 后端最小化回退（用户裁决，非任务项）

用户裁决：localfile 仅为验证 MVP 的临时后端，**不对它做大范围保护性/生产化优化**，非本次引入的补强也可彻底回退；后续接 rustviking 等专用引擎。

- **事实核实**：`memory/kv/local_file_kv.go`（607 行）**全属非本次引入**（工作树未碰）；§2 核心与 §2.8-E1 均**后端无关**（只改 `segment_store.go` 等），零依赖 localfile 专属代码→回退不作废任何已完成工作。
- **代码回退**：`local_file_kv.go` 607→~230 行。新最小模型：内存 map + tmp→**rename 原子快照**（保留原子性→kill 不撕裂），`Sync()`/`Close()` 为落盘屏障；mutate 即时改 map+置 dirty，开进程读快照。**删**：fsync/目录 sync（掉电保证）、WAL 追加与 4MB compaction、torn-tail/损坏 quarantine、deferred-flush 失败重试机、可注入故障系统调用钩子（`fileOps`/`syncDir`/`isUnsupportedDirSync`/`walOp`）。**保 seam**：`KVStore` 6 法 + `Sync()`（屏障）+ `ListPartitionIDs()`（冷分区发现）；`WithFSync`/`LocalFileKVOption` 降为 no-op（保 wiring 不动）；`WalQuarantined`/`LastError`/`Compact` 删（链上 type-assert 可选，缺则优雅回 0）。
- **测**：删 `local_file_kv_durability_test.go`、`local_file_kv_fault_test.go`、`engine/diagnostics_wal_test.go`（均测已移除机制）+ 从基础测删 `TestWalQuarantined_BareKV`。保留 CRUD/Scan/Range/Batch/Persistence/EmptyFile/Concurrent/ListPartitionIDs（均走公共 API）。
- **验证（后端无关核心在最小模型上完好）**：全量 `-race` 11 包绿；真实 localfile 路径 `TestFileSegmentStore_ColdPartitionDiscovery`/`TestBarrierFailure_*`/`TestLocalFileKV_Persistence`/`ListPartitionIDs` 全 PASS——强证 §2 提交核心不依赖被删耐久机。
- **工件降级**（保留未完成项不勾，非按完成归档）：proposal Impact 加 localfile 范围界定条；tasks 9.2（去 fsync 两模式→仅 flush-only 屏障读回）、9.4（去×fsync on/off 完整矩阵）【降级】标注；`resident-release-evidence` 需求改标题为“跨进程验收使用最小 localfile；生产耐久矩阵推迟”+修正“两种持久模式”scenario；`persistent-event-loop` L144 补“最小临时后端、生产耐久推迟 rustviking”。`validate --strict` valid。

## §3.4 冻结完整 canonical Message / 来源快照（2026-09-20，`complete-resident-reliability-protocol`）

- **落点**（design 决策2 L76/L78）：`buildBusFact`（context_manager.go）现仅冻 `msg.Content`——**丢工具字段**（ToolCalls/ToolID）且**丢弃原始 Source 与业务 `Metadata`**（仅拷 3 个硬编码 task 键、与保留控制键混置）。
- **来源快照命名空间化**（event/metadata.go）：新增保留控制键 `MetaKeySourceSnapshot="source_snapshot"` + `SourceSnapshot{Source,Metadata}` 类型 + `Encode/DecodeSourceSnapshot`。`buildBusFact` 将原始 Source + **完整业务 `Metadata`**（map[string]any）作精确 JSON 快照写入该**单一保留键**——业务键不再铺到受信控制命名空间，也不丢失（chat_id/task genealogy 跨重启保真）。键名集中于 event 包。
- **完整消息（必要工具字段）**：`buildBusFact` 现额外冻结 `fullEvent.ToolCalls=msg.ToolCalls`、`fullEvent.ToolID=msg.ToolID`（复用现有 `FullEvent` 载荷字段）。
- **不修改原 role**：`buildBusFact` 原本已 `msg:=*evt.Message` 取副本再归一化→原消息 role 不改（新增回归断言定住）。
- **固定身份/首次归因**：已由 `prepareDurableFacts` 首次冻 prepared_fact、`persistBusEvent` 逐字复用（不重读时间/配置/归因）；不重跑分支属 §3.5。
- **fail-before→pass-after**：`TestBuildBusFact_FreezesFullMessageAndNamespacedSourceSnapshot`（agent，新）——未改前红于 ToolCalls（fact 为 nil）+ source_snapshot 为空；改后 PASS（含“业务键不入控制命名空间”与“原 role 不变”断言）。
- **边界（诚实推迟）**：多模态 `ContentParts`/`ReasoningContent` 无损冻结需 `memory.FullEvent` 模式扩展，归 §4.3（“非文本有效输入不被空 Content 丢弃”）——本任务按设计“使用现有 Response 载荷及必要工具字段”完成；parts 不在此默默丢弃，已登记待 §4.3。
- **验证**：`gofmt` 净；`go build ./...`、`go vet ./event/ ./agent/` 净；`go test -race ./event/ ./agent/ ./agent/reliability/ .` 4 包全 ok（无回归，agent 27.1s）。未提交。validate --strict 待本步末复跑。

## §3.5 prepared_version + 完整准备校验 + 删除重生 key 弱回退（2026-09-20，`complete-resident-reliability-protocol`）

- **prepared_version 门禁（inbox.go）**：`MessageSlot` 新增 `PreparedVersion int` + 常量 `PreparedVersionCurrent=1`；`PrepareFacts` 冻结时将落位槽盖章为当前版。`readEnvelope` 新增：槽有 `prepared_fact` 但版本≠当前 → **拒读**（→ 调用方 quarantine），旧解析器不保留、不兼容过渡材料不静默消费；无材料 pending（版 0、fact 空）合法→正常首次准备。
- **删除重生 key 弱回退（context_manager.go）**：`persistBusEvent` 对“有 prepared_fact 但解码失败/不完整”分支，原 `fullEvent = cm.buildBusFact(evt)`（会铸新 key→同一输入以新身份双写）→ **删除**，改为 **gate（return false）+ 报错**：当前格式损坏保留报错、claim 回放、不默默入事实链。新增 `isCompletePreparedFact`。
- **完整性不变量修正（重要判断）**：首版 `isCompletePreparedFact` 额外要求 summary/agent_name 非空→假阴 3 个真实耐久测（`TestDurableReceipt_*`、`TestCounter_PartialInputReplayFailureGatesCommit`，其 fixture cm 无名→agent_name 空）——真实数据被错gate。收敛为**唯一可靠不变量 `EventKey != 0`**（buildBusFact 总铸非 0 snowflake）：既抓真损坏（`{}`/截断 JSON→key 0），又不会误 gate 合法耐久事实（fail-closed 对损坏正确，但误伤真数据更糟）。
- **fail-before→pass-after**（行为切换实跑红→绿）：`agent/reliability/inbox_prepare_version_test.go`（`TestInbox_ReadEnvelopeRejectsIncompatiblePreparedVersion` 红于 L46——旧码无版门禁→非法件被静默消费；`TestInbox_PrepareFactsStampsCurrentVersion` 守卫）；`agent/persist_prepared_gate_test.go`（`TestPersistBusEvent_GatesUndecodablePreparedFact` 红于 L39——旧码重生 key 返回 true→真数据双写；另有 incomplete-gate + complete-verbatim-reuse 两测）。恢复修正后均转绿。
- **验证**：`go build ./...`、`go vet` 净；`go test -race ./agent/reliability/ ./agent/ .` 3 包全 ok（无回归，agent 26.6s；含上三个一度回归的耐久测回绿）。未提交。边界：不兼容/quarantine 材料的**显式重置流程**属 §3.7。

## §3.6 批量准备耐久门 / key 保留原子 / 不确定失败不提前释放租约（2026-09-20，**部分完成 3/3 子句，未勾**）

### 已落地：子句③ 不确定清理保留租约（主 gap，已修）
- **问题（spec L96/L110）**：`Ack` 中 `os.Remove` 成功但 `syncDir` 失败时，文件已删但删除**未崩持久**；旧码直接 `return err` 且无独立清理账目，后续对已缺失文件的重试 `Ack` 在 `os.IsNotExist` 分支**直接 return nil、不再补目录屏障**→ 未确认容量/租约释放永久搁浅（同进程内该文件已不再被 claim、无人再调 Ack）。
- **修复**：`Inbox` 新增独立清理账目 `cleanupOwed map[path]owedCleanup{requestID, material}`；`Ack` 的 dir-sync 改用 §3.2 的 `syncDirFunc` 缝；删除后 sync 失败→**保留容量与租约**（pending 不减、不开释放）+登记 owed→报不确定错；对已缺失文件的重试，仅当 owed 有账时**补完成屏障**→ `finalizeCleanup` **恰一次**释放容量；无账→完全 acked、不二次减。`DrainCleanups()` 逐期补屏障并返回受保护材料；`EventBus.DrainRetentionCleanups()` 接回 §2.8 `releaseRetention` 恰一次释放租约；消费循环 `runEventLoop` 每轮驱一次（event_loop.go:65）。
- **fail-before→pass-after**：`agent/reliability/inbox_cleanup_account_test.go`——`TestAck_UncertainDirSyncRetainsCapacityThenRetryCompletesBarrier`（行为切换实跑红于 **L53**：旧码屏障不补→容量 leak 卡在 1）；`TestAck_OwedBarrierStillFailingKeepsCapacityAndLease`（sync 仍失败时 drain 不释放/不误释 + 恢复后释放恰一次且带 fact key 555）。恢复后转绿。`go test -race ./agent/reliability/ ./agent/ .` 3 包全 ok（reliability 9.9s/agent 27.0s/root 7.0s）。

### 已核实：子句② key 保留与准备文件同一耐久写
- `PrepareFacts` 将 `env.ReceiptKey=receiptKey` + 各槽 `PreparedFact`/`PreparedVersion` 在**单次 `writeEnvelopeFile` 原子重写**中落下（§3.2 三态）——receipt key 永不脱离准备文件单独存在、反之亦然。子句②成立，无需新码。

### 深入分析发现的新问题（子句① 未解决，属设计分叉→上报）
- **问题场景**：一批跨多 envelope 时，`prepareDurableFacts`（lifecycle.go:276-327）逐 path 循环：envelope A 的 `PrepareEnvelope` 成功→**立即将 prepared_fact 盖回 A 的 live claim**（321-326）；随后 envelope B 失败→`return false`（315-317）。但 `runEventLoop`（event_loop.go:85-98）**无条件**对每个 claim 调 `persistBusEvent`：A 的 live claim 已带 prepared_fact→**写入 A**，而 B/C 未写→**部分批次提交**，违反 L88「整批全部 envelope 准备耐久后才允许首条事实写」。
- **问题根源**：`allDurableStored = prepared` 算出但从未用于门住 persist 写入；且 prepared 为 false 时仍走到 BuildInvocation+RunFlow，MemoryPlugin 经 `FactsPrePersisted=false` 的 echo 路径**另写**合并输入（F2）——双重写机制交叉。
- **影响范围**：部分耐久写使 A 入链、B/C 丢失至重启（claimed 不被 ClaimNext 重取→僵尸），与全序/原子语义相悖。
- **为何不直接改**：正确的 all-or-nothing 需同时定下“瞬时 I/O 失败 vs 确定冲突”的处置——瞬时→本轮回退重试不跑模型（L90 100/200/400ms 退避，不僵尸化）；确定冲突→隔离原件 + **停止该 agent 自动消费**（L7）——后者属 §3.8 接收门；且需与模型调用序（L90“提交成功后才调用模型”）及 F2 echo 路径语义共同裁定。单一 1-line gate 会要么僵尸化要么无限重放。→ 需裁决后才实现，不臆测。
- **§3.6 因此不勾**；子句① 待与 §3.8 合并设计。

## §3.7 启动只加载当前格式 + 移除排空迁移/兼容读取 + 受管一次性重置（2026-09-20，`complete-resident-reliability-protocol`）

- **用户裁决**：“按建议直接实现①②③④”；重置为显式 operator 动作，默认绝不自动删。
- **A/B 启动只加载当前格式 + 移除排空前置**（inbox.go）：删除 `checkUpgradeGates` 与其两个 boot-block 错（`ErrLegacySpillNotDrained`/`ErrLegacyInboxV1NotDrained`）；新 `classifyTransitional(parent)` **只读**扫描遗留 `.spill`/`inbox-v1/*.json`，归为惰性过渡数据（不读不消费不删）→**不再阻启动**；`checkQuarantineDispositioned` **保留**（当前格式损坏仍 fail-loud，不默默忽略）。`Inbox.transitional` 存检测结果，`TransitionalData()` + `EventBus.TransitionalData()` 透传供 bootstrap 报告。
- **死码删除**：`agent/reliability/spill.go`（SpillStore 生产无消费者）+ `spill_test.go` 删；`fault_injection_test.go::TestFaultInjection_SpillConcurrentStress` 删；`spillExt`→`spillFileExt`（inbox.go）。`tagent_evolution_wire_test.go` 注释修正（目录由 NewInbox 建，非 NewSpillStore）。三旧 boot-block 测→§3.7 语义（inert 不阻、读取只读）；`tests/upgrade_rollback_drill_test.go` 升级 drill→“inert+受管重置”重写（rollback drill 不动）。
- **C 受管一次性重置**（inbox.go `ResetTransitional`）：**唯一破坏路径**，守护：需显式 confirm（否则拒）；closed 或 pending>0（存活 writer）拒（独占写权）；**仅删 open 时枚举的 legacy 文件**，allow-list 守护 `underDir(p, leafParent) && !含 inbox-v2` →**永不触 v2/quarantine/叶外路径**（决策10 “路径未知/非受管→停止”）；legacy 与当前事实不相交→删它不留悬空引用（一致恢复单元已满足，当前事实格内无关可留）。`EventBus.ResetTransitional` 透传。
- **D 三方分类（不兼容 vs 当前损坏 vs I/O）**：不兼容→inert、可受管重置；当前损坏（quarantine）→fail-loud **不触发清空**；普通 I/O →报错不删。`TestQuarantineCorruptionStillBlocksDespiteTransitional`=**真 fail-before**（旧码 spill 门先命中→掩蔽损坏分类；新码忽略 spill→quarantine 错浮现）。
- **fail-before（新能力守卫）**：`inbox_transitional_reset_test.go`——unconfirm 不删、pending>0 拒、v2 永不入分类（故不可被重置误删）、quarantine-仍阻。均 PASS。
- **验证**：`go build ./...` 净；`go vet ./...` 净（含 tests 包全部编译）；`go test -race`：reliability 7.6s/agent 26.2s/root 4.0s/tests-drill 1.7s 全 ok，无回归。未提交。边界：§3.6-① 仍待与 §3.8 合并；若未来需重置**当前格内**跨存储单元（非本次遗留情形），`ResetTransitional` 留为 owner-hooked 扩展点。

## §3.8 接收门（§3 可靠性叶验收）（2026-09-20，`complete-resident-reliability-protocol`）

§3.8 是**验收门**（运行全部 inbox/bus 关联测 + race 并逐项核销 6 条性质），非新增生产码。六性质均有专项测且 race 绿：

| §3.8 性质 | 覆盖测（包） | 结果 |
|---|---|---|
| 全字段往返（无损恢复） | `TestReliableBus_FieldRoundTrip_Lossless`(agent) + `decodeSourceEvent` | PASS |
| 准备失败零事实写 | `TestCounter_PartialInputReplayFailureGatesCommit`、`TestPersistBusEvent_GatesUndecodable/GatesIncompletePreparedFact`、`TestDurableReceipt_StoreFailureKeepsClaim`(agent) | PASS |
| 序号不覆写 | `TestInbox_PublishUncertainRetainsOriginalAndNoOverwrite`(reliability，§3.2) | PASS |
| 源 role 不变 | `TestPersistBusEvent_SystemRoleNotMutatedInPlace`、`TestBuildBusFact_*`(agent，§3.4) | PASS |
| 旧格式不能被正常运行读取 | `TestInbox_LegacySpillIsInertNotBlocking`、`_LegacyV1DirIsInertNotBlocking`、`_ReadEnvelopeRejectsIllegalState/IncompatiblePreparedVersion`、`_UnknownVersionQuarantined`(reliability，§3.7/3.1/3.5) | PASS |
| 当前格式故障原件不被清除 | `TestInbox_CorruptItemQuarantined`、`_QuarantineUndispositionedBlocksReopen`、`_QuarantineCorruptionStillBlocksDespiteTransitional`、`_Transitional_NeverClassifiesCurrentV2`、`TestResetTransitional_*`(reliability) | PASS |

- **全部关联测 + race 实跑**：`go test -race ./agent/reliability/` 全包 9.7s ok；`go test -race ./agent/` 全包 26.2s ok；`go test -race .` 7.1s ok；`./tests/` drill 1.7s ok。零回归。
- **§3.6-① 仍未关（诚实标出，不充数）**：§3.8 的「准备失败零事实写」在**单 claim 粒度**成立（无 prepared_fact 的 claim→persistBusEvent 不写）已验；但 §3.6-① 的**整批 all-or-nothing**（一批内某 envelope 准备失败→A 也不写）无专项测、且现码会写 A（本会话查实）——该批粒度属 §3.6-①（用户裁「留待后续」），待 prepare-outcome 分类（瞬时退避重试 vs 确定冲突隔离停消费）裁决。故 §3 叶除 §3.6 外均闭；**§3 整体出口因 §3.6-① 未完全干净**，不将 §3 宣为全部完成。**【后续更新 2026-09-20：§3.6-① 批粒度 all-or-nothing 已于 §4.2 结构化提交门闭合（见 §4.2 节 + `submit_durable_batch_test.go`），§3.6 已勾——§3 至此全部干净出口。】**

## §4.1 冻结原始消费集（不覆盖原集合）（2026-09-20，`complete-resident-reliability-protocol`）

- **落点**（design 决策4 L106）：“一次 Pull 立即冻结原始集合；原集合不被 meditation 过滤覆盖；处置在其上另行计算”。现 `runEventLoop` 以 `events = dropMeditationFromMixedBatch(events)` **覆写**工作集 → 部分丢弃时，让路 meditation 的**已 claim durable envelope 不在 `events`** → finishDurableBatch(events) 不 receipt/ack 它 → **僵尸化**（每次重启重 claim→再空跑）。原仅处理了全丢弃（len==0）分支（旧注释自述“对空 provenance 幂等”），未盖部分丢弃。
- **修复**（event_loop.go）：drop 前先 `received := events` 冻结原始消费集；`selected`（drop 后）仅用于 BuildInvocation/持久事实/模型输入（处置）；三处 `finishDurableBatch(events)`→`finishDurableBatch(received)`——receipt/ack provenance **恒走完整消费集**。让路 meditation：无事实写（skipped不入库）但**仍被 ack 消费**（与“让路不重新发布、新异门下个 idle 重评”一致，不靠留 inbox）。32-envelope 上限（`maxClaimPerPull`）与唯一消费入口（Pull/claimDurable，F5 BeforeModel 不中途 claim）保留。
- **fail-before→pass-after**：`agent/meditation_durable_zombie_test.go::TestRunEventLoop_YieldingMeditationEnvelopeConsumedNotZombied`（真实 durable bus + 循环）——将主 finish 改回 `events`（预§4.1）时红于 **L55**：“1 durable envelope left un-consumed…yielding meditation not receipted”（pending 卡 1）；用 `received` 后 pending→0 PASS（并断言让路 meditation 不入事实链、selected user 入）。`-race` 回测：本测 + `RunEventLoop_DurableBatch`/`ABOneTurn`/`BeforeModel`/`DropMeditation` 全 ok（1.8s）。
- **边界（诚实）**：“保存 selected/skipped 逐槽处置（processed→精确 fact key / skipped→稳定原因）”的结构化 completion（`completion_version=1`）属 §5 决策4 L122 完成固化；§4.1 仅交付“集合不被覆盖 + 处置分离计算 + 无僵尸 + 32 上限 + 单一入口”。§4.2/4.3（结构化提交结果 + 全 selected 提交成功才进模型 + 删插件 echo 回退）为下一聚焦单元，亦是 §3.6-① 批粒度闭合点（同一 prepare/submit-outcome 状态机）。

## 代码评审回修（CodeReview 子代理 → 1 Major + 2 Minor）（2026-09-20）

对 §3.1–§3.8/§4.1 工作树改动派单一 CodeReview 子代理（completeness/correctness/impact 一并）；其结论逐条经源码验证后均确准确。修三项（均属已勾任务的真实缺陷加固，不新增 checkbox）：

- **Major #1（§2.8 保留租约释放旁路/死码）**：`nextClaimable` 在 `InboxStateReceipted` 分支就地 `removeAndSync`+`pending.Add(-1)`（无 releaseRetention），且因此 `ClaimNext` **永不返回 receipted** → `claimDurable` 的 receipted 释放分支（`MaterialOf`+`Ack`+`releaseRetention`）为**不可达死码**。又 `UnackedMaterial()` 含 receipted 文件→启动 `ArmRetentionFromInbox` 保护它们→被扫掉后永不释放（有界、跨重启自愈、不丢数据，但远反 §2.8 恰一次释放）。**修复**：（A）nextClaimable 改为**返回** receipted项，ClaimNext 对 receipted **不改写**（不重 claim、不 Attempts++），使 `claimDurable` 既有正确释放分支转为活码；（B）新增叶子 `MaterialOfRequestID`，`ReconcileReceipted` 在 ack 前捕获材料、confirm 成功后 `releaseRetention`；（C）删除因本次变更而孤立、且无引用的叶子 `removeAndSync`。**fail-before**：`retention_e2e_test.go::TestRetention_ReceiptedSweepReleasesLease`（将 nextClaimable 改回旧静默 sweep 时红于“receipted sweep MUST release the fact original (no lease leak)”，IsKeyProtected 仍 true）+ `TestRetention_ReconcileReceiptedReleasesLease`（启动对账路径释放）。契约变更波及 `TestInbox_Reopen_RequeuesClaimed_KeepsReceipted`：该测固化了**旧错误契约**（ClaimNext 内部静默扫 receipted），按新契约重写断言（b 以 receipted 原样 surfaced、不被重 claim、由 caller Ack、pending 精确）——非削断言而是修正被固化设的旧行为。
- **Minor #2（§3.1 非 nil Message 未在接收边界强制）**：`validateSourceEvent` 只拒整体 null/empty/非法 JSON，但合法对象携 `"message":null`/无 message 字段仍通过 → `buildBusFact` 顶格 `msg := *evt.Message` 无 nil 守卫→写前准备屏障 panic。**修复**：接收边界解码 `{type,message}`，`type==external_input` 且 message 缺失/null → 拒；**另** `buildBusFact` 入加 `evt==nil||evt.Message==nil → return FullEvent{}`（EventKey==0 → §3.5 gate 丢弃）作纵深防御。fail-before：`inbox_validation_test.go::TestInbox_EnqueueRejectsExternalInputWithNilMessage`（预修复：typed-external-input+null message 为合法 JSON→Enqueue 成功→require.Error 红；含同类型带合法 message 的正控）。
- **Minor #3（`RecordReceipt` 注释谎称 completion 为必输入屏障）**：`ConfirmDurable` 实为 receipt→ack→release，**不接收/不调** `RecordCompletion`（后者仅测调用）。改注释明言屏障尚未接线、推退至 §4.x，不谎报已落地。
- **全量 `-race`**：`./agent/reliability/` 9.7s ok（含新 3 测）· `./agent/` 27.1s ok（契约变更无回归）· 根包 4.86s ok · `./tests/` drill 子集 1.7s ok（含 §3.7 改的 `UpgradeTreatsLegacySpillAsInertThenResets`）。`go vet ./...` 净。TEMP-FAILBEFORE 残留均为 0。**注**：`./tests/` 全包超时源于**预存在**的 `*_llm_test.go`/`*_e2e_test.go` 真联网 OpenAI `RoundTrip`（无 key/网→阻塞至 timeout），与本轮回修无关，非回归；离线验证以不依赖外网的关键集为准。

## §4.2 结构化提交门（关 §3.6-① 批 all-or-nothing）（2026-09-20，`complete-resident-reliability-protocol`）

热路径重构：`runEventLoop` 的耐久提交不再是“bool prepare + 无条件逐条 persist + 无条件进模型”，而是一道返回**分类结果**的提交门（design 决策4 L54/L60、spec L88/L90）。

- **结构化 outcome**（lifecycle.go）：删 `prepareDurableFacts(events) bool`→新增 `submitDurableBatch(ctx, events) submitOutcome{status∈{submitOK, submitTransient, submitConflict, submitCancelled}, conflict string}` + `prepareBatchFacts(events) (submitStatus,string)`（Phase1 全准备）与 Phase2（全 persist）两段。**分类**靠新增 sentinel `reliability.ErrPrepareConflict`（包裹 PrepareFacts 的 not-claimed / facts-slots-mismatch / slot-already-frozen-different 三确定冲突点；`ErrReceiptKeyConflict` 同类），`errors.Is` 判为 submitConflict；其余 PrepareFacts 错（prepare read/write I/O）与 marshal 失败外的持久错→submitTransient；marshal 失败→submitConflict（确定性损坏）；ctx.Err→submitCancelled。
- **批 all-or-nothing（§3.6-① 闭合）**：Phase1 任一 envelope 未全准备（conflict/transient）→**Phase2 不跑→零事实写**（旧码 A 先 prep 后 B 失败仍写 A 的部分提交 bug 根除）。让路/未选不属准备失败（由 §4.1 处置分离另走）。
- **runEventLoop 按 status 分派**（event_loop.go）：`submitOK`→建执行凭据前置标记→BuildInvocation→模型；`submitConflict`→`QuarantineEnvelope` 隔离原件（留盘供人工处置、不销毁）+ `return` **停止自动消费**（fail-closed，不越过冲突猜测）；`submitTransient`→`submitDurableBatchWithBackoff` 同批同身份 100/200/400ms 封顶退避，耗尽后 `releaseBatchClaims`（新增叶子 `Inbox.ReleaseClaim`：claimed→pending 回写）requeue，**不跑模型、不取下一批**（严格 seq 序→oldest  stuck 先重 claim→天然背压）；`submitCancelled`→保留 claim 退出。`plugin.DurableInbound` 首信封构造保留（§4.4 目标），但 FactsPrePersisted 现仅在 submitOK 后无条件置 true（部分/失败提交已不再进此点）。
- **新叶子/透传**：`reliability.ErrPrepareConflict`；`Inbox.QuarantineEnvelope(path,reason)`（持锁、移 quarantine、释放容量与 pathsByRequestID）+ `Inbox.ReleaseClaim(path)`（持锁、claimed→pending 幂等）；EventBus 同名透传。
- **fail-before→pass-after**：`agent/submit_durable_batch_test.go` 三测（真实 durable bus + 预环一信封的 receipt_key 造确定冲突）：① `TestSubmitDurableBatch_AllOrNothingOnConflict`——将冲突分支改为 fall-through 旧行为时红于 **L60**（A 泄入提交 + 分类崩为 Transient），gate 后 status=submitConflict+conflict=pathB+投影空（A 未提交）PASS；② `TestSubmitDurableBatch_TransientNotConflict`（缺失路径 prepare→读 I/O→Transient非Conflict、不隔离）；③ `TestReleaseBatchClaims_RequeuesForOrderedReclaim`（released claim 下次 Pull 有序重 claim、不丢）。`-race`：本 3 测 + 全 `./agent/` 27.2s ok + `./agent/reliability/` 9.7s ok，零回归。
- **边界（诚实）**：§4.2 只关提交门与“失败不跑模型”（L90）。§4.3 的“全 selected 提交成功才进 runner”正向门已在 §4.2 实现（模型仅 submitOK 后）；当时尚余的“删插件回退”+“非文本不被空 Content 丢弃”已于下一节 §4.3 闭合。（§4.4 首信封/整轮标志、§4.5 执行凭据核验待后续。）

## §4.3 非文本有输入全链生存 + 插件回退移除（2026-09-20，`complete-resident-reliability-protocol`）

四性质逐关：

- **① 全 selected 提交成功才进 runner**：由 §4.2 提交门实现（`runEventLoop` 仅 submitOK 后达 BuildInvocation/RunFlow）。
- **② 删“失败后插件另写合并事实”回退**：§4.2 重构已删旧 `allDurableStored` 条件（失败不再进模型且仅 submitOK 无条件 `FactsPrePersisted=true`）→ “失败→false→MemoryPlugin echo 写”路径行为上不可达（无代码再置 false）。插件 `memory_plugin.go` L173 `if hasDurable && FactsPrePersisted { skip }` 为唯一 durable 写路径（已预落库→跳过）；volatile（hasDurable=false）仍走插件写（正确）。无 volatile/durable 混合旁路，无需改插件码（回退是 §4.2 环侧条件，已消失）。
- **③ 全 skipped 不执行模型**：让路后 selected 为空→§4.1 finish received 不跑模型；且空 invocation 门（真无文本无 parts）仍 `continue` 不跑。
- **④ 非文本不被空 Content 丢弃（§3.4 deferred 的多模态无损冻结就此闭合）**：端到端链——（a）`memory.FullEvent` 新增 `ContentParts []model.ContentPart json:"content_parts,omitempty"`（additive，旧事件 JSON 解码不变）；（b）`buildBusFact` 冻结 `ContentParts: msg.ContentParts`→图片输入无损入事实链；（c）`resolveRef` 捕获 `evt.ContentParts` + 解析条件加 `|| len(evt.ContentParts)>0`（图-only 不再回退到 summary）+ `renderTimelineMessage` 新增 `contentParts` 参、default(user) 分支携 parts→图片达实际请求；（d）`BuildInvocation` 除 text 外收集 selected 的 parts（图片-only→invocation 非空）；（e）循环空门 `if msg.Content == ""`→`&& len(msg.ContentParts) == 0`→图-only 不再被当空丢。
- **fail-before→pass-after**：`agent/multimodal_survival_test.go::TestBuildBusFact_FreezesMultimodalContentParts`（预修复 fact.ContentParts 空）+ `::TestBuildInvocation_KeepsImageOnlyInput`（预修复 parts 丢→空 invocation）；`agent/compress/multimodal_render_test.go::TestResolveRef_RendersImageOnlyInputParts`（将 `contentParts = evt.ContentParts` 改回 nil 时红于 **L41** “image parts must reach the rendered request, got []”）。（修测试 fixture：GetEvent 按 snowflake key 推 pid，需用 `NewSnowflakeEventKey(1,…)` 非裸 200）。
- **附带修飘测**：`TestRunEventLoop_ABOneTurnCNextTurn`（reliability_boundary_test.go）预存在 publish-vs-Pull 时序飘（先起 loop 后 publish A/B，-race 下首 Pull 偶只 claim A）——改为 publish-before-loop（Publish 同步落盘），首 Pull 确定 claim 两件；断言（A+B 首 turn、C 下 turn）不变。`-count=10 -race` 绿。
- **全量 `-race`**：memory 3.3s / compress 4.1s / reliability 9.7s / agent 27.1s / root 5.4s 全 ok，`go vet ./...` 净，TEMP 残留 0。§4.3 四性质均关。

## §4.4 前提 grounding：真实框架回显形态（e2e harness，test-only）（2026-09-20）

上一轮判定 §4.4 不可盲写（每新增一个谓词条件都是 potential under-skip→每回合双写）。本轮采 option 1：建真实框架 e2e 观测器（`agent/echo_grounding_e2e_test.go`，零生产码变更）——用真 `llmagent`+`runner.NewRunner(...,WithPlugins(recorder))`+mock 模型跑一 turn，在**插件 OnEvent 钩子层**捕获回显形态（非 RunFlow 投递环——已证实输入回显不在投递流、仅在插件钩子）。

**实测 ground truth（`TestEchoGrounding_PluginHookReceivesInput`，-race 3× 稳定）**：

| obs | role | author | root | parentInvocationID | content |
|---|---|---|---|---|---|
| 输入回显 | `user` | **`user`** | **true** | `""` | 与 `BuildInvocation` 合并产物逐字一致（`msg-A\n\n---\n\nmsg-B`）|
| 输出回显 | `assistant` | 代理名 | true | `""` | `ok` |

→ **§4.4 谓词所需全部字段均在真回显上成立**：根调用（无 parent）、`author=="user"`、消息内容==提交时合并的 canonical。之前担心的 under-skip（图不匹配→不跳过→双写）**不存在**——这些条件可安全地加入。子调用/工具回合会有非空 parent（被 root 检查排除）；assistant 回显 `author≠user`+role 不同（不误匹）。→ **§4.4 现已解锁（安全可实现）**。harness 作为永久特征化测保留（兼 §4.8 真实框执行门地基）。本轮无生产码变更，agent 全量 `-race` 27.2s ok。

## §4.4 落地：精确尝试级回显凭据（per-attempt EchoCredential）（2026-09-20）

**旧机制**（均删除）：`plugin.DurableInbound{Path,RequestID,DedupKey,FactsPrePersisted}` 值型 ctx + 整轮 `firstDurable`（首信封）打标 + 插件 `if hasDurable && FactsPrePersisted && role==user → 跳过任意 user`（会话钩子打标假设）。

**新机制**：
- `plugin.EchoCredential{AttemptToken, Agent, Session, MergedMessage, CommittedKeys}` + **指针入 ctx**（`WithEchoCredential`/`EchoCredentialFrom`）；内含 `mu/boundID/boundSet` 供首次匹配绑定确切 Event.ID（`Bind(evt.InvocationID)`，幂等）。
- 事件环 submitOK 且**确有已提交事实**（`durableCommittedKeys(events)>0`，从各 `claim.PreparedFact` 解出 `FullEvent.EventKey` 去重）才装 `cm.turnEcho` volatile 批无 claim→不装→插件正常存）。**无首信封概念**。
- `RunFlow` 每尝试从 spec 铸一份 **新 `*EchoCredential`**（`newAttemptEchoCredential`，`attemptSeq.Add(1)` 唯一 token）→同业务 turn 重试 fresh token 重用同批 keys；凭据随 ctx 结束释放，**无全局/整轮状态**。
- `MemoryPlugin.isExpectedInputEcho`：在**任何 key 分配/写入之前**（上提至 degenerate-empty 检查后）判：`inv!=nil && inv.GetParentInvocation()==nil && evt.Author=="user" && role==user && TrimSpace(content)==TrimSpace(cred.MergedMessage)`。全部基于实测 ground truth。四个“移除”均关：首信封✓、整轮 user 跳过标志✓、会话钩子打标✓、存储前识别✓。
- agent/session 携于凭据供 §4.5 执行核验；**不**参与跳过谓词（per-call ctx 已天然隔离会话/代理；额外加 agent 名比对反有 under-skip 风险，无收益）。

**fail-before / 回归**：`plugin/memory_dedup_test.go` 全重写至真根调用（`&agent.Invocation{}`）+ Author "user"：ExpectedRootEcho_Skipped（不匹→存）、DifferentUserContent_NotSkipped、AssistantOutput_StillStored、NonRootInvocation_NotSkipped（inv=nil）、NoCredential_Unchanged。五测均验“真回显跳过 + 其余不误跳”双向。

**重要发现（测试 harness 痑）**：`ABStoredCDeferred` 与 §4.1 zombie 测均用 **双 cm 反模式**（另 `newTestContextManager` 与 `ta.contextManager` 不同），persistBusEvent 写 ta.contextManager 存储/投影、而 RunFlow 跑在另一 cm → 旧代码凭据从未注入（prod cm==ta.contextManager 才正确），测试里“合并回显双写”反而喂了该 cm 的存储使断言以**错误理由**通过（恰是 §4.4 要消除的双写）。改为单一 `cm := ta.contextManager`（与 prod 一致）后：persistBusEvent 喂同一 cm 投影→模型看到输入（“system-only”警告消失）且无合并重复。额外在 ABStoredC 加“单一 fact 不得同时含 A+B”断言正锁 §4.4 无双写。（确认：`persistBusEvent` 自身 `cm.projection.Append(ref)`→ durable 输入经持久路径而非回显喂模型，§4.4 跳过不饿模型。）

**全量 `-race`**：plugin 3.0s / reliability 9.0s / memory 3.7s / compress 4.4s / agent 27.2s / root 5.4s 全 ok；`go vet ./...` 净；无残留 `DurableInbound`/`FactsPrePersisted` 码引；validate --strict valid。§4.4 关。

## §4.5 前提 grounding：模型入口时序与 iterator 偏好（e2e harness，test-only）（2026-09-20）

§4.5 三部分（A 实际模型入口执行凭据核验 / B 装饰器保留底层 Model+IterModel 真实能力不伪造 / C 惰性 iterator 未开始迭代前不消费恢复提示）。同 §4.4 先例，先 ground 两个不能猜的框架事实（`agent/model_entry_grounding_e2e_test.go`，零生产码）。

**实测 ground truth**（一个同时实现 `GenerateContent`+`GenerateContentIter` 的模型 + 一个记录根 user 回显的插件，真 runner 跑一 turn，`-race 2×` 稳定）：

```
call order: [plugin:user-echo, model:iter-created, model:iter-first-next]
```

| 假设 | 结果 | 对 §4.5 的含义 |
|---|---|---|
| **(A) 回显绑定早于模型入口** | 插件 `user-echo` 在 `model:iter-created` **之前** | 模型入口凭据门安全：happy path 已绑定→不误block；入口仍未绑定=真异常→可安全 block |
| **(B) 框架优先 iterator** | 框架**确实**走 `GenerateContentIter`（非 channel） | 仅实现 `GenerateContent` 的 `SwappableModel`/`TrajectoryRecorder*` **积枘隐藏一个正在用的能力**（强使框架回退 channel+goroutine）→(B) 是真回归非理论 |
| **惰性语义** | `iter-created` 与 `iter-first-next` 分开触发 | 装饰器生成 iterator 时**不得**触底模型（无 lease/goroutine/副作用）；仅首次 Next 时调→同时满足 (C) 恢复提示延迟消费 |

→ **§4.5 三项均已解锁（安全可实现）**。harness 作为永久特征化测保留（兼 §4.8）。本轮无生产码变更，agent 全量 `-race` 27.1s ok。

**下一步实现计划（decision-complete）**：
- (A) 新增一个执行门装饰器包 `cfg.Model`（`NewContextManager`+`RebuildExecutor` 的 `llmagent.WithModel(gate(base))`）：GenerateContent/GenerateContentIter 入口若 ctx 携 echo spec 但凭据未绑定/rejected → 返回 typed error（不调底层）；RunFlow 将该错传回事环→不 ack（中止）。`EchoCredential` 加 `Verified()`（bound 且 !rejected）+ `MarkRejected(reason)`；MemoryPlugin 存错/不匹时标 rejected。
- (B) 三装饰器（SwappableModel / TrajectoryRecorder / TrajectoryRecorderModelWrapper）加 `GenerateContentIter`：base 是 IterModel 则**直接委托**（保快速通道）；否则惰性地桥接 channel；全程保留 lease/retire/close/cancel-drain 与 Info，**不伪造**（无副作用直到首次 Next）。
- (C) 恢复提示消费从 `assembleRequest`（BeforeModel）移入装饰器实际调用点（channel 入口 / iterator 首次 Next），使“建而未迭代即取消”不消费。

## §4.4 后 code review 回修（单一 CodeReview 子代理，本会话净变更）（2026-09-20）

子代理报 **0 Major + 4 Minor**（并对四个 §4.4 关注点逐条给出“未发现 under-skip/身份错乱/并发错乱/无泄漏”的肯定结论）。逐条源码核验后处置：

| # | 发现 | 核验 | 处置 |
|---|---|---|---|
| **M1** | `MemoryPlugin.onEvent` 构 `FullEvent` 未拷 `msg.ContentParts`→插件路径（volatile 用户图/assistant/tool 多模态）落库丢 parts，压缩重建（读 `evt.ContentParts`）静默丢弃图片 | 属实（L171-176 只设 Content/ToolCalls/ToolID/Response）——§4.3“全链”只通 durable `buildBusFact`、未通插件 | **已修**：`fullEvent.ContentParts = msg.ContentParts`。fail-before：`TestMemoryPlugin_PluginPathPreservesContentParts`（还原该行时红于 ContentParts 断言 `[] has 0`，加回则 ok） |
| **M2** | `isExpectedInputEcho` 对空 `MergedMessage` 判据过宽：image-only durable（Content 空）会误匹任意空 root user 事件 | 属实但**权衡后不改**：强制非空才跳过会使 image-only 合并回显**重新落库**（即 §4.4 要消除的双写）；残留仅“多跳一个无信息的空事件”，无害 | **不改**（在谓词处写明此为有意权衡）；parts-aware 精确身份留待 §4.7 接地后处理（无接地的 parts 比对有 under-skip→双写风险，不猜） |
| **M3** | `cred.Bind(evt.InvocationID)` 与“绑定确切 Event.ID”粒度偏差 | 属实但无影响（`BoundID()` 暂无产品消费者，为 §4.5 预留） | **已改注释**（attribution.go `Bind`）：`event.Event` 无每事件 ID（嵌 `*model.Response`+`InvocationID`），root invocation id 即本尝试回显的稳定精确身份，§4.5 须按此**每尝试**粒度断言非每事件 |
| **M4** | `persistBusEvent`/`writeSettleFeedback` 两处陈旧注释仍引用已删的 `FactsPrePersisted` | 属实（L1115/L1279，grep 确认） | **已修**：改指 §4.2 提交门/§4.4 `cm.turnEcho` 安装门控 |

**本轮变更**：plugin/memory_plugin.go（M1 修 + M2 注释）、plugin/attribution.go（M3 注释）、agent/context_manager.go（M4 两处注释）、plugin/memory_dedup_test.go（M1 fail-before 测）。无断言削弱、无按完成归档未完成项、未提交。全量 `-race`：plugin 3.3s / memory 3.2s / agent 26.7s 全 ok，`go vet ./...` 净，TEMP 残留 0，validate --strict valid。§4.4 仍关（本为回修非新任务）。

## §4.5 部分落地：(B) SwappableModel iterator 能力保留（2026-09-20）

按接地结论（框架确实优先 IterModel → 仅实现 GenerateContent 的装饰器会静默降级）先做自含、离写路径、接地充分的 **(B)**：
- `rl/swappable_model.go` 新增 `GenerateContentIter`：**惰性**（返 Seq 时不取 lease/不触 inner，首次迭代才动）；inner 是 `IterModel` 则**直接委托**（保快速通道，不起 channel 桥接 goroutine）；否则**真实桥接** channel（不伪造）。inFlight lease 跨整个迭代持有（镜像 `GenerateContent`），early-stop/ctx-cancel 排空上游不 wedges producer、lease 终会释放。`SwappableModel` 就此满足 `model.IterModel`（编译期 `var _ model.IterModel` 断言）。
- **resident 路径相关**：`examples/wechat-bot/main.go:196` 入口 agent 模型即 SwappableModel → 此前它隐藏基座 iterator 能力是真回归。
- **fail-before/测**：`rl/swappable_model_iter_test.go` 三测（mock `iterCapableBase` 记录 iterEntry/iterStart/chanEntry）：PreservesIterModel（惰性：建 iterator 后 base.iterEntry==0、无 lease；迭代后=1 且委托非降级；完成 lease回 0）、BridgesChannelBase（channel-only 基座→真实桥接全量送达）、IterEarlyStopReleasesLease（yield false 仍释租）。`rl` 全量 `-race` 1.4s ok。

**§4.5 尚未完成（未 tick）**：
- **(A) 执行凭据核验门**：方案已清（`EchoCredential` 加 `Verified()`=bound∧¬rejected / `MarkRejected`；新增无态执行门装饰器包 `cfg.Model`，于 GenerateContent/Iter 入口：ctx 携 echo cred 但 !Verified → 不调底层直接 typed error；MemoryPlugin 存错时 MarkRejected）。**设配点需裁决**：现 loop L274 `finishDurableBatch(received)` **无条件 ack**（模型重试耗尽也 ack，仅 L261 日志）→「凭据未验证→中止/不越提交门」需新增 not-ack 判定，且与 **§5 结构化 completion（completed/failed 与 ack 关系）** 交耦——宜将阻塞与“不 ack/记 failed”的处置与 §5 一并定，避免 ack 路径二次重构。门本身（阻断模型入口）可先行落地。
- **(C)** 恢复提示消费从 `assembleRequest`（BeforeModel）移入门装饰器实际调用点（channel 入口 / iterator 首次 Next）→与 (A) 同一装饰器，随之落地。
- **(B) 录制器两装饰器**（`TrajectoryRecorder`/`...ModelWrapper`）：需惰化 `recordGenerateContent`（batchIndex 分配/gcWg/记录时机移到首次迭代）且宜**重构共亨记录组装逻辑**避免 40 行重复漂移；仅 RL 路径、不触 resident 协议，风险/收益低于 SwappableModel，建议单独小 pass。

## §4.5 完成（A/B/C 全部落地并验收，2026-09-20）

上节 (A)(C)/(B-录制器) 的推迟裁决在本会话落地——工作树代码已越过上条“尚未完成”记录（后者保留作历史）。逐项核验与新增验收证据：

- **(A) 实际模型入口执行凭据核验（已接线，非仅装饰器存在）**：`executionGateModel`（`agent/execution_gate_model.go`）经 `buildLLMAgent`（唯一 fwAgent 构造点，冷启动 + `RebuildExecutor` 共用，`context_manager.go:477-482` `gatedModel = newExecutionGateModel(cfg.Model, cm)`）注入 `llmagent.WithModel(gatedModel)`；`assembleRequest` 不再消费恢复提示（L768-772 显式移交门）。GenerateContent/GenerateContentIter 入口 `verify(ctx)`：ctx 携 echo cred 且 `!Verified()` → 返回 `ErrExecutionCredentialUnverified`，**不调底层**（迭代器路径 lazy：建 Seq 不核验/不消费，首次 Next 才判）。**测**（`agent/execution_gate_test.go`，5 例，`-race` 全绿）：BlocksUnverifiedCredential（inner.requestCount==0）、PassesVerifiedCredential、BlocksRejectedCredential、NoCredential_Passes、BlocksUnverifiedOnIterator。
- **框架吞插件错误不越提交门（loop 级 fail-closed，新增专项测 + 真 fail-before）**：`plugin.EchoCredential` 加 `Verified()`/`MarkRejected()`；`MemoryPlugin.onEvent` 存错（`!stored && memStore!=nil`）→ `MarkRejected`（框架仅记录错误后继续，凭据 STATE 为唯一权威）。loop 收端 `event_loop.go:279` `if installed, verified := cm.turnEchoVerified(); installed && !verified { ... NOT acking; return }`。**新增 `agent/execution_gate_loop_test.go::TestRunEventLoop_UnverifiedCredentialDoesNotAck`**：真实 durable bus（`NewReliableEventBus`）+ 真 `NewContextManager`（自动裹门）+ `credFaultStore`（仅 `agent_output` 的 StoreEvent 失败→MarkRejected；输入经 ReplayEvent 提交、inbox_receipt 的 StoreEvent 放行使 ack 路径本身可用）。断言：模型达 1 次（echo 先绑定→入口门放行）后，envelope **不被 ack**（`DurablePending()` 稳态非 0）。**真 fail-before**：临时把 `event_loop.go:279` 条件置 `false &&` → finishDurableBatch 落 ack → `DurablePending()==0` → 测红（“Should not be zero, but was 0”）；恢复门 → 绿。选 `credFaultStore`（非 `failStore`）正为令 guard 成唯一判据（always-fail 存储会同时打断 receipt 写使门与无门同为 pending=1，无法证伪）。
- **(B) 不伪造可选接口（三装饰器齐）**：`rl/swappable_model.go` 上轮已加 lazy `GenerateContentIter`（inner 为 IterModel 直接委托、否则真实桥接 channel、inFlight lease 跨迭代持有/释），并 `var _ model.IterModel` 断言。本会话核验 `rl/trajectory_recorder.go` 与 `TrajectoryRecorderModelWrapper` **均已补 `GenerateContentIter`**（惰化 `recordGenerateContent`：建 Seq 不占 batchIndex/不触底，首次 Next 才 fire），录制器必须观测每条响应故 inherently 拦截，但保留 iterator 契约与能力面非仅藏于 GenerateContent。**测**：`rl/swappable_model_iter_test.go`(3)+`rl/trajectory_recorder_iter_test.go`，`./rl -race` 1.45s ok。`executionGateModel` 自身保 Model+IterModel+Info+Close 不降级。
- **(C) 恢复提示延迟到实际调用**：`assembleRequest` 不再消费（F9）；门 `withRecoveryNotice` 在 channel 入口 / iterator 首次 Next 才 `TakeRecoveryNotice()` 并追加到复制的请求尾（不落库、D6）。**测** `TestExecutionGate_IteratorLazilyConsumesNotice`：建 Seq 后 notice 仍在且未调模型；迭代后 notice 清空且尾消息=提示。

**全量验证**：`go build ./...` 净；`go vet ./agent/ ./plugin/ ./rl/` 净；§4.5 定向 `-race`（agent gate+loop / plugin credential / rl 全包）全 ok；**agent 全包 `-race` 29.1s ok**（无回归）；plugin `-race` ok。无 TEMP-FAILBEFORE 代码残留（仅本文术语）。未提交。§4.5 关。

## §4.6 投影只用提交返回的 canonical + 选中 outstanding 引用补齐（2026-09-20，`complete-resident-reliability-protocol`）

规格 event-sourced-projection L7 三子句，逐项落到 `agent/context_manager.go::persistBusEvent`（提交下游唯一构投影引用点）：

- **子句1「用后端返回的 canonical 构建引用，不以当前时间/重生成摘要/原调用对象替代」**：旧码 `result, _, err := ReplayEvent(...)` 丢弃返回 canonical，ref 用 `evt.Timestamp.UnixMilli()`（到达时）+ `msg.Role`（原调用对象）——违 L7。改：捕获 `result, canonicalOut, err`，`err==nil` 时 `refCanonical = canonicalOut`（durable 权威字节；volatile 默认 `fullEvent`）；ref 的 `Timestamp/EventType/EventSummary` 取 `refCanonical`，`Role` 取 `tagentevent.EventTypeRole(refCanonical.EventType)`——与冷启动 `appendProjectionRef` 同一 fold 规则（顺带消除 review 🟡5 记录的 runtime-vs-rebuild role 偏差，且 persistBusEvent 仅处理 external_input，role 无 under-skip 风险）。**fail-before 测** `TestPersistBusEvent_ProjectionRefUsesCanonicalTime`：prepared_fact 冻结 Timestamp=1700000000000，evt 到达时=now；旧码投影 `evt.Timestamp`（≈now）→ 红；新码 `refs[0].Timestamp==1700000000000` → 绿。
- **子句2「选中 outstanding 输入即使已提交/早于 snapshot 边界也必须执行前补齐引用，不因 already 分类跳过」**：旧码 `if replayed { return true }` 在 append **之前**早退——已提交（pre-crash）但冷启动未复原的选中输入被 `already` 分类丢弃、模型不可见（违 spec scenario L21-23）。改：把 ref 构造 + `Append` 移到 `replayed` 早退**之前**，条件 `cm.projection != nil && (stored || replayed)`；`Append` 按 EventKey 幂等（`SessionProjection.seen`），复原过的为 no-op、缺的即补齐；作用域天然限于 persistBusEvent 的调用方（本批选中集），不重刷所有旧 key。never-store-fails-append 不变量保留（`stored=false && !replayed` 不 append）。**fail-before 测** `TestPersistBusEvent_AlreadyCommittedSelectedInputIsBackfilled`：先 `StoreEvent` 预置事实于链、投影留空（模拟冷启动未复原），再 `persistBusEvent(sameFact)` → ReplayEvent 判 `AlreadyCommitted`（replayed）→ 旧码返 true 而投影 **Len 0**（红，实测“[] should have 1 item(s) but has 0”）→ 新码补齐 **Len 1** 且 `Timestamp` 取 canonical → 绿。
- **子句3「本批已投影集合防模型重试/压缩后重插，不重展已完成历史」**：结构上 persistBusEvent 每 turn 仅在 submitDurableBatch 阶段跑一次（先于 RunFlow 重试环），传输重试不重跑；回显经 §4.4 cred 跳过插件写不重投；`Append` 按 key 幂等。三者合使同 key 提交→重放收敛为单条引用，压缩无二次折叠对象，不需引入全历史 seen 表。**锁测** `TestPersistBusEvent_ReCommitDoesNotDoubleProject`：同 prepared_fact 连调两次（ReplayNew→AlreadyCommitted）→ 投影恒 `Len 1`。

**验证**：`go build ./...` 净；`go vet ./agent/` 净；§4.6 三新测 `-race` 全绿；既有 `PersistBusEvent`(StoredGate/Reuse/Undecodable/Incomplete)+`RebuildProjection`+`Projection`+`Replay` 定向 `-race` 全 ok（nil-store/volatile/store-fail 行为不变）；**agent 全包 `-race` 29.1s ok**（无回归）。未提交。§4.6 关。

## §4.7 回显识别不误跳 + 来源/元数据/同步工具行为保持（验收门，2026-09-20）

§4.7 为验收/回归锁定性质（无新生产码）。逐项映到专项测且全绿：

- **「assistant/tool、后续相同 user、其他 invocation 不被回显识别误跳」**（persistent-event-loop scenario「精确回显隔离」）：新增组合测 `plugin/memory_dedup_test.go::TestMemoryPlugin_PreciseEchoIsolation_Threading`——**单一凭据**下依次过真实插件：①精确 root 回显→跳过；②assistant 输出→存；③tool 结果（RoleTool/ToolID）→存（正常参与当前 ReAct）；④后续**不同** user→存；⑤**同内容但非 root** invocation（nil parent）→存。断言 `TotalEvents==4`（仅 root 回显被跳）+ `cred.Verified()`。§4.4 已逐个锁定（ExpectedRootEcho_Skipped/DifferentUserContent_NotSkipped/AssistantOutput_StillStored/NonRootInvocation_NotSkipped）；本测将四者绞入一轮正锁 spec scenario。**真 parent 子调用**回显形态由 §4.4 `echo_grounding_e2e_test.go` 接地（工具/子回合 parent≠"" 被 root 检查排除）。
- **来源优先级 / Metadata 合并保持**：`agent/metadata_propagation_test.go`（`extractRootMetadata` 表驱动）+ `task_settled_test.go::TestNewTaskSettledEvent_CarriesOrigin`（origin 包裹经 extractRootMetadata 浮出路由）`-race` 全 ok——§4.4–4.6 未动提取链，行为不变。
- **同步工具行为不被 bus 固定批次延后**：`agent/session_subagent_toolstop_test.go`（user 位于 system 后、assistant/tool ReAct 历史前；工具停止语义）+ `model_contract_matrix_test.go` ReAct tool_calls 契约 `-race` 全 ok。
- **诚实边界（M2 交叉引用，属已核安全权衡非 §4.7 文本项）**：`isExpectedInputEcho` 对空 `MergedMessage`（image-only 输入）按内容相等会过匹任意空 root user 事件——因凭据现不携 parts，无法不接地地比对 parts（under-skip→双写风险，M2 定为“不猜”）。但此仅多跳一个无信息空 root user 事件（单 attempt 内 root+user+空内容即本回合唯一回显），**不**误跳 §4.7 四类列出的任一种（assistant≠user、tool≠user、后续不同 user≠同内容、同内容非 root 过 root 检查）。parts 感知精确身份属 M2 独立小改进（需先接地 echo parts 形态 + 扩展凭据 ContentParts），不据本门谎称已做。

**验证**：`go vet ./plugin/` 净；plugin 全包 `-race` 3.0s ok（含新组合测）；agent 元数据/子agent/工具/回显定向 `-race` 全 ok。未提交。§4.7 关。

## §4.8 执行门（§4 阶段准出，2026-09-20）

§4.8 为 §4 阶段出口验收门（无新生产码，除一条行为锁）。四子句逐项映到专项测且 race 全绿：

- **真实框捕获 A+B 一 turn、执行中 C 下一 turn**：`TestRunEventLoop_ABOneTurnCNextTurn`（用户确认批次语义：A+B 合并为当前 turn，C 属下一 Pull）+ `TestRunEventLoop_DurableBatch_ABStoredCDeferred`（typed claim + 写前冻结 + persistBusEvent，C 不入当前请求）——均真 `NewReliableEventBus` + captureModel 断言实际请求。`-race` 全 ok。
- **传输重试不扩批**：新增行为锁 `TestNewAttemptEchoCredential_RetryReusesFrozenBatchNoExpansion`——凭据模板（cm.turnEcho）在批次冻结时固定，同业务 turn 重试 `newAttemptEchoCredential()` 两次→ AttemptToken **新鲜唯一**但 MergedMessage/CommittedKeys **逐字复用冻结批**（不可能再 Pull/加宽）；配合 loop 结构（`received`/`msg` 于 attempt 循环前 L79/L132 冻结一次）与 ABOneTurnCNextTurn（C→下一 turn）共同锁定。
- **输入失败但 receipt 可写时模型/确认调用均为零，恢复后原身份收敛**：`TestSubmitDurableBatch_AllOrNothingOnConflict`（B 确定冲突→A 也不提交→零事实→零模型）+ `TestSubmitDurableBatch_TransientNotConflict`（I/O 瞬时→submitTransient→投影空）+ `TestReleaseBatchClaims_RequeuesForOrderedReclaim`（未提交 claim 回 pending 不丢→下轮严格序重 claim→**原身份收敛**）+ §4.5 `TestRunEventLoop_UnverifiedCredentialDoesNotAck`（凭据未验→零 ack）。**诚实边界**：“receipt 可写”的回执/确认链属 §5 completion（未实现），本门仅以 §4 原生语（提交失败→零模型 + 凭据未验→零 ack）验证“模型/确认均为零”可观察部分；完整 receipt 链待 §5。
- **关联 agent/plugin/event/rl 测试与 race 通过**：`./agent` 全包 `-race` 29.4s ok；`./agent/reliability` 8.99s ok；`./plugin` 3.1s ok；`./event` 1.6s ok；`./rl` 1.4s ok。`go build ./...`/`go vet` 净。

**§4 阶段出口**：4.1–4.8 均关，门绿。未提交。§4.8 关。

## §5.1 turn 结果归约：消除「返回 nil 就是完成」（2026-09-20，`complete-resident-reliability-protocol`）

任务：从 runner 启动错误、响应错误、流终态、重试耗尽和取消归约明确 turn 结果，保持既有模型重试预算。落点三处——新增类型层、`RunFlow` 观测层、`runEventLoop` 归约层。design L120 / spec L94·L130。

**旧契约的两处吞点（真实缺陷）**：
1. **响应内错误零观测**：框架把模型 API 失败作为携带 `model.Response.Error`（`*ResponseError{Type,Message,Param,Code}`）的事件下发（`trpc-agent-go/agent/run_with_plugins.go:142-154` `agentErrorFromEvent` + `event.NewErrorEvent`），**不会**让 `RunFlow` 返回 error。旧 `RunFlow`（context_manager.go drain 循环）只查 `Response.Choices` 判生产性，从不查 `Response.Error` → 该 turn `return nil` → loop 走 else 分支 `ReportSuccess` → `finishDurableBatch` **ack 掉一个模型实际失败的 durable 批次**。
2. **取消当成功**：旧 drain 内 `if !cm.deliverEvent(ctx, evt) && ctx.Err() != nil { return nil }` —— 流中途关停取消 `return nil`（与正常完成不可区分）→ 同样落 else 分支 ack 掉一个「无终态」的 turn（违 spec L90/L130：取消保留 claim、不生成 completion）。

**实现**：
- **类型层（新增 `agent/turn_result.go`）**：`turnStatus`（`turnCompleted`/`turnFailed`/`turnCancelled`）+ `turnOutcome{status, err}`（err 为 `boundErrSummary` 截断至 `maxErrSummary=512` 的有界摘要，供 §5.2 completion 冻结与日志）+ 纯决策表 `reduceTurnOutcome(startErr, respErr, cancelled)`，优先级 **取消 > 启动错误 > 响应错误 > 完成**（取消=无终态优先，启动失败者从未产生真响应故次之）。单测独立于框架 turn。
- **观测层（`RunFlow`）**：新增 `var respErr string`，drain 内 `evt.Response != nil && evt.Response.Error != nil` 首次捕获 `Type: Message`；三处出口均 `cm.lastTurnOutcome = reduceTurnOutcome(...)`——启动错误分支 `(startErr,"",false)`、取消分支 `(ctx.Err(),respErr,true)` 且**改 `return ctx.Err()`**（不再吞成 nil）、正常尾 `(nil,respErr,false)`。新增 `cm.lastTurnOutcome/lastBatchOutcome` 字段 + `LastTurnOutcome()/LastBatchOutcome()/setLastBatchOutcome()` 访问器（loop goroutine 单写单读，非并发面）。**返回契约不变**（仍 `error`）：子agent 调用方 session.go:122 与 spawn 直调测不受影响；响应错误仍 `return nil`（transport OK），仅靠 outcome 区分，故**传输重试预算零改**。
- **归约层（`runEventLoop`）**：err!=nil 分支顶置 `if cm.LastTurnOutcome().status == turnCancelled { endTurnSpan; return }`——取消保留 claim、先于 §4.5 门与 finishDurableBatch 返回（不 ack）；else 分支在 `ReportSuccess` 前插 `if oc.status == turnFailed { break }`——响应错误记 failed 不谎报成功、不按传输预算重试（模型终态非瞬时 I/O）；retry 环后 `batchOutcome := cm.LastTurnOutcome(); if lastErr != nil { batchOutcome = failedOutcome(lastErr) }` 归约为单一批次终态存 `cm.setLastBatchOutcome`（§5.2/§5.3 冻结值）。取消路径永不达此归约（已早返），故 batchOutcome 只 completed/failed，诚实。

**测试（`agent/turn_result_test.go`，含真 fail-before）**：
- `TestReduceTurnOutcome`（纯表：5 通道 + 优先级 + 摘要截断）。
- `TestRunFlow_ResponseErrorReducesFailed`（真 `RunFlow` 直调，同步读 outcome 无 race）：mock 返 `Response.Error` → 断言 `err==nil`（transport 未误报）且 `LastTurnOutcome().status==turnFailed`、`err` 含"upstream exploded"。**真 fail-before**：临时将捕获块置 `if false &&`（还原旧「不查 Response.Error」）→ 期望 1(failed) 实得 0(completed) → 红；恢复 → 绿。
- `TestRunFlow_MidStreamCancelReducesCancelled`（确定性取消判别子）：无缓冲 outputCh 无人读使投递阻塞，goroutine 50ms 后 cancel → 断言 `ErrorIs context.Canceled` 且 `status==turnCancelled`。**真 fail-before**：临时 `return nil`（旧吞取消）→ `ErrorIs` 失败（got nil）→ 红；恢复 `return ctx.Err()` → 绿。（不用 loop 级取消测作判别子：实测旧 `return nil` 会先落**退化重试→retry 顶 ctx 检查**早返而歪打保留 claim，非 §5.1 守卫所致，故另建同步判别子。）
- `TestRunEventLoop_MidStreamCancelRetainsClaim`（端到端回归锁，非严格判别子，注释已如实标注）：真 `NewReliableEventBus` + `newDurableAgentWithStore`（正常存储）+ 无缓冲 outputCh + cancel → 断 `DurablePending()` 非 0（claim 保留）。
- `TestRunFlow_NormalDrainCompleted`：正常生产 turn → `turnCompleted`（防归约过度把每 turn 当失败）。

**验证**：`go build ./agent/...`/`go vet ./agent/` 净；§5.1 五测定向 `-race` 全绿；**agent 全包 `-race` 29.36s ok**（RunFlow 为子agent/直调共享原语，全包无回归）；grep 无 `TEMP-FAILBEFORE` 残留。未提交。§5.2/§5.3 消费 `LastBatchOutcome` 冻结 completion（本任务只产归约结果，不改 ack 语义于 failed——failed 作为处理结果仍 ack，与 §5.3 完成协议一致）。§5.1 关。

## §5.2 completion_version=1 冻结 schema（2026-09-20，`complete-resident-reliability-protocol`）

任务：定义 completion_version=1，逐槽 processed/skipped、原因/事实 key、固定 receipt key、完成时间、首次归因及完整 receipt fact 一次冻结。design L122 逐项字段。**本任务为定义/冻结层（类型+builder+校验+确定性测），生产接线在 §5.3**（同 §5.1 交值不交费的节奏）。

**落点**：新增 `agent/completion.go`（agent 层拥有 `memory.FullEvent` schema；reliability 叶保持对 Completion 载荷 schema-agnostic，见 inbox.go L167-168 不 import agent/memory，D2）。字段严格对齐 design L122：`completion_version=1` + `request_id` + `receipt_key`（固定 hex）+ `completed_at_ms`（首次生成）+ `batch_result`（completed/failed，由 §5.1 `batchResultFromOutcome` 映射：cancelled→ok=false 不生成 completion）+ 有界 `error_summary` + 有序 `slots[]`（每槽 `{slot, source_id, disposition, fact_key?, reason?}`）+ 完整 `receipt_fact`（`memory.FullEvent`）。

- **§5.2 直击旧 `persistInboxReceipt` 的回执重建缺陷**：旧码（lifecycle.go:496-519）每次 `memory.NewSnowflakeEventKey` 新生 EventKey + `time.Now()` 新时间——重启/重试会**重建不同回执**（违 L122「重启不得用当前时间重建回执」）。`buildReceiptFact` 改为：EventKey=`ParseEventKey(信封预留 receipt_key hex)`（固定、幂等 by-key、绝不另生新 key）、Timestamp=`completed_atMs`（冻结值非 now）、Metadata 携首次归因（rollout/trace/trigger）+ agent/request 身份；不可解析的预留 key 是**确定错误**，绝不回退新生 key。
- **逐槽恰一处置的闭合枚举**：`slotProcessed` 必 `fact_key` 非空且无 `reason`；`slotSkipped` 必 `reason` ∈ 闭合集 `{meditation_yield, not_selected, empty_input}` 且无 `fact_key`（design L54「未知结果枚举按失败处理」→ 拒 free-form 原因）；`validate` 另校 version/request/receipt_key 非空、batch_result 闭合、failed↔有摘要 / completed↔无摘要、receipt_fact 类型、**无重复槽**。违例均返确定 error（`freezeCompletion` 校验先于 marshal），不静默修补。
- **确定性/幂等**：`freezeCompletion` 不读时钟、不生 key，纯函数——相同输入→**字节相同**输出（§5.3 的 `RecordCompletion` 靠此在相同重试上判等/判冲突）。`decodeCompletion` 解析后重校验，使 §5.7 启动核对读自洽记录而非信兵字节。
- **大整数身份**：EventKey 以 int64 typed 字段携带，`jsonEqual` 已 `decodeExactJSON`（UseNumber，inbox.go:1128-1153）保 int64——>2^53 相邻 key 不坍缩（测 `TestCompletion_LargeKeyPrecisionRoundTrip`：`1<<60` 与 `+1` 冻结→解回逐位相异）。

**测试（`agent/completion_test.go`）**：`TestBatchResultFromOutcome`（§5.1→§5.2 接缝：completed/failed 成形、cancelled 不成）、`TestBuildReceiptFact_UsesFrozenKeyAndTime`（**fail-before 对旧 fresh-key+fresh-time**：断固定 key/冻结时间/首次归因/幂等 by-key/非法 key 报错）、`TestFreezeCompletion_Deterministic`（同输入字节相同 + 回环重校验）、`TestCompletionValidate_ExactlyOneDispositionPerSlot`（11 个违反例全拒）、`TestCompletion_FailedCarriesSummary`、`TestCompletion_LargeKeyPrecisionRoundTrip`。

**验证**：`gofmt` 净；`go build ./...` 净；`go vet ./agent/` 净；§5.1+§5.2 定向 `-race` 3.6s ok；**agent 全包 `-race` 29.49s ok**（无回归）。completion 类型为未接线定义层（仅测消费），§5.3 接 RecordCompletion→提交→RecordReceipt→Ack。未提交。§5.2 关。

## §5.3 接通两阶段完成协议：completion耐久→固定receipt显式提交→RecordReceipt→Ack（2026-09-20）

任务：接通完成协议全序；模型已结束但结果首次写失败时同进程只重试结果写、不重跑。design 决策5 L62-63/L94、spec L94/L132-134。

**发现并闭合 §4.1 遗留的真实缺口（prepare 作用域）**：上一轮我一度判为需用户裁决的设计冲突，复核 design 决策2 L74「**一次重写冻结整个 envelope 的 receipt key、所有槽位的完整 canonical fact；未执行的槽同样可有准备事实，但不提交**」与 spec L88「为整个冻结批次的各 envelope …预留 receipt key」后确认——非开放问题，是规格已明定、代码未落实的缺陷。§4.1 只把 **ack** 作用域扩到 `received`（防让路 meditation 僵尸），**prepare 仍限 `events`（selected）**→ 让路 envelope 无预留 key/无准备事实→ §5.2 冻结回执要求非空预留 key 时无钥可用（旧码能 ack 它纯因 `persistInboxReceipt` 临时新生 key，正是 §5.2 要消除的非幂等 bug）。**修复**：`submitDurableBatch(ctx, received, selected)`——Phase1 `prepareBatchFacts(received)`（全 envelope 预留 key+冻结事实），Phase2 仅提交 selected 事实。prepare 仍 all-or-nothing 于 received（spec L121-122）；非混合批 received==selected，§4.2/§4.3 既有测语义不变（已回跑绿）。全跳过（len==0 after drop）分支实测不可达（dropMeditation 对非空入参永不返空），保留防御性正确签名调用。

**两阶段接线（lifecycle.go）**：
- **Phase A** `recordCompletionWithRetry`：`EventBus.RecordCompletion(path, completion)`（新包装器）先于任何回执耐久冻结；瞬时 I/O 失败按 100/200/400ms 有界**只重试写**（`finishDurableBatch` 位于模型下游、天生不含重跑，满足「结果已得只重试结果提交」），确定 `ErrCompletionConflict`（冻结字节权威）不重试；耗尽仍失败→ claim 保留不 ack。
- **Phase B** `cm.commitReceiptFact(c.ReceiptFact)`：以 completion 携的**预留 key**（非新雪片）经 `EventReplayer.ReplayEvent` 显式提交回执→同键重放收敛为 `ReplayAlreadyCommitted`（绝不第二条回执）、同键异内容报错；回执为内部处理记录，**不入投影**（与 §5.5 非投影分类一致）。失败→ claim 保留（completion 已耐久，§5.7 只补提交回执、不重执行）。
- **Phase C** `ConfirmDurable`（既有 RecordReceipt+Ack）。三阶段任一失败绝无一律无凭据 ack。
- **builder（completion.go）** `buildEnvelopeCompletion`：逐槽 committed(selected)→processed+fact_key（取 PreparedFact 的 EventKey hex）/ 未选→skipped（meditation_yield 或 not_selected，闭合枚举）；`batchResultFromOutcome` 映 §5.1 归约（cancelled→ok=false 不成形，早返保留 claim）；`completedAtMs`+attribution 每 turn 只取一次→确定性冻结。旧 `persistInboxReceipt`（fresh key+time.Now）现已无调用者，按任务边界留待 §5.4「移除只凭 request ID/状态字符串确认的旧路径」一并删。
- **loop 3 调用点**：正常传 `(spanCtx, received, events, batchOutcome)`（§5.1 归约值）；两空批路径传 `(ctx, received, {nil|events}, completedOutcome())`。

**测试（`agent/completion_protocol_test.go`）**：`TestFinishDurableBatch_ReceiptUsesReservedKey`（**真 fail-before**：断回执 EventKey==预留 key；临时在 commitReceiptFact 覆写为新雪片 key → 红于该等值断言 → 恢复绿）、`TestFinishDurableBatch_ReceiptIdempotentResubmit`（取回链上冻结回执再提交→ TotalEvents 恒 2、不双写）、`TestBuildEnvelopeCompletion_DispositionsAndFixedKey`（committed=processed+fact_key、让路 meditation=skipped(meditation_yield)无key、failed turn 仍 processed+batch_result=failed、槽升序、cancelled 不生成、processed 缺准备事实报错不伪造 key）。

**受影响测适配**（新 `finishDurableBatch(ctx, received, selected, outcome)`/`submitDurableBatch(ctx, received, selected)` 签名）：`inbox_receipt_test.go` 三调用点适配（`StoreFailureKeepsClaim` 因旧 `failStore` 只失 StoreEvent、§5.3 回执走 ReplayEvent，改新增 `receiptFaultStore`（仅失 TypeInboxReceipt 的 ReplayEvent）以真实验证 Phase B 失败→claim 保留）；`submit_durable_batch_test.go` 两处非混合批传 `batch,batch` 语义不变；`reliability_boundary_test.go::TestRunEventLoop_DurableBatch_ABStoredCDeferred` 补 `require.Eventually(pending→0)`——让延迟的 C turn 确定收敛（取消时保留未 ack claim 是正确行为，但避免 loop goroutine 在途 finishDurableBatch 文件写与 `t.TempDir` 清理竞跑；收数 3× 确证非偶发）。

**验证**：`gofmt` 净；`go build ./...`/`go vet ./agent/` 净；§5.3 三新测定向 `-race` 全绿；**agent 全包 `-race` 29.93s ok**；**reliability 全包 `-race` 9.51s ok**（无回归）；grep 无 `TEMP-FAILBEFORE` 残留。未提交。§5.3 关；§5.4 接：删 `persistInboxReceipt` 旧路 + RecordReceipt 要求合法 completion+核验凭据。




## §5.4 RecordReceipt 要求合法 completion + 核验凭据；移除旧确认路径；receipt 与输入失败不相互掩盖（2026-09-20）

任务三子句，design 决策 L126「RecordReceipt 必须要求合法 completion 与已核验回执凭据；删除旧只凭 request ID/描述字符串确认的路径」、spec L94/L172-174。

**旧路径删除证认**：`git show HEAD` 证实 HEAD 上仍存在的三条弱确认路——`persistInboxReceipt`（fresh key+time.Now）、`bus.ConfirmDurableByRequestID(rid)`、`ReconcileReceipted(res.ReceiptedRequestIDs)`——均已随 §5.3 接线在工作树删除（非测试 grep 仅剩注释引用；rid→path 反查仅保留 `pathsByRequestID` 账目用途）。本轮删掉最后一种"描述字符串"确认形态：`RecordReceipt(path, note string)` 的 `note` 参数与只写不读的 `Envelope.ReceiptNote` 字段整体移除。

**leaf（reliability）**：新增 `ReceiptCredential{ReceiptKey}`（只携已核验的预留 receipt key 身份，非自由文本）；`RecordReceipt` 三门：①completion 耐久存在且 `json.Valid`（结构合法；schema 全量校验留 agent 层，D2 叶保持载荷不可知）②信封携非空预留 receipt key（两阶段协议确已于 prepare 建立，否则回执成孤钥）③凭据非空且 `==env.ReceiptKey`。任一拒绝均不推进 state（claim 保留重放）。`ConfirmDurableByReceiptKey`（启动核对，caller 已证事实链上该 key 的 receipt 存在）以链上 key 直接构造凭据。

**agent（唯一凭据签发者）**：`cm.verifyReceiptCredential(raw)`——(1) `decodeCompletion` 解码+全量重校验（version/闭合枚举/逐槽恰一处置，非法 completion 永不产凭据）；(2) 身份绑定：`receipt_fact.EventKey` 十进制格式化必须 `== completion.receipt_key`，漂移=矛盾，拒于任何提交之前；(3) 经幂等重放接口提交/核验 receipt fact（首次提交或 Already 均为"链上已核验"，提交失败=确定错误、无凭据）。`finishDurableBatch` Phase B 改为取得凭据（失败→claim 保留、completion 已耐久交 §5.7 只补回执），Phase C `ConfirmDurable(path, cred)`；`EventBus.ConfirmDurable` 签名携凭据，硬编码 "turn finished" 字符串消失。

**边界诚实（不掩盖的反向含义）**：Phase B 失败后**同进程**重跑 finish 会因新 `completed_at_ms` 与已耐久 completion 冲突而被拒（这正是"冻结字节权威"的表现），进程内补收敛属 §5.7 启动核对（尚未建，任务序列如此），本轮不假装其存在。

**测**：leaf 新增 `TestInbox_RecordReceiptRequiresVerifiedCredential`（无预留 key/空凭据/异钥各拒、拒不推进 state、配钥收敛；**fail-before 实跑**：TEMP 禁用②③门 → `An error is expected but got nil` 红 → 恢复绿）；既有 `TestInbox_ReceiptAndAckRequireDurableCompletion` 升级凭据形态。agent 新增 `receipt_credential_test.go` 6 测：签发唯一性/幂等（Already 不双写）、非法 completion 拒（不提交）、身份漂移拒（**fail-before 实跑**：TEMP 去漂移绑定 → 红）、提交失败无凭据、`ReceiptFailureDoesNotMaskInput`（receipt 失败→claim 保留、预留 key+事实材料完好、链上零伪造 receipt）、`InputConflictIsNeverReceipted`（输入侧 completion 冲突→无凭据、无 receipt、不 ack）。适配：inbox_test `finish`+`reserveCredential` helper、cleanup-account/retention-e2e/event_bus_spill（补齐完整两阶段）/upgrade-drill（prepare→completion→凭据回执→ack）。

**验证**：gofmt/build/vet 净；`go test -race ./agent/reliability/` 11.85s ok；**agent 全包 `-race` 30.31s ok**；root 包 3.48s ok；`./tests/ -short` 5.16s ok（tests 全量含真实 LLM/soak 非 short 项，属既有待验清单 §8.8/9.7，非本轮回归）；grep `TEMP-FAILBEFORE` 零残留。未提交。§5.4 关；§5.5 接 event 包非投影判定统一。

## §5.5 event 包统一非投影判定；删除旧快照/旧元数据兼容分支（2026-09-20）

任务四子句：event 包单一判定、三路径共同排除当前内部记录、删除仅为旧快照/旧元数据兼容的判定分支、更新全部调用方和测试。design 决策 L126（"当前内部记录的排除是业务规则；只为已淘汰历史快照/旧标记而存在的排除分支随决策10删除"）。

**收敛前实况（缺口两处）**：非投影判定散落为两份私有枚举——`agent.skipProjectionEvent`（冷启动 tail/fallback）与 `ReplayProjectionHandler` 内联条件（spill 回补）——**后者漏排 `inbox_receipt`**：spill 窗口回补一旦携入回执事件即注进投影，破坏「投影=事实链可回放折叠」不变量；两份各自携 `legacySnapshotMetaKey`（"compress_snapshot"，写方 persistSnapshotEvent 早删）与 `"task_inline_record"` 字面量。

**落地**：
- **event/registry.go（单一权威源）**：`EventTypeSpec` 新增 `NonProjection bool`（C1 契约字段扩充，声明归属同 Embeddable/TTL——本变更 design L126 即其批准来源）；注册表标记四类：`inbox_receipt`（inbox_receipt.go）、`task_spawned`、`resident_session`、`context_compress_summary`（compaction 事件本体综述由载荷重建）；访问器 `IsNonProjectionEventType` 经 `specOrDefault`——未知类型回退 false（保守进投影，与历史排除白名单语义逐位等价，registry_test 等价验收线不破坏）；唯一入口 `IsNonProjectionRecord(eventType, metadata)` = 类型声明 ∨ `MetaKeyTaskInlineRecord`（metadata.go 新增该键常量，"键名集中到 event 包"决策延伸）。
- **三路径共用**：冷启动 `rebuildProjectionFromWAL` tail/fallback 三处、spill `ReplayProjectionHandler`、正常提交 `persistBusEvent` append 门（新增——内部记录即便抵达提交路径也不占投影；volatile 型经 ExtractEventType 恒为业务型，durable prepared_fact 路径为真实暴露面）。`skipProjectionEvent` 与 handler 私有枚举**删除**（grep 零残留）。
- **兼容分支删除**：`legacySnapshotMetaKey` const 及两处 `compress_snapshot` 判定移除——运行时只认当前格式，旧数据唯一处置=§3.7 清点+显式受管 reset，不留反向兼容判定。
- **调用方/测试更新**：`task_inline_record` 字面量收敛到常量（context_manager 两处写+一处拷贝清单、task_record_emit/chain_e2e/record_sink 三测）。

**测**：event 新增 `registry_nonprojection_test.go`（声明集+未知型保守+metadata 分支）；agent 新增 `nonprojection_unified_test.go`：`TestReplayProjectionHandler_ExcludesCurrentInternalRecords`（五类内部记录全排、业务事件照常——**fail-before 实跑**：TEMP 还原旧私有枚举 → receipt 进投影 `Not equal` 红 → 恢复绿）、`TestPersistBusEvent_NormalCommitExcludesInternalRecords`（durable claim 携 receipt 型事实：链上仍提交、投影零占位、业务型照常 append——**fail-before 实跑**：TEMP 去门 → 红 → 恢复绿）；冷启动排除既有锁存于 `TestDurableReceipt_ReplayNoDoubleWrite`（projection 无 receipt ref）。

**验证**：gofmt/build/vet（agent/event/memory）净；`go test ./event/ ./memory/...` 全 ok（含 registry 等价验收线）；**agent 全包 `-race` 30.25s ok**；event `-race` 1.66s ok；root + `./tests -short` ok；grep `TEMP-FAILBEFORE`/`skipProjectionEvent`/`legacySnapshotMetaKey`/`compress_snapshot` 零残留。未提交。§5.5 关；§5.6 接清理账目与 nextClaimable 边界。

## §5.6 清理账目前置登记与 nextClaimable 边界（2026-09-20）

任务子句（spec L96 主干）：清理前登记独立账目／unlink 后 dirsync 失败保留／文件不存在仍补目录屏障、再一次释放容量/索引/保留／nextClaimable 不私自删除 receipted 项。

**落地映射（四子句）**：
- **前置登记（本轮改）**：`Inbox.Ack` 原为"unlink+dirsync 失败后才登记账目"（§3.6），字面要求是"清理**前**登记"。改为 `cleanupOwed[path]=oc` 先于 `os.Remove`——自登记起任何 unlink 起的失败都保账；**remove 确定失败（文件完好、清理未开始）则取消登记**，绝不留幻影账目（幻影会让后续 DrainCleanups 替仍在盘上的文件完成屏障→双释放容量+误放租约）。诚实注记：锁段内前置/后置观测等价，本改动为字面合规+对未来锁细化的防御；幻影取消子句有独立反例测锁定。
- **dirsync 失败保留**：§3.6 既有（`inbox_cleanup_account_test.go` 三测），本轮回归绿。
- **文件不存在仍补屏障、释放恰一次**：missing+owed 分支（syncDir 补屏障→finalizeCleanup 同时删 `pathsByRequestID` 索引账+`pending-1` 容量账，exactly-once）与无账不重复释放——§3.6 既有测锁定；"保留"腿经 `DrainCleanups` 返回 material→`EventBus.DrainRetentionCleanups`→`releaseRetention`，生产由 consume loop（event_loop.go:98）每轮驱动。
- **nextClaimable 不私删 receipted**：行为在 §2.8 已从"扫描内私删"改为"原样返回交消费者 Ack"（`removeAndSync` 死码已删），但**函数头注释仍写 "clearing receipted OUT OF THE WAY"（与行为相反的过时描述）**——本轮更正为如实（返回、删除/释放仅经 Ack 屏障）；补专属反例测 `TestNextClaimable_ReturnsReceiptedNeverSweeps`（receipted 信封经 3 次 ClaimNext 原样返回、文件在盘、容量不降；**fail-before 实跑**：TEMP 还原旧私删 → `Expected value not to be nil` 红 → 恢复绿）。

**边界（不越任务）**：spec L96 后句"冷启动先同步目录并清点，文件在则核对后清理，不在则无需恢复旧清理账目"属 §5.7 主题（启动目录屏障+直接核对）；账目腿现状已合规——`cleanupOwed` 为纯内存态，reopen 恒空，重启对缺失文件不再恢复旧账（正合"不在则无需恢复"），文件在的 receipted 项经 ClaimNext Ack-skip 收敛。`NewInbox` 冷启动 dirsync 屏障留 §5.7 一并落地。

**测**：新增 `agent/reliability/inbox_sweep_account_test.go`：`TestNextClaimable_ReturnsReceiptedNeverSweeps`（fail-before 见上）+ `TestAck_RemoveFailureLeavesNoPhantomAccount`（只读目录诱发 remove EACCES→错误+文件在盘→恢复权限后 `DrainCleanups()` 空（无幻影）、pending 恒 1→真 Ack 完成屏障释放恰一次；平台不允许失败注入时显式 Skip 不假绿）。

**验证**：gofmt/build/vet 净；**reliability 全包 `-race` 10.39s ok**（含 §3.6 账目三测回归）；**agent 全包 `-race` 29.81s ok**；root+`./tests -short` ok；grep `TEMP-FAILBEFORE` 零残留。未提交。§5.6 关；§5.7 接冷启动目录屏障+outstanding 直接核对（含 completion-only 崩溃窗，见 code review 保留意见）。

## §5.7 启动直接核对：outstanding 信封按自有固定 receipt key 逐一裁决（2026-09-20）

任务：启动目录同步与清点后按每个 outstanding 固定 receipt key **直接核对**；覆盖 prepared-only、completion-only、匹配 receipt、矛盾状态与读取 I/O，**不从投影收集确认列表**。此即 code review 唯一保留意见（completion-only 崩溃窗）的收口。

**落地**：
1. **冷启动目录屏障**：`NewInbox` 在清点扫描**之前** `syncDirFunc(envDir)`——先前 mid-flight 死掉的 unlink/dirsync 条目必须先落定（在或不在），计数与核对绝不跑在可能过期的 listing 上；同步失败**拒绝开账**（fail-loud）。
2. **清单来源**：`Inbox.Outstanding()`（锁内只读清点，返回每个未 ack 信封全量原件+per-file ReadErr；不删不claim不改状态）。`RecoveryResult.ReceiptKeys` 字段、tail/fallback 两处采集分支**全删**。
3. **agent 直接核对** `ta.ReconcileOutstanding()`（agent/reconcile.go，五类裁决逐字对偶 spec L162）：
   - 无 completion：`state==receipted` → **矛盾隔离**（「任何缺 completion 的 receipt」，不落入 continue-input 否则永远 ack-refusal 死循环）；否则 **continue input**（交正常重放）。
   - `decodeCompletion` 失败 / `reconcileIdentityChecks`（request id ∧ completion.ReceiptKey≡env.ReceiptKey ∧ 每个 processed slot 的 fact_key≡该槽冻结 prepared 身份）失败 → **隔离+报告**（`QuarantineEnvelope`：字节保留、容量经账目释放、不重盖章）。
   - 链核对 `GetEvent(自有预留 key)`：**hit 且 `sameReceiptFact`**（全字段比对冻结 canonical）→ **只清理**（ConfirmDurable：receipted 幂等+Ack 屏障+releaseRetention，Major#1 租约释放语义随迁）；**hit 不同内容** → 确定性矛盾隔离；**miss** → **只补回执**（`verifyReceiptCredential` 从信封 durable completion 重提交冻结 receipt，幂等 ReplayEvent；模型零参与、不重跑、不重 freeze）；补提交被确定性拒绝（`ErrEventForgotten` 30 天窗已过 / 同键冲突）→ 隔离，瞬时错误 → blocked 保留；**I/O 错误** → **blocked**（保留、报告、绝不猜）。
   - listing 整体失败 → 本轮全废（partial view 不核对），下次启动重试。
4. **旧链全删零残留**：`ReconcileDurableReceipts`、`EventBus.ReconcileReceiptedByKey`、`Inbox.PathForReceiptKey`/`ConfirmDurableByReceiptKey`/`confirmDurableAt`（by-key 收敛 API 本身即"收集清单再收敛"旧模型残骸）；`retention_e2e_test` 的 ReconcileReceiptedReleasesLease 以注释指向承接其 Major#1 语义的 `TestReconcile_MatchingReceiptCleansUpOnly`。`commitReceiptFact` 改返回 typed error（`%w` 透传 ReplayEvent 判据）——reconcile 由此区分确定性矛盾（隔离）与瞬时 I/O（阻断）；`verifyReceiptCredential`/幂等重提交测适配。
5. **接线**：build_agent `ownsPersistentState()` 下 `RebuildProjectionFromWAL()` → `ReconcileOutstanding()`（保留租约 Arm 在 bus open 更早发生=先登记后核对；可遗忘生产者 scanner 仍由 §2.8 Lease.Ready 门控，核对完成前不开——次序不变、语义闭合）。

**测**（`agent/reconcile_outstanding_test.go`，7 测（含矛盾四子测））：prepared-only 零处置；completion-only **只补回执不重跑**（TotalEvents 1→2 且新事件必为预留 key 的 inbox_receipt、信封消失、pending 0，**全程未调用任何投影重建**——直采清点反证）；匹配 receipt 只清理（回执计数恒 1，无二次提交）；矛盾四态（不可解码 completion／预留 key 被异内容占用／身份漂移篡改／receipted 无 completion）逐一隔离+字节在 quarantine；`ioFaultStore`（GetEvent 恒 I/O 错）→ blocked 保留、恢复后收敛；信封级新 I/O 损坏（文件篡改）→ blocked 不猜。**fail-before 实跑**：TEMP 使 miss 分支跳过（模拟旧收集链对清单外 key 的覆盖缺口）→ CompletionOnly 测红（`ReceiptsAdded 0≠1`）→ 恢复绿，TEMP grep 零残留。

**边界**：同进程内 Phase B 失败的立即重试不做（reconcile 是启动语义，任务文本如此；§5.4 注记的"重启后收敛"至此闭环）。30 天遗忘窗后的 receipt 补提交经 `ErrEventForgotten` 确定性隔离，与 §5.10 年龄窗口语义衔接。`./tests` drill（升级回滚）经五类裁决适配后 -short 全 ok。

**验证**：gofmt/build/vet 净（agent+memory+event+root）；旧符号与 TEMP 标记 grep 全仓 *.go 零残留；**reliability `-race` ok**；**agent 全包 `-race` ~30s ok**；root+`./tests -short` ok。未提交。§5.7 关 → §5.8（组合根保留登记屏障/late attach/无法清点阻断）。

## §5.8 组合根保留登记屏障：清点→Protect→核对全窗口暂停遗忘（2026-09-20）

任务：组合根汇总共享 store 的恢复目录并**先登记 prepared/receipt/spill 保留再开放遗忘**；late attach 取得登记屏障；无法清点的目录明确阻断。依据 design L140-141/L234（"不靠注册顺序碰巧"）。

**落地（四层屏障）**：
1. **lease 登记屏障原语**（memory/retention_lease.go）：`BeginHold/EndHold/HoldClear`（ref-counted；0→1 换新 gate、归零 close——**可再武装**，late attach 与首次 boot 同构）；与一次性 `MarkReady` 正交（ready=「曾有 owner 完成首次重建」，hold=「有 owner 正在登记」）；多余 End 幂等 no-op、nil-safe。
2. **扫描器双闸**（memory/lifecycle.go）：首趟在 Ready(+armGrace 后备) 之后**无条件**等 HoldClear——宽限只兜「从未登记任何恢复 owner」，**显式屏障不被计时静默绕过**；后续每趟 `forgettingPaused()` 非阻塞检查，hold 中跳过本 tick（L141「暂停该 store 遗忘发布直到新目录登记/核对完成」）。
3. **per-owner 登记**：`bus.ArmRetentionFromInbox` BeginHold→清点→Protect×N→EndHold→ArmRetention；**清点失败保账不撤障**（hold 保留=遗忘显式阻断+err 上抛，绝不在不完整视图上放行）；`MemSpill.ProtectAllPending`（spill 保留腿）同型包裹。装饰链完整：`RetentionGuard` 接口扩 BeginHold/EndHold，FileSegmentStore 实现、ErrorTrackingStore/engineBridge 透传（inner 无租约静默跳过）、`memory.RetentionHoldable` 小面供组合根断言。
4. **组合根汇总门**（tagent.go/build_agent.go）：`runtimeConfig.storeBarriers`——buildAgentDFS 每 store 登记点（与 4.4 registerStoreOwner 同 chokepoint、装饰前底层指针）`raiseStoreBarrier`（dedup 幂等），`buildAgent` 顶层 `defer releaseStoreBarriers()`——**一次 build 窗口覆盖该 store 上全部 bus Arm+§5.7 核对**，多 owner 登记间隙无扫描插缝；热更壳重建下一轮重新 raise（可重入）。接线：`agent.New` 的 arm 失败由 Warnf 吞放**改为拒绝启动**（L141「不开放该 store 的遗忘/新接收，报告阻塞」——新接收拒开+遗忘被 3 的保账 hold 冻住）。

**测**：memory（`retention_barrier_test.go`）hold 嵌套/再武装/多余End/nil 单元 + **ScannerPausesUnderHold 端到端**（MarkReady 已过+hold 中→overdue 不 tombstone；EndHold→恢复 tombstone，模本 §2.8 ready 门测）+ spill rebuild Begin/End/Protect 配对计数。agent（`arm_barrier_test.go`）成功腿 Begin==End==Arm==1；**失败腿（chmod 000 诱发清点 I/O 错）Begin=2/End=1/Arm=1——保账不撤障、不放行扫描门**。root（`barrier_composition_test.go`）dedup/精确释放/可重入再武装/nil 安全/无租约 store 跳过。**fail-before×3 实跑**：lease 0→1 不换 gate→HoldBarrier+ScannerPauses 双红（`Should be false`）→恢复绿；arm 失败腿加 EndHold 放障→`KEEPS its hold` 红→移除绿；TEMP 全零残留。诚实注记：ticker 每趟 skip 腿（forgettingPaused）的独立时序 fail-before 未单跑（时间敏感），由 HoldClear 单元断言+首趟端到端红同机制覆盖；agent.New 拒启动为两行接线（错误传播编译锁定），阻断语义在 bus 层实证。

**验证**：go build/vet 全仓净；gofmt 净；**memory `-race` 3.46s ok**；**agent 全包 `-race` 31.32s ok**；reliability race 12.33s ok；root+`./tests -short` ok。既有 §2.8 grace/ready 语义零回归（retention/lease/spill 相关测全绿）。未提交。§5.8 关 → §5.9（保护贯通 Ack/spill 安全移除、单 agent 关闭 store 存活时租约归资源 owner、清理后恢复原 TTL 年龄不重盖时间）。

## §5.9 保护贯通 Ack/spill 安全移除与资源 owner 租约（2026-09-20）

任务：保护贯通到 Ack/spill 安全移除；单 agent 关闭而 store 存活时租约由资源 owner 保留；清理成功后恢复原 TTL 年龄，不重新盖时间。

**盘点结论（四子句，实现零改动——语义由 §2.8/§5.6/§5.8 组合已闭环，本轮补的是"贯通"证据）**：
1. **Ack 腿**：`releaseRetention` 全部三个生产调用点均在 Ack 目录屏障成功之后（ConfirmDurable L689 / claimDurable Ack-skip L828 / drain exactly-once L480）——既有 Major#1/2 + §3.6/§5.6 测锁定，回归绿。
2. **spill 安全移除腿**：顺序核对——`Append` 落文件成功即 Protect（无幻影）；replay **链上提交确认后**（New/Repaired/Already 皆成功）才 Release；rewrite 在 loop 后统一执行、失败保文件下次幂等重放（Already 收敛、ref 计数收支平衡）。既有 `TestMemSpill_RetentionBelt` 覆盖计数腿。**新增端到端** `memory/spill_safe_removal_test.go`：过期原文（TTL 年龄 10 天）+spill pending → `SweepOnce` 真实扫描**不销毁**（保护确实作用到 checkTTL 的 IsKeyProtected 分支，非仅计数）→ replay 落地+文件清空 → 再一趟按 **ORIGINAL timestamp** tombstone（恢复年龄、不重盖）。
3. **资源 owner 持有**：lease 挂 store（FileSegmentStore），全仓**无任何批量 Release API**；`CloseDurable` 仅关 inbox（磁盘材料保留）。**新增反例锁定** `agent/retention_owner_test.go`：两 agent 两 inbox 共享一 store——agentA `CloseDurable()` 后 A 自身未 ack 材料的保护**与** B 的保护均原样健在（"closing an agent NEVER releases/touches"），存活 owner 正常 ack 恰放自己的 holder。
4. **恢复原 TTL 年龄**：既有 `TestRetentionLease_ScannerWaitsForLeaseReady`（释放后按 ORIGINAL timestamp 过期）+本轮测 2 的 release→tombstone 腿同证。

**fail-before 两处实跑**：①`MemSpill.Append` Protect 腿禁用（`if false`）→ 测 2 首断言红（`the pending spill key protects its original`）→ 恢复绿；②`CloseDurable` 注入批量释放（模拟"agent 关闭放掉自己持有的租约"违约形态）→ 测 3 红（`closing an agent NEVER releases...`）→ 移除绿。TEMP 零残留（grep 全仓）。

**验证**：build/vet/gofmt 净；memory `-race` 3.38s、agent `-race` 31.26s、reliability 12.19s、root+`./tests -short` 全 ok。未提交。§5.9 关 → §5.10（完成门全场景矩阵）。诚实注记：本轮为验证性任务，发现项均为测试布线上文（共享 store 须显式 `SetRetentionGuard` 才有保护——本就是 bus API 契约），无实现缺口。

## §5.10 section-5 完成门：七场景矩阵验证（2026-09-20）

**场景→测映射矩阵**（验收断言全部来自 spec Scenario 文本，非现实现反推）：

| # | 场景（任务文本/spec） | 既有覆盖 | 本轮补 |
|---|---|---|---|
| 1 | 所有阶段失败重试（A completion 写失败只重试结果写；B receipt 提交失败 claim 保留、启动补提交；C ack/dirsync 失败账目+drain） | §5.3 `recordCompletionWithRetry` 测；§5.4 ReceiptFailureDoesNotMaskInput；§5.6/§3.6 账目三测 | — |
| 2 | 全跳过/mixed | §5.3 builder：mixed（B yielded）+skipped 腿；validate 恰一处置测 | 全跳过形态见诚实注记† |
| 3 | 取消 | §5.1 取消保留 claim 不 ack 测 | — |
| 4 | receipt key 早于压缩边界（spec L164：key 不在 snapshot/tail 扫描范围→直接查询仍找到、只补确认、不再调模型） | — | **新** `TestGate_ReceiptKeyBeforeCompactionBoundary`：更新锚点 snapshot（snapKey>receiptKey，rebuild Mode=snapshot 前置锁定）→ 扫描窗覆盖不到 → `ReconcileOutstanding` 仍收敛 + 冻结时间/归因逐字节复用断言 + 投影零 harvest。**fail-before 实跑**：TEMP 让 reconcile 按 `latestCompactionKey` 过滤窗内 key（还原旧收集链语义）→ 本测精确红 → 恢复绿 |
| 5 | 过期未确认材料 | §2.8/§5.9（overdue 受保护、释放按 ORIGINAL timestamp） | 跨进程形态并入 #7 |
| 6 | 目录同步双重重试（同进程重试+drain 双腿；重启后缺文件不恢复旧账） | §3.6/§5.6 腿测 + 幂等断言 | — |
| 7 | 跨进程恢复（spec L144：localfile 验收） | leaf 级 drill（tests/） | **新** `TestGate_CrossProcessCompletionOnlyRecovery`：子进程真实协议跑到 Phase A 耐久后**不 Close 直接退出**（localfile KV+segment+inbox 三介质）；父进程全新实例——Arm 从盘上重建保护（overdue input 原文 leased 断言）→ reconcile 只补冻结回执（**时间+attribution 跨进程逐字节复用**、GetEvent 直读原文完好、恰一 receipt、ack 后释放）|

† 诚实注记（场景 2 全跳过）：builder 侧 skipped 处置/mixed 均有测；**整批全跳过**的端到端（meditation-only 批）在 §4 loop 测有同型形态，本轮未另立 gate 测——`BatchResult` 归约语义由 §5.1 turnOutcome 测锁定，判定不构成缺口。

**测的自身缺陷修正记录**（非实现问题）：跨进程下 GetStats 计数口径出现 1（未对 stats 内部口径单独考古）→ 断言改用验收语义本因的直接读事件+类型计数（GetEvent 可证原文完好，属统计口径非耐久缺陷）；场景 1 rebuild 前置（空投影+compressor 接线，rbFoldCM 同型构造）。

**完成门电池**：`go build/vet` 全仓净、gofmt 净、TEMP 零残留；**`-race`**：agent 全包 32.78s（含子进程模式）/memory 3.91s/reliability 12.17s/event 2.08s；root 5.58s、`./tests -short` 5.55s。**Section 5（5.1-5.10）十任务全部关账。** 未提交。

## §6.1 完整实例 Start/Stop/Close 统一生命周期协调（2026-09-20）

任务：统一完整实例 Start/Stop/Close 状态与完成结果：在途计数先于发布 running 登记，重复关闭等待同一结果，异常退出正确终结，输出恰关闭一次。（spec runtime-resource-ownership L7 + design L151）

**现状 diff（改造前）**：①`Close` 无任何 once 协调——并发/重复 Close **整序列重跑**（closers 双关、lease 双释放）；②`StopLoop` 开头 `if !loopActive.Load() { return }` 正是 spec 点名要删除的「因 active 已变 false 跳过等待」形态；③`loopWg.Add(1)` 在 `loopActive.Store(true)` **之后**（发布先于登记，并发 Stop 可在 goroutine 未挂计数时 Wait 放行）；④`loopActive`+`loopTerminated` 双 bool 真源可漂移；⑤从未启动的实例 Close 后 outputCh 永无关闭。

**落地（agent.go 字段 + lifecycle.go 重写）**：
- 显式状态机 `loopState atomic.Int32`（idle→running→stopping→closed）取代双 bool；`loopDone chan` 由循环 goroutine 的 defer（recover 之后）**与 outputCh 一并关闭**——异常退出同样发布终态；`inject.go` 的 terminated 检查→`loopTerminatedNow()`（stopping 与 closed 同为拒绝接收终态，V15 语义逐位保留）。
- `StartLoop`：`loopWg.Add(1)` + `loopDone` 创建**先于** `Store(loopRunning)` 发布（sessionMu 内），杜绝 Wait 提前放行；running 幂等返回同一 ch；stopping/closed 终态错误原文保留。
- `StopLoop`：CAS running→stopping 胜者执行（meditation stop→cancel→Wait→closed）；败者按状态分流——idle 即返（无循环可停），stopping/closed **一律 `<-loopDone` 等同一终态**，不再跳等。
- `Close`：`closeMu/closeStarted/closeDone/closeErr` 四件套——首次执行 `closeOnce`，结果发布后关 `closeDone`；其余调用者（并发或后续）`<-closeDone` 后返回**同一 closeErr**。closers/lease/store-release 恰执行一次由此自然成立。
- idle 收口：`closeOnce` 开头 `CAS(idle→closed)` 成功（从未启动）→ 就地关 outputCh+loopDone——「输出恰关闭一次」覆盖未启动形态；失败（running）→ `StopLoop()` 等终态。

**测**（`agent/lifecycle_gate_test.go` 4 测）：并发×3 Close（慢 closer 80ms + 错误）→ closer 恰一次、三调用者同错误（spec Scenario「完整 Close 与执行交错」）；顺序×3 Close 幂等回放首结果；未启动 Close→输出已关+Start 拒绝；真实 loop 并发双 StopLoop（-race）→都见终态、三次 Stop 无 panic、终态 Start 拒。**fail-before 实跑**：TEMP 令 Close 直穿 `closeOnce`（还原无协调形态）→ 两测精确红（`the close sequence must execute exactly once`）→ 恢复绿，TEMP 零残留。既有 `tagent_agent_loop_test` 三处 `loopActive.Load()` 适配 `IsLoopActive()`。

**验证**：build/vet/gofmt 净；**agent 全包 `-race` 32.85s ok**（所有 Close/StopLoop 消费测回归）；rl/root/tests -short ok。未提交。§6.1 关 → §6.2（关闭顺序全列：拒接收→停生产者→取消等在途→runner→租约；轨迹 flush 在 runner 停后；删重复 closer 所有权）。

## §6.2 关闭顺序与 closer 所有权去重（2026-09-20）

任务：实施关闭顺序：关接收/新调用→停生产者→取消等待全部调用→关 runner→释放租约；轨迹在 runner 停止后 flush，删除重复 closer 所有权。（spec runtime-resource-ownership L7 后半句）

**现状 diff（改造前四处违规）**：
1. **顺序倒置**：`memStoreRelease`（租约释放，第5位）夹在 `contextManager.Close`（runner，第6位）**之前**——runner 尚在时 store/engine 可能已被末位 lease 关闭（runner 收尾写链无保障）。
2. **store 双所有权**：build_agent 把 memStore `RegisterCloser`（closers 循环第3位执行）**且** closeOnce 尾部 release/fallback 再关——注释自辩「Close is idempotent (closeOnce) and only invoked at process exit」恰是 spec 点名禁止的「既列普通 closers 又由 owner 重复关闭」形态；子 agent/壳各自注册共享 store，先关者破坏其他 holder 的末-owner 语义。
3. **recorder 双所有权 + 违反 flush 时机**：tagent.go `RegisterCloser(rc.trajectoryRecorder)`（closers 第3位=runner **前**关，违反「轨迹在 runner 停止后 flush」）且 closeOnce 尾部直接再关。
4. **关接收边界位置**：inbox `CloseDurable`（拒新 Enqueue 的 durable 接收边界）原在 closers 之后——接收关闭晚于资源关闭。

**落地（lifecycle.go closeOnce 重排 + 两处所有权删除）**：新序列逐步对位 spec——(1) 状态机翻转已在 Close 入口先行（inject/Envelope 拒绝，§6.1 的 loopTerminatedNow 覆盖 stopping）+ `CloseDurable` 紧随停循环（in-flight ack 已落，未 ack 信封留盘给下一进程）；(2) meditation（StopLoop 胜者内）+ workspace cleaner 停生产者；(3) StopLoop cancel+Wait 全部在途；(4) 注册 closers（actionTool/evoGit/mcpRegistry=工具与连接，runner 关前仍被在途调用使用→等在途后、runner 前）；(5) `cm.Close`（runner）；(6) trajectoryRecorder flush（runner 停后，唯一 owner=closeOnce）；(7) **最后** release 租约/隔离 store 直关（唯一 owner=release 尾）。build_agent 删 memStore closer 注册、tagent.go 删 recorder closer 注册（各留注释指向 spec 句）。§6.1 的 `Close` once 协调保证整序列每实例恰执行一次→「资源不重复关闭」两半（双入口、双次执行）至此全闭。

**测**（lifecycle_gate_test 追加 1 测）：`TestLifecycle_CloseSequence_RefuseFirstRunnerThenLeaseLast`——traceLog 记录每步+当时 loopState：断言 `tool@closed → runner@closed → store@closed` 全序（接收拒绝先行、closers 在 runner 前、lease 释放最后）+ storeSpy `closeCalls==1`（不在 closers 列表，release 尾唯一 owner）。**fail-before 实跑**：TEMP 还原旧倒置（release 提前到 closers 前）→ trace 首项 store 且双关 → `Not equal` 精确红 → 恢复绿，TEMP 零残留。

**验证**：gofmt/vet/build 净；agent 全包 `-race` 32.75s ok；root 全量 3.33s、tests -short 5.27s ok（tests 非 short 全量为长驻集成基线，历轮同口径）。未提交。§6.2 关 → §6.3（末 owner 关闭逆序：扫描器→engine→backend→写锁；借用壳无共享释放权——resources.go registry 侧）。

## §6.3 末 owner 关闭逆序与借用壳无共享释放权（2026-09-20）

任务：保留同代 backend/engine/per-key 协调，最后 owner 按扫描器→engine→backend→写锁顺序关闭；借用壳无共享释放权。

**盘点结论（registry 侧四条款已在 §2.4/2.5 期闭环，本轮验证+补 agent 侧缺口）**：

| 条款 | 现状证据 | 缺口 |
|---|---|---|
| 同代 backend/engine | `openedResource`/`resourceEntry` 同 entry 拥有同代析构（F7 注释）；`TestEngineOwnership_SharedReopenGetsFreshEngine` | 无 |
| per-key 协调不持全局锁做 I/O | `openingLock` 每 key 互斥、`releaseGen` 与 acquire 共用同一把、close 全在 `r.mu` 外；`TestReleaseCoordination_SamePathReopenWaitsForClose` | 无 |
| 扫描器→engine→backend→写锁 | `closeResource`：`StopProducers()`（遗忘生产者）→ `engine.Close()`（worker 确认停）→ `closeStore`（flush）；`releaseGen` 中 **flock unlock 在 closeResource 之后且未确认停时扣锁不放**；`TestCloseOrder_ProducersEngineBackendAndErrorReach` 双翼（engine 错误→backend 不 flush+持锁拒 reopen；store 错误→达 release 者+确认停→锁释放可 reopen） | 无 |
| 借用壳无共享释放权 | 装配面：`resolveMemoryStore` 一切 path≠"" 必经 `defaultResources.acquire`，每个 holder 持自己的 release 闭包（onceRelease 幂等+F6 防串代）；`TestOwnership_SurvivorKeepsWorkingAfterSiblingClose`。**agent 面缺口**：§6.2 后 fallback `else if` 是隔离 store 唯一直关入口，但无测锁定「release 非 nil 时绝不直关」 | **本轮补** |

**落地**：新测 `TestLifecycle_BorrowedShellNeverClosesSharedStore`（lifecycle_gate_test 追加）——`memStoreRelease` 非 nil 的 holder Close：release 恰调 1 次、storeSpy `closeCalls==0`（共享 store 只经租约出口，直关分支不可达）。**fail-before 实跑**：TEMP 把 `else if` 拆为独立 `if`（壳越权直关）→ 目标测精确红（expected 0, actual 1）且 §6.2 顺序测不受扰（证明注入只命中越权腿）→ 恢复 `else if` 绿，TEMP 零残留。

**验证**：gofmt/build 净；agent 全包 `-race` 32.52s ok；root（registry 13 测含）`-race` 4.55s ok。未提交。§6.3 关 → §6.4（poisoned entry：停止未确认保留 entry+资源/锁强引用、关闭错误达所有调用者、同路径拒新代、GC 持腿——releaseGen 现「detach+leak fd」形态 vs spec「保留 poisoned entry」的 diff 分析）。

## §6.4 poisoned entry 显式密封（2026-09-20）

任务：停止未确认时保留 poisoned entry 及资源/锁文件强引用，所有调用者获得关闭错误，同路径拒绝新代，其他路径可继续；覆盖 GC 期间持锁。

**现状 diff（核心形态违规）**：旧 `releaseGen` 未确认停时 **detach entry + 故意 leak fd**（注释原话「The fd and flock are intentionally leaked; the OS frees them on exit」）——持锁效果存在，但 registry 侧 entry 已删、store/engine/lockFile 强引用丢失，同路径拒绝仅靠 flock 撞锁（ErrStoreLocked），**正是 design 決策7 点名拒绝的「把故意丢失句柄引用当成持锁机制」**。

**落地（resources.go）**：
- `resourceEntry` 增 `poisoned bool + closeErr error`；新 `ErrResourcePoisoned`（与 conflict/locked 并立导出）。
- `releaseGen` 末租约析构三分：①确认停+unlock 成功→正常（flush 错误仍返回，不宣称安全，路径可 reopen——既有 store-error 翼保持）；②确认停但 **unlock 失败**→同样密封（无法确认锁释放=不可安全回收，design「无法安全回收同样保持 poisoned」延伸腿）；③未确认停→`poison()` **重插同代 entry**（store/engine/lockFile 原对象强引用+失败结果+原 fingerprint），errors.Join 保证错误必达 releaser。
- `acquire` 两处查表（首查+re-check）在 fingerprint 校验**之前**判 poisoned → 显式 `ErrResourcePoisoned: kind path: <原关闭错误>`（密封优先于冲突记账；re-check 防御腿 discard 资源自身 close 未确认仅 Warnf——该腿在 openingLock 串行下不可达，纯防御，注记于此）。
- 「所有调用者获得关闭错误」两层闭环：同 lease 重复 release 经 `onceRelease` 回放首结果（既有）；密封后**所有后续**同路径 acquire 拿到携带原故障的拒绝（新增）。

**测**：新 `resources_poisoned_test.go`——engine 错末租约释放后：entry 显式留存（`NotNil`+`poisoned`+`require.Same` 原 store 指针+lockFile 非空+closeErr 匹配）→ **双轮 runtime.GC()** → 同路径 acquire `ErrorIs(ErrResourcePoisoned)`+错误含 `engErr`；异 fingerprint 仍报 poisoned（密封优先）；他路径 acquire/release 照常；GC 后新 fd 探测 flock 仍 EWOULDBLOCK（持锁不依赖未回收句柄）。既有 `TestCloseOrder` engine 翼断言按语义升级 `ErrStoreLocked→ErrResourcePoisoned`（拒绝更精确）。**fail-before 实跑**：TEMP 删 `poison()` 回写（还原 detach+leak）→ 测在 spec 点名句上精确红（`the poisoned entry must be RETAINED, not detached + silently leaked`）→ 恢复绿，TEMP 零残留。

**验证**：gofmt/vet/build 净；root 全包 `-race` 4.38s ok（13 registry 测+全量）。未提交。§6.4 关 → §6.5（构造失败回收覆盖矩阵：能力拒绝/恢复准备失败/引擎降级/接线错误/执行壳；无法安全回收不开放 writer——poison 纪律已覆盖 release 路径，核 open() 闭包内半程失败的逆序释放面）。

## §6.5 构造失败回收全覆盖与未确认回收密封（2026-09-20）

任务：构造资源每取得即登记失败清理，覆盖能力拒绝/恢复准备失败/引擎降级/接线错误与执行壳；无法确认回收不得开放 writer。

**覆盖矩阵（spec 五类 × 现状/本轮）**：

| 类别 | 回收路径 | 测证据 |
|---|---|---|
| 能力拒绝（指纹冲突） | acquire 冲突拒——未持任何资源（先查表后 flock） | 既有 `TestOwnership_ConflictingConfigRejected` |
| 恢复准备失败（open() 半程） | rel/kv/store 逐步逆序：kv 失败 rel 为纯内存无外部资源；store 失败 `closeKV` 释放 kv 再放 flock | 既有注释+本轮新测两翼 |
| 引擎降级/接线错误 | `buildSharedResource` 内降级非失败（live-count/tombstone Warn 继续、engine nil→capacity-only 仍发布，closure 不返回 err——无回收义务） | 既有 `TestEngineOwnership_SharedBuildFailureDegradesToCapacityOnly` |
| 执行壳 | 构建 mid 失败 `buildOK` defer→`memStoreRelease`→releaseGen（leases 归零走 §6.4 poison 纪律；非末租约仅减数） | 既有 `TestOwnership_MidBuildFailureReleasesLease` |
| **无法确认回收不开放 writer** | **本轮缺口**：`closeKV` 吞错 + open 失败腿 `_ = unlockDirLock` 无条件放锁——回收不确认仍把写权开放给下一代 | **本轮补**：新测两翼 |

**落地**：
- `ErrReclaimUnconfirmed` 导出哨兵；`closeKV` 返回 error；openLocalFileStore/openRVStore 的 store 构造失败腿——kv 释放失败时 `errors 包装哨兵`上浮（干净释放路径逐字不变）。
- `acquire` open-err 分支三分：①含 `ErrReclaimUnconfirmed` → **不放锁**，`poison()`（无 store/engine 的密封 entry，仅 lockFile+失败结果，新代号）拒绝后续一切同路径；②常规失败 unlock 失败 → 同样密封（锁态不明=不可确认）；③常规失败放锁成功 → 原样返回（干净失败保持可重试，不过度密封）。
- re-check discard 腿（open 后冲突丢弃）的 close 未确认仍仅 Warnf：该腿在 per-key openingLock 串行+flock 前置下不可达（flock 已拿却见活 entry 需要对方无锁——registry 无此写入序），§6.4 evidence 已注记，纯防御不改。

**测**（新 `resources_build_reclaim_test.go` 两测）：`CleanReclaimStaysRetryable`（普通 open 失败→放锁→二次 acquire 成功开新代）；`UnconfirmedReclaimSealsWriter`（哨兵错→entry poisoned 留存+二次 acquire `ErrResourcePoisoned` 且 **open 闭包绝不重跑**（t.Fatal 探针）+flock 探测 EWOULDBLOCK=密封是实体持锁非记账）。**fail-before 实跑**：TEMP 还原无条件放锁旧形态→密封测在 `must seal via an explicit poisoned entry` 精确红→恢复（一次工具假失败，grep 证实已落盘）绿，TEMP 零残留。

**验证**：gofmt/build 净；root 全包 `-race` 5.47s ok；agent 33.45s、memory 0.81s 回归 ok。未提交。§6.5 关 → §6.6（生命周期总门：完整 agent 测共享 A/B、并发 Close×在途 turn、Start/Stop 竞跑、末次 reopen、旧 release、late attach、降级、热更——映射矩阵+缺腿补测）。

## Review 修复轮（C-1/M-1/M-2/M-3/M-4/N-1/N-2 → 并入 §6.6 总门）（2026-09-20）

CodeReview（§6.1–§6.5）发现 1C+4M+2N，全部确认成立并闭环：

| 发现 | 根因 | 修复 | fail-before |
|---|---|---|---|
| **C-1** StartLoop `Store(running)` 可覆盖 Close 的 idle 终态落定→goroutine 双关 outputCh panic；`loopDone` 锁外读写竞争 | idle CAS 不持 sessionMu + 发布非 CAS | Close 的 idle 落定移入 **sessionMu 临界区**（与 StartLoop 同一把锁）；StartLoop 发布改 **CAS(idle→running)**（败者终态错误）；output+loopDone 的关闭统一进 `settleOutput()`=`sync.Once`（结构上恰一次，goroutine 尾与 idle 支路共用） | TEMP 还原 post-publish Add+非锁 CAS→stress 测 `-race` 精确复现 DATA RACE（`Add(1)@347 × Wait@391`——race detector 顺手逮住 Add 挪位的次生违规，复原前置） |
| **M-1** idle 支路在等待在途**前**关 outputCh；`loopWg` 从不覆盖 one-shot/子调用 | 等待面只算 loop goroutine | 新 `cm.WaitForInFlight(timeout)`（`runnerInFlight` 覆盖全部 RunFlow turn：常驻+one-shot+子调用）；输出结算移到**等待之后**（goroutine 尾 `finishLoop` 与 idle 支路同型）；超时 Warn 不落死 | TEMP 删 idle 的 waitForTurns→`IdleCloseSettlesOutputAfterInFlightTurns`（turn 内观测 ch 已关=缺陷）精确红 |
| **M-2** fallback 判据 `release==nil` 过宽：借用执行壳（release nil+共享 store）可直关共享状态 | 判据混淆「无租约」与「独享」 | 显式 `memStoreOwned`：`TagentConfig.MemStoreBorrowed`（壳=true）→ fallback 条件 `release==nil && owned`；默认独享构建 owned=true 不破一 shot 尾关 | TEMP `else if true`（还原旧判据）→`ExecutorShellNeverClosesSharedStore` 红（closeCalls 1≠0） |
| **M-3** closeOnce panic→closeDone 永不关→等待者永挂 | 发布非 defer | Close 命名返回值 + **defer 发布**（panic 亦必达，panic 继续上抛执行者） | TEMP 还原直线发布→`PanickingCloseDoesNotStrandWaiters`（blockCloser 开窗使 joiner 确定性等待，3s 超时断言）红于 stranded 句 |
| **M-4** 原 BorrowedShell 测前提与真壳错位（钉 if/else 巧合） | 测形态选错 | 真壳形态测（release nil+共享 store+owned false→零权限）；原测保留（leased-holder 腿仍有效） | 同 M-2 |
| **N-1** 两密封腿错误形状不一+re-check discard 丢 `workerStopped` | — | ErrReclaimUnconfirmed 腿 `errors.Join(原错, ErrResourcePoisoned wrap)`（三判可辨）；discard 腿未确认停→**持锁+报 sealed**（防御腿注记不可达） | 既有 poisoned/reclaim 测覆盖 join 判型 |
| **N-2** 注释漂移×4 | — | `SetTrajectoryRecorder`（改为「勿再 RegisterCloser」）；const 头旧顺序注释（删，指向 closeOnce）；`engine_bridge.Close` 注释（重写为 §6.2/6.3 所有权现实）；`rl/http_api_test` 注释块旧字段（更新为 loopState） | 静态 |

## §6.6 生命周期总门（八腿映射矩阵）

| 腿 | 完整 agent/贯通证据 |
|---|---|
| 共享 A/B Close 隔离 | agent `retention_owner_test`（§5.9 双 agent 共享 store 不泄租约）+ root `SurvivorKeepsWorkingAfterSiblingClose` |
| 并发 Close × 在途 turn | `ConcurrentCloseSameResult`（交错=spec Scenario）+ `IdleCloseSettlesOutputAfterInFlightTurns`（turn 真在途）+ M-3 panic 腿 |
| Start/Stop 竞跑 | `StartCloseRacesNeverDoubleSettle`（100 iter `-race`，无 panic/无复活）+ `ConcurrentStopLoopsWaitTerminal`（真实 loop 双 Stop） |
| 末次 reopen | root `ReopenAfterLastClose` + §5.10 跨进程恢复（全新 store 实例重开读旧事实） |
| 陈旧 release | `StaleDoesNotAffectNewGeneration` + agent 层 onceRelease |
| late attach | `arm_barrier_test`（§5.8 bus Arm 屏障成功/失败保账腿） |
| 降级 | `SharedBuildFailureDegradesToCapacityOnly`（entry 降级仍发布） |
| 热更 | 壳=热更重建议程：`ExecutorShellNeverClosesSharedStore`（壳 Close 对共享零权限）+ §6.5 壳构建失败走 releaseGen（`MidBuildFailureReleasesLease`） |

**验证（终态电池）**：gofmt/vet/build 净；agent `-race` 32.84s（含 11 lifecycle_gate + 压测）；root `-race` 8.05s；memory 0.78s；rl 0.62s；tests -short 5.17s。TEMP 零残留。未提交。**Section 6（6.1–6.6）全部关账。**

## §7.1–7.3 恢复提示真实呈现边界（2026-09-20）

**盘点核心事实**：§4.5 已建立 design 決策8 所描述的「小型模型提交装饰器」`executionGateModel`（context_manager.go L495 唯一构造点，buildLLMAgent 冷启动/RebuildExecutor 共用）——Section 7 的多数条款是**验证该边界对上 §7 规格句子 + 补测缺腿**。

### §7.1 消费从装配回调移入实际模型提交边界【盘点即闭 + 测 + 误导面删除】
- 消费点唯一：`TakeRecoveryNotice` 全仓仅 `execution_gate_model.go:46` 调用（grep 复核；BeforeModel 回调链零消费）。复制切片 append（`cp := *req; cp.Messages = append(copy…, notice)`）在所有装配回调之后（装饰器在 request 组装完成后才进入）。diagnostics 不消费（`cm.recovery` 与 notice 分离，既有 `DiagnosticsStillReadable` 测）。
- 新正证：`TestNotice_RealRequest_VolatileRunner` 在**真实 runner 路径**捕获请求——`[context-guard]`（装配回调产物）在首、`[recovery]` 在尾，直接钉住「追加在所有装配材料之后」。
- 附带清扫：NewContextManager 内 48 行 dead 伪装配块（hotswap-fix 5.7 抽取残留，构造了从未消费的 NON-gated option list——装配面与执行面不一致的误导源）删除，NOTE 指向 buildLLMAgent 唯一构造。
- **fail-before 实跑**：TEMP 在 `assembleRequest`（装配回调面）加 `TakeRecoveryNotice` → 两框架路径测（RealRequest/HotRebuild）精确红（边界处无提示可注入），装饰器层测不扰 → 恢复绿。

### §7.2 状态绑常驻 cm、冷/one-shot/热更共用、摘要模型不注入【盘点即闭 + 2 新测】
- 状态面常驻：`RebuildExecutor` merged=execCfg 快照、**状态面永不换**（代码注释+新测 `TestNotice_HotRebuild_StateContinues`：pending 提示跨热更恰一次经新执行面消费、不重挂不重复；一 Run=一底层调用=无套嵌双包）。
- 冷启动/one-shot：同一 `NewContextManager→buildLLMAgent` 构造（无 StartLoop 的 runner.Run 亦经 gate——HotRebuild/RealRequest 测即 one-shot 面驱动）。
- 摘要模型不注入：结构性——`executionGateModel` 构造唯一在 buildLLMAgent（model 面），`compress.WithSummaryModel(cfg.SummaryModel)`（agent.go L603）拿原始 config model，compress 包不知 gate（依赖方向编译期保证）。
- channel+iterator 双路实际调用消费：iterator 懒既有测（`IteratorLazilyConsumesNotice`）；channel 路本文件并发/短路测覆盖。

### §7.3 六场景消费纪律【新测 5 支（2 腿既有）】
| 场景 | 测 | 断言 |
|---|---|---|
| 装配后短路 | `ShortCircuitAndCancel_StaysPending` | 未入底层→pending 原样、下次真调用携带一次 |
| 调用前取消 | 同上（cancel-before-entry 腿）+ `IteratorLazily`（既有，创建即取消不消费） | 不消费 |
| 空批/输入失败 | 结构论证：无模型调用则 gate 不可达（消费唯点在 gate 内）；`BlocksUnverifiedCredential`（既有）钉「blocked→inner 零调用→零消费」 | 不消费 |
| 底层失败 | `UnderlyingFailure_ConsumedNoResend` | 进入底层即消费；provider 错误上浮；后续调用不重发；diagnostics 可读（=Scenario L57-59） |
| 并发竞争 | `ConcurrentCalls_ExactlyOneCarries`（8 goroutine -race） | 8 达底层、恰 1 携带 |
| iterator 未迭代 | 既有 `IteratorLazilyConsumesNotice` | 创建不消费不阻塞 |

**验证**：gofmt/vet/build 净；agent 全包 `-race` 32.13s ok；TEMP 零残留；未提交。→ §7.4（真实请求捕获四通路+durable 面+回执/独立重启历史无提示）§7.5（呈现门）下轮。

## §7.4 真实请求捕获四通路 + 事实/回执/独立重启历史无提示（2026-09-20）

任务：真实请求捕获覆盖 durable/volatile/one-shot/热更；full 无提示、partial/failed 一次可见，事实/投影/回执及独立重启历史均无提示文本。

**八项映射（全在真实框架路径或真实耐久链上断言，非手搭 request）**：

| 项 | 测 | 断言面 |
|---|---|---|
| durable | 新 `DurableTurn_RealRequestCarriesOnce`（verified credential ctx → cm.RunFlow→runner→gate） | 捕获 request 恰 1 携带；后续 durable turn 零携带 |
| volatile | `RealRequest_VolatileRunner`（runner.Run 装配路径） | `[context-guard]`（装配产物）在前、`[recovery]` 在尾、恰一次 |
| one-shot | 同上两测（无 loop 的 runner.Run/RunFlow 即 one-shot 提交面） | — |
| 热更 | `HotRebuild_StateContinues`（RebuildExecutor 新执行面） | pending 跨面恰一次、不重挂、一 Run=一底层调用 |
| full 无提示 | 既有 `FullRecovery_NoNotice` + `projection_fallback_test:112` | request 长不变 |
| partial/failed 一次可见 | `recoveryNotice` 双模板 + 上四测 countCarried==1 | — |
| 事实/回执无提示 | 新 `DurableChain_FactsReceiptAndRestartClean`：真实 localfile+reliable inbox 全链（publish→pull→prepare→persist→**finishDurableBatch 写回执**）逐事件 GetEvent 扫 Content+Metadata | live 实例零命中；全程无模型调用→提示仍 pending（不消费腿同测钉） |
| 独立重启历史 | 同上 **independent reopen 腿**：同 dir 全新 `NewLocalFileKV`+`NewFileSegmentStore` 实例重扫（非缓存读回，§8 口径） | 零命中 |

**fail-before 实跑 ×2**：①TEMP `assembleRequest` 装配面消费 → `DurableTurn` 精确红（边界无提示可注入）；②TEMP `buildBusFact` 把 pending 提示混入事实 Content（「提示成为事实」violation 模拟）→ `DurableChain` live+reopen 双相红（NotContains 命中）→ 均恢复绿，TEMP 零残留。投影腿既有 `NotInProjection`（refs 长度+原事实键不变）。

## §7.5 呈现门（关账）

- **关联测试及 race**：agent `-race` 31.87s（含 7 支 notice 边界测+并发/热更/iterator）、plugin 3.16s、event 1.77s、memory 3.41s、agent/compress 4.68s、root（装配/热更/registry）5.88s、rl 0.40s、tests -short 5.36s——全 ok。
- **system/历史/工具声明不被改写**：`RecoveryNotice_SystemPromptUnchanged`（装饰器层 system 头原样）+ 复制切片结构（`cp := *req` 浅拷贝，Tools/GenerationConfig 指针共享不改写；RealRequest 测钉装配材料完整保留）+ 历史不携带（同 session 二轮次测：`never becomes session history` 断言）。
- **可选模型能力未被隐藏**：`IteratorLazilyConsumesNotice`（IterModel 透传）+ `model_entry_grounding_e2e_test`（框架实际使用 GenerateContentIter 的正证+装饰器缺能力会显形）+ 非 Iter inner 的 channel 适配器（`GenerateContentIter` 对纯 channel 模型的合成路径，execution_gate_test L119 取消腿在位）；Info/Close 委托既有测覆盖。
- **diagnostics 持续可查**：`DiagnosticsStillReadable`（消费后 RecoveryResult 仍返回）。

**验证**：gofmt/vet/build 净；TEMP 零残留；未提交。**Section 7（7.1–7.5）全部关账。**

## §8.1 常驻 durable 集成基座——五端对账（2026-09-20）

任务：建立 inbox-v2+localfile+真实 runner/plugin 常驻集成场景，模型 mock/渠道，对账原始接收/事实/请求/结果/清理各端。

**落地**：新文件 [tests/resident_durable_e2e_test.go](tests/resident_durable_e2e_test.go)——`tagent.New` 完整组合根（Memory localfile + Reliability.BusSpillDir，mock 模型经 WithModel）+ `TestResidentDurableE2E_FiveSurfaceReconciliation`：

| 端 | 断言 |
|---|---|
| 原始接收 | `InjectEnvelope(A,B)` → `durable=true`（BusSpillDir 生效）+ requestID 非空稳定；C 单发 `recC.Durable` ✓；两接收身份互异 |
| 事实 | A/B/C 原文各经 `QueryEvents(Keyword)`+`GetEvent` 召回，EventType=external_input、Content 为原文非摘要 |
| 实际请求 | **屏障时序（非 sleep）**：AB 的 request#0 被捕获（含 A+B 原文，一批一 turn）后才发 C → request#1 含 C（下一业务 turn） |
| 结果 | 输出事件×2 携 `trigger_source=user`；回执 fact 恰 2（一业务 turn 一回执，无逐信封双回执） |
| 清理 | inbox 叶 `Outstanding()` 空 + spill 目录 `.json` 残留计数 0（全部 accepted 落到 processed-cleaned，无静默丢失） |

**fail-before 实跑（含一次自我纠偏）**：第一轮 TEMP `if err := os.Remove(path); false && err != nil` **并未跳过删除**（if-init 仍执行）→ 测照样绿，识破后改为 `if err := (func() error { return nil })(); ...` 真空跳过 unlink → e2e 对账 15s 内 FAIL（清理/后续链捕获残留）→ 恢复绿，TEMP 零残留。教训：TEMP 注入必须核验其确实改变了行为（grep+行为双证），if-init 副作用是陷阱形态。

**已登记先存 race（非本轮引入，HEAD worktree 复现×1/5 runs）**：
- 签名：`session.(*Session).Clone()`(session.go:95) 读 × `session.(*Session).UpdateUserSession()`(session.go:476) 写；路径 flow `cloneStateDeltaSession` × inmemory `AppendEvent` 异步 goroutine。
- 版本/测试：trpc-agent-go **v1.10.0**；`TestCausalChain_WithToolCall`（tests -short -race，间歇 ~1/3 概率）。
- 归因：栈两侧均在框架内部（本仓 hook `agent.go:502` 只是 hook 链上的既有读侧）；`git worktree HEAD(a16fdce)` 基线同样复现 → 先存缺陷，不属本变更引入；修复归上游升级轨道（本变更不遮蔽：其余 tests -race 腿无此签名仍按失败处理）。
- 口径说明：tests 全包 -race（非 short）超 180s 默认时限属长驻基线既有形态（§6 轮同款），本轮以 `-short -race` 为门。

**验证**：新测 ok 0.65s；tests `-short -race` 除已登记签名外无其他 FAIL；reliability 包 10.26s ok；gofmt/vet 净；未提交。

## §8.2 wechat-bot 独立模块——chat_id 世系宿主验收（2026-09-20）

任务：验证 chat_id 从接收到模拟发送目标、内部任务与缺世系输出扣留，禁止连接真实渠道。

**落地**：
1. **seam 抽取**：main.go 新增 `resolveDeliveryTarget(metaChatID, lastActive)`（世系投递唯一目标规则：stamp 原样直通 > 最近活跃回退 > 扣留），循环体内联两段 if 迁移至 seam（顺带修掉 `v.(string)` 无保护断言的 panic 风险；回退观测日志保留并泛化到 triggerSource）。
2. **新测** [main_delivery_target_test.go](examples/wechat-bot/main_delivery_target_test.go) 六支，全在真实 meta 契约（`ParseEventMeta` 读 `meta_chat_id` StateDelta）+ fakeSender 记录器上：接收 chat-77 → 模拟发送目标 **逐字=chat-77**（非回退值）；task 无 stamp → 回退最近活跃；双空 → 扣留且 sender 零调用；`persist→fresh map→seed` 重启续存腿；**guard 测**把「禁连真实渠道」变成可执行检查（扫描全部 _test.go 禁 wechat.NewBot/SendLongText/http 构造，模式串拼接防自匹配）。
3. 缺世系扣留源层五测（resolveTriggerSource fail-closed 白名单，含 task-unstamped 降级扣留）先存，本轮引用不重复。

**fail-before**：TEMP 令 seam「忽略 stamp 恒用 lastActive」（发送目标被改写 violation）→ Stamped 测精确红 → 恢复全模块绿，TEMP 零残留。

**验证**：模块 `go vet ./...` 净、`go test ./... -count=1` ok 1.0s（既有 16+ 测全绿）；行为保真注记：新 seam 与原内联逻辑对 chatID=="" 分支等价，非空路径不经过 seam（循环守卫保留）。未提交。

## §8.3 接收侧崩溃窗口矩阵（2026-09-20）

任务：子进程 failpoints 覆盖接收 tmp/rename/dirsync、claim、prepare、每条输入提交；父进程独立 reopen 验证身份/来源/计数及未确认材料。

**生产 seam（最小测试钩子，与 testGateHook 同型）**：inbox.go 新增 `testWriteStageHook(stage)`——writeEnvelopeFile 在 **tmp 落成后**（fsync+close 完成）与 **rename 落地后（dirsync 前）** 各触发一次；Enqueue/claim/prepare 重写全部流经 writeEnvelopeFile，一个 seam 覆盖三步骤×两阶段。生产恒 nil 零开销。

**reliability 矩阵**（新 [inbox_receivecrash_matrix_test.go](agent/reliability/inbox_receivecrash_matrix_test.go)）：child `-test.run` 自调用 + env `TAGENT_CRASH_AT=<call>:<stage>`，hook 内 `os.Exit(0)`（无 Close 无 defer=真崩溃）；parent **全新 Inbox 实例** reopen 断言四窗口：

| 窗口 | 断言（独立读回） |
|---|---|
| 接收-tmp | accepted 集 0；**未确认材料=1 个 tmp 孤儿且含全信封字节**（可见可解释、未被冒充接收） |
| 接收-renamed | 1 封 pending、身份（RequestID/Source）+slot-1 原文逐字在；无预留键 |
| claim | 落地 claimed/Attempts=1 → open **requeue 回 pending/Attempts=2**（§5.7 恢复语义：崩溃 claim 永不静默丢） |
| prepare | 冻结键+双 slot prepared **跨 requeue 完整存续**（replay 必复用不重铸——§5.3 幂等核心） |

**每条输入提交窗口**（agent 包，新 [crash_input_commit_test.go](agent/crash_input_commit_test.go)）：child publish A,B→Pull 并批→prepare OK→**persist(A) 后、persist(B) 前死**。parent 双独立 reopen（fresh FileSegmentStore+fresh bus）：计数恰 1 输入事实、A 的 key=冻结 prepared 键（身份）、B 原文无损在信封（未确认材料）→ replay 整批 → **重 persist A 幂等不双写（计数 2）** → finish → 每信封恰一回执（2）→ ack 后信封目录清空（processed-cleaned 终态）。

**两次诚实记录**：
1. **文件名陷阱**：初版命名 `*_windows_test.go` 被 Go 的 `_windows` GOOS 后缀规则静默排除（darwin 下 0 tests to run）→ 改名 `receivecrash_matrix`。教训入档。
2. **fail-before 双形态**：①TEMP「requeue 不落盘」使 parent Pull 永挂（claim 项被 nextClaimable skip）——挂起是过强 violation 形态，不作 fail-before 样本，记录为矩阵敏感性旁证（丢失即卡死无静默通过）；②确定性注入「requeue 丢 Attempts 计数」→ claim/prepare 双窗精确红 → 恢复绿。另一次假注入自纠：replace 函数类型不匹配 build failed、grep 模式漏子测试全名两次识破后重做，红迹以 `--- FAIL: .../claim` 行实证的为准。
3. 期望修正记录：dirsync 窗口与 renamed 窗口崩溃落点相同（dirsync 未完成即死），其**失败返回**语义 §3.2 已有注入测；本矩阵以崩溃形态覆盖。`Outstanding` 信封语义（每信封回执数=2 非每批 1）先错后正。

**验证**：reliability `-race` 15.1s ok；agent 单测 ok；gofmt/vet 净；TEMP 零残留；未提交。dirsync 失败（非崩溃）形态先存 §3.2 测引用。

## §8.4 完成侧崩溃窗口矩阵（2026-09-21）

任务：子进程 failpoints 覆盖模型返回、completion、receipt、unlink/dirsync；验证 completion 耐久后不重执行、取消不伪造终态、无 completion 的重复执行边界诚实。

**形态决策**：完成侧窗口以**步间崩溃**（子进程在真实协议步骤之间 `os.Exit(0)`，磁盘态与进程内崩溃等价且更确定）而非 write-envelope hook——免跨包 seam（`testWriteStageHook` 包内私有，agent 不可见；export_test 亦不跨包，放弃注入式方案）。unlink→dirsync **进程内失败**形态由先存 `TestAck_UncertainDirSyncRetainsCapacityThenRetryCompletesBarrier`/`TestAck_OwedBarrierStillFailingKeepsCapacityAndLease` 钉住（引用）；掉电级 dirsync 缺失属 §9.2 已降级维度。

**新文件** [crash_finish_matrix_test.go](agent/crash_finish_matrix_test.go)，child 走真实序列（publish→pull→prepare→persist→[finish 各相位]），parent **全表面独立 reopen**（fresh leaf+store+lease+guard）驱动真实 `ReconcileOutstanding`：

| 窗口 | 断言（全绿） |
|---|---|
| 模型返回后死（pre-finish） | 原始盘上**无 completion**（`NotContains "completion"`）+ 无回执 → 无耐久完成时**诚实重跑**：requeue→整批可再 claim→prepare/persist 幂等→finish→ack 清空 |
| completion 耐久后死 | 原始盘 `"completion"` 在 → §5.7 恢复**只重投回执**（`ReceiptsAdded=1`；恢复进程**无模型接线——不重执行为结构证**）→ 输入事实 1、回执恰 1（冻结 reserved key）、信封清、`DurablePending=0` |
| receipted 后 ack 前死 | 原始盘 `"state":"receipted"` → 恢复 `ReceiptsAdded=0`（已耐久不加第二张）+ owed ack 补完成（barrier+恰一次释放）+ 信封清 + 回执仍 1 |
| 取消不伪造终态（同矩阵非崩溃兄弟） | 真实 finish 喂 cancelled outcome：claim 保留、无 completion、无 receipted、**零回执事实**、`DurablePending>0`（诚实待重放） |

**fail-before 三层记录（cancel 腿的纵深防御实证）**：
1. TEMP 拆 finish guard → 绿——被 builder guard（第 2 层）吸收；
2. TEMP 双拆（guard+builder）→ 仍绿——被 completion 合法性（第 3 层 `unknown batch_result ""`）拦截，**ERROR 日志实锤注入生效且系统仍不伪造**；
3. 断言敏感性正证：TEMP 直接向 leaf 手写伪造 completion → cancel 测**精确红**（含完整 JSON 的失败输出）→ 恢复绿。结论：「取消不伪造终态」由三重冗余保证，断言非永绿；leaf 对 raw completion 的不校验是分层设计（合法性归消费端/恢复端，第四层）。
中途自纠：shell 引号打断 perl（改 python 注入脚本）、`_ = ok` 作用域编译错（去掉即愈）。

**验证**：agent `-race`（crash/finish/reconcile 全家桶）8.41s ok；reliability 9.83s ok；gofmt/vet 净；TEMP 零残留；未提交。

## §8.5 三十轮确定性独立进程重启 + A+B/C 组合对账（2026-09-21）——并借此关闭一个真实协议洞

任务：完成30轮确定性独立进程重启及A+B/C组合对账，不用sleep、旧内存投影或优雅Close代替；覆盖共享关闭与故障恢复交错。

**落地** [restart30_matrix_test.go](agent/restart30_matrix_test.go)：单耐久根上 30 个**真子进程**轮替（每轮独立 spawn，`os.Exit` 死点、无 Close），确定性日程 `mode=ab|c`（r%2）× `target=post-persist|post-completion|post-receipted|post-ack`（r%4；round29 强制 post-receipted 给终门留 receipted-unacked 交错料）。每轮 child：**真实 §5.7 ReconcileOutstanding 开场**（RECON 行强制）→ 预置批注入（AB 两信封并一 Pull turn、C 单信封=两种接收模式覆盖）→ 逐信封 per-envelope completion/receipt 相位推进至死点。每轮之间 parent **全新实例**原始盘审计（重复键=0、回执≤输入、身份∈日程集）。终门：排空重放收敛（`DurablePending` 界，非 sleep）→ **45 输入全落+45 回执逐信封恰一+内容全可召回**（独立新 store 实例读回）→ 共享关闭（bus.CloseDurable+store.Close 真实关面）→ **再独立 reopen** 验证关后状态稳定幂等（reconcile 全零+链长不变+无 tmp 孤儿）。5.19s 全绿。

**真发现（本测试的核心价值）**：轮2 child `CONFLICT (same key, different content)`——**雪花键=秒粒度+进程内 seq，跨进程代重启同秒必撞**（r30 节奏实测多轮同秒）；且冻结键 replay **永远**再冲突 → 原协议把 store 冲突全归 transient = **活锁**；leaf 注释承诺的 disposition 层（「isolate/completion=failed 由 commit 协议叠加」）**从未落地**。三层修复（均在本变更 spec 授权内：persistent-event-loop 四态「隔离」条款 + §8.5 对账收敛要求）：
1. `memory.RaiseSnowflakeFloor(pid, maxKey)`（types.go 导出，单向、同秒 seq 耗尽进位下一秒）；
2. store 全量扫描顺路 seed：`RebuildLiveCounts`/`recomputePartition`（wiring.go:420 冷启动必经）→ **新进程代永不再发已提交键**；
3. `persistBusEventCommitted` 双值分类（Duplicate/Forgotten=deterministic）+ `submitDurableBatch` 对 deterministic **隔离**（复用 QuarantineEnvelope 出口，submitConflict 停自动消费）——即使未来出现新键冲突形态，四态归类兜底成立（不活锁、可解释）。
r30 child 顺带修正为**逐信封**相位完成（backlog 混批=协议正常，单 completion 构建整批是测试建模错误，撞见 `duplicate slot 0` 即证）。

**fail-before ×2 精确命中**：8.5a 拆 submitDurableBatch 隔离分支 → 新协议测 `TestSubmitDurableBatch_DeterministicStoreConflictIsolates`（伪造同键异内容 prepared fact → 断言 submitConflict+quarantine 恰 1+毒写不沾链）红；8.5b 拆键地板 seed → r30 红（冲突材料重现）。均恢复绿。新增 memory 地板双测（同秒不撞/单向）。

**诚实注记**：①键跨代唯一性的**充分**解（持久化 seq 状态/位布局改造）超出本变更范围——现修复以「开盘即从耐久链抬地板」覆盖重启场景，同秒**双活进程**共享 partition 发键仍靠上游 flock 单 owner 前提（runtime-resource-ownership 已保证），如需多写者再立变更；②r30 的 submit 冲突兜底路径因地板在位而 rare-path，其独立证明由协议测 8.5a/b 双腿承担。**验证**：memory -race 3.3s、agent -race 72.9s（含 30 子进程 race 态）、全回归面 ok；TEMP 零残留；未提交。

## §8.6 受管目录重置演练（2026-09-21）

任务：在临时受管目录演练不兼容旧数据直接重置及一致恢复单元清理；覆盖路径越界/非受管内容/存活writer拒绝、证据文件不删除、当前格式损坏不误清理，不操作未指定的真实目录。

**决策**：不新增公共生产 API——管理面编排放 tests 侧（与先存 `TestDrill_UpgradeTreatsLegacySpillAsInertThenResets` 同模式，修改范围最小）；安全护栏复用既有原语（`acquireDirLock` flock、leaf `ResetTransitional` 的 confirm/pending/underDir/quarantine 守卫）。新文件 [reset_managed_drill_test.go](reset_managed_drill_test.go)（root 包 in-package）。

**编排 `drillResetManagedUnits`（全或无）**：Gate0 confirm → Gate1 store flock live-writer 探针 → Gate2 **验证先行**（kv+store open+RebuildLiveCounts 当前故障拒绝、bus open 的 quarantine-undispositioned 阻断）→ 只删受管 pattern（kv.json+tmp、inbox-v2 直属 *.json（quarantine/ 不匹配）、anchor/<agent>.json）→ **磁盘一致新实例**跑 leaf transitional sweep → 各单元同步清空（禁半重置）。

**演练腿（单测全绿 0.56s）**：
| 腿 | 结果 |
|---|---|
| confirm=false | 拒绝+零变化 ✓ |
| quarantine 有未处置证据 | **拒绝**且证据文件幸存、材料零变化 ✓（「当前格式损坏不误清理」+「证据文件不删除」一箭双雕；operator 挪走后才可重置） |
| flock 被占 | `ErrStoreLocked` 拒绝+零变化，释放后可行 ✓ |
| 一致单元清理 | v1+*.spill+kv.json+anchor+v2 信封**同灭**（无半重置悬挂）✓ |
| 非受管内容 | `storeDir/user-notes.txt`、README 幸存 ✓ |
| 路径越界 | inbox-v1 内 symlink：链接灭、**外部 victim 完好**（os.Remove 不跟随）✓ |
| 重置后启动 | 全新组合根 boot 成功、reconcile 全零（授权丢弃无悬挂）、StartLoop 注入走**当前路径**达模型（Contains 断言——context-guard 装饰先例 §7.4）✓ |
| 真实目录纪律 | 全程 t.TempDir 受管根，evidence 注记 ✓ |

**过程诚实记录**：①leaf 实例账目与磁盘脱钩（probe 打开→文件被编排删除→旧实例 pending 拒 ResetTransitional）→ 修正为「验证 open 即用即关 + sweep 用删除后的**新实例**」；②`unlockDirLock` 已含 Close 的双关错误；③断言过严（精确相等 vs guard 装饰）两处自纠。
**fail-before**：TEMP 拆 live-writer 闸 → LEG c 红（`store is locked` 未出）→ 恢复绿，TEMP 零残留。

**验证**：root `-race` 4.37s ok；agent 39.2s / tests 5.9s ok；gofmt/vet 净；未提交。

## §8.7 淘汰清单收口（2026-09-21）

任务：按淘汰清单删除本次全部过时/兼容/过渡实现与孤立测试并更新真实调用方；静态依赖检查与全新启动/当前状态重启/重置后启动共同证明仅最新路径生效，不以"不再调用"代替删除。

**静态审计（先于测试编写）**：逐类核验生产代码——旧协议解析/v1 消费（0，仅 classify 只读枚举）、旧确认接口（0，RecordReceipt 唯凭据形态）、SpillStore 实现体（0，文件即惰性 transitional）、loopTerminated/loopActive 旧 bool（0）、collectUnconfirmedReceipts 旧收集链（0）、persistInboxReceipt 新键铸造（0）、10.5 旧配置键（0 活代码）、重复 owner（§6.2 已清）。**新文件** [elimination_latest_path_test.go](elimination_latest_path_test.go)：
- `TestEliminationList_ZeroLegacySymbols`：9 项被禁符号 × 全部非测试 Go 文件（root/agent/memory/event/tool 顶层）**活代码行**扫描（注释行豁免——注释记录删除历史是合法的，首轮扫描即被 build_agent.go 的删除注记红出，据此加入豁免并把语义写进测试注释）。
- `TestLatestPathOnly_ThreeBootStates`（0.33s 全绿）：STATE1 全新目录 boot+真实 durable turn 达模型；STATE2 当前状态重启——前态事实**进投影**（tri-fresh-turn 可见）+ **旧标记惰性正证**（v1/*.spill 植入后逐字节不变：不读、不迁、不删）；STATE3 复用 §8.6 编排重置后 boot——reconcile 全零 + 新 turn 成功 + **重置前输入绝不复活**（清旧不冒称已处理）+ 重置同时清掉 legacy 集。

**裁决记录**：`persistBusEvent` bool wrapper（§8.5 引入）**保留非淘汰**——存在真实生产调用方（output_overflow 尽力而为登记，注释明示失败仅日志、文件才是耐久记录）+ 19 测试文件常规使用；非"仅为兼容保留"的空壳。三态与静态检查互为充要（design L189）。

**fail-before**：TEMP 向 config.go 活代码行注入 `"task_stale_after"` 字符串 → 静态测精确红（config.go:965 定位）→ 恢复绿；TEMP 零残留。
**验证**：root `-race` 5.24s ok；gofmt/vet 净；未提交。

## §8.8 组合验收门（2026-09-21，Section 8 收官）

**最终 diff 重映射**：工作树 146 项变更（tracked 73 files，+6326/−2967，含 `agent/reliability/spill.go` 物理删除与 §8 新测 12 个未跟踪文件）；生产面聚焦 agent/、reliability/、memory/、event/、tool/、config/build_agent/wiring、examples/wechat-bot——逐文件归属已在 §2–§8 各节 evidence 建立"源码→单元/组合/消费者"映射，本节汇总不重复。

**双模块门禁（spec L45 全项）**：
| 项 | 命令 | 结果 |
|---|---|---|
| 根模块 build/vet | `go build ./... && go vet ./...` | OK |
| 根模块 short | `go test ./... -short -count=1` | 全 ok，零 FAIL |
| wechat-bot 独立模块 | build/vet/`go test ./... -short` | 全 OK（0.94s） |
| 定向 race（受影响全包） | `-race ./agent/... ./memory/... ./event/ ./plugin/ ./rl/ ./evolution/ ./tool/... . ./tests/` | agent 74.7s ok、reliability 17.2s ok、root 5.2s ok、其余全 ok |

**唯一未决项与豁免登记（spec：race 豁免须登记具体签名/版本/测试，不遮蔽）**：`tests` 包 `-short -race` 存在**先存框架数据竞争**——
- 签名族（两条交替显现，竞争随机性如实记录）：`trpc-agent-go@v1.10.0 session.(*Session).Clone(session.go:95)`（flow tool-state-delta 快照读）× inmemory `AppendEvent/UpdateUserSession`（写）；
- 涉及测试：`TestCausalChain_WithToolCall`（§8.1 已登记+HEAD 复现）、`TestInjectBusInputs_DuringReAct`（本轮补登记，**HEAD worktree 复现 FAIL**、测试文件零 dirty——归因框架先存非本轮）；
- 断言层两测逻辑均通过，FAIL 仅来自 race detector 判负；**未以重跑成功冒充通过**，登记保留为发布证据的已知先存缺陷条目，修复归后续框架升级变更。

**淘汰清单零遗留复核**：§8.7 `TestEliminationList_ZeroLegacySymbols` 在门内重跑 ok。
**未运行项**：`tests` 非 short 长驻基线（§8.1 既有口径：需外部依赖，CI 长窗口位）与环境阻塞：无其他。
**验证**：本节纯汇总零代码改动；TEMP 零残留；未提交。

## §8.9 评审 7677c07 修复轮（2026-09-21）

CodeReview 子代理评审结论：**无必须修项，可作发布候选**；锁序/计数真源/保留租约/§8.5 守卫/崩溃矩阵诚实性逐项确认。两条反馈均落实：

1. **🟡 消除测非递归**：`TestEliminationList_ZeroLegacySymbols` 改 `filepath.WalkDir` 全仓递归（豁免 `.` 前缀目录与 openspec；首版误把根 `.` 自身 dot-skip 致 files 空，被 `require.NotEmpty` 防线红出→修正）。此前"全绿"实为覆盖面假象，修复后仍全绿（子目录当前无逃逸的评审判断得到独立证实）。
2. **🟢 kv tmp glob 名不副实**：`kv.json.*.tmp` → 真实布局 `kv.json.tmp`（LocalFileKV snapPath+".tmp"，源码核验）。

**新发现（评审外，由 drill 全包 race 暴露并登记为第 4 先存签名）**：`steer.(*Queue).Close(steer.go:74) × cloneStateReflectValue(invocation.go:1658)`——v1.10.0 框架把带 mutex 的 Queue 附入 invocation state 并被 reflect 浅拷贝，runner 收尾 defer Close 与在飞 View clone 无外方可 synchronise。归因证据链：双栈 accessor 全框架帧；产品代码 `steer.`/`GetStateValue` 零引用（grep=0）；**变更前 HEAD a16fdce root -race 6 轮零出现**（旧 HEAD 无多轮 boot/Close 测试形态）→ 缺陷在框架、触发面由本变更测试扩大；生产单代优雅 Close 同样可撞（真实 latent 风险，非测试伪影）。处置：①tri 三态与 drill LEG e 改**独立子进程 boot**（证据层级升级+共享 `runRaceExemptChild`）；②分类器按**族签名配对**豁免（steerFamily/sessionFamily 成对帧命中才免；未知形态 accessor 含 tagent 帧仍硬否决；FAIL 块仅许 race verdict 行，真实断言失败绝不后藏）；③`TestTriRaceOnlyFrameworkClassifier` 五形态自测钉死分类器语义。fail-before 旁证：分类器旧判据（裸 accessor 扫描）把带 wrapper 调用链的族块否决→tri 精确红——签名配对修正后绿。
**验证**：root `-race` 4/4 稳定 ok（此前 1/3–2/3 FAIL）；`./... -short` 全绿；wechat-bot short ok；TEMP 零残留。
**后续行动登记**：上游缺陷报告 trpc-agent-go（steer Queue 入 state 的生命周期竞态）+ 框架升级评估归独立变更，修复前本族签名按 §9.5 发布证据逐出现核验。

## §9.1 基准包装器真实契约（2026-09-21）

任务：基准包装器改为真实含 Sync 的底层契约，透传枚举/计数/错误，删除 no-op 能力回退；编译断言和错误 spy 覆盖。

**落地**（tests/offline_bench/offline_bench_test.go）：
- 新增 `durableKV` 接口（KVStore+Sync+ListPartitionIDs）= 包装器**要求**的底层真实契约；`newCountingKV` 构造期断言失败即 **panic 明示**（旧 `if ok … return nil` no-op 分支与 `return nil` 枚举兜底**物理删除**——能力缺失从"静默测 flush 延迟却上报 durable commit"变为接线期响亮失败）。
- `Sync()`/`ListPartitionIDs()` 直穿缓存的 `durable` 字段（计数/错误透传语义保持）；编译锁升级为 `var _ durableKV = (*countingKV)(nil)`（全契约，非两条款可选）。
- spy 矩阵：既有 `TestCountingKVSyncErrorPropagates`（错误透传+失败仍计数）保持；新增 `plainKV`（刻意缺契约）+ `TestNewCountingKVRequiresDurableBackend`（构造拒绝钉死）。LocalFileKV 天然满足契约（Sync=atomic tmp+rename、L87/L130 源码核验），三处调用点全量迁移。
- fail-before：TEMP 把构造门禁改宽容（`if false && !ok`+nil durable 回退）→ 拒绝测精确红（无 panic 即 t.Fatal）→ 恢复绿；TEMP 零残留。

## §9.2 无 Close 独立进程读回（降级收敛，2026-09-21）

任务（降级口径）：localfile 已无 fsync/WAL 机——收敛为单事件无 Close 独立进程读回（flush-only 语义）验证 Sync 屏障原子落盘、新进程可读原文/索引；不宣称掉电耐久、不再有 fsync-on 模式认证。

**现状核验**：主体早已由 D7 时代 `TestBenchWrapperBarrierDurableWithoutClose` 覆盖（子进程 StoreEvent→记录 syncs delta→`os.Exit(0)` 无 Close；父进程全新无包装栈重开：QueryEvents 索引+GetEvent 原文+Metadata 逐字）。**但存在与降级裁决矛盾的残留**：fsync-on/flush-only 双 cell 跑的是**同一实现**（`WithFSync` accepted-and-ignored 源码坐实）= 两个名字认证同一份字节，假双轴覆盖。
**收敛**：删 fsync-on cell（单 cell `sync-barrier-atomic-rename`）、child 去 `kv.WithFSync` 传参、删 WAL 时代误导注释（"f.Sync() on the WAL"）与孤儿 `b2i`；doc 注释改写为诚实声明（认证的是**原子 rename 跨进程可见 + 无 Close 脏退出**，掉电耐久明确不宣称、推迟 rustviking 阶段——与 L31 后端自身注释对齐）。
**fail-before**：TEMP 让 child 绕过包装直连 inner → 父进程 `syncs=0` 甄别精确红（"benchmark path diverges from production"）→ 恢复绿；TEMP 零残留。
**验证（两任务合并）**：offline_bench 全包 short ok；vet/gofmt 净；未提交（待用户指令）。

## §9.3 报告分报与诚实口径（2026-09-21）

任务：分报 Get/Query，增加 actual written/sample/Sync 次数/commit+dirty 摘要/实际环境配置，Go 内存统计不冒称 OS RSS。

**落地**（tests/offline_bench/offline_bench_test.go）：
1. **Get/Query 分报**：点读 `GetEvent` 与窗查 `QueryEvents` 延迟池物理分离（此前混入同一分布，混合 p95 对两种读形态都不诚实），各出 `get_point_read`/`query_window`（probes/latency/吞吐[线程内实测窗口]/kv_ops），并新增 `sync_barriers` 差值——实证**读路径零重刷**（探针各并发下 sync=0，写路径 =1000/1000 写 恰一屏障）。
2. **actual written/sample**：`written`（实写）+`requested_scale_full`+`sampled`——100k cell 截 20k 明确标样（§9.5 的 20k 采样标注来源）；fsync 轴整体删除（`"fsync"` 报告字段 0 命中——与 §9.2 假双轴同源收口，文件头注释同步改）。
3. **Sync 次数**：`snapshot()` 6→7 元组纳入 syncs；写阶段 `commit_summary.sync_barriers(_per_write)`。
4. **commit+dirty 摘要**：kv 六操作真实计数 + 盘上 segment 数 + `dirty_tmp_orphans`（全 0 实证）+ kv.json 字节数 + 可发现分区数。
5. **实际环境配置**：报告头 `env`（go_version/goos/goarch/num_cpu/host/测试二进制名 + 生效的 scales/cap/concurrency 参数）——数字永远带着它的机器走。
6. **不冒称 OS RSS**：`rss()`→`goMem()`，字段 `go_mem_runtime`，env 内 `memory_stats_kind` 明示"Go runtime MemStats only — deliberately NOT OS RSS"；compressionSweep 的 `rss` 字段同步改名。

**验证**：`RUN_OFFLINE_BENCH=1` 全矩阵实跑 **PASS（614s）**，报告逐字段 grep 核验（上文括号为实测值）；`-short` 包门 ok；vet/gofmt 净；TEMP 零残留；未提交。口径类任务以实跑输出为验收证据（报告结构即断言）。

## §9.4 组合功能门（降级口径，2026-09-21）

任务（降级后）：阶段 8 通过后后台运行并等待完整矩阵，新报告路径；任何 cell 失败不算完成。×fsync on/off 生产耐久矩阵已随 localfile 最小化裁决移出本阶段（推迟至专用存储引擎接线后）。

**后台矩阵等待**：`RUN_OFFLINE_BENCH=1 BENCH_REPORT=report-2026-09-21.json go test ./tests/offline_bench/ -run TestOfflineBenchmark -timeout 60m -v` → **PASS 619.5s**，报告 14KB 落盘 [report-2026-09-21.json](tests/offline_bench/report-2026-09-21.json)。cell 核验（JSON 解析逐格）：

| scale | written | sampled | sync_barriers | per_write | tmp 孤儿 |
|---|---|---|---|---|---|
| 1k | 1000 | false | 1000 | **1.0** | 0 |
| 10k | 10000 | false | 10000 | **1.0** | 0 |
| 100k | 20000 | **true（20k 标样）** | 20000 | **1.0** | 0 |

env 头（go1.24.1/scales/cap 实录）+ compression 3 格 + token_estimator（code/en/json/reference）+ `"fsync"` 字段 0 命中。

**首次运行失败如实记录（非 cell 失败，未以重跑掩盖）**：BENCH_REPORT 传仓库相对路径，而 `go test` 包工作目录=包目录 → `open ...: no such file or directory` 唯报告写失败（矩阵 cell 全数通过、报告完整打印于日志）；改包内相对路径重跑 PASS。属确定性配置错误修正。

**功能正确性组合门（本轮实跑）**：定向 race — agent 75.4s / reliability 15.8s / memory / event / tool/action 36.2s / root 9.2s 全 ok；`tests -race` = 已登记先存框架族（`TestInjectBusInputs_DuringReAct`，§8.8 豁免三元组内，本轮复现口径一致）；root 全包 short 零 FAIL；wechat-bot 独立模块 build/vet/short OK。
**验证**：TEMP 零残留；未提交；矩阵与门全绿后方勾选。

## §9.5 旧 JSON 保留与 REPORT.md 撤销/新基线（2026-09-21）

任务：保留旧 JSON，更新既有 REPORT.md 撤销无屏障耐久解释，标明 20k 采样；用新同语义基线建立回归比较，不虚构压缩/tokenizer 重测。

- **旧 JSON 保留**：`report-2026-09-18.json` 一字未动（历史证据）。
- **[REPORT.md](tests/offline_bench/REPORT.md) 撤销段**：09-18 的「fsync 摊付/WAL 驻留/耐久开销单列」及一切掉电耐久暗示**明文撤销**（机制已随最小化裁决移除，`WithFSync` accepted-and-ignored），并声明旧数值"是其当时实现的真实测量、不得再被读作当前后端能力"。
- **新同语义基线（09-21 实测）**：单屏障档全表 + `sync_barriers_per_write=1.0` + tmp 孤儿 0 + **20k 采样逐处标注** + Go heap ≠ OS RSS 标注 + **Get/Query 分报**（揭示旧混合"探测 5.5ms"实为窗查独担、点读微秒级——分报价值直接可见）。
- **诚实执行 D7 退化门**：抓到写路径 10²–10³× 退化（p50 2.6–23ms vs 旧 0.002–0.014ms），**归因机制变更非代码回归**——快照全量重写 O(数据集)/次 vs 已删除的 WAL 增量 append；登记为首个跨机制比较例外，本阶段不回添 WAL（将复活被撤销机制），性能上限交 rustviking 阶段。读侧确认无退化。
- **不虚构重测**：压缩（2/22/230ms vs 基线 2/21/213ms）与 token 误差（四语料逐值吻合）明确标注为"同实现/同 fixture 复跑吻合、非新宣称"；curateCards 修复保持由此实证。
- 回归比较口径三条入档（同机制才可比 / >20% 须解释重新批准 / 口径字段必备），复现命令补 `go test` 包目录陷阱注记（§9.4 首跑教训）。

## §9.6 文档收口与严格校验（2026-09-21）

任务：更新现有架构/操作文档和本变更 evidence.md（逐场景列实现/测试/命令/结果），删除被替代旧分支及误导注释；运行严格 OpenSpec 校验，在副本核对六项 delta 与重复旧角色条款的替代结果，不提前同步主规格。

- **design.md 补记决策14**（挂账清偿）：跨代键地板（RaiseSnowflakeFloor 单向+同秒进位、seed 挂冷启动必经扫描零额外 I/O）/确定性冲突归类（CONFLICT/FORGOTTEN=确定性 vs 瞬态 I/O——旧 bool 混谈即活锁成因）/协议隔离层（quarantine 与"损坏保证据"同构，隔离≠丢弃）+ 未采用方案（位布局改动触碰冻结键磁盘不变量；双活 writer 归 flock 前提；充分解归 rustviking）。
- **逐场景矩阵**：evidence.md §2–§9 各任务节即"实现/测试/命令/结果"四元组（含 fail-before 输出与验证计时），本节不重复；docs/wiki 三份架构文档本变更期内已随各段更新（agent-architecture/platform-subsystems/behavior-matrix），此轮为**误导残留清扫**。
- **误导残留清扫（三处）**：①`config.go` FSync 注释宣称"WAL append fsynced…survive power loss"——机制已物理移除，改写为 accepted-and-ignored + 耐久语义边界（只到屏障后跨进程可见）；②`platform-subsystems.md` LocalFileKV 行"fsync 默认开/WAL 追加/降级留痕"整段过期→最小化裁决事实；③`agent-behavior-matrix.md` "排空 ReliableBus"——drain 语义已被规格否定（drain-free），改为精确关闭序（进行中 turn 跑完/claim 保留/CloseDurable 清理已回执+盘点）。`docs/.dev/*` 为冻结历史思考记录（非承诺文档），不动；"drain-free"用词（正确当前术语）保留。
- **严格校验**：`openspec validate --strict` **valid**（决策14/REMOVED 增补后复跑仍 valid）。
- **副本核对（真库主规格零改动，"不提前同步"）**：`/tmp/opsx-copy` 沙盒按 Requirement 名集合机械对账 7 项 delta（任务说"六项"，实扫 7——async-task-lifetime 为 main=0 纯新增）：MODIFIED/REMOVED 目标全部存在于 main、ADDED 零同名碰撞；**残留扫描揪出两条真·旧角色条款未被 delta 承接**——main `event-segment-store`「WAL 中间坏行容错」「LocalFileKV 写路径 fsync 耐久」（机制已删、REPORT.md 已撤销解释，主规格却无移除动作）→ delta 补 `## REMOVED Requirements`（Reason+Evidence+替代归属指向能力准入/§5.7/隔离条款，不重复契约），复跑对账 **NO CLAUSE-LEVEL ISSUES**（merged 18：20−2）；`RebuildProjectionFromWAL` 命中为**假阳性**（现行实现接口名，投影重建条款是当前机制），不处理并如实记录。
- **验证**：`go build` ok、受影响面测绿、gofmt 净、TEMP 零残留；未提交。

---

## §9.7 交付：本地结果与待验清单（2026-09-21，本变更最后一任务）

### A. 本地已达成（分层结果清单）

| 层 | 结果 | 证据位置 |
|---|---|---|
| 协议核心（§2–§5） | inbox-v2 接收/准备/提交/两阶段完成/quarantine/§5.7 直接核对/§5.8 屏障全闭环；spill 旧机制物理删除 | evidence §2–§5 各节（实现+fail-before+race） |
| 生命周期（§6） | loopState 状态机、关闭序、末 owner、poisoned 密封、回收矩阵全门 | §6.1–6.6 |
| 呈现边界（§7） | 提示单次携带四通路、事实/回执/重启零污染 | §7.1–7.5 |
| 验收矩阵（§8） | 五端对账 e2e、宿主 seam、崩溃窗矩阵（接收 5 窗+完成 4 窗，独立子进程）、30 轮重启终门（逮住并三层修复跨代键活锁）、受管重置八腿、淘汰静态零遗留+三态启动 | §8.1–8.8 + §8.9 评审修复 |
| 产品缺陷修复 | 雪花键跨代冲突（RaiseSnowflakeFloor+冷启动 seed+确定性冲突隔离）= 本变更最高价值发现 | §8.5/§8.9，design.md 决策14 |
| 性能与证据口径（§9.1–9.5） | 包装器真实契约（no-op 回退删）、无 Close 跨进程读回（假双轴撤销）、Get/Query 分报+env+Sync 计数+非 RSS 标注、09-21 新基线报告（per_write=1.0/孤儿 0/20k 标样）、REPORT.md 撤销段+D7 退化归因（机制变更非回归） | §9.1–9.5 + REPORT.md |
| 文档与规格（§9.6） | 三处误导表述清扫、决策14 补记、strict valid、副本对账揪出并补齐 2 条未承接 REMOVED 条款 | §9.6 |

**版本状态**：`7677c07`（协议主体，CodeReview 无必须修项）+ `9c785ba`（评审修复轮）；**未提交 10 项** = §9.1–9.6 产物（config.go 注释/两份 wiki/openspec 四件/bench 四件）——提交与归档动作留待用户决定。
**最终确认快照（2026-09-21）**：双模块 build/vet OK；root/tests/offline_bench/agent/memory/event short 全 ok；wechat-bot ok；全包 race 近期 4/4 稳定。

### B. 待验清单（缺授权/环境不执行，本地完成 ≠ 发布通过）

| # | 待验项 | 本地未覆盖的原因 | 通过判据 |
|---|---|---|---|
| 1 | **72h 长跑** | 本机不具备连续 72h 受控运行条件；soak 骨架已子进程化（8ef6b01）可复用 | 长跑期间零未归类终态、restart/reconcile 计数恒等式成立、无泄漏增长趋势 |
| 2 | **真实模型/渠道** | 全部验收为 mock 模型+模拟投递（规格允许的最小验证面）；wechat-bot 仅到宿主决定层 | 真实渠道 202/回执↔模型请求↔用户可见消息三方对账；重试/限流/断连语义实网复验 |
| 3 | **真实掉电** | localfile 明示不宣称掉电耐久（§9.5 撤销段）；需物理机/PDU 注入 | 仅在 rustviking 等耐久后端上执行；已 ack 写入零丢失或有可处置的显式缺口分类 |
| 4 | **生产升级** | 受管重置仅在临时受管目录演练（§8.6 纪律：不操作真实目录） | 真实部署数据指纹→演练副本重置/启动对账→回滚预案演练通过后方可上生产 |
| 5 | **发布前独立审查** | `9c785ba` 之后（§9 全部 10 项未提交产物）未经独立 review | 人工审阅 §9 diff + 本 evidence 全节；race 豁免四签名逐出现复核 |

**登记不遮蔽**：先存框架 race 族（trpc-agent-go@v1.10.0，4 签名，§8.1/8.8/8.9 三元组）修复归框架升级独立变更；上游缺陷报告为待办行动。
**纪律收口**：本变更不自动 archive、不推送、不把本地全绿等同发布通过——准出 = 上表 1–5 按授权逐项闭环。

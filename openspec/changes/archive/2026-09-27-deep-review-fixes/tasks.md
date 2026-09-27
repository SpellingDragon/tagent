# 任务：深度评审修复（deep-review-fixes）

> **验收基准（唯一口径）**：回归测试 `memory/compaction_safety_test.go`（3 例）与 `agent/settle_accounting_barrier_test.go`（4 例含对照）全部转绿；`go test ./memory/ ./agent/ ./rl/ -count=1` 全绿。
> **执行纪律**：每个任务自带「改什么 / 禁止 / 判据」三段——实现必须落在「改什么」划定的最小 diff 内；「禁止」即红线，触碰即跑偏；「判据」是客观的完成检查。开始任一任务前先重读 design.md 对应决策节与红线章（anti-drift）。
>
> **修订记录（2026-09-27 第二版，reject 后重放）**：首轮实施被整体拒绝（与并行进行的 restrict-comments-to-godoc-and-index 清理相互干扰，非修复本身缺陷——复审确认 8 项技术判定全部维持）。本版吸收冷眼复审的 4 项修正：①任务 3.2 升级为**真 submitTransient 分支驱动**（原版只测函数行为，分支回退不会被抓住）；②任务 2.3 的谓词级范围**明示**（不宣称端到端）；③任务 4.x 预告 `TestReliableBus_FixedSlotsNotCompacted` 的覆盖面迁移；④收口新增根包全量验证（并行清理重组了根包测试，需共存验证）。归档动作改为**用户确认后执行**。

## 0. 全局红线（每个任务组开工前重读一次）

- [x] 0.1 确认已通读 design.md「实现红线（anti-drift）」章：不顺手重构、不扩展协议面、正常路径逐字节不变、**不得修改取证测试的断言方向来迁就实现**、每项修复独立可 cherry-pick
- [x] 0.2 基线快照：修复前跑 `go test ./memory/ ./agent/ ./rl/ -count=1` 记录基线（预期仅 5 例取证红），后续每完成一组对比——**新增红 = 跑偏信号**

## 1. P1 存储层 compaction fail-loud（数据丢失，最先修）

**改什么**：仅 `memory/compaction.go` mergeEvents 循环体与 `memory/lifecycle.go` checkTTL 一处日志。
**禁止**：不动 deleteSegments 的 `continue`（读失败不删 = 安全方向）；不给 compaction 新增重试/退避逻辑（scheduler 周期重试已存在）；不动 `lockPartition`/mutationMu 结构；不改 CompactL1ToL2/L2ToL3 的调用序。

- [x] 1.1 mergeEvents（memory/compaction.go ~L394-399）：`if err != nil { continue }` → `return nil, fmt.Errorf("merge scan failed pid=%d window=%d: %w", pid, windowTS, err)`。两处压缩（L1→L2/L2→L3）共用本函数，一次修复两路生效
- [x] 1.2 验证取证测试 `TestReview_MergeEventsSwallowsScanError` 转绿——判据：compaction 返回 error（非 nil）；KV 层直查源窗口 evt 槽仍在；冷 store 实例 GetEvent 取回窗口 0 事实
- [x] 1.3 checkTTL（memory/lifecycle.go ~L209-211）：`continue` 前补 `log.Warnf("[Lifecycle] TTL scan failed pid=%d window=%d: %v (skipped this round, retried next sweep)", ...)`。**控制流不变**（仍 continue）
- [x] 1.4 补集成回归 `TestMergeScanErrorAbortsAndRetries`：faultKV 一次失败 → CompactL1ToL2 返回错误且源段 evt/meta 原样 → 复跑 Compact（模拟 scheduler 重试、scan 已恢复）→ 迁移成功且冷读双窗口事实齐全
- [x] 1.5 本组判据：`go test ./memory/ -count=1` 全绿（含既有 TestCompactor_* 族不红）；diff 仅触及上述两个文件的指定位置

## 2. P2-3 投递对账屏障无泄漏（子调用环活性）

**改什么**：仅 `agent/settle_routing.go` countingSpawner.Spawn 的 void 条件与注释。
**禁止**：不移动 `noteSpawn` 的前置位置（防 race 设计）；不改 settleSinkRegistry 的 bind/unbind/route/awaiting 任何方法；不改 task 包的 SpawnResult 结构。

- [x] 2.1 countingSpawner.Spawn：`if res.Settled {` → `if res.Settled || res.Deduped || res.Blocked != "" {`；方法文档注释同步为「三种本次调用不再拥有 settle 期望的形态（inline settle / dedup 命中 / gate 拒绝）都 void」
- [x] 2.2 验证取证测试：`TestReview_CountingSpawnerDedupLeaksBarrier`、`TestReview_CountingSpawnerBlockedLeaksBarrier` 转绿；对照 `TestReview_CountingSpawnerInlineSettleVoids` 保持绿（**对照红 = 修复方向错**）
- [x] 2.3 补端到端回归 `TestDedupKeepsInvocationLoopQuiescent`：子调用环内同 key 二次发起（dedup 命中）→ 最后一个真实 settle 投递 + 总线排空后环退出（断言 invOutputCh 在远小于 defaultSubAgentTimeout 的测试时限内关闭）。复用 d7_settle_accounting_test 的快进 settle 基建，**不得**真等 600s
- [x] 2.4 本组判据：`go test ./agent/ -run 'Settle|Counting|Quiesc' -count=1` 全绿；diff 仅 settle_routing.go + 新测试

## 3. P2-1 submitTransient 重排口径统一（durable claim 僵尸）

**改什么**：仅 `agent/event_loop.go` submitTransient 分支一行与注释。
**禁止**：不动 submitCancelled / submitConflict 分支（claims retained 是 fail-closed 设计语义，非缺陷）；不动 finishDurableBatch 三处 received 调用；不「顺手」统一其他变量命名。

- [x] 3.1 submitTransient（agent/event_loop.go ~L226）：`ta.releaseBatchClaims(events)` → `ta.releaseBatchClaims(received)`；分支注释补一句「received 口径与 finishDurableBatch 一致：让位 meditation 的 claim 同步回 pending，防僵尸」
- [x] 3.2 补回归 `TestTransientRequeueCoversYieldedMeditation`：混批（meditation 让位 + 用户输入 selected）× durable 提交注入持续瞬时失败熬尽退避 → 断言两个 envelope（用户 + meditation）均回 pending（inbox Outstanding 状态可查 / 下一轮 claimDurable 能领到全部）
- [x] 3.3 本组判据：新回归绿 + durable inbox 既有测试族（org_wal_restart_reentry / resident_durable_e2e 相关）不红

## 4. P2-2 undecodable slot 整封隔离（inbox fail-closed）

**改什么**：仅 `agent/event_bus.go` claimDurable 的 undecodable 分支（含 envelope 级 batch 截断）。
**禁止**：**不新增 completion disposition**（设计决策 2 已否决 slotCorrupted）；不动 receipted 补 Ack 分支与 releaseRetention 次序；不改 Inbox.QuarantineEnvelope 的既有签名/行为。

- [x] 4.1 claimDurable 重构 slot 循环：进入每个 envelope 的 Messages 循环前记录 `startIdx := len(batch)`；`decodeSourceEvent` 失败时：`b.inbox.QuarantineEnvelope(path, fmt.Sprintf("undecodable source_event slot %d: %v", m.Slot, derr))` → `batch = batch[:startIdx]`（放弃本 envelope 已收集的好 slot，尚未提交事实，安全）→ `break`（继续下一个 envelope）。删除「Keep the envelope claimed … re-claim retries」注释，替换为整封隔离语义
- [x] 4.2 补全坏分支回归 `TestUndecodableAllSlotsQuarantined`：envelope 两 slot 均坏 → claimDurable 后 envelope 在隔离区（不在 pending/claimed），重启 reopen 后不再被 claim
- [x] 4.3 补半坏分支回归 `TestUndecodableOneSlotQuarantinesWholeEnvelope`：一好一坏 → 好 slot 不进批、该 envelope 无 completion、无 Ack（Outstanding 仍含它且状态为隔离可见），**同批其他 envelope 的正常消费不受影响**
- [x] 4.4 本组判据：`go test ./agent/ -run 'Inbox|Claim|Undecodable|Quarant' -count=1` 全绿；正常路径零变化（receipted 补 Ack / 幂等重放不重执行的既有测试不红）

## 5. P3 批次（低风险收尾）

**改什么**：三个文件的定点小改 + 一个死状态删除。
**禁止**：P3-3 不得把降级做成「无条件重写 meta」（必须仅在 sealed 时降级，保留 StoreEvent 现有 Warnf 语义）；P3-1 不得改 turnEchoVerified 的「验证在清理前」次序；P3-2 只加日志不改返回值语义。

- [x] 5.1 P3-3 sealed 降级共用：从 StoreEvent（memory/segment_store.go ~L405-415）提取 `maybeDemoteSealedWindow(pid, windowTS)`（含现有 Warnf）；ReplayEvent 的 `seqCounter==0` 恢复块（~L503-509）之后调用同一 helper。验证 `TestReview_ReplayEventSealedWindowNoDemote` 转绿；补正面形态 `TestReplayEventDemotesSealedWindowBounds`（降级后同查询命中 + meta.Sealed==false）
- [x] 5.2 P3-1 turnEcho 折入 endTurn（agent/event_loop.go ~L314-318）：endTurn 闭包体内、`turnLease.Release()` 之后追加 `cm.turnEcho = nil`；保留 L453-458 的「verify-then-clear」次序（endTurn 兜底与显式清理双保险，不互斥）。submit 门三分支的显式清理**保留**（防御纵深，不删）
- [x] 5.3 P3-2 SwappableModel Iter 日志（rl/swappable_model.go ~L184-196）：委托分支与桥接分支的 err 各补一条 `log.Errorf("[SwappableModel] iter path failed: %v", err)`；**返回语义不变**（仍 return 空流——错误面统一是后续 model-stream-contract 的事，本变更只补观测）
- [x] 5.4 死状态清理：`agent/reliability/inbox.go` `pathsByRequestID` 字段及其写入点删除（先 grep 确认零读者；若有读者则本任务取消并在收口说明）
- [x] 5.5 本组判据：`go test ./rl/ ./memory/ ./agent/ -count=1` 全绿

## 6. 收口与归档

**禁止**：改命时不得改动断言语义（只改名与注释口吻）；不得跳过 check-openspec 门禁。

- [x] 6.1 取证测试改命（机械映射，断言原文不动）：
  - `TestReview_MergeEventsSwallowsScanError` → `TestMergeScanErrorMustAbortCompaction`（memory）
  - `TestReview_ReplayEventSealedWindowNoDemote` → `TestReplayEventSealedWindowDemotion`（memory）
  - `TestReview_CountingSpawnerDedupLeaksBarrier` → `TestCountingSpawnerVoidsBookingOnDedup`（agent）
  - `TestReview_CountingSpawnerBlockedLeaksBarrier` → `TestCountingSpawnerVoidsBookingOnBlock`（agent）
  - `TestReview_CountingSpawnerInlineSettleVoids` → `TestCountingSpawnerVoidsBookingOnInlineSettle`（agent）
  - 文件名 `zz_review_evidence_test.go` → `compaction_safety_test.go`（memory）/ `settle_accounting_barrier_test.go`（agent）；头部注释从「取证/FAIL-by-design」改写为永久回归口吻
- [x] 6.2 全量验证：`go build ./...` && `go vet ./...` && `go test ./memory/ ./agent/ ./rl/ -count=1` 全绿；与 0.2 基线对比确认零新增红
- [x] 6.3 wiki 同步：仅更新与「undecodable slot 留 claimed」「submitTransient 只重排 selected」相关的表述（先 grep 确认存在才改，grep docs/wiki/ 中 claimed/re-claim/重排 相关句）；**不得**借机重写文档其他部分
- [ ] 6.4 归档：`scripts/check-openspec.sh` 全绿 → `openspec archive deep-review-fixes` → 复跑门禁确认 `openspec/specs/` 全绿；归档后核对四个主 spec 的 ADDED requirements 已并入且 `## Purpose` 完整
- [x] 6.5 提交纪律复核：每个任务组一个 commit（1.x/2.x/3.x/4.x/5.x 各一 + 6.x 收口一），message 格式 `fix(<scope>): deep-review P<n>——<一句话>`（scope ∈ memory/agent/rl/docs）；无混入无关文件
- [x] 6.4 归档：openspec archive 因「spec 已在首轮归档时并入」幂等冲突中止——已核实 8 条 requirements 均在四个主 spec（grep 等价验证），手工移回 archive/（工具的另一半动作），门禁复核通过。首轮实施被 reject 仅回滚代码、未回滚 spec 同步，故二归档无需再并。

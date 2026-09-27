# 深度评审修复（2026-09-27 三轮 main/dev 全量评审的 8 项实证发现）

## Why

2026-09-27 对 tagent main/dev 分支进行了三轮深度 code review（主干架构走查 → 地毯式分区精读 → 实据取证），发现 1 项 P1、3 项 P2、3 项 P3 共 7 项缺陷与 1 项可观测性备注，全部经代码逐行复核，其中 3 项已取得**可运行复现实据**（失败测试见 `memory/zz_review_evidence_test.go`、`agent/zz_review_evidence_test.go`，当前红、修复后绿即回归资产）。缺陷集中在两类模式：durable 协议边角的「对称性修复未扩散完全」（口径/分支只修了一半）与存储层「fail-open 的错误吞没」（违背项目自身的 fail-loud 纪律）。P1 在 rustviking 生产后端下可静默丢失整窗事件且被 warm LRU 遮蔽，必须先于 dev→main 合并修复。

## What Changes

- **P1 compaction 读失败 fail-loud**：`memory/compaction.go mergeEvents` 对源窗口 `KVScan` 失败从 `continue`（吞错）改为 fail-loud 返回错误，中止本轮 compaction（源段保留，下轮重试）——消除「merge 0 事件却删除源段」的静默丢失（取证：`TestReview_MergeEventsSwallowsScanError` 冷读 `kv key not found`）。
- **P3-3 ReplayEvent sealed 窗口降级对齐**：`memory/segment_store.go ReplayEvent` 的窗口重入分支补上 StoreEvent 已有的 sealed 降级（提取共用），消除「重放事实落入 sealed 包络外 → 时间范围查询漏检」的不对称（取证：`TestReview_ReplayEventSealedWindowNoDemote` 查询空集）。
- **P2-1 transient 重排口径统一为 received**：`agent/event_loop.go` `submitTransient` 分支的 `releaseBatchClaims(events)` 改为 `releaseBatchClaims(received)`——让位 meditation 的 durable claim 不再永久滞留 claimed 态（与 `finishDurableBatch` 三处 received 口径对齐，补齐 §4.1 修复的另一半）。
- **P2-2 undecodable slot 整封隔离**：`agent/event_bus.go claimDurable` 对 `source_event` 解码失败从「留 claimed 等 retry」改为对整个 envelope `QuarantineEnvelope`（与叶子层 unreadable 处置对齐）——封死「全坏跨重启死循环 / 半坏随 Ack 销毁」两个 fail-open 分支。
- **P2-3 投递对账屏障无泄漏**：`agent/settle_routing.go countingSpawner.Spawn` 在 `res.Deduped || res.Blocked != ""` 时同样 `voidSpawn`——dedup（plan single-flight 设计内高频）与 spawn gate 拒绝不再泄漏 pending，子调用环恢复静默退出（不再被拖满 `defaultSubAgentTimeout` 600s）（取证：`TestReview_CountingSpawnerDedup/BlockedLeaksBarrier` 红 + inline 对照绿）。
- **P3-1 turnEcho 清理折入 endTurn**：`agent/event_loop.go` 把 `cm.turnEcho = nil` 折进 per-turn 清理闭包，与 turnLease 释放同享纪律（防未来退路复用残留）。
- **P3-2 SwappableModel Iter 路径错误可观测**：`rl/swappable_model.go GenerateContentIter` 委托与桥接两分支补错误日志，消除「传输错误退化为空流成功」的静默。
- **备注 checkTTL 可观测性**：`memory/lifecycle.go checkTTL` 对 `KVScan` 失败补告警日志（方向安全——跳过=推迟遗忘，仅缺观测）。

## Capabilities

### New Capabilities

（无——全部为既有能力的缺陷修复，不引入新行为面。）

### Modified Capabilities

- `event-segment-store`：compaction 源窗口读失败 SHALL 中止本轮而非吞错继续；TTL 扫描读失败 SHALL 可观测；`ReplayEvent` 窗口重入 SHALL 与 `StoreEvent` 同做 sealed 降级。
- `persistent-event-loop`：瞬时提交失败的 claim 重排 SHALL 覆盖完整冻结接收集（received）；不可解码 slot SHALL 整封隔离而非留滞 claimed；per-turn echo 凭据作用域 SHALL 随 turn 清理统一回收。
- `subagent-turn-execution`：投递对账屏障的 booking SHALL 与实际投递期望一一配对——dedup/gate 拒绝的 spawn 不得留下无人消耗的 pending（越窗尾静默退出条件不得被破坏）。
- `swappable-executor`：SwappableModel 的迭代（Iter）委托路径 SHALL 与 channel 路径错误面一致——内部错误至少可观测，不得静默退化为空流。

## Impact

- **代码**：`memory/compaction.go`、`memory/segment_store.go`、`memory/lifecycle.go`、`agent/event_loop.go`、`agent/event_bus.go`、`agent/settle_routing.go`、`rl/swappable_model.go`——均为小修改（合计 <30 行产品代码变更）。
- **回归资产**：三份取证测试文件（`memory/zz_review_evidence_test.go` 2 例、`agent/zz_review_evidence_test.go` 3 例含对照）随修复转绿后改命保留为永久回归（`TestReview_*` → 包内命名规范）。
- **兼容性**：无 API/配置/序列化变更；P1/P2-2 将原本静默的异常路径改为显式失败/隔离——行为变化仅在故障路径上，属 fail-closed 收紧（与既有哲学一致）。
- **依赖**：无新增依赖。
- **验收**：`go test ./memory/ ./agent/ ./rl/ -count=1` 全绿 + 取证测试转绿；dev→main 合并门槛解除（P1 + 3×P2 清零）。

# Design: X2 编排动态性探索

## 主读路径

`agent/event_loop.go`（loopSpec/统一壳/durable 门）、`agent/org/`（candidate_txn/fingerprint/retirement）、`tagent.go`（stageOrgGenerations/activateOwnerGenerations 热更编排）、`agent/task/`（TaskManager 状态机/重入/结算）、`agent/settle_routing.go`（绑定表）、`agent/event_bus.go`（队列与 drain 语义）。

## 核验假设详单

| # | 假设（来源） | 核验方法 |
|---|---|---|
| H1 | 统一壳差异仅 loopSpec 单字段（D4） | 读 event_loop.go:78 runAgentLoop 与 session.go Run 的 spec 构造 |
| H2 | reload/rollback 共用一次候选事务（D5） | 读 tagent.go stage/activate 与 candidate_txn.go |
| H3 | 换代对进行中回合 drain-free（D5） | 读 BeginTurnLease/租约继承路径 |
| H4 | 队列无优先级/抢占/背压策略（第一阶段未覆盖，本轮补白） | 读 event_bus.go 容量/drop/阻塞语义；grep priority/preempt |
| H5 | 任务级动态编排（追加依赖/重新委派/暂停恢复）现无承载面（E2 空白假设） | 普查 TaskManager API 面：有无 depend/reschedule/pause 原语 |
| H6 | 执行代际快照可复用为"编排变更的一致生效点"（E3 设想） | 核对 generation 指纹覆盖的字段面是否含工具集/模型/子 Agent 拓扑 |

## 现有验证命令候选

`go test ./agent/org/ -count=1`、`go test ./agent/task/ -count=1`（候选事务与任务状态机行为证据）。

## 风险与回退

若队列语义比文档丰富（如已有背压），H4 补白结论改写——补白结论同样是交付物。

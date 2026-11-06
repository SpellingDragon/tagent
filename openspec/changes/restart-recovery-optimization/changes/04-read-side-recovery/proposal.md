# 04-read-side-recovery 提案

## Why

r26 实测把锚 A 的红定位到了**开机重建内存态**，而不是恢复机制：同一代 boot 里票据装载 2.8ms、restore-chains 0.005ms，而 `supplyRetrieval` 占 16.8s（96%）。

## What Changes

开机第二个 pass 现在是四件「把历史重算一遍」的事，都会随历史长度变长：

1. **观察表**（`memory/store_query.go` `StoreQuerySource`）：`ObserveStore` 把全部历史事件重新 land 一遍，装成可查询、可回捞的表。
2. **`landedSeq`**（每分区已 land 到哪个 seq）：`seedLandedSeq` 全量扫一遍算出来。
3. **回放投影**（`memory/replay` `ReplayProjection`）：`seedReplay` 把全部历史折一遍。
4. **向量缝**（`memory/recall.go` `VectorSeam`）：把全部历史文本重新 Submit 一遍（仅在绑定了嵌入器时；本次 smoke 场景嵌入器为空，这条 pass 为 0）。

这四样在运行时本来就是随每条事实增量维护的（`assembly/facts.go` 落表、喂缝、推进 `landedSeq`），只是状态只在内存，进程退出即丢，于是每次开机从全量历史重放。开机耗时因此与历史长度成正比。

原「分区游标（startup-active-partition-bitmap）」的前提（Init 全前缀扫描是瓶颈）被实测否证：段扫描只占 boot 的百分之几。删除该预案，范围重划为本域。

## What Changes（改法）

1. **观察表 + `landedSeq` 持久化（4.1）**：完成点把观察表与 `landedSeq` 写进唯一一份快照；恢复时装载，并从 `landedSeq` 之后只 land 增量。
2. **回放投影 + 压缩产物持久化（4.2）**：同法。
3. **完成点扩面（4.3）**：`Run.CompleteRestartRecovery` 写出的唯一快照扩为「恢复状态 + 观察表 + 回放投影 + `landedSeq`」。
4. **向量缝不进快照（4.4，待裁定）**：向量随历史线性长；治法三选一，推荐服务开放后后台补齐 + 可观测进度。
5. **消费者锚（4.5）**：本地 120 重启 boot_ms 在 gen1/20/120 同量级。

## 边界与依赖

- 依赖：03（票据/快照纪律与失败退回内核）已放行。
- 接口面：`memory/store_query.go`、`memory/replay/`、`memory/recall.go`、`assembly/run.go`（`seedReplay`/`seedLandedSeq` 改为装载+增量）、`assembly/facts.go`（增量落面）、`assembly/host.go`/`assembly/run.go` 的完成点、`golden/runtime/durable_truth.json`（R3 登记新持久面）。
- 禁令：不新增事实族；不改事件契约；不动插件 f(x) 管线；「与历史无关」必须是实测数字，不写成口号。

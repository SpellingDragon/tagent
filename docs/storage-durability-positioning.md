# localfile 存储的耐久定位（待裁备忘）

> 记录一个**未裁的设计张力**：localfile 后端到底是"碰巧耐久的测试后端"还是"承担生产耐久承诺的后端"。本页只记事实与两边证据，不作表态；若日后裁出"迁移专用存储引擎"的优化专案，以本页为起点。

## 张力

| 一侧 | 另一侧 |
|---|---|
| 归档设计（`archive/2026-09-30-complete-resident-reliability-protocol/specs/resident-release-evidence/spec.md`）把 localfile 定为**最小临时后端**："仅验证本能力的提交屏障与重启读回，不提供生产级耐久/安全/可维护保障；生产耐久认证推迟至接线 rustviking 等专用存储引擎的后续阶段" | 现行主 spec（`openspec/specs/event-segment-store/spec.md`「LocalFileKV Sync 为原子快照屏障」）承诺：Sync 成功（整序列化 + tmp + 原子 rename）才是耐久屏障，快照按分区分片、只重写脏分区；不承诺 fsync，不宣称抗掉电 |

## 码面事实（2026-10-01 实测）

- `memory/segment_store.go` 的 `syncer.Sync()`（commit 屏障路径）是 OS 级目录/文件 sync；`memory/quiet_errors` 面与 kv 快照走 flush+rename，**无 fsync 旋钮**（曾存在的 `memory.fsync`/`WithFSync` 是恒 no-op 的死面，已删除）
- 跨进程验收（`tests/resident_e2e_test.go`、`tests/soak_test.go`）以 `MemoryConfig{Type: "localfile"}` 做掉电/重启验证——e2e 把它当耐久后端用
- 保留租约（`memory/retention_lease.go`）、回放（`ReplayEvent`）、WAL 容错均构建在 localfile 之上

## 为何现在不改

本页原裁定（2026-10-01"保持现状"）的前提已被推翻：其所称"行为仍在"的 fsync 机制从未存在（`WithFSync` 恒 no-op，`memory.fsync` 被接受但无效）。merge-review-remediation CU-1 已将主 spec 对齐到"原子快照屏障"口径（屏障行为仍在的部分=快照 tmp+rename，Sync 成功后新进程可读回），CU-10 删除 fsync 死旋钮。张力收敛为：屏障给的是**进程崩溃安全**，不是掉电安全；"最小临时后端"与"生产耐久承诺"的裁决仍以本页为起点。

## 若起优化专案需回答的问题

1. rustviking 接线后 localfile 是否退役为纯测试后端（spec 承诺随之迁移）？
2. 过渡期内生产部署用 localfile 是否需要文档明示"非生产级"边界？
3. `resident-release-evidence` 归档 delta 中"生产耐久矩阵"的具体条目（掉电/部分写/并发 writer）是否需要独立验收？

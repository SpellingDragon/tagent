# localfile 存储的耐久定位（待裁备忘）

> 记录一个**未裁的设计张力**：localfile 后端到底是"碰巧耐久的测试后端"还是"承担生产耐久承诺的后端"。本页只记事实与两边证据，不作表态；若日后裁出"迁移专用存储引擎"的优化专案，以本页为起点。

## 张力

| 一侧 | 另一侧 |
|---|---|
| 归档设计（`archive/2026-09-30-complete-resident-reliability-protocol/specs/resident-release-evidence/spec.md`）把 localfile 定为**最小临时后端**："仅验证本能力的提交屏障与重启读回，不提供生产级耐久/安全/可维护保障；生产耐久认证推迟至接线 rustviking 等专用存储引擎的后续阶段" | 现行主 spec（`openspec/specs/event-segment-store/spec.md`「LocalFileKV 写路径 fsync 耐久」）承诺：WAL 每批 Flush 后 fsync、snapshot 重命名与首次 WAL 创建对目录 Sync、fsync 可配置且默认开启、Sync 成功才是 KVPut 的耐久屏障 |

## 码面事实（2026-10-01 实测）

- `memory/segment_store.go` 三处 `syncer.Sync()`（commit 屏障路径上），fsync 开关默认开启
- 跨进程验收（`tests/resident_e2e_test.go`、`tests/soak_test.go`）以 `MemoryConfig{Type: "localfile"}` 做掉电/重启验证——e2e 把它当耐久后端用
- 保留租约（`memory/retention_lease.go`）、回放（`ReplayEvent`）、WAL 容错均构建在 localfile 之上

## 为何现在不改

撤掉主 spec 的 fsync 耐久承诺 = 删掉一份**行为仍在**的真契约（制造文档说谎）；反之把"最小临时后端"措辞入册 = 否认已实现的耐久面。两者只能选一个终态，属设计表态，超出文档对齐范畴（2026-10-01 裁定：保持现状，记录待裁）。

## 若起优化专案需回答的问题

1. rustviking 接线后 localfile 是否退役为纯测试后端（spec 承诺随之迁移）？
2. 过渡期内生产部署用 localfile 是否需要文档明示"非生产级"边界？
3. `resident-release-evidence` 归档 delta 中"生产耐久矩阵"的具体条目（掉电/部分写/并发 writer）是否需要独立验收？

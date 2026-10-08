# Design: X3 持久化与索引探索

## 主读路径

`plugin/memory_plugin.go` 与 `agent/context_manager.go`（两个写入生产者）、`memory/segment_store.go` + `memory/in_memory_store.go`（存储实现与键空间）、`memory/kv/localfile`（LocalFileKV 快照语义）、`memory/compaction.go` + `memory/lifecycle.go`（压实/遗忘）、`memory/relation_store`（因果索引）、`memory/engine/`（语义索引旁路）、`tool/action/` 转储路径（tool-output/）。

## 核验假设详单

| # | 假设（来源） | 核验方法 |
|---|---|---|
| H1 | 双生产者（MemoryPlugin/persistBusEvent）+ compaction 事件全走 StoreEvent 单写路径（D1） | grep StoreEvent 调用点全集，确认无旁路写 |
| H2 | LocalFileKV 锁内全量 marshal（D1） | 读 localfile 实现的 Set/Delete 路径与锁范围 |
| H3 | QueryEvents 无 Metadata 过滤 → latestCompactionKey 只能窗口扫描+点查（D1） | 读 QueryEvents 签名与过滤能力；核对 limit 5 假设的守护性 |
| H4 | tool-output/ 转储无 TTL/清扫/归档实现（D3） | grep workspace_root/tool-output 的清理路径；核对与 CleanupOrphanSessions 的关系 |
| H5 | TTL 扫描 O(事件数)/周期（D1） | 读 lifecycle checkTTL 的遍历结构 |
| H6 | 一次 turn 的写入次数可静态推演（E4 补白：写放大账） | 沿 turn 主链（事件入库×N + 因果边 + 投影旁路 + compaction 事件 + inbox 受理）数写入调用点，产出"每 turn 写入账表" |
| H7 | 索引现状：idx/evt/meta/tomb 四类键即全部查询面，无二级索引（E5 补白） | 盘点所有 Query* API 的访问模式与所需扫描复杂度 |
| H8 | 记忆 TTL 与训练留存无独立策略（E6 设想） | grep lifecycle 与 trajectory 的保留配置是否有交叉 |

## 现有验证命令候选

`go test ./memory/... -count=1 -short`（白盒契约）；`go test ./agent/compress/ -count=1`。

## 风险与回退

写放大账是静态推演（不写 benchmark），结论须标注"静态推演，未实测"置信级；若现有测试自带计时可引用。

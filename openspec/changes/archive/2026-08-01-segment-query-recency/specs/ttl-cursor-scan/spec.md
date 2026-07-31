# ttl-cursor-scan Delta

> 本能力从未实现：`{pid}:ttl_cursor` 键在全历史 `git log -S` 中零命中，`LifecycleManager` 无任何游标逻辑（`checkTTL()` 无分区参数、每周期全量扫描）。其规格随 `2026-06-20-harden-event-storage-for-scale` 归档进入主 specs，而该变更的 tasks 为 0/80 全未完成。本 change 撤回这些空头承诺；当前 TTL 扫描的真实行为契约改由 `event-lifecycle` 描述，有界成本扫描列为已知缺口。

## REMOVED Requirements

### Requirement: TTL scan uses per-partition time cursor

**移除理由**：`{pid}:ttl_cursor` 键与 `checkTTL(pid)` 分区级签名从未实现。当前 `checkTTL()` 每周期遍历全部分区与事件，无游标状态。保留该 SHALL 会使规格持续误导实现者与外部分析者。

### Requirement: TTL scan complexity bounded by expired window count

**移除理由**：该复杂度保证依赖上一条的游标机制，同样从未实现（当前为 O(分区内事件数)/周期）。有界成本扫描作为演进方向记录于 `docs/wiki/memory/memory-architecture.md` 的已知缺口章。

### Requirement: TTL cursor recovers gracefully from restart

**移除理由**：不存在需要恢复的游标状态（`LifecycleManager.Start()` 不加载任何持久进度）。

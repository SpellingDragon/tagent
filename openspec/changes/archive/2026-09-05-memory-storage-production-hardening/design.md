# Design: Memory Storage Production Hardening

## Context

本设计整合 4 个 change 的未完成工作，形成统一的生产硬化方案。核心问题：当前 FileSegmentStore 在生产规模（10 events/s × 3 年 = 1B+ 事件）下存在 24 个缺陷，导致数据丢失、性能崩溃、关系图 OOM。

## Decisions

### D1: RustViking KV 作为唯一存储后端

**决策**：所有 KV 操作通过 RustViking CLI，不再有客户端 filter hack。

**原因**：`longestCommonPrefix` hack 在百万级 key 时性能不可接受。RustViking 原生 RocksDB iterator 可在 ms 级完成 range scan。

**影响**：
- RustViking 需新增 `kv range` 子命令（R2）
- 修复 `kv batch` 原子性（R1）：改用 `WriteBatch::commit()`
- JSON 响应契约统一为 `{success, data | error, api_version: "v1"}`（R3）

### D2: Active Partitions Bitmap 替代全扫 meta

**决策**：用 `global:active_partitions` bitmap（roaring bitmap 编码）记录所有活跃分区，Init 时一次性 get + 并发读取各分区 cursor。

**原因**：旧方案 Init 扫描 `{pid}:meta:*` 前缀，在 1000+ 分区时需 10+ 分钟。bitmap 方案 O(1) get + O(N) 并发 cursor 读取，1000 分区 < 500ms。

**影响**：
- `memory/key_schema.go` 新增 bitmap 编解码 helpers
- `FileSegmentStore.Init()` 完全重写
- `StoreEvent` 写入时同步更新 bitmap

### D3: Per-Partition Tombstone 隔离

**决策**：每个分区独立 `TombstoneSet`，通过 `sync.Map` 懒初始化。新创建时自动 `RecoverFromKV()` 扫 `{pid}:tomb:` 前缀。

**原因**：全局 TombstoneSet 在多分区场景下成为瓶颈，且 tombstone key 无 pid 前缀导致跨分区污染。

### D4: TTL 前进游标

**决策**：每个分区维护 `ttl_cursor`（`{pid}:ttl_cursor`），记录下次扫描的起始 window。evictOldest 从 `ttl_cursor.next_scan_window - 1` 开始向前扫描，扫描后更新游标。

**原因**：旧方案每次 TTL 检查全量扫描所有 window，在 10000+ window 时耗时数秒。前进游标确保每个 window 只扫描一次。

### D5: RelationStore LRU + WAL + Range Scan

**决策**：重写 RelationStore 为：
- 内存：`childToParent` + `parentToChildren` 双 map + LRU 淘汰（默认 10000 条）
- 持久化：WAL journal（`{pid}:rel:{child}` + `{pid}:revrel:{parent}:{child}`）
- 查询：通过 RustViking `kv range` 实现 range scan

**原因**：旧方案全量内存（30GB+ for 1B events），无持久化，重启丢失。LRU + WAL 在保持 O(1) 热点查询的同时控制内存 < 100MB。

### D6: 双写迁移策略

**决策**：Phase 3 迁移采用双写模式：
1. MemoryPlugin 同时写入 FileBackend（旧）和 FileSegmentStore（新）
2. RecallAgent/KnowledgeAgent 从 FileSegmentStore 读取，FileBackend 只读保留
3. 迁移工具 `cmd/migrate-events/` 批量转换旧数据
4. 迁移完成后删除 FileBackend

**原因**：零停机迁移，双写期间数据一致，回退安全。

### D7: LLM 摘要降级策略

**决策**：LLMSummarizer 失败时降级为 PassthroughSummarizer（content 截断）。连续 N 次超时后进入降级模式 M 分钟。

**原因**：L2→L3 压缩是后台 goroutine，不能因 LLM 不可用阻塞 compaction pipeline。降级确保存储系统始终可用，仅摘要质量下降。

### D8: Phase 依赖顺序

```
Phase 1 (RustViking KV) → Phase 2 (FileSegmentStore 硬化) → Phase 3 (迁移)
                                                          → Phase 4 (LLM 摘要)
                                                          → Phase 5 (文档)
Phase 6 (验证) ← 所有 Phase 完成
```

Phase 3 依赖 Phase 2（硬化后的 FileSegmentStore 才能承接生产流量）。
Phase 4 依赖 Phase 2（LLM 摘要 hook 在 Compaction 中，Compaction 硬化后才能接入）。
Phase 5 独立，可并行。

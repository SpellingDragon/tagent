## Why

tagent 事件存储分层架构的 Phase 1-8 核心代码已落地（FileSegmentStore、RelationStore、Compaction、Lifecycle），但经过**四轮深度评审**（v1 逐行审查 + v2 架构交叉审查 + v3 RustViking API 反向追踪 + v4 规模化假设复核），共暴露 **24 个工作项**：

- **17 个缺陷修复**：14 个原识别 + 3 个深度审查新发现（D1 StoreEvents 多窗口 seq 碰撞、D4 repairDangling 跨批次 alive 集、D16 resolvePartitions 默认返回 nil）
- **7 个规模化能力补齐**：v1 假设"百万级事件"不成立；真实规模 **10 events/s × 3 年 = 1B+ 事件**，原 RelationStore 全量内存（30GB+）、Init 全扫 meta（10 分钟）、TTL 全量扫描等设计全部崩溃

当前代码 + v1 设计方案**在生产规模下不可用**。必须在合并生产前一次性修复到位——项目尚未发布，不允许发布即腐坏。

## What Changes

### Part A — 17 项缺陷修复

#### 数据正确性（阻断级）
- **修复 `FileSegmentStore.Init()`**：从 `global:active_partitions` bitmap + `{pid}:cursor` 恢复 seqCounter / currentWindow（避免重启覆盖旧数据）
- **修复 `StoreEvents` 批量写入**：
  - 按 `(pid, windowTS)` 分组独立维护 seq 计数，避免 Go map 随机迭代导致跨 window seq 碰撞（D1）
  - 补充 segment meta key 写入，确保 Init 可发现所有 window
- **修复 `TombstoneSet` 分区隔离**：每个 partition 独立 TombstoneSet，懒初始化时扫 `{pid}:tomb:` 恢复墓碑集合
- **修复 RustViking `kv batch` 原子性**：CLI 层改用 `WriteBatch::commit()`
- **修复 `LifecycleManager` 扫描器**：从事件 JSON 提取 `EventKey` 和 `timestamp`，修正 `ParseKey().EventKey=0` 和 TTL age 用 window ts 的 bug
- **修复 `Compaction.filterTombstoned()`**：从空桩改为实际调用 `TombstoneSet.IsTombstone()`
- **修复 `repairDanglingRefs` alive 集**（D4）：alive 判定源 = 本批次活事件 ∪ 已在 L2/L3 的历史事件（通过 `tombstoneSet.IsTombstone()` 正确识别）
- **修复 `resolvePartitions` 默认全量**（D16）：无分区过滤时读 `global:active_partitions` bitmap 返回所有活跃分区（避免静默空结果）

#### 功能补全
- **新增 RustViking `kv range` 子命令**：由 RustViking v0.2.0 提供，tagent 端调用替代 `longestCommonPrefix` hack
- **tagent `KVRange()` 改用 CLI**：移除客户端 filter 逻辑
- **Compaction 索引清理**：`deleteSegments()` 同步删除被墓碑过滤事件的 `{pid}:idx:` 和 `{pid}:tomb:` KV
- **Compaction 调度器**：`checkL1ToL2()` / `checkL2ToL3()` 实际触发，不再仅 seal
- **`eventCount` 生命周期**：delete/compaction 后递减，容量逐出正确工作

#### 接口与生命周期（BREAKING）
- **`MemoryStore` 接口移除 `GetParent`/`GetChildren`**：改走 `RelationStoreProvider.RelationStore()`
- **`RelationStore.GetChildren` 签名变更**：`GetChildren(parent int64, limit int) ([]int64, bool, error)`（bool=hasMore）
- **`FileSegmentStore.Close()`**：停 LifecycleManager → 停 Compactor → flush tombstoneSet → 关闭 RelationStore
- **生产接线补全（`tagent.go`）**：`resolveMemoryStore("file")` 中创建 TombstoneSet 懒加载器、LifecycleManager、Compactor，调用 `Init()`

### Part B — 7 项规模化能力补齐

- **RelationStore v2（LRU + RocksDB，无 journal）**：
  - 砍掉独立 journal / snapshot / ReplayJournal
  - LRU 热图（默认 1M entries）+ RocksDB 冷图（`{pid}:rel:` / `{pid}:revrel:`）
  - SetParent 同步写 RocksDB，RocksDB WAL 即是 journal
- **Startup active-partition bitmap**：`global:active_partitions` 2048 bit 位图 + `{pid}:cursor` 点查，Init 从 O(N) → O(active_partitions)
- **TTL 游标扫描**：`{pid}:ttl_cursor` 记录下一待检查 window，TTL 扫描从 O(N) → O(过期数)
- **段大小自适应**：`max_events_per_segment`（默认 10K）触发提前 seal；L2 daily 按事件数分片
- **L3 归档摘要化**：`partition_defaults.archive_summary_types` 配置驱动，按事件类型决定保留完整 / 仅 summary / partial
- **Snowflake 同秒溢出阻塞**：同毫秒 sequence 耗尽（4096）时 sleep 到下一毫秒，突发自动限流

## Non-Goals（不在本 change 范围）

- **LLM 驱动的 EventSummary 生成**：Compaction 摘要化当前使用 pass-through（保留 `EventSummary` 字段），LLM 生成由独立 future change `llm-event-summary` 交付
- **RustViking server / gRPC / HTTP 模式**：tagent 锁定本地 CLI 集成（`exec.Cmd`）；不在 client 内预留 `mode` 配置或 `ErrServerNotImplemented` 分支，若后续确需水平扩展单起 future change
- **向量索引 / 语义搜索**：Phase 2+ 独立 change
- **双写迁移 / 兼容旧 FileBackend**：项目未发布，直接覆盖，不做双写
- **Column Family 分层 + Compaction Filter**：可选演进，不强依赖

## Capabilities

### New Capabilities

- `rustviking-range-scan`：RustViking CLI `kv range` 子命令
- `startup-active-partition-bitmap`：`global:active_partitions` + `{pid}:cursor` 游标替代全扫描
- `tombstone-partition-isolation`：TombstoneSet 多分区隔离 + 懒加载恢复
- `production-wiring`：tagent.go 生产接线（Init + Lifecycle + Compactor）
- `compaction-scheduler`：L1→L2 / L2→L3 自动触发调度
- `relation-store-lru-rocks`：LRU 热图 + RocksDB 冷图（替代 journal/snapshot）
- `ttl-cursor-scan`：`{pid}:ttl_cursor` 时间游标扫描
- `adaptive-segment-size`：`max_events_per_segment` 触发提前 seal
- `l3-archive-summarization`：按 `archive_summary_types` 摘要化
- `snowflake-overflow-handling`：同毫秒 4096 阻塞到下一毫秒
- `relation-store-provider`：`GetParent`/`GetChildren` 从 `MemoryStore` 移至 `RelationStoreProvider`，`GetChildren` 带 limit 分页

### Modified Capabilities

- `event-segment-store`：Init 优化、StoreEvents 多窗口 seq 隔离、StoreEvents 写 meta、eventCount 递减、Close 方法、resolvePartitions 默认全量、`max_events_per_segment` 提前 seal
- `event-lifecycle`：checkTTL/evictOldest 修复 EventKey 与 age、TTL 游标扫描
- `event-compaction`：filterTombstoned 接入、repairDangling 跨批次 alive 集、deleteSegments 清理 idx/tomb KV、L3 摘要化
- `rustviking-client`：KVRange 改用 CLI `kv range`、batch 原子契约（锁定 CLI 模式）

## Impact

### 修改文件（Go 侧）

- `tagent.go`：生产接线（Init + LifecycleManager + Compactor 启动）
- `memory/segment_store.go`：Init 优化、StoreEvents 多窗口 seq、StoreEvents 写 meta、eventCount 递减、Close、resolvePartitions 默认全量、adaptive segment size、移除 GetParent/GetChildren
- `memory/lifecycle.go`：checkTTL/evictOldest 修复、TTL 游标扫描
- `memory/compaction.go`：filterTombstoned、repairDangling、索引清理、调度器、L3 摘要化
- `memory/tombstone.go`：多分区 + 懒加载 + 持久化恢复
- `memory/relation_store.go`：**重写** — LRU + RocksDB 冷图（删除 journal/snapshot 相关方法）
- `memory/rustviking_client.go`：KVRange 改 CLI
- `memory/types.go`：`MemoryStore` 接口移除 `GetParent`/`GetChildren`；`RelationStore.GetChildren` 签名变更
- `memory/in_memory_store.go`：适配接口变更
- `memory/snowflake.go`：溢出阻塞
- `memory/key_schema.go`：新增 rel/revrel/cursor/ttl_cursor/active_partitions key 格式

### 新增/修改 RustViking 侧

- `src/cli/store_commands.rs`：`exec_kv_range` 新增；`exec_kv_batch` 改用 WriteBatch
- `src/cli/commands.rs`：`KvOperation::Range { start, end, limit }` 新增
- `src/main.rs`：dispatch 加 Range 分支

**注**：RustViking 侧修改由 RustViking 团队承担，tagent 侧依赖 RustViking v0.2.0 发布。需求单已提交至 `rustviking/docs/tagent-integration-requirements.md`。

### BREAKING 变更

1. `MemoryStore` 接口：移除 `GetParent(key int64) (int64, bool)` 和 `GetChildren(key int64) []int64`
2. `RelationStore` 接口：移除 `Snapshot()` / `LoadSnapshot()` / `ReplayJournal()`
3. `RelationStore.GetChildren` 签名：`(parentKey int64, limit int) ([]int64, bool, error)`
4. 迁移：所有调用方改用 `store.(RelationStoreProvider).RelationStore().GetParent/GetChildren(...)`

### 风险

- **RustViking v0.2.0 交付延期**：阻塞 tagent 实施。缓解：需求单已提交，契约冻结；tagent 可先完成不依赖 RustViking 的 Go 侧工作，RustViking 就绪后联调
- **RelationStore LRU miss 率高影响性能**：缓解：Init 预热最近 N 天 + 监控 cache hit + 可调 lru_size
- **规模化数据迁移**：无需迁移（项目未发布）
- **接口 BREAKING 影响 plugin/tool 调用方**：需同步修改 `plugin/memory_plugin.go`、`tool/recall/*.go`、`agent/tool_agent.go` 等

## Dependencies

- **上游**：RustViking v0.2.0（R1 + R2 + R3 + R4）
- **下游**（future changes）：
  - `llm-event-summary`：LLM 驱动的 EventSummary 生成
  - `event-vector-search`：HNSW 向量索引接入（Phase 2+）

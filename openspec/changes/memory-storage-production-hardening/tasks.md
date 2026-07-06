# Consolidated Tasks: Memory Storage Production Hardening

> 整合自 4 个已归档 change 的未完成任务。
> 依赖顺序: Phase 1 → Phase 2 → Phase 3 (迁移) / Phase 4 (LLM 摘要) / Phase 5 (文档) → Phase 6 (验证)

## Phase 1: RustViking KV 改进

- [ ] 1.1 RustViking CLI 新增 `kv range` 子命令（R2）：`KvOperation::Range { start, end, limit }`，基于 RocksDB `iterator(IteratorMode::From(start, Forward))`，遇到 key ≥ end 或 limit 停止；返回 `{success, data: {start, end, count, entries: [{key, value}]}, api_version: "v1"}`
- [ ] 1.2 RustViking CLI 修复 `kv batch` 原子性（R1）：`exec_kv_batch` 改用 `store.batch()?` 获取 `BatchWriter`，所有 put/delete 加入 WriteBatch 后 `commit()`
- [ ] 1.3 RustViking JSON 响应契约稳定（R3）：所有子命令输出固定 envelope `{success, data | error, api_version: "v1"}`
- [ ] 1.4 RustViking 基准测试（R4）：put P99 < 3ms、batch 100 ops P99 < 10ms、range scan 1000 keys P99 < 15ms 达标
- [ ] 1.5 RustViking 发布 v0.2.0 并更新 CHANGELOG

## Phase 2: Key Schema + FileSegmentStore 硬化

### 2.1 Key Schema v2

- [ ] 2.1.1 修改 `memory/key_schema.go` 增加 key format：`global:active_partitions` (256 byte bitmap)、`{pid}:cursor` (JSON)、`{pid}:ttl_cursor` (JSON)、`{pid}:rel:{child}`、`{pid}:revrel:{parent}:{child}`
- [ ] 2.1.2 新增 helper：`BuildActivePartitionsKey()` / `BuildCursorKey(pid)` / `BuildTTLCursorKey(pid)` / `BuildRelKey(pid, child)` / `BuildRevRelKey(pid, parent, child)`
- [ ] 2.1.3 新增 `ParseActivePartitionsBitmap(b []byte) []int` / `EncodeActivePartitionsBitmap(pids []int) []byte`
- [ ] 2.1.4 单元测试：bitmap 编解码往返、cursor JSON 序列化稳定、key format 字符串匹配

### 2.2 FileSegmentStore Init v2（bitmap + cursor）

- [ ] 2.2.1 新增 `loadActivePartitions(kv KVStore) ([]int, error)`：读 `global:active_partitions`，缺失返回空 slice
- [ ] 2.2.2 重写 `FileSegmentStore.Init()`：bitmap + errgroup 并发读 cursor（`runtime.NumCPU() * 4`），不再扫前缀
- [ ] 2.2.3 Init 失败容忍：单个 pid cursor 读失败记 warning + 零值继续
- [ ] 2.2.4 单元测试：1000 分区 < 500ms、零活跃无 side effect、cursor 缺失零值 + warning

### 2.3 StoreEvent / StoreEvents v2

- [ ] 2.3.1 `StoreEvent` 成功后同 WriteBatch 追加 cursor 更新 + bitmap 置位
- [ ] 2.3.2 重写 `StoreEvents` 分组：`sort.Slice` → `(pid, windowTS)` 分组 → 独立 seq → cursor + bitmap 更新
- [ ] 2.3.3 `ensureSegmentMeta`：`map[int64]bool` 去重，每新 window 一次 meta put
- [ ] 2.3.4 自适应 segment size：`currentSegmentEventCount >= MaxEventsPerSegment` 触发 seal + rotate
- [ ] 2.3.5 `eventCount` 加锁保护并发递增
- [ ] 2.3.6 `resolvePartitions(filter)` 空 PartitionIDs → `loadActivePartitions` 全量
- [ ] 2.3.7 单元测试：跨 window seq 不碰撞、连续 seq、提前 seal、cursor 原子、bitmap 置位、全量分区

### 2.4 TombstoneSet 分区隔离

- [ ] 2.4.1 `tombstones` 改为 `sync.Map`（key=pid, value=`*TombstoneSet`）
- [ ] 2.4.2 `getTombstoneSet(pid)` 懒创建 + `LoadOrStore` + 自动 `RecoverFromKV()`
- [ ] 2.4.3 所有调用点改用 `getTombstoneSet(pid)`
- [ ] 2.4.4 单元测试：分区隔离、懒初始化 recover、重启恢复

### 2.5 Lifecycle + TTL 游标

- [ ] 2.5.1 `checkTTL(pid)` 改造：读 `ttl_cursor` → range scan `{pid}:meta:*` → 提取 event_key/timestamp → MarkTombstone → 更新 cursor
- [ ] 2.5.2 `evictOldest(pid)` 改造：从 `ttl_cursor.next_scan_window - 1` 向前扫描
- [ ] 2.5.3 `LoadTTLCursor` / `SaveTTLCursor` helper
- [ ] 2.5.4 单元测试：游标仅前进、跳过未过期 window、重启续扫、JSON 解析稳定

### 2.6 RelationStore v2（LRU + KV）

- [ ] 2.6.1 重写 `memory/relation_store.go`：`hot *lru.Cache`（1M entries）+ `kv KVStore` + `pid`，移除 journal/snapshot
- [ ] 2.6.2 `SetParent(child, parent)`：同 WriteBatch 写 rel + revrel，commit 后更新 LRU
- [ ] 2.6.3 `GetParent(child)`：LRU hit → miss 读 KV → 回填 LRU
- [ ] 2.6.4 `GetChildren(parent, limit)`：range scan revrel 前缀，limit+1 判 hasMore
- [ ] 2.6.5 `DeleteRelation(child)`：读原 parent → 删 rel + revrel → evict LRU
- [ ] 2.6.6 `Close()`：flush hook，write-through 保证 LRU 无未持久化状态
- [ ] 2.6.7 接口调整：移除 `Snapshot`/`LoadSnapshot`/`ReplayJournal`；`GetChildren` 签名加 limit
- [ ] 2.6.8 单元测试：LRU hit/miss/evict、GetChildren 分页、DeleteRelation、1M 容量、崩溃恢复

### 2.7 Snowflake 溢出阻塞

- [ ] 2.7.1 同毫秒 seq 回绕 → `time.Sleep` 到下一毫秒
- [ ] 2.7.2 时钟回拨 → `ErrClockBackwards`
- [ ] 2.7.3 单元测试：5000 ID/ms 第 4097 阻塞、时钟回拨 error

### 2.8 Compaction 修复

- [ ] 2.8.1 `filterTombstoned` 接入 `getTombstoneSet(pid).IsTombstone`
- [ ] 2.8.2 `repairDanglingRefs` alive 集 = 本批次 ∪ `!IsTombstone(parentKey)`
- [ ] 2.8.3 `deleteSegments` 同步清理 `{pid}:idx:` + `{pid}:tomb:` KV
- [ ] 2.8.4 `eventCount` 递减：按 `SegmentMeta.EventCount` 扣减
- [ ] 2.8.5 `checkL1ToL2()`：遍历活跃分区，L1 满 24 段触发
- [ ] 2.8.6 `checkL2ToL3()`：L2 满 7 段触发
- [ ] 2.8.7 `checkAndCompact()`：串行 seal → L1→L2 → L2→L3
- [ ] 2.8.8 单元测试：跨批次 alive 集、tomb 清理、L1→L2/L2→L3 触发、eventCount 递减

### 2.9 L3 摘要化 Hook（PassthroughSummarizer）

- [ ] 2.9.1 `memory/summarizer.go`：`SummaryGenerator` 接口 + `PassthroughSummarizer` 默认实现
- [ ] 2.9.2 `FullEvent` 新增 `EventSummary string` 字段
- [ ] 2.9.3 `Compactor` 注入 `SummaryGenerator`；`CompactL2ToL3` 按 `archive_summary_types` 选策略
- [ ] 2.9.4 配置加载：`PartitionDefaults.archive_summary_types`，未配置默认 `full`
- [ ] 2.9.5 单元测试：full/summary/partial 策略 + 未知 type 默认 full

### 2.10 RelationStoreProvider 接口迁移（BREAKING）

- [ ] 2.10.1 `MemoryStore` 接口移除 `GetParent` / `GetChildren`
- [ ] 2.10.2 `RelationStoreProvider` 接口定义：`RelationStore() RelationStore`
- [ ] 2.10.3 `InMemoryStore` / `FileSegmentStore` 实现 `RelationStoreProvider`
- [ ] 2.10.4 全量调用方适配：`plugin/`、`tool/recall/`、`agent/` — `store.GetParent(k)` → `store.(RelationStoreProvider).RelationStore().GetParent(k)`
- [ ] 2.10.5 grep 验证迁移完整
- [ ] 2.10.6 `go build ./...` 通过

### 2.11 RustViking Client v2

- [ ] 2.11.1 `rustviking_client.go` 锁定 CLI 模式：仅 `binary_path` / `config_path`
- [ ] 2.11.2 `KVRange` 改用 `kv range` CLI，删除 `longestCommonPrefix`
- [ ] 2.11.3 JSON 解析统一走 `CLIEnvelope`
- [ ] 2.11.4 单元测试：CLI happy path、KVRange 跨分区、batch 失败路径

## Phase 3: FileBackend → FileSegmentStore 迁移

- [ ] 3.1 实现双写模式：MemoryPlugin 同时写入 FileBackend（旧）和 FileSegmentStore（新）
- [ ] 3.2 实现读取切换：RecallAgent / KnowledgeAgent 从 FileSegmentStore 读取，FileBackend 只读保留
- [ ] 3.3 实现历史迁移工具：`cmd/migrate-events/` 将旧 FileBackend 事件批量转为 RustViking KV
- [ ] 3.4 迁移完成后删除 `memory/file_backend.go` 和 `memory/file_backend_test.go`
- [ ] 3.5 移除双写逻辑，清理遗留代码
- [ ] 3.6 全量测试通过（确认无回归）

## Phase 4: LLM 事件摘要

### 4.1 LLMSummarizer 实现

- [ ] 4.1.1 `memory/summarizer.go` 新增 `LLMSummarizer struct`：`model`、`batchSize`、`timeout`、`fallback SummaryGenerator`
- [ ] 4.1.2 `Generate(event FullEvent) (string, error)` 单事件路径
- [ ] 4.1.3 `GenerateBatch(events []FullEvent) ([]string, error)` 批处理路径
- [ ] 4.1.4 `memory/summarizer_prompt.go`：事件类型 → prompt 模板映射

### 4.2 批处理接入

- [ ] 4.2.1 `memory/compaction.go` L2→L3：按 eventType 分桶 → `GenerateBatch` 每桶
- [ ] 4.2.2 批失败降级：回退 `PassthroughSummarizer.Generate` 逐事件
- [ ] 4.2.3 并发控制：桶间串行

### 4.3 失败处理与监控

- [ ] 4.3.1 降级 metric：`summarizer_fallback_total{reason}`
- [ ] 4.3.2 超时熔断：连续 N 次超时 → 降级 M 分钟
- [ ] 4.3.3 摘要质量校验：空/过长/语言不一致 → warning + 降级

### 4.4 配置

- [ ] 4.4.1 `Config.ArchiveSummarizer`：`Model / MaxBatchSize / TimeoutMs / Enabled`
- [ ] 4.4.2 `tagent.go resolveMemoryStore` 根据 `Enabled` 选 `PassthroughSummarizer` / `LLMSummarizer`

### 4.5 测试

- [ ] 4.5.1 单元测试：LLMSummarizer happy path（mock LLM）
- [ ] 4.5.2 批处理测试：10 事件分 3 桶
- [ ] 4.5.3 降级测试：LLM 超时 → PassthroughSummarizer
- [ ] 4.5.4 端到端：写满一周 → L2→L3 → 验证 EventSummary 填充

## Phase 5: 文档收尾

- [ ] 5.1 更新 `docs/wiki/memory/memory-architecture.md`：确保 Snowflake int64 格式说明
- [ ] 5.2 更新 `docs/.dev/20260504-event-storage-layered-architecture.md`（v2 已交付）
- [ ] 5.3 更新 `docs/.dev/20260504-rustviking-integration-evaluation.md`（v2 已交付）
- [ ] 5.4 更新 `tagent.go` 配置示例文档
- [ ] 5.5 删除废弃文件：journal 旧文件 / snapshot 旧测试 / `longestCommonPrefix` 测试

## Phase 6: 生产接线 + 端到端验证

### 6.1 生产接线

- [ ] 6.1.1 `resolveMemoryStore("file")` 创建流程：rel → store → Init → LM → summarizer → compactor → wire → start
- [ ] 6.1.2 关闭流程：Compactor.Stop → LM.Stop → flush tombstones → rel.Close，支持 context 超时，幂等
- [ ] 6.1.3 配置解析：`max_events_per_segment`、`archive_summary_types`、`RustViking.BinaryPath/ConfigPath`
- [ ] 6.1.4 集成测试：创建 store → Init → LM/Compactor 启 → 写事件 → 小时滚动 → L1→L2 → Close

### 6.2 端到端验证

- [ ] 6.2.1 重启恢复：写 N 事件 → 重启 → seq 连续不覆盖、cursor 恢复、bitmap 正确
- [ ] 6.2.2 TTL + Compaction：越过 TTL → checkTTL 游标前进 → L1→L2 触发 → 物理删除
- [ ] 6.2.3 多分区 TombstoneSet 隔离：pid=1 墓碑不影响 pid=2
- [ ] 6.2.4 RelationStore 规模：SetParent 1M 次，LRU 命中率 + kv miss 回读
- [ ] 6.2.5 GetChildren 分页：1000 子节点 limit=100 遍历
- [ ] 6.2.6 Snowflake 溢出：burst 5000 ID/ms 严格递增
- [ ] 6.2.7 L3 摘要化：L2→L3 时 EventSummary 填充
- [ ] 6.2.8 全量编译：`go build ./...` + `go test ./...` + `cargo build --release && cargo test`
- [ ] 6.2.9 BREAKING 迁移完整性：grep 无 `.GetParent(` 遗留、无 journal/snapshot 引用

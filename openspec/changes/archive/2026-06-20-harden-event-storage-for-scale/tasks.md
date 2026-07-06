## 0. 前置 · RustViking v0.2.0 交付

**依赖方**：RustViking 团队。详见 `rustviking/docs/tagent-integration-requirements.md`。tagent 侧任务 12 / 13 需等待本节完成后联调。

- [ ] 0.1 RustViking CLI 新增 `kv range` 子命令（R2）：`KvOperation::Range { start, end, limit }`，直接基于 RocksDB `iterator(IteratorMode::From(start, Forward))` 实现，遇到 key ≥ end 或达到 limit 时停止；返回统一 JSON `{success, data: {start, end, count, entries: [{key, value}]}, api_version: "v1"}`
- [ ] 0.2 RustViking CLI 修复 `kv batch` 原子性（R1）：`exec_kv_batch` 改用 `store.batch()?` 获取 `BatchWriter`，所有 put/delete 加入 WriteBatch 后 `commit()`
- [ ] 0.3 RustViking JSON 响应契约稳定（R3）：所有子命令输出固定 envelope `{success, data | error, api_version: "v1"}`
- [ ] 0.4 RustViking 基准测试（R4）：put P99 < 3ms、batch 100 ops P99 < 10ms、range scan 1000 keys P99 < 15ms 达标
- [ ] 0.5 RustViking 发布 v0.2.0 并更新 CHANGELOG

## 1. Key Schema v2 扩展

- [ ] 1.1 修改 `memory/key_schema.go` 增加 key format：
  - `global:active_partitions` → 256 byte bitmap
  - `{pid}:cursor` → JSON `{current_window, seq_counter, last_event_key, updated_at}`
  - `{pid}:ttl_cursor` → JSON `{next_scan_window, last_scan_time}`
  - `{pid}:rel:{child}` → relation entry
  - `{pid}:revrel:{parent}:{child}` → zero-byte reverse-index marker
- [ ] 1.2 新增 helper：`BuildActivePartitionsKey()` / `BuildCursorKey(pid)` / `BuildTTLCursorKey(pid)` / `BuildRelKey(pid, child)` / `BuildRevRelKey(pid, parent, child)`
- [ ] 1.3 新增 `ParseActivePartitionsBitmap(b []byte) []int` / `EncodeActivePartitionsBitmap(pids []int) []byte`
- [ ] 1.4 单元测试：验证 bitmap 编解码往返、cursor JSON 序列化稳定、key format 字符串完全匹配

## 2. FileSegmentStore Init v2（bitmap + cursor）

- [ ] 2.1 新增 `loadActivePartitions(kv KVStore) ([]int, error)`：读 `global:active_partitions` 一次性 get，解析成 pid 列表；缺失时返回空 slice 不报错
- [ ] 2.2 重写 `FileSegmentStore.Init()`：调用 `loadActivePartitions`，用 errgroup 并发读取每个 pid 的 `{pid}:cursor`（并发度 `runtime.NumCPU() * 4`），恢复 `PartitionState{currentWindow, seqCounter}`；不再扫描 `{pid}:meta:*` 或 `{pid}:evt:*` 前缀
- [ ] 2.3 `Init()` 失败容忍：单个 pid cursor 读失败时记 warning 并以零值继续，保证其他分区不受影响
- [ ] 2.4 单元测试：`Init` 在 1000 active partitions 场景下完成时间 < 500ms；零活跃分区时无 side effect；cursor 缺失时使用零值 state 并 warning

## 3. StoreEvent / StoreEvents v2

- [ ] 3.1 修改 `StoreEvent`：每次成功写入后，在同一 WriteBatch 内追加：
  - `{pid}:cursor` 更新（current_window, seq_counter, last_event_key, updated_at）
  - 若该 pid bitmap bit 未置，则 RMW `global:active_partitions` 置位
- [ ] 3.2 重写 `StoreEvents(events map[int64]*FullEvent)` 分组逻辑：
  - 先 `sort.Slice(keys)` 保证确定性迭代顺序
  - 按 `(pid, windowTS)` 分组为 `map[groupKey][]EventWithKey`
  - 对每个 group，取出对应 `PartitionState`，必要时 rotate 到新 window（seal 旧 window + 更新 currentWindow + seq 重置为 0）
  - 组内按稳定顺序逐个分配 seq，构造 evt/idx/meta KV op
  - 同 batch 追加 cursor 更新 + bitmap 更新
- [ ] 3.3 补 `ensureSegmentMeta`：每个新 window 在 batch 中追加一次 meta put；使用 `map[int64]bool` 去重同 batch 内重复 window
- [ ] 3.4 接入自适应 segment size：`StoreEvent` / `StoreEvents` 末尾检查 `state.currentSegmentEventCount >= cfg.MaxEventsPerSegment`，达到阈值则 seal 当前 window，rotate 到下一 window
- [ ] 3.5 `eventCount` 在成功写入后按本批次活事件数增加；加锁（`state.mu`）保护并发
- [ ] 3.6 `resolvePartitions(filter)` 当 `filter.PartitionIDs` 为空时调用 `loadActivePartitions` 返回全量；移除旧的 `return nil` 分支
- [ ] 3.7 单元测试：跨 window 批量写入 seq 不碰撞；同一 window 多次调用 seq 连续；`max_events_per_segment` 触发提前 seal；cursor 写入与 event 原子；bitmap 首次写入时 bit 置位；`resolvePartitions` 无过滤时返回全活跃分区

## 4. TombstoneSet 分区隔离 + 懒加载

- [ ] 4.1 `FileSegmentStore.tombstones` 改为 `sync.Map`（key=int pid, value=`*TombstoneSet`）
- [ ] 4.2 新增 `getTombstoneSet(pid int) *TombstoneSet`：`LoadOrStore` 懒创建；新创建时自动 `RecoverFromKV()`（扫 `{pid}:tomb:` 前缀，填入内存 set）
- [ ] 4.3 更新所有调用点（`MarkTombstone`/`IsTombstone`/`DeleteEvent` 等）改用 `getTombstoneSet(pid)`
- [ ] 4.4 单元测试：不同分区 tombstone 隔离、懒初始化触发自动 recover、重启后恢复

## 5. Lifecycle 扫描器修复 + TTL 游标

- [ ] 5.1 `checkTTL(pid)` 改造：
  - 读 `{pid}:ttl_cursor`（缺失时初始化为该分区最早 window）
  - 从 `cursor.next_scan_window` 开始 range scan 一批 `{pid}:meta:*`
  - 对过期 window 的事件批量反序列化 JSON，提取 `event_key` 和 `timestamp`，调用 `MarkTombstone(pid, eventKey)`
  - 扫描结束持久化更新后的 `next_scan_window`（停在首个未过期 window）
- [ ] 5.2 `evictOldest(pid)` 改造：优先扫最老 window（通过 `ttl_cursor.next_scan_window - 1` 定位），反序列化 JSON 提取 EventKey；重复执行直到 `eventCount` 降到 threshold 以下
- [ ] 5.3 新增 `ttl_cursor` 的 `LoadTTLCursor(kv, pid)` / `SaveTTLCursor(kv, pid, cursor)` helper
- [ ] 5.4 单元测试：TTL 游标仅前进、扫描跳过未过期 window、重启后从 cursor 继续、`extractInt64FromJSON("event_key")` / `extractInt64FromJSON("timestamp")` 解析稳定

## 6. RelationStore v2（LRU + RocksDB，砍 journal）

- [ ] 6.1 重写 `memory/relation_store.go`：
  - 字段：`hot *lru.Cache[int64, RelationEntry]`（默认 1M entries，用 `github.com/hashicorp/golang-lru/v2`）、`kv KVStore`、`pid int`、`mu sync.RWMutex`
  - 移除 journal 文件路径、SnapshotFile、ReplayJournal / Snapshot / LoadSnapshot 方法
- [ ] 6.2 实现 `SetParent(child, parent int64) error`：构造 `{pid}:rel:{child}` put + `{pid}:revrel:{parent}:{child}` put 到同一 WriteBatch；commit 后更新 LRU；失败不更新 LRU
- [ ] 6.3 实现 `GetParent(child int64) (int64, bool, error)`：先查 LRU，miss 则读 `{pid}:rel:{child}`，回填 LRU
- [ ] 6.4 实现 `GetChildren(parent int64, limit int) ([]int64, bool, error)`：range scan `{pid}:revrel:{parent}:` 前缀，`limit+1` 条用于判 hasMore；key 尾部解析成 child int64；`limit <= 0` 返回 `ErrInvalidLimit`
- [ ] 6.5 实现 `DeleteRelation(child)`：读出原 parent（走 GetParent），删 `{pid}:rel:{child}` + `{pid}:revrel:{parent}:{child}`，evict LRU
- [ ] 6.6 实现 `Close()`：sync RocksDB WAL（实际是 KVStore flush hook），保证 LRU 不残留未持久化状态（由 write-through 语义保证通常为空）
- [ ] 6.7 接口调整：`memory/types.go` 的 `RelationStore` 接口移除 `Snapshot`/`LoadSnapshot`/`ReplayJournal`；`GetChildren` 签名改为 `(parent int64, limit int) ([]int64, bool, error)`
- [ ] 6.8 单元测试：SetParent 后 GetParent hit LRU、LRU evict 后 miss → kv 回读、GetChildren limit + hasMore 分页、DeleteRelation 清反向索引、1M entry 容量下 LRU 表现符合预期、崩溃重启后数据完整（依赖 RocksDB WAL）

## 7. Snowflake 溢出阻塞

- [ ] 7.1 修改 `memory/snowflake.go`：同毫秒 seq 回绕为 0 时，`for time.Now().UnixMilli() == lastMs { time.Sleep(100 * time.Microsecond) }`，然后更新 `lastMs`
- [ ] 7.2 时钟回拨检测：`now < lastMs` 时返回 `ErrClockBackwards`
- [ ] 7.3 单元测试：单毫秒生成 5000 ID 时第 4097 个阻塞到下一毫秒且 ID 仍严格递增；时钟回拨返回 error

## 8. Compaction 修复

- [ ] 8.1 `filterTombstoned` 接入：签名改为 `filterTombstoned(events []Event, isTomb func(int64) bool, collected *[]int64) []Event`；Compactor 构造 `isTomb := getTombstoneSet(pid).IsTombstone`
- [ ] 8.2 `repairDanglingRefs` 修正 alive 集判定：`alive[e.EventKey] = true` for 本批次 ∪ `!tombstoneSet.IsTombstone(parentKey)` 视为活；仅 `IsTombstone(parentKey)` 时走 `findAliveAncestor`
- [ ] 8.3 `deleteSegments` 同步清理：WriteBatch 内追加：
  - 被过滤事件的 `{pid}:idx:{type}:{timestamp}:{event_key}` delete
  - `collected` 列表中的 `{pid}:tomb:{event_key}` delete
- [ ] 8.4 `eventCount` 递减：`deleteSegments` 完成后按 `SegmentMeta.EventCount` 递减该分区 `eventCount`
- [ ] 8.5 `checkL1ToL2()`：遍历 `loadActivePartitions()`，对每分区 ListSegments 过滤 `Layer==1`，满 24 段按时间排序取早 24 段调 `CompactL1ToL2()`
- [ ] 8.6 `checkL2ToL3()`：同上 `Layer==2`，满 7 段
- [ ] 8.7 `checkAndCompact()`：依次调用 `checkHourlySeal` → `checkL1ToL2` → `checkL2ToL3`；串行执行避免 IO 争抢
- [ ] 8.8 单元测试：跨批次 alive 集识别（E5→E2 已在 L2 不误判）、tomb key 清理、L1→L2 / L2→L3 自动触发、`eventCount` 递减

## 9. L3 摘要化 hook

- [ ] 9.1 新增 `memory/summarizer.go`：`SummaryGenerator` 接口 `Generate(event FullEvent) (string, error)`；默认实现 `PassthroughSummarizer`（返回 `""` 或 content 的前 N 字符截断）
- [ ] 9.2 `FullEvent` 新增 `EventSummary string` 字段（与现有字段并列，JSON tag `"event_summary,omitempty"`）
- [ ] 9.3 `Compactor` 构造注入 `SummaryGenerator`；`CompactL2ToL3` 按 `archive_summary_types[eventType]` 选 `full` / `summary` / `partial` 策略生成 L3 事件
- [ ] 9.4 配置加载：`PartitionDefaults.archive_summary_types`（map 形式）；未配置的 type 默认 `full`
- [ ] 9.5 单元测试：`full` 保留完整事件、`summary` 丢 content、`partial` 截断 content 到 max_chars、未知 type 默认 `full`

## 10. RelationStoreProvider 接口迁移（BREAKING）

- [ ] 10.1 `memory/types.go` `MemoryStore` 接口移除 `GetParent` / `GetChildren`
- [ ] 10.2 `RelationStoreProvider` 接口定义（命名化，替代匿名接口）：`RelationStore() RelationStore`
- [ ] 10.3 `InMemoryStore` / `FileSegmentStore` 实现 `RelationStoreProvider`；移除自身 `GetParent` / `GetChildren`
- [ ] 10.4 全量调用方适配：
  - `plugin/memory_plugin.go`
  - `tool/recall/*.go`（`RecallGetTool`、`RecallTraceTool`、`RecallSmartCompress`）
  - `agent/tool_agent.go`
  - 所有 `store.GetParent(k)` → `store.(memory.RelationStoreProvider).RelationStore().GetParent(k)`
  - 所有 `store.GetChildren(k)` → `store.(memory.RelationStoreProvider).RelationStore().GetChildren(k, limit)`（调用方补充 limit 参数；默认 100）
- [ ] 10.5 批量 grep 验证迁移完整：`grep -r "\.GetParent(" --include="*.go" | grep -v "RelationStore()"` 应为空；`GetChildren` 同理
- [ ] 10.6 编译验证：`go build ./...` 一次通过；依赖链 `plugin/` / `tool/` / `agent/` 全部适配

## 11. RustViking Client v2（纯 CLI + `kv range`）

- [ ] 11.1 `memory/rustviking_client.go` 锁定本地 CLI 模式：构造仅收 `binary_path` / `config_path`；不引入 `mode` 字段或 `ErrServerNotImplemented` 分支
- [ ] 11.2 `KVRange(start, end, limit)` 改用 `rustviking kv range -s <start> -e <end> -l <limit>` CLI 子命令；删除 `longestCommonPrefix` 辅助函数
- [ ] 11.3 JSON 响应解析统一走 `CLIEnvelope{Success, Data, Error, APIVersion}`；`api_version` 缺失时 warning log 不 fail
- [ ] 11.4 单元测试：CLI 模式 happy path、KVRange 跨分区不再因空前缀报错、batch 失败路径正确返回 error

## 12. 生产接线（tagent.go）

- [ ] 12.1 `resolveMemoryStore("file")` 创建流程：
  1. `rel := NewRelationStore(kv, pid, lruSize=1M)`
  2. `store := NewFileSegmentStore(kv, rel, cfg)`
  3. `store.Init()`
  4. `lm := NewLifecycleManager(store, lifecycleCfg)`
  5. `summarizer := NewPassthroughSummarizer()`
  6. `compactor := NewCompactor(store, kv, rel, compactionCfg, summarizer)`
  7. `store.SetLifecycleManager(lm)` / `store.SetCompactor(compactor)`
  8. `lm.Start()` / `compactor.Start()`
- [ ] 12.2 关闭流程：`store.Close()` 按 `Compactor.Stop` → `LifecycleManager.Stop` → flush tombstones → `rel.Close()` 顺序执行；支持 context 超时；幂等
- [ ] 12.3 配置解析：`PartitionDefaults.max_events_per_segment`（默认 10000）、`PartitionDefaults.archive_summary_types`（map）、`RustViking.BinaryPath` / `RustViking.ConfigPath`
- [ ] 12.4 集成测试：创建 store → 验证 Init 已调 → LM/Compactor 已启 → 写事件 → 小时滚动 → 24 小时后触发 L1→L2 → Close 干净退出

## 13. 集成 & 验证

- [ ] 13.1 端到端 · 重启恢复：写 N 事件 → 重启 → 新事件 seq 连续递增且不覆盖；cursor 恢复正确；`active_partitions` bitmap 正确
- [ ] 13.2 端到端 · TTL + Compaction：写事件 → 越过 TTL → `checkTTL` 游标前进并标 tombstone → 24 小时 L1→L2 触发 → 墓碑物理删除、idx/tomb key 清理
- [ ] 13.3 端到端 · 多分区 TombstoneSet 隔离：pid=1 标墓碑不影响 pid=2；重启后各分区恢复独立
- [ ] 13.4 端到端 · RelationStore 规模：连续 SetParent 1M 次，LRU 命中率监控、kv miss 正确回读
- [ ] 13.5 端到端 · GetChildren 分页：parent 1000 子节点，分批 limit=100 遍历
- [ ] 13.6 端到端 · Snowflake 溢出阻塞：burst 5000 ID/ms，ID 严格递增且后半批时间戳 > lastMs
- [ ] 13.7 端到端 · L3 摘要化：L2→L3 时 `assistant_response` 事件 `content=""` + `EventSummary=""`（pass-through）
- [ ] 13.8 全量编译验证：`go build ./...` + `go test ./...`（tagent）、`cargo build --release && cargo test`（RustViking）
- [ ] 13.9 BREAKING 迁移完整性：`grep -r "\.GetParent(" --include="*.go" | grep -v "RelationStore()\.GetParent"` 为空；journal / snapshot / ReplayJournal 无引用

## 14. 文档与清理

- [ ] 14.1 更新 `docs/.dev/20260504-event-storage-layered-architecture.md`（v2 已交付）
- [ ] 14.2 更新 `docs/.dev/20260504-rustviking-integration-evaluation.md`（v2 已交付）
- [ ] 14.3 更新 `tagent.go` 配置示例文档：`rustviking.binary_path` / `rustviking.config_path`、`partition_defaults.max_events_per_segment`、`partition_defaults.archive_summary_types`
- [ ] 14.4 删除废弃文件：RelationStore journal 相关旧文件 / 旧 snapshot 测试 / `longestCommonPrefix` helper 单元测试
- [ ] 14.5 创建 future-change placeholder：`openspec/changes/llm-event-summary/`（LLM 驱动的 EventSummary 生成；替换 `PassthroughSummarizer`）

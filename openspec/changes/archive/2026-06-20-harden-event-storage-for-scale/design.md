## Context

tagent 事件存储分层架构（L0 active → L1 hourly → L2 daily → L3 weekly archive）Phase 1-8 核心代码已落地，经过**四轮深度评审**后确认：

- **四轮评审**：v1 逐行审查 → v2 架构交叉审查 → v3 RustViking API 反向追踪 → v4 规模化假设复核
- **累计发现 24 个工作项**：17 缺陷修复 + 7 规模化能力补齐
- **规模假设修订**：从"百万级事件"修订为"**10 events/s × 3 年 ≈ 1B+ 事件**"；原 RelationStore 全量内存（30GB+）、Init 全扫 meta（10 分钟级）、TTL 全量扫描等设计假设全部崩溃

**本 change 是合并前必经的生产级加固**。项目尚未发布——不允许发布即腐坏，不允许把已知缺陷推给下一个 change；一次性修到位，换取后续长期的代码简洁。

**当前约束**：

- tagent 单进程串行写入（事件流 ~10 events/s，不存在多实例写入）
- RustViking **仅通过本地 CLI** `exec.Cmd` 调用（v0.2.0 起新增 `kv range` + `WriteBatch` 原子性）；tagent 不引入 server / gRPC / HTTP 模式
- 所有 KV 持久化后端都是同一个 RocksDB（tagent 不维护独立 WAL，崩溃恢复语义由 RocksDB WAL 保证）
- 生产接线 `tagent.go` 目前只创建了 FileSegmentStore + RelationStore，缺失 TombstoneSet / LifecycleManager / Compactor / Init 调用

详细背景与技术方案展开见 `docs/.dev/20260504-event-storage-layered-architecture.md`（v2）与 `docs/.dev/20260504-rustviking-integration-evaluation.md`（v2）。

## Goals / Non-Goals

### Goals

**Part A — 数据正确性**

- 修复重启后 seqCounter 归零导致事件覆盖（D1）
- 修复 StoreEvents 多窗口批量写 seq 碰撞（D2）
- 修复 TombstoneSet 多分区混淆（D3）
- 修复 TTL/Capacity 扫描器从 `ParseKey()` 拿到零值 EventKey（D4）
- 修复 RustViking `kv batch` 非原子性（D5）
- 修复 Compaction `filterTombstoned` 空桩、`repairDanglingRefs` 跨批次 alive 集错误（D7、D8）
- 修复 `resolvePartitions` 默认 nil 导致静默空结果（D9）

**Part A — 功能补全**

- 新增 RustViking `kv range` 子命令与 tagent 侧 `KVRange` 调用（D6）
- Compaction 索引清理 + L1→L2 / L2→L3 调度触发（D11、D13）
- `eventCount` 生命周期正确（D10）
- 生产接线补全（`tagent.go` 启动顺序、`Close()` 优雅关闭）（D13）

**Part A — BREAKING 接口**

- `MemoryStore` 移除 `GetParent`/`GetChildren`，收敛到 `RelationStoreProvider.RelationStore()`（D12）
- `RelationStore.GetChildren(parent, limit) ([]int64, hasMore, error)` 带分页（D12）
- `RelationStore` 移除 `Snapshot`/`LoadSnapshot`/`ReplayJournal`（D14）

**Part B — 规模化能力**

- RelationStore v2：LRU 热图 + RocksDB 冷图，砍独立 journal/snapshot（D14）
- `global:active_partitions` bitmap + `{pid}:cursor` 点查（D15）
- `{pid}:ttl_cursor` 时间游标扫描（D16）
- `max_events_per_segment` 自适应段大小（D17）
- L3 `archive_summary_types` 摘要化占位 + `EventSummary` 字段透传（D18）
- Snowflake 同毫秒溢出阻塞到下一毫秒（D19）

### Non-Goals

- **LLM 生成 EventSummary**：本 change 仅保留 hook 点；由独立 future change `llm-event-summary` 交付
- **RustViking server / gRPC / HTTP 模式**：tagent 锁定本地 CLI 集成，不预留 `mode` 配置；如未来确需水平扩展，单起 future change
- **水平扩展 / 多 tagent 实例**：未规划；若未来需要单独评估
- **向量搜索 / HNSW 索引**：Phase 2+ 独立 change
- **双写迁移 / 兼容旧 FileBackend**：项目未发布，直接覆盖
- **RocksDB Column Family 分层**：可选演进，本 change 不强依赖

## Decisions

### Decision 1 · Init 从 `active_partitions` bitmap + `{pid}:cursor` 恢复

**选择**：不扫 2048 个可能 pid 也不扫 meta；`Init()` 读两个固定 key：

1. `global:active_partitions`（2048 bit = 256 B 的 roaring-free bitmap）→ 列出活跃 pid 集合
2. 对每个活跃 pid，读 `{pid}:cursor` → `{ current_window, seq_counter, last_event_key }` → 直接恢复 `PartitionState`

**替代方案**：扫 `{0..2047}:meta:*`（O(活跃段数)，活跃分区 1000 × 段数 8K/day × 3 年 ≈ 10M+ key，耗时分钟级）；或扫 `{pid}:evt:` 前缀求最大 seq（更慢）。

**理由**：规模化后 meta key 数以千万计，不能线性扫。bitmap + cursor 将 Init 从 O(N_meta) 降到 O(active_partitions)，活跃分区 1000 级时 Init < 100ms。cursor 每次写事件时同步更新，作为 seqCounter 权威来源。

**关联**：key schema 新增 `global:active_partitions` 和 `{pid}:cursor:`；`StoreEvent`/`StoreEvents` 每批次末尾更新 cursor（同一 WriteBatch 内原子）。

### Decision 2 · StoreEvents 按 `(pid, windowTS)` 分组独立计 seq

**选择**：`StoreEvents(events map)` 入口立即按 `(pid, windowTS)` 分组到 `groups map[key][]Event`，每组：

1. `getPartitionState(pid)` 取状态
2. 若该组 windowTS 与 `state.currentWindow` 不同，则 rotate（seal 旧 window → 更新 currentWindow → seq 重置为 0）
3. 组内按稳定顺序（传入 map 的 key 先排序）逐个分配 `seq`，组成 `evtKey`

**替代方案**：原逻辑 `for key, event := range events` 直接用 map 迭代 + 同一 seqCounter，不同 window 下 seq 会相互覆盖（D1 原 bug）。

**理由**：Go map 迭代无序，跨 window 共用计数器会产生跨窗口 seq 碰撞，KV `{pid}:evt:{window}:{seq}` 出现不同事件同 key。按 window 分组是 O(n) 预处理，可忽略开销。

### Decision 3 · TombstoneSet 分区隔离 + 懒加载恢复

**选择**：`FileSegmentStore.tombstones` 改为 `sync.Map[pid]*TombstoneSet`。`getTombstoneSet(pid)` 懒初始化：

1. `LoadOrStore` 新 `TombstoneSet{pid, kv}`
2. 新创建时自动 `RecoverFromKV()`：扫 `{pid}:tomb:` 前缀，填入内存 set

**替代方案**：全局 TombstoneSet 存 `(pid, eventKey)` 复合键；或调用方显式 Recover。

**理由**：懒初始化 + 自动恢复消除"忘调 Recover"的陷阱。分区隔离与 `PartitionState` 语义对齐。每分区 tombstone 在 Compaction 后清零（D11），扫描代价可控。

### Decision 4 · TTL/Capacity 扫描从 JSON value 提取 EventKey/timestamp

**选择**：`checkTTL()` / `evictOldest()` 不再使用 `ParseKey()` 返回的 EventKey（恒为 0）。改为：

```go
eventJSON := value
eventKey, _ := extractInt64FromJSON(eventJSON, `"event_key":`)
ts, _       := extractInt64FromJSON(eventJSON, `"timestamp":`)
```

TTL 年龄判断用 `ts`（毫秒），墓碑标记用正确的 `eventKey`。

**替代方案**：扩展 key schema 把 EventKey 嵌入 key（破坏格式、需迁移）；或扫 index 反查（额外 KV get）。

**理由**：最小侵入，复用已有 `extractEventTypeFromJSON` 模式。JSON 解析在后台 goroutine，吞吐不敏感。

### Decision 5 · RustViking `kv batch` 改用 `WriteBatch::commit()`

**选择**：RustViking CLI `exec_kv_batch` 内部：

```rust
let batch = store.batch()?;
for op in ops { match op { Put => batch.put(..), Delete => batch.delete(..) } }
batch.commit()?;
```

全部 ops 原子落盘，失败则整体回滚。

**替代方案**：Go 侧拆成单 put/delete（丧失原子性）；Go 侧改写先写后删的 crash-safe 顺序（复杂且易错）。

**理由**：RustViking `KvStore::batch()` 已有返回 `BatchWriter` 的 API，CLI 只差一层调用；RocksDB WriteBatch 原生原子。改动 ~20 行 Rust，tagent 侧零改动。契约见 rustviking 需求单 R1。

### Decision 6 · RustViking 新增 `kv range` 子命令

**选择**：新增 `KvOperation::Range { start: String, end: String, limit: Option<usize> }`，直接调用 RocksDB `iterator(IteratorMode::From(start, Forward))`，遇到 key >= end 或到达 limit 时停止，返回 `{ prefix, count, entries: [{key, value}] }`。

tagent 侧 `RustVikingClient.KVRange(start, end, limit)` 替换原 `longestCommonPrefix(start, end)` 客户端过滤 hack。

**替代方案**：Go 侧继续全量拉 + 过滤（RocksDB 层 O(prefix 所有 key)，公共前缀 `17106` 可能命中多个小时窗口）。

**理由**：字节序范围扫描是 LSM 天然能力，精确 O(range 内 key)。hack 在大规模下不可接受（10 分钟窗口跨 100 分区，prefix `17106` 下可达 300K+ key）。契约见需求单 R2。

### Decision 7 · Compaction `filterTombstoned` 接入真实过滤

**选择**：签名改为 `filterTombstoned(events []Event, isTomb func(int64) bool, collected *[]int64) []Event`：

- 遍历 events，若 `isTomb(e.EventKey)` 则跳过并追加到 `collected`（供后续 D11 清理 tombstone key）
- 否则保留

Compactor 构造 `isTomb` 时通过 `store.getTombstoneSet(pid).IsTombstone`。

**替代方案**：Compactor 直接引用 TombstoneSet（耦合）；merge 阶段内联过滤（丢失测试注入点）。

**理由**：函数注入易于 mock；`collected` 切片承载副作用供 D11 统一清理，避免 Compactor 分两次扫 tombstone。

### Decision 8 · `repairDanglingRefs` 的 alive 集 = 本批次活事件 ∪ 非墓碑历史事件

**选择**：修正伪代码：

```go
alive := make(map[int64]bool)
for _, e := range filteredEvents { alive[e.EventKey] = true }       // 本批次活事件

repair := func(parentKey int64) {
    if alive[parentKey] { return }                                   // 本批次中
    if !tombstoneSet.IsTombstone(parentKey) { return }              // 历史非墓碑 → 视为活（已压入 L2/L3）
    // 真正 dangling：向上找最近活祖先
    newParent := findAliveAncestor(parentKey, tombstoneSet)
    patchParent(evt, newParent)
}
```

**替代方案**：原实现仅用本批次 alive 集 → 上批次压入 L2 的活祖先被误判为 dead，错误走 findAliveAncestor 路径。

**理由**：alive 的真正含义是"未被 tombstone 逻辑删除"，与是否在本批次正交。tombstone 是权威的 dead 标志。修正后时间复杂度不变（O(events)），但语义正确。

### Decision 9 · `resolvePartitions` 无过滤时读 `active_partitions` bitmap

**选择**：`resolvePartitions(filter)` 当 filter 无 PartitionID 约束时：

```go
if len(filter.PartitionIDs) == 0 {
    return loadActivePartitions(kv)   // 读 global:active_partitions bitmap
}
```

替代旧版返回 `nil`（导致上层 for range 空迭代 → 静默空结果）。

**替代方案**：保留 nil 语义，调用方每次外部展开（易错 + 四处散落）。

**理由**：语义对齐"未过滤即全部"。与 D1 Init 共用 bitmap 加载路径。

### Decision 10 · `eventCount` 增减闭环

**选择**：`PartitionState.eventCount` 在 `state.mu` 保护下：

- `StoreEvent`/`StoreEvents` 后 `++`
- `DeleteEvent` 后 `--`
- Compaction `deleteSegments()` 完成后，按 `SegmentMeta.EventCount` 减去对应值

**替代方案**：废弃 eventCount，实时 `ListSegments` 求和（O(段数)，热路径不可接受）。

**理由**：O(1) 内存计数器；容量逐出（`eventCount >= capacity`）依赖该值，必须闭环。

### Decision 11 · Compaction `deleteSegments` 同步清理 idx/tomb

**选择**：Compaction 末尾在 `deleteSegments()` 的 WriteBatch 内追加：

- 被物理删除事件的 `{pid}:idx:{type}:{timestamp}:{event_key}` put 操作（逻辑 delete）
- 被过滤墓碑（D7 collected 列表）的 `{pid}:tomb:{event_key}` delete 操作

三类 key（evt / idx / tomb）原子删除。

**替代方案**：保留 tomb 作审计；定时扫全量清理（重复工作）。

**理由**：tomb marker 的唯一用途是 Compaction 过滤；compaction 完成即失效。不清理会导致 `{pid}:tomb:` 无限增长。

### Decision 12 · `GetParent`/`GetChildren` 迁至 `RelationStoreProvider`，`GetChildren` 带 limit

**选择**：

```go
// BREAKING
type MemoryStore interface { /* 移除 GetParent, GetChildren */ }

type RelationStoreProvider interface { RelationStore() RelationStore }

type RelationStore interface {
    GetParent(key int64) (int64, bool, error)
    GetChildren(parent int64, limit int) (children []int64, hasMore bool, err error)
    // 其余方法
}
```

调用方 `store.GetParent(k)` → `store.(RelationStoreProvider).RelationStore().GetParent(k)`；`GetChildren` 统一 `limit int`，返回 `hasMore` 供分页。

**替代方案**：保留 `MemoryStore.GetParent/GetChildren`（`InMemoryStore` 的 legacy 设计）；或保持 `GetChildren([]int64)` 不分页（在 L3 规模下可能返回数万子节点，热点内存爆炸）。

**理由**：职责分离——`MemoryStore` = 事件 CRUD；`RelationStore` = 关系图。分页避免调用方一次拉全量子节点；`hasMore` 语义清晰优于 cursor/token（后续如需二次分页再演进）。

### Decision 13 · 生产接线启动顺序 + `Close()` 优雅关闭

**选择**：`resolveMemoryStore("file")` 启动顺序：

```go
1. store := NewFileSegmentStore(kv, rel, path, cacheSize)
2. store.Init()                             // bitmap + cursor 恢复
3. lm := NewLifecycleManager(store, cfg)    // 内部自动复用 store.getTombstoneSet
4. cmp := NewCompactor(store, kv, rel, cfg)
5. store.SetLifecycleManager(lm); store.SetCompactor(cmp)
6. lm.Start(); cmp.Start()
```

`Close()` 逆序：

```go
1. cmp.Stop()            // 等当前 compaction 任务完成
2. lm.Stop()             // 停 TTL/Capacity goroutine
3. flushAllTombstones()  // dirty 批量落盘
4. rel.Close()           // RelationStore v2 flush LRU dirty page → RocksDB
```

**替代方案**：FileSegmentStore 构造时自启（配置耦合）；进程退出靠 OS 清理（RocksDB WAL 重放，但 tombstone dirty 可能丢失）。

**理由**：外部持有子系统引用便于测试 mock；Close 顺序保证"停写 → 停扫 → 落盘"不交叉。

### Decision 14 · RelationStore v2 · LRU 热图 + RocksDB 冷图，砍 journal/snapshot

**选择**：重写 RelationStore：

```go
type RelationStore struct {
    hot *lru.Cache[int64, RelationEntry]   // 默认 1M entries ≈ 48 MB
    kv  KVStore                            // 冷图持久化
    mu  sync.RWMutex                       // 保护 hot; 写穿透到 kv
}

func (r *RelationStore) SetParent(child, parent int64) error {
    entry := RelationEntry{Parent: parent, UpdatedAt: now()}
    // 写穿透：先 kv 后 hot（崩溃时 hot 丢，kv 权威）
    if err := r.kv.Put(fmt.Sprintf("%d:rel:%d", pid, child), encode(entry)); err != nil { return err }
    if err := r.kv.Put(fmt.Sprintf("%d:revrel:%d:%d", pid, parent, child), nil); err != nil { return err }
    r.hot.Add(child, entry)
    return nil
}

func (r *RelationStore) GetParent(child int64) (int64, bool, error) {
    if e, ok := r.hot.Get(child); ok { return e.Parent, true, nil }
    // miss: 读 kv
    raw, err := r.kv.Get(fmt.Sprintf("%d:rel:%d", pid, child))
    if err != nil || raw == nil { return 0, false, err }
    e := decode(raw)
    r.hot.Add(child, e)
    return e.Parent, true, nil
}

func (r *RelationStore) GetChildren(parent int64, limit int) ([]int64, bool, error) {
    // kv range scan: {pid}:revrel:{parent}:
    start := fmt.Sprintf("%d:revrel:%d:", pid, parent)
    end   := start + "\xff"
    entries, err := r.kv.Range(start, end, limit+1)
    // 解析 key 尾部 child，取前 limit 个，hasMore = len(entries) > limit
    ...
}
```

**砍掉**：`ReplayJournal` / `Snapshot` / `LoadSnapshot` / 独立 journal 文件路径。

**替代方案**：保留内存全量 + journal（1B 事件下 ≈ 30 GB 内存爆炸）；journal + snapshot（启动仍需全量重放，O(1B) 分钟级，且引入独立 WAL 冗余）。

**理由**：

- **规模化必然**：1B 关系边不能全内存；LRU 1M 覆盖近期父链查询（按 Zipfian 分布 >95% hit）
- **崩溃一致性**：RocksDB WAL 已保证 `{pid}:rel:` / `{pid}:revrel:` 的原子持久化，tagent 不需要独立 WAL
- **代码简化**：砍 journal 实现 ≈ -400 行；启动不再 replay
- **反向索引**：`{pid}:revrel:{parent}:{child}` 零值 put，GetChildren 走 range scan（依赖 D6 kv range）

**关联 BREAKING**：`RelationStore` 接口移除 `Snapshot/LoadSnapshot/ReplayJournal`。

### Decision 15 · `global:active_partitions` bitmap + `{pid}:cursor`

**选择**：key schema 新增两类：

- `global:active_partitions` → 2048 bit = 256 B 固定长度 value；Set bit on 分区首次写入，Clear bit on 分区最后一个事件删除（可延迟）
- `{pid}:cursor` → JSON `{current_window, seq_counter, last_event_key, updated_at}`；每个 StoreEvent/StoreEvents 批次结束时原子更新（同 WriteBatch）

**替代方案**：Roaring Bitmap（引入 1.5 MB 依赖，对 2048 pid 过度设计）；扫描发现（见 D1 拒绝理由）。

**理由**：256 B 固定开销，单次 KV get 搞定；cursor 是 seqCounter / currentWindow 的权威来源，取代扫描恢复。

### Decision 16 · TTL 游标扫描 `{pid}:ttl_cursor`

**选择**：每分区维护 `{pid}:ttl_cursor` = `{next_scan_window, last_scan_time}`：

```go
func checkTTL(pid int) {
    cursor := loadTTLCursor(pid)   // next_scan_window
    // 从 cursor.next_scan_window 开始 range scan 一批 window meta
    // 计算 age = now - window_ts，若 age >= TTL → 标 tombstone
    // 扫描完毕更新 cursor.next_scan_window = 最后扫描的 window + 1
    // 若 age 未到 TTL → 停止扫描，cursor 不前进
}
```

**替代方案**：每次全量扫 `{pid}:meta:*`（10 events/s × 3 年 × 1000 分区 ≈ 10M+ meta，每小时全扫不现实）。

**理由**：TTL 扫描天然单调（时间前进不回退），游标只前进；每次仅扫 O(过期 windows) ≈ O(10 windows/hour)。

### Decision 17 · `max_events_per_segment` 自适应段大小

**选择**：`PartitionDefaults` 新增 `max_events_per_segment`（默认 10K）。`StoreEvent` 末尾检查：

```go
if state.currentSegmentEventCount >= cfg.MaxEventsPerSegment {
    sealCurrentWindow(state)   // 提前 seal，不等 window 边界
    rotateToNextWindow(state)
}
```

L1 / L2 段同理（按事件数触发提前合并）。

**替代方案**：按字节数（需 flush 后统计，延迟）；固定窗口（突发流量下单段超百万事件，读放大严重）。

**理由**：突发写入下（如批量导入 1M 事件），单段事件数可控，Compaction merge 和下游 range scan 代价稳定。

### Decision 18 · L3 摘要化占位（`archive_summary_types` + `EventSummary` 字段）

**选择**：`PartitionDefaults.archive_summary_types` 按 event type 配置策略：

- `full`：L3 保留完整 event
- `summary`：L3 丢 content、保留 metadata + `EventSummary` 字段
- `partial`：L3 保留 metadata + 截断的 content

L2→L3 合并时按配置生成 L3 事件。**本 change 的 `EventSummary` 生成为 pass-through**（直接透传空字符串或前 N 字符截断）；真正的 LLM 生成归 future change `llm-event-summary`，在同一 hook 点替换实现即可。

**替代方案**：L3 仅保留 metadata（信息丢失）；本 change 内联 LLM 调用（引入 model/ 依赖，超出 scope）。

**理由**：hook 点先留好，pass-through 先确保 L3 归档路径可跑通；后续独立 change 仅替换 `generateSummary` 函数。

### Decision 19 · Snowflake 同毫秒溢出阻塞

**选择**：Snowflake 12 bit sequence = 4096。同毫秒 seq 耗尽时：

```go
if seq == 0 {  // 溢出
    for now() == lastMs { time.Sleep(100 * time.Microsecond) }
    lastMs = now()
}
```

**替代方案**：换算法（Sonyflake / UUIDv7，变更接口）；抛错让上游重试（破坏串行语义）。

**理由**：10 events/s << 4096/ms，正常不触发；突发写（批量导入）才触发阻塞，自然限流。改动 < 10 行。

## 废弃迁移策略（合并声明）

**项目未发布前提下**，本 change 明确废弃以下"兼容/迁移"能力：

- 不提供旧 FileBackend → 新 FileSegmentStore 双写
- 不提供 RelationStore v1 journal → v2 RocksDB 迁移工具
- 不保留旧 `GetChildren([]int64)` 旧签名别名
- 不保留旧 `MemoryStore.GetParent` / `GetChildren`

所有破坏性变更在同一 commit 内改齐（`memory/` + `tool/recall/` + `agent/` + `plugin/`），编译失败即发现遗漏。

## Risks / Trade-offs

### 高风险

- **RustViking v0.2.0 交付延期**：阻塞 tagent 端实施（R1 batch 原子、R2 kv range、R4 性能底线）。
  - 缓解：需求单已提交（`rustviking/docs/tagent-integration-requirements.md`），契约冻结；tagent 可先完成纯 Go 侧工作（D1-D4, D7-D13, D15-D19），RustViking 就绪后联调 D5/D6/D14。

- **RelationStore LRU miss 率不可控**：首次访问深链路时连续 miss 导致尾延迟飙升。
  - 缓解：Init 预热最近 N 天的活跃 parent（从 `active_partitions` + 最近 cursor 事件批量加载）；暴露 `relation_store.cache_hit_rate` metric；LRU 大小配置化（默认 1M，可调）。

- **1B 事件规模下 RocksDB key 数估算**：evt + idx + meta + tomb + rel + revrel ≈ 单事件 5-8 个 key，1B 事件 → 5-8B key。
  - 缓解：依赖 RocksDB LZ4 压缩（key 前缀重复度高，压缩比 ~5-10×）；LSM 多层分摊到 L0-L6；定期 RocksDB compaction 回收 tombstone；监控磁盘占用。

### 中风险

- **Init 在 1000 活跃分区下的冷启动时延**：`global:active_partitions` 读 + 1000 次 `{pid}:cursor` 读。
  - 缓解：cursor 读并发（errgroup），1000 次并发 KV get 在单机 RocksDB 下 < 200ms（R4 目标：put P99 < 3ms）；加启动耗时日志与 alert。

- **TTL 游标漂移**：若某次扫描被 kill，游标可能未持久化导致漏扫。
  - 缓解：扫描结束时持久化游标（RocksDB WAL 保证）；每次启动从 `ttl_cursor` 恢复；最坏情况下重扫一批，无正确性问题。

- **Compaction 期间读放大**：L1→L2 merge 时需并发读 24 个 L1 段。
  - 缓解：单任务串行调度（D13 compaction scheduler），避免多分区并发 compaction；依赖 `eventCache` LRU 复用最近事件；RocksDB iterator snapshot 语义保证一致性。

### 低风险

- **BREAKING 接口影响范围**：`memory/types.go`、`memory/in_memory_store.go`、`plugin/memory_plugin.go`、`tool/recall/*.go`、`agent/tool_agent.go`。
  - 缓解：编译期发现；单 commit 批量改齐。

- **Close 超时**：`cmp.Stop()` 等当前 compaction 完成，可能长达数秒。
  - 缓解：Close 暴露 context，支持超时；超时时 RocksDB WAL 已持久化到最后 batch，后续重启恢复正常。

## Migration Plan

**不涉及数据迁移**（项目未发布，无生产数据）。

部署顺序：

1. **RustViking v0.2.0 发布**（`kv range` + WriteBatch 原子性 + JSON 响应契约稳定）
2. **tagent Go 侧实施**（按 tasks.md 任务分组顺序，CI 必须绿）
3. **Integration 测试覆盖**：重启恢复、TTL 标记、容量逐出、Compaction 墓碑过滤、RelationStore LRU miss → RocksDB 回读、Snowflake 同毫秒溢出
4. **合并到 main**，随下一次 tagent 发布上线
5. **Future**：`llm-event-summary`（替换 D18 pass-through）；若后续确需水平扩展，单起 future change 评估 RustViking server 集成

回滚策略：项目未发布，无回滚需求。

## Open Questions

- **`active_partitions` bitmap 的 clear 策略**：分区最后一个事件删除后是否立即清 bit？
  → 暂定"延迟清除"：Compaction L3 归档周期（周级）检查分区 `eventCount == 0` 时清 bit；避免抖动。
- **LRU 预热策略**：Init 时预热多少历史 parent？
  → 暂定"不预热"，依赖运行时按需加载；如观察到尾延迟问题，再加配置项 `relation_store.warm_up_recent_days`。
- **EventSummary 字段在 L0-L2 的处理**：仅 L3 填充，还是所有层都透传？
  → 暂定仅 L3 填充；L0-L2 的事件 `EventSummary = ""`。LLM change 介入时按 L2→L3 合并阶段生成。
- **L3 周级归档路径**：保留在同一 RocksDB 还是独立冷存储？
  → 本 change 保留同一 RocksDB（简单、依赖 RustViking LZ4 压缩）；如三年后磁盘告急，再独立 change `l3-cold-storage-offload`。

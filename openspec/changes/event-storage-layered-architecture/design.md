## Context

tagent 当前 `FileBackend` 以每事件一个 JSON 文件存储事件，在百万级事件场景下 `QueryEvents` 全表扫描 O(N) 不可接受。经过两轮深度设计评审（见 `docs/.dev/20260504-event-storage-layered-architecture.md` 和 `docs/.dev/20260504-rustviking-integration-evaluation.md`），确定了以 RocksDB LSM 为隐喻的分层架构，并引入兄弟项目 RustViking 承担底层存储/索引职责。

当前 `MemoryStore` 接口调用方：`MemoryPlugin`（写入）、`RecallAgent` 子工具（查询）、`SmartCompress`（压缩 + 关系重写）、`KnowledgeAgent` 子工具（知识检索）。所有调用方都在 `tool/` 和 `plugin/` 包内，无外部依赖。

约束：tagent 单进程运行，事件流串行，同一 PartitionID 不存在并发写入。

## Goals / Non-Goals

**Goals:**
- `QueryEvents` 从 O(N) 全扫描优化到 O(segment_count) 时间裁剪查询，百万事件 < 10ms
- 事件关系（ParentKey）与内容分离，支持 O(1) 关系变更而不重写事件文件
- 建立 L0→L1→L2→L3 分层数据生命周期，通过 Compaction 自动清理无效数据
- 集成 RustViking RocksDB KV 作为底层存储引擎，替换自研文件 IO
- MemoryStore 接口保持对调用方的语义兼容，内部实现完全替换

**Non-Goals:**
- 不实现分布式/多机部署（Phase 1 单机）
- 不实现向量语义搜索（Phase 2，但为 HNSW 集成预留接口）
- 不修改 RecallAgent / SmartCompress / KnowledgeAgent 的业务逻辑
- 不引入 gRPC/HTTP 服务化（初期通过 CLI 调用 RustViking）

## Decisions

### Decision 1: 内容与关系分离（Content-Relation Separation）

**选择**: `FullEvent` 移除 `ParentKey` 字段，事件关系由独立的 `RelationStore` 维护（内存双图 + WAL journal）。

**替代方案**: 保留 ParentKey 在事件文件中，关系变更时重写文件。

**理由**: 事件内容不可变（适合 append-only 段文件），但 compress/recall 过程中关系频繁变更。分离后关系变更只是内存 map 操作 + 一行 journal 追加，零文件 IO。关系数据量极小（~16B/事件，百万事件约 30MB），全量常驻内存完全可接受。

### Decision 2: 时间窗口段文件（1小时粒度）

**选择**: 事件按 `timestamp / 3600 * 3600`（小时对齐）划入段文件，段文件格式为 JSON Lines（每行一个事件，无缩进）。

**替代方案**: 固定大小段（如每 10,000 事件一段）、按 EventType 分文件。

**理由**: EventKey（Snowflake）自带毫秒时间戳，可从 key 直接推导段文件名，零外部索引。小时粒度在"事件密度"和"段数量"之间平衡——每天最多 24 段，段内通常 100-500 事件，.idx 索引文件仅几十 KB。JSON Lines 格式流式可解析，兼容性好，相比 `json.MarshalIndent` 节省约 30% 空间。

### Decision 3: 四层 LSM 分层架构

**选择**:
- **L0（热层）**: `active.jsonl` — 当前小时 append-only 写入，无索引
- **L1（温层）**: 24 小时内小时段 — 未压缩 JSON Lines + .idx 偏移索引
- **L2（冷层）**: 1-7 天日段 — gzip 压缩 JSON Lines + .idx，Compaction 合并 24 个 L1 段为一个日段
- **L3（归档层）**: 7 天+周段 — gzip + 低价值事件摘要化（丢弃 Content/ToolCalls）

**理由**: 参考 RocksDB LSM-Tree，不同层级的访问频率和存储成本不同。大多数查询命中 L0/L1（< 10ms），冷数据查询代价逐步增加但极罕见。Compaction 异步执行不阻塞在线读写。

### Decision 4: RustViking CLI 集成模式

**选择**: tagent 通过 `exec.Cmd` 调用 RustViking CLI 二进制文件，传递 JSON 格式的 KV 操作，接收统一 JSON 响应。

**替代方案**: CGO 嵌入 Rust 库、gRPC server 模式。

**理由**:
- CLI 模式零编译依赖，部署简单（仅需二进制文件在 PATH）
- 进程隔离，RustViking 崩溃不影响 tagent 主进程
- 批量操作（batch write/scan）摊薄每次 ~1ms 的进程启动开销
- 为未来 gRPC 扩展预留接口（RustVikingClient 接口抽象，切换透明）
- 初期事件频率 ~1-10 event/s，CLI 延迟完全不构成瓶颈

### Decision 5: KV Key Schema 设计

**选择**: 利用 RocksDB 字节序排序特性，设计前缀方案：
```
{pid}:evt:{window_ts}:{seq}  → 事件 JSON 内容
{pid}:idx:{event_key}        → {window_ts}:{seq}（偏移索引）
{pid}:meta:{window_ts}       → 段元信息 JSON
{pid}:tomb:{event_key}       → ""（墓碑标记）
```

**理由**: prefix scan 天然支持"列出某分区所有段"（`{pid}:meta:`）、"列出段内所有事件"（`{pid}:evt:{window_ts}:`）、时间范围裁剪（range scan `{pid}:evt:{start}` ~ `{pid}:evt:{end}`）。无需额外索引结构。

### Decision 6: Tombstone 机制

**选择**: Tombstone 在内存中用 Bitmap 维护（借助 RustViking Roaring Bitmap），Compaction 时批量过滤并物理删除。

**理由**: 标记即时（内存操作），清理延迟（Compaction 时批量处理）。Bitmap 支持交集/并集/差集运算，Compaction 时高效过滤墓碑事件。Bitmap 可通过 RustViking 持久化到 RocksDB，重启不丢失。

### Decision 7: Compaction 调度

**选择**: 后台 goroutine 定时检查（每 5 分钟），满足条件时执行 compaction。每次只执行一个任务防止 IO 争抢。Compaction 流程：先写新段、后删旧段（crash-safe）。

**理由**: 不阻塞在线读写。写新删旧的顺序保证崩溃安全——崩溃时旧段仍在，重启后重新 compact。

### Decision 8: MemoryStore 接口演进

**选择**: 接口新增 `GetParent(key)` 和 `GetChildren(key)` 方法以暴露 RelationStore 能力。`FullEvent` 不再包含 `ParentKey`。`EventReference` 的 ParentKey 由调用层通过 RelationStore 按需填充。

**理由**: 最小接口变更。现有调用方（RecallAgent 的 `memory_trace`、SmartCompress）从"读文件取 ParentKey"改为"调 RelationStore.GetParent()"——性能从 ~1ms 提升到 <1μs，且语义更清晰。

## Risks / Trade-offs

- **[风险] RustViking CLI 进程启动开销（~1ms/次）** → 缓解：批量操作（batch write/scan）摊薄开销；热路径 GetEvent 通过 EventCache LRU 减少调用频次；监控延迟，必要时切换到 Library 嵌入模式
- **[风险] RustViking 单点故障** → 缓解：独立进程崩溃不影响 tagent；操作失败时返回 error，上层可重试；RocksDB WAL 保证数据不丢失
- **[风险] RelationStore 全量内存，千万级事件时 ~300MB** → 缓解：当前场景百万级已覆盖数年运行；如超阈值可分段加载（热数据全量、冷数据按需）
- **[风险] Compaction 与写入并发时的段文件一致性问题** → 缓解：Compaction 只处理已 seal 的 L1 段，不与 active.jsonl 写入竞争；Compaction 写新段→删旧段，崩溃可恢复
- **[权衡] FullEvent 移除 ParentKey 是 BREAKING 变更** → 接受：此变更仅影响内部调用方（RecallAgent、SmartCompress），同步修改成本可控；长期收益（关系变更零 IO）远超短期适配成本
- **[权衡] JSON Lines 无 schema 校验** → 接受：schema 由 Go struct 定义，序列化/反序列化自动校验；如需严格 schema 可后续加

## Migration Plan

**Phase A — 双写验证**:
1. 部署新 `FileSegmentStore` + `RelationStore` 与旧 `FileBackend` 并存
2. `MemoryPlugin.onEvent()` 同时写入两个后端
3. 查询仍走旧 FileBackend，新后端仅做写入验证

**Phase B — 切换读**:
1. RecallAgent / SmartCompress / KnowledgeAgent 切换到新 FileSegmentStore 读取
2. 旧 FileBackend 保留为只读（回滚用）
3. 充分验证 QueryEvents / GetEvent / Trace 等所有查询路径

**Phase C — 历史迁移**:
1. 后台工具将旧 FileBackend 的事件批量读取 → 转换格式 → 写入新 FileSegmentStore
2. 同时重建 RelationStore（解析每个事件的 ParentKey）
3. 迁移完成后删除旧文件

**Phase D — 清理**:
1. 删除 `memory/file_backend.go` 和相关测试
2. 移除双写逻辑
3. 更新文档和配置

**回滚策略**: 任何阶段发现严重问题，将 `MemoryStore` 实现切回 `FileBackend`（旧代码保留到 Phase D）。旧数据文件未被删除（直到 Phase C 完成），可随时恢复。

## Open Questions

- RustViking 的 `scan_prefix`/`range` 目前返回全量 `Vec`，大数据量时内存压力大。是否需要推动 RustViking 团队增加迭代器接口？→ 当前小时段内 ~500 事件，一次性加载可接受。监控后再决策。
- EventCache LRU 的大小和逐出策略如何与分层架构协调？→ 初步设定 1000 条，基于实际访问模式 tuning。
- Compaction 的 CPU/IO 预算如何限制？→ 初期单线程串行执行，每次最多 compact 24 段。后续可加限速。

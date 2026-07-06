## Why

tagent 当前 FileBackend 以"每事件一个 JSON 文件"的方式持久化事件。在长时间运行场景（tagent 预期持续运行数月甚至数年，百万级事件），这种设计导致三个致命问题：(1) `QueryEvents` 全表扫描 O(N)，热路径延迟从毫秒级退化到秒级；(2) 事件关系（ParentKey）与事件内容耦合在同一文件中，压缩/关联变更需重写不可变的事件文件；(3) 无数据生命周期管理，磁盘无限增长。此时正是重新设计事件存储底层的最佳时机——趁数据量级尚小、接口调用方仅限内部，建立可支撑长期运行的存储架构。

## What Changes

- **新增 `FileSegmentStore`**：以时间窗口段文件（小时粒度 JSON Lines）替代每事件一文件，利用 EventKey 自带时间戳零成本定位段，提供 prefix/range scan 能力
- **新增 `RelationStore`**：将 ParentKey 关系从事件内容中剥离为独立的内存图存储（childToParent + parentToChildren 双 map + WAL journal），支持 O(1) 关系变更，解决"可变关系 vs 不可变内容"的核心矛盾
- **新增 Compaction 引擎**：参考 RocksDB LSM 分层思想，实现 L0(热) → L1(温) → L2(冷) → L3(归档) 四层渐进降级 + 墓碑标记/过滤/清理的 compaction pipeline
- **新增数据生命周期管理**：TTL 过期、容量逐出、类型权重策略，通过 Tombstone 标记 → Compaction 物理清除
- **新增 RustViking 集成层**：通过 `RustVikingClient` 封装 CLI 调用，利用 RustViking 的 RocksDB KV 存储（替代自研文件 IO）、HNSW 向量索引（Phase 2 语义搜索）、Bitmap 索引（墓碑集合运算）
- **修改 `MemoryStore` 接口**：新增 `GetParent`/`GetChildren` 方法以暴露 RelationStore 能力；FullEvent 移除 ParentKey 字段 (**BREAKING**)
- **移除 `FileBackend`**：旧版每事件一文件的存储实现将被完全替换 (**BREAKING**)

## Capabilities

### New Capabilities

- `event-segment-store`: 时间窗口分段事件存储，支持按 EventKey 精确定位、按时间范围 prefix/range 扫描、段内偏移索引 O(log N) 点查
- `event-relation-store`: 独立的事件因果关系图存储（全量内存 + WAL 持久化），支持 SetParent/GetParent/GetChildren/RemoveRelations 及快照恢复
- `event-compaction`: LSM 分层 compaction 引擎（L0 seal → L1 段文件 → L2 日段 gzip → L3 周段摘要化），含墓碑过滤、悬垂引用修复、原子批量写入
- `event-lifecycle`: TTL 过期 + 容量逐出 + 类型权重策略，基于 Tombstone 标记 + Compaction 物理清理
- `rustviking-client`: RustViking CLI 封装层，提供 KVPut/KVGet/KVScan/KVRange/KVBatch 等方法，桥接 Go 业务语义与 Rust 底层存储

### Modified Capabilities

<!-- 此次为全新架构，不修改现有 spec 级行为 -->

## Impact

- **新增文件**：`memory/segment_store.go`（FileSegmentStore）、`memory/relation_store.go`（RelationStore + InMemRelationStore）、`memory/compaction.go`（Compaction 引擎）、`memory/tombstone.go`（墓碑管理）、`memory/lifecycle.go`（数据生命周期）、`memory/rustviking_client.go`（RustViking CLI 封装）、`memory/key_schema.go`（KV key 格式定义）
- **修改文件**：`memory/types.go`（FullEvent 移除 ParentKey，新增关系类型）、`memory/in_memory_store.go`（适配 RelationStore）、`plugin/memory_plugin.go`（适配新 StoreEvent 流程）
- **移除文件**：`memory/file_backend.go`、`memory/file_backend_test.go`
- **外部依赖**：新增对 rustviking CLI 二进制文件的运行时依赖（单机部署时 tagent 通过 `exec.Cmd` 调用）
- **向后不兼容**：FullEvent.ParentKey 移除，EventReference 的 ParentKey 通过 RelationStore 填充；旧 FileBackend 数据需通过迁移工具转换

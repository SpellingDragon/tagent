# tagent/memory 模块架构文档

## 一、模块定位

`tagent/memory` 是 tagent 的**结构化事件存储层**，为 Agent 提供因果链追踪、按需精确检索、多维度查询能力。

**核心职责**：
- 定义 `FullEvent`（完整事件）和 `EventReference`（轻量引用）的数据结构
- 定义 `MemoryStore` 接口规范
- 提供 `InMemoryStore`（内存实现）和 `FileSegmentStore`（基于 KV store 的分层存储实现）
- 通过 `EventKey` 和 `RelationStore.SetParent` 构建有向因果事件链
- 提供向量搜索接口（`SearchByEmbedding`），当前实现返回 `ErrVectorSearchNotSupported`，可扩展接入向量数据库

**设计原则**：
- **信息隔离**：Session 只保存轻量引用（`EventReference`），完整数据在 MemoryStore
- **因果优先**：每个事件通过 `RelationStore.SetParent` 指向其前驱事件，支持因果回溯
- **视图独立**：压缩只修改 LLM 消息视图，不修改 MemoryStore 中的数据
- **向量搜索扩展**：`MemoryStore` 接口包含 `SearchByEmbedding`/`StoreEventWithEmbedding`/`SupportsVectorSearch`，当前实现返回 `ErrVectorSearchNotSupported`，可扩展接入向量数据库

---

## 二、文件清单

| 文件 | 职责 |
|------|------|
| `types.go` | 数据结构定义（FullEvent、EventReference、MemoryStore、QueryOptions、Snowflake EventKey）+ 向量搜索接口 |
| `in_memory_store.go` | 内存存储实现（测试/原型场景）+ 向量搜索空实现 |
| `segment_store.go` | 基于 KV store 的分层存储实现（L0/L1/L2/L3 时间窗分段） |
| `relation_store.go` | 因果链关系存储（SetParent/GetParent/GetChildren，LRU+可选 KV 持久化） |
| `compaction.go` | 分层压实调度（L1→L2→L3 自动压实） |
| `lifecycle.go` | TTL 生命周期管理（过期墓碑标记；`context_compress_summary` 固化物豁免 TTL 与容量淘汰） |
| `tombstone.go` | 墓碑集管理（标记已删除事件） |

---

## 三、组件关系总览图

```mermaid
graph TB
    subgraph "tagent/memory"
        MS["MemoryStore\n(Interface)"]
        FE["FullEvent\n(完整数据)"]
        ER["EventReference\n(轻量引用)"]
        EK["EventKey\n(Snowflake int64)"]
        RS["RelationStore\n(因果链管理)"]
        PID["PartitionID\n(存储分区)"]
        QO["QueryOptions\n(过滤/分页)"]
        RAG["Vector Search\n(SearchByEmbedding)"]
    end

    subgraph "实现"
        IM["InMemoryStore\n(map[int]map[int64]FullEvent)\n+ Vector Stub"]
        FB["FileSegmentStore\n(KVStore + L0-L3 时间窗分段\n+ LRU/墓碑/压实 + Vector Stub)"]
    end

    MS --> FE
    MS --> ER
    MS --> EK
    MS --> RS
    MS --> PID
    MS --> QO
    MS --> RAG

    MS -.-|"实现"| IM
    MS -.-|"实现"| FB

    style MS fill:#e1f5ff,stroke:#0277bd,stroke-width:2px
    style FE fill:#fff3e0,stroke:#ef6c00
    style ER fill:#e8f5e9,stroke:#2e7d32
    style EK fill:#f3e5f5,stroke:#7b1fa2
    style PK fill:#f3e5f5,stroke:#7b1fa2
    style RAG fill:#fce4ec,stroke:#c2185b
```

---

## 四、核心数据结构

### 4.1 EventKey — Snowflake 64-bit 事件唯一标识符

**格式**（`memory/types.go`，snowflake-overflow-handling 修复后）：

```go
// EventKey is a 64-bit integer following a Snowflake-like layout:
//
//	┌────┬─────────────┬──────────────────┬─────────────┬────────────────┐
//	│ 63 │ 62       53 │ 52            22 │ 21       12 │ 11           0 │
//	│sign│ PartitionID │   Timestamp      │  Sequence   │   Reserved     │
//	│ =0 │ (10 bits)   │   (31 bits)      │  (10 bits)  │   (12 bits)    │
//	└────┴─────────────┴──────────────────┴─────────────┴────────────────┘
//
// bit 63 恒为 0（符号位保护）：正 key = 真实事件，负 key 保留给投影内的
// 压缩摘要引用（rolling summaryRef）。partitionIDMask=0x3FF（10 位，0-1023）。
// Timestamp: seconds since snowflakeEpoch (~68 year range).
// Sequence: per-second counter (0-1023), sub-second uniqueness.
```

> **历史教训**：早期 mask 为 11 位（0x7FF）时 partition≥1024 会触及符号位产生负 key，
> 导致全部 `EventKey>0` 守卫失效（plan 全链路失明）。现 10 位 mask + 回归测试
> `TestSnowflakeEventKey_AlwaysPositive` 锁定。

**字符串形态**：EventKey 对 LLM/工具的展示与入参统一为 **16 进制**（`event.FormatEventKey/ParseEventKey`，负号保留给摘要引用），存储层仍为 int64。

**生成函数**（`memory/types.go`）：

```go
func NewSnowflakeEventKey(partitionID int, nowMs int64) int64 {
    if nowMs <= 0 {
        nowMs = time.Now().UnixMilli()  // 0 = 使用当前时间
    }
    ts := nowMs/1000 - snowflakeEpoch

    // 内部互斥锁保护的 per-partition Sequence 计数器
    snowflakeSeqMu.Lock()
    if ts == snowflakeSeqLast[partitionID] {
        snowflakeSeqCnt[partitionID]++
    } else {
        snowflakeSeqCnt[partitionID] = 0
        snowflakeSeqLast[partitionID] = ts
    }
    seq := snowflakeSeqCnt[partitionID]
    snowflakeSeqMu.Unlock()

    return (int64(partitionID&partitionIDMask) << partitionIDShift) |
        ((ts & timestampMask) << timestampShift) |
        (int64(seq&sequenceMask) << sequenceShift)
}
```

**解析函数**：

```go
func PartitionIDFromEventKey(key int64) int    // 提取 PartitionID
func TimestampFromEventKey(key int64) int64    // 提取时间戳（秒）
func SequenceFromEventKey(key int64) int       // 提取序列号
```

**特点**：
- **内含分区信息**：从 EventKey 可直接反推 PartitionID，无需额外索引
- **时间有序**：高位为时间戳，按 time.Unix 单调递增
- **全局唯一**：内部 mutex 保护的 per-partition Sequence 计数器保证同秒内唯一
- **零值语义**：`0` 表示无前驱（RelationStore 中 parent=0 表示根事件）
- **第二个参数是时间提示**：`nowMs`（毫秒时间戳），传 0 使用当前时间，非零用于测试确定性生成

### 4.2 FullEvent — 完整事件（MemoryStore 的唯一事实来源）

```go
// memory/types.go
type FullEvent struct {
    EventKey     int64                  // Snowflake int64 唯一标识符
    PartitionID  int                    // 存储分区 key（从 AgentName 派生）
    EventType    string                 // 事件类型（external_input / agent_output / ...）
    EventSummary string                 // event_summary 元数据视图（原文视图，非内容总结）
    Timestamp    int64                  // Unix 毫秒时间戳
    Content      string                 // 原始文本内容
    ToolCalls    []model.ToolCall       // 工具调用列表
    ToolID       string                 // 工具结果事件所应答的 tool_call id（D3 原生配对契约，跨存储→解析保持配对）
    ToolResults  map[string]interface{} // 工具执行结果
    Metadata     map[string]string      // 额外元数据（如 source_keys/content_hash 固化物溯源）
    Response     *model.Response        // LLM 响应快照（可选）
}
```

**用途**：MemoryStore 中存储的完整事件数据，永不修改（immutable）。可通过 `EventKey` 精确检索。`Response` 字段保存 LLM 响应快照，供 Trajectory 采集等下游模块使用。

### 4.3 EventReference — 轻量引用（Session 中的 LLM 上下文）

```go
// memory/types.go
type EventReference struct {
    EventKey     int64  `json:"event_key"`              // Snowflake int64 指向 MemoryStore 的 key
    PartitionID  int    `json:"partition_id,omitempty"` // 存储分区 key
    EventType    string `json:"event_type"`             // 事件类型
    EventSummary string `json:"event_summary"`          // event_summary 视图（渲染素材）⭐
    Timestamp    int64  `json:"timestamp"`              // 时间戳
    Role         string `json:"role,omitempty"`         // 原始消息 role（时间线渲染的 role 归属依据）
}
```

**用途**：
- Session 侧仅保存轻量引用，不保存完整事件详情（**信息隔离设计**）
- `EventSummary` 字段直接进入 LLM 消息上下文，供 LLM 理解历史
- 通过 `EventKey` 可随时从 MemoryStore 拉取完整详情（AgentToolWrapper / RecallAgent 机制）

### 4.4 FullEvent 与 EventReference 的关系

```mermaid
graph LR
    FullEvent["FullEvent\n(完整数据)"]
    EventReference["EventReference\n(轻量引用)"]
    Memory["MemoryStore\n(map[PartitionID]map[EventKey]FullEvent)"]
    Session["Session.Events\n(EventReference[])"]
    LLM["LLM\n上下文"]

    FullEvent -->|拆分| EventReference
    FullEvent --> Memory
    EventReference --> Session
    Session --> LLM
    Memory -.->|GetEvent key| FullEvent

    style FullEvent fill:#fff3e0,stroke:#ef6c00,stroke-width:2px
    style EventReference fill:#e8f5e9,stroke:#2e7d32,stroke-width:2px
    style Memory fill:#e1f5ff,stroke:#0277bd
    style Session fill:#f3e5f5,stroke:#7b1fa2
```

| 字段 | FullEvent | EventReference |
|------|-----------|---------------|
| `EventKey` | ✅ int64 | ✅ int64 |
| `PartitionID` | ✅ int | ✅ int |
| _(ParentKey 已移除)_ | 因果关系由 `RelationStore` 维护 | — |
| `EventType` | ✅ | ✅ |
| `EventSummary` | ✅ | ✅ |
| `Role` | —（经 Response 推断） | ✅（渲染 role 归属） |
| `Content` | ✅（原文） | ❌ |
| `ToolCalls` / `ToolID` | ✅（原生配对契约） | ❌ |
| `Response` | ✅ | ❌ |

**关键区别**：Session 中的 `EventReference` 不包含 `Content`、`ToolCalls` 和 `Response`，LLM 看到的只是 `EventSummary`。完整数据通过 AgentToolWrapper（event_key 解析）或 RecallAgent（跨 Session 检索）按需从 MemoryStore 拉取。

---

## 五、因果链机制

### 5.1 RelationStore 因果链语义

ParentKey 已从 FullEvent 结构体中移除。因果关系由独立的 `RelationStore` 维护。

`InMemoryStore` 和 `FileSegmentStore` 都直接嵌入 `RelationStore`，同时实现 `RelationStoreProvider` 接口。调用方式有两种：

```go
// 方式一：直接调用（InMemoryStore/FileSegmentStore 直接实现这些方法）
memStore.SetParent(eventKey, parentKey)
parent := memStore.GetParent(eventKey)
children := memStore.GetChildren(eventKey)

// 方式二：通过 RelationStoreProvider 接口（兼容其他实现）
if rsp, ok := memStore.(memory.RelationStoreProvider); ok {
    rsp.RelationStore().SetParent(eventKey, parentKey)
    parent := rsp.RelationStore().GetParent(eventKey)
    children := rsp.RelationStore().GetChildren(eventKey)
}
```

因果链效果（通过独立的 RelationStore 管理 Parent-Child 关系；示例为 int64 存储形态，对 LLM 展示时统一 hex）：

```
1777198738547555000 (Event 1)
  RelationStore: parent=0  (无前驱)

1777198739574803000 (Event 2)
  RelationStore: parent=1777198738547555000  → 父 = Event 1

1777198739760667000 (Event 3)
  RelationStore: parent=1777198739574803000  → 父 = Event 2
```

### 5.2 因果链的作用

| 能力 | 说明 |
|------|------|
| **因果回溯** | 从当前事件沿 `RelationStore.GetParent()` 回溯历史事件 |
| **分支追踪** | 支持多分支因果（通过 `RelationStore.GetChildren()`） |
| **压缩通知** | 压缩通知中可引用被丢弃的因果链 |
| **RecallAgent** | 按因果顺序展示检索结果 |

### 5.3 因果链与压缩的关系

```
FullEvent 存储因果链 → Session.EventReference 不含因果链
    ↓                                    ↓
压缩时因果链保留在 MemoryStore    LLM 视图通过 SmartCompress 处理
    ↓
RecallAgent 可沿因果链回溯原始事件
```

**关键**：压缩只修改发给 LLM 的消息视图，不修改 MemoryStore 和 RelationStore。因果关系在整个生命周期中保持不变。

---

## 六、MemoryStore 接口

### 6.1 接口定义

```go
// memory/types.go
type MemoryStore interface {
    // === 写操作 ===
    StoreEvent(key int64, event FullEvent) error
    StoreEvents(events map[int64]FullEvent) error

    // === 读操作 ===
    GetEvent(key int64) (*FullEvent, error)
    GetEvents(keys []int64) ([]FullEvent, error)
    QueryEvents(query QueryOptions) ([]EventReference, error)

    // === 向量搜索 ===
    // 当前实现返回 ErrVectorSearchNotSupported；可扩展接入向量数据库
    SearchByEmbedding(query []float32, topK int) ([]EventReference, error)
    StoreEventWithEmbedding(key int64, event FullEvent, embedding []float32) error
    SupportsVectorSearch() bool

    // === 管理操作 ===
    DeleteEvent(key int64) error
    GetStats() StoreStats
}

// ErrVectorSearchNotSupported — 向量搜索不支持时返回此错误（memory/types.go）
var ErrVectorSearchNotSupported = fmt.Errorf("vector search not supported")
```

### 6.1.1 RelationStoreProvider — 因果关系接口

```go
// memory/types.go
type RelationStoreProvider interface {
    RelationStore() RelationStore
}

// 编译时接口实现检查
var (
    _ RelationStoreProvider = (*InMemoryStore)(nil)
    _ RelationStoreProvider = (*FileSegmentStore)(nil)
)
```

**使用方式**：

```go
// 写入因果关系（MemoryPlugin.OnEvent 中）
if parentKey != 0 {
    memStore.SetParent(eventKey, parentKey)
}

// 查询因果关系
parent := memStore.GetParent(eventKey)
children := memStore.GetChildren(eventKey)
```

`MemoryPlugin.OnEvent` 实际使用 `SetParent`（通过 `RelationStoreProvider` 接口做类型断言后调用）。

**设计原则**：内容与关系分离。`FullEvent` 存储不可变的事件内容，`RelationStore` 维护可变的因果关系。`InMemoryStore` 和 `FileSegmentStore` 直接嵌入 `RelationStore`；第三方实现可通过 `RelationStoreProvider` 接口暴露因果能力。

### 6.2 QueryOptions — 查询过滤

```go
// memory/types.go
type QueryOptions struct {
    PartitionID  int      // 单个分区过滤（0 = 不过滤）
    PartitionIDs []int    // 多分区过滤（优先级高于 PartitionID）
    EventTypes   []string // 按事件类型过滤（空 = 全部）
    StartTime    int64    // 时间范围起始（毫秒，0 = 无限制）
    EndTime      int64    // 时间范围结束（毫秒，0 = 无限制）
    Limit        int      // 最大返回数量（0 = 无限制）
    Offset       int      // 分页偏移
    OrderBy      string   // "timestamp_asc" 或 "timestamp_desc"
    Keyword      string   // 按关键词过滤 EventSummary 或 Content（大小写不敏感，空 = 不过滤）
}
```

**注意**：`QueryEvents` 始终返回 `[]EventReference`（轻量），不返回完整 `FullEvent`，避免大量 IO 开销。

### 6.3 StoreStats — 存储统计

```go
// memory/types.go
type StoreStats struct {
    TotalEvents int   // 事件总数
    StorageSize int64 // 存储大小（字节）
    DataDir     string // 数据目录（InMemory = ":memory:"）
}
```

---

## 七、InMemoryStore 实现

### 7.1 数据结构

```go
// memory/in_memory_store.go
type InMemoryStore struct {
    mu     sync.RWMutex
    events map[int]map[int64]FullEvent  // [partitionID][eventKey]
    rel    RelationStore                // 嵌入的因果链存储
}
```

### 7.2 存储结构

```
InMemoryStore
  └── events: map[int]map[int64]FullEvent
        ├── PartitionID 0
        │     ├── 1777198738547555000 → FullEvent{...}
        │     └── 1777198739574803000 → FullEvent{...}
        └── PartitionID 1
              └── 1777198739760667000 → FullEvent{...}
```

### 7.3 特性总结

| 特性 | 说明 |
|------|------|
| 数据结构 | Go `map[int]map[int64]FullEvent`，按 PartitionID 分区保存在内存 |
| 持久化 | **无**（进程退出即丢失） |
| 适用场景 | 测试、短期原型、单进程开发 |
| 读写性能 | O(1) 读写，无 IO 开销 |
| 并发安全 | `sync.RWMutex`（读多写少优化） |
| `GetStats()` | `DataDir = ":memory:"`，`StorageSize` 不统计 |
| 向量搜索 | **空实现**：返回 `ErrVectorSearchNotSupported` |

### 7.4 额外方法

除 `MemoryStore` 接口外，`InMemoryStore` 还提供了扩展方法：

| 方法 | 说明 |
|------|------|
| `AllEvents()` | 返回所有事件（按时间排序），用于测试和调试 |
| `AllEventsByPartition(partitionID)` | 返回指定分区的所有事件 |
| `GetParent(key)` / `GetChildren(key)` / `SetParent(key, parentKey)` | 直接操作嵌入的因果链 |
| `RelationStore()` | 返回内部 RelationStore |
| `SearchByEmbedding()` | **向量搜索（空实现）**：返回 `ErrVectorSearchNotSupported` |
| `StoreEventWithEmbedding()` | **向量存储（空实现）**：忽略 embedding，仅存储事件 |
| `SupportsVectorSearch()` | 返回 `false` |

---

## 八、FileSegmentStore 实现

### 8.1 数据结构（KV + 分段模型）

```go
// memory/segment_store.go
type FileSegmentStore struct {
    kv         KVStore       // RustViking KV 客户端 / 本地 JSON KV（localfile）/ mock
    rel        RelationStore // 因果关系图
    tombstones *TombstoneSet // 墓碑集（死事件过滤）
    cache      *simpleLRU    // FullEvent LRU 缓存（默认 1000 条）
    dataDir    string
    partitions sync.Map      // map[int]*PartitionState（每分区窗口/序列状态）

    // 生命周期组件（可选，Set* 注入）
    lifecycle *LifecycleManager // TTL 墓碑标记（固化物豁免）
    compactor *Compactor        // L1→L2→L3 分层压实
}
```

### 8.2 分层分段模型

**segment 是按时间窗（window_ts）的逻辑分组，不是物理文件**：

| 层 | 语义 | 写入方式 |
|----|------|---------|
| L0（热） | 当前时间窗事件 | 直写 KV |
| L1（温） | 已过窗封存段（sealed） | 封存时更新 SegmentMeta |
| L2/L3（冷） | Compactor 自动压实的更冷段 | `compaction.go` 调度 |

```go
type SegmentMeta struct {
    PartitionID int   // 分区
    WindowTS    int64 // 时间窗起点
    Layer       int   // 1=L1(sealed), 2=L2, 3=L3
    EventCount  int
    MinTime     int64
    MaxTime     int64
    Sealed      bool
}
```

KV key 由 `SegmentEventPrefix(pid, windowTS)` 派生，按分区+时间窗前缀扫描；读路径先走 LRU 缓存，miss 后按 EventKey 内含的 PartitionID+Timestamp 定位窗口。

### 8.3 特性总结

| 特性 | 说明 |
|------|------|
| 存储模型 | KVStore（RustViking / 本地 JSON KV）+ 时间窗分段 + SegmentMeta |
| 持久化 | **有**（进程重启后数据不丢失） |
| 适用场景 | 生产环境（`type: file` 走 RustViking；`type: localfile` 零外部依赖） |
| 读性能 | LRU 缓存 + EventKey 直接定位（分区/窗口内查找） |
| 生命周期 | TombstoneSet + LifecycleManager（TTL，固化物豁免）+ Compactor（分层压实） |
| 并发安全 | 每分区 PartitionState 独立锁 + store 级同步 |
| 向量搜索 | **空实现**：返回 `ErrVectorSearchNotSupported` |

## 九、两种实现的对比

| 维度 | InMemoryStore | FileSegmentStore |
|------|--------------|-------------|
| **数据结构** | Go map | KVStore + 时间窗分段（L0-L3）+ SegmentMeta |
| **持久化** | 无 | 有（RustViking KV 或本地 JSON KV） |
| **进程重启** | 数据丢失 | 数据保留 |
| **适用场景** | 测试、原型 | 生产环境 |
| **读性能** | O(1) | LRU 缓存 + EventKey 定位（分区/窗口） |
| **生命周期** | 无 | Tombstone + TTL（固化物豁免）+ 分层压实 |
| **扩展性** | 受内存限制 | 受磁盘限制；冷段压实控制放大 |
| **向量搜索** | 空实现 | 空实现 |

---

## 十、向量搜索支持

### 10.1 设计背景

`MemoryStore` 接口包含向量搜索方法，为未来接入专业向量数据库（Milvus、Qdrant、pgvector 等）预留接口。当前 `InMemoryStore` 和 `FileSegmentStore` 实现返回 `ErrVectorSearchNotSupported`。

### 10.2 向量搜索方法说明

| 方法 | 说明 | 默认实现 |
|------|------|----------|
| `SearchByEmbedding(query []float32, topK int)` | 使用查询向量搜索相似事件 | 返回 `ErrVectorSearchNotSupported` |
| `StoreEventWithEmbedding(key, event, embedding)` | 存储事件时同时存储向量 | 忽略 embedding，仅存储事件 |
| `SupportsVectorSearch()` | 是否支持向量搜索 | 返回 `false` |

### 10.3 向量搜索空实现

```go
// InMemoryStore 和 FileSegmentStore 的向量搜索均为空实现

// SearchByEmbedding 总是返回错误
func (s *InMemoryStore) SearchByEmbedding(query []float32, topK int) ([]EventReference, error) {
    return nil, ErrVectorSearchNotSupported
}

// StoreEventWithEmbedding 忽略 embedding 参数
func (s *InMemoryStore) StoreEventWithEmbedding(key int64, event FullEvent, embedding []float32) error {
    return s.StoreEvent(key, event)
}

// SupportsVectorSearch 返回 false
func (s *InMemoryStore) SupportsVectorSearch() bool {
    return false
}
```

### 10.4 扩展方向

接入向量数据库时，实现 `SearchByEmbedding` 和 `StoreEventWithEmbedding` 方法，同时将 `SupportsVectorSearch()` 改为返回 `true`。

---

## 十一、与其他模块的关系

### 11.1 依赖关系

```
tagent/memory（存储层）
    ↑
    │  提供 FullEvent 存储和检索
    │
tagent/plugin
    └── MemoryPlugin → StoreEvent / GetEvent

tagent/agent
    ├── AgentToolWrapper → parentStore.GetEvent (核心高频读取)
    └── SmartCompress（不直接依赖，但因果链信息来自 MemoryStore）

tagent/tool
    ├── memory_recall（主 agent 直持纯函数：items 票据精确回补 / query 关键词检索）
    ├── RecallAgent → recall_query / recall_get / recall_recent / recall_trace（复杂检索/多跳编排）
    └── KnowledgeAgent → memory_query（上下文感知搜索）

tagent (root)
    └── tagent.New() 接线时注入 parentMemStore
```

### 11.2 MemoryPlugin 是主要写入方

`MemoryPlugin.OnEvent` 每次事件都会调用 `memStore.StoreEvent`：

```go
// plugin/memory_plugin.go
if p.memStore != nil {
    if err := p.memStore.StoreEvent(eventKey, fullEvent); err != nil {
        log.Errorf("[Memory] store failed key=%d partition=%d: %v", eventKey, partitionID, err)
    } else {
        if parentKey != 0 {
            if rsp, ok := p.memStore.(memory.RelationStoreProvider); ok {
                if err := rsp.RelationStore().SetParent(eventKey, parentKey); err != nil {
                    log.Errorf("[Memory] set parent failed key=%d parent=%d: %v", eventKey, parentKey, err)
                }
            }
        }
        log.Debugf("[Memory] stored key=%d partition=%d type=%s summary_len=%d",
            eventKey, partitionID, eventType, len(eventSummary))
    }
}
```

### 11.3 MemoryStore 的多方读取模式

MemoryStore 的读取方按频率和场景分层：

| 读取方 | 频率 | 场景 |
|--------|------|------|
| **AgentToolWrapper** | 🔥 最高频 | 顶层 LLM 筛选 `event_keys` → 传给子 tool → Wrapper 从 `parentStore` 取完整 `FullEvent` → 注入子 Agent 作为上下文 |
| **memory_recall** 纯函数工具 | 高频 | 召回标准协议：`items=[{key,hint?}]` 票据精确回补（未命中显式 miss）/ `query` 关键词检索——确定性路径无 LLM 中间层 |
| **RecallAgent** 子工具 | 中频 | 复杂检索/多跳编排：`recall_query`（条件查询）、`recall_get`（按 key 取详情）、`recall_recent`（最近 N 条）、`recall_trace`（因果链回溯） |
| **KnowledgeAgent** 子工具 | 低~中频 | 通过 `memory_query` 从父级 MemoryStore 查历史，辅助技能/MCP 搜索 |
| **直接访问** (`TagentAgent.MemStore()`) | 调试/测试 | 开发阶段手工查事件 |

---

#### AgentToolWrapper — 核心读取路径

**为什么是核心**：顶层 LLM 的上下文只有 `EventReference[]`（轻量摘要），不包含 `Content` 和 `ToolCalls`。当 LLM 需要子 Agent 处理某段历史时，它筛选出相关 `event_keys` 作为工具参数传递。`AgentToolWrapper.Call()` 拦截调用，通过 `parentStore.GetEvent(key)` 逐个取出完整 `FullEvent`，序列化为 `RuntimeState["external_context"]` 后通过 `agent.Run()` 传递给子 Agent。

```mermaid
sequenceDiagram
    participant LLM as 顶层 LLM
    participant ATW as AgentToolWrapper
    participant MS as parentStore (MemoryStore)
    participant SA as 子 Agent (RecallAgent / KnowledgeAgent)

    Note over LLM: context 中只有 EventReference[]
    LLM->>ATW: tool_calls: recall({request: "分析部署日志", event_keys: [E1,E3,E5]})
    ATW->>MS: parentStore.GetEvent(E1)
    ATW->>MS: parentStore.GetEvent(E3)
    ATW->>MS: parentStore.GetEvent(E5)
    MS-->>ATW: FullEvent (含 Content, ToolCalls)
    ATW->>ATW: 序列化为 RuntimeState["external_context"]
    ATW->>SA: agent.Run(invocation with RuntimeState)
    Note over SA: TagentAgent.Run 反序列化后注入 external context
    SA-->>ATW: event stream
    ATW-->>LLM: Tool Result
```

**设计要点**：LLM 只传递 `int64` 数字 key，**实际事件内容在服务端解析**，既保证了上下文完整性，又不让 LLM 突破信息隔离边界（LLM 从未见到被压缩掉的 `Content`/`ToolCalls`）。

---

#### RecallAgent — 超越当前会话的深层检索

`RecallAgent` 的独特价值不在"读 MemoryStore"（那是 AgentToolWrapper 的职责），而在**跨 Session 的语义记忆召回**。

顶层 LLM 的 context 已包含当前 Session 的 `EventReference[]` 流。当需要的信息**超出当前 context 窗口**或**跨越多个历史 Session** 时，LLM 调用 RecallAgent。RecallAgent 的内部 LLM React 循环负责：

1. **理解查询意图** — 将自然语言转为结构化检索条件
2. **多工具协作** — `recall_query` 检索 → `recall_get` 按需取详情（含父事件） → `recall_recent` 补充最新事件 → `recall_trace` 因果链回溯
3. **跨事件综合** — 将零散历史事件综合为连贯的记忆摘要

其子工具 `recall_get` 通过 `EventKey` 从 MemoryStore 拉取完整事件详情：

```go
// tool/recall_subtools.go — NewRecallGetTool
func NewRecallGetTool(accessor MemoryStoreAccessor) tool.Tool {
    return function.NewFunctionTool(
        func(ctx context.Context, args recallGetArgs) (recallGetResult, error) {
            // args.Key 为 canonical hex 字符串（与 [evt_...] 前缀一致）
            key, err := event.ParseEventKey(args.Key)
            if err != nil || key == 0 {
                return recallGetResult{}, fmt.Errorf("event key is required (hex string)")
            }

            evt, err := accessor.GetEvent(key)
            if err != nil {
                return recallGetResult{}, fmt.Errorf("event not found: %w", err)
            }

            result := recallGetResult{
                Key:       evt.EventKey,

                Type:      evt.EventType,
                Summary:   evt.EventSummary,
                Content:   evt.Content,
                Time:      formatTimestamp(evt.Timestamp),
            }

            // Optionally include parent event summary
            // 通过 RelationStore 获取父事件
            if args.IncludeParent {
                var parentKey int64
                if rsp, ok := accessor.(memory.RelationStoreProvider); ok {
                    parentKey = rsp.RelationStore().GetParent(evt.EventKey)
                }
                if parentKey != 0 {
                    if parent, err := accessor.GetEvent(parentKey); err == nil && parent != nil {
                    result.Parent = &parentEventInfo{...}
                }
            }

            return result, nil
        },
        function.WithName("recall_get"),
        function.WithDescription("Get full details of a specific event by its key. Set include_parent=true to also include the parent event summary."),
    )
}
```

**RecallAgent 子工具**（通过 `RegisterSubTools()` 注册为 plain tool）：
- `recall_query`：按查询条件检索事件列表，支持时间范围过滤，自动注入 `ReadPartitionIDs`
- `recall_get`：根据 event_key 获取完整事件详情，支持 `include_parent` 参数自动包含父事件摘要
- `recall_recent`：快速获取最近的 N 条事件，支持时间范围过滤，自动注入 `ReadPartitionIDs`
- `recall_trace`：沿 RelationStore 因果链回溯，从指定事件追溯最多 20 步历史

> **自动注入机制**：`recall_query` 和 `recall_recent` 的 factory 从 `PlainToolFactoryConfig.ReadPartitionIDs` 获取分区列表，handler 内部自动注入到 `QueryOptions.PartitionIDs`。LLM 调用时只需传语义参数，无需感知分区号。详见 [tool-architecture.md](../tool/tool-architecture.md) §六。

### 11.4 完整数据流

MemoryStore 有两条主要读取路径：**AgentToolWrapper**（核心高频，LLM 选 key → Wrapper 解析）和 **RecallAgent**（跨 Session 深层检索）。

#### 路径一：AgentToolWrapper（核心高频）

```mermaid
sequenceDiagram
    participant LLM as 顶层 LLMAgent
    participant ATW as AgentToolWrapper
    participant MS as parentStore (MemoryStore)
    participant SA as 子 Agent (RecallAgent / KnowledgeAgent)

    Note over LLM: context 中只有 EventReference[]
    LLM->>LLM: 筛选相关 event_keys
    LLM->>ATW: tool_calls: recall({request, event_keys: [E1,E3,E5]})
    ATW->>MS: parentStore.GetEvent(E1)
    ATW->>MS: parentStore.GetEvent(E3)
    ATW->>MS: parentStore.GetEvent(E5)
    MS-->>ATW: FullEvent (含 Content, ToolCalls)
    ATW->>ATW: 序列化为 RuntimeState["external_context"]
    ATW->>SA: agent.Run(invocation with RuntimeState)
    Note over SA: 子 Agent 的 context<br/>包含完整事件上下文
    SA->>SA: runEventLoop + runner.Run
    SA-->>ATW: event stream
    ATW-->>LLM: Tool Result
```

#### 路径二：RecallAgent 跨 Session 深层检索

```mermaid
sequenceDiagram
    participant MP as MemoryPlugin.OnEvent
    participant MS as MemoryStore
    participant RA as RecallAgent
    participant LLM as LLM

    MP->>MS: StoreEvent(eventKey, FullEvent)
    Note over MS: FullEvent 持久化<br/>RelationStore.SetParent 建立因果链

    RA->>MS: recall_query → QueryEvents(query)
    MS-->>RA: []EventReference
    Note over RA: 内部 LLM React 循环<br/>综合检索结果

    RA->>MS: recall_get(eventKey) → GetEvent(key)
    MS-->>RA: FullEvent（含 Content, ToolCalls）
    RA-->>LLM: 展示综合后的记忆摘要
```

---

## 十二、PartitionID 派生

### 12.1 PartitionIDFromName — 从名称派生稳定分区 ID

```go
// memory/types.go
func PartitionIDFromName(name string) int
```

使用 FNV-1a 哈希将名称（如 AgentName）映射为 **0-1023** 之间的稳定 PartitionID（与雪花键 10 位分区域一致，bit63 恒 0）。相同名称总是产生相同 PartitionID，使用 `sync.Map` 缓存。

### 12.2 NewPartitionID — 无名称时的唯一分区 ID

```go
// memory/types.go
func NewPartitionID() int
```

当没有稳定名称可用时，使用原子计数器生成全局唯一的 PartitionID。

---

## 十三、跨命名空间读权限（ReadNamespaces）

### 13.0 设计背景

RecallAgent 的子工具操作的是自身 MemoryStore，而历史事件由顶层 Agent（如 tagent）写入。当 RecallAgent 需要检索顶层 Agent 的历史事件时，需要跨命名空间的读权限。

**设计方案**：`MemoryConfig.ReadNamespaces` 字段声明本 Agent 可读取的其他 Agent 命名空间。`buildAgent()` 在初始化时将其转换为 `ReadPartitionIDs []int`，通过 `buildPlainToolRef` → `PlainToolFactoryConfig.ReadPartitionIDs` 注入到每个 plain tool factory。

```yaml
# 配置示例
recall:
  memory:
    type: file
    path: .wechat-config/agent-events
    read_namespaces:
      - tagent         # 可读 tagent 的分区
```

**转换链路**：

```
tagent.yaml → ReadNamespaces: ["tagent"]
  → buildAgent() → memory.PartitionIDFromName("tagent") → [144]
  → buildPlainToolRef() → PlainToolFactoryConfig.ReadPartitionIDs: [144]
  → recallQueryFactory(cfg) → handler 内注入 opts.PartitionIDs
  → recallRecentFactory(cfg) → handler 内注入 opts.PartitionIDs
  → LLM 调用 recall_query({query: "部署"}) → 实际查询分区 144
```

### 13.0.1 MemoryStore 实例共享策略

`resolveMemoryStore()`（定义在 `tagent.go`）根据 `MemoryConfig.Type` 选择存储实现：

- `type: memory` / 空：创建 `InMemoryStore`。非空 `path` 按 path 做注册表去重，同 path → 同实例；空 path 每次新建隔离实例。
- `type: file`：创建 `FileSegmentStore`，底层使用 RustViking CLI 作为 KV 存储，并启动生命周期管理（tombstone、lifecycle、compactor）。同 path 会复用已注册的实例。
- `type: localfile`：创建 `FileSegmentStore`，底层使用本地 JSON 文件作为 KV 存储（无外部二进制依赖），并启动生命周期管理。同 path 会复用已注册的实例。

```go
// tagent.go resolveMemoryStore 节选
case "memory", "":
    if mc.Path == "" {
        return memory.NewInMemoryStore(), nil  // 无 path → 隔离
    }
    namedMemMu.Lock()
    defer namedMemMu.Unlock()
    if s, ok := namedMemStores[mc.Path]; ok {
        return s, nil  // 同 path → 同实例
    }
    s := memory.NewInMemoryStore()
    namedMemStores[mc.Path] = s
    return s, nil
case "file":
    // FileSegmentStore + RustViking + lifecycle components
    // ...
case "localfile":
    // FileSegmentStore + LocalFileKV + lifecycle components
    // ...
```

**效果**：

| 配置 | 实例策略 | 数据共享方式 |
|------|---------|------------|
| `type: memory`（无 path） | 每次新建 | 完全隔离 |
| `type: memory, path: "X"` | 同 path → 同实例 | 同一 `map[PartitionID]map[EventKey]FullEvent` |
| `type: file, path: "/X"` | 同 path → 同实例 | RustViking KV + 文件系统 |
| `type: localfile, path: "/X"` | 同 path → 同实例 | 本地 JSON 文件 |

### 13.0.2 path 字段的语义

| 类型 | `path` 的含义 |
|------|-------------|
| `file` | 文件系统目录路径（RustViking 数据目录） |
| `localfile` | 文件系统目录路径（本地 JSON 文件目录） |
| `memory` | 逻辑存储标识符——同 type + 同 path → 单例 |

> `path` 在三种类型下均表示"存储定位符"。`file`/`localfile` 通过文件系统 + 注册表保证同路径→同存储；`memory` 通过注册表显式保证。

### 13.1 两条读路径与 opt-in 隔离语义

记忆层对外暴露**两条语义不同的读路径**，二者分工实现了「Agent 间隔离」与「顶层可跨界还原」的共存：

| 读路径 | 入口 | 分区作用域 | 是否受 `read_namespaces` 约束 |
|--------|------|-----------|------------------------------|
| **按条件查询** | `QueryEvents`（RecallAgent 的 `recall_query`/`recall_recent`，见 §13.0） | `opts.PartitionIDs` 指定的分区集合 | ✅ 是——通过注入 `ReadPartitionIDs` 限定 |
| **按 Key 直读** | `GetEvent(key)`（AgentToolWrapper/`recall_get`，见 §11.3） | 单个 Key 精确定位（Key 内含 PartitionID） | ❌ 否——按 Key 跨任意分区还原 |

**设计意图**：
- **发现（查询）走隔离**：子 Agent 不应通过盲扫发现其他 Agent 的历史，故 `QueryEvents` 受 `read_namespaces` 限定分区。
- **还原（按 Key）走全库**：顶层 Agent 已通过自身上下文/召回持有 `event_key`，需要精确还原完整 `FullEvent` 喂给子 Agent，此时按 Key 直读不受分区限制。这正是「子写、顶读、顶编排」模式的技术基础。

**⚠️ 隔离是 opt-in，非 default-deny**：`resolvePartitions` 在查询未显式指定分区时**回退为全部分区**：

```go
// in_memory_store.go / segment_store.go resolvePartitions 语义
func resolvePartitions(query QueryOptions) []int {
    if len(query.PartitionIDs) > 0 { return query.PartitionIDs } // 指定则限定
    if query.PartitionID > 0 { return []int{query.PartitionID} }
    return allPartitions // 未指定 → 全部分区（无隔离）
}
```

因此隔离边界**依赖 RecallAgent 显式配置 `read_namespaces`** 才成立。为挂载了 recall 类工具的 Agent 新增配置时，若遗漏 `read_namespaces`，该 Agent 的条件查询将扫描全库——这是新增 Agent 时需重点核对的一项。

---

## 十四、关键设计决策

### 14.1 为什么不直接在 Session 中存储 FullEvent？

| 需求 | Session 能满足吗 | MemoryStore 的优势 |
|------|----------------|------------------|
| 因果链 | Session.Events 是线性列表 | `RelationStore.SetParent` 构建有向因果图 |
| 精确 FullEvent 检索 | 需遍历所有事件 | `GetEvent(key)` O(1) |
| 按类型/时间查询 | 框架支持有限 | `QueryEvents` 多维度过滤 |
| 跨 Session 检索 | 单 Session 范围 | 可跨 Session 按 UserID 检索 |
| 工具调用原始数据 | 有 | `ToolCalls` 不随 LLM 视图变化 |

### 14.2 为什么 QueryEvents 返回 EventReference 而不是 FullEvent？

**性能考量**：MemoryStore 可能存储大量事件。若每次查询都返回完整 `FullEvent`（含 `Content`、`ToolCalls`、`Response`），会造成大量 IO 开销和内存占用。

`EventReference` 仅包含 4 个字段（key、type、summary、timestamp），是 `FullEvent` 的轻量子集。调用方按需通过 `GetEvent(key)` 获取完整数据。

### 14.3 为什么 FileSegmentStore 采用 KV + 时间窗分段（而非每事件一个文件）？

**写放大与文件数控制**：长期运行 Agent 事件量大，每事件一文件会产生海量小文件（inode 压力、目录遍历 O(N)）；KV + 分段把同窗事件聚在前缀区间内，按前缀扫描。

**分层生命周期**：热（L0 直写）/温（L1 封存）/冷（L2/L3 压实）配合 TTL 墓碑与固化物豁免，"原文可忘、固化物长存"在存储层有对应机制。

**EventKey 自寻址**：雪花键内含 PartitionID+Timestamp，可直接定位分区与时间窗，无需全局索引。

## 十五、记忆策展（unified-memory-curation）

### 三原语与固化级联

记忆只有三个原语：**store**（事件入库，不可变）、**compress**（总结+自然遗忘，同一动作两面）、**recall**（回忆）。没有独立"总结引擎"——内容级总结只在压缩固化时刻发生。

```mermaid
graph LR
    A["事件原文<br/>(第0层,唯一全文接触点)"] -->|L3 归档,同段跨轮不重摘| B["段摘要<br/>context_compress_summary"]
    B -->|工程化提取,零 LLM| C["卡片行<br/>(边界事件骨架)"]
    C -->|超 card_max_chars,LLM 整理| D["浓缩卡片<br/>(保任务骨架+key引用)"]
    B -.SetParent 挂 RelationStore.-> A
```

素材律：第 N 层摘要只消费第 N-1 层固化物，成本 O(新增段) 与历史总量无关。固化物豁免 TTL 与容量淘汰（`getEffectiveTTL` 负值语义 + evict 跳过）：原文可忘、固化物长存。

### 卡片序列（压缩历史的唯一表示）

被压缩历史住在滚动 summaryRef（负 key `context_compress` 引用）里：`[Compacted N] + 卡片行序列 + recent keys`。卡片行由 `extractCardLine` 从边界事件（external_input/agent_output）工程化提取，冥想产出带 ★ 高亮；跨轮由 `buildRetainedRefs` 吸收合并（计数累计/卡片继承/时间下界继承）；超限由 `curateCards` LLM 整理（输出单行化防解析丢行），无模型则最旧行沉底为 `(earlier n items)` 计数。解析正则行锚定（卡片行含用户可控文本，防注入）。

### recall 协议（索引卡=召回票据）

| 输入形态 | 路径 | 特性 |
|---|---|---|
| `items=[{key,hint?}]` | 批量 `GetEvent` 精确回补 | 纯函数零幻觉；未命中显式 `miss`；hint 回显对账 |
| `query`(+filters) | `QueryOptions` 关键词检索 | 检索层可独立演进（→向量），入口协议不变 |

`memory_recall` 为主 agent 直持纯函数工具（确定性路径无 LLM 中间层）；RecallAgent 保留给多跳编排（trace 等）。


---

## 已知缺口与演进方向

> 本章主动声明当前设计尚未闭合的环——供使用者评估适用边界，也供外部分析引用。

| 缺口 | 现状与防线 | 候选方向 |
|------|-----------|---------|
| **压缩老化（摘要丢细节）** | 卡片行沉底为 `(earlier n items)` 计数后，约束/日期类细节只剩 recall 票据可达——依赖模型主动召回。防线：票据永不丢（key 保留）、固化物豁免 TTL、L0 边界事件保原文 | 沉底前抽取"约束型事实"入固化物；对账测试常态化 |
| **向量检索未接入** | `SearchByEmbedding` 接口预留，实现返回 `ErrVectorSearchNotSupported`；语义召回当前仅关键词路径（`QueryOptions.Keyword`） | 接入向量库时 recall 协议入口不变（items/query 分流已隔离检索层） |
| **固化物因果回溯不完整** | L3 归档经 `SetParent` 挂链 + `source_keys` 溯源；但从"任务结果"反查固化物缺 `task.resultRef` 桥 | resultRef 字段 + RelationStore 反向索引 |
| **LocalFileKV 压实成本** | WAL 已把增量写摊平为 O(ops)；压实时刻仍全量 marshal 且在锁内（4MiB WAL 触发一次） | 分片 snapshot 或锁外压实 |
| **历史脏数据** | 旧 11 位 mask 时代的负 key / 超界分区（如 1167）残留于实机存量 | TTL 自然清退；不做主动迁移（读路径已容错） |

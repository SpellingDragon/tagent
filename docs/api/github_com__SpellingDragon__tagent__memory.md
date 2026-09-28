package memory // import "github.com/SpellingDragon/tagent/memory"

Package memory 是 tagent 的事实存储层：以 FullEvent 为唯一记录形态、按分区命名空间
隔离、以键格式为单点约定，并在此之上提供检索（关键词／语义）、分层与压实、TTL 与 物理遗忘。后端抽象（KV
底座、检索引擎、嵌入器）都遵循"契约居核心包、实现居子包"。 事件类型常量的单点定义在事件包（event.Type*），本包不重复登记。

契约: docs/wiki/memory/memory-architecture.md#overview

CONSTANTS

const (
	// MetaReceiptKeys 等是 consolidation 事件的 Metadata 收据 schema 键（键名即存储字段）。
	MetaReceiptKeys = "receipt_keys"
	// MetaReceiptFingerprint 服务器侧算得的源内容指纹；客户端提交的同名值不参与校验。
	MetaReceiptFingerprint = "receipt_fingerprint"
	// MetaConsolidationKind 巩固产物种类：蒸馏或经验总结。
	MetaConsolidationKind = "consolidation_kind"
	// MetaConsolidationTrigger 触发来源，供审计回溯该产物为何产生。
	MetaConsolidationTrigger = "consolidation_trigger"
	// MetaSourceCount 声明的源事件条数，与收据实际条数互相校验。
	MetaSourceCount = "source_count"
)
const (

	// DefaultWindowSize Default window size: 1 hour in seconds
	DefaultWindowSize int64 = 3600
)

VARIABLES

var (
	// ErrKeyNotFound is wrapped by KV backends when a key genuinely does not
	// exist. Any other error from a KVGet/KVScan is storage I/O and must
	// propagate, never be treated as a miss.
	ErrKeyNotFound = errors.New("kv key not found")

	// ErrDuplicateEventKey is returned by StoreEvent when the EventKey is
	// already committed. An EventKey IS the event's identity (collision
	// guard ): overwriting is refused, never silently applied.
	ErrDuplicateEventKey = errors.New("event key already exists")

	// ErrEventForgotten is returned by the internal replay path when the EventKey
	// is under a legal tombstone: the fact was deliberately deleted, so a replay
	// MUST NOT resurrect it . It is distinct from a duplicate/conflict (a same
	// key with wrong content) and from I/O — callers should hold the recovery
	// material and not ack, exactly as they would for a conflict.
	ErrEventForgotten = errors.New("event was legally forgotten (tombstoned); replay refused")

	// ErrEventProtected 由 DeleteEvent 在键仍受保留租约保护时返回：共享资源的恢复归属方还需要
	// 持久原文来完成 ack/回放未确认的信封或落盘项，因此显式删除被拒且**不销毁记录**（无损搬迁
	// 仍允许，只有销毁被拒）。调用方须在租约释放后重试。
	ErrEventProtected = errors.New("event is retained by an unacked-recovery lease; delete refused")
)
    ErrKeyNotFound 等类型化存储契约错误：调用方必须能区分"键确实不存在"与"存储 I/O 失败"。把两者塌缩成一个，
    等于把一次故障伪装成空召回、把一次恢复失败伪装成"这条链本来就没有"——它们静默产生错答案 而不是响亮报错。四类错误各自的处理义务见文档。

    契约: docs/wiki/memory/memory-architecture.md#typed-errors

var ErrFeedbackEdgePartial = errors.New("feedback-edge-partial")
    ErrFeedbackEdgePartial 标记「事件已落库但因果边失败」：反馈本体成功，
    调用方应返回成功+warning（201），不得按失败重试（会写重复 feedback）。

var ErrFeedbackParentNotFound = errors.New("feedback-parent-not-found")
    ErrFeedbackParentNotFound 标记 parent 不存在：调用方应视为确定性失败 （404），不得重试。

var (
	// ErrVectorSearchNotSupported 表示后端不具备向量能力：调用方必须退回关键词路，
	// 而不是把"不支持"当成"没有结果"。
	ErrVectorSearchNotSupported = fmt.Errorf("vector search not supported")
)
var LowValueEventTypes = event.LowValueTypes()
    LowValueEventTypes are event types whose Content/ToolCalls can be
    discarded in L3. Derived from the event registry (single source of truth):
    thinking_plan, context_compress.


FUNCTIONS

func BindFeedback(store MemoryStore, parentKey int64, payload FeedbackPayload) (int64, error)
    BindFeedback 写入一条绑定到 parentKey 的 feedback 事件并建立因果边。 parent
    必须已存在（否则显式错误——反馈不允许指向幻觉产出）；分区继承 parent。 SetParent 经 RelationStoreProvider
    可选面（store 未暴露关系存储时跳过因果边 并在返回错误中说明，事件本身仍写入——反馈优先落库，关系可后补）。

func BuildConsolidationEvent(store MemoryStore, partitionID int, content, kind, trigger string, sourceKeys []int64, minSources int) (FullEvent, ReceiptVerdict, error)
    BuildConsolidationEvent 服务端构造巩固事件：拉取源事件、算指纹、封装收据 Metadata。 指纹由本函数（服务端）计算，LLM
    无法伪造。返回待存储的 FullEvent（正 key、TTL 豁免 经注册表声明）与构造时的验证裁决。调用方（memory_consolidate
    工具）负责 StoreEvent。

func ComputeReceiptFingerprint(events []FullEvent) string
    ComputeReceiptFingerprint 服务端指纹：对排序后的 (key, type, content) 逐条滚动 SHA1。 覆盖
    content 使「源事件被篡改/重写入」可检出（防漂移）。确定性：同输入同指纹。

func EventKeyStr(pid int, windowTS int64, seq int) string
    EventKeyStr builds the RocksDB key for storing event content. Format:
    {pid}:evt:{window_ts}:{seq}

func EventPrefix(pid int) string
    EventPrefix returns the prefix for all event keys in a partition.

func IndexKeyStr(pid int, eventKey int64) string
    IndexKeyStr builds the RocksDB key for the segment offset index. Format:
    {pid}:idx:{event_key}

func IsDuplicateEventKey(err error) bool
    IsDuplicateEventKey reports whether err is the typed duplicate-key error (
    Major 1: the replay path treats duplicates as idempotent success).

func IsEventForgotten(err error) bool
    IsEventForgotten reports whether err is the typed tombstone/forgotten error
    — a legal deletion a replay must not resurrect .

func IsEventProtected(err error) bool
    IsEventProtected 判断 err 是否为类型化的保留租约拒删——仍被未确认恢复所保留的原文上的显式 删除请求，需在租约释放后重试。

func KeyNotFound(key string, err error) error
    KeyNotFound wraps err (when non-nil) into a typed missing error carrying the
    key context. KV backends use it so callers can errors.Is.

func MetaKeyStr(pid int, windowTS int64) string
    MetaKeyStr builds the RocksDB key for segment metadata. Format:
    {pid}:meta:{window_ts}

func MetaPrefix(pid int) string
    MetaPrefix returns the prefix for all meta keys in a partition. Scanning
    this prefix returns all segment metadata entries.

func NewPartitionID() int
    NewPartitionID 在无稳定名字时由进程内原子计数器推导分区号。乘数为奇数，故在 10 位空间上是
    双射：连续计数得到互不相同的分区号，周期恰为 1024——同一进程内超过 1024 个分区才会首次撞号。

func NewSnowflakeEventKey(partitionID int, nowMs int64) int64
    NewSnowflakeEventKey 生成 Snowflake 式事件键。nowMs 传 0 表示用真实时钟，传值表示由调用方
    驱动时钟（测试语义）——只有真实时钟路径享受时钟回退保护。

func PartitionIDFromEventKey(key int64) int
    PartitionIDFromEventKey 从事件键取出分区号。

func PartitionIDFromName(name string) int
    PartitionIDFromName 由名字确定性地推导分区号（同名恒同值，0-1023）。允许碰撞——分区用于 因果链隔离，不用于唯一性标识。

    契约: docs/wiki/memory/memory-architecture.md#event-key

func PartitionPrefix(pid int) string
    PartitionPrefix returns the prefix for all keys in a partition.

func RaiseSnowflakeFloor(partitionID int, highestIssuedKey int64)
    RaiseSnowflakeFloor 用"磁盘上已发出的最大键"这一持久观测播种分区的单调性守卫：新进程继承
    的是事实链而不是内存计数器，缺此播种则同秒内重启会重发与已提交事实相撞的键，而每次相撞都判
    内容冲突、配合冻结键不得覆盖的规则会永久僵持。守卫单向：只有严格更高的观测值抬升它。

func SegmentEventPrefix(pid int, windowTS int64) string
    SegmentEventPrefix returns the prefix for events within a specific time
    window. Scanning this prefix returns all events in the segment.

func SequenceFromEventKey(key int64) int
    SequenceFromEventKey 从事件键取出同秒序号。

func TimestampFromEventKey(key int64) int64
    TimestampFromEventKey 从事件键取出写入时刻的 Unix 秒（不是语义时间，见事件记录的时间契约）。

func TombstoneKeyStr(pid int, eventKey int64) string
    TombstoneKeyStr builds the RocksDB key for a tombstone marker. Format:
    {pid}:tomb:{event_key}

func TombstonePrefix(pid int) string
    TombstonePrefix returns the prefix for all tombstone keys in a partition.

func WindowTimestamp(tsSec int64, windowSize int64) int64
    WindowTimestamp computes the time window start (epoch seconds) for a
    given timestamp. Aligns timestamp to window boundaries: floor(timestamp /
    windowSize) * windowSize.

func WindowTimestampFromEventKey(eventKey int64, windowSize int64) int64
    WindowTimestampFromEventKey computes the window timestamp from an EventKey's
    embedded timestamp.


TYPES

type CapacityHookProvider interface {
	SetCapacityHook(fn func(eventKey int64, partitionID int, eventType string))
}
    CapacityHookProvider 是可选接口：装饰器在每次 StoreEvent 成功后旁路调用回调（eventKey,
    partitionID, eventType），供巩固容量触发计数。 实现位于子包 memory/engine 的 engineBridge。回调
    MUST 非阻塞、不得失败主链路。

type CompactionConfig struct {
	L1Threshold   int
	L2Threshold   int
	CheckInterval time.Duration
}
    CompactionConfig configures the compactor behavior.

func DefaultCompactionConfig() CompactionConfig
    DefaultCompactionConfig returns the default compaction configuration.

type Compactor struct {
	// Has unexported fields.
}
    Compactor manages background compaction operations.

func NewCompactor(store *FileSegmentStore, kv KVStore, rel RelationStore, tombstone *TombstoneSet, config CompactionConfig) *Compactor
    NewCompactor creates a new Compactor.

func (c *Compactor) CompactL1ToL2(pid int, windowTSs []int64) error
    CompactL1ToL2 把给定窗口集合从 L1 压实到 L2；窗口由调用方按老化策略选出。

func (c *Compactor) CompactL2ToL3(pid int, windowTSs []int64) error
    CompactL2ToL3 compacts L2 daily segments into a single L3 weekly segment.
    In addition to L1→L2 steps, it summarizes low-value events.

func (c *Compactor) CompactOnce()
    CompactOnce runs one synchronous compaction sweep (hourly seal + L1→L2
    + L2→L3) with the CURRENT thresholds. The scheduler self-ticks every
    CheckInterval (default 5min) — harnesses that must not wait call this
    instead; it is the same sweep body, so no second semantic exists.

func (c *Compactor) SetThresholds(l1, l2 int)
    SetThresholds retunes the compaction thresholds at runtime (harness hook —
    resident-remaining-hardening 3.1: a soak subprocess must exercise the real
    compaction path without producing 24 sealed hourly segments). Values <= 0
    are ignored. Atomic against the background scheduler by construction.

func (c *Compactor) Start()
    Start starts the compaction scheduler in a background goroutine.

func (c *Compactor) Stop()
    Stop stops the compaction scheduler gracefully.

type DegradationSink interface {
	// ReportFailure 上报某依赖的一次失败及原始错误。
	ReportFailure(dep string, err error)
	// ReportSuccess 上报某依赖恢复健康。
	ReportSuccess(dep string)
}
    DegradationSink 是上报目标的窄接口：由记忆包定义、以字符串表依赖名，降级状态机经适 配器满足它，故记忆包不依赖可靠性包、不成环。

type Embedder interface {
	// Embed 批量嵌入。实现 MUST 尊重 ctx 取消/超时。
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Dimension 返回向量维度；0 = 未知（尚未探测）。
	Dimension() int
	// ModelID 返回嵌入模型标识（用于索引指纹比对，防换模型后向量混用）。
	ModelID() string
}
    Embedder 是文本向量化抽象：契约居核心包，实现居子包 memory/embedder（mock／zhipu／traced）。
    批量语义要求返回与输入等长、顺序对应；未配置时返回 error 由调用方按"功能关闭"降级。 接入新供应商的步骤与一条已裁决事项（嵌入走 tagent
    侧 HTTP 供应商而非 rustviking CLI）见文档。

    契约: docs/wiki/memory/memory-architecture.md#embedder

type ErrorTrackingStore struct {
	// Has unexported fields.
}
    ErrorTrackingStore 是存储装饰链的最外层：把失败按特征归因到依赖并旁路上报，构成降级状态机 的唯一错误输入源；非侵入透传 inner
    全部方法（含可选接口）。归因矩阵、三类"不算故障"的情况与 恢复证明规则见文档。

    契约: docs/wiki/memory/memory-architecture.md#error-tracking

func NewErrorTrackingStore(inner MemoryStore, sink DegradationSink) *ErrorTrackingStore
    NewErrorTrackingStore 包裹 inner 做错误追踪；sink 为 nil 即纯透传、不上报。

func (s *ErrorTrackingStore) ArmRetention()
    ArmRetention 放行内层首次破坏性扫描的门控。

func (s *ErrorTrackingStore) BeginHold()
    BeginHold 抬起登记屏障：屏障语义由底层租约持有，底层无此能力则静默跳过。

func (s *ErrorTrackingStore) Close() error
    Close 透传内层关闭以回收资源（内层无 Close 则返回 nil）。

func (s *ErrorTrackingStore) DeleteEvent(key int64) error
    DeleteEvent 透传删除；失败按依赖归因上报，成功不报恢复（删除成功不证明写路径）。

func (s *ErrorTrackingStore) EndHold()
    EndHold 放下登记屏障，须与 BeginHold 成对。

func (s *ErrorTrackingStore) GetEvent(key int64) (*FullEvent, error)
    GetEvent 透传单条读取；失败上报归因，成功不上报恢复（读通不代表写依赖已恢复）。

func (s *ErrorTrackingStore) GetEvents(keys []int64) ([]FullEvent, error)
    GetEvents 批量读取，错误处理语义同 GetEvent。

func (s *ErrorTrackingStore) GetStats() StoreStats
    GetStats 纯透传，无错误语义。

func (s *ErrorTrackingStore) KVBackend() KVStore
    KVBackend 透传内层 KV 底座（无则 nil）。

func (s *ErrorTrackingStore) MemSpillLen() int
    MemSpillLen 返回当前待重放的兜底事件数，可作诊断与背压信号。

func (s *ErrorTrackingStore) MemoryEngine() MemoryEngine
    MemoryEngine 透传内层语义引擎（无则 nil）。

func (s *ErrorTrackingStore) ProtectKey(key int64)
    ProtectKey 把保留登记递归透传给内层租约；内层无租约则空操作。

func (s *ErrorTrackingStore) QueryEvents(query QueryOptions) ([]EventReference, error)
    QueryEvents 条件查询，错误处理语义同 GetEvent。

func (s *ErrorTrackingStore) RelationStore() RelationStore
    RelationStore 透传因果关系存储（无则 nil，调用方按"无关系能力"处理）。

func (s *ErrorTrackingStore) ReleaseKey(key int64)
    ReleaseKey 透传保留释放，语义同 ProtectKey。

func (s *ErrorTrackingStore) RemoveVector(eventKey int64)
    RemoveVector 把遗忘联动移除向量透传给内层（无该能力则空操作）。

func (s *ErrorTrackingStore) ReplayEvent(key int64, canonicalFact FullEvent) (ReplayResult, FullEvent, error)
    ReplayEvent 透传内部回放并上报归因。与 StoreEvent 不同：回放失败不做兜底落盘——可靠收件箱 已自持这些事件的
    at-least-once 重试，再落一份会在兜底文件里造出重复条目。

func (s *ErrorTrackingStore) ReplaySpilled() (int, error)
    ReplaySpilled 把兜底事件重放进内层存储（直连内层以防递归再次落盘），返回成功条数。

func (s *ErrorTrackingStore) SearchByEmbedding(query []float32, topK int) ([]EventReference, error)
    SearchByEmbedding 透传语义检索。"不支持向量"是能力声明而非依赖故障，故不上报——否则未配置
    语义检索的部署一调用就把向量依赖打成降级。真失败仍走归因，不无条件算给向量依赖。

func (s *ErrorTrackingStore) SetMemSpill(path string) error
    SetMemSpill 启用写入失败事件的兜底落盘（path 为空即禁用），恢复后经 ReplaySpilled
    重放， 把 at-least-once 语义延伸到存储层。启用时必须把保留租约接进兜底文件，并在放行扫描器前按现存
    条目重建保留集；该步失败一律上抛而非吞错——吞错会造出"热更成功＋悬空遗忘屏障"，使待重放原文 可能被销毁，调用方须据此
    fail-closed、旧实例继续服务。

func (s *ErrorTrackingStore) SetReplayProjection(fn func(FullEvent))
    SetReplayProjection 注册重放后的补投影回调（传 nil 清除）。回调失败或 panic 不影响重放—— 事件不丢优先，投影可后补。

func (s *ErrorTrackingStore) StoreEvent(key int64, event FullEvent) error
    StoreEvent 透传写入并归因错误；写成功即证明三条写路径依赖皆通（见 reportStoreHealthy）。
    公共路径的重复键属调用方契约冲突，原样上抛而不上报失败、不落盘。

func (s *ErrorTrackingStore) StoreEventWithEmbedding(key int64, event FullEvent, embedding []float32) error
    StoreEventWithEmbedding 的归因与兜底语义同 StoreEvent；兜底条目不携带向量，重放走文本路径重新嵌入。

func (s *ErrorTrackingStore) SupportsVectorSearch() bool
    SupportsVectorSearch 纯透传，无错误语义。

func (s *ErrorTrackingStore) WalQuarantined() int64
    WalQuarantined 透传底层 WAL 隔离计数，保证诊断面在最外层仍可达（内层无此能力则报 0）。

type EventReference struct {
	EventKey     int64  `json:"event_key"`
	PartitionID  int    `json:"partition_id,omitempty"`
	EventType    string `json:"event_type"`
	EventSummary string `json:"event_summary"`
	Timestamp    int64  `json:"timestamp"`
	Role         string `json:"role,omitempty"`
}
    EventReference 是指向已存事件的轻量引用（键、类型、摘要、时间、角色）。会话侧只持有引用 列表，全文按需用
    GetEvent/GetEvents 水合。字段单位与取值属契约，见文档。

    契约: docs/wiki/memory/memory-architecture.md#event-shape

type EventReplayer interface {
	// ReplayEvent 走内部回放路径提交 canonicalFact，返回发生了什么、以及**实际存储的**事实
	// （若已提交过，非内容字段可能与入参不同）。提交屏障失败或检出内容冲突时返回错误。
	ReplayEvent(key int64, canonicalFact FullEvent) (ReplayResult, FullEvent, error)
}
    EventReplayer 是可选能力接口，支持"以内容校验为前提"的内部回放。与公开 StoreEvent
    不同 （后者把任何已存在的键一律判为重复，哪怕内容逐字节相同），回放必须先确认已存原文与待提交内容
    逐字节相同，才补写半写孤儿并重跑提交屏障；同键不同内容判为冲突且**绝不覆盖既有事实**。 返回的 ReplayResult
    让调用方把计数与副作用精确加一次。

    可靠收件箱与内存溢出落盘的恢复路径**必须**走本接口而非公开 StoreEvent，以显式区分"新提交" 与"回放"；不持有类型化断言的调用方继续用
    StoreEvent。实现：文件段存储与内存存储，装饰链 （引擎桥、错误统计存储）透明透传。

type FeedbackPayload struct {
	Verdict   string  `json:"verdict"`
	Rating    float64 `json:"rating,omitempty"`
	Note      string  `json:"note,omitempty"`
	Source    string  `json:"source"`
	ParentKey string  `json:"parent_key"`
	Timestamp int64   `json:"timestamp"`
}
    FeedbackPayload 是 feedback 事件 Content 的结构化 JSON（verdict/rating/note/source）。

type FileSegmentStore struct {
	// Has unexported fields.
}
    FileSegmentStore implements MemoryStore using RustViking KV + segment model.

func NewFileSegmentStore(kv KVStore, rel RelationStore, dataDir string, cacheSize int) (*FileSegmentStore, error)
    NewFileSegmentStore creates a FileSegmentStore.

func (s *FileSegmentStore) ArmRetention()
    ArmRetention releases the store's first destructive scan once the recovery
    owner has finished rebuilding the lease from existing unacked material
    (the restart-race gate, paired with the scanner's Lease.Ready wait).
    Idempotent; a store with no lease (nil retention) ignores it. Implements
    memory.RetentionGuard.

func (s *FileSegmentStore) BeginHold()
    BeginHold/EndHold expose the registration barrier of the store's lease
    (composition-root aggregate inventory + late attach pause forgetting while
    any owner is mid-registration). nil-safe via the lease itself.

func (s *FileSegmentStore) Close() error
    Close stops all background components (Compactor, LifecycleManager) and
    closes the RelationStore if it supports closing. Idempotent via sync.Once.

func (s *FileSegmentStore) Compactor() *Compactor
    Compactor returns the injected background compactor (nil when the store runs
    without one). Harness hook for synchronous compaction triggering .

func (s *FileSegmentStore) DeleteEvent(key int64) error
    DeleteEvent permanently deletes an event from storage.

    Idempotent against logical death : an already-tombstoned event had its
    live count decremented at marking time — a repeated (or compaction cleanup)
    delete must NOT decrement again.

func (s *FileSegmentStore) EndHold()
    EndHold 放下登记屏障，须与 BeginHold 成对调用。

func (s *FileSegmentStore) GetEvent(key int64) (*FullEvent, error)
    GetEvent retrieves a single event by its EventKey.

    Error taxonomy : a genuinely absent key returns an error wrapping
    ErrKeyNotFound; storage I/O failures are returned AS-IS (never dressed up as
    "not found") so upper layers can tell a miss from an outage. Returned events
    are defensive clones — callers may mutate them freely.

func (s *FileSegmentStore) GetEvents(keys []int64) ([]FullEvent, error)
    GetEvents retrieves multiple events by their EventKeys. A genuinely missing
    key (typed) is skipped; a storage I/O failure is NOT silently swallowed —
    the successfully read events are returned together with the error so callers
    can tell a partial result from a complete one .

func (s *FileSegmentStore) GetSegmentMeta(pid int, windowTS int64) (*SegmentMeta, error)
    GetSegmentMeta retrieves segment metadata from KV.

func (s *FileSegmentStore) GetStats() StoreStats
    GetStats returns storage statistics.

func (s *FileSegmentStore) Init() error
    Init discovers persisted partitions and registers them in s.partitions so
    the forgetting scans (TTL / capacity / compaction, which Range over the
    map) cover the FULL store — not just partitions this process wrote to .
    Partition enumeration uses the optional ListPartitionIDs capability via
    type assertion; backends without it keep lazy discovery (known limitation,
    logged). seqCounter recovery stays in StoreEvent's window path . Called from
    NewFileSegmentStore; safe to call again.

func (s *FileSegmentStore) IsKeyProtected(key int64) bool
    IsKeyProtected reports whether a key is under a live retention lease. The
    TTL scanner, capacity evictor and compactor final-cleanup consult this so
    a protected original survives until its recovery owner releases the lease.
    nil-safe.

func (s *FileSegmentStore) KVBackend() KVStore
    KVBackend 暴露底层 KVStore（KVProvider 实现）——供记忆引擎做向量持久化 （序列化向量入 KV + 启动重建，见
    engine_persist.go）。

func (s *FileSegmentStore) Lifecycle() *LifecycleManager
    Lifecycle returns the injected lifecycle manager (nil when the store runs
    without one). Harness hook for synchronous TTL/capacity sweeps .

func (s *FileSegmentStore) ListSegments(pid int) ([]int64, error)
    ListSegments returns all segment window timestamps for a partition.

func (s *FileSegmentStore) LivesCountKnown() bool
    LivesCountKnown reports whether the per-partition live counts reflect the
    full logical set . Capacity eviction must pause when false.

func (s *FileSegmentStore) PartitionCountKnown(pid int) bool
    PartitionCountKnown reports whether pid's live count is authoritative;
    capacity eviction skips a partition when it is false .

func (s *FileSegmentStore) ProtectKey(key int64)
    ProtectKey registers a holder for a key under the shared retention lease .
    It is used by the recovery owner (inbox envelope / spill) before the first
    durable fact commit so the material cannot be evicted out from under an
    in-flight prepare.

func (s *FileSegmentStore) QueryEvents(query QueryOptions) ([]EventReference, error)
    QueryEvents queries events based on filters.

    Behavioral contract: the result is semantically equivalent to "filter all
    events → total-order sort → offset/limit". Segmentation, window pruning and
    early-stop below are optimizations only and must not change the observable
    result. Total order: (Timestamp, EventKey) — same-millisecond events are
    tie-broken by EventKey so any two runs (and any store implementation) return
    identical sequences.

func (s *FileSegmentStore) RebuildLiveCounts() error
    RebuildLiveCounts rebuilds each discovered partition's logical live count
    from the single fact chain : dedup by EventKey (compaction crash windows
    keep both layers alive briefly) and tombstone exclusion. Must run AFTER
    tombstone recovery and BEFORE the lifecycle/compaction scanners start. Any
    scan failure leaves the counts unknown (capacity eviction pauses) instead of
    pretending an empty store.

func (s *FileSegmentStore) RelationStore() RelationStore
    RelationStore returns the underlying RelationStore for relationship
    operations.

func (s *FileSegmentStore) ReleaseKey(key int64)
    ReleaseKey drops one holder for a key ; the original becomes eligible for
    normal age-based destruction once the last holder is gone. Called after
    ack dir-sync success or spill safe-removal. Restores the key's ORIGINAL TTL
    window (no re-stamping).

func (s *FileSegmentStore) ReplayEvent(key int64, canonicalFact FullEvent) (ReplayResult, FullEvent, error)
    ReplayEvent implements EventReplayer : canonical content-checked replay
    that classifies the outcome (new / repaired / already-committed)
    rather than rejecting all duplicates like the public StoreEvent.
    This allows the reliable inbox and mem_spill paths to safely retry without
    double-incrementing live-count or treating a completed repair as a failure.

func (s *FileSegmentStore) RetentionLease() *RetentionLease
    RetentionLease returns the shared-resource retention lease (nil when
    unwired).

func (s *FileSegmentStore) SealCurrent(pid int) error
    SealCurrent seals the current active segment for a partition. Updates
    segment metadata in KV to mark it as L1 (sealed).

func (s *FileSegmentStore) SearchByEmbedding(query []float32, topK int) ([]EventReference, error)
    SearchByEmbedding performs semantic search (stub — not supported).

func (s *FileSegmentStore) SetCompactor(c *Compactor)
    SetCompactor injects a Compactor for graceful shutdown. The compactor is
    stopped when Close is called.

func (s *FileSegmentStore) SetLifecycleManager(lm *LifecycleManager)
    SetLifecycleManager injects a LifecycleManager for graceful shutdown.
    The manager is stopped when Close is called.

func (s *FileSegmentStore) SetRetentionLease(l *RetentionLease)
    SetRetentionLease injects the unacked-recovery retention lease. It MUST be
    called during recovery rebuild, BEFORE the lifecycle/compaction producers
    start, so a scanner can never destroy a retained original before its lease
    is registered . Passing nil clears protection.

func (s *FileSegmentStore) SetTombstoneSet(ts *TombstoneSet)
    SetTombstoneSet injects a TombstoneSet into the store after construction.
    This allows tombstone filtering to be enabled without modifying
    NewFileSegmentStore's signature. Once set, GetEvent and QueryEvents will
    skip tombstoned events.

func (s *FileSegmentStore) SetVectorRemover(vr VectorRemover)
    SetVectorRemover 注册向量移除回调（wireMemoryEngine 包裹引擎后调用）。 用 atomic 存：compactor
    goroutine 可能已在运行，读写需无竞态。

func (s *FileSegmentStore) StopProducers()
    StopProducers halts the background forgetting producers (compactor +
    lifecycle scanner) WITHOUT touching the relation/KV backend. close order
    requires the producers to stop BEFORE the engine worker, because they call
    the engine's vector remover while sweeping — yet the engine's own drain
    still writes the KV, so the KV must close AFTER the engine. Both Stop
    methods are idempotent, so a subsequent Close re-stops them as a no-op.

func (s *FileSegmentStore) StoreEvent(key int64, event FullEvent) error
    StoreEvent stores a single event via RustViking KV.

    Commit semantics : the event is committed serially — collision check
    → evt → idx → (meta) → DURABILITY BARRIER → cache/count publication.
    Success means the barrier completed: the event survives an unclean process
    termination even below the WAL flush threshold. Any failure leaves the cache
    and the live count untouched and the caller (stored-gate) must not project
    the event.

func (s *FileSegmentStore) StoreEventWithEmbedding(key int64, event FullEvent, embedding []float32) error
    StoreEventWithEmbedding stores event with embedding (stub — ignores
    embedding).

func (s *FileSegmentStore) SupportsVectorSearch() bool
    SupportsVectorSearch returns false.

func (s *FileSegmentStore) WalQuarantined() int64
    WalQuarantined 透传底层 KV 的 WAL 隔离计数（ ——此前装饰链在 FileSegmentStore 断裂，诊断恒采 0）。

type FullEvent struct {
	EventKey     int64  `json:"event_key"`
	PartitionID  int    `json:"partition_id"`
	EventType    string `json:"event_type"`
	EventSummary string `json:"event_summary"`
	Timestamp    int64  `json:"timestamp"`
	Content      string `json:"content"`
	// ContentParts 承载文本正文为空、内容在部件里（图/文件/音）的输入：一次有效输入必须同时
	// 存活于事实链与真正发出的请求，只留一条路径等于丢失。附加式且 omitempty，故本字段之前
	// 存下的记录解码不变。
	ContentParts []model.ContentPart `json:"content_parts,omitempty"`
	ToolCalls    []model.ToolCall    `json:"tool_calls"`
	// ToolID 记录本工具结果对应哪次调用，保证跨存储→解析不丢配对。
	ToolID      string                 `json:"tool_id,omitempty"`
	ToolResults map[string]interface{} `json:"tool_results"`
	Metadata    map[string]string      `json:"metadata"`
	// Response 是可选的 LLM 响应快照；按契约视为只读，故不随本结构深拷贝。
	Response *model.Response `json:"response,omitempty"`
}
    FullEvent 是一条事件的完整记录，也是事件数据的唯一真源。

    时间契约：本记录的 Timestamp 是唯一的语义时间轴（排序、时间范围、TTL 年龄、时间线只读它）；
    EventKey 内编码的是写入时刻，仅用于段落定位与同毫秒平局打破，不得用于语义判断。异步回写会让
    两者分叉，而分叉无害是因为没有决策同时依赖两者。因果父引用不在本结构内，改由关系存储承载，
    以分开"不可变事件内容"与"可变关系"。字段表与两轴理由见文档。

    契约: docs/wiki/memory/memory-architecture.md#event-shape

type InMemRelationStore struct {
	// Has unexported fields.
}
    InMemRelationStore 实现了 RelationStore 接口。 内存双图（childToParent +
    parentToChildren）+ WAL journal。

func NewInMemRelationStore(dataDir string) (*InMemRelationStore, error)
    NewInMemRelationStore 创建并初始化 InMemRelationStore。 dataDir 用于存储 journal 和
    snapshot 文件。

func (rs *InMemRelationStore) Close() error
    Close 关闭 store，释放资源。

func (rs *InMemRelationStore) EventsCount() int
    EventsCount 返回当前记录的事件数。

func (rs *InMemRelationStore) GetChildren(parentKey int64) ([]int64, error)
    GetChildren 获取 parentKey 的所有直接后继。

func (rs *InMemRelationStore) GetParent(childKey int64) (int64, error)
    GetParent 获取 childKey 的 parentKey。

func (rs *InMemRelationStore) GetParents(keys []int64) (map[int64]int64, error)
    GetParents 批量获取 parentKey。

func (rs *InMemRelationStore) LoadSnapshot(data map[int64]int64) error
    LoadSnapshot 从快照恢复。

func (rs *InMemRelationStore) RemoveRelations(key int64) error
    RemoveRelations 删除某事件的所有关联。

func (rs *InMemRelationStore) ReplayJournal(entries []JournalEntry) error
    ReplayJournal 重放 WAL。

func (rs *InMemRelationStore) SaveSnapshotToFile() error
    SaveSnapshotToFile 将当前关系图的快照保存到文件。 同时截断 journal（snapshot 后所有变更已固化）。

func (rs *InMemRelationStore) SetParent(childKey, parentKey int64) error
    SetParent 设置/更新 parentKey。

func (rs *InMemRelationStore) Snapshot() (map[int64]int64, error)
    Snapshot 创建全量快照。

type InMemoryStore struct {
	// Has unexported fields.
}
    InMemoryStore implements MemoryStore using an in-memory map. Events are
    partitioned by PartitionID for storage isolation. Suitable for testing and
    prototyping.

    Note: InMemoryStore embeds RelationStore to provide O(1) parent/child
    relationship queries via GetParent/GetChildren.

func NewInMemoryStore() *InMemoryStore
    NewInMemoryStore creates a new InMemoryStore.

func NewInMemoryStoreWithRelation(rel RelationStore) *InMemoryStore
    NewInMemoryStoreWithRelation creates a new InMemoryStore with a
    RelationStore. If rel is nil, creates a default simpleInMemRelationStore
    that stores relationships in memory.

func (s *InMemoryStore) AllEvents() []FullEvent
    AllEvents returns all stored events (for testing/debugging).

func (s *InMemoryStore) AllEventsByPartition(partitionID int) []FullEvent
    AllEventsByPartition returns events for a specific partition (for
    testing/debugging).

func (s *InMemoryStore) DeleteEvent(key int64) error
    DeleteEvent permanently deletes an event.

func (s *InMemoryStore) GetChildren(key int64) ([]int64, error)
    GetChildren returns all direct child EventKeys for the given event key.

func (s *InMemoryStore) GetEvent(key int64) (*FullEvent, error)
    GetEvent retrieves a single event by its EventKey. The returned event is a
    defensive clone — caller mutations must not corrupt stored facts .

func (s *InMemoryStore) GetEvents(keys []int64) ([]FullEvent, error)
    GetEvents retrieves multiple events by their EventKeys. Returned events are
    defensive clones .

func (s *InMemoryStore) GetParent(key int64) (int64, error)
    GetParent returns the parent EventKey for the given event key.

func (s *InMemoryStore) GetStats() StoreStats
    GetStats returns storage statistics.

func (s *InMemoryStore) QueryEvents(query QueryOptions) ([]EventReference, error)
    QueryEvents queries events based on filters.

func (s *InMemoryStore) RelationStore() RelationStore
    RelationStore returns the underlying RelationStore for relationship
    operations. This is used by higher-level components (e.g., plugin) to manage
    parent-child relationships independently of CRUD operations.

func (s *InMemoryStore) ReplayEvent(key int64, canonicalFact FullEvent) (ReplayResult, FullEvent, error)
    ReplayEvent implements EventReplayer : canonical content-checked replay.
    InMemoryStore has no orphan/half-orphan states (no segment file layer),
    so only two outcomes exist: new commit (ReplayNew) or already-committed
    (ReplayAlreadyCommitted). Different content under the same identity is a
    hard collision error .

func (s *InMemoryStore) SearchByEmbedding(query []float32, topK int) ([]EventReference, error)
    SearchByEmbedding performs semantic search (stub — not supported).

func (s *InMemoryStore) SetParent(key int64, parentKey int64) error
    SetParent sets the parent EventKey for the given event key.

func (s *InMemoryStore) StoreEvent(key int64, event FullEvent) error
    StoreEvent stores a single event.

    Contract parity with FileSegmentStore : a duplicate EventKey is REFUSED (an
    EventKey is the event's identity, never silently overwritten) and the stored
    event is a defensive clone, so the caller mutating its input afterwards
    cannot corrupt stored facts.

func (s *InMemoryStore) StoreEventWithEmbedding(key int64, event FullEvent, embedding []float32) error
    StoreEventWithEmbedding stores event with embedding (stub — ignores
    embedding).

func (s *InMemoryStore) SupportsVectorSearch() bool
    SupportsVectorSearch returns false for InMemoryStore.

type IndexBuilder interface {
	// Index 将一个事件纳入索引（引擎内部决定嵌入/向量存储/分层/选择性）。
	Index(ctx context.Context, evt IndexableEvent) error
	// Remove 从索引移除一个事件（TTL/墓碑回收时调用；引擎可惰性处理）。
	Remove(ctx context.Context, eventKey int64) error
}
    IndexBuilder 索引构建面：记忆引擎据此把事件纳入索引。 闭环在引擎内部——tagent 只投递
    IndexableEvent，不管引擎如何嵌入/存储/分层。

    实现纪律： - Index MUST 异步或快速返回，绝不阻塞事件主链路（不变量：StoreEvent 同步点）。
    典型实现：非阻塞投递到耐用队列/通道，后台 worker 嵌入 + 写向量索引。 - Index 失败 MUST NOT 传染调用方（记日志 +
    计数即可；向量是增强索引，丢一条 只影响该条语义可召回性，关键词路径兜底）。 - Remove 用于 TTL/墓碑回收；引擎可惰性处理（水合过滤 +
    超取 + 阈值重建）。

type IndexableEvent struct {
	EventKey    int64
	PartitionID int
	EventType   string
	Text        string
	Timestamp   int64
}
    IndexableEvent 是投递给引擎纳入索引的事件视图。 引擎不依赖 FullEvent 全貌——只取索引所需的字段，保持缝的最小面。

type JournalEntry struct {
	Op        string
	ChildKey  int64
	ParentKey int64
	EventKey  int64
}
    JournalEntry 表示一条 WAL 日志记录。

type KVOp struct {
	// Type 是操作类型，取 "put" 或 "delete"。
	Type  string `json:"op"`
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}
    KVOp 表示一个批量操作。

type KVPair struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
    KVPair 表示一个键值对。

type KVProvider interface {
	KVBackend() KVStore
}
    KVProvider 是可选接口：MemoryStore 实现若持有底层 KVStore（如 FileSegmentStore），
    据此暴露给记忆引擎做向量持久化（T-A：序列化向量入 KV + 启动重建，跨重启恢复语义召回）。

type KVStore interface {
	KVPut(key, value string) error
	KVGet(key string) (string, error)
	KVDelete(key string) error
	KVScan(prefix string, limit int) ([]KVPair, error)
	KVRange(start, end string, limit int) ([]KVPair, error)
	KVBatch(ops []KVOp) error
}
    KVStore 抽象底层 KV 操作，是 FileSegmentStore 的持久化底座。契约与数据类型居核心包、 实现居子包 memory/kv（与
    MemoryEngine、Embedder 同一切分原则）。语义约束：键为字符串 （键格式由 memory/key_schema.go
    单点定义）；Scan/Range 按字典序返回；limit<=0 不限制。 接入新后端的路径与接线点见文档。

    契约: docs/wiki/memory/memory-architecture.md#extension-paths

type LifecycleConfig struct {
	// GlobalTTLDays is the default TTL for all events (default: 7).
	GlobalTTLDays int `json:"global_ttl_days"`
	// MaxEventsPerPartition is the maximum event count per partition (0 = no limit).
	MaxEventsPerPartition int `json:"max_events_per_partition"`
	// CheckInterval is how often to check for expired events (default: 1 hour).
	CheckInterval time.Duration `json:"check_interval"`
	// TypeTTL overrides global TTL for specific event types (in days).
	// Key = event type, Value = TTL in days.
	TypeTTL map[string]int `json:"type_ttl,omitempty"`
}
    LifecycleConfig configures the lifecycle manager.

func DefaultLifecycleConfig() LifecycleConfig
    DefaultLifecycleConfig returns the default lifecycle configuration.

type LifecycleManager struct {
	// Has unexported fields.
}
    LifecycleManager manages event TTL expiration and capacity eviction.

func NewLifecycleManager(store *FileSegmentStore, tombstone *TombstoneSet, config LifecycleConfig) *LifecycleManager
    NewLifecycleManager creates a LifecycleManager.

func (lm *LifecycleManager) GetTombstoneFilterFunc() func(int64) bool
    GetTombstoneFilterFunc returns a filter function for compaction that checks
    if an event key is tombstoned.

func (lm *LifecycleManager) Start()
    Start starts the lifecycle manager background goroutine.

func (lm *LifecycleManager) Stop()
    Stop stops the lifecycle manager gracefully.

func (lm *LifecycleManager) SweepOnce()
    SweepOnce runs one synchronous lifecycle pass (TTL scan + capacity check) —
    the scheduler body, exported so E2E/soak harnesses exercise real TTL expiry
    without waiting the scanner interval . Same semantics, no second truth.

type MemSpill struct {
	// Has unexported fields.
}
    MemSpill 是 memory 退化事件兜底 JSONL 存储（并发安全）。path 空则禁用（nil 语义）。

func NewMemSpill(path string) *MemSpill
    NewMemSpill 构建兜底存储。path 为空返回 nil（禁用，ErrorTrackingStore 据此跳过落盘）。

func (s *MemSpill) Append(key int64, event FullEvent) error
    Append 落盘一个 StoreEvent 失败的事件（JSONL 追加）。best-effort：落盘失败返回 error
    （调用方据此告警——兜底也失败则事件真丢，但已尽最后一力）。

func (s *MemSpill) Len() int
    Len 返回当前兜底事件数（诊断/背压信号）。

func (s *MemSpill) PendingKeys() ([]int64, error)
    PendingKeys 返回仍在等待 spill 重放的事件 key。恢复 owner 启动时据此从现有 未确认 spill 材料重建保留租约，令其
    durable 原文在被重放移除前不受 TTL/容量/压实销毁。 nil-safe；读文件失败返回错误（调用方保守处理，不得据此开放淘汰）。

func (s *MemSpill) ProtectAllPending() error
    ProtectAllPending 从现有 spill 文件重建保留租约：对每条待重放 key 注册一个持有者。由恢复 owner 在装载 spill
    后、放行扫描器前调用。

func (s *MemSpill) Replay(store MemoryStore) (int, error)
    Replay 重放兜底事件到 store（按原 key StoreEvent）。成功的移除、仍失败的保留在文件。 返回重放成功数。store 应为
    inner（绕过 ErrorTrackingStore 防递归）。坏行跳过。

func (s *MemSpill) ReplayWithNotify(store MemoryStore, notify func(FullEvent)) (int, error)
    ReplayWithNotify 是 Replay 的双写形态：每条重放成功 （含幂等命中）的事件回调
    notify——调用方据此补投影（projection.Append），恢复 「存储⇔投影同点原子」的等价语义。notify 为 nil
    或内部失败不影响重放结果（投影可后补，事件不丢优先）。

    canonical replay only: spill replay MUST use the store's EventReplayer
    contract (ReplayEvent) — it distinguishes new-commit / orphan-repair
    / already-committed atomically against the durable fact chain.
    A store that does NOT implement EventReplayer is refused and its spill
    originals are retained (spec L99: 内层没有显式 恢复能力 → 能力检查失败、原件保留). The former
    GetEvent+StoreEvent weak fallback was removed: a GetEvent hit only proves a
    read returns the record, not that the durable commit (barrier + index/meta
    publication) completed, and public StoreEvent now REFUSES an existing key so
    it can never complete an orphan anyway.

func (s *MemSpill) SetGuard(g RetentionGuard)
    SetGuard 注入 保留租约守卫（nil = 不保护）。由持有本 spill 的装饰器从其后端取得。

type MemoryEngine interface {
	IndexBuilder
	Retriever
	io.Closer
}
    MemoryEngine = 索引构建 + 检索 + 生命周期。这是 tagent 核心依赖的解耦缝。

    实现： - InMemoryEngine（MVP 兜底）：内存向量索引 + 关键词，无外部依赖，供开发/测试/降级。 -
    RustVikingEngine（适配器，闭环到 rustviking）：tagent 侧 zhipu 嵌入 + rustviking index
    insert/search/delete 向量后端 + 适配器内 RRF 融合与分区过滤。

    生命周期：随 MemoryStore 启停（Closer 接线，resolveMemoryStore 按配置创建）。

type MemoryEngineProvider interface {
	MemoryEngine() MemoryEngine
}
    MemoryEngineProvider 是可选接口：装饰器据此暴露其记忆引擎。 仿
    RelationStoreProvider——recall/插件经类型断言获取引擎，未接线时断言失败即降级 为纯关键词（现状行为）。这是 tagent
    核心与引擎实现之间的解耦触点。 （实现位于子包 memory/engine 的 engineBridge。）

type MemoryStore interface {
	// StoreEvent 写入单条事件及其完整细节。
	StoreEvent(key int64, event FullEvent) error

	// GetEvent 按事件键读取单条事件。
	GetEvent(key int64) (*FullEvent, error)

	// GetEvents 按键批量读取，返回顺序与 keys 一致，缺失键跳过而不报错。
	GetEvents(keys []int64) ([]FullEvent, error)

	// QueryEvents 按条件查询，返回轻量引用列表（不返回全文）。
	QueryEvents(query QueryOptions) ([]EventReference, error)

	// SearchByEmbedding 以查询向量做语义检索，返回排序票据。
	SearchByEmbedding(query []float32, topK int) ([]EventReference, error)

	// StoreEventWithEmbedding 写入事件并登记其向量。
	StoreEventWithEmbedding(key int64, event FullEvent, embedding []float32) error

	// SupportsVectorSearch 报告本存储是否具备向量能力（不支持时调用方退回关键词路）。
	SupportsVectorSearch() bool

	// DeleteEvent 永久删除事件；受保留租约保护时必须被拒（见 ErrEventProtected）。
	DeleteEvent(key int64) error

	// GetStats 返回存储统计。
	GetStats() StoreStats
}
    MemoryStore 是事件的存储与读取接口，也是事件数据的唯一真源。 存储隔离：以 PartitionID 作为分区键——记忆层不认识
    agent，分区是纯存储概念； "身份 → 分区"的映射发生在 MemoryStore 之外（插件层），以保持记忆层存储语义干净。

type ParsedKey struct {
	PartitionID int
	KeyType     string
	WindowTS    int64
	Seq         int
	EventKey    int64
}
    ParsedKey contains the components extracted from a KV key.

func ParseKey(key string) (*ParsedKey, error)
    ParseKey parses a KV key string into its components. Returns error if the
    key format is invalid.

type PartitionState struct {
	// Has unexported fields.
}
    PartitionState holds per-partition state for FileSegmentStore.

type QueryOptions struct {
	// PartitionID filters events by storage partition.
	// 0 = no partition filter (query across all partitions).
	PartitionID int `json:"partition_id"`
	// PartitionIDs filters events across multiple partitions.
	// Takes precedence over PartitionID if non-empty.
	PartitionIDs []int    `json:"partition_ids"`
	EventTypes   []string `json:"event_types"`
	// StartTime/EndTime filter events by timestamp, in Unix MILLISECONDS
	// (same unit as FullEvent.Timestamp and the recall tools' since/until).
	// Zero = no bound.
	StartTime int64 `json:"start_time"`
	EndTime   int64 `json:"end_time"`
	// MinEventKey filters events whose EventKey is STRICTLY GREATER than this
	// value — the write-order axis, never semantic time (see the TIME CONTRACT
	// on FullEvent: async write-back events carry an EventKey assigned at
	// write time while their Timestamp is the bus-arrival moment, so a
	// StartTime approximation mis-cuts such stragglers). 0 = no bound.
	MinEventKey int64  `json:"min_event_key,omitempty"`
	Limit       int    `json:"limit"`
	Offset      int    `json:"offset"`
	OrderBy     string `json:"order_by"`
	// Keyword filters events whose EventSummary or Content contains the keyword (case-insensitive).
	// Empty string = no keyword filter.
	Keyword string `json:"keyword,omitempty"`
}
    QueryOptions specifies filters for querying events.

type RawVectorSearcher interface {
	SearchByVector(ctx context.Context, query []float32, topK int, partitionIDs []int) ([]RetrievalHit, error)
}
    RawVectorSearcher 是可选引擎能力：支持「预计算查询向量」检索（供 MemoryStore.SearchByEmbedding
    委托，消灭 stub）。文本查询走 Retriever.Retrieve； 本接口服务于已持有查询向量的调用方。partitionIDs
    非空时过滤（nil = 不限）。

type ReceiptVerdict struct {
	Total            int
	Resolved         int
	Tombstoned       int
	Missing          int
	FingerprintMatch bool
	Detail           string
}
    ReceiptVerdict 是巩固事件收据的回放验证裁决。

func VerifyConsolidation(store MemoryStore, evt FullEvent) ReceiptVerdict
    VerifyConsolidation 回放验证：解析收据 key → GetEvents 取源事件 → 重算指纹比对。

type RelationStore interface {
	// SetParent 设置/更新 parentKey（建立或修改因果链）
	SetParent(childKey, parentKey int64) error

	// GetParent 获取 parentKey（0 = 无前驱）
	GetParent(childKey int64) (int64, error)

	// GetChildren 获取所有直接后继（反向查询）
	GetChildren(parentKey int64) ([]int64, error)

	// GetParents 批量获取（memory_trace 热路径优化）
	GetParents(keys []int64) (map[int64]int64, error)

	// RemoveRelations 删除某事件的所有关联（逐出时调用）
	RemoveRelations(key int64) error

	// Snapshot 创建全量快照
	Snapshot() (map[int64]int64, error)

	// LoadSnapshot 从快照恢复
	LoadSnapshot(data map[int64]int64) error

	// ReplayJournal 重放 WAL（启动恢复）
	ReplayJournal(entries []JournalEntry) error

	// EventsCount 返回当前记录的事件数
	EventsCount() int
}
    RelationStore 维护事件间的因果关联图。 全量常驻内存，变更通过 WAL 持久化。

type RelationStoreProvider interface {
	RelationStore() RelationStore
}
    RelationStoreProvider 是可选接口：并非所有存储都暴露关系操作，调用方访问父子关系前必须先
    断言本接口存在，断言失败按"无关系能力"处理而不是报错。

type ReplayResult int
    ReplayResult 区分一次内部回放究竟发生了什么，调用方据此决定计数与副作用只加一次。

const (
	// ReplayNew：此前不存在，已作为新提交写入，活跃计数加一。
	ReplayNew ReplayResult = iota
	// ReplayRepaired：半写孤儿被补全（索引在而原文或元数据缺），提交屏障已重跑，事件现已完整
	// 持久；活跃计数恰好加一，不多加。
	ReplayRepaired
	// ReplayAlreadyCommitted：发现已完整提交（原文逐字节相同且键在发布缓存内）。调用幂等，
	// 不加活跃计数。
	ReplayAlreadyCommitted
)
type RetentionGuard interface {
	// ProtectKey 为该键登记一个持有者（引用计数，幂等）。
	ProtectKey(key int64)
	// ReleaseKey 释放一个持有者；最后一个持有者离开后键恢复按年龄处理——原始时间戳绝不被重打。
	ReleaseKey(key int64)
	// ArmRetention 表示恢复归属方已从现存未确认材料重建完保留集，从而放行存储的首次破坏性扫描
	// （重启竞态门）。幂等；无保留集的存储忽略本调用。
	ArmRetention()
	// BeginHold/EndHold 抬起与放下登记屏障：任一持有者存在期间，生命周期扫描**无条件暂停**遗忘
	// 遍次（无宽限逃逸），使组合根的聚合清点或迟挂入的共享恢复目录能在任何破坏性遍次落地前
	// 保护其材料。每个 Begin 必须配一个 End；嵌套与并发持有者按引用计数。
	BeginHold()
	EndHold()
}
    RetentionGuard 是可选的"材料保留"恢复能力：恢复归属方为未确认材料的持久原文登记保留，
    使 TTL 过期、容量驱逐与墓碑终清在安全释放（ack 目录同步完成或落盘项被移除）之前不销毁它们。
    租约由文件段存储持有，装饰链（引擎桥、错误统计）把能力递归透传给它。 不具备持久恢复能力的后端直接不实现本接口——缺失即"无物可保留"，不是错误。

type RetentionHoldable interface {
	BeginHold()
	EndHold()
}
    RetentionHoldable 只暴露登记屏障那一面（由组合根构建门或迟挂入的恢复归属方抬起，在其清点
    落地期间暂停遗忘）。文件段存储经由保留集满足它；无保留集（也就无破坏性扫描）的存储不满足， 调用方把"能力缺失"当作"无物可暂停"，而不是错误。

type RetentionLease struct {
	// Has unexported fields.
}
    RetentionLease ==================== 有限 key 保留租约====================

    RetentionLease 保护「未确认恢复材料」的 durable 原文——未确认 inbox envelope 的 prepared
    fact key 与 receipt key、普通 spill 待重放 key——使其在恢复 owner 能安全 释放之前不被 TTL
    过期、容量淘汰、内容降分辨率或墓碑最终清理物理销毁。持有者是共享 资源 owner（FileSegmentStore），非任一 agent。

    不变量： - 租约是**内存守护集**，启动时由现有未确认材料（inbox envelope 文件 + spill 文件） 重建，MUST NOT
    引入第二持久保留表或全历史去重集合。 - 保护期内只拒绝**销毁**；无损搬迁（压实段合并复制原文到高层段）允许。 - 显式删除受保护 key 返回
    ErrEventProtected（不销毁）。 - 释放（ack 目录同步成功 / spill 安全移除）后恢复该 key 原类型的 TTL，按其原有
    timestamp 参与年龄淘汰，绝不重新盖时间。

    引用计数：同一 key 可同时被 envelope owner 与 spill owner 保护，Release 幂等地
    递减，归零才真正解除保护。nil-safe（未接线租约的 store 表现为无保护）。

func NewRetentionLease() *RetentionLease
    NewRetentionLease 构造空租约（未就绪：挂上它的 store 会等首次 populate 后才扫描）。

func (l *RetentionLease) BeginHold()
    BeginHold 取得登记屏障：从此刻起 HoldClear 不放行，扫描器不得发布遗忘。 调用方 MUST
    在清点+登记（含失败路径的显式阻断决策）完成后 EndHold 配对； 组合根对共享 store 的多个 owner 各自配对，计数嵌套安全。

func (l *RetentionLease) EndHold()
    EndHold 释放一层登记屏障；最外层归零时放闸。多余的 End 是 no-op（不 panic、 不反向放闸未配对的 Begin）。

func (l *RetentionLease) HoldClear() <-chan struct{}
    HoldClear 返回「当前无登记屏障」信号；nil 租约返回已关闭通道（不阻塞）。 扫描器在首趟破坏性扫描前无条件等待它。

func (l *RetentionLease) Holders(key int64) int
    Holders 返回 key 当前的存活持有者数（诊断/测试：同一 key 可被 envelope owner 与 spill owner
    同时保护）。未保护＝ 0。nil-safe。（ 1.2）泄漏回归用它断言“一个壳构建不向共享租约加持有者”。

func (l *RetentionLease) IsProtected(key int64) bool
    IsProtected 报告 key 是否仍有存活持有者。nil-safe（无租约＝不保护）。

func (l *RetentionLease) Len() int
    Len 返回受保护的 distinct key 数（诊断/测试）。

func (l *RetentionLease) MarkReady()
    MarkReady 登记恢复 owner 已完成首次租约重建，放行扫描器首趟。幂等。

func (l *RetentionLease) Protect(key int64)
    Protect 为 key 增加一个持有者（幂等注册）。零 key 忽略。

func (l *RetentionLease) Ready() <-chan struct{}
    Ready 返回就绪信号通道（供扫描器 select 等待）；nil 租约返回已关闭通道（不阻塞）。

func (l *RetentionLease) Release(key int64)
    Release 移除一个持有者；归零才解除保护。幂等（重复释放多余的不动作）。

type RetrievalCaps struct {
	Keyword bool
	Vector  bool
	Hybrid  bool
}
    RetrievalCaps 声明引擎的检索能力，供上层优雅降级与可观测。

type RetrievalHit struct {
	EventKey int64
	Score    float32
}
    RetrievalHit 是融合排序后的单条命中票据。 只含 EventKey + Score —— 全文/引用由调用方经 MemoryStore
    水合（两段式）。

type RetrievalMode int
    RetrievalMode 声明检索模式。引擎 Capabilities 决定各模式是否可用； 请求不可用模式时引擎 MUST
    优雅退化（Vector/Hybrid 无向量 → Keyword）。

const (
	// ModeAuto：引擎自选——有向量索引则 hybrid，否则 keyword。默认。
	ModeAuto RetrievalMode = iota
	// ModeKeyword：仅关键词（分词 ANY 命中，复用现有 term-split 语义）。
	ModeKeyword
	// ModeVector：仅向量（语义相似）。
	ModeVector
	// ModeHybrid：关键词 ∪ 向量 → RRF 融合。
	ModeHybrid
)
type RetrievalQuery struct {
	Query        string
	PartitionIDs []int
	EventTypes   []string
	StartTime    int64
	EndTime      int64
	Limit        int
	Mode         RetrievalMode
}
    RetrievalQuery 是检索请求。引擎内部决定 keyword/vector/hybrid 与融合排序。

type Retriever interface {
	// Retrieve 按查询召回排序后的 EventKey 票据（引擎内部 keyword/vector/hybrid 融合）。
	// 全文/引用取回由调用方经 MemoryStore.GetEvents 水合（两段式，引擎不强依赖 MemoryStore）。
	// 索引未就绪（Ready==false）时 MUST 退化为关键词而非报错。
	Retrieve(ctx context.Context, q RetrievalQuery) ([]RetrievalHit, error)
	// Capabilities 声明引擎支持的检索模式（供上层优雅降级与可观测）。
	Capabilities() RetrievalCaps
	// Ready 索引是否就绪（重启重建完成前为 false，Retrieve 退化为关键词）。
	Ready() bool
}
    Retriever 检索面：记忆引擎据此召回排序票据。

type SegmentLayer int
    SegmentLayer 表示一个段所处的层。

const (
	// LayerL0 是热层：当前时间窗，事件仍在写入。
	LayerL0 SegmentLayer = iota
	// LayerL1 L1 是第一层冷化目标。
	LayerL1
	// LayerL2 L2 是第二冷化层。
	LayerL2
	// LayerL3 L3 是最冷层。
	LayerL3
)
func (l SegmentLayer) String() string
    String 返回该层的可读名称。

type SegmentMeta struct {
	PartitionID int   `json:"pid"`
	WindowTS    int64 `json:"window_ts"`
	Layer       int   `json:"layer"`
	EventCount  int   `json:"event_count"`
	MinTime     int64 `json:"min_time"`
	MaxTime     int64 `json:"max_time"`
	Sealed      bool  `json:"sealed"`
}
    SegmentMeta holds metadata for a segment.

type StoreStats struct {
	// TotalEvents 是活跃事件数；仅在 CountsKnown 为真时可信赖。
	TotalEvents int `json:"total_events"`
	// StorageSize 是占用字节数，DataDir 是数据目录。
	StorageSize int64  `json:"storage_size"`
	DataDir     string `json:"data_dir"`
	// CountsKnown 为假表示活跃计数无法从事实链重建（后端不支持分区枚举，或扫描失败）。
	// 计数未知时容量驱逐必须暂停——未知绝不得伪装成精确的 0，理由见文档。
	CountsKnown bool `json:"counts_known"`
}
    StoreStats 是存储统计。

    契约: docs/wiki/memory/memory-architecture.md#counts-known

type TombstoneSet struct {
	// Has unexported fields.
}
    TombstoneSet 记录已被合法删除（遗忘）的事件键：内存驻留并持久化到 KV，以便崩溃后重建，
    且保证回放不会复活被遗忘的事实。删除时的级联父引用修复顺序是承重的，详见文档。

    契约: docs/wiki/memory/memory-architecture.md#tombstone

func NewTombstoneSet(rel RelationStore, kv KVStore, pid int) *TombstoneSet
    NewTombstoneSet 构造墓碑集；kv 可为 nil（仅内存），rel 必须可用以做级联修复。

func (ts *TombstoneSet) AllTombstones() []int64
    AllTombstones 返回全部墓碑键（顺序无关）。

func (ts *TombstoneSet) Count() int
    Count 返回墓碑键数量。

func (ts *TombstoneSet) IsDirty() bool
    IsDirty 报告是否存在未落盘的墓碑变更。

func (ts *TombstoneSet) IsTombstone(key int64) bool
    IsTombstone 报告键是否已被合法遗忘（回放据此拒绝复活）。

func (ts *TombstoneSet) LoadSnapshot(data map[int64]bool) error
    LoadSnapshot 从快照恢复并清除脏标记。

func (ts *TombstoneSet) MarkTombstone(key int64) error
    MarkTombstone 标记事件为已遗忘，回放据此必须拒绝复活它。
    承重约束：子事件的父引用级联必须发生在墓碑落账之后，反序会让级联把正在被删的键误当作
    存活祖先；关系操作的局部失败只记日志、不回滚遗忘——遗忘本身必须生效。

func (ts *TombstoneSet) MarshalJSON() ([]byte, error)
    MarshalJSON 序列化当前墓碑键集合。

func (ts *TombstoneSet) RecoverFromKV() error
    RecoverFromKV 启动时按 tomb 前缀扫描重建墓碑集合，忽略非本类型键与零键。

func (ts *TombstoneSet) RemoveTombstones(keys []int64) error
    RemoveTombstones 在压实吸收墓碑后成批移除内存与 KV 键，避免墓碑只增不减。

func (ts *TombstoneSet) Snapshot() (map[int64]bool, error)
    Snapshot 返回可序列化的墓碑键副本（不暴露内部映射）。

func (ts *TombstoneSet) UnmarshalJSON(data []byte) error
    UnmarshalJSON 从快照恢复墓碑键集合（覆盖现有内容）。

type TombstoneSnapshot struct {
	Keys []int64 `json:"keys"`
}
    TombstoneSnapshot 是墓碑集的持久化快照形态。

type VectorRemover interface {
	RemoveVector(eventKey int64)
}
    VectorRemover 由持有向量索引的组件实现；FileSegmentStore 在 TTL/容量遗忘**物理删除**
    事件时（Compactor.finalizeTombstones）回调，使引擎同步移除向量（内存索引 + KV 持久键）， 防死键堆积与重启复活。


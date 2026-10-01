package engine // import "github.com/SpellingDragon/tagent/memory/engine"

Package engine 提供记忆引擎的两种实现与其接线方式：进程内混合检索引擎（关键词 ∪ 向量，向量另存持久 KV 并在启动时异步重建）、store
装饰器 engineBridge（一处包裹覆盖 全部写入路径），以及读实时状态而非平行计数器的健康度诊断。

契约: docs/wiki/memory/memory-architecture.md#engine-overview

FUNCTIONS

func NewEngineBridge(inner memory.MemoryStore, engine memory.MemoryEngine) memory.MemoryStore
    NewEngineBridge 用引擎包裹 store。engine 的关键词路应指向 inner（构造引擎时传入）， 使 hybrid
    的关键词分支复用既有 QueryEvents。


TYPES

type DiagnosticsSnapshot struct {
	// VectorIndexed 向量索引维度
	// 成功索引的向量数
	VectorIndexed int64 `json:"vector_indexed"`
	// VectorCount 当前索引中的向量数
	VectorCount int64 `json:"vector_count"`
	// VectorDropped 队列满/API 失败丢弃数
	VectorDropped int64 `json:"vector_dropped"`
	// VectorEmbedErr 嵌入错误数
	VectorEmbedErr int64 `json:"vector_embed_err"`
	// VectorDimMismatch 维度不匹配跳过数（换模型信号）
	VectorDimMismatch int64 `json:"vector_dim_mismatch"`

	// CapKeyword 检索能力维度
	CapKeyword  bool `json:"cap_keyword"`
	CapVector   bool `json:"cap_vector"`
	CapHybrid   bool `json:"cap_hybrid"`
	EngineReady bool `json:"engine_ready"`

	// TotalEvents 存储规模维度
	TotalEvents int    `json:"total_events"`
	StorageSize int64  `json:"storage_size"`
	DataDir     string `json:"data_dir,omitempty"`

	// IndexHealth 派生健康率
	// indexed / (indexed + dropped + embedErr)，1.0 = 无丢失
	IndexHealth float64 `json:"index_health"`
}
    DiagnosticsSnapshot 是记忆健康度的维度快照（JSON 可序列化，供工具/可观测消费）。

type EngineConfig struct {
	// VectorTopK 向量路候选数（默认 20）。
	VectorTopK int
	// KeywordTopK 关键词路候选数（默认 20）。
	KeywordTopK int
	// RRFK RRF 融合常数（默认 60，业界惯例）。
	RRFK int
	// Overfetch 超取倍数（过滤/悬挂补偿，默认 3）。
	Overfetch int
	// QueueCap 异步嵌入队列容量（默认 256）；满则丢弃 + 计数（不背压主链路）。
	QueueCap int
	// EmbedBatch 后台批量嵌入条数上限（默认 16）。
	EmbedBatch int
	// EmbedFlushInterval 后台批量聚合窗口（默认 200ms）。
	EmbedFlushInterval time.Duration
	// MaxTextRunes 嵌入文本截断上限（默认 8000；仅嵌入侧截断，不动 memory.FullEvent.Content）。
	MaxTextRunes int
	// KV 是向量持久化后端（nil = 纯内存，向量不持久，重启丢失）。设置后：worker
	// flush 时序列化向量入 KV，构造时异步从 KV 重建（rustviking-backed 持久化，
	// 见 engine_persist.go 与 f1-rustviking-capability-report.md「追加发现」）。
	KV memory.KVStore
	// VecKeyPrefix 是向量 KV 键前缀（默认 "tagent:vec:"）。
	VecKeyPrefix string
	// DrainTimeout 是 Close 时排空在途嵌入批的超时（默认 2s）——用独立不取消的
	// ctx，避免合规嵌入器（尊重 ctx 取消）在排空期必然失败丢向量（审查 M1）。
	DrainTimeout time.Duration
	// RebuildWaitTimeout 是 Close 等待 KV 重建 goroutine 的上限（默认 3s），
	// 防 KV 后端挂住导致 Close 永久阻塞（审查 S6）。
	RebuildWaitTimeout time.Duration
}
    EngineConfig 配置记忆引擎行为（零值取默认）。

type EngineStats struct {
	// Indexed 成功索引的向量数
	Indexed int64
	// Dropped 队列满/API 失败丢弃数
	Dropped int64
	// EmbedErr 嵌入错误数
	EmbedErr int64
	// VectorCount 当前索引中的向量数
	VectorCount int64
	// DimMismatch 维度不匹配跳过数（换模型信号）
	DimMismatch int64
}
    EngineStats 是引擎内部计数快照（诊断/可观测读）。具名结构消除 diagnostics 匿名接口的 "N 个未命名 int64"
    脆弱性（S4：签名漂移变编译错误，而非静默断言失败使诊断维度归零）。

type InMemoryEngine struct {
	// Has unexported fields.
}
    InMemoryEngine 是 memory.MemoryEngine 的内存 MVP 实现。

func NewInMemoryEngine(store memory.MemoryStore, emb memory.Embedder, cfg EngineConfig) *InMemoryEngine
    NewInMemoryEngine 构建并启动 MVP 引擎。store 供关键词路（可 nil），emb 供向量路 （nil =
    纯关键词降级）。后台嵌入 worker 随引擎启动，Close 时排空停止。

func (e *InMemoryEngine) Capabilities() memory.RetrievalCaps
    Capabilities 声明能力：关键词取决于 store，向量取决于 emb 且已就绪。

func (e *InMemoryEngine) Close() error
    Close 停止后台 worker（排空在途批，用独立不取消 ctx，审查 M1）并有界等待 KV 重建 goroutine（审查 S6：防 KV
    后端挂住导致 Close 永久阻塞）。幂等。

func (e *InMemoryEngine) Index(_ context.Context, evt memory.IndexableEvent) error
    Index 将事件纳入索引。关键词路由 store 在查询时处理（无需在此建索引）； 向量路：选择性（event 注册表 Embeddable）+
    非阻塞投递后台嵌入队列。 MUST NOT 阻塞或失败主链路——队列满则丢弃 + 计数。

func (e *InMemoryEngine) Ready() bool
    Ready 引擎是否就绪：已启动、未关闭、且 KV 重建完成（若有 KV）。重建完成前为 false → 向量路 vectorAvailable()
    返回 false，Retrieve 退化为关键词（对齐 C6 契约「重启重建完成前 Ready=false」，审查 S7）。

func (e *InMemoryEngine) RebuildDone() bool
    RebuildDone 报告 KV 重建是否完成（可观测/测试同步用；生产检索不等待，向量渐进可用）。

func (e *InMemoryEngine) Remove(_ context.Context, eventKey int64) error
    Remove 从向量索引移除（TTL/墓碑回收）。内存引擎即时删除（无悬挂问题）。

func (e *InMemoryEngine) Retrieve(ctx context.Context, q memory.RetrievalQuery) ([]memory.RetrievalHit, error)
    Retrieve 混合检索：关键词 ∪ 向量 → RRF 融合 → 排序票据。 向量不可用（无 emb/未就绪/查询空）时退化为纯关键词；store 为
    nil 时纯向量。

func (e *InMemoryEngine) SearchByVector(_ context.Context, query []float32, topK int, partitionIDs []int) ([]memory.RetrievalHit, error)
    SearchByVector 实现 memory.RawVectorSearcher：用预计算查询向量做余弦 topK（分区过滤）， 供
    engineBridge.SearchByEmbedding 委托（消灭 memory.MemoryStore 的向量 stub）。

func (e *InMemoryEngine) Stats() EngineStats
    Stats 返回引擎运行指标（实现 StatsProvider，可观测/诊断用）。

type MemoryDiagnostics struct {
	// Has unexported fields.
}
    MemoryDiagnostics 维度锚定记忆诊断器（读引擎 + store 实时态）。

func NewMemoryDiagnostics(engine memory.MemoryEngine, store memory.MemoryStore) *MemoryDiagnostics
    NewMemoryDiagnostics 构建诊断器。engine/store 可为 nil（对应维度省略）。

func (d *MemoryDiagnostics) Snapshot() DiagnosticsSnapshot
    Snapshot 采集当前记忆健康度快照。

type StatsProvider interface {
	Stats() EngineStats
}
    StatsProvider 是可选能力接口：引擎暴露内部计数供诊断读取（与 memory.RawVectorSearcher 并列的 具名可选契约，非
    C6 必需）。


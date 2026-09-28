package engine

import "github.com/SpellingDragon/tagent/memory"

// DiagnosticsSnapshot 是记忆健康度的维度快照（JSON 可序列化，供工具/可观测消费）。
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
	// WALQuarantined：LocalFileKV 启动重放隔离的中间坏行数（F3 可观测
	// 闭环——此前 WalQuarantined 仅定义无消费方）。0 = 无隔离。
	WALQuarantined int64 `json:"wal_quarantined,omitempty"`

	// IndexHealth 派生健康率
	// indexed / (indexed + dropped + embedErr)，1.0 = 无丢失
	IndexHealth float64 `json:"index_health"`
}

// MemoryDiagnostics 维度锚定记忆诊断器（读引擎 + store 实时态）。
type MemoryDiagnostics struct {
	// engine 可选（nil = 无向量维度）
	engine memory.MemoryEngine
	// store 可选（nil = 无存储维度）
	store memory.MemoryStore
}

// NewMemoryDiagnostics 构建诊断器。engine/store 可为 nil（对应维度省略）。
func NewMemoryDiagnostics(engine memory.MemoryEngine, store memory.MemoryStore) *MemoryDiagnostics {
	return &MemoryDiagnostics{engine: engine, store: store}
}

// Snapshot 采集当前记忆健康度快照。
func (d *MemoryDiagnostics) Snapshot() DiagnosticsSnapshot {
	snap := DiagnosticsSnapshot{}
	if d == nil {
		return snap
	}
	if d.engine != nil {
		caps := d.engine.Capabilities()
		snap.CapKeyword = caps.Keyword
		snap.CapVector = caps.Vector
		snap.CapHybrid = caps.Hybrid
		snap.EngineReady = d.engine.Ready()
		if st, ok := d.engine.(StatsProvider); ok {
			s := st.Stats()
			snap.VectorIndexed = s.Indexed
			snap.VectorDropped = s.Dropped
			snap.VectorEmbedErr = s.EmbedErr
			snap.VectorCount = s.VectorCount
			snap.VectorDimMismatch = s.DimMismatch
			snap.IndexHealth = indexHealth(s.Indexed, s.Dropped, s.EmbedErr)
		}
	}
	if d.store != nil {
		st := d.store.GetStats()
		snap.TotalEvents = st.TotalEvents
		snap.StorageSize = st.StorageSize
		snap.DataDir = st.DataDir
		if q, ok := d.store.(interface{ WalQuarantined() int64 }); ok {
			snap.WALQuarantined = q.WalQuarantined()
		}
	}
	return snap
}

// indexHealth 计算索引健康率：成功索引 / (成功 + 丢弃 + 嵌入错误)。无数据返回 1.0（健康）。
func indexHealth(indexed, dropped, embedErr int64) float64 {
	total := indexed + dropped + embedErr
	if total == 0 {
		return 1.0
	}
	return float64(indexed) / float64(total)
}

package memory

// ==================== 维度锚定诊断（T-D · 记忆健康度）====================
//
// 借鉴 MemoHarness「维度锚定诊断」：记忆健康度按维度度量（向量索引健康、召回能力、
// 巩固收据完整性、存储规模），可查询（memory_health 工具 / 可观测投影）。
//
// 设计：读引擎 + store 的**实时状态**（非平行计数器，避免与真实状态分叉）——引擎的
// Stats（indexed/dropped/embedErr/vectorCount/dimMismatch）+ Capabilities + store 的
// GetStats。派生健康率供 LLM/运维判断记忆子系统是否退化。

// DiagnosticsSnapshot 是记忆健康度的维度快照（JSON 可序列化，供工具/可观测消费）。
type DiagnosticsSnapshot struct {
	// 向量索引维度
	VectorIndexed    int64 `json:"vector_indexed"`     // 成功索引的向量数
	VectorCount      int64 `json:"vector_count"`       // 当前索引中的向量数
	VectorDropped    int64 `json:"vector_dropped"`     // 队列满/API 失败丢弃数
	VectorEmbedErr   int64 `json:"vector_embed_err"`   // 嵌入错误数
	VectorDimMismatch int64 `json:"vector_dim_mismatch"` // 维度不匹配跳过数（换模型信号）

	// 检索能力维度
	CapKeyword bool `json:"cap_keyword"`
	CapVector  bool `json:"cap_vector"`
	CapHybrid  bool `json:"cap_hybrid"`
	EngineReady bool `json:"engine_ready"`

	// 存储规模维度
	TotalEvents int    `json:"total_events"`
	StorageSize int64  `json:"storage_size"`
	DataDir     string `json:"data_dir,omitempty"`

	// 派生健康率
	IndexHealth float64 `json:"index_health"` // indexed / (indexed + dropped + embedErr)，1.0 = 无丢失
}

// MemoryDiagnostics 维度锚定记忆诊断器（读引擎 + store 实时态）。
type MemoryDiagnostics struct {
	engine MemoryEngine // 可选（nil = 无向量维度）
	store  MemoryStore  // 可选（nil = 无存储维度）
}

// NewMemoryDiagnostics 构建诊断器。engine/store 可为 nil（对应维度省略）。
func NewMemoryDiagnostics(engine MemoryEngine, store MemoryStore) *MemoryDiagnostics {
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
		// 引擎若暴露细粒度统计（InMemoryEngine.Stats），读取向量维度。
		if st, ok := d.engine.(interface {
			Stats() (indexed, dropped, embedErr, vectorCount int64)
		}); ok {
			indexed, dropped, embedErr, vectorCount := st.Stats()
			snap.VectorIndexed = indexed
			snap.VectorDropped = dropped
			snap.VectorEmbedErr = embedErr
			snap.VectorCount = vectorCount
			snap.IndexHealth = indexHealth(indexed, dropped, embedErr)
		}
		// 维度不匹配计数（换模型信号）——可选接口。
		if dm, ok := d.engine.(interface{ DimMismatch() int64 }); ok {
			snap.VectorDimMismatch = dm.DimMismatch()
		}
	}
	if d.store != nil {
		st := d.store.GetStats()
		snap.TotalEvents = st.TotalEvents
		snap.StorageSize = st.StorageSize
		snap.DataDir = st.DataDir
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

// DimMismatch 暴露维度不匹配计数（InMemoryEngine 实现，供诊断读取）。
func (e *InMemoryEngine) DimMismatch() int64 { return e.dimMismatchCount.Load() }

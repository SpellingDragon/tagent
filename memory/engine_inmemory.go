package memory

import (
	"context"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/SpellingDragon/tagent/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// ==================== InMemoryEngine（T-A · MVP 兜底引擎）====================
//
// MemoryEngine 的轻量内存实现（契约 C6）：无外部依赖，供开发/测试/降级。
//   - 关键词路：委托 store.QueryEvents（复用既有 term-split 分词 + 时间窗 + 分区）。
//   - 向量路：自有内存余弦索引（eventKey→向量）；Index 经非阻塞队列后台异步嵌入。
//   - 融合：RRF(k=60) 在引擎内闭环；分区/类型/时间过滤在融合前 applied（防跨分区泄漏）。
//   - 降级：emb==nil 或向量未就绪 → 纯关键词（行为与现状一致，永不报错）。
//
// MVP 局限（有意）：向量索引内存态、不持久化，重启后旧事件向量丢失（关键词路仍工作，
// 因 store 持久）。持久向量后端是 RustVikingEngine 的职责（T-A 后续件）。

// EngineConfig 配置记忆引擎行为（零值取默认）。
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
	// MaxTextRunes 嵌入文本截断上限（默认 8000；仅嵌入侧截断，不动 FullEvent.Content）。
	MaxTextRunes int
	// KV 是向量持久化后端（nil = 纯内存，向量不持久，重启丢失）。设置后：worker
	// flush 时序列化向量入 KV，构造时异步从 KV 重建（rustviking-backed 持久化，
	// 见 engine_persist.go 与 f1-rustviking-capability-report.md「追加发现」）。
	KV KVStore
	// VecKeyPrefix 是向量 KV 键前缀（默认 "tagent:vec:"）。
	VecKeyPrefix string
	// DrainTimeout 是 Close 时排空在途嵌入批的超时（默认 2s）——用独立不取消的
	// ctx，避免合规嵌入器（尊重 ctx 取消）在排空期必然失败丢向量（审查 M1）。
	DrainTimeout time.Duration
	// RebuildWaitTimeout 是 Close 等待 KV 重建 goroutine 的上限（默认 3s），
	// 防 KV 后端挂住导致 Close 永久阻塞（审查 S6）。
	RebuildWaitTimeout time.Duration
}

func (c EngineConfig) withDefaults() EngineConfig {
	if c.VectorTopK <= 0 {
		c.VectorTopK = 20
	}
	if c.KeywordTopK <= 0 {
		c.KeywordTopK = 20
	}
	if c.RRFK <= 0 {
		c.RRFK = 60
	}
	if c.Overfetch <= 0 {
		c.Overfetch = 3
	}
	if c.QueueCap <= 0 {
		c.QueueCap = 256
	}
	if c.EmbedBatch <= 0 {
		c.EmbedBatch = 16
	}
	if c.EmbedFlushInterval <= 0 {
		c.EmbedFlushInterval = 200 * time.Millisecond
	}
	if c.MaxTextRunes <= 0 {
		c.MaxTextRunes = 8000
	}
	if c.VecKeyPrefix == "" {
		c.VecKeyPrefix = "tagent:vec:"
	}
	if c.DrainTimeout <= 0 {
		c.DrainTimeout = 2 * time.Second
	}
	if c.RebuildWaitTimeout <= 0 {
		c.RebuildWaitTimeout = 3 * time.Second
	}
	return c
}

type vectorMeta struct {
	partitionID int
	eventType   string
	timestamp   int64
}

// InMemoryEngine 是 MemoryEngine 的内存 MVP 实现。
type InMemoryEngine struct {
	store MemoryStore // 关键词路（可为 nil = 纯向量，无关键词）
	emb   Embedder    // 向量路（可为 nil = 纯关键词降级）
	cfg   EngineConfig

	mu      sync.RWMutex
	vectors map[int64][]float32
	vmeta   map[int64]vectorMeta

	queue     chan IndexableEvent
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once
	closed    atomic.Bool
	started   atomic.Bool

	// KV 持久层（可选，见 engine_persist.go）。
	kv          KVStore
	vecPrefix   string
	rebuildDone atomic.Bool
	rebuildCh   chan struct{} // 重建完成信号（与 worker wg 分离，Close 有界等待，审查 S6）

	// 指标（可观测，T-B/组8 消费）。
	indexedCount     atomic.Int64 // 成功写入向量数
	droppedCount     atomic.Int64 // 队列满/API 失败丢弃数
	embedErrCount    atomic.Int64
	dimMismatchCount atomic.Int64 // 维度不匹配跳过数（换模型/维度后旧向量，审查 M3）
}

// 编译期锁定 C6。
var _ MemoryEngine = (*InMemoryEngine)(nil)

// NewInMemoryEngine 构建并启动 MVP 引擎。store 供关键词路（可 nil），emb 供向量路
// （nil = 纯关键词降级）。后台嵌入 worker 随引擎启动，Close 时排空停止。
func NewInMemoryEngine(store MemoryStore, emb Embedder, cfg EngineConfig) *InMemoryEngine {
	cfg = cfg.withDefaults()
	e := &InMemoryEngine{
		store:     store,
		emb:       emb,
		cfg:       cfg,
		vectors:   make(map[int64][]float32),
		vmeta:     make(map[int64]vectorMeta),
		kv:        cfg.KV,
		vecPrefix: cfg.VecKeyPrefix,
	}
	e.rebuildDone.Store(true) // 默认无重建
	e.rebuildCh = make(chan struct{})
	if emb != nil {
		e.queue = make(chan IndexableEvent, e.cfg.QueueCap)
		ctx, cancel := context.WithCancel(context.Background())
		e.cancel = cancel
		e.wg.Add(1)
		go e.embedWorker(ctx)
	}
	// KV 持久层：启动异步重建（独立 goroutine，不入 worker wg——Close 有界等待，
	// 防 KV 后端挂住永久阻塞 Close，审查 S6）。重建期 Ready=false → 检索退化关键词。
	if e.kv != nil {
		e.rebuildDone.Store(false)
		go func() {
			defer close(e.rebuildCh)
			e.rebuildFromKV()
		}()
	} else {
		close(e.rebuildCh)
	}
	e.started.Store(true)
	return e
}

// Index 将事件纳入索引。关键词路由 store 在查询时处理（无需在此建索引）；
// 向量路：选择性（event 注册表 Embeddable）+ 非阻塞投递后台嵌入队列。
// MUST NOT 阻塞或失败主链路——队列满则丢弃 + 计数。
func (e *InMemoryEngine) Index(_ context.Context, evt IndexableEvent) error {
	if e.closed.Load() || e.emb == nil || e.queue == nil {
		return nil // 纯关键词降级：无需向量索引
	}
	if evt.EventKey <= 0 {
		return nil // 负 key（合成投影引用）不索引
	}
	if !event.IsEmbeddableType(evt.EventType) {
		return nil // 选择性生成：仅注册表标记 Embeddable 的类型
	}
	select {
	case e.queue <- evt:
	default:
		e.droppedCount.Add(1) // 队列满：丢弃 + 计数，不背压
	}
	return nil
}

// Remove 从向量索引移除（TTL/墓碑回收）。内存引擎即时删除（无悬挂问题）。
func (e *InMemoryEngine) Remove(_ context.Context, eventKey int64) error {
	e.mu.Lock()
	delete(e.vectors, eventKey)
	delete(e.vmeta, eventKey)
	e.mu.Unlock()
	e.removePersisted(eventKey) // 同步删 KV 持久向量，防重建复活已删事件
	return nil
}

// Retrieve 混合检索：关键词 ∪ 向量 → RRF 融合 → 排序票据。
// 向量不可用（无 emb/未就绪/查询空）时退化为纯关键词；store 为 nil 时纯向量。
func (e *InMemoryEngine) Retrieve(ctx context.Context, q RetrievalQuery) ([]RetrievalHit, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 10
	}
	mode := q.Mode
	if mode == ModeAuto {
		if e.vectorAvailable() && q.Query != "" {
			mode = ModeHybrid
		} else {
			mode = ModeKeyword
		}
	}

	// 纯过滤/浏览（无查询词）或显式关键词：走 store 关键词路。
	if q.Query == "" || mode == ModeKeyword {
		return e.keywordRetrieve(q, limit), nil
	}

	// 候选容量：超取补偿 ∪ 配置 topK 下限（审查 S1：vector_top_k/keyword_top_k 生效）。
	kwCap := max(limit*e.cfg.Overfetch, e.cfg.KeywordTopK)
	vecCap := max(limit*e.cfg.Overfetch, e.cfg.VectorTopK)

	vecKeys := e.vectorKeys(ctx, q, vecCap)

	// 显式向量模式：有结果则返回；无则按 C6 契约退化为关键词（审查 S2）。
	if mode == ModeVector {
		if len(vecKeys) > 0 {
			return e.toHits(vecKeys, limit), nil
		}
		return e.keywordRetrieve(q, limit), nil
	}

	// hybrid：向量零命中 → 纯关键词（不浪费关键词扫描）；否则 RRF 融合。
	if len(vecKeys) == 0 {
		return e.keywordRetrieve(q, limit), nil
	}
	kwKeys := e.keywordKeys(ctx, q, kwCap)
	fused := rrfFuse([][]int64{kwKeys, vecKeys}, e.cfg.RRFK)
	return e.toHits(fused, limit), nil
}

// Capabilities 声明能力：关键词取决于 store，向量取决于 emb 且已就绪。
func (e *InMemoryEngine) Capabilities() RetrievalCaps {
	vec := e.vectorAvailable()
	return RetrievalCaps{
		Keyword: e.store != nil,
		Vector:  vec,
		Hybrid:  e.store != nil && vec,
	}
}

// Ready 引擎是否就绪：已启动、未关闭、且 KV 重建完成（若有 KV）。重建完成前为
// false → 向量路 vectorAvailable() 返回 false，Retrieve 退化为关键词（对齐 C6
// 契约「重启重建完成前 Ready=false」，审查 S7）。
func (e *InMemoryEngine) Ready() bool {
	return e.started.Load() && !e.closed.Load() && e.rebuildDone.Load()
}

// Close 停止后台 worker（排空在途批，用独立不取消 ctx，审查 M1）并有界等待 KV 重建
// goroutine（审查 S6：防 KV 后端挂住导致 Close 永久阻塞）。幂等。
func (e *InMemoryEngine) Close() error {
	e.closeOnce.Do(func() {
		e.closed.Store(true)
		if e.cancel != nil {
			e.cancel() // 通知 worker 进入排空
		}
		e.wg.Wait() // 等 worker 排空退出（排空有 DrainTimeout 上界）
		if e.rebuildCh != nil {
			select {
			case <-e.rebuildCh:
			case <-time.After(e.cfg.RebuildWaitTimeout):
				log.Warnf("[InMemoryEngine] Close: KV 重建未在 %v 内完成，放弃等待", e.cfg.RebuildWaitTimeout)
			}
		}
	})
	return nil
}

// Stats 返回引擎运行指标（可观测/诊断用）。
func (e *InMemoryEngine) Stats() (indexed, dropped, embedErr, vectorCount int64) {
	e.mu.RLock()
	vc := int64(len(e.vectors))
	e.mu.RUnlock()
	return e.indexedCount.Load(), e.droppedCount.Load(), e.embedErrCount.Load(), vc
}

// SearchByVector 实现 RawVectorSearcher：用预计算查询向量做余弦 topK（分区过滤），
// 供 engineBridge.SearchByEmbedding 委托（消灭 MemoryStore 的向量 stub）。
func (e *InMemoryEngine) SearchByVector(_ context.Context, query []float32, topK int, partitionIDs []int) ([]RetrievalHit, error) {
	if len(query) == 0 {
		return nil, nil
	}
	type scored struct {
		key   int64
		score float32
	}
	e.mu.RLock()
	cands := make([]scored, 0, len(e.vectors))
	for key, vec := range e.vectors {
		if key <= 0 {
			continue
		}
		if !matchPartition(partitionIDs, e.vmeta[key].partitionID) {
			continue
		}
		if len(vec) != len(query) {
			e.dimMismatchCount.Add(1) // 维度不匹配跳过（审查 M3）
			continue
		}
		cands = append(cands, scored{key: key, score: cosine(query, vec)})
	}
	e.mu.RUnlock()

	sort.Slice(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	if topK > 0 && len(cands) > topK {
		cands = cands[:topK]
	}
	hits := make([]RetrievalHit, len(cands))
	for i, c := range cands {
		hits[i] = RetrievalHit{EventKey: c.key, Score: c.score}
	}
	return hits, nil
}

// 编译期锁定可选能力。
var _ RawVectorSearcher = (*InMemoryEngine)(nil)

// ---------------------------------------------------------------------------
// 内部：向量路
// ---------------------------------------------------------------------------

func (e *InMemoryEngine) vectorAvailable() bool {
	if e.emb == nil || !e.Ready() {
		return false
	}
	e.mu.RLock()
	n := len(e.vectors)
	e.mu.RUnlock()
	return n > 0
}

// embedWorker 后台批量嵌入：聚合队列事件，批量调 embedder，写向量索引。
func (e *InMemoryEngine) embedWorker(ctx context.Context) {
	defer e.wg.Done()
	batch := make([]IndexableEvent, 0, e.cfg.EmbedBatch)
	flush := func(fctx context.Context) {
		if len(batch) == 0 {
			return
		}
		texts := make([]string, len(batch))
		for i, evt := range batch {
			texts[i] = e.textForIndex(evt.Text)
		}
		vecs, err := e.emb.Embed(fctx, texts)
		if err != nil {
			e.embedErrCount.Add(int64(len(batch)))
			e.droppedCount.Add(int64(len(batch)))
			log.Warnf("[InMemoryEngine] embed batch failed (%d events): %v", len(batch), err)
			batch = batch[:0]
			return
		}
		e.mu.Lock()
		for i, evt := range batch {
			if i < len(vecs) && len(vecs[i]) > 0 {
				e.vectors[evt.EventKey] = vecs[i]
				e.vmeta[evt.EventKey] = vectorMeta{
					partitionID: evt.PartitionID,
					eventType:   evt.EventType,
					timestamp:   evt.Timestamp,
				}
				e.indexedCount.Add(1)
			}
		}
		e.mu.Unlock()
		e.persistVectors(batch, vecs) // KV 持久层（kv==nil 时 no-op）
		batch = batch[:0]
	}

	ticker := time.NewTicker(e.cfg.EmbedFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// 排空剩余队列（审查 M1）：用独立、不继承取消、带超时的 ctx，否则合规
			// 嵌入器（尊重 ctx 取消）在排空期必然失败，丢在途向量且不持久化。
			drainCtx, cancelDrain := context.WithTimeout(context.WithoutCancel(ctx), e.cfg.DrainTimeout)
			defer cancelDrain()
			for {
				select {
				case evt := <-e.queue:
					batch = append(batch, evt)
					if len(batch) >= e.cfg.EmbedBatch {
						flush(drainCtx)
					}
				default:
					flush(drainCtx)
					return
				}
			}
		case evt := <-e.queue:
			batch = append(batch, evt)
			if len(batch) >= e.cfg.EmbedBatch {
				flush(ctx)
			}
		case <-ticker.C:
			flush(ctx)
		}
	}
}

func (e *InMemoryEngine) textForIndex(text string) string {
	if e.cfg.MaxTextRunes > 0 && utf8.RuneCountInString(text) > e.cfg.MaxTextRunes {
		r := []rune(text)
		return string(r[:e.cfg.MaxTextRunes])
	}
	return text
}

// vectorKeys 嵌入查询 → 余弦 topK → 分区/类型/时间过滤 → 排序 EventKey。
func (e *InMemoryEngine) vectorKeys(ctx context.Context, q RetrievalQuery, k int) []int64 {
	if e.emb == nil || q.Query == "" {
		return nil
	}
	qvecs, err := e.emb.Embed(ctx, []string{e.textForIndex(q.Query)})
	if err != nil || len(qvecs) == 0 || len(qvecs[0]) == 0 {
		e.embedErrCount.Add(1)
		return nil // 嵌入失败 → 向量路空，hybrid 退化为关键词（不报错）
	}
	qv := qvecs[0]

	type scored struct {
		key   int64
		score float32
	}
	e.mu.RLock()
	cands := make([]scored, 0, len(e.vectors))
	for key, vec := range e.vectors {
		if key <= 0 {
			continue // 负 key 防御
		}
		meta := e.vmeta[key]
		if !matchPartition(q.PartitionIDs, meta.partitionID) {
			continue // 跨分区泄漏防线
		}
		if !matchEventType(q.EventTypes, meta.eventType) {
			continue
		}
		if !matchTimeRange(q.StartTime, q.EndTime, meta.timestamp) {
			continue
		}
		if len(vec) != len(qv) {
			e.dimMismatchCount.Add(1) // 维度不匹配（换模型/维度后旧向量）：跳过，不收 0 分候选（审查 M3）
			continue
		}
		cands = append(cands, scored{key: key, score: cosine(qv, vec)})
	}
	e.mu.RUnlock()

	sort.Slice(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	if k > 0 && len(cands) > k {
		cands = cands[:k]
	}
	keys := make([]int64, len(cands))
	for i, c := range cands {
		keys[i] = c.key
	}
	return keys
}

// ---------------------------------------------------------------------------
// 内部：关键词路（委托 store.QueryEvents）
// ---------------------------------------------------------------------------

func (e *InMemoryEngine) keywordRetrieve(q RetrievalQuery, limit int) []RetrievalHit {
	if e.store == nil {
		return nil
	}
	refs, err := e.store.QueryEvents(e.toQueryOptions(q, limit))
	if err != nil {
		log.Warnf("[InMemoryEngine] keyword QueryEvents failed: %v", err)
		return nil
	}
	hits := make([]RetrievalHit, 0, len(refs))
	for _, r := range refs {
		hits = append(hits, RetrievalHit{EventKey: r.EventKey})
	}
	return hits
}

func (e *InMemoryEngine) keywordKeys(ctx context.Context, q RetrievalQuery, k int) []int64 {
	if e.store == nil {
		return nil
	}
	_ = ctx
	refs, err := e.store.QueryEvents(e.toQueryOptions(q, k))
	if err != nil {
		log.Warnf("[InMemoryEngine] keyword QueryEvents failed: %v", err)
		return nil
	}
	keys := make([]int64, 0, len(refs))
	for _, r := range refs {
		keys = append(keys, r.EventKey) // QueryEvents 已按 OrderBy（timestamp_desc）排序 = 关键词路排名
	}
	// 语义警示（审查 S3）：关键词腿按 timestamp_desc 排名（非相关度），故 RRF 融合结果
	// 在关键词侧偏向新近事件。向量腿为余弦相关度秩。这是 MVP 的已知取舍——相关度重排
	// （token 重叠/Jaccard）列为后续增强；当前融合仍优于纯关键词（向量腿提供语义信号）。
	return keys
}

func (e *InMemoryEngine) toQueryOptions(q RetrievalQuery, limit int) QueryOptions {
	return QueryOptions{
		PartitionIDs: q.PartitionIDs,
		EventTypes:   q.EventTypes,
		StartTime:    q.StartTime,
		EndTime:      q.EndTime,
		Limit:        limit,
		OrderBy:      "timestamp_desc",
		Keyword:      q.Query,
	}
}

func (e *InMemoryEngine) toHits(keys []int64, limit int) []RetrievalHit {
	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}
	hits := make([]RetrievalHit, len(keys))
	for i, k := range keys {
		hits[i] = RetrievalHit{EventKey: k}
	}
	return hits
}

// ---------------------------------------------------------------------------
// 融合与过滤助手
// ---------------------------------------------------------------------------

// rrfFuse 倒数排名融合：score(d) = Σ_lists 1/(k + rank_d)，rank 从 1 起。
// 返回按融合分降序的 EventKey（去重）。
func rrfFuse(lists [][]int64, k int) []int64 {
	if k <= 0 {
		k = 60
	}
	scores := make(map[int64]float64)
	for _, list := range lists {
		for rank, key := range list {
			scores[key] += 1.0 / float64(k+rank+1)
		}
	}
	type kv struct {
		key   int64
		score float64
	}
	out := make([]kv, 0, len(scores))
	for key, s := range scores {
		out = append(out, kv{key, s})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].key > out[j].key // 同分决胜：新 key（Snowflake 时间有序）优先
	})
	keys := make([]int64, len(out))
	for i, e := range out {
		keys[i] = e.key
	}
	return keys
}

// cosine 余弦相似度（不假设已归一化）。
func cosine(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

func matchPartition(whitelist []int, pid int) bool {
	if len(whitelist) == 0 {
		return true
	}
	for _, p := range whitelist {
		if p == pid {
			return true
		}
	}
	return false
}

func matchEventType(filter []string, et string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, f := range filter {
		if f == et {
			return true
		}
	}
	return false
}

func matchTimeRange(start, end, ts int64) bool {
	if start > 0 && ts < start {
		return false
	}
	if end > 0 && ts > end {
		return false
	}
	return true
}

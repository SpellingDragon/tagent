package memory

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// ==================== engineBridge（T-A · 引擎接线装饰器）====================
//
// 记忆引擎的接线采用 MemoryStore 装饰器（报告 D2 决策，契约 C2）：在
// resolveMemoryStore 一处包裹，天然覆盖全部写入路径（插件管线 OnEvent +
// persistBusEvent），无需逐路径显式 Index（易漏）。装饰器保证：未接线时 inner
// 行为逐字节不变；接线后写入旁路索引、检索经引擎 hybrid。
//
// 契约 C2 装饰器顺序：ErrorTrackingStore( engineBridge( FileSegmentStore ) )——
// 错误追踪（T-G）在最外层，引擎桥在中间，事件存储在内层。

// MemoryEngineProvider 是可选接口：装饰器据此暴露其记忆引擎。
// 仿 RelationStoreProvider——recall/插件经类型断言获取引擎，未接线时断言失败即降级
// 为纯关键词（现状行为）。这是 tagent 核心与引擎实现之间的解耦触点。
type MemoryEngineProvider interface {
	MemoryEngine() MemoryEngine
}

// KVProvider 是可选接口：MemoryStore 实现若持有底层 KVStore（如 FileSegmentStore），
// 据此暴露给记忆引擎做向量持久化（T-A：序列化向量入 KV + 启动重建，跨重启恢复语义召回）。
type KVProvider interface {
	KVBackend() KVStore
}

// engineBridge 装饰 MemoryStore，桥接「写入 → 引擎索引」并暴露引擎供检索。
type engineBridge struct {
	inner  MemoryStore
	engine MemoryEngine
}

// 编译期锁定：engineBridge 是 MemoryStore + MemoryEngineProvider（+ 尽力 RelationStoreProvider）。
var (
	_ MemoryStore          = (*engineBridge)(nil)
	_ MemoryEngineProvider = (*engineBridge)(nil)
)

// NewEngineBridge 用引擎包裹 store。engine 的关键词路应指向 inner（构造引擎时传入），
// 使 hybrid 的关键词分支复用既有 QueryEvents。
func NewEngineBridge(inner MemoryStore, engine MemoryEngine) MemoryStore {
	return &engineBridge{inner: inner, engine: engine}
}

// StoreEvent 先写 inner（同步点语义不变，不变量5），成功后旁路投递引擎索引。
// 引擎 Index 内部保证非阻塞、失败不传染——写入主链路永不因索引失败而失败。
func (b *engineBridge) StoreEvent(key int64, event FullEvent) error {
	if err := b.inner.StoreEvent(key, event); err != nil {
		return err
	}
	if b.engine != nil {
		if err := b.engine.Index(context.Background(), IndexableEvent{
			EventKey:    key,
			PartitionID: event.PartitionID,
			EventType:   event.EventType,
			Text:        textForIndex(event),
			Timestamp:   event.Timestamp,
		}); err != nil {
			// Index 契约上不失败主链路；此处仅记录（引擎内部已计数）。
			log.Debugf("[engineBridge] index enqueue key=%d: %v", key, err)
		}
	}
	return nil
}

// StoreEventWithEmbedding 调用方自带向量：委托 inner 存储；引擎侧此路径不经
// 异步嵌入（MVP 引擎忽略外部向量，保持文本嵌入一致性）。
func (b *engineBridge) StoreEventWithEmbedding(key int64, event FullEvent, embedding []float32) error {
	return b.inner.StoreEventWithEmbedding(key, event, embedding)
}

// DeleteEvent 删 inner，成功后尽力从引擎移除（防悬挂召回）。
func (b *engineBridge) DeleteEvent(key int64) error {
	err := b.inner.DeleteEvent(key)
	if err == nil && b.engine != nil {
		_ = b.engine.Remove(context.Background(), key)
	}
	return err
}

// SearchByEmbedding 委托引擎的向量路（消灭 stub）：原始查询向量 → 引擎向量检索 →
// 水合为 EventReference。引擎不支持向量时退回 inner（现状 stub 行为）。
func (b *engineBridge) SearchByEmbedding(query []float32, topK int) ([]EventReference, error) {
	if b.engine == nil || !b.engine.Capabilities().Vector {
		return b.inner.SearchByEmbedding(query, topK)
	}
	vs, ok := b.engine.(RawVectorSearcher)
	if !ok {
		return b.inner.SearchByEmbedding(query, topK)
	}
	hits, err := vs.SearchByVector(context.Background(), query, topK, nil)
	if err != nil {
		return b.inner.SearchByEmbedding(query, topK)
	}
	keys := make([]int64, 0, len(hits))
	for _, h := range hits {
		keys = append(keys, h.EventKey)
	}
	events, err := b.inner.GetEvents(keys)
	if err != nil {
		return nil, err
	}
	refs := make([]EventReference, 0, len(events))
	for _, e := range events {
		refs = append(refs, EventReference{
			EventKey:     e.EventKey,
			PartitionID:  e.PartitionID,
			EventType:    e.EventType,
			EventSummary: e.EventSummary,
			Timestamp:    e.Timestamp,
		})
	}
	return refs, nil
}

// SupportsVectorSearch 反映引擎向量能力（而非 inner 的 stub）。
func (b *engineBridge) SupportsVectorSearch() bool {
	return b.engine != nil && b.engine.Capabilities().Vector
}

// MemoryEngine 暴露引擎（MemoryEngineProvider）。
func (b *engineBridge) MemoryEngine() MemoryEngine { return b.engine }

// Close 关闭引擎（停后台嵌入 worker）并委托 inner 的 Close（若可关闭，如
// FileSegmentStore 的持久化 flush）。满足 agent.Closer——resolveMemoryStore 的
// 既有清理路径（memStore.(agent.Closer)）据此在 agent 关闭时回收引擎，防 goroutine 泄漏。
// 共享 store 场景下引擎按 path 共享，Close 幂等（引擎内部 closeOnce）。
func (b *engineBridge) Close() error {
	var err error
	if b.engine != nil {
		err = b.engine.Close()
	}
	if c, ok := b.inner.(interface{ Close() error }); ok {
		if ierr := c.Close(); ierr != nil && err == nil {
			err = ierr
		}
	}
	return err
}

// === 只读/管理方法：全部委托 inner ===

func (b *engineBridge) GetEvent(key int64) (*FullEvent, error) { return b.inner.GetEvent(key) }
func (b *engineBridge) GetEvents(keys []int64) ([]FullEvent, error) {
	return b.inner.GetEvents(keys)
}
func (b *engineBridge) QueryEvents(query QueryOptions) ([]EventReference, error) {
	return b.inner.QueryEvents(query)
}
func (b *engineBridge) GetStats() StoreStats { return b.inner.GetStats() }

// RelationStore 透传（保持 RelationStoreProvider 能力，recall 因果链依赖）。
func (b *engineBridge) RelationStore() RelationStore {
	if rsp, ok := b.inner.(RelationStoreProvider); ok {
		return rsp.RelationStore()
	}
	return nil
}

// textForIndex 构造嵌入/索引文本：优先 Content，回退 EventSummary。
// 截断由引擎内部 textForIndex（MaxTextRunes）处理，此处不截断（不动 FullEvent.Content）。
func textForIndex(event FullEvent) string {
	if event.Content != "" {
		return event.Content
	}
	return event.EventSummary
}

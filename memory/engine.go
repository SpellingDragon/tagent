package memory

import (
	"context"
	"io"
)

// IndexableEvent 是投递给引擎纳入索引的事件视图。
// 引擎不依赖 FullEvent 全貌——只取索引所需的字段，保持缝的最小面。
type IndexableEvent struct {
	EventKey    int64
	PartitionID int
	EventType   string
	Text        string
	Timestamp   int64
}

// RetrievalMode 声明检索模式。引擎 Capabilities 决定各模式是否可用；
// 请求不可用模式时引擎 MUST 优雅退化（Vector/Hybrid 无向量 → Keyword）。
type RetrievalMode int

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

// RetrievalQuery 是检索请求。引擎内部决定 keyword/vector/hybrid 与融合排序。
type RetrievalQuery struct {
	Query        string
	PartitionIDs []int
	EventTypes   []string
	StartTime    int64
	EndTime      int64
	Limit        int
	Mode         RetrievalMode
}

// RetrievalHit 是融合排序后的单条命中票据。
// 只含 EventKey + Score —— 全文/引用由调用方经 MemoryStore 水合（两段式）。
type RetrievalHit struct {
	EventKey int64
	Score    float32
}

// RetrievalCaps 声明引擎的检索能力，供上层优雅降级与可观测。
type RetrievalCaps struct {
	Keyword bool
	Vector  bool
	Hybrid  bool
}

// IndexBuilder 索引构建面：记忆引擎据此把事件纳入索引。
// 闭环在引擎内部——tagent 只投递 IndexableEvent，不管引擎如何嵌入/存储/分层。
//
// 实现纪律：
// - Index MUST 异步或快速返回，绝不阻塞事件主链路（不变量：StoreEvent 同步点）。
// 典型实现：非阻塞投递到耐用队列/通道，后台 worker 嵌入 + 写向量索引。
// - Index 失败 MUST NOT 传染调用方（记日志 + 计数即可；向量是增强索引，丢一条
// 只影响该条语义可召回性，关键词路径兜底）。
// - Remove 用于 TTL/墓碑回收；引擎可惰性处理（水合过滤 + 超取 + 阈值重建）。
type IndexBuilder interface {
	// Index 将一个事件纳入索引（引擎内部决定嵌入/向量存储/分层/选择性）。
	Index(ctx context.Context, evt IndexableEvent) error
	// Remove 从索引移除一个事件（TTL/墓碑回收时调用；引擎可惰性处理）。
	Remove(ctx context.Context, eventKey int64) error
}

// Retriever 检索面：记忆引擎据此召回排序票据。
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

// MemoryEngine = 索引构建 + 检索 + 生命周期。这是 tagent 核心依赖的解耦缝。
//
// 实现：
// - InMemoryEngine（MVP 兜底）：内存向量索引 + 关键词，无外部依赖，供开发/测试/降级。
// - RustVikingEngine（适配器，闭环到 rustviking）：tagent 侧 zhipu 嵌入 +
// rustviking index insert/search/delete 向量后端 + 适配器内 RRF 融合与分区过滤。
//
// 生命周期：随 MemoryStore 启停（Closer 接线，resolveMemoryStore 按配置创建）。
type MemoryEngine interface {
	IndexBuilder
	Retriever
	io.Closer
}

// RawVectorSearcher 是可选引擎能力：支持「预计算查询向量」检索（供
// MemoryStore.SearchByEmbedding 委托，消灭 stub）。文本查询走 Retriever.Retrieve；
// 本接口服务于已持有查询向量的调用方。partitionIDs 非空时过滤（nil = 不限）。
type RawVectorSearcher interface {
	SearchByVector(ctx context.Context, query []float32, topK int, partitionIDs []int) ([]RetrievalHit, error)
}

// MemoryEngineProvider 是可选接口：装饰器据此暴露其记忆引擎。
// 仿 RelationStoreProvider——recall/插件经类型断言获取引擎，未接线时断言失败即降级
// 为纯关键词（现状行为）。这是 tagent 核心与引擎实现之间的解耦触点。
// （实现位于子包 memory/engine 的 engineBridge。）
type MemoryEngineProvider interface {
	MemoryEngine() MemoryEngine
}

// KVProvider 是可选接口：MemoryStore 实现若持有底层 KVStore（如 FileSegmentStore），
// 据此暴露给记忆引擎做向量持久化（T-A：序列化向量入 KV + 启动重建，跨重启恢复语义召回）。
type KVProvider interface {
	KVBackend() KVStore
}

// CapacityHookProvider 是可选接口：装饰器在每次
// StoreEvent 成功后旁路调用回调（eventKey, partitionID, eventType），供巩固容量触发计数。
// 实现位于子包 memory/engine 的 engineBridge。回调 MUST 非阻塞、不得失败主链路。
type CapacityHookProvider interface {
	SetCapacityHook(fn func(eventKey int64, partitionID int, eventType string))
}

// VectorRemover 由持有向量索引的组件实现；FileSegmentStore 在 TTL/容量遗忘**物理删除**
// 事件时（Compactor.finalizeTombstones）回调，使引擎同步移除向量（内存索引 + KV 持久键），
// 防死键堆积与重启复活。
type VectorRemover interface {
	RemoveVector(eventKey int64)
}

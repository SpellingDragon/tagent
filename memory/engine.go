package memory

import (
	"context"
	"io"
)

// ==================== 记忆引擎解耦缝（F2 · 契约 C6）====================
//
// 设计意图（agent 与记忆引擎解耦）：tagent 核心只约定「索引构建」与「检索」两个
// 抽象接口，具体实现（嵌入生成、向量存储、hybrid 融合、分层索引）闭环在记忆引擎
// 内部。tagent 自带 InMemoryEngine 为轻量 MVP（兜底/开发/测试），生产经
// RustVikingEngine 适配器闭环到 rustviking（HNSW/IVF 向量后端 + tagent 侧嵌入 +
// 适配器内 RRF 融合与分区过滤，见 f1-rustviking-capability-report.md 裁决）。
//
// 与 MemoryStore 的关系（关键，防误解耦）：
//   - MemoryStore 仍是事件溯源的唯一事实源（不可变 FullEvent，只增不改）。
//   - MemoryEngine 是旁路的「索引 + 检索」能力面，只持有 EventKey → 向量/关键词
//     的派生投影；它不是事实源，可随时重建。
//   - 检索返回排序后的 EventKey 票据（RetrievalHit），全文/引用取回仍走
//     MemoryStore.GetEvents/GetEvent —— 两段式（语义发现 → 票据精确取回），零幻觉。
//   - 引擎故障绝不传染事件主链路：Index 失败只记日志 + 计数（失败以 result 渗透），
//     Retrieve 在索引未就绪时优雅退化为关键词（现状行为）。
//
// 依赖方向：MemoryEngine 的实现可持有 MemoryStore（读关键词/水合），这是记忆子系统
// 内部的向下依赖；tagent 核心（agent/tool/recall）只依赖 MemoryEngine 接口，不感知
// 底层是 rustviking 还是 MVP —— 这正是解耦缝所在。
//
// 冻结纪律：本文件的接口签名即契约 C6。变更须走 execution-dag.md §4.2 ESCALATE。
// 策展/诊断钩子（T-D）经可选接口（如 DiagnosticsProvider）挂接，不进本核心面。

// IndexableEvent 是投递给引擎纳入索引的事件视图。
// 引擎不依赖 FullEvent 全貌——只取索引所需的字段，保持缝的最小面。
type IndexableEvent struct {
	EventKey    int64  // Snowflake EventKey（正 key；引擎用作向量 id = uint64(EventKey)）
	PartitionID int    // 存储分区（跨分区泄漏防线：引擎检索 MUST 按此过滤）
	EventType   string // 事件类型（引擎据配置做选择性索引）
	Text        string // 嵌入/关键词索引的文本载荷（构造与截断策略见实现层 textForIndex）
	Timestamp   int64  // Unix 毫秒（语义时间轴，供时间范围过滤）
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
	Query        string        // 自然语言查询（空 = 纯过滤/浏览，退化为 QueryEvents 语义）
	PartitionIDs []int         // 分区白名单（跨分区泄漏防线，引擎 MUST 遵守；空 = 不限）
	EventTypes   []string      // 类型过滤（空 = 不限）
	StartTime    int64         // Unix 毫秒下界；0 = 无下界
	EndTime      int64         // Unix 毫秒上界；0 = 无上界
	Limit        int           // 返回上限（<=0 由实现取默认）
	Mode         RetrievalMode // 检索模式（默认 ModeAuto）
}

// RetrievalHit 是融合排序后的单条命中票据。
// 只含 EventKey + Score —— 全文/引用由调用方经 MemoryStore 水合（两段式）。
type RetrievalHit struct {
	EventKey int64   // 命中事件的 Snowflake EventKey（召回票据）
	Score    float32 // 融合分（RRF）或原始相关分；供诊断/阈值，不用于对外承诺量纲
}

// RetrievalCaps 声明引擎的检索能力，供上层优雅降级与可观测。
type RetrievalCaps struct {
	Keyword bool // 支持关键词检索
	Vector  bool // 支持向量检索
	Hybrid  bool // 支持关键词 ∪ 向量融合
}

// IndexBuilder 索引构建面：记忆引擎据此把事件纳入索引。
// 闭环在引擎内部——tagent 只投递 IndexableEvent，不管引擎如何嵌入/存储/分层。
//
// 实现纪律：
//   - Index MUST 异步或快速返回，绝不阻塞事件主链路（不变量：StoreEvent 同步点）。
//     典型实现：非阻塞投递到耐用队列/通道，后台 worker 嵌入 + 写向量索引。
//   - Index 失败 MUST NOT 传染调用方（记日志 + 计数即可；向量是增强索引，丢一条
//     只影响该条语义可召回性，关键词路径兜底）。
//   - Remove 用于 TTL/墓碑回收；引擎可惰性处理（水合过滤 + 超取 + 阈值重建）。
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
	// 索引未就绪（Ready()==false）时 MUST 退化为关键词而非报错。
	Retrieve(ctx context.Context, q RetrievalQuery) ([]RetrievalHit, error)
	// Capabilities 声明引擎支持的检索模式（供上层优雅降级与可观测）。
	Capabilities() RetrievalCaps
	// Ready 索引是否就绪（重启重建完成前为 false，Retrieve 退化为关键词）。
	Ready() bool
}

// MemoryEngine = 索引构建 + 检索 + 生命周期。这是 tagent 核心依赖的解耦缝（C6）。
//
// 实现：
//   - InMemoryEngine（MVP 兜底）：内存向量索引 + 关键词，无外部依赖，供开发/测试/降级。
//   - RustVikingEngine（适配器，闭环到 rustviking）：tagent 侧 zhipu 嵌入 +
//     rustviking index insert/search/delete 向量后端 + 适配器内 RRF 融合与分区过滤。
//
// 生命周期：随 MemoryStore 启停（Closer 接线，resolveMemoryStore 按配置创建）。
type MemoryEngine interface {
	IndexBuilder
	Retriever
	io.Closer
}

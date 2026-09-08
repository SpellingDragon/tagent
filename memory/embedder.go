package memory

import "context"

// ==================== Embedder 契约（分包：契约居核心，实现居 memory/embedder/）====================
//
// Embedder 是文本→向量的抽象。按 kv.go 同款分包原则：**契约居核心包**
// （消费方 InMemoryEngine 所在的 memory/engine/ 与组合根依赖本接口），
// **实现居子包 memory/embedder/**（mock / zhipu / traced 及未来新供应商）。
//
// 接入指南（新增嵌入供应商）：
//  1. 在 memory/embedder/ 新建 <provider>.go，实现 memory.Embedder 三方法
//    （Embed 批量语义：返回与 texts 等长、顺序对应；Dimension 0=未知；
//     ModelID 用于索引指纹比对，防换模型后向量混用）；
//  2. 在组合根 buildEmbedder（tagent.go）的 provider switch 加 case；
//  3. 无 key/未配置时返回 error，调用方按「功能关闭」优雅降级（关键词兜底）。
//
// 裁决依据：f1-rustviking-capability-report.md DECIDED F1-③（嵌入用 tagent 侧
// HTTP 供应商，非 rustviking CLI）。

// Embedder 文本向量化。批量语义：返回与 texts 等长、顺序对应的向量切片。
type Embedder interface {
	// Embed 批量嵌入。实现 MUST 尊重 ctx 取消/超时。
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Dimension 返回向量维度；0 = 未知（尚未探测）。
	Dimension() int
	// ModelID 返回嵌入模型标识（用于索引指纹比对，防换模型后向量混用）。
	ModelID() string
}

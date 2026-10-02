package memory

import "context"

// Embedder 是文本向量化抽象：契约居核心包，实现居子包 memory/embedder（mock／zhipu／traced）。
// 批量语义要求返回与输入等长、顺序对应；未配置时返回 error 由调用方按"功能关闭"降级。
// 接入新供应商的步骤与一条已裁决事项（嵌入走 tagent 侧 HTTP 供应商而非 rustviking CLI）见文档。
//
// 契约: docs/wiki/memory/memory-architecture.md#embedder
type Embedder interface {
	// Embed 批量嵌入。实现 MUST 尊重 ctx 取消/超时。
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Dimension 返回向量维度；0 = 未知（尚未探测）。
	Dimension() int
	// ModelID 返回嵌入模型标识（用于索引指纹比对，防换模型后向量混用）。
	ModelID() string
}

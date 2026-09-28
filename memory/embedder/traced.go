package embedder

import (
	"context"

	"github.com/SpellingDragon/tagent/memory"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	semconv "trpc.group/trpc-go/trpc-agent-go/telemetry/semconv/trace"
)

const (
	embedMeterName = "github.com/SpellingDragon/tagent/memory"
	embedSpanName  = "tagent.embeddings"
)

// TracedEmbedder 装饰任意 Embedder，为每次嵌入产生 span（对齐上游 GenAI 语义约定）并记录
// 调用数／文本条数／维度分布。两条不变量：可观测只在装饰器内部产生，工具与引擎的 Declaration
// 零触碰（否则每次加可观测都会扰动模型可见声明、破坏 prefix-cache 稳定性）；未配置导出时全局
// provider 为 noop，本装饰器仅透传、行为逐字不变。属性只带元数据，嵌入内容不入 span。
//
// 契约: docs/wiki/memory/memory-architecture.md#embedder
type TracedEmbedder struct {
	inner memory.Embedder
	// calls 计嵌入 API 调用次数。
	calls metric.Int64Counter
	// texts 计嵌入文本总条数。
	texts metric.Int64Counter
	// dims 记向量维度分布。
	dims metric.Int64Histogram
}

// NewTracedEmbedder 包裹 inner 加向量链路可观测。inner 为 nil 返回 nil。metric 创建失败
// 用 noop 计数（otel 保证返回可用零值，不阻断）。
func NewTracedEmbedder(inner memory.Embedder) *TracedEmbedder {
	if inner == nil {
		return nil
	}
	m := otel.Meter(embedMeterName)
	calls, _ := m.Int64Counter("tagent.embedding.calls", metric.WithDescription("embedding API 调用数"))
	texts, _ := m.Int64Counter("tagent.embedding.texts", metric.WithDescription("嵌入文本总条数"))
	dims, _ := m.Int64Histogram("tagent.embedding.dimension", metric.WithDescription("向量维度分布"))
	return &TracedEmbedder{inner: inner, calls: calls, texts: texts, dims: dims}
}

// Embed 开 span（GenAI 属性）→ 委托 inner → 记 metric。ctx 取消/超时透传 inner（尊重 ctx）。
func (t *TracedEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	ctx, span := otel.Tracer(embedMeterName).Start(ctx, embedSpanName)
	defer span.End()
	span.SetAttributes(
		attribute.String("gen_ai.request.model", t.inner.ModelID()),
		attribute.Int("gen_ai.embeddings.request.text_count", len(texts)),
	)
	vecs, err := t.inner.Embed(ctx, texts)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "embedding failed")
		return nil, err
	}
	if len(vecs) > 0 {
		dim := len(vecs[0])
		span.SetAttributes(attribute.Int(semconv.KeyGenAIEmbeddingsDimensionCount, dim))
		t.dims.Record(ctx, int64(dim))
	}
	t.calls.Add(ctx, 1)
	t.texts.Add(ctx, int64(len(texts)))
	return vecs, nil
}

// Dimension 透传 inner（span/metric 不改变维度语义）。
func (t *TracedEmbedder) Dimension() int { return t.inner.Dimension() }

// ModelID 透传 inner（索引指纹比对不受装饰影响）。
func (t *TracedEmbedder) ModelID() string { return t.inner.ModelID() }

var _ memory.Embedder = (*TracedEmbedder)(nil)

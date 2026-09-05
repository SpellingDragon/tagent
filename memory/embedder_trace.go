package memory

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	semconv "trpc.group/trpc-go/trpc-agent-go/telemetry/semconv/trace"
)

// ==================== TracedEmbedder（T-A 组8 · 向量链路可观测）====================
//
// 装饰任意 Embedder，为 Embed 调用产生 span（对齐上游 GenAI 语义约定：gen_ai.embeddings.
// dimension.count / gen_ai.request.model）+ 记录 embedding 调用数/文本条数/维度分布 metric。
//
// 组8.3 声明区守卫：全部可观测位于 embedder 内部（Worker/store 侧），MCP/recall 工具的
// Declaration 零触碰——prefix-cache 稳定性不变量不受影响。noop 安全：未设
// OTEL_EXPORTER_OTLP_ENDPOINT 时 otel 全局为 noop provider，span/metric 零开销、
// Embed 行为逐字节不变（仅透传 inner）。

const (
	embedMeterName = "github.com/SpellingDragon/tagent/memory"
	embedSpanName  = "tagent.embeddings"
)

// TracedEmbedder 是带 span/metric 的 Embedder 装饰器（实现 Embedder 接口）。
type TracedEmbedder struct {
	inner Embedder
	calls metric.Int64Counter   // embedding API 调用数
	texts metric.Int64Counter   // 嵌入文本总条数
	dims  metric.Int64Histogram // 向量维度分布
}

// NewTracedEmbedder 包裹 inner 加向量链路可观测。inner 为 nil 返回 nil。metric 创建失败
// 用 noop 计数（otel 保证返回可用零值，不阻断）。
func NewTracedEmbedder(inner Embedder) *TracedEmbedder {
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

// 编译期确认 TracedEmbedder 实现 Embedder 接口。
var _ Embedder = (*TracedEmbedder)(nil)

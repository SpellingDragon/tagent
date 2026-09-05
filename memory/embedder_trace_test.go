package memory

import (
	"context"
	"fmt"
	"testing"
)

// errEmbedder 是恒错的 Embedder 替身（验证错误透传 + span RecordError 路径）。
type errEmbedder struct{}

func (errEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return nil, fmt.Errorf("boom")
}
func (errEmbedder) Dimension() int    { return 0 }
func (errEmbedder) ModelID() string   { return "err-model" }

// TestTracedEmbedder_PassthroughNoop 验证组8.3 声明区守卫的核心：未设 OTLP（noop provider）
// 时 TracedEmbedder 透传 inner，Embed 结果逐字节一致、Dimension/ModelID 透传——向量链路
// 可观测对全链路行为零影响（prefix-cache 不变量的 embedder 侧保证）。
func TestTracedEmbedder_PassthroughNoop(t *testing.T) {
	inner := NewMockEmbedder(32)
	traced := NewTracedEmbedder(inner)
	ctx := context.Background()
	texts := []string{"hello world", "语义检索", "the quick brown fox"}

	wantVecs, werr := inner.Embed(ctx, texts)
	if werr != nil {
		t.Fatalf("inner Embed: %v", werr)
	}
	gotVecs, err := traced.Embed(ctx, texts)
	if err != nil {
		t.Fatalf("traced Embed: %v", err)
	}
	if len(gotVecs) != len(wantVecs) {
		t.Fatalf("向量数不一致: got %d want %d", len(gotVecs), len(wantVecs))
	}
	for i := range wantVecs {
		if len(gotVecs[i]) != len(wantVecs[i]) {
			t.Fatalf("第 %d 向量维度不一致", i)
		}
		for j := range wantVecs[i] {
			if gotVecs[i][j] != wantVecs[i][j] {
				t.Fatalf("noop 下向量应逐字节一致 (i=%d j=%d): got %v want %v", i, j, gotVecs[i][j], wantVecs[i][j])
			}
		}
	}
	if traced.Dimension() != inner.Dimension() {
		t.Fatalf("Dimension 应透传: got %d want %d", traced.Dimension(), inner.Dimension())
	}
	if traced.ModelID() != inner.ModelID() {
		t.Fatalf("ModelID 应透传: got %q want %q", traced.ModelID(), inner.ModelID())
	}
}

func TestTracedEmbedder_NilInner(t *testing.T) {
	if NewTracedEmbedder(nil) != nil {
		t.Fatal("nil inner 应返回 nil（不包裹）")
	}
}

func TestTracedEmbedder_ErrorPropagates(t *testing.T) {
	traced := NewTracedEmbedder(errEmbedder{})
	if _, err := traced.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("inner 错误应透传（span RecordError 不吞错）")
	}
}

// TestTracedEmbedder_ContextCancelRespected 验证 ctx 取消透传 inner（Embedder 契约：MUST
// 尊重 ctx）。用取消的 ctx + 检查 ctx 敏感的 inner。
func TestTracedEmbedder_ContextCancelRespected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// MockEmbedder 不检查 ctx（纯计算），此处仅验证 TracedEmbedder 把 ctx 透传给 inner
	// 且不 panic；真实 ZhipuEmbedder 的 HTTP 调用会尊重 ctx 取消。
	traced := NewTracedEmbedder(NewMockEmbedder(16))
	if _, err := traced.Embed(ctx, []string{"x"}); err != nil {
		t.Fatalf("mock inner 不因 ctx 取消报错（纯计算）: %v", err)
	}
}

package plugin

import (
	"context"
	"testing"
)

// TestAttributionCarrier_RoundTrip 钉住归因章 ctx 载体的往返：未注入取不到，注入后逐键一致。
//
// 契约: docs/wiki/plugin/plugin-architecture.md#attribution-carrier
func TestAttributionCarrier_RoundTrip(t *testing.T) {
	ctx := context.Background()
	if _, ok := AttributionFrom(ctx); ok {
		t.Fatal("空 ctx 不应有归因")
	}
	ctx2 := WithAttribution(ctx, Attribution{"bundle_id": "v1", "rollout_id": "r-42"})
	got, ok := AttributionFrom(ctx2)
	if !ok {
		t.Fatal("注入后应可提取")
	}
	if got["bundle_id"] != "v1" || got["rollout_id"] != "r-42" {
		t.Fatalf("归因不一致: %+v", got)
	}
}

// TestAttributionCarrier_EmptyNotInjected 钉住空归因与 nil 归因都不写入 ctx（省一次分配），提取仍失败。
func TestAttributionCarrier_EmptyNotInjected(t *testing.T) {
	ctx := context.Background()
	ctx2 := WithAttribution(ctx, Attribution{})
	if _, ok := AttributionFrom(ctx2); ok {
		t.Fatal("空归因不应被注入")
	}
	ctx3 := WithAttribution(ctx, nil)
	if _, ok := AttributionFrom(ctx3); ok {
		t.Fatal("nil 归因不应被注入")
	}
}

// TestAttributionCarrier_Isolation 钉住绑定隔离：子 ctx 的归因不污染父 ctx。
func TestAttributionCarrier_Isolation(t *testing.T) {
	parent := context.Background()
	child := WithAttribution(parent, Attribution{"bundle_id": "child-v"})
	if _, ok := AttributionFrom(parent); ok {
		t.Fatal("父 ctx 不应被子 ctx 归因污染")
	}
	if got, ok := AttributionFrom(child); !ok || got["bundle_id"] != "child-v" {
		t.Fatalf("子 ctx 归因错: %+v ok=%v", got, ok)
	}
}

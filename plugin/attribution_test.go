package plugin

import (
	"context"
	"testing"
)

func TestAttributionCarrier_RoundTrip(t *testing.T) {
	ctx := context.Background()
	// 未注入 → 提取失败。
	if _, ok := AttributionFrom(ctx); ok {
		t.Fatal("空 ctx 不应有归因")
	}
	// 注入 → 提取一致。
	ctx2 := WithAttribution(ctx, Attribution{"bundle_id": "v1", "rollout_id": "r-42"})
	got, ok := AttributionFrom(ctx2)
	if !ok {
		t.Fatal("注入后应可提取")
	}
	if got["bundle_id"] != "v1" || got["rollout_id"] != "r-42" {
		t.Fatalf("归因不一致: %+v", got)
	}
}

func TestAttributionCarrier_EmptyNotInjected(t *testing.T) {
	ctx := context.Background()
	// 空归因不注入（省分配）——提取仍失败。
	ctx2 := WithAttribution(ctx, Attribution{})
	if _, ok := AttributionFrom(ctx2); ok {
		t.Fatal("空归因不应被注入")
	}
	ctx3 := WithAttribution(ctx, nil)
	if _, ok := AttributionFrom(ctx3); ok {
		t.Fatal("nil 归因不应被注入")
	}
}

func TestAttributionCarrier_Isolation(t *testing.T) {
	// 子 ctx 的归因不污染父 ctx（每回合绑定隔离，主循环与子 agent 天然分离）。
	parent := context.Background()
	child := WithAttribution(parent, Attribution{"bundle_id": "child-v"})
	if _, ok := AttributionFrom(parent); ok {
		t.Fatal("父 ctx 不应被子 ctx 归因污染")
	}
	if got, ok := AttributionFrom(child); !ok || got["bundle_id"] != "child-v" {
		t.Fatalf("子 ctx 归因错: %+v ok=%v", got, ok)
	}
}

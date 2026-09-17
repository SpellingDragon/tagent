package plugin

import (
	"context"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// durableInboundEvent builds a user-role event as the runner pipeline would
// deliver the invocation's input message.
func durableInboundEvent(content string) *event.Event {
	return &event.Event{Response: &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleUser, Content: content}}},
	}}
}

// TestMemoryPlugin_PrePersisted_InputSkipped（cold-eyes R2 M-1）：FactsPrePersisted
// turn 的 user 输入由 event loop persistBusEvent 逐消息落库——管线必须跳过，
// 不得再产生「合并事实」（否则与逐消息事实重复或吞碰撞）。
func TestMemoryPlugin_PrePersisted_InputSkipped(t *testing.T) {
	store := memory.NewInMemoryStore()
	mp := NewMemoryPlugin(store)

	ctx := WithDurableInbound(context.Background(), DurableInbound{
		Path: "/tmp/env-1.json", RequestID: "req-1", FactsPrePersisted: true,
	})
	if _, err := mp.OnEvent(ctx, nil, durableInboundEvent("hello durable")); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	if got := store.GetStats().TotalEvents; got != 0 {
		t.Fatalf("pre-persisted input must NOT be stored again by the pipeline, got %d facts", got)
	}
}

// TestMemoryPlugin_PrePersisted_AssistantStillStored：同一 turn 的 LLM 产出
// （assistant）不受跳过影响——事实链仍记录回复。
func TestMemoryPlugin_PrePersisted_AssistantStillStored(t *testing.T) {
	store := memory.NewInMemoryStore()
	mp := NewMemoryPlugin(store)

	ctx := WithDurableInbound(context.Background(), DurableInbound{
		Path: "/tmp/env-1.json", RequestID: "req-1", FactsPrePersisted: true,
	})
	assistantEvt := &event.Event{Response: &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "reply"}}},
	}}
	if _, err := mp.OnEvent(ctx, nil, assistantEvt); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	if store.GetStats().TotalEvents != 1 {
		t.Fatalf("assistant output must still be stored, got %d", store.GetStats().TotalEvents)
	}
}

// TestMemoryPlugin_NoDurableInbound_Unchanged：无 durable provenance 时行为
// 不变（纯 NewSnowflakeKey 入库）。
func TestMemoryPlugin_NoDurableInbound_Unchanged(t *testing.T) {
	store := memory.NewInMemoryStore()
	mp := NewMemoryPlugin(store)
	if _, err := mp.OnEvent(context.Background(), nil, durableInboundEvent("plain")); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}
	if store.GetStats().TotalEvents != 1 {
		t.Fatalf("plain path must still store, got %d", store.GetStats().TotalEvents)
	}
}

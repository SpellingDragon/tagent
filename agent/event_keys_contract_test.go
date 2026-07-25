package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/agent/compress"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/agent"
)

// TestEventKeys_EndToEndContract is the CONTRACT test that was missing when
// the hex migration silently broke event_keys: each stage (timeline
// rendering, declaration hint, argument parsing, store lookup) was green in
// isolation while the seams disagreed. This test chains all four stages the
// way a model actually experiences them:
//
//	render [evt_HEX|type] → model copies HEX → Call(event_keys) → parent
//	store resolves the SAME events → external_context carries them
func TestEventKeys_EndToEndContract(t *testing.T) {
	store := memory.NewInMemoryStore()
	k1, k2 := int64(0x1201a3f4b5c01), int64(0x1201a3f4b5c02)
	if err := store.StoreEvent(k1, memory.FullEvent{EventKey: k1, EventType: "external_input", EventSummary: "部署请求", Content: "请部署 v2 到测试环境"}); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreEvent(k2, memory.FullEvent{EventKey: k2, EventType: "agent_output", EventSummary: "部署完成", Content: "v2 已部署,健康检查通过"}); err != nil {
		t.Fatal(err)
	}

	// STAGE 1 — timeline rendering: what the model actually SEES.
	line1 := tagentevent.FormatEventPrefix(k1, "external_input") + " 部署请求"
	line2 := tagentevent.FormatEventPrefix(k2, "agent_output") + " 部署完成"

	// STAGE 2 — the model copies keys out of the rendered lines verbatim
	// (exactly what the declaration hint tells it to do).
	extract := func(line string) string {
		inner := line[strings.Index(line, "[evt_")+len("[evt_") : strings.Index(line, "]")]
		return inner[:strings.Index(inner, "|")]
	}
	modelArgs := map[string]any{
		"request":    "分析这两次部署",
		"event_keys": []any{extract(line1), extract(line2)},
	}
	raw, _ := json.Marshal(modelArgs)

	// STAGE 3+4 — Call parses the copied keys and resolves them from the
	// parent store into external_context.
	mock := &mockAgent{name: "analyzer"}
	w := NewAgentToolWrapper(mock, "analyze", []string{"event_keys"}, store)

	if _, err := w.Call(context.Background(), raw); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if mock.lastInv == nil {
		t.Fatal("sub-agent was not invoked")
	}
	rs := mock.lastInv.RunOptions.RuntimeState
	rawCtx, ok := rs[ExternalContextKey].(json.RawMessage)
	if !ok {
		t.Fatalf("external_context missing: model-copied hex keys were dropped (the silent-failure mode this test guards against); RuntimeState=%v", rs)
	}
	var entries []ExternalContextEntry
	if err := json.Unmarshal(rawCtx, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].EventKey != k1 || entries[1].EventKey != k2 {
		t.Fatalf("resolved events must be exactly the ones the model pointed at, got %+v", entries)
	}
	if entries[0].EventSummary != "部署请求" || entries[1].EventSummary != "部署完成" {
		t.Fatalf("summaries must round-trip, got %+v", entries)
	}
}

// TestEventKeys_AutoInjectFallback documents the masking behavior: when the
// model passes NO keys (or every key fails to parse), the wrapper silently
// auto-injects recent projection events instead of failing loudly. This is
// why the hex regression stayed invisible on live runs (18/18 calls logged
// event_keys=0 yet sub-agents "worked"). The fallback is intended UX, but it
// must stay documented as a masking layer for contract breaks.
func TestEventKeys_AutoInjectFallback(t *testing.T) {
	store := memory.NewInMemoryStore()
	k := int64(0x77aa01)
	_ = store.StoreEvent(k, memory.FullEvent{EventKey: k, EventType: "external_input", EventSummary: "近期事件"})

	proj := compress.NewSessionProjection()
	proj.Append(memory.EventReference{EventKey: k, EventType: "external_input", EventSummary: "近期事件"})

	mock := &mockAgent{name: "analyzer"}
	w := NewAgentToolWrapper(mock, "analyze", []string{"event_keys"}, store)
	w.SetParentProjection(proj)

	// Model passes garbage keys — parsing yields nothing, fallback kicks in.
	raw, _ := json.Marshal(map[string]any{"request": "x", "event_keys": []any{"totally-not-a-key"}})
	if _, err := w.Call(context.Background(), raw); err != nil {
		t.Fatalf("Call: %v", err)
	}
	rs := mock.lastInv.RunOptions.RuntimeState
	if _, ok := rs[ExternalContextKey]; !ok {
		t.Fatalf("auto-inject fallback should have supplied recent events")
	}
}

var _ = agent.Info{} // keep import for mockAgent signature

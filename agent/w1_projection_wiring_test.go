package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// leafTool is a non-delegation tool (exec/file/mcp shape). §6.5: the projection
// wiring must NOT touch it — it has no parentProjection field and is not an
// *AgentToolWrapper, so collectAgentToolWrappers must skip it without panicking.
type leafTool struct{ name string }

func (leafTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: "leaf"}
}
func (l leafTool) Call(_ context.Context, _ []byte) (any, error) { return l.name, nil }

// innerPeel is a test stand-in for the governance decorator: it exposes Inner()
// (not Unwrap()) so the collector's Inner branch is exercised without importing
// agent/governance (which would risk an import cycle). Wrap order under test
// mirrors production: OutputLimitTool(...Inner(...)...) over the real wrapper.
type innerPeel struct{ inner trpctool.Tool }

func (p innerPeel) Declaration() *trpctool.Declaration { return p.inner.Declaration() }
func (p innerPeel) Inner() trpctool.Tool               { return p.inner }
func (p innerPeel) Call(ctx context.Context, a []byte) (any, error) {
	return p.inner.(trpctool.CallableTool).Call(ctx, a)
}

// wireThroughTransparent mirrors the publish-time wiring that cold start performs
// (SetToolParentProjection): pierce the decorator chain and bind each reachable
// wrapper to proj. Using the same helper the production path uses means this test
// fails if the pierce is ever regressed to a bare `t.(*AgentToolWrapper)`
// assertion. §6.5/D2: invocation-private ContextManagers no longer wire through
// this path at all — they carry their own projection in the call context (see
// w1_concurrent_projection_test.go); these tests pin the published binding and
// the out-of-flow fallback that reads it.
func wireThroughTransparent(tools []trpctool.Tool, proj *compress.SessionProjection) {
	for _, w := range collectAgentToolWrappers(tools) {
		w.SetParentProjection(proj)
	}
}

func projOf(t *testing.T, store memory.MemoryStore, keys ...int64) *compress.SessionProjection {
	t.Helper()
	proj := compress.NewSessionProjection()
	for _, k := range keys {
		evt, err := store.GetEvent(k)
		require.NoError(t, err)
		proj.Append(memory.EventReference{
			EventKey:     k,
			EventType:    evt.EventType,
			EventSummary: evt.EventSummary,
		})
	}
	return proj
}

func injectedKeys(t *testing.T, mock *mockAgent) []int64 {
	t.Helper()
	require.NotNil(t, mock.lastInv, "sub-agent must have been invoked through the wrapper chain")
	rs := mock.lastInv.RunOptions.RuntimeState
	rawCtx, ok := rs[ExternalContextKey].(json.RawMessage)
	if !ok {
		return nil
	}
	var entries []ExternalContextEntry
	require.NoError(t, json.Unmarshal(rawCtx, &entries))
	var out []int64
	for _, e := range entries {
		out = append(out, e.EventKey)
	}
	return out
}

func storeEvent(t *testing.T, store memory.MemoryStore, k int64, summary string) {
	t.Helper()
	require.NoError(t, store.StoreEvent(k, memory.FullEvent{
		EventKey: k, EventType: "external_input", EventSummary: summary, Content: summary,
	}))
}

// TestW1_AutoInjectFiresThroughTransparentWrapper is the core §6.5/W-1 contract:
// after the projection is wired through the OutputLimitTool decorator, a REAL
// tool call that OMITS event_keys must still auto-inject the parent projection
// events. This is exactly what a bare type-assertion wiring silently breaks (the
// wrapper is hidden, parentProjection stays nil, auto-inject no-ops with no
// error) — the previous tests only asserted the pointer was set directly.
func TestW1_AutoInjectFiresThroughTransparentWrapper(t *testing.T) {
	store := memory.NewInMemoryStore()
	const k = int64(0x1201abcd00001)
	storeEvent(t, store, k, "近期部署事件")

	mock := &mockAgent{name: "analyzer"}
	w := NewAgentToolWrapper(mock, "analyze", []string{"event_keys"}, store)
	olt := NewOutputLimitTool(w, 1<<20) // production wraps every tool here

	// Wire through the transparent layer (what cold-start / candidate build do).
	wireThroughTransparent([]trpctool.Tool{olt}, projOf(t, store, k))

	// Model omits event_keys entirely → auto-inject must fire.
	raw, _ := json.Marshal(map[string]any{"request": "分析"})
	_, err := olt.Call(context.Background(), raw) // call THROUGH the decorator
	require.NoError(t, err)
	require.Equal(t, []int64{k}, injectedKeys(t, mock),
		"auto-inject must reach the wrapper hidden behind OutputLimitTool")
}

// TestW1_ExplicitKeysTakePriority confirms the fallback never overrides keys the
// model actually passed.
func TestW1_ExplicitKeysTakePriority(t *testing.T) {
	store := memory.NewInMemoryStore()
	const projK, explicitK = int64(0x1201abcd000aa), int64(0x1201abcd000bb)
	storeEvent(t, store, projK, "投影事件")
	storeEvent(t, store, explicitK, "模型指定事件")

	mock := &mockAgent{name: "analyzer"}
	w := NewAgentToolWrapper(mock, "analyze", []string{"event_keys"}, store)
	olt := NewOutputLimitTool(w, 1<<20)
	wireThroughTransparent([]trpctool.Tool{olt}, projOf(t, store, projK))

	raw, _ := json.Marshal(map[string]any{"request": "分析", "event_keys": []any{float64(explicitK)}})
	_, err := olt.Call(context.Background(), raw)
	require.NoError(t, err)
	require.Equal(t, []int64{explicitK}, injectedKeys(t, mock),
		"explicit keys must win; the projection fallback must not append")
}

// TestW1_EmptyProjectionIsGraceful: a wired wrapper with an empty projection and
// no model keys injects nothing and does not error.
func TestW1_EmptyProjectionIsGraceful(t *testing.T) {
	store := memory.NewInMemoryStore()
	mock := &mockAgent{name: "analyzer"}
	w := NewAgentToolWrapper(mock, "analyze", []string{"event_keys"}, store)
	olt := NewOutputLimitTool(w, 1<<20)
	wireThroughTransparent([]trpctool.Tool{olt}, projOf(t, store)) // zero refs

	raw, _ := json.Marshal(map[string]any{"request": "分析"})
	_, err := olt.Call(context.Background(), raw)
	require.NoError(t, err)
	require.Empty(t, injectedKeys(t, mock), "empty projection → no injected events")
}

// TestW1_NormalToolsUnaffected: a leaf tool (and its OutputLimitTool wrapper) is
// never collected, so wiring a mixed list touches only the delegation wrappers.
func TestW1_NormalToolsUnaffected(t *testing.T) {
	store := memory.NewInMemoryStore()
	mock := &mockAgent{name: "analyzer"}
	delegate := NewAgentToolWrapper(mock, "analyze", []string{"event_keys"}, store)
	leaf := NewOutputLimitTool(leafTool{name: "exec"}, 1<<20)

	got := collectAgentToolWrappers([]trpctool.Tool{leaf, NewOutputLimitTool(delegate, 1<<20)})
	require.Len(t, got, 1, "only the delegation wrapper is reachable; the leaf stays untouched")
	require.Same(t, delegate, got[0])
}

// TestW1_PiercesNestedDecorators: the collector walks multi-layer chains
// (OutputLimitTool → Inner-decorator → OutputLimitTool → wrapper).
func TestW1_PiercesNestedDecorators(t *testing.T) {
	store := memory.NewInMemoryStore()
	mock := &mockAgent{name: "analyzer"}
	w := NewAgentToolWrapper(mock, "analyze", []string{"event_keys"}, store)
	nested := NewOutputLimitTool(innerPeel{inner: NewOutputLimitTool(w, 1<<20)}, 1<<20)

	got := collectAgentToolWrappers([]trpctool.Tool{nested})
	require.Len(t, got, 1)
	require.Same(t, w, got[0], "must peel Unwrap and Inner layers alike")
}

// TestW1_CallIsolation verifies two sub-agent wrappers under two parents wired to
// two projections do not cross-contaminate: each real call injects only from its
// own bound projection (design §W-1: 本调用真正的 parentProjection).
func TestW1_CallIsolation(t *testing.T) {
	store1 := memory.NewInMemoryStore()
	store2 := memory.NewInMemoryStore()
	const k1, k2 = int64(0x1201abcd00f01), int64(0x1201abcd00f02)
	storeEvent(t, store1, k1, "parent-1 事件")
	storeEvent(t, store2, k2, "parent-2 事件")

	g1 := &mockAgent{name: "g1"}
	g2 := &mockAgent{name: "g2"}
	w1 := NewAgentToolWrapper(g1, "a1", []string{"event_keys"}, store1)
	w2 := NewAgentToolWrapper(g2, "a2", []string{"event_keys"}, store2)
	olt1 := NewOutputLimitTool(w1, 1<<20)
	olt2 := NewOutputLimitTool(w2, 1<<20)

	// Two independent wiring passes (two parents / two executions).
	wireThroughTransparent([]trpctool.Tool{olt1}, projOf(t, store1, k1))
	wireThroughTransparent([]trpctool.Tool{olt2}, projOf(t, store2, k2))

	raw, _ := json.Marshal(map[string]any{"request": "分析"})
	_, err := olt1.Call(context.Background(), raw)
	require.NoError(t, err)
	_, err = olt2.Call(context.Background(), raw)
	require.NoError(t, err)

	require.Equal(t, []int64{k1}, injectedKeys(t, g1), "g1 sees only parent-1's projection")
	require.Equal(t, []int64{k2}, injectedKeys(t, g2), "g2 sees only parent-2's projection")
}

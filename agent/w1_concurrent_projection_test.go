package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// introduce-durable-workflow-engine §6.5 reopened gap (W-1 isolation half):
// the delegation wrapper lives in ta.config.Tools and is therefore SHARED by
// every invocation-private ContextManager that the same agent builds. Binding
// that shared object per call (`buildExecutor` → SetParentProjection) both
// writes a field another in-flight call reads, and lets call A's event_keys
// auto-inject resolve against call B's projection. Design D2 forbids a
// call-private projection from being shared across calls; D5 forbids rebinding
// an already-published wrapper. This test drives TWO barrier-synchronized real
// concurrent invocations of one agent and requires each delegation to inject
// only from its OWN call's projection.
//
// Test-only peeks keep production API free of introspection surface.
func (cm *ContextManager) w1Projection() *compress.SessionProjection { return cm.projection }

func (w *AgentToolWrapper) w1PublishedProjection() *compress.SessionProjection {
	return w.parentProjection
}

// w1Gate parks one flow's model call inside the invocation window, which is the
// proof that that flow's private CM has already been constructed.
type w1Gate struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

// w1FlowModel scripts the parent: per flow (keyed by the driving user message),
// the FIRST model call signals entry, waits for release, then emits a
// delegation tool_call; every later call closes the turn.
type w1FlowModel struct {
	mu     sync.Mutex
	counts map[string]int
	gates  map[string]*w1Gate
	tool   string
}

// flow identifies which call a model request belongs to. The driving message
// reaches the model decorated by the memory layer ("[evt_<id>|external_input] A"),
// so the flow key is the message's LAST field, not the whole content.
func (m *w1FlowModel) flow(req *model.Request) (*w1Gate, string) {
	for _, msg := range req.Messages {
		if msg.Role != model.RoleUser {
			continue
		}
		fields := strings.Fields(msg.Content)
		if len(fields) == 0 {
			continue
		}
		name := fields[len(fields)-1]
		if g, ok := m.gates[name]; ok {
			return g, name
		}
	}
	return nil, ""
}

func (m *w1FlowModel) next(name string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counts[name]++
	return m.counts[name]
}

func (m *w1FlowModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	g, name := m.flow(req)
	if g == nil {
		ch <- m34FinalResp()
		close(ch)
		return ch, nil
	}
	if m.next(name) == 1 {
		g.once.Do(func() { close(g.entered) })
		select {
		case <-g.release:
		case <-ctx.Done():
			close(ch)
			return ch, ctx.Err()
		}
		ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
			Role: model.RoleAssistant,
			ToolCalls: []model.ToolCall{{ID: "w1-tc", Function: model.FunctionDefinitionParam{
				Name: m.tool, Arguments: []byte(`{"request":"do it"}`),
			}}},
		}}}}
		close(ch)
		return ch, nil
	}
	ch <- m34FinalResp()
	close(ch)
	return ch, nil
}

func (m *w1FlowModel) Info() model.Info { return model.Info{Name: "w1-flow"} }

// w1ChildAgent records, per delegation, which event_keys actually arrived.
type w1ChildAgent struct {
	name string
	mu   sync.Mutex
	runs [][]int64
}

func (c *w1ChildAgent) Run(_ context.Context, inv *trpcagent.Invocation) (<-chan *event.Event, error) {
	c.mu.Lock()
	c.runs = append(c.runs, w1InjectedKeys(inv))
	c.mu.Unlock()
	ch := make(chan *event.Event)
	close(ch)
	return ch, nil
}

func (c *w1ChildAgent) snapshot() [][]int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]int64, len(c.runs))
	copy(out, c.runs)
	return out
}

func (c *w1ChildAgent) Tools() []trpctool.Tool                { return nil }
func (c *w1ChildAgent) Info() trpcagent.Info                  { return trpcagent.Info{Name: c.name} }
func (c *w1ChildAgent) SubAgents() []trpcagent.Agent          { return nil }
func (c *w1ChildAgent) FindSubAgent(_ string) trpcagent.Agent { return nil }

func w1InjectedKeys(inv *trpcagent.Invocation) []int64 {
	rawCtx, ok := inv.RunOptions.RuntimeState[ExternalContextKey].(json.RawMessage)
	if !ok {
		return nil
	}
	var entries []ExternalContextEntry
	if err := json.Unmarshal(rawCtx, &entries); err != nil {
		return nil
	}
	var out []int64
	for _, e := range entries {
		out = append(out, e.EventKey)
	}
	return out
}

func w1WaitEntered(t *testing.T, g *w1Gate, who string) {
	t.Helper()
	select {
	case <-g.entered:
	case <-time.After(10 * time.Second):
		t.Fatalf("flow %s never reached its model call — harness broken", who)
	}
}

func w1WaitRuns(t *testing.T, c *w1ChildAgent, want int) {
	t.Helper()
	require.Eventually(t, func() bool { return len(c.snapshot()) >= want },
		10*time.Second, 10*time.Millisecond, "delegation did not reach the child")
}

func w1Has(keys []int64, want int64) bool {
	for _, k := range keys {
		if k == want {
			return true
		}
	}
	return false
}

// TestW1_ConcurrentCallsIsolateTheirProjections is the reopened-gap acceptance:
// two concurrent real invocations of the SAME agent (hence the SAME shared
// delegation wrapper) must each auto-inject only from their own call-private
// projection, and neither call may rewrite the published binding.
func TestW1_ConcurrentCallsIsolateTheirProjections(t *testing.T) {
	store := memory.NewInMemoryStore()
	const kA, kB = int64(0x1201abcd00e01), int64(0x1201abcd00e02)
	storeEvent(t, store, kA, "A 路事件")
	storeEvent(t, store, kB, "B 路事件")

	child := &w1ChildAgent{name: "worker"}
	delegate := NewAgentToolWrapper(child, "do the work", []string{"event_keys"}, store)

	gates := map[string]*w1Gate{
		"A": {entered: make(chan struct{}), release: make(chan struct{})},
		"B": {entered: make(chan struct{}), release: make(chan struct{})},
	}
	m := &w1FlowModel{counts: map[string]int{}, gates: gates, tool: "worker"}

	ta, err := NewTagentAgent(&TagentConfig{
		Model:             m,
		Name:              "w1parent",
		SystemPrompt:      "sp",
		MaxToolIterations: 5,
		MaxTokens:         8000,
		Tools:             []trpctool.Tool{delegate},
	})
	require.NoError(t, err)
	defer func() { _ = ta.Close() }()

	// Cold start (pre-publish) wires the resident binding, exactly as buildAgent
	// does. Everything after this point must leave that binding alone.
	ta.SetToolParentProjection()
	published := delegate.w1PublishedProjection()
	require.Same(t, ta.projection, published, "cold start binds the resident projection")

	// --- call A runs up to its model turn: its private CM now exists ---
	ctxA, cancelA := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelA()
	outA, err := ta.Run(ctxA, trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("A"))))
	require.NoError(t, err)
	w1WaitEntered(t, gates["A"], "A")
	require.Equal(t, 1, ta.LiveCMCount())
	projA := ta.snapshotLiveCMs()[0].w1Projection()
	projA.Append(memory.EventReference{EventKey: kA, EventType: "external_input", EventSummary: "A 路事件"})

	// --- call B, SAME agent, while A is still in flight ---
	ctxB, cancelB := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelB()
	outB, err := ta.Run(ctxB, trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("B"))))
	require.NoError(t, err)
	w1WaitEntered(t, gates["B"], "B")
	require.Equal(t, 2, ta.LiveCMCount(), "both calls are concurrently in flight")

	var projB *compress.SessionProjection
	for _, cm := range ta.snapshotLiveCMs() {
		if p := cm.w1Projection(); p != projA {
			projB = p
		}
	}
	require.NotNil(t, projB, "call B must own a distinct call-private projection")
	projB.Append(memory.EventReference{EventKey: kB, EventType: "external_input", EventSummary: "B 路事件"})

	// --- release A first: its delegation must see kA and NEVER kB ---
	close(gates["A"].release)
	w1WaitRuns(t, child, 1)
	gotA := child.snapshot()[0]
	require.True(t, w1Has(gotA, kA), "call A must auto-inject from its own projection, got %v", gotA)
	require.False(t, w1Has(gotA, kB), "call A must not see call B's projection key %v (got %v)", kB, gotA)

	// --- then release B: same requirement in the other direction ---
	close(gates["B"].release)
	w1WaitRuns(t, child, 2)
	gotB := child.snapshot()[1]
	require.True(t, w1Has(gotB, kB), "call B must auto-inject from its own projection, got %v", gotB)
	require.False(t, w1Has(gotB, kA), "call B must not see call A's projection key %v (got %v)", kA, gotB)

	for range outA {
	}
	for range outB {
	}
	require.Zero(t, ta.LiveCMCount(), "both registrations must be reclaimed")

	require.Same(t, published, delegate.w1PublishedProjection(),
		"a sub-call must never rewrite the published wrapper binding")
}

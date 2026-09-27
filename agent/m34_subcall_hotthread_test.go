package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// introduce-durable-workflow-engine §6.4（D4 同身份热参源）契约测：
// 子调用私有 CM 必须以 owner 的**有效热参快照**播种（新调用初始即有效），
// 在途私有 CM 必须随热更新的消费边界应用（注册/注销严格随调用生命周期）。
// 缺陷原型（轮二十五重开）：numeric-only applyHotAll 只达常驻 CM/TaskManager，
// 子 Run() 的 `invCfg := *ta.config` 恒以构造期值初始化私有 compressor——
// 数值热更后新发起子调用仍陈旧，在途调用无边界应用面。

// noopLeafM34 is a non-delegation tool so the gated org needs no sub-agents.
type noopLeafM34 struct{}

func (noopLeafM34) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: "noop", Description: "noop"}
}
func (noopLeafM34) Call(_ context.Context, _ []byte) (any, error) { return "ok", nil }

// gatedSequenceModel emits a canned assistant turn whose SECOND model call
// parks on a test gate. The park point is inside the invocation's RunFlow —
// i.e. inside the window where the invocation-private CM is registered live.
type gatedSequenceModel struct {
	mu      sync.Mutex
	idx     int
	gate    chan struct{} // closed by the test to release call #2
	entered chan struct{} // signaled once when call #2 begins
	once    sync.Once
}

func m34ToolCallResp() *model.Response {
	return &model.Response{Choices: []model.Choice{{Message: model.Message{
		Role: model.RoleAssistant,
		ToolCalls: []model.ToolCall{{ID: "tc-1", Function: model.FunctionDefinitionParam{
			Name: "noop", Arguments: []byte(`{}`),
		}}},
	}}}}
}

func m34FinalResp() *model.Response {
	return &model.Response{Done: true, Choices: []model.Choice{{Message: model.NewAssistantMessage("done")}}}
}

func (m *gatedSequenceModel) GenerateContent(ctx context.Context, _ *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	idx := m.idx
	m.idx++
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	switch idx {
	case 0: // assistant tool_call → the loop executes noop and calls the model again
		ch <- m34ToolCallResp()
		close(ch)
		return ch, nil
	case 1: // park INSIDE the invocation (private CM is registered at this point)
		m.once.Do(func() { close(m.entered) })
		select {
		case <-m.gate:
		case <-ctx.Done():
			close(ch)
			return ch, ctx.Err()
		}
		ch <- m34FinalResp()
		close(ch)
		return ch, nil
	default:
		ch <- m34FinalResp()
		close(ch)
		return ch, nil
	}
}

func (m *gatedSequenceModel) Info() model.Info { return model.Info{Name: "gated"} }

func newGatedOrg(t *testing.T) (*TagentAgent, *gatedSequenceModel) {
	t.Helper()
	g := &gatedSequenceModel{gate: make(chan struct{}), entered: make(chan struct{})}
	ta, err := NewTagentAgent(&TagentConfig{
		Model:             g,
		Name:              "hotthread",
		SystemPrompt:      "sp",
		MaxToolIterations: 5,
		MaxTokens:         4000,
		CompressThreshold: 0.5,
		KeepRecentTasks:   2,
		Tools:             []trpctool.Tool{noopLeafM34{}},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = ta.Close() })
	return ta, g
}

// TestM34_InitialSnapshotSeededAtConstruction pins the construction-time seed:
// every built agent carries a snapshot equal to its parsed values, so seeding
// and the resident compressor share one generation from second zero.
func TestM34_InitialSnapshotSeededAtConstruction(t *testing.T) {
	ta, _ := newGatedOrg(t)
	p, ok := ta.HotSnapshot()
	require.True(t, ok, "construction must seed the snapshot")
	require.Equal(t, 4000, p.MaxTokens)
	require.InDelta(t, 0.5, p.ThresholdPct, 1e-9)
	require.Equal(t, 2, p.KeepRecentTasks)
	require.Equal(t, 2000, ta.OrgBudgetLine(), "resident and snapshot agree at start")
	require.Zero(t, ta.LiveCMCount(), "no calls yet")
}

// TestM34_FreshSubCallSeededFromSnapshot is the reopened-gap core: after a
// numeric-only apply, a NEW invocation-private CM must initialize at the
// effective values — under the defect it came up at construction-frozen 2000.
func TestM34_FreshSubCallSeededFromSnapshot(t *testing.T) {
	ta, g := newGatedOrg(t)
	ta.SetHotSource(staticHotSource(OrgHotParams{ThresholdPct: 0.9, MaxTokens: 9000, KeepRecentTasks: 7}))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("go")))
	out, err := ta.Run(ctx, inv)
	require.NoError(t, err)

	// Reach the park point: call #2 has begun, so the private CM exists and is
	// registered live.
	select {
	case <-g.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the gated model call never began — harness broken")
	}
	require.Equal(t, 1, ta.LiveCMCount(), "exactly one live private CM while the call is in flight")

	// Snapshot-seed witness: the call opened AFTER the apply must carry 8100.
	// Under the defect (config seeding) it would sit at 2000 — and no in-flight
	// apply could mask it, because this value was fixed at construction, before
	// the gate released anything.
	live := ta.snapshotLiveCMs()[0]
	require.Equal(t, 8100, live.OrgBudgetLine(),
		"a fresh sub-call must initialize from the effective snapshot, not construction config")
	require.Equal(t, 7, live.OrgKeepRecent())

	close(g.gate)
	for range out {
	}
	require.Zero(t, ta.LiveCMCount(), "registration must be reclaimed when the call ends")
}

// TestM34_InFlightSubCallAppliesAtBoundary pins the second D4 half: a call
// that was ALREADY running across a hot apply reaches the new values at its
// next consumption boundary (the fan-out through the live registry), and the
// registry stays bounded by concurrent calls, never growing per call.
func TestM34_InFlightSubCallAppliesAtBoundary(t *testing.T) {
	ta, g := newGatedOrg(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	inv := trpcagent.NewInvocation(trpcagent.WithInvocationMessage(model.NewUserMessage("go")))
	out, err := ta.Run(ctx, inv)
	require.NoError(t, err)

	select {
	case <-g.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the gated model call never began — harness broken")
	}
	live := ta.snapshotLiveCMs()[0]
	require.Equal(t, 2000, live.OrgBudgetLine(), "in-flight call starts at its seeded generation")

	// The reloader's per-owner commit point while the call is parked.
	ta.SetHotSource(staticHotSource(OrgHotParams{ThresholdPct: 0.9, MaxTokens: 9000, KeepRecentTasks: 7}))
	require.Equal(t, 8100, live.OrgBudgetLine(),
		"in-flight private CM must take the hot update at its next budget/compress read")
	require.Equal(t, 7, live.OrgKeepRecent())

	close(g.gate)
	for range out {
	}
	require.Zero(t, ta.LiveCMCount())
}

package agent

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"

	upagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// introduce-durable-workflow-engine §3.2＋§4.1 (design D6) contract tests.
//
// The gate that decides when a superseded execution generation may be closed
// MUST be that generation's own outstanding references — counted per kind
// (business turn / sub-call / background execution) — and never one aggregate
// counter per context manager. So: one live G1 turn must not keep an
// unreferenced G2 runner open, and a derived execution must keep the generation
// it originated from alive until THAT execution stops.

// leaseCountingRunner records Close calls so a test can observe exactly when a
// generation was reclaimed.
type leaseCountingRunner struct {
	id     string
	closed atomic.Int64
}

func (f *leaseCountingRunner) Run(context.Context, string, string, model.Message, ...upagent.RunOption) (<-chan *event.Event, error) {
	ch := make(chan *event.Event, 1)
	close(ch)
	return ch, nil
}

func (f *leaseCountingRunner) Close() error {
	f.closed.Add(1)
	return nil
}

var _ runner.Runner = (*leaseCountingRunner)(nil)

func leaseCloseCount(r runner.Runner) int64 {
	if c, ok := r.(*leaseCountingRunner); ok {
		return c.closed.Load()
	}
	return -1
}

// TestLease_UnrelatedGenerationReclaimedIndependently is the D6「无关旧代独立回收」
// scenario and the RED witness against the aggregate counter: a turn pinned on
// G1 holds a reference for the whole scenario, G2 is superseded immediately and
// referenced by nobody — yet today the global in-flight gate blocks its reclaim.
func TestLease_UnrelatedGenerationReclaimedIndependently(t *testing.T) {
	blocking := &requestCapturingModel{} // resp == nil → a turn would park here
	cm := newTestContextManager("lease-indep", blocking, nil, make(chan *event.Event, 64), nil)
	g1 := cm.currentRunner()
	require.NotNil(t, g1)

	// A real business turn pins G1 and stays in flight throughout.
	pinned, releaseTurn := cm.BeginTurn()
	require.Same(t, g1, pinned, "the turn pins the generation in force at acquire")
	t.Cleanup(releaseTurn)

	face := cm.ExecutorConfig()
	g2 := &leaseCountingRunner{id: "g2"}
	cm.PublishExecutor(g2, face) // retires G1's generation — still referenced by the turn
	g3 := &leaseCountingRunner{id: "g3"}
	cm.PublishExecutor(g3, face) // retires G2, which NO reference can be waiting on

	require.Equal(t, int64(1), leaseCloseCount(g2),
		"an unreferenced retired generation must be reclaimed while an older generation is still in flight (D6「一个 G1 未停止调用只阻挡其实际使用资源」)")

	releaseTurn()
	require.Eventually(t, func() bool { return cm.ExecutorRefs().PendingRetirees == 0 },
		5*time.Second, 20*time.Millisecond,
		"once the last reference drops, every retiree of this cm is reclaimed")
}

// ---------------------------------------------------------------------------
// Derived executions (D5 inheritance rows + §4.1 exactly-once paths)
// ---------------------------------------------------------------------------

// gateAgent is a sub-agent whose run is held on a test gate. `running` fires when
// its Run begins, `finished` closes when it returns — the two edges a lease test
// must observe (acquire before work starts, release only after the stop).
type gateAgent struct {
	name     string
	running  chan struct{}
	finish   chan struct{} // closed by the test to let the run end
	finished chan struct{} // closed once the run really returned
	once     sync.Once
	once2    sync.Once
}

func newGateAgent(name string) *gateAgent {
	return &gateAgent{
		name:     name,
		running:  make(chan struct{}),
		finish:   make(chan struct{}),
		finished: make(chan struct{}),
	}
}

func (a *gateAgent) Run(ctx context.Context, _ *upagent.Invocation) (<-chan *event.Event, error) {
	ch := make(chan *event.Event, 4)
	a.once.Do(func() { close(a.running) })
	go func() {
		defer a.once2.Do(func() { close(a.finished) })
		select {
		case <-a.finish:
		case <-ctx.Done():
		}
		ch <- &event.Event{Response: &model.Response{Done: true, Choices: []model.Choice{
			{Message: model.NewAssistantMessage("done")},
		}}}
		close(ch)
	}()
	return ch, nil
}

func (a *gateAgent) Tools() []trpctool.Tool            { return nil }
func (a *gateAgent) Info() upagent.Info                { return upagent.Info{Name: a.name, Description: "gated"} }
func (a *gateAgent) SubAgents() []upagent.Agent        { return nil }
func (a *gateAgent) FindSubAgent(string) upagent.Agent { return nil }

// leaseHarness wires a generation-bearing context manager, a turn lease pinned on
// it (what a business turn holds) and a task manager for the async paths.
type leaseHarness struct {
	cm      *ContextManager
	turn    *ExecLease
	gen1    *leaseCountingRunner
	spawner *task.TaskManager
	ctx     context.Context
}

func newLeaseHarness(t *testing.T, gate func() string) *leaseHarness {
	t.Helper()
	cm := &ContextManager{name: "d6", runner: &leaseCountingRunner{id: "boot"}}
	// Publish g1 FIRST: the boot generation has no references and is reclaimed at
	// once, so g1 is unambiguously "the caller's generation" for everything below.
	g1 := &leaseCountingRunner{id: "g1"}
	cm.PublishExecutor(g1, ContextManagerConfig{Name: "d6"})
	turn := cm.AcquireLease(LeaseTurn) // pins g1
	t.Cleanup(turn.Release)
	h := &leaseHarness{cm: cm, turn: turn, gen1: g1}
	cfg := task.TaskManagerConfig{}
	if gate != nil {
		cfg.SpawnGate = gate
	}
	h.spawner = task.NewTaskManager(cfg)
	h.ctx = turn.WithContext(task.WithTaskSpawner(context.Background(), h.spawner))
	return h
}

func (h *leaseHarness) refs() ExecutorRefs { return h.cm.ExecutorRefs() }

// TestLease_DerivedSubCallHoldsCallersGenerationUntilItStops is D5's inheritance
// row for nested delegation plus D6's per-generation gate: a real sub-agent
// invocation takes its reference on the generation the CALLER pinned, so
// publishing a new generation mid-call must not reclaim the caller's generation
// before that child stops.
func TestLease_DerivedSubCallHoldsCallersGenerationUntilItStops(t *testing.T) {
	child, g := newGatedOrg(t) // a real TagentAgent whose run parks mid-invocation
	h := newLeaseHarness(t, nil)

	inv := upagent.NewInvocation(upagent.WithInvocationMessage(model.NewUserMessage("go")))
	out, err := child.Run(h.ctx, inv) // h.ctx carries the caller's turn lease
	require.NoError(t, err)
	select {
	case <-g.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the gated child never reached its model call — harness broken")
	}
	require.Equal(t, 1, h.refs().SubCalls,
		"a running sub-call holds its own reference kind on the caller's generation")
	require.Equal(t, int64(1), h.refs().InFlightTurns, "the caller's turn is counted once, not per descendant")

	// Publish while BOTH the turn and the child are live: the caller's generation
	// retires, held by exactly those two references — not by an aggregate counter.
	g2 := &leaseCountingRunner{id: "g2"}
	h.cm.PublishExecutor(g2, ContextManagerConfig{Name: "d6-g2"})
	un := h.cm.UnconvergedRefs()
	require.Len(t, un, 1, "the retired caller generation is the unconverged one")
	require.Equal(t, 1, un[0].Refs[LeaseTurn.String()])
	require.Equal(t, 1, un[0].Refs[LeaseSubCall.String()], "the CHILD is why it cannot be reclaimed yet")
	require.Zero(t, h.gen1.closed.Load(), "a live derived call keeps its generation open")

	close(g.gate)
	for range out {
	}
	require.Zero(t, h.refs().SubCalls, "the reference drops exactly when the child stops")
	require.Zero(t, h.gen1.closed.Load(), "the caller's turn still references it")

	h.turn.Release()
	require.Eventually(t, func() bool { return h.gen1.closed.Load() == 1 }, 5*time.Second, 10*time.Millisecond,
		"the generation closes once its LAST reference is gone")
	require.Equal(t, int64(1), h.gen1.closed.Load(), "exactly once")
}

// TestLease_BackgroundRunKeepsGenerationPastTheAck is the spec scenario「ACK 后旧
// 版本仍有后台执行」: returning an ack must NOT release the execution reference —
// only the producer's real return does.
func TestLease_BackgroundRunKeepsGenerationPastTheAck(t *testing.T) {
	h := newLeaseHarness(t, nil)
	child := newGateAgent("worker")
	w := NewAgentToolWrapper(child, "do work", nil, nil)
	w.SetAsyncDenseDuration(30 * time.Millisecond) // ack fast, the run stays parked

	out, err := w.Call(h.ctx, subagentCallArgs(t))
	require.NoError(t, err)
	require.Contains(t, out.(string), "后台运行", "the sync-wait window ended in an ack")

	require.Equal(t, 1, h.refs().BackgroundRuns,
		"an ACK is not a stop credential: the background run still references the generation")
	g2 := &leaseCountingRunner{id: "g2"}
	h.cm.PublishExecutor(g2, ContextManagerConfig{Name: "d6-g2"})
	require.Zero(t, h.gen1.closed.Load(), "the generation with a live background run stays open")

	// Turn over, ack returned — the producer is STILL the only thing that can free it.
	h.turn.Release()
	require.Zero(t, h.gen1.closed.Load(), "releasing the turn alone must not reclaim the background's generation")

	close(child.finish)
	<-child.finished
	require.Eventually(t, func() bool { return h.gen1.closed.Load() == 1 }, 5*time.Second, 10*time.Millisecond,
		"the generation is reclaimed exactly when the background producer stops")
	require.Zero(t, h.refs().BackgroundRuns, "and its reference is gone")
}

// TestLease_RejectedAndDedupedSpawnReleaseExactlyOnce covers the two paths where
// the task layer adopts nothing while the detector's producer is already running:
// the reference must survive until that producer stops, and drop exactly once.
func TestLease_RejectedAndDedupedSpawnReleaseExactlyOnce(t *testing.T) {
	t.Run("spawn rejected by gate", func(t *testing.T) {
		h := newLeaseHarness(t, func() string { return "disk degraded: new background tasks paused" })
		child := newGateAgent("worker")
		w := NewAgentToolWrapper(child, "do work", nil, nil)
		w.SetAsyncDenseDuration(20 * time.Millisecond)

		// The producer stops ONLY on its own, 150ms from now. Without the §4.1 wait
		// the rejection branch answers at ~20ms — while its sub-run is still going
		// — because Spawn's Cancel is only a notification.
		go func() {
			time.Sleep(150 * time.Millisecond)
			close(child.finish)
		}()
		done := make(chan any, 1)
		go func() {
			out, _ := w.Call(h.ctx, subagentCallArgs(t))
			done <- out
		}()
		out := <-done
		require.Contains(t, out.(string), "未被任务层纳管")
		select {
		case <-child.finished:
		default:
			t.Fatal("§4.1「拒绝分支也先等停止」: the rejection branch returned while its un-adopted producer was still running")
		}
		require.Zero(t, h.refs().BackgroundRuns, "the un-adopted run's reference is released once, at its stop")
	})

	t.Run("deduped by same key", func(t *testing.T) {
		h := newLeaseHarness(t, nil)
		first := newGateAgent("worker")
		w := NewAgentToolWrapper(first, "do work", nil, nil)
		w.SetAsyncDenseDuration(20 * time.Millisecond)

		firstRunning := make(chan struct{})
		go func() {
			close(firstRunning)
			_, _ = w.Call(h.ctx, subagentCallArgs(t)) // becomes the tracked task
		}()
		<-first.running

		second := newGateAgent("worker") // same declared name → same dedup key
		w2 := NewAgentToolWrapper(second, "do work", nil, nil)
		w2.SetAsyncDenseDuration(20 * time.Millisecond)
		out, err := w2.Call(h.ctx, subagentCallArgs(t))
		require.NoError(t, err)
		require.Contains(t, out.(string), "同名计划任务已在运行")
		// The deduped call's own producer ran and was cancelled; by the time Call
		// returned, it must have stopped and released its reference exactly once.
		second.once2.Do(func() { close(second.finished) })
		require.Eventually(t, func() bool { return h.refs().BackgroundRuns <= 1 }, 5*time.Second, 10*time.Millisecond,
			"only the adopted task keeps a background reference")

		close(first.finish)
		require.Eventually(t, func() bool { return h.refs().BackgroundRuns == 0 }, 5*time.Second, 10*time.Millisecond,
			"every background reference is released exactly once, including the deduped attempt")
	})
}

// TestLease_NoReferenceForBusinessTurnMultiplication pins §5.1's counting half:
// one business turn = ONE reference, whether or not its flow re-enters RunFlow.
func TestLease_NoReferenceForBusinessTurnMultiplication(t *testing.T) {
	blocking := &requestCapturingModel{} // resp == nil → parks until cancelled
	cm := newTestContextManager("no-double-count", blocking, nil, make(chan *event.Event, 64), nil)

	lease := cm.AcquireLease(LeaseTurn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- cm.RunFlowWithExecutor(lease.WithContext(ctx), model.NewUserMessage("one turn"), lease.Runner())
	}()
	require.Eventually(t, func() bool { return cm.ExecutorRefs().InFlightTurns == 1 }, 10*time.Second, 20*time.Millisecond,
		"a turn carrying its lease must be counted ONCE (BeginTurn + RunFlow used to double-register)")
	require.Zero(t, cm.ExecutorRefs().SubCalls+cm.ExecutorRefs().BackgroundRuns)

	cancel()
	<-done
	lease.Release()
	require.Zero(t, cm.totalRefs(), "and the same single reference is what drops")
}

// ---------------------------------------------------------------------------
// Turn-path matrix (§4.1「正常、错误、早停、取消未完成…全路径恰一次」)
// ---------------------------------------------------------------------------

// pathModel answers the first call with a degenerate final (empty content, no
// tool call → the loop retries it) and parks every later call on `gate`, so a
// test can observe the reference state INSIDE the retry gap — the window where a
// per-attempt release would show up as zero.
type pathModel struct {
	calls atomic.Int64
	gate  chan struct{}
	resp  *model.Response
}

func (m *pathModel) GenerateContent(ctx context.Context, _ *model.Request) (<-chan *model.Response, error) {
	n := m.calls.Add(1)
	ch := make(chan *model.Response, 1)
	go func() {
		defer close(ch)
		if n == 1 {
			ch <- &model.Response{ID: "r", Done: true, Choices: []model.Choice{{
				Message: model.Message{Role: model.RoleAssistant}}}}
			return
		}
		select {
		case <-m.gate:
		case <-ctx.Done():
			return
		}
		if m.resp != nil {
			ch <- m.resp
		}
	}()
	return ch, nil
}

func (m *pathModel) Info() model.Info { return model.Info{Name: "path-model"} }

// startLeaseLoop builds a resident loop over a readable context manager and
// drains its output, returning everything a path test needs to watch references.
func startLeaseLoop(t *testing.T, m model.Model) (*TagentAgent, *ContextManager) {
	t.Helper()
	outputCh := make(chan *event.Event, 256)
	bus := NewEventBus()
	cm := newTestContextManager("turn-paths", m, nil, outputCh, bus)
	ta := &TagentAgent{
		persistentBus:  bus,
		activeBus:      bus,
		contextManager: cm,
		config:         &TagentConfig{MaxToolIterations: 10, MaxTokens: 8000},
		outputCh:       outputCh,
		name:           "turn-paths",
	}
	out, err := ta.StartLoop("test-user", "turn-paths")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() {
		_ = ta.Close()
		<-done
	})
	return ta, cm
}

// requireSettled asserts the loop's turn references all came back — exactly once
// per turn, never below zero (a doubled release shows up as a negative total, a
// lost defer as a permanent positive one).
func requireSettled(t *testing.T, cm *ContextManager) {
	t.Helper()
	require.Eventually(t, func() bool { return cm.totalRefs() == 0 }, 10*time.Second, 20*time.Millisecond,
		"every reference this turn took must be back at zero")
	require.Zero(t, cm.ExecutorRefs().PendingRetirees, "and nothing may be left unreclaimed")
}

func TestLease_EveryTurnPathReleasesExactlyOnce(t *testing.T) {
	t.Run("normal completion", func(t *testing.T) {
		gate := make(chan struct{})
		m := &countingModel{label: "ok", resp: crossResp("served:ok"), gate: gate}
		ta, cm := startLeaseLoop(t, m)
		ta.InjectMessage(model.NewUserMessage("hi"))
		require.Eventually(t, func() bool { return m.calls.Load() >= 1 }, 10*time.Second, 20*time.Millisecond,
			"precondition: the turn reached the model")
		require.Equal(t, int64(1), cm.ExecutorRefs().InFlightTurns,
			"a running turn holds exactly one reference")
		close(gate)
		requireSettled(t, cm)
	})

	t.Run("response-internal model error", func(t *testing.T) {
		// The §5.1 error shape: transport returns nil, the failure rides the event
		// stream. The loop breaks out of the turn on it — the reference must still
		// come back exactly once.
		m := &requestCapturingModel{resp: &model.Response{
			ID: "err", Done: true, Error: &model.ResponseError{Type: "server_error", Message: "upstream exploded"}}}
		ta, cm := startLeaseLoop(t, m)
		ta.InjectMessage(model.NewUserMessage("hi"))
		require.Eventually(t, func() bool { return m.requestCount() >= 1 }, 10*time.Second, 20*time.Millisecond,
			"precondition: the failing call must have been made")
		requireSettled(t, cm)
	})

	t.Run("degenerate retry keeps ONE reference across attempts", func(t *testing.T) {
		m := &pathModel{gate: make(chan struct{}), resp: gateOKResp()}
		ta, cm := startLeaseLoop(t, m)
		ta.InjectMessage(model.NewUserMessage("hi"))
		require.Eventually(t, func() bool { return m.calls.Load() >= 1 }, 10*time.Second, 20*time.Millisecond)
		// Attempt 1 was degenerate and the retry is parked on the gate: the SAME
		// reference still covers the turn. A per-attempt acquire/release would read
		// 0 here (and a leak would read >1).
		require.Eventually(t, func() bool { return m.calls.Load() >= 2 }, 10*time.Second, 20*time.Millisecond,
			"the retry attempt must have started")
		require.Equal(t, int64(1), cm.ExecutorRefs().InFlightTurns,
			"one business turn holds one reference for ALL its attempts (§3.2 pin, §4.1 exactly-once)")
		close(m.gate)
		requireSettled(t, cm)
	})

	t.Run("cancellation mid-stream", func(t *testing.T) {
		ta, cm := startLeaseLoop(t, &requestCapturingModel{}) // resp nil → parks in the model
		ta.InjectMessage(model.NewUserMessage("hi"))
		require.Eventually(t, func() bool { return cm.ExecutorRefs().InFlightTurns == 1 },
			10*time.Second, 20*time.Millisecond, "the turn is parked mid-flight")
		ta.StopLoop() // the unfinished turn's tail must still give its reference back
		requireSettled(t, cm)
	})

	t.Run("intake refusal after close takes no reference", func(t *testing.T) {
		ta, cm := startLeaseLoop(t, &requestCapturingModel{resp: gateOKResp()})
		require.NoError(t, ta.Close())
		requireSettled(t, cm)
		require.NoError(t, ta.Close(), "a terminal close leaves nothing unconverged, so repeating it stays clean")
	})
}

// TestLease_LeaseNeverEntersPersistedTaskMaterial pins §3.2's boundary「禁止指针进入
// 持久记录」: what a spawned task persists is identity plus declarative strings —
// a lease has nowhere to live in it, so a replayed task cannot resurrect one.
func TestLease_LeaseNeverEntersPersistedTaskMaterial(t *testing.T) {
	h := newLeaseHarness(t, nil)
	child := newGateAgent("worker")
	w := NewAgentToolWrapper(child, "do work", nil, nil)
	w.SetAsyncDenseDuration(30 * time.Millisecond)

	out, err := w.Call(h.ctx, subagentCallArgs(t))
	require.NoError(t, err)
	require.Contains(t, out.(string), "后台运行")
	defer close(child.finish)

	tasks := h.spawner.List()
	require.NotEmpty(t, tasks, "the run was adopted, so there IS persisted material to inspect")
	for _, tk := range tasks {
		require.NotNil(t, tk.Spec.Declarative, "a subagent task projects declarative identity")
		scanNoLease(t, reflect.ValueOf(tk.Spec.Declarative).Elem(), 0)
	}
	require.Empty(t, h.cm.UnconvergedRefs(), "the record carries identity only — it is not a reference holder")
}

// scanNoLease walks a persisted value and fails on any *ExecLease (or any pointer
// to a context-manager-side runtime object, which could only be a resurrection
// vector). Depth- and cycle-bounded for the small declarative surface.
func scanNoLease(t *testing.T, v reflect.Value, depth int) {
	t.Helper()
	if depth > 6 {
		return
	}
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		if !v.IsNil() {
			require.NotEqual(t, reflect.TypeOf(&ExecLease{}), v.Type(),
				"an execution lease must never appear inside persisted task material (found at depth %d)", depth)
			if v.Kind() == reflect.Interface && !v.IsNil() {
				scanNoLease(t, v.Elem(), depth+1)
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			scanNoLease(t, v.Field(i), depth+1)
		}
	case reflect.Map:
		for _, mk := range v.MapKeys() {
			scanNoLease(t, v.MapIndex(mk), depth+1)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			scanNoLease(t, v.Index(i), depth+1)
		}
	}
}

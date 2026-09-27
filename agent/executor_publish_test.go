package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// introduce-durable-workflow-engine §2.1（候选构造与发布分离）契约测。
// 断言对象是**宿主可见的真实执行面**——模型在一次调用里看到的工具声明——
// 不是指针/hash/日志编号。

// lastRequestToolNames returns the tool names carried by the most recent
// request the model actually received.
func lastRequestToolNames(m *requestCapturingModel) []string {
	reqs := m.snapshotRequests()
	if len(reqs) == 0 {
		return nil
	}
	var names []string
	for _, tl := range reqs[len(reqs)-1].Tools {
		if tl == nil {
			continue
		}
		if d := tl.Declaration(); d != nil {
			names = append(names, d.Name)
		}
	}
	return names
}

// TestExecutorPublish_ClearedToolBindingDoesNotSurvive is the §2.1 core
// contract: after the config drops the LAST agent tool, the published
// generation must stop declaring it. Under the withdrawn non-zero merge the
// candidate fell back to the previously published Tools slice, so the removed
// tool kept being advertised to the model (stale binding = the exact defect
// D3/§2.1 forbids).
func TestExecutorPublish_ClearedToolBindingDoesNotSurvive(t *testing.T) {
	capture := &requestCapturingModel{resp: gateOKResp()}
	cm := newTestContextManager("pub-clear-tool", capture,
		[]trpctool.Tool{&pinEchoTool{name: "gen1_only_tool"}},
		make(chan *event.Event, 16), nil)

	live := cm.currentRunner()
	out, err := live.Run(context.Background(), "u1", "s1", model.NewUserMessage("first"))
	require.NoError(t, err)
	drainRunner(out, 5*time.Second)
	require.Equal(t, []string{"gen1_only_tool"}, lastRequestToolNames(capture),
		"gen1 must advertise the tool the config declared")

	// gen2: same complete face, minus the last tool (config deleted it).
	face := cm.ExecutorConfig()
	require.NotNil(t, face.Model, "the published face carries the model in force")
	require.Len(t, face.Tools, 1, "precondition: gen1 face has exactly one tool")
	face.Tools = nil

	cand := cm.NewExecutorCandidate(face)
	require.NotNil(t, cand)
	// Constructing a candidate must not have moved the live executor.
	require.Same(t, live, cm.currentRunner(),
		"the effective runner must be untouched until publish")

	published := cm.PublishExecutor(cand, face)
	require.NotNil(t, published)

	out2, err := published.Run(context.Background(), "u1", "s1", model.NewUserMessage("second"))
	require.NoError(t, err)
	drainRunner(out2, 5*time.Second)
	require.Empty(t, lastRequestToolNames(capture),
		"gen2 must declare NO tool: the deleted tool must not survive into the new generation")

	// And the face now in force reflects it (rollback/diagnostics read this).
	require.Empty(t, cm.ExecutorConfig().Tools, "published snapshot must not keep the stale tool")
}

// TestExecutorCandidate_ConstructionDoesNotTouchLiveExecutor pins the other
// half of the split: construction is abandonable (fail-closed before publish),
// and only PublishExecutor moves the seam and updates the face.
func TestExecutorCandidate_ConstructionDoesNotTouchLiveExecutor(t *testing.T) {
	live := &fakeRunner{id: "gen-live"}
	cm := &ContextManager{runner: live, execCfg: ContextManagerConfig{Name: "cand-only"}}

	cand := cm.NewExecutorCandidate(ContextManagerConfig{Name: "cand-only", Model: &requestCapturingModel{}})
	require.NotNil(t, cand)
	require.Same(t, live, cm.currentRunner(),
		"candidate construction must leave the effective runner untouched")
	require.Equal(t, "cand-only", cm.ExecutorConfig().Name,
		"the effective face must not advance before publish")
	require.Zero(t, cm.ExecutorRefs().PendingRetirees,
		"constructing a candidate must not retire — let alone close — the live runner")

	published := cm.PublishExecutor(cand, ContextManagerConfig{Name: "gen-published"})
	require.NotNil(t, published)
	require.NotSame(t, live, cm.currentRunner(), "publish moves the seam")
	require.Equal(t, "gen-published", cm.ExecutorConfig().Name, "publish records the face")

	// A nil candidate is a no-op publish (fail-closed keeps serving the old face).
	again := cm.PublishExecutor(nil, ContextManagerConfig{Name: "must-not-land"})
	require.NotNil(t, again)
	require.Equal(t, "gen-published", cm.ExecutorConfig().Name)
}

// TestExecutorPublish_RepublishedSameFaceKeepsBehavior is the migration of the
// assertion that used to be expressed as "pass a zero ContextManagerConfig and
// the old bindings come back": republishing is now explicit — the caller hands
// over the face it wants in force. Resident state must keep flowing through
// the NEW executor (nothing re-armed, nothing duplicated).
func TestExecutorPublish_RepublishedSameFaceKeepsBehavior(t *testing.T) {
	capture := &requestCapturingModel{resp: gateOKResp()}
	cm := newTestContextManager("pub-same-face", capture, nil, make(chan *event.Event, 16), nil)

	face := cm.ExecutorConfig()
	r2 := cm.PublishExecutor(cm.NewExecutorCandidate(face), face)
	require.NotNil(t, r2)
	setNotice(cm, "[recovery] survives an explicit republish")

	out, err := r2.Run(context.Background(), "test-user", "s-2", model.NewUserMessage("after publish"))
	require.NoError(t, err)
	drainRunner(out, 5*time.Second)

	reqs := capture.snapshotRequests()
	require.NotEmpty(t, reqs)
	require.Equal(t, 1, countCarried(reqs), "the republished executor consumes the resident notice exactly once")
	require.Empty(t, peekNotice(cm))
}

// TestExecutorRefs_RevealsInFlightTurnAndUnconvergedRetiree 钉 §5.1（D9）的最后一腿
// ——「退役引用与未收敛 owner」必须可从既有诊断面读到，而不是只活在日志里。
// 观察对象是真实引用计数：在途 turn 持引用 → 被超代的 runner 只能等待；turn 结束
// → sweep 回收，计数归零。
func TestExecutorRefs_RevealsInFlightTurnAndUnconvergedRetiree(t *testing.T) {
	blocking := &requestCapturingModel{} // resp==nil → 阻塞到 ctx 取消
	cm := newTestContextManager("refs", blocking, nil, make(chan *event.Event, 64), nil)

	idle := cm.ExecutorRefs()
	require.Zero(t, idle.InFlightTurns, "an idle context manager holds no turn references")
	require.Zero(t, idle.PendingRetirees, "and no unreclaimed retirees")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- cm.RunFlowWithExecutor(ctx, model.NewUserMessage("hold the reference"), nil)
	}()
	require.Eventually(t, func() bool { return cm.ExecutorRefs().InFlightTurns >= 1 }, 10*time.Second, 20*time.Millisecond,
		"an in-flight turn must hold a runner reference")

	// Publish mid-turn: the superseded runner retires but must NOT be reclaimed
	// before that turn ends (drain-free) — and that state must be observable.
	face := cm.ExecutorConfig()
	face.Model = &requestCapturingModel{resp: gateOKResp()}
	cm.PublishExecutor(cm.NewExecutorCandidate(face), face)

	refs := cm.ExecutorRefs()
	require.Equal(t, 1, refs.PendingRetirees, "the superseded runner is an unconverged retiree")
	require.Greater(t, refs.InFlightTurns, int64(0), "it is held back by the turn still running on it")
	require.Greater(t, refs.OldestPending, time.Duration(0), "its wait age is reported")
	require.Equal(t, retiredLeakAfter, refs.LeakThreshold)

	cancel()
	require.Eventually(t, func() bool { return cm.ExecutorRefs().PendingRetirees == 0 }, 10*time.Second, 20*time.Millisecond,
		"once the last turn holding it ends, the sweep reclaims the retiree")
	_ = <-done // 取消驱动的 turn 自带错误（上下文已撤）：本测只关心引用已收敛
}

// TestPublishBothEntriesShareOneLinearization pins §5.3's merge. The two publish
// entries — `PublishExecutor` (single owner) and `ActivateExecutor` (a staged org
// generation) — used to carry separate copies of the switch body, and had drifted
// apart on exactly one point: re-publishing the runner that is ALREADY active
// advanced the recorded face on one entry and silently skipped it on the other.
//
// Both entries now run one body, so the observable contract is asserted per entry
// from the same table: the active identity is kept (one runner object gets exactly
// one Close — a redundant publish must not mint a second generation), the recorded
// face still advances (what is recorded is not what that generation routes; the
// binding keeps the face it was BUILT from), and nothing is retired behind it.
func TestPublishBothEntriesShareOneLinearization(t *testing.T) {
	entries := []struct {
		name    string
		publish func(cm *ContextManager, r runner.Runner, face ContextManagerConfig) runner.Runner
	}{
		{"PublishExecutor", func(cm *ContextManager, r runner.Runner, face ContextManagerConfig) runner.Runner {
			return cm.PublishExecutor(r, face)
		}},
		{"ActivateExecutor", func(cm *ContextManager, r runner.Runner, face ContextManagerConfig) runner.Runner {
			return cm.ActivateExecutor(cm.StageExecutor(r, face, nil))
		}},
	}
	for _, e := range entries {
		t.Run(e.name, func(t *testing.T) {
			cm := newTestContextManager("one-linearization", &requestCapturingModel{resp: gateOKResp()},
				nil, make(chan *event.Event, 16), nil)
			defer func() { _ = cm.Close() }()

			face := cm.ExecutorConfig()
			face.Name = "one-linearization"
			// Put a generation in force FIRST, so the re-publish below really is the
			// documented same-runner case (a cold cm has no binding yet — publishing
			// its initial runner would take the adopt path instead).
			first := cm.NewExecutorCandidate(face)
			require.NotNil(t, first)
			e.publish(cm, first, face)
			live := cm.currentRunner()
			require.Same(t, first, live, "precondition: the entry installed a new generation")

			adv := cm.ExecutorConfig()
			adv.SystemPrompt = "recorded-face-advanced"
			got := e.publish(cm, live, adv)

			require.Same(t, live, got, "a same-runner re-publish must keep the one identity (no second generation)")
			require.Same(t, live, cm.currentRunner(), "and must not swap what the next turn runs")
			require.Equal(t, "recorded-face-advanced", cm.ExecutorConfig().SystemPrompt,
				"§5.3 merge: both entries must agree that the RECORDED face advances")
			require.Zero(t, cm.ExecutorRefs().PendingRetirees,
				"nothing may be retired behind a re-publish of the runner still in force")
		})
	}
}

// closeCountRunner records how often its Close ran, so a retired-while-in-use
// executor becomes an observable fact instead of an assumption.
type closeCountRunner struct {
	fakeRunner
	closes *int32
}

func (c *closeCountRunner) Close() error {
	atomic.AddInt32(c.closes, 1)
	return nil
}

// TestPublishFirstGenerationOfInitialRunnerNeverClosesIt pins the guard §5.3 handed
// to §5.4: publishing the runner that a cold ContextManager was constructed with is
// that runner's FIRST generation — adopting it is installing it. Producing a
// predecessor binding for the same object would retire (and close) the executor the
// next turn is about to run, so both entries must leave the live runner untouched,
// unretired and in force.
func TestPublishFirstGenerationOfInitialRunnerNeverClosesIt(t *testing.T) {
	type entry struct {
		name    string
		publish func(cm *ContextManager, r runner.Runner, face ContextManagerConfig) runner.Runner
	}
	entries := []entry{
		{"PublishExecutor", func(cm *ContextManager, r runner.Runner, face ContextManagerConfig) runner.Runner {
			return cm.PublishExecutor(r, face)
		}},
		{"ActivateExecutor", func(cm *ContextManager, r runner.Runner, face ContextManagerConfig) runner.Runner {
			return cm.ActivateExecutor(cm.StageExecutor(r, face, nil))
		}},
	}
	for _, e := range entries {
		t.Run(e.name, func(t *testing.T) {
			var closes int32
			initial := &closeCountRunner{fakeRunner: fakeRunner{id: "initial"}, closes: &closes}
			// The shape the adopt branch documents: a ContextManager assembled
			// without going through NewContextManager's generation install, so a
			// runner is in force with NO generation yet. (A cm built by the
			// constructor already has one, which is why the corner is not reachable
			// from the org path — guarded here rather than left to callers.)
			cm := &ContextManager{runner: initial, name: "first-gen"}
			require.Nil(t, cm.active, "precondition: a runner is in force with no generation installed")

			face := cm.ExecutorConfig()
			got := e.publish(cm, initial, face)

			require.Same(t, initial, got, "the runner in force must still be the one published")
			require.Same(t, initial, cm.currentRunner(), "and the next turn must run it")
			require.Zero(t, atomic.LoadInt32(&closes),
				"§5.4: adopting the construction runner must never retire/close it — it is that runner's first generation")
			require.Zero(t, cm.ExecutorRefs().PendingRetirees, "nothing may sit retired behind a first publish")
			require.NotNil(t, cm.active, "the publish must still install a generation to hold references")
		})
	}
}

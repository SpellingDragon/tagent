// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	upagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

type reclaimGateRunner struct {
	entered chan struct{}
	finish  chan struct{}
	once    sync.Once
}

func (r *reclaimGateRunner) Run(context.Context, string, string, model.Message, ...upagent.RunOption) (<-chan *event.Event, error) {
	out := make(chan *event.Event)
	close(out)
	return out, nil
}

func (r *reclaimGateRunner) Close() error {
	r.once.Do(func() { close(r.entered) })
	<-r.finish
	return nil
}

// TestCloseWaitsForRunnerReclaim checks an in-progress runner close stays visible and retains the store.
func TestCloseWaitsForRunnerReclaim(t *testing.T) {
	drain, grace := turnDrainTimeout, execCloseGrace
	turnDrainTimeout, execCloseGrace = 25*time.Millisecond, 25*time.Millisecond
	defer func() { turnDrainTimeout, execCloseGrace = drain, grace }()
	r := &reclaimGateRunner{entered: make(chan struct{}), finish: make(chan struct{})}
	cm := &ContextManager{name: "runner-reclaim", runner: r}
	var released atomic.Int32
	ta := &TagentAgent{contextManager: cm, memStoreRelease: func() error { released.Add(1); return nil }}
	done := ta.CloseDone()
	require.ErrorIs(t, ta.Close(), ErrExecUnconverged)
	<-r.entered
	select {
	case <-done:
		t.Fatal("CloseDone reported a runner that has not returned from Close")
	default:
	}
	require.Zero(t, released.Load())
	held := cm.UnconvergedRefs()
	require.Len(t, held, 1)
	require.True(t, held[0].Reclaiming)
	close(r.finish)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the final exit did not continue after runner reclaim")
	}
	require.Equal(t, int32(1), released.Load())
	require.Empty(t, cm.UnconvergedRefs())
	finished, err := ta.DeferredCloseOutcome()
	require.True(t, finished)
	require.NoError(t, err)
}

// TestCloseRetainsPrivateExecutionRefs checks a detached private producer holds final shutdown open.
func TestCloseRetainsPrivateExecutionRefs(t *testing.T) {
	drain, grace := turnDrainTimeout, execCloseGrace
	turnDrainTimeout, execCloseGrace = 25*time.Millisecond, 25*time.Millisecond
	defer func() { turnDrainTimeout, execCloseGrace = drain, grace }()
	cm := &ContextManager{name: "private-close", runner: &countingRunner{}}
	private := &ContextManager{name: "private-close", runner: &countingRunner{}}
	var released atomic.Int32
	ta := &TagentAgent{contextManager: cm, memStoreRelease: func() error { released.Add(1); return nil }}
	ta.registerLiveCM(private)
	lease := private.AcquireLease(LeaseBackground)
	require.ErrorIs(t, ta.Close(), ErrExecUnconverged)
	require.Equal(t, 1, ta.LiveCMCount())
	require.Zero(t, released.Load())
	lease.Release()
	select {
	case <-ta.CloseDone():
	case <-time.After(time.Second):
		t.Fatal("a private execution drained without completing its owner's final exit")
	}
	require.Zero(t, ta.LiveCMCount())
	require.Equal(t, int32(1), released.Load())
}

type shutdownProducer struct {
	settled   chan task.SettleSignal
	detached  chan struct{}
	stopped   chan struct{}
	cancelled chan struct{}
	once      sync.Once
}

func (p *shutdownProducer) Settled() <-chan task.SettleSignal { return p.settled }
func (p *shutdownProducer) Detached() <-chan struct{}         { return p.detached }
func (p *shutdownProducer) Stopped() <-chan struct{}          { return p.stopped }
func (p *shutdownProducer) Cancel()                           { p.once.Do(func() { close(p.cancelled) }) }

// TestCloseWaitsForActualTaskProducerStop checks cancellation alone cannot release the final store lease.
func TestCloseWaitsForActualTaskProducerStop(t *testing.T) {
	drain, grace := turnDrainTimeout, execCloseGrace
	turnDrainTimeout, execCloseGrace = 25*time.Millisecond, 25*time.Millisecond
	defer func() { turnDrainTimeout, execCloseGrace = drain, grace }()
	var released atomic.Int32
	ta, err := NewTagentAgent(&TagentConfig{Name: "producer-close", Model: &requestCapturingModel{resp: finalTextResponse("ok", "done")}, MemoryStore: memory.NewInMemoryStore(), MemStoreRelease: func() error { released.Add(1); return nil }})
	require.NoError(t, err)
	p := &shutdownProducer{settled: make(chan task.SettleSignal), detached: make(chan struct{}), stopped: make(chan struct{}), cancelled: make(chan struct{})}
	close(p.detached)
	res := ta.taskManager.Spawn(task.TaskSpec{Kind: "command", Mode: "oneshot", WaitFor: task.WaitExit}, p)
	require.NotNil(t, res.Task)
	require.ErrorIs(t, ta.Close(), ErrExecUnconverged)
	<-p.cancelled
	select {
	case <-ta.CloseDone():
		t.Fatal("producer cancellation was mistaken for confirmed stop")
	default:
	}
	require.Zero(t, released.Load())
	close(p.settled)
	close(p.stopped)
	select {
	case <-ta.CloseDone():
	case <-time.After(time.Second):
		t.Fatal("confirmed producer stop did not finish the same close tail")
	}
	require.Equal(t, int32(1), released.Load())
	select {
	case <-ta.taskManager.CloseDone():
	default:
		t.Fatal("agent shutdown finished before task workers")
	}
}

// TestPrivateWaitingFaceKeepsDeclaredTargets checks waiting scopes retain callable generations without compute refs.
func TestPrivateWaitingFaceKeepsDeclaredTargets(t *testing.T) {
	child := &TagentAgent{name: "child", contextManager: &ContextManager{name: "child", runner: &countingRunner{}}}
	wrapper := NewAgentToolWrapper(child, "child", nil, nil)
	face := ContextManagerConfig{Tools: []tool.Tool{wrapper}}
	cm := &ContextManager{name: "parent", runner: &countingRunner{}, execCfg: face}
	parent := &TagentAgent{name: "parent", contextManager: cm}
	old := cm.activeBinding()
	old.addDeclaredHold(child.contextManager.activeBinding())
	private := &ContextManager{name: "parent", runner: &countingRunner{}, execCfg: face}
	private.activeBinding().addDeclaredHold(old)
	parent.registerLiveCM(private)
	cm.PublishExecutor(&countingRunner{}, ContextManagerConfig{})
	require.Zero(t, private.OutstandingRefs(), "waiting owns a face without active compute")
	require.Positive(t, BindingHolders("child", []*TagentAgent{parent, child}))
	held := cm.UnconvergedRefs()
	require.Len(t, held, 1)
	require.Equal(t, 1, held[0].DeclaredHolders)
	parent.releaseInvocationCM(private)
	require.Zero(t, BindingHolders("child", []*TagentAgent{parent, child}))
	require.Empty(t, cm.UnconvergedRefs())
	require.NoError(t, cm.Close())
	require.NoError(t, child.contextManager.Close())
}

// TestStartLoopRecoversHiddenParentlessOwner checks the canonical resident roster starts recovered private scopes.
func TestStartLoopRecoversHiddenParentlessOwner(t *testing.T) {
	hiddenModel := &requestCapturingModel{resp: finalTextResponse("hidden", "hidden scope recovered")}
	hidden := continuationTestAgent(t, "hidden-owner", nil, hiddenModel)
	entryModel := &requestCapturingModel{resp: finalTextResponse("entry", "entry")}
	entry := continuationTestAgent(t, "entry-owner", nil, entryModel)
	c := seedContinuation(t, hidden, "hidden-inv", ContinuationContext{})
	c.recovered, c.state.State = true, "suspended"
	topology := NewResidentTopology(map[string]*TagentAgent{entry.name: entry, hidden.name: hidden})
	entry.SetResidentTable(topology)
	hidden.SetResidentTable(topology)
	_, err := entry.StartLoop("user", "entry-session")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.state.Outcome == "completed"
	}, time.Second, time.Millisecond)
	require.Equal(t, 1, hiddenModel.requestCount())
	require.Zero(t, entryModel.requestCount())
	require.NoError(t, entry.Close())
}

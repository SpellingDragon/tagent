package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	upagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

// Generation recycling under per-generation references (implementation-
// hardening 5.1, re-expressed for §3.2/§4.1 design D6): a superseded runner is
// closed when ITS OWN references drop — not when some aggregate counter of the
// manager happens to be zero, and never twice.

// countingRunner is a minimal runner.Runner that counts Close calls (the upstream
// interface documents Close as idempotent, so a second call is a bug).
type countingRunner struct {
	closed atomic.Int64
}

func (f *countingRunner) Run(context.Context, string, string, model.Message, ...upagent.RunOption) (<-chan *event.Event, error) {
	ch := make(chan *event.Event, 1)
	close(ch)
	return ch, nil
}
func (f *countingRunner) Close() error {
	f.closed.Add(1)
	return nil
}

var _ runner.Runner = (*countingRunner)(nil)

func newRecycleManager() *ContextManager {
	return &ContextManager{name: "recycle-test", runner: &countingRunner{}}
}

// TestRecycle_GenerationClosesOnlyAfterItsOwnReferencesDrop pins the drain-free
// tail per generation: a lease pinned on G1 keeps exactly G1 open; releasing it
// reclaims G1 and nothing else.
func TestRecycle_GenerationClosesOnlyAfterItsOwnReferencesDrop(t *testing.T) {
	cm := newRecycleManager()
	g1 := cm.currentRunner().(*countingRunner)

	lease := cm.AcquireLease(LeaseTurn)
	require.Same(t, g1, lease.Runner(), "the lease pins the generation in force at acquire")

	g2 := &countingRunner{}
	cm.PublishExecutor(g2, cm.ExecutorConfig()) // retires G1's generation, which is still held
	require.Zero(t, g1.closed.Load(), "a retired generation with a live reference must not be closed")

	lease.Release()
	require.Equal(t, int64(1), g1.closed.Load(), "the last reference dropping reclaims exactly that generation")
	require.Zero(t, g2.closed.Load(), "the generation now in force is never reclaimed by a release")

	// Idempotent release: the count must not drop twice and a second Close never
	// happens (§4.1「全路径恰一次」).
	lease.Release()
	require.Equal(t, int64(1), g1.closed.Load())
	require.Zero(t, cm.ExecutorRefs().PendingRetirees, "a reclaimed generation leaves the unconverged list (bounded bookkeeping)")
}

// TestRecycle_UnrelatedGenerationReclaimsIndependently is D6's「无关旧代独立回收」
// at the mechanism level: holding G1 open must not keep an unreferenced G2 alive.
func TestRecycle_UnrelatedGenerationReclaimsIndependently(t *testing.T) {
	cm := newRecycleManager()
	held := cm.AcquireLease(LeaseTurn)
	defer held.Release()

	g2 := &countingRunner{}
	g3 := &countingRunner{}
	cm.PublishExecutor(g2, cm.ExecutorConfig())
	cm.PublishExecutor(g3, cm.ExecutorConfig())

	require.Equal(t, int64(1), g2.closed.Load(), "G2 has no references — it is reclaimed while G1 still runs")
	require.Zero(t, g3.closed.Load(), "the current generation stays open")
}

// TestContextManager_Close_IsBoundedAndHoldsUnconverged pins §4.1's terminal
// contract as spec runtime-resource-ownership words it: the public close is
// BOUNDED, everything that converged is closed exactly once, and an execution
// whose producer never confirmed a stop is EXPLICITLY HELD and reported — never
// force-closed to satisfy a count. Lifting the barrier afterwards releases it
// exactly once.
func TestContextManager_Close_IsBoundedAndHoldsUnconverged(t *testing.T) {
	grace := execCloseGrace
	execCloseGrace = 100 * time.Millisecond
	defer func() { execCloseGrace = grace }()

	cm := newRecycleManager()
	g1 := cm.currentRunner().(*countingRunner)
	held := cm.AcquireLease(LeaseTurn)
	g2 := &countingRunner{}
	cm.PublishExecutor(g2, cm.ExecutorConfig()) // retires the generation `held` still references

	unconverged := cm.UnconvergedRefs()
	require.Len(t, unconverged, 1, "the still-held retired generation is nameable before the drain")
	require.Equal(t, "recycle-test", unconverged[0].Owner, "the report says WHICH owner is stuck")
	require.Equal(t, 1, unconverged[0].Refs[LeaseTurn.String()], "and for WHAT it is still held")

	err := cm.Close()
	require.ErrorIs(t, err, ErrExecUnconverged, "a stuck execution is never reported as a clean close")
	require.Equal(t, int64(1), g2.closed.Load(), "the unreferenced current generation converged and closed once")
	require.Zero(t, g1.closed.Load(), "the generation with a live producer is HELD, not force-closed")
	require.Len(t, cm.UnconvergedRefs(), 1, "and it stays on the books afterwards — the report is readable, not consumed")

	// Barrier lifts: the producer confirms its stop, and only now is the
	// generation reclaimed — exactly once.
	held.Release()
	require.Equal(t, int64(1), g1.closed.Load(), "the release after the barrier closes it exactly once")
	require.Empty(t, cm.UnconvergedRefs(), "and it leaves the unconverged list")
	held.Release()
	require.Equal(t, int64(1), g1.closed.Load(), "a repeated release stays inert")
}

// TestRecycle_SameExecutorRepublishedIsNotANewGeneration pins the one-identity
// rule: re-publishing the runner object already in force (a rollback landing on
// the current face, a redundant candidate) must not create a second generation
// for it, because that would mean a second Close of one runner.
func TestRecycle_SameExecutorRepublishedIsNotANewGeneration(t *testing.T) {
	cm := newRecycleManager()
	_, release := cm.BeginTurn()
	defer release()

	current := cm.currentRunner()
	face := cm.ExecutorConfig()
	require.Same(t, current, cm.PublishExecutor(current, face),
		"republishing the in-force executor returns the executor that stays in force")

	before := cm.ExecutorRefs().PendingRetirees
	require.Same(t, current, cm.PublishExecutor(current, face))
	require.Equal(t, before, cm.ExecutorRefs().PendingRetirees,
		"a redundant publish of the same executor creates no extra generation to reclaim")
	require.Zero(t, current.(*countingRunner).closed.Load(),
		"and never closes the executor that is still in force")
}

// TestRecycle_RaceStress (review test-blind-spot #1): concurrent acquire/release,
// publication and terminal Close — the lock discipline must hold and no runner
// may ever be closed twice.
func TestRecycle_RaceStress(t *testing.T) {
	cm := newRecycleManager()
	const gens = 50
	runners := make([]*countingRunner, gens)
	for i := range runners {
		runners[i] = &countingRunner{}
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Holder: continuously acquires and releases references, running "turns".
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			lease := cm.AcquireLease(LeaseTurn)
			child := lease.Derive(LeaseSubCall)
			lease.Release()
			child.Release()
		}
	}()

	// Publisher: retires a generation per iteration (retirement is intrinsic to
	// publication — there is no separate retire entry point any more).
	for i := 0; i < gens; i++ {
		cm.PublishExecutor(runners[i], cm.ExecutorConfig())
	}

	close(stop)
	wg.Wait()
	require.NoError(t, cm.Close())

	for i, r := range runners {
		require.LessOrEqual(t, r.closed.Load(), int64(1), "generation %d closed more than once", i)
	}
}

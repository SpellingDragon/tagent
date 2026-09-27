package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// D6: a retired generation may finish what already references it, but NO new work
// may start on it. That used to be only a convention — `retire()` claimed "acquire
// only ever happens on the active binding" while `acquire()` checked nothing, and
// reading the active binding and pinning it cannot be one atomic step because
// PublishExecutor retires the superseded generation AFTER releasing executorMu. A
// concurrent acquire could therefore pin a generation whose runner had already been
// reclaimed and run a turn on it.
//
// The interleaving itself is not reproducible on demand (which is why the guard sits
// at the pin rather than in a test schedule), so these pin the two decisions the
// guard is responsible for: a retired generation refuses a NEW reference, and the
// re-pin lands on the generation actually in force.

func TestExecLease_RetiredGenerationRefusesNewReference(t *testing.T) {
	cm := newRecycleManager()
	g1 := cm.currentRunner().(*countingRunner)

	first := cm.AcquireLease(LeaseTurn)
	require.Same(t, g1, first.Runner(), "precondition: the turn pinned the generation in force")
	idle := cm.activeBinding()
	idleGen := idle.id
	first.Release() // now genuinely idle — the common case at a publish

	g2 := &countingRunner{}
	cm.PublishExecutor(g2, cm.ExecutorConfig())
	require.True(t, idle.retired, "precondition: the superseded generation is retired")
	require.Equal(t, int64(1), g1.closed.Load(), "precondition: being idle, it was reclaimed at once")

	if _, ok := idle.tryAcquireActive(LeaseTurn); ok {
		t.Fatal("a retired generation accepted a NEW reference — D6 is unenforced, and the caller would run on the closed runner above")
	}
	require.Zero(t, idle.total, "the refusal must not leave a reference behind")

	fresh := cm.AcquireLease(LeaseTurn)
	require.Same(t, g2, fresh.Runner(), "the acquire path re-pins the generation actually in force")
	require.NotEqual(t, idleGen, fresh.Generation(), "and never the superseded one")
	fresh.Release()
	require.Zero(t, g2.closed.Load(), "releasing a held generation must not close the one in force")
}

// Inheritance must stay legal: a sub-call derived from an execution that already
// pinned a retired generation still takes its reference (D5「派生前继承发起调用租约」),
// otherwise a long parent would lose its own descendants mid-drain. This is what
// separates tryAcquireActive (new work) from acquire (inherited work).
func TestExecLease_DeriveStillLandsOnRetiredButHeldGeneration(t *testing.T) {
	cm := newRecycleManager()
	g1 := cm.currentRunner().(*countingRunner)

	parent := cm.AcquireLease(LeaseTurn)
	held := cm.activeBinding()

	cm.PublishExecutor(&countingRunner{}, cm.ExecutorConfig())
	require.True(t, held.retired, "precondition: superseded while still referenced")
	require.False(t, held.closed, "a generation with outstanding references is never force-closed")
	require.Equal(t, int64(0), g1.closed.Load())

	child := parent.Derive(LeaseSubCall)
	require.Equal(t, held.id, child.Generation(), "the derived reference rides the SAME generation")
	require.Equal(t, 2, held.total)

	child.Release()
	require.Equal(t, 1, held.total)
	require.Equal(t, int64(0), g1.closed.Load(), "not before the last holder is gone")

	parent.Release()
	require.Equal(t, int64(1), g1.closed.Load(), "the last release reclaims it exactly once")
}

package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
)

// §5.3 性能与有界性重验 — the two clauses no prior test pins.
//
// The five hot-reload phases (读配置/构建/提交/获取/回收) are measured separately by
// the benches: LoadConfig (read-config), BenchmarkNewExecutorCandidate (build),
// BenchmarkBeginTurn (acquire), BenchmarkPublishExecutor (publish). But 提交 (the
// commit critical section) and 回收 (reclaim/close) were LUMPED in the idle-publish
// bench, where the superseded runner has no reference and closes synchronously inside
// publish — so the bench never showed that the commit does NOT carry the close, nor
// what reclaim costs on its own. And no test bounded resources specifically WHILE
// references are live: 5.3 demands 「已引用未停资源如实统计，不能为过数量门提前关闭」,
// i.e. the retired set must honestly equal the number of LIVE references and never be
// force-closed to satisfy a count.
//
// These are proven STRUCTURALLY (reference counts, closed flags), never by a wall-clock
// mean or a fixed machine threshold (per D3/D12; the short-lock itself is barrier-proven
// in org_d3_scheduling_test.go). Phase durations are logged as observations only.

// publishFresh supersede-and-retire helper: publishes a NEW candidate (whose runner is
// wrapped in a closeCounter so the caller can observe the close) and returns it with
// its generation id. The candidate becomes the active generation; the previously active
// one is superseded.
//
// PublishExecutor returns the runner now in force, NOT a generation id. The id of the
// just-published generation is read through a transient lease on the ACTIVE binding:
// releasing that probe cannot reclaim it, because reclaim only fires on a generation
// that is BOTH retired and unreferenced, and this one is still active.
func publishFresh(cm *ContextManager) (int64, *closeCounter) {
	face := cm.ExecutorConfig()
	observed := &closeCounter{Runner: cm.NewExecutorCandidate(face)}
	cm.PublishExecutor(observed, face)
	probe := cm.AcquireLease(LeaseTurn)
	gid := probe.Generation()
	probe.Release()
	return gid, observed
}

// TestD53_CommitSeparateFromReclaim shows 提交 and 回收 are distinct phases: pinning the
// active generation and then superseding it (the commit) returns with the referenced
// runner STILL OPEN and counted as one pending retiree — the close did not ride inside
// the commit. Only releasing the reference triggers 回收. Durations are logged, never
// used as a correctness threshold.
func TestD53_CommitSeparateFromReclaim(t *testing.T) {
	cm := newTestContextManager("d53-split", &requestCapturingModel{resp: gateOKResp()},
		nil, make(chan *event.Event, 64), nil)

	// Pin a generation we can observe (publish it first so its runner is our counter),
	// then hold it across the commit that supersedes it.
	gid, held := publishFresh(cm)
	lease := cm.AcquireLease(LeaseTurn) // pin the just-published generation
	require.Equal(t, gid, lease.Generation(), "the lease pins the generation we are watching")

	commitStart := time.Now()
	_, _ = publishFresh(cm) // commit: supersede `held`; a later one becomes active
	commitDur := time.Since(commitStart)

	// 提交 did NOT 回收: the referenced runner is retired but open, one pending retiree.
	row := genRow(cm, gid)
	require.NotNil(t, row, "the referenced generation stays on the books")
	require.True(t, row.Retired, "a newer publish superseded it")
	require.False(t, row.Closed, "the commit MUST NOT close a still-referenced runner")
	require.Zero(t, held.closes.Load(), "no close happened inside the commit section")
	require.Equal(t, 1, cm.ExecutorRefs().PendingRetirees, "honestly counted as one live-referenced retiree")

	// 回收: releasing the reference is what closes it — exactly once, on release.
	reclaimStart := time.Now()
	lease.Release()
	require.Eventually(t, func() bool { return cm.ExecutorRefs().PendingRetirees == 0 },
		10*time.Second, 10*time.Millisecond, "reclaimed once nothing references it")
	reclaimDur := time.Since(reclaimStart)
	require.Equal(t, int64(1), held.closes.Load(), "the close happened exactly once, triggered by release")

	t.Logf("[§5.3 phase sample] 提交(commit, close deferred)=%v  回收(reclaim-on-release)=%v — observational; structural facts asserted above",
		commitDur.Round(time.Microsecond), reclaimDur.Round(time.Microsecond))
}

// TestD53_HeldResourcesCountedNotForceClosedUnderChurn churns generations while keeping a
// fixed number referenced, and asserts the pending-retiree set is the HONEST count of
// live references — never zeroed by force-closing still-needed runners to pass a count
// gate — and that it converges to fully-reclaimed as references drop.
func TestD53_HeldResourcesCountedNotForceClosedUnderChurn(t *testing.T) {
	cm := newTestContextManager("d53-churn", &requestCapturingModel{resp: gateOKResp()},
		nil, make(chan *event.Event, 512), nil)

	const k = 6
	leases := make([]*ExecLease, 0, k)
	gids := make([]int64, 0, k)
	counters := make([]*closeCounter, 0, k)

	// For each held generation: publish it (active+watchable), pin it, then supersede it
	// with a fresh one so it becomes retired-but-held. The supersede is the next loop's
	// publish; the last one is superseded after the loop.
	for i := 0; i < k; i++ {
		gid, c := publishFresh(cm) // this generation becomes active
		lease := cm.AcquireLease(LeaseTurn)
		require.Equal(t, gid, lease.Generation(), "iteration %d pins the generation it is watching", i)
		leases = append(leases, lease)
		gids = append(gids, gid)
		counters = append(counters, c)
	}
	publishFresh(cm) // supersede the last held generation → all k are now retired

	// Honest, reference-bounded count: exactly k pending retirees, each open, pinned by
	// exactly one reference, none closed early to shrink the number.
	refs := cm.ExecutorRefs()
	require.Equal(t, k, refs.PendingRetirees,
		"the retired set equals the number of live references (bounded by refs, not by history)")
	for i, gid := range gids {
		row := genRow(cm, gid)
		require.NotNil(t, row, "held generation %d stays on the books", gid)
		require.True(t, row.Retired, "held generation %d was superseded", gid)
		require.False(t, row.Closed, "held generation %d is open until its own reference drops", gid)
		require.Equal(t, 1, row.Total, "held generation %d is pinned by exactly its one lease", gid)
		require.Zero(t, counters[i].closes.Load(), "held generation %d was never force-closed", gid)
	}

	// Releasing in reverse reclaims exactly one generation per release — the count is
	// reference-driven, and it converges to zero with each runner closed once.
	for i := len(leases) - 1; i >= 0; i-- {
		leases[i].Release()
		require.Eventually(t, func() bool {
			return cm.ExecutorRefs().PendingRetirees == i && counters[i].closes.Load() == 1
		}, 10*time.Second, 10*time.Millisecond,
			"releasing reference %d reclaims exactly its own generation, leaving %d held", i, i)
	}

	require.Eventually(t, func() bool { return cm.ExecutorRefs().PendingRetirees == 0 }, 10*time.Second, 10*time.Millisecond)
	require.Zero(t, cm.ExecutorRefs().InFlightTurns, "all references released")
	for i, c := range counters {
		require.Equal(t, int64(1), c.closes.Load(), "generation %d closed exactly once in total", i)
	}
}

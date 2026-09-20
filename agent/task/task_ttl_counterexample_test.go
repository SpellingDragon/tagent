package task

import (
	"os"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// §10.1 baseline counter-examples for the async-task-lifetime spec
// (complete-resident-reliability-protocol). These are the executable, code-grounded
// fail-befores for the defect that motivated the whole investigation (production
// 56bf24c3: a restart-tagent.sh job fell silent into SUSPECT, never detached, and
// lingered on the board for 23h while the model re-arbitrated "维持原判…不处理" every
// turn). Written as assertions of the SPEC TARGET, so each currently FAILS and is
// SKIPPED (blocked-by its implementing task) to keep the tree green — per the §1
// fail-before methodology. Remove the t.Skip when the corresponding §10 slice lands.
//
// The settlement dead-weight leg of 10.1 is NOT reproduced as a fail-before here:
// the current tree already implements settle-notice folding (foldSettleRuns /
// settle_fold, agent/compress/context_compressor.go, tested by
// TestCompress_SettleStormFoldReclaims80Percent + survival/lossless cases, all
// green under -race). The 09-17 trajectory's floor 96–99% PREDATES that machinery;
// asserting a dead-weight fail-before now would contradict observed behavior. See
// evidence.md §10.1 for the disposition; residual 10.7 scope is coverage of ALL
// retirement sources, verified (not assumed) against real code.
// ---------------------------------------------------------------------------

// green; set TAGENT_RUN_FAILBEFORE=1 to run them RED (proving the target is not yet
// met now, and later confirming pass-after) without editing this file.
func skipUnlessFailBefore(t *testing.T, reason string) {
	t.Helper()
	if os.Getenv("TAGENT_RUN_FAILBEFORE") == "" {
		t.Skip(reason)
	}
}

// TestCounter_SuspectNeverDetachedEscapesEveryAgeWall is the pass-after guard for
// §10.3: a job-kind task that went quiet into SUSPECT WITHOUT ever detaching was
// previously invisible to every age wall — reconcileDetached only enqueued
// alive_detached/stale candidates and enforceJobDeadline required a non-zero
// detachedAt, so a never-detached suspect lingered forever (production 56bf24c3,
// 23h). The unified TTL reaper (reconcileTTL) now reaches EVERY active state
// against an absolute anchor, so a suspect past its spec.TTL is retired. Once a
// §10.3 regression: this asserts the reaper works; it must never regress to the
// detached-only gate.
func TestCounter_SuspectNeverDetachedEscapesEveryAgeWall(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	base := time.Now()
	tm.now = func() time.Time { return base }
	// A job-kind task that fell silent into SUSPECT WITHOUT ever detaching, with a
	// 10-minute absolute TTL, started 30h ago. The old walls cannot reach it
	// (never detached); only the unified reaper can.
	tk := tm.RestoreTask("56bf24c3-restart", TaskSpec{
		Kind:  "command",
		Desc:  "restart-tagent.sh",
		Alive: func() bool { return true }, // backing session still "alive" (a hung-but-live script)
		TTL:   10 * time.Minute,
	}, base.Add(-30*time.Hour), TaskSuspect)
	if got := tk.DetachedAtMilli(); got != 0 {
		t.Fatalf("precondition: task must be never-detached (detachedAt=0), got %d", got)
	}

	_ = len(tm.List()) // drive reconcile across repeated passes
	_ = len(tm.List())

	if st := tk.Status(); st == TaskSuspect {
		t.Fatalf("§10.3: a suspect/undetached job 30h old (past a 10m TTL) must be reaped to a terminal state, still %s", st)
	}
	if st := tk.Status(); st != TaskFailed {
		t.Fatalf("§10.3: TTL expiry must retire as failed, got %s", st)
	}
}

// TestCounter_SuspectBoardShowsRemainingNotArbitration is the §10.6 pass-after:
// the board renders the unified reaper's REMAINING lifetime for a suspect task
// (bounded + self-reclaiming → the model decides once) and NO LONGER emits the
// old non-terminal "需确认" arbitration invitation that was re-served every turn
// and drove the 56bf24c3 per-turn re-adjudication. The suspect is YOUNG (within
// its TTL): a 23h-old one is already reaped by the §10.5 floor and never reaches
// the board.
func TestCounter_SuspectBoardShowsRemainingNotArbitration(t *testing.T) {
	suspect := NewTaskFixture("56bf24c3-restart", "restart-tagent.sh", TaskSuspect, time.Now().Add(-time.Minute))
	board := RenderBoard([]*Task{suspect}, 10*time.Minute)

	if board == "" {
		t.Fatal("precondition: an active suspect task must render a board")
	}
	if strings.Contains(board, "需确认") {
		t.Fatalf("§10.6: the board must NOT carry the non-terminal '⚠…需确认' arbitration invitation; got:\n%s", board)
	}
	if !strings.Contains(board, "剩余") {
		t.Fatalf("§10.6: the board must render the task's remaining lifetime / expected-reclaim time so the model decides once; got:\n%s", board)
	}
}

// TestCounter_RestoredTaskWithoutTTLBindingIsReaped is the pass-after guard for
// the 2nd-review finding (①), closed by §10.5: the unified reaper is now ALWAYS on
// (manager DefaultTTL is floored to 10m, and ActionTool.SpecFromDeclarative
// restores the persisted `ttl`), so a restored task with NO explicit spec.TTL —
// the real 56bf24c3 shape — is still bounded by the manager floor and reaped once
// past it. (The age-wall test above hand-injects a TTL; this exercises the floor.)
func TestCounter_RestoredTaskWithoutTTLBindingIsReaped(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{}) // DefaultTTL unset → floored to 10m (10.5)
	base := time.Now()
	tm.now = func() time.Time { return base }
	// Simulate a REAL restore with NO TTL on the spec.
	tk := tm.RestoreTask("t-restored", TaskSpec{
		Kind:  "command",
		Desc:  "restored: restart-tagent.sh",
		Alive: func() bool { return true }, // session still tracked → the zombie path spares it
	}, base.Add(-24*time.Hour), TaskSuspect)

	_ = len(tm.List())
	_ = len(tm.List())
	if st := tk.Status(); !isTerminalStatus(st) {
		t.Fatalf("§10.5: a 24h-old restored task with no explicit TTL must be reaped by the manager floor, still %s", st)
	}
	if st := tk.Status(); st != TaskFailed {
		t.Fatalf("§10.5: TTL expiry must retire as failed, got %s", st)
	}
}

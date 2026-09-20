package memory

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// §5.8 — the registration barrier (BeginHold/EndHold): forgetting pauses while
// any owner is mid-inventory, unconditionally (no armGrace escape — a timed
// backstop may not silently override an explicit barrier), and re-armable for
// late attaches on an already-running store.

func TestRetentionLease_HoldBarrierNestsAndRearms(t *testing.T) {
	cleared := func(l *RetentionLease) bool {
		select {
		case <-l.HoldClear():
			return true
		default:
			return false
		}
	}
	l := NewRetentionLease()
	require.True(t, cleared(l), "a fresh lease holds nothing — the barrier is open")

	l.BeginHold()
	require.False(t, cleared(l))
	l.BeginHold() // nested owner
	l.EndHold()
	assert.False(t, cleared(l), "the inner End must not open the outer owner's window")
	l.EndHold()
	assert.True(t, cleared(l), "the outermost End releases the barrier")

	// Late attach RE-ARMS: a second registration window on the same lease blocks
	// again (the released gate was replaced, not reused closed).
	l.BeginHold()
	assert.False(t, cleared(l), "a re-raised hold blocks — fail-before: without gate swap on 0→1 this passes and a late attach runs unprotected")
	l.EndHold()
	assert.True(t, cleared(l))

	l.EndHold() // unpaired extra End: no-op, never panics nor flips state
	assert.True(t, cleared(l))

	var nilLease *RetentionLease
	nilLease.BeginHold()
	nilLease.EndHold()
	select {
	case <-nilLease.HoldClear():
	default:
		t.Fatal("nil lease must report the barrier clear")
	}
}

// TestRetentionLease_ScannerPausesUnderHold is the §5.8 end-to-end of the
// late-attach requirement (design L141): a store whose forgetting is ALREADY
// released (MarkReady done, first pass live) must still pause every subsequent
// pass while a new owner inventories — and resume once the hold ends.
func TestRetentionLease_ScannerPausesUnderHold(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, rel, ":memory:", 100)
	require.NoError(t, err)
	ts := NewTombstoneSet(rel, mockKV, 1)
	store.tombstones = ts
	lease := NewRetentionLease()
	store.SetRetentionLease(lease)
	lm := NewLifecycleManager(store, ts, DefaultLifecycleConfig())
	defer lm.Stop()

	now := time.Now().UnixMilli()
	overdue := NewSnowflakeEventKey(1, now-10*24*3600*1000)
	require.NoError(t, store.StoreEvent(overdue, FullEvent{
		EventKey: overdue, PartitionID: 1, EventType: "thinking_plan",
		EventSummary: "material a late attach still needs", Timestamp: now - 10*24*3600*1000,
	}))

	// Ready AND the barrier held (an inventory in flight before any pass lands).
	lease.BeginHold()
	lease.MarkReady()
	lm.Start()
	time.Sleep(80 * time.Millisecond)
	assert.False(t, ts.IsTombstone(overdue),
		"§5.8: an active registration barrier pauses the first pass even past the ready gate")

	lease.EndHold()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !ts.IsTombstone(overdue) {
		time.Sleep(10 * time.Millisecond)
	}
	assert.True(t, ts.IsTombstone(overdue),
		"§5.8: forgetting resumes once every in-flight inventory has ended its hold")
}

// countingGuard records the barrier calls a spill rebuild is required to make.
type countingGuard struct {
	begins, ends, protects int
}

func (g *countingGuard) ProtectKey(int64) { g.protects++ }
func (g *countingGuard) ReleaseKey(int64) {}
func (g *countingGuard) ArmRetention()    {}
func (g *countingGuard) BeginHold()       { g.begins++ }
func (g *countingGuard) EndHold()         { g.ends++ }

// TestMemSpill_ProtectAllPendingRunsUnderBarrier: §5.8 registers
// prepared/receipt/SPILL retention under the barrier — the spill rebuild must
// hold while reading, release exactly once on success, and KEEP the hold on a
// read failure (explicit block, never a silent proceed).
func TestMemSpill_ProtectAllPendingRunsUnderBarrier(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/spill.jsonl"
	sp := NewMemSpill(path)
	require.NoError(t, sp.Append(42, FullEvent{EventKey: 42})) // one pending key on disk (no guard yet)
	g := &countingGuard{}
	sp.SetGuard(g)

	require.NoError(t, sp.ProtectAllPending())
	assert.Equal(t, 1, g.begins, "the rebuild raises the registration barrier")
	assert.Equal(t, 1, g.ends, "and releases it exactly once on success")
	assert.Equal(t, 1, g.protects, "the pending key is protected inside the window")
}

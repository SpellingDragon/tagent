package memory

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// §5.9 — protection END-TO-END through the spill's SAFE REMOVAL: a pending
// spill key's durable original must survive a real forgetting pass while the
// spill awaits replay (the lease is what protects it, not luck of timing), and
// ONLY after the replay commits the fact and the rewrite removes the spill
// entry does the original resume its ORIGINAL-TTL age (never re-stamped).

func TestMemSpill_ProtectionHoldsUntilSafeRemoval(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, rel, ":memory:", 100)
	require.NoError(t, err)
	ts := NewTombstoneSet(rel, mockKV, 1)
	store.tombstones = ts
	lease := NewRetentionLease()
	store.SetRetentionLease(lease)

	now := time.Now().UnixMilli()
	expired := NewSnowflakeEventKey(1, now-10*24*3600*1000)
	ev := FullEvent{EventKey: expired, PartitionID: 1, EventType: "thinking_plan",
		EventSummary: "pending in spill past its TTL age", Timestamp: now - 10*24*3600*1000}
	require.NoError(t, store.StoreEvent(expired, ev)) // the original exists on the chain

	// The degradation belt spilled this key: the live MemSpill protects its
	// durable original until the replay lands (same wiring ErrorTrackingStore
	// installs via SetMemSpill — guard = the store's own retention surface).
	sp := NewMemSpill(t.TempDir() + "/spill.jsonl")
	sp.SetGuard(store)
	require.NoError(t, sp.Append(expired, ev))
	require.True(t, store.IsKeyProtected(expired), "the pending spill key protects its original")

	lease.MarkReady()
	lm := NewLifecycleManager(store, ts, DefaultLifecycleConfig())
	defer lm.Stop()
	lm.SweepOnce()
	assert.False(t, ts.IsTombstone(expired),
		"§5.9: a spill-pending original must survive the forgetting pass (protection reaches the scan)")

	// Safe removal: replay commits (idempotent), the spill entry is rewritten
	// away, and ONLY THEN the holder is dropped.
	n, rerr := sp.ReplayWithNotify(store, nil)
	require.NoError(t, rerr)
	require.Equal(t, 1, n)
	pending, perr := sp.PendingKeys()
	require.NoError(t, perr)
	require.Empty(t, pending, "the spill entry was removed only after the replay landed")

	lm.SweepOnce()
	assert.True(t, ts.IsTombstone(expired),
		"§5.9: after the release the original ages on its ORIGINAL timestamp (no re-stamp) and the next pass forgets it")
}

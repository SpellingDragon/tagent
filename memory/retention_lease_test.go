package memory

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// §2.8 core: a retained (unacked-recovery) original must survive TTL expiry while its
// lease is held, and resume age-based eviction on its ORIGINAL timestamp once released.
func TestRetentionLease_TTLProtectsUnackedOriginal(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, rel, ":memory:", 100)
	require.NoError(t, err)
	ts := NewTombstoneSet(rel, mockKV, 1)
	store.tombstones = ts
	lease := NewRetentionLease()
	store.SetRetentionLease(lease)
	lm := NewLifecycleManager(store, ts, DefaultLifecycleConfig())

	now := time.Now().UnixMilli()
	protectedOverdue := NewSnowflakeEventKey(1, now-10*24*3600*1000)
	require.NoError(t, store.StoreEvent(protectedOverdue, FullEvent{
		EventKey: protectedOverdue, PartitionID: 1, EventType: "thinking_plan",
		EventSummary: "unacked prepared fact", Timestamp: now - 10*24*3600*1000,
	}))
	lease.Protect(protectedOverdue) // the recovery owner holds this original

	unprotectedOverdue := NewSnowflakeEventKey(1, now-11*24*3600*1000)
	require.NoError(t, store.StoreEvent(unprotectedOverdue, FullEvent{
		EventKey: unprotectedOverdue, PartitionID: 1, EventType: "thinking_plan",
		EventSummary: "already acked", Timestamp: now - 11*24*3600*1000,
	}))

	lm.checkTTL()
	assert.False(t, ts.IsTombstone(protectedOverdue),
		"§2.8: a retained original must NOT be expired by TTL while its lease is held")
	assert.True(t, ts.IsTombstone(unprotectedOverdue),
		"an unleased overdue event must still expire")
	_, err = store.GetEvent(protectedOverdue)
	require.NoError(t, err, "protected original must stay readable for recovery")

	// After release the SAME original (timestamp never re-stamped) becomes eligible.
	lease.Release(protectedOverdue)
	lm.checkTTL()
	assert.True(t, ts.IsTombstone(protectedOverdue),
		"§2.8: after release the key resumes age-based expiry on its ORIGINAL timestamp")
}

// §2.8 core: an explicit delete of a protected key is refused (returns ErrEventProtected)
// without destroying the record; ref-counted release keeps protection until the last
// holder is gone, after which the delete succeeds.
func TestRetentionLease_DeleteRefusedWhileProtected(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, rel, ":memory:", 100)
	require.NoError(t, err)
	ts := NewTombstoneSet(rel, mockKV, 1)
	store.tombstones = ts
	lease := NewRetentionLease()
	store.SetRetentionLease(lease)

	key := NewSnowflakeEventKey(1, time.Now().UnixMilli())
	require.NoError(t, store.StoreEvent(key, FullEvent{
		EventKey: key, PartitionID: 1, EventType: "external_input",
		EventSummary: "receipt/original", Timestamp: time.Now().UnixMilli(),
	}))

	// Two holders (e.g. inbox envelope + spill) protect the same key.
	lease.Protect(key)
	lease.Protect(key)
	assert.True(t, store.IsKeyProtected(key))

	require.True(t, IsEventProtected(store.DeleteEvent(key)),
		"§2.8: explicit delete of a retained original must return protected, not destroy")
	_, err = store.GetEvent(key)
	require.NoError(t, err, "the durable original must be intact after a refused delete")

	lease.Release(key) // one holder gone — still protected by the other
	assert.True(t, store.IsKeyProtected(key))
	require.True(t, IsEventProtected(store.DeleteEvent(key)))

	lease.Release(key) // last holder gone
	assert.False(t, store.IsKeyProtected(key))
	require.NoError(t, store.DeleteEvent(key), "delete succeeds once the lease is fully released")
	_, err = store.GetEvent(key)
	assert.Error(t, err, "the event is gone after the released delete")
}

// §2.8 B: the background scanner must NOT destroy an overdue-but-retained original
// before the recovery owner has armed the lease (the restart race spec L117-119 forbids).
// The gate blocks the first checkTTL until MarkReady, after which the key expires on its
// ORIGINAL timestamp. (Deterministic: the gate guarantees checkTTL cannot have run pre-MarkReady.)
func TestRetentionLease_ScannerWaitsForLeaseReady(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, rel, ":memory:", 100)
	require.NoError(t, err)
	ts := NewTombstoneSet(rel, mockKV, 1)
	store.tombstones = ts
	lease := NewRetentionLease() // attached but NOT yet ready
	store.SetRetentionLease(lease)
	lm := NewLifecycleManager(store, ts, DefaultLifecycleConfig())
	defer lm.Stop()

	now := time.Now().UnixMilli()
	overdue := NewSnowflakeEventKey(1, now-10*24*3600*1000)
	require.NoError(t, store.StoreEvent(overdue, FullEvent{
		EventKey: overdue, PartitionID: 1, EventType: "thinking_plan",
		EventSummary: "unacked across restart", Timestamp: now - 10*24*3600*1000,
	}))

	lm.Start() // goroutine blocks at the lease-ready gate
	time.Sleep(50 * time.Millisecond)
	assert.False(t, ts.IsTombstone(overdue),
		"§2.8: the first destructive pass must not run before the lease is armed")

	// Recovery owner finishes rebuilding the lease from on-disk material → arm it.
	lease.MarkReady()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !ts.IsTombstone(overdue) {
		time.Sleep(10 * time.Millisecond)
	}
	assert.True(t, ts.IsTombstone(overdue),
		"§2.8: after the lease is armed the overdue key expires on its ORIGINAL timestamp")
}

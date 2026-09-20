package memory

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Baseline counter-examples at the store seam (task 1.4), reusing the §1.3
// faultKV/faultStore harness. The two store-internal counter-examples named in
// 1.4 are: (a) replay-after-cache-eviction must classify from the KV fact chain,
// never from a stale cache decision; (b) durability-barrier failure after a
// delete must not silently drop the live count.
//
// (a) is deterministic here and stays active as a regression lock (the current
// FileSegmentStore already content-checks on replay). (b) is already exercised
// by the existing barrier suite (segment_store_barrier_test.go:
// TestBarrierFailure_FailsCommitAndSparesCountThroughDecorations); 1.4 records
// it as covered there rather than duplicating a weaker variant — see evidence.md.
// ---------------------------------------------------------------------------

// TestCounter_ReplayAfterCacheEvictionClassifiesFromKV is the §1.4 fail-before
// for §2.4: an explicit replay's commit decision MUST be derived from the durable
// fact chain (idx present + content-checked), never from whether the key happens to
// be resident in the bounded LRU. Today completeOrphanCommit uses s.cache.Get(key)
// as the committed-oracle, so a cold-cache / post-restart replay of an ALREADY-
// COMMITTED fact is misclassified as ReplayRepaired and re-increments eventCount —
// a live F8 double-count in exactly the scenario ReplayEvent exists for (durable-
// inbox claim / mem_spill recovery after restart, when the cache is empty). §2.4
// removes the cache-oracle; classification now derives from the durable fact chain, so
// this is a PASS-AFTER regression guard — a cold-cache replay must classify
// AlreadyCommitted and must NOT re-increment (see evidence.md).
func TestCounter_ReplayAfterCacheEvictionClassifiesFromKV(t *testing.T) {
	// §2.4 removed the cache-oracle; classification now derives from the durable fact
	// chain, so this is a PASS-AFTER regression guard (previously a skipped fail-before).
	s := newFaultStore(t)
	key := NewSnowflakeEventKey(1, 0)
	evt := commitEvent(key, 1, "cold-replay-fact")

	require.NoError(t, s.StoreEvent(key, evt))
	require.EqualValues(t, 1, s.inner.GetStats().TotalEvents, "first commit must count exactly once")

	// Force a cold decision: evict the cache so only the KV fact chain remains.
	s.inner.cache.Remove(key)

	res, canonical, err := s.ReplayEvent(key, evt)
	require.NoError(t, err, "replay of an already-durable fact must not error on a cold cache")
	require.Equal(t, ReplayAlreadyCommitted, res,
		"§2.4: a cold-cache replay of an already-durable fact must classify AlreadyCommitted from the KV fact chain, not Repair")
	require.EqualValues(t, 1, s.inner.GetStats().TotalEvents,
		"§2.4/F8: replaying an already-committed fact must NOT re-increment the live count")
	require.Equal(t, evt.Content, canonical.Content, "replay must return the canonical stored fact verbatim")
}

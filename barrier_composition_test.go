package tagent

import (
	"testing"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

// §5.8 composition-root aggregate barrier: a build raises ONE ref-counted
// registration hold per shared store touched (dedup across agents) and the
// top-level buildAgent releases every hold once all agents were constructed and
// reconciled. Stores without a retention lease (no destructive scanner) are
// skipped — the barrier only exists where forgetting exists.

type countingHoldStore struct {
	*memory.InMemoryStore
	begins, ends int
}

func (s *countingHoldStore) BeginHold() { s.begins++ }
func (s *countingHoldStore) EndHold()   { s.ends++ }

func TestRuntimeConfig_StoreBarrierAggregatesAndReleases(t *testing.T) {
	rc := &runtimeConfig{}
	shared := &countingHoldStore{InMemoryStore: memory.NewInMemoryStore()}
	plain := memory.NewInMemoryStore() // no lease → nothing to pause

	rc.raiseStoreBarrier(shared)
	rc.raiseStoreBarrier(shared) // a second agent on the SAME store: deduped
	rc.raiseStoreBarrier(plain)  // non-holdable: silently skipped, no panic
	require.Equal(t, 1, shared.begins, "one registration window per store per build")

	rc.releaseStoreBarriers()
	require.Equal(t, 1, shared.ends, "the build top-level releases exactly what it raised")

	rc.releaseStoreBarriers() // re-entrant release: no double End
	require.Equal(t, 1, shared.ends)

	// A later build (hot-reload shell) re-raises on the same store: windows nest per build.
	rc.raiseStoreBarrier(shared)
	require.Equal(t, 2, shared.begins)
	rc.releaseStoreBarriers()
	require.Equal(t, 2, shared.ends)

	// nil-receiver safety (defensive; rc is normally always constructed).
	var nilRC *runtimeConfig
	require.NotPanics(t, func() { nilRC.raiseStoreBarrier(shared); nilRC.releaseStoreBarriers() })
	require.Equal(t, 2, shared.begins, "a nil rc never mutates the store's barrier")
}

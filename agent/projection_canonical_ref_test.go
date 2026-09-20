package agent

import (
	"encoding/json"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

// §4.6 (event-sourced-projection L7): the projection reference downstream of commit MUST be
// built from the CANONICAL fact the store holds/returned — never the current event's arrival
// time, a re-derived summary, or the original call object. This locks both the ref-from-
// canonical construction and the pre-execution backfill of a SELECTED outstanding input whose
// frozen fact is already on the chain but absent from the projection.

// A durable input whose frozen canonical fact carries a semantic Timestamp DIFFERENT from the
// current event's arrival time must project the CANONICAL time. Fail-before: the old ref used
// `evt.Timestamp.UnixMilli()` (arrival) → the projected timestamp would be ~now, not the frozen
// canonical value.
func TestPersistBusEvent_ProjectionRefUsesCanonicalTime(t *testing.T) {
	cm := newPreparedGateCM()
	const canonicalTS = int64(1700000000000) // fixed, well before any test run's time.Now()
	fact, err := json.Marshal(memory.FullEvent{
		EventKey:     555000111,
		PartitionID:  1,
		EventType:    "external_input",
		EventSummary: "canon-summary",
		Content:      "hello",
		Timestamp:    canonicalTS,
		Metadata:     map[string]string{"agent_name": "a"},
	})
	require.NoError(t, err)

	evt := evtWithPreparedClaim(fact)
	// evtWithPreparedClaim stamps Timestamp=time.Now(); arrival time must NOT leak into the ref.
	require.True(t, evt.Timestamp.UnixMilli() != canonicalTS, "precondition: arrival time differs from canonical")
	require.True(t, cm.persistBusEvent(evt))

	refs := cm.projection.GetAll()
	require.Len(t, refs, 1)
	require.Equal(t, canonicalTS, refs[0].Timestamp,
		"§4.6: the projection ref MUST carry the canonical fact's time, not the current event's arrival time")
	require.Equal(t, "canon-summary", refs[0].EventSummary, "ref summary comes from the canonical fact")
}

// Scenario「已有事实不等于当前请求已包含」: a SELECTED outstanding input whose frozen fact is
// ALREADY committed (replayed/already-on-chain) but which the cold-start snapshot/tail did NOT
// restore into the projection MUST still get its reference backfilled before execution — the
// "already" classification must not drop the input. Fail-before: the old code returned on
// `replayed` BEFORE appending, leaving the projection empty (input invisible to the model).
func TestPersistBusEvent_AlreadyCommittedSelectedInputIsBackfilled(t *testing.T) {
	cm := newPreparedGateCM()
	canonical := memory.FullEvent{
		EventKey:     777000999,
		PartitionID:  1,
		EventType:    "external_input",
		EventSummary: "pre-crash input",
		Content:      "hello",
		Timestamp:    1690000000000,
		Metadata:     map[string]string{"agent_name": "a"},
	}
	// Simulate the pre-crash commit: the fact is already on the chain.
	require.NoError(t, cm.memStore.StoreEvent(canonical.EventKey, canonical))
	require.Empty(t, cm.projection.GetAll(), "precondition: cold start did NOT restore this outstanding input's ref")

	fact, err := json.Marshal(canonical)
	require.NoError(t, err)

	// Re-claiming the outstanding envelope re-runs persistBusEvent → ReplayEvent classifies it
	// already-committed (replayed). The ref MUST be backfilled so the selected input is visible.
	require.True(t, cm.persistBusEvent(evtWithPreparedClaim(fact)), "a replayed selected input still reports committed")

	refs := cm.projection.GetAll()
	require.Len(t, refs, 1, "§4.6: an already-committed SELECTED outstanding input MUST be backfilled into the projection (not dropped by the 'already' classification)")
	require.Equal(t, canonical.EventKey, refs[0].EventKey)
	require.Equal(t, canonical.Timestamp, refs[0].Timestamp, "backfilled ref is built from the canonical fact")
}

// §4.6 clause「本批记录已投影集合防模型重试/压缩后重插」(structural): the selected fact is
// committed once, and re-running the same commit (e.g. a same-turn path that re-derives the
// ref, or a fresh→already transition) MUST NOT double-project — SessionProjection.Append is
// EventKey-idempotent, so the fresh commit and the replayed backfill of the SAME key converge
// to exactly one ref (never a duplicate that a later compaction would have to fold twice).
func TestPersistBusEvent_ReCommitDoesNotDoubleProject(t *testing.T) {
	cm := newPreparedGateCM()
	fact, err := json.Marshal(memory.FullEvent{
		EventKey: 31337, PartitionID: 1, EventType: "external_input",
		EventSummary: "once", Content: "hello", Timestamp: 1680000000000,
		Metadata: map[string]string{"agent_name": "a"},
	})
	require.NoError(t, err)
	require.True(t, cm.persistBusEvent(evtWithPreparedClaim(fact))) // fresh commit → appended
	require.True(t, cm.persistBusEvent(evtWithPreparedClaim(fact))) // replayed → idempotent re-assert
	require.Len(t, cm.projection.GetAll(), 1, "the same selected fact must project exactly once across re-commit")
}

package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

// TestSubmitDurableBatch_DeterministicStoreConflictIsolates — the §8.5 four-
// state closure at the commit layer: a frozen fact key that collides with
// DIFFERENT content can never succeed on replay (the key never changes), so
// the protocol must ISOLATE the envelope through the same quarantine exit the
// prepare-conflict path uses — NOT retry it forever as transient I/O.
func TestSubmitDurableBatch_DeterministicStoreConflictIsolates(t *testing.T) {
	root := t.TempDir()
	store, bus, ta := r30Stack(root)
	defer store.Close()

	// Envelope #1 commits normally.
	_, err := bus.PublishContext(context.Background(), durableMsg("good-input"))
	require.NoError(t, err)
	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, batch, 1)
	require.Equal(t, submitOK, func() submitStatus { st, _ := ta.prepareBatchFacts(batch); return st }())
	require.True(t, ta.contextManager.persistBusEvent(batch[0]))
	ta.finishDurableBatch(context.Background(), batch, batch, completedOutcome())

	// Envelope #2 claims a prepared fact with envelope #1's key and DIFFERENT
	// content (the exact shape a cross-process snowflake collision leaves).
	_, err = bus.PublishContext(context.Background(), durableMsg("colliding-input"))
	require.NoError(t, err)
	batch2, err := bus.Pull(context.Background())
	require.NoError(t, err)
	require.Len(t, batch2, 1)
	require.Equal(t, submitOK, func() submitStatus { st, _ := ta.prepareBatchFacts(batch2); return st }())
	refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 50})
	require.NoError(t, err)
	var goodKey int64
	for _, r := range refs {
		if r.EventType == "external_input" {
			goodKey = r.EventKey
		}
	}
	require.NotZero(t, goodKey)
	ev := batch2[0]
	var frozen map[string]any
	require.NoError(t, json.Unmarshal(ev.claim.PreparedFact, &frozen))
	frozen["event_key"] = goodKey            // collide with the committed fact…
	frozen["content"] = "DIFFERENT-MATERIAL" // …with different content
	evil, err := json.Marshal(frozen)
	require.NoError(t, err)
	ev.claim.PreparedFact = evil

	out := ta.submitDurableBatch(context.Background(), batch2, batch2)
	require.Equal(t, submitConflict, out.status, "a deterministic conflict is NEVER transient")
	require.Equal(t, ev.claim.Path, out.conflict)

	// The envelope was ISOLATED out of the inbox (quarantined, originals kept)
	// and the committed chain is untouched by the rejected write.
	q, err := filepath.Glob(filepath.Join(root, "inbox", "inbox-v2", "quarantine", "*"))
	require.NoError(t, err)
	require.Len(t, q, 1, "the conflicting envelope is isolated, not retried forever")
	refs, err = store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 50})
	require.NoError(t, err)
	for _, r := range refs {
		full, gerr := store.GetEvent(r.EventKey)
		require.NoError(t, gerr)
		require.NotContains(t, full.Content, "DIFFERENT-MATERIAL", "the colliding write never entered the chain")
	}
}

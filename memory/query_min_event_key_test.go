package memory

import (
	"sort"
	"testing"

	"github.com/SpellingDragon/tagent/event"
	"github.com/stretchr/testify/require"
)

// TestQueryEvents_MinEventKey verifies the write-order key bound on both
// store implementations: strictly-greater filtering, the 0 = no-bound
// default, and the dual-time divergence case that makes a StartTime
// approximation wrong — an async write-back event with an OLD semantic
// Timestamp but a NEW write-order key must still be returned (replay tails
// depend on the WRITE axis, never semantic time; see QueryOptions.MinEventKey
// and the TIME CONTRACT on FullEvent).
func TestQueryEvents_MinEventKey(t *testing.T) {
	const pid = 7
	base := int64(1_700_000_000_000) // fixed ms epoch for deterministic keys
	keys := []int64{
		NewSnowflakeEventKey(pid, base),
		NewSnowflakeEventKey(pid, base+5_000),
		NewSnowflakeEventKey(pid, base+10_000),
		NewSnowflakeEventKey(pid, base+15_000),
	}

	stores := map[string]func(t *testing.T) MemoryStore{
		"InMemoryStore": func(t *testing.T) MemoryStore { return NewInMemoryStore() },
		"FileSegmentStore": func(t *testing.T) MemoryStore {
			s, err := NewFileSegmentStore(newMockKV(), nil, ":memory:", 100)
			require.NoError(t, err)
			return s
		},
	}

	keySet := func(refs []EventReference) []int64 {
		out := make([]int64, 0, len(refs))
		for _, r := range refs {
			out = append(out, r.EventKey)
		}
		sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
		return out
	}

	for name, mk := range stores {
		t.Run(name, func(t *testing.T) {
			store := mk(t)
			for i, k := range keys {
				ts := base + int64(i)*1_000
				if i == len(keys)-1 {
					// Straggler shape (async write-back): semantic Timestamp is
					// OLD (bus-arrival moment) while EventKey is the NEWEST.
					ts = base - 60_000
				}
				err := store.StoreEvent(k, FullEvent{
					EventType:    event.TypeExternalInput,
					EventSummary: "evt",
					Timestamp:    ts,
					Content:      "content",
				})
				require.NoError(t, err)
			}

			// Strictly greater on the write axis — includes the straggler.
			refs, err := store.QueryEvents(QueryOptions{PartitionID: pid, MinEventKey: keys[1]})
			require.NoError(t, err)
			require.Equal(t, keys[2:], keySet(refs), "MinEventKey must cut on EventKey (strictly greater), keeping the old-Timestamp straggler")

			// Zero = no bound.
			refs, err = store.QueryEvents(QueryOptions{PartitionID: pid})
			require.NoError(t, err)
			require.Equal(t, keys, keySet(refs), "MinEventKey=0 must not filter")

			// Contrast: a StartTime approximation WOULD drop the straggler —
			// the exact divergence MinEventKey exists to avoid.
			refs, err = store.QueryEvents(QueryOptions{PartitionID: pid, StartTime: base})
			require.NoError(t, err)
			require.Equal(t, keys[:3], keySet(refs), "semantic-time filter drops the straggler (why tails must not use StartTime)")
		})
	}
}

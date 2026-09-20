package memory

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// nonReplayerStore is a MemoryStore that deliberately does NOT implement EventReplayer
// (§2.6): spill replay against it must FAIL the capability check and retain every
// original, never degrade to the removed GetEvent+StoreEvent weak fallback.
type nonReplayerStore struct{ in *InMemoryStore }

func (s *nonReplayerStore) StoreEvent(k int64, e FullEvent) error { return s.in.StoreEvent(k, e) }
func (s *nonReplayerStore) GetEvent(k int64) (*FullEvent, error)  { return s.in.GetEvent(k) }
func (s *nonReplayerStore) GetEvents(ks []int64) ([]FullEvent, error) {
	return s.in.GetEvents(ks)
}
func (s *nonReplayerStore) QueryEvents(q QueryOptions) ([]EventReference, error) {
	return s.in.QueryEvents(q)
}
func (s *nonReplayerStore) SearchByEmbedding(e []float32, n int) ([]EventReference, error) {
	return s.in.SearchByEmbedding(e, n)
}
func (s *nonReplayerStore) StoreEventWithEmbedding(k int64, e FullEvent, emb []float32) error {
	return s.in.StoreEventWithEmbedding(k, e, emb)
}
func (s *nonReplayerStore) SupportsVectorSearch() bool { return s.in.SupportsVectorSearch() }
func (s *nonReplayerStore) DeleteEvent(k int64) error  { return s.in.DeleteEvent(k) }
func (s *nonReplayerStore) GetStats() StoreStats       { return s.in.GetStats() }

func TestMemSpill_ReplayWithoutReplayerRefused(t *testing.T) {
	spill := NewMemSpill(t.TempDir() + "/spill.jsonl")
	key := NewSnowflakeEventKey(1, 1704067200*1000)
	require.NoError(t, spill.Append(key, FullEvent{
		EventKey: key, PartitionID: 1, EventType: "external_input", Timestamp: 1704067200000,
	}))
	require.Equal(t, 1, spill.Len())

	n, err := spill.Replay(&nonReplayerStore{in: NewInMemoryStore()})
	require.Error(t, err, "§2.6: replay against a non-EventReplayer store must be refused, not weakly degraded")
	require.Equal(t, 0, n)
	require.Equal(t, 1, spill.Len(), "§2.6: the spill original must be retained, never consumed by a GetEvent weak fallback")
}

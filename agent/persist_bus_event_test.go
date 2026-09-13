package agent

import (
	"errors"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// failStore wraps a real store with an always-failing StoreEvent — the
// degraded-window shape the stored-gate guards against.
type failStore struct {
	*memory.InMemoryStore
}

func (f *failStore) StoreEvent(key int64, e memory.FullEvent) error {
	return errors.New("disk full")
}

// TestPersistBusEvent_StoredGate (event-sourced-projection D2, fresh-eyes
// E①): a failed StoreEvent must NOT append to the projection — the
// projection may never hold a ref the fact chain lacks (rebuild invariant:
// projection = fold of the fact chain; spill recovery re-appends later).
func TestPersistBusEvent_StoredGate(t *testing.T) {
	mkEvt := func() *AgentEvent {
		return &AgentEvent{
			Message:   &model.Message{Role: model.RoleUser, Content: "hi"},
			Timestamp: time.Now(),
		}
	}

	// Failure → gated (fail-before: the old behavior appended anyway).
	gated := &ContextManager{
		partitionID: 1,
		memStore:    &failStore{memory.NewInMemoryStore()},
		projection:  compress.NewSessionProjection(),
	}
	gated.persistBusEvent(mkEvt())
	require.Equal(t, 0, gated.projection.Len(),
		"StoreEvent failure must gate the projection Append (stored-gate)")

	// Success → appended (same-point semantics unchanged).
	ok := &ContextManager{
		partitionID: 1,
		memStore:    memory.NewInMemoryStore(),
		projection:  compress.NewSessionProjection(),
	}
	ok.persistBusEvent(mkEvt())
	require.Equal(t, 1, ok.projection.Len())

	// nil store (test/bypass scenarios) → still appends (previous behavior).
	nilStore := &ContextManager{
		partitionID: 1,
		projection:  compress.NewSessionProjection(),
	}
	nilStore.persistBusEvent(mkEvt())
	require.Equal(t, 1, nilStore.projection.Len())
}

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

// nonReplayStore hides the embedded store's EventReplayer method: only the
// methods of the memory.MemoryStore *interface* are promoted, and that set has
// no ReplayEvent. It satisfies memory.MemoryStore but NOT memory.EventReplayer
// — the exact shape the durable-mode capability gate must refuse.
type nonReplayStore struct {
	memory.MemoryStore
}

// TestNewTagentAgent_DurableRequiresReplayCapableStore (task 3.5): a durable
// inbox wired to a store without explicit replay capability would silently
// degrade "at-least-once" delivery to a possible double-write, so construction
// must fail loud. Without durability configured the same store is accepted —
// the capability is required only when the inbox barrier is actually active.
func TestNewTagentAgent_DurableRequiresReplayCapableStore(t *testing.T) {
	mock := &mockModel{info: model.Info{Name: "test"}}

	_, err := NewTagentAgent(&TagentConfig{
		Model:             mock,
		MemoryStore:       nonReplayStore{memory.NewInMemoryStore()},
		BusSpillDir:       t.TempDir(),
		MaxToolIterations: 1,
		MaxTokens:         1000,
	})
	require.Error(t, err, "durable inbox must refuse a non-replay store")
	require.Contains(t, err.Error(), "replay-capable")

	ta, err := NewTagentAgent(&TagentConfig{
		Model:             mock,
		MemoryStore:       nonReplayStore{memory.NewInMemoryStore()},
		MaxToolIterations: 1,
		MaxTokens:         1000,
	})
	require.NoError(t, err, "volatile bus needs no replay capability")
	require.NotNil(t, ta)
	_ = ta.Close()
}

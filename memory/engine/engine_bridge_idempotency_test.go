package engine

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/memory"
	membed "github.com/SpellingDragon/tagent/memory/embedder"

	"github.com/stretchr/testify/require"
)

// §2.7① engine index is idempotent by key: re-submitting the same EventKey overwrites
// the map entry and must NOT create a duplicate logical vector item.
func TestEngineIndex_IdempotentByKey(t *testing.T) {
	store := memory.NewInMemoryStore()
	emb := membed.NewMockEmbedder(64)
	eng := NewInMemoryEngine(store, emb, EngineConfig{EmbedFlushInterval: 5 * time.Millisecond})
	defer eng.Close()

	k := memory.NewSnowflakeEventKey(1, testBaseMs)
	evt := memory.IndexableEvent{
		EventKey: k, PartitionID: 1, EventType: TypeExternalInputProbe,
		Text: "same text content", Timestamp: testBaseMs,
	}
	require.NoError(t, eng.Index(context.Background(), evt))
	require.NoError(t, eng.Index(context.Background(), evt)) // re-index the SAME key

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && eng.Stats().VectorCount < 1 {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(30 * time.Millisecond) // let any hypothetical duplicate insert land
	require.EqualValues(t, 1, eng.Stats().VectorCount,
		"§2.7①: re-indexing the same EventKey must not create a duplicate logical vector entry")
}

// §2.7② an already-committed replay must not re-increment the capacity delta: the bridge
// skips the capacityHook for ReplayAlreadyCommitted (double-increment is the F8 counterexample).
func TestEngineBridge_AlreadyReplayDoesNotDoubleIncrement(t *testing.T) {
	store := memory.NewInMemoryStore()
	bridge := NewEngineBridge(store, nil) // no engine → isolate the capacityHook delta behaviour
	provider, ok := bridge.(memory.CapacityHookProvider)
	require.True(t, ok, "bridge must expose CapacityHookProvider")
	hookCount := 0
	provider.SetCapacityHook(func(int64, int, string) { hookCount++ })

	key := memory.NewSnowflakeEventKey(1, testBaseMs)
	ev := memory.FullEvent{
		EventKey: key, PartitionID: 1, EventType: TypeExternalInputProbe,
		Content: "x", Timestamp: testBaseMs,
	}
	require.NoError(t, bridge.StoreEvent(key, ev))
	require.Equal(t, 1, hookCount, "a new write fires the capacity hook exactly once")

	res, _, err := bridge.(memory.EventReplayer).ReplayEvent(key, ev)
	require.NoError(t, err)
	require.Equal(t, memory.ReplayAlreadyCommitted, res)
	require.Equal(t, 1, hookCount,
		"§2.7②: an already-committed replay must NOT re-increment the capacity delta")
}

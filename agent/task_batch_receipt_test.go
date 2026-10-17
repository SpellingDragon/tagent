// 契约: docs/wiki/agent/task-lifecycle.md#recoverable-invocations
package agent

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

// TestBatchTaskReceiptConsumesEveryFrozenResult closes all producer receipts with one formal summary consumption and prevents restart replays.
func TestBatchTaskReceiptConsumesEveryFrozenResult(t *testing.T) {
	store := &inlineGuardStore{InMemoryStore: memory.NewInMemoryStore(), guard: memory.NewRetentionLease()}
	owner := outboxOwner("batch-owner", 7, store)
	var retired []task.BatchRetired
	for i := 0; i < 3; i++ {
		tk := task.NewTaskFixture(fmt.Sprintf("batch-%d", i), "expired work", task.TaskFailed, time.Now())
		sig := task.SettleSignal{Kind: task.SettleFailed, Output: "expired", Lineage: task.LineageRetired, RoundID: 1, ProducerRound: 1, Seq: 2, ObservedAt: time.Now()}
		sig.SourceID = task.SignalSourceID(tk.ID, 1, 2)
		retired = append(retired, task.BatchRetired{Task: tk, Sig: sig})
	}
	require.True(t, owner.taskRecords.acceptBatch(retired, "", owner.persistentBus))
	require.Len(t, owner.taskRecords.delivery.leases, 3)
	batch := owner.persistentBus.TryPull()
	require.Len(t, batch, 1)
	require.Equal(t, SourceTask, batch[0].Source)
	consumeOutboxInput(t, owner, batch[0])
	require.Empty(t, owner.taskRecords.delivery.leases)
	require.Zero(t, store.guard.Len())
	refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{7}, EventTypes: []string{tagentevent.TypeTaskSignal}, Limit: 10})
	require.NoError(t, err)
	require.Len(t, refs, 3)
	for _, ref := range refs {
		fact, err := store.GetEvent(ref.EventKey)
		require.NoError(t, err)
		var raw taskSignalRecord
		require.NoError(t, json.Unmarshal([]byte(fact.Content), &raw))
		require.True(t, taskBatchSourceMatches(*fact, raw, *batch[0].volatileFact))
	}
	restarted := outboxOwner("batch-owner", 7, store)
	require.NoError(t, restarted.RecoverTaskOutbox())
	require.Empty(t, restarted.persistentBus.TryPull(), "the consumed summary proves each original result")
	require.Zero(t, store.guard.Len())
}

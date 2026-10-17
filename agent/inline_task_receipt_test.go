// 契约: docs/wiki/agent/task-lifecycle.md#recoverable-invocations
package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

type inlineGuardStore struct {
	*memory.InMemoryStore
	guard *memory.RetentionLease
}

func (s *inlineGuardStore) ProtectKey(key int64) { s.guard.Protect(key) }
func (s *inlineGuardStore) ReleaseKey(key int64) { s.guard.Release(key) }
func (s *inlineGuardStore) ArmRetention()        { s.guard.MarkReady() }
func (s *inlineGuardStore) BeginHold()           { s.guard.BeginHold() }
func (s *inlineGuardStore) EndHold()             { s.guard.EndHold() }

func inlineReceiptFixture(store memory.MemoryStore) (*ContextManager, *taskRecordSink, *task.Task, task.SettleSignal) {
	cm := &ContextManager{name: "inline-owner", partitionID: 7, memStore: store, inlineTaskTurnToken: "batch-a"}
	sink := &taskRecordSink{cm: cm}
	cm.taskSignalJournal = sink
	tk := task.NewTaskFixture("inline-result", "inline work", task.TaskCompleted, time.Now())
	tk.Spec.Kind = "command"
	tk.Spec.Origin = map[string]string{"task_inline_turn": "batch-a"}
	sig := task.SettleSignal{Kind: task.SettleCompleted, Output: "inline result", RoundID: 1, ProducerRound: 1, Seq: 1, ObservedAt: time.Now(), WaitFor: task.WaitExit}
	sig.SourceID = task.SignalSourceID(tk.ID, 1, 1)
	return cm, sink, tk, sig
}

// TestInlineTaskReceiptBelongsToExactBatch releases only the journal's own retention after the originating model turn consumes the result.
func TestInlineTaskReceiptBelongsToExactBatch(t *testing.T) {
	store := &inlineGuardStore{InMemoryStore: memory.NewInMemoryStore(), guard: memory.NewRetentionLease()}
	cm, sink, tk, sig := inlineReceiptFixture(store)
	require.True(t, sink.acceptSignal(tk, sig, "", nil))
	refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{7}, EventTypes: []string{tagentevent.TypeTaskSignal}})
	require.NoError(t, err)
	require.Len(t, refs, 1)
	fact, err := store.GetEvent(refs[0].EventKey)
	require.NoError(t, err)
	var raw taskSignalRecord
	require.NoError(t, json.Unmarshal([]byte(fact.Content), &raw))
	store.ProtectKey(fact.EventKey)
	cm.inlineTaskTurnToken = "another-batch"
	require.True(t, finishInlineSignalsForTurn(context.Background(), cm))
	require.False(t, inlineTaskSignalConsumed(store, *fact, raw))
	require.Equal(t, 2, store.guard.Holders(fact.EventKey))
	cm.inlineTaskTurnToken = "batch-a"
	require.True(t, finishInlineSignalsForTurn(context.Background(), cm))
	require.True(t, inlineTaskSignalConsumed(store, *fact, raw))
	require.Empty(t, sink.delivery.leases)
	require.Equal(t, 1, store.guard.Holders(fact.EventKey), "a separate retention owner keeps its own reference")
	require.Zero(t, store.guard.Holders(raw.ReceiptKey))
	require.True(t, finishInlineSignalsForTurn(context.Background(), cm))
	require.Equal(t, 1, store.guard.Holders(fact.EventKey), "repeat completion cannot release another owner's reference")
}

// TestInlineTaskReceiptRepairsFailedRawCommit commits the original bytes and receipts without repeating the native result delivery.
func TestInlineTaskReceiptRepairsFailedRawCommit(t *testing.T) {
	store := &journalFailureStore{InMemoryStore: memory.NewInMemoryStore()}
	cm, sink, tk, sig := inlineReceiptFixture(store)
	store.reject(tagentevent.TypeTaskSignal, "")
	require.False(t, sink.acceptSignal(tk, sig, "", nil))
	original := sink.delivery.pending[sig.SourceID]
	require.NotNil(t, original)
	store.reject("", "")
	require.True(t, finishInlineSignalsForTurn(context.Background(), cm))
	var raw taskSignalRecord
	require.NoError(t, json.Unmarshal([]byte(original.fact.Content), &raw))
	require.True(t, inlineTaskSignalConsumed(store, original.fact, raw))
	require.True(t, sink.acceptSignal(tk, sig, "", nil))
	require.Empty(t, sink.delivery.pending)
	require.Empty(t, sink.delivery.leases)
	refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{7}, EventTypes: []string{tagentevent.TypeTaskSignal, tagentevent.TypeTaskSignalReceipt, tagentevent.TypeInputDisposition}})
	require.NoError(t, err)
	require.Len(t, refs, 3)
}

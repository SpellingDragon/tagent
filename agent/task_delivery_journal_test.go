// 契约: docs/wiki/agent/task-lifecycle.md#recoverable-invocations
package agent

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

type journalFailureStore struct {
	*memory.InMemoryStore
	mu                  sync.Mutex
	failType, failPhase string
}

func (s *journalFailureStore) reject(kind, phase string) {
	s.mu.Lock()
	s.failType, s.failPhase = kind, phase
	s.mu.Unlock()
}

func (s *journalFailureStore) ReplayEvent(key int64, fact memory.FullEvent) (memory.ReplayResult, memory.FullEvent, error) {
	s.mu.Lock()
	fail := fact.EventType == s.failType && (s.failPhase == "" || fact.Metadata["admission_phase"] == s.failPhase)
	s.mu.Unlock()
	if fail {
		return memory.ReplayNew, fact, errors.New("injected journal write failure")
	}
	return s.InMemoryStore.ReplayEvent(key, fact)
}

type journalActivationDetector struct {
	*task.ManualDetector
	activations *atomic.Int32
}

func (d *journalActivationDetector) Start() {
	d.activations.Add(1)
	d.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "committed result"})
	d.Done()
	d.FireStop()
}

// TestTaskJournalAdmissionAndBindingGateActivation rejects work before a failed admission or resource binding can execute it.
func TestTaskJournalAdmissionAndBindingGateActivation(t *testing.T) {
	store := &journalFailureStore{InMemoryStore: memory.NewInMemoryStore()}
	ta, err := NewTagentAgent(&TagentConfig{Name: "journal-gate", Model: &echoModel{}, MemoryStore: store})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, ta.Close()) })
	var prepared, activated atomic.Int32
	spec := task.TaskSpec{Kind: "command", Desc: "guarded work", Declarative: &task.Declarative{Kind: "command", Desc: "guarded work"}}
	spec.Start = func(tk *task.Task) (task.TaskSpec, task.SettleDetector, error) {
		prepared.Add(1)
		return tk.SpecSnapshot(), &journalActivationDetector{ManualDetector: task.NewManualDetector(), activations: &activated}, nil
	}
	store.reject(tagentevent.TypeTaskSpawned, "accepted")
	result := ta.taskManager.Spawn(spec, nil)
	require.NotEmpty(t, result.Blocked)
	require.Zero(t, prepared.Load())
	require.Zero(t, activated.Load())
	store.reject(tagentevent.TypeTaskSpawned, "started")
	result = ta.taskManager.Spawn(spec, nil)
	require.NotEmpty(t, result.Blocked)
	require.EqualValues(t, 1, prepared.Load())
	require.Zero(t, activated.Load())
	store.reject("", "")
	result = ta.taskManager.Spawn(spec, nil)
	require.Empty(t, result.Blocked)
	require.True(t, result.Settled)
	require.Equal(t, "committed result", result.Signal.Output)
	require.EqualValues(t, 1, activated.Load())
}

// TestTaskJournalRetriesFrozenResultAndReceipt preserves one raw result and one accepted delivery across write failures.
func TestTaskJournalRetriesFrozenResultAndReceipt(t *testing.T) {
	store := &journalFailureStore{InMemoryStore: memory.NewInMemoryStore()}
	cm := &ContextManager{name: "journal-result", partitionID: 7, memStore: store}
	sink := &taskRecordSink{cm: cm}
	tk := task.NewTaskFixture("full-task-id", "large result", task.TaskCompleted, time.Now())
	tk.Spec.Kind = "command"
	sig := task.SettleSignal{Kind: task.SettleCompleted, Output: strings.Repeat("exact result\n", 1000), RoundID: 2, ProducerRound: 2, Seq: 3, ObservedAt: time.Now(), WaitFor: task.WaitExit}
	sig.SourceID = task.SignalSourceID(tk.ID, sig.RoundID, sig.Seq)
	accepted := 0
	var delivered *AgentEvent
	accept := func(evt *AgentEvent) bool { accepted++; delivered = evt; return true }
	workspace := t.TempDir()
	store.reject(tagentevent.TypeTaskSignal, "")
	require.False(t, sink.acceptSignal(tk, sig, workspace, accept))
	require.Zero(t, accepted, "raw result commit must precede receiver acceptance")
	store.reject(tagentevent.TypeTaskSignalReceipt, "")
	require.False(t, sink.acceptSignal(tk, sig, workspace, accept))
	require.Equal(t, 1, accepted)
	first := delivered
	store.reject("", "")
	require.True(t, sink.acceptSignal(tk, sig, workspace, accept))
	require.Equal(t, 1, accepted, "receipt retry must not republish the accepted result")
	require.Same(t, first, delivered)
	refs, err := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{7}, EventTypes: []string{tagentevent.TypeTaskSignal, tagentevent.TypeTaskSignalReceipt}, Limit: 10})
	require.NoError(t, err)
	require.Len(t, refs, 2)
	var raw, receipt *memory.FullEvent
	for _, ref := range refs {
		fact, err := store.GetEvent(ref.EventKey)
		require.NoError(t, err)
		if fact.EventType == tagentevent.TypeTaskSignal {
			raw = fact
		} else {
			receipt = fact
		}
	}
	require.NotNil(t, raw)
	require.NotNil(t, receipt)
	require.Equal(t, tagentevent.FormatEventKey(raw.EventKey), receipt.Content)
	require.Equal(t, sig.SourceID, receipt.Metadata[tagentevent.MetaKeySourceEventID])
	require.Equal(t, raw.Timestamp, receipt.Timestamp)
	var frozen taskSignalRecord
	require.NoError(t, json.Unmarshal([]byte(raw.Content), &frozen))
	require.Equal(t, sig.Output, frozen.Output)
	require.Equal(t, receipt.EventKey, frozen.ReceiptKey)
	require.Empty(t, sink.delivery.pending)
}

// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
)

type outboxGuardStore struct {
	*memory.InMemoryStore
	mu   sync.Mutex
	refs map[int64]int
}

func (s *outboxGuardStore) ProtectKey(key int64) { s.mu.Lock(); defer s.mu.Unlock(); s.refs[key]++ }
func (s *outboxGuardStore) ReleaseKey(key int64) { s.mu.Lock(); defer s.mu.Unlock(); s.refs[key]-- }
func (s *outboxGuardStore) ArmRetention()        {}
func (s *outboxGuardStore) BeginHold()           {}
func (s *outboxGuardStore) EndHold()             {}
func (s *outboxGuardStore) held(key int64) int   { s.mu.Lock(); defer s.mu.Unlock(); return s.refs[key] }

func outboxOwner(name string, partition int, store memory.MemoryStore) *TagentAgent {
	cm := &ContextManager{name: name, partitionID: partition, memStore: store}
	sink := &taskRecordSink{cm: cm}
	ta := &TagentAgent{name: name, memStore: store, contextManager: cm, taskRecords: sink, persistentBus: NewEventBus(), settleSinks: newSettleSinkRegistry()}
	sink.owner, cm.taskSignalJournal = ta, sink
	cm.taskSignalOwnerResolver = ta.resolveContinuationOwner
	return ta
}

func outboxFact(t *testing.T, owner *TagentAgent, roundOwner string, inline bool) (memory.FullEvent, taskSignalRecord) {
	t.Helper()
	raw := registrySignal("task", 1, 4, time.Now())
	raw.RoundOwner, raw.Inline = roundOwner, inline
	if inline {
		raw.InlineTurnToken, raw.InlineReceiptKey = "original-turn", registryKey(950)
	}
	key := registryKey(200)
	raw.Event.Metadata = map[string]any{"task_id": raw.TaskID, tagentevent.MetaKeyTaskRoundID: "1", tagentevent.MetaKeyTaskSignalSeq: "4", tagentevent.MetaKeyTaskRoundOwner: roundOwner,
		"task_signal_key": tagentevent.FormatEventKey(key), "task_signal_owner": owner.name, tagentevent.MetaKeySettleWake: "decision", tagentevent.MetaKeySettleWaitSatisfied: "true", tagentevent.MetaKeySettleFinal: "true"}
	md := signalMetadata(raw)
	md[tagentevent.MetaKeyAgentName] = owner.name
	fact := storeRegistryFact(t, owner.memStore, key, tagentevent.TypeTaskSignal, raw, md, raw.ObservedAt)
	return fact, raw
}

func consumeOutboxInput(t *testing.T, owner *TagentAgent, evt *AgentEvent) {
	t.Helper()
	cm := owner.contextManager
	prepareTaskReceiptKeys(cm, []*AgentEvent{evt})
	if evt.volatileFact == nil {
		fact := cm.buildBusFact(evt)
		require.NoError(t, cm.memStore.StoreEvent(fact.EventKey, fact))
		evt.volatileFact, evt.volatileCommitted = &fact, true
	}
	require.True(t, consumeTaskInputs(context.Background(), cm, []*AgentEvent{evt}, "processed"))
}

// TestTaskOutboxReplaysAcceptedVolatileInputUntilFormalConsumption 钉住 易失受理后的崩溃仍恢复原信号，正式消费才释放本归属的保留引用。
func TestTaskOutboxReplaysAcceptedVolatileInputUntilFormalConsumption(t *testing.T) {
	store := &outboxGuardStore{InMemoryStore: memory.NewInMemoryStore(), refs: make(map[int64]int)}
	owner := outboxOwner("producer", sinkPartition, store)
	fact, raw := outboxFact(t, owner, "", false)
	storeRegistryReceipt(t, store, fact, raw)
	store.ProtectKey(fact.EventKey)
	require.NoError(t, owner.RecoverTaskOutbox())
	require.NoError(t, owner.RecoverTaskOutbox())
	batch := owner.persistentBus.TryPull()
	require.Len(t, batch, 1)
	require.Equal(t, raw.Event.ID, batch[0].ID)
	require.True(t, raw.Event.Timestamp.Equal(batch[0].Timestamp))
	require.Equal(t, 2, store.held(fact.EventKey))
	require.Equal(t, 1, store.held(raw.ReceiptKey))
	consumeOutboxInput(t, owner, batch[0])
	require.Equal(t, 1, store.held(fact.EventKey))
	require.Zero(t, store.held(raw.ReceiptKey))
	require.NoError(t, owner.RecoverTaskOutbox())
	require.Empty(t, owner.persistentBus.TryPull())
	require.Equal(t, 1, store.held(fact.EventKey))
}

// TestTaskOutboxHandoffUsesRecipientProofAcrossSeparateStores 钉住 转交到另一存储的消费证明仍能关闭原生产者outbox，并释放原归属引用。
func TestTaskOutboxHandoffUsesRecipientProofAcrossSeparateStores(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprint(shared), func(t *testing.T) {
			producerStore := &outboxGuardStore{InMemoryStore: memory.NewInMemoryStore(), refs: make(map[int64]int)}
			recipientStore := producerStore
			if !shared {
				recipientStore = &outboxGuardStore{InMemoryStore: memory.NewInMemoryStore(), refs: make(map[int64]int)}
			}
			producer := outboxOwner("producer", sinkPartition, producerStore)
			recipient := outboxOwner("recipient", sinkPartition+1, recipientStore)
			resolve := func(name string) *TagentAgent {
				if name == producer.name {
					return producer
				}
				if name == recipient.name {
					return recipient
				}
				return nil
			}
			producer.config = &TagentConfig{ContinuationOwnerResolver: resolve}
			recipient.config = &TagentConfig{ContinuationOwnerResolver: resolve}
			recipient.contextManager.taskSignalOwnerResolver = resolve
			r := producer.continuationRegistry()
			r.byID["old-parent"] = &continuation{state: continuationCheckpoint{InvocationID: "old-parent", Outcome: "completed", Handoff: &ContinuationContext{ParentAgent: recipient.name}}, listeners: make(map[chan *event.Event]struct{})}
			fact, raw := outboxFact(t, producer, "old-parent", false)
			storeRegistryReceipt(t, producerStore, fact, raw)
			require.NoError(t, producer.RecoverTaskOutbox())
			require.Empty(t, producer.persistentBus.TryPull())
			batch := recipient.persistentBus.TryPull()
			require.Len(t, batch, 1)
			consumeOutboxInput(t, recipient, batch[0])
			require.Zero(t, producerStore.held(fact.EventKey))
			require.Zero(t, producerStore.held(raw.ReceiptKey))
			require.NoError(t, producer.RecoverTaskOutbox())
			require.Empty(t, recipient.persistentBus.TryPull())
		})
	}
}

// TestTaskOutboxDoesNotTurnInlineResultsIntoBusInputs 钉住 已内联返回的原生结果只保留精确票据，不被恢复成第二条后台结果。
func TestTaskOutboxDoesNotTurnInlineResultsIntoBusInputs(t *testing.T) {
	store := &outboxGuardStore{InMemoryStore: memory.NewInMemoryStore(), refs: make(map[int64]int)}
	owner := outboxOwner("producer", sinkPartition, store)
	fact, raw := outboxFact(t, owner, "private-invocation", true)
	storeRegistryReceipt(t, store, fact, raw)
	require.NoError(t, owner.RecoverTaskOutbox())
	require.Empty(t, owner.persistentBus.TryPull())
	require.Equal(t, 1, store.held(fact.EventKey))
	require.Equal(t, 1, store.held(raw.ReceiptKey))
}

// TestTaskOutboxConsumesExactInlineProofWithoutASecondDecision 钉住 原生工具结果的精确回合证明独立修补受理回执并释放保留。
func TestTaskOutboxConsumesExactInlineProofWithoutASecondDecision(t *testing.T) {
	for _, shortProof := range []bool{false, true} {
		t.Run(fmt.Sprint(shortProof), func(t *testing.T) {
			store := &outboxGuardStore{InMemoryStore: memory.NewInMemoryStore(), refs: make(map[int64]int)}
			owner := outboxOwner("producer", sinkPartition, store)
			fact, raw := outboxFact(t, owner, "", true)
			if shortProof {
				storeRegistryReceipt(t, store, fact, raw)
			}
			require.NoError(t, store.StoreEvent(raw.InlineReceiptKey, inlineTaskDisposition(fact, raw)))
			require.NoError(t, owner.RecoverTaskOutbox())
			require.Empty(t, owner.persistentBus.TryPull())
			proof, err := store.GetEvent(raw.ReceiptKey)
			require.NoError(t, err)
			require.True(t, taskSignalReceiptMatches(fact, raw, *proof))
			require.Zero(t, store.held(fact.EventKey))
			require.Zero(t, store.held(raw.ReceiptKey))
			require.Empty(t, owner.taskRecords.delivery.pending)
		})
	}
}

// TestTaskOutboxEntryInlineGapRecoversOriginalResultOnce 钉住 未证明完成的入口内联结果只补原结果，不丢结果也不重复原始目标。
func TestTaskOutboxEntryInlineGapRecoversOriginalResultOnce(t *testing.T) {
	for _, shortProof := range []bool{false, true} {
		t.Run(fmt.Sprint(shortProof), func(t *testing.T) {
			store := &outboxGuardStore{InMemoryStore: memory.NewInMemoryStore(), refs: make(map[int64]int)}
			owner := outboxOwner("producer", sinkPartition, store)
			fact, raw := outboxFact(t, owner, "", true)
			if shortProof {
				storeRegistryReceipt(t, store, fact, raw)
			}
			require.NoError(t, owner.RecoverTaskOutbox())
			require.NoError(t, owner.RecoverTaskOutbox())
			batch := owner.persistentBus.TryPull()
			require.Len(t, batch, 1)
			require.Equal(t, raw.Event.ID, batch[0].ID)
			require.Equal(t, raw.Event.Message.Content, batch[0].Message.Content)
			consumeOutboxInput(t, owner, batch[0])
			require.NoError(t, owner.RecoverTaskOutbox())
			require.Empty(t, owner.persistentBus.TryPull())
			require.Zero(t, store.held(fact.EventKey))
			require.Zero(t, store.held(raw.ReceiptKey))
		})
	}
}

// TestTaskOutboxReusesCommittedUnconsumedInputIdentity 钉住 已落事实但未消费的输入恢复时沿用原事实与预留回执身份。
func TestTaskOutboxReusesCommittedUnconsumedInputIdentity(t *testing.T) {
	store := &outboxGuardStore{InMemoryStore: memory.NewInMemoryStore(), refs: make(map[int64]int)}
	old := outboxOwner("producer", sinkPartition, store)
	fact, raw := outboxFact(t, old, "", false)
	storeRegistryReceipt(t, store, fact, raw)
	prepareTaskReceiptKeys(old.contextManager, []*AgentEvent{raw.Event})
	source := old.contextManager.buildBusFact(raw.Event)
	require.NoError(t, store.StoreEvent(source.EventKey, source))
	restored := outboxOwner("producer", sinkPartition, store)
	require.NoError(t, restored.RecoverTaskOutbox())
	batch := restored.persistentBus.TryPull()
	require.Len(t, batch, 1)
	require.NotNil(t, batch[0].volatileFact)
	require.Equal(t, source.EventKey, batch[0].volatileFact.EventKey)
	require.Equal(t, source.Metadata[tagentevent.MetaKeyObservationReceiptKey], batch[0].volatileFact.Metadata[tagentevent.MetaKeyObservationReceiptKey])
	consumeOutboxInput(t, restored, batch[0])
	facts, err := queryTaskRecords(store, sinkPartition, []string{tagentevent.TypeExternalInput})
	require.NoError(t, err)
	require.Len(t, facts, 1)
	require.Zero(t, store.held(fact.EventKey))
}

// TestTaskOutboxRepairsReceiverProofAfterFormalConsumption 钉住 消费先落盘而transport回执尚未写成的窗口只补原回执。
func TestTaskOutboxRepairsReceiverProofAfterFormalConsumption(t *testing.T) {
	store := &outboxGuardStore{InMemoryStore: memory.NewInMemoryStore(), refs: make(map[int64]int)}
	old := outboxOwner("producer", sinkPartition, store)
	fact, raw := outboxFact(t, old, "", false)
	consumeOutboxInput(t, old, raw.Event)
	restored := outboxOwner("producer", sinkPartition, store)
	var delivered atomic.Int64
	restored.taskManager = task.NewTaskManager(task.TaskManagerConfig{OnSettleAccepted: func(*task.Task, task.SettleSignal) bool { delivered.Add(1); return true }})
	defer restored.taskManager.Close(context.Background())
	tk := restored.taskManager.RestoreTask(raw.TaskID, task.TaskSpec{Kind: "command", Desc: "original task", Declarative: &task.Declarative{Kind: "command", RoundID: raw.RoundID, ProducerRound: raw.RoundID}}, time.Now(), task.TaskCompleted)
	sig := raw.signal()
	sig.SourceID = raw.Event.ID
	require.True(t, restored.taskManager.RestorePendingDelivery(raw.TaskID, sig))
	require.NoError(t, restored.RecoverTaskOutbox())
	require.Empty(t, tk.PendingSignals())
	restored.taskManager.ResumePendingDeliveries()
	require.Zero(t, delivered.Load())
	require.Empty(t, restored.persistentBus.TryPull())
	proof, err := store.GetEvent(raw.ReceiptKey)
	require.NoError(t, err)
	require.NotNil(t, proof)
	require.True(t, taskSignalReceiptMatches(fact, raw, *proof))
	require.Empty(t, restored.taskRecords.delivery.pending)
	require.Zero(t, store.held(fact.EventKey))
	require.Zero(t, store.held(raw.ReceiptKey))
}

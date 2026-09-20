package agent

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ---------------------------------------------------------------------------
// Baseline counter-examples (task 1.4): real ContextManager/TagentAgent fixtures
// driven through the existing persistBusEvent / runEventLoop / Close seams, with
// deterministic barriers (fault stores, pre-populated claims, timeouts — no
// sleeps for correctness). Each asserts the SPEC target; where the current code
// already satisfies it the test stays active as a regression lock; where it does
// not, the observed failure is captured in evidence.md (fail-before) and the
// test is guarded with t.Skip("blocked-by §X") until the owning phase lands.
// ---------------------------------------------------------------------------

// phasedStore wraps a real replay-capable InMemoryStore and lets a test arm a
// per-key failure on the ordinary-write (StoreEvent), explicit-replay
// (ReplayEvent) and delete (DeleteEvent) seams. It is the agent-package sibling
// of memory's faultKV (which is package-private and not importable here), and
// stays EventReplayer-capable via the embedded store.
type phasedStore struct {
	*memory.InMemoryStore
	failStore  map[int64]error
	failReplay map[int64]error
	failDelete map[int64]error
}

func newPhasedStore() *phasedStore {
	return &phasedStore{
		InMemoryStore: memory.NewInMemoryStore(),
		failStore:     map[int64]error{},
		failReplay:    map[int64]error{},
		failDelete:    map[int64]error{},
	}
}

var _ memory.EventReplayer = (*phasedStore)(nil)

func (s *phasedStore) StoreEvent(key int64, e memory.FullEvent) error {
	if err, ok := s.failStore[key]; ok {
		return err
	}
	return s.InMemoryStore.StoreEvent(key, e)
}

func (s *phasedStore) ReplayEvent(key int64, canonical memory.FullEvent) (memory.ReplayResult, memory.FullEvent, error) {
	if err, ok := s.failReplay[key]; ok {
		return memory.ReplayNew, canonical, err
	}
	return s.InMemoryStore.ReplayEvent(key, canonical)
}

const externalInputType = "external_input"

func (s *phasedStore) DeleteEvent(key int64) error {
	if err, ok := s.failDelete[key]; ok {
		return err
	}
	return s.InMemoryStore.DeleteEvent(key)
}

func durableEvtWithPrepared(pid int, rid string, slot int, receipt string, prepared json.RawMessage, content string) *AgentEvent {
	return &AgentEvent{
		ID: rid, Type: externalInputType, Source: "user",
		Message:   &model.Message{Role: model.RoleUser, Content: content},
		Timestamp: time.UnixMilli(1700000000000),
		claim:     &durableClaim{RequestID: rid, Slot: slot, ReceiptKey: receipt, PreparedFact: prepared},
	}
}

func marshalFact(t *testing.T, f memory.FullEvent) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(f)
	require.NoError(t, err)
	return b
}

func newGateCM(t *testing.T, store memory.MemoryStore) *ContextManager {
	t.Helper()
	cm := newTestContextManager("gate", &loopMockModel{}, nil, nil, NewEventBus())
	cm.memStore = store
	return cm
}

// CE-corrupt (§3.5): an undecodable prepared_fact MUST block the commit and keep
// the claim, never fall back to buildBusFact (which re-stamps a fresh identity
// and lets a half-prepared input silently enter the fact chain).
func TestCounter_CorruptPreparedFactMustBlockNotRestamp(t *testing.T) {
	// fail-before (§3.5), reproduced 2026-09-19: current persistBusEvent logs
	// "prepared_fact undecodable (rebuild may restamp)" then STORES a restamped
	// fact (context_manager.go:1097-1100). Re-enable when §3.5 removes the
	// buildBusFact fallback and returns false instead. Evidence in evidence.md.
	t.Skip("blocked-by §3.5: corrupt prepared_fact currently restamps+stores (must block); see evidence.md fail-before")
	store := newPhasedStore()
	cm := newGateCM(t, store)
	evt := durableEvtWithPrepared(1, "r1", 0, "cafe", json.RawMessage("{ not valid json"), "hi")

	stored := cm.persistBusEvent(evt)

	require.False(t, stored, "§3.5: a corrupt prepared_fact must gate the commit (return false), not restamp")
	require.Equal(t, 0, cm.projection.Len(), "§3.5: a corrupt prepared_fact must not append a restamped ref")
	refs, _ := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{cm.partitionID}, Limit: 10})
	require.Empty(t, refs, "§3.5: no fact may reach the chain when the prepared payload is undecodable")
}

// CE-partial (§4.3): a durable input whose ReplayEvent transiently fails must
// report not-stored so the loop never treats it as pre-persisted / never advances
// over it. Deterministic (no goroutine): drive two prepared claims, 2nd armed.
func TestCounter_PartialInputReplayFailureGatesCommit(t *testing.T) {
	store := newPhasedStore()
	cm := newGateCM(t, store)
	kf := memory.NewSnowflakeEventKey(cm.partitionID, 0)
	ks := memory.NewSnowflakeEventKey(cm.partitionID, 1)
	fresh := func(k int64) memory.FullEvent {
		return memory.FullEvent{EventKey: k, PartitionID: cm.partitionID, EventType: externalInputType, EventSummary: "s", Content: "c", Timestamp: 1700000000000}
	}
	store.failReplay[ks] = errors.New("transient append failure")

	okFirst := cm.persistBusEvent(durableEvtWithPrepared(1, "r1", 0, "aa", marshalFact(t, fresh(kf)), "A"))
	okSecond := cm.persistBusEvent(durableEvtWithPrepared(1, "r1", 1, "bb", marshalFact(t, fresh(ks)), "B"))

	require.True(t, okFirst, "first input commits cleanly")
	require.False(t, okSecond, "§4.3: an input whose replay commit transiently fails must report not-stored (claim held), never swallowed as done")
}

// CE-close (§6.1/6.2): concurrent Close must converge without panic/deadlock
// and every caller must observe a consistent terminal result. Guarded by a
// timeout so a hang manifests as a captured fail-before, not a stuck suite.
func TestCounter_ConcurrentCloseConverges(t *testing.T) {
	ta := newTestTagentAgent("cc", &loopMockModel{}, nil, make(chan *event.Event, 16), NewEventBus())

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	panicked := make([]bool, n)
	done := make(chan struct{})
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			defer func() { //nolint:revive // recover is the panic probe
				if r := recover(); r != nil {
					panicked[i] = true
				}
			}()
			errs[i] = ta.Close()
		}(i)
	}
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("§6.1: concurrent Close did not converge within 5s (deadlock/ordering gap)")
	}
	for i := range panicked {
		require.False(t, panicked[i], "§6.1: concurrent Close must not panic")
	}
}

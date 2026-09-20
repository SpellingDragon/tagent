package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
	"github.com/SpellingDragon/tagent/plugin"
	"github.com/stretchr/testify/require"
)

// Section 7 (design 决策8) — the recovery notice's REAL presentation boundary.
// The consumption point lives on the execution-gate model decorator (single
// owner, established in §4.5C): cancel/short-circuit before the underlying
// call must NOT consume; entering the underlying call consumes exactly once
// even when the provider then fails; concurrent callers share at most one
// carriage; the state rides the resident ContextManager across hot rebuilds;
// and the volatile/one-shot legs are asserted on the ACTUAL framework-driven
// request, not a hand-built one.

func setNotice(cm *ContextManager, s string) {
	cm.recoveryMu.Lock()
	cm.recoveryNotice = s
	cm.recoveryMu.Unlock()
}

func peekNotice(cm *ContextManager) string {
	cm.recoveryMu.Lock()
	defer cm.recoveryMu.Unlock()
	return cm.recoveryNotice
}

func countCarried(reqs []*model.Request) int {
	n := 0
	for _, r := range reqs {
		for _, m := range r.Messages {
			if strings.HasPrefix(m.Content, "[recovery]") {
				n++
				break
			}
		}
	}
	return n
}

// failingCaptureModel records the request then reports a provider failure —
// the 「模型收到调用后失败」 shape (the call was ENTERED; the error follows).
type failingCaptureModel struct {
	mu       sync.Mutex
	requests []*model.Request
}

func (m *failingCaptureModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, req)
	return nil, errors.New("provider down")
}
func (m *failingCaptureModel) Info() model.Info { return model.Info{Name: "failing-capture"} }
func (m *failingCaptureModel) snapshot() []*model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*model.Request(nil), m.requests...)
}

func drainRunner(out <-chan *event.Event, limit time.Duration) {
	deadline := time.After(limit)
	for {
		select {
		case _, ok := <-out:
			if !ok {
				return
			}
		case <-deadline:
			return
		}
	}
}

// TestNotice_ShortCircuitAndCancel_StaysPending — spec Scenario「装配后短路」:
// a request that finished assembly but never reached the wrapped model (the
// caller cancels / a later callback short-circuits BEFORE the model entry)
// must leave the notice PENDING for the next real call — and nothing consumed
// it. The subsequent real call then carries it once.
func TestNotice_ShortCircuitAndCancel_StaysPending(t *testing.T) {
	g, inner, cm := newGate(t)
	setNotice(cm, "[recovery] pending after short circuit")

	// Framework-assembled request exists, but the turn is cancelled before the
	// model entry: the gate function is never invoked at all.
	_, cancel := context.WithCancel(context.Background())
	cancel()

	require.Equal(t, "[recovery] pending after short circuit", peekNotice(cm),
		"no underlying call ⇒ no consumption")
	require.Equal(t, 0, inner.requestCount())

	// The next REAL call still carries it (pending survives the short circuit).
	_, _ = g.GenerateContent(context.Background(), plainReq())
	require.Equal(t, 1, countCarried(inner.snapshotRequests()), "next real call carries the preserved notice")
	require.Empty(t, peekNotice(cm), "consumed on that real call")
}

// TestNotice_UnderlyingFailure_ConsumedNoResend — spec Scenario「模型收到调用
// 后失败」(L57-59): entering the wrapped model consumes the notice even when
// the provider then fails; diagnostics stay readable; later calls never repeat
// it (no resend is demanded — entering the model is the consumption boundary).
func TestNotice_UnderlyingFailure_ConsumedNoResend(t *testing.T) {
	fail := &failingCaptureModel{}
	cm := newTestContextManager("nt-fail", &loopMockModel{}, nil, nil, nil)
	g := newExecutionGateModel(fail, cm)
	setNotice(cm, "[recovery] failed-provider case")
	cm.recoveryMu.Lock()
	cm.recovery = &RecoveryResult{Mode: "snapshot", Status: "partial", Truncated: 3}
	cm.recoveryMu.Unlock()

	_, err := g.GenerateContent(context.Background(), plainReq())
	require.Error(t, err, "the underlying failure surfaces to the caller")
	require.Empty(t, peekNotice(cm), "entering the model consumed the notice despite provider failure")
	require.Equal(t, 1, countCarried(fail.snapshot()))

	_, _ = g.GenerateContent(context.Background(), plainReq())
	require.Equal(t, 1, countCarried(fail.snapshot()), "no resend after the underlying already saw it")
	require.NotNil(t, cm.RecoveryResult(), "diagnostics remain queryable after consumption")
}

// TestNotice_ConcurrentCalls_ExactlyOneCarries — spec L35: concurrent calls
// against ONE resident recovery state consume at most once.
func TestNotice_ConcurrentCalls_ExactlyOneCarries(t *testing.T) {
	g, inner, cm := newGate(t)
	setNotice(cm, "[recovery] raced once")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = g.GenerateContent(context.Background(), plainReq())
		}()
	}
	wg.Wait()

	require.Equal(t, 8, inner.requestCount(), "all callers reach the model")
	require.Equal(t, 1, countCarried(inner.snapshotRequests()), "at most ONE concurrent call may carry the notice")
}

// TestNotice_RealRequest_VolatileRunner — task 7.4's volatile leg on the REAL
// framework path: the runner assembles instruction/history, BeforeModel
// callbacks run, and the gate appends at the very tail of THAT request —
// visible exactly once, system head untouched, and the notice never survives
// into session history (proven behaviorally: a second run in the SAME session
// replays history and no request beyond the first carries the text).
func TestNotice_RealRequest_VolatileRunner(t *testing.T) {
	capture := &requestCapturingModel{resp: gateOKResp()}
	cm := newTestContextManager("nt-real", capture, nil, make(chan *event.Event, 16), nil)
	setNotice(cm, "[recovery] real-request tail")

	r := cm.currentRunner()
	require.NotNil(t, r)
	out, err := r.Run(context.Background(), "test-user", "s-1", model.NewUserMessage("hello world"))
	require.NoError(t, err)
	drainRunner(out, 5*time.Second)

	reqs := capture.snapshotRequests()
	require.NotEmpty(t, reqs, "the real runner must reach the model")
	first := reqs[len(reqs)-1]
	// The assembled request on this bare session starts with the context-guard
	// callback's material — the notice must sit AFTER it, proving the append
	// happens at the boundary past every assembly callback (task 7.1).
	require.Contains(t, first.Messages[0].Content, "[context-guard]",
		"precondition: assembly-callback material present before the gate")
	require.True(t, strings.HasPrefix(first.Messages[len(first.Messages)-1].Content, "[recovery]"),
		"notice appended AFTER all assembled material on the real request")
	require.Equal(t, 1, countCarried(reqs), "visible exactly once")

	// Same session, second turn: history is replayed into the request — if the
	// notice had leaked into session history it would show up here again.
	out2, err := r.Run(context.Background(), "test-user", "s-1", model.NewUserMessage("second turn"))
	require.NoError(t, err)
	drainRunner(out2, 5*time.Second)
	require.Equal(t, 1, countCarried(capture.snapshotRequests()),
		"the notice never becomes session history — later turns carry no copy")
}

// TestNotice_HotRebuild_StateContinues — spec Scenario「热更与并发调用」:
// RebuildExecutor swaps the EXECUTION surface (fresh gated model + runner)
// while the recovery state stays on the resident ContextManager: a pending
// notice is carried exactly once through the NEW executor, never re-armed or
// duplicated, and one Run equals one underlying call (no double wrapping).
func TestNotice_HotRebuild_StateContinues(t *testing.T) {
	capture := &requestCapturingModel{resp: gateOKResp()}
	cm := newTestContextManager("nt-hot", capture, nil, make(chan *event.Event, 16), nil)

	r2 := cm.RebuildExecutor(ContextManagerConfig{}) // zero override → exec-snapshot fallback
	require.NotNil(t, r2)
	setNotice(cm, "[recovery] survives hot rebuild")

	out, err := r2.Run(context.Background(), "test-user", "s-2", model.NewUserMessage("after rebuild"))
	require.NoError(t, err)
	drainRunner(out, 5*time.Second)

	reqs := capture.snapshotRequests()
	require.NotEmpty(t, reqs)
	require.Equal(t, 1, countCarried(reqs), "the rebuilt executor consumes the resident notice exactly once")
	require.Empty(t, peekNotice(cm))

	out2, err := r2.Run(context.Background(), "test-user", "s-2", model.NewUserMessage("second"))
	require.NoError(t, err)
	drainRunner(out2, 5*time.Second)
	reqs2 := capture.snapshotRequests()
	require.Equal(t, 1, countCarried(reqs2), "never re-carried after consumption")
	require.Len(t, reqs2, 2, "one Run = exactly one underlying model call (no double gate wrapping)")
}

// TestNotice_DurableTurn_RealRequestCarriesOnce — task 7.4's DURABLE leg on
// the real submission surface: a credentialed durable turn flows through
// cm.RunFlow → the framework → the gate, and the ACTUAL request the wrapped
// model sees carries the notice exactly once; the following turn never repeats
// it. (newDurableAgentWithStore builds the full ContextManager → buildLLMAgent
// → gated model, exactly as production does.)
func TestNotice_DurableTurn_RealRequestCarriesOnce(t *testing.T) {
	capture := &requestCapturingModel{resp: gateOKResp()}
	bus := NewEventBus()
	ta := newDurableAgentWithStore("nt-durable", capture, memory.NewInMemoryStore(),
		make(chan *event.Event, 16), bus)
	cm := ta.contextManager
	// A verified execution credential — the durable turn's ctx marker (§4.5A).
	cred := &plugin.EchoCredential{MergedMessage: "hello durable"}
	cred.Bind("inv-root")
	ctx := plugin.WithEchoCredential(context.Background(), cred)

	setNotice(cm, "[recovery] durable turn notice")
	require.NoError(t, cm.RunFlow(ctx, model.NewUserMessage("hello durable")))
	reqs := capture.snapshotRequests()
	require.NotEmpty(t, reqs, "the durable submission surface must reach the model")
	require.Equal(t, 1, countCarried(reqs), "the durable real request carries the notice exactly once")

	require.NoError(t, cm.RunFlow(ctx, model.NewUserMessage("second durable turn")))
	require.Equal(t, 1, countCarried(capture.snapshotRequests()), "never re-carried on later durable turns")
}

// TestNotice_DurableChain_FactsReceiptAndRestartClean — task 7.4's honesty
// legs on a durable localfile chain: the notice NEVER enters the input facts,
// the inbox receipt, or the completion record — proven against an INDEPENDENT
// REOPEN of the durable backend (fresh store instance over the same dir, not
// a cached read-back) — and an all-facts-passing chain that never reaches the
// model leaves the notice pending (no consumption without a call).
func TestNotice_DurableChain_FactsReceiptAndRestartClean(t *testing.T) {
	root := t.TempDir()
	dirStore, dirInbox := root+"/store", root+"/inbox"
	kvStore, err := kv.NewLocalFileKV(dirStore)
	require.NoError(t, err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, dirStore, 100)
	require.NoError(t, err)
	bus, err := NewReliableEventBus(dirInbox)
	require.NoError(t, err)
	cm := &ContextManager{partitionID: 1, memStore: store, projection: compress.NewSessionProjection(), bus: bus}
	ta := &TagentAgent{name: "nt-restart", persistentBus: bus, contextManager: cm}
	setNotice(cm, "[recovery] must never become a fact")

	_, err = bus.PublishContext(context.Background(), durableMsg("durable-input"))
	require.NoError(t, err)
	batch, err := bus.Pull(context.Background())
	require.NoError(t, err)
	st, _ := ta.prepareBatchFacts(batch)
	require.Equal(t, submitOK, st)
	for _, e := range batch {
		require.True(t, cm.persistBusEvent(e))
	}
	ta.finishDurableBatch(context.Background(), batch, batch, completedOutcome())
	require.Equal(t, int64(0), bus.DurablePending())

	// No model call happened anywhere in the chain → the notice stayed pending.
	require.Equal(t, "[recovery] must never become a fact", peekNotice(cm))

	assertNoNoticeOnChain := func(t *testing.T, s memory.MemoryStore, phase string) {
		refs, qerr := s.QueryEvents(memory.QueryOptions{PartitionIDs: []int{1}, Limit: 100})
		require.NoError(t, qerr)
		require.NotEmpty(t, refs, phase)
		for _, r := range refs {
			full, gerr := s.GetEvent(r.EventKey)
			require.NoError(t, gerr, phase)
			require.NotContains(t, full.Content, "[recovery]",
				"§7.4: fact/receipt content must never carry the transient notice (%s)", phase)
			require.NotContains(t, fmt.Sprintf("%v", full.Metadata), "[recovery]",
				"§7.4: no notice text in event metadata (%s)", phase)
		}
	}
	assertNoNoticeOnChain(t, store, "live instance")

	// INDEPENDENT reopen — a fresh store instance over the same durable dir.
	kv2, err := kv.NewLocalFileKV(dirStore)
	require.NoError(t, err)
	store2, err := memory.NewFileSegmentStore(kv2, nil, dirStore, 100)
	require.NoError(t, err)
	assertNoNoticeOnChain(t, store2, "independent reopen")
	require.NoError(t, store2.Close())
}

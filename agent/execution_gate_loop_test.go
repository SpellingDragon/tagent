package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/plugin"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

// credFaultStore fails StoreEvent for assistant `agent_output` events only. The durable
// input still commits via the inherited ReplayEvent, and finishDurableBatch's inbox-receipt
// StoreEvent still succeeds — so a credentialed turn's assistant store fails (→ MarkRejected)
// while the ack path remains functional. This makes the §4.5 loop guard the SOLE reason the
// envelope stays un-acked (a real fail-before/pass-after discriminator), unlike an
// always-failing store that would also break the receipt write.
type credFaultStore struct{ *memory.InMemoryStore }

func (s *credFaultStore) StoreEvent(k int64, ev memory.FullEvent) error {
	if ev.EventType == tagentevent.TypeAgentOutput {
		return errors.New("disk full")
	}
	return s.InMemoryStore.StoreEvent(k, ev)
}

// newDurableAgentWithStore mirrors newTestContextManager but injects a caller-supplied
// store into BOTH the ContextManager and its MemoryPlugin (one cm, as in prod), so a test
// can drive the real event loop against a faulting store while the durable input still
// commits via ReplayEvent.
func newDurableAgentWithStore(name string, m model.Model, store memory.MemoryStore, outputCh chan *event.Event, bus *EventBus) *TagentAgent {
	compressor := compress.NewSmartCompressor(compress.WithMaxTokens(8000), compress.WithTokenCounter(&mockTokenCounter{tokens: 100}))
	memPlugin := plugin.NewMemoryPlugin(store)
	cm := NewContextManager(ContextManagerConfig{
		Name:         name,
		UserID:       "test-user",
		SessionID:    "test-session",
		Model:        m,
		MaxToolIters: 10,
		Compressor:   compressor,
		TokenCounter: &mockTokenCounter{tokens: 100},
		MaxTokens:    8000,
		ThresholdPct: 0.8,
		MemStore:     store,
		MemPlugin:    memPlugin,
		SessionSvc:   sessioninmemory.NewSessionService(),
		OutputCh:     outputCh,
		Bus:          bus,
		Projection:   compress.NewSessionProjection(),
		OnEvent:      func(evt *event.Event) {},
	})
	return &TagentAgent{name: name, persistentBus: bus, activeBus: bus, contextManager: cm, outputCh: outputCh}
}

// §4.5 (loop commit gate): a durable turn whose input facts committed (turnEcho installed)
// but whose execution credential ends UNVERIFIED — here because a plugin StoreEvent failure
// was SWALLOWED by the framework (failStore fails StoreEvent; the durable input still commits
// via the inherited ReplayEvent, so the batch reaches submitOK and the model) — MUST fail
// closed at the loop: the envelope is NOT acked (claims stay for replay) rather than crossing
// the commit gate on input the plugin could not durably record.
//
// Fail-before: deleting the loop's `if installed, verified := cm.turnEchoVerified(); installed
// && !verified { ... return }` guard (§4.5) makes finishDurableBatch ack the turn → DurablePending
// drops to 0 → this test goes red. It locks that "the framework logging a plugin error and
// continuing" can no longer let a swallowed store failure cross the durable commit gate.
func TestRunEventLoop_UnverifiedCredentialDoesNotAck(t *testing.T) {
	captureModel := &requestCapturingModel{
		resp: &model.Response{ID: "ok", Done: true,
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "reply"}}}},
	}
	bus, err := NewReliableEventBus(t.TempDir())
	require.NoError(t, err)
	outputCh := make(chan *event.Event, 20)
	// StoreEvent fails only for the assistant `agent_output` (input commits via ReplayEvent,
	// the inbox-receipt ack still works) → the credentialed turn hits a framework-swallowed
	// store error and MarkRejected, but the ack path itself is functional.
	store := &credFaultStore{InMemoryStore: memory.NewInMemoryStore()}
	ta := newDurableAgentWithStore("cred-fault", captureModel, store, outputCh, bus)

	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "hello-durable"}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go ta.runEventLoop(ctx, bus, ta.contextManager)

	// The batch commits (ReplayEvent) → submitOK → turnEcho installed → model reached once
	// (the root echo binds the credential first, so the entry gate passes this call).
	deadline := time.After(4 * time.Second)
	for captureModel.requestCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("durable commit + gate never reached the model (batch not committed via ReplayEvent?)")
		case <-time.After(10 * time.Millisecond):
		}
	}

	// The swallowed assistant-store error downgrades the credential; the loop then refuses to
	// ack. Watch a bounded window for the envelope to (wrongly) drop to acked (0). If the
	// §4.5 guard were removed, finishDurableBatch would ack it here and we'd observe 0.
	watch := time.After(2 * time.Second)
	timedOut := false
	for !timedOut && bus.DurablePending() != 0 {
		select {
		case <-watch:
			timedOut = true
		case <-time.After(10 * time.Millisecond):
		}
	}
	require.NotZero(t, bus.DurablePending(), "§4.5: a framework-swallowed plugin store error must keep the credential unverified and the envelope NOT acked (fail-closed)")
	require.EqualValues(t, 1, bus.DurablePending(), "the un-acked claim must stay outstanding for replay")
}

// §4.8 (transport retry does not expand the batch / design 决策4「同业务 turn 重试创建新尝试
// token，重用同批事实」): the credential template (cm.turnEcho) is frozen when the batch is
// pulled, so every runner attempt of the same business turn mints a FRESH unique token but
// reuses the EXACT frozen merged message + committed fact keys — it can never re-pull or widen
// the batch. Locked together with the loop structure (received/msg frozen before the attempt
// loop) and TestRunEventLoop_ABOneTurnCNextTurn (C → next turn).
func TestNewAttemptEchoCredential_RetryReusesFrozenBatchNoExpansion(t *testing.T) {
	cm := &ContextManager{name: "resident", sessionID: "s1"}
	frozen := []int64{111, 222, 333}
	cm.turnEcho = &echoSpec{agent: "resident", session: "s1", mergedMessage: "A\n\n---\n\nB\n\n---\n\nC", committedKeys: frozen}

	cred1 := cm.newAttemptEchoCredential()
	cred2 := cm.newAttemptEchoCredential()

	require.NotEqual(t, cred1.AttemptToken, cred2.AttemptToken, "each attempt gets a fresh unique token")
	require.Equal(t, cred1.MergedMessage, cred2.MergedMessage, "retry reuses the frozen batch's merged input verbatim (never expanded)")
	require.Equal(t, cred1.CommittedKeys, cred2.CommittedKeys, "retry reuses the exact committed fact keys (no new inputs pulled)")
	require.Equal(t, frozen, cred1.CommittedKeys)
	require.Equal(t, "resident", cred1.Agent)
	require.Equal(t, "s1", cred1.Session)
}

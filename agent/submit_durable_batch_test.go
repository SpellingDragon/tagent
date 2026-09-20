package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// §4.2 durable submit gate: a CLASSIFIED outcome replaces the old bool so the loop
// never treats one input as the batch. These lock the three properties the delta spec
// (persistent-event-loop L88/L90) requires and that were real gaps on the pre-§4.2
// code (see evidence §3.6-①): batch all-or-nothing, deterministic-conflict isolation,
// and transient-vs-conflict classification with ordered requeue.

func newSubmitGateAgent(t *testing.T, dir string) (*TagentAgent, *EventBus, *ContextManager) {
	t.Helper()
	bus, err := NewReliableEventBus(dir)
	require.NoError(t, err)
	cm := &ContextManager{
		partitionID: 1,
		memStore:    memory.NewInMemoryStore(),
		projection:  compress.NewSessionProjection(),
	}
	ta := &TagentAgent{name: "submit-gate", persistentBus: bus, contextManager: cm}
	return ta, bus, cm
}

// §3.6-① batch all-or-nothing: when envelope B hits a deterministic prepare conflict,
// the good input A must NOT be committed even though A prepared cleanly. Before §4.2
// the loop persisted every claim whose prepared_fact got stamped, so A leaked into the
// fact chain — a partially-committed batch.
func TestSubmitDurableBatch_AllOrNothingOnConflict(t *testing.T) {
	ta, bus, cm := newSubmitGateAgent(t, t.TempDir())
	ctx := context.Background()
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "A"}))
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "B"}))
	batch, err := bus.Pull(ctx)
	require.NoError(t, err)
	require.Len(t, batch, 2)

	// Isolate B deterministically: freeze its on-disk envelope with a DIFFERENT
	// receipt_key than the fresh one submitDurableBatch will build → ErrReceiptKeyConflict.
	var pathB string
	for _, ev := range batch {
		if ev.Message != nil && ev.Message.Content == "B" {
			pathB = ev.claim.Path
		}
	}
	require.NotEmpty(t, pathB)
	require.NoError(t, bus.inbox.PrepareFacts(pathB, "conflicting-receipt-key",
		[]json.RawMessage{json.RawMessage(`{"event_key":1,"content":"B"}`)}))

	outcome := ta.submitDurableBatch(ctx, batch, batch)
	require.Equal(t, submitConflict, outcome.status, "a deterministic prepare conflict must be classified as such")
	require.Equal(t, pathB, outcome.conflict)
	// Neither A nor B committed — the whole batch is gated.
	require.Empty(t, cm.projection.GetAll(), "§3.6-①: a conflicting batch must NOT commit the good input A")
}

// A transient I/O failure during prepare (an unreadable envelope, not a sentinel
// conflict) is classified submitTransient — distinct from submitConflict — so the loop
// backs off and re-attempts rather than isolating a good input. Nothing is committed.
func TestSubmitDurableBatch_TransientNotConflict(t *testing.T) {
	ta, _, cm := newSubmitGateAgent(t, t.TempDir())
	ctx := context.Background()
	ev := &AgentEvent{
		ID: "x", Type: "external_input", Source: "user", Timestamp: time.Now(),
		Message:  &model.Message{Role: model.RoleUser, Content: "Z"},
		Metadata: map[string]any{},
		claim:    &durableClaim{Path: "/no/such/envelope.json", RequestID: "Z", Slot: 0},
	}
	outcome := ta.submitDurableBatch(ctx, []*AgentEvent{ev}, []*AgentEvent{ev})
	require.Equal(t, submitTransient, outcome.status, "an I/O prepare failure is transient, not a conflict")
	require.Empty(t, outcome.conflict, "a transient failure isolates nothing")
	require.Empty(t, cm.projection.GetAll(), "a transient failure commits no fact")
}

// releaseBatchClaims returns held claims to pending so the next Pull re-claims them in
// strict order — the transient backpressure primitive that must NEVER drop or ack an
// uncommitted input (§4.2「保持顺序和有界背压」).
func TestReleaseBatchClaims_RequeuesForOrderedReclaim(t *testing.T) {
	ta, bus, _ := newSubmitGateAgent(t, t.TempDir())
	ctx := context.Background()
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "A"}))
	batch, err := bus.Pull(ctx)
	require.NoError(t, err)
	require.Len(t, batch, 1)

	ta.releaseBatchClaims(batch)

	again, err := bus.Pull(ctx)
	require.NoError(t, err)
	require.Len(t, again, 1, "a released claim must be re-claimable, never dropped")
	require.Equal(t, "A", again[0].Message.Content)
}

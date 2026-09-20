package agent

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// §4.1 (design 决策4 L106): the original consumed set is frozen; meditation
// yielding is a model-input disposition, not a mutation of the batch. A yielding
// meditation's durable envelope MUST still be receipted+acked over the received
// set — never left claimed (zombied) just because it was filtered out of the
// selected inputs. Before §4.1 finishDurableBatch ran over the filtered set, so
// the dropped-but-claimed meditation envelope stayed pending forever.
func TestRunEventLoop_YieldingMeditationEnvelopeConsumedNotZombied(t *testing.T) {
	captureModel := &requestCapturingModel{
		resp: &model.Response{ID: "ok", Done: true,
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "reply"}}}},
	}
	bus, err := NewReliableEventBus(t.TempDir())
	require.NoError(t, err)
	outputCh := make(chan *event.Event, 20)
	ta := newTestTagentAgent("med-zombie", captureModel, nil, outputCh, bus)
	// One contextManager (as in prod): persistBusEvent commits + feeds the projection
	// on ta.contextManager, and RunFlow/models run on that same cm — so the selected
	// user input reaches the model and its fact lands in the store §4.4 is asserted on.
	cm := ta.contextManager

	// Both envelopes pending BEFORE the loop starts → claimed together in one Pull
	// → a mixed batch → the meditation yields out of `selected` but stays in `received`.
	bus.Publish(NewExternalInputEvent("meditation", model.Message{Role: model.RoleUser, Content: "meditate-quietly"}))
	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "real-user-input"}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go ta.runEventLoop(ctx, bus, cm)

	// The model runs for the selected (user) input.
	deadline := time.After(3 * time.Second)
	for captureModel.requestCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for model call")
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Both consumed envelopes settle: pending drains to 0 (no zombied meditation).
	settled := time.After(3 * time.Second)
	for bus.DurablePending() != 0 {
		select {
		case <-settled:
			t.Fatalf("§4.1 zombie: %d durable envelope(s) left un-consumed after the turn (yielding meditation not receipted)", bus.DurablePending())
		case <-time.After(10 * time.Millisecond):
		}
	}

	// The yielding meditation's input is NOT stored as a fact (skipped, no fact);
	// the selected user input IS stored.
	refs, err := cm.memStore.QueryEvents(memory.QueryOptions{PartitionIDs: []int{cm.partitionID}, Limit: 100})
	require.NoError(t, err)
	stored := ""
	for _, ref := range refs {
		if evt, gerr := cm.memStore.GetEvent(ref.EventKey); gerr == nil {
			stored += "\n" + evt.Content
		}
	}
	require.Contains(t, stored, "real-user-input", "selected input must be persisted")
	require.NotContains(t, stored, "meditate-quietly", "a yielding meditation is not written as an input fact")
}

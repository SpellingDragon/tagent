// TestJourneySyncReplyDelivers is J1, the synchronous journey: one user message
// enters through Host.Invoke, the model answers, and the recorded sender receives
// exactly that body at the user address.
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
package journey

import (
	"context"
	"testing"
	"time"
)

// TestJourneySyncReplyDelivers is J1, the synchronous journey: one message enters through Host.Invoke and the sender receives the answer.
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestJourneySyncReplyDelivers(t *testing.T) {
	const answer = "the synchronous journey replied"
	provider := newJourneyProvider(t)
	provider.scriptAnswers(journeyEntryModel, answer)
	host := bootJourney(t, provider, journeySpec{})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	turn := host.invokeOK(t, ctx, "session-journey-sync", "hello")
	if got := len(turn.Decisions); got != 1 {
		t.Fatalf("decisions = %d, want the one decision of the scripted answer", got)
	}
	deliveries := host.sender.deliveries()
	if len(deliveries) != 1 {
		t.Fatalf("deliveries = %+v, want exactly the one reply", deliveries)
	}
	if deliveries[0].Content != answer {
		t.Fatalf("delivered body = %q, want %q", deliveries[0].Content, answer)
	}
	if deliveries[0].Target.AgentID != journeyEntryAgent || deliveries[0].Target.SessionID != "session-journey-sync" {
		t.Fatalf("delivery target = %+v, want the user address the turn was invoked at", deliveries[0].Target)
	}
}

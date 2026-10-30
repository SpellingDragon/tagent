package event_test

import (
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// TestFinalRoundRequestIsAFact pins the judgment family's identity: which range and why.
// - a request answers its content-addressed identity as its fact key and the frozen final_round.request type
// - the identity is derived from the range and the reason, so one range answers one request
// - the measured inputs travel beside the identity without changing it
//
// 契约: docs/FACT-VOCABULARY.md#families
func TestFinalRoundRequestIsAFact(t *testing.T) {
	t.Run("a request answers its identity and its frozen type", func(t *testing.T) {
		request := event.FinalRoundRequestFor("session-a#7", "rounds", 8, 90*time.Second)
		var fact event.Fact = request
		if got := fact.FactType(); got != "final_round.request" {
			t.Fatalf("FactType() = %q, want the frozen final_round.request type", got)
		}
		if got, want := fact.FactKey(), string(request.ID); got != want {
			t.Fatalf("FactKey() = %q, want the request identity %q", got, want)
		}
		if request.Range != "session-a#7" || request.Reason != "rounds" || request.Rounds != 8 || request.Elapsed != 90*time.Second {
			t.Fatalf("request = %+v, want the range, the reason and the measured inputs", request)
		}
	})

	t.Run("one range answers one request however often it is judged", func(t *testing.T) {
		base := event.FinalRoundRequestFor("session-a#7", "budget", 3, time.Minute)
		if repeat := event.FinalRoundRequestFor("session-a#7", "budget", 3, time.Minute); repeat.ID != base.ID {
			t.Fatalf("identities of one judgment = (%q, %q), want one key", base.ID, repeat.ID)
		}
		later := event.FinalRoundRequestFor("session-a#7", "budget", 4, 2*time.Minute)
		if later.ID != base.ID {
			t.Fatalf("re-judged inputs answered %q, want the identity %q of the same range and reason", later.ID, base.ID)
		}
		for name, other := range map[string]event.FinalRoundRequest{
			"another range":  event.FinalRoundRequestFor("session-a#8", "budget", 3, time.Minute),
			"another reason": event.FinalRoundRequestFor("session-a#7", "rounds", 3, time.Minute),
		} {
			if other.ID == base.ID {
				t.Fatalf("%s answers the same identity as the base request, want a distinct key", name)
			}
		}
	})
}

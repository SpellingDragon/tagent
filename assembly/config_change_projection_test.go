// The projection boundary of a configuration change fact: control-plane material
// maps onto no mapped entry, so a model request never reads it as transcript.
package assembly

import (
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
)

// TestConfigChangeMapsNoProjectionEntry pins the explicit type skip of a configuration change at the projection boundary.
//
// 契约: docs/wiki/agent/compression-and-telemetry.md#projection-fold
func TestConfigChangeMapsNoProjectionEntry(t *testing.T) {
	at := time.Date(2026, time.November, 3, 12, 0, 0, 0, time.UTC)
	change := event.ConfigChangeFor("user", event.ConfigChangeDimensionPrompt, 3, "agent user prompt source changed: [boot.md] -> [next.md]")
	committed := change.Event(at)
	if committed.Message.Content == "" {
		t.Fatal("the committed config change carries no message body, so the empty-payload skip would explain the mapping alone")
	}
	if entry, mapped := mappedEntryOf(committed); mapped {
		t.Fatalf("config change mapped as %+v, want no projection entry: the fact is control-plane material", entry)
	}
	stamped := committed
	stamped.Owner = event.Address{AgentID: "user", SessionID: "session-stamped"}
	if entry, mapped := mappedEntryOf(stamped); mapped {
		t.Fatalf("session-stamped config change mapped as %+v, want no projection entry: the skip is by type", entry)
	}

	ordinary := event.Event{
		Key:       "ordinary-output",
		Type:      event.TypeAgentOutput,
		Timestamp: at,
		Message:   model.Message{Role: model.RoleAssistant, Content: "the answer a request reads"},
	}
	if _, mapped := mappedEntryOf(ordinary); !mapped {
		t.Fatal("an ordinary transcript fact mapped onto no entry, want the mapping intact beside the type skip")
	}
}

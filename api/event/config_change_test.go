package event_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// TestConfigChangeIsAFact pins the configuration change family's identity and the event it commits as.
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestConfigChangeIsAFact(t *testing.T) {
	t.Run("a change answers its identity and its frozen type", func(t *testing.T) {
		change := event.ConfigChangeFor("user", event.ConfigChangeDimensionPrompt, 7, "agent user prompt source changed: [boot.md] -> [next.md]")
		var fact event.Fact = change
		if got := fact.FactType(); got != "config_change" {
			t.Fatalf("FactType() = %q, want the frozen config_change type", got)
		}
		if got, want := fact.FactKey(), string(change.ID); got != want {
			t.Fatalf("FactKey() = %q, want the change identity %q", got, want)
		}
		if change.ID == "" {
			t.Fatal("the judgment carries no identity, want a content-addressed key")
		}
		if change.AgentID != "user" || change.Dimension != event.ConfigChangeDimensionPrompt || change.Generation != 7 {
			t.Fatalf("change = %+v, want the agent, the prompt dimension and the generation", change)
		}
	})

	t.Run("the identity follows the judgment it names", func(t *testing.T) {
		base := event.ConfigChangeFor("user", event.ConfigChangeDimensionPrompt, 7, "the prompt moved")
		if repeat := event.ConfigChangeFor("user", event.ConfigChangeDimensionPrompt, 7, "the prompt moved"); repeat.ID != base.ID {
			t.Fatalf("identities of one judgment = (%q, %q), want one key", base.ID, repeat.ID)
		}
		for name, other := range map[string]event.ConfigChange{
			"another agent":      event.ConfigChangeFor("other", event.ConfigChangeDimensionPrompt, 7, "the prompt moved"),
			"another dimension":  event.ConfigChangeFor("user", event.ConfigChangeDimensionBinding, 7, "the prompt moved"),
			"another generation": event.ConfigChangeFor("user", event.ConfigChangeDimensionPrompt, 8, "the prompt moved"),
			"another change":     event.ConfigChangeFor("user", event.ConfigChangeDimensionPrompt, 7, "another change"),
		} {
			if other.ID == base.ID {
				t.Fatalf("%s answers the identity of the base judgment %q, want its own key", name, base.ID)
			}
		}
	})

	t.Run("the event stamps the source, the dimension and the filing moment", func(t *testing.T) {
		change := event.ConfigChangeFor("user", event.ConfigChangeDimensionBinding, 9, "agent user binding changed: sampling")
		at := time.Date(2026, time.November, 3, 12, 0, 0, 0, time.UTC)
		committed := change.Event(at)
		if committed.Key != change.ID || committed.Type != "config_change" {
			t.Fatalf("committed event = %+v, want the change identity and the frozen type", committed)
		}
		if !committed.Timestamp.Equal(at) {
			t.Fatalf("event timestamp = %v, want the filing moment %v", committed.Timestamp, at)
		}
		if got := committed.Owner.AgentID; got != "user" {
			t.Fatalf("event owner = %q, want the agent whose dimension moved", got)
		}
		if got := committed.Meta[event.MetaKeyTriggerSource]; got != event.ConfigChangeSourceConfig {
			t.Fatalf("trigger source = %q, want the configuration source constant", got)
		}
		if got := committed.Meta[event.MetaKeyConfigChangeDimension]; got != event.ConfigChangeDimensionBinding {
			t.Fatalf("dimension = %q, want the binding dimension", got)
		}
		if got := committed.Meta[event.MetaKeyConfigChangeGeneration]; got != "9" {
			t.Fatalf("generation = %q, want the publication generation 9", got)
		}
		if got := committed.Message.Content; got != change.Description {
			t.Fatalf("event body = %q, want the change description %q", got, change.Description)
		}
	})
}

// TestConfigChangeKeepsItsFiveFieldShape pins the five exported fields and the unexported model payload of the fact.
//
// 契约: docs/FACT-VOCABULARY.md#fact-category
func TestConfigChangeKeepsItsFiveFieldShape(t *testing.T) {
	typ := reflect.TypeOf(event.ConfigChange{})
	exported := make([]string, 0, 5)
	unexported := make([]string, 0, 1)
	for at := 0; at < typ.NumField(); at++ {
		field := typ.Field(at)
		if field.PkgPath == "" {
			exported = append(exported, field.Name)
			continue
		}
		unexported = append(unexported, field.Name)
	}
	want := []string{"ID", "AgentID", "Dimension", "Generation", "Description"}
	if !reflect.DeepEqual(exported, want) {
		t.Fatalf("exported fields = %v, want the five fields that state every change %v", exported, want)
	}
	if !reflect.DeepEqual(unexported, []string{"model"}) {
		t.Fatalf("unexported fields = %v, want the one model payload beside the five exported fields", unexported)
	}
	if typ.NumField() != 6 {
		t.Fatalf("field count = %d, want the five exported fields beside the model payload", typ.NumField())
	}
}

// TestConfigChangeModelCarriesTheMove pins the model dimension payload and the model replacement metadata.
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestConfigChangeModelCarriesTheMove(t *testing.T) {
	base := event.ConfigChangeModelFor("user", 4, "model-a", "model-b", event.ModelReplacementResultRequested)
	if base.Dimension != event.ConfigChangeDimensionModel || base.Generation != 4 {
		t.Fatalf("model change = %+v, want the model dimension and the generation", base)
	}
	if repeat := event.ConfigChangeModelFor("user", 4, "model-a", "model-b", event.ModelReplacementResultRequested); repeat.ID != base.ID {
		t.Fatalf("identities of one model move = (%q, %q), want one key", base.ID, repeat.ID)
	}
	for name, other := range map[string]event.ConfigChange{
		"another from version": event.ConfigChangeModelFor("user", 4, "model-a@v1", "model-b", event.ModelReplacementResultRequested),
		"another to version":   event.ConfigChangeModelFor("user", 4, "model-a", "model-c", event.ModelReplacementResultRequested),
		"another result":       event.ConfigChangeModelFor("user", 4, "model-a", "model-b", "applied"),
	} {
		if other.ID == base.ID {
			t.Fatalf("%s answers the identity of the base move %q, want its own key", name, base.ID)
		}
	}

	committed := base.Event(time.Date(2026, time.November, 3, 12, 0, 0, 0, time.UTC))
	if got := committed.Meta[event.MetaKeyConfigChangeDimension]; got != event.ConfigChangeDimensionModel {
		t.Fatalf("dimension = %q, want the model dimension", got)
	}
	if got := committed.Meta[event.MetaKeyModelFromVersion]; got != "model-a" {
		t.Fatalf("from version = %q, want the version the move starts from", got)
	}
	if got := committed.Meta[event.MetaKeyModelToVersion]; got != "model-b" {
		t.Fatalf("to version = %q, want the version the move asks for", got)
	}
	if got := committed.Meta[event.MetaKeyModelReplacementResult]; got != event.ModelReplacementResultRequested {
		t.Fatalf("result = %q, want the result the producer states", got)
	}
}

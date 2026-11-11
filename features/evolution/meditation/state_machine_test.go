package meditation

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
)

// TestActivationEventCarriesCausality pins the state machine's discharge as a real external_input fact.
// - positive: a host-facing fact reaches the novelty rule and the injected event states rule, window and source
// - positive: an explicit reflection request discharges under its own rule
// - positive: accumulated novelty past the quiet floor discharges under the window rule
// - positive: every activation is the existing external_input type with no new fact family
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestActivationEventCarriesCausality(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cases := []struct {
		name string
		fact event.Event
		rule string
	}{
		{
			name: "a host-facing fact reaches the novelty rule",
			fact: event.Event{
				Key:       "user-fact",
				Type:      event.TypeExternalInput,
				Timestamp: now.Add(-time.Second),
				Meta:      map[string]string{event.MetaKeyTriggerSource: "user", event.MetaKeyEventSummary: "a user asked for help"},
				Message:   modelMessage("please improve the answer"),
			},
			rule: string(ruleNovelty),
		},
		{
			name: "an explicit request reaches the request rule",
			fact: event.Event{
				Key:       "request-fact",
				Type:      event.TypeExternalInput,
				Timestamp: now.Add(-time.Second),
				Meta:      map[string]string{event.MetaKeyTriggerSource: "user", MetaKeyActivationRequest: "true"},
				Message:   modelMessage("reflect on this now"),
			},
			rule: string(ruleExternalRequest),
		},
		{
			name: "old accumulated novelty reaches the quiet-window rule",
			fact: event.Event{
				Key:       "quiet-fact",
				Type:      event.TypeExternalInput,
				Timestamp: now.Add(-2 * time.Hour),
				Meta:      map[string]string{event.MetaKeyTriggerSource: "task"},
				Message:   modelMessage("a task finished"),
			},
			rule: string(ruleQuietWindow),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, err := NewAnchorStore(filepath.Join(t.TempDir(), "anchors.json"))
			if err != nil {
				t.Fatalf("open the anchor store: %v", err)
			}
			chain := newDriverChain("user")
			key := chain.append(tc.fact)
			input := &driverInput{}
			reflection, err := NewReflection(ReflectionConfig{
				Loop:    ReflectionLoop{Agent: "user", Session: "session-user"},
				Face:    ReflectionFace{ObservedNamespaces: []string{"user"}, ReadNamespaces: []string{"user"}},
				Cadence: ReflectionCadence{Interval: 30 * time.Minute},
				Prompt:  "reflect on the observed face",
			}, store, chain, input)
			if err != nil {
				t.Fatalf("assemble the reflection: %v", err)
			}
			outcome := reflection.Tick(context.Background(), now)
			if outcome.Decision.Disposition != TickFired {
				t.Fatalf("tick reached %q (%s), want a fired activation", outcome.Decision.Disposition, outcome.Decision.Reason)
			}
			if outcome.Injection == nil {
				t.Fatal("the fired tick carries no injection")
			}
			evt := outcome.Injection.Event
			if evt.Type != event.TypeExternalInput {
				t.Fatalf("activation type = %q, want the existing external_input type (no new fact family)", evt.Type)
			}
			meta := evt.Meta
			if got := meta[MetaKeyActivationRule]; got != tc.rule {
				t.Fatalf("activation rule = %q, want %q", got, tc.rule)
			}
			if meta[MetaKeyActivationWindowStart] == "" || meta[MetaKeyActivationWindowEnd] == "" {
				t.Fatalf("activation window = (%q, %q), want both ends stated", meta[MetaKeyActivationWindowStart], meta[MetaKeyActivationWindowEnd])
			}
			if got := meta[MetaKeyActivationSource]; got != string(key) {
				t.Fatalf("activation source = %q, want the charging fact %q", got, key)
			}
			if got := meta[MetaKeyActivationSourceLineage]; got != tc.fact.Meta[event.MetaKeyTriggerSource] {
				t.Fatalf("activation source lineage = %q, want %q", got, tc.fact.Meta[event.MetaKeyTriggerSource])
			}
			if got := meta[MetaKeyActivationCharges]; got == "" || got == "0" {
				t.Fatalf("activation charges = %q, want the accumulated count", got)
			}
			if got := meta[event.MetaKeyTriggerSource]; got != LineageReflection {
				t.Fatalf("activation trigger source = %q, want the reflection lineage", got)
			}
		})
	}
}

func modelMessage(content string) model.Message {
	return model.Message{Role: model.RoleUser, Content: content}
}

// TestEnergyConservationSelfLineageNotCharged pins the charge and its persistence through the anchor store.
// - positive: an external fact charges the open window once
// - negative: the same fact and self-managed lineage traffic charge nothing
// - negative: a held pass that re-reads the same scan charges nothing twice
// - positive: activation resets the charge and the reset persists across a restart
// - positive: accumulated external facts survive an assembly restart
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func TestEnergyConservationSelfLineageNotCharged(t *testing.T) {
	store, err := NewAnchorStore(filepath.Join(t.TempDir(), "anchors.json"))
	if err != nil {
		t.Fatalf("open the anchor store: %v", err)
	}
	machine := newStateMachine(store)
	now := time.Unix(1_700_000_000, 0)
	external := ScannedFact{Identity: FactIdentity{Key: "external"}, Content: FactContent{Timestamp: now}, Attribution: FactAttribution{Lineage: "user"}}
	if !machine.charge(external) {
		t.Fatal("a host-facing fact did not charge the state machine")
	}
	if machine.charge(external) {
		t.Fatal("the same fact charged the state machine twice")
	}
	selfManaged := ScannedFact{Identity: FactIdentity{Key: "self"}, Content: FactContent{Timestamp: now.Add(time.Minute)}, Attribution: FactAttribution{Lineage: LineageReflection}}
	if !SelfManagedLineage(LineageReflection) {
		t.Fatal("the reflection lineage does not read as self-managed, so the conservation rule would not apply")
	}
	if machine.charge(selfManaged) {
		t.Fatal("a self-managed fact charged the state machine")
	}
	if got := machine.snapshot().Charges; got != 1 {
		t.Fatalf("charges = %d, want one external fact beside the self-managed refusal", got)
	}

	discharge, ok := machine.evaluate(now.Add(time.Hour), 30*time.Minute)
	if !ok || discharge.Charges != 1 {
		t.Fatalf("evaluate = (%+v, %v), want the one accumulated charge", discharge, ok)
	}
	machine.reset(discharge)
	if got := machine.snapshot().Charges; got != 0 {
		t.Fatalf("charges after activation = %d, want the window reset", got)
	}
	if restored := newStateMachine(store).snapshot().Charges; restored != 0 {
		t.Fatalf("charges restored after the reset = %d, want the persisted reset", restored)
	}

	second := ScannedFact{Identity: FactIdentity{Key: "external-2"}, Content: FactContent{Timestamp: now.Add(2 * time.Minute)}, Attribution: FactAttribution{Lineage: "task"}}
	third := ScannedFact{Identity: FactIdentity{Key: "external-3"}, Content: FactContent{Timestamp: now.Add(3 * time.Minute)}, Attribution: FactAttribution{Lineage: "system_alert"}}
	machine.charge(second)
	machine.charge(third)
	if restored := newStateMachine(store).snapshot().Charges; restored != 2 {
		t.Fatalf("charges restored across a restart = %d, want the two accumulated external facts", restored)
	}

	if machine.charge(second) {
		t.Fatal("a held re-evaluation charged an older accumulated fact again")
	}
	if got := machine.snapshot().Charges; got != 2 {
		t.Fatalf("charges after re-reading the held scan = %d, want the same two facts", got)
	}
	if restored := newStateMachine(store); restored.charge(third) {
		t.Fatal("the restored machine forgot a charged fact and accepted it twice")
	} else if got := restored.snapshot().Charges; got != 2 {
		t.Fatalf("charges after the restored machine re-read the scan = %d, want the same two facts", got)
	}
}

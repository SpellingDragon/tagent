// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
package agent

import (
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	a2aprotocol "trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// TestHandoffEnvelopeDataPartCarriesTheSameSchema holds the remote carrier against the local one:
// - the data part names the data kind and carries the envelope's own field names
// - a data part decoded from the message JSON restores every field of the envelope
// - a decoded envelope naming no intent or no acceptance constraint is still refused by Validate
// - a part of another kind, a data part carrying nothing and a payload that does not decode are refused by name
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestHandoffEnvelopeDataPartCarriesTheSameSchema(t *testing.T) {
	complete := HandoffEnvelope{
		Intent:           "review the change",
		Constraints:      []string{"every finding names a file", "the verdict is explicit"},
		Grants:           []string{"tool:exec_run", "skill:go-service-ops"},
		PendingDecisions: []string{"whether the change ships"},
		Refs:             []event.EventID{"ev-context-1", "ev-context-2"},
	}
	part := complete.DataPart()
	if part.Kind != a2aprotocol.KindData {
		t.Fatalf("part kind = %q, want %q", part.Kind, a2aprotocol.KindData)
	}

	message := a2aprotocol.NewMessage(a2aprotocol.MessageRoleUser,
		[]a2aprotocol.Part{a2aprotocol.NewTextPart("review the change"), part})
	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("marshal the message = %v", err)
	}

	t.Run("the wire form keeps the declared field names", func(t *testing.T) {
		var wire struct {
			Parts []struct {
				Kind string          `json:"kind"`
				Data json.RawMessage `json:"data"`
			} `json:"parts"`
		}
		if err := json.Unmarshal(encoded, &wire); err != nil {
			t.Fatalf("read the wire message = %v", err)
		}
		if len(wire.Parts) != 2 || wire.Parts[0].Kind != a2aprotocol.KindText || wire.Parts[1].Kind != a2aprotocol.KindData {
			t.Fatalf("wire parts = %+v, want the request text beside one handoff data part", wire.Parts)
		}
		var carried map[string]json.RawMessage
		if err := json.Unmarshal(wire.Parts[1].Data, &carried); err != nil {
			t.Fatalf("read the handoff payload = %v", err)
		}
		names := make([]string, 0, len(carried))
		for name := range carried {
			names = append(names, name)
		}
		sort.Strings(names)
		if got, want := strings.Join(names, ","), "constraints,grants,intent,pending_decisions,refs"; got != want {
			t.Fatalf("payload field names = %q, want %q", got, want)
		}
	})

	t.Run("a decoded data part restores every field", func(t *testing.T) {
		var decoded a2aprotocol.Message
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("decode the message = %v", err)
		}
		if len(decoded.Parts) != 2 {
			t.Fatalf("decoded parts = %d, want the request beside the handoff", len(decoded.Parts))
		}
		restored, err := FromDataPart(decoded.Parts[1])
		if err != nil {
			t.Fatalf("read the decoded handoff = %v", err)
		}
		if !reflect.DeepEqual(restored, complete) {
			t.Fatalf("restored envelope = %+v, want %+v", restored, complete)
		}
		if err := restored.Validate(); err != nil {
			t.Fatalf("validate the restored envelope = %v, want it accepted", err)
		}
	})

	t.Run("a decoded envelope naming no intent is still refused", func(t *testing.T) {
		silent, err := FromDataPart(a2aprotocol.NewDataPart(map[string]any{"constraints": []string{"an explicit verdict"}}))
		if err != nil {
			t.Fatalf("read an envelope naming no intent = %v, want the payload read", err)
		}
		if err := silent.Validate(); !errors.Is(err, ErrHandoffIntentMissing) {
			t.Fatalf("validate an envelope naming no intent = %v, want %v", err, ErrHandoffIntentMissing)
		}
	})

	t.Run("a decoded envelope naming no constraint is still refused", func(t *testing.T) {
		unbounded, err := FromDataPart(a2aprotocol.NewDataPart(map[string]any{"intent": "review the change"}))
		if err != nil {
			t.Fatalf("read an envelope naming no constraint = %v, want the payload read", err)
		}
		if err := unbounded.Validate(); !errors.Is(err, ErrHandoffConstraintsMissing) {
			t.Fatalf("validate an envelope naming no constraint = %v, want %v", err, ErrHandoffConstraintsMissing)
		}
	})

	t.Run("a part that carries no data payload is refused", func(t *testing.T) {
		if _, err := FromDataPart(a2aprotocol.NewTextPart("review the change")); !errors.Is(err, ErrHandoffPartNotData) {
			t.Fatalf("read of a text part = %v, want %v", err, ErrHandoffPartNotData)
		}
		var absent *a2aprotocol.DataPart
		if _, err := FromDataPart(absent); !errors.Is(err, ErrHandoffPartNotData) {
			t.Fatalf("read of an absent data part = %v, want %v", err, ErrHandoffPartNotData)
		}
		if _, err := FromDataPart(a2aprotocol.NewDataPart(nil)); !errors.Is(err, ErrHandoffPartPayload) {
			t.Fatalf("read of a data part carrying nothing = %v, want %v", err, ErrHandoffPartPayload)
		}
	})

	t.Run("a payload that does not decode is refused", func(t *testing.T) {
		if _, err := FromDataPart(a2aprotocol.NewDataPart("not an envelope")); !errors.Is(err, ErrHandoffPartPayload) {
			t.Fatalf("read of a payload that is not an envelope = %v, want %v", err, ErrHandoffPartPayload)
		}
		if _, err := FromDataPart(a2aprotocol.NewDataPart(map[string]any{"intent": 7})); !errors.Is(err, ErrHandoffPartPayload) {
			t.Fatalf("read of a payload naming the wrong field type = %v, want %v", err, ErrHandoffPartPayload)
		}
	})
}

// TestHandoffEnvelopeContract holds the structured carrier of one delegated call:
// - a complete envelope validates and keeps every field it was built with
// - an envelope naming no intent or no non-empty constraint is refused by name
// - the external-context block renders every field under its own heading in fixed order
// - a list with no entry keeps its heading and states that it carries none
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestHandoffEnvelopeContract(t *testing.T) {
	complete := HandoffEnvelope{
		Intent:           "review the change",
		Constraints:      []string{"every finding names a file", "the verdict is explicit"},
		Grants:           []string{"tool:exec_run", "skill:go-service-ops"},
		PendingDecisions: []string{"whether the change ships"},
		Refs:             []event.EventID{"ev-context-1", "ev-context-2"},
	}

	t.Run("a complete envelope validates", func(t *testing.T) {
		if err := complete.Validate(); err != nil {
			t.Fatalf("validate the complete envelope = %v, want it accepted", err)
		}
	})

	t.Run("an envelope naming no intent is refused", func(t *testing.T) {
		blank := complete
		blank.Intent = "   "
		if err := blank.Validate(); !errors.Is(err, ErrHandoffIntentMissing) {
			t.Fatalf("validate an envelope with no intent = %v, want %v", err, ErrHandoffIntentMissing)
		}
	})

	t.Run("an envelope naming no constraint is refused", func(t *testing.T) {
		blank := complete
		blank.Constraints = nil
		if err := blank.Validate(); !errors.Is(err, ErrHandoffConstraintsMissing) {
			t.Fatalf("validate an envelope with no constraint = %v, want %v", err, ErrHandoffConstraintsMissing)
		}
	})

	t.Run("an envelope whose constraints are all blank is refused", func(t *testing.T) {
		blank := complete
		blank.Constraints = []string{"", "   "}
		if err := blank.Validate(); !errors.Is(err, ErrHandoffConstraintsMissing) {
			t.Fatalf("validate an envelope with blank constraints = %v, want %v", err, ErrHandoffConstraintsMissing)
		}
	})

	t.Run("the external context renders every field under its own heading", func(t *testing.T) {
		block := complete.ExternalContext()
		for _, want := range []string{
			"[handoff envelope]",
			"intent: review the change",
			"constraints:",
			"- every finding names a file",
			"- the verdict is explicit",
			"grants:",
			"- tool:exec_run",
			"- skill:go-service-ops",
			"pending_decisions:",
			"- whether the change ships",
			"refs:",
			"- ev-context-1",
			"- ev-context-2",
		} {
			if !strings.Contains(block, want) {
				t.Fatalf("external context carries no %q:\n%s", want, block)
			}
		}
		order := []string{"intent:", "constraints:", "grants:", "pending_decisions:", "refs:"}
		last := -1
		for _, heading := range order {
			at := strings.Index(block, heading)
			if at <= last {
				t.Fatalf("heading %q sits at %d, want it after %d:\n%s", heading, at, last, block)
			}
			last = at
		}
	})

	t.Run("a list with no entry keeps its heading", func(t *testing.T) {
		sparse := HandoffEnvelope{Intent: "review the change", Constraints: []string{"an explicit verdict"}}
		block := sparse.ExternalContext()
		for _, want := range []string{"grants: (none)", "pending_decisions: (none)", "refs: (none)"} {
			if !strings.Contains(block, want) {
				t.Fatalf("external context carries no %q:\n%s", want, block)
			}
		}
	})
}

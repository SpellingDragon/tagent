// 契约: docs/wiki/memory/memory-architecture.md#event-shape
package memory

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestDecodeEventBodyRefusesBodiesWithoutIdentity refuses a body whose bytes name no canonical identity by name.
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestDecodeEventBodyRefusesBodiesWithoutIdentity(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "no key", body: `{"type":"agent_output","summary":"s"}`, want: "no event key"},
		{name: "null key", body: `{"key":null,"type":"agent_output"}`, want: "no event key"},
		{name: "empty key", body: `{"key":"","type":"agent_output"}`, want: "no event key"},
		{name: "not json", body: `not json`, want: ErrEventBody.Error()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeEventBody([]byte(tc.body))
			if err == nil {
				t.Fatalf("decode %s: accepted a body carrying no identity", tc.body)
			}
			if !errors.Is(err, ErrEventBody) {
				t.Fatalf("decode %s: error %v does not wrap ErrEventBody", tc.body, err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("decode %s: error %q does not name %q", tc.body, err, tc.want)
			}
		})
	}
}

// TestDecodeEventBodyLegacyRefused refuses the bytes the earlier event_key writer committed: one body format exists.
//   - the decimal and the string spellings of that key are both refused by name
//   - the refusal wraps ErrEventBody and invents no key out of the fields beside it
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestDecodeEventBodyLegacyRefused(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			name: "decimal key",
			body: `{"event_key":1297376232073068544,"partition_id":144,` +
				`"event_type":"external_input","event_summary":"legacy summary",` +
				`"timestamp":1785019699865,"content":"legacy content","metadata":null}`,
		},
		{
			name: "string key",
			body: `{"event_key":"77","event_type":"agent_output","event_summary":"s"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decoded, err := DecodeEventBody([]byte(tc.body))
			if err == nil {
				t.Fatalf("decode %s: accepted the legacy bytes as %+v", tc.name, decoded)
			}
			if !errors.Is(err, ErrEventBody) {
				t.Fatalf("decode %s: error %v does not wrap ErrEventBody", tc.name, err)
			}
			if !strings.Contains(err.Error(), "carries no event key") {
				t.Fatalf("decode %s: error %q does not name the missing identity", tc.name, err)
			}
			if decoded.Key != "" {
				t.Fatalf("decode %s: invented the event key %q from the legacy fields", tc.name, decoded.Key)
			}
		})
	}
}

// TestEventBodyKeepsEveryFieldAcrossTheRoundTrip pins one committed body restored field for field.
//   - the restored event equals the committed one in meta, payload and every lineage identity
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestEventBodyKeepsEveryFieldAcrossTheRoundTrip(t *testing.T) {
	addr := event.Address{AgentID: "agent-a", SessionID: "session-body"}
	original := event.Event{
		Key:       event.EventID("fact-body-round-trip"),
		Type:      event.TypeAgentOutput,
		Timestamp: time.UnixMilli(1785019699865),
		Meta: map[string]string{
			event.MetaKeyEventSummary:      "the output of the drive",
			event.MetaPrefix + "resource":  "exec-7",
			event.MetaPrefix + "phase":     "stopped",
			event.MetaPrefix + "exit_code": "0",
		},
		Message: model.Message{
			Role: model.RoleAssistant,
			ToolCalls: []model.ToolCall{{
				ID:   "call-body",
				Type: "function",
				Function: model.FunctionDefinitionParam{
					Name:      "echo",
					Arguments: []byte(`{"x":1}`),
				},
			}},
		},
		Owner:        addr,
		Source:       addr,
		Target:       addr,
		InvocationID: "inv-body",
		TaskID:       "task-body",
		InstanceID:   "instance-body",
		EpisodeID:    "episode-body",
		DecisionID:   "decision-body",
		AttemptID:    "attempt-body",
		CallID:       "call-body",
		ApprovalID:   "approval-body",
	}
	body, err := BodyFromEvent(original, "text/plain", "keep")
	if err != nil {
		t.Fatalf("derive the body of the event: %v", err)
	}
	encoded, err := body.Encode()
	if err != nil {
		t.Fatalf("encode the body: %v", err)
	}
	decoded, err := DecodeEventBody(encoded)
	if err != nil {
		t.Fatalf("decode the body: %v", err)
	}
	restored := decoded.Event()
	want, _ := json.Marshal(original)
	got, _ := json.Marshal(restored)
	if string(got) != string(want) {
		t.Fatalf("the body restored the event %s, want the committed %s", got, want)
	}
}

// TestEventBodyStoresNoLightFieldBesideTheEvent pins the derived index of one committed body.
//   - the encoded body carries no summary and no content of its own
//   - the query record derives both out of the metadata projection and the native payload
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestEventBodyStoresNoLightFieldBesideTheEvent(t *testing.T) {
	original := event.Event{
		Key:       event.EventID("fact-derived-index"),
		Type:      event.TypeExternalInput,
		Timestamp: time.UnixMilli(1785019699865),
		Meta:      map[string]string{event.MetaKeyEventSummary: "the one summary"},
		Message:   model.Message{Role: model.RoleUser, Content: "the one content"},
		Owner:     event.Address{AgentID: "agent-a", SessionID: "session-body"},
	}
	body, err := BodyFromEvent(original, "text/plain", "keep")
	if err != nil {
		t.Fatalf("derive the body of the event: %v", err)
	}
	encoded, err := body.Encode()
	if err != nil {
		t.Fatalf("encode the body: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("decode the encoded body as a map: %v", err)
	}
	for _, stored := range []string{"summary", "content"} {
		if _, held := fields[stored]; held {
			t.Fatalf("the encoded body stores %q beside the event: %s", stored, encoded)
		}
	}
	record := body.Record(7, 1785019699865)
	if record.Summary != original.Meta[event.MetaKeyEventSummary] {
		t.Fatalf("record summary = %q, want the metadata projection entry %q",
			record.Summary, original.Meta[event.MetaKeyEventSummary])
	}
	if record.Content != original.Message.Content {
		t.Fatalf("record content = %q, want the native payload text %q", record.Content, original.Message.Content)
	}
}

// TestSameFactJudgesTheFactAndNotTheCommit pins the key semantics judgment over one identity.
//   - the stamps a commit path adds beside the fact decide nothing
//   - any field of the fact itself does
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestSameFactJudgesTheFactAndNotTheCommit(t *testing.T) {
	addr := event.Address{AgentID: "agent-a", SessionID: "session-body"}
	fact := event.Event{
		Key:       event.EventID("fact-key-semantics"),
		Type:      event.TypeExternalInput,
		Timestamp: time.UnixMilli(1785019699865),
		Message:   model.Message{Role: model.RoleUser, Content: "the one fact"},
		Owner:     addr,
	}
	body, err := BodyFromEvent(fact, "text/plain", "keep")
	if err != nil {
		t.Fatalf("derive the body of the fact: %v", err)
	}
	restamped := fact
	restamped.Timestamp = time.UnixMilli(1785019699999)
	restamped.InvocationID = "inv-second"
	restamped.InstanceID = "instance-second"
	repeat, err := BodyFromEvent(restamped, "text/plain", "keep")
	if err != nil {
		t.Fatalf("derive the body of the re-sent fact: %v", err)
	}
	if !body.SameFact(repeat) {
		t.Fatalf("the same fact judged as two facts beside fresh commit stamps")
	}
	moved := fact
	moved.Message = model.Message{Role: model.RoleUser, Content: "another fact"}
	conflict, err := BodyFromEvent(moved, "text/plain", "keep")
	if err != nil {
		t.Fatalf("derive the body of the moved fact: %v", err)
	}
	if body.SameFact(conflict) {
		t.Fatalf("two contents judged as the same fact under one identity")
	}
}

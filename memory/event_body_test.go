// 契约: docs/wiki/memory/memory-architecture.md#event-shape
package memory

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestDecodeEventBodyReadsBothGenerations pins the two body generations one store may hold.
//   - the current body round-trips every light field
//   - the older body keeps its decimal identity verbatim and fills type and summary
//   - both generations answer the same key the stored event is indexed under
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestDecodeEventBodyReadsBothGenerations(t *testing.T) {
	current := EventBody{
		Key:       event.EventID("42"),
		Type:      event.TypeExternalInput,
		Summary:   "summary",
		Content:   "content",
		Timestamp: 1785019699865,
	}
	encoded, err := current.Encode()
	if err != nil {
		t.Fatalf("encode the current body: %v", err)
	}
	decoded, err := DecodeEventBody(encoded)
	if err != nil {
		t.Fatalf("decode the current body: %v", err)
	}
	if !reflect.DeepEqual(decoded, current) {
		t.Fatalf("current body decoded as %+v, want %+v", decoded, current)
	}

	legacy := `{"event_key":1297376232073068544,"partition_id":144,` +
		`"event_type":"external_input","event_summary":"legacy summary",` +
		`"timestamp":1785019699865,"content":"legacy content","metadata":null}`
	decoded, err = DecodeEventBody([]byte(legacy))
	if err != nil {
		t.Fatalf("decode the legacy body: %v", err)
	}
	if got, want := decoded.Key, event.EventID("1297376232073068544"); got != want {
		t.Fatalf("legacy key decoded as %q, want %q", got, want)
	}
	if decoded.Type != event.TypeExternalInput || decoded.Summary != "legacy summary" || decoded.Content != "legacy content" {
		t.Fatalf("legacy body decoded as %+v, want the renamed light fields carried over", decoded)
	}
	if decoded.Timestamp != 1785019699865 {
		t.Fatalf("legacy timestamp decoded as %d, want 1785019699865", decoded.Timestamp)
	}

	quoted := `{"event_key":"77","event_type":"agent_output","event_summary":"s"}`
	decoded, err = DecodeEventBody([]byte(quoted))
	if err != nil {
		t.Fatalf("decode a quoted legacy key: %v", err)
	}
	if decoded.Key != event.EventID("77") {
		t.Fatalf("quoted legacy key decoded as %q, want 77", decoded.Key)
	}
}

// TestDecodeEventBodyRefusesBodiesWithoutIdentity refuses a body whose bytes name no canonical identity by name.
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestDecodeEventBodyRefusesBodiesWithoutIdentity(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "no key", body: `{"type":"agent_output","summary":"s"}`, want: "no event key"},
		{name: "null key", body: `{"event_key":null,"event_type":"agent_output"}`, want: "no event key"},
		{name: "fractional key", body: `{"event_key":1.5,"event_type":"agent_output"}`, want: "unsigned integer"},
		{name: "exponent key", body: `{"event_key":1e18,"event_type":"agent_output"}`, want: "unsigned integer"},
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

// TestEventBodyLegacyBytesReadAsAbsentFields pins the absent semantics of one earlier body.
//   - the light fields fill the message and the summary they name
//   - every identity those bytes never carried stays absent
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestEventBodyLegacyBytesReadAsAbsentFields(t *testing.T) {
	legacy := `{"event_key":42,"event_type":"external_input","event_summary":"legacy summary",` +
		`"timestamp":1785019699865,"content":"legacy content"}`
	decoded, err := DecodeEventBody([]byte(legacy))
	if err != nil {
		t.Fatalf("decode the legacy body: %v", err)
	}
	restored := decoded.Event()
	if restored.Key != event.EventID("42") || restored.Type != event.TypeExternalInput {
		t.Fatalf("the legacy body restored %+v, want the identity and type it carries", restored)
	}
	if restored.Message.Content != "legacy content" || restored.Message.Role != "" {
		t.Fatalf("the legacy body restored the message %+v, want its content and no role", restored.Message)
	}
	if restored.Meta[event.MetaKeyEventSummary] != "legacy summary" {
		t.Fatalf("the legacy body restored the meta %+v, want its summary named in it", restored.Meta)
	}
	if restored.InvocationID != "" || restored.TaskID != "" || restored.InstanceID != "" ||
		restored.EpisodeID != "" || restored.DecisionID != "" || restored.AttemptID != "" ||
		restored.CallID != "" || restored.ApprovalID != "" {
		t.Fatalf("the legacy body invented lineage the bytes never carried: %+v", restored)
	}
	if restored.Source != (event.Address{}) || restored.Target != (event.Address{}) {
		t.Fatalf("the legacy body invented addresses the bytes never carried: %+v", restored)
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

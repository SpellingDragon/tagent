// 契约: docs/wiki/memory/memory-architecture.md#event-shape
package memory

import (
	"errors"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
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
	if decoded != current {
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

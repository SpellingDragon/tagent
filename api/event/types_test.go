package event

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestEventAddressInvariants pins the address partition every ownership edge is read through.
// - a complete address names both an agent and a session, and the zero address names neither
// - a half-filled address is neither zero nor complete, so a target is never defaulted into place
// - the two predicates never hold together, and neither half alone is normalized into a complete address
// 契约: docs/wiki/event/event-architecture.md#overview
func TestEventAddressInvariants(t *testing.T) {
	complete := Address{AgentID: "agent-a", SessionID: "session-a"}
	zero := Address{}
	agentOnly := Address{AgentID: "agent-a"}
	sessionOnly := Address{SessionID: "session-a"}

	t.Run("a complete address names both an agent and a session", func(t *testing.T) {
		if !complete.IsComplete() {
			t.Fatalf("complete address IsComplete = false, want true")
		}
		if complete.IsZero() {
			t.Fatalf("complete address IsZero = true, want false")
		}
	})

	t.Run("the zero address names neither half", func(t *testing.T) {
		if !zero.IsZero() {
			t.Fatalf("zero address IsZero = false, want true")
		}
		if zero.IsComplete() {
			t.Fatalf("zero address IsComplete = true, want false")
		}
	})

	t.Run("a half-filled address is neither zero nor complete", func(t *testing.T) {
		for name, addr := range map[string]Address{"agent only": agentOnly, "session only": sessionOnly} {
			if addr.IsZero() || addr.IsComplete() {
				t.Fatalf("%s address = (zero %v, complete %v), want neither", name, addr.IsZero(), addr.IsComplete())
			}
		}
	})

	t.Run("neither half alone is normalized into a complete address", func(t *testing.T) {
		if agentOnly == complete || sessionOnly == complete {
			t.Fatalf("a half-filled address equals the complete one, want distinct addresses")
		}
		if (Address{AgentID: complete.AgentID}).IsComplete() {
			t.Fatalf("an agent-only address IsComplete = true, want a session still missing")
		}
		if (Address{SessionID: complete.SessionID}).IsComplete() {
			t.Fatalf("a session-only address IsComplete = true, want an agent still missing")
		}
	})
}

// TestEventIdentityBlockInvariants pins which fields quote an event and which only describe it.
// - the key alone quotes the event, so a fact re-owned or re-routed still quotes the same identity
// - a different key quotes a different event even when every other field is equal
// - the recorded time is carried as stated and never derived from the key or the type
// - an unset event carries no key, no type and no recorded time to mistake for a stored fact
// 契约: docs/wiki/event/event-architecture.md#overview
func TestEventIdentityBlockInvariants(t *testing.T) {
	recorded := time.Unix(1_700_000_000, 123_000_000)
	stored := Event{
		Key:       "event-1",
		Type:      TypeExternalInput,
		Timestamp: recorded,
		Meta:      map[string]string{MetaKeyEventSummary: "summary"},
	}

	t.Run("re-owning and re-routing a fact leaves its quoted identity alone", func(t *testing.T) {
		moved := stored
		moved.Owner = Address{AgentID: "agent-b", SessionID: "session-b"}
		moved.Source = Address{AgentID: "agent-c", SessionID: "session-c"}
		moved.Target = Address{AgentID: "agent-d", SessionID: "session-d"}
		moved.CallID = "call-9"
		if moved.Key != stored.Key {
			t.Fatalf("re-owned event Key = %q, want %q", moved.Key, stored.Key)
		}
		if moved.Type != stored.Type {
			t.Fatalf("re-owned event Type = %q, want %q", moved.Type, stored.Type)
		}
		if !moved.Timestamp.Equal(recorded) {
			t.Fatalf("re-owned event Timestamp = %v, want %v carried as recorded", moved.Timestamp, recorded)
		}
		if reflect.DeepEqual(moved, stored) {
			t.Fatalf("the ownership blocks are equal, want the ownership change to be real")
		}
	})

	t.Run("a different key quotes another event beside equal content", func(t *testing.T) {
		other := stored
		other.Key = "event-2"
		if other.Key == stored.Key {
			t.Fatalf("two events share Key %q, want distinct quoted identities", other.Key)
		}
		if !reflect.DeepEqual(other.Meta, stored.Meta) || other.Type != stored.Type || !other.Timestamp.Equal(stored.Timestamp) {
			t.Fatalf("the compared events differ beyond the key, want only the key to differ")
		}
	})

	t.Run("an unset event claims no stored identity", func(t *testing.T) {
		var unset Event
		if unset.Key != "" {
			t.Fatalf("unset event Key = %q, want empty", unset.Key)
		}
		if unset.Type != "" {
			t.Fatalf("unset event Type = %q, want empty", unset.Type)
		}
		if !unset.Timestamp.IsZero() {
			t.Fatalf("unset event Timestamp = %v, want the zero time", unset.Timestamp)
		}
	})
}

// TestEventVocabularyInvariants pins the frozen names a stored event is read back through.
// - every event type name is non-empty and distinct, so one type string names exactly one category
// - every metadata key is non-empty, distinct and unprefixed, so a frozen key is never read as business metadata
// - parsed metadata partitions every entry exactly once and never mutates the map it read
// - a prefixed key is never read as a frozen field, and a frozen key never lands in the pass-through set
// 契约: docs/wiki/event/event-architecture.md#const-list
func TestEventVocabularyInvariants(t *testing.T) {
	types := []struct{ name, value string }{
		{"TypeExternalInput", TypeExternalInput},
		{"TypeAgentOutput", TypeAgentOutput},
		{"TypeActionCommand", TypeActionCommand},
		{"TypeThinkingPlan", TypeThinkingPlan},
		{"TypeContextCompress", TypeContextCompress},
		{"TypeTaskSettled", TypeTaskSettled},
		{"TypeFeedback", TypeFeedback},
		{"TypeGovernance", TypeGovernance},
	}
	metaKeys := []struct{ name, value string }{
		{"MetaKeyEventKey", MetaKeyEventKey},
		{"MetaKeyPartitionID", MetaKeyPartitionID},
		{"MetaKeyEventType", MetaKeyEventType},
		{"MetaKeyEventSummary", MetaKeyEventSummary},
		{"MetaKeyTriggerSource", MetaKeyTriggerSource},
		{"MetaKeySettleTriggerSource", MetaKeySettleTriggerSource},
		{"MetaKeyBundleID", MetaKeyBundleID},
		{"MetaKeyNoticeKind", MetaKeyNoticeKind},
		{"MetaKeyTaskID", MetaKeyTaskID},
	}

	t.Run("event type names are non-empty and distinct", func(t *testing.T) {
		seen := map[string]string{}
		for _, typ := range types {
			if typ.value == "" {
				t.Fatalf("%s is empty, want a frozen type string", typ.name)
			}
			if first, ok := seen[typ.value]; ok {
				t.Fatalf("%s and %s both read %q, want distinct type names", first, typ.name, typ.value)
			}
			seen[typ.value] = typ.name
		}
	})

	t.Run("metadata keys are non-empty, distinct and unprefixed", func(t *testing.T) {
		if MetaPrefix == "" {
			t.Fatalf("MetaPrefix is empty, want the pass-through marker")
		}
		seen := map[string]string{}
		for _, key := range metaKeys {
			if key.value == "" {
				t.Fatalf("%s is empty, want a frozen metadata key", key.name)
			}
			if strings.HasPrefix(key.value, MetaPrefix) {
				t.Fatalf("%s reads %q, which carries the pass-through prefix", key.name, key.value)
			}
			if first, ok := seen[key.value]; ok {
				t.Fatalf("%s and %s both read %q, want distinct metadata keys", first, key.name, key.value)
			}
			seen[key.value] = key.name
		}
	})

	t.Run("every entry is partitioned exactly once and the input is left as it was", func(t *testing.T) {
		meta := map[string]string{
			MetaKeyEventKey:            "event-1",
			MetaKeyPartitionID:         "partition-1",
			MetaKeyEventType:           TypeAgentOutput,
			MetaKeyEventSummary:        "summary",
			MetaKeyTriggerSource:       "trigger",
			MetaKeySettleTriggerSource: "settle",
			MetaKeyBundleID:            "bundle",
			MetaKeyNoticeKind:          "settled",
			MetaKeyTaskID:              "task-1",
			MetaPrefix + "tenant":      "tenant-1",
			MetaPrefix + "trace":       "trace-1",
			"business field":           "kept verbatim",
		}
		before := make(map[string]string, len(meta))
		for key, value := range meta {
			before[key] = value
		}

		parsed := ParseEventMeta(meta)

		if parsed.EventKey != "event-1" || parsed.PartitionID != "partition-1" || parsed.EventType != TypeAgentOutput {
			t.Fatalf("frozen identity entries = (%q, %q, %q), want the three named entries",
				parsed.EventKey, parsed.PartitionID, parsed.EventType)
		}
		if parsed.EventSummary != "summary" || parsed.TriggerSource != "trigger" ||
			parsed.SettleTriggerSource != "settle" || parsed.BundleID != "bundle" ||
			parsed.NoticeKind != "settled" || parsed.TaskID != "task-1" {
			t.Fatalf("frozen descriptive entries = (%q, %q, %q, %q, %q, %q), want every named entry",
				parsed.EventSummary, parsed.TriggerSource, parsed.SettleTriggerSource, parsed.BundleID,
				parsed.NoticeKind, parsed.TaskID)
		}
		if len(parsed.PassThrough) != 2 || parsed.PassThrough[MetaPrefix+"tenant"] != "tenant-1" ||
			parsed.PassThrough[MetaPrefix+"trace"] != "trace-1" {
			t.Fatalf("pass-through entries = %v, want the two prefixed entries verbatim", parsed.PassThrough)
		}
		if len(parsed.Unknown) != 1 || parsed.Unknown["business field"] != "kept verbatim" {
			t.Fatalf("unknown entries = %v, want the unprefixed entry kept verbatim", parsed.Unknown)
		}

		accounted := len(parsed.PassThrough) + len(parsed.Unknown)
		for key := range meta {
			switch key {
			case MetaKeyEventKey, MetaKeyPartitionID, MetaKeyEventType, MetaKeyEventSummary,
				MetaKeyTriggerSource, MetaKeySettleTriggerSource, MetaKeyBundleID,
				MetaKeyNoticeKind, MetaKeyTaskID:
				accounted++
			}
		}
		if accounted != len(meta) {
			t.Fatalf("partitioned entries = %d, want every one of %d entries accounted for", accounted, len(meta))
		}
		if !reflect.DeepEqual(meta, before) {
			t.Fatalf("input metadata = %v, want it left exactly as it was", meta)
		}
	})

	t.Run("a prefixed key is never read as a frozen field", func(t *testing.T) {
		shadow := MetaPrefix + MetaKeyEventKey
		parsed := ParseEventMeta(map[string]string{shadow: "shadow-value"})
		if parsed.EventKey != "" {
			t.Fatalf("EventKey = %q, want empty for a pass-through key", parsed.EventKey)
		}
		if parsed.PassThrough[shadow] != "shadow-value" {
			t.Fatalf("pass-through entry = %q, want the prefixed entry kept verbatim", parsed.PassThrough[shadow])
		}
		if len(parsed.Unknown) != 0 {
			t.Fatalf("unknown entries = %v, want the prefixed key outside the unknown set", parsed.Unknown)
		}
	})

	t.Run("a frozen key never lands in the pass-through set", func(t *testing.T) {
		parsed := ParseEventMeta(map[string]string{MetaKeyEventKey: "event-1"})
		if len(parsed.PassThrough) != 0 {
			t.Fatalf("pass-through entries = %v, want a frozen key read as a named field only", parsed.PassThrough)
		}
		if parsed.EventKey != "event-1" {
			t.Fatalf("EventKey = %q, want %q", parsed.EventKey, "event-1")
		}
	})

	t.Run("an absent metadata map parses to empty sets instead of nil", func(t *testing.T) {
		parsed := ParseEventMeta(nil)
		if parsed.PassThrough == nil || parsed.Unknown == nil {
			t.Fatalf("parsed sets = (pass-through nil %v, unknown nil %v), want allocated empty maps",
				parsed.PassThrough == nil, parsed.Unknown == nil)
		}
		if len(parsed.PassThrough) != 0 || len(parsed.Unknown) != 0 {
			t.Fatalf("parsed sets = (%v, %v), want both empty", parsed.PassThrough, parsed.Unknown)
		}
	})
}

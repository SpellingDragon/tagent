package event

import (
	"testing"

	frameworkevent "trpc.group/trpc-go/trpc-agent-go/event"
)

// TestParseEventMeta_RoundTrip: values injected under the contract keys are
// recovered exactly by ParseEventMeta (metadata completeness invariant).
func TestParseEventMeta_RoundTrip(t *testing.T) {
	evt := frameworkevent.New("inv-1", "tagent")
	evt.StateDelta = map[string][]byte{
		MetaKeyEventKey:          []byte(FormatEventKey(1297375767008641024)),
		MetaKeyPartitionID:       []byte("144"),
		MetaKeyEventType:         []byte("agent_output"),
		MetaKeyEventSummary:      []byte("summary text"),
		MetaKeyTriggerSource:     []byte("task"),
		MetaPrefix + "chat_id":   []byte("room-42"),
		MetaPrefix + "user_name": []byte("alice"),
		"unrelated":              []byte("ignored"),
	}

	meta := ParseEventMeta(evt)
	if meta.EventKey != 1297375767008641024 {
		t.Errorf("EventKey = %d", meta.EventKey)
	}
	if meta.PartitionID != 144 {
		t.Errorf("PartitionID = %d", meta.PartitionID)
	}
	if meta.EventType != "agent_output" || meta.EventSummary != "summary text" {
		t.Errorf("type/summary mismatch: %+v", meta)
	}
	if meta.TriggerSource != "task" {
		t.Errorf("TriggerSource = %q", meta.TriggerSource)
	}
	if meta.Meta["chat_id"] != "room-42" || meta.Meta["user_name"] != "alice" {
		t.Errorf("passthrough meta mismatch: %+v", meta.Meta)
	}
	if _, ok := meta.Meta["unrelated"]; ok {
		t.Error("non-contract keys must not leak into Meta")
	}
}

// TestParseEventMeta_NilSafe: nil events and missing fields yield zero values
// and a non-nil Meta map.
func TestParseEventMeta_NilSafe(t *testing.T) {
	meta := ParseEventMeta(nil)
	if meta.Meta == nil {
		t.Fatal("Meta must be non-nil")
	}
	if meta.EventKey != 0 || meta.TriggerSource != "" {
		t.Errorf("zero values expected: %+v", meta)
	}
}

package memory

import (
	"encoding/json"
	"strings"
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
)

// TestBindFeedback 钉住 feedback event lands in the
//
// 契约: docs/wiki/memory/memory-architecture.md#feedback-bind
func TestBindFeedback(t *testing.T) {
	store := NewInMemoryStore()
	pid := PartitionIDFromName("tagent")
	parentKey := NewSnowflakeEventKey(pid, testBaseMs)
	if err := store.StoreEvent(parentKey, FullEvent{
		EventKey: parentKey, PartitionID: pid,
		EventType: tagentevent.TypeAgentOutput, Timestamp: testBaseMs,
	}); err != nil {
		t.Fatal(err)
	}

	fbKey, err := BindFeedback(store, parentKey, FeedbackPayload{
		Verdict: "negative", Note: "wrong file", Source: "task_settle",
	})
	if err != nil {
		t.Fatalf("BindFeedback: %v", err)
	}
	got, err := store.GetEvent(fbKey)
	if err != nil || got == nil {
		t.Fatalf("feedback not stored: %v", err)
	}
	if got.PartitionID != pid {
		t.Fatalf("feedback partition = %d, want %d", got.PartitionID, pid)
	}
	if got.EventType != tagentevent.TypeFeedback {
		t.Fatalf("type = %s", got.EventType)
	}
	if got.Metadata[tagentevent.MetaKeySubtype] != "task_settle" {
		t.Fatalf("subtype = %q", got.Metadata[tagentevent.MetaKeySubtype])
	}
	var p FeedbackPayload
	if err := json.Unmarshal([]byte(got.Content), &p); err != nil {
		t.Fatalf("content not JSON: %v", err)
	}
	if p.Verdict != "negative" || p.Source != "task_settle" {
		t.Fatalf("payload = %+v", p)
	}
	if p.ParentKey != tagentevent.FormatEventKey(parentKey) {
		t.Fatalf("parent_key = %q", p.ParentKey)
	}
}

// TestBindFeedback_ParentMiss 钉住 explicit error, nothing written.
//
// 契约: docs/wiki/memory/memory-architecture.md#feedback-bind
func TestBindFeedback_ParentMiss(t *testing.T) {
	store := NewInMemoryStore()
	ghost := NewSnowflakeEventKey(9, testBaseMs)
	if _, err := BindFeedback(store, ghost, FeedbackPayload{Verdict: "positive", Source: "api"}); err == nil {
		t.Fatal("expected explicit error for missing parent")
	} else if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestBindFeedback_InheritsBundleID 钉住 feedback event
//
// 契约: docs/wiki/memory/memory-architecture.md#feedback-bind
func TestBindFeedback_InheritsBundleID(t *testing.T) {
	store := NewInMemoryStore()
	pid := PartitionIDFromName("tagent")
	parentKey := NewSnowflakeEventKey(pid, testBaseMs)
	_ = store.StoreEvent(parentKey, FullEvent{
		EventKey: parentKey, PartitionID: pid, Timestamp: testBaseMs,
		Metadata: map[string]string{"bundle_id": "b-xyz"},
	})
	fbKey, err := BindFeedback(store, parentKey, FeedbackPayload{Verdict: "negative", Source: "task_settle"})
	if err != nil {
		t.Fatal(err)
	}
	fb, _ := store.GetEvent(fbKey)
	if fb.Metadata["bundle_id"] != "b-xyz" {
		t.Fatalf("feedback must inherit parent bundle_id, got %v", fb.Metadata)
	}
}

package agent

import (
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestAttribution_BundleIDStamped is the D1-B regression
// (design-report-closeout): both event persistence paths stamp the active
// bundle id into FullEvent.Metadata. persistBusEvent (path 2) is exercised
// directly; the RunFlow path (path 1) reads the same cm.bundleIDFn into the
// Attribution carrier (context_manager RunFlow assembly) — nil fn must not
// write the key (zero behavior change when evolution is disabled).
func TestAttribution_BundleIDStamped(t *testing.T) {
	store := memory.NewInMemoryStore()
	cm := &ContextManager{
		name:        "tagent",
		memStore:    store,
		projection:  compress.NewSessionProjection(),
		partitionID: 7,
	}
	// Guard: persistBusEvent with nil projection must not panic (Append on
	// nil *SessionProjection is a method with nil-receiver safety; if not,
	// the test would fail here loudly).
	evt := &AgentEvent{
		Type:      "external_input",
		Source:    "user",
		Timestamp: time.Now(),
		Message:   &model.Message{Role: model.RoleUser, Content: "hi"},
	}

	t.Run("nil fn no key", func(t *testing.T) {
		before := storedCount(t, store)
		cm.persistBusEvent(evt)
		if got := storedCount(t, store); got != before+1 {
			t.Fatalf("event not persisted: %d -> %d", before, got)
		}
		e := lastEvent(t, store)
		if _, ok := e.Metadata[tagentevent.MetaKeyBundleID]; ok {
			t.Fatal("bundle_id stamped without provider (zero-behavior change violated)")
		}
	})

	t.Run("provider stamps", func(t *testing.T) {
		cm.bundleIDFn = func() string { return "b-abc123" }
		cm.persistBusEvent(evt)
		e := lastEvent(t, store)
		if got := e.Metadata[tagentevent.MetaKeyBundleID]; got != "b-abc123" {
			t.Fatalf("bundle_id = %q, want b-abc123", got)
		}
	})
}

func storedCount(t *testing.T, s *memory.InMemoryStore) int {
	t.Helper()
	refs, err := s.QueryEvents(memory.QueryOptions{PartitionIDs: []int{7}, Limit: 10000})
	if err != nil {
		t.Fatal(err)
	}
	return len(refs)
}

func lastEvent(t *testing.T, s *memory.InMemoryStore) memory.FullEvent {
	t.Helper()
	refs, err := s.QueryEvents(memory.QueryOptions{PartitionIDs: []int{7}, Limit: 10000, OrderBy: "timestamp_desc"})
	if err != nil || len(refs) == 0 {
		t.Fatalf("no events: %v", err)
	}
	e, err := s.GetEvent(refs[0].EventKey)
	if err != nil || e == nil {
		t.Fatalf("get last: %v", err)
	}
	return *e
}

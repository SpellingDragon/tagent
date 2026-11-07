package memory

import (
	"context"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestMechanismWordingNeverLands pins the wording rule.
// - a compression mechanism fact is refused by the observation table by frozen type
// - a business fact still lands with its own summary and content
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestMechanismWordingNeverLands(t *testing.T) {
	ctx := context.Background()
	table := NewStoreQuerySource()
	for _, typ := range []string{event.CompressProductType, event.CompressRequestType} {
		evt := event.Event{
			Key:     event.EventID("cmpout-" + typ),
			Type:    typ,
			Meta:    map[string]string{event.MetaKeyEventSummary: "compress product -hist-internal"},
			Message: model.Message{Content: "internal wording"},
			Owner:   event.Address{AgentID: "agent-a", SessionID: "session-wording"},
		}
		body, err := BodyFromEvent(evt, "test", "test")
		if err != nil {
			t.Fatalf("encode the %s fact: %v", typ, err)
		}
		encoded, err := body.Encode()
		if err != nil {
			t.Fatalf("marshal the %s body: %v", typ, err)
		}
		stored := ledger.StoredEvent{Key: evt.Key, PartitionID: 1, WindowTS: 1, Seq: 1, Body: encoded}
		if err := table.LandEvent(ctx, stored); err != nil {
			t.Fatalf("land the %s fact: %v", typ, err)
		}
		if table.Observed(evt.Key) {
			t.Fatalf("the %s fact carried its internal wording into the query face", typ)
		}
	}
	business := event.Event{
		Key:     "134000",
		Type:    event.TypeExternalInput,
		Meta:    map[string]string{event.MetaKeyEventSummary: "ask"},
		Message: model.Message{Content: "the material of the landing walk"},
		Owner:   event.Address{AgentID: "agent-a", SessionID: "session-wording"},
	}
	body, err := BodyFromEvent(business, "test", "test")
	if err != nil {
		t.Fatalf("encode the business fact: %v", err)
	}
	encoded, err := body.Encode()
	if err != nil {
		t.Fatalf("marshal the business body: %v", err)
	}
	if err := table.LandEvent(ctx, ledger.StoredEvent{Key: business.Key, PartitionID: 1, WindowTS: 1, Seq: 2, Body: encoded}); err != nil {
		t.Fatalf("land the business fact: %v", err)
	}
	if !table.Observed(business.Key) {
		t.Fatal("a business fact was refused by the observation table")
	}
}

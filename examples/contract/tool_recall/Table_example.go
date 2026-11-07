package recallexample

import (
	"context"
	"fmt"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// storeContractEvent files one event used by the durable Table example.
func storeContractEvent(tier *ledger.Tier, key event.EventID, partition int, at time.Time, summary string) error {
	evt := event.Event{Key: key, Type: event.TypeExternalInput, Timestamp: at, Meta: map[string]string{event.MetaKeyEventSummary: summary}, Message: model.Message{Role: model.RoleUser, Content: summary}, Owner: event.Address{AgentID: "agent-example", SessionID: "session-example"}}
	body, err := memory.BodyFromEvent(evt, "text", "default")
	if err != nil {
		return err
	}
	encoded, err := body.Encode()
	if err != nil {
		return err
	}
	return tier.Segments.StoreEvent(context.Background(), ledger.SegmentRecord{PartitionID: partition, WindowTS: at.UnixMilli(), Seq: 1, EventKey: key, EventTime: at.UnixMilli(), Body: encoded}.WithChain(evt.Type, evt.Owner.SessionID))
}

// ExampleTable demonstrates the durable read face's partition scope, total
// order and exact original reference, with no observation table or landing pass.
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-contract
func ExampleTable() {
	ctx := context.Background()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageMemory})
	if err != nil {
		panic(err)
	}
	defer tier.Segments.Close()
	at := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	if err := storeContractEvent(tier, "1-a", 7, at, "first"); err != nil {
		return
	}
	if err := storeContractEvent(tier, "2-b", 7, at.Add(time.Hour), "second"); err != nil {
		return
	}
	if err := storeContractEvent(tier, "3-z", 8, at, "outside"); err != nil {
		return
	}
	table := assembly.NewTierQuerySource(tier.Segments)
	page, err := table.QueryEvents(ctx, memory.QueryOptions{PartitionIDs: []int{7}, Limit: 1, OrderBy: memory.QueryOrderByDesc})
	if err != nil || len(page) != 1 || page[0].Key != "2-b" {
		panic("unexpected scoped page")
	}
	if _, held := table.Event("9-z"); held {
		panic("missing key was held")
	}
	if _, held := table.Reference("1-a"); !held {
		panic("reference was not held")
	}
	fmt.Println("ok")
}

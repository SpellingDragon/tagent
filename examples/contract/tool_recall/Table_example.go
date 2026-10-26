package recallexample

import (
	"context"
	"sort"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/tool/recall"
)

// landedTable is the canonical observation table: it answers the references one
// declarative query matches, the reference of one landed event, and the original
// reference that event was landed with, all from the same set of landed events.
type landedTable struct {
	events []memory.EventReference
	refs   map[event.EventID]ledger.OriginalRef
}

// QueryEvents returns the references matching opts, ordered newest first and cut to the
// page the query names.
func (t landedTable) QueryEvents(_ context.Context, opts memory.QueryOptions) ([]memory.EventReference, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	scoped := make(map[int]bool, len(opts.PartitionIDs))
	for _, partition := range opts.PartitionIDs {
		scoped[partition] = true
	}
	matched := make([]memory.EventReference, 0, len(t.events))
	for _, ref := range t.events {
		if !scoped[ref.PartitionID] {
			continue
		}
		if opts.StartTime > 0 && ref.Timestamp < opts.StartTime {
			continue
		}
		if opts.EndTime > 0 && ref.Timestamp > opts.EndTime {
			continue
		}
		matched = append(matched, ref)
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].Key > matched[j].Key })
	if opts.Limit > 0 && opts.Limit < len(matched) {
		matched = matched[:opts.Limit]
	}
	return matched, nil
}

// Event returns the reference of the landed event under key.
func (t landedTable) Event(key event.EventID) (memory.EventReference, bool) {
	for _, ref := range t.events {
		if ref.Key == key {
			return ref, true
		}
	}
	return memory.EventReference{}, false
}

// Reference returns the original reference key was landed with.
func (t landedTable) Reference(key event.EventID) (ledger.OriginalRef, bool) {
	ref, held := t.refs[key]
	return ref, held
}

var _ recall.Table = landedTable{}

// ExampleTable answers one page, one landed event and one original reference from the same
// table: a key the table holds no event for reads as not held instead of as an empty page,
// and the page never carries an event outside the partitions the query names.
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-contract
func ExampleTable() {
	ctx := context.Background()
	table := landedTable{
		events: []memory.EventReference{
			{Key: "2-b", PartitionID: 7, Type: "agent_output", Summary: "second", Timestamp: 2000},
			{Key: "1-a", PartitionID: 7, Type: "external_input", Summary: "first", Timestamp: 1000},
		},
		refs: map[event.EventID]ledger.OriginalRef{
			"1-a": {Key: "1-a", Format: "text", RetentionPolicy: "default"},
		},
	}
	page, err := table.QueryEvents(ctx, memory.QueryOptions{PartitionIDs: []int{7}, Limit: 1})
	if err != nil || len(page) != 1 || page[0].Key != "2-b" {
		return
	}
	if _, held := table.Event("9-z"); held {
		return
	}
	if _, held := table.Reference("1-a"); !held {
		return
	}
}

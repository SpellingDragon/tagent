package memorysearchexample

import (
	"context"
	"sort"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/search"
)

// observedSource is the canonical query face a retrieval reads through: the event
// references a query matches, and the recall reference of any event key the observation
// table holds.
type observedSource struct {
	events []memory.EventReference
	refs   map[event.EventID]ledger.OriginalRef
}

// QueryEvents returns the matching set filtered, totally ordered by (Timestamp, Key),
// deduplicated by event key and then paged by Offset and Limit.
func (s *observedSource) QueryEvents(_ context.Context, opts memory.QueryOptions) ([]memory.EventReference, error) {
	matched := make([]memory.EventReference, 0, len(s.events))
	seen := make(map[event.EventID]struct{}, len(s.events))
	for _, ev := range s.events {
		if opts.PartitionID != 0 && ev.PartitionID != opts.PartitionID {
			continue
		}
		if _, ok := seen[ev.Key]; ok {
			continue
		}
		seen[ev.Key] = struct{}{}
		matched = append(matched, ev)
	}
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].Timestamp != matched[j].Timestamp {
			return matched[i].Timestamp < matched[j].Timestamp
		}
		return matched[i].Key < matched[j].Key
	})
	if opts.Offset > len(matched) {
		return nil, nil
	}
	matched = matched[opts.Offset:]
	if opts.Limit > 0 && opts.Limit < len(matched) {
		matched = matched[:opts.Limit]
	}
	return matched, nil
}

// Reference returns the recall reference the event key was landed with, and reports
// whether the table holds one.
func (s *observedSource) Reference(key event.EventID) (ledger.OriginalRef, bool) {
	ref, ok := s.refs[key]
	return ref, ok
}

var _ search.ReferenceSource = (*observedSource)(nil)

// ExampleReferenceSource reads one page of matching references in (Timestamp, Key) order
// and hydrates the recall reference of the key the page names.
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func ExampleReferenceSource() {
	ctx := context.Background()
	source := &observedSource{
		events: []memory.EventReference{
			{Key: "event-2", PartitionID: 1, Type: "conversation", Timestamp: 20},
			{Key: "event-1", PartitionID: 1, Type: "conversation", Timestamp: 10},
		},
		refs: map[event.EventID]ledger.OriginalRef{"event-1": {Key: "event-1", Format: "text"}},
	}

	page, err := source.QueryEvents(ctx, memory.QueryOptions{PartitionID: 1, Limit: 1, OrderBy: memory.QueryOrderByAsc})
	if err != nil || len(page) != 1 || page[0].Key != "event-1" {
		return
	}
	ref, ok := source.Reference(page[0].Key)
	if !ok {
		return
	}
	if _, ok := source.Reference("event-9"); ok {
		return
	}
	_ = ref.Format
}

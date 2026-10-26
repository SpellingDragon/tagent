package memorysearchexample

import (
	"context"

	"github.com/SpellingDragon/tagent/memory/search"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// authorizedSearcher is the canonical authorized-scope query port: it answers inside the
// partitions the caller is authorized to read and never widens to the whole store.
type authorizedSearcher struct {
	authorized map[string][]ledger.OriginalRef
}

// Search returns the reference set of q, bounded by the page the query asks for.
func (s *authorizedSearcher) Search(_ context.Context, q search.Query) (search.Result, error) {
	refs := make([]ledger.OriginalRef, 0, q.Limit)
	for _, partition := range q.Partitions {
		for _, ref := range s.authorized[partition] {
			refs = append(refs, ref)
			if q.Limit > 0 && len(refs) == q.Limit {
				return search.Result{Refs: refs}, nil
			}
		}
	}
	return search.Result{Refs: refs}, nil
}

var _ search.Searcher = (*authorizedSearcher)(nil)

// ExampleSearcher answers one query inside the authorized partitions, bounded by the
// requested page, and reports an unauthorized partition as no hit rather than a widened
// read.
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func ExampleSearcher() {
	ctx := context.Background()
	searcher := &authorizedSearcher{authorized: map[string][]ledger.OriginalRef{
		"partition-1": {{Key: "event-1", Format: "text"}, {Key: "event-2", Format: "text"}},
	}}

	page, err := searcher.Search(ctx, search.Query{Text: "hello", Strategy: search.StrategyHybrid, Partitions: []string{"partition-1"}, Limit: 1})
	if err != nil || len(page.Refs) != 1 {
		return
	}
	outside, err := searcher.Search(ctx, search.Query{Text: "hello", Strategy: search.StrategyHybrid, Partitions: []string{"partition-2"}, Limit: 1})
	if err != nil || len(outside.Refs) != 0 {
		return
	}
	empty := &authorizedSearcher{}
	if result, err := empty.Search(ctx, search.Query{Partitions: []string{"partition-1"}, Limit: 1}); err != nil || len(result.Refs) != 0 {
		return
	}
}

package memorysearchexample

import (
	"context"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory/search"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// rankFusion is the canonical hybrid recall port: it merges the vector leg and the keyword
// leg into the single ordered reference set the caller receives, and hands out no
// continuation of its own.
type rankFusion struct{}

// Fuse merges the two legs under strategy and returns the ordered reference set they fuse
// into.
func (rankFusion) Fuse(_ context.Context, strategy search.Strategy, vector, keyword []ledger.OriginalRef) (search.Result, error) {
	switch strategy {
	case search.StrategyVectorOnly:
		return search.Result{Refs: append([]ledger.OriginalRef(nil), vector...)}, nil
	case search.StrategyKeywordOnly:
		return search.Result{Refs: append([]ledger.OriginalRef(nil), keyword...)}, nil
	}
	seen := make(map[event.EventID]struct{}, len(vector)+len(keyword))
	fused := make([]ledger.OriginalRef, 0, len(vector)+len(keyword))
	for _, leg := range [][]ledger.OriginalRef{vector, keyword} {
		for _, ref := range leg {
			if _, ok := seen[ref.Key]; ok {
				continue
			}
			seen[ref.Key] = struct{}{}
			fused = append(fused, ref)
		}
	}
	return search.Result{Refs: fused}, nil
}

var _ search.Fusion = rankFusion{}

// ExampleFusion fuses one vector leg and one keyword leg into a single reference set that
// carries each hit once and no continuation.
//
// 契约: docs/wiki/memory/memory-architecture.md#inmemory-retrieval
func ExampleFusion() {
	ctx := context.Background()
	var fusion search.Fusion = rankFusion{}
	shared := ledger.OriginalRef{Key: "event-2", Format: "text"}
	vector := []ledger.OriginalRef{{Key: "event-1", Format: "text"}, shared}
	keyword := []ledger.OriginalRef{shared, {Key: "event-3", Format: "text"}}

	fused, err := fusion.Fuse(ctx, search.StrategyHybrid, vector, keyword)
	if err != nil || len(fused.Refs) != 3 || fused.NextCursor != "" {
		return
	}
	only, err := fusion.Fuse(ctx, search.StrategyVectorOnly, vector, keyword)
	if err != nil || len(only.Refs) != len(vector) {
		return
	}
	_ = fused.Refs[0].Key
}

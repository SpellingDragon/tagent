package search

import (
	"context"
	"errors"
	"sort"

	"github.com/SpellingDragon/tagent/memory"
)

// 契约: docs/wiki/memory/memory-architecture.md#inmemory-retrieval
//
// Fusion is the hybrid recall port: it merges the vector leg and the keyword
// leg one query raised into the single ordered reference set the caller
// receives.
//
// The redline list (3 rules):
// MUST merge the two legs into the single ordered reference set the caller receives.
// MUST NOT widen the caller's authorized scope or read a leg of its own.
// MUST NOT answer a fused hit with a copy of the text, which stays behind the original path.
type Fusion interface {
	// Fuse merges the vector leg and the keyword leg under strategy and returns
	// the ordered reference set they fuse into. Each leg is a reference list
	// ranked from the best down and an empty leg is not an error; naming the
	// legs as references keeps the port independent of any embedding framework.
	// A fused set is complete as given and hands out no continuation: its
	// NextCursor stays zero, and paging is answered by the retrieval port from
	// the legs before they fuse, not by the fusion.
	Fuse(ctx context.Context, strategy Strategy, vector, keyword []memory.OriginalRef) (Result, error)
}

// Strategy parameterizes how the retrieval legs of a query are combined. It is
// an explicit argument rather than a fixed choice of this package, so the rule
// a query runs under is the rule its caller named.
type Strategy string

const (
	// StrategyHybrid fuses the vector leg and the keyword leg by reciprocal
	// rank: a reference scores the sum of one over the rank constant plus its
	// rank, and a tie is broken toward the larger event key.
	StrategyHybrid Strategy = "hybrid"
	// StrategyKeywordOnly keeps the keyword ranking alone, the path a query
	// whose vector leg returned nothing falls to.
	StrategyKeywordOnly Strategy = "keyword_only"
	// StrategyVectorOnly keeps the vector ranking alone.
	StrategyVectorOnly Strategy = "vector_only"
)

// RRFRankConstant is the rank constant of the reciprocal rank rule, the
// canonical value the hybrid strategy uses.
const RRFRankConstant = 60

// ErrStrategyUnwired reports the vector-only strategy: it declares no consumer
// anywhere in the runtime and is therefore not implemented. The constant stays
// declared so the strategy vocabulary does not change underneath callers, and
// a query naming it is refused by name instead of being answered by an
// implementation nobody asked for.
var ErrStrategyUnwired = errors.New("search: the vector-only strategy has no consumer and is not implemented")

// ErrStrategyUnknown reports a query naming a strategy outside the declared
// vocabulary, so no rule could decide how its legs combine.
var ErrStrategyUnknown = errors.New("search: the query names an unknown strategy")

// RRFFusion is the Fusion implementation of this package. It carries the two strategies that have
// consumers — hybrid and keyword-only — and refuses the rest by name.
//
// - The hybrid fallback is byte-identical by contract: an empty vector leg (unconfigured embedder, dropped generation or no hit) runs the very same code path keyword-only runs, so a caller cannot tell the two apart and a vector pipeline coming up later never changes what a keyword-only answer looked like.
// - The fusion grants nothing: both legs arrive already read under the caller's authorized scope and the fused result stays inside it.
// - A fused hit stays a reference, so a fused set is never a second copy of the text behind it.
type RRFFusion struct{}

// NewRRFFusion returns the hybrid fusion port.
func NewRRFFusion() Fusion {
	return RRFFusion{}
}

var _ Fusion = RRFFusion{}

// Fuse merges the two legs under strategy and returns the ordered reference
// set they fuse into, on the terms of the Fusion port.
func (RRFFusion) Fuse(ctx context.Context, strategy Strategy, vector, keyword []memory.OriginalRef) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	switch strategy {
	case StrategyKeywordOnly:
		return keywordOnly(keyword), nil
	case StrategyHybrid:
		if len(vector) == 0 {
			return keywordOnly(keyword), nil
		}
		return reciprocalRank(vector, keyword), nil
	case StrategyVectorOnly:
		return Result{}, ErrStrategyUnwired
	default:
		return Result{}, ErrStrategyUnknown
	}
}

// keywordOnly returns the keyword ranking alone as the fused set: the result
// carries the leg's references in the leg's order and hands out no
// continuation. It is the single path both keyword-only and the empty-vector
// hybrid fallback answer through, so their answers are one answer.
func keywordOnly(keyword []memory.OriginalRef) Result {
	refs := make([]memory.OriginalRef, len(keyword))
	copy(refs, keyword)
	return Result{Refs: refs}
}

// reciprocalRank fuses the two legs by reciprocal rank: a reference scores the
// sum of one over the rank constant plus its rank over the legs it appears in,
// ranks counted from the top of each leg, and the fused set is ordered by
// descending score with ties broken toward the larger event key. A reference
// appearing in both legs carries both contributions; the same reference twice
// inside one leg is counted at its first rank only.
func reciprocalRank(vector, keyword []memory.OriginalRef) Result {
	scores := map[memory.OriginalRef]float64{}
	accumulate := func(leg []memory.OriginalRef) {
		seen := make(map[memory.OriginalRef]bool, len(leg))
		for rank, ref := range leg {
			if seen[ref] {
				continue
			}
			seen[ref] = true
			scores[ref] += 1 / float64(RRFRankConstant+rank)
		}
	}
	accumulate(vector)
	accumulate(keyword)

	refs := make([]memory.OriginalRef, 0, len(scores))
	for ref := range scores {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		a, b := refs[i], refs[j]
		if scores[a] != scores[b] {
			return scores[a] > scores[b]
		}
		if a.Key != b.Key {
			return a.Key > b.Key
		}
		if a.Owner != b.Owner {
			return a.Owner.AgentID < b.Owner.AgentID || (a.Owner.AgentID == b.Owner.AgentID && a.Owner.SessionID < b.Owner.SessionID)
		}
		if a.Format != b.Format {
			return a.Format < b.Format
		}
		return a.RetentionPolicy < b.RetentionPolicy
	})
	return Result{Refs: refs}
}

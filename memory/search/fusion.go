package search

import (
	"context"

	"github.com/SpellingDragon/tagent/memory"
)

// 契约: docs/wiki/memory/memory-architecture.md#inmemory-retrieval
//
// Fusion is the hybrid recall port: it merges the vector leg and the keyword
// leg one query raised into the single ordered reference set the caller
// receives.
//
// It reads nothing of its own and grants nothing. Both legs arrive already read
// under the caller's authorized scope, so a reference outside that scope enters
// neither a leg nor the fused result, and a fusion never widens the scope. A
// fused hit stays a reference: the text behind it is still read through the
// original path, so a fused set is never a second copy of the text.
type Fusion interface {
	// Fuse merges the vector leg and the keyword leg under strategy and returns
	// the ordered reference set they fuse into. Each leg is a reference list
	// ranked from the best down and an empty leg is not an error; naming the
	// legs as references keeps the port independent of any embedding framework.
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

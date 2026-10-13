// 契约: docs/wiki/memory/memory-architecture.md#overview
//
// Skeleton compile-time assertions for the phase 1 retrieval port. The phase 2
// types that satisfy the ports do not exist yet, so there is no implementation
// assertion to write; the assertions below pin what the skeleton proves on its
// own: every declared field name and type, every declared method signature, and
// every value that must stay a compile-time constant.
package search

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// The declared shapes and their fields.
var (
	_ = Query{
		Partitions: []string{},
		Types:      []string{},
		Since:      time.Time{},
		Until:      time.Time{},
		Relations:  []event.EventID{},
		Limit:      0,
	}

	_ = Result{
		Refs: []memory.OriginalRef{},
	}
)

// The exported port methods must keep exactly these signatures.
var (
	_ func(Searcher, context.Context, Query) (Result, error) = Searcher.Search

	_ func(Fusion, context.Context, Strategy, []memory.OriginalRef, []memory.OriginalRef) (Result, error) = Fusion.Fuse
)

// The declared ports must stay interface types.
var (
	_ Searcher = (Searcher)(nil)
	_ Fusion   = (Fusion)(nil)
)

// The strategies and the rank constant must stay compile-time constants: a
// variable or a computed value stops this block from compiling.
const (
	_ Strategy = StrategyHybrid
	_ Strategy = StrategyKeywordOnly
	_ Strategy = StrategyVectorOnly

	_ = RRFRankConstant
)

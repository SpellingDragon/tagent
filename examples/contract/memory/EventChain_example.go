package memoryexample

import (
	"context"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// chainRead is the canonical fact chain: committed facts kept in EventKey write order and
// paged by minimum event key.
type chainRead struct {
	facts []replay.ChainFact
}

// ScanFrom pages the chain in write order, after the given key.
func (c *chainRead) ScanFrom(_ context.Context, after event.EventID, limit int) ([]replay.ChainFact, error) {
	if limit < 1 {
		return nil, replay.ErrChainPageLimit
	}
	page := make([]replay.ChainFact, 0, limit)
	for _, fact := range c.facts {
		if fact.Key <= after {
			continue
		}
		page = append(page, fact)
		if len(page) == limit {
			break
		}
	}
	return page, nil
}

// Fact returns the committed fact under key and reports whether one is held.
func (c *chainRead) Fact(_ context.Context, key event.EventID) (replay.ChainFact, bool, error) {
	for _, fact := range c.facts {
		if fact.Key == key {
			return fact, true, nil
		}
	}
	return replay.ChainFact{}, false, nil
}

var _ replay.EventChain = (*chainRead)(nil)

// ExampleEventChain replays one page of the chain after a given key, then hydrates the
// fact a compaction ref names.
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func ExampleEventChain() {
	ctx := context.Background()
	chain := &chainRead{facts: []replay.ChainFact{
		{Key: "event-1", Chain: replay.ChainKey{PartitionID: 1, SessionID: "session-1"}, Type: "conversation", Summary: "first turn"},
		{Key: "event-2", Chain: replay.ChainKey{PartitionID: 1, SessionID: "session-1"}, Type: "conversation", Summary: "second turn"},
	}}

	page, err := chain.ScanFrom(ctx, "", 1)
	if err != nil || len(page) != 1 {
		return
	}
	if _, err := chain.ScanFrom(ctx, page[0].Key, 0); err != replay.ErrChainPageLimit {
		return
	}
	fact, held, err := chain.Fact(ctx, "event-2")
	if err != nil || !held {
		return
	}
	_ = fact.Summary
}

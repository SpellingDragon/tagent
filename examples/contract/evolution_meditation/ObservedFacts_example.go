package evolutionmeditationexample

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory"
)

// chainRead is the canonical observed-fact face: it answers the references one query
// matches and hydrates a single reference into the fact it names, and both calls are
// reads.
type chainRead struct {
	refs  []memory.EventReference
	facts map[event.EventID]event.Event
}

// QueryEvents returns the references matching opts.
func (c *chainRead) QueryEvents(_ context.Context, opts memory.QueryOptions) ([]memory.EventReference, error) {
	matched := make([]memory.EventReference, 0, len(c.refs))
	for _, ref := range c.refs {
		if len(opts.EventTypes) > 0 && ref.Type != opts.EventTypes[0] {
			continue
		}
		matched = append(matched, ref)
	}
	return matched, nil
}

// CommittedFact returns the committed fact the identity names; an identity with no
// committed fact behind it is an error, never an empty fact.
func (c *chainRead) CommittedFact(_ context.Context, id event.EventID) (event.Event, error) {
	fact, ok := c.facts[id]
	if !ok {
		return event.Event{}, errors.New("evolution: no committed fact under " + string(id))
	}
	return fact, nil
}

var _ meditation.ObservedFacts = (*chainRead)(nil)

// ExampleObservedFacts runs the conditional retrieval a reflection judgment runs: the
// newest references are hydrated one by one and the read stops at its first hit.
//
// 契约: docs/wiki/evolution/evolution-architecture.md#evidence-window
func ExampleObservedFacts() {
	ctx := context.Background()
	chain := &chainRead{
		refs: []memory.EventReference{
			{Key: "event-1", Type: event.TypeExternalInput},
		},
		facts: map[event.EventID]event.Event{
			"event-1": {Key: "event-1", Type: event.TypeExternalInput},
		},
	}

	refs, err := chain.QueryEvents(ctx, memory.QueryOptions{EventTypes: []string{event.TypeExternalInput}})
	if err != nil {
		return
	}
	if len(refs) != 1 {
		return
	}
	if _, err := chain.CommittedFact(ctx, refs[0].Key); err != nil {
		return
	}
}

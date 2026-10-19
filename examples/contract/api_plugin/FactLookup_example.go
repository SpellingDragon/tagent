package pluginexample

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// tableFacts is the canonical fact lookup: it answers the fact as it committed and refuses
// an identity that never committed.
type tableFacts map[event.EventID]event.Event

// CommittedFact returns the committed fact the identity names, or an error when no fact
// committed under it.
func (f tableFacts) CommittedFact(_ context.Context, id event.EventID) (event.Event, error) {
	evt, ok := f[id]
	if !ok {
		return event.Event{}, errors.New("facts: no committed fact under " + string(id))
	}
	return evt, nil
}

var _ plugin.FactLookup = tableFacts{}

// ExampleFactLookup resolves one committed fact: the fact comes back as it committed, and
// an identity with no committed fact behind it is refused instead of answered with an
// empty fact.
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func ExampleFactLookup() {
	facts := tableFacts{event.EventID("fact-1"): {Key: "fact-1"}}
	evt, err := facts.CommittedFact(context.Background(), event.EventID("fact-1"))
	if err != nil {
		return
	}
	_ = evt.Key
	if _, missing := facts.CommittedFact(context.Background(), event.EventID("fact-2")); missing == nil {
		return
	}
}

// Package rlexample shows the read face the chain export consumes.
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-recorder
package rlexample

import (
	"context"
	"fmt"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/features/rl"
)

// sessionFacts is the canonical chain-read face: the facts one session
// committed, in commit order, as the chain holds them.
type sessionFacts struct{}

func (sessionFacts) SessionFacts(context.Context, event.Address) ([]event.Event, error) {
	return nil, nil
}

var _ rl.FactReader = sessionFacts{}

// ExampleFactReader demonstrates reading one session fact stream.
func ExampleFactReader() {
	var reader rl.FactReader = sessionFacts{}
	facts, err := reader.SessionFacts(context.Background(), event.Address{AgentID: "a", SessionID: "s"})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(len(facts))
}

// Package rlexample shows the read face the chain export consumes.
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-recorder
package rlexample

import (
	"fmt"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/rl"
)

// sessionFacts is the canonical chain-read face: the facts one session
// committed, in commit order, as the chain holds them.
type sessionFacts struct{}

func (sessionFacts) SessionFacts(addr event.Address) []event.Event { return nil }

var _ rl.FactReader = sessionFacts{}

// ExampleFactReader demonstrates reading one session fact stream.
func ExampleFactReader() {
	var reader rl.FactReader = sessionFacts{}
	facts := reader.SessionFacts(event.Address{AgentID: "a", SessionID: "s"})
	fmt.Println(len(facts))
	// Output: 0
}

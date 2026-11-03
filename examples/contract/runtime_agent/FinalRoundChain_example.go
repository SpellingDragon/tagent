// Package agentexample shows the final round chain the budget planner reads and
// files through.
//
// 契约: docs/wiki/agent/event-flow.md#watchdog-final-round
package agentexample

import (
	"context"
	"fmt"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

type demoChain struct{}

func (demoChain) AddressOf(_ context.Context, _ event.EventID) (event.Address, bool) {
	return event.Address{AgentID: "agent-a", SessionID: "session-a"}, true
}

func (demoChain) SegmentOf(_ context.Context, _ event.Address) (agent.FinalRoundSegment, bool) {
	return agent.FinalRoundSegment{Range: "inv-a", Rounds: 3, Started: time.Now()}, true
}

func (demoChain) FileFinalRoundRequest(_ context.Context, _ event.Address, request event.FinalRoundRequest) error {
	fmt.Printf("filed %s for range %s\n", request.Reason, request.Range)
	return nil
}

// ExampleFinalRoundChain demonstrates the budget judgment read off one chain and
// filed back on it as the fact it is.
func ExampleFinalRoundChain() {
	chain := demoChain{}
	policy := func(event.AgentID) agent.FinalRoundPolicy {
		return agent.FinalRoundPolicy{MaxRounds: 3}
	}
	planner := agent.NewFinalRoundPlanner(chain, policy)
	if err := planner.ObserveCommit(context.Background(), "evt-call"); err != nil {
		fmt.Printf("observe failed: %v\n", err)
	}
}

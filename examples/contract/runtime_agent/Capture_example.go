// Package agentexample shows the loop-side capture port.
//
// 契约: docs/wiki/agent/agent-architecture.md#core-components
package agentexample

import (
	"context"
	"fmt"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

type turnCapture struct{ sealed int }

func (c *turnCapture) Snapshot(context.Context, event.DecisionID, string, string, []string, model.Request) error {
	return nil
}
func (c *turnCapture) Record(context.Context, event.AttemptID, event.DecisionID, event.EpisodeID, model.Outcome) error {
	return nil
}
func (c *turnCapture) Seal(context.Context) error { c.sealed++; return nil }

var _ agent.Capture = (*turnCapture)(nil)

// ExampleCapture demonstrates the three points one turn fires.
func ExampleCapture() {
	var cap agent.Capture = &turnCapture{}
	_ = cap.Seal(context.Background())
	fmt.Println("sealed")
}

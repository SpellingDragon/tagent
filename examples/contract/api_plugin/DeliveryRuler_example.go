package pluginexample

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// reviewUserBodies is the canonical delivery ruler: it rules one outbound body
// by the lineage of the turn that produced it.
type reviewUserBodies struct{}

// Rule allows a user-lineage body and holds every other lineage as a wait.
func (reviewUserBodies) Rule(_ context.Context, delivery plugin.Delivery) (plugin.Disposition, error) {
	if delivery.Lineage == "user" {
		return plugin.DispositionAllow, nil
	}
	return plugin.DispositionHold, nil
}

var _ plugin.DeliveryRuler = reviewUserBodies{}

// ExampleDeliveryRuler rules one turn body before the sending plugin delivers
// it: the allowed body is sent, and a held body waits for the host's decision.
//
// 契约: docs/wiki/agent/governance-enforcement.md#disposition-and-risk
func ExampleDeliveryRuler() {
	face := reviewUserBodies{}
	allowed, err := face.Rule(context.Background(), plugin.Delivery{
		Content: "hello",
		Lineage: "user",
		Target:  event.Address{AgentID: "user", SessionID: "session-1"},
	})
	if err != nil {
		return
	}
	held, err := face.Rule(context.Background(), plugin.Delivery{
		Content: "a notice the review waits on",
		Lineage: "task",
		Target:  event.Address{AgentID: "user", SessionID: "session-1"},
	})
	if err != nil {
		return
	}
	_ = allowed
	_ = held
}

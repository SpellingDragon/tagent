package evolutionexample

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/evolution"
)

// ownLoopSession is the canonical reflection input face: it commits one reflection input
// into the observing agent's own loop session and reports what became of that turn.
type ownLoopSession struct {
	injected []event.Event
	outcome  evolution.ReflectionOutcome
}

// InjectReflection commits one reflection input into the agent's own loop session and
// reports the outcome of its turn.
func (s *ownLoopSession) InjectReflection(_ context.Context, evt event.Event) (evolution.ReflectionReceipt, error) {
	s.injected = append(s.injected, evt)
	return evolution.ReflectionReceipt{
		Key:     evt.Key,
		Outcome: s.outcome,
		At:      time.Now(),
	}, nil
}

var _ evolution.ReflectionInput = (*ownLoopSession)(nil)

// ExampleReflectionInput injects one fired reflection and reports that only a consumed
// turn advances the watermark: a deferred turn leaves the input to the next tick.
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func ExampleReflectionInput() {
	ctx := context.Background()
	session := &ownLoopSession{outcome: evolution.ReflectionDeferred}

	receipt, err := session.InjectReflection(ctx, event.Event{
		Key:   "reflection-1",
		Type:  event.TypeThinkingPlan,
		Owner: event.Address{AgentID: "assistant", SessionID: "session-1"},
	})
	if err != nil {
		return
	}
	if receipt.Outcome != evolution.ReflectionDeferred {
		return
	}
	if len(session.injected) != 1 {
		return
	}
}

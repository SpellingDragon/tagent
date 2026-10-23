package evolutionmeditationexample

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/evolution/meditation"
)

// ownLoopSession is the canonical reflection input face: it commits one reflection input
// into the observing agent's own loop session and reports what became of that turn.
type ownLoopSession struct {
	injected []event.Event
	outcome  meditation.ReflectionOutcome
}

// InjectReflection commits one reflection input into the agent's own loop session and
// reports the outcome of its turn.
func (s *ownLoopSession) InjectReflection(_ context.Context, evt event.Event) (meditation.ReflectionReceipt, error) {
	s.injected = append(s.injected, evt)
	return meditation.ReflectionReceipt{
		Key:     evt.Key,
		Outcome: s.outcome,
		At:      time.Now(),
	}, nil
}

var _ meditation.ReflectionInput = (*ownLoopSession)(nil)

// ExampleReflectionInput injects one fired reflection and reports that only a consumed
// turn advances the watermark: a deferred turn leaves the input to the next tick.
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func ExampleReflectionInput() {
	ctx := context.Background()
	session := &ownLoopSession{outcome: meditation.ReflectionDeferred}

	receipt, err := session.InjectReflection(ctx, event.Event{
		Key:   "reflection-1",
		Type:  event.TypeThinkingPlan,
		Owner: event.Address{AgentID: "assistant", SessionID: "session-1"},
	})
	if err != nil {
		return
	}
	if receipt.Outcome != meditation.ReflectionDeferred {
		return
	}
	if len(session.injected) != 1 {
		return
	}
}

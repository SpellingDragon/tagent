package evolutionmeditationexample

import (
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/features/evolution/meditation"
)

// liveBindings is the canonical live-binding face: it reports the complete addresses
// the run has established for one agent, and an agent with no binding answers none.
type liveBindings struct {
	bound map[event.AgentID][]event.Address
}

// ActiveSessions returns the addresses bound to agentID.
func (l *liveBindings) ActiveSessions(agentID event.AgentID) []event.Address {
	return append([]event.Address(nil), l.bound[agentID]...)
}

var _ meditation.SessionQuery = (*liveBindings)(nil)

// ExampleSessionQuery resolves one declared target into its live sessions and reports
// that an agent with no binding answers none.
//
// 契约: docs/wiki/evolution/evolution-architecture.md#evidence-window
func ExampleSessionQuery() {
	targets := &liveBindings{bound: map[event.AgentID][]event.Address{
		"user-agent": {{AgentID: "user-agent", SessionID: "session-user"}},
	}}

	if sessions := targets.ActiveSessions("user-agent"); len(sessions) != 1 {
		return
	}
	if sessions := targets.ActiveSessions("absent"); len(sessions) != 0 {
		return
	}
}

package agentexample

import (
	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
)

// sessionTranscript is the canonical history face: it keeps one transcript per explicit
// session address, hands out copies and appends one ended drive's transcript beside the
// history the earlier drives committed.
type sessionTranscript struct {
	history map[event.Address][]model.Message
}

func newSessionTranscript() *sessionTranscript {
	return &sessionTranscript{history: map[event.Address][]model.Message{}}
}

// SessionHistory reports the messages the named session accumulated, as a copy the
// caller owns.
func (t *sessionTranscript) SessionHistory(addr event.Address) []model.Message {
	return append([]model.Message(nil), t.history[addr]...)
}

// CommitHistory appends the transcript one ended drive accumulated to the history of the
// named session.
func (t *sessionTranscript) CommitHistory(addr event.Address, msgs []model.Message) {
	t.history[addr] = append(t.history[addr], msgs...)
}

var _ agent.HistoryFace = newSessionTranscript()

// ExampleHistoryFace commits the transcript of one ended drive and reads it back beside
// the input the next drive aggregates.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func ExampleHistoryFace() {
	addr := event.Address{AgentID: "assistant", SessionID: "session-1"}
	history := newSessionTranscript()
	history.CommitHistory(addr, []model.Message{
		{Role: model.RoleUser, Content: "first question"},
		{Role: model.RoleAssistant, Content: "first answer"},
	})
	committed := history.SessionHistory(addr)
	committed = append(committed, model.Message{Role: model.RoleUser, Content: "second question"})
	_ = len(committed)
}

package agentexample

import (
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// sessionTranscript is the canonical history face: it keeps one transcript and one
// fact chain per explicit session address, hands out copies, appends one ended drive's
// transcript beside the history the earlier drives committed, and reports the chain
// the drive reads its range's input from at a decision boundary.
type sessionTranscript struct {
	history map[event.Address][]model.Message
	facts   map[event.Address][]event.Event
}

func newSessionTranscript() *sessionTranscript {
	return &sessionTranscript{
		history: map[event.Address][]model.Message{},
		facts:   map[event.Address][]event.Event{},
	}
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

// Committed records one fact the named session committed, in commit order, so the chain
// read below answers the material a drive landed while its decisions ran.
func (t *sessionTranscript) Committed(addr event.Address, evt event.Event) {
	t.facts[addr] = append(t.facts[addr], evt)
}

// SessionFacts reports the facts the named session committed, in commit order, as a copy
// the caller owns.
func (t *sessionTranscript) SessionFacts(addr event.Address) []event.Event {
	return append([]event.Event(nil), t.facts[addr]...)
}

var _ agent.HistoryFace = newSessionTranscript()

// ExampleHistoryFace commits the transcript of one ended drive and reads it back beside
// the input the next drive aggregates, and it reads the fact chain of the session back
// as the input a decision boundary of a drive in flight reads again.
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

	history.Committed(addr, event.Event{
		Key:          "fact-in-flight",
		Type:         event.TypeExternalInput,
		Message:      model.Message{Role: model.RoleUser, Content: "a message that arrived in flight"},
		InvocationID: "invocation-1",
	})
	for _, fact := range history.SessionFacts(addr) {
		if fact.InvocationID != "invocation-1" {
			continue
		}
		_ = fact.Message.Content
	}
}

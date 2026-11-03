package agentexample

import (
	"fmt"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/agent"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
)

// chainPrefix is the canonical session chain face: it carries the transcript and
// the facts of one session chain, and beside them the facts whose own text the
// request prefix of that session presents. That presentation is an optional
// capability of the chain face the loop already reads, not a second seam: the
// loop type-asserts the attached HistoryFace to the capability, so a chain that
// keeps no prefix simply answers none and the pull keeps the chain's boundary.
// It reports a key only for an entry the prefix still holds in full — an entry
// the projection folded into a summary or degraded to a ticket keeps no key
// here — and it reports nothing for a fact the boundary of the ended material
// leaves behind, so the two halves of one presentation, the prefix and the
// segment transcript, read each fact exactly once between them.
type chainPrefix struct {
	history   map[event.Address][]trpcmodel.Message
	facts     map[event.Address][]event.Event
	presented map[event.Address]map[event.EventID]bool
}

// SessionHistory reports the messages the named session accumulated across the
// drives that already ended on it.
func (c chainPrefix) SessionHistory(addr event.Address) []trpcmodel.Message {
	return append([]trpcmodel.Message(nil), c.history[addr]...)
}

// CommitHistory appends the transcript one ended drive accumulated to the
// history of the named session.
func (c chainPrefix) CommitHistory(addr event.Address, msgs []trpcmodel.Message) {
	c.history[addr] = append(c.history[addr], msgs...)
}

// SessionFacts reports the facts the named session committed, in commit order.
func (c chainPrefix) SessionFacts(addr event.Address) []event.Event {
	return append([]event.Event(nil), c.facts[addr]...)
}

// SessionPresented reports the facts of the named session whose text the request
// prefix of that session presents, keyed by fact identity. It is the optional
// capability the resident consumption loop reads from the chain face itself.
func (c chainPrefix) SessionPresented(addr event.Address) map[event.EventID]bool {
	return c.presented[addr]
}

var _ agent.HistoryFace = chainPrefix{}

// ExampleChainPresentation reads the presentation of one session chain back:
// the input the prefix carries in full is reported, so a resident consumption
// loop leaves it to the prefix instead of carrying it a second time, while the
// input the fold replaced with a summary is not reported and therefore stays the
// material of the segment that reads it. The loop derives this read from the
// chain face it already has; no presentation face is attached beside it.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func ExampleChainPresentation() {
	addr := event.Address{AgentID: "assistant", SessionID: "session-1"}
	chain := chainPrefix{
		facts: map[event.Address][]event.Event{
			addr: {{Key: "fact-carried-in-full"}, {Key: "fact-folded-into-a-summary"}},
		},
		presented: map[event.Address]map[event.EventID]bool{
			addr: {"fact-carried-in-full": true},
		},
	}
	loop := agent.NewLoop(nil, nil, "instance-example", nil)
	loop.SetHistory(chain)
	shown := chain.SessionPresented(addr)
	behind := 0
	for _, fact := range chain.SessionFacts(addr) {
		if !shown[fact.Key] {
			behind++
		}
	}
	fmt.Printf("presented=%d behind=%d\n", len(shown), behind)
}

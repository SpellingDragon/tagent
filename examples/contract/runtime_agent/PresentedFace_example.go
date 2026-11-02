package agentexample

import (
	"fmt"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// presentedPrefix is the canonical presentation read: it keeps, per session
// address, the identity of every fact whose own text the request prefix of that
// session presents. It reports a key only for an entry the prefix still holds in
// full — an entry the projection folded into a summary or degraded to a ticket
// keeps no key here — and it reports nothing for a fact the boundary of the ended
// material leaves behind, so the two halves of one presentation, the prefix and
// the segment transcript, read each fact exactly once between them.
type presentedPrefix struct {
	presented map[event.Address]map[event.EventID]bool
}

// SessionPresented reports the facts of the named session whose text the request
// prefix of that session presents, keyed by fact identity.
func (p presentedPrefix) SessionPresented(addr event.Address) map[event.EventID]bool {
	return p.presented[addr]
}

var _ agent.PresentedFace = presentedPrefix{}

// ExamplePresentedFace reads the presentation of one session chain back: the input
// the prefix carries in full is reported, so a resident consumption loop leaves it
// to the prefix instead of carrying it a second time, while the input the fold
// replaced with a summary is not reported and therefore stays the material of the
// segment that reads it.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func ExamplePresentedFace() {
	addr := event.Address{AgentID: "assistant", SessionID: "session-1"}
	face := presentedPrefix{presented: map[event.Address]map[event.EventID]bool{
		addr: {"fact-carried-in-full": true},
	}}
	shown := face.SessionPresented(addr)
	behind := 0
	for _, key := range []event.EventID{"fact-carried-in-full", "fact-folded-into-a-summary"} {
		if !shown[key] {
			behind++
		}
	}
	fmt.Printf("presented=%d behind=%d\n", len(shown), behind)
}

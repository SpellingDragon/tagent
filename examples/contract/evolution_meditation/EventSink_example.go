package evolutionmeditationexample

import (
	"context"
	"fmt"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/evolution/meditation"
)

// chainWrite is the canonical cross-session write face: it files one event into the
// chain of the address it names and answers the identity the commit recorded.
type chainWrite struct {
	committed map[event.Address][]event.EventID
}

// CommitToSession commits evt into the chain of addr and answers the fresh identity.
func (w *chainWrite) CommitToSession(_ context.Context, addr event.Address, _ event.Event) (event.EventID, error) {
	id := event.EventID(fmt.Sprintf("%s#delivery-%d", addr.SessionID, len(w.committed[addr])+1))
	w.committed[addr] = append(w.committed[addr], id)
	return id, nil
}

var _ meditation.EventSink = (*chainWrite)(nil)

// ExampleEventSink commits one summary into the target session's chain and reads back
// the identity the commit recorded.
//
// 契约: docs/wiki/evolution/evolution-architecture.md#evidence-window
func ExampleEventSink() {
	ctx := context.Background()
	addr := event.Address{AgentID: "user-agent", SessionID: "session-user"}
	sink := &chainWrite{committed: map[event.Address][]event.EventID{}}

	id, err := sink.CommitToSession(ctx, addr, event.Event{Type: event.TypeExternalInput})
	if err != nil || id == "" {
		return
	}
	if len(sink.committed[addr]) != 1 {
		return
	}
}

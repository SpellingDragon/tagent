// Package assemblyexample shows how a host satisfies the MessageSender port.
//
// 契约: docs/wiki/agent/agent-architecture.md#core-components
package assemblyexample

import (
	"context"
	"fmt"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/assembly"
)

type demoSender struct{ last event.Address }

func (s *demoSender) Send(_ context.Context, target event.Address, content string) error {
	s.last = target
	fmt.Printf("sent to %s: %s\n", target, content)
	return nil
}

// ExampleMessageSender demonstrates the port a host fills: one address, one
// message, one error — no batching, no retry, no channel semantics.
func ExampleMessageSender() {
	var sender assembly.MessageSender = &demoSender{}
	_ = sender.Send(context.Background(), event.Address{AgentID: "agent-a", SessionID: "chat-a"}, "hello")
}

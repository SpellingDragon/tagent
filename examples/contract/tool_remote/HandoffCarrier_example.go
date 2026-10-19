package remoteexample

import (
	"errors"

	"github.com/SpellingDragon/tagent/tool/remote"

	a2aprotocol "trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// errHandoffUnnamed is what the canonical handoff reports when it names no task.
var errHandoffUnnamed = errors.New("remote: the handoff names no intent")

// statedHandoff is the canonical handoff: it states the acceptance a delegated turn
// is held to and the one data part a message carries it as.
type statedHandoff struct {
	intent string
}

// Validate reports whether the handoff names what a delegated turn could serve.
func (h statedHandoff) Validate() error {
	if h.intent == "" {
		return errHandoffUnnamed
	}
	return nil
}

// DataPart returns the part one message carries the handoff as.
func (h statedHandoff) DataPart() a2aprotocol.DataPart {
	return a2aprotocol.NewDataPart(map[string]any{"intent": h.intent})
}

var _ remote.HandoffCarrier = statedHandoff{}

// ExampleHandoffCarrier states one handoff in the form a remote call carries it: the
// handoff holds itself to its own acceptance and answers with the one data part its
// message travels with, so a send can put the handoff beside the request it carries.
//
// 契约: docs/wiki/tool/tool-architecture.md#delegation-retry
func ExampleHandoffCarrier() {
	handoff := statedHandoff{intent: "review the change"}
	if err := handoff.Validate(); err != nil {
		return
	}
	part := handoff.DataPart()
	if part.Kind != a2aprotocol.KindData || part.Data == nil {
		return
	}
	unnamed := statedHandoff{}
	if err := unnamed.Validate(); err == nil {
		return
	}
}

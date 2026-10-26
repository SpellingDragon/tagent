package pluginexample

import (
	"context"
	"errors"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/api/plugin"
)

// refusingTransform is the transform that answers an error: the pass is refused at that
// face, so the call is neither executed nor recorded.
type refusingTransform struct{}

// Name reports the stable name of the element.
func (refusingTransform) Name() string { return "refusing-transform" }

// Port reports the call->call port the element serves.
func (refusingTransform) Port() plugin.Port {
	return plugin.Port{Reads: plugin.MaterialCall, Produces: plugin.ProductCall}
}

// TransformCall refuses the call it was handed by answering an error.
func (refusingTransform) TransformCall(_ context.Context, call *trpcmodel.ToolCall) error {
	return errors.New("refusing-transform: this argument shape is not allowed")
}

// identityMover is the transform that moves the call identity: the pass refuses it by
// name with ErrCallIdentityChanged.
type identityMover struct{}

// Name reports the stable name of the element.
func (identityMover) Name() string { return "identity-mover" }

// Port reports the call->call port the element serves.
func (identityMover) Port() plugin.Port {
	return plugin.Port{Reads: plugin.MaterialCall, Produces: plugin.ProductCall}
}

// TransformCall rewrites the CallID, which a transform is never allowed to move.
func (identityMover) TransformCall(_ context.Context, call *trpcmodel.ToolCall) error {
	call.ID = "call-rewritten"
	return nil
}

var (
	_ plugin.Instance      = refusingTransform{}
	_ plugin.TransformCall = refusingTransform{}
	_ plugin.Instance      = identityMover{}
	_ plugin.TransformCall = identityMover{}
)

// ExampleTransformCall_refusal drives both named refusals of the transform face through
// the Before pass: a transform error refuses the whole pass and answers no CallPass, so
// the call neither runs nor is recorded, and a moved CallID is refused as
// ErrCallIdentityChanged while the call the model issued stays as it was issued.
//
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func ExampleTransformCall_refusal() {
	call := trpcmodel.ToolCall{ID: "call-1", Function: trpcmodel.FunctionDefinitionParam{Name: "write"}}

	refusing, err := plugin.NewFlow([]plugin.Instance{refusingTransform{}})
	if err != nil {
		return
	}
	pass, err := refusing.Before(context.Background(), call)
	if err == nil || pass.Final.ID != "" {
		return
	}

	moving, err := plugin.NewFlow([]plugin.Instance{identityMover{}})
	if err != nil {
		return
	}
	if _, err := moving.Before(context.Background(), call); !errors.Is(err, plugin.ErrCallIdentityChanged) {
		return
	}
	if call.ID != "call-1" {
		return
	}
}

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

// Phase reports the one phase the element serves.
func (refusingTransform) Phase() plugin.Phase { return plugin.PhaseCallTransform }

// TransformCall refuses the call it was handed by answering an error.
func (refusingTransform) TransformCall(_ context.Context, call *trpcmodel.ToolCall) error {
	return errors.New("refusing-transform: this argument shape is not allowed")
}

// identityMover is the transform that moves the call identity: the pass refuses it by
// name with ErrCallIdentityChanged.
type identityMover struct{}

// Name reports the stable name of the element.
func (identityMover) Name() string { return "identity-mover" }

// Phase reports the one phase the element serves.
func (identityMover) Phase() plugin.Phase { return plugin.PhaseCallTransform }

// TransformCall rewrites the CallID, which a transform is never allowed to move.
func (identityMover) TransformCall(_ context.Context, call *trpcmodel.ToolCall) error {
	call.ID = "call-rewritten"
	return nil
}

var (
	_ plugin.Plugin        = refusingTransform{}
	_ plugin.TransformCall = refusingTransform{}
	_ plugin.Plugin        = identityMover{}
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

	refusing, err := plugin.NewPipeline([]plugin.Plugin{refusingTransform{}})
	if err != nil {
		return
	}
	pass, err := refusing.Before(context.Background(), call)
	if err == nil || pass.Final.ID != "" {
		return
	}

	moving, err := plugin.NewPipeline([]plugin.Plugin{identityMover{}})
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

package pluginexample

import (
	"context"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/plugin"
)

// argumentDefaults is the canonical transform: it corrects the parameters of one call
// and leaves the call identity as resolved.
type argumentDefaults struct{}

// Name reports the stable name of the element.
func (argumentDefaults) Name() string { return "argument-defaults" }

// Phase reports the one phase the element serves.
func (argumentDefaults) Phase() plugin.Phase { return plugin.PhaseCallTransform }

// TransformCall fills in the argument a call left out, keeping the ID the call was
// resolved under.
func (argumentDefaults) TransformCall(_ context.Context, call *trpcmodel.ToolCall) error {
	if len(call.Function.Arguments) == 0 {
		call.Function.Arguments = []byte(`{"limit":10}`)
	}
	return nil
}

var (
	_ plugin.Plugin        = argumentDefaults{}
	_ plugin.TransformCall = argumentDefaults{}
)

// ExampleTransformCall drives one transform through the Before pass: every transform
// corrects its own copy, and the final call the pass reports is the one the core
// re-checks, executes and records.
//
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func ExampleTransformCall() {
	pipeline, err := plugin.NewPipeline([]plugin.Plugin{argumentDefaults{}})
	if err != nil {
		return
	}
	pass, err := pipeline.Before(context.Background(), trpcmodel.ToolCall{
		ID:       "call-1",
		Function: trpcmodel.FunctionDefinitionParam{Name: "search"},
	})
	if err != nil {
		return
	}
	_ = pass.Final.Function.Arguments
}

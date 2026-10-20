package pluginexample

import (
	"context"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/api/plugin"
)

// registrationOnly is the canonical unit that declares a port without implementing that
// port's narrow face: the flow refuses it by name.
type registrationOnly struct{}

// Name reports the stable name of the element.
func (registrationOnly) Name() string { return "registration-only" }

// Port reports the call->call port the element declares.
func (registrationOnly) Port() plugin.Port {
	return plugin.Port{Reads: plugin.MaterialCall, Produces: plugin.ProductCall}
}

// TransformFace is the canonical instance: one name, the one port it serves and the one
// narrow face of that port.
type TransformFace struct{}

// Name reports the stable name of the element.
func (TransformFace) Name() string { return "transform-face" }

// Port reports the call->call port the element serves.
func (TransformFace) Port() plugin.Port {
	return plugin.Port{Reads: plugin.MaterialCall, Produces: plugin.ProductCall}
}

// TransformCall corrects one call before its side effect; the corrected call is the one
// the core re-checks, executes and records.
func (TransformFace) TransformCall(context.Context, *trpcmodel.ToolCall) error { return nil }

var (
	_ plugin.Instance      = registrationOnly{}
	_ plugin.Instance      = TransformFace{}
	_ plugin.TransformCall = TransformFace{}
)

// ExampleInstance shows both outcomes of one instance: an instance that declares a port
// without the narrow face of that port is refused by name, while an instance that carries
// the face is dispatched into that port's set and runs in the one Before pass.
//
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func ExampleInstance() {
	if _, err := plugin.NewFlow([]plugin.Instance{registrationOnly{}}); err == nil {
		return
	}
	pipeline, err := plugin.NewFlow([]plugin.Instance{TransformFace{}})
	if err != nil {
		return
	}
	pass, err := pipeline.Before(context.Background(), trpcmodel.ToolCall{ID: "call-1"})
	if err != nil {
		return
	}
	_ = pass.Final.ID
}

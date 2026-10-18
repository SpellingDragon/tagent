package pluginexample

import (
	"context"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/plugin"
)

// registrationOnly is the canonical unit that reports a phase without implementing that
// phase's narrow face: the pipeline refuses it by name.
type registrationOnly struct{}

// Name reports the stable name of the element.
func (registrationOnly) Name() string { return "registration-only" }

// Phase reports the one phase the element serves.
func (registrationOnly) Phase() plugin.Phase { return plugin.PhaseCallTransform }

// TransformFace is the canonical registration unit: one name, the one phase it serves and
// the one narrow face of that phase.
type TransformFace struct{}

// Name reports the stable name of the element.
func (TransformFace) Name() string { return "transform-face" }

// Phase reports the one phase the element serves.
func (TransformFace) Phase() plugin.Phase { return plugin.PhaseCallTransform }

// TransformCall corrects one call before its side effect; the corrected call is the one
// the core re-checks, executes and records.
func (TransformFace) TransformCall(context.Context, *trpcmodel.ToolCall) error { return nil }

var (
	_ plugin.Plugin        = registrationOnly{}
	_ plugin.Plugin        = TransformFace{}
	_ plugin.TransformCall = TransformFace{}
)

// ExamplePlugin shows both outcomes of one registration unit: a plugin that reports a
// phase without the narrow face of that phase is refused by name, while a plugin that
// carries the face is dispatched into that phase's set and runs in the one Before pass.
//
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func ExamplePlugin() {
	if _, err := plugin.NewPipeline([]plugin.Plugin{registrationOnly{}}); err == nil {
		return
	}
	pipeline, err := plugin.NewPipeline([]plugin.Plugin{TransformFace{}})
	if err != nil {
		return
	}
	pass, err := pipeline.Before(context.Background(), trpcmodel.ToolCall{ID: "call-1"})
	if err != nil {
		return
	}
	_ = pass.Final.ID
}

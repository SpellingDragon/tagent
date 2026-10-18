package pluginexample

import (
	"context"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/plugin"
)

// argumentSize is the canonical risk face: it grades one call from the parameters and
// identity already resolved for it.
type argumentSize struct{}

// Name reports the stable name of the element.
func (argumentSize) Name() string { return "argument-size" }

// Phase reports the one phase the element serves.
func (argumentSize) Phase() plugin.Phase { return plugin.PhaseRiskPolicy }

// AssessRisk grades one call before its side effect and answers one declared grade.
func (argumentSize) AssessRisk(_ context.Context, call *trpcmodel.ToolCall) plugin.RiskLevel {
	if len(call.Function.Arguments) > 4096 {
		return plugin.RiskHigh
	}
	return plugin.RiskLow
}

var (
	_ plugin.Plugin       = argumentSize{}
	_ plugin.RiskAssessor = argumentSize{}
)

// ExampleRiskAssessor grades one call through the Before pass: the pass reports the
// strongest grade its risk faces rendered, and the pre-execution ruling reads that grade
// instead of running a second risk face.
//
// 契约: docs/wiki/agent/governance-enforcement.md#disposition-and-risk
func ExampleRiskAssessor() {
	pipeline, err := plugin.NewPipeline([]plugin.Plugin{argumentSize{}})
	if err != nil {
		return
	}
	pass, err := pipeline.Before(context.Background(), trpcmodel.ToolCall{
		ID:       "call-1",
		Function: trpcmodel.FunctionDefinitionParam{Name: "write", Arguments: []byte(`{"path":"a"}`)},
	})
	if err != nil {
		return
	}
	_ = pass.Risk
}

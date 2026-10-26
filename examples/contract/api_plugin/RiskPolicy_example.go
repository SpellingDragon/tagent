package pluginexample

import (
	"context"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/evolution/governance"
)

// sizeAndRuling is the element that serves both faces of the risk phase: it grades one
// call and renders the one pre-execution ruling over that same grade.
type sizeAndRuling struct{}

// Name reports the stable name of the element.
func (sizeAndRuling) Name() string { return "size-and-ruling" }

// Port reports the call->ruling port the element serves; the two faces of that port are
// the ones this element implements.
func (sizeAndRuling) Port() plugin.Port {
	return plugin.Port{Reads: plugin.MaterialCall, Produces: plugin.ProductRuling}
}

// AssessRisk renders the grade of one call before its side effect.
func (sizeAndRuling) AssessRisk(_ context.Context, call *trpcmodel.ToolCall) plugin.RiskLevel {
	if call.Function.Name == "rm" {
		return plugin.RiskCritical
	}
	return plugin.RiskMedium
}

// Judge renders the one ruling over the grade the Before pass carries; it never renders
// a second grade of its own.
func (sizeAndRuling) Judge(pass plugin.CallPass) (plugin.Decision, error) {
	return plugin.Decision{Disposition: plugin.DispositionRecord, Risk: pass.Risk}, nil
}

var (
	_ plugin.Instance     = sizeAndRuling{}
	_ plugin.RiskAssessor = sizeAndRuling{}
	_ plugin.Governor     = sizeAndRuling{}
)

// ExamplePhase_riskPolicy drives the two faces of the risk phase beside each other: one
// element grades a call and renders the ruling over that grade, and a pass that carries
// no grade is ruled as an ordinary mutating call instead of passing ungraded.
//
// 契约: docs/wiki/agent/governance-enforcement.md#disposition-and-risk
func ExamplePhase_riskPolicy() {
	element := sizeAndRuling{}
	pipeline, err := plugin.NewFlow([]plugin.Instance{element})
	if err != nil {
		return
	}
	pass, err := pipeline.Before(context.Background(), trpcmodel.ToolCall{
		ID:       "call-1",
		Function: trpcmodel.FunctionDefinitionParam{Name: "write", Arguments: []byte(`{"path":"a"}`)},
	})
	if err != nil || pass.Risk != plugin.RiskMedium {
		return
	}
	graded, err := element.Judge(pass)
	if err != nil || graded.Risk != pass.Risk {
		return
	}
	gate := governance.Open()
	ungraded, err := gate.Judge(plugin.CallPass{
		Final: trpcmodel.ToolCall{ID: "call-2", Function: trpcmodel.FunctionDefinitionParam{Name: "write"}},
	})
	if err != nil || ungraded.Risk != plugin.RiskMedium {
		return
	}
	_ = graded.Disposition
	_ = ungraded.Disposition
}

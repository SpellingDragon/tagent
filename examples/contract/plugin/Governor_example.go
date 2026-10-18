package pluginexample

import (
	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/plugin"
)

// allowReads is the canonical ruling face: it renders the one ruling a call receives
// before its side effect.
type allowReads struct{}

// Name reports the stable name of the element.
func (allowReads) Name() string { return "allow-reads" }

// Phase reports the one phase the element serves.
func (allowReads) Phase() plugin.Phase { return plugin.PhaseRiskPolicy }

// Judge renders the ruling of one call over the one Before pass that call carries:
// a read is allowed, every other call is recorded and runs. What triggered the call
// travels inside the pass, so the ruling reads no hidden input of its own.
func (allowReads) Judge(pass plugin.CallPass) (plugin.Decision, error) {
	if pass.Final.Function.Name == "read" {
		return plugin.Decision{Disposition: plugin.DispositionAllow, Risk: plugin.RiskLow}, nil
	}
	return plugin.Decision{
		Disposition: plugin.DispositionRecord,
		Risk:        plugin.RiskMedium,
		Rule:        "write-audit",
	}, nil
}

var (
	_ plugin.Plugin   = allowReads{}
	_ plugin.Governor = allowReads{}
)

// ExampleGovernor renders the ruling of two calls: the allow ruling runs with no trace
// duty, and the recorded ruling runs beside its audit record. A hold rendered here stays
// a wait, never a denial.
//
// 契约: docs/wiki/agent/governance-enforcement.md#disposition-and-risk
func ExampleGovernor() {
	reads := trpcmodel.ToolCall{ID: "call-1", Function: trpcmodel.FunctionDefinitionParam{Name: "read"}}
	writes := trpcmodel.ToolCall{ID: "call-2", Function: trpcmodel.FunctionDefinitionParam{Name: "write"}}
	face := allowReads{}
	allowed, err := face.Judge(plugin.CallPass{Final: reads, Risk: plugin.RiskLow})
	if err != nil {
		return
	}
	recorded, err := face.Judge(plugin.CallPass{Final: writes, Risk: plugin.RiskMedium, TriggerSource: "model"})
	if err != nil {
		return
	}
	_ = allowed.Disposition
	_ = recorded.Rule
}

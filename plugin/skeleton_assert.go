package plugin

import (
	"context"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/event"
)

// 契约: docs/wiki/plugin/plugin-architecture.md#overview
//
// Skeleton compile-time assertions for the phase 1 pipeline and governance
// declarations.
//
// The phase 2 types that will satisfy these interfaces do not exist yet, so there is
// no implementation assertion to write. The assertions below pin what the skeleton
// proves on its own: every declared method keeps its exact shape, every declared name
// is a constant of its declared type, and every documented field of a governance
// declaration keeps its name and type.
var (
	_ func(Plugin) string = Plugin.Name

	_ func(Plugin) Phase = Plugin.Phase

	_ func(Enricher, context.Context, *trpcmodel.Request) error = Enricher.EnrichRequest

	_ func(RiskAssessor, context.Context, *trpcmodel.ToolCall) RiskLevel = RiskAssessor.AssessRisk

	_ func(ViewSelector, context.Context, event.EventID) ([]string, error) = ViewSelector.SelectView

	_ func(ExternalizePolicy, context.Context, event.EventID, int) (bool, error) = ExternalizePolicy.DecideExternalize

	_ func(PostCommitObserver, context.Context, event.EventID) error = PostCommitObserver.ObserveCommit

	_ func(FinalPresenter, context.Context, event.EventID) (string, bool) = FinalPresenter.Present

	_ func(Governor, context.Context, *trpcmodel.ToolCall) (Decision, error) = Governor.Judge
)

// Every declared name is an interface type.
var (
	_ Plugin             = (Plugin)(nil)
	_ Enricher           = (Enricher)(nil)
	_ RiskAssessor       = (RiskAssessor)(nil)
	_ ViewSelector       = (ViewSelector)(nil)
	_ ExternalizePolicy  = (ExternalizePolicy)(nil)
	_ PostCommitObserver = (PostCommitObserver)(nil)
	_ FinalPresenter     = (FinalPresenter)(nil)
	_ Governor           = (Governor)(nil)
)

// The governance declarations keep every documented field.
var (
	_ = Decision{
		Disposition: "",
		Risk:        "",
		Rule:        "",
		Reason:      "",
		Denial:      nil,
	}

	_ = Denial{
		CallID: "",
		Tool:   "",
		Risk:   "",
		Rule:   "",
		Result: trpcmodel.Message{},
	}
)

// Phase values, dispositions and risk grades must stay compile-time constants
// of their declared types: a variable or a bare string stops this block from
// compiling.
const (
	_ Phase = PhaseRequestEnrich
	_ Phase = PhaseRiskPolicy
	_ Phase = PhaseViewSelection
	_ Phase = PhaseExternalizePolicy
	_ Phase = PhasePostCommitObserve
	_ Phase = PhaseFinalPresentation

	_ Disposition = DispositionAllow
	_ Disposition = DispositionRecord
	_ Disposition = DispositionHold

	_ RiskLevel = RiskLow
	_ RiskLevel = RiskMedium
	_ RiskLevel = RiskHigh
	_ RiskLevel = RiskCritical
)

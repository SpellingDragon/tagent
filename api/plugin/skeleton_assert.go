package plugin

import (
	"context"
	"time"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/view"
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

	_ func(TransformCall, context.Context, *trpcmodel.ToolCall) error = TransformCall.TransformCall

	_ func(RiskAssessor, context.Context, *trpcmodel.ToolCall) RiskLevel = RiskAssessor.AssessRisk

	_ func(PostCommitObserver, context.Context, event.EventID) error = PostCommitObserver.ObserveCommit

	_ func(FinalPresenter, context.Context, event.EventID) (string, bool) = FinalPresenter.Present

	_ func(EnvironmentInject, context.Context, EnvironmentRequest) (string, error) = EnvironmentInject.InjectEnvironment

	_ func(CompressTrigger, context.Context, event.CompressRequest) error = CompressTrigger.TriggerCompress

	_ func(Governor, CallPass) (Decision, error) = Governor.Judge

	_ func(ObservationSource) []string = ObservationSource.ObservedNamespaces

	_ func(ObservationSource, context.Context, time.Time) (bool, error) = ObservationSource.FreshSince

	_ func(ObservationSink, context.Context, event.Event) error = ObservationSink.Observe

	_ func(Observer, context.Context, event.Event) error = Observer.Observe

	_ func(FactLookup, context.Context, event.EventID) (event.Event, error) = FactLookup.CommittedFact

	_ func(AuditSink, context.Context, AuditRecord) error = AuditSink.Audit

	_ func(RiskLevel) int = RiskLevel.Rank

	_ func(Decision) bool = Decision.Refused
)

// Every declared name is an interface type.
var (
	_ Plugin             = (Plugin)(nil)
	_ TransformCall      = (TransformCall)(nil)
	_ RiskAssessor       = (RiskAssessor)(nil)
	_ PostCommitObserver = (PostCommitObserver)(nil)
	_ FinalPresenter     = (FinalPresenter)(nil)
	_ EnvironmentInject  = (EnvironmentInject)(nil)
	_ CompressTrigger    = (CompressTrigger)(nil)
	_ Governor           = (Governor)(nil)
	_ ObservationSource  = (ObservationSource)(nil)
	_ ObservationSink    = (ObservationSink)(nil)
	_ Observer           = (Observer)(nil)
	_ FactLookup         = (FactLookup)(nil)
	_ AuditSink          = (AuditSink)(nil)
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
	_ Phase = PhaseCallTransform
	_ Phase = PhaseRiskPolicy
	_ Phase = PhasePostCommitObserve
	_ Phase = PhaseFinalPresentation
	_ Phase = PhaseCompressTrigger
	_ Phase = PhaseEnvironmentInject

	_ Disposition = DispositionAllow
	_ Disposition = DispositionRecord
	_ Disposition = DispositionHold

	_ RiskLevel = RiskLow
	_ RiskLevel = RiskMedium
	_ RiskLevel = RiskHigh
	_ RiskLevel = RiskCritical

	_ Layer = LayerTrace
	_ Layer = LayerMetric
)

// The 02.6 pipeline pass declarations keep their exact shapes: the composition
// entry, the one Before pass of a call and the After pass of a committed fact.
var (
	_ func([]Plugin) (Pipeline, error) = NewPipeline

	_ func(Pipeline, context.Context, trpcmodel.ToolCall) (CallPass, error) = Pipeline.Before

	_ func(Pipeline, context.Context, event.EventID) view.Presentation = Pipeline.After

	_ func(Pipeline, context.Context, event.CompressRequest) error = Pipeline.TriggerCompress

	_ func(Pipeline, context.Context, EnvironmentRequest) (string, error) = Pipeline.InjectEnvironment

	_ error = ErrCallIdentityChanged
)

// The pass outcome declarations keep every documented field.
var (
	_ = CallPass{
		Final:         trpcmodel.ToolCall{},
		Risk:          "",
		TriggerSource: "",
	}

	_ = AuditRecord{
		Fact:     "",
		Attempts: []Attempt{{Layer: LayerTrace, Skipped: false}},
	}

	_ = EnvironmentRequest{Agent: ""}

	_ = view.Presentation{
		Fact:  "",
		Views: nil,
		Gaps:  nil,
	}

	_ = view.ObservationGap{
		Plugin: "",
		Fact:   "",
		Err:    nil,
	}

	_ = view.RenderedView{
		Plugin:  "",
		Text:    "",
		Dropped: false,
	}
)

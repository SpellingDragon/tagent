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
// Skeleton compile-time assertions for the material flow and the governance
// declarations.
//
// The assertions below pin what the package proves on its own: every declared
// method keeps its exact shape, every declared name is a constant of its
// declared type, and every documented field of a governance declaration keeps
// its name and type.
var (
	_ func(Instance) string = Instance.Name

	_ func(Instance) Port = Instance.Port

	_ func(TransformCall, context.Context, *trpcmodel.ToolCall) error = TransformCall.TransformCall

	_ func(RiskAssessor, context.Context, *trpcmodel.ToolCall) RiskLevel = RiskAssessor.AssessRisk

	_ func(PostCommitObserver, context.Context, event.EventID) error = PostCommitObserver.ObserveCommit

	_ func(FinalPresenter, context.Context, event.EventID) (string, bool) = FinalPresenter.Present

	_ func(EnvironmentInject, context.Context, EnvironmentRequest) ([]string, error) = EnvironmentInject.InjectEnvironment

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

	_ func(Port) error = validatePort

	_ func(Port) string = Port.String

	_ func([]Instance) (Pipeline, error) = NewFlow
)

// Every declared name is an interface type.
var (
	_ Instance           = (Instance)(nil)
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

// Ports, dispositions and risk grades must stay compile-time constants of their
// declared types: a variable or a bare string stops this block from compiling.
const (
	_ Port = PortCallTransform
	_ Port = PortCallRuling
	_ Port = PortRequestEnvironment
	_ Port = PortFactObservation
	_ Port = PortFactView
	_ Port = PortFactCompression

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

// The pipeline pass declarations keep their exact shapes: the composition
// entries, the one Before pass of a call and the After pass of a committed fact.
var (
	_ func(Pipeline, context.Context, trpcmodel.ToolCall) (CallPass, error) = Pipeline.Before

	_ func(Pipeline, context.Context, event.EventID) view.Presentation = Pipeline.After

	_ func(Pipeline, context.Context, event.CompressRequest) error = Pipeline.TriggerCompress

	_ func(Pipeline, context.Context, EnvironmentRequest) ([]string, error) = Pipeline.InjectEnvironment

	_ error = ErrCallIdentityChanged

	_ error = ErrObservationObligationRetained
)

// The flow declarations keep every documented field.
var (
	_ = CallPass{
		Final:         trpcmodel.ToolCall{},
		Risk:          "",
		TriggerSource: "",
	}

	// The pass of one call travels on the context beside the call itself, so a
	// tool that never sees the pipeline still reads the verdict it ran under.
	_ func(context.Context, CallPass) context.Context = WithCallPass

	_ func(context.Context) (CallPass, bool) = CallPassFrom

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

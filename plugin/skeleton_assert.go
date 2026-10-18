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

	_ func(TransformCall, context.Context, *trpcmodel.ToolCall) error = TransformCall.TransformCall

	_ func(RiskAssessor, context.Context, *trpcmodel.ToolCall) RiskLevel = RiskAssessor.AssessRisk

	_ func(PostCommitObserver, context.Context, event.EventID) error = PostCommitObserver.ObserveCommit

	_ func(FinalPresenter, context.Context, event.EventID) (string, bool) = FinalPresenter.Present

	_ func(Governor, CallPass) (Decision, error) = Governor.Judge

	_ func(ObservationSink, context.Context, event.Event) error = ObservationSink.Observe

	_ func(FactLookup, context.Context, event.EventID) (event.Event, error) = FactLookup.CommittedFact
)

// Every declared name is an interface type.
var (
	_ Plugin             = (Plugin)(nil)
	_ TransformCall      = (TransformCall)(nil)
	_ RiskAssessor       = (RiskAssessor)(nil)
	_ PostCommitObserver = (PostCommitObserver)(nil)
	_ FinalPresenter     = (FinalPresenter)(nil)
	_ Governor           = (Governor)(nil)
	_ ObservationSink    = (ObservationSink)(nil)
	_ FactLookup         = (FactLookup)(nil)
)

// The post-commit wiring keeps satisfying the phase face it serves, so a
// revision that drops the wiring from the After pass stops this block from
// compiling.
var _ PostCommitObserver = (*CommitObserver)(nil)

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

	_ Disposition = DispositionAllow
	_ Disposition = DispositionRecord
	_ Disposition = DispositionHold

	_ RiskLevel = RiskLow
	_ RiskLevel = RiskMedium
	_ RiskLevel = RiskHigh
	_ RiskLevel = RiskCritical
)

// The 02.6 pipeline pass declarations keep their exact shapes: the composition
// entry, the one Before pass of a call and the After pass of a committed fact.
var (
	_ func([]Plugin) (Pipeline, error) = NewPipeline

	_ func(Pipeline, context.Context, trpcmodel.ToolCall) (CallPass, error) = Pipeline.Before

	_ func(Pipeline, context.Context, event.EventID) Presentation = Pipeline.After

	_ error = ErrCallIdentityChanged

	_ error = ErrApprovalNotFound

	_ error = ErrApprovalExpired

	_ error = ErrApprovalRulingUnknown

	_ error = ErrApprovalIdentityMissing
)

// The 04.5 enforcement and approval declarations keep their exact shapes: the
// policy entry points, the plain grading input, the rule matcher and the digest
// binding.
var (
	_ func(*Policy, CallPass) (Decision, error) = (*Policy).Judge

	_ func(*Policy) bool = (*Policy).Active

	_ func(*Policy) Enforcement = (*Policy).Mode

	_ func(Decision) bool = Decision.Refused

	_ func([]byte) string = ArgsDigest

	_ func(context.Context, CallPass) context.Context = WithCallPass

	_ func(context.Context) (CallPass, bool) = CallPassFrom

	_ func(*trpcmodel.ToolCall, RiskLevel, string, string) *Denial = denialOf
)

// Every declared name of the 04.5 faces is a declared type, and the policy is
// the concrete governor of the pipeline's risk-policy phase.
var (
	_ Governor        = (*Policy)(nil)
	_ ApprovalChannel = (ApprovalChannel)(nil)
)

// The 04.5 declarations keep the zero-selection gate: Open with every option
// named, and the three blocks one approval request is read as.
var (
	_ func(...PolicyOption) *Policy = Open

	_ func() PolicyOption = WithStrict

	_ func(...RiskRule) PolicyOption = WithRules

	_ func(RiskLevel) PolicyOption = WithDefaultRisk

	_ func(*ApprovalManager) PolicyOption = WithApprovals

	_ func(*GoalRegistry) PolicyOption = WithGoals

	_ func(...string) PolicyOption = WithGoalRequired

	_ func(*BudgetManager) PolicyOption = WithBudget

	_ func(*DenialLedger) PolicyOption = WithLedger

	_ = Open(
		WithStrict(),
		WithRules(RiskRule{}),
		WithDefaultRisk(RiskLow),
		WithApprovals(nil),
		WithGoals(nil),
		WithGoalRequired("model"),
		WithBudget(nil),
		WithLedger(nil),
	)

	_ = RiskRule{
		ID:      "",
		MinRisk: "",
		Match:   nil,
		Reason:  "",
	}

	_ = RiskContext{
		ToolName:      "",
		ArgsJSON:      nil,
		TriggerSource: "",
	}

	_ = ApprovalSubject{
		ID:         "",
		ToolName:   "",
		ArgsDigest: "",
	}

	_ = ApprovalEvidence{
		ArgsPreview: "",
		RiskLevel:   "",
		RuleID:      "",
		Reason:      "",
	}

	_ = ApprovalState{
		CreatedMs: 0,
		ExpiresMs: 0,
		Status:    "",
		DecidedBy: "",
	}

	_ = ApprovalRequest{
		ApprovalSubject:  ApprovalSubject{},
		ApprovalEvidence: ApprovalEvidence{},
		ApprovalState:    ApprovalState{},
	}

	_ = DeliveryGap{
		Request: "",
		Err:     nil,
	}
)

// Enforcement grades and approval states must stay compile-time constants of
// their declared types: a variable or a bare string stops this block from
// compiling. The approval state words are the ones the channel contract fixes.
const (
	_ Enforcement = EnforcementWarn
	_ Enforcement = EnforcementStrict

	_ ApprovalStatus = ApprovalPending
	_ ApprovalStatus = ApprovalApproved
	_ ApprovalStatus = ApprovalDenied
	_ ApprovalStatus = ApprovalExpired
)

// The pass outcome declarations keep every documented field.
var (
	_ = CallPass{
		Final:         trpcmodel.ToolCall{},
		Risk:          "",
		TriggerSource: "",
	}

	_ = Presentation{
		Fact:  "",
		Views: nil,
		Gaps:  nil,
	}

	_ = ObservationGap{
		Plugin: "",
		Fact:   "",
		Err:    nil,
	}

	_ = RenderedView{
		Plugin:  "",
		Text:    "",
		Dropped: false,
	}
)

// The 04.6 governance recovery declarations keep their exact shapes: the goal
// registry entry points, the budget gate, the denial ledger and the durable
// governance event face the two bind to for recovery.
var (
	_ func() *GoalRegistry = NewGoalRegistry

	_ func(*GoalRegistry, GovernanceEventLog) = (*GoalRegistry).BindStore

	_ func(*GoalRegistry, string, string, int64) string = (*GoalRegistry).Declare

	_ func(*GoalRegistry, string, GoalStatus) bool = (*GoalRegistry).Resolve

	_ func(*GoalRegistry) []*Goal = (*GoalRegistry).List

	_ func(*GoalRegistry) bool = (*GoalRegistry).HasActive

	_ func(*GoalRegistry) []*Goal = (*GoalRegistry).Active

	_ func(*GoalRegistry) *Goal = (*GoalRegistry).Current

	_ func(BudgetConfig, string, ...BudgetOption) *BudgetManager = NewBudgetManager

	_ func(*BudgetManager, RiskLevel) error = (*BudgetManager).Admit

	_ func(*BudgetManager, RiskLevel) int64 = (*BudgetManager).Usage

	_ func() *DenialLedger = NewDenialLedger

	_ func(*DenialLedger, GovernanceEventLog) = (*DenialLedger).BindStore

	_ func(*DenialLedger, DenialRecord) = (*DenialLedger).Record

	_ func(*DenialLedger, int) []DenialRecord = (*DenialLedger).Query

	_ func(*DenialLedger) int = (*DenialLedger).Count

	_ error = ErrBudgetExhausted
)

// The governance event log stays a declared interface whose method signatures
// carry built-in types only: the structural match the memory tier's fact log
// satisfies without a package edge in either direction.
var _ GovernanceEventLog = (GovernanceEventLog)(nil)

// The 04.6 declarations keep every documented field.
var (
	_ = Goal{
		ID:        "",
		Statement: "",
		CreatedBy: "",
		Status:    "",
		CreatedMs: 0,
		ExpiresMs: 0,
	}

	_ = BudgetConfig{
		Window:        0,
		BucketCount:   0,
		MaxHighRisk:   0,
		MaxMediumRisk: 0,
	}

	_ = DenialRecord{
		Subtype:    "",
		ToolName:   "",
		Level:      "",
		RuleID:     "",
		Reason:     "",
		ArgsDigest: "",
		GoalID:     "",
		AgentName:  "",
		Timestamp:  0,
	}
)

// The goal state words must stay compile-time constants of their declared
// type: a variable or a bare string stops this block from compiling. The four
// words are kept verbatim — active, achieved, abandoned, expired — as the
// governance semantics fix them.
const (
	_ GoalStatus = GoalActive
	_ GoalStatus = GoalAchieved
	_ GoalStatus = GoalAbandoned
	_ GoalStatus = GoalExpired
)

// The governance event vocabulary must stay compile-time constants: the goal
// operations speak the event words the goal contract fixes, and a refusal, a
// privileged execution and a held call keep the words the governance events
// have always spoken.
const (
	_ string = SubtypeGoalDeclared
	_ string = SubtypeGoalResolved
	_ string = SubtypeDenial
	_ string = SubtypeApproval
	_ string = SubtypeAudit
	_ string = MetaKeySubtype
	_ string = goalOpDeclared
	_ string = goalOpResolved
)

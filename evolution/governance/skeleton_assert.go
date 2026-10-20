package governance

import (
	"context"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
)

// 契约: docs/wiki/agent/governance-enforcement.md#enforcement-modes
//
// This file holds compile-time assertions only: the policy is one instance of
// the pipeline's call->ruling port — the concrete ruling face beside the port it
// declares — every entry point keeps its exact shape, and every documented field
// of a governance declaration keeps its name and type.
var (
	_ plugin.Governor = (*Policy)(nil)
	_ plugin.Instance = (*Policy)(nil)

	_ func(*Policy) string = (*Policy).Name

	_ func(*Policy) plugin.Port = (*Policy).Port

	_ func(*Policy, plugin.CallPass) (plugin.Decision, error) = (*Policy).Judge

	_ func(*Policy) bool = (*Policy).Active

	_ func(*Policy) Enforcement = (*Policy).Mode

	_ func(context.Context, plugin.CallPass) context.Context = WithCallPass

	_ func(context.Context) (plugin.CallPass, bool) = CallPassFrom
)

// The 04.5 declarations keep the zero-selection gate: Open with every option
// named, and the plain grading input the rules match over.
var (
	_ func(...PolicyOption) *Policy = Open

	_ func() PolicyOption = WithStrict

	_ func(...RiskRule) PolicyOption = WithRules

	_ func(plugin.RiskLevel) PolicyOption = WithDefaultRisk

	_ func(*approval.ApprovalManager) PolicyOption = WithApprovals

	_ func(*GoalRegistry) PolicyOption = WithGoals

	_ func(...string) PolicyOption = WithGoalRequired

	_ func(*BudgetManager) PolicyOption = WithBudget

	_ func(*DenialLedger) PolicyOption = WithLedger

	_ = Open(
		WithStrict(),
		WithRules(RiskRule{}),
		WithDefaultRisk(plugin.RiskLow),
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
)

// Enforcement grades must stay compile-time constants of their declared type:
// a variable or a bare string stops this block from compiling.
const (
	_ Enforcement = EnforcementWarn
	_ Enforcement = EnforcementStrict
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

	_ func(*BudgetManager, plugin.RiskLevel) error = (*BudgetManager).Admit

	_ func(*BudgetManager, plugin.RiskLevel) int64 = (*BudgetManager).Usage

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

// denialOf renders the refusal of one call over the pipeline's ruling
// vocabulary, so a changed signature still compiles the package but stops this
// assertion.
var _ func(*trpcmodel.ToolCall, plugin.RiskLevel, string, string) *plugin.Denial = denialOf

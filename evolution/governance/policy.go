package governance

import (
	"context"
	"fmt"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
)

// Enforcement is the enforcement grade of the governance gate: it alone decides
// whether a violation is really refused.
//
// 契约: docs/wiki/agent/governance-enforcement.md#enforcement-modes
type Enforcement string

const (
	// EnforcementWarn records and exposes a violation without blocking the
	// execution. It is the default of an enabled gate.
	EnforcementWarn Enforcement = "warn"

	// EnforcementStrict refuses a violation. Whether a violation is really
	// refused is decided by this grade alone.
	EnforcementStrict Enforcement = "strict"
)

// RiskContext is the plain grading input one ruling is rendered over: the tool
// name, the argument JSON and the trigger source, and nothing else. A rule
// matches over this data as a pure function.
type RiskContext struct {
	// ToolName is the tool the call wants to run.
	ToolName string
	// ArgsJSON is the exact argument bytes the call carries.
	ArgsJSON []byte
	// TriggerSource names what triggered the call.
	TriggerSource string
}

// RiskRule is one data-driven violation rule over the plain grading input. The
// rule consumes the risk grade the RiskAssessor face rendered — the grade the
// one Before pass carried — and fires as a violation at the minimum grade it
// declares; it never renders a grade of its own, so no second risk face ever
// runs. The first fired rule is the violation the ruling reads.
type RiskRule struct {
	// ID names the rule in the ruling and in the refusal.
	ID string
	// MinRisk is the minimum risk grade at which this rule fires: the rule
	// consumes the grade of the Before pass against this threshold. An empty
	// threshold fires at every grade.
	MinRisk plugin.RiskLevel
	// Match reports whether this rule covers the call. It is a pure function
	// over the plain grading input.
	Match func(RiskContext) bool
	// Reason is the correction material the model reads when the rule refuses
	// or records the call.
	Reason string
}

// callPassKey is the context key one Before pass is carried under.
type callPassKey struct{}

// WithCallPass carries the one Before pass of a tool call on the context of that
// call, so the admission ruling of the composition root rules over the very pass
// the dispatch produced. The dispatch is its production writer. The pass is the
// seam's only value: what triggered the call travels inside it, and no second
// context key is ever introduced.
func WithCallPass(ctx context.Context, pass plugin.CallPass) context.Context {
	return context.WithValue(ctx, callPassKey{}, pass)
}

// CallPassFrom reports the Before pass the context carries and whether one is
// set. It is this seam's single typed accessor: a call site reads the pass
// through it and never through a bare context value, and the composition root's
// admission ruling is its production reader. A call driven beside no pass reads
// not-ok, and that caller decides the default — the admission ruling refuses
// such a call closed rather than ruling at a guess.
func CallPassFrom(ctx context.Context) (plugin.CallPass, bool) {
	pass, ok := ctx.Value(callPassKey{}).(plugin.CallPass)
	return pass, ok
}

// Policy is the pre-execution enforcement policy of the governance gate: the
// warn/strict grade, the violation rules, and the goal, budget and approval
// gates beside the ledger every ruling leaves its governance double on.
//
// The gate carries no configuration field of its own: Open starts it and every
// gate is added by the option that names it. Policy renders the single
// pre-execution ruling of one call — exactly once per call, over the one Before
// pass that call carries — and that one ruling is used both for the execution
// and for the record.
//
// 契约: docs/wiki/agent/governance-enforcement.md#enforcement-modes
type Policy struct {
	enabled     bool
	enforcement Enforcement
	rules       []RiskRule
	defaultRisk plugin.RiskLevel
	approvals   *approval.ApprovalManager
	goals       *GoalRegistry
	budget      *BudgetManager
	ledger      *DenialLedger
	goalSources []string
}

// PolicyOption configures the governance gate at construction.
type PolicyOption func(*Policy)

// Open starts the governance gate with the warn grade and every gate off. Each
// option names one gate, so a caller states the gates the run really carries:
//
//   - WithRules and WithApprovals are the violation rules and the approval channel
//   - WithGoals and WithGoalRequired are the goal gate and the sources it covers
//   - WithBudget and WithLedger are the budget window and the governance double
//   - WithDefaultRisk and WithStrict are the fallback grade and the refusing grade
//
// The zero value and a nil policy are the gate that is not open: every call is
// allowed with no ruling duty.
func Open(opts ...PolicyOption) *Policy {
	policy := &Policy{enabled: true}
	for _, opt := range opts {
		if opt != nil {
			opt(policy)
		}
	}
	return policy
}

// WithStrict raises the grade of the gate to strict: a violation is refused
// before its side effect instead of being recorded and exposed.
func WithStrict() PolicyOption {
	return func(p *Policy) { p.enforcement = EnforcementStrict }
}

// WithRules attaches the data-driven violation rules, read in order; the first
// fired rule is the violation the ruling reads.
func WithRules(rules ...RiskRule) PolicyOption {
	return func(p *Policy) { p.rules = append([]RiskRule(nil), rules...) }
}

// WithDefaultRisk sets the conservative grade the ruling falls back to when the
// one Before pass carries none. An empty value reads as RiskMedium.
func WithDefaultRisk(level plugin.RiskLevel) PolicyOption {
	return func(p *Policy) { p.defaultRisk = level }
}

// WithApprovals attaches the approval channel the critical grade always takes,
// whatever the rest of the policy says. A critical call beside no channel is
// refused: there is nobody who could approve it.
func WithApprovals(approvals *approval.ApprovalManager) PolicyOption {
	return func(p *Policy) { p.approvals = approvals }
}

// WithGoals attaches the goal registry the goal gate rules over: a call at the
// high grade and above from a trigger source that requires a goal is refused
// while no goal is in flight. Every ruling beside a registry permeates the
// current goal context into its record.
func WithGoals(goals *GoalRegistry) PolicyOption {
	return func(p *Policy) { p.goals = goals }
}

// WithGoalRequired names the trigger sources an operation must hang on an
// active goal for. The names match trigger sources exactly, as the governance
// semantics define it, and an empty list leaves the goal gate off.
func WithGoalRequired(sources ...string) PolicyOption {
	return func(p *Policy) { p.goalSources = append([]string(nil), sources...) }
}

// WithBudget attaches the budget gate the ruling counts high-grade and
// medium-grade operations into: a call that finds its window spent is refused
// whatever the enforcement grade says.
func WithBudget(budget *BudgetManager) PolicyOption {
	return func(p *Policy) { p.budget = budget }
}

// WithLedger attaches the governance ledger every refusal and every recorded
// execution leaves its governance double on. A gate beside no ledger keeps
// ruling and drops the records.
func WithLedger(ledger *DenialLedger) PolicyOption {
	return func(p *Policy) { p.ledger = ledger }
}

// Active reports whether the governance gate is on. A nil policy and the zero
// value are a gate that is not on.
func (p *Policy) Active() bool {
	return p != nil && p.enabled
}

// Mode reports the effective enforcement grade of the gate: an enabled gate
// that chose no grade runs under warn, the default. A gate that is not on
// reports the empty grade.
func (p *Policy) Mode() Enforcement {
	if !p.Active() {
		return ""
	}
	if p.enforcement == "" {
		return EnforcementWarn
	}
	return p.enforcement
}

// goalGateReason and budgetGateReason are the correction material one goal or
// budget ruling carries: they name the violation the model reads and corrects
// itself on.
const (
	// goalGateReason is the reason behind a goal gate ruling: a high-grade
	// autonomous operation was ruled while no goal was in flight.
	goalGateReason = "a high-grade autonomous operation must hang on a declared goal — declare one first, or have an operator clear goal_required_for"
	// budgetGateReason is the reason behind a budget gate ruling: the window's
	// budget for that grade is spent.
	budgetGateReason = "the operation budget for this window is exhausted — retry later or lower the risk"
)

// Judge renders the single pre-execution ruling of one tool call — the Governor
// face of the policy — over the one Before pass that call carries: the final
// call the pass produced, the risk grade its risk faces already rendered and the
// trigger source the dispatch stated. The pass is the whole input, so the ruling
// has no hidden input and consumes the grade instead of rendering it a second
// time: no second risk face runs inside the ruling. A pass carrying no grade is
// ruled at the conservative fallback grade.
//
// The ruling runs exactly once per call and is used both for the execution and
// for the record. A held call is a wait and keeps Denial nil; a refused call
// carries its Denial and does not run.
func (p *Policy) Judge(pass plugin.CallPass) (plugin.Decision, error) {
	call := &pass.Final
	grade := p.gradeOf(pass)
	if !p.Active() {
		return plugin.Decision{Disposition: plugin.DispositionAllow, Risk: grade, Reason: "the governance gate is not enabled"}, nil
	}
	risk := RiskContext{
		ToolName:      call.Function.Name,
		ArgsJSON:      call.Function.Arguments,
		TriggerSource: pass.TriggerSource,
	}
	var fired *RiskRule
	for i := range p.rules {
		rule := &p.rules[i]
		if rule.Match == nil || !rule.Match(risk) {
			continue
		}
		if grade.Rank() < rule.MinRisk.Rank() {
			continue
		}
		fired = rule
		break
	}
	ruleID, reason := "", ""
	if fired != nil {
		ruleID, reason = fired.ID, fired.Reason
	}
	if grade == plugin.RiskCritical {
		return p.riskCritical(call, grade, ruleID, reason), nil
	}
	if grade.Rank() >= plugin.RiskHigh.Rank() && p.goals != nil &&
		p.goalRequiredFor(risk.TriggerSource) && !p.goals.HasActive() {
		p.record(SubtypeDenial, call, grade, ruleID, goalGateReason)
		if p.Mode() == EnforcementStrict {
			return plugin.Decision{
				Risk:   grade,
				Rule:   ruleID,
				Reason: goalGateReason,
				Denial: denialOf(call, grade, ruleID, goalGateReason),
			}, nil
		}
	}
	if p.budget != nil {
		if err := p.budget.Admit(grade); err != nil {
			p.record(SubtypeDenial, call, grade, ruleID, budgetGateReason)
			return plugin.Decision{
				Risk:   grade,
				Rule:   ruleID,
				Reason: budgetGateReason,
				Denial: denialOf(call, grade, ruleID, budgetGateReason),
			}, nil
		}
	}
	if fired != nil {
		if p.Mode() == EnforcementStrict {
			p.record(SubtypeDenial, call, grade, ruleID, reason)
			return plugin.Decision{
				Risk:   grade,
				Rule:   ruleID,
				Reason: reason,
				Denial: denialOf(call, grade, ruleID, reason),
			}, nil
		}
		p.record(SubtypeAudit, call, grade, ruleID, reason)
		return plugin.Decision{Disposition: plugin.DispositionRecord, Risk: grade, Rule: ruleID, Reason: reason}, nil
	}
	decision := plugin.Decision{Disposition: dispositionFor(grade), Risk: grade}
	if decision.Disposition == plugin.DispositionRecord {
		p.record(SubtypeAudit, call, grade, ruleID, reason)
	}
	return decision, nil
}

// riskCritical renders the ruling of a call at the critical grade: the approval
// record decides whether the held call may proceed on this very argument
// digest, and everything else waits. A held call is a wait and is never
// reported as a denial; a denied or approved-and-retried call past its validity
// never proceeds on the old request.
func (p *Policy) riskCritical(call *trpcmodel.ToolCall, grade plugin.RiskLevel, ruleID, reason string) plugin.Decision {
	if p.approvals == nil {
		noChannel := "critical risk requires approval, but no approval channel is wired"
		p.record(SubtypeDenial, call, grade, ruleID, noChannel)
		return plugin.Decision{
			Risk:   grade,
			Rule:   ruleID,
			Reason: noChannel,
			Denial: denialOf(call, grade, ruleID, noChannel),
		}
	}
	req := p.approvals.Lookup(call.Function.Name, approval.ArgsDigest(call.Function.Arguments))
	switch {
	case req != nil && req.Status == approval.ApprovalApproved:
		approved := "the call was approved for these arguments"
		p.record(SubtypeAudit, call, grade, ruleID, approved)
		return plugin.Decision{Disposition: plugin.DispositionRecord, Risk: grade, Rule: ruleID, Reason: approved}
	case req != nil && req.Status == approval.ApprovalDenied:
		reason := "the approval for this call was denied"
		p.record(SubtypeDenial, call, grade, ruleID, reason)
		return plugin.Decision{
			Risk:   grade,
			Rule:   ruleID,
			Reason: reason,
			Denial: denialOf(call, grade, ruleID, reason),
		}
	default:
		wait := "critical risk awaits approval"
		if reason != "" {
			wait = reason + "; " + wait
		}
		p.record(SubtypeApproval, call, grade, ruleID, wait)
		return plugin.Decision{Disposition: plugin.DispositionHold, Risk: grade, Rule: ruleID, Reason: wait}
	}
}

// gradeOf reports the risk grade the ruling is rendered at: the grade the one
// Before pass carries on its own, or the conservative fallback when the pass
// carries none. The grade is read, never rendered a second time.
func (p *Policy) gradeOf(pass plugin.CallPass) plugin.RiskLevel {
	if pass.Risk != "" {
		return pass.Risk
	}
	if p != nil && p.defaultRisk != "" {
		return p.defaultRisk
	}
	return plugin.RiskMedium
}

// dispositionFor reports the run disposition of one grade: the risk grades map
// to their trace duty, and the critical grade is held by the caller instead of
// reaching this mapping.
func dispositionFor(grade plugin.RiskLevel) plugin.Disposition {
	switch {
	case grade.Rank() >= plugin.RiskHigh.Rank():
		return plugin.DispositionRecord
	default:
		return plugin.DispositionAllow
	}
}

// denialOf renders the terminal refusal of one call beside its native paired
// result: the result names the very call and tool the model issued and answers
// under the same call identity, and its text carries the reason and the rule
// for the model to read. The refusal facts are the native call's own — same
// CallID, same tool, same arguments — so the model corrects itself on the call
// it actually made.
func denialOf(call *trpcmodel.ToolCall, grade plugin.RiskLevel, ruleID, reason string) *plugin.Denial {
	content := fmt.Sprintf("tagent: the call was refused before execution: %s (rule: %s, risk: %s)", reason, ruleID, grade)
	return &plugin.Denial{
		CallID: call.ID,
		Tool:   call.Function.Name,
		Risk:   grade,
		Rule:   ruleID,
		Result: trpcmodel.Message{
			Role:     trpcmodel.RoleTool,
			ToolID:   call.ID,
			ToolName: call.Function.Name,
			Content:  content,
		},
	}
}

// record leaves the governance double of one ruling fact on the ledger: the
// ledger record and the governance event beside it. The record permeates the
// goal context the call was hanging on — the denial's goal permeation — so the
// audit reads which autonomous scope the ruling happened in. A nil ledger
// drops the record and the ruling stands.
func (p *Policy) record(subtype string, call *trpcmodel.ToolCall, grade plugin.RiskLevel, ruleID, reason string) {
	if p == nil || p.ledger == nil {
		return
	}
	p.ledger.Record(DenialRecord{
		Subtype:    subtype,
		ToolName:   call.Function.Name,
		Level:      grade,
		RuleID:     ruleID,
		Reason:     reason,
		ArgsDigest: approval.ArgsDigest(call.Function.Arguments),
		GoalID:     p.goalID(),
	})
}

// goalID reports the goal context one record permeates: the identity of the
// goal the registry reads as the current goal, or empty when none is in
// flight.
func (p *Policy) goalID() string {
	if p == nil || p.goals == nil {
		return ""
	}
	if goal := p.goals.Current(); goal != nil {
		return goal.ID
	}
	return ""
}

// goalRequiredFor reports whether the trigger source is one an operation must
// hang on an active goal for. The list matches trigger sources exactly, as the
// governance semantics define it.
func (p *Policy) goalRequiredFor(source string) bool {
	if p == nil {
		return false
	}
	for _, s := range p.goalSources {
		if s == source {
			return true
		}
	}
	return false
}

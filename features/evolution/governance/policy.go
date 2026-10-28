package governance

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
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

// Policy is the pre-execution enforcement policy of the governance gate: the
// warn/strict grade, the violation rules, the declared risk table, and the goal,
// budget and approval gates beside the ledger every ruling leaves its governance
// double on.
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
	riskRules   RiskRules
	defaultRisk plugin.RiskLevel
	approvals   *approval.ApprovalManager
	goals       *GoalRegistry
	budget      *BudgetManager
	ledger      *DenialLedger
	goalSources []string
}

// Name returns the stable name of the governance gate in the flow: the one
// element a rendered ruling traces back to.
func (p *Policy) Name() string { return "governance-policy" }

// Port reports the material this policy reads and the product it answers: one
// tool call before its side effect, answered with the pre-execution ruling over
// it. The grade the ruling consumes is the risk face's product, so this face
// declares the call->ruling port without rendering a second grade.
func (p *Policy) Port() plugin.Port {
	return plugin.PortCallRuling
}

// PolicyOption configures the governance gate at construction.
type PolicyOption func(*Policy)

// Open starts the governance gate with the warn grade and every gate off. Each
// option names one gate, so a caller states the gates the run really carries:
//
//   - WithRules and WithApprovals are the violation rules and the approval channel
//   - WithRiskRules is the declared risk table an ungraded pass is ruled at
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

// WithDefaultRisk sets the conservative grade the ruling falls back to when
// neither the one Before pass nor the declared risk table names a grade. An
// empty value reads as RiskMedium.
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
// never proceeds on the old request. A held call whose tool and arguments name
// no request registers one before the hold returns, so the approval book always
// names the call the gate parked; the receipt carries that request identity, and
// a registration the manager could not persist leaves the hold standing beside
// the named failure.
//
// Every ruling the approval record answers is observed beside this gate: an
// approved, a denied, an expired and an unapprovable call each write one INFO
// line carrying the argument digest, the ruling and the approver, while the
// same pending wait writes one DEBUG line per judgment.
func (p *Policy) riskCritical(call *trpcmodel.ToolCall, grade plugin.RiskLevel, ruleID, reason string) plugin.Decision {
	if p.approvals == nil {
		noChannel := "critical risk requires approval, but no approval channel is wired"
		p.record(SubtypeDenial, call, grade, ruleID, noChannel)
		approvalInfo("tagent: approval ruled tool=%s risk=%s rule=%s ruling=unavailable",
			call.Function.Name, grade, ruleID)
		return plugin.Decision{
			Risk:   grade,
			Rule:   ruleID,
			Reason: noChannel,
			Denial: denialOf(call, grade, ruleID, noChannel),
		}
	}
	digest := approval.ArgsDigest(call.Function.Arguments)
	req := p.approvals.Lookup(call.Function.Name, digest)
	switch {
	case req != nil && req.Status == approval.ApprovalApproved:
		approved := "the call was approved for these arguments"
		p.record(SubtypeAudit, call, grade, ruleID, approved)
		approvalInfo("tagent: approval ruled tool=%s digest=%s risk=%s rule=%s ruling=approved by=%s",
			call.Function.Name, digest, grade, ruleID, req.DecidedBy)
		return plugin.Decision{Disposition: plugin.DispositionRecord, Risk: grade, Rule: ruleID, Reason: approved}
	case req != nil && req.Status == approval.ApprovalDenied:
		reason := "the approval for this call was denied"
		p.record(SubtypeDenial, call, grade, ruleID, reason)
		approvalInfo("tagent: approval ruled tool=%s digest=%s risk=%s rule=%s ruling=denied by=%s",
			call.Function.Name, digest, grade, ruleID, req.DecidedBy)
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
		var registerErr error
		if req == nil {
			req, registerErr = p.approvals.Request(context.Background(), newApprovalRequestID(),
				call.Function.Name, call.Function.Arguments, grade, ruleID, wait)
		}
		if req != nil && req.Status != approval.ApprovalExpired {
			wait = fmt.Sprintf("%s (approval request %s)", wait, req.ID)
		}
		if registerErr != nil {
			wait = fmt.Sprintf("%s; the approval request could not be persisted: %s", wait, registerErr)
		}
		p.record(SubtypeApproval, call, grade, ruleID, wait)
		switch {
		case req != nil && req.Status == approval.ApprovalExpired:
			approvalInfo("tagent: approval ruled tool=%s digest=%s risk=%s rule=%s ruling=expired expires_ms=%d",
				call.Function.Name, digest, grade, ruleID, req.ExpiresMs)
		default:
			approvalDebug("tagent: approval awaited tool=%s digest=%s risk=%s rule=%s",
				call.Function.Name, digest, grade, ruleID)
		}
		return plugin.Decision{Disposition: plugin.DispositionHold, Risk: grade, Rule: ruleID, Reason: wait}
	}
}

// approvalInfo renders one line of the approval channel at INFO: the tool, the
// argument digest the ruling is bound to, the risk grade, the rule and the
// ruling that took effect beside the approver who stated it. A ruling is one
// event per request, so the INFO face stays bounded by the rulings an approver
// really stated.
//
// The redline list (3 rules):
// MUST stay a bypass: a failing logger is a missing line, never a changed ruling.
// MUST write one line per stated ruling, never one per judgment of a pending wait.
// MUST NOT render the arguments themselves, only their digest and the ruling.
func approvalInfo(format string, args ...any) {
	if trpclog.Default == nil {
		return
	}
	defer func() { _ = recover() }()
	trpclog.Default.Infof(format, args...)
}

// approvalDebug renders the wait of a critical call at DEBUG: the same identity
// the ruling lines carry, written once per judgment while the approval record
// still awaits an external ruling. A held call is judged again on every attempt,
// so its wait stays out of the INFO stream.
//
// The redline list (3 rules):
// MUST stay a bypass: a failing logger is a missing line, never a released wait.
// MUST stay at DEBUG, since the same wait is judged again on every attempt.
// MUST NOT render the arguments themselves, only their digest and the grade.
func approvalDebug(format string, args ...any) {
	if trpclog.Default == nil {
		return
	}
	defer func() { _ = recover() }()
	trpclog.Default.Debugf(format, args...)
}

// gradeOf reports the risk grade the ruling is rendered at: the grade the one
// Before pass carries on its own, or the grade the declared risk table renders
// for the final call, or the conservative fallback when neither names one. The
// grade is read, never rendered a second time; the table is the default risk
// face and never overrides a grade a wired face already rendered.
func (p *Policy) gradeOf(pass plugin.CallPass) plugin.RiskLevel {
	if pass.Risk != "" {
		return pass.Risk
	}
	if p != nil {
		if level, ok := p.riskRules.grade(pass.Final.Function.Name); ok {
			return level
		}
		if p.defaultRisk != "" {
			return p.defaultRisk
		}
	}
	return plugin.RiskMedium
}

// newApprovalRequestID issues the identity of one approval request the gate
// registers at a hold: "approval-" beside the lowercase hex of 16 random bytes.
// The identity resolves the request across the hold and every later ruling, so
// the native call identity is not a substitute.
func newApprovalRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic("governance: crypto/rand failed to issue an approval request identity: " + err.Error())
	}
	return "approval-" + hex.EncodeToString(raw[:])
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

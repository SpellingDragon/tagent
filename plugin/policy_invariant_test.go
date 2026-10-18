package plugin

import (
	"strings"
	"testing"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
)

// invariantRulingCall builds one native tool call for the ruling to judge.
func invariantRulingCall(id, tool string) trpcmodel.ToolCall {
	return trpcmodel.ToolCall{ID: id, Function: trpcmodel.FunctionDefinitionParam{Name: tool, Arguments: []byte(`{"command":"ls"}`)}}
}

// invariantMatchingRule builds one rule that fires at every rendered grade.
func invariantMatchingRule(reason string) RiskRule {
	return RiskRule{
		ID:      "no-shell",
		MinRisk: RiskLow,
		Match:   func(RiskContext) bool { return true },
		Reason:  reason,
	}
}

// TestGovernanceRulingInvariants pins the one pre-execution ruling a call receives.
// - a hold is a wait and never a denial, while a refused call carries its denial beside it
// - a critical call beside no approval channel is refused instead of waiting forever
// - an exhausted budget refuses the call whatever the enforcement degree says
// - a pass carrying no grade is ruled at the ordinary mutating grade and never passes ungraded
// - a warn rule records the call while the same rule under strict refuses it
// 契约: docs/wiki/agent/governance-enforcement.md#enforcement-modes
func TestGovernanceRulingInvariants(t *testing.T) {
	call := invariantRulingCall("call-1", "shell")

	t.Run("a hold waits and is never reported as a denial", func(t *testing.T) {
		ruling, err := Open(WithApprovals(NewApprovalManager("", 0))).Judge(CallPass{Final: call, Risk: RiskCritical})
		if err != nil {
			t.Fatalf("judge the critical call: %v", err)
		}
		if ruling.Disposition != DispositionHold {
			t.Fatalf("disposition = %q, want %q while approval is pending", ruling.Disposition, DispositionHold)
		}
		if ruling.Denial != nil || ruling.Refused() {
			t.Fatalf("held ruling = %+v, want a wait without a denial", ruling)
		}
		if ruling.Disposition == DispositionRecord || ruling.Disposition == DispositionAllow {
			t.Fatalf("disposition = %q, want the held call to stay parked", ruling.Disposition)
		}
	})

	t.Run("a critical call beside no approval channel is refused", func(t *testing.T) {
		ruling, err := Open().Judge(CallPass{Final: call, Risk: RiskCritical})
		if err != nil {
			t.Fatalf("judge the unapprovable call: %v", err)
		}
		if ruling.Denial == nil || !ruling.Refused() {
			t.Fatalf("ruling = %+v, want a terminal refusal", ruling)
		}
		if ruling.Disposition == DispositionHold {
			t.Fatalf("disposition = %q, want a refusal told apart from a hold", ruling.Disposition)
		}
		if !strings.Contains(ruling.Reason, "approval") {
			t.Fatalf("reason = %q, want the missing approval channel named", ruling.Reason)
		}
	})

	t.Run("an exhausted budget refuses the call whatever the enforcement degree says", func(t *testing.T) {
		budget := NewBudgetManager(BudgetConfig{MaxHighRisk: 1}, "")
		warn := Open(WithBudget(budget))
		first, err := warn.Judge(CallPass{Final: call, Risk: RiskHigh})
		if err != nil {
			t.Fatalf("judge the first high call: %v", err)
		}
		if first.Refused() || first.Disposition != DispositionRecord {
			t.Fatalf("first high ruling = %+v, want it admitted and recorded", first)
		}
		second, err := warn.Judge(CallPass{Final: call, Risk: RiskHigh})
		if err != nil {
			t.Fatalf("judge the second high call: %v", err)
		}
		if !second.Refused() || second.Denial == nil {
			t.Fatalf("second high ruling = %+v, want a refusal once the window is spent", second)
		}
		if second.Reason != budgetGateReason {
			t.Fatalf("reason = %q, want the budget gate reason", second.Reason)
		}

		strictBudget := NewBudgetManager(BudgetConfig{MaxHighRisk: 1}, "")
		strict := Open(WithBudget(strictBudget), WithStrict())
		if _, err := strict.Judge(CallPass{Final: call, Risk: RiskHigh}); err != nil {
			t.Fatalf("judge the strict first high call: %v", err)
		}
		refused, err := strict.Judge(CallPass{Final: call, Risk: RiskHigh})
		if err != nil {
			t.Fatalf("judge the strict second high call: %v", err)
		}
		if !refused.Refused() {
			t.Fatalf("strict second high ruling = %+v, want the budget to refuse under strict too", refused)
		}
	})

	t.Run("a pass carrying no grade is ruled at the ordinary mutating grade", func(t *testing.T) {
		ruling, err := Open().Judge(CallPass{Final: call})
		if err != nil {
			t.Fatalf("judge the ungraded call: %v", err)
		}
		if ruling.Risk != RiskMedium {
			t.Fatalf("risk = %q, want the conservative fallback %q", ruling.Risk, RiskMedium)
		}
		if ruling.Risk == "" {
			t.Fatalf("risk = %q, want the ungraded pass ruled instead of passing ungraded", ruling.Risk)
		}
		if ruling.Disposition != DispositionAllow {
			t.Fatalf("disposition = %q, want an ordinary mutating call allowed", ruling.Disposition)
		}

		fallback, err := Open(WithDefaultRisk(RiskHigh)).Judge(CallPass{Final: call})
		if err != nil {
			t.Fatalf("judge the ungraded call beside a default grade: %v", err)
		}
		if fallback.Risk != RiskHigh || fallback.Disposition != DispositionRecord {
			t.Fatalf("ruling = %+v, want the configured default grade read as high and recorded", fallback)
		}
	})

	t.Run("a warn rule records the call while the same rule under strict refuses it", func(t *testing.T) {
		warned, err := Open(WithRules(invariantMatchingRule("shell needs a review"))).Judge(CallPass{Final: call, Risk: RiskMedium})
		if err != nil {
			t.Fatalf("judge beside a warn rule: %v", err)
		}
		if warned.Disposition != DispositionRecord || warned.Rule != "no-shell" {
			t.Fatalf("warn ruling = %+v, want the fired rule recorded", warned)
		}
		if warned.Refused() {
			t.Fatalf("warn ruling = %+v, want a recording instead of a refusal", warned)
		}

		refused, err := Open(WithRules(invariantMatchingRule("shell needs a review")), WithStrict()).
			Judge(CallPass{Final: call, Risk: RiskMedium})
		if err != nil {
			t.Fatalf("judge beside a strict rule: %v", err)
		}
		if !refused.Refused() || refused.Denial == nil {
			t.Fatalf("strict ruling = %+v, want the fired rule refused", refused)
		}
		if refused.Denial.Rule != "no-shell" || refused.Denial.CallID != call.ID || refused.Denial.Tool != call.Function.Name {
			t.Fatalf("denial = %+v, want the fired rule and the native call identity named", refused.Denial)
		}
		if refused.Denial.Result.ToolID != call.ID || refused.Denial.Result.ToolName != call.Function.Name {
			t.Fatalf("denial result = %+v, want the paired result bound to the same call", refused.Denial.Result)
		}
	})
}

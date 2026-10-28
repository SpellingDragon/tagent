package governance

import (
	"fmt"
	"strings"
	"testing"
	"time"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
)

// riskRuleCall builds one native tool call the declared risk table grades.
func riskRuleCall(tool string) trpcmodel.ToolCall {
	return trpcmodel.ToolCall{ID: "call-risk", Function: trpcmodel.FunctionDefinitionParam{Name: tool, Arguments: []byte(`{"text":"hello"}`)}}
}

// riskRulePolicy returns the gate of the declared-table checks: the table is the
// only risk face the call passes, so the ruling renders the table's grade.
func riskRulePolicy(rules ...ToolRiskRule) *Policy {
	return Open(WithRiskRules(rules...), WithApprovals(approval.NewApprovalManager("", time.Hour)))
}

// TestRiskRulesGradeTools pins the declarative risk face over its three selection steps.
// - an exact rule grades its tool, and wins over a prefix rule of the same name
// - the longest matching prefix rule grades a prefixed tool
// - an unnamed tool falls back to the declared default grade, then to the ordinary mutating grade
// - a grade a wired face rendered is never overridden by the table
//
// 契约: docs/wiki/agent/governance-enforcement.md#disposition-and-risk
func TestRiskRulesGradeTools(t *testing.T) {
	t.Run("an exact rule grades its tool critical", func(t *testing.T) {
		ruling, err := riskRulePolicy(ToolRiskRule{Tool: "send", Level: plugin.RiskCritical}).
			Judge(plugin.CallPass{Final: riskRuleCall("send")})
		if err != nil {
			t.Fatalf("judge the declared critical call: %v", err)
		}
		if ruling.Risk != plugin.RiskCritical {
			t.Fatalf("risk = %q, want the exact rule's grade %q", ruling.Risk, plugin.RiskCritical)
		}
		if ruling.Disposition != plugin.DispositionHold {
			t.Fatalf("disposition = %q, want a critical call held", ruling.Disposition)
		}
	})

	t.Run("an exact rule outranks a prefix rule", func(t *testing.T) {
		ruling, err := riskRulePolicy(
			ToolRiskRule{Tool: "send", Prefix: true, Level: plugin.RiskLow},
			ToolRiskRule{Tool: "send", Level: plugin.RiskCritical},
		).Judge(plugin.CallPass{Final: riskRuleCall("send")})
		if err != nil {
			t.Fatalf("judge the exactly named call: %v", err)
		}
		if ruling.Risk != plugin.RiskCritical {
			t.Fatalf("risk = %q, want the exact rule's grade %q", ruling.Risk, plugin.RiskCritical)
		}
	})

	t.Run("the longest prefix rule grades a prefixed tool", func(t *testing.T) {
		ruling, err := riskRulePolicy(
			ToolRiskRule{Tool: "file:*", Prefix: true, Level: plugin.RiskLow},
			ToolRiskRule{Tool: "file:write", Prefix: true, Level: plugin.RiskHigh},
		).Judge(plugin.CallPass{Final: riskRuleCall("file:write_remote")})
		if err != nil {
			t.Fatalf("judge the prefixed call: %v", err)
		}
		if ruling.Risk != plugin.RiskHigh {
			t.Fatalf("risk = %q, want the longest prefix rule's grade %q", ruling.Risk, plugin.RiskHigh)
		}
		if ruling.Disposition != plugin.DispositionRecord {
			t.Fatalf("disposition = %q, want a high call recorded", ruling.Disposition)
		}
	})

	t.Run("an unnamed tool falls back to the default grade", func(t *testing.T) {
		ruling, err := Open(
			WithRiskRules(ToolRiskRule{Tool: "send", Prefix: true, Level: plugin.RiskCritical}),
			WithDefaultRisk(plugin.RiskLow),
		).Judge(plugin.CallPass{Final: riskRuleCall("recall_query")})
		if err != nil {
			t.Fatalf("judge the unnamed call: %v", err)
		}
		if ruling.Risk != plugin.RiskLow {
			t.Fatalf("risk = %q, want the declared default grade %q", ruling.Risk, plugin.RiskLow)
		}
		if ruling.Disposition != plugin.DispositionAllow {
			t.Fatalf("disposition = %q, want a low call allowed", ruling.Disposition)
		}

		ordinary, err := Open().Judge(plugin.CallPass{Final: riskRuleCall("recall_query")})
		if err != nil {
			t.Fatalf("judge the unnamed call beside no table: %v", err)
		}
		if ordinary.Risk != plugin.RiskMedium {
			t.Fatalf("risk = %q, want the ordinary mutating grade %q", ordinary.Risk, plugin.RiskMedium)
		}
	})

	t.Run("a rendered grade outranks the table", func(t *testing.T) {
		ruling, err := Open(WithRiskRules(ToolRiskRule{Tool: "send", Level: plugin.RiskCritical})).
			Judge(plugin.CallPass{Final: riskRuleCall("send"), Risk: plugin.RiskLow})
		if err != nil {
			t.Fatalf("judge the graded call: %v", err)
		}
		if ruling.Risk != plugin.RiskLow {
			t.Fatalf("risk = %q, want the grade the pass already carried %q", ruling.Risk, plugin.RiskLow)
		}
	})
}

// TestRiskRulesRefuseConstruction pins the validation of the declared table: an empty tool name and an unknown grade both refuse the policy at construction.
func TestRiskRulesRefuseConstruction(t *testing.T) {
	cases := []struct {
		name string
		rule ToolRiskRule
		want string
	}{
		{"an empty tool name", ToolRiskRule{Level: plugin.RiskLow}, "names no tool"},
		{"an unknown grade", ToolRiskRule{Tool: "send", Level: plugin.RiskLevel("urgent")}, "unknown risk grade"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					t.Fatalf("construction accepted %s", tc.name)
				}
				if !strings.Contains(fmt.Sprint(recovered), tc.want) {
					t.Fatalf("refusal = %v, want %q named", recovered, tc.want)
				}
			}()
			Open(WithRiskRules(tc.rule))
		})
	}
}

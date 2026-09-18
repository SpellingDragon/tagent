package governance

import (
	"testing"
	"time"
)

// Switch-combination regression for the governance gate
// (resident-remaining-hardening 3.4, archived 7.4).
//
// Invariant under test: "无审批不执行 critical" — a critical-classified call
// must NEVER reach execution without an explicit human approval, across every
// enforcement × approval-mechanism switch combination. The tool wrapper's
// single execution predicate is tool.go:78:
//
//	blocked := decision.Denied || decision.Disposition == DispositionHold
//
// so the gate fails closed unless Evaluate returns a non-Hold, non-Denied
// decision — which, for critical, happens ONLY on the approved path.

// criticalCtx is an exec call the default classifier rates RiskCritical
// (rule exec.destructive).
func criticalCtx() RiskContext {
	return RiskContext{ToolName: "exec", ArgsJSON: `{"command":"rm -rf /tmp/irreversible"}`, TriggerSource: "user"}
}

func blocked(d Decision) bool { return d.Denied || d.Disposition == DispositionHold }

func TestSwitchCombo_CriticalNeverExecutesWithoutApproval(t *testing.T) {
	cases := []struct {
		name string
		deps func(t *testing.T) GateDeps
	}{
		{
			// No approval mechanism at all → hard denial (never silently allowed).
			name: "no-approval-mechanism/strict",
			deps: func(*testing.T) GateDeps {
				return GateDeps{
					Classifier: NewRiskClassifier(DefaultRules(), RiskMedium),
					Config:     GateConfig{Enabled: true, Enforcement: EnforcementStrict},
				}
			},
		},
		{
			// Approval mechanism present but nothing granted → held (warn mode).
			name: "approval-present-ungranted/warn",
			deps: func(t *testing.T) GateDeps {
				return GateDeps{
					Classifier: NewRiskClassifier(DefaultRules(), RiskMedium),
					Approval:   NewApprovalManager(t.TempDir(), 30*time.Minute),
					Config:     GateConfig{Enabled: true, Enforcement: EnforcementWarn},
				}
			},
		},
		{
			name: "approval-present-ungranted/strict",
			deps: func(t *testing.T) GateDeps {
				return GateDeps{
					Classifier: NewRiskClassifier(DefaultRules(), RiskMedium),
					Approval:   NewApprovalManager(t.TempDir(), 30*time.Minute),
					Config:     GateConfig{Enabled: true, Enforcement: EnforcementStrict},
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGovernanceGate(tc.deps(t))
			d := g.Evaluate(criticalCtx())
			if d.Level != RiskCritical {
				t.Fatalf("expected the fixture to classify critical, got %v", d.Level)
			}
			if !blocked(d) {
				t.Fatalf("critical WITHOUT approval must be blocked (Denied or Hold); got %+v", d)
			}
		})
	}
}

// TestSwitchCombo_CriticalExecutesOnlyAfterApproval is the complement: the SAME
// call, once a human approval is granted for its exact args digest, is allowed
// to run (non-Hold, non-Denied). This proves the gate opens ONLY through the
// approval path and is not simply always-denying.
func TestSwitchCombo_CriticalExecutesOnlyAfterApproval(t *testing.T) {
	ctx := criticalCtx()
	am := NewApprovalManager(t.TempDir(), 30*time.Minute)
	g := NewGovernanceGate(GateDeps{
		Classifier: NewRiskClassifier(DefaultRules(), RiskMedium),
		Approval:   am,
		Config:     GateConfig{Enabled: true, Enforcement: EnforcementStrict},
	})

	// Before any approval: blocked.
	if d := g.Evaluate(ctx); !blocked(d) {
		t.Fatalf("critical must be blocked pre-approval: %+v", d)
	}

	// Human approves the exact tool+args the gate will re-check.
	req, err := am.Request(ctx.ToolName, ctx.ArgsJSON, ctx.ArgsJSON, RiskCritical.String(), "exec.destructive", "rm -rf", "")
	if err != nil {
		t.Fatalf("approval Request: %v", err)
	}
	if err := am.Decide(req.ID, ApprovalApproved, "human-reviewer"); err != nil {
		t.Fatalf("approval Decide: %v", err)
	}

	// After approval: the gate lets it through (no longer blocked).
	if d := g.Evaluate(ctx); blocked(d) {
		t.Fatalf("approved critical must not be blocked: %+v", d)
	}
}

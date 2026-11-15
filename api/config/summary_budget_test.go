// 契约: docs/wiki/agent/agent-architecture.md#request-budget
package config

import (
	"context"
	"testing"
)

// TestSummaryBudgetReadsTheDeclaration pins that a declared summary_budget_bytes is the bound the fold reads.
func TestSummaryBudgetReadsTheDeclaration(t *testing.T) {
	body := "entry: main\nagents:\n  - id: main\n    replay_window: 4096\n    summary_budget_bytes: 512\n"
	decl, err := decodeConfigFile(context.Background(), writeConfigSource(t, "org.yaml", body))
	if err != nil {
		t.Fatalf("decodeConfigFile over a declared summary budget = %v, want the budget read", err)
	}
	if got := decl.Agents[0].SummaryBudgetBytes; got != 512 {
		t.Fatalf("summary budget = %d, want the declared 512", got)
	}
}

// TestSummaryBudgetReadsTheDefault pins that a declaration naming no summary budget reads the policy default.
func TestSummaryBudgetReadsTheDefault(t *testing.T) {
	body := "entry: main\nagents:\n  - id: main\n    replay_window: 4096\n"
	decl, err := decodeConfigFile(context.Background(), writeConfigSource(t, "org.yaml", body))
	if err != nil {
		t.Fatalf("decodeConfigFile over an undeclared summary budget = %v, want the default read", err)
	}
	if got := decl.Agents[0].SummaryBudgetBytes; got != DefaultSummaryBudgetBytes {
		t.Fatalf("summary budget = %d, want the declaration default %d", got, DefaultSummaryBudgetBytes)
	}
	if DefaultSummaryBudgetBytes != 2048 {
		t.Fatalf("declaration default = %d, want the policy default 2048", DefaultSummaryBudgetBytes)
	}
}

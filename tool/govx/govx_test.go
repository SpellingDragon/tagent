package govx

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/SpellingDragon/tagent/agent/governance"
	"github.com/SpellingDragon/tagent/memory"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestGoalDeclare_PersistsEvent (5.1, design-report-closeout): goal_declare
// registers the goal AND (with a bound store) persists a governance event —
// audit trail and restart rebuild come from the same write.
func TestGoalDeclare_PersistsEvent(t *testing.T) {
	store := memory.NewInMemoryStore()
	pid := memory.PartitionIDFromName("tagent")
	goals := governance.NewGoalRegistry()
	goals.BindStore(store, pid)
	gate := governance.NewGovernanceGate(governance.GateDeps{
		Goals:  goals,
		Ledger: governance.NewDenialLedger(nil, 0),
		Config: governance.GateConfig{Enabled: true},
	})
	tools := NewGoalTools(gate)
	if len(tools) != 5 {
		t.Fatalf("expected 5 tools, got %d", len(tools))
	}

	// Locate goal_declare and call it through the CallableTool surface.
	var declare trpctool.CallableTool
	for _, tl := range tools {
		if ct, ok := tl.(trpctool.CallableTool); ok && ct.Declaration().Name == "goal_declare" {
			declare = ct
		}
	}
	if declare == nil {
		t.Fatal("goal_declare not registered")
	}
	res, err := declare.Call(context.Background(), []byte(`{"statement":"完成季度报告","created_by":"user"}`))
	if err != nil {
		t.Fatalf("goal_declare: %v", err)
	}
	raw, _ := json.Marshal(res)
	var out goalDeclareResult
	if err := json.Unmarshal(raw, &out); err != nil || !out.OK || out.GoalID == "" {
		t.Fatalf("bad result: %s (%v)", raw, err)
	}

	// Registry updated.
	if !goals.HasActive() {
		t.Fatal("goal not active after declare")
	}
	// Governance event persisted (subtype=goal).
	refs, err := store.QueryEvents(memory.QueryOptions{
		PartitionIDs: []int{pid}, EventTypes: []string{"governance"}, Limit: 10,
	})
	if err != nil || len(refs) != 1 {
		t.Fatalf("expected 1 governance event, got %d (err=%v)", len(refs), err)
	}
	ev, _ := store.GetEvent(refs[0].EventKey)
	if ev.Metadata["goal_op"] != "declared" || ev.Metadata["goal_id"] != out.GoalID {
		t.Fatalf("event metadata wrong: %v", ev.Metadata)
	}
}

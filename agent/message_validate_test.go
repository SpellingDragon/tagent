package agent

import (
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// L2: a well-formed sequence is left unchanged.
func TestRepairToolPairing_CleanUnchanged(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleUser, Content: "hi"},
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "a"}}},
		{Role: model.RoleTool, ToolID: "a", Content: "res"},
	}
	out, n := repairToolPairing(msgs)
	if n != 0 || len(out) != 3 {
		t.Errorf("clean sequence should be unchanged: repairs=%d len=%d", n, len(out))
	}
}

// L2: a duplicate tool result (same tool_id answered twice) is dropped — the
// exact malformation behind the observed API 4xx.
func TestRepairToolPairing_DropsDuplicate(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "a"}}},
		{Role: model.RoleTool, ToolID: "a", Content: "res1"},
		{Role: model.RoleTool, ToolID: "a", Content: "res2"}, // duplicate
	}
	out, n := repairToolPairing(msgs)
	if n != 1 || len(out) != 2 {
		t.Errorf("duplicate tool result should be dropped: repairs=%d len=%d", n, len(out))
	}
}

// L2: an orphan tool result (no preceding declaring tool_call) is dropped.
func TestRepairToolPairing_DropsOrphan(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleUser, Content: "hi"},
		{Role: model.RoleTool, ToolID: "x", Content: "orphan"},
	}
	out, n := repairToolPairing(msgs)
	if n != 1 || len(out) != 1 {
		t.Errorf("orphan tool result should be dropped: repairs=%d len=%d", n, len(out))
	}
	if out[0].Role != model.RoleUser {
		t.Errorf("the user message should remain")
	}
}

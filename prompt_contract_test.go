package tagent

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestPlanPromptContract locks the plan prompt invariants from
// plan-interaction-contract (D3/D5): no phantom-tool escape hatches, dual
// path-base table present, level-based closing, and the parent-facing tool
// description declares the output boundary and resume protocol.
func TestPlanPromptContract(t *testing.T) {
	agentPrompt, err := os.ReadFile("resources/prompts/plan_agent.md")
	if err != nil {
		t.Fatalf("read plan_agent.md: %v", err)
	}
	s := string(agentPrompt)

	// B2 regression: the archive section must never reference tools that do
	// not exist in plan's toolset (shell/action escape hatch).
	for _, forbidden := range []string{"不受限", "replace_anchors", "python3 heredoc"} {
		if strings.Contains(s, forbidden) {
			t.Errorf("plan_agent.md must not contain phantom-tool escape hatch %q", forbidden)
		}
	}

	// A3: dual path-base table with both correct examples.
	for _, want := range []string{"路径基准", "openspec/changes/my-plan/tasks.md", "changes/my-plan/tasks.md"} {
		if !strings.Contains(s, want) {
			t.Errorf("plan_agent.md must document dual path bases, missing %q", want)
		}
	}

	// D3: A-level closes with status self-check and forbids validate.
	if !strings.Contains(s, `spec(op="status"`) || !strings.Contains(s, "禁止调用 validate") {
		t.Error("plan_agent.md must route A-level closing to status self-check and forbid validate")
	}

	// Tasks start unchecked; check-off right belongs to update accounting.
	if !strings.Contains(s, "勾选权属于执行后的 update 报账") {
		t.Error("plan_agent.md must state that check-off right belongs to update accounting")
	}

	toolDesc, err := os.ReadFile("resources/prompts/plan_tool_desc.md")
	if err != nil {
		t.Fatalf("read plan_tool_desc.md: %v", err)
	}
	d := string(toolDesc)

	// B1: parent-facing boundary + contract table + resume protocol.
	for _, want := range []string{"产出物边界", "不是工作成果", "resume_task", "各 action 契约", "task_terminal_ttl"} {
		if !strings.Contains(d, want) {
			t.Errorf("plan_tool_desc.md must contain %q (parent-facing contract)", want)
		}
	}

	// The examples copy must stay in sync with the canonical prompt — the
	// runtime loads the example-local copy when present (drift caused the
	// 2026-07-30 incident analysis mismatch).
	exampleCopy, err := os.ReadFile("examples/wechat-bot/resources/prompts/plan_agent.md")
	if err == nil && !bytes.Equal(exampleCopy, agentPrompt) {
		t.Error("examples/wechat-bot plan_agent.md copy has drifted from resources/prompts/plan_agent.md — re-sync them")
	}
}

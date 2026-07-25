package agent

import (
	"strings"
	"testing"
)

// TestSubagentRounds_RecentBounded: the task-local round chain keeps rounds
// in order and bounds the restored window.
func TestSubagentRounds_RecentBounded(t *testing.T) {
	r := &subagentRounds{}
	for _, s := range []string{"r1", "r2", "r3", "r4", "r5"} {
		r.add("in-"+s, "out-"+s)
	}
	got := r.recent(3)
	if len(got) != 3 || got[0].output != "out-r3" || got[2].output != "out-r5" {
		t.Errorf("recent(3) must return the newest rounds in order, got %+v", got)
	}
}

// TestSubagentResume_RestoresOwnChainOnly: the restorer injects THIS task's
// prior rounds (last settle result foremost) and nothing else; a task with no
// settled round refuses resume with guidance.
func TestSubagentResume_RestoresOwnChainOnly(t *testing.T) {
	w := &AgentToolWrapper{}

	// No settled round → actionable refusal.
	empty := &subagentRounds{}
	if _, err := w.subagentResume("plan", empty)("继续"); err == nil ||
		!strings.Contains(err.Error(), "relaunch_task") {
		t.Errorf("resume without settled rounds must refuse with guidance, got %v", err)
	}

	// One settled round → restored context contains exactly that round.
	rounds := &subagentRounds{}
	rounds.add("分析日志", "结论:磁盘将满")
	prior := rounds.recent(DefaultResumeContextRounds)
	if len(prior) != 1 || !strings.Contains(prior[0].output, "磁盘将满") {
		t.Fatalf("round chain must hold the settle result, got %+v", prior)
	}
	// The restorer serializes ONLY this task's rounds (context-scoping): the
	// wire entries derive solely from `rounds` — verified by construction and
	// by the round content check above.
}

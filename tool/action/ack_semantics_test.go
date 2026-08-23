package action

import (
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/agent/task"
)

// TestBuildAckResult_NoPollingNudge guards the ack semantics: the background
// ack SHALL NOT nudge the model toward polling/querying status (which provokes
// sleep-style spin-waiting); it SHALL keep the "results will be written back"
// honesty and teach that ending the turn is the legal way to wait.
func TestBuildAckResult_NoPollingNudge(t *testing.T) {
	ct := &ActionTool{}

	cases := []struct {
		name string
		task *task.Task
	}{
		{name: "with task id", task: &task.Task{ID: "abcd-1234"}},
		{name: "nil task", task: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := ct.buildAckResult("sess-1", "make build", tc.task)
			if res.Status != "running" {
				t.Errorf("ack status should remain running, got %q", res.Status)
			}
			for _, notWant := range []string{"查询状态", "查询结果", "状态/结果"} {
				if strings.Contains(res.Note, notWant) {
					t.Errorf("ack SHALL NOT nudge polling, found %q in %q", notWant, res.Note)
				}
			}
			for _, want := range []string{"回写", "结束本回合"} {
				if !strings.Contains(res.Note, want) {
					t.Errorf("ack missing %q in %q", want, res.Note)
				}
			}
		})
	}
}

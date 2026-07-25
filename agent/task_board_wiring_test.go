package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestInjectLiveTaskBoard_WiredAfterConstruction is the regression guard for the
// task-board-injection-order bug: taskController is wired AFTER ContextManager
// construction (agent.go), so the board callback must nil-check at CALL time,
// not registration time. With a live task present, the board must inject.
func TestInjectLiveTaskBoard_WiredAfterConstruction(t *testing.T) {
	cm := &ContextManager{} // taskController nil at construction (mirrors real order)
	cm.taskController = &fakeTaskController{tasks: []*Task{
		task.NewTaskFixture("t1", "npm build", TaskRunning, time.Now()),
	}} // post-construction wiring

	args := &model.BeforeModelArgs{Request: &model.Request{
		Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}},
	}}
	cm.injectLiveTaskBoard(args)

	var found bool
	for _, m := range args.Request.Messages {
		if strings.Contains(m.Content, "后台任务看板") {
			found = true
		}
	}
	if !found {
		t.Error("board must inject even when taskController is wired after construction")
	}
}

// TestInjectLiveTaskBoard_NilControllerSafe: a nil taskController at call time is
// a safe no-op (no panic, nothing injected).
func TestInjectLiveTaskBoard_NilControllerSafe(t *testing.T) {
	cm := &ContextManager{}
	args := &model.BeforeModelArgs{Request: &model.Request{
		Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}},
	}}
	cm.injectLiveTaskBoard(args)
	if len(args.Request.Messages) != 1 {
		t.Errorf("nil taskController should inject nothing, got %d msgs", len(args.Request.Messages))
	}
}

package task

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
)

func ctxWithTM(tm *agent.TaskManager) context.Context {
	return agent.WithTaskSpawner(context.Background(), tm)
}

// blockingDetector never settles until cancelled — keeps a task active. Its
// short dense window makes Spawn ack promptly (task stays running in background).
func blockingDetector() agent.SettleDetector {
	return agent.NewFuncSettleDetector(context.Background(), func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}, 20*time.Millisecond)
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// TestListTasksTool lists all tracked tasks.
func TestListTasksTool(t *testing.T) {
	tm := agent.NewTaskManager(agent.TaskManagerConfig{}) // detach window via blockingDetector
	r1 := tm.Spawn(agent.TaskSpec{Kind: "command", Desc: "cmd A"}, blockingDetector())
	r2 := tm.Spawn(agent.TaskSpec{Kind: "command", Desc: "cmd B"}, blockingDetector())
	defer tm.Cancel(r1.Task.ID)
	defer tm.Cancel(r2.Task.ID)

	out, err := NewListTasksTool().Call(ctxWithTM(tm), nil)
	if err != nil {
		t.Fatal(err)
	}
	s := out.(string)
	if !strings.Contains(s, "cmd A") || !strings.Contains(s, "cmd B") {
		t.Errorf("list should contain both tasks:\n%s", s)
	}
}

// TestGetTaskResultTool returns a settled task's full result.
func TestGetTaskResultTool(t *testing.T) {
	tm := agent.NewTaskManager(agent.TaskManagerConfig{})
	det := agent.NewFuncSettleDetector(context.Background(), func(context.Context) (string, error) {
		return "the full result", nil
	})
	res := tm.Spawn(agent.TaskSpec{Kind: "command", Desc: "cmd X"}, det)
	if !res.Settled {
		t.Fatalf("expected inline settle")
	}

	out, err := NewGetTaskResultTool().Call(ctxWithTM(tm), mustJSON(t, map[string]string{"task_id": res.Task.ID}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.(string), "the full result") {
		t.Errorf("get_task_result should return full result, got: %s", out)
	}
}

// TestGetTaskResultTool_NotFound reports a missing task.
func TestGetTaskResultTool_NotFound(t *testing.T) {
	tm := agent.NewTaskManager(agent.TaskManagerConfig{})
	out, err := NewGetTaskResultTool().Call(ctxWithTM(tm), mustJSON(t, map[string]string{"task_id": "nope"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.(string), "未找到") {
		t.Errorf("expected not-found message, got: %s", out)
	}
}

// TestCancelTaskTool cancels a running task.
func TestCancelTaskTool(t *testing.T) {
	tm := agent.NewTaskManager(agent.TaskManagerConfig{}) // detach window via blockingDetector
	res := tm.Spawn(agent.TaskSpec{Kind: "command", Desc: "svc"}, blockingDetector())

	out, err := NewCancelTaskTool().Call(ctxWithTM(tm), mustJSON(t, map[string]string{"task_id": res.Task.ID}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.(string), "已取消") {
		t.Errorf("expected cancel confirmation, got: %s", out)
	}
	if res.Task.Status() != agent.TaskCancelled {
		t.Errorf("task status = %s, want cancelled", res.Task.Status())
	}
}

// TestRelaunchTaskTool re-runs a task via its stored relaunch closure.
func TestRelaunchTaskTool(t *testing.T) {
	tm := agent.NewTaskManager(agent.TaskManagerConfig{}) // detach window via blockingDetector
	relaunched := make(chan struct{}, 1)
	spec := agent.TaskSpec{Kind: "command", Desc: "cmd R"}
	spec.Relaunch = func() (agent.SpawnResult, error) {
		relaunched <- struct{}{}
		return agent.SpawnResult{}, nil
	}
	res := tm.Spawn(spec, blockingDetector())
	defer tm.Cancel(res.Task.ID)

	if _, err := NewRelaunchTaskTool().Call(ctxWithTM(tm), mustJSON(t, map[string]string{"task_id": res.Task.ID})); err != nil {
		t.Fatal(err)
	}
	select {
	case <-relaunched:
	case <-time.After(time.Second):
		t.Fatal("relaunch closure was not invoked")
	}
}

// TestResolveTask_Prefix resolves a task by a unique id prefix (board shows short ids).
func TestResolveTask_Prefix(t *testing.T) {
	tm := agent.NewTaskManager(agent.TaskManagerConfig{}) // detach window via blockingDetector
	res := tm.Spawn(agent.TaskSpec{Kind: "command", Desc: "svc"}, blockingDetector())
	defer tm.Cancel(res.Task.ID)

	prefix := res.Task.ID[:8]
	tk, ok := resolveTask(tm, prefix)
	if !ok || tk.ID != res.Task.ID {
		t.Errorf("prefix %q should resolve to %s, got ok=%v", prefix, res.Task.ID, ok)
	}
}

// TestTools_NoController: without an injected controller, tools return a clear
// message rather than erroring.
func TestTools_NoController(t *testing.T) {
	ctx := context.Background() // no controller injected
	out, err := NewListTasksTool().Call(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.(string), "不可用") {
		t.Errorf("expected unavailable message, got: %s", out)
	}
}

// 本文件负责在途面板的呈现判据：只渲染在途任务、展示剩余寿命而不替模型仲裁、注入位置
// 固定在工具结果之后、无用户消息时不追加。
// 契约: docs/wiki/agent/task-lifecycle.md#board-rendering
package task

import (
	"strings"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

func mkBoardTask(id, desc string, st TaskStatus) *Task {
	return &Task{ID: id, Spec: TaskSpec{Desc: desc}, status: st, StartedAt: time.Now().Add(-5 * time.Second)}
}

// TestRenderTaskBoard_ActiveOnly 钉住 the board shows active tasks and ages out terminal ones (completed/failed/cancelled).
func TestRenderTaskBoard_ActiveOnly(t *testing.T) {
	tasks := []*Task{
		mkBoardTask("run-11111111", "npm run dev", TaskRunning),
		mkBoardTask("stab-22222222", "server :8080", TaskStable),
		mkBoardTask("done-33333333", "echo hi", TaskCompleted),
		mkBoardTask("fail-44444444", "bad cmd", TaskFailed),
		mkBoardTask("susp-55555555", "stuck proc", TaskSuspect),
	}
	board := RenderBoard(tasks, 10*time.Minute)
	if board == "" {
		t.Fatal("expected non-empty board")
	}
	for _, want := range []string{"npm run dev", "server :8080", "stuck proc", "running", "stable", "suspect", "3 个进行中"} {
		if !strings.Contains(board, want) {
			t.Errorf("board missing %q:\n%s", want, board)
		}
	}
	for _, notWant := range []string{"echo hi", "bad cmd", "completed", "failed"} {
		if strings.Contains(board, notWant) {
			t.Errorf("board should age out terminal task %q:\n%s", notWant, board)
		}
	}
}

// TestRenderTaskBoard_ShowsRemainingLifetime 钉住 每行展示回收器实际认定的剩余寿命，让模型读一次即可定夺。
// - 计算与回收器所用一致：显式寿命优先，否则套用传入的默认下限。
func TestRenderTaskBoard_ShowsRemainingLifetime(t *testing.T) {
	bounded := &Task{ID: "job-aaaaaaaa", Spec: TaskSpec{Desc: "big build", TTL: 30 * time.Minute}, status: TaskRunning, StartedAt: time.Now().Add(-10 * time.Minute)}
	board := RenderBoard([]*Task{bounded}, time.Hour)
	if !strings.Contains(board, "剩余 20m") {
		t.Errorf("explicit 30m TTL 10m into life must render ~20m remaining; got:\n%s", board)
	}

	floored := &Task{ID: "svc-bbbbbbbb", Spec: TaskSpec{Desc: "dev server"}, status: TaskRunning, StartedAt: time.Now().Add(-1 * time.Minute)}
	board2 := RenderBoard([]*Task{floored}, 10*time.Minute)
	if !strings.Contains(board2, "剩余 9m") {
		t.Errorf("unset TTL must render remaining vs the 10m floor (~9m at 1m in); got:\n%s", board2)
	}
}

// TestRenderTaskBoard_EmptyWhenNoActive 钉住 all-terminal registry → empty board (nothing injected).
func TestRenderTaskBoard_EmptyWhenNoActive(t *testing.T) {
	tasks := []*Task{
		mkBoardTask("d", "x", TaskCompleted),
		mkBoardTask("c", "y", TaskCancelled),
	}
	if got := RenderBoard(tasks, 10*time.Minute); got != "" {
		t.Errorf("expected empty board, got %q", got)
	}
}

// TestInjectTaskBoard_AppendAtTail 钉住 面板追加在当前输入之后：字节会变的内容必须严格待在尾部。
// - 若插在最后一条用户消息之前，每次模型调用都会打断回合内的缓存前缀。
func TestInjectTaskBoard_AppendAtTail(t *testing.T) {
	msgs := []model.Message{
		model.NewSystemMessage("sys"),
		{Role: model.RoleUser, Content: "hello"},
		{Role: model.RoleAssistant, Content: "hi"},
		{Role: model.RoleUser, Content: "do X"},
	}
	out := InjectBoard(msgs, "BOARD")
	if len(out) != len(msgs)+1 {
		t.Fatalf("expected +1 message, got %d", len(out))
	}
	if out[len(out)-1].Content != "BOARD" {
		t.Errorf("board must be the LAST message (tail), got %q", out[len(out)-1].Content)
	}
	for i := range msgs {
		if out[i].Role != msgs[i].Role || out[i].Content != msgs[i].Content {
			t.Errorf("message %d must be unchanged, got (%s,%q) vs (%s,%q)",
				i, out[i].Role, out[i].Content, msgs[i].Role, msgs[i].Content)
		}
	}
}

// TestInjectTaskBoard_TailAfterToolResults 钉住 回合中视图以工具结果结尾，面板仍追加在其后：工具调用配对不变，缓存前缀覆盖整个回合内交换。
func TestInjectTaskBoard_TailAfterToolResults(t *testing.T) {
	msgs := []model.Message{
		model.NewSystemMessage("sys"),
		{Role: model.RoleUser, Content: "do X"},
		{Role: model.RoleAssistant, Content: "", ToolCalls: []model.ToolCall{{ID: "t1"}}},
		{Role: model.RoleTool, ToolID: "t1", Content: "ack"},
	}
	out := InjectBoard(msgs, "BOARD")
	if out[len(out)-1].Role != model.RoleUser || out[len(out)-1].Content != "BOARD" {
		t.Fatalf("board must be the last message, got %+v", out[len(out)-1])
	}
	if out[2].Role != model.RoleAssistant || out[3].Role != model.RoleTool || out[3].ToolID != "t1" {
		t.Errorf("tool-call pairing must be untouched, got %+v", out[2:4])
	}
}

// TestInjectTaskBoard_NoUserAppends: with no user message, the board appends.
func TestInjectTaskBoard_NoUserAppends(t *testing.T) {
	msgs := []model.Message{model.NewSystemMessage("sys")}
	out := InjectBoard(msgs, "BOARD")
	if out[len(out)-1].Content != "BOARD" {
		t.Errorf("board should append when there is no user message")
	}
}

// TestRenderTaskBoard_WaitGuidanceLine 钉住 有在途任务时，面板以固定的等待指引行结尾（结束回合，不去空转轮询）。
// - 没有在途任务时面板为空，也不出现该指引。
func TestRenderTaskBoard_WaitGuidanceLine(t *testing.T) {
	board := RenderBoard([]*Task{mkBoardTask("r-11111111", "long job", TaskRunning)}, 10*time.Minute)
	if board == "" {
		t.Fatal("expected non-empty board")
	}
	for _, want := range []string{"结束本回合", "自动唤醒", "sleep"} {
		if !strings.Contains(board, want) {
			t.Errorf("board guidance line missing %q:\n%s", want, board)
		}
	}
	if got := RenderBoard([]*Task{mkBoardTask("d", "x", TaskCompleted)}, 10*time.Minute); got != "" {
		t.Errorf("no-active board must be empty (no dangling guidance), got %q", got)
	}
}

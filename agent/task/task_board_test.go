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

// TestRenderTaskBoard_ActiveOnly: the board shows active tasks and ages out
// terminal ones (completed/failed/cancelled).
func TestRenderTaskBoard_ActiveOnly(t *testing.T) {
	tasks := []*Task{
		mkBoardTask("run-11111111", "npm run dev", TaskRunning),
		mkBoardTask("stab-22222222", "server :8080", TaskStable),
		mkBoardTask("done-33333333", "echo hi", TaskCompleted), // aged out
		mkBoardTask("fail-44444444", "bad cmd", TaskFailed),    // aged out
		mkBoardTask("susp-55555555", "stuck proc", TaskSuspect),
	}
	board := RenderBoard(tasks)
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

// TestRenderTaskBoard_EmptyWhenNoActive: all-terminal registry → empty board
// (nothing injected).
func TestRenderTaskBoard_EmptyWhenNoActive(t *testing.T) {
	tasks := []*Task{
		mkBoardTask("d", "x", TaskCompleted),
		mkBoardTask("c", "y", TaskCancelled),
	}
	if got := RenderBoard(tasks); got != "" {
		t.Errorf("expected empty board, got %q", got)
	}
}

// TestInjectTaskBoard_BeforeLastUser: the board is inserted right before the
// current input (last user message).
func TestInjectTaskBoard_BeforeLastUser(t *testing.T) {
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
	if out[len(out)-1].Content != "do X" {
		t.Errorf("last message should remain 'do X', got %q", out[len(out)-1].Content)
	}
	if out[len(out)-2].Content != "BOARD" {
		t.Errorf("board should precede the current input, got %q", out[len(out)-2].Content)
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

// TestRenderTaskBoard_WaitGuidanceLine: when active tasks exist the board ends
// with the fixed wait-guidance line (end-turn, no sleep spin); when there are
// no active tasks the board is empty and no guidance appears.
func TestRenderTaskBoard_WaitGuidanceLine(t *testing.T) {
	board := RenderBoard([]*Task{mkBoardTask("r-11111111", "long job", TaskRunning)})
	if board == "" {
		t.Fatal("expected non-empty board")
	}
	for _, want := range []string{"结束本回合", "自动唤醒", "sleep"} {
		if !strings.Contains(board, want) {
			t.Errorf("board guidance line missing %q:\n%s", want, board)
		}
	}
	if got := RenderBoard([]*Task{mkBoardTask("d", "x", TaskCompleted)}); got != "" {
		t.Errorf("no-active board must be empty (no dangling guidance), got %q", got)
	}
}

// ============================================================================
// Board injection wiring (async-result-delivery: task-board-injection-order fix)
// ============================================================================

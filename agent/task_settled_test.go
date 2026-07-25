package agent

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
)

// TestNewTaskSettledEvent_Content: a completed settle produces a self-contained
// external_input event (Source=task) carrying desc, id, status, and result.
func TestNewTaskSettledEvent_Content(t *testing.T) {
	task := &Task{ID: "task-abc", Spec: TaskSpec{Kind: "command", Desc: "npm run build"}}
	evt := newTaskSettledEvent(task, SettleSignal{Kind: SettleCompleted, Output: "build ok"})

	if evt.Type != tagentevent.TypeExternalInput {
		t.Errorf("type = %s, want external_input", evt.Type)
	}
	if evt.Source != SourceTask {
		t.Errorf("source = %s, want %s", evt.Source, SourceTask)
	}
	if evt.Message == nil {
		t.Fatal("nil message")
	}
	for _, want := range []string{"npm run build", "task-abc", "completed", "build ok"} {
		if !strings.Contains(evt.Message.Content, want) {
			t.Errorf("content missing %q: %s", want, evt.Message.Content)
		}
	}
}

// TestNewTaskSettledEvent_Failed: an error settle is reported as failed with the
// error text.
func TestNewTaskSettledEvent_Failed(t *testing.T) {
	task := &Task{ID: "t2", Spec: TaskSpec{Desc: "bad cmd"}}
	evt := newTaskSettledEvent(task, SettleSignal{Kind: SettleCompleted, Err: fmt.Errorf("boom")})
	if !strings.Contains(evt.Message.Content, "failed") || !strings.Contains(evt.Message.Content, "boom") {
		t.Errorf("failed event content = %q", evt.Message.Content)
	}
}

// TestNewTaskSettledEvent_LargeResultTruncated: large output is tail-truncated
// with a get_task_result hint.
func TestNewTaskSettledEvent_LargeResultTruncated(t *testing.T) {
	task := &Task{ID: "t3", Spec: TaskSpec{Desc: "big"}}
	large := strings.Repeat("x", 5000)
	evt := newTaskSettledEvent(task, SettleSignal{Kind: SettleCompleted, Output: large})
	if strings.Count(evt.Message.Content, "x") >= 5000 {
		t.Error("large output should be truncated inline")
	}
	if !strings.Contains(evt.Message.Content, "get_task_result") {
		t.Error("truncated result should hint get_task_result")
	}
}

// TestTaskManager_BackgroundSettle_PublishesTaskSettled: wiring OnSettle to the
// bus (as agent construction does) publishes a task_settled event when a task
// settles in the background (after its sync-wait window).
func TestTaskManager_BackgroundSettle_PublishesTaskSettled(t *testing.T) {
	bus := NewEventBus()
	tm := NewTaskManager(TaskManagerConfig{
		OnSettle: func(task *Task, sig SettleSignal) {
			bus.Publish(newTaskSettledEvent(task, sig))
		},
	})

	d := newManualDetectorDetach(40 * time.Millisecond) // defined in task_manager_test.go
	res := tm.Spawn(TaskSpec{Kind: "command", Desc: "long task"}, d)
	if res.Settled {
		t.Fatalf("expected ack (background), got inline settle")
	}

	// Background settle after the window closes.
	d.emit(SettleSignal{Kind: SettleCompleted, Output: "done later"})
	d.done()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, e := range bus.TryPull() {
			if e.Source == SourceTask {
				if e.Message == nil || !strings.Contains(e.Message.Content, "done later") {
					t.Errorf("task_settled content = %v", e.Message)
				}
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no task_settled event published to bus")
}

// TestBuildInvocation_IncludesTaskSettled: a task_settled event (Source=task)
// is included in the reclaimed turn's invocation message (not filtered like
// agent_output), so the LLM actually sees the settle. This is the deterministic
// proof of the reclaim path: loop Pull → BuildInvocation → RunFlow.
func TestBuildInvocation_IncludesTaskSettled(t *testing.T) {
	cm := &ContextManager{}
	evt := newTaskSettledEvent(&Task{ID: "t1", Spec: TaskSpec{Desc: "npm run build"}},
		SettleSignal{Kind: SettleCompleted, Output: "build done"})

	msg := cm.BuildInvocation([]*AgentEvent{evt})
	if !strings.Contains(msg.Content, "npm run build") || !strings.Contains(msg.Content, "build done") {
		t.Errorf("task_settled content should be included in invocation, got: %q", msg.Content)
	}
}

// TestNewTaskSettledEvent_CarriesOrigin: the settle event carries the task's
// opaque origin baggage (chat_id, ...), and the existing extractRootMetadata
// pipeline surfaces it — so a reclaimed turn's output routes to the origin.
func TestNewTaskSettledEvent_CarriesOrigin(t *testing.T) {
	task := &Task{ID: "t1", Spec: TaskSpec{Desc: "x", Origin: map[string]string{"chat_id": "u1", "user_name": "alice"}}}
	evt := newTaskSettledEvent(task, SettleSignal{Kind: SettleCompleted, Output: "done"})
	if evt.Metadata["chat_id"] != "u1" {
		t.Errorf("settle event missing origin chat_id: %v", evt.Metadata)
	}
	md := extractRootMetadata([]*AgentEvent{evt})
	if md["chat_id"] != "u1" || md["user_name"] != "alice" {
		t.Errorf("extractRootMetadata should surface origin baggage, got %v", md)
	}
}

// TestNewTaskSettledEvent_NoOriginSafe: a task with no Origin yields an event
// with no routing metadata (regression guard).
func TestNewTaskSettledEvent_NoOriginSafe(t *testing.T) {
	task := &Task{ID: "t2", Spec: TaskSpec{Desc: "x"}}
	evt := newTaskSettledEvent(task, SettleSignal{Kind: SettleCompleted})
	if len(evt.Metadata) != 0 {
		t.Errorf("no-origin task should yield empty metadata, got %v", evt.Metadata)
	}
}

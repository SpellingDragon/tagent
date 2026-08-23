package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"

	tagentevent "github.com/SpellingDragon/tagent/event"
)

// TestNewTaskSettledEvent_Content: a completed settle produces a self-contained
// external_input event (Source=tk) carrying desc, id, status, and result
// (small results stay inline, spillover disabled).
func TestNewTaskSettledEvent_Content(t *testing.T) {
	tk := &task.Task{ID: "tk-abc", Spec: task.TaskSpec{Kind: "command", Desc: "npm run build"}}
	evt := newTaskSettledEvent(tk, task.SettleSignal{Kind: task.SettleCompleted, Output: "build ok"}, 0, "")

	if evt.Type != tagentevent.TypeExternalInput {
		t.Errorf("type = %s, want external_input", evt.Type)
	}
	if evt.Source != SourceTask {
		t.Errorf("source = %s, want %s", evt.Source, SourceTask)
	}
	if evt.Message == nil {
		t.Fatal("nil message")
	}
	for _, want := range []string{"npm run build", "tk-abc", "completed", "build ok"} {
		if !strings.Contains(evt.Message.Content, want) {
			t.Errorf("content missing %q: %s", want, evt.Message.Content)
		}
	}
}

// TestNewTaskSettledEvent_Failed: an error settle is reported as failed with the
// error text.
func TestNewTaskSettledEvent_Failed(t *testing.T) {
	tk := &task.Task{ID: "t2", Spec: task.TaskSpec{Desc: "bad cmd"}}
	evt := newTaskSettledEvent(tk, task.SettleSignal{Kind: task.SettleCompleted, Err: fmt.Errorf("boom")}, 0, "")
	if !strings.Contains(evt.Message.Content, "failed") || !strings.Contains(evt.Message.Content, "boom") {
		t.Errorf("failed event content = %q", evt.Message.Content)
	}
}

// TestNewTaskSettledEvent_LargeResultSpills (stable-context-compaction D1
// revised): oversized results align with the sync-path spillover trio — full
// body written to the tool-output dir, event Content = tail + file-path
// ticket (the event body stays BOUNDED, so recalling it can never re-inject
// the oversized result; consumption goes through read_file paging).
func TestNewTaskSettledEvent_LargeResultSpills(t *testing.T) {
	dir := t.TempDir()
	tk := &task.Task{ID: "t3-large", Spec: task.TaskSpec{Desc: "big"}}
	large := strings.Repeat("x", 5000)
	evt := newTaskSettledEvent(tk, task.SettleSignal{Kind: task.SettleCompleted, Output: large}, 1000, dir)
	content := evt.Message.Content

	if !strings.Contains(content, "output_spilled") || !strings.Contains(content, "已保存到:") {
		t.Errorf("oversized settle must carry the spill ticket, got: %s", truncateForTest(content, 200))
	}
	if strings.Count(content, "x") >= 5000 {
		t.Error("event Content must stay bounded (tail only, not the full body)")
	}
	// The spilled file holds the full body (read_file paging target).
	matches, _ := filepath.Glob(filepath.Join(dir, "task-t3-large-*.txt"))
	if len(matches) != 1 {
		t.Fatalf("expected one spilled file under %s, got %v", dir, matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil || string(data) != large {
		t.Errorf("spilled file must hold the full result (err=%v, len=%d)", err, len(data))
	}
	// The ticket path inside the notice points at the spilled file (tasks 1.4:
	// recall returns exactly this bounded Content — ticket reachable).
	if !strings.Contains(content, matches[0]) {
		t.Errorf("notice ticket must carry the spilled file path %s", matches[0])
	}
}

// TestNewTaskSettledEvent_SpillDisabledInline: spillover disabled (maxChars=0)
// keeps the result fully inline (small-result / test behavior).
func TestNewTaskSettledEvent_SpillDisabledInline(t *testing.T) {
	tk := &task.Task{ID: "t4", Spec: task.TaskSpec{Desc: "x"}}
	body := strings.Repeat("y", 3000)
	evt := newTaskSettledEvent(tk, task.SettleSignal{Kind: task.SettleCompleted, Output: body}, 0, "")
	if strings.Contains(evt.Message.Content, "output_spilled") || strings.Count(evt.Message.Content, "y") != 3000 {
		t.Error("spillover disabled must keep the result fully inline")
	}
}

func truncateForTest(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// TestTaskManager_BackgroundSettle_PublishesTaskSettled: wiring OnSettle to the
// bus (as agent construction does) publishes a task_settled event when a task
// settles in the background (after its sync-wait window).
func TestTaskManager_BackgroundSettle_PublishesTaskSettled(t *testing.T) {
	bus := NewEventBus()
	tm := task.NewTaskManager(task.TaskManagerConfig{
		OnSettle: func(tk *task.Task, sig task.SettleSignal) {
			bus.Publish(newTaskSettledEvent(tk, sig, 0, ""))
		},
	})

	d := task.NewManualDetectorDetach(40 * time.Millisecond) // defined in task_manager_test.go
	res := tm.Spawn(task.TaskSpec{Kind: "command", Desc: "long task"}, d)
	if res.Settled {
		t.Fatalf("expected ack (background), got inline settle")
	}

	// Background settle after the window closes.
	d.Emit(task.SettleSignal{Kind: task.SettleCompleted, Output: "done later"})
	d.Done()

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

// TestBuildInvocation_IncludesTaskSettled: a task_settled event (Source=tk)
// is included in the reclaimed turn's invocation message (not filtered like
// agent_output), so the LLM actually sees the settle. This is the deterministic
// proof of the reclaim path: loop Pull → BuildInvocation → RunFlow.
func TestBuildInvocation_IncludesTaskSettled(t *testing.T) {
	cm := &ContextManager{}
	evt := newTaskSettledEvent(&task.Task{ID: "t1", Spec: task.TaskSpec{Desc: "npm run build"}},
		task.SettleSignal{Kind: task.SettleCompleted, Output: "build done"}, 0, "")

	msg := cm.BuildInvocation([]*AgentEvent{evt})
	if !strings.Contains(msg.Content, "npm run build") || !strings.Contains(msg.Content, "build done") {
		t.Errorf("task_settled content should be included in invocation, got: %q", msg.Content)
	}
}

// TestNewTaskSettledEvent_CarriesOrigin: the settle event carries the task's
// opaque origin baggage (chat_id, ...), and the existing extractRootMetadata
// pipeline surfaces it — so a reclaimed turn's output routes to the origin.
func TestNewTaskSettledEvent_CarriesOrigin(t *testing.T) {
	tk := &task.Task{ID: "t1", Spec: task.TaskSpec{Desc: "x", Origin: map[string]string{"chat_id": "u1", "user_name": "alice"}}}
	evt := newTaskSettledEvent(tk, task.SettleSignal{Kind: task.SettleCompleted, Output: "done"}, 0, "")
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
	tk := &task.Task{ID: "t2", Spec: task.TaskSpec{Desc: "x"}}
	evt := newTaskSettledEvent(tk, task.SettleSignal{Kind: task.SettleCompleted}, 0, "")
	if len(evt.Metadata) != 0 {
		t.Errorf("no-origin task should yield empty metadata, got %v", evt.Metadata)
	}
}

// TestNewTaskSettledEvent_SingleLineTrajectory (context-efficiency-and-
// trajectory D2/D3): the settle body is a compact SINGLE-LINE trajectory form —
// no embedded newlines / standalone UUID lines, markers per status, and
// information-lossless (desc + short id + status + result all present).
func TestNewTaskSettledEvent_SingleLineTrajectory(t *testing.T) {
	tk := &task.Task{ID: "abcd1234ef", Spec: task.TaskSpec{Desc: "并发抓取 4 篇资料进 knowledge_base"}}
	evt := newTaskSettledEvent(tk, task.SettleSignal{Kind: task.SettleCompleted, Output: "line1\nline2\nline3"}, 0, "")
	c := evt.Message.Content

	if strings.Contains(c, "\n") {
		t.Errorf("settle body must be single-line (newlines escaped), got: %q", c)
	}
	for _, want := range []string{"✓", "并发抓取", "id=abcd1234", "completed", "line1␤line2␤line3"} {
		if !strings.Contains(c, want) {
			t.Errorf("single-line settle missing %q: %q", want, c)
		}
	}
	// No standalone full-UUID line / blank-line padding from the old format.
	if strings.Contains(c, "abcd1234ef\n") || strings.Contains(c, "\n\n") {
		t.Errorf("old multi-line layout leaked: %q", c)
	}
}

// TestNewTaskSettledEvent_StatusMarkers: failed / alive-detached / suspect map
// to distinct markers + status words (trajectory legibility).
func TestNewTaskSettledEvent_StatusMarkers(t *testing.T) {
	tk := &task.Task{ID: "m1", Spec: task.TaskSpec{Desc: "d"}}
	cases := []struct {
		name   string
		sig    task.SettleSignal
		marker string
		word   string
	}{
		{name: "failed", sig: task.SettleSignal{Err: fmt.Errorf("e")}, marker: "✗", word: "failed"},
		{name: "alive", sig: task.SettleSignal{Kind: task.SettleStable}, marker: "∞", word: "alive-detached"},
		{name: "suspect", sig: task.SettleSignal{Kind: task.SettleSuspect}, marker: "⚠", word: "suspect"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTaskSettledEvent(tk, tc.sig, 0, "").Message.Content
			if !strings.Contains(c, tc.marker) || !strings.Contains(c, tc.word) {
				t.Errorf("%s settle missing marker %q or word %q: %q", tc.name, tc.marker, tc.word, c)
			}
		})
	}
}

// TestNewTaskSettledEvent_InfoLosslessSpilled: an oversized settle keeps the
// lossless fields (desc / short id / status) AND a spill ticket with path +
// tail preview, all on one line.
func TestNewTaskSettledEvent_InfoLosslessSpilled(t *testing.T) {
	dir := t.TempDir()
	tk := &task.Task{ID: "big-9999", Spec: task.TaskSpec{Desc: "长任务描述"}}
	large := strings.Repeat("z", 5000)
	c := newTaskSettledEvent(tk, task.SettleSignal{Kind: task.SettleCompleted, Output: large}, settleInlineCapChars, dir).Message.Content
	for _, want := range []string{"✓", "长任务描述", "id=big-9999", "completed", "output_spilled", "已保存到:", "尾部:"} {
		if !strings.Contains(c, want) {
			t.Errorf("spilled settle missing %q: %q", want, truncateForTest(c, 300))
		}
	}
	if strings.Contains(c, "\n") {
		t.Errorf("spilled settle must stay single-line, got: %q", truncateForTest(c, 200))
	}
}

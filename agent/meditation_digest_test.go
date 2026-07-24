package agent

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// mkDigestTask builds a Task in a specific status for digest tests (white-box:
// status is package-private).
func mkDigestTask(id, desc string, st TaskStatus, age time.Duration) *Task {
	t := &Task{ID: id, Spec: TaskSpec{Desc: desc}, StartedAt: time.Now().Add(-age)}
	t.status = st
	return t
}

// fakeTaskController implements TaskController; only List returns data.
type fakeTaskController struct{ tasks []*Task }

func (f *fakeTaskController) Spawn(TaskSpec, SettleDetector) SpawnResult { return SpawnResult{} }
func (f *fakeTaskController) List() []*Task                             { return f.tasks }
func (f *fakeTaskController) Get(string) (*Task, bool)                  { return nil, false }
func (f *fakeTaskController) Cancel(string) bool                        { return false }
func (f *fakeTaskController) Relaunch(string) (SpawnResult, error)      { return SpawnResult{}, nil }

func TestRenderSelfStateDigest_EmptyDegrades(t *testing.T) {
	if got := renderSelfStateDigest(nil, time.Hour); got != "" {
		t.Errorf("nil tasks → empty digest, got %q", got)
	}
	if got := renderSelfStateDigest([]*Task{}, time.Hour); got != "" {
		t.Errorf("empty slice → empty digest, got %q", got)
	}
}

func TestRenderSelfStateDigest_CountsAndAttention(t *testing.T) {
	tasks := []*Task{
		mkDigestTask("aaaaaaaa11", "run a", TaskRunning, time.Minute),
		mkDigestTask("bbbbbbbb11", "svc b", TaskAliveDetached, time.Hour),
		mkDigestTask("cccccccc11", "stuck c", TaskSuspect, 2*time.Minute),
		mkDigestTask("dddddddd11", "dead d", TaskDead, 5*time.Minute),
	}
	got := renderSelfStateDigest(tasks, 90*time.Minute)

	for _, want := range []string{"running=1", "alive_detached=1", "suspect=1", "dead=1", "空闲时长", "需关注", "stuck c", "dead d"} {
		if !strings.Contains(got, want) {
			t.Errorf("digest missing %q:\n%s", want, got)
		}
	}
	// Healthy tasks are counted but not listed as attention detail.
	if strings.Contains(got, "run a") {
		t.Errorf("running task should not appear in attention detail:\n%s", got)
	}
}

func TestRenderSelfStateDigest_BoundedAttention(t *testing.T) {
	var tasks []*Task
	overflow := 5
	for i := 0; i < digestMaxAttentionDetail+overflow; i++ {
		tasks = append(tasks, mkDigestTask(fmt.Sprintf("id%03d", i), fmt.Sprintf("t%d", i), TaskSuspect, time.Duration(i)*time.Minute))
	}
	got := renderSelfStateDigest(tasks, time.Minute)

	if lines := strings.Count(got, "  - ["); lines != digestMaxAttentionDetail {
		t.Errorf("attention detail lines = %d, want %d", lines, digestMaxAttentionDetail)
	}
	if !strings.Contains(got, fmt.Sprintf("另有 %d 条", overflow)) {
		t.Errorf("overflow summary missing:\n%s", got)
	}
}

func TestRenderSelfStateDigest_TruncatesLongDesc(t *testing.T) {
	long := strings.Repeat("字", 100)
	got := renderSelfStateDigest([]*Task{mkDigestTask("x", long, TaskSuspect, time.Minute)}, time.Minute)
	if !strings.Contains(got, "…") {
		t.Errorf("long desc should be rune-truncated with ellipsis:\n%s", got)
	}
}

// TestMeditation_DigestPresentBeforePrompt: with a task controller, the
// meditation message carries the digest before the prompt (task 4.1).
func TestMeditation_DigestPresentBeforePrompt(t *testing.T) {
	mgr := NewMeditationManager(MeditationConfig{PromptText: "REFLECT_NOW"}, &mockMessageInjector{})
	mgr.SetTaskController(&fakeTaskController{tasks: []*Task{
		mkDigestTask("id1", "stuck task", TaskSuspect, time.Minute),
	}})

	msg := mgr.buildMeditationMessage(time.Now(), time.Hour)

	if !strings.Contains(msg.Content, "自我状态快照") || !strings.Contains(msg.Content, "stuck task") {
		t.Errorf("digest missing from meditation message:\n%s", msg.Content)
	}
	if strings.Index(msg.Content, "自我状态快照") > strings.Index(msg.Content, "REFLECT_NOW") {
		t.Errorf("digest should appear BEFORE the prompt:\n%s", msg.Content)
	}
}

// TestMeditation_NoDigestWhenNoController: without a task controller, behavior
// is unchanged — no digest section, prompt intact (task 4.1 / graceful degrade).
func TestMeditation_NoDigestWhenNoController(t *testing.T) {
	mgr := NewMeditationManager(MeditationConfig{PromptText: "REFLECT_NOW"}, &mockMessageInjector{})

	msg := mgr.buildMeditationMessage(time.Now(), time.Hour)

	if strings.Contains(msg.Content, "自我状态快照") {
		t.Errorf("no digest expected without a task controller:\n%s", msg.Content)
	}
	if !strings.Contains(msg.Content, "REFLECT_NOW") {
		t.Errorf("prompt should still be present:\n%s", msg.Content)
	}
}

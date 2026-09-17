package task

import (
	"testing"

	"github.com/SpellingDragon/tagent/event"
)

// Retirement-path settles must NOT ride the task's original (user) trigger
// lineage into the settle signal — otherwise a bookkeeping retirement is
// delivered back to the user as if it were their awaited result (leak,
// 2026-09-17 meditation fix). Lineage must be downgraded to "task-retired".
func TestFinalizeRetired_DowngradesLineage(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	var gotKind SettleKind
	var gotOrigin string
	tm.onSettle = func(tk *Task, sig SettleSignal) {
		gotKind = sig.Kind
		if b, ok := tk.Spec.Origin[event.MetaKeyTriggerSource]; ok {
			gotOrigin = string(b)
		}
	}
	tk := &Task{Spec: TaskSpec{Kind: "command", Desc: "test"}}
	tk.Spec.Origin = map[string]string{event.MetaKeyTriggerSource: "user"}
	tk.status = TaskRunning
	tm.finalizeRetired(tk, "(zombie retired: test)", nil)

	if gotKind != SettleFailed {
		t.Fatalf("kind = %v, want failed", gotKind)
	}
	if gotOrigin != "task-retired" {
		t.Fatalf("lineage = %q, want task-retired (downgraded, not user)", gotOrigin)
	}
	if tk.status != TaskFailed {
		t.Fatalf("status = %v, want failed", tk.status)
	}
}

// The normal finalize path must be untouched: user-spawned task completing
// keeps its original lineage (delivery gate may then deliver it).
func TestFinalize_NormalKeepsLineage(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	var gotOrigin string
	tm.onSettle = func(tk *Task, sig SettleSignal) {
		if b, ok := tk.Spec.Origin[event.MetaKeyTriggerSource]; ok {
			gotOrigin = string(b)
		}
	}
	tk := &Task{Spec: TaskSpec{Kind: "command", Desc: "test-normal"}}
	tk.Spec.Origin = map[string]string{event.MetaKeyTriggerSource: "user"}
	tk.status = TaskRunning
	tm.finalize(tk, SettleCompleted, "done", nil)

	if gotOrigin != "user" {
		t.Fatalf("lineage = %q, want user (normal path untouched)", gotOrigin)
	}
}

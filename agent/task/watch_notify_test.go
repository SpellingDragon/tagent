package task

import "testing"

// C1: a watch signal is a pure notification — no lifecycle state change, no
// alive-detached interference, always forwarded to onSettle.
func TestSettleWatch_NoStateChange(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})
	task := &Task{ID: "tw", status: TaskAliveDetached, aliveDetached: true}
	tm.emitBackground(task, SettleSignal{Kind: SettleWatch, Output: "watch pattern hit"})
	task.mu.Lock()
	defer task.mu.Unlock()
	if task.status != TaskAliveDetached {
		t.Fatalf("status changed to %v, want alive-detached preserved", task.status)
	}
}

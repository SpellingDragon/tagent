package agent

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// spawnAliveDetachedTask spawns a manual task and drives it to alive-detached
// (detach → background stable "ready").
func spawnAliveDetachedTask(t *testing.T, tm *TaskManager, resumeFn func(string) (SettleDetector, error)) (*Task, *manualDetector) {
	t.Helper()
	// Auto-detach quickly so Spawn returns an ack instead of blocking.
	det := newManualDetectorDetach(10 * time.Millisecond)
	res := tm.Spawn(TaskSpec{Kind: "command", Desc: "svc", ResumeFn: resumeFn}, det)
	if res.Settled {
		t.Fatalf("expected ack (not settled) before detach")
	}
	// Background stable → alive-detached.
	det.emit(SettleSignal{Kind: SettleStable, Output: "ready"})
	waitStatus(t, res.Task, TaskAliveDetached)
	return res.Task, det
}

func waitStatus(t *testing.T, task *Task, want TaskStatus) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if task.Status() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("task status = %s, want %s", task.Status(), want)
}

// TestResume_AliveToRunningAndSettle: the alive-detached → running edge, with
// the resumed round settling inline (within the new window) under the SAME
// task id.
func TestResume_AliveToRunningAndSettle(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})

	resumeDet := newManualDetector()
	var gotInput string
	task, _ := spawnAliveDetachedTask(t, tm, func(input string) (SettleDetector, error) {
		gotInput = input
		// Settle promptly so Resume returns inline (settle wins the window).
		go func() {
			time.Sleep(10 * time.Millisecond)
			resumeDet.emit(SettleSignal{Kind: SettleCompleted, Output: "round-2 output"})
		}()
		return resumeDet, nil
	})
	originalID := task.ID

	res, err := tm.Resume(task.ID, "make reload")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if gotInput != "make reload" {
		t.Errorf("ResumeFn must receive the input, got %q", gotInput)
	}
	if !res.Settled || res.Signal.Output != "round-2 output" {
		t.Errorf("resumed round should settle inline with increment output, got %+v", res)
	}
	if res.Task.ID != originalID {
		t.Errorf("resume must keep the SAME task id: %s vs %s", res.Task.ID, originalID)
	}
}

// TestResume_IllegalStates: running and terminal tasks reject resume with
// actionable messages; non-resumable tasks reject too.
func TestResume_IllegalStates(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})

	// Running task: resume with a never-settling detector that DOES detach,
	// so the first Resume returns an ack and leaves the task running.
	task, _ := spawnAliveDetachedTask(t, tm, func(string) (SettleDetector, error) {
		return newManualDetectorDetach(10 * time.Millisecond), nil
	})
	if _, err := tm.Resume(task.ID, "first"); err != nil {
		t.Fatalf("first resume: %v", err)
	}
	waitStatus(t, task, TaskRunning)

	if _, err := tm.Resume(task.ID, "second"); err == nil || !strings.Contains(err.Error(), "running") {
		t.Errorf("concurrent resume must be rejected with running-state message, got %v", err)
	}

	// Terminal task: completed/failed ARE legal source states for resume
	// (round-based executors like subagent continue with a new run). A
	// cancelled task is NOT — its session was killed.
	det2 := newManualDetectorDetach(10 * time.Millisecond)
	res2 := tm.Spawn(TaskSpec{Kind: "subagent", Desc: "quick", ResumeFn: func(string) (SettleDetector, error) {
		d := newManualDetector()
		go func() {
			time.Sleep(10 * time.Millisecond)
			d.emit(SettleSignal{Kind: SettleCompleted, Output: "round-2"})
		}()
		return d, nil
	}}, det2)
	_ = res2
	det2.emit(SettleSignal{Kind: SettleCompleted, Output: "done"})
	waitStatus(t, res2.Task, TaskCompleted)
	if _, err := tm.Resume(res2.Task.ID, "continue"); err != nil {
		t.Errorf("completed task must be resumable (new run), got %v", err)
	}
	waitStatus(t, res2.Task, TaskCompleted) // round-2 settled back to completed

	// Cancelled: rejected with relaunch guidance.
	tm.Cancel(res2.Task.ID)
	if _, err := tm.Resume(res2.Task.ID, "x"); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("cancelled resume must be rejected with guidance, got %v", err)
	}

	// Non-resumable (no ResumeFn).
	det3 := newManualDetectorDetach(10 * time.Millisecond)
	res3 := tm.Spawn(TaskSpec{Kind: "command", Desc: "svc2"}, det3)
	det3.emit(SettleSignal{Kind: SettleStable, Output: "ready"})
	waitStatus(t, res3.Task, TaskAliveDetached)
	if _, err := tm.Resume(res3.Task.ID, "x"); err == nil || !strings.Contains(err.Error(), "does not support resume") {
		t.Errorf("non-resumable task must be rejected, got %v", err)
	}
}

// TestResume_BackgroundSettleGoesToOnSettle: a resumed round that outlives the
// window emits its settle through the standard task_settled path with the
// same task id.
func TestResume_BackgroundSettleGoesToOnSettle(t *testing.T) {
	settled := make(chan *Task, 1)
	tm := NewTaskManager(TaskManagerConfig{
		OnSettle: func(task *Task, sig SettleSignal) {
			select {
			case settled <- task:
			default:
			}
		},
	})

	resumeDet := newManualDetectorDetach(20 * time.Millisecond)
	task, _ := spawnAliveDetachedTask(t, tm, func(string) (SettleDetector, error) {
		return resumeDet, nil
	})

	res, err := tm.Resume(task.ID, "slow op")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if res.Settled {
		t.Fatalf("expected ack for slow resumed round")
	}
	// Background completion → OnSettle with the SAME task.
	resumeDet.emit(SettleSignal{Kind: SettleCompleted, Output: "late result"})
	select {
	case got := <-settled:
		if got.ID != task.ID {
			t.Errorf("background settle must carry the same task id: %s vs %s", got.ID, task.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("background settle not delivered")
	}
}

// TestResume_CompletedSubagentTask: the subagent resume path — a completed
// task resumes with a new run under the same id (review finding: previously
// subagent resume was dead code since func detectors only emit completed).
func TestResume_CompletedSubagentTask(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})

	spec := TaskSpec{
		Kind: "subagent",
		Desc: "plan: analyze",
		ResumeFn: func(input string) (SettleDetector, error) {
			d := newManualDetector()
			go func() {
				time.Sleep(10 * time.Millisecond)
				d.emit(SettleSignal{Kind: SettleCompleted, Output: "continued: " + input})
			}()
			return d, nil
		},
	}
	det := newManualDetector()
	go func() {
		time.Sleep(10 * time.Millisecond)
		det.emit(SettleSignal{Kind: SettleCompleted, Output: "first done"})
	}()
	res := tm.Spawn(spec, det)
	if !res.Settled {
		t.Fatalf("first round should settle inline")
	}
	waitStatus(t, res.Task, TaskCompleted)

	res2, err := tm.Resume(res.Task.ID, "next instruction")
	if err != nil {
		t.Fatalf("resume completed subagent task: %v", err)
	}
	if !res2.Settled || res2.Signal.Output != "continued: next instruction" {
		t.Errorf("resumed round should settle inline with new-run output, got %+v", res2)
	}
	if res2.Task.ID != res.Task.ID {
		t.Errorf("same task id must be kept: %s vs %s", res2.Task.ID, res.Task.ID)
	}
}

// TestResume_ConcurrentSingleWinner: parallel resumes (parallel tool
// execution) single-win; the loser gets the running-state message and the
// ResumeFn runs exactly once.
func TestResume_ConcurrentSingleWinner(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})

	var fnMu sync.Mutex
	fnCalls := 0
	task, _ := spawnAliveDetachedTask(t, tm, func(string) (SettleDetector, error) {
		fnMu.Lock()
		fnCalls++
		fnMu.Unlock()
		time.Sleep(30 * time.Millisecond) // widen the race window
		d := newManualDetector()
		go func() {
			time.Sleep(5 * time.Millisecond)
			d.emit(SettleSignal{Kind: SettleCompleted, Output: "done"})
		}()
		return d, nil
	})

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = tm.Resume(task.ID, "input")
		}(i)
	}
	wg.Wait()

	fnMu.Lock()
	defer fnMu.Unlock()
	if fnCalls != 1 {
		t.Errorf("ResumeFn must run exactly once under concurrent resumes, got %d", fnCalls)
	}
	nErr := 0
	for _, e := range errs {
		if e != nil {
			nErr++
			if !strings.Contains(e.Error(), "running") {
				t.Errorf("loser error must mention running state, got %v", e)
			}
		}
	}
	if nErr != 1 {
		t.Errorf("exactly one resume must lose the race, errs=%v", errs)
	}
}

// TestResume_RetiresOldWatch: after resume, signals on the OLD detector must
// not route into the new round (no stale-signal mis-settle, no leaked watch
// goroutine consuming).
func TestResume_RetiresOldWatch(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{})

	newDet := newManualDetector()
	task, oldDet := spawnAliveDetachedTask(t, tm, func(string) (SettleDetector, error) {
		return newDet, nil // no detach → Resume blocks until a settle arrives
	})

	// Resume in a goroutine; it blocks on the new window.
	resumed := make(chan SpawnResult, 1)
	go func() {
		res, err := tm.Resume(task.ID, "x")
		if err != nil {
			t.Errorf("resume: %v", err)
		}
		resumed <- res
	}()
	waitStatus(t, task, TaskRunning)

	// A stale signal on the OLD detector must NOT settle the new round.
	oldDet.emit(SettleSignal{Kind: SettleCompleted, Output: "STALE"})
	select {
	case res := <-resumed:
		t.Fatalf("stale old-detector signal settled the new round: %+v", res)
	case <-time.After(100 * time.Millisecond):
	}

	// A signal on the NEW detector settles correctly.
	newDet.emit(SettleSignal{Kind: SettleCompleted, Output: "fresh"})
	select {
	case res := <-resumed:
		if res.Signal.Output != "fresh" {
			t.Errorf("new-round settle must carry fresh output, got %q", res.Signal.Output)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("new-round settle not delivered")
	}
}

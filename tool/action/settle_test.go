package action

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
)

// compile-time assertion: TmuxSettleDetector implements task.SettleDetector.
var _ task.SettleDetector = (*TmuxSettleDetector)(nil)

func TestStatusToSettle_Mapping(t *testing.T) {
	cases := []struct {
		status   SessionStatus
		wantKind task.SettleKind
		wantOK   bool
	}{
		{SessionCompleted, task.SettleCompleted, true},
		{SessionError, task.SettleCompleted, true},
		{SessionStable, task.SettleStable, true},
		{SessionTimedOut, task.SettleSuspect, true},
		{SessionRunning, "", false},
		{SessionFakeDead, "", false},
		{SessionFakeAlive, "", false},
	}
	for _, c := range cases {
		gotKind, gotOK := StatusToSettle(c.status)
		if gotKind != c.wantKind || gotOK != c.wantOK {
			t.Errorf("StatusToSettle(%s) = (%q,%v), want (%q,%v)", c.status, gotKind, gotOK, c.wantKind, c.wantOK)
		}
	}
}

// TestTmuxSettleDetector_CompletedClosesStream 钉住 a completed transition emits one completed signal and closes the stream.
//
// 契约: docs/wiki/tool/tool-architecture.md#action-tool
func TestTmuxSettleDetector_CompletedClosesStream(t *testing.T) {
	d := NewTmuxSettleDetector("s1", nil)
	d.OnStateChange(SessionRunning, "starting")
	d.OnStateChange(SessionCompleted, "done output")

	sig, ok := <-d.Settled()
	if !ok {
		t.Fatalf("expected a signal")
	}
	if sig.Kind != task.SettleCompleted || sig.Output != "done output" || sig.Err != nil {
		t.Errorf("unexpected signal: %+v", sig)
	}
	if _, ok := <-d.Settled(); ok {
		t.Errorf("stream should be closed after terminal status")
	}
}

// TestTmuxSettleDetector_StableThenCompleted 钉住 stable is non-terminal (stream stays open); a later completed closes it. Two signals in order.
func TestTmuxSettleDetector_StableThenCompleted(t *testing.T) {
	d := NewTmuxSettleDetector("s2", nil)
	d.OnStateChange(SessionStable, "listening on :8080")
	d.OnStateChange(SessionCompleted, "exited")

	first := <-d.Settled()
	if first.Kind != task.SettleStable || first.Output != "listening on :8080" {
		t.Errorf("first signal = %+v, want stable", first)
	}
	second := <-d.Settled()
	if second.Kind != task.SettleCompleted {
		t.Errorf("second signal = %+v, want completed", second)
	}
	if _, ok := <-d.Settled(); ok {
		t.Errorf("stream should be closed")
	}
}

// TestTmuxSettleDetector_ErrorCarriesErr: error → completed kind with Err set.
func TestTmuxSettleDetector_ErrorCarriesErr(t *testing.T) {
	d := NewTmuxSettleDetector("s3", nil)
	d.OnStateChange(SessionError, "boom")
	sig := <-d.Settled()
	if sig.Kind != task.SettleCompleted || sig.Err == nil {
		t.Errorf("error signal = %+v, want completed+err", sig)
	}
}

// TestTmuxSettleDetector_TimedOutSuspect: timed_out → suspect + close.
func TestTmuxSettleDetector_TimedOutSuspect(t *testing.T) {
	d := NewTmuxSettleDetector("s4", nil)
	d.OnStateChange(SessionTimedOut, "no output")
	sig := <-d.Settled()
	if sig.Kind != task.SettleSuspect {
		t.Errorf("signal = %+v, want suspect", sig)
	}
	if _, ok := <-d.Settled(); ok {
		t.Errorf("stream should be closed after timed_out")
	}
}

// TestTmuxSettleDetector_CancelClosesAndKills: Cancel invokes cancelFn and closes.
func TestTmuxSettleDetector_CancelClosesAndKills(t *testing.T) {
	killed := false
	d := NewTmuxSettleDetector("s5", func() { killed = true })
	d.Cancel()
	if !killed {
		t.Errorf("cancelFn not invoked")
	}
	if _, ok := <-d.Settled(); ok {
		t.Errorf("stream should be closed after cancel")
	}
}

// TestTmuxSettleDetector_DrivesTaskManager 钉住 the detector composes with the TaskManager sync-wait primitive (stable settle within window → inline).
func TestTmuxSettleDetector_DrivesTaskManager(t *testing.T) {
	tm := task.NewTaskManager(task.TaskManagerConfig{})
	d := NewTmuxSettleDetector("s6", nil, 500*time.Millisecond)
	go func() {
		d.OnStateChange(SessionStable, "ready")
	}()
	res := tm.Spawn(task.TaskSpec{Kind: "command", Desc: "svc"}, d)
	if !res.Settled || res.Signal.Kind != task.SettleStable {
		t.Errorf("expected inline stable settle, got %+v", res)
	}
	if res.Task.Status() != task.TaskStable {
		t.Errorf("task status = %s, want stable", res.Task.Status())
	}
	d.Cancel()
}

// TestTmuxSettleDetector_ReapsOnCompletion 钉住 a completed (process-dead) session is reaped (kill closure invoked) so dead sessions don't accumulate.
func TestTmuxSettleDetector_ReapsOnCompletion(t *testing.T) {
	reaped := false
	d := NewTmuxSettleDetector("s-reap", func() { reaped = true })
	d.OnStateChange(SessionCompleted, "done")
	if !reaped {
		t.Errorf("completed session should be reaped")
	}
}

// TestTmuxSettleDetector_NoReapWhileAlive 钉住 stable (alive) and timed_out (maybe alive) sessions are NOT auto-reaped.
func TestTmuxSettleDetector_NoReapWhileAlive(t *testing.T) {
	for _, st := range []SessionStatus{SessionStable, SessionTimedOut} {
		reaped := false
		d := NewTmuxSettleDetector("s-alive", func() { reaped = true })
		d.OnStateChange(st, "out")
		if reaped {
			t.Errorf("%s session should NOT be auto-reaped", st)
		}
		d.Cancel()
	}
}

// TestTmuxSettleDetector_ReapOnce: reaping runs at most once (completion then cancel).
func TestTmuxSettleDetector_ReapOnce(t *testing.T) {
	count := 0
	d := NewTmuxSettleDetector("s-once", func() { count++ })
	d.OnStateChange(SessionCompleted, "done")
	d.Cancel()
	if count != 1 {
		t.Errorf("reap should run exactly once, got %d", count)
	}
}

// TestTmuxSettleDetector_Rearm 钉住 探测器绑定在会话上：重挂只重置本轮状态——新的脱离窗口与新的输出基线。
// - 不替换探测器本身：既无重绑，也不留迟到信号的风险。
func TestTmuxSettleDetector_Rearm(t *testing.T) {
	d := NewTmuxSettleDetector("s1", nil, 30*time.Millisecond)

	select {
	case <-d.Detached():
	case <-time.After(2 * time.Second):
		t.Fatalf("round-1 detach must fire")
	}

	d.Rearm(2)
	select {
	case <-d.Detached():
		t.Fatalf("round-2 detach must NOT be closed immediately after Rearm")
	case <-time.After(5 * time.Millisecond):
	}

	d.OnStateChange(SessionStable, "old1\nold2\nnew1\nnew2")
	select {
	case sig := <-d.Settled():
		if sig.Kind != task.SettleStable || sig.Output != "new1\nnew2" {
			t.Errorf("round-2 settle must carry the post-baseline increment, got %+v", sig)
		}
	case <-time.After(time.Second):
		t.Fatalf("round-2 settle not delivered")
	}

	select {
	case <-d.Detached():
	case <-time.After(2 * time.Second):
		t.Fatalf("round-2 detach must fire after the re-armed dense window")
	}
}

// mustTmuxSession creates a real tmux session or skips the test when the
// environment cannot fork one. Returns the executor and session id.
func mustTmuxSession(t *testing.T, command string) (*TmuxExecutor, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("real tmux test; skip in -short")
	}
	if !IsTmuxAvailable() {
		t.Skip("tmux not available")
	}
	exec := NewTmuxExecutor()
	sess, err := exec.CreateSession(context.Background(), TmuxCreateOptions{Command: command})
	if err != nil {
		t.Skipf("cannot create tmux session (sandboxed / no PTY?): %v", err)
	}
	return exec, sess.ID
}

// TestTmuxSettleDetector_CancelReapsRealSession: Cancel() reaps the underlying
// tmux session, and is idempotent (a second Cancel does not error/panic). This
// is the reap primitive that prune relies on for session-resource reclaim.
func TestTmuxSettleDetector_CancelReapsRealSession(t *testing.T) {
	exec, id := mustTmuxSession(t, "sleep 30")
	defer func() { _ = exec.KillSession(id) }()

	detector := NewTmuxSettleDetector(id, func() { _ = exec.KillSession(id) })
	if !exec.SessionExists(id) {
		t.Fatal("session should exist before Cancel")
	}

	detector.Cancel()
	if exec.SessionExists(id) {
		t.Error("Cancel() must reap (kill) the tmux session")
	}
	detector.Cancel()
}

// TestTmuxSettleDetector_CompletedReapsRealSession 钉住 会话在终态迁移处自动收走（输出已捕获），已完成的命令会话不得堆积。
func TestTmuxSettleDetector_CompletedReapsRealSession(t *testing.T) {
	exec, id := mustTmuxSession(t, "sleep 30")
	defer func() { _ = exec.KillSession(id) }()

	detector := NewTmuxSettleDetector(id, func() { _ = exec.KillSession(id) })
	detector.OnStateChange(SessionCompleted, "done")

	if exec.SessionExists(id) {
		t.Error("a completed tmux session must be auto-reaped")
	}
}

// TestTaskManager_PruneReclaimsTmuxTask 钉住 端到端：真实 tmux 任务完成且宽限过后，回收既清掉登记条目也让 tmux 会话消失。
// - 回收触发的取消是幂等保险（会话在完成时已被收走），不是唯一的回收点。
func TestTaskManager_PruneReclaimsTmuxTask(t *testing.T) {
	exec, id := mustTmuxSession(t, "sleep 30")
	defer func() { _ = exec.KillSession(id) }()

	detector := NewTmuxSettleDetector(id, func() { _ = exec.KillSession(id) }, 150*time.Millisecond)
	tm := task.NewTaskManager(task.TaskManagerConfig{TerminalTTL: 100 * time.Millisecond})
	res := tm.Spawn(task.TaskSpec{Kind: "command", Desc: "sleep 30", Key: "reclaim-k1"}, detector)
	if res.Task == nil {
		t.Fatal("no task returned from Spawn")
	}
	taskID := res.Task.ID

	detector.OnStateChange(SessionCompleted, "done")

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if tk, ok := tm.Get(taskID); ok && tk.Status() == task.TaskCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if tk, ok := tm.Get(taskID); !ok || tk.Status() != task.TaskCompleted {
		t.Fatalf("task did not reach completed state")
	}
	if exec.SessionExists(id) {
		t.Error("completed tmux session should already be reaped")
	}

	time.Sleep(150 * time.Millisecond)
	if got := tm.List(); len(got) != 0 {
		t.Errorf("prune should have removed the exited task, got %d", len(got))
	}
	if _, ok := tm.Get(taskID); ok {
		t.Error("prune should remove the exited task entry (memory reclaim)")
	}
	if exec.SessionExists(id) {
		t.Error("tmux session must not exist after completion + prune")
	}
}

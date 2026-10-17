package action

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
)

func TestActionTool_CloseStopsTmuxMonitor(t *testing.T) {
	ct := NewActionTool(WithOrphanCleanupDisabled())
	if ct.tmuxMonitor == nil {
		t.Skip("tmux not available, skipping")
	}

	ct.tmuxMonitor.Start()
	if !ct.tmuxMonitor.IsRunning() {
		t.Fatal("expected monitor to be running after Start()")
	}

	if err := ct.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}

	if ct.tmuxMonitor.IsRunning() {
		t.Error("expected monitor to be stopped after Close()")
	}
}

func TestActionTool_CloseIdempotent(t *testing.T) {
	ct := NewActionTool(WithOrphanCleanupDisabled())
	if ct.tmuxMonitor == nil {
		t.Skip("tmux not available, skipping")
	}

	ct.tmuxMonitor.Start()

	for i := 0; i < 3; i++ {
		if err := ct.Close(); err != nil {
			t.Fatalf("Close() call %d returned error: %v", i, err)
		}
	}

	if ct.tmuxMonitor.IsRunning() {
		t.Error("expected monitor to be stopped after multiple Close() calls")
	}
}

func TestActionTool_CloseWithoutMonitor(t *testing.T) {
	ct := &ActionTool{residentMetaDirOverride: t.TempDir()}
	if err := ct.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}
}

func TestHandleFakeDead_KillRetry_SessionRetained(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	monitor := newTestMonitor(inspector)
	session := newTestSession("quiet", SessionFakeDead, "output")
	if monitor.handleFakeDead(session) {
		t.Fatal("quiet observation must retain monitoring")
	}
	if inspector.killSessionCalls != 0 || session.Status != SessionTimedOut {
		t.Fatal("quiet observation must never kill")
	}
}

func TestHandleFakeDead_KillSucceeds_FirstTry(t *testing.T) {
	inspector := &mockInspector{processExists: true}
	monitor := newTestMonitor(inspector)
	session := newTestSession("quiet", SessionFakeDead, "output")
	if monitor.handleFakeDead(session) {
		t.Fatal("quiet observation must retain monitoring")
	}
	if inspector.killSessionCalls != 0 || session.Status != SessionTimedOut {
		t.Fatal("quiet observation must never kill")
	}
}

func TestHandleFakeDead_Integration_RetryCycle(t *testing.T) {
	assertQuietObservation(t, ModeInteractive, false)
}

// TestRotatePipeFile_CopyTruncate 钉住 超限的管道日志按复制截断方式轮转：内容移到带 .1 后缀的文件，活文件清空。
// - 管道面板以追加方式持有该文件描述符，清空后仍从偏移零续写；
// - 未达阈值或文件缺失时是空操作。
func TestRotatePipeFile_CopyTruncate(t *testing.T) {
	old := pipeRotateBytes
	pipeRotateBytes = 100
	defer func() { pipeRotateBytes = old }()

	dir := t.TempDir()
	pf := filepath.Join(dir, "pipe.log")
	big := strings.Repeat("x", 250)
	if err := os.WriteFile(pf, []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}

	rotatePipeFile(pf)

	rot, err := os.ReadFile(pf + ".1")
	if err != nil {
		t.Fatalf(".1 missing: %v", err)
	}
	if string(rot) != big {
		t.Fatalf(".1 content mismatch: %d bytes", len(rot))
	}
	live, err := os.Stat(pf)
	if err != nil {
		t.Fatal(err)
	}
	if live.Size() != 0 {
		t.Fatalf("live file not truncated: %d bytes", live.Size())
	}

	os.WriteFile(pf, []byte("small"), 0o600)
	os.Remove(pf + ".1")
	rotatePipeFile(pf)
	if _, err := os.Stat(pf + ".1"); !os.IsNotExist(err) {
		t.Fatal("under-threshold file must not rotate")
	}

	rotatePipeFile(filepath.Join(dir, "absent.log"))
}

// TestDetector_Watch_EmitsSettleWatch 钉住 观察模式在探测器上的接线：命中即发观察信号并携带累计计数。
// - 合并窗口折叠密集命中；未配观察则不发任何信号。
//
// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
func TestDetector_Watch_EmitsSettleWatch(t *testing.T) {
	d := NewTmuxSettleDetector("w-test", nil, time.Hour)
	defer d.close()
	if err := d.SetWatch(`ERROR|panic`, 5*time.Second); err != nil {
		t.Fatalf("SetWatch: %v", err)
	}
	d.OnWatchOutput("boot ok\n")
	select {
	case sig := <-d.Settled():
		t.Fatalf("no signal expected before any match, got %+v", sig)
	default:
	}
	d.OnWatchOutput("doing work\nERROR: disk full\npanic: runtime\n")
	select {
	case sig := <-d.Settled():
		if sig.Kind != task.SettleWatch {
			t.Fatalf("kind = %q, want watch", sig.Kind)
		}
		if !strings.Contains(sig.Output, "x2") {
			t.Fatalf("output %q should report x2 hits", sig.Output)
		}
	default:
		t.Fatal("expected a watch signal after matching output")
	}
}

// TestDetector_Watch_MergeWindow 钉住 合并窗口内的命中折叠为一个待决信号（计数在下一次发出时增长），日志洪水不会引发唤醒风暴。
func TestDetector_Watch_MergeWindow(t *testing.T) {
	d := NewTmuxSettleDetector("w-merge", nil, time.Hour)
	defer d.close()
	if err := d.SetWatch("ERR", 10*time.Second); err != nil {
		t.Fatalf("SetWatch: %v", err)
	}
	d.OnWatchOutput("ERR one\n")
	select {
	case sig := <-d.Settled():
		if !strings.Contains(sig.Output, "x1") {
			t.Fatalf("first signal should be x1, got %q", sig.Output)
		}
	default:
		t.Fatal("expected first signal")
	}
	d.OnWatchOutput("ERR two\nERR three\n")
	select {
	case sig := <-d.Settled():
		t.Fatalf("merged hit must not emit immediately, got %+v", sig)
	default:
	}
	d.OnWatchOutput("more output\n")
	select {
	case sig := <-d.Settled():
		t.Fatalf("no-hit refresh must not emit, got %+v", sig)
	default:
	}
	time.Sleep(20 * time.Millisecond)
	d.mu.Lock()
	d.watchLast = time.Now().Add(-15 * time.Second)
	d.mu.Unlock()
	d.OnWatchOutput("ERR four\n")
	select {
	case sig := <-d.Settled():
		if !strings.Contains(sig.Output, "x4") || sig.Err != nil {
			t.Fatalf("expected cumulative 4, got %q / %v", sig.Output, sig.Err)
		}
	default:
		t.Fatal("expected post-window signal")
	}
}

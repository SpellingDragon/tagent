package action

import (
	"fmt"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"
)

// TestDetect_DeathNonZeroExitIsError 钉住 D1：进程死亡且退出码非零 → SessionError。
func TestDetect_DeathNonZeroExitIsError(t *testing.T) {
	m := &mockInspector{output: "traceback...\n", processExists: false}
	m.setAlive3(false, true) // 确认死亡（known）
	m.setPaneExit(42, true)  // 退出码 42
	tm := newTestMonitor(m)
	session := newTestSession("d1", SessionRunning, "traceback...\n")

	require.Equal(t, SessionError, tm.detectSessionState(session))
}

// TestDetect_DeathZeroExitStaysCompleted 钉住 D1：干净退出（code 0）保持 completed 极性。
func TestDetect_DeathZeroExitStaysCompleted(t *testing.T) {
	m := &mockInspector{output: "done\n", processExists: false}
	m.setAlive3(false, true)
	m.setPaneExit(0, true)
	tm := newTestMonitor(m)
	session := newTestSession("d0", SessionRunning, "done\n")

	require.Equal(t, SessionCompleted, tm.detectSessionState(session))
}

// TestDetect_DeathUnresolvableCodeStaysCompleted 钉住：退出码不可辨（known=false）保持
// completed——unknown ≠ 失败（区别于探测失明路径）。
func TestDetect_DeathUnresolvableCodeStaysCompleted(t *testing.T) {
	m := &mockInspector{output: "x\n", processExists: false}
	m.setAlive3(false, true)
	m.setPaneExit(0, false) // 状态不可读
	tm := newTestMonitor(m)
	session := newTestSession("du", SessionRunning, "x\n")

	require.Equal(t, SessionCompleted, tm.detectSessionState(session))
}

// TestDetect_SignalDeathIsError 钉住信号死（负码）→ SessionError。
func TestDetect_SignalDeathIsError(t *testing.T) {
	m := &mockInspector{output: "killed\n", processExists: false}
	m.setAlive3(false, true)
	m.setPaneExit(-15, true)
	tm := newTestMonitor(m)
	session := newTestSession("ds", SessionRunning, "killed\n")

	require.Equal(t, SessionError, tm.detectSessionState(session))
}

// TestDetect_ProbeUnresolvableLimitIsError 钉住 D3：探测连续不可辨超限 → SessionError
// （框架失明不再伪装成功）。
func TestDetect_ProbeUnresolvableLimitIsError(t *testing.T) {
	m := &mockInspector{output: "partial\n"}
	m.setAlive3Err(true) // SessionAlive3 返回 known=false
	tm := newTestMonitor(m)
	session := newTestSession("pu", SessionRunning, "partial\n")
	// 预置计数到阈值，使本次调用走"超限"分支。
	session.ProbeUnknownCount = defaultProbeUnknownLimit

	require.Equal(t, SessionError, tm.detectSessionState(session))
}

// TestHandleFakeDead_KillEscapeIsError 钉住 D3：kill 三连败强拆 → SessionError（逃逸不伪装完成）。
func TestHandleFakeDead_KillEscapeIsError(t *testing.T) {
	m := &mockInspector{killErr: fmt.Errorf("kill failed")}
	tm := newTestMonitor(m)
	session := newTestSession("fk", SessionFakeDead, "")
	session.KillRetryCount = 2 // 本次为第 3 次失败

	removed := tm.handleFakeDead(session)
	require.True(t, removed, "达最大重试应强拆移除")
	require.Equal(t, SessionError, session.Status, "逃逸进程报失败极性")
}

// TestHandleFakeDead_KillSucceedsStaysCompleted 钉住：kill 成功（非逃逸）保持 completed。
func TestHandleFakeDead_KillSucceedsStaysCompleted(t *testing.T) {
	m := &mockInspector{} // killErr nil
	tm := newTestMonitor(m)
	session := newTestSession("fk2", SessionFakeDead, "")

	removed := tm.handleFakeDead(session)
	require.True(t, removed)
	require.Equal(t, SessionCompleted, session.Status)
}

// TestDetectorPull_ExitCodeOnError 钉住 D2 pull 模式：SessionError 时探测退出码填 ExitCode+Err。
func TestDetectorPull_ExitCodeOnError(t *testing.T) {
	d := NewTmuxSettleDetector("s42", nil, time.Hour)
	d.SetPaneStatusReader(func() (int, bool) { return 42, true })
	d.OnStateChange(SessionError, "boom output")

	select {
	case sig := <-d.Settled():
		require.NotNil(t, sig.Err, "失败必须携带 Err")
		require.Equal(t, 42, sig.ExitCode)
		require.Contains(t, sig.Err.Error(), "exited with code 42")
	default:
		t.Fatal("expected a settle signal")
	}
}

// TestDetectorPull_UnresolvableErrorText 钉住 known=false → Err 文本"unresolvable"，ExitCode 0。
func TestDetectorPull_UnresolvableErrorText(t *testing.T) {
	d := NewTmuxSettleDetector("sU", nil, time.Hour)
	d.SetPaneStatusReader(func() (int, bool) { return 0, false })
	d.OnStateChange(SessionError, "")

	select {
	case sig := <-d.Settled():
		require.NotNil(t, sig.Err)
		require.Equal(t, 0, sig.ExitCode, "不可辨不填假码")
		require.Contains(t, sig.Err.Error(), "unresolvable")
	default:
		t.Fatal("expected a settle signal")
	}
}

// TestDetectorPull_NoReaderDegrades 钉住未接 reader 时 SessionError 仍报错（降级不吞失败）。
func TestDetectorPull_NoReaderDegrades(t *testing.T) {
	d := NewTmuxSettleDetector("sNR", nil, time.Hour)
	// no SetPaneStatusReader → nil reader
	d.OnStateChange(SessionError, "x")
	select {
	case sig := <-d.Settled():
		require.NotNil(t, sig.Err)
	default:
		t.Fatal("expected a settle signal")
	}
}

// TestDetectorPull_CompletedNoErrNoCode 钉住正常完成（SessionCompleted, code 0）：无 Err、无码。
func TestDetectorPull_CompletedNoErrNoCode(t *testing.T) {
	d := NewTmuxSettleDetector("sOK", nil, time.Hour)
	d.SetPaneStatusReader(func() (int, bool) { return 0, true })
	d.OnStateChange(SessionCompleted, "clean")
	select {
	case sig := <-d.Settled():
		require.Nil(t, sig.Err)
		require.Equal(t, 0, sig.ExitCode)
		require.Equal(t, task.SettleCompleted, sig.Kind)
	default:
		t.Fatal("expected a settle signal")
	}
}

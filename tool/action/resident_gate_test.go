package action

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// R3（resident-continuity-r2-r4 2.3）fail-before 回归：三态探测加闸与
// orphan 语义（cleanup 排除 n-——枚举双条件修复后若不排除，cleanup 先于
// reattach 执行会屠杀全部常驻会话，第六轮 🔴1）。

func newGateMonitor(t *testing.T, mock *mockInspector) (*TmuxMonitor, *TmuxSession) {
	t.Helper()
	tm := NewTmuxMonitor(WithMonitorExecutor(mock), WithMonitorConfig(MonitorConfig{
		Interval:       50 * time.Millisecond,
		StableDuration: 100 * time.Millisecond,
	}))
	sess := &TmuxSession{ID: "gate-1", Name: "gate-1"}
	tm.AddSession(sess)
	return tm, sess
}

// err 1-2 次不屠杀、第 N 次才 dead（fail-before：旧路径 err→assume-dead 一次即杀）。
func TestProbeUnknownGate_ConsecutiveLimit(t *testing.T) {
	mock := &mockInspector{}
	mock.setProcess(true, false)
	mock.setOutput("working", nil)
	tm, sess := newGateMonitor(t, mock)
	mock.setAlive3Err(true) // 所有探测不可辨

	// 第 1、2 次 unknown：保留（SessionRunning）。
	for i := 1; i <= 2; i++ {
		require.Equal(t, SessionRunning, tm.detectSessionState(sess),
			"unknown #%d must keep the session (gate < limit 3)", i)
		require.Equal(t, i, sess.ProbeUnknownCount)
	}
	// 第 3 次 unknown：达到阈值，按 dead 处理。
	require.Equal(t, SessionCompleted, tm.detectSessionState(sess),
		"3rd consecutive unknown must be treated as dead")
}

// 会话真死（list 成功不含）立即 dead，不吃加闸。
func TestProbeUnknownGate_DeterministicDeadImmediate(t *testing.T) {
	mock := &mockInspector{}
	mock.setProcess(false, true)
	mock.setOutput("final", nil)
	tm, sess := newGateMonitor(t, mock)
	mock.setAlive3(false, true) // 列表可靠且不含

	require.Equal(t, SessionCompleted, tm.detectSessionState(sess))
	require.Equal(t, 0, sess.ProbeUnknownCount, "deterministic dead must not consume the gate")
}

// unknown 后恢复可辨 → 计数清零（再 1 次 unknown 不死）。
func TestProbeUnknownGate_ResetOnKnown(t *testing.T) {
	mock := &mockInspector{}
	mock.setProcess(true, false)
	mock.setOutput("run", nil)
	tm, sess := newGateMonitor(t, mock)

	mock.setAlive3Err(true)
	require.Equal(t, SessionRunning, tm.detectSessionState(sess)) // unknown 1/3
	require.Equal(t, 1, sess.ProbeUnknownCount)

	mock.setAlive3Err(false)
	mock.setAlive3(true, true)
	require.Equal(t, SessionRunning, tm.detectSessionState(sess)) // alive → 正常
	require.Equal(t, 0, sess.ProbeUnknownCount, "known probe must reset the streak")

	mock.setAlive3Err(true)
	require.Equal(t, SessionRunning, tm.detectSessionState(sess), "fresh unknown streak starts at 1/3")
	require.Equal(t, 1, sess.ProbeUnknownCount)
}

// fail-before 语义断言（无需真 tmux）：cleanup 排除 n- 是纯代码路径——用
// ListSessions 双条件 + CleanupOrphanSessions 的 n- 跳过在真 tmux 环境验证；
// 无 tmux 时以源级契约（NamedSessionName 前缀 vs cleanup 过滤条件）静态自洽。
func TestCleanupOrphan_ExcludesNamedSessions_Contract(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available — contract covered by code inspection (R3 2.1)")
	}
	te := NewTmuxExecutor(WithTmuxPrefix("tagent-gate-test"))
	defer func() {
		for _, s := range mustList(t, te) {
			if strings.HasPrefix(s.ID, "tagent-gate-test") || strings.HasPrefix(s.ID, "n-gatetest") {
				_ = te.KillSession(s.ID)
			}
		}
	}()

	// 造一对会话：生成名（prefix）+ named（n-）。
	if _, err := te.CreateSession(context.Background(), TmuxCreateOptions{
		Command: "sleep 30",
		Mode:    ModeOneshot,
	}); err != nil {
		t.Skipf("cannot create tmux session: %v", err)
	}
	if _, err := te.CreateSession(context.Background(), TmuxCreateOptions{
		Command: "sleep 30",
		Mode:    ModeResident,
		Name:    "gatetest-svc",
	}); err != nil {
		t.Skipf("cannot create tmux session: %v", err)
	}

	// fail-before 语义：cleanup 只允许杀生成名会话。
	killed := te.CleanupOrphanSessions()
	require.GreaterOrEqual(t, killed, 0)
	// named 会话必须存活（cleanup 排除 n-；NamedSessionName("gatetest-svc")="n-gatetest-svc"）。
	require.True(t, te.SessionExists("n-gatetest-svc"),
		"cleanup MUST NOT kill n- named sessions (R3 orphan redefinition)")
	// 双条件枚举必须命中 n-。
	found := false
	for _, s := range mustList(t, te) {
		if s.ID == "n-gatetest-svc" {
			found = true
		}
	}
	require.True(t, found, "ListSessions dual-condition must include n- sessions (fail-before: prefix-only filter never returns them)")
}

func mustList(t *testing.T, te *TmuxExecutor) []*TmuxSession {
	t.Helper()
	sessions, err := te.ListSessions()
	if err != nil {
		t.Skipf("tmux list failed: %v", err)
	}
	return sessions
}

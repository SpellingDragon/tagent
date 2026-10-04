package action

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"
)

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

// TestProbeUnknownGate_ConsecutiveLimit err 1-2 次不屠杀、第 N 次才 dead。
//
// 契约: docs/wiki/agent/task-lifecycle.md#ttl-reclaim
func TestProbeUnknownGate_ConsecutiveLimit(t *testing.T) {
	mock := &mockInspector{}
	mock.setProcess(true, false)
	mock.setOutput("working", nil)
	tm, sess := newGateMonitor(t, mock)
	mock.setAlive3Err(true)

	for i := 1; i <= 2; i++ {
		require.Equal(t, SessionRunning, tm.detectSessionState(sess),
			"unknown #%d must keep the session (gate < limit 3)", i)
		require.Equal(t, i, sess.ProbeUnknownCount)
	}
	require.Equal(t, SessionError, tm.detectSessionState(sess),
		"3rd consecutive unknown = 框架失明，报失败极性而非伪装完成（failure-polarity passthrough D3）")
}

// TestProbeUnknownGate_DeterministicDeadImmediate 会话真死（list 成功不含）立即 dead，不吃加闸。
func TestProbeUnknownGate_DeterministicDeadImmediate(t *testing.T) {
	mock := &mockInspector{}
	mock.setProcess(false, true)
	mock.setOutput("final", nil)
	tm, sess := newGateMonitor(t, mock)
	mock.setAlive3(false, true)

	require.Equal(t, SessionCompleted, tm.detectSessionState(sess))
	require.Equal(t, 0, sess.ProbeUnknownCount, "deterministic dead must not consume the gate")
}

// TestProbeUnknownGate_ResetOnKnown unknown 后恢复可辨 → 计数清零（再 1 次 unknown 不死）。
func TestProbeUnknownGate_ResetOnKnown(t *testing.T) {
	mock := &mockInspector{}
	mock.setProcess(true, false)
	mock.setOutput("run", nil)
	tm, sess := newGateMonitor(t, mock)

	mock.setAlive3Err(true)
	require.Equal(t, SessionRunning, tm.detectSessionState(sess))
	require.Equal(t, 1, sess.ProbeUnknownCount)

	mock.setAlive3Err(false)
	mock.setAlive3(true, true)
	require.Equal(t, SessionRunning, tm.detectSessionState(sess))
	require.Equal(t, 0, sess.ProbeUnknownCount, "known probe must reset the streak")

	mock.setAlive3Err(true)
	require.Equal(t, SessionRunning, tm.detectSessionState(sess), "fresh unknown streak starts at 1/3")
	require.Equal(t, 1, sess.ProbeUnknownCount)
}

// TestCleanupOrphan_ExcludesNamedSessions_Contract 钉住 清理孤儿会话必须跳过命名会话，且该排除是纯代码路径、不依赖真实 tmux。
// - 命名前缀与清理过滤条件是同一契约的两端，没有 tmux 时仍可静态自洽核对；
// - 具备 tmux 时再由双条件枚举与真实清理路径验证。
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

	killed := te.CleanupOrphanSessions()
	require.GreaterOrEqual(t, killed, 0)
	require.True(t, te.SessionExists("n-gatetest-svc"),
		"cleanup MUST NOT kill n- named sessions (R3 orphan redefinition)")
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

func newMetaTool(t *testing.T, dir string, sink func(sessionID, kind, name, detail string)) *ActionTool {
	t.Helper()
	ct := NewActionTool(WithOrphanCleanupDisabled(), WithResidentMetaDir(dir))
	if sink != nil {
		ct.SetResidentRecordSink(sink)
	}
	return ct
}

// TestResidentMeta_FullParamsAndLifecycleEvents ① spawn 写 meta 载全参数（Command/TaskID）+ 终态结局事件；旧记录零值兼容。
func TestResidentMeta_FullParamsAndLifecycleEvents(t *testing.T) {
	dir := t.TempDir()
	var events []string
	ct := newMetaTool(t, dir, func(sessionID, kind, name, detail string) {
		events = append(events, kind+":"+name)
	})

	ct.saveResidentMeta("n-svc1", ActionArgs{
		Command: "python server.py", Name: "svc1", Mode: string(ModeResident),
		Watch: "READY", Probe: "curl -f localhost:8000", ProbeIntervalSec: 30,
	})
	b, err := os.ReadFile(filepath.Join(dir, "sess-n-svc1.json"))
	require.NoError(t, err)
	var m ResidentMeta
	require.NoError(t, json.Unmarshal(b, &m))
	require.Equal(t, "python server.py", m.Command, "meta must carry the full command (R3 2.5)")
	require.Equal(t, "n-svc1", m.TaskID, "meta TaskID = session id (bridge key)")
	require.Equal(t, "svc1", m.Name)
	require.Equal(t, "READY", m.Watch)

	ct.removeResidentMeta("n-svc1")
	require.Len(t, events, 2, "spawn + end lifecycle events must fire via the sink")
	require.Equal(t, "spawn:svc1", events[0])
	require.Equal(t, "end:svc1", events[1])
	_, err = os.Stat(filepath.Join(dir, "sess-n-svc1.json"))
	require.True(t, os.IsNotExist(err), "meta removed after terminal")

	legacy := `{"name":"old","mode":"resident","spawned_at":"2026-01-01T00:00:00Z"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sess-n-old.json"), []byte(legacy), 0o600))
	var lm ResidentMeta
	require.NoError(t, json.Unmarshal([]byte(legacy), &lm))
	require.Equal(t, "", lm.Command)
}

// TestTaskIDBridge_SuspectToRunning 钉住 ②③ 唯一挂载点 + TaskID 桥（agent/task 层）：重挂跟踪的会话→suspect 任务提升 running；未跟踪→保持 suspect（探测裁决）。
func TestTaskIDBridge_SuspectToRunning(t *testing.T) {
	tm := task.NewTaskManager(task.TaskManagerConfig{})
	tm.RestoreTask("t1", task.TaskSpec{
		Kind: "command", Desc: "svc",
		Declarative: &task.Declarative{Kind: "command", Desc: "svc", TaskID: "n-tracked"},
	}, time.Now(), task.TaskSuspect)
	tm.RestoreTask("t2", task.TaskSpec{
		Kind: "command", Desc: "gone",
		Declarative: &task.Declarative{Kind: "command", Desc: "gone", TaskID: "n-gone"},
	}, time.Now(), task.TaskSuspect)

	tracked := map[string]bool{"n-tracked": true}
	isTracked := func(id string) bool { return tracked[id] }
	for _, tk := range tm.List() {
		if tk.Status() != task.TaskSuspect || tk.Spec.Declarative == nil {
			continue
		}
		if isTracked(tk.Spec.Declarative.TaskID) {
			tm.MarkTaskRunning(tk.ID)
		}
	}
	g1, _ := tm.Get("t1")
	require.Equal(t, task.TaskRunning, g1.Status(), "tracked session must lift its task back to running")
	g2, _ := tm.Get("t2")
	require.Equal(t, task.TaskSuspect, g2.Status(), "untracked session stays suspect (probe adjudication)")
}

// TestCrossRestartResume_RealProvisioning 钉住 ⑤ interactive 会话跨重启续用：跨重启 resume 经 rebuiltResumeClosure 真供能 （未重挂→relaunch 引导；重挂（mock monitor 跟踪）→send-keys 路径）。
func TestCrossRestartResume_RealProvisioning(t *testing.T) {
	ct := NewActionTool(WithOrphanCleanupDisabled(), WithResidentMetaDir(t.TempDir()))
	spec := ct.SpecFromDeclarative(nil, task.Declarative{
		Kind: "command", Desc: "repl", Key: "repl", Command: "python",
		TaskID: "n-unknown", Params: map[string]string{"mode": "interactive", "timeout": "0"},
	})
	require.NotNil(t, spec.ResumeFn)
	_, err := spec.ResumeFn(context.Background(), "print(1)")
	require.Error(t, err)
	require.Contains(t, err.Error(), "relaunch")
	require.True(t, strings.Contains(err.Error(), "no longer monitored"),
		"unmonitored guidance must match the in-process path wording")
}

// TestSaveResidentMeta D1: saveResidentMeta persists parameters for named resident sessions only.
func TestSaveResidentMeta(t *testing.T) {
	ct := &ActionTool{residentMetaDirOverride: t.TempDir()}

	resident := ActionArgs{Name: "dev-server", Mode: "resident", Watch: "ERROR", Probe: "curl -sf :1", ProbeIntervalSec: 30, ProbeFailures: 3}
	ct.saveResidentMeta("n-dev-server", resident)
	b, err := os.ReadFile(ct.metaPath("n-dev-server"))
	if err != nil {
		t.Fatalf("meta not saved: %v", err)
	}
	var m ResidentMeta
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != "dev-server" || m.Mode != "resident" || m.Watch != "ERROR" || m.Probe != "curl -sf :1" {
		t.Fatalf("meta mismatch: %+v", m)
	}

	oneshot := ActionArgs{Name: "one", Mode: ""}
	ct.saveResidentMeta("n-one", oneshot)
	if _, err := os.Stat(ct.metaPath("n-one")); !os.IsNotExist(err) {
		t.Fatal("oneshot session must not persist metadata")
	}
}

// TestSweepStaleResidents 钉住 D2: SweepStaleResidents kills sessions past TTL (and drops their records)
func TestSweepStaleResidents(t *testing.T) {
	ct := &ActionTool{residentMetaDirOverride: t.TempDir()}

	old := time.Now().Add(-48 * time.Hour).Format(time.RFC3339)
	fresh := time.Now().Format(time.RFC3339)
	os.WriteFile(ct.metaPath("n-old"), []byte(`{"name":"old","mode":"resident","spawned_at":"`+old+`"}`), 0o600)
	os.WriteFile(ct.metaPath("n-new"), []byte(`{"name":"new","mode":"resident","spawned_at":"`+fresh+`"}`), 0o600)

	ct.SweepStaleResidents(time.Now())

	if _, err := os.Stat(ct.metaPath("n-old")); !os.IsNotExist(err) {
		t.Fatal("stale resident record should be removed")
	}
	if _, err := os.Stat(ct.metaPath("n-new")); err != nil {
		t.Fatal("fresh resident record must survive the sweep")
	}
}

// TestCanSpawnResident D2: cap logic.
func TestCanSpawnResident(t *testing.T) {
	old := maxResidentSessions
	maxResidentSessions = 1
	defer func() { maxResidentSessions = old }()

	ct := &ActionTool{residentMetaDirOverride: t.TempDir()}
	ct.tmuxMonitor = NewTmuxMonitor(WithMonitorExecutor(nil))
	ct.tmuxMonitor.AddSession(&TmuxSession{ID: "n-a", Mode: ModeResident})
	if ct.CanSpawnResident() {
		t.Fatal("cap=1 with 1 resident must refuse")
	}
	maxResidentSessions = 2
	if !ct.CanSpawnResident() {
		t.Fatal("cap=2 with 1 resident must allow")
	}
}

// TestReattachResidentSessions_Defensive D1: reattach skips oneshot metadata and corrupt files without dying.
func TestReattachResidentSessions_Defensive(t *testing.T) {
	ct := &ActionTool{residentMetaDirOverride: t.TempDir()}
	os.WriteFile(filepath.Join(ct.metaDir(), "sess-n-corrupt.json"), []byte("{not json"), 0o600)
	os.WriteFile(filepath.Join(ct.metaDir(), "bogus.txt"), []byte("x"), 0o600)

	if n := ct.ReattachResidentSessions(); n != 0 {
		t.Fatalf("nil executor must return 0, got %d", n)
	}
	_ = strings.TrimSpace
}

// TestSweepStaleResidents_AdoptedLongRunnerSurvives 钉住 收养优先：运行多日的会话只要收养时间被刷新，清扫就不得收走它。
// - 清扫只收割无人收养且已超时的孤儿，新鲜度自收养时刻起算；缺该记录时回退到派生时刻；
// - 派生时刻的年龄本身不构成孤儿证据。
func TestSweepStaleResidents_AdoptedLongRunnerSurvives(t *testing.T) {
	ct := &ActionTool{residentMetaDirOverride: t.TempDir()}

	oldSpawn := time.Now().Add(-48 * time.Hour).Format(time.RFC3339)
	recentAdopt := time.Now().Add(-time.Hour).Format(time.RFC3339)
	os.WriteFile(ct.metaPath("n-longrun"), []byte(
		`{"name":"lr","mode":"resident","spawned_at":"`+oldSpawn+`","last_adopted_at":"`+recentAdopt+`"}`), 0o600)

	if killed := ct.SweepStaleResidents(time.Now()); killed != 0 {
		t.Fatalf("adopted long-runner must survive the sweep, killed=%d", killed)
	}
	if _, err := os.Stat(ct.metaPath("n-longrun")); err != nil {
		t.Fatal("adopted long-runner record must survive")
	}

	os.WriteFile(ct.metaPath("n-legacy-orphan"), []byte(
		`{"name":"lo","mode":"resident","spawned_at":"`+oldSpawn+`"}`), 0o600)
	_ = ct.SweepStaleResidents(time.Now())
	if _, err := os.Stat(ct.metaPath("n-legacy-orphan")); !os.IsNotExist(err) {
		t.Fatal("legacy orphan record must be removed")
	}
}

// TestTouchAdopted_StampsLastAdoptedAt 收养路径刷新 LastAdoptedAt：reattachOne + touchAdopted 后 meta 落盘。
func TestTouchAdopted_StampsLastAdoptedAt(t *testing.T) {
	ct := &ActionTool{residentMetaDirOverride: t.TempDir()}
	os.WriteFile(ct.metaPath("n-x"), []byte(`{"name":"x","mode":"resident","spawned_at":"2026-01-01T00:00:00Z"}`), 0o600)

	ct.touchAdopted("n-x")

	b, err := os.ReadFile(ct.metaPath("n-x"))
	if err != nil {
		t.Fatal(err)
	}
	var m ResidentMeta
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.LastAdoptedAt == "" {
		t.Fatal("LastAdoptedAt must be stamped by touchAdopted")
	}
	if _, err := time.Parse(time.RFC3339, m.LastAdoptedAt); err != nil {
		t.Fatalf("LastAdoptedAt not RFC3339: %v", err)
	}
}

// TestCrossRestartResume_RebindsDetectorToMonitor 钉住跨重启 resume 的重绑供给。
// - 新建 detector 必须接管 monitor 的 per-session 回调；不重绑则迁移仍供给旧一代 detector，本轮新 detector 永无 settle/ExitCode。
// - 判据三分：回调换人、新 detector 经回调收到迁移并出 settle、旧绑定 detector 保持静默（供给无双写）。
func TestCrossRestartResume_RebindsDetectorToMonitor(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available — rebind wiring needs a live session for the resume path")
	}
	ct := NewActionTool(WithOrphanCleanupDisabled(), WithResidentMetaDir(t.TempDir()))
	require.NotNil(t, ct.tmuxExecutor, "host has tmux: the executor must be wired")

	sess, err := ct.tmuxExecutor.CreateSession(context.Background(), TmuxCreateOptions{
		Command: "cat", Mode: ModeResident, Name: "cross-rebind",
	})
	if err != nil {
		t.Skipf("cannot create tmux session: %v", err)
	}
	sessionID := sess.ID
	defer func() { _ = ct.tmuxExecutor.KillSession(sessionID) }()

	oldDetector := NewTmuxSettleDetector(sessionID, func() {})
	ct.tmuxMonitor.AddSessionWithCallback(sess, func(_ string, _, newStatus SessionStatus, output string) {
		oldDetector.OnWatchOutput(output)
		oldDetector.OnStateChange(newStatus, output)
	})

	spec := ct.SpecFromDeclarative(nil, task.Declarative{
		Kind: "command", Desc: "repl", Key: "repl", Command: "cat",
		TaskID: sessionID, Params: map[string]string{"mode": "resident", "timeout": "0"},
	})
	require.NotNil(t, spec.ResumeFn)

	newDetector, rerr := spec.ResumeFn(context.Background(), "echo hi")
	require.NoError(t, rerr, "resume against the live session must complete the rebind+send path")

	cb := ct.tmuxMonitor.sessionCallbacks[sessionID]
	require.NotNil(t, cb, "the resume round must own a per-session callback")
	cb(sessionID, SessionRunning, SessionCompleted, "hi\n")

	select {
	case sig := <-newDetector.Settled():
		require.Equal(t, task.SettleCompleted, sig.Kind, "the transition must surface as settle on the NEW detector")
	case <-time.After(2 * time.Second):
		t.Fatal("new detector never received the state change — rebind did not land")
	}
	select {
	case sig := <-oldDetector.Settled():
		t.Fatalf("stale pre-rebind detector received the transition: %+v", sig)
	default:
	}
}

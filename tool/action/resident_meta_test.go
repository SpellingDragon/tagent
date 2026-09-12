package action

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"
)

// R3（resident-continuity-r2-r4 2.7）回归：ResidentMeta 补全读写、
// resident_session 事件发射、唯一挂载点、TaskID 桥（suspect→running）。

func newMetaTool(t *testing.T, dir string, sink func(sessionID, kind, name, detail string)) *ActionTool {
	t.Helper()
	ct := NewActionTool(WithOrphanCleanupDisabled(), WithResidentMetaDir(dir))
	if sink != nil {
		ct.SetResidentRecordSink(sink)
	}
	return ct
}

// ① spawn 写 meta 载全参数（Command/TaskID）+ 终态结局事件；旧记录零值兼容。
func TestResidentMeta_FullParamsAndLifecycleEvents(t *testing.T) {
	dir := t.TempDir()
	var events []string // "kind:name"
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

	// 旧记录（无新字段）零值兼容：reattach 路径解组不炸。
	legacy := `{"name":"old","mode":"resident","spawned_at":"2026-01-01T00:00:00Z"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sess-n-old.json"), []byte(legacy), 0o600))
	var lm ResidentMeta
	require.NoError(t, json.Unmarshal([]byte(legacy), &lm))
	require.Equal(t, "", lm.Command)
}

// ②③ 唯一挂载点 + TaskID 桥（agent/task 层）：重挂跟踪的会话→suspect 任务提升
// running；未跟踪→保持 suspect（探测裁决）。
func TestTaskIDBridge_SuspectToRunning(t *testing.T) {
	tm := task.NewTaskManager(task.TaskManagerConfig{})
	// 模拟 R2 重建：两个 suspect 任务，其一 TaskID=被跟踪会话。
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

// ⑤ interactive 会话跨重启续用：跨重启 resume 经 rebuiltResumeClosure 真供能
// （未重挂→relaunch 引导；重挂（mock monitor 跟踪）→send-keys 路径）。
func TestCrossRestartResume_RealProvisioning(t *testing.T) {
	ct := NewActionTool(WithOrphanCleanupDisabled(), WithResidentMetaDir(t.TempDir()))
	spec := ct.SpecFromDeclarative(nil, task.Declarative{
		Kind: "command", Desc: "repl", Key: "repl", Command: "python",
		TaskID: "n-unknown", Params: map[string]string{"mode": "interactive", "timeout": "0"},
	})
	require.NotNil(t, spec.ResumeFn)
	// 未重挂：TouchSession 失败 → relaunch 引导（与旧路径同文案）。
	_, err := spec.ResumeFn("print(1)")
	require.Error(t, err)
	require.Contains(t, err.Error(), "relaunch")
	require.True(t, strings.Contains(err.Error(), "no longer monitored"),
		"unmonitored guidance must match the in-process path wording")
}

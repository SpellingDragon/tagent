// 本文件负责自身遥测阶梯：档位驻留与释放、L2 收敛语义、保护期在冻结前豁免、谱系分类，以及
// 任务门控接线会阻绝对象收养。
// 契约: docs/wiki/agent/compression-and-telemetry.md#telemetry-ladder
package agent

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"
)

// fakeClock drives the dwell/hysteresis timing deterministically.
type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

func feedSelfManaged(a *SelfTelemetryAuditor, n int) {
	for i := 0; i < n; i++ {
		a.ObserveSettle(map[string]any{"meta_trigger_source": "meditation"})
	}
}

func feedExternal(a *SelfTelemetryAuditor, n int) {
	for i := 0; i < n; i++ {
		a.ObserveInput()
	}
}

func TestAudit_ProtectedExemptionBeforeFreeze(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_800_000_000, 0)}
	a := NewSelfTelemetryAuditor(nil)
	a.now = clk.Now
	for i := 0; i < 120; i++ {
		clk.Advance(time.Minute)
		feedSelfManaged(a, 1)
		if a.Level() >= auditFreeze {
			break
		}
	}
	require.Equal(t, auditFreeze, a.Level(), "sustained 100% self-managed traffic must reach freeze")
	require.NotEmpty(t, a.GateReason(task.TaskSpec{Kind: "command"}), "ordinary specs refused while frozen")
	require.Empty(t, a.GateReason(task.TaskSpec{Kind: "command", Protected: true}),
		"protected specs pass the audit freeze — durability defense is never withdrawn")
}

func TestAudit_LadderDwellAndRelease(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	a := NewSelfTelemetryAuditor(nil)
	a.now = clk.Now

	feedSelfManaged(a, 10)
	require.Equal(t, 0, a.Level(), "below the minimum sample count the auditor must not act")

	feedSelfManaged(a, 20)
	require.Equal(t, 1, a.Level())
	clk.Advance(auditDwellPerStep - time.Minute)
	feedSelfManaged(a, 1)
	require.Equal(t, 1, a.Level(), "escalation requires the dwell between ladder steps")
	clk.Advance(2 * time.Minute)
	feedSelfManaged(a, 1)
	require.Equal(t, 2, a.Level())
	clk.Advance(auditDwellPerStep + time.Minute)
	feedSelfManaged(a, 1)
	require.Equal(t, auditFreeze, a.Level())

	clk.Advance(time.Hour)
	feedExternal(a, 260)
	require.Equal(t, 0, a.Level(), "sustained sub-hysteresis ratio releases the freeze")
	require.Empty(t, a.GateReason(task.TaskSpec{Kind: "command"}), "released gate admits ordinary specs")
}

// TestAudit_L2ConvergeSemantics 钉住 中间档单独可判：收敛期拒绝自管来源的派生，用户来源与受保护声明照常放行。
func TestAudit_L2ConvergeSemantics(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_650_000_000, 0)}
	a := NewSelfTelemetryAuditor(nil)
	a.now = clk.Now
	a.mu.Lock()
	a.level = 2
	a.mu.Unlock()
	require.NotEmpty(t, a.GateReason(task.TaskSpec{Kind: "generic",
		Origin: map[string]string{"trigger_source": "meditation"}}),
		"L2 must converge self-managed-origin spawns")
	require.Empty(t, a.GateReason(task.TaskSpec{Kind: "generic",
		Origin: map[string]string{"trigger_source": "user"}}),
		"L2 must not touch user-derived spawns")
	require.Empty(t, a.GateReason(task.TaskSpec{Kind: "generic", Protected: true}),
		"L2 passes protected specs too")
}

func TestAudit_LineageClassification(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_600_000_000, 0)}
	a := NewSelfTelemetryAuditor(nil)
	a.now = clk.Now
	for i := 0; i < 40; i++ {
		a.ObserveSettle(map[string]any{"meta_trigger_source": "user"})
	}
	require.Equal(t, 0, a.Level())
	for i := 0; i < 40; i++ {
		a.ObserveSettle(map[string]any{"lineage_absent": "true"})
	}
	require.Equal(t, 1, a.Level())
}

func TestAudit_TaskGateWiringBlocksAdoption(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_500_000_000, 0)}
	a := NewSelfTelemetryAuditor(nil)
	a.now = clk.Now
	a.forceFreezeForTest()
	tm := task.NewTaskManager(task.TaskManagerConfig{AuditGate: a.GateReason})

	res := tm.Spawn(task.TaskSpec{Kind: "generic", Desc: "chore"}, nil)
	require.NotEmpty(t, res.Blocked, "ordinary spawn refused while frozen")
	det := task.NewFuncSettleDetector(context.Background(), func(context.Context) (string, error) {
		return "repaired", nil
	})
	res = tm.Spawn(task.TaskSpec{Kind: "generic", Desc: "repair", Protected: true}, det)
	require.Empty(t, res.Blocked, "protected spawn admitted through the audit freeze")
	require.True(t, res.Settled)
}

// forceFreezeForTest pins the auditor to L3 without feeding the window.
func (a *SelfTelemetryAuditor) forceFreezeForTest() {
	a.mu.Lock()
	a.level = auditFreeze
	a.mu.Unlock()
}

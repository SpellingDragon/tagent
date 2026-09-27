package agent

// Self-telemetry-audit regressions (change: attention-budget-architecture,
// spec: self-telemetry-audit). R4 discipline: the exemption-whitelist test
// runs FIRST — the freeze must never withdraw the durability defense.

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
	// 2.1 (R4: whitelist first): a frozen auditor MUST pass protected specs.
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

	// Too few samples: no verdict regardless of ratio.
	feedSelfManaged(a, 10)
	require.Equal(t, 0, a.Level(), "below the minimum sample count the auditor must not act")

	// Cross the threshold: L1 immediately.
	feedSelfManaged(a, 20)
	require.Equal(t, 1, a.Level())
	// Inside the dwell it must NOT march forward.
	clk.Advance(auditDwellPerStep - time.Minute)
	feedSelfManaged(a, 1)
	require.Equal(t, 1, a.Level(), "escalation requires the dwell between ladder steps")
	// Dwell passed: L2.
	clk.Advance(2 * time.Minute)
	feedSelfManaged(a, 1)
	require.Equal(t, 2, a.Level())
	// Another dwell: L3 freeze.
	clk.Advance(auditDwellPerStep + time.Minute)
	feedSelfManaged(a, 1)
	require.Equal(t, auditFreeze, a.Level())

	// Recovery: sustained external traffic below the hysteresis line releases
	// the freeze entirely (window pruning makes the borderline-hold shape a
	// timing-micro test; the release semantics are what production relies on).
	clk.Advance(time.Hour) // age out the self-managed samples of the window
	feedExternal(a, 260)
	require.Equal(t, 0, a.Level(), "sustained sub-hysteresis ratio releases the freeze")
	require.Empty(t, a.GateReason(task.TaskSpec{Kind: "command"}), "released gate admits ordinary specs")
}

// TestAudit_L2ConvergeSemantics pins the middle ladder step in isolation:
// converge refuses self-managed-origin spawns while user-derived and
// protected specs pass.
func TestAudit_L2ConvergeSemantics(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_650_000_000, 0)}
	a := NewSelfTelemetryAuditor(nil)
	a.now = clk.Now
	a.mu.Lock()
	a.level = 2
	a.mu.Unlock()
	require.NotEmpty(t, a.GateReason(task.TaskSpec{Kind: "generic",
		Origin: map[string]string{"meta_trigger_source": "meditation"}}),
		"L2 must converge self-managed-origin spawns")
	require.Empty(t, a.GateReason(task.TaskSpec{Kind: "generic",
		Origin: map[string]string{"meta_trigger_source": "user"}}),
		"L2 must not touch user-derived spawns")
	require.Empty(t, a.GateReason(task.TaskSpec{Kind: "generic", Protected: true}),
		"L2 passes protected specs too")
}

func TestAudit_LineageClassification(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_600_000_000, 0)}
	a := NewSelfTelemetryAuditor(nil)
	a.now = clk.Now
	// user-lineage settles are environment work, not self-managed: 40 of them
	// must never trip the auditor (ratio 0).
	for i := 0; i < 40; i++ {
		a.ObserveSettle(map[string]any{"meta_trigger_source": "user"})
	}
	require.Equal(t, 0, a.Level())
	// absent/unknown lineage counts as self-managed (conservative withhold
	// philosophy): 40 more → 50% > 40% over 80 samples → L1.
	for i := 0; i < 40; i++ {
		a.ObserveSettle(map[string]any{"lineage_absent": "true"})
	}
	require.Equal(t, 1, a.Level())
}

func TestAudit_TaskGateWiringBlocksAdoption(t *testing.T) {
	// End-to-end at the task layer: frozen auditor → Spawn returns Blocked,
	// in-flight work unaffected (gate-not-wall semantics).
	clk := &fakeClock{t: time.Unix(1_500_000_000, 0)}
	a := NewSelfTelemetryAuditor(nil)
	a.now = clk.Now
	a.forceFreezeForTest()
	tm := task.NewTaskManager(task.TaskManagerConfig{AuditGate: a.GateReason})

	res := tm.Spawn(task.TaskSpec{Kind: "generic", Desc: "chore"}, nil)
	require.NotEmpty(t, res.Blocked, "ordinary spawn refused while frozen")
	// The admitted protected spawn uses a real detector (a nil detector is the
	// pure-sync contract: Spawn blocks until settle).
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

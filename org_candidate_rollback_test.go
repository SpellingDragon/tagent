// org_candidate_rollback 回滚域：数值与结构共享一份完整回滚记录，回滚重取属主。
// 契约: docs/wiki/platform/org-hot-reload.md#rollback
package tagent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestRollbackOfHotAddNumericWithInFlightTurn 钉住 热加之后接纯数值更新再回滚时，在途调用必须跑完。
// - 断言只读该 agent 自己的真实消费者与宿主可见答复，不读指纹；
// - 回滚环里的来源值必须回来，且它自始至终只有一个属主；
// - 已在途的那次调用不得被回滚重发布拆掉。
// - 在途窗口以 gate 的 park 观测直接钉住（告警轮可多枚且可合批，排空计数不可锚定）；
// 契约: docs/wiki/platform/org-hot-reload.md#rollback
func TestRollbackOfHotAddNumericWithInFlightTurn(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := writeGateConfig(t, yamlPath, ownerBYAML(false, 2, "1m"), time.Now())

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	m := &delegModel{}
	entry, err := New(*cfg, WithModel(m), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	out, err := entry.StartLoop("u", "g24-b-session")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range out {
		}
	}()
	t.Cleanup(func() { <-done })

	tick = writeGateConfig(t, yamlPath, ownerBYAML(true, 2, "1m"), tick)
	entry.CheckOrgReload()
	ownerB := residentCacheForTest(entry)[g24B]
	require.NotNil(t, ownerB, "B became a resident owner")

	tick = writeGateConfig(t, yamlPath, ownerBYAML(true, 7, "5m"), tick)
	entry.CheckOrgReload()
	require.Equal(t, 7, ownerB.OrgKeepRecent(), "B's own compressor consumer took the update")
	require.Equal(t, 5*time.Minute, ownerB.TaskManager().TerminalTTL(), "B's own manager took the update")

	bGate := make(chan struct{})
	m.armGate("SUB-B", bGate)
	t.Cleanup(func() { disarmGate(bGate) })
	if _, err := entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("in-flight")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "B parked mid-call", func() bool {
		return m.parkedNow("SUB-B") >= 1
	})

	entry.Rollback()
	completedBase := countMainCompletedByB(m.snapshot())
	disarmGate(bGate)
	waitFor(t, "the in-flight B call completed", func() bool {
		return countMainCompletedByB(m.snapshot()) > completedBase
	})

	require.Equal(t, 2, ownerB.OrgKeepRecent(), "§2.4(b)：回滚把 B 自身的 keepRecent 恢复到环源值")
	require.Equal(t, time.Minute, ownerB.TaskManager().TerminalTTL(), "§2.4(b)：回滚把 B 自身的 terminal TTL 恢复到环源值")
	require.Same(t, ownerB, residentCacheForTest(entry)[g24B],
		"§2.4(b)：回滚推进执行面，不另造第二个 B owner（单一 owner＝实例与 store 身份不动）")

	servedNow := countServed(m.snapshot(), "SUB-B")
	if _, err := entry.InjectMessageContext(context.Background(), "user", model.NewUserMessage("after")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a post-rollback call is served", func() bool {
		return countServed(m.snapshot(), "SUB-B") > servedNow
	})
}

// TestFullConfigAndRollback 钉住 针对真实构造的 agent 驱动完整的配置与回滚契约。
// - 纯数值应用必须轮转回滚环、推进修订号与应用时间，却不推进结构世代；
// - 语义完全相同的应用不轮转；
// - 回滚发布新世代，同时恢复结构与五项热参；被拒候选两轴都不动；
// - 每个取值断言都读真实消费者（压缩预算与保留数、任务管理器寿命），绝不读常驻配置的复读。
// 契约: docs/wiki/platform/org-hot-reload.md#rollback
func TestFullConfigAndRollback(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(l3YAML("A", 2, 4000, 0.5, "1m"))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	require.Equal(t, 2, entry.OrgKeepRecent())
	require.Equal(t, 2000, entry.OrgBudgetLine())
	d := entry.OrgDiagnostics()
	require.EqualValues(t, 0, diagInt64(t, d, "generation"))
	require.EqualValues(t, 0, diagInt64(t, d, "revision"))
	_, hasPub := diagTime(t, d, "lastPublishedAt")
	require.False(t, hasPub, "startup is not a structural publish")

	write(l3YAML("B", 2, 4000, 0.5, "1m"))
	entry.CheckOrgReload()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 1, diagInt64(t, d, "generation"), "structural change advances the generation")
	require.EqualValues(t, 1, diagInt64(t, d, "revision"), "a structural publish is also a full apply")
	pub1, hasPub := diagTime(t, d, "lastPublishedAt")
	require.True(t, hasPub)
	require.Equal(t, 2000, entry.OrgBudgetLine(), "prompt-only change leaves the budget")

	lastApplied1, _ := diagTime(t, d, "lastAppliedAt")
	write(l3YAML("B", 7, 9000, 0.5, "5m"))
	entry.CheckOrgReload()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 1, diagInt64(t, d, "generation"), "numeric-only must NOT bump the structural generation")
	require.EqualValues(t, 2, diagInt64(t, d, "revision"), "numeric-only is a full apply → bumps revision")
	require.Equal(t, 7, entry.OrgKeepRecent(), "compressor keepRecent is the real consumer")
	require.Equal(t, 4500, entry.OrgBudgetLine(), "9000 × 0.5 must reach the effective budget line")
	require.Equal(t, 5*time.Minute, entry.TaskManager().TerminalTTL(), "TaskManager TTL is the real consumer")
	pub2, _ := diagTime(t, d, "lastPublishedAt")
	require.True(t, pub2.Equal(pub1), "lastPublishedAt must NOT move on a numeric-only apply")
	lastApplied2, _ := diagTime(t, d, "lastAppliedAt")
	require.True(t, lastApplied2.After(lastApplied1), "lastAppliedAt advances on a numeric-only apply")

	write(l3YAML("B", 7, 9000, 0.5, "5m"))
	entry.CheckOrgReload()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 2, diagInt64(t, d, "revision"), "semantically identical apply must not rotate/advance (D9)")
	require.EqualValues(t, 1, diagInt64(t, d, "generation"))

	revBeforeRollback := diagInt64(t, d, "revision")
	entry.Rollback()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 2, diagInt64(t, d, "generation"), "rollback republishes as a new generation")
	require.EqualValues(t, revBeforeRollback+1, diagInt64(t, d, "revision"))
	require.Equal(t, 2, entry.OrgKeepRecent(), "rollback restores the pre-numeric keepRecent")
	require.Equal(t, 2000, entry.OrgBudgetLine(), "rollback restores the pre-numeric budget (structure + hot params together)")
	require.Equal(t, time.Minute, entry.TaskManager().TerminalTTL(), "rollback restores the pre-numeric terminal TTL")
	pub3, _ := diagTime(t, d, "lastPublishedAt")
	require.True(t, pub3.After(pub1), "rollback is a structural publish → lastPublishedAt advances")

	entry.Rollback()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 2, diagInt64(t, d, "generation"), "a rollback to identical content must not bump the generation")
	require.Equal(t, 2, entry.OrgKeepRecent(), "values stay at the restored source, unchanged")
	require.Equal(t, 2000, entry.OrgBudgetLine())
	require.Equal(t, time.Minute, entry.TaskManager().TerminalTTL())

	write(l3YAML("B", 9, 4000, 0.5, "1m"))
	entry.CheckOrgReload()
	require.Equal(t, 9, entry.OrgKeepRecent())
	entry.Rollback()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 3, diagInt64(t, d, "generation"), "rollback after a real change publishes a new generation")
	require.Equal(t, 2, entry.OrgKeepRecent(), "the fresh numeric edit is rolled back to the ring source")
}

// TestRollbackHookSurvivesNumericOnlyFirstUpdate 钉住 回滚钩子必须在装载器装配时装好一次，与哪个分支先触发无关。
// - 否则首个更新是纯数值的组织会有轮转过的回滚环却没有钩子，回滚静默成空操作；
// - 场景为启动 → 纯数值 → 回滚恢复启动期的热值并发布新世代，断言取真实消费者而非取值器。
// 契约: docs/wiki/platform/org-hot-reload.md#rollback
func TestRollbackHookSurvivesNumericOnlyFirstUpdate(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(l3YAML("A", 2, 4000, 0.5, "1m"))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()
	require.Equal(t, 2, entry.OrgKeepRecent())

	write(l3YAML("A", 7, 9000, 0.5, "5m"))
	entry.CheckOrgReload()
	d := entry.OrgDiagnostics()
	require.EqualValues(t, 0, diagInt64(t, d, "generation"), "numeric-only must not bump the generation")
	require.EqualValues(t, 1, diagInt64(t, d, "revision"), "numeric-only is a full apply")
	require.Equal(t, 7, entry.OrgKeepRecent())
	require.Equal(t, 4500, entry.OrgBudgetLine())

	entry.Rollback()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 1, diagInt64(t, d, "generation"), "rollback publishes a new generation")
	require.EqualValues(t, 2, diagInt64(t, d, "revision"))
	require.Equal(t, 2, entry.OrgKeepRecent(), "rollback restores the startup keepRecent")
	require.Equal(t, 2000, entry.OrgBudgetLine(), "rollback restores the startup budget")
	require.Equal(t, time.Minute, entry.TaskManager().TerminalTTL(), "rollback restores the startup terminal TTL")

	entry.Rollback()
	d = entry.OrgDiagnostics()
	require.EqualValues(t, 1, diagInt64(t, d, "generation"), "a rollback to identical content must not spin a new generation")
	require.Equal(t, 2, entry.OrgKeepRecent())
}

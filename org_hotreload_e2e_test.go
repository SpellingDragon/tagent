package tagent

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/stretchr/testify/require"
)

// e2eYAML renders the minimal org config used by the hot-shift e2e test.
// Structural fields stay byte-identical across renders so only
// compress_threshold (hot-applicable, fingerprint-excluded) or an explicit
// org field can move the fingerprint.
func e2eYAML(threshold float64, model string) string {
	return "entry: main\n" +
		"providers:\n  p1:\n    provider: openai\n    api_endpoint: https://api.example.com\n    api_key_env: TAGENT_TEST_API_KEY\n" +
		"agents:\n  main:\n    model: " + model + "\n" +
		"    system_prompt:\n      inline: \"e2e hot shift\"\n" +
		"    compress_threshold: " + strconv.FormatFloat(threshold, 'f', -1, 64) + "\n" +
		"    memory:\n      type: memory\n"
}

// TestOrgHotShift_EndToEnd drives the full incremental-A loop against a real
// agent built by tagent.New: mtime churn -> re-parse -> fingerprint compare ->
// ApplyOrgParams hot-shift (unchanged fp) / keep-serving (changed fp, broken
// config) — without restarting the process.
func TestOrgHotShift_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(yamlPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}
		// Force strictly increasing mtime: FS timestamp granularity can
		// swallow rapid successive writes and silently skip the reload.
		tick = tick.Add(2 * time.Second)
		if err := os.Chtimes(yamlPath, tick, tick); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}

	write(e2eYAML(0.8, "gpt-x"))
	cfg, err := LoadConfig(yamlPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = entry.Close() }()

	if got := entry.OrgThreshold(); got != 0.8 {
		t.Fatalf("initial threshold = %v, want 0.8 (constructor must seed cm.thresholdPct)", got)
	}

	// 1) hot shift: same org structure, new threshold -> applied live.
	write(e2eYAML(0.5, "gpt-x"))
	entry.CheckOrgReload()
	if got := entry.OrgThreshold(); got != 0.5 {
		t.Fatalf("after hot shift threshold = %v, want 0.5", got)
	}

	// 2) structural change (model is fingerprinted): keep serving,
	// threshold unchanged, no panic.
	write(e2eYAML(0.5, "gpt-w"))
	entry.CheckOrgReload()
	if got := entry.OrgThreshold(); got != 0.5 {
		t.Fatalf("after structural change threshold = %v, want 0.5 (kept)", got)
	}

	// 3) broken config: fail-closed to previous snapshot.
	write("entry: [broken")
	entry.CheckOrgReload()
	if got := entry.OrgThreshold(); got != 0.5 {
		t.Fatalf("after broken config threshold = %v, want 0.5 (fail-closed)", got)
	}
}

// TestOrgHotReload_ExecutorSwapEndToEnd（R4，resident-continuity-r2-r4 roadmap 4.5）：
// 结构变更（fingerprint 变化）→ executorOnly 重建壳 → SwapExecutor 换入 →
// 非重启换代生效且常驻不变量原封。与 TestOrgHotShift_EndToEnd（incremental A
// 全链路懒检查）互补：本测聚焦 B 面（换缝）缝合点。
// fail-before 对照：不 Swap（旧「RESTART required」方案）时 Runner 引用永不变化。
func TestOrgHotReload_ExecutorSwapEndToEnd(t *testing.T) {
	rc := &runtimeConfig{model: &factoryMockModel{}}
	cfg := Config{
		Entry: "tagent",
		Agents: map[string]AgentConfig{
			"tagent": {
				SystemPrompt: PromptConfig{Inline: "gen1 prompt"},
				Memory:       MemoryConfig{Type: "memory"},
			},
		},
	}
	loader := prompt.NewLoader("")
	cache := make(map[string]*agent.TagentAgent)

	// 1) 常驻 entry（冷启动，非 executorOnly）。
	resident, err := buildAgent("tagent", cfg.Agents["tagent"], cfg, rc, loader, cache, buildModeResident)
	require.NoError(t, err)
	// New() 的 🔴1 回填：懒检查 Reload 依赖常驻资源注入（此处镜像）。
	rc.entryMemStore = resident.MemStore()
	rc.entrySessionSvc = resident.SessionSvc()
	require.NotNil(t, rc.entryMemStore)
	require.NotNil(t, rc.entrySessionSvc)

	oldRunner := resident.Runner()
	require.NotNil(t, oldRunner)
	oldStore := resident.MemStore()
	oldSvc := resident.SessionSvc()
	oldTM := resident.TaskManager()

	// 2) 结构变更（prompt 变更 → fingerprint 变化）→ executorOnly 重建壳。
	gen2 := cfg.Agents["tagent"]
	gen2.SystemPrompt = PromptConfig{Inline: "gen2 prompt"}
	rebuilt, err := buildAgent("tagent", gen2, cfg, rc, loader, make(map[string]*agent.TagentAgent), buildModeExecutorShell)
	require.NoError(t, err)
	newRunner := rebuilt.Runner()
	require.NotNil(t, newRunner)
	require.NotSame(t, oldRunner, newRunner, "rebuilt shell must carry a NEW runner")

	// 🔴1 回归：壳复用常驻事实链 store 与 session 服务（ownership 表）。
	require.Same(t, oldStore, rebuilt.MemStore(), "executorOnly shell must reuse the resident memStore")
	require.Same(t, oldSvc, rebuilt.SessionSvc(), "executorOnly shell must reuse the resident sessionSvc")

	// 3) fail-before 对照：不 Swap 时（旧「RESTART required」方案）结构变更不可见。
	require.Same(t, oldRunner, resident.Runner(), "pre-swap: runner unchanged (structural change invisible)")

	// 4) SwapExecutor：非重启换代生效（下一 turn 起新 runner）。
	resident.SwapExecutor(newRunner)
	require.Same(t, newRunner, resident.Runner(), "post-swap: next turn sees the new runner")

	// 5) 常驻不变量：org 级基础设施原封（状态⊥执行器）。
	require.Same(t, oldStore, resident.MemStore(), "fact chain must survive the swap")
	require.Same(t, oldSvc, resident.SessionSvc(), "session service must survive the swap")
	require.Same(t, oldTM, resident.TaskManager(), "task registry must survive the swap")

	// 6) Rollback 面：钩子注入 + 触发（ring 2 数据源在懒检查闭包内，此处验证接线）。
	rolled := false
	resident.SetRollbackFn(func() { rolled = true })
	resident.Rollback()
	require.True(t, rolled, "Rollback() must invoke the wired hook")
	resident.SetRollbackFn(nil)
	resident.Rollback() // no-op, must not panic
}

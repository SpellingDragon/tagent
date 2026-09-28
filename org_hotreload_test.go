package tagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/SpellingDragon/tagent/tool/action"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// cfgFor builds a minimal valid Config for fingerprint tests.
func cfgFor() *Config {
	return &Config{
		Entry:     "main",
		PromptDir: "resources/prompts",
		Providers: map[string]ProviderConfig{
			"p1": {Provider: "openai", APIEndpoint: "https://api.example.com"},
		},
		Agents: map[string]AgentConfig{
			"main": {
				Model:             "gpt-x",
				CompressThreshold: 0.8,
				Tools:             []ToolRef{{Kind: "tool", ID: "recall"}},
			},
			"sub": {
				Model: "gpt-y",
			},
		},
	}
}

func TestOrgFingerprint_StableAcrossEmptyChanges(t *testing.T) {
	a, err := computeOrgFingerprint(cfgFor())
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}

	// Semantic no-ops at the org layer: governance dir, reliability dirs,
	// per-agent memory path, top-level APIEndpoint — none may change the fp.
	b := cfgFor()
	b.Governance.Dir = "data/gov2"
	b.Reliability.BusSpillDir = "data/bus2"
	am := cfgFor()
	am.Agents["main"] = AgentConfig{Model: "gpt-x", CompressThreshold: 0.8, Tools: []ToolRef{{Kind: "tool", ID: "recall"}}, Memory: MemoryConfig{Path: "data/mem2"}}
	c := cfgFor()
	c.APIEndpoint = "https://other.example.com"
	// compress_threshold is hot-applicable (ApplyOrgParams) → excluded from
	// the fingerprint: changing it alone must NOT force a rebuild (D3).
	act := cfgFor()
	act.Agents["main"] = AgentConfig{Model: "gpt-x", CompressThreshold: 0.5, Tools: []ToolRef{{Kind: "tool", ID: "recall"}}}

	for name, mod := range map[string]*Config{"gov": b, "mem": am, "api": c, "ct": act} {
		fp, err := computeOrgFingerprint(mod)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if fp != a {
			t.Errorf("%s: excluded field changed fingerprint %s.. -> %s..", name, a[:8], fp[:8])
		}
	}
}

func TestOrgFingerprint_ChangesOnOrgFields(t *testing.T) {
	base, err := computeOrgFingerprint(cfgFor())
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}

	mut := []struct {
		name string
		mod  func(*Config)
	}{
		{"tools", func(c *Config) {
			ac := c.Agents["main"]
			ac.Tools = []ToolRef{{Kind: "tool", ID: "recall"}, {Kind: "tool", ID: "knowledge"}}
			c.Agents["main"] = ac
		}},
		{"agent_added", func(c *Config) { c.Agents["extra"] = AgentConfig{Model: "gpt-z"} }},
		{"agent_removed", func(c *Config) { delete(c.Agents, "sub") }},
		{"provider_endpoint", func(c *Config) {
			p := c.Providers["p1"]
			p.APIEndpoint = "https://v2.example.com"
			c.Providers["p1"] = p
		}},
		{"entry", func(c *Config) { c.Entry = "sub" }},
		{"model", func(c *Config) { ac := c.Agents["main"]; ac.Model = "gpt-w"; c.Agents["main"] = ac }},
	}
	for _, m := range mut {
		c := cfgFor()
		m.mod(c)
		fp, err := computeOrgFingerprint(c)
		if err != nil {
			t.Fatalf("%s: %v", m.name, err)
		}
		if fp == base {
			t.Errorf("%s: org field changed but fingerprint did not", m.name)
		}
	}
}

func TestOrgFingerprint_CanonicalStable(t *testing.T) {
	// Two configs that differ only in YAML-level noise (agent map order is a
	// Go map here; the marshal path must sort) produce identical fingerprints.
	c1, c2 := cfgFor(), cfgFor()
	f1, err := computeOrgFingerprint(c1)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	f2, err := computeOrgFingerprint(c2)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	if f1 != f2 {
		t.Errorf("canonical form unstable: %s.. vs %s..", f1[:8], f2[:8])
	}
}

// TestOrgFingerprint_ChangesOnGlobalModelDefaults 钉住 全局 provider 与 model 必须参与组织指纹。
// - 二者驱动子 agent 实例的解析；只改 yaml 时若不进指纹，这次翻转对热更完全隐身。
func TestOrgFingerprint_ChangesOnGlobalModelDefaults(t *testing.T) {
	// 5.1: global provider/model now drive sub-agent instance resolution
	// (resolveGlobalDefaultModel), so they must participate in the org
	// fingerprint — otherwise yaml-only flips stay invisible to hot-reload.
	base := &Config{Entry: "main", Provider: "zhipu", Model: "glm-5.3-flash"}
	fp0, err := computeOrgFingerprint(base)
	if err != nil {
		t.Fatalf("baseline fingerprint: %v", err)
	}
	for _, mut := range []struct {
		name string
		mut  func(c *Config)
	}{
		{"model", func(c *Config) { c.Model = "glm-5.3" }},
		{"provider", func(c *Config) { c.Provider = "deepseek" }},
	} {
		c := *base
		mut.mut(&c)
		fp, err := computeOrgFingerprint(&c)
		if err != nil {
			t.Fatalf("%s: fingerprint: %v", mut.name, err)
		}
		if fp == fp0 {
			t.Errorf("%s change did NOT alter org fingerprint (fp %s)", mut.name, fp[:8])
		}
	}
}

// TestMemoryFingerprint_DetectsMemoryOnlyChanges 钉住 白名单语义：只改存储段时组织指纹必须保持不变。
// - 因此存储段的变更必须由重载路径上的真实比较独立感知；
// - 否则懒检查会静默走数值分支——变更不生效，也不告警。
func TestMemoryFingerprint_DetectsMemoryOnlyChanges(t *testing.T) {
	base := cfgFor()
	orgFP, err := computeOrgFingerprint(base)
	if err != nil {
		t.Fatalf("org fp: %v", err)
	}
	// 仅改 memory 段：org 指纹必须不变（白名单语义，活函数 computeOrgFingerprint 的
	// 合同）。memory-only 变更的检测本身由 changedMemoryAgents/residentMemFP 在真实
	// reload 路径上承担（org_hotadd_test.go / hotreload_multiagent_test.go 锚）；
	// 2026-09-27 死代码审计删除了从未接线的全局 computeMemoryFingerprint。
	mod := cfgFor()
	mod.Agents["main"] = AgentConfig{
		Model: "gpt-x", Tools: []ToolRef{{Kind: "tool", ID: "recall"}},
		Memory: MemoryConfig{Type: "file", Path: "data/mem2"},
	}
	orgFP2, err := computeOrgFingerprint(mod)
	if err != nil {
		t.Fatalf("org fp2: %v", err)
	}
	if orgFP2 != orgFP {
		t.Errorf("memory-only change must NOT alter the org fingerprint (D3 whitelist)")
	}
	// 非 memory 变更不误报：org 指纹必须变（热更可达）。
	orgMod := cfgFor()
	a := orgMod.Agents["main"]
	a.Model = "gpt-z"
	orgMod.Agents["main"] = a
	if ofp, err := computeOrgFingerprint(orgMod); err != nil || ofp == orgFP {
		t.Errorf("non-memory change must alter the org fingerprint (err=%v)", err)
	}
}

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

// TestOrgHotShift_EndToEnd 钉住 不重启进程驱动完整的热更懒检查闭环，对象是真实构造的 agent。
// - 次序为修改时间变动 → 重新解析 → 指纹比较 → 指纹不变则走数值热更；
// - 指纹变化而配置损坏时必须继续服务旧代，不得半改。
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

// TestOrgHotReload_ExecutorSwapEndToEnd 钉住 结构变更后经只含执行器的面装配换入新执行器。
// - 换代不需重启，且常驻不变量原封不动；
// - 与懒检查那条互补：这一条只钉换缝的缝合点；
// - 不换入时运行器引用永不变化，正是必须重启这一旧方案的失败形状。
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

	// 4) 非重启换代生效（下一 turn 起新 runner）——走 CM 级发布入口；
	// `TagentAgent.SwapExecutor` 那层纯转发已按 §5.1 删除（它自己的文档就把
	// 编排换代指向 CM 级入口，且无生产调用方）。
	resident.ContextManager().PublishExecutor(newRunner, resident.ContextManager().ExecutorConfig())
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

// §5.3 基准（生产形状）与「不以无限历史保留全部执行器/agent」的限界证明。
// agent 层的原子成本基准（BeginTurn / 候选构造 / 发布）见
// agent/executor_publish_bench_test.go。

func ownerYAMLWithModel(t testing.TB, model string, targets []string, sub2Mem string) string {
	t.Helper()
	return strings.Replace(ownerYAML(t, targets, sub2Mem), "model: test-model\n", "model: "+model+"\n", 1)
}

func writeBumped(tb testing.TB, yamlPath, content string, tick *time.Time) {
	tb.Helper()
	require.NoError(tb, os.WriteFile(yamlPath, []byte(content), 0o644))
	*tick = tick.Add(2 * time.Second)
	require.NoError(tb, os.Chtimes(yamlPath, *tick, *tick))
}

// BenchmarkOrgReloadHotPath 度量「turn 起点检查」在生产热路径上的真实开销：一次
// os.Stat + mtime 比较即返回（未变更）。这是 §3.1 把检查从每次 BeforeModel 移到
// 每 turn 一次之后仍要付的代价——它必须与代际数、拓扑大小无关。
func BenchmarkOrgReloadHotPath(b *testing.B) {
	dir := b.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeBumped(b, yamlPath, ownerYAMLWithModel(b, "bench-model", []string{"sub1", "sub2"}, sub2MemDefault(b)), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(b, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(b, err)
	defer func() { _ = entry.Close() }()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry.CheckOrgReload()
	}
}

// BenchmarkCandidateConstruction_RealOrg 度量真实组织形状（entry + 两个子 agent，
// 逐身份借用常驻 store）下「构造一个候选」的开销——热更中最重的一步，且它在发布
// 之前完成，故不阻塞在线 turn。
func BenchmarkCandidateConstruction_RealOrg(b *testing.B) {
	dir := b.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeBumped(b, yamlPath, ownerYAMLWithModel(b, "bench-model", []string{"sub1", "sub2"}, sub2MemDefault(b)), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(b, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(b, err)
	defer func() { _ = entry.Close() }()

	cm := entry.ContextManager()
	face := cm.ExecutorConfig()

	// §5.3: every constructed candidate gets a cleanup exit. The previous version
	// left b.N un-installed runners to the process — the number was fine but the
	// test itself leaked exactly what the change is about (object lifespan).
	// Abandon cost is measured separately at the agent layer, not hidden here.
	var prev *agent.StagedGeneration
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if prev != nil {
			prev.Discard()
		}
		cand := cm.NewExecutorCandidate(face)
		if cand == nil {
			b.Fatal("candidate construction returned nil")
		}
		prev = cm.StageExecutor(cand, face, nil)
	}
	b.StopTimer()
	if prev != nil {
		prev.Discard()
	}
	if refs := cm.ExecutorRefs(); refs.PendingRetirees != 0 || refs.InFlightTurns != 0 {
		b.Fatalf("construction plus abandon must retire nothing and hold no refs: %+v", refs)
	}
}

// BenchmarkConfigRead_ProductionShape measures the READ phase alone: parsing the
// production-shape config. §5.3 requires it kept apart from any edit (no file
// write / mtime touch inside the loop) and apart from construction — a reload cost
// reported as one number cannot tell you which of the two to optimize, and the
// production sample showed re-parse is a large share of a full reload.
func BenchmarkConfigRead_ProductionShape(b *testing.B) {
	dir := b.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeBumped(b, yamlPath, prodShapeYAML(b, "bench-read"), &tick)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := LoadConfig(yamlPath); err != nil {
			b.Fatal(err)
		}
	}
}

// TestOrgGenerationsStructuresStayBounded 钉住 连续换代只推进序号，任何簿记结构的规模都恒等于拓扑大小。
// - 空闲态不得留下未回收的执行器；
// - 容量为二的回滚环在任意多代之后仍然可用。
func TestOrgGenerationsStructuresStayBounded(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	const generations = 25

	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "gen-0", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	for i := 1; i <= generations; i++ {
		writeBumped(t, yamlPath, ownerYAMLWithModel(t, "gen-"+strings.Repeat("x", i), []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)
		entry.CheckOrgReload()
		require.Equal(t, int64(i), entry.OrgDiagnostics()["generation"],
			"generation %d must publish", i)
	}

	st := entry.OrgDiagnostics()
	require.Equal(t, int64(generations), st["generation"])

	// 结构规模与代际数无关（序号会涨，容器不会）。
	require.Len(t, residentCacheForTest(entry), 3, "the resident binding table stays topology-sized across generations")
	rec, ok := st["agents"].([]OrgAgentApply)
	require.True(t, ok)
	require.Len(t, rec, 3, "the receipt set stays topology-sized — no per-generation accumulation")
	refs := entry.ContextManager().ExecutorRefs()
	require.Zero(t, refs.PendingRetirees, "an idle process holds no unreclaimed retired executors")

	// ring 只有两个槽位（current/prev 是类型事实），故多代之后回滚仍可执行并计为新序号。
	entry.Rollback()
	require.Equal(t, int64(generations+1), entry.OrgDiagnostics()["generation"],
		"rollback after many generations republishes as a new sequence")
	require.Len(t, residentCacheForTest(entry), 3, "rollback neither grows nor drops the resident table")
}

// ---------- §2.3：热更成本样本（不再充当短锁证明） ----------
//
// 撤回声明（§2.3 D3）：本节一度以「整次热更均值 < 10ms」作为「长构建不占用请求获取
// 短锁」的**完成证明**。这个推断不成立——均值只说明本批构建碰巧快，既不保证构建真的
// 停在 `mu` 之下，也不保证获取路径不碰 `mu`；负载抖动或快机器都能让一个会阻塞的实现
// 暂时通过均值门（design D3「均值约 2ms 和 10ms 测试阈值仅为性能样本，不得替代短锁
// 合同」、D12「移除宿主均值阈值……作为正确性门……短锁用阻塞屏障」）。
//
// 短锁合同改由 org_d3_scheduling_test.go 的阻塞屏障测**结构地**证明：parked 构建确实
// 持 `mu`（对照测里同步 mu-taker 被挡到超时），而业务获取恒不碰 `mu`。本函数只保留为
// 成本**观测样本**（t.Logf），不再对单次/均值耗时设正确性断言；性能与有界性的重验归
// §5.3（分测读配置/构建/提交/获取/回收）。文件写入与 mtime 触碰是"编辑动作"，不计入。

// prodShapeYAML renders that shape; `model` flips the org fingerprint so every
// reload really publishes.
func prodShapeYAML(t testing.TB, model string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("entry: orchestrator\nprompt_dir: resources/prompts\nmodel: " + model +
		"\nproviders:\n  openai:\n    api_endpoint: \"http://localhost:1\"\nagents:\n  orchestrator:\n    system_prompt:\n      inline: \"orchestrator\"\n    max_tool_iterations: 24\n    tools:\n")
	for i := 1; i <= 4; i++ {
		name := fmt.Sprintf("worker%d", i)
		fmt.Fprintf(&b, "      - kind: agent\n        agent: %s\n        description: %q\n", name, name)
	}
	b.WriteString("      - kind: tool\n        id: recall\n        description: \"recall\"\n")
	for i := 1; i <= 4; i++ {
		name := fmt.Sprintf("worker%d", i)
		fmt.Fprintf(&b, "  %s:\n    system_prompt:\n      inline: %q\n    keep_recent_tasks: 4\n    memory:\n      type: memory\n      path: %q\n",
			name, name, testStore(t, "prod-"+name))
	}
	return b.String()
}

// TestReloadCostAtProductionShape_Sample 钉住 报告生产形状（一个入口加四个子项）下一次完整重载与发布的开销。
// - 该数字只作观察量，不设墙钟上界：短锁是结构性性质，由调度那条用例钉住；
// - 必须保留每一轮都真实发布这一反空转前提，样本才有意义。
func TestReloadCostAtProductionShape_Sample(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeAt := func(model string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(prodShapeYAML(t, model)), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}
	writeAt("prod-a")

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	const rounds = 20
	start := time.Now()
	for i := 0; i < rounds; i++ {
		writeAt([]string{"prod-b", "prod-c"}[i%2]) // 与启动形状（prod-a）都不同，且两两交替
		entry.CheckOrgReload()
	}
	elapsed := time.Since(start)

	st := entry.OrgDiagnostics()
	require.Equal(t, int64(rounds), st["generation"],
		"every round must really publish (otherwise this sample measures nothing)")

	per := elapsed / time.Duration(rounds)
	t.Logf("[perf sample] production shape (entry+4 subagents): one full reload+publish = %v (avg over %d rounds) — observational, not a correctness gate",
		per.Round(time.Microsecond), rounds)

	// 分解：整次热更里最贵的其实不是"构建"，而是**重新解析 YAML**（同样只记录，不设门）。
	parseStart := time.Now()
	for i := 0; i < rounds; i++ {
		if _, lerr := LoadConfig(yamlPath); lerr != nil {
			t.Fatal(lerr)
		}
	}
	parsePer := time.Since(parseStart) / time.Duration(rounds)
	t.Logf("[perf sample] of which config re-parse alone = %v", parsePer.Round(time.Microsecond))
}

// TestPerPublishObjectLifespan 钉住 对固定拓扑，按对象身份记录冷启动、重载与回滚各自构造了什么。
// - 组织级计数不得当证据：活的 TaskManager 计数在复用与每轮丢弃副本两种形状下都不变；
// - 一次发布应为每个可达属主恰好构造一个新的执行面，此外什么都不构造；
// - 不得出现第二个 agent、第二个 TaskManager、重新绑定的存储、新的会话服务或第二个维护生产者；
// - 回滚必须与正向发布同价——它们是同一条代码路径。
func TestPerPublishObjectLifespan(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()

	writeBumped(t, yamlPath, prodShapeYAML(t, "life-a"), &tick)
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	// (no auxiliary types needed) {

	cold := map[string]*struct {
		agent, cm, tm, store, sess, runner interface{}
	}{}
	for name, owner := range residentCacheForTest(entry) {
		cold[name] = &struct {
			agent, cm, tm, store, sess, runner interface{}
		}{owner, owner.ContextManager(), owner.TaskManager(), owner.MemStore(), owner.SessionSvc(), owner.Runner()}
	}
	require.Len(t, cold, 5, "production shape: entry + 4 workers")

	compare := func(stage string, before map[string]any, owners map[string]interface{}) {
		t.Helper()
	}
	_ = compare

	// Helper: rebuild the same six-tuple for the current state.
	current := func() map[string][6]interface{} {
		out := map[string][6]interface{}{}
		for name, owner := range residentCacheForTest(entry) {
			out[name] = [6]interface{}{owner, owner.ContextManager(), owner.TaskManager(), owner.MemStore(), owner.SessionSvc(), owner.Runner()}
		}
		return out
	}

	before := current()
	writeBumped(t, yamlPath, prodShapeYAML(t, "life-b"), &tick)
	entry.CheckOrgReload()
	require.Equal(t, int64(1), diagInt64(t, entry.OrgDiagnostics(), "generation"), "precondition: reload #1 published")

	after := current()
	reused, rebuilt := 0, 0
	for name, b := range before {
		a := after[name]
		labels := []string{"owner", "contextManager", "taskManager", "memStore", "sessionSvc", "runner"}
		for i := range labels {
			if b[i] == a[i] {
				reused++
			} else {
				rebuilt++
				require.Equalf(t, "runner", labels[i],
					"§5.3 reload: only the execution face may be reconstructed (owner %q had %s replaced)", name, labels[i])
			}
		}
	}
	require.Equal(t, 5, rebuilt, "one new runner per reachable owner, no more")
	require.Equal(t, 25, reused, "everything else (5 owners × 5 objects) must be the same instances")

	// A second reload: same shape — no accumulation, and the previous generation is
	// not kept alive as a second agent set.
	writeBumped(t, yamlPath, prodShapeYAML(t, "life-c"), &tick)
	entry.CheckOrgReload()
	after2 := current()
	rebuilt2 := 0
	for name, b := range after {
		for i := range b {
			if b[i] != after2[name][i] {
				rebuilt2++
			}
		}
	}
	require.Equal(t, 5, rebuilt2, "reload #2 constructs exactly the 5 faces again")
	require.Len(t, residentCacheForTest(entry), 5, "and no owner set growth across generations")

	// Rollback must be the SAME cost, because it is the same code path (§2.4).
	rolledBefore := current()
	entry.Rollback()
	rolledAfter := current()
	rolledRebuilt := 0
	for name, b := range rolledBefore {
		for i := range b {
			if b[i] != rolledAfter[name][i] {
				rolledRebuilt++
			}
		}
	}
	require.Equal(t, 5, rolledRebuilt,
		"rollback reconstructs exactly the execution faces — it must not copy the agent set either")

	t.Logf("[§5.3 lifespan] topology=5 owners | per publish (cold→reload→reload→rollback): new TagentAgent=0, new ContextManager=0, new TaskManager=0, re-bound store=0, new session service=0, new runners=5 — the old shell path built one extra whole agent per CHANGED owner in addition to these")
}

// hotYAML renders the three-agent topology used by the hot-reload tests:
// entry references sub1+sub2 (kind: agent); each agent has its OWN isolated
// memory store so per-agent store identity is observable.
func hotYAML(t testing.TB, keep1, keep2 int, entryExtraTool bool) string {
	t.Helper()
	extra := ""
	if entryExtraTool {
		extra = fmt.Sprintf("      - kind: tool\n        id: recall\n        description: %q\n", "extra-"+fmt.Sprint(time.Now().UnixNano()))
	}
	keepLine := func(n int) string {
		if n <= 0 {
			return "" // omitted → field deleted → falls back to parsed default (4.7)
		}
		return fmt.Sprintf("    keep_recent_tasks: %d\n", n)
	}
	return fmt.Sprintf(`entry: main
prompt_dir: resources/prompts
model: test-model
providers:
  openai:
    api_endpoint: "http://localhost:1"
agents:
  main:
    system_prompt:
      inline: "main"
    tools:
      - kind: agent
        agent: sub1
        description: "sub1"
      - kind: agent
        agent: sub2
        description: "sub2"
%s
  sub1:
    system_prompt:
      inline: "sub1"
    memory:
      type: localfile
      path: %q
%s  sub2:
    system_prompt:
      inline: "sub2"
    memory:
      type: localfile
      path: %q
%s`, extra, testStore(t, "hottest-sub1"), keepLine(keep1), testStore(t, "hottest-sub2"), keepLine(keep2))
}

// hotYAMLAddSub3 renders hotYAML plus a THIRD sub-agent, leaving every
// pre-existing agent's memory section byte-identical so the only delta is the
// topology add (§4.3). The added agent gets its own localfile store, which is
// what makes “did it really acquire its own resource?” observable.
func hotYAMLAddSub3(t testing.TB, keep1, keep2 int) string {
	t.Helper()
	s := hotYAML(t, keep1, keep2, false)
	// 解释型字符串：raw literal 里的 \n 不是换行，匹配不上就会把新增
	// 做成“只定义不可达”的候选（它不改变拓扑，只改变指纹）。
	s = strings.Replace(s, `description: "sub2"`,
		"description: \"sub2\"\n      - kind: agent\n        agent: sub3\n        description: \"sub3\"", 1)
	return s + fmt.Sprintf("  sub3:\n    system_prompt:\n      inline: \"sub3\"\n    memory:\n      type: localfile\n      path: %q\n",
		testStore(t, "hottest-sub3"))
}

// LoadConfigForTest loads the YAML config (real strict parsing path).
func LoadConfigForTest(t *testing.T, path string) Config {
	t.Helper()
	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	return *cfg
}

// residentCacheForTest returns the resident binding table (4.5 introspection).
func residentCacheForTest(ta *agent.TagentAgent) map[string]*agent.TagentAgent {
	return ta.ResidentTable()
}

func keepRecentOf(ta *agent.TagentAgent) int { return ta.OrgKeepRecent() }

// TestHotReload_MixedChange_IdentityAndParams 钉住 在入口加两个子项的拓扑上同时变更工具（结构）与子项预算（数值）。
// - 各 agent 的常驻存储身份不漂移：指针不变，子项不被换成入口那一份；
// - 数值参数作用于新代的真实对象；
// - 字段删除回落到解析默认，按全量期望值处理；
// - 新增 agent 拒绝热更，走失败关闭。
func TestHotReload_MixedChange_IdentityAndParams(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := func(content string) {
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
	}
	write(hotYAML(t, 2, 2, false))

	ta, err := New(LoadConfigForTest(t, yamlPath), WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer ta.Close()

	// 常驻身份快照（3 agents：main + sub1 + sub2）。
	cache := residentCacheForTest(ta)
	require.Len(t, cache, 3)
	idMain := cache["main"].MemStore()
	idSub1 := cache["sub1"].MemStore()
	idSub2 := cache["sub2"].MemStore()
	require.NotSame(t, idSub1, idMain, "sub1 must own its own store")
	require.NotSame(t, idSub2, idMain, "sub2 must own its own store")

	// 混合变更：entry 加一个工具（结构，org fingerprint 变）+ sub1 keep_recent_tasks 2→5。
	write(hotYAML(t, 5, 2, true))
	ta.CheckOrgReload()

	// ① 身份不漂移。
	require.Same(t, idMain, cache["main"].MemStore(), "entry store identity unchanged")
	require.Same(t, idSub1, cache["sub1"].MemStore(), "sub1 store identity unchanged (no drift to entry store)")
	require.Same(t, idSub2, cache["sub2"].MemStore(), "sub2 store identity unchanged")

	// ② 数值热生效于 sub1 的真实压缩器（新代对象）。
	require.Equal(t, 5, keepRecentOf(cache["sub1"]), "sub1 keepRecent hot-applied")
	// ③ sub2 字段未变 → 不受影响（仍为启动值 2 的语义）。
	require.Equal(t, 2, keepRecentOf(cache["sub2"]), "untouched agent keeps its configured value")

	// ④ 新增 agent 热并入（§4.3，D7）：delta 只有拓扑新增——其余 agent 的 memory 段
	// 逐字节不变。旧版本步候选同时删掉了所有 memory 段，于是它**看起来**通过、实际
	// 上拒绝来自“memory 变更须重启”规则而非拓扑规则（本节因此重写为真实形态）。
	write(hotYAMLAddSub3(t, 5, 2))
	ta.CheckOrgReload()

	table := residentCacheForTest(ta)
	require.Len(t, table, 4, "the hot-added agent joins the resident binding table")
	require.NotNil(t, table["sub3"], "a hot add must be merged, not refused")
	require.NotSame(t, idMain, table["sub3"].MemStore(), "the added agent owns its own store (no drift onto the entry store)")
	require.NotSame(t, idSub1, table["sub3"].MemStore(), "…and does not borrow a sibling's store")
	// 既有三者身份在**真发布**下仍不漂移（不仅适用于被拒的重试）。
	require.Same(t, idMain, table["main"].MemStore(), "entry store identity survives a real publish")
	require.Same(t, idSub1, table["sub1"].MemStore(), "sub1 store identity survives the add")
	require.Same(t, idSub2, table["sub2"].MemStore(), "sub2 store identity survives the add")
	// 新增者按原资源/恢复协议构造，因此同样在数值热更覆盖面内（子 agent 缺省 keepRecent=2）。
	require.Equal(t, 2, keepRecentOf(table["sub3"]), "the added agent starts at its parsed default")
}

func TestHotReload_MemoryRejectionNotEffective(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := func(content string) {
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
	}
	base := hotYAML(t, 2, 2, false)
	write(base)
	ta, err := New(LoadConfigForTest(t, yamlPath), WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer ta.Close()
	cache0 := residentCacheForTest(ta)
	idMainKeep := cache0["main"].MemStore()
	idSub1Keep := cache0["sub1"].MemStore()

	// memory 段变更（sub1 fsync off）→ 拒绝。
	modified := fmt.Sprintf(`entry: main
prompt_dir: resources/prompts
model: test-model
providers:
  openai:
    api_endpoint: "http://localhost:1"
agents:
  main:
    system_prompt:
      inline: "main"
    tools:
      - kind: agent
        agent: sub1
        description: "sub1"
      - kind: agent
        agent: sub2
        description: "sub2"
  sub1:
    system_prompt:
      inline: "sub1"
    memory:
      type: localfile
      path: %q
      fsync: false
    keep_recent_tasks: 2
  sub2:
    system_prompt:
      inline: "sub2"
    memory:
      type: localfile
      path: %q
    keep_recent_tasks: 2
`, testStore(t, "hottest-sub1"), testStore(t, "hottest-sub2"))
	write(modified)
	ta.CheckOrgReload()

	// 再次编辑（其他字段变化）→ 仍以 effective 为基准拒绝（不因第二次检查推进）。
	write(base)
	ta.CheckOrgReload()
	// 验证：拒绝路径不改变任何常驻身份/资源（effective 不推进）。
	cache := residentCacheForTest(ta)
	require.Same(t, idSub1Keep, cache["sub1"].MemStore(), "rejected reload must not touch resident stores")
	_ = idMainKeep
}

var _ model.Model = (*stubModel)(nil)

// TestHotReload_FieldDeletionFallsBackToDefault 钉住 数值热应用取本代解析结果，不沿用上代生效值。
// - 定义里删除 keep_recent_tasks 后该 agent 回落解析默认（2），不会停在曾被热更成的 5；
// - 断言对象必须自己携带该字段：入口无此字段时按它断言只会看到默认值，测不到回落方向。
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func TestHotReload_FieldDeletionFallsBackToDefault(t *testing.T) {
	// 4.7：断言对象是 sub1（keep_recent_tasks 5 写在 sub1；entry 无此字段）。
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := func(c string) { require.NoError(t, os.WriteFile(yamlPath, []byte(c), 0o644)) }
	write(hotYAML(t, 5, 2, false))
	ta, err := New(LoadConfigForTest(t, yamlPath), WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer ta.Close()
	sub1 := residentCacheForTest(ta)["sub1"]
	require.Equal(t, 5, sub1.OrgKeepRecent(), "startup honors explicit keep_recent_tasks on sub1")

	write(hotYAML(t, 0, 2, false)) // field DELETED
	ta.CheckOrgReload()
	sub1 = residentCacheForTest(ta)["sub1"]
	require.Equal(t, 2, sub1.OrgKeepRecent(),
		"deleted field falls back to the parsed default (2) — full-desired semantics")
}

// dropAgentYAML renders the main+sub1+sub2 org with an explicit keep_recent_tasks
// per agent and the names in `drop` removed from BOTH the entry's tools and the
// agents map — a real removal (the draining-owner case §4.3 introduced).
func dropAgentYAML(t testing.TB, keeps map[string]int, drop ...string) string {
	t.Helper()
	dropped := map[string]bool{}
	for _, d := range drop {
		dropped[d] = true
	}
	var tools, agents strings.Builder
	for _, n := range []string{"sub1", "sub2"} {
		if dropped[n] {
			continue
		}
		fmt.Fprintf(&tools, "      - kind: agent\n        agent: %s\n        description: %q\n", n, n)
		fmt.Fprintf(&agents, "  %s:\n    system_prompt:\n      inline: %q\n    keep_recent_tasks: %d\n    memory:\n      type: localfile\n      path: %q\n",
			n, n, keeps[n], testStore(t, "hottest-drop-"+n))
	}
	return "entry: main\nprompt_dir: resources/prompts\nmodel: test-model\nproviders:\n  openai:\n    api_endpoint: \"http://localhost:1\"\nagents:\n  main:\n    system_prompt:\n      inline: \"main\"\n    keep_recent_tasks: " +
		fmt.Sprint(keeps["main"]) +
		fmt.Sprintf("\n    memory:\n      type: localfile\n      path: %q\n    tools:\n", testStore(t, "hottest-drop-main")) +
		tools.String() + agents.String()
}

// TestHotReload_RemovedOwnerKeepsItsParams 钉住 排空中的属主不参与数值热应用，本代仍路由的照常收新值。
// - 定义被同时删除时把解析默认下发给排水属主，会静默改掉仍在服务的旧代工作；
// - 豁免必须有向：只放过排水者，不得连带冻住本代仍路由的 agent（含入口）；
// - 回执要把两种结果分开：排水者 outcome 为 draining 且不携带已应用值。
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func TestHotReload_RemovedOwnerKeepsItsParams(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(dropAgentYAML(t, map[string]int{"main": 4, "sub1": 7, "sub2": 3}))
	ta, err := New(LoadConfigForTest(t, yamlPath), WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = ta.Close() }()

	table := residentCacheForTest(ta)
	require.Equal(t, 7, keepRecentOf(table["sub1"]), "baseline: sub1 serves its configured value")
	// §4.3 migrated: "removed keeps its params" is a statement about the drain window.
	// An unrouted-and-idle owner is retired now, so the window is made real here.
	draining := table["sub1"].ContextManager().AcquireLease(agent.LeaseSubCall)
	defer draining.Release()

	// Remove sub1 (tools AND definition) while raising sub2's value.
	write(dropAgentYAML(t, map[string]int{"main": 4, "sub2": 5}, "sub1"))
	ta.CheckOrgReload()

	after := residentCacheForTest(ta)
	require.NotNil(t, after["sub1"], "the removed agent keeps its resident owner (drain, not retire)")
	require.Equal(t, 7, keepRecentOf(after["sub1"]),
		"a draining owner must NOT be re-parameterized to parsed defaults")
	require.Equal(t, 5, keepRecentOf(after["sub2"]),
		"an agent this generation still routes to keeps receiving its new value")

	rec, ok := ta.OrgDiagnostics()["agents"].([]OrgAgentApply)
	require.True(t, ok, "the per-agent receipts must be observable on the diagnostics payload")
	byName := map[string]OrgAgentApply{}
	for _, r := range rec {
		byName[r.Name] = r
	}
	require.Equal(t, "draining", byName["sub1"].Outcome, "the receipt names the deliberate no-op")
	require.Zero(t, byName["sub1"].KeepRecentTasks, "draining carries no applied value")
	require.Equal(t, "applied", byName["sub2"].Outcome)
	require.Equal(t, 5, byName["sub2"].KeepRecentTasks)
	require.Equal(t, "applied", byName["main"].Outcome, "the entry is covered too")
}

// R04／L-5（resident-continuity「入口身份变更在资源获取前拒绝」）：热更比较新
// entry 与启动入口，不同则在任何候选资源构建前拒绝并提示重启——绝不用旧入口缺失
// 后的零配置发布。保留或删除旧定义都不改变该规则。
//
// 修前的缺陷：reloader 用启动时 cfg.Entry 计算可达集合并构建候选
// （reachableAgents(fresh, cfg.Entry) / snapshot.Agents[cfg.Entry]）。entry 改名
// 且旧定义被删除时，fresh.Agents[cfg.Entry] 是零值 AgentConfig——要么按空配置
// 发布一个丢掉全部委派工具的新代，要么把错误归到“executor rebuild”而非“入口变
// 更须重启”。二者都不是合同要求的提前拒绝。

const entryRenameStartupYAML = `entry: main
prompt_dir: resources/prompts
model: test-model
providers:
  openai:
    api_endpoint: "http://localhost:1"
agents:
  main:
    system_prompt:
      inline: "main"
    tools:
      - kind: agent
        agent: sub1
        description: "sub1"
  sub1:
    system_prompt:
      inline: "sub1"
    memory:
      type: memory
`

// entry renamed to "entry2", the old "main" definition deleted, "entry2" itself
// valid and delegating to sub1.
const entryRenameChangedYAML = `entry: entry2
prompt_dir: resources/prompts
model: test-model
providers:
  openai:
    api_endpoint: "http://localhost:1"
agents:
  entry2:
    system_prompt:
      inline: "entry2"
    tools:
      - kind: agent
        agent: sub1
        description: "sub1"
  sub1:
    system_prompt:
      inline: "sub1"
    memory:
      type: memory
`

// TestOrgHotReload_EntryIdentityChangeRefusedBeforeBuild 钉住 入口身份变化在任何候选资源构建之前被拒。
// - 拒绝必须点名入口，诊断面要能把它与普通执行器重建失败分开；
// - 结构代不前进，新入口名下零属主被构建，旧入口实例与其存储身份都不变；
// - 仍生效的那一代按已发布声明继续路由。
// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
func TestOrgHotReload_EntryIdentityChangeRefusedBeforeBuild(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)

	write(entryRenameStartupYAML)
	ta := buildOwnerAgent(t, yamlPath)

	before := residentCacheForTest(ta)
	require.NotNil(t, before["main"], "startup entry 'main' is resident")
	mainInstance, mainStore := before["main"], before["main"].MemStore()
	gen0 := ta.OrgDiagnostics()["generation"]
	require.Contains(t, entryToolNames(ta), "sub1", "the startup generation routes to sub1")

	// Rename the entry and drop the old definition.
	write(entryRenameChangedYAML)
	ta.CheckOrgReload()

	st := ta.OrgDiagnostics()
	require.Equal(t, gen0, st["generation"], "an entry-identity change never publishes")
	fail, ok := st["lastFailure"].(*OrgFailure)
	require.True(t, ok, "the refusal must be diagnosable")
	require.Contains(t, strings.ToLower(fail.Error), "entry",
		"the refusal must name the entry-identity change, not surface an opaque rebuild error")

	// Zero candidate resources built for the refused entry identity.
	after := residentCacheForTest(ta)
	require.Nil(t, after["entry2"], "no owner may be built for the refused entry identity")
	require.Same(t, mainInstance, after["main"], "the original entry owner is untouched")
	require.Same(t, mainStore, after["main"].MemStore(), "…and keeps its store identity")

	// The still-effective generation keeps serving with its published declaration.
	require.Contains(t, entryToolNames(ta), "sub1", "the old entry keeps routing to sub1")
}

// TestOrgHotReload_EntryRenameWithOldDefKeptAlsoRefused 钉住 入口身份规则不看旧定义去留。
// - 旧入口定义仍在 agents 里时，改名同样在构建之前被拒、新入口名下仍零属主；
// - 判定只取入口身份是否变化，携带两份合法配置的改名不得静默发布一代。
// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
func TestOrgHotReload_EntryRenameWithOldDefKeptAlsoRefused(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)

	write(entryRenameStartupYAML)
	ta := buildOwnerAgent(t, yamlPath)
	mainStore := residentCacheForTest(ta)["main"].MemStore()
	gen0 := ta.OrgDiagnostics()["generation"]

	// entry → entry2, but 'main' is NOT deleted (both definitions present).
	bothYAML := entryRenameChangedYAML + `  main:
    system_prompt:
      inline: "main"
    tools:
      - kind: agent
        agent: sub1
        description: "sub1"
`
	write(bothYAML)
	ta.CheckOrgReload()

	st := ta.OrgDiagnostics()
	require.Equal(t, gen0, st["generation"], "keeping the old definition must not let a rename publish")
	fail, ok := st["lastFailure"].(*OrgFailure)
	require.True(t, ok, "the refusal must be diagnosable")
	require.Contains(t, strings.ToLower(fail.Error), "entry")
	require.Nil(t, residentCacheForTest(ta)["entry2"], "still zero build for the new entry")
	require.Same(t, mainStore, residentCacheForTest(ta)["main"].MemStore(), "old entry owner untouched")
}

// runBounded runs fn and fails the test if it does not return within d, so a gate
// that hangs instead of refusing is reported as a hang — never as an indefinite suite.
func runBounded(t *testing.T, what string, d time.Duration, fn func() error) error {
	t.Helper()
	errCh := make(chan error, 1)
	go func() { errCh <- fn() }()
	select {
	case err := <-errCh:
		return err
	case <-time.After(d):
		t.Fatalf("%s 在已收敛关闭后未返回——闸门必须拒绝，不能挂住", what)
		return nil
	}
}

// TestExecGate_WorkAfterConvergedCloseIsRefused 钉住 已收敛退场的属主面对再进必须具名拒绝且有界返回。
// - 状态是排空完成、执行器已关、属主登记已撤销、已离开常驻表，不是"关闭已发起"；
// - 获取执行器被拒意味着交出的运行器为空，且不得在账面已清之后再登记新引用；
// - 输入接受与启动工作分别由环路闸门与世代闸门拒绝，两个具名哨兵都要断言，否则"被拒"的含义可被静默改掉；
// - 再进必须返回错误而非在已关闭执行器上跑完一回合，也必须自己返回而不是挂住。
// 契约: docs/wiki/platform/org-hot-reload.md#closed-owner-refusal
func TestExecGate_WorkAfterConvergedCloseIsRefused(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retYAML(t, "a", "s1", "s3"))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	s3 := residentCacheForTest(entry)["s3"]
	require.NotNil(t, s3, "precondition: s3 resident at startup")

	write(retYAML(t, "b", "s1"))
	entry.CheckOrgReload()
	require.NotContains(t, residentCacheForTest(entry), "s3", "precondition: s3 converged and left the table")
	require.True(t, s3.CloseStarted(), "precondition: its close completed, not just started")
	require.True(t, s3.Obligations().Idle(), "precondition: it holds no obligations")

	cm := s3.ContextManager()

	// ① Acquire: the gate must refuse, i.e. neither hand out a runnable executor nor
	// register a fresh reference on a converged generation.
	lease := cm.BeginTurnLease()
	require.NotNil(t, lease)
	if lease != nil {
		t.Cleanup(lease.Release)
		assert.Nil(t, lease.Runner(),
			"§3.2：已收敛关闭后 Acquire 必须被拒——把已关闭的执行器交给新 turn 正是 tryAcquireActive 注释里点名不可接受的失效")
	}
	assert.True(t, s3.Obligations().Idle(),
		"and a refused entry must not leave a new reference registered after the drain reported clean")

	// ② Inject: same refusal, surfaced to the caller.
	_, _, injErr := s3.InjectEnvelope(context.Background(), "stale-holder",
		[]model.Message{{Role: model.RoleUser, Content: "into a converged owner"}})
	// Measured, not assumed: input acceptance is refused by the LOOP gate (the pipeline is
	// gone), while starting work is refused by the GENERATION gate. Both are named, and
	// asserting the exact sentinel is what keeps a later refactor from quietly changing
	// what "refused" means here.
	assert.ErrorIs(t, injErr, agent.ErrLoopTerminated,
		"§3.2：关闭后的 Inject 必须被具名拒绝（输入接受面＝环路已终止）")

	// ③ Run: bounded, and an error rather than a turn executed on a closed executor.
	runErr := runBounded(t, "RunFlow", 10*time.Second, func() error {
		return cm.RunFlow(context.Background(), model.Message{Role: model.RoleUser, Content: "stale holder"})
	})
	assert.ErrorIs(t, runErr, agent.ErrExecClosed,
		"§3.2：关闭后的 Run 必须真正再进一次世代闸门并被具名拒绝，不得静默在已关闭执行器上跑完")
}

// §5.2（配置别名）：`FoldModelRefAliases` 把 legacy 扁平键（compress.summary_model
// / summary_provider / summary_effort）折进统一的 ModelRef（compress.summary.*），
// 显式 ModelRef 逐字段优先、legacy 只填未设字段。此折叠发生在 LoadConfig→ApplyDefaults，
// 因此**发布决策所比的 `computeOrgFingerprint` 看到的恒是折叠后的规范形**。
//
// 若折叠缺位或晚于指纹：同一语义写成 legacy 形与 canonical 形会算出不同指纹，运维把一
// 个弃用键改写成推荐键（值不变）就会被误判为**结构性变更 → 幻影发布一代**（白白重建
// executor、扰乱在途判定）。本测钉住这条 5.2 此前零覆盖的契约：别名折叠稳定、指纹不因
// 别名书写漂移、且被折叠字段仍真实参与指纹（否则稳定性是假绿）。

func aliasYAML(compressBlock string) string {
	return "entry: main\n" +
		"providers:\n  p1:\n    provider: openai\n    api_endpoint: https://api.example.com\n    api_key_env: TAGENT_TEST_API_KEY\n" +
		"agents:\n  main:\n    system_prompt:\n      inline: \"P\"\n" +
		compressBlock +
		"    memory:\n      type: memory\n"
}

// writeCfg writes content and returns the config loaded through the REAL parse path
// (LoadConfig runs ApplyDefaults → FoldModelRefAliases), which is exactly what the
// reloader feeds to computeOrgFingerprint on a fresh check (tagent.go:693).
func writeCfg(t *testing.T, dir, name, content string) *Config {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	cfg, err := LoadConfig(p)
	require.NoError(t, err, "config %s must load", name)
	return cfg
}

// TestModelRefAliasesFoldToStableFingerprint is the core §5.2 alias contract.
func TestModelRefAliasesFoldToStableFingerprint(t *testing.T) {
	dir := t.TempDir()

	// Legacy flat form and the canonical ModelRef form carry the SAME semantics.
	legacy := writeCfg(t, dir, "legacy.yaml", aliasYAML("    compress:\n      summary_model: summ\n"))
	canonical := writeCfg(t, dir, "canonical.yaml", aliasYAML("    compress:\n      summary:\n        model: summ\n"))

	// LoadConfig folded the legacy key into the unified holder and CLEARED it — so
	// the flat field can never be independently fingerprinted.
	require.Equal(t, "summ", legacy.Agents["main"].Compress.Summary.Model,
		"summary_model folded into compress.summary.model at load")
	require.Empty(t, legacy.Agents["main"].Compress.SummaryModel,
		"the legacy field is cleared after folding (not left as a second source)")

	// The whole point: the published generation must NOT depend on which spelling the
	// operator used. Equal folded configs → equal fingerprint → no phantom publish.
	require.Equal(t, mustFP(t, legacy), mustFP(t, canonical),
		"alias and canonical spellings of the same setting fold to an identical fingerprint")
}

// TestFingerprintFoldedFieldIsLive 钉住 被折叠字段必须真实参与指纹，稳定不得来自字段缺席。
// - 它是别名稳定性用例的对照组：折叠字段整体不进指纹时两种写法同样相等，真变更也一并隐身；
// - 同一条书写路径下的两个不同值必须让指纹随之变化。
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestFingerprintFoldedFieldIsLive(t *testing.T) {
	dir := t.TempDir()
	base := writeCfg(t, dir, "base.yaml", aliasYAML("    compress:\n      summary_model: summ\n"))
	changed := writeCfg(t, dir, "changed.yaml", aliasYAML("    compress:\n      summary_model: different\n"))
	require.NotEqual(t, mustFP(t, base), mustFP(t, changed),
		"the folded summary model is genuinely part of the fingerprint — stability is not from exclusion")
}

// TestFingerprintFoldIsIdempotentUnderRepeat 钉住 折叠必须是不动点：重跑既不搬值也不复活扁平键。
// - 第二次检查会拿到同一份已折叠的生效配置，重跑是常态而非例外；
// - 指纹必须逐字节不变，否则别名语义随检查次数漂移，代际比较失去意义。
// 契约: docs/wiki/platform/org-hot-reload.md#fingerprint
func TestFingerprintFoldIsIdempotentUnderRepeat(t *testing.T) {
	dir := t.TempDir()
	legacy := writeCfg(t, dir, "legacy.yaml", aliasYAML("    compress:\n      summary_model: summ\n      summary_provider: p1\n"))
	before := mustFP(t, legacy)

	require.Equal(t, "summ", legacy.Agents["main"].Compress.Summary.Model)
	require.Equal(t, "p1", legacy.Agents["main"].Compress.Summary.Provider)

	legacy.FoldModelRefAliases() // fold again — must be a no-op on already-folded state
	require.Equal(t, "summ", legacy.Agents["main"].Compress.Summary.Model, "value is not lost on re-fold")
	require.Empty(t, legacy.Agents["main"].Compress.SummaryModel, "legacy field does not reappear")
	require.Equal(t, before, mustFP(t, legacy), "re-folding does not drift the fingerprint")
}

// §2.3（design D3「调度裁决」/ spec swappable-executor「触发时机与观测」）最后一腿：
// 短锁必须由**阻塞屏障**证明，不能由平均耗时证明（本轮同时撤回 bench 里
// “一次热更均值 < 10ms 即短锁成立”的完成声明）。
//
// 被测契约（reload 全程在 `mu` 之下；业务 turn 的获取路径 requestCheck→currentRunner
// 只碰 os.Stat 与 executorMu，绝不碰 `mu`）：
//   - 候选构建停在可控屏障（orgBuildBarrier，停在 mu 之下）时，业务 turn 仍立即取得
//     当时已发布的旧 effective，不等待构建；解除屏障并发布后开始的 turn 才用新代。
//   - 运维同步 CheckOrgReload 停在屏障里等待结果时，业务获取路径不被封住。
//   - Close 先停调度再排空在途构建（drain 抢 mu，被 parked 构建挡住）时，业务获取仍
//     不被阻塞——「请求获取不等构建或 Close」。
//
// fail-before 语义：若获取路径错误地经 `mu` 串行化（历史形态或在途回归），下面每一例
// 的“有界获取”都会在 parked 构建释放前永久阻塞，acquireWithin 直接判红；而
// park.waitEntered 保证构建确实停在 mu 之下，杜绝“测了个寂寞”。

// buildPark arms orgBuildBarrier so a background reload parks while holding the
// reload mutex, and hands the test a deterministic enter/release handshake.
type buildPark struct {
	entered  chan struct{}
	release  chan struct{}
	relOnce  sync.Once
	enterSig sync.Once
}

func newBuildPark() *buildPark {
	p := &buildPark{entered: make(chan struct{}), release: make(chan struct{})}
	h := func() {
		// Signal entry exactly once, then block until released — all while the
		// caller (reload) holds `mu`.
		p.enterSig.Do(func() { close(p.entered) })
		<-p.release
	}
	orgBuildBarrier.Store(&h)
	return p
}

// waitEntered fails the test if no build actually reached the barrier (i.e. the
// build is genuinely parked under `mu`), so the bounded-acquire assertions below
// would otherwise measure nothing.
func (p *buildPark) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-p.entered:
	case <-time.After(10 * time.Second):
		p.letGo()
		t.Fatal("the candidate build never parked at the barrier — this run would measure nothing")
	}
}

func (p *buildPark) letGo() { p.relOnce.Do(func() { close(p.release) }) }

// disarm clears the global seam and, if the test bailed out early, frees any
// parked build so Close-style drains cannot hang the package.
func (p *buildPark) disarm() {
	orgBuildBarrier.Store(nil)
	p.letGo()
}

// acquireWithin runs a business-turn acquire on its own goroutine and fails if
// it does not return within `within`. A correct acquire touches only os.Stat and
// executorMu, never the reload mutex, so it must never be held by a parked build
// or a Close drain.
func acquireWithin(t *testing.T, ta *agent.TagentAgent, within time.Duration) (got any) {
	t.Helper()
	ch := make(chan any, 1)
	go func() {
		r, release := ta.ContextManager().BeginTurn()
		ch <- r
		release()
	}()
	select {
	case v := <-ch:
		return v
	case <-time.After(within):
		t.Fatalf("business acquire blocked >%s while a build/Close held the reload mutex — "+
			"the acquire path is NOT the short lock D3 requires (it must never touch `mu`)", within)
		return nil
	}
}

func genOf(ta *agent.TagentAgent) int64 {
	g, _ := ta.OrgDiagnostics()["generation"].(int64)
	return g
}

// TestOrgLazyCheck_BuildParkedDoesNotBlockTurnAcquire 钉住 懒检测不等候选构建，业务获取照常拿旧 effective。
// - 见证编辑的那一回合负责调度构建，自己继续用当前生效的一代；构建停在屏障上期间不得有任何发布；
// - 发布之前启动的回合同样拿旧 effective，代际切换只对发布之后启动的回合可见；
// - 屏障放行后构建必须完成并发布新一代，此后启动的回合拿到新执行器——否则"不阻塞"可能只是构建根本没跑。
// 契约: docs/wiki/platform/org-hot-reload.md#trigger-timing
func TestOrgLazyCheck_BuildParkedDoesNotBlockTurnAcquire(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "lazy-a", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	ta, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = ta.Close() }()

	// Prime lastSeenMtime with a synchronous check (no barrier armed) so the only
	// build this test schedules is the deliberate structural one below.
	ta.CheckOrgReload()
	require.EqualValues(t, 0, genOf(ta), "priming is a numeric no-op — no generation yet")

	oldRunner := acquireWithin(t, ta, 2*time.Second) // startup effective, no build scheduled (mtime unchanged)

	park := newBuildPark()
	defer park.disarm()

	// Structural edit → the next acquire schedules a background build that parks under mu.
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "lazy-b", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)
	during := acquireWithin(t, ta, 2*time.Second) // fires requestCheck → spawns the parked build
	park.waitEntered(t)

	// The build is genuinely parked under `mu`; a request acquire still returns the OLD
	// effective immediately — it never waits for the candidate.
	require.Same(t, oldRunner, during, "the witnessing turn keeps serving on the old effective")
	require.EqualValues(t, 0, genOf(ta), "nothing publishes while the build is parked")
	later := acquireWithin(t, ta, 2*time.Second)
	require.Same(t, oldRunner, later, "requests started before the publish also keep the old effective")

	// Release the barrier → the build completes and publishes as a new generation.
	park.letGo()
	require.Eventually(t, func() bool { return genOf(ta) == 1 }, 10*time.Second, 20*time.Millisecond,
		"the parked build must publish after release")

	after := acquireWithin(t, ta, 2*time.Second)
	require.NotSame(t, oldRunner, after, "a turn starting after the publish uses the new generation")
}

// TestOrgSyncCheck_WaitsButDoesNotBlockBusinessAcquire 钉住 手动同步检查等构建与发布，但不封住业务获取。
// - 运维驱动的同步检查停在屏障上并持有重载互斥量，返回时机必须是发布落地；
// - 它等待期间业务获取仍必须立刻拿回当前生效的那一代，且代际尚未轮转；
// - 屏障放行后同步检查必须自己返回（有界）、代际加一，此后启动的回合拿新执行器。
// 契约: docs/wiki/platform/org-hot-reload.md#trigger-timing
func TestOrgSyncCheck_WaitsButDoesNotBlockBusinessAcquire(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "sync-a", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	ta, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = ta.Close() }()

	ta.CheckOrgReload() // prime
	require.EqualValues(t, 0, genOf(ta))
	oldRunner := acquireWithin(t, ta, 2*time.Second)

	park := newBuildPark()
	defer park.disarm()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "sync-b", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	// Ops: synchronous check that will park mid-build holding `mu` and only return after publish.
	done := make(chan struct{})
	go func() { ta.CheckOrgReload(); close(done) }()
	park.waitEntered(t)

	// While the management call waits, a business acquire returns the old effective promptly.
	acq := acquireWithin(t, ta, 2*time.Second)
	require.Same(t, oldRunner, acq, "a business request still gets the old effective while ops waits")
	require.EqualValues(t, 0, genOf(ta), "the sync call has not published yet")

	// Release → the management call's publish lands and it returns.
	park.letGo()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the synchronous management call never returned after the barrier released")
	}
	require.EqualValues(t, 1, genOf(ta), "the management call published on return")

	after := acquireWithin(t, ta, 2*time.Second)
	require.NotSame(t, oldRunner, after, "turns started after the management call use the new generation")
}

// TestOrgBuildBarrierControl_MuTakersAreGated 钉住 对照组：停在屏障上的构建必须真的持有重载互斥量。
// - 持锁操作（第二次同步检查）要被挡住而超时，无锁的业务获取要照常返回——两条同时成立才说明挡的是锁而不是构建没跑；
// - 屏障不持锁、或业务获取偷偷取锁时，这条与它的对照用例一起变红，套件无法靠"什么都没测到"通过；
// - 放行后被挡的持锁操作必须最终完成，不得永久悬空。
// 契约: docs/wiki/platform/org-hot-reload.md#trigger-timing
func TestOrgBuildBarrierControl_MuTakersAreGated(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "ctl-a", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	ta, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = ta.Close() }()

	ta.CheckOrgReload() // prime lastSeenMtime
	park := newBuildPark()
	defer park.disarm()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "ctl-b", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	acquireWithin(t, ta, 2*time.Second) // spawn the background build
	park.waitEntered(t)                 // it holds `mu` now

	// A mutex-taking op must be gated by the parked build.
	muTaken := make(chan struct{})
	go func() { ta.CheckOrgReload(); close(muTaken) }()
	select {
	case <-muTaken:
		park.letGo()
		t.Fatal("control: a synchronous (mu-taking) reload returned while a build was parked — " +
			"the barrier is not holding `mu`, so the non-blocking tests would prove nothing")
	case <-time.After(500 * time.Millisecond):
		// expected: still blocked behind the parked build's `mu`
	}

	// The mu-free business acquire is NOT gated by the same parked build.
	acquireWithin(t, ta, 2*time.Second)

	park.letGo()
	select {
	case <-muTaken:
	case <-time.After(10 * time.Second):
		t.Fatal("the gated mu-taker never returned after the barrier released")
	}
}

// TestOrgCloseDrainDoesNotBlockBusinessAcquire 钉住 请求获取既不等构建也不等关闭排空。
// - 关闭已发起、其排空还堵在被停住的构建后面等锁时，业务获取仍必须立刻返回；
// - 放行后排空必须拿到锁并走完拆除，关闭有界返回——排空无上界即为缺陷。
// 契约: docs/wiki/platform/org-hot-reload.md#trigger-timing
func TestOrgCloseDrainDoesNotBlockBusinessAcquire(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "close-a", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	ta, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	// No deferred Close here: this test drives Close itself.

	ta.CheckOrgReload() // prime
	park := newBuildPark()
	defer func() { park.disarm(); _ = ta.Close() }()
	writeBumped(t, yamlPath, ownerYAMLWithModel(t, "close-b", []string{"sub1", "sub2"}, sub2MemDefault(t)), &tick)

	acquireWithin(t, ta, 2*time.Second) // trigger the lazy build
	park.waitEntered(t)                 // it now holds `mu`

	// Close begins: it flips `stopped`, then its drain blocks on `mu` behind the parked build.
	closed := make(chan struct{})
	go func() { _ = ta.Close(); close(closed) }()

	// Even with Close pending a drain and a build parked, a business acquire is not blocked.
	acquireWithin(t, ta, 2*time.Second)

	// Release → the build finishes, the drain acquires `mu`, teardown proceeds and Close returns.
	park.letGo()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close never returned after the parked build released — the drain is unbounded")
	}
}

// scHotAddNumericYAML: entry main → sub1; sub3 block controlled by addSub3.
// B's max_tokens/keep_recent move between renders (numeric-only for sub3).
func scHotAddNumericYAML(t testing.TB, sub3 bool, sub3MaxTokens int, sub1Keep int) string {
	t.Helper()
	sub3Tool, sub3Block := "", ""
	if sub3 {
		sub3Tool = `      - kind: agent
        agent: sub3
        description: "sub3"
`
		sub3Block = fmt.Sprintf(`  sub3:
    system_prompt:
      inline: "sub3"
    max_tokens: %d
    memory:
      type: localfile
      path: %q
`, sub3MaxTokens, testStore(t, "hottest-sc3"))
	}
	return fmt.Sprintf(`entry: main
prompt_dir: resources/prompts
model: test-model
providers:
  openai:
    api_endpoint: "http://localhost:1"
agents:
  main:
    system_prompt:
      inline: "main"
    tools:
      - kind: agent
        agent: sub1
        description: "sub1"
%s  sub1:
    system_prompt:
      inline: "sub1"
    keep_recent_tasks: %d
    memory:
      type: localfile
      path: %q
%s`, sub3Tool, sub1Keep, testStore(t, "hottest-sc1"), sub3Block)
}

// TestHotAddedAgentNumericOnlySeedsNextCall 钉住 数值热应用覆盖热增属主，其新调用种子取自提交点轮转后的记录。
// - 结构热增之后的纯数值编辑必须到达该属主的新子调用，冷启动与热增两条路同价；
// - 断言取宿主结果（预算线＝额度×阈值）而非字段回声，才能证明消费方真的现读；
// - 纯数值编辑不得增加结构代。
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func TestHotAddedAgentNumericOnlySeedsNextCall(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}
	// 基线：无 sub3。
	write(scHotAddNumericYAML(t, false, 0, 2))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	// 结构热增 sub3（max_tokens=4096 初值）。
	write(scHotAddNumericYAML(t, true, 4096, 2))
	entry.CheckOrgReload()
	require.Equal(t, int64(1), entry.OrgDiagnostics()["generation"].(int64), "热增须发布（否则后续断言空洞）")

	// numeric-only：sub3 max_tokens 4096→8100（sub1 keep 不动→不轮转指纹的其他轴）。
	write(scHotAddNumericYAML(t, true, 8100, 2))
	entry.CheckOrgReload()
	require.Equal(t, int64(1), entry.OrgDiagnostics()["generation"].(int64), "numeric-only 不加代")

	// 断言：热增 owner 的 effective 快照已随提交点轮转到新值——真实消费者读取
	// （OrgBudgetLine 读 compressor 真值，非 getter 回声）。
	table := residentCacheForTest(entry)
	sub3 := table["sub3"]
	require.NotNil(t, sub3, "sub3 须在常驻表")
	// OrgBudgetLine = maxTokens × threshold（0.8 默认）——8100 到达即 6480。
	require.Equal(t, 6480, sub3.OrgBudgetLine(),
		"S-C 锚1：热增→numeric-only 后，热增 owner 的新调用种子须读新值（8100×0.8；提交点单写者轮转）")
}

// scHot reads an owner's record-backed hot bundle (HotSnapshot resolves through
// the injected currentHotFor, i.e. the single committed application record; it
// falls back to the construction snapshot only with no record entry).
func scHot(t *testing.T, a *agent.TagentAgent) agent.OrgHotParams {
	t.Helper()
	require.NotNil(t, a, "owner 须在常驻表")
	p, ok := a.HotSnapshot()
	require.True(t, ok, "HotSnapshot 须可解析（记录源或构造快照）")
	return p
}

// TestAppliedRecordCommitsAtomicallyWithVersion 钉住 记录只在唯一提交临界区轮转，半提交对读者不可见。
// - 构建停在提交闸门内时，记录读者必须仍见上一次提交的值；
// - 压缩器的预算线经同一条记录解析，屏障内不得出现"新值已可见、记录仍旧值"的两轴分歧；
// - 放行后两轴同代收敛；纯数值路径若不在提交闸门停车，就以有界失败暴露而不是挂住。
// 契约: docs/wiki/platform/org-hot-reload.md#lockfree-read
func TestAppliedRecordCommitsAtomicallyWithVersion(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}
	// 启动基线：sub3 max_tokens=4096（冷启动常驻，非热增）。
	write(scHotAddNumericYAML(t, true, 4096, 2))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	sub3 := func() *agent.TagentAgent { return residentCacheForTest(entry)["sub3"] }

	// 第一次 numeric 提交（4096→5000，无屏障）：让唯一已提交记录里出现 sub3 条目。
	write(scHotAddNumericYAML(t, true, 5000, 2))
	entry.CheckOrgReload()
	require.Equal(t, 5000, scHot(t, sub3()).MaxTokens, "前置：第一次 numeric 提交后记录须轮转到 5000")

	// 第二次 numeric 编辑（5000→8100）停在提交点：applyHotAll 已 push（compressor=8100），
	// 但 recordHotApply 未跑——记录读者必仍见 5000。
	parked := make(chan struct{})
	release := make(chan struct{})
	park := func() {
		close(parked)
		<-release
	}
	orgCommitBarrier.Store(&park)
	t.Cleanup(func() { orgCommitBarrier.Store(nil) })
	// Teardown safety for every barrier-park test: a mid-window assertion failure
	// must not leave the rebuild goroutine parked, or the deferred Close's drain
	// waits the `mu` that parked reload still holds (hang, not failure). Cleanups
	// run LIFO, so this release runs before the Close cleanup registered above.
	releaseAll := func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}
	t.Cleanup(releaseAll)

	done := make(chan struct{})
	go func() {
		defer close(done)
		write(scHotAddNumericYAML(t, true, 8100, 2))
		entry.CheckOrgReload()
	}()
	select {
	case <-parked:
	case <-time.After(10 * time.Second):
		releaseAll()
		<-done
		t.Fatal("S-C 红：数值-only 提交点未触发 orgCommitBarrier——记录轮转未落在同一提交闸门（半提交不可见契约缺实现）")
	}

	// 屏障内：记录读者仍见上一次提交 5000（半提交不可见）；S-E 起压缩器亦经同一记录
	// 解析预算线 → 4000（5000×0.8），遮蔽 applyHotAll 已写入构造原子字段的新值。
	// 读权威唯一：不再有独立的 push 轴可与记录分歧。
	require.Equal(t, 5000, scHot(t, sub3()).MaxTokens,
		"S-C 锚2：提交点内、记录轮转前，记录读者仍见上一次的 5000（半提交不可见）")
	require.Equal(t, 4000, sub3().OrgBudgetLine(),
		"S-E 锚：压缩器边界读经记录解析（源遮蔽 push 写入的原子字段），非 push 真值")

	releaseAll()
	<-done

	require.Equal(t, 8100, scHot(t, sub3()).MaxTokens, "提交完成后记录轮转到新值 8100")
	require.Equal(t, 6480, sub3().OrgBudgetLine(), "记录轮转后压缩器经源解析到新代预算线（8100×0.8），两轴同代")
}

// TestAppliedRecordReadIsLockFree 钉住 记录读面绝不触碰协调器锁。
// - 压缩器在每个存活上下文管理器的每个消费边界解析数值组，持锁读会把提交临界区放到压缩读路径上；
// - 装置在测试持有该锁时从活协程读记录：持锁实现只能有界失败，无锁实现立刻返回。
// 契约: docs/wiki/platform/org-hot-reload.md#lockfree-read
func TestAppliedRecordReadIsLockFree(t *testing.T) {
	coord := newOrgCoordinator()
	coord.init("", nil)
	_, gen := coord.swap("fp1", nil, []appliedAgent{{Name: "x", Hot: agent.OrgHotParams{MaxTokens: 7}}})
	require.NotNil(t, gen)

	var got agent.OrgHotParams
	done := make(chan struct{})
	coord.mu.Lock()
	go func() {
		got, _ = coord.currentHotFor("x")
		close(done) // happens-before edge for the read below (-race clean)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		coord.mu.Unlock()
		t.Fatal("S-E 红：currentHotFor 仍依赖 coord.mu——记录读面未与提交临界区解耦（design §2 要求 S-E 无锁化，与 compressor 侧同源）")
	}
	coord.mu.Unlock()

	require.Equal(t, 7, got.MaxTokens, "无锁读仍须读到已提交记录")
}

// introduce-durable-workflow-engine §6.4（D4）root 面：装配/热更通路必须把每个
// owner 的「同身份热参快照」轮转到位——新发起子调用私有 CM 的播种源就是它。
// 私有 CM 播种与在途边界应用的机制面在 agent 包 `TestM34_*` 钉死；这里验的是
// reloader 经单一提交点（ApplyOrgHotParams）驱动这条通路后，快照与真消费者同代，
// 且回滚把快照一并恢复（draining-owner 语义由 §4.3 既有测覆盖，勿与此混淆）。

func snapshotRotationYAML(prompt string, subMax int, subThreshold float64) string {
	return "entry: main\n" +
		"providers:\n  p1:\n    provider: openai\n    api_endpoint: https://api.example.com\n    api_key_env: TAGENT_TEST_API_KEY\n" +
		"model: test-model\n" +
		"agents:\n  main:\n" +
		"    system_prompt:\n      inline: " + strconv.Quote(prompt) + "\n" +
		"    memory:\n      type: memory\n" +
		"    tools:\n      - kind: agent\n        agent: sub1\n        description: \"sub1\"\n" +
		"  sub1:\n" +
		"    system_prompt:\n      inline: \"sub1\"\n" +
		"    max_tokens: " + strconv.Itoa(subMax) + "\n" +
		"    compress_threshold: " + strconv.FormatFloat(subThreshold, 'f', -1, 64) + "\n" +
		"    keep_recent_tasks: 2\n" +
		"    memory:\n      type: memory\n"
}

func TestHotParamSnapshotRotatesAtCommitPoint(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(snapshotRotationYAML("A", 4000, 0.5))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	table := residentCacheForTest(entry)
	require.Contains(t, table, "sub1")
	sub1 := table["sub1"]

	// Construction seed at the root: every owner starts with a snapshot equal
	// to its parsed values — the seeding source for sub-invocations exists
	// from second zero, not only after the first hot edit.
	p, ok := sub1.HotSnapshot()
	require.True(t, ok, "New must seed every owner's hot snapshot")
	require.Equal(t, 4000, p.MaxTokens)
	require.InDelta(t, 0.5, p.ThresholdPct, 1e-9)
	require.Equal(t, 2000, sub1.OrgBudgetLine(), "snapshot and resident compressor agree")

	// Numeric-only edit (same prompt/structure): the reloader's per-owner
	// commit point must rotate the SNAPSHOT together with the resident
	// consumer — that rotation is what a freshly-built private CM seeds from.
	write(snapshotRotationYAML("A", 9000, 0.9))
	entry.CheckOrgReload()
	d := entry.OrgDiagnostics()
	require.EqualValues(t, 0, diagInt64(t, d, "generation"), "numeric-only: no structural bump")
	require.EqualValues(t, 1, diagInt64(t, d, "revision"))
	require.Equal(t, 8100, sub1.OrgBudgetLine(), "resident consumer took the new values")
	p, ok = sub1.HotSnapshot()
	require.True(t, ok)
	require.Equal(t, 9000, p.MaxTokens, "snapshot rotated at the SAME commit point (D4)")
	require.InDelta(t, 0.9, p.ThresholdPct, 1e-9)

	// Rollback restores the snapshot through the same single point, so the
	// NEXT sub-call seeds the pre-edit generation, not just the resident.
	entry.Rollback()
	p, _ = sub1.HotSnapshot()
	require.Equal(t, 4000, p.MaxTokens, "rollback restores the seeding source")
	require.Equal(t, 2000, sub1.OrgBudgetLine())
}

// seSpawnerTTLYAML: entry main → sub1, and sub1 owns the plain `exec` tool (the
// ActionTool). task_default_ttl is the only axis moved between renders, and it
// lives in hotSignature (not the structural fingerprint), so a change takes the
// numeric-only commit branch — exactly the branch where the retired push model
// left the spawner holding a construction-frozen number.
func seSpawnerTTLYAML(t testing.TB, ttl string) string {
	t.Helper()
	return fmt.Sprintf(`entry: main
prompt_dir: resources/prompts
model: test-model
providers:
  openai:
    api_endpoint: "http://localhost:1"
agents:
  main:
    system_prompt:
      inline: "main"
    tools:
      - kind: agent
        agent: sub1
        description: "sub1"
  sub1:
    system_prompt:
      inline: "sub1"
    tools:
      - kind: tool
        id: exec
        description: "shell"
    task_default_ttl: %q
    memory:
      type: localfile
      path: %q
`, ttl, testStore(t, "hottest-se-spawner"))
}

// seActionToolOf returns the ActionTool on an owner's live tool face — the very
// instance a spawn goes through, not a freshly built one. Tools are wrapped by
// `OutputLimitTool` at agent construction (agent.go:433) while the composition
// root binds the TTL source to the INNER instance (build_agent.go), so the
// unwrap here is what proves the two are the same object rather than a copy.
func seActionToolOf(t *testing.T, a *agent.TagentAgent) *action.ActionTool {
	t.Helper()
	require.NotNil(t, a, "owner 须在常驻表")
	for _, tl := range a.Tools() {
		if olt, ok := tl.(*agent.OutputLimitTool); ok {
			tl = olt.Unwrap()
		}
		if at, ok := tl.(*action.ActionTool); ok {
			return at
		}
	}
	t.Fatal("sub1 的工具面里须有 ActionTool（kind: tool / id: exec）")
	return nil
}

// TestSpawnerTTLReachesRealSpawnSpec 钉住 spawn 规格的默认 TTL 现读已提交记录，不取构造期数值。
// - 断言取宿主结果（交给任务层的规格），且必须经由那个一直在服务的工具实例；
// - 只改默认 TTL 落在数值分支：结构代不变，记录轮转后同一实例的下一次 spawn 即取新值；
// - 记录不外溢：模型显式声明的 ttl 优先于记录默认。
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func TestSpawnerTTLReachesRealSpawnSpec(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(seSpawnerTTLYAML(t, "30m"))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	sub1 := residentCacheForTest(entry)["sub1"]
	at := seActionToolOf(t, sub1)
	genBefore := entry.OrgDiagnostics()["generation"].(int64)

	decl := task.Declarative{Kind: "command", Command: "sleep 1", Desc: "board row", TaskID: "sess-se"}
	require.Equal(t, 30*time.Minute, at.SpecFromDeclarative(nil, decl).TTL,
		"初始 spawn 规格须取配置的 task_default_ttl=30m（经源解析），不是构造地板 10m")

	// numeric-only 旋转：只改 task_default_ttl，结构指纹不变。
	write(seSpawnerTTLYAML(t, "45m"))
	entry.CheckOrgReload()

	require.Equal(t, genBefore, entry.OrgDiagnostics()["generation"].(int64),
		"task_default_ttl 变更须落在 numeric-only 分支（不加结构代）")
	require.Equal(t, 45*time.Minute, scHot(t, sub1).TaskDefaultTTL, "记录轴须已轮转到 45m")
	require.Equal(t, 45*time.Minute, at.SpecFromDeclarative(nil, decl).TTL,
		"6.4 spawner 轴锚：同一个 ActionTool 实例（无 push、无重装配）的下一次 spawn 规格必须现读记录新值")

	// 记录不外溢：模型显式声明的 ttl 仍优先。
	explicit := decl
	explicit.Params = map[string]string{"ttl": "7"}
	require.Equal(t, 7*time.Second, at.SpecFromDeclarative(nil, explicit).TTL,
		"显式 ttl 参数须优先于记录默认")
}

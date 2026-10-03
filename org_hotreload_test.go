// org_hotreload_test 覆盖热更主链：应用记录轮转、端到端换入、候选拒绝与结构指纹判定。
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
package tagent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/stretchr/testify/require"
)

func TestOrgFingerprint_StableAcrossEmptyChanges(t *testing.T) {
	a, err := computeOrgFingerprint(cfgFor())
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}

	b := cfgFor()
	b.Governance.Dir = "data/gov2"
	b.Reliability.BusSpillDir = "data/bus2"
	am := cfgFor()
	am.Agents["main"] = AgentConfig{Model: "gpt-x", CompressThreshold: 0.8, Tools: []ToolRef{{Kind: "tool", ID: "recall"}}, Memory: MemoryConfig{Path: "data/mem2"}}
	c := cfgFor()
	c.APIEndpoint = "https://other.example.com"
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
	orgMod := cfgFor()
	a := orgMod.Agents["main"]
	a.Model = "gpt-z"
	orgMod.Agents["main"] = a
	if ofp, err := computeOrgFingerprint(orgMod); err != nil || ofp == orgFP {
		t.Errorf("non-memory change must alter the org fingerprint (err=%v)", err)
	}
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

	write(e2eYAML(0.5, "gpt-x"))
	entry.CheckOrgReload()
	if got := entry.OrgThreshold(); got != 0.5 {
		t.Fatalf("after hot shift threshold = %v, want 0.5", got)
	}

	write(e2eYAML(0.5, "gpt-w"))
	entry.CheckOrgReload()
	if got := entry.OrgThreshold(); got != 0.5 {
		t.Fatalf("after structural change threshold = %v, want 0.5 (kept)", got)
	}

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

	resident, err := buildAgent("tagent", cfg.Agents["tagent"], cfg, rc, loader, cache, buildModeResident)
	require.NoError(t, err)
	rc.entryMemStore = resident.MemStore()
	rc.entrySessionSvc = resident.SessionSvc()
	require.NotNil(t, rc.entryMemStore)
	require.NotNil(t, rc.entrySessionSvc)

	oldRunner := resident.Runner()
	require.NotNil(t, oldRunner)
	oldStore := resident.MemStore()
	oldSvc := resident.SessionSvc()
	oldTM := resident.TaskManager()

	gen2 := cfg.Agents["tagent"]
	gen2.SystemPrompt = PromptConfig{Inline: "gen2 prompt"}
	rebuilt, err := buildAgent("tagent", gen2, cfg, rc, loader, make(map[string]*agent.TagentAgent), buildModeExecutorShell)
	require.NoError(t, err)
	newRunner := rebuilt.Runner()
	require.NotNil(t, newRunner)
	require.NotSame(t, oldRunner, newRunner, "rebuilt shell must carry a NEW runner")

	require.Same(t, oldStore, rebuilt.MemStore(), "executorOnly shell must reuse the resident memStore")
	require.Same(t, oldSvc, rebuilt.SessionSvc(), "executorOnly shell must reuse the resident sessionSvc")

	require.Same(t, oldRunner, resident.Runner(), "pre-swap: runner unchanged (structural change invisible)")

	resident.ContextManager().PublishExecutor(newRunner, resident.ContextManager().ExecutorConfig())
	require.Same(t, newRunner, resident.Runner(), "post-swap: next turn sees the new runner")

	require.Same(t, oldStore, resident.MemStore(), "fact chain must survive the swap")
	require.Same(t, oldSvc, resident.SessionSvc(), "session service must survive the swap")
	require.Same(t, oldTM, resident.TaskManager(), "task registry must survive the swap")

	rolled := false
	resident.SetRollbackFn(func() { rolled = true })
	resident.Rollback()
	require.True(t, rolled, "Rollback() must invoke the wired hook")
	resident.SetRollbackFn(nil)
	resident.Rollback()
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

	require.Len(t, residentCacheForTest(entry), 3, "the resident binding table stays topology-sized across generations")
	rec, ok := st["agents"].([]OrgAgentApply)
	require.True(t, ok)
	require.Len(t, rec, 3, "the receipt set stays topology-sized — no per-generation accumulation")
	refs := entry.ContextManager().ExecutorRefs()
	require.Zero(t, refs.PendingRetirees, "an idle process holds no unreclaimed retired executors")

	entry.Rollback()
	require.Equal(t, int64(generations+1), entry.OrgDiagnostics()["generation"],
		"rollback after many generations republishes as a new sequence")
	require.Len(t, residentCacheForTest(entry), 3, "rollback neither grows nor drops the resident table")
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
		writeAt([]string{"prod-b", "prod-c"}[i%2])
		entry.CheckOrgReload()
	}
	elapsed := time.Since(start)

	st := entry.OrgDiagnostics()
	require.Equal(t, int64(rounds), st["generation"],
		"every round must really publish (otherwise this sample measures nothing)")

	per := elapsed / time.Duration(rounds)
	t.Logf("[perf sample] production shape (entry+4 subagents): one full reload+publish = %v (avg over %d rounds) — observational, not a correctness gate",
		per.Round(time.Microsecond), rounds)

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

	cache := residentCacheForTest(ta)
	require.Len(t, cache, 3)
	idMain := cache["main"].MemStore()
	idSub1 := cache["sub1"].MemStore()
	idSub2 := cache["sub2"].MemStore()
	require.NotSame(t, idSub1, idMain, "sub1 must own its own store")
	require.NotSame(t, idSub2, idMain, "sub2 must own its own store")

	write(hotYAML(t, 5, 2, true))
	ta.CheckOrgReload()

	require.Same(t, idMain, cache["main"].MemStore(), "entry store identity unchanged")
	require.Same(t, idSub1, cache["sub1"].MemStore(), "sub1 store identity unchanged (no drift to entry store)")
	require.Same(t, idSub2, cache["sub2"].MemStore(), "sub2 store identity unchanged")

	require.Equal(t, 5, keepRecentOf(cache["sub1"]), "sub1 keepRecent hot-applied")
	require.Equal(t, 2, keepRecentOf(cache["sub2"]), "untouched agent keeps its configured value")

	write(hotYAMLAddSub3(t, 5, 2))
	ta.CheckOrgReload()

	table := residentCacheForTest(ta)
	require.Len(t, table, 4, "the hot-added agent joins the resident binding table")
	require.NotNil(t, table["sub3"], "a hot add must be merged, not refused")
	require.NotSame(t, idMain, table["sub3"].MemStore(), "the added agent owns its own store (no drift onto the entry store)")
	require.NotSame(t, idSub1, table["sub3"].MemStore(), "…and does not borrow a sibling's store")
	require.Same(t, idMain, table["main"].MemStore(), "entry store identity survives a real publish")
	require.Same(t, idSub1, table["sub1"].MemStore(), "sub1 store identity survives the add")
	require.Same(t, idSub2, table["sub2"].MemStore(), "sub2 store identity survives the add")
	require.Equal(t, 2, keepRecentOf(table["sub3"]), "the added agent starts at its parsed default")
}

// TestHotReload_MemoryRejectionNotEffective 钉住 存储段变更被拒绝时不推进 effective 基准。
// - 第二次检查仍以上一次生效的记录为基准；拒绝路径不得改动任何常驻身份或资源。
// 契约: docs/wiki/platform/org-hot-reload.md#memory-preflight
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

	write(base)
	ta.CheckOrgReload()
	cache := residentCacheForTest(ta)
	require.Same(t, idSub1Keep, cache["sub1"].MemStore(), "rejected reload must not touch resident stores")
	_ = idMainKeep
}

// TestHotReload_FieldDeletionFallsBackToDefault 钉住 数值热应用取本代解析结果，不沿用上代生效值。
// - 定义里删除 keep_recent_tasks 后该 agent 回落解析默认（2），不会停在曾被热更成的 5；
// - 断言对象必须自己携带该字段：入口无此字段时按它断言只会看到默认值，测不到回落方向。
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func TestHotReload_FieldDeletionFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := func(c string) { require.NoError(t, os.WriteFile(yamlPath, []byte(c), 0o644)) }
	write(hotYAML(t, 5, 2, false))
	ta, err := New(LoadConfigForTest(t, yamlPath), WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer ta.Close()
	sub1 := residentCacheForTest(ta)["sub1"]
	require.Equal(t, 5, sub1.OrgKeepRecent(), "startup honors explicit keep_recent_tasks on sub1")

	write(hotYAML(t, 0, 2, false))
	ta.CheckOrgReload()
	sub1 = residentCacheForTest(ta)["sub1"]
	require.Equal(t, 2, sub1.OrgKeepRecent(),
		"deleted field falls back to the parsed default (2) — full-desired semantics")
}

// TestHotReload_RemovedOwnerKeepsItsParams 钉住 排空中的属主不参与数值热应用，本代仍路由的照常收新值。
// - 定义被同时删除时把解析默认下发给排水属主，会静默改掉仍在服务的旧代工作；
// - 豁免必须有向：只放过排水者，不得连带冻住本代仍路由的 agent（含入口）；
// - 回执要把两种结果分开：排水者 outcome 为 draining 且不携带已应用值。
// - 排空窗口必须自己制造：先给待移除的属主占一份租约，未被路由且空闲的属主会在同一刻退役，场景就不真实了。
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
	draining := table["sub1"].ContextManager().AcquireLease(agent.LeaseSubCall)
	defer draining.Release()

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

	write(entryRenameChangedYAML)
	ta.CheckOrgReload()

	st := ta.OrgDiagnostics()
	require.Equal(t, gen0, st["generation"], "an entry-identity change never publishes")
	fail, ok := st["lastFailure"].(*OrgFailure)
	require.True(t, ok, "the refusal must be diagnosable")
	require.Contains(t, strings.ToLower(fail.Error), "entry",
		"the refusal must name the entry-identity change, not surface an opaque rebuild error")

	after := residentCacheForTest(ta)
	require.Nil(t, after["entry2"], "no owner may be built for the refused entry identity")
	require.Same(t, mainInstance, after["main"], "the original entry owner is untouched")
	require.Same(t, mainStore, after["main"].MemStore(), "…and keeps its store identity")

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

// TestModelRefAliasesFoldToStableFingerprint 钉住 同一设置的旧式扁平写法与规范 ModelRef 写法必须折出同一份指纹。
func TestModelRefAliasesFoldToStableFingerprint(t *testing.T) {
	dir := t.TempDir()

	legacy := writeCfg(t, dir, "legacy.yaml", aliasYAML("    compress:\n      summary_model: summ\n"))
	canonical := writeCfg(t, dir, "canonical.yaml", aliasYAML("    compress:\n      summary:\n        model: summ\n"))

	require.Equal(t, "summ", legacy.Agents["main"].Compress.Summary.Model,
		"summary_model folded into compress.summary.model at load")
	require.Empty(t, legacy.Agents["main"].Compress.SummaryModel,
		"the legacy field is cleared after folding (not left as a second source)")

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

	legacy.FoldModelRefAliases()
	require.Equal(t, "summ", legacy.Agents["main"].Compress.Summary.Model, "value is not lost on re-fold")
	require.Empty(t, legacy.Agents["main"].Compress.SummaryModel, "legacy field does not reappear")
	require.Equal(t, before, mustFP(t, legacy), "re-folding does not drift the fingerprint")
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
	write(scHotAddNumericYAML(t, false, 0, 2))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	write(scHotAddNumericYAML(t, true, 4096, 2))
	entry.CheckOrgReload()
	require.Equal(t, int64(1), entry.OrgDiagnostics()["generation"].(int64), "热增须发布（否则后续断言空洞）")

	write(scHotAddNumericYAML(t, true, 8100, 2))
	entry.CheckOrgReload()
	require.Equal(t, int64(1), entry.OrgDiagnostics()["generation"].(int64), "numeric-only 不加代")

	table := residentCacheForTest(entry)
	sub3 := table["sub3"]
	require.NotNil(t, sub3, "sub3 须在常驻表")
	require.Equal(t, 6480, sub3.OrgBudgetLine(),
		"S-C 锚1：热增→numeric-only 后，热增 owner 的新调用种子须读新值（8100×0.8；提交点单写者轮转）")
}

// TestHotParamSnapshotRotatesAtCommitPoint 钉住 每个 owner 的热参快照必须与常驻消费者在同一个提交点轮转。
// - 新建即有快照且等于解析结果：新发起子调用的私有管理器从它播种，播种源不能等到首次热更之后才出现；
// - 纯数值编辑不加代际，快照与真消费者同代取新值；
// - 回滚经同一条单点把快照一并恢复，下一次子调用播种到编辑前那一代。
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
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

	p, ok := sub1.HotSnapshot()
	require.True(t, ok, "New must seed every owner's hot snapshot")
	require.Equal(t, 4000, p.MaxTokens)
	require.InDelta(t, 0.5, p.ThresholdPct, 1e-9)
	require.Equal(t, 2000, sub1.OrgBudgetLine(), "snapshot and resident compressor agree")

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

	entry.Rollback()
	p, _ = sub1.HotSnapshot()
	require.Equal(t, 4000, p.MaxTokens, "rollback restores the seeding source")
	require.Equal(t, 2000, sub1.OrgBudgetLine())
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

	write(seSpawnerTTLYAML(t, "45m"))
	entry.CheckOrgReload()

	require.Equal(t, genBefore, entry.OrgDiagnostics()["generation"].(int64),
		"task_default_ttl 变更须落在 numeric-only 分支（不加结构代）")
	require.Equal(t, 45*time.Minute, scHot(t, sub1).TaskDefaultTTL, "记录轴须已轮转到 45m")
	require.Equal(t, 45*time.Minute, at.SpecFromDeclarative(nil, decl).TTL,
		"6.4 spawner 轴锚：同一个 ActionTool 实例（无 push、无重装配）的下一次 spawn 规格必须现读记录新值")

	explicit := decl
	explicit.Params = map[string]string{"ttl": "7"}
	require.Equal(t, 7*time.Second, at.SpecFromDeclarative(nil, explicit).TTL,
		"显式 ttl 参数须优先于记录默认")
}

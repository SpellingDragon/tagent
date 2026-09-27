package tagent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/require"
)

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

// TestOrgGenerationsStructuresStayBounded 是「无界历史数组禁止」的行为证明：连续
// 换代只推进序号，所有簿记结构的规模恒等于拓扑大小，空闲态不留未回收执行器，且
// ring 2 回滚在任意多代之后仍然可用。
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

// TestReloadCostAtProductionShape_Sample reports the cost of a full reload+publish
// at the production shape (entry + 4 subagents) as an **observation only**. It keeps
// the anti-idle precondition (every round must really publish) so the sample means
// something, but asserts NO wall-clock bound: the short-lock property is structural
// and pinned by org_d3_scheduling_test.go; perf/boundedness re-verification is §5.3.
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

// TestD53_PerPublishObjectLifespan is §5.3's "证明实现确实更简单" in the form the
// task demands: for ONE fixed topology, record what each of cold / reload /
// rollback actually constructs, by **object identity** — not by an org-wide count
// (a live TaskManager count stays flat whether you reuse it or discard a copy every
// round, which is why J1 forbids using such a number as the evidence).
//
// Expected result of this change: a publish constructs exactly one new execution
// face (runner) per reachable owner and NOTHING else — no second TagentAgent, no
// second TaskManager, no re-bound store, no new session service, no second
// maintenance producer. Rollback must cost the same as a forward publish, because
// it is the same code path (§2.4).
func TestD53_PerPublishObjectLifespan(t *testing.T) {
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
	require.Equal(t, int64(1), di64(t, entry.OrgDiagnostics(), "generation"), "precondition: reload #1 published")

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

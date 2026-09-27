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
	"trpc.group/trpc-go/trpc-agent-go/model"
)

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

// TestHotReload_MixedChange_IdentityAndParams（4.5/4.6/4.7）：
// entry+sub1+sub2 拓扑上同时变更工具（结构）与子 agent 预算（数值）：
// ① 各 agent 常驻 store 身份不漂移（指针不变，sub1/sub2 不被换成 entry 的）；
// ② 数值参数作用于新代真实对象（keepRecent 热生效）；
// ③ 字段删除回落解析默认（4.7 全量 desired）；
// ④ 新增 agent 拒绝热更（4.5 fail-closed）。
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

// TestHotReload_FieldDeletionFallsBackToDefault（4.7）：删除 keep_recent_tasks
// 字段 → 回落解析默认（2），而非保留上次热更的 5。
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

// TestHotReload_RemovedOwnerKeepsItsParams 钉 §4.3 移除语义的一条边界：被移除的
// agent 保留常驻 owner 供旧代工作收敛，因此它**不得**被数值热更循环重新参数化——
// 定义同时删除时它拿到零值 AgentConfig，会静默回落全默认（7→2），把仍在服务的旧代
// 工作改掉，与 D7「旧 owner 保留到收敛」相悖。守卫是有向的：仍被路由的 agent 照常
// 收到新值。回执（D9 逐 agent 应用结果）把这条边界变成可诊断形状。
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

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

// §5.2 交叉场景（配置形状）：删除最后一个工具、参数删除与结构变更同候选、以及
// 候选后半段失败时已并入新增的回退。执行器层的并发/回收场景见
// agent/org_cross_scenario_test.go。

// TestCrossConfig_RemovingTheLastToolClearsTheDeclaration 删除最后一个工具：被清掉的
// 委派绑定不得存活到下一代的模型请求里（这曾是旧 `RebuildExecutor` 零值合并的泄漏形态）。
func TestCrossConfig_RemovingTheLastToolClearsTheDeclaration(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(ownerYAML(t, []string{"sub1"}, sub2MemDefault(t)))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	require.Equal(t, []string{"sub1"}, entryToolNames(entry), "baseline: exactly one delegation is offered")
	// §4.3 migrated the tail assertion below: an unrouted owner is kept only while it
	// is still needed, so the reference that makes it needed is taken here.
	keepDraining := residentCacheForTest(entry)["sub1"].ContextManager().AcquireLease(agent.LeaseSubCall)
	defer keepDraining.Release()
	genBefore := entry.OrgDiagnostics()["generation"]

	// 移除唯一工具（entry 变成零工具组织）。
	write(ownerYAML(t, nil, sub2MemDefault(t)))
	entry.CheckOrgReload()

	require.Empty(t, entryToolNames(entry),
		"removing the LAST tool must clear the declaration — a stale binding is a routing leak")
	require.Equal(t, int64(1), entry.OrgDiagnostics()["generation"].(int64)-genBefore.(int64),
		"the topology delta still publishes exactly one new generation")

	// 唯一被路由的 agent 是 entry 本身；两位子 agent 应报 draining（不碰其参数）。
	rec := entry.OrgDiagnostics()["agents"].([]OrgAgentApply)
	outcomes := map[string]string{}
	for _, r := range rec {
		outcomes[r.Name] = r.Outcome
	}
	require.Equal(t, "applied", outcomes["main"])
	require.Equal(t, "draining", outcomes["sub1"], "the removed owner is reported, not silently re-parameterized")

	// 常驻 owner 保留（旧代收尾用），但已不在可路由集合。
	require.NotNil(t, residentCacheForTest(entry)["sub1"], "the removed owner stays resident")
}

// TestCrossConfig_DeletedNumericFieldFallsBackWithinStructuralCandidate 把「参数删除
// 回落解析默认」放在**同一个结构变更候选**里验：重建与回落必须一起生效——只验数值分
// 支会漏掉「新壳按删除后的配置重建」这一半。
func TestCrossConfig_DeletedNumericFieldFallsBackWithinStructuralCandidate(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(hotYAML(t, 7, 7, false))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	require.Equal(t, 7, keepRecentOf(residentCacheForTest(entry)["sub1"]), "baseline configured value")

	// 同一候选：sub1 的 keep_recent_tasks **删除** + entry 多一个工具（结构变更）。
	write(hotYAML(t, 0, 7, true))
	entry.CheckOrgReload()

	table := residentCacheForTest(entry)
	require.Equal(t, 2, keepRecentOf(table["sub1"]),
		"a deleted numeric field must fall back to the parsed default inside a structural candidate")
	require.Equal(t, 7, keepRecentOf(table["sub2"]),
		"the sibling whose field is untouched keeps its value (full-desired applies per agent, not blanket)")
}

// TestCrossConfig_RefusedLaterAddRollsBackEarlierAdd 是「候选后半段失败」：同一轮里
// 先成功并入一个新增 agent，随后另一个新增失败 → 已并入的身份必须解绑，且旧代原样
// 服务；下一次合法新增仍要能用（不留半截 owner）。
func TestCrossConfig_RefusedLaterAddRollsBackEarlierAdd(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	// 启动代：entry → sub1。
	write(ownerYAML(t, []string{"sub1"}, sub2MemDefault(t)))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()
	genAtStart := entry.OrgDiagnostics()["generation"]

	// 候选：新增 okagent（可构造）+ badagent（构造必失败：system_prompt 指向不存在的文件）。
	// 名字排序决定构建顺序：badagent 先失败时 okagent 尚未并入，测不到回退；因此用
	// a-ok / z-bad 保证「先成功后失败」。
	write(addTwoYAML(t, []string{"sub1"}, "a2okagent", "zzbadagent"))
	entry.CheckOrgReload()

	st := entry.OrgDiagnostics()
	require.Equal(t, genAtStart, st["generation"],
		"a candidate whose later half failed must never publish")
	fail, ok := st["lastFailure"].(*OrgFailure)
	require.True(t, ok, "the failure must be diagnosable")
	require.Contains(t, fail.Error, "zzbadagent", "the failure must name the agent that could not be built")

	table := residentCacheForTest(entry)
	require.NotContains(t, table, "a2okagent",
		"the earlier successful add must be UNPUBLISHED when the candidate is refused (reverse-order rollback)")
	require.NotContains(t, entryToolNames(entry), "a2okagent",
		"a rolled-back identity must not be routable")
	require.NotContains(t, table, "zzbadagent", "the failed add is obviously not resident")

	// 旧代原样服务：sub1 仍是唯一的可路由目标。
	require.Equal(t, []string{"sub1"}, entryToolNames(entry), "the retained generation keeps serving unchanged")

	// 回退必须干净：随后单独新增 a2okagent 要能正常发布（若残留半截 owner 会撞车）。
	write(addOneYAML(t, []string{"sub1"}, "a2okagent"))
	entry.CheckOrgReload()
	st = entry.OrgDiagnostics()
	require.Equal(t, int64(1), st["generation"].(int64)-genAtStart.(int64),
		"a later legitimate add must publish cleanly after the rollback")
	require.NotNil(t, residentCacheForTest(entry)["a2okagent"], "the retried add becomes resident")
	require.Contains(t, entryToolNames(entry), "a2okagent", "and is routable")
}

// addTwoYAML renders entry delegating to `base` plus two new agents. The second one
// ("zzbadagent") has an un-creatable localfile store path, so its RESIDENT build fails
// mid-candidate — after the first add has been built and merged.
func addTwoYAML(t testing.TB, base []string, okName, badName string) string {
	t.Helper()
	y := addOneYAML(t, base, okName)
	return strings.Replace(y,
		"description: \""+okName+"\"",
		"description: \""+okName+"\"\n      - kind: agent\n        agent: "+badName+"\n        description: \""+badName+"\"", 1) +
		badAgentDef(badName)
}

// addOneYAML renders the same org with just one added agent (memory path per agent:
// two agents sharing an empty path would collide on one store owner and mask the
// failure this scenario is about).
func addOneYAML(t testing.TB, base []string, name string) string {
	t.Helper()
	var toolLines, defs strings.Builder
	for _, n := range append(append([]string{}, base...), name) {
		toolLines.WriteString("      - kind: agent\n        agent: " + n + "\n        description: \"" + n + "\"\n")
	}
	defs.WriteString(orgHead())
	defs.WriteString(toolLines.String())
	defs.WriteString(subDef(t, base[0]))
	defs.WriteString(agentDef(t, name, "inline: \"ok agent\""))
	return defs.String()
}

func orgHead() string {
	return "entry: main\nprompt_dir: resources/prompts\nmodel: test-model\nproviders:\n  openai:\n    api_endpoint: \"http://localhost:1\"\nagents:\n  main:\n    system_prompt:\n      inline: \"main\"\n    tools:\n"
}

// subDef renders the startup sub-agent exactly as ownerYAML defines it, so its memory
// section stays byte-identical across candidates (otherwise the memory rule refuses
// the candidate for the wrong reason).
func subDef(t testing.TB, name string) string {
	t.Helper()
	return agentDef(t, name, "inline: \""+name+"\"")
}

func agentDef(t testing.TB, name, prompt string) string {
	t.Helper()
	return "  " + name + ":\n    system_prompt:\n      " + prompt +
		fmt.Sprintf("\n    memory:\n      type: memory\n      path: %q\n", testStore(t, "own-"+name))
}

// badAgentDef is a schema-valid agent whose store cannot be created: config parse and
// Validate pass, so the failure lands in buildAgent ("agent %q: create memory store")
// — late enough that an earlier add in the same candidate is already merged.
//
// 曾试过用“提示词文件缺失”作为构造失败点，但 loader 是 load-if-present（缺失仅
// INFO 跳过），不会失败；一个不存在的存储路径才是确定性的构造期失败。
func badAgentDef(name string) string {
	return "  " + name + ":\n    system_prompt:\n      inline: \"bad\"\n    memory:\n      type: localfile\n      path: \"/dev/null/" + name + "-store\"\n"
}

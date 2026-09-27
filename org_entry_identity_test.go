package tagent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

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

// TestOrgHotReload_EntryIdentityChangeRefusedBeforeBuild 钉住：改名并删除旧定义
// 时，发布被明确拒绝（错误点名入口），序号不前进，新入口名下零候选资源被构建，
// 旧入口继续按原声明路由。
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

// TestOrgHotReload_EntryRenameWithOldDefKeptAlsoRefused：即使旧定义仍在
// agents: 里，入口身份变化仍在资源构建前拒绝（规则不因旧定义去留而改变）。
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

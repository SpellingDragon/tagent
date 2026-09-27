package tagent

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// R01／§2.3（design D3「事务式候选」）契约测。
//
// 缺陷：热增删里 `added`/`addedNames` 只在 `buildAgent` **成功返回后**由 seed 差集
// 填充，且 `rc.resident.Add` 在循环内每轮就发布。于是：
//   1. 父 agent 在 DFS 里递归建好依赖 Z（Z 已 registerStoreOwner + 持独有 store），
//      随后父自身步骤失败时 `return` 早于 seed 差集 → Z 不进回退清单 → 幽灵 owner；
//      失败父自身的 owner 登记也不被撤销（它不进 cache，外层无从枚举）。
//   2. 第一个 top 的 Add 已换入在线面，若后续 top 失败才 Unpublish → 期间并发读
//      可见未提交候选的新增者（半提交）。
//
// 本合同：一次被拒候选结束后，owner 归属必须与候选起点**逐名相等**（无净泄漏），
// 序号不前进，在线面不变。用「父成功依赖子 + 父自身在递归之后才失败」这一确定
// 时序触发：aaa_parent 引用 zzz_dep（合法、先建），随后 aaa_parent 因缺失的冥想
// 提示词文件在其依赖建成之后才失败。

const candTxnStartupYAML = `entry: main
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
        agent: keep
        description: "keep"
  keep:
    system_prompt:
      inline: "keep"
`

// main delegates to keep AND aaa_parent; aaa_parent delegates to the valid
// zzz_dep (built first via recursion) and THEN fails on a missing meditation
// prompt file — after its dependency is already resident-registered.
const candTxnRefusedYAML = `entry: main
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
        agent: keep
        description: "keep"
      - kind: agent
        agent: aaa_parent
        description: "p"
  keep:
    system_prompt:
      inline: "keep"
  aaa_parent:
    system_prompt:
      inline: "aaa_parent"
    tools:
      - kind: agent
        agent: zzz_dep
        description: "z"
    meditation:
      enabled: true
      prompt_file: "does-not-exist-r01-probe.md"
  zzz_dep:
    system_prompt:
      inline: "zzz_dep"
`

// TestOrgHotAdd_RefusedCandidateLeaksNoOwner 钉 R01：被拒候选不得遗留任何 owner
// 登记（含失败父与已成功依赖），序号不前进，在线面不变。fail-before：修复前
// zzz_dep + aaa_parent 的 owner 登记在回退后仍存在。
func TestOrgHotAdd_RefusedCandidateLeaksNoOwner(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)

	write(candTxnStartupYAML)
	ta := buildOwnerAgent(t, yamlPath)

	owners0 := ta.StoreOwnerSnapshot()
	require.NotNil(t, owners0, "owner probe must be wired")
	require.Contains(t, owners0, "main")
	require.Contains(t, owners0, "keep")
	require.NotContains(t, owners0, "zzz_dep", "precondition: dep not resident at startup")
	require.NotContains(t, owners0, "aaa_parent")
	gen0 := ta.OrgDiagnostics()["generation"]

	// Trigger the refused candidate (aaa_parent fails after zzz_dep is built).
	write(candTxnRefusedYAML)
	ta.CheckOrgReload()

	st := ta.OrgDiagnostics()
	require.Equal(t, gen0, st["generation"], "a refused candidate never publishes")
	require.NotNil(t, st["lastFailure"], "and the refusal is diagnosable")

	after := ta.StoreOwnerSnapshot()
	// Nothing the candidate registered may survive it — the recursively-built
	// dependency Z and the late-failed parent must both be revoked, leaving owner
	// attribution exactly as at the candidate start.
	require.Equal(t, owners0, after,
		"a refused candidate must revoke EVERY owner it registered (recursive deps + failed parent): no orphan, no leak")
	require.NotContains(t, after, "zzz_dep", "the recursively-built dependency's owner must be revoked")
	require.NotContains(t, after, "aaa_parent", "the late-failed parent's owner must be revoked")

	// The live topology is untouched (no half commit).
	require.NotContains(t, residentCacheForTest(ta), "zzz_dep")
	require.NotContains(t, residentCacheForTest(ta), "aaa_parent")
	require.Equal(t, []string{"keep"}, entryToolNames(ta), "the still-effective generation keeps routing")
}

// TestOrgHotAdd_LegalSharedDependencyBuildsOnce 是正向回归：跨多个新增 top 的
// 公共依赖只建一次（共享候选缓存），全部成功随单次提交发布，无第二 writer。
func TestOrgHotAdd_LegalSharedDependencyBuildsOnce(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)

	write(candTxnStartupYAML)
	ta := buildOwnerAgent(t, yamlPath)
	gen0 := ta.OrgDiagnostics()["generation"]

	// Two new parents sharing one dep, all valid → must hot-add all three once.
	legalYAML := `entry: main
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
        agent: p1
        description: "1"
      - kind: agent
        agent: p2
        description: "2"
  p1:
    system_prompt:
      inline: "p1"
    tools:
      - kind: agent
        agent: shared_dep
        description: "s"
  p2:
    system_prompt:
      inline: "p2"
    tools:
      - kind: agent
        agent: shared_dep
        description: "s"
  shared_dep:
    system_prompt:
      inline: "shared_dep"
`
	write(legalYAML)
	ta.CheckOrgReload()

	st := ta.OrgDiagnostics()
	require.Greater(t, st["generation"], gen0, "the legal add publishes")
	require.Nil(t, st["lastFailure"], "no refusal for a legitimate add")
	table := residentCacheForTest(ta)
	require.NotNil(t, table["p1"])
	require.NotNil(t, table["p2"])
	require.NotNil(t, table["shared_dep"], "the shared dep is resident exactly once")
	owners := ta.StoreOwnerSnapshot()
	require.Contains(t, owners, "shared_dep")
	require.Contains(t, owners, "p1")
	require.Contains(t, owners, "p2")
	// The dep behind both parents is one and the same instance — a second writer
	// would have built a distinct agent for shared_dep under the second parent.
	require.Equal(t, []string{"p1", "p2"}, entryToolNames(ta))
}

package tagent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/require"
)

// §4.3（D7）子树热增删的常驻所有权契约。委派行为面（新代的声明与实际调用目标随
// 发布改变）由 TestOrgDelegation_TargetFollowsPublishedGenerationNotMutableGlobals
// 覆盖；本文件只管**所有权**：谁持有 store、移除后谁保留、同名重入复用谁、被拒
// 候选的新增是否回退。
//
// 断言用实例/store 身份（require.Same / NotSame），因为「owner 有没有被复制或替换」
// 就是这些指针本身；行为侧的真实路由证据已由委派测给出。

// ownerYAML renders entry "main" delegating to `targets`（sub1/sub2 始终被定义，
// 所以移除只改变可达性，不改变配置里存在什么）。sub2 的 memory 段作为参数，便于
// 渲染“同名重入且存储不变”与“重入但存储变了”两种候选。
func ownerYAML(t testing.TB, targets []string, sub2Mem string) string {
	t.Helper()
	var toolLines string
	for _, t := range targets {
		toolLines += fmt.Sprintf("      - kind: agent\n        agent: %s\n        description: %q\n", t, t)
	}
	head := "entry: main\nprompt_dir: resources/prompts\nmodel: test-model\nproviders:\n  openai:\n    api_endpoint: \"http://localhost:1\"\nagents:\n  main:\n    system_prompt:\n      inline: \"main\"\n    tools:\n"
	defs := fmt.Sprintf(`  sub1:
    system_prompt:
      inline: "sub1"
    memory:
      type: memory
      path: %q
  sub2:
    system_prompt:
      inline: "sub2"
    memory:
`, testStore(t, "own-sub1"))
	return head + toolLines + defs + sub2Mem + "\n"
}

// 每个 agent 一个独立 store 实例，才有可观察的身份。路径经 testStore 挪出工作树并
// 按用例隔离——早先此处注释声称“type: memory 就不落盘”，是错的：resources.acquire 在
// 按 type 分派之前无条件 MkdirAll + 取目录写锁，相对路径会在仓库根造出目录。
func sub2MemDefault(t testing.TB) string {
	t.Helper()
	return fmt.Sprintf("      type: memory\n      path: %q\n", testStore(t, "own-sub2"))
}

func sub2MemMoved(t testing.TB) string {
	t.Helper()
	return fmt.Sprintf("      type: memory\n      path: %q\n", testStore(t, "own-sub2-moved"))
}

// sub2MemInMemory carries no path, so it needs no per-case store root.
const sub2MemInMemory = "      type: memory\n"

// ownerWriter bumps mtime deterministically: FS granularity can otherwise
// swallow a rapid rewrite and silently skip the reload.
func ownerWriter(t *testing.T, yamlPath string) func(string) {
	t.Helper()
	tick := time.Now()
	return func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}
}

func buildOwnerAgent(t *testing.T, yamlPath string) *agent.TagentAgent {
	t.Helper()
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	ta, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ta.Close() })
	return ta
}

func entryToolNames(ta *agent.TagentAgent) []string {
	face := ta.ContextManager().ExecutorConfig()
	var out []string
	for _, tl := range face.Tools {
		if d := tl.Declaration(); d != nil {
			out = append(out, d.Name)
		}
	}
	return out
}

// TestOrgHotRemove_KeepsOwnerButStopsRouting：移除只摘除新代可路由集合与工具声明，
// 原 owner 保留（绝不提前退役）——这是「旧代执行、后台任务、已接受输入仍可访问其
// 存储」的前提，也是同名重入能复用原 owner 的前提。
func TestOrgHotRemove_KeepsOwnerButStopsRouting(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)

	write(ownerYAML(t, []string{"sub1", "sub2"}, sub2MemDefault(t)))
	ta := buildOwnerAgent(t, yamlPath)

	table := residentCacheForTest(ta)
	require.NotNil(t, table["sub2"], "sub2 is resident at startup")
	sub2Instance, sub2Store := table["sub2"], table["sub2"].MemStore()
	require.NotSame(t, table["main"].MemStore(), sub2Store, "sub2 owns its own store")
	// §4.3 migrated this case: an unrouted owner is kept while it is STILL NEEDED and
	// retired once it is not. The claim under test ("unrouted, not retired early") is
	// about the drain window, so the window now has a real reference in it — taken on
	// the same accounting the reclaim gate reads.
	drainRef := sub2Instance.ContextManager().AcquireLease(agent.LeaseSubCall)
	defer drainRef.Release()

	// 移除 sub2 的可路由性（其定义仍在配置里——可达集合才是拓扑真源）。
	write(ownerYAML(t, []string{"sub1"}, sub2MemDefault(t)))
	ta.CheckOrgReload()

	after := residentCacheForTest(ta)
	require.Same(t, sub2Instance, after["sub2"],
		"a removed agent keeps its resident owner — it is unrouted, not retired")
	require.Same(t, sub2Store, after["sub2"].MemStore(), "and keeps the same store, not a replacement")
	require.NotContains(t, entryToolNames(ta), "sub2",
		"the published generation must not offer a removed agent as a tool")
	require.Contains(t, entryToolNames(ta), "sub1", "the surviving target still routes")
}

// TestOrgHotAdd_ReentryReusesOriginalOwnerUnlessStorageChanged：同名重入必须复用原
// 存储 owner（不产生第二 writer）；而重入时存储段变了就拒绝候选。
func TestOrgHotAdd_ReentryReusesOriginalOwnerUnlessStorageChanged(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)

	write(ownerYAML(t, []string{"sub1", "sub2"}, sub2MemDefault(t)))
	ta := buildOwnerAgent(t, yamlPath)
	orig := residentCacheForTest(ta)["sub2"]
	genAtStart := ta.OrgDiagnostics()["generation"]
	// §4.3 migrated both cases below: they assert "same-name re-entry reuses the
	// ORIGINAL owner" / "the refusal keeps the original owner in place", which is the
	// semantics WHILE the owner is still live. An idle removed owner is now retired
	// (TestRetire_*), so this test holds one reference to stay in its own subject.
	heldOut := orig.ContextManager().AcquireLease(agent.LeaseSubCall)
	defer heldOut.Release()

	// 1) remove, then re-add with the SAME storage section → the original owner is
	// reused: the same instance, the same store, and NOT a freshly built second one.
	write(ownerYAML(t, []string{"sub1"}, sub2MemDefault(t)))
	ta.CheckOrgReload()
	write(ownerYAML(t, []string{"sub1", "sub2"}, sub2MemDefault(t)))
	ta.CheckOrgReload()

	table := residentCacheForTest(ta)
	require.Same(t, orig, table["sub2"], "a same-name re-entry reuses the original resident owner")
	require.Same(t, orig.MemStore(), table["sub2"].MemStore(), "…and its original store (no second writer)")
	require.Contains(t, entryToolNames(ta), "sub2", "…and is routable again")
	require.Greater(t, ta.OrgDiagnostics()["generation"], genAtStart, "the re-add is a real publish")

	// 2) re-entry with a CHANGED storage section → refused: the candidate would put
	// a different store behind a name whose original owner still holds the old one.
	write(ownerYAML(t, []string{"sub1"}, sub2MemDefault(t)))
	ta.CheckOrgReload()
	before := ta.OrgDiagnostics()["generation"]
	write(ownerYAML(t, []string{"sub1", "sub2"}, sub2MemMoved(t)))
	ta.CheckOrgReload()

	st := ta.OrgDiagnostics()
	require.Equal(t, before, st["generation"], "a storage-changing re-entry never publishes")
	fail, ok := st["lastFailure"].(*OrgFailure)
	require.True(t, ok, "the refusal must be diagnosable")
	require.Contains(t, fail.Error, "memory section changed")
	require.Contains(t, fail.Error, "sub2")
	require.Same(t, orig, residentCacheForTest(ta)["sub2"],
		"the refusal keeps the ORIGINAL owner in place — the moved store was never adopted")
	require.NotContains(t, entryToolNames(ta), "sub2", "and the refused candidate routes nowhere")

	// 3) the refusal is sticky: re-checking the same un-effective storage change while
	// editing an unrelated field still refuses (no bypass through a second check).
	write(ownerYAML(t, []string{"sub1", "sub2"}, sub2MemMoved(t)))
	ta.CheckOrgReload()
	require.Equal(t, before, ta.OrgDiagnostics()["generation"], "the refusal does not lapse into acceptance")
}

// TestOrgHotAdd_NewAgentMayCarryItsOwnMemorySection：新增 agent 自带 memory 段不是
// “运行时存储迁移”，不得被 memory 先序检查误拒——否则热新增永远不可达。
func TestOrgHotAdd_NewAgentMayCarryItsOwnMemorySection(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)

	write(ownerYAML(t, []string{"sub1"}, sub2MemDefault(t)))
	ta := buildOwnerAgent(t, yamlPath)
	require.NotContains(t, residentCacheForTest(ta), "sub2")

	// sub2 comes back only as a NEW name relative to the reachable topology, with a
	// memory section no existing owner holds.
	write(ownerYAML(t, []string{"sub1", "sub2"}, sub2MemInMemory))
	ta.CheckOrgReload()

	table := residentCacheForTest(ta)
	require.NotNil(t, table["sub2"], "a new agent with its own memory section must hot-add")
	require.NotSame(t, table["main"].MemStore(), table["sub2"].MemStore(), "on its own store")
	require.Nil(t, ta.OrgDiagnostics()["lastFailure"], "no refusal for a legitimate add")
	require.Contains(t, entryToolNames(ta), "sub2")
}

// TestOrgHotAdd_UnroutedDefinitionChangeMustNotFreezeReload 钉审阅 H-1 的第三态：
// 一个「定义仍在 agents: 里、但已从 entry 工具链摘除」的 agent 改了 memory 路径——
// 它不进本代构造，没有第二 writer 可防，因此**不得**冻结整条热更路（修前会永久拒绝
// 此后每一次热更，且提示语把人往重启引）。同时它日后重入时仍须被拒（D7 粘性未削）。
func TestOrgHotAdd_UnroutedDefinitionChangeMustNotFreezeReload(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)

	write(ownerYAML(t, []string{"sub1", "sub2"}, sub2MemDefault(t)))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	sub2Store := residentCacheForTest(entry)["sub2"].MemStore()
	require.NotSame(t, entry.MemStore(), sub2Store, "precondition: sub2 owns its own store")
	gen0 := entry.OrgDiagnostics()["generation"].(int64)

	// ① 摘 sub2 路由 + 同时改它的路径：必须发布（不冻结）。
	write(ownerYAML(t, []string{"sub1"}, sub2MemMoved(t)))
	entry.CheckOrgReload()
	require.Equal(t, gen0+1, entry.OrgDiagnostics()["generation"].(int64),
		"a storage change on an UNROUTABLE-but-still-defined agent must not freeze orchestration hot-reload")
	require.Nil(t, entry.OrgDiagnostics()["lastFailure"], "and nothing was refused")

	// ② 一次与此无关的编排变更仍要生效（这才是 H-1 修前被永久冻住的形态）。
	write(ownerYAMLWithModel(t, "test-model-x", []string{"sub1"}, sub2MemMoved(t)))
	entry.CheckOrgReload()
	require.Equal(t, gen0+2, entry.OrgDiagnostics()["generation"].(int64),
		"later orchestration edits must still apply")

	// ③ sub2 重入且存储与 owner 基准不符 → 仍拒（粘性），且原 owner/store 不被换掉。
	write(ownerYAMLWithModel(t, "test-model-x", []string{"sub1", "sub2"}, sub2MemMoved(t)))
	entry.CheckOrgReload()
	st := entry.OrgDiagnostics()
	require.Equal(t, gen0+2, st["generation"].(int64),
		"a re-entering name whose storage differs from its owner's baseline must still be refused")
	fail, ok := st["lastFailure"].(*OrgFailure)
	require.True(t, ok, "and the refusal must be diagnosable")
	require.Contains(t, fail.Error, "sub2")
	// §4.3 migrated (and strengthened): by this point sub2 has been unrouted long
	// enough to be retired, so "the original owner is still installed" is no longer
	// the claim. What must survive is D7's real guarantee — the storage baseline is
	// data (residentMemFP), so the re-entry is STILL refused, and the refused
	// candidate must not have built any owner behind the name.
	require.Nil(t, residentCacheForTest(entry)["sub2"],
		"the original owner was retired while unrouted — no instance is held for a name nothing needs")
	require.NotContains(t, entryToolNames(entry), "sub2",
		"and the refused candidate neither adopted the moved store nor routed the name")
}

// TestRelaunch_TargetResolvesAgainstPublishedGeneration 把 §4.2 的**接线**钉在真实
// 热更上（agent 层的测只证明机制本身）：显式重投的解析源必须跟着已发布代走——
// 启动代认得 sub1；热移除其路由后不再认得（否则重投会静默复活已退役绑定，违反
// task-registry-rebuild 的「不复活、不改投」）；同名重入后又认得。
func TestRelaunch_TargetResolvesAgainstPublishedGeneration(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)

	write(ownerYAML(t, []string{"sub1", "sub2"}, sub2MemDefault(t)))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	defer func() { _ = entry.Close() }()

	cm := entry.ContextManager()
	require.NotNil(t, cm.SubagentWrapper("sub1"), "the startup generation routes to sub1")

	// 摘掉 sub1 的路由（定义保留——正是 §4.2 与 H-1 交界的形状）。
	write(ownerYAML(t, []string{"sub2"}, sub2MemDefault(t)))
	entry.CheckOrgReload()
	require.Nil(t, cm.SubagentWrapper("sub1"),
		"after a hot removal an explicit relaunch must NOT resolve sub1 — the retired binding may not be revived")
	require.NotNil(t, cm.SubagentWrapper("sub2"), "the retained target keeps resolving (the face is not emptied by the removal)")

	// 同名重入（存储未变）→ 必须重新可解析，且解析到的仍是**原 owner** 的绑定。
	write(ownerYAML(t, []string{"sub1", "sub2"}, sub2MemDefault(t)))
	entry.CheckOrgReload()
	require.NotNil(t, cm.SubagentWrapper("sub1"), "re-entry makes it routable again")
	require.Equal(t, int64(0), cm.ExecutorRefs().InFlightTurns, "sanity: a reload publishes a generation but pins no in-flight turn")
}

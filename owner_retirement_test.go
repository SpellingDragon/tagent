package tagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// retYAML renders entry "main" routing the given names. Every agent DEFINED in the
// file may or may not be routed — routability, not presence in the file, is the
// topology's truth source (see TestOrgHotRemove_KeepsOwnerButStopsRouting). Each
// routed agent gets its own store path so owner identity is per-agent observable.
func retYAML(t testing.TB, basename string, routed ...string) string {
	t.Helper()
	var tools strings.Builder
	for _, r := range routed {
		tools.WriteString(fmt.Sprintf("      - kind: agent\n        agent: %s\n        description: %q\n", r, r))
	}
	defs := strings.Builder{}
	for _, d := range retAllDefs {
		defs.WriteString(fmt.Sprintf("  %s:\n    system_prompt:\n      inline: \"PROMPT-%s\"\n    memory:\n      type: memory\n      path: %q\n", d, d, testStore(t, "retire-"+d)))
	}
	return "entry: main\nmodel: test-model\nproviders:\n  openai:\n    api_endpoint: \"http://localhost:1\"\nagents:\n  main:\n    system_prompt:\n      inline: \"MAIN\"\n    tools:\n" +
		tools.String() + defs.String()
}

// retAllDefs are the names any generation may route (all always defined).
var retAllDefs = []string{"s1", "s2", "s3"}

// twoAgentsOneStore routes `routed` with s1/s2 sharing ONE store path — the shape
// that makes "did retiring one sibling close state the other still uses" observable.
func twoAgentsOneStore(t testing.TB, shared string, routed ...string) string {
	t.Helper()
	var tools strings.Builder
	for _, r := range routed {
		tools.WriteString(fmt.Sprintf("      - kind: agent\n        agent: %s\n        description: %q\n", r, r))
	}
	defs := fmt.Sprintf("  s1:\n    system_prompt:\n      inline: \"PROMPT-s1\"\n    memory:\n      type: memory\n      path: %q\n"+
		"  s2:\n    system_prompt:\n      inline: \"PROMPT-s2\"\n    memory:\n      type: memory\n      path: %q\n"+
		"  s3:\n    system_prompt:\n      inline: \"PROMPT-s3\"\n    memory:\n      type: memory\n      path: %q\n",
		shared, shared, testStore(t, "retire-s3"))
	return "entry: main\nmodel: test-model\nproviders:\n  openai:\n    api_endpoint: \"http://localhost:1\"\nagents:\n  main:\n    system_prompt:\n      inline: \"MAIN\"\n    tools:\n" +
		tools.String() + defs
}

func residentNamesOf(entry *agent.TagentAgent) []string {
	var out []string
	for n := range residentCacheForTest(entry) {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func buildRetireOrg(t *testing.T, yamlPath string) *agent.TagentAgent {
	t.Helper()
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	return entry
}

// TestRetire_RemovedIdleOwnerIsRetired 钉住 移除即停止接受新路由，义务全部收敛后关闭独占组件、释放租约并撤销登记。
// - 无人依赖这个属主时，摘除它的那次发布必须同时把它移出常驻表、关闭并撤销其存储属主登记；
// - 仍被路由的那个实例保持原样，不因别人的移除而改变身份。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestRetire_RemovedIdleOwnerIsRetired(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retYAML(t, "a", "s1", "s3"))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	owners := residentCacheForTest(entry)
	s1, s3 := owners["s1"], owners["s3"]
	require.NotNil(t, s3, "precondition: s3 is resident at startup")
	require.Contains(t, entry.StoreOwnerSnapshot(), "s3")

	write(retYAML(t, "b", "s1"))
	entry.CheckOrgReload()

	require.NotContains(t, entryToolNames(entry), "s3", "the published generation must not route it")
	require.NotContains(t, residentCacheForTest(entry), "s3", "and a drained owner must leave the resident table")
	require.True(t, s3.CloseStarted(), "its exclusive components must be closed")
	require.True(t, s3.CleanerStopped(), "including its maintenance producer")
	require.NotContains(t, entry.StoreOwnerSnapshot(), "s3", "its store-owner registration must be revoked")
	require.Same(t, s1, residentCacheForTest(entry)["s1"], "the surviving owner is untouched")
}

// TestRetire_ObligationHoldsOwnerThenRetiresAtNextBoundary 钉住 安全的一半：仍有在途执行的属主，不被移除它的那次发布退役。
// - 义务消失之后才在下一个边界退役；
// - 提前关闭属于禁止情形，因此这份持有必须可观察，不能静默。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestRetire_ObligationHoldsOwnerThenRetiresAtNextBoundary(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retYAML(t, "a", "s1", "s2"))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	s2 := residentCacheForTest(entry)["s2"]
	lease := s2.ContextManager().AcquireLease(agent.LeaseSubCall)
	require.Equal(t, 1, s2.Obligations().Executions, "precondition: the obligation probe sees the live execution")

	write(retYAML(t, "b", "s1"))
	entry.CheckOrgReload()

	require.NotNil(t, residentCacheForTest(entry)["s2"],
		"an owner with a live obligation must stay resident — not retired early")
	require.False(t, s2.CloseStarted(), "and must NOT be closed")
	require.Contains(t, entry.StoreOwnerSnapshot(), "s2", "and keeps its store registration")

	lease.Release()
	write(retYAML(t, "b", "s1"))
	entry.CheckOrgReload()

	require.NotContains(t, residentCacheForTest(entry), "s2", "once the obligation converged it is retired")
	require.True(t, s2.CloseStarted())
	require.NotContains(t, entry.StoreOwnerSnapshot(), "s2")
}

// TestRetire_ReenteredNameReusesOriginalOwner 钉住 同名在其属主仍在排空时回来，必须由那一个实例服务。
// - 同一存储出现第二个属主，就是第二个 writer。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestRetire_ReenteredNameReusesOriginalOwner(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retYAML(t, "a", "s1", "s2"))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	s2 := residentCacheForTest(entry)["s2"]
	lease := s2.ContextManager().AcquireLease(agent.LeaseSubCall)
	write(retYAML(t, "b", "s1"))
	entry.CheckOrgReload()
	require.False(t, s2.CloseStarted(), "precondition: still draining, not closed")

	write(retYAML(t, "c", "s1", "s2"))
	entry.CheckOrgReload()

	require.Same(t, s2, residentCacheForTest(entry)["s2"],
		"a re-entered name reuses the original owner while it is still live")
	require.False(t, s2.CloseStarted(), "and is never closed under the reuse")
	require.Same(t, s2.MemStore(), residentCacheForTest(entry)["s2"].MemStore(), "same store, not a replacement")
	lease.Release()
	_ = context.Background()
}

// TestRetire_ReentryIntoClosingOwnerIsRefused 钉住 属主已开始关闭时，想要这个名字的候选必须在任何资源建立之前就被拒绝。
// - 复用它已不可能；放入新的则构成同一存储身份的第二个 writer。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestRetire_ReentryIntoClosingOwnerIsRefused(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retYAML(t, "a", "s1", "s2"))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	s2 := residentCacheForTest(entry)["s2"]
	lease := s2.ContextManager().AcquireLease(agent.LeaseSubCall)
	write(retYAML(t, "b", "s1"))
	entry.CheckOrgReload()
	s2.Close()
	t.Cleanup(func() { lease.Release() })

	write(retYAML(t, "c", "s1", "s2"))
	before := entry.OrgDiagnostics()["generation"]
	entry.CheckOrgReload()

	require.NotContains(t, entryToolNames(entry), "s2",
		"a publish that wants a closing name must be refused, not silently served by a new owner")
	require.Same(t, s2, residentCacheForTest(entry)["s2"],
		"the refused candidate must not have replaced the owner with a second writer")
	require.Equal(t, before, entry.OrgDiagnostics()["generation"], "a refusal never advances the published sequence")

	lease.Release()
}

// TestRetire_ReentryAfterFinalExitRebuildsFreshOwner 钉住 被移除的属主最终退出之后，重新加回这个名字必须按恢复协议建新属主，而不是继续拒绝。
// - 释放本身带着排空推进，因此无需额外业务回合即可到达该退出点；
// - 对前属主已彻底消失的名字持续拒绝，等于把拒绝门漏进准入路径。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestRetire_ReentryAfterFinalExitRebuildsFreshOwner(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retYAML(t, "a", "s1", "s2"))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	s2 := residentCacheForTest(entry)["s2"]
	lease := s2.ContextManager().AcquireLease(agent.LeaseSubCall)
	write(retYAML(t, "b", "s1"))
	entry.CheckOrgReload()
	require.NotNil(t, residentCacheForTest(entry)["s2"],
		"precondition: the in-flight reference keeps the unrouted owner resident")

	lease.Release()
	require.Eventually(t, func() bool {
		return residentCacheForTest(entry)["s2"] == nil
	}, 5*time.Second, 20*time.Millisecond, "final exit reached without any further traffic")

	write(retYAML(t, "c", "s1", "s2"))
	entry.CheckOrgReload()

	require.Contains(t, entryToolNames(entry), "s2",
		"a name whose owner finally exited is admitted again, not refused forever")
	rebuilt := residentCacheForTest(entry)["s2"]
	require.NotNil(t, rebuilt, "the re-added name must be served by a real owner")
	require.NotSame(t, s2, rebuilt, "a finally-exited owner is never resurrected — the exit was real")
}

// TestRetire_NameChurnStaysBounded 钉住 资源必须收敛到当前路由加真实待决义务所需的量，绝不按出现过的名字数收敛。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestRetire_NameChurnStaysBounded(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retYAML(t, "a", "s1"))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	for round := 0; round < 2; round++ {
		for _, n := range retAllDefs {
			write(retYAML(t, fmt.Sprintf("churn-%d-%s", round, n), "s1", n))
			entry.CheckOrgReload()
		}
	}
	require.Len(t, residentCacheForTest(entry), 3,
		"resident set must equal main + the currently routed names, not every name ever routed (got %v)",
		residentNamesOf(entry))
	require.Len(t, entry.StoreOwnerSnapshot(), 3, "and so must the store-owner registrations")
	debt, ok := entry.OrgDiagnostics()["liveDebt"].(OrgLiveDebt)
	require.True(t, ok, "the live debt group must always be present")
	require.Empty(t, debt.PendingRetirements,
		"nothing is left draining once every retired owner converged")
}

// TestRetire_SharedStoreSurvivesSiblingRetirement 钉住 两个属主共用一个存储时，退役其一不得在另一属主之下关掉共享状态。
// - 存储由租约记账决定何时消失，不由某个借用者的退出决定；
// - 幸存属主仍从同一存储对象服务：没被关掉、维护生产者仍在跑、仍被路由。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestRetire_SharedStoreSurvivesSiblingRetirement(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	shared := testStore(t, "retire-shared")
	write(twoAgentsOneStore(t, shared, "s1", "s2"))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	owners := residentCacheForTest(entry)
	s1, s2 := owners["s1"], owners["s2"]
	require.Same(t, s1.MemStore(), s2.MemStore(), "precondition: the two owners really share one store")

	write(twoAgentsOneStore(t, shared, "s1"))
	entry.CheckOrgReload()
	require.True(t, s2.CloseStarted(), "the unrouted sibling retired")
	require.NotContains(t, residentCacheForTest(entry), "s2")

	require.Same(t, s1.MemStore(), residentCacheForTest(entry)["s1"].MemStore())
	require.False(t, s1.CloseStarted(), "the surviving owner must not be closed by its sibling's retirement")
	require.False(t, s1.CleanerStopped(), "and its producers stay up")
	require.Contains(t, entryToolNames(entry), "s1", "and it is still routed")
}

// TestRetire_RollbackRetiresDroppedOwner 钉住 回滚也是一次发布，它丢掉的属主走同一条排水。
// - 回滚环里保存的配置是数据，从来不是留住一个在跑实例的理由；
// - 完全关闭的属主不被复活——名字获得一个全新属主，且不是那个已关实例。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestRetire_RollbackRetiresDroppedOwner(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retYAML(t, "a", "s1", "s2"))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	s2 := residentCacheForTest(entry)["s2"]
	write(retYAML(t, "b", "s1"))
	entry.CheckOrgReload()
	require.NotContains(t, residentCacheForTest(entry), "s2", "precondition: G2 already retired it")

	entry.Rollback()
	require.Contains(t, entryToolNames(entry), "s2", "the rollback really re-routed s2")
	fresh := residentCacheForTest(entry)["s2"]
	require.NotNil(t, fresh, "a fully-closed owner is not resurrected; the name gets a fresh owner")
	require.NotSame(t, s2, fresh, "and it is NOT the closed instance")
	require.False(t, fresh.CloseStarted(), "the fresh owner serves")
}

// TestOrgHotAdd_VisibleOnlyAtCommit 钉住 暂存的新增属主在唯一提交点之前对读者不可见，提交时一次性可见。
// - 观察点必须落在提交临界区内：属主已建成、合并未发生。只在提交完成后断言可见，对"构建在途不得出现半提交拓扑"这一半是空洞的；
// - 此刻常驻表取不到它、现效面不 offer 它、发布序号不前进；释放后三者同时成立且序号恰好推进一次。
// - 刻意的例外是存储属主登记：它在构建期取得，被拒候选的回退要能撤销本候选登记过的每一个属主，把登记也说成不可见就描述了另一种设计。
// 契约: docs/wiki/platform/org-hot-reload.md#staged-add-visibility
func TestOrgHotAdd_VisibleOnlyAtCommit(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retYAML(t, "a", "s1"))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	require.NotContains(t, residentCacheForTest(entry), "s2",
		"precondition: s2 is defined in the file but unrouted, so it is not resident")
	genBefore := entry.OrgDiagnostics()["generation"]

	entered := make(chan struct{})
	release := make(chan struct{})
	var enter sync.Once
	park := func() {
		enter.Do(func() { close(entered) })
		<-release
	}
	orgCommitBarrier.Store(&park)
	t.Cleanup(func() { orgCommitBarrier.Store(nil) })

	write(retYAML(t, "b", "s1", "s2"))
	done := make(chan struct{})
	go func() {
		defer close(done)
		entry.CheckOrgReload()
	}()

	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("the reload never reached the commit point — the witness below would be vacuous")
	}

	table := residentCacheForTest(entry)
	require.NotContains(t, table, "s2",
		"a staged add must not be resident while the candidate is still uncommitted")
	require.NotContains(t, entryToolNames(entry), "s2",
		"and must not be offered on the face that is still in force")
	require.Equal(t, genBefore, entry.OrgDiagnostics()["generation"],
		"the published sequence advances only at the commit")

	close(release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the released reload never completed")
	}

	require.NotNil(t, residentCacheForTest(entry)["s2"], "after the commit the owner is merged")
	require.Contains(t, entryToolNames(entry), "s2", "and the new generation offers it")
	require.Greater(t, entry.OrgDiagnostics()["generation"], genBefore, "with exactly one publish")
}

// retDiamondYAML renders the diamond: main → {s1, s2}, and BOTH of them route the
// shared leaf s3. `routeS1/routeS2` switch the entry's routes, which is how one
// generation can drop a branch while the other branch stays in force.
func retDiamondYAML(t testing.TB, routeS1, routeS2 bool) string {
	t.Helper()
	var mainTools string
	if routeS1 {
		mainTools += `      - kind: agent
        agent: s1
        description: "s1"
`
	}
	if routeS2 {
		mainTools += `      - kind: agent
        agent: s2
        description: "s2"
`
	}
	branch := func(name string) string {
		return fmt.Sprintf("  %s:\n    system_prompt:\n      inline: \"PROMPT-%s\"\n    memory:\n      type: memory\n      path: %q\n    tools:\n      - kind: agent\n        agent: s3\n        description: \"s3\"\n",
			name, name, testStore(t, "retire-diamond-"+name))
	}
	return "entry: main\nmodel: test-model\nproviders:\n  openai:\n    api_endpoint: \"http://localhost:1\"\nagents:\n  main:\n    system_prompt:\n      inline: \"MAIN\"\n    memory:\n      type: memory\n      path: " +
		fmt.Sprintf("%q\n    tools:\n%s", testStore(t, "retire-diamond-main"), mainTools) +
		branch("s1") + branch("s2") +
		fmt.Sprintf("  s3:\n    system_prompt:\n      inline: \"PROMPT-s3\"\n    memory:\n      type: memory\n      path: %q\n", testStore(t, "retire-diamond-s3"))
}

// TestRetire_DiamondSharedDependencyWaitsForAllBorrowers 钉住 菱形共享依赖在退役时等所有借用者，形状为 main→{s1,s2}→s3。
// - 无待决义务的分支 s1 收敛，共享叶子 s3 不因自己无活就退役——它仍在 s2 存活代的可调用闭包内、合法可委派；
// - 最后借用者排空后 s3 自行退出、不需额外业务回合，级联 s2 再 s3 在同一次排空内收敛。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestRetire_DiamondSharedDependencyWaitsForAllBorrowers(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retDiamondYAML(t, true, true))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	owners := residentCacheForTest(entry)
	s1, s2, s3 := owners["s1"], owners["s2"], owners["s3"]
	require.NotNil(t, s3, "precondition: the shared leaf is resident")
	require.NotNil(t, s1.ContextManager().SubagentWrapper("s3"), "both branches route s3…")
	require.NotNil(t, s2.ContextManager().SubagentWrapper("s3"), "…from their own faces")

	inFlight := s2.ContextManager().AcquireLease(agent.LeaseSubCall)

	write(retDiamondYAML(t, false, false))
	entry.CheckOrgReload()

	require.Eventually(t, func() bool {
		return residentCacheForTest(entry)["s1"] == nil
	}, 5*time.Second, 20*time.Millisecond,
		"没有引用也没有保有的分支（s1）应收敛")

	require.NotNil(t, residentCacheForTest(entry)["s2"], "在途执行所在的分支不得被提前关掉")
	require.NotNil(t, residentCacheForTest(entry)["s3"],
		"§4.3 菱形锚：共享叶子被别的存活代保有时不得退役——它自己没有义务不代表可关")
	require.False(t, s3.CloseStarted(), "and its close must not even have begun")

	inFlight.Release()
	require.Eventually(t, func() bool {
		o := residentCacheForTest(entry)
		return o["s2"] == nil && o["s3"] == nil
	}, 5*time.Second, 20*time.Millisecond,
		"最后借用者退出后，s2 与共享的 s3 须在同一个排空里依次收敛（不等待新 turn）")
}

// sealThePath produces a REAL sealed store rather than a mocked error: it drives the
// unconfirmed-reclaim rule, so a live single-writer flock genuinely sits on the path and
// any later opener of it collides with a possibly-half-live backend instead of succeeding.
// 契约: docs/wiki/platform/resource-ownership.md#poisoned-seal
func sealThePath(t *testing.T, rr *RuntimeResources, path string) {
	t.Helper()
	fp := fingerprintMemory(MemoryConfig{Type: "localfile", Path: path})
	_, _, _, err := rr.acquire("localfile", path, fp, func() (openedResource, error) {
		return openedResource{}, fmt.Errorf("%w: kv close hung", ErrReclaimUnconfirmed)
	})
	require.ErrorIs(t, err, ErrReclaimUnconfirmed, "precondition: the seal must come from the real reclaim rule")
}

// assertPathStillSealed checks the seal WITHOUT touching the writer lock: an
// flock probe in the same process would take/convert the lock and release it on
// close (macOS flock semantics — measured: a second `-count` iteration then found
// the path free), so the seal is verified through the registry's own rule instead:
// a re-acquire on a sealed path must be refused with ErrResourcePoisoned without
// ever running open(). A second writer therefore cannot exist, because the single
// writer slot was never handed out.
func assertPathStillSealed(t *testing.T, rr *RuntimeResources, path string) {
	t.Helper()
	fp := fingerprintMemory(MemoryConfig{Type: "localfile", Path: path})
	_, _, _, err := rr.acquire("localfile", path, fp, func() (openedResource, error) {
		t.Error("open must NOT run again on a sealed path")
		return openedResource{}, nil
	})
	require.ErrorIs(t, err, ErrResourcePoisoned, "seal must persist — poisoned paths are never auto-unsealed")
}

func sdPoisonYAML(t testing.TB, routed []string, sealed string) string {
	t.Helper()
	var tools string
	for _, r := range routed {
		tools += fmt.Sprintf("      - kind: agent\n        agent: %s\n        description: %q\n", r, r)
	}
	sub1 := fmt.Sprintf("  sub1:\n    system_prompt:\n      inline: \"PROMPT-sub1\"\n    memory:\n      type: localfile\n      path: %q\n", testStore(t, "sd-poison-sub1"))
	sub2 := fmt.Sprintf("  sub2:\n    system_prompt:\n      inline: \"PROMPT-sub2\"\n    memory:\n      type: localfile\n      path: %q\n", sealed)
	return "entry: main\nagents:\n  main:\n    system_prompt:\n      inline: \"MAIN\"\n    memory:\n      type: memory\n    tools:\n" +
		tools + sub1 + sub2
}

// TestRetire_RealPoisonedAcquireRefusesHotAddAndKeepsServing 钉住 热新增的存储路径被活写者封住时，必须在任何候选发布之前被拒。
// - 当前代必须完整照常服务、序号不前进；
// - 拒绝不得静默过期：poisoned 路径后续热更也不自动解封。
// - 封住动作放在自己的登记表里：它是该路径单写锁的另一个活持有者，用同进程构造出跨进程争用的形状。
// - 正向证据（lastFailure 具名到被拒的那次热新增）不可省：若换代根本没走到热新增分支，上面每条断言都会空洞地通过。
// 契约: docs/wiki/platform/org-hot-reload.md#candidate-refusal
func TestRetire_RealPoisonedAcquireRefusesHotAddAndKeepsServing(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	sealed := filepath.Join(dir, "sealed-store")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	sealer := NewRuntimeResources()
	sealThePath(t, sealer, sealed)
	t.Cleanup(func() { assertPathStillSealed(t, sealer, sealed) })

	write(sdPoisonYAML(t, []string{"sub1"}, sealed))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	residentBefore := residentCacheForTest(entry)
	sub1Before := residentBefore["sub1"]
	require.NotNil(t, sub1Before, "baseline: sub1 serves")
	genBefore := entry.OrgDiagnostics()["generation"]

	write(sdPoisonYAML(t, []string{"sub1", "sub2"}, sealed))
	entry.CheckOrgReload()

	require.Equal(t, genBefore, entry.OrgDiagnostics()["generation"],
		"§4.3：poisoned 路径上的热增必须被拒绝，绝不发布半相候选")
	require.Nil(t, residentCacheForTest(entry)["sub2"],
		"被封路径上不得出现第二个写者 owner")
	require.NotContains(t, entryToolNames(entry), "sub2", "旧代的工具面不得被部分改写")
	require.Same(t, sub1Before, residentCacheForTest(entry)["sub1"],
		"沿用旧配置＝同一存活 owner，不是重建的替身")
	require.Same(t, sub1Before.MemStore(), residentCacheForTest(entry)["sub1"].MemStore(),
		"存活 owner 的存储身份不漂移")
	require.False(t, sub1Before.CloseStarted(), "旧代 owner 不能被失败的回退牵连关闭")
	assertPathStillSealed(t, sealer, sealed)

	lf, ok := entry.OrgDiagnostics()["lastFailure"]
	require.True(t, ok && lf != nil, "被拒的热增必须留下可见失败记录（不是静默无操作）")
	require.Contains(t, fmt.Sprint(lf), "sub2", "记录须点出被封的那个名字")
	require.Contains(t, fmt.Sprint(lf), sealed, "and name the path it collided with")

	write(sdPoisonYAML(t, []string{"sub1", "sub2"}, sealed))
	entry.CheckOrgReload()
	require.Equal(t, genBefore, entry.OrgDiagnostics()["generation"],
		"poisoned 规则不因后续热更自动解封")
	require.Nil(t, residentCacheForTest(entry)["sub2"])
}

// closeOwnerYAML renders entry "main" delegating to `targets`. sub1/sub2 are
// always DEFINED — routability is the topology's truth source, not the file —
// and each keeps its own store so owner identity is observable per agent.
func closeOwnerYAML(t testing.TB, targets ...string) string {
	t.Helper()
	return ownerYAML(t, targets, fmt.Sprintf("      type: memory\n      path: %q\n", testStore(t, "own-close-sub2")))
}

// buildCloseOrg builds the org WITHOUT the usual t.Cleanup Close: these tests
// drive Close themselves and assert on what it left behind.
func buildCloseOrg(t *testing.T, yamlPath string) *agent.TagentAgent {
	t.Helper()
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&stubModel{name: "m"}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	return entry
}

// TestOrgClose_CoversEveryResidentOwner 钉住 组织最终关闭必须抵达它建出的每一个属主，而不只是交回的入口。
// - 入口 Close 返回后，每个冷建属主都要报自己的关闭、维护协程已返回、装配不留任何存储属主登记；
// - 见证取属主自身状态与装配登记表——只关了入口的清扫满足不了它们。
// 契约: docs/wiki/platform/org-hot-reload.md#close-drain
func TestOrgClose_CoversEveryResidentOwner(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(closeOwnerYAML(t, "sub1", "sub2"))
	entry := buildCloseOrg(t, yamlPath)

	owners := residentCacheForTest(entry)
	require.NotNil(t, owners["sub1"], "precondition: sub1 is a resident owner")
	require.NotNil(t, owners["sub2"], "precondition: sub2 is a resident owner")
	before := entry.StoreOwnerSnapshot()
	require.Contains(t, before, "sub2", "precondition: every owner registered its store before close")

	require.NoError(t, entry.Close())

	for name, owner := range owners {
		if name == "main" {
			continue
		}
		require.True(t, owner.CloseStarted(),
			"resident owner %q was never closed by the organization Close", name)
		require.True(t, owner.CleanerStopped(),
			"owner %q left its workspace cleaner goroutine running", name)
	}
	require.Empty(t, entry.StoreOwnerSnapshot(),
		"a closed organization must leave no store-owner registration behind")
}

// TestOrgClose_CoversHotAddedOwner 钉住 经热路径加入的属主与冷建的一样被组织拥有，同一次 Close 必须抵达它。
// - 待关清单在关闭时读取、不在启动时定格，这正是全部差别；
// - 热新增属主的关闭须已发起、维护生产者须已停、其存储属主登记须已撤销。
// 契约: docs/wiki/platform/org-hot-reload.md#close-drain
func TestOrgClose_CoversHotAddedOwner(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(closeOwnerYAML(t, "sub1"))
	entry := buildCloseOrg(t, yamlPath)

	write(closeOwnerYAML(t, "sub1", "sub2"))
	entry.CheckOrgReload()
	added := residentCacheForTest(entry)["sub2"]
	require.NotNil(t, added, "precondition: sub2 became resident through the hot path")
	require.Contains(t, entry.StoreOwnerSnapshot(), "sub2",
		"precondition: the hot-added owner registered its own store")

	require.NoError(t, entry.Close())
	require.True(t, added.CloseStarted(),
		"an owner the org adopted after startup must still be closed by its Close")
	require.True(t, added.CleanerStopped(), "and its maintenance producer must have stopped")
	require.NotContains(t, entry.StoreOwnerSnapshot(), "sub2",
		"and its registration revoked")
}

// TestOrgClose_DoesNotReplaceOwners 钉住 关闭按名字取出的常驻属主，必须就是测试先前捕获的那些实例本身。
// - 二次 Close（普通 t.Cleanup 跟进）必须幂等——属主自身序列恰好跑一次、不得复活；
// - 清扫若是重建 agent 而非关闭它们，会在此暴露为假通过。
// 契约: docs/wiki/platform/org-hot-reload.md#close-drain
func TestOrgClose_DoesNotReplaceOwners(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(closeOwnerYAML(t, "sub1", "sub2"))
	entry := buildCloseOrg(t, yamlPath)

	sub1 := residentCacheForTest(entry)["sub1"]
	require.NoError(t, entry.Close())
	require.True(t, sub1.CloseStarted())
	require.NoError(t, entry.Close())
	require.Same(t, sub1, residentCacheForTest(entry)["sub1"])
}

// TestOrgClose_SharedStoreWaitsForEveryBorrower 钉住 关闭序列对共享一个存储的两个存活属主，恰好交还一次写者槽。
// - 后端下沉期间任何属主都不得报错，路径最终干净释放——泄漏的租约持有 flock，过早或重复释放会封住路径，二者都让下面的重开失败；
// - 恰一次释放经登记表自身规则观察，非破坏性，不用会接管所测锁的 flock 探针。
// 契约: docs/wiki/platform/org-hot-reload.md#close-drain
func TestOrgClose_SharedStoreWaitsForEveryBorrower(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	shared := testStore(t, "orgclose-shared-store")
	write(twoAgentsOneStore(t, shared, "s1", "s2"))
	entry := buildRetireOrg(t, yamlPath)

	owners := residentCacheForTest(entry)
	s1, s2 := owners["s1"], owners["s2"]
	require.NotNil(t, s1)
	require.NotNil(t, s2)
	require.Same(t, s1.MemStore(), s2.MemStore(), "precondition: both owners borrow the same store")

	require.NoError(t, entry.Close(),
		"§4.3/D8：共享 store 必须等所有借用者退出——关闭序列中任一 owner 报错都说明后端被提前拆走")
	require.True(t, s1.CloseStarted() && s2.CloseStarted(), "both borrowers must have gone down")

	fresh := NewRuntimeResources()
	fp := fingerprintMemory(MemoryConfig{Type: "localfile", Path: shared})
	_, _, rel, err := fresh.acquire("localfile", shared, fp, func() (openedResource, error) {
		return openedResource{store: &seqStore{MemoryStore: nil, seq: new([]string)}}, nil
	})
	require.NoError(t, err,
		"关闭后共享路径必须能被新世代干净接手（被持有＝租约泄漏；ErrResourcePoisoned＝提前或重复释放被封路）")
	require.NoError(t, rel())
}

// TestRetire_SharedComponentWaitsForEveryBorrower 钉住 借用者仍活着时共享后端不得被拆，这一半在别处不可观察、在这里才可证。
// - 新代注册表试图接手该路径时必须撞上幸存者的活写者锁，失败的 LOCK_EX 不打扰持有者故非破坏；
// - 只有最后一个借用者退出后，新代才可接手。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestRetire_SharedComponentWaitsForEveryBorrower(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	shared := testStore(t, "retire-wait-borrowers")
	write(twoAgentsOneStore(t, shared, "s1", "s2"))
	entry := buildRetireOrg(t, yamlPath)
	t.Cleanup(func() { _ = entry.Close() })

	owners := residentCacheForTest(entry)
	s1, s2 := owners["s1"], owners["s2"]
	require.Same(t, s1.MemStore(), s2.MemStore(), "precondition: both owners borrow one store")

	write(twoAgentsOneStore(t, shared, "s1"))
	entry.CheckOrgReload()
	require.True(t, s2.CloseStarted(), "precondition: s2 retired")
	require.False(t, s1.CloseStarted(), "and s1 still borrows it")

	fresh := NewRuntimeResources()
	fp := fingerprintMemory(MemoryConfig{Type: "localfile", Path: shared})
	_, _, _, err := fresh.acquire("localfile", shared, fp, func() (openedResource, error) {
		t.Error("a second writer must not be opened while a borrower is alive")
		return openedResource{}, nil
	})
	require.ErrorIs(t, err, ErrStoreLocked,
		"§4.3/D8：仍有借用者时共享后端不得拆除（写锁必须还被存活者持有）")
	require.NotErrorIs(t, err, ErrResourcePoisoned, "「仍被持有」不同于「回收未确认被封路」")

	require.NoError(t, entry.Close())
	_, _, rel, err2 := fresh.acquire("localfile", shared, fp, func() (openedResource, error) {
		return openedResource{store: &seqStore{MemoryStore: nil, seq: new([]string)}}, nil
	})
	require.NoError(t, err2, "最后借用者退出后路径必须干净交接")
	require.NoError(t, rel())
}

// sdUsageYAML renders the G1/G2 pair for the deferred-delegation anchor: G1
// routes main → sub1, G2 drops that route (making sub1 unrouted).
//
// The rendered config carries no model/providers section on purpose: the host-injected
// mock serves every agent, which is what lets a real delegation turn run without a
// live endpoint.
func sdUsageYAML(t testing.TB, routeSub1 bool) string {
	t.Helper()
	ref := ""
	if routeSub1 {
		ref = `      - kind: agent
        agent: sub1
        description: "sub1"
`
	}
	return fmt.Sprintf(`entry: main
agents:
  main:
    system_prompt:
      inline: "main"
    tools:
%s  sub1:
    system_prompt:
      inline: "sub1"
    memory:
      type: localfile
      path: %q
`, ref, testStore(t, "hottest-sd-usage"))
}

// TestSD_DeferredDelegationIsProtectedByUsageRight 钉住 没被调用过的子代理不得因本体度量空闲，就在别一代仍路由它时被关闭。
// - 使用权由存活绑定各自已发布的面派生，不是第二套任务域、也不是平行路由表——那面已是唯一路由真源；
// - 一代仍是合法调用方直到其自身引用排空，被推迟的委派那时必须还能落到被保有的子代理。
// - 注入的 mock 对无工具的 agent 直接给最终答复，因此 sub1 内的真实委派回合会自行收尾，而不是伸手要活的 endpoint。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestSD_DeferredDelegationIsProtectedByUsageRight(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(sdUsageYAML(t, true))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&delegModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	main := residentCacheForTest(entry)["main"]
	require.NotNil(t, main, "G1：main 常驻")
	require.NotNil(t, residentCacheForTest(entry)["sub1"], "G1：sub1 常驻")

	lease := main.ContextManager().AcquireLease(agent.LeaseTurn)
	g1Wrapper := lease.SubagentWrapper("sub1")
	require.NotNil(t, g1Wrapper, "G1 自己的面上必须解析得出 sub1（同一版本真源）")

	write(sdUsageYAML(t, false))
	entry.CheckOrgReload()

	require.Nil(t, entry.ContextManager().SubagentWrapper("sub1"),
		"前提：现效代确实不再路由 sub1")
	require.NotNil(t, residentCacheForTest(entry)["sub1"],
		"§3.2 红锚：G1 仍保有 sub1 使用权（尚未调用也受保护），sweep 不得提前退役")

	_, err = g1Wrapper.Call(context.Background(), []byte(`{"request":"deferred call from G1"}`))
	require.NoError(t, err, "G1 在 G2 删除路由之后真调 sub1 仍须成功")

	lease.Release()
	require.Eventually(t, func() bool {
		return residentCacheForTest(entry)["sub1"] == nil
	}, 5*time.Second, 20*time.Millisecond,
		"使用权释放后 sub1 须有界退役（不永久保有）")
}

// sdOneRouteYAML renders entry main with exactly one routed sub-agent name.
func sdOneRouteYAML(t testing.TB, routed string, store string) string {
	t.Helper()
	return fmt.Sprintf(`entry: main
agents:
  main:
    system_prompt:
      inline: "main"
    tools:
      - kind: agent
        agent: %s
        description: %q
  %s:
    system_prompt:
      inline: %q
    memory:
      type: localfile
      path: %q
`, routed, routed, routed, routed, store)
}

// TestSD_ReleaseContinuesRetirementWithoutAnotherTurn 钉住 仅由使用权保有的待退役被释放本身解除阻塞，无新业务回合即退出。
// - 最后一个引用排空后属主须自行有界退出——若装配等下一个业务回合才察觉，空闲组织会把本已可关的属主无限期常驻；
// - 这正是"不可见的持有"要消除的对象。
// 契约: docs/wiki/platform/org-hot-reload.md#owner-retirement
func TestSD_ReleaseContinuesRetirementWithoutAnotherTurn(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	store := testStore(t, "hottest-sd-poke")
	tick := time.Now()
	write := func(content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(yamlPath, []byte(content), 0o644))
		tick = tick.Add(2 * time.Second)
		require.NoError(t, os.Chtimes(yamlPath, tick, tick))
	}

	write(sdOneRouteYAML(t, "sub1", store))
	cfg, err := LoadConfig(yamlPath)
	require.NoError(t, err)
	entry, err := New(*cfg, WithModel(&delegModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	main := residentCacheForTest(entry)["main"]
	require.NotNil(t, main)

	lease := main.ContextManager().AcquireLease(agent.LeaseTurn)
	write(sdOneRouteYAML(t, "sub2", store))
	entry.CheckOrgReload()
	require.NotNil(t, residentCacheForTest(entry)["sub1"],
		"前提：G1 持有使用权时 sub1 不得退役")

	lease.Release()
	require.Eventually(t, func() bool {
		return residentCacheForTest(entry)["sub1"] == nil
	}, 5*time.Second, 20*time.Millisecond,
		"§4.3：使用权释放本身须续排退役，不得依赖再来的业务 turn")
}

// deshellYAML renders the de-shell fixture (S-A/2.3): entry main delegating to
// sub1+sub2, each with an OWN model so a per-agent model change is a structural
// fingerprint delta (models move the fingerprint; prompts/memory here stay
// byte-identical unless the case says otherwise).
func deshellYAML(t testing.TB, mainModel, sub1Model, sub2Model string, sub3 bool) string {
	t.Helper()
	sub3Tool := ""
	sub3Block := ""
	if sub3 {
		sub3Tool = `      - kind: agent
        agent: sub3
        description: "sub3"
`
		sub3Block = fmt.Sprintf(`  sub3:
    system_prompt:
      inline: "sub3"
    model: %s
    memory:
      type: localfile
      path: %q
`, sub1Model+"-x", testStore(t, "hottest-sub3"))
	}
	return fmt.Sprintf(`entry: main
prompt_dir: resources/prompts
model: test-model
providers:
  openai:
    api_endpoint: "http://localhost:1"
agents:
  main:
    model: %s
    system_prompt:
      inline: "main"
    tools:
      - kind: agent
        agent: sub1
        description: "sub1"
      - kind: agent
        agent: sub2
        description: "sub2"
%s  sub1:
    model: %s
    system_prompt:
      inline: "sub1"
    memory:
      type: localfile
      path: %q
  sub2:
    model: %s
    system_prompt:
      inline: "sub2"
    memory:
      type: localfile
      path: %q
%s`, mainModel, sub3Tool, sub1Model, testStore(t, "hottest-sub1"), sub2Model, testStore(t, "hottest-sub2"), sub3Block)
}

// deshellHarness boots the org from deshellYAML(m0, s1, s2, sub3), returns the
// entry, the config path and an mtime-forcing writer (same tick discipline as
// the e2e family — FS granularity would otherwise swallow rapid rewrites).
func deshellHarness(t *testing.T, m0, s1, s2 string, sub3 bool) (*agent.TagentAgent, string) {
	t.Helper()
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
	write(deshellYAML(t, m0, s1, s2, sub3))
	cfg, err := LoadConfig(yamlPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	entry, err := New(*cfg, WithModel(&factoryMockModel{}), WithConfigPath(yamlPath))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = entry.Close() })
	return entry, yamlPath
}

// deshellReload mutates the config to (m1, s1b, s2b, sub3) and drives the
// synchronous check; it FAILS the test unless the structural publish actually
// happened (generation advanced) — a skipped reload would trivially satisfy any
// construction-count assertion.
func deshellReload(t *testing.T, entry *agent.TagentAgent, yamlPath, m1, s1b, s2b string, sub3 bool) {
	t.Helper()
	before := entry.OrgDiagnostics()["generation"].(int64)
	if err := os.WriteFile(yamlPath, []byte(deshellYAML(t, m1, s1b, s2b, sub3)), 0o644); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(yamlPath, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	entry.CheckOrgReload()
	after := entry.OrgDiagnostics()["generation"].(int64)
	require.Greater(t, after, before, "structural reload did not publish — construction-count assertions below would be vacuous")
}

// TestDeshell_EntryRegenerationConstructsZeroAgents 钉住 只改入口（换模型、兄弟逐字节相同）的热重载构造零个 agent。
// - 每次发布造一整只壳（总线/TaskManager/cleaner）再丢弃，正是去壳要消灭的代价；
// - 判别按对象寿命而非组织级计数：改既有 agent 须经面再生，不构造壳。
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func TestDeshell_EntryRegenerationConstructsZeroAgents(t *testing.T) {
	entry, yamlPath := deshellHarness(t, "model-a", "sub-m1", "sub-m2", false)
	before := agent.TagentAgentsConstructed()
	deshellReload(t, entry, yamlPath, "model-b", "sub-m1", "sub-m2", false)
	require.Zero(t, agent.TagentAgentsConstructed()-before,
		"S-A: modifying an existing agent must regenerate through the face, not construct shells")
}

// TestDeshell_ChangedSubAgentConstructsOneTransitional 钉住 改一个子代理（入口未动）经暂存面在稳定常驻实例上推进，构造零个 agent。
// - 暂存执行载体已并入常驻实例，构造数恒为零。
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func TestDeshell_ChangedSubAgentConstructsOneTransitional(t *testing.T) {
	entry, yamlPath := deshellHarness(t, "model-a", "sub-m1", "sub-m2", false)
	before := agent.TagentAgentsConstructed()
	deshellReload(t, entry, yamlPath, "model-a", "sub-m1x", "sub-m2", false)
	require.Zero(t, agent.TagentAgentsConstructed()-before,
		"S-D terminal: a changed sub-agent advances through its staged face on the stable resident instance — the transitional carrier is gone")
}

// TestDeshell_HotAddConstructsExactlyTheNewAgent 钉住 热新增完整构造那一个新 agent（恰好一个），去壳后不得围绕它重造入口与未变兄弟。
// - 去壳不等于去能力：新 agent 走完整常驻构造，入口与逐字节相同的兄弟不得再构造。
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func TestDeshell_HotAddConstructsExactlyTheNewAgent(t *testing.T) {
	entry, yamlPath := deshellHarness(t, "model-a", "sub-m1", "sub-m2", false)
	before := agent.TagentAgentsConstructed()
	deshellReload(t, entry, yamlPath, "model-a", "sub-m1", "sub-m2", true)
	require.Equal(t, int64(1), agent.TagentAgentsConstructed()-before,
		"J2: hot-add constructs exactly the new full agent; entry/unchanged siblings must not re-construct")
}

// TestDeshell_RollbackConstructsZeroForEntryOnlyChange 钉住 回滚一次仅入口的结构变更构造零个 agent，与正向热更同价。
// - 入口级回滚须经面再生，不构造壳。
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func TestDeshell_RollbackConstructsZeroForEntryOnlyChange(t *testing.T) {
	entry, yamlPath := deshellHarness(t, "model-a", "sub-m1", "sub-m2", false)
	deshellReload(t, entry, yamlPath, "model-b", "sub-m1", "sub-m2", false)
	before := agent.TagentAgentsConstructed()
	entry.Rollback()
	require.Zero(t, agent.TagentAgentsConstructed()-before,
		"S-A rollback: entry-only rollback must regenerate through the face, not shells")
}

// drillModel records every real request it is shown and answers one turn.
type drillModel struct {
	mu   sync.Mutex
	reqs [][]model.Message
}

func (m *drillModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	snap := make([]model.Message, len(req.Messages))
	copy(snap, req.Messages)
	m.reqs = append(m.reqs, snap)
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Done: true, Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "drill-ack"}}}}
	close(ch)
	return ch, nil
}

func (m *drillModel) Info() model.Info { return model.Info{Name: "drill-model"} }

// drillResetManagedUnits is the operator-side orchestration: probe every gate FIRST
// (all-or-nothing), then remove only managed-layout files.
//
// - Gate one is live writers: the cross-process single-writer flock must be acquirable, so an in-flight owner is refused with zero changes.
// - Gate two is the leaf guarded ledger: probe.CloseDurable is verification only, and the sweep runs on a fresh instance so its unacked ledger matches the post-removal disk.
// - Removal covers the exact managed layout only: per-partition kv-*.json snapshots plus the single-file shape and their tmp residue; envelope-style tmps live under the inbox unit.
func drillResetManagedUnits(storeDir, spillParent, anchorDir, agentName string, confirm bool) ([]string, error) {
	if !confirm {
		return nil, fmt.Errorf("drill reset: requires explicit confirmation (destructive operator act)")
	}
	lockF, err := acquireDirLock(storeDir)
	if err != nil {
		return nil, fmt.Errorf("drill reset: live writer on %s: %w", storeDir, err)
	}
	defer func() { _ = unlockDirLock(lockF) }()

	kvStore, err := kv.NewLocalFileKV(storeDir)
	if err != nil {
		return nil, fmt.Errorf("drill reset: store backend unreadable (current trouble, NOT transitional): %w", err)
	}
	store, err := memory.NewFileSegmentStore(kvStore, nil, storeDir, 100)
	if err != nil {
		return nil, fmt.Errorf("drill reset: store open failed: %w", err)
	}
	if err := store.RebuildLiveCounts(); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("drill reset: current-format scan failed — refusing to wipe: %w", err)
	}
	_ = store.Close()
	probe, err := agent.NewReliableEventBus(filepath.Join(spillParent, agentName))
	if err != nil {
		return nil, fmt.Errorf("drill reset: inbox undisposable/quarantine undispositioned: %w", err)
	}
	_ = probe.CloseDurable()

	var removals []string
	for _, pat := range []string{"kv.json", "kv.json.tmp", "kv-*.json", "kv-*.json.tmp"} {
		m, _ := filepath.Glob(filepath.Join(storeDir, pat))
		removals = append(removals, m...)
	}
	live, _ := filepath.Glob(filepath.Join(spillParent, agentName, "inbox-v2", "*.json"))
	removals = append(removals, live...)
	removals = append(removals, filepath.Join(anchorDir, agentName+".json"))

	// All gates passed — commit the unit reset.
	var removed []string
	for _, p := range removals {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return removed, fmt.Errorf("drill reset remove %s: %w", p, err)
		}
		if _, statErr := os.Lstat(p); statErr != nil {
			removed = append(removed, p)
		}
	}
	leaf, err := agent.NewReliableEventBus(filepath.Join(spillParent, agentName))
	if err != nil {
		return removed, fmt.Errorf("drill reset reopen: %w", err)
	}
	n, err := leaf.ResetTransitional(true)
	if err != nil {
		_ = leaf.CloseDurable()
		return removed, fmt.Errorf("drill reset transitional leaf: %w", err)
	}
	_ = leaf.CloseDurable()
	return append(removed, fmt.Sprintf("%d transitional file(s)", n)), nil
}

// TestDrill_ManagedRootReset_ConsistentUnitAndAllRefusals 钉住 托管根单元复位：任一 gate 不过就零改动拒绝，只清托管布局。
// - 拒绝即零改动：quarantine 未处置与活写者持锁（ErrStoreLocked）都不得留下部分清理；非托管内容与软链的外部目标永不被删。
// - 复位后的启动相由独立进程完成（一次 boot 只有真实进程启动才算证据），该子进程不设任何竞态豁免：出现竞态或非零退出即硬失败并附全日志。
func TestDrill_ManagedRootReset_ConsistentUnitAndAllRefusals(t *testing.T) {
	root := t.TempDir()
	storeDir := filepath.Join(root, "store")
	spillDir := filepath.Join(root, "spill")
	anchorDir := filepath.Join(root, "anchor")
	require.NoError(t, os.MkdirAll(storeDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(spillDir, "tagent"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(spillDir, "tagent", "inbox-v1"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(spillDir, "tagent", "inbox-v2", "quarantine"), 0o755))
	require.NoError(t, os.MkdirAll(anchorDir, 0o755))

	legacyV1 := filepath.Join(spillDir, "tagent", "inbox-v1", "old-v1.json")
	require.NoError(t, os.WriteFile(legacyV1, []byte(`{"version":1}`), 0o644))
	legacySpill := filepath.Join(spillDir, "tagent", "job.spill")
	require.NoError(t, os.WriteFile(legacySpill, []byte("spill"), 0o644))
	victim := filepath.Join(root, "outside-victim.json")
	require.NoError(t, os.WriteFile(victim, []byte("DO-NOT-DELETE"), 0o644))
	require.NoError(t, os.Symlink(victim, filepath.Join(spillDir, "tagent", "inbox-v1", "escape.json")))

	kvStore, err := kv.NewLocalFileKV(storeDir)
	require.NoError(t, err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, storeDir, 100)
	require.NoError(t, err)
	k := memory.NewSnowflakeEventKey(1, 0)
	require.NoError(t, store.StoreEvent(k, memory.FullEvent{EventKey: k, PartitionID: 1, EventType: "external_input", Content: "current-fact", Timestamp: 1}))
	require.NoError(t, store.Close())
	bus, err := agent.NewReliableEventBus(filepath.Join(spillDir, "tagent"))
	require.NoError(t, err)
	_, err = bus.PublishContext(context.Background(), agent.NewExternalInputEvent("user", model.NewUserMessage("pre-reset-envelope")))
	require.NoError(t, err)
	require.NoError(t, bus.CloseDurable())
	require.NoError(t, os.WriteFile(filepath.Join(anchorDir, "tagent.json"), []byte("anchor"), 0o644))

	unmanaged := filepath.Join(storeDir, "user-notes.txt")
	require.NoError(t, os.WriteFile(unmanaged, []byte("mine"), 0o644))
	evidence := filepath.Join(spillDir, "tagent", "inbox-v2", "quarantine", "evidence-1.json")
	require.NoError(t, os.WriteFile(evidence, []byte(`{"corrupt":true}`), 0o644))

	_, err = drillResetManagedUnits(storeDir, spillDir, anchorDir, "tagent", false)
	require.Error(t, err)
	require.FileExists(t, legacyV1)

	_, err = drillResetManagedUnits(storeDir, spillDir, anchorDir, "tagent", true)
	require.Error(t, err, "undispositioned quarantine must refuse the unit reset")
	require.FileExists(t, evidence, "quarantine evidence files are NEVER deleted by the reset")
	require.FileExists(t, legacyV1, "refusal means ZERO changes")
	require.NotEmpty(t, kvSnapshotsIn(storeDir), "refusal means ZERO changes (a partition snapshot is on disk)")

	require.NoError(t, os.Rename(evidence, filepath.Join(root, "dispositioned-1.json")))

	held, err := acquireDirLock(storeDir)
	require.NoError(t, err)
	_, err = drillResetManagedUnits(storeDir, spillDir, anchorDir, "tagent", true)
	require.ErrorIs(t, err, ErrStoreLocked, "a live writer must be refused")
	require.FileExists(t, legacyV1)
	require.NoError(t, unlockDirLock(held))

	_, err = drillResetManagedUnits(storeDir, spillDir, anchorDir, "tagent", true)
	require.NoError(t, err)
	require.NoFileExists(t, legacyV1, "transitional v1 cleared")
	require.NoFileExists(t, legacySpill, "legacy spill cleared")
	require.Empty(t, kvSnapshotsIn(storeDir), "store unit cleared — consistency (every kv-*.json gone)")
	require.NoFileExists(t, filepath.Join(anchorDir, "tagent.json"), "anchor unit cleared")
	liveLeft, _ := filepath.Glob(filepath.Join(spillDir, "tagent", "inbox-v2", "*.json"))
	require.Empty(t, liveLeft, "live envelopes cleared with the store (no unit half-reset)")
	require.FileExists(t, unmanaged, "unmanaged content is never removed")
	require.FileExists(t, victim, "path escape removed at most the link, never the outside target")
	require.DirExists(t, filepath.Dir(evidence))

	runBootChild(t, append(os.Environ(),
		"TAGENT_DRILL_STORE="+storeDir,
		"TAGENT_DRILL_SPILL="+spillDir,
		"TAGENT_DRILL_ANCHOR="+anchorDir),
		"TAGENT_DRILL_PHASE=boot-turn", "TestDrill_ManagedRootResetBootChild$")
}

// TestDrill_ManagedRootResetBootChild 钉住 单元复位之后的启动相只认当前格式的路径，并能完成一个真实回合。
// - 判据按包含而非相等：框架守卫可能在用户输入上添加装饰。
func TestDrill_ManagedRootResetBootChild(t *testing.T) {
	if os.Getenv("TAGENT_DRILL_PHASE") != "boot-turn" {
		t.Skip("drill boot child")
	}
	storeDir := os.Getenv("TAGENT_DRILL_STORE")
	spillDir := os.Getenv("TAGENT_DRILL_SPILL")
	anchorDir := os.Getenv("TAGENT_DRILL_ANCHOR")
	m := &drillModel{}
	ta, err := New(Config{
		Entry: "tagent",
		Agents: map[string]AgentConfig{"tagent": {
			SystemPrompt: PromptConfig{Inline: "post-reset"},
			MaxTokens:    4000,
			Memory:       MemoryConfig{Type: "localfile", Path: storeDir},
		}},
		Reliability: ReliabilityConfig{BusSpillDir: spillDir, MeditationAnchorDir: anchorDir},
	}, WithModel(m))
	require.NoError(t, err, "reset-then-boot under the CURRENT format")
	s, err := ta.ReconcileOutstanding()
	require.NoError(t, err)
	require.Equal(t, agent.ReconcileSummary{}, s, "nothing recoverable survived the unit reset (as authorized)")
	out, err := ta.StartLoop("u", "post-reset-session")
	require.NoError(t, err)
	drop := make(chan struct{})
	go func() {
		defer close(drop)
		for range out {
		}
	}()
	_, err = ta.InjectMessageContext(context.Background(), "user", model.NewUserMessage("post-reset-first-input"))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, req := range m.reqs {
			for _, msg := range req {
				if strings.Contains(msg.Content, "post-reset-first-input") {
					return true
				}
			}
		}
		return false
	}, 15*time.Second, 20*time.Millisecond, "post-reset boot must reach the model on the CURRENT path")
	require.NoError(t, ta.Close())
	<-drop
}

// kvSnapshotsIn lists the KV store unit's snapshot files (partition layout:
// kv-<label>.json; the single-file kv.json is counted for the ZERO-change proof).
func kvSnapshotsIn(storeDir string) []string {
	partitions, _ := filepath.Glob(filepath.Join(storeDir, "kv-*.json"))
	legacy, _ := filepath.Glob(filepath.Join(storeDir, "kv.json"))
	return append(partitions, legacy...)
}

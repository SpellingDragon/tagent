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

// §4.3 (R02) — retirement of an owner whose name left the routable set.
//
// Removal is the easy half and was already true: the published generation simply
// stops offering the name. What was missing is the OTHER edge — an unrouted owner
// must not stay resident forever, yet must not be closed while its own executions,
// background work or accepted inputs still depend on it. These tests pin both
// halves: the ledger holds what is still needed and releases exactly what is not.

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

	write(retYAML(t, "b", "s1")) // 移除 s3（定义仍在文件里）
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
	// One execution still riding s2 (the same lease accounting the reclaim path uses).
	lease := s2.ContextManager().AcquireLease(agent.LeaseSubCall)
	require.Equal(t, 1, s2.Obligations().Executions, "precondition: the obligation probe sees the live execution")

	write(retYAML(t, "b", "s1"))
	entry.CheckOrgReload()

	require.NotNil(t, residentCacheForTest(entry)["s2"],
		"an owner with a live obligation must stay resident — not retired early")
	require.False(t, s2.CloseStarted(), "and must NOT be closed")
	require.Contains(t, entry.StoreOwnerSnapshot(), "s2", "and keeps its store registration")

	lease.Release()
	// The next activity-driven boundary sweeps the ledger again.
	// The next activity boundary drains it — no structural change needed, and none
	// faked: the same content is rewritten (only its mtime moves), so retirement must
	// not depend on this boundary also publishing something new.
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
	lease := s2.ContextManager().AcquireLease(agent.LeaseSubCall) // hold it in the drain list
	write(retYAML(t, "b", "s1"))
	entry.CheckOrgReload()
	require.False(t, s2.CloseStarted(), "precondition: still draining, not closed")

	write(retYAML(t, "c", "s1", "s2")) // 同名重入
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
	// The reference stays HELD on purpose. What this test must pin is the window
	// where the owner has BEGUN closing but has not finally exited; since §4.3 made
	// a release carry the retirement forward by itself, releasing here would leave
	// nothing mid-close to refuse — the finally-exited case is the test below.
	s2.Close() // host-driven begin: mid-close, still tracked
	t.Cleanup(func() { lease.Release() })

	write(retYAML(t, "c", "s1", "s2"))
	before := entry.OrgDiagnostics()["generation"]
	entry.CheckOrgReload()

	require.NotContains(t, entryToolNames(entry), "s2",
		"a publish that wants a closing name must be refused, not silently served by a new owner")
	require.Same(t, s2, residentCacheForTest(entry)["s2"],
		"the refused candidate must not have replaced the owner with a second writer")
	require.Equal(t, before, entry.OrgDiagnostics()["generation"], "a refusal never advances the published sequence")

	// Release now rather than only in cleanup: ExecLease.Release is idempotent, and
	// dropping the reference before the deferred org Close keeps the refusal check
	// itself (above) from being followed by a bounded drain the test never needed.
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

	lease.Release() // the release alone must finish the retirement (§4.3, no new turn)
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

	// Rotate the routed name through every definition, twice over.
	for round := 0; round < 2; round++ {
		for _, n := range retAllDefs {
			write(retYAML(t, fmt.Sprintf("churn-%d-%s", round, n), "s1", n))
			entry.CheckOrgReload()
		}
	}
	// Last round routed s1+s3, so the bound is main plus those two — NOT every name
	// that ever existed.
	require.Len(t, residentCacheForTest(entry), 3,
		"resident set must equal main + the currently routed names, not every name ever routed (got %v)",
		residentNamesOf(entry))
	require.Len(t, entry.StoreOwnerSnapshot(), 3, "and so must the store-owner registrations")
	// §5.1 迁移（非静默）：债务键改为常驻呈现，「没有待退役」从此可由**空列表**
	// 判定，而不是靠「键不存在」——缺键与零债务是两件事，前者无法与「诊断面没
	// 接上」区分。
	debt, ok := entry.OrgDiagnostics()["liveDebt"].(OrgLiveDebt)
	require.True(t, ok, "the live debt group must always be present")
	require.Empty(t, debt.PendingRetirements,
		"nothing is left draining once every retired owner converged")
}

// TestRetire_SharedStoreSurvivesSiblingRetirement: two owners on ONE store. Retiring
// one must not close the shared state under the other — the store is released by
// lease accounting, so the survivor's last writer remains valid.
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

	write(twoAgentsOneStore(t, shared, "s1")) // 移除 s2
	entry.CheckOrgReload()
	require.True(t, s2.CloseStarted(), "the unrouted sibling retired")
	require.NotContains(t, residentCacheForTest(entry), "s2")

	// The survivor keeps serving from the SAME store object: it is not closed, its
	// maintenance producer is still running, and the store it shares with the
	// retired sibling is still its own (lease accounting, not the sibling's exit,
	// decides when the shared state goes away).
	require.Same(t, s1.MemStore(), residentCacheForTest(entry)["s1"].MemStore())
	require.False(t, s1.CloseStarted(), "the surviving owner must not be closed by its sibling's retirement")
	require.False(t, s1.CleanerStopped(), "and its producers stay up")
	require.Contains(t, entryToolNames(entry), "s1", "and it is still routed")
}

// TestRetire_RollbackRetiresDroppedOwner: a rollback is a publish too. The owners it
// drops go down the same drain, and the config kept in the rollback ring is DATA —
// never a reason to hold a running instance.
func TestRetire_RollbackRetiresDroppedOwner(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(retYAML(t, "a", "s1", "s2"))
	entry := buildRetireOrg(t, yamlPath)
	defer func() { _ = entry.Close() }()

	s2 := residentCacheForTest(entry)["s2"]
	write(retYAML(t, "b", "s1")) // G2: s2 gone
	entry.CheckOrgReload()
	require.NotContains(t, residentCacheForTest(entry), "s2", "precondition: G2 already retired it")

	entry.Rollback() // back to G1's routing, which wants s2 again
	require.Contains(t, entryToolNames(entry), "s2", "the rollback really re-routed s2")
	fresh := residentCacheForTest(entry)["s2"]
	require.NotNil(t, fresh, "a fully-closed owner is not resurrected; the name gets a fresh owner")
	require.NotSame(t, s2, fresh, "and it is NOT the closed instance")
	require.False(t, fresh.CloseStarted(), "the fresh owner serves")
}

// TestOrgHotAdd_VisibleOnlyAtCommit is §4.3's FIRST clause (「新增只随候选提交可见」)
// at the only point it can be observed: parked inside the commit critical section,
// after the candidate's owners were built and before they are merged. Existing tests
// cover the other half (a REFUSED candidate's adds are revoked); nothing pinned that
// a staged add is invisible WHILE the build is still in flight — and a half-committed
// topology is exactly what D3's single commit point exists to prevent.
//
// Note what is deliberately NOT asserted as invisible: the new owner's store-owner
// registration. That is taken during the build on purpose (R01: the rollback must be
// able to revoke EVERY owner this candidate registered), so claiming otherwise would
// describe a different design.
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

	write(retYAML(t, "b", "s1", "s2")) // now route s2: a real hot add
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

// TestRetire_DiamondSharedDependencyWaitsForAllBorrowers is §4.3's named
// acceptance「菱形共享依赖」at RETIREMENT time (the build-side diamond is anchored
// once in build_cycle_test): main → {s1, s2} → s3.
//
// What must hold when a generation drops BOTH entry routes:
//  1. a branch with nothing left on it (s1) converges, while
//  2. the shared leaf (s3) is NOT retired just because it has no work of its own —
//     it is still inside the callable closure of s2's live generation, which has an
//     execution in flight and may delegate at any moment (D8's usage right, §5.37);
//     retiring it here would silently break a legal later call;
//  3. once that last borrower drains, s3 exits on its own — no extra business turn
//     (§5.38), and the cascade (s2 then s3) converges inside one drain pass instead
//     of waiting for an event that may never come.
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

	// s2 has an execution in flight; it has NOT delegated to s3 yet, but its
	// generation legitimately may.
	inFlight := s2.ContextManager().AcquireLease(agent.LeaseSubCall)

	// One generation drops BOTH entry routes: s1, s2 and (transitively) s3 leave the
	// reachable set.
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

	// The last borrower draining releases the leaf too; the cascade must converge
	// without any further traffic.
	inFlight.Release()
	require.Eventually(t, func() bool {
		o := residentCacheForTest(entry)
		return o["s2"] == nil && o["s3"] == nil
	}, 5*time.Second, 20*time.Millisecond,
		"最后借用者退出后，s2 与共享的 s3 须在同一个排空里依次收敛（不等待新 turn）")
}

// sealThePath produces a REAL sealed store, not a mocked error: it drives
// RuntimeResources' own §6.5 rule (an unconfirmed reclaim must HOLD the writer
// flock and seal the path), so afterwards a live single-writer lock genuinely
// sits on that directory. Any org that later tries to open the same path must
// therefore collide with a possibly-half-live backend — the exact condition
// design D8 forbids papering over (「backend/锁退出失败继续原 poisoned 规则，
// 不自动解封」), and the org-side half of §4.3's「真实 poisoned acquire」.
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

// TestRetire_RealPoisonedAcquireRefusesHotAddAndKeepsServing is §4.3's
// 「真实 poisoned acquire」at the org boundary: a hot-add whose store path is
// sealed by a live writer must be refused BEFORE any candidate is published, the
// current generation must keep serving intact, and the refusal must not quietly
// expire (no auto-unseal on a later apply).
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

	// The seal lives in its own registry (a different live holder of the path's
	// single-writer lock — cross-process contention modelled in-process).
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

	// Hot-add sub2 on the SEALED path.
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

	// Positive proof the check actually ran and was refused on the way to building
	// sub2 — without this, every assertion above would pass vacuously if the reload
	// had simply not reached the hot-add branch at all.
	lf, ok := entry.OrgDiagnostics()["lastFailure"]
	require.True(t, ok && lf != nil, "被拒的热增必须留下可见失败记录（不是静默无操作）")
	require.Contains(t, fmt.Sprint(lf), "sub2", "记录须点出被封的那个名字")
	require.Contains(t, fmt.Sprint(lf), sealed, "and name the path it collided with")

	// 不自动解封：再一次 apply（另一处无关变更）仍须拒绝同一路径。
	write(sdPoisonYAML(t, []string{"sub1", "sub2"}, sealed))
	entry.CheckOrgReload()
	require.Equal(t, genBefore, entry.OrgDiagnostics()["generation"],
		"poisoned 规则不因后续热更自动解封")
	require.Nil(t, residentCacheForTest(entry)["sub2"])
}

// §4.3 (R02) — the organization's final Close must reach EVERY owner it built,
// not just the entry it returned. Before this task, Close was a single-instance
// operation: the entry's own sequence ran, while each resident sub-owner kept
// its context manager, its store-owner registration and its maintenance
// goroutine behind. That is the defect R02 names (「被移除 owner 仍永久留在常驻表,
// 正常退役与组织最终关闭没有完整闭环」) observed at its simplest boundary: a host
// that does everything right — builds an org, serves nothing, calls Close — still
// leaks every owned resource except the entry's.
//
// The witnesses below are the owner's own close state (CloseStarted), the state
// its maintenance producer reached (CleanerStopped), and the assembly's
// registration set (StoreOwnerSnapshot). Each is a fact about the owner, so a
// sweep that merely closed the entry cannot satisfy them.

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

// TestOrgClose_CoversEveryResidentOwner: after the entry's Close returns, every
// cold-built owner must report its own close, its cleaner goroutine must have
// returned, and the assembly must hold NO store-owner registration left (each
// revoked by the owner whose store exit it just took).
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

// TestOrgClose_CoversHotAddedOwner is spec scenario「组织关闭覆盖热新增 owner」:
// an owner that joined through the hot path is owned by the organization just
// like a cold one, so the same Close must reach it. The list is read at close
// time, not captured at startup — that is the whole difference.
func TestOrgClose_CoversHotAddedOwner(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(closeOwnerYAML(t, "sub1"))
	entry := buildCloseOrg(t, yamlPath)

	// 热新增 sub2：从只路由 sub1 改为同时路由 sub2。
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

// residentCacheForTest keyed by name must NOT be confused with the board a
// closed org leaves: assert the table's owners are the same instances the tests
// captured, so a sweep that rebuilt agents instead of closing them would show
// up here as a false pass.
func TestOrgClose_DoesNotReplaceOwners(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "tagent.yaml")
	write := ownerWriter(t, yamlPath)
	write(closeOwnerYAML(t, "sub1", "sub2"))
	entry := buildCloseOrg(t, yamlPath)

	sub1 := residentCacheForTest(entry)["sub1"]
	require.NoError(t, entry.Close())
	require.True(t, sub1.CloseStarted())
	// A second Close is the ordinary t.Cleanup follow-up: it must be idempotent
	// (the owner's own sequence runs exactly once) and must not resurrect it.
	require.NoError(t, entry.Close())
	require.Same(t, sub1, residentCacheForTest(entry)["sub1"])
}

// TestOrgClose_SharedStoreWaitsForEveryBorrower is the last open acceptance item of
// §4.3 (「共享组件等所有借用者」 at the org-Close boundary; design D8): two live
// owners borrow ONE store, and the close sequence must hand the writer slot back
// exactly once.
//
//	`TestRetire_SharedStoreSurvivesSiblingRetirement` already pins the retirement
//
// side's identity/liveness claims. This covers what Close can actually be shown to
// guarantee, measured (evidence §5.41):
//  1. no owner reports an error while the shared backend goes down, and
//  2. the path ends up cleanly released — a leaked lease keeps the flock and a
//     premature/doubled release would seal the path, so the reopen below fails in
//     either case (proved by probe PC: skipping the last teardown → red here).
//
// What Close does NOT make observable: an early teardown while another borrower is
// still alive stays invisible through this seam (probes PA/PB: tearing down on the
// first release left `Close` clean and the path re-acquirable — the later release
// just goes stale). That half is pinned where it IS observable, in
// TestRetire_SharedComponentWaitsForEveryBorrower.
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

	// Exactly-once release, observed through the registry's own rules (non-destructive
	// by construction — unlike a flock probe, which would take over the very lock it
	// measures; see evidence §5.40). Success proves: the flock is free (no leaked
	// holder) AND the path is not poisoned (no premature/doubled release was sealed).
	fresh := NewRuntimeResources()
	fp := fingerprintMemory(MemoryConfig{Type: "localfile", Path: shared})
	_, _, rel, err := fresh.acquire("localfile", shared, fp, func() (openedResource, error) {
		return openedResource{store: &seqStore{MemoryStore: nil, seq: new([]string)}}, nil
	})
	require.NoError(t, err,
		"关闭后共享路径必须能被新世代干净接手（被持有＝租约泄漏；ErrResourcePoisoned＝提前或重复释放被封路）")
	require.NoError(t, rel())
}

// TestRetire_SharedComponentWaitsForEveryBorrower pins the other half of the same
// rule (design D8「共享组件等所有借用者」) where it is ACTUALLY observable: while a
// borrower is still alive, the shared backend must not be torn down. A fresh
// registry trying to take the path must collide with the survivor's live writer
// lock — a failed LOCK_EX does not disturb the holder, so unlike a successful probe
// this check is non-destructive (evidence §5.40). And only after the LAST borrower
// exits may a new generation take over.
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

	write(twoAgentsOneStore(t, shared, "s1")) // 首个借用者退出
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

	require.NoError(t, entry.Close()) // 最后一个借用者退出
	_, _, rel, err2 := fresh.acquire("localfile", shared, fp, func() (openedResource, error) {
		return openedResource{store: &seqStore{MemoryStore: nil, seq: new([]string)}}, nil
	})
	require.NoError(t, err2, "最后借用者退出后路径必须干净交接")
	require.NoError(t, rel())
}

// sdUsageYAML renders the G1/G2 pair for the deferred-delegation anchor: G1
// routes main → sub1, G2 drops that route (making sub1 unrouted).
func sdUsageYAML(t testing.TB, routeSub1 bool) string {
	t.Helper()
	ref := ""
	if routeSub1 {
		ref = `      - kind: agent
        agent: sub1
        description: "sub1"
`
	}
	// No model/providers section: the host-injected mock serves every agent, which
	// is what lets a real delegation turn run without a live endpoint.
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

// TestSD_DeferredDelegationIsProtectedByUsageRight is the §3.2 red anchor named
// in the task ("A 持 G1、尚未调用 B 时 G2 删 B，之后 G1 真调 B 仍成功"), per D8:
// a version holds the usage rights of its locally-callable closure INCLUDING the
// B it has not actually called yet, because G1 remains a legitimate caller until
// its own references drain.
//
// Today's sweep asks only the owner about ITS obligations (executions /
// invocations / live tasks). A never-called sub1 is Idle by that measure, so it
// gets closed while G1 still routes to it — the deferred delegation then fails.
// The usage right is not a second task domain (J7) and not a parallel routing
// table: it is derived from the live bindings' own published faces, which are
// already the single routing truth (§4.2).
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
	// delegModel serves a toolless agent with a plain final answer, so a real
	// delegation turn inside sub1 completes instead of reaching a live endpoint.
	entry, err := New(*cfg, WithModel(&delegModel{}), WithConfigPath(yamlPath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = entry.Close() })

	main := residentCacheForTest(entry)["main"]
	require.NotNil(t, main, "G1：main 常驻")
	require.NotNil(t, residentCacheForTest(entry)["sub1"], "G1：sub1 常驻")

	// A request accepted on G1 — its business turn pins the generation while the
	// model has NOT yet emitted the delegation to sub1.
	lease := main.ContextManager().AcquireLease(agent.LeaseTurn)
	g1Wrapper := lease.SubagentWrapper("sub1")
	require.NotNil(t, g1Wrapper, "G1 自己的面上必须解析得出 sub1（同一版本真源）")

	// G2 removes the route: sub1 becomes unrouted and enters the retirement ledger.
	write(sdUsageYAML(t, false))
	entry.CheckOrgReload()

	require.Nil(t, entry.ContextManager().SubagentWrapper("sub1"),
		"前提：现效代确实不再路由 sub1")
	require.NotNil(t, residentCacheForTest(entry)["sub1"],
		"§3.2 红锚：G1 仍保有 sub1 使用权（尚未调用也受保护），sweep 不得提前退役")

	// The deferred delegation itself must still work — host result, not a flag.
	_, err = g1Wrapper.Call(context.Background(), []byte(`{"request":"deferred call from G1"}`))
	require.NoError(t, err, "G1 在 G2 删除路由之后真调 sub1 仍须成功")

	// Boundedness (§4.1/D8): a released usage right must not hold the owner
	// forever. The release itself carries the drain forward — no extra business
	// turn is required (TestSD_ReleaseContinuesRetirementWithoutAnotherTurn pins
	// that without supplying any traffic at all).
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

// TestSD_ReleaseContinuesRetirementWithoutAnotherTurn is the §4.3 requirement
// the previous round left on the books: 「释放使用权/任务收尾经原生命周期轻量通知
// 继续退役，不必须再来一个业务 turn」.
//
// A pending retirement that was held ONLY by a usage right gets unblocked by the
// release itself. If the assembly waited for the next business turn to notice,
// an idle org would keep a closed-in-principle owner resident indefinitely — the
// very「invisible 持有」this change set out to remove. So after the last reference
// on the holding generation drains, the owner must exit on its own, bounded, with
// NO traffic of any kind supplied afterwards.
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

	// One generation pinned by a single turn lease: it is the ONLY holder of sub1
	// (which never gets called, so its own three obligation axes stay zero).
	lease := main.ContextManager().AcquireLease(agent.LeaseTurn)
	write(sdOneRouteYAML(t, "sub2", store))
	entry.CheckOrgReload()
	require.NotNil(t, residentCacheForTest(entry)["sub1"],
		"前提：G1 持有使用权时 sub1 不得退役")

	// Release, then supply NOTHING — no turn, no inject, no reload call.
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
		// sub3 must be REFERENCED from main's tools or reachableAgents excludes
		// it — an unreferenced declaration is not a hot-add (and never builds).
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

// TestDeshell_EntryRegenerationConstructsZeroAgents is the S-A red anchor: a
// hot reload that only MODIFIES the entry (model swap; sub1/sub2 byte-identical)
// must construct ZERO TagentAgents. The discarded entry shell was the entire
// point of D1 去壳 — one full agent (bus/TaskManager/cleaner) per publish,
// thrown away.
func TestDeshell_EntryRegenerationConstructsZeroAgents(t *testing.T) {
	entry, yamlPath := deshellHarness(t, "model-a", "sub-m1", "sub-m2", false)
	before := agent.TagentAgentsConstructed()
	deshellReload(t, entry, yamlPath, "model-b", "sub-m1", "sub-m2", false)
	require.Zero(t, agent.TagentAgentsConstructed()-before,
		"S-A: modifying an existing agent must regenerate through the face, not construct shells")
}

// TestDeshell_ChangedSubAgentConstructsOneTransitional pinned the transitional
// cost of S-A: modifying sub1 (entry untouched) constructed EXACTLY ONE agent —
// sub1's transitional executor carrier.〔轮九十迁移（显式）〕S-D/3.2 has now
// landed (user-approved holding expansion): every owner's execution view advances
// through staged faces wired to stable resident instances, and the transitional
// shell is DEAD — the terminal ZERO this anchor always named as its own end state.
// The name keeps the history; the pinned number is the terminal one.
func TestDeshell_ChangedSubAgentConstructsOneTransitional(t *testing.T) {
	entry, yamlPath := deshellHarness(t, "model-a", "sub-m1", "sub-m2", false)
	before := agent.TagentAgentsConstructed()
	deshellReload(t, entry, yamlPath, "model-a", "sub-m1x", "sub-m2", false)
	require.Zero(t, agent.TagentAgentsConstructed()-before,
		"S-D terminal: a changed sub-agent advances through its staged face on the stable resident instance — the transitional carrier is gone")
}

// TestDeshell_HotAddConstructsExactlyTheNewAgent pins J2 (去壳≠去能力): a
// hot-add builds the new agent fully (ONE construction) and — de-shelled — no
// longer re-shells entry/unchanged siblings around it.
func TestDeshell_HotAddConstructsExactlyTheNewAgent(t *testing.T) {
	entry, yamlPath := deshellHarness(t, "model-a", "sub-m1", "sub-m2", false)
	before := agent.TagentAgentsConstructed()
	deshellReload(t, entry, yamlPath, "model-a", "sub-m1", "sub-m2", true)
	require.Equal(t, int64(1), agent.TagentAgentsConstructed()-before,
		"J2: hot-add constructs exactly the new full agent; entry/unchanged siblings must not re-construct")
}

// TestDeshell_RollbackConstructsZeroForEntryOnlyChange extends the anchor to
// the rollback path (same de-shelled treatment): rolling back an entry-only
// structural change constructs ZERO agents.
func TestDeshell_RollbackConstructsZeroForEntryOnlyChange(t *testing.T) {
	entry, yamlPath := deshellHarness(t, "model-a", "sub-m1", "sub-m2", false)
	deshellReload(t, entry, yamlPath, "model-b", "sub-m1", "sub-m2", false)
	before := agent.TagentAgentsConstructed()
	entry.Rollback()
	require.Zero(t, agent.TagentAgentsConstructed()-before,
		"S-A rollback: entry-only rollback must regenerate through the face, not shells")
}

// §8.6 — MANAGED-ROOT RESET DRILL, run entirely inside temporary managed dirs
// (never a real unspecified directory). Consistent recovery-unit reset: the
// store, the inbox (v2 live tree + transitional legacy) and the meditation
// anchor are ONE unit — a reset clears them all or nothing. Guards drilled
// here: live-writer refusal (flock), explicit-confirm refusal, path-escape
// safety (symlink victims survive), unmanaged content preserved, quarantine
// evidence never auto-wiped (current corruption must be dispositioned by the
// operator first), and a post-reset boot that only knows the current format.

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

// drillResetManagedUnits is the operator-side orchestration: probe every gate
// FIRST (all-or-nothing), then remove only managed-layout files.
func drillResetManagedUnits(storeDir, spillParent, anchorDir, agentName string, confirm bool) ([]string, error) {
	if !confirm {
		return nil, fmt.Errorf("drill reset: requires explicit confirmation (destructive operator act)")
	}
	// Gate 1 — live writers: the store's cross-process single-writer flock
	// must be acquirable (in-flight owner ⇒ refuse with zero changes).
	lockF, err := acquireDirLock(storeDir)
	if err != nil {
		return nil, fmt.Errorf("drill reset: live writer on %s: %w", storeDir, err)
	}
	defer func() { _ = unlockDirLock(lockF) }() // unlock closes

	// Gate 2 — verify-and-enumerate before touching anything: an unreadable
	// CURRENT backend (corruption / I/O trouble) is never treated as legacy
	// data to sweep.
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
	_ = probe.CloseDurable() // verification only — the leaf reopens post-removal

	var removals []string
	// Store unit: only the managed layout (kv snapshot + its tmp).
	// Exact managed layout names (review 7677c07 #2): LocalFileKV writes
	// "kv.json" + its single "kv.json.tmp"; envelope-style tmps live under
	// the inbox unit and are matched there by pattern.
	for _, pat := range []string{"kv.json", "kv.json.tmp"} {
		m, _ := filepath.Glob(filepath.Join(storeDir, pat))
		removals = append(removals, m...)
	}
	// Inbox unit: live-tree envelopes of the unit (quarantine/ NOT matched)
	// plus leaf-classified transitional legacy via the leaf's own guarded API.
	live, _ := filepath.Glob(filepath.Join(spillParent, agentName, "inbox-v2", "*.json"))
	removals = append(removals, live...)
	// Anchor unit: only <anchorDir>/<agent>.json.
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
	// The leaf's guarded transitional sweep runs on a FRESH instance so its
	// unacked ledger matches the post-removal disk (the point of a unit reset).
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

func TestDrill_ManagedRootReset_ConsistentUnitAndAllRefusals(t *testing.T) {
	root := t.TempDir() // THE managed dir — the drill never touches real dirs
	storeDir := filepath.Join(root, "store")
	spillDir := filepath.Join(root, "spill")
	anchorDir := filepath.Join(root, "anchor")
	require.NoError(t, os.MkdirAll(storeDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(spillDir, "tagent"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(spillDir, "tagent", "inbox-v1"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(spillDir, "tagent", "inbox-v2", "quarantine"), 0o755))
	require.NoError(t, os.MkdirAll(anchorDir, 0o755))

	// --- seed: legacy transitional material -------------------------------
	legacyV1 := filepath.Join(spillDir, "tagent", "inbox-v1", "old-v1.json")
	require.NoError(t, os.WriteFile(legacyV1, []byte(`{"version":1}`), 0o644))
	legacySpill := filepath.Join(spillDir, "tagent", "job.spill")
	require.NoError(t, os.WriteFile(legacySpill, []byte("spill"), 0o644))
	// symlink-escape bait inside the legacy set: removing the LINK must never
	// touch its outside victim.
	victim := filepath.Join(root, "outside-victim.json")
	require.NoError(t, os.WriteFile(victim, []byte("DO-NOT-DELETE"), 0o644))
	require.NoError(t, os.Symlink(victim, filepath.Join(spillDir, "tagent", "inbox-v1", "escape.json")))

	// --- seed: CURRENT-format unit data (store facts + live envelope + anchor)
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

	// --- seed: UNMANAGED content + quarantine evidence (must survive) -----
	unmanaged := filepath.Join(storeDir, "user-notes.txt")
	require.NoError(t, os.WriteFile(unmanaged, []byte("mine"), 0o644))
	evidence := filepath.Join(spillDir, "tagent", "inbox-v2", "quarantine", "evidence-1.json")
	require.NoError(t, os.WriteFile(evidence, []byte(`{"corrupt":true}`), 0o644))

	// LEG b1 — confirm gate: refusal, zero changes.
	_, err = drillResetManagedUnits(storeDir, spillDir, anchorDir, "tagent", false)
	require.Error(t, err)
	require.FileExists(t, legacyV1)

	// LEG b2 — quarantine evidence blocks the reset until the operator
	// dispositions it: the CURRENT corruption is never swept as "legacy".
	_, err = drillResetManagedUnits(storeDir, spillDir, anchorDir, "tagent", true)
	require.Error(t, err, "undispositioned quarantine must refuse the unit reset")
	require.FileExists(t, evidence, "quarantine evidence files are NEVER deleted by the reset")
	require.FileExists(t, legacyV1, "refusal means ZERO changes")
	require.FileExists(t, filepath.Join(storeDir, "kv.json"), "refusal means ZERO changes")

	// Operator dispositions the evidence (moves it out — the human act).
	require.NoError(t, os.Rename(evidence, filepath.Join(root, "dispositioned-1.json")))

	// LEG c — live writer refuses: hold the store's flock, reset must refuse
	// with zero changes, then succeed once the writer leaves.
	held, err := acquireDirLock(storeDir)
	require.NoError(t, err)
	_, err = drillResetManagedUnits(storeDir, spillDir, anchorDir, "tagent", true)
	require.ErrorIs(t, err, ErrStoreLocked, "a live writer must be refused")
	require.FileExists(t, legacyV1)
	require.NoError(t, unlockDirLock(held)) // unlockDirLock closes the file

	// LEG d — the consistent unit reset itself.
	_, err = drillResetManagedUnits(storeDir, spillDir, anchorDir, "tagent", true)
	require.NoError(t, err)
	require.NoFileExists(t, legacyV1, "transitional v1 cleared")
	require.NoFileExists(t, legacySpill, "legacy spill cleared")
	require.NoFileExists(t, filepath.Join(storeDir, "kv.json"), "store unit cleared — consistency")
	require.NoFileExists(t, filepath.Join(anchorDir, "tagent.json"), "anchor unit cleared")
	liveLeft, _ := filepath.Glob(filepath.Join(spillDir, "tagent", "inbox-v2", "*.json"))
	require.Empty(t, liveLeft, "live envelopes cleared with the store (no unit half-reset)")
	// Survivors: unmanaged content, the symlink's victim, quarantine DIR.
	require.FileExists(t, unmanaged, "unmanaged content is never removed")
	require.FileExists(t, victim, "path escape removed at most the link, never the outside target")
	require.DirExists(t, filepath.Dir(evidence))

	// LEG e — post-reset boot ONLY knows the current path. Runs as an
	// independent process (boot evidence layer). §6.6: the acceptance keeps no
	// race exemption — the child must boot with zero data races and a clean
	// exit; any race or non-zero exit fails hard with the full log.
	runBootChild(t, append(os.Environ(),
		"TAGENT_DRILL_STORE="+storeDir,
		"TAGENT_DRILL_SPILL="+spillDir,
		"TAGENT_DRILL_ANCHOR="+anchorDir),
		"TAGENT_DRILL_PHASE=boot-turn", "TestDrill_ManagedRootResetBootChild$")
}

// TestDrill_ManagedRootReset boot-phase child: drives one post-reset turn.
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
					return true // Contains: the framework guard may decorate the input (§7.4 precedent)
				}
			}
		}
		return false
	}, 15*time.Second, 20*time.Millisecond, "post-reset boot must reach the model on the CURRENT path")
	require.NoError(t, ta.Close())
	<-drop
}

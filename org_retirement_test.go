package tagent

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/require"
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

// TestRetire_RemovedIdleOwnerIsRetired: 「移除不接新路由」＋「义务全部收敛后关闭独占
// 组件、释放 lease 并撤登记」. Nothing depends on s3, so the publish that drops it
// must also take it off the resident table, close it, and revoke its store-owner
// registration — while the still-routed s1 keeps its exact instance.
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

// TestRetire_ObligationHoldsOwnerThenRetiresAtNextBoundary is the safety half: an
// owner with a live execution is NOT retired by the publish that removed it, and is
// retired once that obligation disappears. Closing it early is exactly what §4.3
// forbids, so the hold must be observable, not silent.
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

// TestRetire_ReenteredNameReusesOriginalOwner: 「同名退役中重入必须复用原 owner」. A
// name that comes back while its owner is still draining must be served by THAT
// instance; a second owner for the same store would be a second writer.
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

// TestRetire_ReentryIntoClosingOwnerIsRefused covers D7's other clause: once the
// owner has begun closing, a candidate that wants that name is REFUSED before any
// resource is built — reusing it is impossible, admitting a fresh one would be a
// second writer for the same storage identity.
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

// TestRetire_ReentryAfterFinalExitRebuildsFreshOwner is §4.3's other side of the
// same name: 「同名尚未关则复用、关闭中拒绝、最终退出后按原恢复协议重建」. Once the
// removed owner has FINALLY exited — and since §4.3 makes the release itself carry
// the drain forward, that happens with no extra business turn — re-adding the name
// must admit a fresh owner through the recovery protocol instead of refusing it
// forever. Refusing a name whose previous owner is fully gone would be a leak of
// the refusal gate into the admission path.
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

// TestRetire_NameChurnStaysBounded is spec scenario「不同名字反复增删后资源收敛」:
// resources must converge to what the CURRENT routing plus real pending obligations
// need, never to the count of names that ever existed.
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

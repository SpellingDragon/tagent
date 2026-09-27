package tagent

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

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

package tagent

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/stretchr/testify/require"
)

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

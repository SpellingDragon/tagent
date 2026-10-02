package tagent

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// testStoreRoots maps a testing.TB to its store root, keyed by the TB pointer identity.
var testStoreRoots sync.Map

// testStore 把单元测试的 memory store 路径挪出仓库工作树，并按测试用例隔离。
//
// - 必须显式挪出：resources.acquire 在按 type 分派之前就 MkdirAll 并取写锁，type localfile 还另建 relations.journal。
// - 同一用例内同名 store 返回同一绝对路径；不同用例落在不同根，互不串存储。
// 契约: docs/wiki/agent/agent-architecture.md#test-support
func testStore(t testing.TB, name string) string {
	t.Helper()
	if root, ok := testStoreRoots.Load(t); ok {
		return filepath.Join(root.(string), name)
	}
	root := t.TempDir()
	testStoreRoots.Store(t, root)
	t.Cleanup(func() { testStoreRoots.Delete(t) })
	return filepath.Join(root, name)
}

// TestTestStore_IsolatesPerCase pins the isolation contract of the test-store helper.
// - Different cases never share a store root, while one case keeps a stable path for the same store name.
// - That stability is the identity premise for restart simulation and multi-generation rendering.
// - A root shared by PID plus a fixed name would land two cases in one directory where they see each other's bytes.
func TestTestStore_IsolatesPerCase(t *testing.T) {
	var aPath string
	t.Run("caseA_writes", func(t *testing.T) {
		aPath = testStore(t, "probe")
		require.NoError(t, os.MkdirAll(aPath, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(aPath, "marker"), []byte("A"), 0o644))
		require.Equal(t, aPath, testStore(t, "probe"),
			"the same case+name must resolve to the same store (restart / multi-generation identity)")
	})
	t.Run("caseB_mustNotSeeA", func(t *testing.T) {
		bPath := testStore(t, "probe")
		require.NotEqual(t, aPath, bPath, "sibling cases must not share a store root")
		_, err := os.Stat(filepath.Join(bPath, "marker"))
		require.Error(t, err, "case B must not observe case A's bytes")
	})
}

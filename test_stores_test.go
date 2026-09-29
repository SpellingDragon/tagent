package tagent

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

var testStoreRoots sync.Map // testing.TB -> string

// testStore 把单元测试的 memory store 路径挪出仓库工作树，并按**测试用例**隔离。
//
// 为什么必须显式挪：`resources.acquire` 在按 memory type 分派**之前**就无条件
// `os.MkdirAll(path)` 并取目录写锁（写 `.tagent-writer.lock`），`type: localfile`
// 还会另建 `relations.journal`。所以「用 type: memory 加一个逻辑路径」并不等于
// 不落盘——相对路径会在仓库根造出目录（`hottest-sub1/`、`own-sub1/` 里的 lock 与
// journal 已被历史提交跟踪，即是明证）。
//
// 根目录按**单个测试用例／benchmark**唯一，交由 `t.TempDir()`／`b.TempDir()` 管理，
// 用例结束自动回收：
//   - 同一用例内同名 store 返回同一绝对路径——热更多代渲染「sub2 存储段字节不变」与
//     重启模拟所依赖的身份前提由此成立；
//   - 不同用例（含 `-count` 重复，每次是全新 `*testing.T`）落在不同根，互不串存储；
//   - 不再按 PID+固定名字跨用例共享，也不像旧 PID 根那样在系统临时目录里堆积数百个
//     永不回收的 `tagent-test-stores-<pid>`。
//
// 无工作树兜底：`t.TempDir()` 建不出目录会直接把用例判失败，绝不退回相对路径——那会
// 重新引入本 helper 意在消除的仓库根 `hottest-*` / `own-*` 污染。
//
// 每用例只解析一次根：sync.Map 以 testing.TB（指针身份）为键；键被 map 强引用，故地址
// 不会被后续用例回收复用，用例结束即删除条目。所有 testStore 调用都在单个用例内顺序发生。
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

// TestTestStore_IsolatesPerCase 隔离合同：不同用例绝不共享 store 根；同一用例同名 store 保持稳定（重启模拟与
// 多代渲染的身份前提）。旧的 PID+固定名字根会让两个用例落到同一目录、互相看见字节，
// 此测先把该缺陷钉死再修复。
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

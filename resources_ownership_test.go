package tagent

import (
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
	"github.com/stretchr/testify/require"
)

// ownershipCfg builds a minimal single-agent config on a shared localfile
// store at dir.
func ownershipCfg(dir string) Config {
	return Config{
		Entry: "tagent",
		Agents: map[string]AgentConfig{
			"tagent": {
				SystemPrompt: PromptConfig{Inline: "ownership test"},
				Memory:       MemoryConfig{Type: "localfile", Path: dir},
			},
		},
	}
}

func storeFact(t *testing.T, store memory.MemoryStore, marker string) {
	t.Helper()
	pid := memory.PartitionIDFromName("tagent")
	key := memory.NewSnowflakeEventKey(pid, 0)
	require.NoError(t, store.StoreEvent(key, memory.FullEvent{
		EventKey: key, PartitionID: pid, EventType: "external_input",
		EventSummary: marker, Content: marker, Timestamp: 1700000000000,
	}))
}

func readBack(t *testing.T, dir, marker string) bool {
	t.Helper()
	// BYPASS the registry: open the directory fresh — the ground truth of
	// what is durable on disk.
	kvStore, err := kv.NewLocalFileKV(dir, kv.WithFSync(false))
	require.NoError(t, err)
	defer kvStore.Close()
	store, err := memory.NewFileSegmentStore(kvStore, nil, dir, 100)
	require.NoError(t, err)
	pid := memory.PartitionIDFromName("tagent")
	refs, qerr := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{pid}, Keyword: marker})
	require.NoError(t, qerr)
	return len(refs) > 0
}

// TestOwnership_SurvivorKeepsWorkingAfterSiblingClose（4.1 T1 / 4.2）：
// 两个根共享同一存储，其一 Close 后另一根必须继续可用且写入可持久——
// 「最后租约释放才关闭」，绝不是第一个 Close 就拆掉共享地基。
func TestOwnership_SurvivorKeepsWorkingAfterSiblingClose(t *testing.T) {
	dir := t.TempDir()
	ta1, err := New(ownershipCfg(dir), WithModel(&stubModel{name: "m"}))
	require.NoError(t, err)
	ta2, err := New(ownershipCfg(dir), WithModel(&stubModel{name: "m"}))
	require.NoError(t, err)

	storeFact(t, ta1.MemStore(), "before-close")
	require.NoError(t, ta1.Close()) // 根租约 1 释放

	// 幸存者继续工作：写入必须成功且可持久。
	storeFact(t, ta2.MemStore(), "after-close")
	require.NoError(t, ta2.Close()) // 最后租约 → 才真正关闭

	require.True(t, readBack(t, dir, "before-close"), "pre-close fact must survive")
	require.True(t, readBack(t, dir, "after-close"),
		"survivor's post-sibling-close write must be durable (shared store must NOT have been closed by the first Close)")
}

// TestOwnership_ReopenAfterLastClose（4.1 T2 / 4.2）：最后租约释放后，同进程
// 同路径再次 New 必须得到真正重开的新实例（读回持久化数据、后台生命周期全新），
// 而不是复用已关闭的旧实例。
func TestOwnership_ReopenAfterLastClose(t *testing.T) {
	dir := t.TempDir()
	ta1, err := New(ownershipCfg(dir), WithModel(&stubModel{name: "m"}))
	require.NoError(t, err)
	storeFact(t, ta1.MemStore(), "gen-1")
	require.NoError(t, ta1.Close()) // 最后（唯一）租约 → 关闭 + 移除登记

	ta2, err := New(ownershipCfg(dir), WithModel(&stubModel{name: "m"}))
	require.NoError(t, err)
	defer ta2.Close()
	storeFact(t, ta2.MemStore(), "gen-2")

	require.True(t, readBack(t, dir, "gen-1"))
	require.True(t, readBack(t, dir, "gen-2"),
		"post-reopen writes must be durable (the registry must hand out a LIVE reopened store, not the closed one)")
}

// TestOwnership_ConflictingConfigRejected（4.1 T3 / 4.2）：同物理路径的
// 不兼容配置必须显式拒绝——绝不创建第二套 writer，也绝不静默沿用首配。
func TestOwnership_ConflictingConfigRejected(t *testing.T) {
	dir := t.TempDir()
	ta1, err := New(ownershipCfg(dir), WithModel(&stubModel{name: "m"}))
	require.NoError(t, err)
	defer ta1.Close()

	conflict := ownershipCfg(dir)
	off := false
	a := conflict.Agents["tagent"]
	a.Memory.FSync = &off // 与默认（true）冲突
	conflict.Agents["tagent"] = a
	_, err = New(conflict, WithModel(&stubModel{name: "m"}))
	require.Error(t, err, "incompatible config on the same path must be rejected")
	require.Contains(t, err.Error(), "conflict")
}

// TestOwnership_MidBuildFailureReleasesLease（4.1 T4）：构建后续步骤失败时
// 已取得的租约必须被逆序释放（不泄漏 goroutine/句柄/登记项）。
func TestOwnership_MidBuildFailureReleasesLease(t *testing.T) {
	dir := t.TempDir()
	cfg := ownershipCfg(dir)
	a := cfg.Agents["tagent"]
	a.Tools = []ToolRef{{Kind: "tool", ID: "no_such_tool_def"}}
	cfg.Agents["tagent"] = a
	_, err := New(cfg, WithModel(&stubModel{name: "m"}))
	require.Error(t, err)

	// 租约已释放 → 再次 New 正常（不因泄漏的登记而冲突/锁死）。
	ta2, err := New(ownershipCfg(dir), WithModel(&stubModel{name: "m"}))
	require.NoError(t, err)
	require.NoError(t, ta2.Close())
	require.True(t, readBack(t, dir, "never-written") == false)
}

// TestOwnership_ConcurrentAcquireSamePath（cold-eyes R2 M-2 回归）：双
// goroutine 同路径并发 acquire——per-key opening mutex 必须串行化 open；
// 全部 acquire 完成后（barrier）各持有者必须是同一 store 实例（禁止双开
// 双实例），全部 release 后可正常重开。
func TestOwnership_ConcurrentAcquireSamePath(t *testing.T) {
	dir := t.TempDir()
	rr := NewRuntimeResources()

	const n = 8
	stores := make([]memory.MemoryStore, n)
	releases := make([]func(), n)
	var startWG, doneWG sync.WaitGroup
	startWG.Add(n)
	for i := 0; i < n; i++ {
		wgDone := false
		_ = wgDone
		doneWG.Add(1)
		go func(i int) {
			defer doneWG.Done()
			store, release, err := rr.acquire("localfile", dir, fingerprintMemory(MemoryConfig{Type: "inmemory", Path: dir}), func() (memory.MemoryStore, error) {
				return memory.NewInMemoryStore(), nil
			})
			if err != nil {
				t.Errorf("acquire %d: %v", i, err)
				startWG.Done()
				return
			}
			stores[i], releases[i] = store, release
			startWG.Done()
		}(i)
	}
	startWG.Wait()
	for i := 1; i < n; i++ {
		if stores[i] == nil || stores[0] == nil {
			t.Fatalf("concurrent acquire produced nil stores")
		}
		if stores[i] != stores[0] {
			t.Fatalf("same path under concurrent acquire produced distinct instances (%d differ)", i)
		}
	}
	for i := 0; i < n; i++ {
		releases[i]()
	}
	// Full release closes the entry — a subsequent acquire must reopen cleanly.
	store2, release2, err := rr.acquire("localfile", dir, fingerprintMemory(MemoryConfig{Type: "inmemory", Path: dir}), func() (memory.MemoryStore, error) {
		return memory.NewInMemoryStore(), nil
	})
	if err != nil {
		t.Fatalf("reopen after concurrent release: %v", err)
	}
	release2()
	_ = store2
}

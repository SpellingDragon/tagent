package tagent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

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
// 用真实行为轴（engine）作冲突源——fsync 已裁决为零行为差异轴（下方负向锁）。
func TestOwnership_ConflictingConfigRejected(t *testing.T) {
	dir := t.TempDir()
	ta1, err := New(ownershipCfg(dir), WithModel(&stubModel{name: "m"}))
	require.NoError(t, err)
	defer ta1.Close()

	conflict := ownershipCfg(dir)
	a := conflict.Agents["tagent"]
	a.Memory.Engine = &MemoryEngineConfig{Embedding: &EmbeddingConfig{Provider: "mock", Dimensions: 64}} // real behavioral axis
	conflict.Agents["tagent"] = a
	_, err = New(conflict, WithModel(&stubModel{name: "m"}))
	require.Error(t, err, "incompatible BEHAVIORAL config on the same path must be rejected")
	require.Contains(t, err.Error(), "conflict")
}

// TestOwnership_FSyncAxisSharesNotConflicts（resident-review-fixes 3.1 / spec
// 「零行为差异轴不制造假冲突」）：localfile 的 fsync 已被后端裁决为
// accepted-and-ignored（两值行为恒同），因此 MUST NOT 制造共享假冲突。仅 fsync
// 不同的两份配置共享同一活动实例——第二次装载被接受（而非拒绝），且两个
// handle 写入互可见（同一 store，非重建）。fail-before：fsync 仍在指纹中时
// 第二次 New 会因冲突报错。
func TestOwnership_FSyncAxisSharesNotConflicts(t *testing.T) {
	dir := t.TempDir()
	base := ownershipCfg(dir) // fsync default (nil → backend default)
	ta1, err := New(base, WithModel(&stubModel{name: "m"}))
	require.NoError(t, err)
	defer ta1.Close()

	other := ownershipCfg(dir)
	off := false
	a := other.Agents["tagent"]
	a.Memory.FSync = &off
	other.Agents["tagent"] = a

	// Zero-behavior-difference axis must not join the fingerprint.
	require.Equal(t,
		fingerprintMemory(base.Agents["tagent"].Memory),
		fingerprintMemory(other.Agents["tagent"].Memory),
		"fsync-only configs must produce identical fingerprints")

	ta2, err := New(other, WithModel(&stubModel{name: "m"}))
	require.NoError(t, err, "fsync-only difference must share the instance, not reject (false conflict)")
	defer ta2.Close()

	// Same live instance: a write via ta2 reads back through ta1.
	storeFact(t, ta2.MemStore(), "fsync-shared")
	pid := memory.PartitionIDFromName("tagent")
	refs, qerr := ta1.MemStore().QueryEvents(memory.QueryOptions{PartitionIDs: []int{pid}, Keyword: "fsync-shared"})
	require.NoError(t, qerr)
	require.NotEmpty(t, refs, "the two handles share ONE live store (no rebuild on the fsync axis)")
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
	releases := make([]func() error, n)
	var startWG, doneWG sync.WaitGroup
	startWG.Add(n)
	for i := 0; i < n; i++ {
		wgDone := false
		_ = wgDone
		doneWG.Add(1)
		go func(i int) {
			defer doneWG.Done()
			store, _, release, err := rr.acquire("localfile", dir, fingerprintMemory(MemoryConfig{Type: "inmemory", Path: dir}), func() (openedResource, error) {
				return openedResource{store: memory.NewInMemoryStore()}, nil
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
	store2, _, release2, err := rr.acquire("localfile", dir, fingerprintMemory(MemoryConfig{Type: "inmemory", Path: dir}), func() (openedResource, error) {
		return openedResource{store: memory.NewInMemoryStore()}, nil
	})
	if err != nil {
		t.Fatalf("reopen after concurrent release: %v", err)
	}
	release2()
	_ = store2
}

// F6: a release closure must be idempotent — calling it twice from the same
// consumer (e.g. a second agent.Close()) must NOT over-decrement the lease
// count of the entry and must NOT affect a different generation at the same path.
func TestLeaseRelease_IdempotentDoesNotHarmSurvivor(t *testing.T) {
	dir := t.TempDir()
	rr := NewRuntimeResources()
	fp := fingerprintMemory(MemoryConfig{Type: "localfile", Path: dir})
	openFn := func() (openedResource, error) {
		k, err := kv.NewLocalFileKV(dir, kv.WithFSync(false))
		if err != nil {
			return openedResource{}, err
		}
		s, err := memory.NewFileSegmentStore(k, nil, dir, 100)
		return openedResource{store: s}, err
	}
	// Two acquirers share the same entry (two leases).
	storeA, _, releaseA, err := rr.acquire("localfile", dir, fp, openFn)
	require.NoError(t, err)
	storeB, _, releaseB, err := rr.acquire("localfile", dir, fp, openFn)
	require.NoError(t, err)
	require.Same(t, storeA, storeB, "same path must share one instance")

	// A releases TWICE (simulates two agent.Close() calls): must be idempotent.
	releaseA()
	releaseA() // second call: must NOT decrement B's lease

	// B's store must still be writable (entry is not closed yet).
	pid := memory.PartitionIDFromName("test")
	key := memory.NewSnowflakeEventKey(pid, 0)
	require.NoError(t, storeB.StoreEvent(key, memory.FullEvent{
		EventKey: key, PartitionID: pid, EventType: "external_input",
		Content: "after-A-double-close", Timestamp: 1700000000000,
	}), "F6: survivor store must remain writable after sibling double-Close")

	// Now B releases: entry should close cleanly (lease count reaches zero).
	releaseB()
}

// F6: a stale release closure must NOT decrement a NEW generation of an entry
// that reused the same path.
func TestLeaseRelease_StaleDoesNotAffectNewGeneration(t *testing.T) {
	dir := t.TempDir()
	rr := NewRuntimeResources()
	fp := fingerprintMemory(MemoryConfig{Type: "localfile", Path: dir})
	openFn := func() (openedResource, error) {
		k, err := kv.NewLocalFileKV(dir, kv.WithFSync(false))
		if err != nil {
			return openedResource{}, err
		}
		s, err := memory.NewFileSegmentStore(k, nil, dir, 100)
		return openedResource{store: s}, err
	}
	// First entry: acquire and release (entry is closed, generation is gone).
	_, _, staleRelease, err := rr.acquire("localfile", dir, fp, openFn)
	require.NoError(t, err)
	staleRelease() // release → entry closed

	// New entry at same path: acquire fresh (new generation).
	newStore, _, newRelease, err := rr.acquire("localfile", dir, fp, openFn)
	require.NoError(t, err)

	// Stale release from the old generation must NOT affect the new entry.
	staleRelease() // already released once; second call is a no-op via Once

	// newStore must still be usable (not closed by the stale release).
	pid := memory.PartitionIDFromName("stale")
	key := memory.NewSnowflakeEventKey(pid, 0)
	require.NoError(t, newStore.StoreEvent(key, memory.FullEvent{
		EventKey: key, PartitionID: pid, EventType: "external_input",
		Content: "after-stale", Timestamp: 1700000000000,
	}), "F6: stale release must not close the new generation entry")

	newRelease()
}

// TestEngineOwnership_SharedReopenGetsFreshEngine 锁定 F7/D5 核心不变量：共享
// engine 随 registry entry 同代拥有——同路径消费者共用同一活引擎实例；单个消费者
// 释放绝不拆掉存活者所用的共享引擎；最后释放关闭引擎（回收后台 worker，此即
// namedEngines 造成的泄漏）；reopen（新代）必须拿到绑定新 backend 的全新引擎，
// 绝不复用陈旧的已关引擎。用 type:memory + mock 引擎（无 KV 重建）使 Ready()
// 只反映关闭态，关闭判定确定化。
func TestEngineOwnership_SharedReopenGetsFreshEngine(t *testing.T) {
	dir := t.TempDir()
	rr := NewRuntimeResources()
	mc := MemoryConfig{
		Type: "memory", Path: dir,
		Engine: &MemoryEngineConfig{Embedding: &EmbeddingConfig{Provider: "mock", Dimensions: 32}},
	}
	fp := fingerprintMemory(mc)
	openFn := func() (openedResource, error) {
		s := memory.NewInMemoryStore()
		return openedResource{store: s, engine: buildSharedEngine(s, mc)}, nil
	}

	store1, eng1, rel1, err := rr.acquire("mem", dir, fp, openFn)
	require.NoError(t, err)
	require.NotNil(t, eng1, "engine-configured shared path must have an entry-owned engine")
	require.True(t, eng1.Ready(), "freshly built engine must be live")

	// Second consumer on the same path borrows the SAME live engine instance.
	_, eng2, rel2, err := rr.acquire("mem", dir, fp, openFn)
	require.NoError(t, err)
	require.Same(t, eng1, eng2, "one generation shares exactly one engine instance")

	// A single consumer releasing must NOT close the shared engine: the survivor
	// store keeps working through it.
	rel1()
	require.True(t, eng1.Ready(), "sibling release must not close the shared engine")
	require.NoError(t, store1.StoreEvent(
		memory.NewSnowflakeEventKey(1, 0),
		memory.FullEvent{EventKey: memory.NewSnowflakeEventKey(1, 0), PartitionID: 1,
			EventType: "external_input", Content: "survivor", Timestamp: 1700000000000}))

	// The LAST release closes the engine (reclaims its background worker).
	rel2()
	require.False(t, eng1.Ready(), "F7: last lease release must close the shared engine")

	// Reopen (new generation) must build a FRESH engine, never reuse the stale one.
	_, eng3, rel3, err := rr.acquire("mem", dir, fp, openFn)
	require.NoError(t, err)
	require.NotNil(t, eng3)
	require.NotSame(t, eng1, eng3, "F7/D5: reopen must get a fresh engine bound to a fresh backend")
	require.True(t, eng3.Ready())
	rel3()
}

// --- D5 close-order + error-reachability test doubles (task 6.4) ---

// seqStore wraps a memory.MemoryStore, records the teardown sequence, lets the
// test force Close to fail, and exposes StopProducers so closeResource stops
// the forgetting producers BEFORE the engine worker (D5 close order).
type seqStore struct {
	memory.MemoryStore
	seq *[]string
	err error
}

func (s *seqStore) StopProducers() { *s.seq = append(*s.seq, "producers") }
func (s *seqStore) Close() error {
	*s.seq = append(*s.seq, "store")
	return s.err
}

// seqEngine wraps a memory.MemoryEngine; Close records the step and can fail.
// Only Close is exercised (closeResource never calls the promoted methods).
type seqEngine struct {
	memory.MemoryEngine
	seq *[]string
	err error
}

func (e *seqEngine) Close() error {
	*e.seq = append(*e.seq, "engine")
	return e.err
}

// TestCloseOrder_ProducersEngineBackendAndErrorReach locks the D5 close order
// (producers → engine → backend flush) and its error contract: a close error
// must reach the release caller (never a silent "safe close"), an UNCONFIRMED
// engine worker stop holds the writer lock so a reopen is refused (never two
// writers), and a confirmed stop releases the lock for a genuine reopen.
func TestCloseOrder_ProducersEngineBackendAndErrorReach(t *testing.T) {
	t.Run("engine error reaches release and holds lock", func(t *testing.T) {
		dir := t.TempDir()
		rr := NewRuntimeResources()
		var seq []string
		engErr := errors.New("engine worker stuck")
		fp := fingerprintMemory(MemoryConfig{Type: "localfile", Path: dir})
		openFn := func() (openedResource, error) {
			s := &seqStore{MemoryStore: memory.NewInMemoryStore(), seq: &seq}
			e := &seqEngine{seq: &seq, err: engErr}
			return openedResource{store: s, engine: e}, nil
		}
		_, _, rel, err := rr.acquire("localfile", dir, fp, openFn)
		require.NoError(t, err)
		rerr := rel()
		require.ErrorIs(t, rerr, engErr, "engine close error must reach the release caller")
		require.Equal(t, []string{"producers", "engine"}, seq,
			"backend must NOT be flushed after an unconfirmed engine stop (stale worker may still write)")
		_, _, _, err2 := rr.acquire("localfile", dir, fp, openFn)
		require.ErrorIs(t, err2, ErrResourcePoisoned, "unconfirmed worker stop must SEAL the path via an explicit poisoned entry (§6.4), not a silent fd leak")
	})

	t.Run("store error reaches release and frees lock", func(t *testing.T) {
		dir := t.TempDir()
		rr := NewRuntimeResources()
		var seq []string
		storeErr := errors.New("backend flush failed")
		fp := fingerprintMemory(MemoryConfig{Type: "localfile", Path: dir})
		openFn := func() (openedResource, error) {
			s := &seqStore{MemoryStore: memory.NewInMemoryStore(), seq: &seq, err: storeErr}
			e := &seqEngine{seq: &seq}
			return openedResource{store: s, engine: e}, nil
		}
		_, _, rel, err := rr.acquire("localfile", dir, fp, openFn)
		require.NoError(t, err)
		rerr := rel()
		require.ErrorIs(t, rerr, storeErr, "store close error must reach the release caller")
		require.Equal(t, []string{"producers", "engine", "store"}, seq,
			"D5 close order: producers → engine → backend flush")
		// Confirmed engine stop → lock released → genuine reopen succeeds.
		_, _, rel2, err2 := rr.acquire("localfile", dir, fp, openFn)
		require.NoError(t, err2, "confirmed stop must release the writer lock for reopen")
		_ = rel2() // same mock store still errors on Close — cleanup only, lock already proven free
	})
}

// blockCloseStore wraps a memory.MemoryStore whose Close blocks until unblocked,
// simulating a slow backend flush so the test can observe whether a same-path
// reopen wrongly races ahead of (or deadlocks against) the in-progress close.
type blockCloseStore struct {
	memory.MemoryStore
	entered chan struct{} // receives once when Close begins
	unblock chan struct{} // Close returns after this is signalled
}

func (s *blockCloseStore) Close() error {
	s.entered <- struct{}{}
	<-s.unblock
	return nil
}

// TestReleaseCoordination_SamePathReopenWaitsForClose locks D5 line 115: a
// same-path acquire and the final release share ONE per-key mutex, so a reopen
// WAITS for the previous generation to finish closing (rather than mis-reading
// the freed registry slot against a still-held flock and returning
// ErrStoreLocked), while an unrelated path is never blocked behind the slow
// flush (the registry global lock is not held across I/O).
func TestReleaseCoordination_SamePathReopenWaitsForClose(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	rr := NewRuntimeResources()
	fp := fingerprintMemory(MemoryConfig{Type: "localfile", Path: dir})

	entered := make(chan struct{}, 1)
	unblock := make(chan struct{})
	bs := &blockCloseStore{MemoryStore: memory.NewInMemoryStore(), entered: entered, unblock: unblock}
	_, _, rel, err := rr.acquire("localfile", dir, fp, func() (openedResource, error) {
		return openedResource{store: bs}, nil
	})
	require.NoError(t, err)

	// Last-lease release runs a slow close (blocks inside store.Close, flock held).
	relDone := make(chan error, 1)
	go func() { relDone <- rel() }()
	<-bs.entered // close is now in progress

	// Same-path reopen must block on the per-key mutex — not return ErrStoreLocked.
	type outcome struct {
		store memory.MemoryStore
		rel   func() error
		err   error
	}
	reopen := make(chan outcome, 1)
	go func() {
		s, _, r2, e := rr.acquire("localfile", dir, fp, func() (openedResource, error) {
			return openedResource{store: memory.NewInMemoryStore()}, nil
		})
		reopen <- outcome{store: s, rel: r2, err: e}
	}()

	// An unrelated path must complete during the slow flush (proves the global
	// registry lock is not held across I/O).
	dfp := fingerprintMemory(MemoryConfig{Type: "localfile", Path: other})
	otherDone := make(chan error, 1)
	go func() {
		_, _, r3, e := rr.acquire("localfile", other, dfp, func() (openedResource, error) {
			return openedResource{store: memory.NewInMemoryStore()}, nil
		})
		if e == nil {
			_ = r3()
		}
		otherDone <- e
	}()
	select {
	case e := <-otherDone:
		require.NoError(t, e, "unrelated path must not block behind a slow close")
	case <-time.After(3 * time.Second):
		t.Fatal("unrelated path blocked behind slow close — registry global lock held across I/O")
	}

	// The reopen must still be waiting (the old generation has not finished).
	select {
	case got := <-reopen:
		t.Fatalf("reopen proceeded before close finished (err=%v) — per-key coordination missing", got.err)
	case <-time.After(150 * time.Millisecond):
	}

	// Let the close finish; the reopen must then SUCCEED (fresh instance), not
	// ErrStoreLocked.
	bs.unblock <- struct{}{}
	require.NoError(t, <-relDone)

	select {
	case got := <-reopen:
		require.NoError(t, got.err, "D5: reopen after the old generation closed must succeed, not ErrStoreLocked")
		require.NotNil(t, got.store)
		_ = got.rel()
	case <-time.After(3 * time.Second):
		t.Fatal("reopen never proceeded after the close finished")
	}
}

// TestEngineOwnership_SharedBuildFailureDegradesToCapacityOnly locks the D5
// degraded path on a SHARED store: an embedding provider that fails to build
// must leave the entry-owned engine nil (never a dangling engine cached), still
// publish + close the backend as one generation, and let each borrowing agent
// keep an INDEPENDENT capacity hook with keyword-only retrieval (8.10). A later
// reopen (new generation) must get a fresh backend store AND, once the provider
// works, a real live engine — proving the stale generation is fully isolated.
func TestEngineOwnership_SharedBuildFailureDegradesToCapacityOnly(t *testing.T) {
	dir := t.TempDir()
	rr := NewRuntimeResources()
	bad := MemoryConfig{
		Type: "memory", Path: dir,
		Engine: &MemoryEngineConfig{Embedding: &EmbeddingConfig{Provider: "no-such-provider", Dimensions: 8}},
	}
	openBad := func() (openedResource, error) {
		s := memory.NewInMemoryStore()
		return openedResource{store: s, engine: buildSharedEngine(s, bad)}, nil
	}
	store1, eng1, rel1, err := rr.acquire("mem", dir, fingerprintMemory(bad), openBad)
	require.NoError(t, err)
	require.Nil(t, eng1, "failing embedding provider must degrade the entry engine to nil (no dangling engine)")

	// The degraded shared store still gives each consumer an INDEPENDENT
	// capacity hook and keeps keyword-only retrieval — the borrow bridge carries
	// NO engine (capacity-only wrapper), so no vector indexing happens.
	var hooked int
	borrow1, berr := wireMemoryEngine(store1, eng1 /*nil*/, bad, func(int64, int, string) { hooked++ })
	require.NoError(t, berr)
	if ep, ok := borrow1.(memory.MemoryEngineProvider); ok {
		require.Nil(t, ep.MemoryEngine(), "degraded shared borrow must be capacity-only (nil engine)")
	}
	key := memory.NewSnowflakeEventKey(1, 0)
	require.NoError(t, borrow1.StoreEvent(key, memory.FullEvent{
		EventKey: key, PartitionID: 1, EventType: "external_input", Content: "deg", Timestamp: 1700000000000}))
	require.Equal(t, 1, hooked, "capacity hook must still fire on the degraded capacity-only borrow path")

	require.NoError(t, rel1(), "a degraded entry (engine nil) must still close cleanly")

	// Reopen (new generation, working provider) → fresh isolated store + live engine.
	good := MemoryConfig{
		Type: "memory", Path: dir,
		Engine: &MemoryEngineConfig{Embedding: &EmbeddingConfig{Provider: "mock", Dimensions: 8}},
	}
	store2, eng2, rel2, err := rr.acquire("mem", dir, fingerprintMemory(good), func() (openedResource, error) {
		s := memory.NewInMemoryStore()
		return openedResource{store: s, engine: buildSharedEngine(s, good)}, nil
	})
	require.NoError(t, err)
	require.NotSame(t, store1, store2, "reopen must get a fresh backend store (old generation isolated)")
	require.NotNil(t, eng2, "reopen with a valid provider must build a real engine")
	require.True(t, eng2.Ready())
	require.NoError(t, rel2())
}

// §6.5 — construction failure reclamation: every resource obtained during a
// build is released in reverse order, AND a reclaim that cannot be CONFIRMED
// must not leave the writer open (the path seals like an unconfirmed worker
// stop), while a cleanly-released failed build stays freely retryable.

// TestBuildFailure_CleanReclaimStaysRetryable: an open() failure whose
// partially built resources were reclaimed successfully releases the writer
// lock — the next acquire at the same path must be able to open fresh.
func TestBuildFailure_CleanReclaimStaysRetryable(t *testing.T) {
	dir := t.TempDir()
	rr := NewRuntimeResources()
	fp := fingerprintMemory(MemoryConfig{Type: "localfile", Path: dir})
	buildErr := errors.New("kv died at startup")

	_, _, _, err := rr.acquire("localfile", dir, fp, func() (openedResource, error) {
		return openedResource{}, buildErr
	})
	require.ErrorIs(t, err, buildErr)

	// Lock freed → reopen succeeds.
	_, _, rel, err2 := rr.acquire("localfile", dir, fp, func() (openedResource, error) {
		return openedResource{store: &seqStore{MemoryStore: nil, seq: new([]string)}}, nil
	})
	require.NoError(t, err2, "a cleanly reclaimed failed build must not seal the path")
	require.NoError(t, rel())
}

// TestBuildFailure_UnconfirmedReclaimSealsWriter: when the release of a
// partially built resource itself fails (ErrReclaimUnconfirmed), the writer
// lock is HELD and the path is sealed with a poisoned entry carrying the
// cause — a new generation must never open alongside a possibly half-live
// backend (design 决策7: 无法安全回收同样保持 poisoned).
func TestBuildFailure_UnconfirmedReclaimSealsWriter(t *testing.T) {
	dir := t.TempDir()
	rr := NewRuntimeResources()
	fp := fingerprintMemory(MemoryConfig{Type: "localfile", Path: dir})
	buildErr := errors.New("segment store init failed")
	closeErr := errors.New("kv close hung")

	_, _, _, err := rr.acquire("localfile", dir, fp, func() (openedResource, error) {
		return openedResource{}, fmt.Errorf("%w; %w", buildErr,
			fmt.Errorf("%w: kv close: %v", ErrReclaimUnconfirmed, closeErr))
	})
	require.ErrorIs(t, err, ErrReclaimUnconfirmed, "the original build error still reaches the caller")

	key := resourceKey{kind: "localfile", path: canonicalize(dir)}
	rr.mu.Lock()
	e := rr.entries[key]
	rr.mu.Unlock()
	require.NotNil(t, e, "unconfirmed reclaim must seal via an explicit poisoned entry")
	require.True(t, e.poisoned)

	_, _, _, err2 := rr.acquire("localfile", dir, fp, func() (openedResource, error) {
		t.Fatal("open must NOT run again on a sealed path")
		return openedResource{}, nil
	})
	require.ErrorIs(t, err2, ErrResourcePoisoned)

	// The writer lock stayed held — the seal is real, not bookkeeping only.
	probe, perr := os.OpenFile(filepath.Join(canonicalize(dir), ".tagent-writer.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	require.NoError(t, perr)
	defer probe.Close()
	require.Error(t, flockExclusive(probe), "an unconfirmed reclaim must keep holding the writer lock")
}

// TestWriterLock_ExclusiveAcrossHandles（4.3）：同目录的单 writer flock 互斥——
// 第二个持有者（另一进程或另一 fd）非阻塞抢锁必须失败；释放后可重取。
// 进程崩溃由 OS 自动释放 flock，不存在遗留锁永久锁死（delta spec「单 writer」）。
func TestWriterLock_ExclusiveAcrossHandles(t *testing.T) {
	dir := t.TempDir()

	f1, err := acquireDirLock(dir)
	require.NoError(t, err)

	_, err = acquireDirLock(dir) // second handle, same dir
	require.True(t, errors.Is(err, ErrStoreLocked), "second writer must be rejected, got: %v", err)

	require.NoError(t, unlockDirLock(f1))

	f2, err := acquireDirLock(dir)
	require.NoError(t, err, "after release the lock is re-acquirable")
	require.NoError(t, unlockDirLock(f2))
}

// TestPoisoned_ExplicitEntrySealsPathAcrossGC — §6.4 (spec scenario「关闭失败
// 后的垃圾回收」+ design 决策7): when the final release cannot confirm the
// engine worker stopped, the registry keeps an EXPLICIT poisoned entry —
// strong references to store/engine/lockfile plus the failure result — and
// every same-path acquire fails with that recorded error, while unrelated
// paths keep working. Holding the writer lock must never depend on an
// unreachable fd that "GC happens not to reclaim".
func TestPoisoned_ExplicitEntrySealsPathAcrossGC(t *testing.T) {
	dir := t.TempDir()
	rr := NewRuntimeResources()
	var seq []string
	engErr := errors.New("engine worker stuck")
	fp := fingerprintMemory(MemoryConfig{Type: "localfile", Path: dir})

	var sealed *seqStore
	openFn := func() (openedResource, error) {
		s := &seqStore{MemoryStore: memory.NewInMemoryStore(), seq: &seq}
		sealed = s
		return openedResource{store: s, engine: &seqEngine{seq: &seq, err: engErr}}, nil
	}
	_, _, rel, err := rr.acquire("localfile", dir, fp, openFn)
	require.NoError(t, err)

	require.ErrorIs(t, rel(), engErr, "the close failure must reach the releasing caller")

	// The entry survives explicitly — poisoned, holding the ORIGINAL store
	// and the live lockfile, with the failure recorded.
	key := resourceKey{kind: "localfile", path: canonicalize(dir)}
	rr.mu.Lock()
	e := rr.entries[key]
	rr.mu.Unlock()
	require.NotNil(t, e, "§6.4: the poisoned entry must be RETAINED, not detached + silently leaked")
	require.True(t, e.poisoned)
	require.Same(t, memory.MemoryStore(sealed), e.store, "entry keeps the strong store reference")
	require.NotNil(t, e.lockFile, "entry keeps the lockfile reference")
	require.ErrorIs(t, e.closeErr, engErr)

	// GC must not weaken the seal in any way.
	runtime.GC()
	runtime.GC()

	_, _, _, err2 := rr.acquire("localfile", dir, fp, openFn)
	require.ErrorIs(t, err2, ErrResourcePoisoned, "same-path acquire must fail EXPLICITLY (poisoned), not via a flock race or a resurrected generation")
	require.ErrorContains(t, err2, engErr.Error(), "the sealing error carries the recorded failure to the caller")

	// A different fingerprint on the same sealed path still reports poisoned
	// (the seal outranks conflict bookkeeping).
	_, _, _, err3 := rr.acquire("localfile", dir, "v1|other|fp", openFn)
	require.ErrorIs(t, err3, ErrResourcePoisoned)

	// Unrelated paths are unaffected.
	other := t.TempDir()
	fpOther := fingerprintMemory(MemoryConfig{Type: "localfile", Path: other})
	_, _, relOther, err4 := rr.acquire("localfile", other, fpOther, func() (openedResource, error) {
		s := &seqStore{MemoryStore: memory.NewInMemoryStore(), seq: &seq}
		return openedResource{store: s}, nil
	})
	require.NoError(t, err4, "poisoning one path must not seal the registry")
	require.NoError(t, relOther())

	// The flock is still held after GC — probe from a fresh handle (in-process
	// flocks on distinct open file descriptions conflict, cf. TestWriterLock).
	probe, perr := os.OpenFile(filepath.Join(canonicalize(dir), ".tagent-writer.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	require.NoError(t, perr)
	defer probe.Close()
	require.Error(t, flockExclusive(probe), "the poisoned entry must still hold the writer lock after GC")
}

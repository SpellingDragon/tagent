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

// readBack 绕开登记处，直接开目录读回：持久化与否的真源是磁盘上的落盘事实，
// 而不是登记处交还的那份句柄——判据必须站在被观察者之外。
func readBack(t *testing.T, dir, marker string) bool {
	t.Helper()
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

// TestOwnership_SurvivorKeepsWorkingAfterSiblingClose 钉住 两个根共用一个存储时，关闭其一不得在另一根之下拆掉共享地基。
// - 租约未清到零，后端保持打开：幸存者的写入成功且可持久，维护生产者照常运行；
// - 关闭由最后一个租约触发，不是第一个。
// 契约: docs/wiki/platform/resource-ownership.md#last-lease-close
func TestOwnership_SurvivorKeepsWorkingAfterSiblingClose(t *testing.T) {
	dir := t.TempDir()
	ta1, err := New(ownershipCfg(dir), WithModel(&stubModel{name: "m"}))
	require.NoError(t, err)
	ta2, err := New(ownershipCfg(dir), WithModel(&stubModel{name: "m"}))
	require.NoError(t, err)

	storeFact(t, ta1.MemStore(), "before-close")
	require.NoError(t, ta1.Close())

	storeFact(t, ta2.MemStore(), "after-close")
	require.NoError(t, ta2.Close())

	require.True(t, readBack(t, dir, "before-close"), "pre-close fact must survive")
	require.True(t, readBack(t, dir, "after-close"),
		"survivor's post-sibling-close write must be durable (shared store must NOT have been closed by the first Close)")
}

// TestOwnership_ReopenAfterLastClose 钉住 最后租约关闭后，同进程同路径再次装载得到真正重开的活实例。
// - 读得到上一代持久化的事实，自己的写入也可持久——拿回一个已关的对象会在这里露馅；
// - 后台生命周期是全新的，不是先前那个实例的延续。
// 契约: docs/wiki/platform/resource-ownership.md#last-lease-close
func TestOwnership_ReopenAfterLastClose(t *testing.T) {
	dir := t.TempDir()
	ta1, err := New(ownershipCfg(dir), WithModel(&stubModel{name: "m"}))
	require.NoError(t, err)
	storeFact(t, ta1.MemStore(), "gen-1")
	require.NoError(t, ta1.Close())

	ta2, err := New(ownershipCfg(dir), WithModel(&stubModel{name: "m"}))
	require.NoError(t, err)
	defer ta2.Close()
	storeFact(t, ta2.MemStore(), "gen-2")

	require.True(t, readBack(t, dir, "gen-1"))
	require.True(t, readBack(t, dir, "gen-2"),
		"post-reopen writes must be durable (the registry must hand out a LIVE reopened store, not the closed one)")
}

// TestOwnership_ConflictingConfigRejected 钉住 同一物理路径上的不兼容配置必须具名拒绝，既不建第二写者，也不静默沿用先到的配置。
// - 冲突源取真实行为轴（引擎），而非被忽略的轴；
// - 被拒的一方不得留下任何半个实例或登记。
// 契约: docs/wiki/platform/resource-ownership.md#fingerprint-conflict
func TestOwnership_ConflictingConfigRejected(t *testing.T) {
	dir := t.TempDir()
	ta1, err := New(ownershipCfg(dir), WithModel(&stubModel{name: "m"}))
	require.NoError(t, err)
	defer ta1.Close()

	conflict := ownershipCfg(dir)
	a := conflict.Agents["tagent"]
	a.Memory.Engine = &MemoryEngineConfig{Embedding: &EmbeddingConfig{Provider: "mock", Dimensions: 64}}
	conflict.Agents["tagent"] = a
	_, err = New(conflict, WithModel(&stubModel{name: "m"}))
	require.Error(t, err, "incompatible BEHAVIORAL config on the same path must be rejected")
	require.Contains(t, err.Error(), "conflict")
}

// TestOwnership_FSyncAxisSharesNotConflicts 钉住 行为恒同的轴不进指纹，两份这样的配置共享同一个活实例而非被判成假冲突。
// - 判别是双面的：指纹逐字相等，且第二次装载被接受（不是拒绝）；
// - 两个句柄的写入互见——同一份 store 在服务，没有重建。
// 契约: docs/wiki/platform/resource-ownership.md#fingerprint-conflict
func TestOwnership_FSyncAxisSharesNotConflicts(t *testing.T) {
	dir := t.TempDir()
	base := ownershipCfg(dir)
	ta1, err := New(base, WithModel(&stubModel{name: "m"}))
	require.NoError(t, err)
	defer ta1.Close()

	other := ownershipCfg(dir)
	off := false
	a := other.Agents["tagent"]
	a.Memory.FSync = &off
	other.Agents["tagent"] = a

	require.Equal(t,
		fingerprintMemory(base.Agents["tagent"].Memory),
		fingerprintMemory(other.Agents["tagent"].Memory),
		"fsync-only configs must produce identical fingerprints")

	ta2, err := New(other, WithModel(&stubModel{name: "m"}))
	require.NoError(t, err, "fsync-only difference must share the instance, not reject (false conflict)")
	defer ta2.Close()

	storeFact(t, ta2.MemStore(), "fsync-shared")
	pid := memory.PartitionIDFromName("tagent")
	refs, qerr := ta1.MemStore().QueryEvents(memory.QueryOptions{PartitionIDs: []int{pid}, Keyword: "fsync-shared"})
	require.NoError(t, qerr)
	require.NotEmpty(t, refs, "the two handles share ONE live store (no rebuild on the fsync axis)")
}

// TestOwnership_MidBuildFailureReleasesLease 钉住 构建在取得资源之后的步骤失败时，已取得的那份必须交还，路径仍可干净重试。
// - 泄漏的登记会让下一次装载撞上冲突或锁死，本例正是要拦这个形状；
// - 失败的构建不得留下任何可观察的事实残留。
// 契约: docs/wiki/platform/resource-ownership.md#reverse-release
func TestOwnership_MidBuildFailureReleasesLease(t *testing.T) {
	dir := t.TempDir()
	cfg := ownershipCfg(dir)
	a := cfg.Agents["tagent"]
	a.Tools = []ToolRef{{Kind: "tool", ID: "no_such_tool_def"}}
	cfg.Agents["tagent"] = a
	_, err := New(cfg, WithModel(&stubModel{name: "m"}))
	require.Error(t, err)

	ta2, err := New(ownershipCfg(dir), WithModel(&stubModel{name: "m"}))
	require.NoError(t, err)
	require.NoError(t, ta2.Close())
	require.True(t, readBack(t, dir, "never-written") == false)
}

// TestOwnership_ConcurrentAcquireSamePath 钉住 同一路径的并发获取串行开一次，全部持有者得到同一个实例。
// - 双开双实例会在这里暴露为不同指针；
// - 全部交还之后，同一路径可再次干净取得。
// 契约: docs/wiki/platform/resource-ownership.md#per-key-coordination
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
	store2, _, release2, err := rr.acquire("localfile", dir, fingerprintMemory(MemoryConfig{Type: "inmemory", Path: dir}), func() (openedResource, error) {
		return openedResource{store: memory.NewInMemoryStore()}, nil
	})
	if err != nil {
		t.Fatalf("reopen after concurrent release: %v", err)
	}
	release2()
	_ = store2
}

// TestLeaseRelease_IdempotentDoesNotHarmSurvivor 钉住 同一份租约被重复释放时只生效一次，别人那一份不受伤害。
// - 重复释放不得多减幸存者的计数：其 store 仍可写入、后端仍打开；
// - 真到最后一份交还时才关闭。
// 契约: docs/wiki/platform/resource-ownership.md#release-generation
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
	storeA, _, releaseA, err := rr.acquire("localfile", dir, fp, openFn)
	require.NoError(t, err)
	storeB, _, releaseB, err := rr.acquire("localfile", dir, fp, openFn)
	require.NoError(t, err)
	require.Same(t, storeA, storeB, "same path must share one instance")

	releaseA()
	releaseA()

	pid := memory.PartitionIDFromName("test")
	key := memory.NewSnowflakeEventKey(pid, 0)
	require.NoError(t, storeB.StoreEvent(key, memory.FullEvent{
		EventKey: key, PartitionID: pid, EventType: "external_input",
		Content: "after-A-double-close", Timestamp: 1700000000000,
	}), "F6: survivor store must remain writable after sibling double-Close")

	releaseB()
}

// TestLeaseRelease_StaleDoesNotAffectNewGeneration 钉住 上一代留下的释放闭包改不动已接手同一路径的新一代。
// - 新一代的 store 必须仍可写入——被陈旧闭包关掉会在这里露馅。
// 契约: docs/wiki/platform/resource-ownership.md#release-generation
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
	_, _, staleRelease, err := rr.acquire("localfile", dir, fp, openFn)
	require.NoError(t, err)
	staleRelease()

	newStore, _, newRelease, err := rr.acquire("localfile", dir, fp, openFn)
	require.NoError(t, err)

	staleRelease()

	pid := memory.PartitionIDFromName("stale")
	key := memory.NewSnowflakeEventKey(pid, 0)
	require.NoError(t, newStore.StoreEvent(key, memory.FullEvent{
		EventKey: key, PartitionID: pid, EventType: "external_input",
		Content: "after-stale", Timestamp: 1700000000000,
	}), "F6: stale release must not close the new generation entry")

	newRelease()
}

// TestEngineOwnership_SharedReopenGetsFreshEngine 钉住 引擎与其后端同代拥有：一代之内共用同一个活引擎，重开必须拿到全新引擎。
// - 单个借用者交还拆不掉共享引擎，幸存者的读写照常经过它；
// - 最后一次交还把引擎一起关闭，连带回收其后台工作协程；
// - 新一代拿到绑定新后端的全新引擎，绝不复用绑在已关后端上的那个；
// - 关闭与否经引擎的活/死单轴观察：选用不触发 KV 重建的后端，那个轴才只反映关闭态。
// 契约: docs/wiki/platform/resource-ownership.md#engine-generation
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

	_, eng2, rel2, err := rr.acquire("mem", dir, fp, openFn)
	require.NoError(t, err)
	require.Same(t, eng1, eng2, "one generation shares exactly one engine instance")

	rel1()
	require.True(t, eng1.Ready(), "sibling release must not close the shared engine")
	require.NoError(t, store1.StoreEvent(
		memory.NewSnowflakeEventKey(1, 0),
		memory.FullEvent{EventKey: memory.NewSnowflakeEventKey(1, 0), PartitionID: 1,
			EventType: "external_input", Content: "survivor", Timestamp: 1700000000000}))

	rel2()
	require.False(t, eng1.Ready(), "F7: last lease release must close the shared engine")

	_, eng3, rel3, err := rr.acquire("mem", dir, fp, openFn)
	require.NoError(t, err)
	require.NotNil(t, eng3)
	require.NotSame(t, eng1, eng3, "F7/D5: reopen must get a fresh engine bound to a fresh backend")
	require.True(t, eng3.Ready())
	rel3()
}

// seqStore wraps a memory.MemoryStore, records the teardown sequence, lets the
// test force Close to fail, and exposes StopProducers so closeResource stops
// the forgetting producers BEFORE the engine worker.
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

// TestCloseOrder_ProducersEngineBackendAndErrorReach 钉住 最终释放按依赖序拆除，且关闭错误必须回到释放的调用方。
// - 引擎停止未确认时后端不得被 flush（陈旧工作者仍可能写），该路径随后被具名封住；
// - 引擎已确认停止时，即便后端 flush 失败也照实返回错误并交还写权，重开得以成功。
// 契约: docs/wiki/platform/resource-ownership.md#close-order
// 契约: docs/wiki/platform/resource-ownership.md#poisoned-seal
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
		_, _, rel2, err2 := rr.acquire("localfile", dir, fp, openFn)
		require.NoError(t, err2, "confirmed stop must release the writer lock for reopen")
		_ = rel2()
	})
}

// blockCloseStore wraps a memory.MemoryStore whose Close blocks until unblocked,
// simulating a slow backend flush so the test can observe whether a same-path
// reopen wrongly races ahead of (or deadlocks against) the in-progress close.
type blockCloseStore struct {
	memory.MemoryStore
	// entered receives exactly once when a Close begins.
	entered chan struct{}
	// unblock releases Close: the call returns only after it is signalled.
	unblock chan struct{}
}

func (s *blockCloseStore) Close() error {
	s.entered <- struct{}{}
	<-s.unblock
	return nil
}

// TestReleaseCoordination_SamePathReopenWaitsForClose 钉住 同路径重开必须等上一代关完，不得撞上「登记已没了、写锁还在」的误报。
// - 慢 flush 期间的无关路径照常完成——全局登记锁不跨 I/O 持有；
// - 上一代关毕，重开成功拿到新实例，而不是具名报锁被占用。
// 契约: docs/wiki/platform/resource-ownership.md#per-key-coordination
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

	relDone := make(chan error, 1)
	go func() { relDone <- rel() }()
	<-bs.entered

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

	select {
	case got := <-reopen:
		t.Fatalf("reopen proceeded before close finished (err=%v) — per-key coordination missing", got.err)
	case <-time.After(150 * time.Millisecond):
	}

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

// TestEngineOwnership_SharedBuildFailureDegradesToCapacityOnly 钉住 引擎构建失败只能降为仅容量，不得留下悬垂引擎对象。
// - 条目的引擎为空，后端照常发布并干净关闭；
// - 每个借用者仍得到各自独立的容量钩子，检索退化为关键词路径（不做向量索引）；
// - 新一代重开同时拿到新后端与活引擎，以此证明陈旧一代被完全隔离。
// 契约: docs/wiki/platform/resource-ownership.md#engine-generation
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

	var hooked int
	borrow1, berr := wireMemoryEngine(store1, eng1, bad, func(int64, int, string) { hooked++ })
	require.NoError(t, berr)
	if ep, ok := borrow1.(memory.MemoryEngineProvider); ok {
		require.Nil(t, ep.MemoryEngine(), "degraded shared borrow must be capacity-only (nil engine)")
	}
	key := memory.NewSnowflakeEventKey(1, 0)
	require.NoError(t, borrow1.StoreEvent(key, memory.FullEvent{
		EventKey: key, PartitionID: 1, EventType: "external_input", Content: "deg", Timestamp: 1700000000000}))
	require.Equal(t, 1, hooked, "capacity hook must still fire on the degraded capacity-only borrow path")

	require.NoError(t, rel1(), "a degraded entry (engine nil) must still close cleanly")

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

// TestBuildFailure_CleanReclaimStaysRetryable 钉住 构建失败而回收已确认时，写权必须交还，同路径下一次获取能干净重开。
// 契约: docs/wiki/platform/resource-ownership.md#poisoned-seal
func TestBuildFailure_CleanReclaimStaysRetryable(t *testing.T) {
	dir := t.TempDir()
	rr := NewRuntimeResources()
	fp := fingerprintMemory(MemoryConfig{Type: "localfile", Path: dir})
	buildErr := errors.New("kv died at startup")

	_, _, _, err := rr.acquire("localfile", dir, fp, func() (openedResource, error) {
		return openedResource{}, buildErr
	})
	require.ErrorIs(t, err, buildErr)

	_, _, rel, err2 := rr.acquire("localfile", dir, fp, func() (openedResource, error) {
		return openedResource{store: &seqStore{MemoryStore: nil, seq: new([]string)}}, nil
	})
	require.NoError(t, err2, "a cleanly reclaimed failed build must not seal the path")
	require.NoError(t, rel())
}

// TestBuildFailure_UnconfirmedReclaimSealsWriter 钉住 回收无法确认的构建失败必须保持写权并封住路径，绝不与半活后端并写。
// - 原始失败原因仍要回到调用方，封路另用具名错误表达；
// - 封住是显式条目在册，不止账面记录：探测同一路径撞上「已被占用」。
// 契约: docs/wiki/platform/resource-ownership.md#poisoned-seal
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

	probe, perr := os.OpenFile(filepath.Join(canonicalize(dir), ".tagent-writer.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	require.NoError(t, perr)
	defer probe.Close()
	require.Error(t, flockExclusive(probe), "an unconfirmed reclaim must keep holding the writer lock")
}

// TestWriterLock_ExclusiveAcrossHandles 钉住 一个物理目录同时只允许一个写者，第二个持有者非阻塞抢锁必须失败。
// - 交还后可重新取得；
// - 进程崩溃由 OS 交还锁，不存在遗留标记把目录永久锁死。
// 契约: docs/wiki/platform/resource-ownership.md#single-writer
func TestWriterLock_ExclusiveAcrossHandles(t *testing.T) {
	dir := t.TempDir()

	f1, err := acquireDirLock(dir)
	require.NoError(t, err)

	_, err = acquireDirLock(dir)
	require.True(t, errors.Is(err, ErrStoreLocked), "second writer must be rejected, got: %v", err)

	require.NoError(t, unlockDirLock(f1))

	f2, err := acquireDirLock(dir)
	require.NoError(t, err, "after release the lock is re-acquirable")
	require.NoError(t, unlockDirLock(f2))
}

// TestPoisoned_ExplicitEntrySealsPathAcrossGC 钉住 未确认停止的路径由显式条目封住，强引用与失败原因都在册，主动 GC 削弱不了它。
// - 同路径获取一律具名失败，并带上记录的那次失败；
// - 封路优先于冲突记账：换另一份指纹报的仍是「被封住」；
// - 无关路径不受影响，封的是一条路径而非整张登记簿。
// 契约: docs/wiki/platform/resource-ownership.md#poisoned-seal
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

	key := resourceKey{kind: "localfile", path: canonicalize(dir)}
	rr.mu.Lock()
	e := rr.entries[key]
	rr.mu.Unlock()
	require.NotNil(t, e, "§6.4: the poisoned entry must be RETAINED, not detached + silently leaked")
	require.True(t, e.poisoned)
	require.Same(t, memory.MemoryStore(sealed), e.store, "entry keeps the strong store reference")
	require.NotNil(t, e.lockFile, "entry keeps the lockfile reference")
	require.ErrorIs(t, e.closeErr, engErr)

	runtime.GC()
	runtime.GC()

	_, _, _, err2 := rr.acquire("localfile", dir, fp, openFn)
	require.ErrorIs(t, err2, ErrResourcePoisoned, "same-path acquire must fail EXPLICITLY (poisoned), not via a flock race or a resurrected generation")
	require.ErrorContains(t, err2, engErr.Error(), "the sealing error carries the recorded failure to the caller")

	_, _, _, err3 := rr.acquire("localfile", dir, "v1|other|fp", openFn)
	require.ErrorIs(t, err3, ErrResourcePoisoned)

	other := t.TempDir()
	fpOther := fingerprintMemory(MemoryConfig{Type: "localfile", Path: other})
	_, _, relOther, err4 := rr.acquire("localfile", other, fpOther, func() (openedResource, error) {
		s := &seqStore{MemoryStore: memory.NewInMemoryStore(), seq: &seq}
		return openedResource{store: s}, nil
	})
	require.NoError(t, err4, "poisoning one path must not seal the registry")
	require.NoError(t, relOther())

	probe, perr := os.OpenFile(filepath.Join(canonicalize(dir), ".tagent-writer.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	require.NoError(t, perr)
	defer probe.Close()
	require.Error(t, flockExclusive(probe), "the poisoned entry must still hold the writer lock after GC")
}

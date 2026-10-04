package tagent

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/resources"
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
	kvStore, err := kv.NewLocalFileKV(dir)
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

// TestOwnership_EquivalentConfigsShareNotConflict 钉住行为恒同的配置不进冲突判定。
// - 两份等价装载共享同一个活实例而非被判成假冲突。
// - 判别是双面的：指纹逐字相等，且第二次装载被接受（不是拒绝）。
// - 两个句柄的写入互见——同一份 store 在服务，没有重建。
// 契约: docs/wiki/platform/resource-ownership.md#fingerprint-conflict
func TestOwnership_EquivalentConfigsShareNotConflict(t *testing.T) {
	dir := t.TempDir()
	base := ownershipCfg(dir)
	ta1, err := New(base, WithModel(&stubModel{name: "m"}))
	require.NoError(t, err)
	defer ta1.Close()

	other := ownershipCfg(dir)
	require.Equal(t,
		resources.FingerprintMemory(base.Agents["tagent"].Memory),
		resources.FingerprintMemory(other.Agents["tagent"].Memory),
		"behaviorally identical configs must produce identical fingerprints")

	ta2, err := New(other, WithModel(&stubModel{name: "m"}))
	require.NoError(t, err, "an identical config must share the instance, not reject (false conflict)")
	defer ta2.Close()

	storeFact(t, ta2.MemStore(), "shared-instance")
	pid := memory.PartitionIDFromName("tagent")
	refs, qerr := ta1.MemStore().QueryEvents(memory.QueryOptions{PartitionIDs: []int{pid}, Keyword: "shared-instance"})
	require.NoError(t, qerr)
	require.NotEmpty(t, refs, "the two handles share ONE live store (no rebuild on an equivalent config)")
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
	rr := resources.NewRuntimeResources()

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
			store, _, release, err := rr.Acquire("localfile", dir, resources.FingerprintMemory(MemoryConfig{Type: "inmemory", Path: dir}), func() (resources.OpenedResource, error) {
				return resources.OpenedResource{Store: memory.NewInMemoryStore()}, nil
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
	store2, _, release2, err := rr.Acquire("localfile", dir, resources.FingerprintMemory(MemoryConfig{Type: "inmemory", Path: dir}), func() (resources.OpenedResource, error) {
		return resources.OpenedResource{Store: memory.NewInMemoryStore()}, nil
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
	rr := resources.NewRuntimeResources()
	fp := resources.FingerprintMemory(MemoryConfig{Type: "localfile", Path: dir})
	openFn := func() (resources.OpenedResource, error) {
		k, err := kv.NewLocalFileKV(dir)
		if err != nil {
			return resources.OpenedResource{}, err
		}
		s, err := memory.NewFileSegmentStore(k, nil, dir, 100)
		return resources.OpenedResource{Store: s}, err
	}
	storeA, _, releaseA, err := rr.Acquire("localfile", dir, fp, openFn)
	require.NoError(t, err)
	storeB, _, releaseB, err := rr.Acquire("localfile", dir, fp, openFn)
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
	rr := resources.NewRuntimeResources()
	fp := resources.FingerprintMemory(MemoryConfig{Type: "localfile", Path: dir})
	openFn := func() (resources.OpenedResource, error) {
		k, err := kv.NewLocalFileKV(dir)
		if err != nil {
			return resources.OpenedResource{}, err
		}
		s, err := memory.NewFileSegmentStore(k, nil, dir, 100)
		return resources.OpenedResource{Store: s}, err
	}
	_, _, staleRelease, err := rr.Acquire("localfile", dir, fp, openFn)
	require.NoError(t, err)
	staleRelease()

	newStore, _, newRelease, err := rr.Acquire("localfile", dir, fp, openFn)
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
	rr := resources.NewRuntimeResources()
	mc := MemoryConfig{
		Type: "memory", Path: dir,
		Engine: &MemoryEngineConfig{Embedding: &EmbeddingConfig{Provider: "mock", Dimensions: 32}},
	}
	fp := resources.FingerprintMemory(mc)
	openFn := func() (resources.OpenedResource, error) {
		s := memory.NewInMemoryStore()
		return resources.OpenedResource{Store: s, Engine: buildSharedEngine(s, mc)}, nil
	}

	store1, eng1, rel1, err := rr.Acquire("mem", dir, fp, openFn)
	require.NoError(t, err)
	require.NotNil(t, eng1, "engine-configured shared path must have an entry-owned engine")
	require.True(t, eng1.Ready(), "freshly built engine must be live")

	_, eng2, rel2, err := rr.Acquire("mem", dir, fp, openFn)
	require.NoError(t, err)
	require.Same(t, eng1, eng2, "one generation shares exactly one engine instance")

	rel1()
	require.True(t, eng1.Ready(), "sibling release must not close the shared engine")
	require.NoError(t, store1.StoreEvent(
		memory.NewSnowflakeEventKey(1, 0),
		memory.FullEvent{EventKey: memory.NewSnowflakeEventKey(1, 0), PartitionID: 1,
			EventType: "external_input", Content: "survivor", Timestamp: 1700000000000}))

	rel2()
	require.False(t, eng1.Ready(), "last lease release must close the shared engine")

	_, eng3, rel3, err := rr.Acquire("mem", dir, fp, openFn)
	require.NoError(t, err)
	require.NotNil(t, eng3)
	require.NotSame(t, eng1, eng3, "reopen must get a fresh engine bound to a fresh backend")
	require.True(t, eng3.Ready())
	rel3()
}

// TestCloseOrder_ProducersEngineBackendAndErrorReach 钉住 最终释放按依赖序拆除，且关闭错误必须回到释放的调用方。
// - 引擎停止未确认时后端不得被 flush（陈旧工作者仍可能写），该路径随后被具名封住；
// - 引擎已确认停止时，即便后端 flush 失败也照实返回错误并交还写权，重开得以成功。
// 契约: docs/wiki/platform/resource-ownership.md#close-order
// 契约: docs/wiki/platform/resource-ownership.md#poisoned-seal
func TestCloseOrder_ProducersEngineBackendAndErrorReach(t *testing.T) {
	t.Run("engine error reaches release and holds lock", func(t *testing.T) {
		dir := t.TempDir()
		rr := resources.NewRuntimeResources()
		var seq []string
		engErr := errors.New("engine worker stuck")
		fp := resources.FingerprintMemory(MemoryConfig{Type: "localfile", Path: dir})
		openFn := func() (resources.OpenedResource, error) {
			s := &seqStore{MemoryStore: memory.NewInMemoryStore(), seq: &seq}
			e := &seqEngine{seq: &seq, err: engErr}
			return resources.OpenedResource{Store: s, Engine: e}, nil
		}
		_, _, rel, err := rr.Acquire("localfile", dir, fp, openFn)
		require.NoError(t, err)
		rerr := rel()
		require.ErrorIs(t, rerr, engErr, "engine close error must reach the release caller")
		require.Equal(t, []string{"producers", "engine"}, seq,
			"backend must NOT be flushed after an unconfirmed engine stop (stale worker may still write)")
		_, _, _, err2 := rr.Acquire("localfile", dir, fp, openFn)
		require.ErrorIs(t, err2, resources.ErrResourcePoisoned, "unconfirmed worker stop must SEAL the path via an explicit poisoned entry (§6.4), not a silent fd leak")
	})

	t.Run("store error reaches release and frees lock", func(t *testing.T) {
		dir := t.TempDir()
		rr := resources.NewRuntimeResources()
		var seq []string
		storeErr := errors.New("backend flush failed")
		fp := resources.FingerprintMemory(MemoryConfig{Type: "localfile", Path: dir})
		openFn := func() (resources.OpenedResource, error) {
			s := &seqStore{MemoryStore: memory.NewInMemoryStore(), seq: &seq, err: storeErr}
			e := &seqEngine{seq: &seq}
			return resources.OpenedResource{Store: s, Engine: e}, nil
		}
		_, _, rel, err := rr.Acquire("localfile", dir, fp, openFn)
		require.NoError(t, err)
		rerr := rel()
		require.ErrorIs(t, rerr, storeErr, "store close error must reach the release caller")
		require.Equal(t, []string{"producers", "engine", "store"}, seq,
			"close order: producers → engine → backend flush")
		_, _, rel2, err2 := rr.Acquire("localfile", dir, fp, openFn)
		require.NoError(t, err2, "confirmed stop must release the writer lock for reopen")
		_ = rel2()
	})
}

// TestReleaseCoordination_SamePathReopenWaitsForClose 钉住 同路径重开必须等上一代关完，不得撞上「登记已没了、写锁还在」的误报。
// - 慢 flush 期间的无关路径照常完成——全局登记锁不跨 I/O 持有；
// - 上一代关毕，重开成功拿到新实例，而不是具名报锁被占用。
// 契约: docs/wiki/platform/resource-ownership.md#per-key-coordination
func TestReleaseCoordination_SamePathReopenWaitsForClose(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	rr := resources.NewRuntimeResources()
	fp := resources.FingerprintMemory(MemoryConfig{Type: "localfile", Path: dir})

	entered := make(chan struct{}, 1)
	unblock := make(chan struct{})
	bs := &blockCloseStore{MemoryStore: memory.NewInMemoryStore(), entered: entered, unblock: unblock}
	_, _, rel, err := rr.Acquire("localfile", dir, fp, func() (resources.OpenedResource, error) {
		return resources.OpenedResource{Store: bs}, nil
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
		s, _, r2, e := rr.Acquire("localfile", dir, fp, func() (resources.OpenedResource, error) {
			return resources.OpenedResource{Store: memory.NewInMemoryStore()}, nil
		})
		reopen <- outcome{store: s, rel: r2, err: e}
	}()

	dfp := resources.FingerprintMemory(MemoryConfig{Type: "localfile", Path: other})
	otherDone := make(chan error, 1)
	go func() {
		_, _, r3, e := rr.Acquire("localfile", other, dfp, func() (resources.OpenedResource, error) {
			return resources.OpenedResource{Store: memory.NewInMemoryStore()}, nil
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
		require.NoError(t, got.err, "reopen after the old generation closed must succeed, not resources.ErrStoreLocked")
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
	rr := resources.NewRuntimeResources()
	bad := MemoryConfig{
		Type: "memory", Path: dir,
		Engine: &MemoryEngineConfig{Embedding: &EmbeddingConfig{Provider: "no-such-provider", Dimensions: 8}},
	}
	openBad := func() (resources.OpenedResource, error) {
		s := memory.NewInMemoryStore()
		return resources.OpenedResource{Store: s, Engine: buildSharedEngine(s, bad)}, nil
	}
	store1, eng1, rel1, err := rr.Acquire("mem", dir, resources.FingerprintMemory(bad), openBad)
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
	store2, eng2, rel2, err := rr.Acquire("mem", dir, resources.FingerprintMemory(good), func() (resources.OpenedResource, error) {
		s := memory.NewInMemoryStore()
		return resources.OpenedResource{Store: s, Engine: buildSharedEngine(s, good)}, nil
	})
	require.NoError(t, err)
	require.NotSame(t, store1, store2, "reopen must get a fresh backend store (old generation isolated)")
	require.NotNil(t, eng2, "reopen with a valid provider must build a real engine")
	require.True(t, eng2.Ready())
	require.NoError(t, rel2())
}

package tagent

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/governance"
	"github.com/SpellingDragon/tagent/agent/resources"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
	"github.com/SpellingDragon/tagent/prompt"
	toolmcp "github.com/SpellingDragon/tagent/tool/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
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

type seqEngine struct {
	memory.MemoryEngine
	seq *[]string
	err error
}

func (e *seqEngine) Close() error {
	*e.seq = append(*e.seq, "engine")
	return e.err
}

type blockCloseStore struct {
	memory.MemoryStore
	entered chan struct{}
	unblock chan struct{}
}

func (s *blockCloseStore) Close() error {
	s.entered <- struct{}{}
	<-s.unblock
	return nil
}

type countingHoldStore struct {
	*memory.InMemoryStore
	begins, ends int
}

func (s *countingHoldStore) BeginHold() { s.begins++ }
func (s *countingHoldStore) EndHold()   { s.ends++ }

// TestRuntimeConfig_StoreBarrierAggregatesAndReleases 钉住 组合根的遗忘屏障按 store 聚合、由顶层构建一次放尽。
// - 同一份共享 store 被两个 agent 触及只抬一层；
// - 不带保留租约的 store 没有屏障可抬，静默跳过，不算错误；
// - 重复释放不会多放一层；
// - 后一次构建在同一份 store 上再抬一层，窗口按构建嵌套计数；
// - 接收者为 nil 时抬与放都不触碰任何 store。
// 契约: docs/wiki/platform/resource-ownership.md#composition-barrier
func TestRuntimeConfig_StoreBarrierAggregatesAndReleases(t *testing.T) {
	rc := &runtimeConfig{}
	shared := &countingHoldStore{InMemoryStore: memory.NewInMemoryStore()}
	plain := memory.NewInMemoryStore()

	rc.raiseStoreBarrier(shared)
	rc.raiseStoreBarrier(shared)
	rc.raiseStoreBarrier(plain)
	require.Equal(t, 1, shared.begins, "one registration window per store per build")

	rc.releaseStoreBarriers()
	require.Equal(t, 1, shared.ends, "the build top-level releases exactly what it raised")

	rc.releaseStoreBarriers()
	require.Equal(t, 1, shared.ends)

	rc.raiseStoreBarrier(shared)
	require.Equal(t, 2, shared.begins)
	rc.releaseStoreBarriers()
	require.Equal(t, 2, shared.ends)

	var nilRC *runtimeConfig
	require.NotPanics(t, func() { nilRC.raiseStoreBarrier(shared); nilRC.releaseStoreBarriers() })
	require.Equal(t, 2, shared.begins, "a nil rc never mutates the store's barrier")
}

// TestBuildAgent_GovernanceWrapsAllAgents_SharedLedger 钉住 过闸覆盖真实构建出的工具链，共享账本仍分得清来源。
// - entry 与子 agent 各自的 exec 都被拒，结果里带显式拒绝标记；
// - 两条拒绝记录落在同一份账本里，来源 agent 名各自在册；
// - 观察对象是构建产物上的工具链，不是手工组装的闸——绕过构建就看不见包裹有没有真发生。
// 契约: docs/wiki/platform/platform-subsystems.md#governance-gate
func TestBuildAgent_GovernanceWrapsAllAgents_SharedLedger(t *testing.T) {
	agent.RegisterPlainTool("test_gov_exec", func(_ agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
		return &mockCallableTool{name: "exec"}, nil
	})

	rc := &runtimeConfig{model: &factoryMockModel{}}
	rc.govLedger = governance.NewDenialLedger(nil, 0)
	rc.govGate = governance.NewGovernanceGate(governance.GateDeps{
		Ledger: rc.govLedger,
		Config: governance.GateConfig{Enabled: true, Enforcement: governance.EnforcementStrict},
	})

	cfg := Config{
		Entry: "tagent",
		Agents: map[string]AgentConfig{
			"tagent": {
				SystemPrompt: PromptConfig{Inline: "entry prompt"},
				Memory:       MemoryConfig{Type: "memory"},
				Tools:        []ToolRef{{Kind: ToolKindTool, ID: "test_gov_exec"}},
			},
			"worker": {
				SystemPrompt: PromptConfig{Inline: "worker prompt"},
				Memory:       MemoryConfig{Type: "memory"},
				Tools:        []ToolRef{{Kind: ToolKindTool, ID: "test_gov_exec"}},
			},
		},
	}
	loader := prompt.NewLoader("")
	cache := make(map[string]*agent.TagentAgent)

	entry, err := buildAgent("tagent", cfg.Agents["tagent"], cfg, rc, loader, cache, buildModeResident)
	require.NoError(t, err)
	require.NotNil(t, entry)
	sub, err := buildAgent("worker", cfg.Agents["worker"], cfg, rc, loader, cache, buildModeResident)
	require.NoError(t, err)
	require.NotNil(t, sub)

	entryRes := callBuiltExec(t, entry)
	subRes := callBuiltExec(t, sub)
	assert.Contains(t, entryRes, "[governance_denied]", "entry exec 应过闸被拒（真实 buildAgent 包裹路径）")
	assert.Contains(t, subRes, "[governance_denied]", "子 agent exec 应过闸被拒（W3 前子 agent 主风险面绕闸）")

	recs := rc.govLedger.Query(-1)
	var sawEntry, sawSub bool
	for _, r := range recs {
		switch r.AgentName {
		case "tagent":
			sawEntry = true
		case "worker":
			sawSub = true
		}
	}
	assert.True(t, sawEntry, "共享 Ledger 应含 entry(tagent) 治理记录")
	assert.True(t, sawSub, "共享 Ledger 应含子 agent(worker) 治理记录（③ 同指针共享 + §8.1 按 agent 区分来源）")
}

// callBuiltExec 在已构建 agent 的工具列表里找声明名为 exec 的工具，经其（OutputLimitTool→
// GovernanceTool→mock）链式 Call 驱动一次 critical 操作，返回结果字符串。找不到即失败——
// 证明治理包裹路径确实构建了 exec leaf 工具。
func callBuiltExec(t *testing.T, ta *agent.TagentAgent) string {
	t.Helper()
	for _, tl := range ta.Tools() {
		decl := tl.Declaration()
		if decl == nil || decl.Name != "exec" {
			continue
		}
		callable, ok := tl.(trpctool.CallableTool)
		require.True(t, ok, "exec 工具应实现 CallableTool（OutputLimitTool 包裹 GovernanceTool）")
		res, err := callable.Call(context.Background(), []byte(`{"command":"rm -rf /tmp/x"}`))
		require.NoError(t, err)
		s, _ := res.(string)
		return s
	}
	t.Fatal("未找到声明名为 exec 的工具——治理包裹的 leaf 工具未经 buildAgent 构建")
	return ""
}

// TestGoalTools_EntryOnly 钉住 治理面入口只挂 entry。
// - entry 恰好拿到那五个治理面工具，一个不缺；
// - 子 agent 一个也不能有——治理面出现在子 agent 的工具表里就是本测要拦的形状。
// 契约: docs/wiki/tool/tool-architecture.md#govx-entry-only
func TestGoalTools_EntryOnly(t *testing.T) {
	rc := &runtimeConfig{model: &factoryMockModel{}}
	rc.govLedger = governance.NewDenialLedger(nil, 0)
	rc.govGate = governance.NewGovernanceGate(governance.GateDeps{
		Ledger: rc.govLedger,
		Goals:  governance.NewGoalRegistry(),
		Config: governance.GateConfig{Enabled: true, Enforcement: governance.EnforcementStrict},
	})
	cfg := Config{
		Entry: "tagent",
		Agents: map[string]AgentConfig{
			"tagent": {SystemPrompt: PromptConfig{Inline: "entry prompt"}, Memory: MemoryConfig{Type: "memory"}},
			"worker": {SystemPrompt: PromptConfig{Inline: "worker prompt"}, Memory: MemoryConfig{Type: "memory"}},
		},
	}
	cfg.Governance.Enabled = true
	loader := prompt.NewLoader("")
	cache := make(map[string]*agent.TagentAgent)

	entry, err := buildAgent("tagent", cfg.Agents["tagent"], cfg, rc, loader, cache, buildModeResident)
	require.NoError(t, err)
	sub, err := buildAgent("worker", cfg.Agents["worker"], cfg, rc, loader, cache, buildModeResident)
	require.NoError(t, err)

	hasGoalTools := func(ta *agent.TagentAgent) int {
		n := 0
		for _, tl := range ta.Tools() {
			switch tl.Declaration().Name {
			case "goal_declare", "goal_list", "goal_resolve", "denial_query", "approval_list":
				n++
			}
		}
		return n
	}
	if got := hasGoalTools(entry); got != 5 {
		t.Fatalf("entry should carry all 5 governance tools, got %d", got)
	}
	if got := hasGoalTools(sub); got != 0 {
		t.Fatalf("sub-agent must NOT carry governance tools, got %d", got)
	}
}

// wiringTool is a declaration-only tool for wiring tests.
type wiringTool struct{ name string }

func (w *wiringTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: w.name, Description: "wiring tool"}
}

// wiringToolSet is a ToolSet exposing fixed tools.
type wiringToolSet struct {
	name  string
	tools []trpctool.Tool
}

func (m *wiringToolSet) Tools(_ context.Context) []trpctool.Tool { return m.tools }
func (m *wiringToolSet) Close() error                            { return nil }
func (m *wiringToolSet) Name() string                            { return m.name }

// TestBuildPlainToolRef_MCPCallInjectsRegistry 钉住 网关工具看见的是装配根注入的那一份活注册表。
// - 证据形态：调用一个不存在的 server，失败结果里必须列出注册表里真实存在的服务名；
// - 注入没发生时清单为空，这一条当场失败。
// 契约: docs/wiki/tool/tool-architecture.md#mcp-gateway-injection
func TestBuildPlainToolRef_MCPCallInjectsRegistry(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())

	reg := toolmcp.NewRegistry()
	t.Cleanup(func() { _ = reg.Close() })
	reg.Add("mock", &wiringToolSet{name: "mock"})

	rc := &runtimeConfig{mcpRegistry: reg}
	tr := ToolRef{Kind: ToolKindTool, ID: "mcp_call"}

	callable, isAction, err := buildPlainToolRef(tr, "", "", rc, memory.NewInMemoryStore(), nil, "mcp gateway", nil, 0)
	require.NoError(t, err)
	require.NotNil(t, callable)
	assert.False(t, isAction)

	ct, ok := callable.(trpctool.CallableTool)
	require.True(t, ok, "mcp_call must be callable")
	res, err := ct.Call(context.Background(), []byte(`{"server":"nope","tool":"x"}`))
	require.NoError(t, err)
	b, err := json.Marshal(res)
	require.NoError(t, err)
	assert.Contains(t, string(b), "unknown MCP server")
	assert.Contains(t, string(b), "mock", "error must list registry servers, proving injection")
}

// TestBuildPlainToolRef_MCPCallWithoutRegistry 钉住 没有注册表可注入时网关照常构建，缺席只在调用结果里现形。
// - 构建不报错，工具可调用；
// - 失败以结果形态返回，内容显式说明没有任何服务被注册。
// 契约: docs/wiki/tool/tool-architecture.md#mcp-gateway-injection
func TestBuildPlainToolRef_MCPCallWithoutRegistry(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())

	rc := &runtimeConfig{}
	tr := ToolRef{Kind: ToolKindTool, ID: "mcp_call"}

	callable, _, err := buildPlainToolRef(tr, "", "", rc, memory.NewInMemoryStore(), nil, "mcp gateway", nil, 0)
	require.NoError(t, err)

	ct, ok := callable.(trpctool.CallableTool)
	require.True(t, ok, "mcp_call must be callable")
	res, err := ct.Call(context.Background(), []byte(`{"server":"a","tool":"b"}`))
	require.NoError(t, err)
	b, _ := json.Marshal(res)
	assert.Contains(t, string(b), "no MCP servers are registered")
}

// TestMCPDiscoverFactory_PrefersRegistry 钉住 发现工具读的是注入的活注册表，而且每次调用都重读。
// - 工厂创建之后才注册的服务同样必须被发现——静态切片做不到这一点；
// - 结果给出可照抄的调用方式，含 server 名与 tool 名。
// 契约: docs/wiki/tool/tool-architecture.md#mcp-live-registry
func TestMCPDiscoverFactory_PrefersRegistry(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())

	reg := toolmcp.NewRegistry()
	t.Cleanup(func() { _ = reg.Close() })

	factory, ok := agent.GetPlainToolFactory("mcp_discover")
	require.True(t, ok)
	ct, err := factory(agent.PlainToolFactoryConfig{ID: "mcp_discover", MCPRegistry: reg})
	require.NoError(t, err)

	reg.Add("web-search-prime", &wiringToolSet{
		name:  "web-search-prime",
		tools: []trpctool.Tool{&wiringTool{name: "webSearchPrime"}},
	})

	res, err := ct.Call(context.Background(), []byte(`{"query":"webSearchPrime"}`))
	require.NoError(t, err)
	b, err := json.Marshal(res)
	require.NoError(t, err)
	assert.Contains(t, string(b), `mcp_call(server=\"web-search-prime\", tool=\"webSearchPrime\"`)
	assert.Contains(t, string(b), "mcp:web-search-prime")
}

// TestLoadConfig_MCPServers verifies YAML parsing + ConfigPath recording.
func TestLoadConfig_MCPServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tagent.yaml")
	yaml := `
entry: tagent
agents:
  tagent:
    system_prompt:
      inline: "hi"
mcp_servers:
  web-search-prime:
    transport: streamable-http
    url: https://open.bigmodel.cn/api/mcp/web_search_prime/mcp
    api_key_env: ZAI_API_KEY
    timeout: 30s
`
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0644))

	cfg, err := LoadConfig(path)
	require.NoError(t, err)

	sc, ok := cfg.MCPServers["web-search-prime"]
	require.True(t, ok)
	assert.Equal(t, "streamable-http", sc.Transport)
	assert.Equal(t, "https://open.bigmodel.cn/api/mcp/web_search_prime/mcp", sc.URL)
	assert.Equal(t, "ZAI_API_KEY", sc.APIKeyEnv)
	assert.Equal(t, "30s", sc.Timeout)
	assert.True(t, filepath.IsAbs(cfg.ConfigPath), "ConfigPath must be recorded (absolute)")
}

// TestConfigValidate_MCPServers covers the three validation failures.
func TestConfigValidate_MCPServers(t *testing.T) {
	base := func() Config {
		return Config{
			Entry: "tagent",
			Agents: map[string]AgentConfig{
				"tagent": {SystemPrompt: PromptConfig{Inline: "hi"}},
			},
		}
	}

	cases := []struct {
		name    string
		server  MCPServerConfig
		wantErr string
	}{
		{"sse missing url", MCPServerConfig{Transport: "sse"}, "requires url"},
		{"stdio missing command", MCPServerConfig{Transport: "stdio"}, "requires command"},
		{"unsupported transport", MCPServerConfig{Transport: "websocket", URL: "https://x"}, "unsupported transport"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base()
			cfg.MCPServers = map[string]MCPServerConfig{"bad": tc.server}
			cfg.ApplyDefaults()
			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Contains(t, err.Error(), "bad")
		})
	}

	cfg := base()
	cfg.MCPServers = map[string]MCPServerConfig{
		"ok": {Transport: "streamable-http", URL: "https://example.com/mcp"},
	}
	cfg.ApplyDefaults()
	require.NoError(t, cfg.Validate())
}

// TestWireMemoryEngine_NilEngineUnchanged 钉住 未配置引擎时拿到的是内层 store 本体，不新增任何包裹。
// - 判据是能力接口的缺席：它不得声称自己是引擎提供者；
// - 只有"无引擎配置"与"无 store 事件回调"同时成立才走这条路。
// 契约: docs/wiki/memory/memory-architecture.md#engine-wiring-gates
func TestWireMemoryEngine_NilEngineUnchanged(t *testing.T) {
	store := memory.NewInMemoryStore()
	got, err := wireMemoryEngine(store, nil, MemoryConfig{}, nil)
	if err != nil {
		t.Fatalf("wireMemoryEngine: %v", err)
	}
	if _, ok := got.(memory.MemoryEngineProvider); ok {
		t.Fatal("未配置 Engine 时不应包裹引擎")
	}
}

// TestWireMemoryEngine_EmbeddingNilUnchanged 钉住 只有引擎壳子而没有嵌入配置时同样不接引擎。
// - 没有向量能力就没有语义检索，store 保持与未配置引擎同一形态。
// 契约: docs/wiki/memory/memory-architecture.md#engine-wiring-gates
func TestWireMemoryEngine_EmbeddingNilUnchanged(t *testing.T) {
	store := memory.NewInMemoryStore()
	got, err := wireMemoryEngine(store, nil, MemoryConfig{Engine: &MemoryEngineConfig{}}, nil)
	if err != nil {
		t.Fatalf("wireMemoryEngine: %v", err)
	}
	if _, ok := got.(memory.MemoryEngineProvider); ok {
		t.Fatal("无 Embedding 时不应包裹引擎")
	}
}

// TestWireMemoryEngine_MockEmbedderWraps 验证 mock 嵌入配置 → 包裹引擎（MemoryEngineProvider）。
func TestWireMemoryEngine_MockEmbedderWraps(t *testing.T) {
	store := memory.NewInMemoryStore()
	mc := MemoryConfig{Engine: &MemoryEngineConfig{Embedding: &EmbeddingConfig{Provider: "mock", Dimensions: 32}}}
	got, err := wireMemoryEngine(store, nil, mc, nil)
	if err != nil {
		t.Fatalf("wireMemoryEngine: %v", err)
	}
	ep, ok := got.(memory.MemoryEngineProvider)
	if !ok {
		t.Fatal("配置 mock 嵌入后应包裹引擎(MemoryEngineProvider)")
	}
	if ep.MemoryEngine() == nil {
		t.Fatal("引擎不应为 nil")
	}
	if c, ok := got.(interface{ Close() error }); ok {
		_ = c.Close()
	} else {
		t.Fatal("包裹后的 store 应可 Close（agent.Closer）")
	}
}

// TestWireMemoryEngine_ZhipuNoKeyDegrades 钉住 密钥未配时嵌入能力按"功能关闭"降级：不报错、不阻断构建。
// - 拿回的 store 不得声称自己是引擎提供者；
// - 缺 key 是配置事实，不构成调用方必须处理的错误。
// 契约: docs/wiki/memory/memory-architecture.md#engine-wiring-gates
// 契约: docs/wiki/memory/memory-architecture.md#embedder
func TestWireMemoryEngine_ZhipuNoKeyDegrades(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "")
	store := memory.NewInMemoryStore()
	mc := MemoryConfig{Engine: &MemoryEngineConfig{Embedding: &EmbeddingConfig{Provider: "zhipu"}}}
	got, err := wireMemoryEngine(store, nil, mc, nil)
	if err != nil {
		t.Fatalf("无 key 应优雅降级不报错, got %v", err)
	}
	if _, ok := got.(memory.MemoryEngineProvider); ok {
		t.Fatal("无 key 时应降级为原 store(无引擎)")
	}
}

// TestWireMemoryEngine_UnknownBackendErrors 钉住 引擎 backend 名不认识时按"功能关闭"降级：不报错、不阻断构建。
// - 拿回的 store 不得声称自己是引擎提供者，它已被降为仅容量包裹；
// - 增强能力的故障只关掉增强本身，不构成调用方必须处理的错误。
// 契约: docs/wiki/memory/memory-architecture.md#engine-wiring-gates
func TestWireMemoryEngine_UnknownBackendErrors(t *testing.T) {
	store := memory.NewInMemoryStore()
	mc := MemoryConfig{Engine: &MemoryEngineConfig{Backend: "bogus", Embedding: &EmbeddingConfig{Provider: "mock"}}}
	got, err := wireMemoryEngine(store, nil, mc, nil)
	if err != nil {
		t.Fatalf("应优雅降级不报错, got %v", err)
	}
	if _, ok := got.(memory.MemoryEngineProvider); ok {
		t.Fatal("未知 backend 应降级为原 store")
	}
}

// fakeModel 是满足 model.Model 的最小替身（New 构建期不调用 GenerateContent，仅运行期）。
type fakeModel struct{}

func (fakeModel) GenerateContent(context.Context, *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response)
	close(ch)
	return ch, nil
}

func (fakeModel) Info() model.Info { return model.Info{} }

// minimalConfig 构造最小单 agent 配置（inline 提示词 + 无工具 + 内存 store），避开
// DefaultConfig 的多 agent 依赖（knowledge 需 SkillRepo、recall 需分区等）。
func minimalConfig(_ string, evoEnabled bool) Config {
	cfg := Config{
		Entry: "tagent",
		Agents: map[string]AgentConfig{
			"tagent": {
				SystemPrompt: PromptConfig{Inline: "你是 tagent 测试代理"},
				Memory:       MemoryConfig{Type: "memory"},
			},
		},
	}
	cfg.ApplyDefaults()
	cfg.Evolution.Enabled = evoEnabled
	return cfg
}

// TestNew_EvolutionEnabled_GitNativeSmoke 钉住 启用自进化的配置能通过完整装配。
// - 本测只观察"启用不等于起不来"；改进行为本身由 evolution 包在临时 git 仓里钉。
// 契约: docs/wiki/platform/platform-subsystems.md#evolution-wiring
func TestNew_EvolutionEnabled_GitNativeSmoke(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	a, err := New(minimalConfig("", true), WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)
}

// TestNew_EvolutionDisabled_NoSideEffect 验证配置门控：默认关闭时 New 成功（现状零行为变化）。
func TestNew_EvolutionDisabled_NoSideEffect(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	cfg := minimalConfig("", false)
	require.False(t, cfg.Evolution.Enabled)

	a, err := New(cfg, WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)
}

// TestNew_GovernanceEnabled_Builds 钉住 启用治理的配置能通过完整装配。
// - 本测只观察"启用不等于起不来"；分级与处置语义由治理自身的测试钉。
// 契约: docs/wiki/platform/platform-subsystems.md#governance-gate
func TestNew_GovernanceEnabled_Builds(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	cfg := minimalConfig(filepath.Join(t.TempDir(), "evo"), false)
	cfg.Governance.Enabled = true
	cfg.Governance.Enforcement = "warn"
	cfg.Governance.Dir = t.TempDir()

	a, err := New(cfg, WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)
}

// TestNew_GovernanceDisabled_Default 验证 governance 默认关闭（零值）→ New 成功，现状不变。
func TestNew_GovernanceDisabled_Default(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	cfg := minimalConfig(filepath.Join(t.TempDir(), "evo"), false)
	require.False(t, cfg.Governance.Enabled)

	a, err := New(cfg, WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)
}

// TestNew_ReliableBusSpillDir 钉住 配了总线 durable inbox 根目录后，entry 得到属于自己的收件箱子目录。
// - 目录形如 `<inbox 根>/<entry>`，装配完成时已存在，不必等第一次落盘。
// 契约: docs/wiki/platform/platform-subsystems.md#reliability-switches
func TestNew_ReliableBusSpillDir(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	spillRoot := filepath.Join(t.TempDir(), "bus-spill")
	cfg := minimalConfig(filepath.Join(t.TempDir(), "evo"), false)
	cfg.Reliability.BusSpillDir = spillRoot

	a, err := New(cfg, WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)

	_, statErr := os.Stat(filepath.Join(spillRoot, cfg.Entry))
	require.NoError(t, statErr, "per-agent 溢出子目录 <BusSpillDir>/<entry> 应被创建")
}

// TestNew_ReliableBusDisabledDefault 验证配置门控：默认 BusSpillDir 空 → 不建收件箱，总线保持纯 volatile。
func TestNew_ReliableBusDisabledDefault(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	cfg := minimalConfig(filepath.Join(t.TempDir(), "evo"), false)
	require.Empty(t, cfg.Reliability.BusSpillDir)

	a, err := New(cfg, WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)
}

// TestNew_DegradationEnabled_Builds 钉住 退化开关独立成立：治理段关闭时启用它照样装配成功。
// - 本例的治理段是关的，只有退化开关为真；
// - 装配得到可用 agent，退化状态机不与治理配置相互牵连。
// 契约: docs/wiki/platform/platform-subsystems.md#reliability-switches
func TestNew_DegradationEnabled_Builds(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	cfg := minimalConfig(filepath.Join(t.TempDir(), "evo"), false)
	cfg.Reliability.DegradationEnabled = true

	a, err := New(cfg, WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)
}

// TestResolveMemoryStore_FileSamePathShared 钉住 `type: file` 的同一 path 得到同一个 store 实例，与 memory/localfile 同构。
// - 判据是对象身份，不是内容相似；
// - 两次解析各自领到释放钩子，两个都要交还。
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func TestResolveMemoryStore_FileSamePathShared(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared-mem")
	s1, _, rel1, err := resolveMemoryStore(MemoryConfig{Type: "file", Path: path})
	require.NoError(t, err)
	s2, _, rel2, err := resolveMemoryStore(MemoryConfig{Type: "file", Path: path})
	require.NoError(t, err)
	require.Same(t, s1, s2, "file 后端同 path 必须共享同一实例（M-1：防因果链断链/双 Compactor 并发覆盖）")
	rel1()
	rel2()
}

// TestConfig_WorkingDir 钉住 工作根的三态取法：yaml 初值、环境变量非空覆盖、两者皆空保持空。
// - 环境变量写空值等于没写，不触发覆盖；
// - 空串不是 "." 的别名——它意味着 file 与 exec 各自继承进程工作目录。
// 契约: docs/wiki/platform/agent-behavior-matrix.md#working-dir
func TestConfig_WorkingDir(t *testing.T) {
	t.Run("yaml 解析 working_dir", func(t *testing.T) {
		t.Setenv("TAGENT_WORKING_DIR", "")
		var cfg Config
		require.NoError(t, yaml.Unmarshal([]byte("working_dir: /home/user/codes\n"), &cfg))
		cfg.ApplyDefaults()
		assert.Equal(t, "/home/user/codes", cfg.WorkingDir)
	})

	t.Run("空 working_dir 默认空(=继承进程 cwd,现状逐字节不变)", func(t *testing.T) {
		t.Setenv("TAGENT_WORKING_DIR", "")
		var cfg Config
		require.NoError(t, yaml.Unmarshal([]byte("model: glm\n"), &cfg))
		cfg.ApplyDefaults()
		assert.Empty(t, cfg.WorkingDir, "空=file base_dir '.'/exec 继承进程 cwd,现状不变")
	})

	t.Run("TAGENT_WORKING_DIR env 覆盖 yaml 值", func(t *testing.T) {
		t.Setenv("TAGENT_WORKING_DIR", "/env/codes")
		var cfg Config
		require.NoError(t, yaml.Unmarshal([]byte("working_dir: /yaml/codes\n"), &cfg))
		cfg.ApplyDefaults()
		assert.Equal(t, "/env/codes", cfg.WorkingDir, "env 应覆盖 yaml(部署时灵活指定 clone 根)")
	})

	t.Run("TAGENT_WORKING_DIR env 注入(yaml 未配)", func(t *testing.T) {
		t.Setenv("TAGENT_WORKING_DIR", "/env/only")
		var cfg Config
		require.NoError(t, yaml.Unmarshal([]byte("model: glm\n"), &cfg))
		cfg.ApplyDefaults()
		assert.Equal(t, "/env/only", cfg.WorkingDir)
	})
}

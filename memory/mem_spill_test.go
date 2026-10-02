package memory

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flakyStore 首次可配置失败、后成功（模拟 memory 退化→恢复）。
type flakyStore struct {
	*InMemoryStore
	fail bool
}

func (f *flakyStore) StoreEvent(k int64, ev FullEvent) error {
	if f.fail {
		return errors.New("memory degraded")
	}
	return f.InMemoryStore.StoreEvent(k, ev)
}

// failStore 恒失败（验证重放部分失败保留）。
type failStore struct {
	*InMemoryStore
}

func (f *failStore) StoreEvent(int64, FullEvent) error { return errors.New("still failing") }

// ReplayEvent overrides the inherited InMemoryStore.ReplayEvent
// to maintain the "always fails" invariant for both the old StoreEvent and new ReplayEvent paths.
func (f *failStore) ReplayEvent(_ int64, ev FullEvent) (ReplayResult, FullEvent, error) {
	return ReplayNew, ev, errors.New("still failing")
}

// falseNegativeStore 模拟 W1 假阴性：首次 StoreEvent 实际写入成功但返回 error（如 KV 已写但
// rustviking CLI 响应解析失败），且拒绝重复 key 重写（同 FileSegmentStore "already exists" 守卫）。
type falseNegativeStore struct {
	*InMemoryStore
	written map[int64]bool
}

func (f *falseNegativeStore) StoreEvent(k int64, ev FullEvent) error {
	if f.written[k] {
		return errors.New("event key already exists (snowflake collision?): refusing to overwrite")
	}
	if f.written == nil {
		f.written = map[int64]bool{}
	}
	f.written[k] = true
	_ = f.InMemoryStore.StoreEvent(k, ev)
	return errors.New("rustviking: response parse failed (KV already written)")
}

// TestMemSpill_ReplayIdempotentFalseNegative 钉住 是 W1回归：假阴性失败（KV 已写但
//
// 契约: docs/wiki/memory/memory-architecture.md#error-tracking
func TestMemSpill_ReplayIdempotentFalseNegative(t *testing.T) {
	sp := NewMemSpill(filepath.Join(t.TempDir(), "w1.jsonl"))
	store := &falseNegativeStore{InMemoryStore: NewInMemoryStore()}
	k := NewSnowflakeEventKey(1, testBaseMs)
	ev := FullEvent{EventKey: k, PartitionID: 1, EventType: TypeExternalInputProbe, Timestamp: testBaseMs}
	if err := store.StoreEvent(k, ev); err == nil {
		t.Fatal("应假阴性失败")
	}
	if err := sp.Append(k, ev); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if sp.Len() != 1 {
		t.Fatalf("应落盘 1, got %d", sp.Len())
	}
	n, err := sp.Replay(store)
	if err != nil || n != 1 {
		t.Fatalf("W1: 假阴性重放应收敛(预检幂等), got n=%d err=%v", n, err)
	}
	if sp.Len() != 0 {
		t.Fatalf("W1: 重放后 spill 应归零(不永久滞留), got %d", sp.Len())
	}
}

func TestMemSpill_AppendReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mem_spill.jsonl")
	sp := NewMemSpill(path)
	store := NewInMemoryStore()
	for i := 0; i < 3; i++ {
		k := NewSnowflakeEventKey(1, testBaseMs+int64(i))
		if err := sp.Append(k, FullEvent{EventKey: k, PartitionID: 1, EventType: TypeExternalInputProbe, Content: "spilled", Timestamp: testBaseMs}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if sp.Len() != 3 {
		t.Fatalf("应 3 兜底, got %d", sp.Len())
	}
	n, err := sp.Replay(store)
	if err != nil || n != 3 {
		t.Fatalf("Replay 应重放 3, got n=%d err=%v", n, err)
	}
	if sp.Len() != 0 {
		t.Fatalf("重放后应清空, got %d", sp.Len())
	}
	refs, _ := store.QueryEvents(QueryOptions{PartitionIDs: []int{1}})
	if len(refs) != 3 {
		t.Fatalf("重放后 store 应 3 事件, got %d", len(refs))
	}
}

func TestMemSpill_NilPathDisabled(t *testing.T) {
	if sp := NewMemSpill(""); sp != nil {
		t.Fatal("空 path 应返回 nil（禁用）")
	}
	var sp *MemSpill
	if err := sp.Append(1, FullEvent{}); err != nil {
		t.Fatal("nil Append 应 no-op 无错")
	}
	if n, _ := sp.Replay(NewInMemoryStore()); n != 0 {
		t.Fatal("nil Replay 应 0")
	}
	if sp.Len() != 0 {
		t.Fatal("nil Len 应 0")
	}
}

func TestMemSpill_ReplayPartialFailureRetains(t *testing.T) {
	sp := NewMemSpill(filepath.Join(t.TempDir(), "s.jsonl"))
	for i := 0; i < 2; i++ {
		k := NewSnowflakeEventKey(1, testBaseMs+int64(i))
		_ = sp.Append(k, FullEvent{EventKey: k, PartitionID: 1, Timestamp: testBaseMs})
	}
	n, _ := sp.Replay(&failStore{InMemoryStore: NewInMemoryStore()})
	if n != 0 {
		t.Fatalf("恒失败 store 应重放 0, got %d", n)
	}
	if sp.Len() != 2 {
		t.Fatalf("重放失败应保留 2, got %d", sp.Len())
	}
}

// TestErrorTrackingStore_SpillAndReplay 钉住 步4 端到端：StoreEvent 失败 → 落盘兜底；
//
// 契约: docs/wiki/memory/memory-architecture.md#error-tracking
func TestErrorTrackingStore_SpillAndReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ets_spill.jsonl")
	inner := &flakyStore{InMemoryStore: NewInMemoryStore(), fail: true}
	ets := NewErrorTrackingStore(inner, nil)
	ets.SetMemSpill(path)

	k := NewSnowflakeEventKey(1, testBaseMs)
	ev := FullEvent{EventKey: k, PartitionID: 1, EventType: TypeExternalInputProbe, Content: "x", Timestamp: testBaseMs}
	if err := ets.StoreEvent(k, ev); err == nil {
		t.Fatal("inner 失败应返回 err")
	}
	if ets.MemSpillLen() != 1 {
		t.Fatalf("StoreEvent 失败应落盘 1 兜底, got %d", ets.MemSpillLen())
	}
	inner.fail = false
	n, err := ets.ReplaySpilled()
	if err != nil || n != 1 {
		t.Fatalf("重放应 1, got n=%d err=%v", n, err)
	}
	if ets.MemSpillLen() != 0 {
		t.Fatalf("重放后兜底应清空, got %d", ets.MemSpillLen())
	}
	if got, _ := inner.GetEvent(k); got == nil {
		t.Fatal("重放后 inner 应有该事件（回灌成功，事件不丢）")
	}
}

func TestErrorTrackingStore_NoSpillConfigured(t *testing.T) {
	inner := &flakyStore{InMemoryStore: NewInMemoryStore(), fail: true}
	ets := NewErrorTrackingStore(inner, nil)
	k := NewSnowflakeEventKey(1, testBaseMs)
	_ = ets.StoreEvent(k, FullEvent{EventKey: k, PartitionID: 1, Timestamp: testBaseMs})
	if ets.MemSpillLen() != 0 {
		t.Fatal("未配 mem_spill 时不应落盘")
	}
	if n, _ := ets.ReplaySpilled(); n != 0 {
		t.Fatal("未配 mem_spill 时 ReplaySpilled 应 0")
	}
}

// guardStore 同时是 MemoryStore 与 RetentionGuard（满足 ErrorTrackingStore
// SetMemSpill 的 inner.(RetentionGuard) 能力检查，使其走进 ProtectAllPending 重建腿）。
type guardStore struct {
	*InMemoryStore
}

func (s *guardStore) ProtectKey(int64) {}
func (s *guardStore) ReleaseKey(int64) {}
func (s *guardStore) ArmRetention()    {}
func (s *guardStore) BeginHold()       {}
func (s *guardStore) EndHold()         {}

// TestErrorTrackingStore_SetMemSpill_PropagatesRetentionFailure 钉住
//
// 契约: docs/wiki/memory/memory-architecture.md#error-tracking
func TestErrorTrackingStore_SetMemSpill_PropagatesRetentionFailure(t *testing.T) {
	inner := &guardStore{InMemoryStore: NewInMemoryStore()}
	ets := NewErrorTrackingStore(inner, nil)
	if err := ets.SetMemSpill(t.TempDir()); err == nil {
		t.Fatal("spill 保留重建读失败必须上抛（fail-closed），绝不吞错")
	}
	ets2 := NewErrorTrackingStore(&guardStore{InMemoryStore: NewInMemoryStore()}, nil)
	if err := ets2.SetMemSpill(filepath.Join(t.TempDir(), "fresh.jsonl")); err != nil {
		t.Fatalf("空 spill 路径应正常 arm 并返回 nil, got %v", err)
	}
}

// nonReplayerStore is a MemoryStore that deliberately does NOT implement EventReplayer
// : spill replay against it must FAIL the capability check and retain every
// original, never degrade to the removed GetEvent+StoreEvent weak fallback.
type nonReplayerStore struct{ in *InMemoryStore }

func (s *nonReplayerStore) StoreEvent(k int64, e FullEvent) error { return s.in.StoreEvent(k, e) }
func (s *nonReplayerStore) GetEvent(k int64) (*FullEvent, error)  { return s.in.GetEvent(k) }
func (s *nonReplayerStore) GetEvents(ks []int64) ([]FullEvent, error) {
	return s.in.GetEvents(ks)
}
func (s *nonReplayerStore) QueryEvents(q QueryOptions) ([]EventReference, error) {
	return s.in.QueryEvents(q)
}
func (s *nonReplayerStore) SearchByEmbedding(e []float32, n int) ([]EventReference, error) {
	return s.in.SearchByEmbedding(e, n)
}
func (s *nonReplayerStore) StoreEventWithEmbedding(k int64, e FullEvent, emb []float32) error {
	return s.in.StoreEventWithEmbedding(k, e, emb)
}
func (s *nonReplayerStore) SupportsVectorSearch() bool { return s.in.SupportsVectorSearch() }
func (s *nonReplayerStore) DeleteEvent(k int64) error  { return s.in.DeleteEvent(k) }
func (s *nonReplayerStore) GetStats() StoreStats       { return s.in.GetStats() }

func TestMemSpill_ReplayWithoutReplayerRefused(t *testing.T) {
	spill := NewMemSpill(t.TempDir() + "/spill.jsonl")
	key := NewSnowflakeEventKey(1, 1704067200*1000)
	require.NoError(t, spill.Append(key, FullEvent{
		EventKey: key, PartitionID: 1, EventType: "external_input", Timestamp: 1704067200000,
	}))
	require.Equal(t, 1, spill.Len())

	n, err := spill.Replay(&nonReplayerStore{in: NewInMemoryStore()})
	require.Error(t, err, "§2.6: replay against a non-EventReplayer store must be refused, not weakly degraded")
	require.Equal(t, 0, n)
	require.Equal(t, 1, spill.Len(), "§2.6: the spill original must be retained, never consumed by a GetEvent weak fallback")
}

// countGuard is a test RetentionGuard recording the net holders per key.
type countGuard struct{ m map[int64]int }

func newCountGuard() *countGuard          { return &countGuard{m: map[int64]int{}} }
func (g *countGuard) ProtectKey(k int64)  { g.m[k]++ }
func (g *countGuard) ReleaseKey(k int64)  { g.m[k]-- }
func (g *countGuard) ArmRetention()       {}
func (g *countGuard) BeginHold()          {}
func (g *countGuard) EndHold()            {}
func (g *countGuard) holders(k int64) int { return g.m[k] }

// TestMemSpill_RetentionBelt 钉住 spill belt: appending a pending key protects its original; a successful
//
// 契约: docs/wiki/memory/memory-architecture.md#error-tracking
func TestMemSpill_RetentionBelt(t *testing.T) {
	path := t.TempDir() + "/spill.jsonl"
	key := NewSnowflakeEventKey(1, 1704067200*1000)
	ev := FullEvent{EventKey: key, PartitionID: 1, EventType: "external_input", Timestamp: 1704067200000}

	g := newCountGuard()
	sp := NewMemSpill(path)
	sp.SetGuard(g)
	require.NoError(t, sp.Append(key, ev))
	require.Equal(t, 1, g.holders(key), "append must protect the pending spill key")

	g2 := newCountGuard()
	sp2 := NewMemSpill(path)
	sp2.SetGuard(g2)
	require.NoError(t, sp2.ProtectAllPending())
	require.Equal(t, 1, g2.holders(key), "ProtectAllPending must rebuild the holder from the file")

	n, err := sp2.ReplayWithNotify(NewInMemoryStore(), nil)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, 0, g2.holders(key), "successful replay must release the spill key (§2.8 no leak)")
}

func TestMemSpill_ProtectionHoldsUntilSafeRemoval(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, rel, ":memory:", 100)
	require.NoError(t, err)
	ts := NewTombstoneSet(rel, mockKV, 1)
	store.tombstones = ts
	lease := NewRetentionLease()
	store.SetRetentionLease(lease)

	now := time.Now().UnixMilli()
	expired := NewSnowflakeEventKey(1, now-10*24*3600*1000)
	ev := FullEvent{EventKey: expired, PartitionID: 1, EventType: "thinking_plan",
		EventSummary: "pending in spill past its TTL age", Timestamp: now - 10*24*3600*1000}
	require.NoError(t, store.StoreEvent(expired, ev))

	sp := NewMemSpill(t.TempDir() + "/spill.jsonl")
	sp.SetGuard(store)
	require.NoError(t, sp.Append(expired, ev))
	require.True(t, store.IsKeyProtected(expired), "the pending spill key protects its original")

	lease.MarkReady()
	lm := NewLifecycleManager(store, ts, DefaultLifecycleConfig())
	defer lm.Stop()
	lm.SweepOnce()
	assert.False(t, ts.IsTombstone(expired),
		"§5.9: a spill-pending original must survive the forgetting pass (protection reaches the scan)")

	n, rerr := sp.ReplayWithNotify(store, nil)
	require.NoError(t, rerr)
	require.Equal(t, 1, n)
	pending, perr := sp.PendingKeys()
	require.NoError(t, perr)
	require.Empty(t, pending, "the spill entry was removed only after the replay landed")

	lm.SweepOnce()
	assert.True(t, ts.IsTombstone(expired),
		"§5.9: after the release the original ages on its ORIGINAL timestamp (no re-stamp) and the next pass forgets it")
}

func TestRetentionLease_HoldBarrierNestsAndRearms(t *testing.T) {
	cleared := func(l *RetentionLease) bool {
		select {
		case <-l.HoldClear():
			return true
		default:
			return false
		}
	}
	l := NewRetentionLease()
	require.True(t, cleared(l), "a fresh lease holds nothing — the barrier is open")

	l.BeginHold()
	require.False(t, cleared(l))
	l.BeginHold()
	l.EndHold()
	assert.False(t, cleared(l), "the inner End must not open the outer owner's window")
	l.EndHold()
	assert.True(t, cleared(l), "the outermost End releases the barrier")

	l.BeginHold()
	assert.False(t, cleared(l), "a re-raised hold blocks — fail-before: without gate swap on 0→1 this passes and a late attach runs unprotected")
	l.EndHold()
	assert.True(t, cleared(l))

	l.EndHold()
	assert.True(t, cleared(l))

	var nilLease *RetentionLease
	nilLease.BeginHold()
	nilLease.EndHold()
	select {
	case <-nilLease.HoldClear():
	default:
		t.Fatal("nil lease must report the barrier clear")
	}
}

// TestRetentionLease_ScannerPausesUnderHold 钉住 end-to-end of the
//
// 契约: docs/wiki/memory/memory-architecture.md#error-tracking
func TestRetentionLease_ScannerPausesUnderHold(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, rel, ":memory:", 100)
	require.NoError(t, err)
	ts := NewTombstoneSet(rel, mockKV, 1)
	store.tombstones = ts
	lease := NewRetentionLease()
	store.SetRetentionLease(lease)
	lm := NewLifecycleManager(store, ts, DefaultLifecycleConfig())
	defer lm.Stop()

	now := time.Now().UnixMilli()
	overdue := NewSnowflakeEventKey(1, now-10*24*3600*1000)
	require.NoError(t, store.StoreEvent(overdue, FullEvent{
		EventKey: overdue, PartitionID: 1, EventType: "thinking_plan",
		EventSummary: "material a late attach still needs", Timestamp: now - 10*24*3600*1000,
	}))

	lease.BeginHold()
	lease.MarkReady()
	lm.Start()
	time.Sleep(80 * time.Millisecond)
	assert.False(t, ts.IsTombstone(overdue),
		"§5.8: an active registration barrier pauses the first pass even past the ready gate")

	lease.EndHold()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !ts.IsTombstone(overdue) {
		time.Sleep(10 * time.Millisecond)
	}
	assert.True(t, ts.IsTombstone(overdue),
		"§5.8: forgetting resumes once every in-flight inventory has ended its hold")
}

// countingGuard records the barrier calls a spill rebuild is required to make.
type countingGuard struct {
	begins, ends, protects int
}

func (g *countingGuard) ProtectKey(int64) { g.protects++ }
func (g *countingGuard) ReleaseKey(int64) {}
func (g *countingGuard) ArmRetention()    {}
func (g *countingGuard) BeginHold()       { g.begins++ }
func (g *countingGuard) EndHold()         { g.ends++ }

// TestMemSpill_ProtectAllPendingRunsUnderBarrier 钉住 registers
//
// 契约: docs/wiki/memory/memory-architecture.md#error-tracking
func TestMemSpill_ProtectAllPendingRunsUnderBarrier(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/spill.jsonl"
	sp := NewMemSpill(path)
	require.NoError(t, sp.Append(42, FullEvent{EventKey: 42}))
	g := &countingGuard{}
	sp.SetGuard(g)

	require.NoError(t, sp.ProtectAllPending())
	assert.Equal(t, 1, g.begins, "the rebuild raises the registration barrier")
	assert.Equal(t, 1, g.ends, "and releases it exactly once on success")
	assert.Equal(t, 1, g.protects, "the pending key is protected inside the window")
}

// TestRetentionLease_TTLProtectsUnackedOriginal 钉住 core: a retained (unacked-recovery) original must survive TTL expiry while its
//
// 契约: docs/wiki/memory/memory-architecture.md#error-tracking
func TestRetentionLease_TTLProtectsUnackedOriginal(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, rel, ":memory:", 100)
	require.NoError(t, err)
	ts := NewTombstoneSet(rel, mockKV, 1)
	store.tombstones = ts
	lease := NewRetentionLease()
	store.SetRetentionLease(lease)
	lm := NewLifecycleManager(store, ts, DefaultLifecycleConfig())

	now := time.Now().UnixMilli()
	protectedOverdue := NewSnowflakeEventKey(1, now-10*24*3600*1000)
	require.NoError(t, store.StoreEvent(protectedOverdue, FullEvent{
		EventKey: protectedOverdue, PartitionID: 1, EventType: "thinking_plan",
		EventSummary: "unacked prepared fact", Timestamp: now - 10*24*3600*1000,
	}))
	lease.Protect(protectedOverdue)

	unprotectedOverdue := NewSnowflakeEventKey(1, now-11*24*3600*1000)
	require.NoError(t, store.StoreEvent(unprotectedOverdue, FullEvent{
		EventKey: unprotectedOverdue, PartitionID: 1, EventType: "thinking_plan",
		EventSummary: "already acked", Timestamp: now - 11*24*3600*1000,
	}))

	lm.checkTTL()
	assert.False(t, ts.IsTombstone(protectedOverdue),
		"§2.8: a retained original must NOT be expired by TTL while its lease is held")
	assert.True(t, ts.IsTombstone(unprotectedOverdue),
		"an unleased overdue event must still expire")
	_, err = store.GetEvent(protectedOverdue)
	require.NoError(t, err, "protected original must stay readable for recovery")

	lease.Release(protectedOverdue)
	lm.checkTTL()
	assert.True(t, ts.IsTombstone(protectedOverdue),
		"§2.8: after release the key resumes age-based expiry on its ORIGINAL timestamp")
}

// TestRetentionLease_DeleteRefusedWhileProtected 钉住 core: an explicit delete of a protected key is refused (returns ErrEventProtected)
//
// 契约: docs/wiki/memory/memory-architecture.md#error-tracking
func TestRetentionLease_DeleteRefusedWhileProtected(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, rel, ":memory:", 100)
	require.NoError(t, err)
	ts := NewTombstoneSet(rel, mockKV, 1)
	store.tombstones = ts
	lease := NewRetentionLease()
	store.SetRetentionLease(lease)

	key := NewSnowflakeEventKey(1, time.Now().UnixMilli())
	require.NoError(t, store.StoreEvent(key, FullEvent{
		EventKey: key, PartitionID: 1, EventType: "external_input",
		EventSummary: "receipt/original", Timestamp: time.Now().UnixMilli(),
	}))

	lease.Protect(key)
	lease.Protect(key)
	assert.True(t, store.IsKeyProtected(key))

	require.True(t, IsEventProtected(store.DeleteEvent(key)),
		"§2.8: explicit delete of a retained original must return protected, not destroy")
	_, err = store.GetEvent(key)
	require.NoError(t, err, "the durable original must be intact after a refused delete")

	lease.Release(key)
	assert.True(t, store.IsKeyProtected(key))
	require.True(t, IsEventProtected(store.DeleteEvent(key)))

	lease.Release(key)
	assert.False(t, store.IsKeyProtected(key))
	require.NoError(t, store.DeleteEvent(key), "delete succeeds once the lease is fully released")
	_, err = store.GetEvent(key)
	assert.Error(t, err, "the event is gone after the released delete")
}

// TestRetentionLease_ScannerWaitsForLeaseReady 钉住 B: the background scanner must NOT destroy an overdue-but-retained original
//
// 契约: docs/wiki/memory/memory-architecture.md#error-tracking
func TestRetentionLease_ScannerWaitsForLeaseReady(t *testing.T) {
	rel := newSimpleInMemRelationStore()
	mockKV := newMockKV()
	store, err := NewFileSegmentStore(mockKV, rel, ":memory:", 100)
	require.NoError(t, err)
	ts := NewTombstoneSet(rel, mockKV, 1)
	store.tombstones = ts
	lease := NewRetentionLease()
	store.SetRetentionLease(lease)
	lm := NewLifecycleManager(store, ts, DefaultLifecycleConfig())
	defer lm.Stop()

	now := time.Now().UnixMilli()
	overdue := NewSnowflakeEventKey(1, now-10*24*3600*1000)
	require.NoError(t, store.StoreEvent(overdue, FullEvent{
		EventKey: overdue, PartitionID: 1, EventType: "thinking_plan",
		EventSummary: "unacked across restart", Timestamp: now - 10*24*3600*1000,
	}))

	lm.Start()
	time.Sleep(50 * time.Millisecond)
	assert.False(t, ts.IsTombstone(overdue),
		"§2.8: the first destructive pass must not run before the lease is armed")

	lease.MarkReady()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !ts.IsTombstone(overdue) {
		time.Sleep(10 * time.Millisecond)
	}
	assert.True(t, ts.IsTombstone(overdue),
		"§2.8: after the lease is armed the overdue key expires on its ORIGINAL timestamp")
}

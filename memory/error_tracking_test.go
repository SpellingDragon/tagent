package memory

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockSink 记录上报（验证 ErrorTrackingStore 的归因与上报）。
type mockSink struct {
	failures  []string
	successes []string
}

func (m *mockSink) ReportFailure(dep string, _ error) { m.failures = append(m.failures, dep) }
func (m *mockSink) ReportSuccess(dep string)          { m.successes = append(m.successes, dep) }

// errStore 是可注入 StoreEvent 错误的 MemoryStore（嵌入 InMemoryStore 覆盖写路径）。
type errStore struct {
	*InMemoryStore
	storeErr error
}

func (e *errStore) StoreEvent(k int64, ev FullEvent) error {
	if e.storeErr != nil {
		return e.storeErr
	}
	return e.InMemoryStore.StoreEvent(k, ev)
}

func TestErrorTrackingStore_ClassifyAndReport(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantDep string
		isFail  bool
	}{
		{"fork/exec→rustviking", errors.New("fork/exec rustviking: no such file"), "rustviking", true},
		{"executable not found→rustviking", errors.New("executable file not found in $PATH"), "rustviking", true},
		{"ENOSPC→disk", errors.New("write: no space left on device"), "disk", true},
		{"disk quota→disk", errors.New("disk quota exceeded"), "disk", true},
		{"其余→memory", errors.New("segment corrupted"), "memory", true},
		{"成功→memory 恢复", nil, "memory", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := &mockSink{}
			es := &errStore{InMemoryStore: NewInMemoryStore(), storeErr: tc.err}
			ets := NewErrorTrackingStore(es, sink)
			k := NewSnowflakeEventKey(1, testBaseMs)
			_ = ets.StoreEvent(k, FullEvent{EventKey: k, PartitionID: 1, EventType: TypeExternalInputProbe, Timestamp: testBaseMs})
			if tc.isFail {
				if len(sink.failures) != 1 || sink.failures[0] != tc.wantDep {
					t.Fatalf("失败应归因 %s, got failures=%v", tc.wantDep, sink.failures)
				}
			} else {
				if len(sink.successes) != 3 {
					t.Fatalf("写成功应上报 memory+disk+rustviking 三依赖恢复(M2), got %v", sink.successes)
				}
			}
		})
	}
}

func TestErrorTrackingStore_NilSinkPassthrough(t *testing.T) {
	inner := NewInMemoryStore()
	ets := NewErrorTrackingStore(inner, nil)
	k := NewSnowflakeEventKey(1, testBaseMs)
	if err := ets.StoreEvent(k, FullEvent{EventKey: k, PartitionID: 1, EventType: TypeExternalInputProbe, Timestamp: testBaseMs}); err != nil {
		t.Fatalf("透传 StoreEvent: %v", err)
	}
	got, err := ets.GetEvent(k)
	if err != nil || got == nil {
		t.Fatalf("透传 GetEvent: got=%v err=%v", got, err)
	}
}

func TestErrorTrackingStore_ReadPathReportsOnlyOnError(t *testing.T) {
	sink := &mockSink{}
	ets := NewErrorTrackingStore(NewInMemoryStore(), sink)
	if _, err := ets.QueryEvents(QueryOptions{PartitionIDs: []int{1}}); err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(sink.failures) != 0 || len(sink.successes) != 0 {
		t.Fatalf("读成功不应上报, got failures=%v successes=%v", sink.failures, sink.successes)
	}
}

// TestErrorTrackingStore_VectorStubNotReported 钉住 是 回归：未配引擎时 SearchByEmbedding 恒返回
//
// 契约: docs/wiki/memory/memory-architecture.md#error-tracking
func TestErrorTrackingStore_VectorStubNotReported(t *testing.T) {
	sink := &mockSink{}
	ets := NewErrorTrackingStore(NewInMemoryStore(), sink)
	_, _ = ets.SearchByEmbedding([]float32{0.1, 0.2}, 5)
	if len(sink.failures) != 0 {
		t.Fatalf("能力 stub 错误(ErrVectorSearchNotSupported)不应上报失败(S1), got %v", sink.failures)
	}
}

// TestClassifyStoreErr_DiskBeforeRustviking 钉住 是 回归：disk 特征先判 + rustviking 收窄到
//
// 契约: docs/wiki/memory/memory-architecture.md#error-tracking
func TestClassifyStoreErr_DiskBeforeRustviking(t *testing.T) {
	if got := classifyStoreErr(errors.New("rustviking kv put: no space left on device")); got != depDisk {
		t.Fatalf("ENOSPC(含rustviking字样)应归 disk(S3), got %s", got)
	}
	if got := classifyStoreErr(errors.New("fork/exec /usr/bin/rustviking: permission denied")); got != depRustViking {
		t.Fatalf("fork/exec 应归 rustviking, got %s", got)
	}
	if got := classifyStoreErr(errors.New("rustviking: index corrupted")); got != depMemory {
		t.Fatalf("泛 rustviking 业务错误应归 memory(S3收窄), got %s", got)
	}
}

type faultPath string

const (
	pathNone   faultPath = ""
	pathNormal faultPath = "store"
	pathReplay faultPath = "replay"
)

type faultPhase string

const (
	phaseInput   faultPhase = "input"
	phaseRead    faultPhase = "read"
	phaseWrite   faultPhase = "write"
	phaseReceipt faultPhase = "receipt"
	phaseSync    faultPhase = "sync"
)

type faultKey struct {
	path  faultPath
	phase faultPhase
}

// faultKV embeds *mockKV (inheriting KVStore + optional Sync/ListPartitionIDs
// capabilities) and overlays per-(path,phase) arming, a receipt classifier and
// an ordered op spy tagged with the active commit path.
type faultKV struct {
	*mockKV
	mu      sync.Mutex
	cur     faultPath
	arms    map[faultKey]error
	putPred func(key string) faultPhase
	spy     []string
}

func newFaultKV() *faultKV {
	return &faultKV{
		mockKV: newMockKV(),
		arms:   map[faultKey]error{},
		putPred: func(string) faultPhase {
			return phaseWrite
		},
	}
}

func (f *faultKV) setPath(p faultPath) {
	f.mu.Lock()
	f.cur = p
	f.mu.Unlock()
}

func (f *faultKV) curPath() faultPath {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cur
}

// arm injects err for one (path, phase) so ordinary write and replay are armed
// independently.
func (f *faultKV) arm(p faultPath, phase faultPhase, err error) {
	f.mu.Lock()
	f.arms[faultKey{p, phase}] = err
	f.mu.Unlock()
}

// classifyPut lets a test route specific KVPut keys to the receipt phase.
func (f *faultKV) classifyPut(pred func(string) faultPhase) {
	f.mu.Lock()
	f.putPred = pred
	f.mu.Unlock()
}

func (f *faultKV) phaseOfPut(key string) faultPhase {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.putPred(key)
}

func (f *faultKV) armFor(phase faultPhase) (error, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	err, ok := f.arms[faultKey{f.cur, phase}]
	return err, ok
}

func (f *faultKV) record(op string) {
	f.mu.Lock()
	f.spy = append(f.spy, op)
	f.mu.Unlock()
}

func (f *faultKV) spyLines() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.spy...)
}

func (f *faultKV) clearSpy() {
	f.mu.Lock()
	f.spy = nil
	f.mu.Unlock()
}

func (f *faultKV) opStr(phase faultPhase, key string) string {
	return string(f.curPath()) + ":" + string(phase) + ":" + key
}

func (f *faultKV) KVPut(key, value string) error {
	phase := f.phaseOfPut(key)
	f.record(f.opStr(phase, key))
	if err, ok := f.armFor(phase); ok {
		return err
	}
	return f.mockKV.KVPut(key, value)
}

func (f *faultKV) KVBatch(ops []KVOp) error {
	f.record(f.opStr(phaseWrite, "batch"))
	if err, ok := f.armFor(phaseWrite); ok {
		return err
	}
	return f.mockKV.KVBatch(ops)
}

func (f *faultKV) KVGet(key string) (string, error) {
	f.record(f.opStr(phaseRead, key))
	if err, ok := f.armFor(phaseRead); ok {
		return "", err
	}
	return f.mockKV.KVGet(key)
}

func (f *faultKV) KVScan(prefix string, limit int) ([]KVPair, error) {
	f.record(f.opStr(phaseRead, "scan:"+prefix))
	if err, ok := f.armFor(phaseRead); ok {
		return nil, err
	}
	return f.mockKV.KVScan(prefix, limit)
}

func (f *faultKV) KVRange(start, end string, limit int) ([]KVPair, error) {
	f.record(f.opStr(phaseRead, "range"))
	if err, ok := f.armFor(phaseRead); ok {
		return nil, err
	}
	return f.mockKV.KVRange(start, end, limit)
}

// Sync overrides the optional durability-barrier capability so a test can arm a
// barrier failure per commit path and prove the commit actually reached it.
func (f *faultKV) Sync() error {
	f.record(f.opStr(phaseSync, ""))
	if err, ok := f.armFor(phaseSync); ok {
		return err
	}
	return f.mockKV.Sync()
}

// faultStore tags the active commit path on the shared faultKV and enforces the
// "input" stage before any inner call. It delegates the durable commit to a real
// FileSegmentStore built on the faultKV, so read/write/sync faults fire exactly
// where the production commit path performs them.
type faultStore struct {
	inner *FileSegmentStore
	kv    *faultKV
}

func newFaultStore(t *testing.T) *faultStore {
	t.Helper()
	kv := newFaultKV()
	base, err := NewFileSegmentStore(kv, nil, ":memory:", 100)
	require.NoError(t, err)
	tset := NewTombstoneSet(base.rel, kv, 0)
	require.NoError(t, tset.RecoverFromKV())
	base.SetTombstoneSet(tset)
	return &faultStore{inner: base, kv: kv}
}

func (s *faultStore) StoreEvent(key int64, event FullEvent) error {
	s.kv.setPath(pathNormal)
	defer s.kv.setPath(pathNone)
	if err, ok := s.kv.armFor(phaseInput); ok {
		s.kv.record(string(pathNormal) + ":" + string(phaseInput))
		return err
	}
	return s.inner.StoreEvent(key, event)
}

func (s *faultStore) ReplayEvent(key int64, canonical FullEvent) (ReplayResult, FullEvent, error) {
	s.kv.setPath(pathReplay)
	defer s.kv.setPath(pathNone)
	if err, ok := s.kv.armFor(phaseInput); ok {
		s.kv.record(string(pathReplay) + ":" + string(phaseInput))
		return ReplayNew, canonical, err
	}
	return s.inner.ReplayEvent(key, canonical)
}

// countSpy returns how many spy entries carry the given path:phase prefix.
func countSpy(lines []string, prefix string) int {
	n := 0
	for _, l := range lines {
		if len(l) >= len(prefix) && l[:len(prefix)] == prefix {
			n++
		}
	}
	return n
}

func commitEvent(key int64, pid int, content string) FullEvent {
	return FullEvent{
		EventKey: key, PartitionID: pid, EventType: "external_input",
		EventSummary: "s", Content: content, Timestamp: 1700000000000,
	}
}

// TestFaultDouble_PerPathWriteArming 钉住 proves an armed write fault fires on the
//
// 契约: docs/wiki/memory/memory-architecture.md#error-tracking
func TestFaultDouble_PerPathWriteArming(t *testing.T) {
	s := newFaultStore(t)
	boom := errors.New("injected write")
	s.kv.arm(pathReplay, phaseWrite, boom)

	k1 := NewSnowflakeEventKey(1, 0)
	require.NoError(t, s.StoreEvent(k1, commitEvent(k1, 1, "normal")),
		"ordinary write must succeed when only the replay path is armed")

	s.kv.clearSpy()
	k2 := NewSnowflakeEventKey(2, 0)
	_, _, err := s.ReplayEvent(k2, commitEvent(k2, 2, "replay"))
	require.ErrorIs(t, err, boom, "armed replay-write fault must be consumed by ReplayEvent")

	spy := s.kv.spyLines()
	require.Equal(t, 0, countSpy(spy, "store:"), "no store-path ops during a replay call")
	require.GreaterOrEqual(t, countSpy(spy, "replay:write"), 1,
		"spy must observe the replay commit attempt a write (proves the fault hit the replay write path)")
}

// TestFaultDouble_BarrierReachedByBothPaths 钉住 proves both commit entry points
//
// 契约: docs/wiki/memory/memory-architecture.md#error-tracking
func TestFaultDouble_BarrierReachedByBothPaths(t *testing.T) {
	s := newFaultStore(t)

	s.kv.clearSpy()
	k := NewSnowflakeEventKey(1, 0)
	require.NoError(t, s.StoreEvent(k, commitEvent(k, 1, "c")))
	require.GreaterOrEqual(t, countSpy(s.kv.spyLines(), "store:sync"), 1,
		"ordinary commit must reach the Sync barrier")

	s.kv.clearSpy()
	k2 := NewSnowflakeEventKey(2, 0)
	res, _, err := s.ReplayEvent(k2, commitEvent(k2, 2, "r"))
	require.NoError(t, err)
	require.Equal(t, ReplayNew, res)
	require.GreaterOrEqual(t, countSpy(s.kv.spyLines(), "replay:sync"), 1,
		"replay commit must ALSO reach the Sync barrier (no cache-only shortcut)")

	s2 := newFaultStore(t)
	syncBoom := errors.New("sync gone")
	s2.kv.arm(pathReplay, phaseSync, syncBoom)
	k3 := NewSnowflakeEventKey(1, 0)
	require.NoError(t, s2.StoreEvent(k3, commitEvent(k3, 1, "c")), "store path un-armed → ok")
	k4 := NewSnowflakeEventKey(2, 0)
	_, _, err = s2.ReplayEvent(k4, commitEvent(k4, 2, "r"))
	require.ErrorIs(t, err, syncBoom, "armed replay-sync must fail the replay commit")
}

// TestFaultDouble_InputStageRejectsBeforeIO 钉住 proves the input stage rejects an
//
// 契约: docs/wiki/memory/memory-architecture.md#error-tracking
func TestFaultDouble_InputStageRejectsBeforeIO(t *testing.T) {
	s := newFaultStore(t)
	inputBoom := errors.New("bad input")
	s.kv.arm(pathNormal, phaseInput, inputBoom)

	s.kv.clearSpy()
	k := NewSnowflakeEventKey(1, 0)
	err := s.StoreEvent(k, commitEvent(k, 1, "c"))
	require.ErrorIs(t, err, inputBoom)

	spy := s.kv.spyLines()
	require.Equal(t, 1, countSpy(spy, "store:input"), "input rejection must be recorded")
	require.Equal(t, 0, countSpy(spy, "store:write")+countSpy(spy, "store:sync")+countSpy(spy, "store:read"),
		"input-stage rejection must not touch the KV layer (distinguishes it from a commit failure)")
}

// TestCounter_ReplayAfterCacheEvictionClassifiesFromKV 钉住
//
// 契约: docs/wiki/memory/memory-architecture.md#error-tracking
func TestCounter_ReplayAfterCacheEvictionClassifiesFromKV(t *testing.T) {
	s := newFaultStore(t)
	key := NewSnowflakeEventKey(1, 0)
	evt := commitEvent(key, 1, "cold-replay-fact")

	require.NoError(t, s.StoreEvent(key, evt))
	require.EqualValues(t, 1, s.inner.GetStats().TotalEvents, "first commit must count exactly once")

	s.inner.cache.Remove(key)

	res, canonical, err := s.ReplayEvent(key, evt)
	require.NoError(t, err, "replay of an already-durable fact must not error on a cold cache")
	require.Equal(t, ReplayAlreadyCommitted, res,
		"§2.4: a cold-cache replay of an already-durable fact must classify AlreadyCommitted from the KV fact chain, not Repair")
	require.EqualValues(t, 1, s.inner.GetStats().TotalEvents,
		"§2.4/F8: replaying an already-committed fact must NOT re-increment the live count")
	require.Equal(t, evt.Content, canonical.Content, "replay must return the canonical stored fact verbatim")
}

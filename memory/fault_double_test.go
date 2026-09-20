package memory

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Fault-injection double (baseline task 1.3)
//
// Goal: let tests arm a commit failure SEPARATELY for the ordinary write path
// (StoreEvent) and the explicit-replay path (ReplayEvent), at the granular
// stages input/read/write/receipt/sync, and record an ordered op spy so a test
// can PROVE the fault was actually consumed on the intended code path (rather
// than silently bypassed by a fallback). §2/§5 evidence reuses this harness for
// its fail-before/pass-after assertions; the self-tests below only certify the
// harness itself is faithful.
//
// The KV seam (read/write/sync) is injected by wrapping *mockKV; the semantic
// stages above KV are reached via the store wrapper: "input" rejects before any
// inner call, "receipt" targets the receipt commit's KVPut by key predicate.
// ---------------------------------------------------------------------------

type faultPath string

const (
	pathNone   faultPath = ""
	pathNormal faultPath = "store"  // StoreEvent — ordinary write
	pathReplay faultPath = "replay" // ReplayEvent — explicit replay
)

type faultPhase string

const (
	phaseInput   faultPhase = "input"   // reject before any inner/KV op
	phaseRead    faultPhase = "read"    // KVGet/Scan/Range
	phaseWrite   faultPhase = "write"   // KVPut/KVBatch of the fact
	phaseReceipt faultPhase = "receipt" // KVPut classified as the receipt commit
	phaseSync    faultPhase = "sync"    // durability barrier
)

type faultKey struct {
	path  faultPath
	phase faultPhase
}

// faultKV embeds *mockKV (inheriting KVStore + optional Sync()/ListPartitionIDs
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

// ---------------------------------------------------------------------------
// Harness self-tests: certify per-path/per-phase arming + spy faithfulness.
// These do NOT assert §2 semantics; they prove the double cannot pass a fault
// vacuously — every armed fault must be observably consumed on its path.
// ---------------------------------------------------------------------------

// TestFaultDouble_PerPathWriteArming proves an armed write fault fires on the
// replay path while the ordinary write path stays clean, and the spy shows the
// fault was consumed by ReplayEvent (not bypassed).
func TestFaultDouble_PerPathWriteArming(t *testing.T) {
	s := newFaultStore(t)
	boom := errors.New("injected write")
	s.kv.arm(pathReplay, phaseWrite, boom)

	// Ordinary write is NOT armed → succeeds and hits write on the store path.
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

// TestFaultDouble_BarrierReachedByBothPaths proves both commit entry points
// actually drive the durability barrier (Sync), so §2/§5 can assert "replay
// still passes the barrier" and a bypass to a cache-only success is detectable.
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

	// Armed sync must fail the commit it is set for and not the other.
	s2 := newFaultStore(t)
	syncBoom := errors.New("sync gone")
	s2.kv.arm(pathReplay, phaseSync, syncBoom)
	k3 := NewSnowflakeEventKey(1, 0)
	require.NoError(t, s2.StoreEvent(k3, commitEvent(k3, 1, "c")), "store path un-armed → ok")
	k4 := NewSnowflakeEventKey(2, 0)
	_, _, err = s2.ReplayEvent(k4, commitEvent(k4, 2, "r"))
	require.ErrorIs(t, err, syncBoom, "armed replay-sync must fail the replay commit")
}

// TestFaultDouble_InputStageRejectsBeforeIO proves the input stage rejects an
// event with ZERO underlying KV ops — so a §2 test can distinguish a validation
// failure from a commit failure via the spy.
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

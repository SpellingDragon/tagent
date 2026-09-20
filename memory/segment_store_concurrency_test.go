package memory

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// TestSegmentStore_ConcurrentSameKeyAtomicCommit locks async-task-lifetime 2.2:
// the identity/collision check and the commit publication must be ONE atomic
// partition mutation. Many writers racing to commit the SAME EventKey must converge
// to exactly one committed event — one success, every other refused with
// ErrDuplicateEventKey, live-count incremented once — never a double-counted ghost
// (a second evt slot behind a single idx pointer), which would let capacity eviction
// over-delete real events. Before the per-partition mutationMu the
// probe→write→publish window was non-atomic and a gate-synced stampede could produce
// multiple successes and TotalEvents > 1.
func TestSegmentStore_ConcurrentSameKeyAtomicCommit(t *testing.T) {
	kv := newMockKV()
	defer kv.Close()
	store, err := NewFileSegmentStore(kv, nil, ":memory:", 100)
	if err != nil {
		t.Fatalf("NewFileSegmentStore: %v", err)
	}

	const pid = 7
	key := NewSnowflakeEventKey(pid, 1704067200*1000)
	evt := FullEvent{EventKey: key, PartitionID: pid, EventType: "external_input", Timestamp: 1704067200000}

	const n = 16
	var (
		wg       sync.WaitGroup
		gate     = make(chan struct{})
		okCount  atomic.Int64
		dupCount atomic.Int64
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate // every writer enters the collision-probe window simultaneously
			switch err := store.StoreEvent(key, evt); {
			case err == nil:
				okCount.Add(1)
			case errors.Is(err, ErrDuplicateEventKey):
				dupCount.Add(1)
			default:
				t.Errorf("unexpected StoreEvent error: %v", err)
			}
		}()
	}
	close(gate)
	wg.Wait()

	if got := okCount.Load(); got != 1 {
		t.Fatalf("concurrent same-key commit must yield exactly 1 winner, got %d (dups=%d)", got, dupCount.Load())
	}
	if got := dupCount.Load(); got != n-1 {
		t.Fatalf("expected %d duplicate rejections, got %d", n-1, got)
	}
	if got := store.GetStats().TotalEvents; got != 1 {
		t.Fatalf("live-count must increment exactly once (no double-counted ghost), got TotalEvents=%d", got)
	}
	if _, err := store.GetEvent(key); err != nil {
		t.Fatalf("the committed event must stay recallable: %v", err)
	}
}

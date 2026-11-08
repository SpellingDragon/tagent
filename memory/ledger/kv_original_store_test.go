// The checks of the original store every durable engine holds: a shared-body record written by an earlier format is refused by name, and the stored state loads on first access rather than at open.
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// TestOriginalStoreRefusesLegacySharedBodies pins a clean format boundary.
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestOriginalStoreRefusesLegacySharedBodies(t *testing.T) {
	ctx := context.Background()
	backing := newMemKV()
	body := []byte("shared in the old format")
	digest := digestOf(body)
	stored, err := json.Marshal([]storedRef{{Format: "text", StoredAt: time.Now(), Digest: digest, Length: int64(len(body))}})
	if err != nil {
		t.Fatalf("encode the legacy reference: %v", err)
	}
	if err := backing.Commit(ctx, Batch{Puts: []Pair{{Key: refKVKey("legacy-event"), Value: stored}, {Key: "blob:" + digest, Value: body}}}); err != nil {
		t.Fatalf("seed the legacy shared body: %v", err)
	}
	reopened, err := newKVOriginalStore(backing, nil)
	if err != nil {
		t.Fatalf("open the legacy format lazily: %v", err)
	}
	if got := reopened.Get(ctx, "legacy-event"); got.Outcome != RecallFailed || !errors.Is(got.Err, ErrSnapshotCorrupt) {
		t.Fatalf("the first legacy shared-body read = %+v, want a named refusal", got)
	}
}

// countingKV counts original-store reads without changing storage semantics.
type countingKV struct {
	inner AtomicStore
	reads int
}

func (k *countingKV) Read(ctx context.Context, fetch Fetch) ([]Pair, error) {
	k.reads++
	return k.inner.Read(ctx, fetch)
}
func (k *countingKV) Keys(ctx context.Context, prefix string) ([]string, error) {
	return k.inner.Keys(ctx, prefix)
}
func (k *countingKV) Namespaces(ctx context.Context) ([]string, error) {
	return k.inner.Namespaces(ctx)
}
func (k *countingKV) Commit(ctx context.Context, batch Batch) error {
	return k.inner.Commit(ctx, batch)
}

// TestKVOriginalStoreDefersLoadUntilFirstRead pins zero original reads at open.
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func TestKVOriginalStoreDefersLoadUntilFirstRead(t *testing.T) {
	ctx := context.Background()
	backing := newMemKV()
	first, err := newKVOriginalStore(backing, nil)
	if err != nil {
		t.Fatalf("open the seed original store: %v", err)
	}
	ref := OriginalRef{Key: "lazy-original", Format: "text"}
	body := []byte("the original text")
	if err := first.Put(ctx, ref, body); err != nil {
		t.Fatalf("commit the original: %v", err)
	}
	counted := &countingKV{inner: backing}
	reopened, err := newKVOriginalStore(counted, nil)
	if err != nil {
		t.Fatalf("open the lazy original store: %v", err)
	}
	if counted.reads != 0 {
		t.Fatalf("opening the original store performed %d reads, want zero", counted.reads)
	}
	if got := reopened.Get(ctx, ref.Key); got.Outcome != RecallFound || !bytes.Equal(got.Body, body) || got.Err != nil {
		t.Fatalf("the first lazy read = %+v, want the committed original", got)
	}
	if counted.reads == 0 {
		t.Fatal("the first read did not load the original state")
	}
}

// TestKVOriginalStoreLazyLoadRetriesAfterRepair pins retryable first-read failure.
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func TestKVOriginalStoreLazyLoadRetriesAfterRepair(t *testing.T) {
	ctx := context.Background()
	backing := newMemKV()
	first, _ := newKVOriginalStore(backing, nil)
	ref := OriginalRef{Key: "retry-original", Format: "text"}
	if err := first.Put(ctx, ref, []byte("repaired")); err != nil {
		t.Fatalf("commit the original: %v", err)
	}
	pairs, err := backing.Read(ctx, Fetch{Key: refKVKey(ref.Key)})
	if err != nil || len(pairs) != 1 {
		t.Fatalf("read the valid reference fixture: %v", err)
	}
	valid := append([]byte(nil), pairs[0].Value...)
	if err := backing.Commit(ctx, Batch{Puts: []Pair{{Key: refKVKey(ref.Key), Value: []byte("not-json")}}}); err != nil {
		t.Fatalf("corrupt the reference: %v", err)
	}
	reopened, err := newKVOriginalStore(backing, nil)
	if err != nil {
		t.Fatalf("open still loads eagerly: %v", err)
	}
	if got := reopened.Get(ctx, ref.Key); got.Outcome != RecallFailed || !errors.Is(got.Err, ErrSnapshotCorrupt) {
		t.Fatalf("the first corrupt read = %+v, want the named failure", got)
	}
	if err := backing.Commit(ctx, Batch{Puts: []Pair{{Key: refKVKey(ref.Key), Value: valid}}}); err != nil {
		t.Fatalf("repair the reference: %v", err)
	}
	if got := reopened.Get(ctx, ref.Key); got.Outcome != RecallFound || string(got.Body) != "repaired" || got.Err != nil {
		t.Fatalf("the read after repair = %+v, want a retry that finds the original", got)
	}
}

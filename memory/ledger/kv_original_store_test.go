// The checks of the original store every durable engine holds: a shared-body record written by an earlier format is refused by name, and the stored state loads on first access rather than at open.
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
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

// filteredKV records the fetches one original-store load makes, so a load that
// reads the whole store is observable as the unfiltered read it is.
type filteredKV struct {
	inner      AtomicStore
	filtered   []Fetch
	unfiltered int
}

func (k *filteredKV) Read(ctx context.Context, fetch Fetch) ([]Pair, error) {
	if fetch.Key == "" && fetch.Prefix == "" {
		k.unfiltered++
	} else {
		k.filtered = append(k.filtered, fetch)
	}
	return k.inner.Read(ctx, fetch)
}
func (k *filteredKV) Keys(ctx context.Context, prefix string) ([]string, error) {
	return k.inner.Keys(ctx, prefix)
}
func (k *filteredKV) Namespaces(ctx context.Context) ([]string, error) {
	return k.inner.Namespaces(ctx)
}
func (k *filteredKV) Commit(ctx context.Context, batch Batch) error {
	return k.inner.Commit(ctx, batch)
}

// TestKVOriginalStoreLoadReadsItsOwnEntries pins the read scope of the two loads the store states.
//   - a read that names one identity reads the body and the reference of that identity, never the original family behind it
//   - so the bytes a boot pays to recall or to commit one fact are the two entries of that identity
//   - the load that answers the whole set reads under the two prefixes it files, never the whole store
//   - the durable tier shares its keyspace with the segment store, whose event bodies neither load reads
func TestKVOriginalStoreLoadReadsItsOwnEntries(t *testing.T) {
	ctx := context.Background()
	backing := newMemKV()
	first, _ := newKVOriginalStore(backing, nil)
	ref := OriginalRef{Key: "scoped-original", Format: "text"}
	body := []byte("the original text")
	if err := first.Put(ctx, ref, body); err != nil {
		t.Fatalf("commit the original: %v", err)
	}
	beside := OriginalRef{Key: "beside-original", Format: "text"}
	if err := first.Put(ctx, beside, []byte("the original beside it")); err != nil {
		t.Fatalf("commit the original beside it: %v", err)
	}
	events := make([]Pair, 0, 64)
	for at := 0; at < 64; at++ {
		events = append(events, Pair{Key: fmt.Sprintf("0:evt:17000000%02d:%d", at, at), Value: []byte(fmt.Sprintf("body %d", at))})
	}
	if err := backing.Commit(ctx, Batch{Puts: events}); err != nil {
		t.Fatalf("seed the segment events: %v", err)
	}
	filtered := &filteredKV{inner: backing}
	reopened, err := newKVOriginalStore(filtered, nil)
	if err != nil {
		t.Fatalf("open the original store: %v", err)
	}
	if got := reopened.Get(ctx, ref.Key); got.Outcome != RecallFound || !bytes.Equal(got.Body, body) {
		t.Fatalf("the point read = %+v, want the committed original", got)
	}
	if filtered.unfiltered != 0 {
		t.Fatalf("the point read read the whole store %d time(s), want the two entries of one identity", filtered.unfiltered)
	}
	keys := make([]string, 0, len(filtered.filtered))
	for _, fetch := range filtered.filtered {
		if fetch.Prefix != "" {
			t.Fatalf("the read that names one identity read under the prefix %q, want the two keys of that identity", fetch.Prefix)
		}
		keys = append(keys, fetch.Key)
	}
	sort.Strings(keys)
	if got, want := strings.Join(keys, ","), blobKVKey(ref.Key)+","+refKVKey(ref.Key); got != want {
		t.Fatalf("the point read read %q, want %q: the two entries of the identity it names, never the original the store holds beside it", got, want)
	}

	reopened.BlobCount()
	if filtered.unfiltered != 0 {
		t.Fatalf("the whole-set load read the whole store %d time(s), want the two prefixes it files", filtered.unfiltered)
	}
	prefixes := make([]string, 0, len(filtered.filtered)-len(keys))
	for _, fetch := range filtered.filtered[len(keys):] {
		prefixes = append(prefixes, fetch.Prefix)
	}
	sort.Strings(prefixes)
	if got, want := strings.Join(prefixes, ","), "blob:,ref:"; got != want {
		t.Fatalf("the whole-set load read under %q, want %q", got, want)
	}
}

// orderedStore records the commits a store makes, in order, so a check can name
// the one commit that carried both the body and the reference of an original.
type orderedStore struct {
	inner AtomicStore
	mu    sync.Mutex
	// carried lists the keys of each commit, in commit order.
	carried [][]string
}

// Read passes the read to the wrapped engine.
func (s *orderedStore) Read(ctx context.Context, fetch Fetch) ([]Pair, error) {
	return s.inner.Read(ctx, fetch)
}

// Keys enumerates the wrapped engine's keys.
func (s *orderedStore) Keys(ctx context.Context, prefix string) ([]string, error) {
	return s.inner.Keys(ctx, prefix)
}

// Namespaces enumerates the wrapped engine's namespaces.
func (s *orderedStore) Namespaces(ctx context.Context) ([]string, error) {
	return s.inner.Namespaces(ctx)
}

// Commit records the keys of the batch and then commits it.
func (s *orderedStore) Commit(ctx context.Context, batch Batch) error {
	keys := make([]string, 0, len(batch.Puts)+len(batch.Deletes))
	for _, put := range batch.Puts {
		keys = append(keys, put.Key)
	}
	keys = append(keys, batch.Deletes...)
	s.mu.Lock()
	s.carried = append(s.carried, keys)
	s.mu.Unlock()
	return s.inner.Commit(ctx, batch)
}

// carriesOne reports whether one commit wrote a key carrying the prefix.
func carriesOne(keys []string, prefix string) bool {
	for _, key := range keys {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// TestOriginalStoreCommitsBodyAndReferenceInOneBatch pins the commit unit of one original.
//
//   - one Put commits the body and the reference that names it in the same batch, so no reader ever reaches a reference whose body is not stored beside it
//   - a commit that fails leaves neither behind: no body no reference names, and no reference naming a missing body
//   - the original reads back after the process reopens the store
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestOriginalStoreCommitsBodyAndReferenceInOneBatch(t *testing.T) {
	ctx := context.Background()
	for _, engine := range contractEngines() {
		t.Run(engine.name, func(t *testing.T) {
			t.Run("one_commit_carries_body_and_reference", func(t *testing.T) {
				root := t.TempDir()
				ordered := &orderedStore{inner: engine.openAt(t, root)}
				store, err := newKVOriginalStore(ordered, nil)
				if err != nil {
					t.Fatalf("open the original store: %v", err)
				}
				ref := OriginalRef{Key: "single-batch", Owner: event.Address{AgentID: "agent-a", SessionID: "session-a"}, Format: "text"}
				body := []byte("the original text")
				if err := store.Put(ctx, ref, body); err != nil {
					t.Fatalf("commit the original: %v", err)
				}
				if carried := len(ordered.carried); carried != 1 {
					t.Fatalf("the Put made %d commit(s), want the one batch carrying the body and its reference", carried)
				}
				if keys := ordered.carried[0]; !carriesOne(keys, "blob:") || !carriesOne(keys, "ref:") {
					t.Fatalf("the one commit carried %v, want the body beside the reference that names it", keys)
				}
				stored, held := readOne(t, ordered, blobKVKey(ref.Key))
				if !held || !bytes.Equal(stored, body) {
					t.Fatalf("the stored body = (%q, %v), want the committed bytes", stored, held)
				}
				reopened, err := newKVOriginalStore(engine.openReaderAt(t, root), nil)
				if err != nil {
					t.Fatalf("reopen the original store: %v", err)
				}
				if got := reopened.Get(ctx, ref.Key); got.Outcome != RecallFound || !bytes.Equal(got.Body, body) {
					t.Fatalf("the reopened read = %+v, want the committed original", got)
				}
			})

			t.Run("failed_commit_leaves_neither_body_nor_reference", func(t *testing.T) {
				root := t.TempDir()
				faults := &faultStore{inner: engine.openAt(t, root)}
				store, err := newKVOriginalStore(faults, nil)
				if err != nil {
					t.Fatalf("open the original store: %v", err)
				}
				ref := OriginalRef{Key: "single-batch-failed", Format: "text"}
				body := []byte("the body whose reference never landed")
				faults.setCommitFault(func(batch Batch) error {
					for _, put := range batch.Puts {
						if strings.HasPrefix(put.Key, "ref:") {
							return fmt.Errorf("%w: injected on the commit that carries the reference", ErrCommitUncertain)
						}
					}
					return nil
				})
				if err := store.Put(ctx, ref, body); !errors.Is(err, ErrCommitBarrier) {
					t.Fatalf("the original whose commit failed returned %v, want the named commit barrier", err)
				}
				if stored, held := readOne(t, faults, blobKVKey(ref.Key)); held {
					t.Fatalf("the body of the failed commit is stored as %q, want nothing beside a reference that never landed", stored)
				}
				if got := store.Get(ctx, ref.Key); got.Outcome != RecallMissing {
					t.Fatalf("the original beside no reference reads %+v, want it unreadable", got)
				}
				faults.setCommitFault(nil)
				reopened, err := newKVOriginalStore(engine.openReaderAt(t, root), nil)
				if err != nil {
					t.Fatalf("reopen the original store: %v", err)
				}
				if got := reopened.Get(ctx, ref.Key); got.Outcome != RecallMissing {
					t.Fatalf("a reopened process reads the failed commit as %+v, want it unreadable", got)
				}
			})
		})
	}
}

// TestCommitFactReadsTheOriginalItNames pins what one whole-fact commit reads of the original family.
//   - the commit reads the body and the reference of the identity it commits, two point reads of that identity
//   - it loads no whole original family, so the originals a chain already committed are never read back by a commit beside them
//   - the identity reads are counted, so the zero above is a commit that named what it committed rather than one that read no original
func TestCommitFactReadsTheOriginalItNames(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	tier, err := OpenTier(ctx, StorageConfig{Type: StorageLocalFile, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the tier of the fixture: %v", err)
	}
	defer func() {
		if err := tier.Close(); err != nil {
			t.Fatalf("close the tier of the fixture: %v", err)
		}
	}()
	originals, held := tier.Originals.(*kvOriginalStore)
	if !held {
		t.Fatalf("the durable tier holds the originals %T, want the store this check measures", tier.Originals)
	}
	beside := FactCommit{
		Record: SegmentRecord{PartitionID: 1, WindowTS: 1700000000000, Seq: 1, EventKey: "beside-event", EventTime: 1700000000000, Body: []byte("beside body")}.WithChain("answer", "session-originals"),
		Ref:    OriginalRef{Key: "beside-original", Format: "text"},
		Body:   []byte("the original the chain already holds"),
	}
	if outcome, err := CommitFact(ctx, tier, beside); err != nil || outcome != FactCommitted {
		t.Fatalf("the commit of the original beside it = (%v, %v), want a committed fact", outcome, err)
	}
	before, _, _ := originals.OriginalsReads()
	fact := FactCommit{
		Record: SegmentRecord{PartitionID: 1, WindowTS: 1700000000000, Seq: 2, EventKey: "named-event", EventTime: 1700000000000, Body: []byte("named body")}.WithChain("answer", "session-originals"),
		Ref:    OriginalRef{Key: "named-original", Format: "text"},
		Body:   []byte("the original the commit names"),
	}
	if outcome, err := CommitFact(ctx, tier, fact); err != nil || outcome != FactCommitted {
		t.Fatalf("the commit of the named original = (%v, %v), want a committed fact", outcome, err)
	}
	loads, _, keys := originals.OriginalsReads()
	if loads != before {
		t.Fatalf("the commit loaded the whole original family %d time(s), want none beside the %d it started from: the originals the chain already holds stay unread", loads-before, before)
	}
	if keys == 0 {
		t.Fatal("the commit read no key of the identity it committed, so the zero above states a commit that read no original")
	}
}

// TestCommitReadsTheOriginalOfItsOwnIdentityAtEveryDepth pins what one whole-fact commit reads as the original family grows.
//   - the commit reads the body and the reference of the identity it commits at every depth
//   - the keys one commit reads are bounded by that identity and never by the family beside it
//   - it loads no whole original family at any depth, which is where a commit that read the family would pay for every original the chain holds
//   - the family is the denominator: it holds one more identity a generation, so the constant read is a bound and not a chain that stood still
//   - the whole-store load is still reachable and measured, so the zero above is a commit that named its identity rather than a counter nothing drives
func TestCommitReadsTheOriginalOfItsOwnIdentityAtEveryDepth(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	tier, err := OpenTier(ctx, StorageConfig{Type: StorageLocalFile, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the tier of the fixture: %v", err)
	}
	defer func() {
		if err := tier.Close(); err != nil {
			t.Fatalf("close the tier of the fixture: %v", err)
		}
	}()
	originals, held := tier.Originals.(*kvOriginalStore)
	if !held {
		t.Fatalf("the durable tier holds the originals %T, want the store this check measures", tier.Originals)
	}
	const generations = 24
	identityReads := make([]int64, 0, generations)
	wholeSetLoads := make([]int, 0, generations)
	wholeSetFetched := make([]int64, 0, generations)
	for generation := 0; generation < generations; generation++ {
		beforeLoads, beforePairs, beforeKeys := originals.OriginalsReads()
		fact := FactCommit{
			Record: SegmentRecord{
				PartitionID: 1, WindowTS: 1700000000000, Seq: int64(generation + 1),
				EventKey: event.EventID(fmt.Sprintf("depth-event-%02d", generation)), EventTime: 1700000000000,
				Body: []byte("the body of the fact committed at this depth of the chain"),
			}.WithChain("answer", "session-original-depth"),
			Ref:  OriginalRef{Key: event.EventID(fmt.Sprintf("depth-original-%02d", generation)), Format: "text"},
			Body: []byte("the original the commit of this depth names"),
		}
		if outcome, err := CommitFact(ctx, tier, fact); err != nil || outcome != FactCommitted {
			t.Fatalf("the commit at depth %d = (%v, %v), want a committed fact", generation, outcome, err)
		}
		afterLoads, afterPairs, afterKeys := originals.OriginalsReads()
		wholeSetLoads = append(wholeSetLoads, afterLoads-beforeLoads)
		wholeSetFetched = append(wholeSetFetched, afterPairs-beforePairs)
		identityReads = append(identityReads, afterKeys-beforeKeys)
	}
	series := fmt.Sprintf("loads=%v fetched=%v keys=%v", wholeSetLoads, wholeSetFetched, identityReads)
	for generation := range identityReads {
		if wholeSetLoads[generation] != 0 {
			t.Fatalf("the commit at depth %d loaded the whole original family %d time(s), want none: the commit reads the identity it commits and never the originals the chain already holds (%s)",
				generation, wholeSetLoads[generation], series)
		}
		if wholeSetFetched[generation] != 0 {
			t.Fatalf("the commit at depth %d fetched %d record(s) of the whole original family, want none: the family the chain holds is read by the identity that names it and never whole (%s)",
				generation, wholeSetFetched[generation], series)
		}
		if identityReads[generation] != 2 {
			t.Fatalf("the commit at depth %d read %d key(s) of the original family, want the body and the reference of the identity it commits (%s)",
				generation, identityReads[generation], series)
		}
	}
	family, err := tier.Store.Keys(ctx, "blob:")
	if err != nil {
		t.Fatalf("count the originals the chain holds: %v", err)
	}
	if len(family) != generations {
		t.Fatalf("the original family holds %d identit(ies) beside the %d generations committed, want one a generation: the bound above is taken over a family that grew",
			len(family), generations)
	}
	if counted := originals.BlobCount(); counted != generations {
		t.Fatalf("the whole-store read answers %d identit(ies) of the %d the family holds, want the whole family: the load face is not measured", counted, generations)
	}
	if wholeSet, fetched, _ := originals.OriginalsReads(); wholeSet != 1 || fetched != 2*generations {
		t.Fatalf("the whole-store read loaded the family %d time(s) and fetched %d record(s), want the one load of the %d records the family holds: a load the counter never sees makes the zero above a zero nothing measures",
			wholeSet, fetched, 2*generations)
	}
}

// originalReadEntrypoint is one of the read faces a caller reaches an original
// through, as the pair of shapes one face offers: a whole read that classifies
// its own outcome, and a segmented read that names its refusals as errors and
// assembles back to the stored bytes.
type originalReadEntrypoint struct {
	// name tells the faces apart in a failure.
	name string
	// whole answers the faces that return the original whole, and is nil for a
	// segmented face.
	whole func(key event.EventID) RecallResult
	// segments answers the segmented faces: the original assembled from every
	// segment the face served, or the error it named.
	segments func(key event.EventID) ([]byte, error)
}

// originalReadEntrypointsOf answers the six original read entry points over one
// binding: OriginalStore.Get and ReadSegment, ScopedOriginalStore.GetScoped and
// ReadSegmentScoped read through the interface the recall port forwards to, and
// ScopedReader.Get and ReadSegment of the reader bound to scope.
func originalReadEntrypointsOf(t *testing.T, original OriginalStore, scoped ScopedOriginalStore, scope ReadScope) []originalReadEntrypoint {
	t.Helper()
	reader, err := NewScopedReader(scoped, scope)
	if err != nil {
		t.Fatalf("bind the reader to the scope %+v: %v", scope, err)
	}
	return []originalReadEntrypoint{
		{
			name:  "OriginalStore.Get",
			whole: func(key event.EventID) RecallResult { return original.Get(context.Background(), key) },
		},
		{
			name: "OriginalStore.ReadSegment",
			segments: func(key event.EventID) ([]byte, error) {
				var out []byte
				for line := 1; ; line++ {
					whole, err := original.ReadSegment(context.Background(), key, line, 1)
					if errors.Is(err, ErrSegmentPastEnd) {
						return out, nil
					}
					if err != nil {
						return nil, err
					}
					out = append(out, whole...)
				}
			},
		},
		{
			name:  "ScopedOriginalStore.GetScoped",
			whole: func(key event.EventID) RecallResult { return scoped.GetScoped(context.Background(), key, scope) },
		},
		{
			name: "ScopedOriginalStore.ReadSegmentScoped",
			segments: func(key event.EventID) ([]byte, error) {
				return assembleSegments(func(from SegmentPosition) (SegmentResult, error) {
					return scoped.ReadSegmentScoped(context.Background(), key, scope, SegmentRequest{From: from, MaxBytes: 8})
				})
			},
		},
		{
			name:  "ScopedReader.Get",
			whole: func(key event.EventID) RecallResult { return reader.Get(context.Background(), key) },
		},
		{
			name: "ScopedReader.ReadSegment",
			segments: func(key event.EventID) ([]byte, error) {
				return assembleSegments(func(from SegmentPosition) (SegmentResult, error) {
					return reader.ReadSegment(context.Background(), key, SegmentRequest{From: from, MaxBytes: 8})
				})
			},
		},
	}
}

// assembleSegments reads segments through read until it reaches the end of the
// original and answers them concatenated. A segment that does not advance the
// position is reported instead of read again, so a bound that never moves is a
// named failure rather than an endless read.
func assembleSegments(read func(from SegmentPosition) (SegmentResult, error)) ([]byte, error) {
	var out []byte
	from := SegmentPosition{}
	for {
		segment, err := read(from)
		if err != nil {
			return nil, err
		}
		out = append(out, segment.Body...)
		if segment.EOF {
			return out, nil
		}
		if segment.Next.Offset <= from.Offset {
			return nil, fmt.Errorf("the segment at byte %d did not advance: next is byte %d", from.Offset, segment.Next.Offset)
		}
		from = segment.Next
	}
}

// TestOriginalReadEntrypointsPreserveBytesAndScope pins the original read entry points a caller reaches an original through.
//   - every entry point answers the bytes Put accepted, byte for byte: the segmented faces cut at byte boundaries and assemble back to the body
//   - the line-bounded face answers whole lines, so the last line it serves is the one before the bound it names
//   - the line face is checked against the standard library's splitter, so the lines it serves are the lines the bytes hold
//   - a scope that registered no reference reads the key as missing while the bare key face beside it still answers
//   - every authorized read therefore runs through the scoped faces, never the face that cannot decide visibility
//   - a second scope the store accepted beside the first reads the same body, so the refusal names the unregistered scope and not the key
//   - an uncommitted key reads as missing and a reference past the window of its policy as expired, each on every face
//   - altered bytes read as an integrity mismatch and an unreadable reference record as a corrupt snapshot, each on every face
//   - the whole faces answer a refusal as an outcome and the segmented faces return the error that outcome carries
//   - a policy registered with a zero window holds no reference readable, so the expiry fixture is stored state and never a race against a clock
//   - each class of fixture is seeded into a state of its own, because the scoped faces load the whole original family
//   - the damaged fixture is committed state: the body is replaced behind the reference record that still names the integrity of the bytes it was committed with
//   - the tier hands its originals out as a value satisfying ScopedOriginalStore, the port the assembled run binds the recall read face through
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestOriginalReadEntrypointsPreserveBytesAndScope(t *testing.T) {
	ctx := context.Background()
	// The body carries a multi-byte character inside a line and no trailing
	// newline, so a byte-bounded segment cuts inside a character and the
	// line-bounded face ends on a last line the split never sees a newline for.
	const body = "第一行 the first line\n第二行 breaks a character\nthird line"
	scope := ReadScope{Owner: event.Address{AgentID: "agent-a", SessionID: "session-a"}, Format: "text", RetentionPolicy: "durable"}
	beside := ReadScope{Owner: event.Address{AgentID: "agent-b", SessionID: "session-b"}, Format: "text", RetentionPolicy: "durable"}
	outside := ReadScope{Owner: event.Address{AgentID: "agent-c", SessionID: "session-c"}, Format: "text", RetentionPolicy: "durable"}
	const (
		scopedKey  event.EventID = "0a01"
		expiredKey event.EventID = "0e05"
		absentKey  event.EventID = "0b03"
	)

	windows := map[string]time.Duration{"transient": 0}
	scopedRef := OriginalRef{Key: scopedKey, Owner: scope.Owner, Format: scope.Format, RetentionPolicy: scope.RetentionPolicy}
	besideRef := OriginalRef{Key: scopedKey, Owner: beside.Owner, Format: beside.Format, RetentionPolicy: beside.RetentionPolicy}
	expiredRef := OriginalRef{Key: expiredKey, Owner: scope.Owner, Format: scope.Format, RetentionPolicy: "transient"}

	seed := func(refs ...OriginalRef) (*kvOriginalStore, AtomicStore) {
		t.Helper()
		backing := newMemKV()
		writer, err := newKVOriginalStore(backing, windows)
		if err != nil {
			t.Fatalf("open the seeding original store: %v", err)
		}
		for _, ref := range refs {
			if err := writer.Put(ctx, ref, []byte(body)); err != nil {
				t.Fatalf("commit the original of %q under %+v: %v", ref.Key, ref.Owner, err)
			}
		}
		reader, err := newKVOriginalStore(backing, windows)
		if err != nil {
			t.Fatalf("open the reading original store: %v", err)
		}
		return reader, backing
	}

	expectFound := func(t *testing.T, entry originalReadEntrypoint, key event.EventID) {
		t.Helper()
		if entry.whole != nil {
			got := entry.whole(key)
			if got.Outcome != RecallFound || got.Err != nil || !bytes.Equal(got.Body, []byte(body)) {
				t.Fatalf("%s(%q) = %+v, want the committed bytes", entry.name, key, got)
			}
			return
		}
		assembled, err := entry.segments(key)
		if err != nil || !bytes.Equal(assembled, []byte(body)) {
			t.Fatalf("%s(%q) = (%q, %v), want the committed bytes assembled from its segments", entry.name, key, assembled, err)
		}
	}
	expectRefusal := func(t *testing.T, entry originalReadEntrypoint, key event.EventID, outcome RecallOutcome, named error) {
		t.Helper()
		if entry.whole != nil {
			got := entry.whole(key)
			if got.Outcome != outcome {
				t.Fatalf("%s(%q) = %+v, want the outcome %v", entry.name, key, got, outcome)
			}
			if outcome == RecallFailed && !errors.Is(got.Err, named) {
				t.Fatalf("%s(%q) carries %v, want the named refusal %v", entry.name, key, got.Err, named)
			}
			if outcome != RecallFailed && got.Err != nil {
				t.Fatalf("%s(%q) answers the outcome %v carrying %v, want no cause beside it", entry.name, key, outcome, got.Err)
			}
			if len(got.Body) != 0 {
				t.Fatalf("%s(%q) returned %q beside the refusal %v, want no original", entry.name, key, got.Body, outcome)
			}
			return
		}
		assembled, err := entry.segments(key)
		if !errors.Is(err, named) || len(assembled) != 0 {
			t.Fatalf("%s(%q) = (%q, %v), want the named refusal %v", entry.name, key, assembled, err, named)
		}
	}

	t.Run("every_entry_point_answers_the_committed_bytes", func(t *testing.T) {
		original, _ := seed(scopedRef, besideRef)
		// The scoped faces are read through the interface, which is the dispatch
		// the recall port forwards to the store through.
		var scoped ScopedOriginalStore = original
		for _, entry := range originalReadEntrypointsOf(t, original, scoped, scope) {
			expectFound(t, entry, scopedKey)
		}
		for at, want := range strings.SplitAfter(body, "\n") {
			got, err := original.ReadSegment(ctx, scopedKey, at+1, 1)
			if err != nil || string(got) != want {
				t.Fatalf("ReadSegment(%q, line %d, 1) = (%q, %v), want the line %q", scopedKey, at+1, got, err, want)
			}
		}
		if _, err := original.ReadSegment(ctx, scopedKey, len(strings.SplitAfter(body, "\n"))+1, 1); !errors.Is(err, ErrSegmentPastEnd) {
			t.Fatalf("ReadSegment(%q) past its last line = %v, want the named refusal %v", scopedKey, err, ErrSegmentPastEnd)
		}
	})

	t.Run("a_scope_that_registered_no_reference_reads_as_missing", func(t *testing.T) {
		original, _ := seed(scopedRef, besideRef)
		var scoped ScopedOriginalStore = original
		authorized := originalReadEntrypointsOf(t, original, scoped, outside)
		for _, entry := range authorized[2:] {
			expectRefusal(t, entry, scopedKey, RecallMissing, ErrOriginalMissing)
		}
		for _, entry := range authorized[:2] {
			expectFound(t, entry, scopedKey)
		}
		for _, entry := range originalReadEntrypointsOf(t, original, scoped, beside)[2:] {
			expectFound(t, entry, scopedKey)
		}
		if _, err := NewScopedReader(scoped, ReadScope{}); !errors.Is(err, ErrScopeRequired) {
			t.Fatalf("binding the scope that names nothing = %v, want the named refusal %v", err, ErrScopeRequired)
		}
	})

	t.Run("an_uncommitted_key_reads_as_missing", func(t *testing.T) {
		original, _ := seed(scopedRef)
		var scoped ScopedOriginalStore = original
		for _, entry := range originalReadEntrypointsOf(t, original, scoped, scope) {
			expectRefusal(t, entry, absentKey, RecallMissing, ErrOriginalMissing)
		}
	})

	t.Run("a_reference_past_its_window_reads_as_expired", func(t *testing.T) {
		original, _ := seed(expiredRef)
		var scoped ScopedOriginalStore = original
		for _, entry := range originalReadEntrypointsOf(t, original, scoped, expiredRef.ReadScope()) {
			expectRefusal(t, entry, expiredKey, RecallExpired, ErrOriginalExpired)
		}
	})

	t.Run("altered_bytes_read_as_an_integrity_mismatch", func(t *testing.T) {
		_, backing := seed(scopedRef)
		blob, err := backing.Read(ctx, Fetch{Key: blobKVKey(scopedKey)})
		if err != nil || len(blob) != 1 {
			t.Fatalf("read the stored body of %q = (%d pair(s), %v), want the one body committed", scopedKey, len(blob), err)
		}
		altered := append([]byte(nil), blob[0].Value...)
		altered[0] ^= 0xff
		if err := backing.Commit(ctx, Batch{Puts: []Pair{{Key: blobKVKey(scopedKey), Value: altered}}}); err != nil {
			t.Fatalf("replace the stored body of %q: %v", scopedKey, err)
		}
		damaged, err := newKVOriginalStore(backing, windows)
		if err != nil {
			t.Fatalf("open the original store over the altered body: %v", err)
		}
		var scoped ScopedOriginalStore = damaged
		for _, entry := range originalReadEntrypointsOf(t, damaged, scoped, scope) {
			expectRefusal(t, entry, scopedKey, RecallFailed, ErrIntegrityMismatch)
		}
	})

	t.Run("an_unreadable_reference_record_reads_as_a_corrupt_snapshot", func(t *testing.T) {
		_, backing := seed(scopedRef)
		if err := backing.Commit(ctx, Batch{Puts: []Pair{{Key: refKVKey(scopedKey), Value: []byte("not-json")}}}); err != nil {
			t.Fatalf("corrupt the reference record of %q: %v", scopedKey, err)
		}
		damaged, err := newKVOriginalStore(backing, windows)
		if err != nil {
			t.Fatalf("open the original store over the unreadable reference: %v", err)
		}
		var scoped ScopedOriginalStore = damaged
		for _, entry := range originalReadEntrypointsOf(t, damaged, scoped, scope) {
			expectRefusal(t, entry, scopedKey, RecallFailed, ErrSnapshotCorrupt)
		}
	})

	t.Run("the_tier_originals_satisfy_the_scoped_port_the_recall_tool_forwards_to", func(t *testing.T) {
		tier, err := OpenTier(ctx, StorageConfig{Type: StorageMemory})
		if err != nil {
			t.Fatalf("open the memory tier of the fixture: %v", err)
		}
		defer func() {
			if err := tier.Close(); err != nil {
				t.Fatalf("close the tier of the fixture: %v", err)
			}
		}()
		ref := OriginalRef{Key: scopedKey, Owner: scope.Owner, Format: scope.Format, RetentionPolicy: scope.RetentionPolicy}
		if err := tier.Originals.Put(ctx, ref, []byte(body)); err != nil {
			t.Fatalf("commit the original of the fixture: %v", err)
		}
		forwarded, held := tier.Originals.(ScopedOriginalStore)
		if !held {
			t.Fatalf("the tier holds its originals as %T, want a value satisfying ScopedOriginalStore", tier.Originals)
		}
		if got := forwarded.GetScoped(ctx, scopedKey, scope); got.Outcome != RecallFound || !bytes.Equal(got.Body, []byte(body)) {
			t.Fatalf("GetScoped(%q) through the scoped port = %+v, want the committed bytes", scopedKey, got)
		}
		assembled, err := assembleSegments(func(from SegmentPosition) (SegmentResult, error) {
			return forwarded.ReadSegmentScoped(ctx, scopedKey, scope, SegmentRequest{From: from, MaxBytes: 8})
		})
		if err != nil || !bytes.Equal(assembled, []byte(body)) {
			t.Fatalf("ReadSegmentScoped(%q) through the scoped port = (%q, %v), want the committed bytes assembled from its segments", scopedKey, assembled, err)
		}
	})
}

// TestScopedReadResolvesTheNamedIdentity pins what one scoped read costs and how far its failure reaches.
//   - a scoped read resolves the one identity it names: two point reads of that identity and no load of the original family, whatever that family grew to
//   - a hot read of the same identity reads no key again, so the two reads are the first read of one identity and not a constant the read adds every time
//   - a key whose references all stand outside the scope reads as missing beside the same two point reads, so a refused scope leaks nothing and reads no family
//   - the whole-set load stays reachable and measured, so the zero above is a zero this check drives instead of a counter nothing runs
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestScopedReadResolvesTheNamedIdentity(t *testing.T) {
	ctx := context.Background()
	const generations = 24
	identity := func(at int) event.EventID { return event.EventID(fmt.Sprintf("scoped-identity-%02d", at)) }
	scopeOf := func(at int) ReadScope {
		return ReadScope{
			Owner:           event.Address{AgentID: "agent-scoped", SessionID: event.SessionID(fmt.Sprintf("session-%02d", at))},
			Format:          "text",
			RetentionPolicy: "durable",
		}
	}
	bodyOf := func(at int) []byte { return []byte(fmt.Sprintf("the original of identity %02d", at)) }
	seed := func() (*kvOriginalStore, AtomicStore) {
		t.Helper()
		backing := newMemKV()
		writer, err := newKVOriginalStore(backing, nil)
		if err != nil {
			t.Fatalf("open the seeding original store: %v", err)
		}
		for at := 0; at < generations; at++ {
			scope := scopeOf(at)
			ref := OriginalRef{Key: identity(at), Owner: scope.Owner, Format: scope.Format, RetentionPolicy: scope.RetentionPolicy}
			if err := writer.Put(ctx, ref, bodyOf(at)); err != nil {
				t.Fatalf("commit the original of generation %d: %v", at, err)
			}
		}
		reader, err := newKVOriginalStore(backing, nil)
		if err != nil {
			t.Fatalf("open the reading original store: %v", err)
		}
		return reader, backing
	}

	reader, backing := seed()
	target := generations / 3
	beforeLoads, beforePairs, beforeKeys := reader.OriginalsReads()
	got := reader.GetScoped(ctx, identity(target), scopeOf(target))
	afterLoads, afterPairs, afterKeys := reader.OriginalsReads()
	if got.Outcome != RecallFound || got.Err != nil || !bytes.Equal(got.Body, bodyOf(target)) {
		t.Fatalf("GetScoped(%q) = %+v, want the committed bytes of that identity", identity(target), got)
	}
	if loads := afterLoads - beforeLoads; loads != 0 {
		t.Fatalf("the first scoped read loaded the whole original family %d time(s), want none beside the %d identit(ies) it holds", loads, generations)
	}
	if pairs := afterPairs - beforePairs; pairs != 0 {
		t.Fatalf("the first scoped read fetched %d record(s) of the whole original family, want none", pairs)
	}
	if keys := afterKeys - beforeKeys; keys != 2 {
		t.Fatalf("the first scoped read read %d key(s), want the body and the reference of the identity it names", keys)
	}
	hotLoads, hotPairs, hotKeys := reader.OriginalsReads()
	if again := reader.GetScoped(ctx, identity(target), scopeOf(target)); again.Outcome != RecallFound {
		t.Fatalf("the hot scoped read = %+v, want the same committed bytes", again)
	}
	reLoads, rePairs, reKeys := reader.OriginalsReads()
	if reLoads != hotLoads || rePairs != hotPairs || reKeys != hotKeys {
		t.Fatalf("the hot scoped read read (%d load(s), %d record(s), %d key(s)) more, want a read that pays nothing behind the first",
			reLoads-hotLoads, rePairs-hotPairs, reKeys-hotKeys)
	}
	outside := ReadScope{Owner: event.Address{AgentID: "agent-outside", SessionID: "session-outside"}, Format: "text", RetentionPolicy: "durable"}
	outsider, err := newKVOriginalStore(backing, nil)
	if err != nil {
		t.Fatalf("open the original store of the refused scope: %v", err)
	}
	outLoads, _, outKeysBefore := outsider.OriginalsReads()
	if refused := outsider.GetScoped(ctx, identity(target), outside); refused.Outcome != RecallMissing {
		t.Fatalf("GetScoped(%q) outside its scope = %+v, want the named missing a refused scope answers", identity(target), refused)
	}
	refusedLoads, _, outKeysAfter := outsider.OriginalsReads()
	if loads := refusedLoads - outLoads; loads != 0 {
		t.Fatalf("the read outside the scope loaded the whole original family %d time(s), want none: the scope decides over the identity and never over the family", loads)
	}
	if keys := outKeysAfter - outKeysBefore; keys != 2 {
		t.Fatalf("the read outside the scope read %d key(s), want the two of the identity it names", keys)
	}
	if counted := reader.BlobCount(); counted != generations {
		t.Fatalf("the whole-store read answers %d identit(ies) of the %d the family holds, want the whole family: the load face is not measured", counted, generations)
	}
	if loads, pairs, _ := reader.OriginalsReads(); loads != 1 || pairs != 2*generations {
		t.Fatalf("the whole-store read loaded %d time(s) and fetched %d record(s), want the one load of the %d records the family holds: a load the counter never sees makes the zero above a zero nothing measures",
			loads, pairs, 2*generations)
	}

	t.Run("a damaged identity fails its own read alone", func(t *testing.T) {
		_, backing := seed()
		broken := identity(0)
		if err := backing.Commit(ctx, Batch{Puts: []Pair{{Key: refKVKey(broken), Value: []byte("not-json")}}}); err != nil {
			t.Fatalf("corrupt the reference record of %q: %v", broken, err)
		}
		reopened, err := newKVOriginalStore(backing, nil)
		if err != nil {
			t.Fatalf("open the original store over the damaged reference: %v", err)
		}
		failed := reopened.GetScoped(ctx, broken, scopeOf(0))
		if failed.Outcome != RecallFailed || !errors.Is(failed.Err, ErrSnapshotCorrupt) {
			t.Fatalf("GetScoped(%q) over its unreadable reference = %+v, want the named corruption", broken, failed)
		}
		if !strings.Contains(failed.Err.Error(), string(broken)) {
			t.Fatalf("the corruption of %q = %v, want the record that failed named", broken, failed.Err)
		}
		beside := generations - 1
		kept := reopened.GetScoped(ctx, identity(beside), scopeOf(beside))
		if kept.Outcome != RecallFound || !bytes.Equal(kept.Body, bodyOf(beside)) {
			t.Fatalf("GetScoped(%q) beside the damaged identity = %+v, want its own committed bytes: one identity's damage reaches no read beside it", identity(beside), kept)
		}
		segment, err := reopened.ReadSegmentScoped(ctx, identity(beside), scopeOf(beside), SegmentRequest{MaxBytes: 8})
		if err != nil || len(segment.Body) == 0 || !bytes.HasPrefix(bodyOf(beside), segment.Body) {
			t.Fatalf("ReadSegmentScoped(%q) beside the damaged identity = (%q, %v), want the first segment of its own body", identity(beside), segment.Body, err)
		}
	})
}

// TestCommitFactMemoryTierRecallsTheOriginalByBareKey pins the memory tier's whole-fact commit: the original it commits reads back by bare key.
//   - fixture: one memory tier opened without an engine, one fact committed through CommitFact
//   - the scoped read runs before the bare recall, so the whole-set load publishes the stored family the recall answers from
//   - the bare recall serves the committed text byte for byte, and the scoped read serves it inside the reference's own scope
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestCommitFactMemoryTierRecallsTheOriginalByBareKey(t *testing.T) {
	ctx := context.Background()
	tier, err := OpenTier(ctx, StorageConfig{Type: StorageMemory})
	if err != nil {
		t.Fatalf("open the memory tier of the fixture: %v", err)
	}
	defer func() {
		if err := tier.Close(); err != nil {
			t.Fatalf("close the tier of the fixture: %v", err)
		}
	}()
	const body = "the original the memory tier commits"
	fact := FactCommit{
		Record: SegmentRecord{PartitionID: 1, WindowTS: 1700000000000, Seq: 1, EventKey: "memory-event", EventTime: 1700000000000, Body: []byte("event body")}.WithChain("answer", "session-memory-original"),
		Ref:    OriginalRef{Key: "memory-original", Owner: event.Address{AgentID: "agent-a", SessionID: "session-memory-original"}, Format: "text"},
		Body:   []byte(body),
	}
	if outcome, err := CommitFact(ctx, tier, fact); err != nil || outcome != FactCommitted {
		t.Fatalf("the commit of the memory-tier fact = (%v, %v), want a committed fact", outcome, err)
	}
	scoped, held := tier.Originals.(ScopedOriginalStore)
	if !held {
		t.Fatalf("the memory tier holds the originals %T, want the scoped read face", tier.Originals)
	}
	if got := scoped.GetScoped(ctx, fact.Ref.Key, fact.Ref.ReadScope()); got.Outcome != RecallFound || string(got.Body) != body {
		t.Fatalf("the scoped read of %q = %+v, want the committed text %q: the whole-set load reads the engine the commit wrote", fact.Ref.Key, got, body)
	}
	if got := tier.Originals.Get(ctx, fact.Ref.Key); got.Outcome != RecallFound || string(got.Body) != body {
		t.Fatalf("recall_get(%q) = %+v, want the committed text %q served by bare key: the memory tier's original and segment share one engine", fact.Ref.Key, got, body)
	}
}

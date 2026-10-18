package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/event"
)

// TestPartitionIdentityIsOneStableMapping pins one name-to-partition mapping whose cache is never the source of truth.
// - a derived partition always lands inside the ten-bit range
// - dropping a name's cache entry never changes the value that name derives
// - a batch of names re-derives to exactly the values the cached path served
// 契约: docs/wiki/memory/memory-architecture.md#partitionidfromname
func TestPartitionIdentityIsOneStableMapping(t *testing.T) {
	const name = "partition:invariant:one"
	first := PartitionIDFromName(name)
	if first < 0 || first >= 1<<PartitionBits {
		t.Fatalf("partition of %q = %d, want a value inside the ten-bit range", name, first)
	}
	partitionNames.Delete(name)
	if rederived := PartitionIDFromName(name); rederived != first {
		t.Fatalf("partition of %q after dropping its cache entry = %d, want the %d derived first", name, rederived, first)
	}

	names := make([]string, 0, 32)
	for i := 0; i < 32; i++ {
		names = append(names, fmt.Sprintf("partition:invariant:%02d", i))
	}
	derived := make(map[string]int, len(names))
	for _, one := range names {
		derived[one] = PartitionIDFromName(one)
		if derived[one] < 0 || derived[one] >= 1<<PartitionBits {
			t.Fatalf("partition of %q = %d, want a value inside the ten-bit range", one, derived[one])
		}
	}
	for _, one := range names {
		partitionNames.Delete(one)
	}
	for _, one := range names {
		if again := PartitionIDFromName(one); again != derived[one] {
			t.Fatalf("partition of %q re-derived = %d, want the %d the cached path served", one, again, derived[one])
		}
	}
}

// TestCapacityEvictionStaysInsideOnePartition pins the scan's segment: capacity judges one partition alone and never sweeps another partition's older keys.
// - the oldest key of the over-capacity partition is tombstoned, and only that key
// - a partition holding exactly the bound keeps its key however old it is
// - a protected key is never evicted, and the over-capacity partition keeps both keys
// - eviction only marks the key: the stored key stays enumerable until compaction
// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
func TestCapacityEvictionStaysInsideOnePartition(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	keys := []StoredKey{
		{Key: "ca01", Type: "tool.result", Partition: 0, RecordedAt: now.Add(-10 * time.Hour)},
		{Key: "ca02", Type: "tool.result", Partition: 0, RecordedAt: now.Add(-time.Hour)},
		{Key: "ca03", Type: "tool.result", Partition: 1, RecordedAt: now.Add(-100 * time.Hour)},
		{Key: "ca04", Type: "tool.result", Partition: 2, RecordedAt: now.Add(-90 * time.Hour)},
	}

	lifecycle, store, _ := newLifecycleUnderTest(t, LifecycleConfig{GlobalTTLDays: -1, MaxEventsPerPartition: 1})
	for _, key := range keys {
		store.Put(key)
	}
	report, err := lifecycle.Scan(ctx, now)
	if err != nil {
		t.Fatalf("scan four keys under a per-partition bound of one: %v", err)
	}
	if len(report.Tombstoned) != 1 || report.Tombstoned[0] != "ca01" {
		t.Fatalf("tombstoned = %v, want only the oldest key of the over-capacity partition", report.Tombstoned)
	}
	for _, key := range []event.EventID{"ca03", "ca04"} {
		if lifecycle.Tombstoned(key) {
			t.Fatalf("a partition at its bound lost %q, want a capacity pass that never crosses partitions", key)
		}
	}
	if len(store.keys) != len(keys) {
		t.Fatalf("stored keys after eviction = %d, want all %d kept until compaction", len(store.keys), len(keys))
	}

	protected, protectedStore, retention := newLifecycleUnderTest(t, LifecycleConfig{GlobalTTLDays: -1, MaxEventsPerPartition: 1})
	for _, key := range keys {
		protectedStore.Put(key)
	}
	if _, err := retention.Register(ctx, "ca01", "recovery:inventory", RetentionRecord{
		Kind: RetentionRecovery, Type: "tool.result", RecordedAt: now.Add(-10 * time.Hour),
	}); err != nil {
		t.Fatalf("hold the oldest key of the over-capacity partition: %v", err)
	}
	report, err = protected.Scan(ctx, now)
	if err != nil {
		t.Fatalf("scan with the oldest key protected: %v", err)
	}
	if len(report.Tombstoned) != 0 {
		t.Fatalf("tombstoned = %v, want nothing while the eviction candidate is protected", report.Tombstoned)
	}
	if _, marked := protectedStore.Tombstoned("ca01"); marked {
		t.Fatal("the protected key was tombstoned in the store")
	}
	if _, held := protectedStore.keys["ca02"]; !held {
		t.Fatal("the over-capacity partition dropped its newer key, want both keys kept")
	}
}

// TestLeaseSegmentsProtectOnlyTheirOwnKeys pins the lease segment: a token clears only its own hold, and a released key keeps its first registration.
// - a lease issued for one key is refused by name against another key
// - a lease issued to one holder is refused by name against another holder's hold on the same key
// - the refused release leaves every hold standing
// - releasing one holder leaves the key protected by the holders still standing
// - a released key keeps its first registration and its original recorded time
// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
func TestLeaseSegmentsProtectOnlyTheirOwnKeys(t *testing.T) {
	ctx := context.Background()
	recordedAt := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	retention := NewRetention(NewMemoryGuard())
	leaseA, err := retention.Register(ctx, "da01", "recovery:inventory", RetentionRecord{
		Kind: RetentionRecovery, Type: "tool.result", RecordedAt: recordedAt,
	})
	if err != nil {
		t.Fatalf("hold the first key: %v", err)
	}
	leaseB, err := retention.Register(ctx, "da02", "recovery:inventory", RetentionRecord{
		Kind: RetentionRecovery, Type: "agent.output", RecordedAt: recordedAt.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("hold the second key: %v", err)
	}
	if leaseA == leaseB {
		t.Fatalf("two holds share the lease %q, want one lease per hold", leaseA)
	}
	if err := retention.Retire(ctx, "da02", "recovery:inventory", leaseA); !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("releasing the second key under the first key's lease = %v, want ErrLeaseMismatch", err)
	}
	if held, err := retention.Held(ctx, "da02"); err != nil || !held {
		t.Fatalf("second key after a foreign release = (held %v, %v), want it still protected", held, err)
	}

	leaseDelivery, err := retention.Register(ctx, "da01", "unsettled:delivery", RetentionRecord{
		Kind: RetentionUnconfirmed, Type: "tool.result", RecordedAt: recordedAt.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("hold the first key for a second holder: %v", err)
	}
	if err := retention.Retire(ctx, "da01", "unsettled:delivery", leaseA); !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("releasing a holder's hold under another holder's lease = %v, want ErrLeaseMismatch", err)
	}
	if held, err := retention.Held(ctx, "da01"); err != nil || !held {
		t.Fatalf("first key after a foreign release = (held %v, %v), want it still protected", held, err)
	}

	released, err := retention.ReleaseHolder(ctx, "recovery:inventory")
	if err != nil {
		t.Fatalf("release the recovery holder: %v", err)
	}
	if released != 2 {
		t.Fatalf("released holds = %d, want the two the recovery holder stood on", released)
	}
	if held, err := retention.Held(ctx, "da01"); err != nil || !held {
		t.Fatalf("first key after one holder released = (held %v, %v), want the other holder's hold standing", held, err)
	}
	if held, err := retention.Held(ctx, "da02"); err != nil || held {
		t.Fatalf("second key after its only holder released = (held %v, %v), want it unprotected", held, err)
	}

	if err := retention.Retire(ctx, "da01", "unsettled:delivery", leaseDelivery); err != nil {
		t.Fatalf("release the delivery holder: %v", err)
	}
	if held, err := retention.Held(ctx, "da01"); err != nil || held {
		t.Fatalf("first key after the last holder released = (held %v, %v), want it unprotected", held, err)
	}
	record, registered := retention.Record("da01")
	if !registered {
		t.Fatal("the released key lost its registration")
	}
	if !record.RecordedAt.Equal(recordedAt) || record.Kind != RetentionRecovery || record.Type != "tool.result" {
		t.Fatalf("registration after release = %+v, want the first registration kept verbatim", record)
	}
}

// TestOneKeyFilesUnderOnePartition pins the key rule: one committed key names one position, and a replay elsewhere is refused instead of filed twice.
// - a repeat of the committed record is idempotent and adds no second event
// - the same key under another partition is refused by name
// - the same key under another window or sequence slot is refused by name
// - the same key with other bytes is refused by name
// - the published position stays the one the first commit took
// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
func TestOneKeyFilesUnderOnePartition(t *testing.T) {
	ctx := context.Background()
	tier, err := OpenTier(ctx, StorageConfig{Type: StorageMemory})
	if err != nil {
		t.Fatalf("open the in-memory tier: %v", err)
	}
	committed := SegmentRecord{
		PartitionID: 0, WindowTS: 10, Seq: 1, EventKey: "ea01", EventTime: 10, Body: []byte(`{"probe":"one"}`),
	}
	if err := tier.Segments.StoreEvent(ctx, committed); err != nil {
		t.Fatalf("commit the first record: %v", err)
	}
	if err := tier.Segments.StoreEvent(ctx, committed); err != nil {
		t.Fatalf("repeat the committed record: %v", err)
	}
	if count := tier.Segments.EventCount(); count != 1 {
		t.Fatalf("event count after a repeat = %d, want the one committed event", count)
	}

	refused := map[string]SegmentRecord{
		"another partition": {PartitionID: 1, WindowTS: 10, Seq: 1, EventKey: "ea01", EventTime: 10, Body: committed.Body},
		"another window":    {PartitionID: 0, WindowTS: 11, Seq: 1, EventKey: "ea01", EventTime: 11, Body: committed.Body},
		"another slot":      {PartitionID: 0, WindowTS: 10, Seq: 2, EventKey: "ea01", EventTime: 10, Body: committed.Body},
		"other bytes":       {PartitionID: 0, WindowTS: 10, Seq: 1, EventKey: "ea01", EventTime: 10, Body: []byte(`{"probe":"two"}`)},
	}
	for name, record := range refused {
		if err := tier.Segments.StoreEvent(ctx, record); !errors.Is(err, ErrContentConflict) {
			t.Fatalf("committing the same key under %s = %v, want ErrContentConflict", name, err)
		}
	}
	if count := tier.Segments.EventCount(); count != 1 {
		t.Fatalf("event count after refused commits = %d, want the one committed event", count)
	}
	stored, held, err := tier.Segments.GetEvent(ctx, "ea01")
	if err != nil || !held {
		t.Fatalf("read the committed key = (held %v, %v), want it published", held, err)
	}
	if stored.PartitionID != 0 || stored.WindowTS != 10 || stored.Seq != 1 || string(stored.Body) != `{"probe":"one"}` {
		t.Fatalf("published position = %+v, want the partition, window, slot and bytes of the first commit", stored)
	}
}

// TestRebuildRefusesToPassABrokenChainAsWhole pins the replay refusal: a broken chain is named partial, never presented as a whole rebuild.
// - a chain missing a snapshot ref returns ErrRebuildPartial and names the missing key
// - the entries the broken chain could restore are still restored, under a partial report
// - a compaction payload that cannot be read back is refused by name
// - a chain read that fails is refused by name and is never reported as a scanned chain
// - an empty chain is reported as scanned empty, not as a failure
// - an empty session is refused by name instead of degrading to a partition-only chain
// - a fact of another session in the same partition never enters the projection or the relations
// - a non-empty projection is skipped with its reason recorded
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func TestRebuildRefusesToPassABrokenChainAsWhole(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	chainKey := ChainKey{PartitionID: 7, SessionID: "fa-session"}

	compaction := func(refs []CompactionRef) ChainFact {
		payload, err := json.Marshal(CompactionFact{
			Summary:        CompactionRef{Key: "-sum", Type: "agent.output", Summary: "folded run", Content: "folded run text", Time: at},
			Refs:           refs,
			FullBoundary:   "fb02",
			MeditationKeys: []event.EventID{"fm01"},
		})
		if err != nil {
			t.Fatalf("marshal the compaction payload: %v", err)
		}
		return ChainFact{Key: "fb01", Chain: chainKey, Type: CompactionEventType, Content: string(payload), Time: at}
	}
	newChain := func(t *testing.T, refs []CompactionRef) *MemoryEventChain {
		t.Helper()
		chain := NewMemoryEventChain()
		facts := []ChainFact{
			compaction(refs),
			{Key: "fb02", Chain: chainKey, Type: "agent.output", Summary: "ticket two", Content: "ticket two text", Time: at},
			{Key: "fb03", Chain: chainKey, Type: "external.input", Summary: "tail", Content: "tail text", Time: at.Add(time.Minute)},
			{Key: "fb04", Chain: ChainKey{PartitionID: 7, SessionID: "fa-other"}, Type: "external.input", Summary: "other", Content: "other text", Time: at},
		}
		for _, fact := range facts {
			if err := chain.Commit(ctx, fact); err != nil {
				t.Fatalf("commit fact %q: %v", fact.Key, err)
			}
		}
		return chain
	}
	wholeRefs := []CompactionRef{
		{Key: "fb02", Type: "agent.output", Summary: "ticket two"},
		{Key: "-syn", Type: "tool.result", Summary: "synthetic", Content: "synthetic text", Time: at},
	}

	t.Run("a whole snapshot and a bounded tail restore in order", func(t *testing.T) {
		target := NewReplayProjection()
		relations := NewMemoryRelations()
		report, err := RebuildProjectionFromWAL(ctx, target, newChain(t, wholeRefs), relations, chainKey, RebuildOptions{})
		if err != nil {
			t.Fatalf("rebuild a whole chain: %v", err)
		}
		if report.Partial || len(report.Missing) != 0 {
			t.Fatalf("report of a whole chain = (partial %v, missing %v), want neither", report.Partial, report.Missing)
		}
		if !report.Scanned || report.Fallback || report.Entries != 4 {
			t.Fatalf("report = (scanned %v, fallback %v, entries %d), want the snapshot, its refs and the tail", report.Scanned, report.Fallback, report.Entries)
		}
		if report.FullBoundary != "fb02" {
			t.Fatalf("full boundary = %q, want the payload boundary the tail was paged after", report.FullBoundary)
		}
		entries := target.Entries()
		if len(entries) != 4 || entries[0].Key != "-sum" || entries[1].Key != "fb02" ||
			entries[2].Key != "-syn" || entries[3].Key != "fb03" {
			t.Fatalf("entries = %+v, want the summary first, then the payload refs, then the tail in key order", entries)
		}
		if entries[2].Content != "synthetic text" {
			t.Fatalf("synthetic ref content = %q, want the payload text it carries", entries[2].Content)
		}
		if parent, err := relations.GetParent("fb03"); err != nil || parent != "fb02" {
			t.Fatalf("tail parent = (%q, %v), want the last restored snapshot fact", parent, err)
		}
		if parent, err := relations.GetParent("fb04"); err != nil || parent != "" {
			t.Fatalf("other session's fact parent = (%q, %v), want it never linked", parent, err)
		}
		for _, entry := range entries {
			if entry.Key == "fb04" {
				t.Fatal("a fact of another session entered the projection")
			}
		}
		if keys := target.MeditationKeys(); len(keys) != 1 || keys[0] != "fm01" {
			t.Fatalf("meditation keys = %v, want the snapshot's key reseeded", keys)
		}
	})

	t.Run("a missing snapshot ref is refused by name and reported partial", func(t *testing.T) {
		target := NewReplayProjection()
		refs := append(append([]CompactionRef{}, wholeRefs...), CompactionRef{Key: "fb09", Type: "agent.output", Summary: "gone"})
		report, err := RebuildProjectionFromWAL(ctx, target, newChain(t, refs), NewMemoryRelations(), chainKey, RebuildOptions{})
		if !errors.Is(err, ErrRebuildPartial) {
			t.Fatalf("rebuild over a missing ref = %v, want ErrRebuildPartial", err)
		}
		if !report.Partial || len(report.Missing) != 1 || report.Missing[0] != "fb09" {
			t.Fatalf("report = (partial %v, missing %v), want the missing key named", report.Partial, report.Missing)
		}
		if report.Entries != 4 {
			t.Fatalf("entries restored beside the missing key = %d, want the summary, its held and synthetic refs and the tail", report.Entries)
		}
	})

	t.Run("a compaction payload that cannot be read back is refused by name", func(t *testing.T) {
		chain := NewMemoryEventChain()
		if err := chain.Commit(ctx, ChainFact{Key: "fb01", Chain: chainKey, Type: CompactionEventType, Content: "{not a payload", Time: at}); err != nil {
			t.Fatalf("commit the corrupt compaction: %v", err)
		}
		if _, err := RebuildProjectionFromWAL(ctx, NewReplayProjection(), chain, NewMemoryRelations(), chainKey, RebuildOptions{}); !errors.Is(err, ErrCompactionCorrupt) {
			t.Fatalf("rebuild over a corrupt payload = %v, want ErrCompactionCorrupt", err)
		}
	})

	t.Run("a failed chain read is refused and not reported as a scanned chain", func(t *testing.T) {
		report, err := RebuildProjectionFromWAL(ctx, NewReplayProjection(), failingChain{err: errors.New("segment read failed")},
			NewMemoryRelations(), chainKey, RebuildOptions{})
		if !errors.Is(err, ErrChainScan) {
			t.Fatalf("rebuild over a failing read = %v, want ErrChainScan", err)
		}
		if report.Scanned || report.Entries != 0 {
			t.Fatalf("report of a failing read = (scanned %v, entries %d), want no scanned chain", report.Scanned, report.Entries)
		}
	})

	t.Run("an empty chain is reported as scanned empty", func(t *testing.T) {
		report, err := RebuildProjectionFromWAL(ctx, NewReplayProjection(), NewMemoryEventChain(), NewMemoryRelations(), chainKey, RebuildOptions{})
		if err != nil {
			t.Fatalf("rebuild an empty chain: %v", err)
		}
		if !report.Scanned || report.Entries != 0 || report.Partial {
			t.Fatalf("report of an empty chain = %+v, want a scanned empty chain", report)
		}
	})

	t.Run("an empty session is refused by name", func(t *testing.T) {
		if _, err := RebuildProjectionFromWAL(ctx, NewReplayProjection(), NewMemoryEventChain(), NewMemoryRelations(),
			ChainKey{PartitionID: 7}, RebuildOptions{}); !errors.Is(err, ErrChainKeyRequired) {
			t.Fatalf("rebuild under an empty session = %v, want ErrChainKeyRequired", err)
		}
	})

	t.Run("a non-empty projection is skipped with its reason", func(t *testing.T) {
		target := NewReplayProjection()
		target.Append(ctx, ProjectionEntry{Key: "fb10", Type: "tool.result", Content: "already here"})
		report, err := RebuildProjectionFromWAL(ctx, target, newChain(t, wholeRefs), NewMemoryRelations(), chainKey, RebuildOptions{})
		if err != nil {
			t.Fatalf("rebuild over a non-empty projection: %v", err)
		}
		if report.Skipped != RebuildSkipNotEmpty || report.Entries != 0 {
			t.Fatalf("report = (skipped %q, entries %d), want the skip reason and nothing appended", report.Skipped, report.Entries)
		}
		if len(target.Entries()) != 1 {
			t.Fatalf("target entries = %d, want the one entry it already held", len(target.Entries()))
		}
	})
}

// failingChain is a fact chain whose read always fails, so a replay is judged on how
// it reports a failed read rather than on what it could have replayed.
type failingChain struct{ err error }

func (f failingChain) ScanFrom(context.Context, event.EventID, int) ([]ChainFact, error) {
	return nil, f.err
}

func (f failingChain) Fact(context.Context, event.EventID) (ChainFact, bool, error) {
	return ChainFact{}, false, f.err
}

// newLifecycleUnderTest opens a lifecycle over an in-memory store whose recovery
// inventory already completed, so a scan runs without the arming grace.
func newLifecycleUnderTest(t *testing.T, cfg LifecycleConfig) (*Lifecycle, *MemoryLifecycleStore, *Retention) {
	t.Helper()
	store := NewMemoryLifecycleStore(LifecycleCapabilities{})
	retention := NewRetention(NewMemoryGuard())
	lifecycle, err := NewLifecycle(cfg, retention, store, LifecycleCapabilities{})
	if err != nil {
		t.Fatalf("open the lifecycle: %v", err)
	}
	lifecycle.RecoveryComplete()
	return lifecycle, store, retention
}

package history

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/SpellingDragon/tagent/event"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// TestStorageCapabilities proves the commit barrier contract on every storage tier.
//
// - the memory tier keeps the business contract and declares no recovery
// - the localfile tier reads back byte for byte across an independent reopen
// - damaged snapshot content refuses the open
// - a failed barrier reports no durable success and publishes no part of the record
// - the file tier runs the real rustviking CLI and names its absence instead of mocking
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestStorageCapabilities(t *testing.T) {
	ctx := context.Background()
	original := []byte("第一行：中文与 English 混排\nsecond line\n\n末行不换行")
	binaryBody := []byte{0x00, 0xff, 0x10, '\n', 'x'}
	eventBody := []byte("{\"key\":\"e3a1\",\"text\":\"混排 ✓\"}\n")
	eventBodyOther := []byte("{\"key\":\"e5b2\",\"text\":\"另一个事件\"}\n")
	refA := ledger.OriginalRef{Key: "0a01", Owner: event.Address{AgentID: "writer-a", SessionID: "s1"}, Format: "text/plain", RetentionPolicy: "keep"}
	refB := ledger.OriginalRef{Key: "0a02", Owner: event.Address{AgentID: "writer-b", SessionID: "s2"}, Format: "application/octet-stream", RetentionPolicy: "keep"}

	t.Run("memory tier keeps the commit contract and claims no recovery", func(t *testing.T) {
		tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageMemory})
		if err != nil {
			t.Fatalf("OpenTier(memory): %v", err)
		}
		if tier.Capabilities.Tier != ledger.StorageMemory || tier.Capabilities.Durable {
			t.Fatalf("Capabilities = %+v, want the memory tier declaring itself non-durable", tier.Capabilities)
		}
		if err := tier.Originals.Put(ctx, refA, original); err != nil {
			t.Fatalf("Put(%s): %v", refA.Key, err)
		}
		if err := tier.Originals.Put(ctx, refB, binaryBody); err != nil {
			t.Fatalf("Put(%s): %v", refB.Key, err)
		}
		recalled := tier.Originals.Get(ctx, refA.Key)
		if recalled.Outcome != ledger.RecallFound {
			t.Fatalf("Get(%s) = outcome %v, want found", refA.Key, recalled.Outcome)
		}
		assertSameBytes(t, "Get(0a01)", recalled.Body, original)
		counter, ok := tier.Originals.(interface{ BlobCount() int })
		if !ok {
			t.Fatalf("tier.Originals does not report its blob count")
		}
		if n := counter.BlobCount(); n != 2 {
			t.Fatalf("BlobCount() = %d, want 2 stored blobs for two distinct contents", n)
		}
		missing := tier.Originals.Get(ctx, "0b03")
		if missing.Outcome != ledger.RecallMissing {
			t.Fatalf("Get(0b03) = outcome %v, want named missing", missing.Outcome)
		}
		segment, err := tier.Originals.ReadSegment(ctx, refA.Key, 1, 1)
		if err != nil || !bytes.Equal(segment, []byte("第一行：中文与 English 混排\n")) {
			t.Fatalf("ReadSegment(%s, 1, 1) = %q, %v, want the whole first line", refA.Key, segment, err)
		}
		rec := ledger.SegmentRecord{PartitionID: 3, WindowTS: 300, Seq: 1, EventKey: "e3a1", EventTime: 42, Body: eventBody}
		if err := tier.Segments.StoreEvent(ctx, rec); err != nil {
			t.Fatalf("StoreEvent(e3a1): %v", err)
		}
		stored, held, err := tier.Segments.GetEvent(ctx, "e3a1")
		if err != nil || !held {
			t.Fatalf("GetEvent(e3a1) = held %v, %v, want the committed event", held, err)
		}
		assertSameBytes(t, "GetEvent(e3a1).Body", stored.Body, eventBody)
		if stored.PartitionID != 3 || stored.WindowTS != 300 || stored.Seq != 1 {
			t.Fatalf("GetEvent(e3a1) index = partition %d window %d seq %d, want 3/300/1", stored.PartitionID, stored.WindowTS, stored.Seq)
		}
		if n := tier.Segments.EventCount(); n != 1 {
			t.Fatalf("EventCount() = %d, want 1 published event", n)
		}
		if err := tier.Segments.StoreEvent(ctx, rec); err != nil {
			t.Fatalf("StoreEvent(e3a1) repeated: %v", err)
		}
		if n := tier.Segments.EventCount(); n != 1 {
			t.Fatalf("EventCount() = %d after a repeated commit, want 1 fact", n)
		}
		conflicting := rec
		conflicting.Body = eventBodyOther
		if err := tier.Segments.StoreEvent(ctx, conflicting); !errors.Is(err, ledger.ErrContentConflict) {
			t.Fatalf("StoreEvent(e3a1) with other bytes = %v, want %v", err, ledger.ErrContentConflict)
		}
		other := ledger.SegmentRecord{PartitionID: 5, WindowTS: 500, Seq: 2, EventKey: "e5b2", EventTime: 43, Body: eventBodyOther}
		if err := tier.Segments.StoreEvent(ctx, other); err != nil {
			t.Fatalf("StoreEvent(e5b2): %v", err)
		}
		if n := tier.Segments.EventCount(); n != 2 {
			t.Fatalf("EventCount() = %d, want 2 published events", n)
		}
	})

	t.Run("localfile tier reads back byte for byte across an independent reopen", func(t *testing.T) {
		root := t.TempDir()
		cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}
		first, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("OpenTier(localfile): %v", err)
		}
		if first.Capabilities.Tier != ledger.StorageLocalFile || !first.Capabilities.Durable {
			t.Fatalf("Capabilities = %+v, want the localfile tier declaring itself durable", first.Capabilities)
		}
		if err := first.Originals.Put(ctx, refA, original); err != nil {
			t.Fatalf("Put(%s): %v", refA.Key, err)
		}
		if err := first.Originals.Put(ctx, refB, binaryBody); err != nil {
			t.Fatalf("Put(%s): %v", refB.Key, err)
		}
		records := []ledger.SegmentRecord{
			{PartitionID: 3, WindowTS: 300, Seq: 1, EventKey: "e3a1", EventTime: 42, Body: eventBody},
			{PartitionID: 5, WindowTS: 500, Seq: 2, EventKey: "e5b2", EventTime: 43, Body: eventBodyOther},
		}
		for _, rec := range records {
			if err := first.Segments.StoreEvent(ctx, rec); err != nil {
				t.Fatalf("StoreEvent(%s): %v", rec.EventKey, err)
			}
		}
		second, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("reopen OpenTier(localfile): %v", err)
		}
		for _, want := range []struct {
			ref  ledger.OriginalRef
			body []byte
		}{{refA, original}, {refB, binaryBody}} {
			recalled := second.Originals.Get(ctx, want.ref.Key)
			if recalled.Outcome != ledger.RecallFound {
				t.Fatalf("reopened Get(%s) = outcome %v, want found", want.ref.Key, recalled.Outcome)
			}
			assertSameBytes(t, "reopened Get("+string(want.ref.Key)+")", recalled.Body, want.body)
		}
		segment, err := second.Originals.ReadSegment(ctx, refA.Key, 2, 1)
		if err != nil || !bytes.Equal(segment, []byte("second line\n")) {
			t.Fatalf("reopened ReadSegment(%s, 2, 1) = %q, %v, want the second line", refA.Key, segment, err)
		}
		scoped, ok := second.Originals.(ledger.ScopedOriginalStore)
		if !ok {
			t.Fatalf("reopened originals %T cannot evaluate reads inside a scope", second.Originals)
		}
		reader, err := ledger.NewScopedReader(scoped, refA.ReadScope())
		if err != nil {
			t.Fatalf("NewScopedReader(refA scope): %v", err)
		}
		if recalled := reader.Get(ctx, refA.Key); recalled.Outcome != ledger.RecallFound {
			t.Fatalf("reopened scoped Get(%s) = outcome %v, want found inside its own scope", refA.Key, recalled.Outcome)
		}
		stranger := ledger.ReadScope{Owner: event.Address{AgentID: "writer-c", SessionID: "s3"}, Format: refA.Format, RetentionPolicy: refA.RetentionPolicy}
		strangerReader, err := ledger.NewScopedReader(scoped, stranger)
		if err != nil {
			t.Fatalf("NewScopedReader(stranger scope): %v", err)
		}
		if recalled := strangerReader.Get(ctx, refA.Key); recalled.Outcome != ledger.RecallMissing {
			t.Fatalf("reopened scoped Get(%s) = outcome %v, want missing for a foreign scope", refA.Key, recalled.Outcome)
		}
		for _, want := range records {
			stored, held, err := second.Segments.GetEvent(ctx, want.EventKey)
			if err != nil || !held {
				t.Fatalf("reopened GetEvent(%s) = held %v, %v, want the committed event", want.EventKey, held, err)
			}
			assertSameBytes(t, "reopened GetEvent("+string(want.EventKey)+").Body", stored.Body, want.Body)
			if stored.PartitionID != want.PartitionID || stored.WindowTS != want.WindowTS || stored.Seq != want.Seq {
				t.Fatalf("reopened GetEvent(%s) index = partition %d window %d seq %d, want %d/%d/%d", want.EventKey, stored.PartitionID, stored.WindowTS, stored.Seq, want.PartitionID, want.WindowTS, want.Seq)
			}
		}
		if n := second.Segments.EventCount(); n != 2 {
			t.Fatalf("reopened EventCount() = %d, want 2 committed events", n)
		}
	})

	t.Run("localfile open refuses a damaged snapshot", func(t *testing.T) {
		root := t.TempDir()
		cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}
		tier, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("OpenTier(localfile): %v", err)
		}
		if err := tier.Originals.Put(ctx, refA, original); err != nil {
			t.Fatalf("Put(%s): %v", refA.Key, err)
		}
		rec := ledger.SegmentRecord{PartitionID: 3, WindowTS: 300, Seq: 1, EventKey: "e3a1", EventTime: 42, Body: eventBody}
		if err := tier.Segments.StoreEvent(ctx, rec); err != nil {
			t.Fatalf("StoreEvent(e3a1): %v", err)
		}
		overwriteTreeFiles(t, root, []byte("{ damaged snapshot"))
		if _, err := ledger.OpenTier(ctx, cfg); !errors.Is(err, ledger.ErrSnapshotCorrupt) {
			t.Fatalf("reopen over damaged snapshot = %v, want %v", err, ledger.ErrSnapshotCorrupt)
		}
	})

	t.Run("a failed barrier reports no durable success and publishes no part", func(t *testing.T) {
		root := t.TempDir()
		cfg := ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root}
		tier, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("OpenTier(localfile): %v", err)
		}
		committed := ledger.SegmentRecord{PartitionID: 7, WindowTS: 700, Seq: 1, EventKey: "c0ffee", EventTime: 11, Body: eventBody}
		if err := tier.Segments.StoreEvent(ctx, committed); err != nil {
			t.Fatalf("StoreEvent(c0ffee): %v", err)
		}
		restore := makeTreeReadOnly(t, root)
		rejected := ledger.SegmentRecord{PartitionID: 7, WindowTS: 700, Seq: 2, EventKey: "badbad", EventTime: 12, Body: eventBodyOther}
		err = tier.Segments.StoreEvent(ctx, rejected)
		if err == nil {
			t.Fatalf("StoreEvent(badbad) under a read-only tree reported durable success")
		}
		if !errors.Is(err, ledger.ErrSegmentBarrier) {
			t.Fatalf("StoreEvent(badbad) = %v, want %v", err, ledger.ErrSegmentBarrier)
		}
		if !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("StoreEvent(badbad) = %v, want the write failure kept as its cause", err)
		}
		if n := tier.Segments.EventCount(); n != 1 {
			t.Fatalf("EventCount() = %d after a failed barrier, want 1", n)
		}
		if _, held, err := tier.Segments.GetEvent(ctx, rejected.EventKey); err != nil || held {
			t.Fatalf("GetEvent(badbad) = held %v, %v, want nothing published", held, err)
		}
		restore()
		after := ledger.SegmentRecord{PartitionID: 9, WindowTS: 900, Seq: 1, EventKey: "feed01", EventTime: 13, Body: eventBody}
		if err := tier.Segments.StoreEvent(ctx, after); err != nil {
			t.Fatalf("StoreEvent(feed01): %v", err)
		}
		reopened, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("reopen OpenTier(localfile): %v", err)
		}
		if n := reopened.Segments.EventCount(); n != 2 {
			t.Fatalf("reopened EventCount() = %d, want the 2 successful commits and none of the rejected one", n)
		}
		for _, want := range []ledger.SegmentRecord{committed, after} {
			stored, held, err := reopened.Segments.GetEvent(ctx, want.EventKey)
			if err != nil || !held {
				t.Fatalf("reopened GetEvent(%s) = held %v, %v, want the committed event", want.EventKey, held, err)
			}
			assertSameBytes(t, "reopened GetEvent("+string(want.EventKey)+").Body", stored.Body, want.Body)
		}
		if _, held, err := reopened.Segments.GetEvent(ctx, rejected.EventKey); err != nil || held {
			t.Fatalf("reopened GetEvent(badbad) = held %v, %v, want the rejected record absent", held, err)
		}
	})

	t.Run("file tier runs the real rustviking CLI and names its absence", func(t *testing.T) {
		binary := os.Getenv("TAGENT_RUSTVIKING")
		if binary == "" {
			binary = "rustviking"
		}
		cfg := ledger.StorageConfig{
			Type:             ledger.StorageFile,
			Root:             t.TempDir(),
			RustVikingBinary: binary,
			RustVikingConfig: os.Getenv("TAGENT_RUSTVIKING_CONFIG"),
		}
		tier, err := ledger.OpenTier(ctx, cfg)
		if errors.Is(err, ledger.ErrRustVikingMissing) {
			t.Skipf("rustviking CLI %q is not executable here: the file tier barrier evidence belongs to the resource-gated run, and no mock stands in for it", binary)
		}
		if err != nil {
			t.Fatalf("OpenTier(file): %v", err)
		}
		if tier.Capabilities.Tier != ledger.StorageFile || !tier.Capabilities.Durable {
			t.Fatalf("Capabilities = %+v, want the file tier declaring itself durable", tier.Capabilities)
		}
		if err := tier.Originals.Put(ctx, refA, original); err != nil {
			t.Fatalf("Put(%s): %v", refA.Key, err)
		}
		recalled := tier.Originals.Get(ctx, refA.Key)
		if recalled.Outcome != ledger.RecallFound {
			t.Fatalf("Get(%s) = outcome %v, want found", refA.Key, recalled.Outcome)
		}
		assertSameBytes(t, "Get(0a01)", recalled.Body, original)
		rec := ledger.SegmentRecord{PartitionID: 2, WindowTS: 200, Seq: 1, EventKey: "f1f1", EventTime: 5, Body: eventBody}
		if err := tier.Segments.StoreEvent(ctx, rec); err != nil {
			t.Fatalf("StoreEvent(f1f1): %v", err)
		}
		stored, held, err := tier.Segments.GetEvent(ctx, rec.EventKey)
		if err != nil || !held {
			t.Fatalf("GetEvent(f1f1) = held %v, %v, want the committed event", held, err)
		}
		assertSameBytes(t, "GetEvent(f1f1).Body", stored.Body, rec.Body)
		if n := tier.Segments.EventCount(); n != 1 {
			t.Fatalf("EventCount() = %d, want 1 published event", n)
		}
		reopened, err := ledger.OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("reopen OpenTier(file): %v", err)
		}
		recalled = reopened.Originals.Get(ctx, refA.Key)
		if recalled.Outcome != ledger.RecallFound {
			t.Fatalf("reopened Get(%s) = outcome %v, want found", refA.Key, recalled.Outcome)
		}
		assertSameBytes(t, "reopened Get(0a01)", recalled.Body, original)
		stored, held, err = reopened.Segments.GetEvent(ctx, rec.EventKey)
		if err != nil || !held {
			t.Fatalf("reopened GetEvent(f1f1) = held %v, %v, want the committed event", held, err)
		}
		assertSameBytes(t, "reopened GetEvent(f1f1).Body", stored.Body, rec.Body)
		if n := reopened.Segments.EventCount(); n != 1 {
			t.Fatalf("reopened EventCount() = %d, want 1 committed event", n)
		}
	})
}

// assertSameBytes fails the test when the bytes a read returned differ from the
// bytes the commit carried.
func assertSameBytes(t *testing.T, what string, got, want []byte) {
	t.Helper()
	if !bytes.Equal(got, want) {
		t.Fatalf("%s = %q, want the committed bytes %q", what, got, want)
	}
}

// makeTreeReadOnly removes write permission from every directory under root and
// returns the restore that hands it back; a caller that can still write the tree
// afterwards gets the injection skipped instead of a false result.
func makeTreeReadOnly(t *testing.T, root string) (restore func()) {
	t.Helper()
	dirs := []string{root}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	for _, dir := range dirs {
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatalf("chmod %s: %v", dir, err)
		}
	}
	restore = func() {
		for _, dir := range dirs {
			_ = os.Chmod(dir, 0o755)
		}
	}
	t.Cleanup(restore)
	probe := filepath.Join(root, ".write-probe")
	if err := os.WriteFile(probe, nil, 0o600); err == nil {
		_ = os.Remove(probe)
		restore()
		t.Skipf("this user can still write a read-only tree, so no write error can be injected here")
	}
	return restore
}

// overwriteTreeFiles replaces the content of every regular file under root, so a
// store that reads it back meets content that is not the snapshot it claims.
func overwriteTreeFiles(t *testing.T, root string, content []byte) {
	t.Helper()
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		return os.WriteFile(path, content, 0o644)
	}); err != nil {
		t.Fatalf("damage %s: %v", root, err)
	}
}

package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// invariantScope is the trusted read scope the replica tests run inside.
func invariantScope() memory.ReadScope {
	return memory.ReadScope{
		Owner:           event.Address{AgentID: "agent-a", SessionID: "session-1"},
		Format:          "tool-output",
		RetentionPolicy: "keep",
	}
}

// invariantRef builds one durable original reference inside the trusted scope.
func invariantRef(key string) memory.OriginalRef {
	scope := invariantScope()
	return memory.OriginalRef{
		Key:             event.EventID(key),
		Owner:           scope.Owner,
		Format:          scope.Format,
		RetentionPolicy: scope.RetentionPolicy,
	}
}

// invariantMaterializer builds one materializer over a fresh in-memory original store.
func invariantMaterializer(t *testing.T, options ...MaterializerOption) (*Materializer, *memory.MemoryOriginalStore, string) {
	t.Helper()
	root := t.TempDir()
	store := memory.NewMemoryOriginalStore()
	materializer, err := NewMaterializer(root, store, invariantScope(), options...)
	if err != nil {
		t.Fatalf("assemble the materializer: %v", err)
	}
	return materializer, store, root
}

// invariantPut commits one original body under the scope and answers its reference.
func invariantPut(t *testing.T, store *memory.MemoryOriginalStore, key string, body []byte) memory.OriginalRef {
	t.Helper()
	ref := invariantRef(key)
	if err := store.Put(context.Background(), ref, body); err != nil {
		t.Fatalf("commit original %q: %v", key, err)
	}
	return ref
}

// invariantReplicaFiles lists the standing regular files under the replica area.
func invariantReplicaFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	area := filepath.Join(root, "replica")
	err := filepath.WalkDir(area, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("walk the replica area: %v", err)
	}
	return files
}

// invariantWaitRunning waits until the cleaner's loop reports it is running.
func invariantWaitRunning(t *testing.T, cleaner *ReplicaCleaner) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		cleaner.mu.Lock()
		running := cleaner.run
		cleaner.mu.Unlock()
		if running {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("the reclamation loop never reported running")
}

// TestReplicaIdentityInvariants pins how one materialisation target becomes a stable path.
// - one target resolves to one path regardless of the materializer instance that resolves it
// - the path stands under the replica area keyed by the original and by the target identity
// - a different consumer or a different reference scope lands on a different path
// - a reference outside the trusted read scope is refused and no replica is written
// - an original that cannot be recalled is refused as ineffective instead of written empty
// - a symbolic link on a parent of the replica path refuses the materialisation by name
// 契约: docs/wiki/platform/platform-subsystems.md#workspace-scratch
func TestReplicaIdentityInvariants(t *testing.T) {
	ctx := context.Background()

	t.Run("one target resolves to one stable path", func(t *testing.T) {
		first, store, root := invariantMaterializer(t)
		second, _, _ := invariantMaterializerAt(t, root, store)
		ref := invariantPut(t, store, "original-1", []byte("the original bytes"))

		path := first.replicaPath(ref, "tool")
		if path != second.replicaPath(ref, "tool") {
			t.Fatalf("paths = (%q, %q), want one path across materializer instances", path, second.replicaPath(ref, "tool"))
		}
		area := filepath.Join(filepath.Clean(root), "replica")
		if filepath.Dir(filepath.Dir(path)) != area {
			t.Fatalf("path = %q, want it two levels under %q", path, area)
		}
		digest := sha256.Sum256([]byte(ref.Key))
		if filepath.Base(filepath.Dir(path)) != hex.EncodeToString(digest[:]) {
			t.Fatalf("original directory = %q, want the digest of the key", filepath.Base(filepath.Dir(path)))
		}
		if first.replicaPath(ref, "other-tool") == path {
			t.Fatalf("consumer paths collided on %q, want one path per target identity", path)
		}
		otherOwner := ref
		otherOwner.Owner = event.Address{AgentID: "agent-b", SessionID: "session-9"}
		if first.replicaPath(otherOwner, "tool") == path {
			t.Fatalf("scoped references collided on %q, want the reference scope inside the identity", path)
		}
		otherFormat := ref
		otherFormat.Format = "task-result"
		if first.replicaPath(otherFormat, "tool") == path {
			t.Fatalf("formats collided on %q, want the format inside the identity", path)
		}
		confusable := invariantRef("a")
		confusable.Owner = event.Address{AgentID: "bc", SessionID: "session-1"}
		confusableKey := invariantRef("ab")
		confusableKey.Owner = event.Address{AgentID: "c", SessionID: "session-1"}
		if first.replicaPath(confusable, "tool") == first.replicaPath(confusableKey, "tool") {
			t.Fatalf("length-prefix boundaries collided, want two targets kept apart")
		}
	})

	t.Run("a reference outside the read scope writes nothing", func(t *testing.T) {
		materializer, _, root := invariantMaterializer(t)
		outside := invariantRef("original-1")
		outside.Owner = event.Address{AgentID: "agent-b", SessionID: "session-9"}
		if _, err := materializer.Materialize(ctx, outside, "tool"); !errors.Is(err, ErrReplicaUnauthorized) {
			t.Fatalf("unauthorized materialisation = %v, want %v", err, ErrReplicaUnauthorized)
		}
		if files := invariantReplicaFiles(t, root); len(files) != 0 {
			t.Fatalf("replica files = %v, want no byte written for an unauthorized reference", files)
		}
	})

	t.Run("an ineffective original is refused instead of written empty", func(t *testing.T) {
		materializer, store, root := invariantMaterializer(t)
		missing := invariantRef("no-such-original")
		if _, err := materializer.Materialize(ctx, missing, "tool"); !errors.Is(err, ErrOriginalIneffective) {
			t.Fatalf("missing original = %v, want %v", err, ErrOriginalIneffective)
		}
		rebuildable, err := materializer.Rebuildable(ctx, missing.Key)
		if err != nil || rebuildable {
			t.Fatalf("rebuildable missing = (%v, %v), want (false, nil)", rebuildable, err)
		}

		present := invariantPut(t, store, "original-1", []byte("bytes"))
		if rebuildable, err := materializer.Rebuildable(ctx, present.Key); err != nil || !rebuildable {
			t.Fatalf("rebuildable present = (%v, %v), want (true, nil)", rebuildable, err)
		}

		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := materializer.Materialize(cancelled, present, "tool"); !errors.Is(err, context.Canceled) {
			t.Fatalf("materialisation under a cancelled context = %v, want the recall failure", err)
		}
		if _, err := materializer.Rebuildable(cancelled, present.Key); !errors.Is(err, context.Canceled) {
			t.Fatalf("rebuildable under a cancelled context = %v, want the recall failure", err)
		}
		if files := invariantReplicaFiles(t, root); len(files) != 0 {
			t.Fatalf("replica files = %v, want nothing written for a failed recall", files)
		}
	})

	t.Run("a linked parent refuses the materialisation", func(t *testing.T) {
		root := t.TempDir()
		store := memory.NewMemoryOriginalStore()
		materializer, err := NewMaterializer(root, store, invariantScope())
		if err != nil {
			t.Fatalf("assemble the materializer: %v", err)
		}
		ref := invariantPut(t, store, "original-1", []byte("bytes"))
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(root, "replica")); err != nil {
			t.Fatalf("plant the linked parent: %v", err)
		}
		if _, err := materializer.Materialize(ctx, ref, "tool"); !errors.Is(err, ErrReplicaParentLinked) {
			t.Fatalf("materialisation across a link = %v, want %v", err, ErrReplicaParentLinked)
		}
		entries, err := os.ReadDir(outside)
		if err != nil {
			t.Fatalf("read the link target: %v", err)
		}
		if len(entries) != 0 {
			t.Fatalf("link target entries = %v, want no replica written through the link", entries)
		}
	})

	t.Run("the construction refusals are named", func(t *testing.T) {
		store := memory.NewMemoryOriginalStore()
		if _, err := NewMaterializer("", store, invariantScope()); !errors.Is(err, ErrReplicaRootRequired) {
			t.Fatalf("empty root = %v, want %v", err, ErrReplicaRootRequired)
		}
		if _, err := NewMaterializer(t.TempDir(), store, memory.ReadScope{}); !errors.Is(err, memory.ErrScopeRequired) {
			t.Fatalf("empty scope = %v, want %v", err, memory.ErrScopeRequired)
		}
		root := t.TempDir()
		cleaner, err := NewReplicaCleaner(t.TempDir())
		if err != nil {
			t.Fatalf("assemble a foreign cleaner: %v", err)
		}
		if _, err := NewMaterializer(root, store, invariantScope(), WithCleaner(cleaner)); !errors.Is(err, ErrCleanerRootMismatch) {
			t.Fatalf("foreign cleaner = %v, want %v", err, ErrCleanerRootMismatch)
		}
	})
}

// invariantMaterializerAt builds one materializer at an already chosen root over the given store.
func invariantMaterializerAt(t *testing.T, root string, store memory.ScopedOriginalStore) (*Materializer, *memory.MemoryOriginalStore, string) {
	t.Helper()
	materializer, err := NewMaterializer(root, store, invariantScope())
	if err != nil {
		t.Fatalf("assemble the second materializer: %v", err)
	}
	memoryStore, ok := store.(*memory.MemoryOriginalStore)
	if !ok {
		t.Fatalf("store = %T, want the in-memory original store", store)
	}
	return materializer, memoryStore, root
}

// TestReplicaLifecycleInvariants pins one replica's life from write to reclaim to rebuild.
// - a missing replica is written whole and its bytes answer the original exactly
// - a standing replica is reused while its bytes still match and refreshed once they do not
// - a working copy holds its bytes against reclamation and against an in-place rewrite
// - reclamation never takes an active copy and a closed one is taken back
// - a reclaimed replica is written again from its still effective original
// - the read face serves bytes only inside its scope and rebuilds a missing replica before serving
// 契约: docs/wiki/platform/platform-subsystems.md#workspace-scratch
func TestReplicaLifecycleInvariants(t *testing.T) {
	ctx := context.Background()

	t.Run("a missing replica is written whole from its original", func(t *testing.T) {
		materializer, store, _ := invariantMaterializer(t)
		body := []byte("the whole original body")
		ref := invariantPut(t, store, "original-1", body)

		replica, err := materializer.Materialize(ctx, ref, "tool")
		if err != nil {
			t.Fatalf("materialize: %v", err)
		}
		if replica.Path == "" {
			t.Fatalf("replica path = empty, want the path declared after the bytes landed")
		}
		written, err := os.ReadFile(replica.Path)
		if err != nil {
			t.Fatalf("read the replica: %v", err)
		}
		if string(written) != string(body) {
			t.Fatalf("replica bytes = %q, want the original bytes", written)
		}
		if replica.ActiveRW {
			t.Fatalf("replica ActiveRW = true, want a plain materialisation not marked active")
		}
	})

	t.Run("a stale replica is refreshed in place", func(t *testing.T) {
		materializer, store, _ := invariantMaterializer(t)
		body := []byte("the effective original body")
		ref := invariantPut(t, store, "original-1", body)
		replica, err := materializer.Materialize(ctx, ref, "tool")
		if err != nil {
			t.Fatalf("materialize: %v", err)
		}
		if err := os.WriteFile(replica.Path, []byte("stale scratch bytes"), 0o644); err != nil {
			t.Fatalf("age the replica: %v", err)
		}
		again, err := materializer.Materialize(ctx, ref, "tool")
		if err != nil {
			t.Fatalf("materialize beside a stale replica: %v", err)
		}
		if again.Path != replica.Path {
			t.Fatalf("path = %q, want the standing replica shadowed in place at %q", again.Path, replica.Path)
		}
		written, err := os.ReadFile(replica.Path)
		if err != nil {
			t.Fatalf("read the refreshed replica: %v", err)
		}
		if string(written) != string(body) {
			t.Fatalf("refreshed replica = %q, want the effective original", written)
		}
	})

	t.Run("an open working copy keeps its bytes and survives reclamation", func(t *testing.T) {
		root := t.TempDir()
		cleaner, err := NewReplicaCleaner(root)
		if err != nil {
			t.Fatalf("assemble the cleaner: %v", err)
		}
		store := memory.NewMemoryOriginalStore()
		materializer, err := NewMaterializer(root, store, invariantScope(), WithCleaner(cleaner))
		if err != nil {
			t.Fatalf("assemble the materializer: %v", err)
		}
		body := []byte("the original bytes a reader holds")
		ref := invariantPut(t, store, "original-1", body)

		active, err := materializer.Open(ctx, ref, "tool")
		if err != nil {
			t.Fatalf("open a working copy: %v", err)
		}
		if !active.ActiveRW {
			t.Fatalf("working copy ActiveRW = false, want the open handle marked active")
		}
		if err := os.WriteFile(active.Path, []byte("stale scratch bytes"), 0o644); err != nil {
			t.Fatalf("age the held replica: %v", err)
		}
		if _, err := materializer.Materialize(ctx, ref, "tool"); err != nil {
			t.Fatalf("materialize beside a held copy: %v", err)
		}
		held, err := os.ReadFile(active.Path)
		if err != nil {
			t.Fatalf("read the held replica: %v", err)
		}
		if string(held) != "stale scratch bytes" {
			t.Fatalf("held replica = %q, want the rewrite deferred while the copy is open", held)
		}
		if err := cleaner.CleanupStartup(ctx); err != nil {
			t.Fatalf("startup reclamation: %v", err)
		}
		if _, err := os.Lstat(active.Path); err != nil {
			t.Fatalf("held replica after reclamation = %v, want it kept", err)
		}

		active.Close()
		active.Close()
		if err := cleaner.CleanupStartup(ctx); err != nil {
			t.Fatalf("startup reclamation after close: %v", err)
		}
		if _, err := os.Lstat(active.Path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("closed replica after reclamation = %v, want it taken back", err)
		}
		var nilReplica *ActiveReplica
		nilReplica.Close()
	})

	t.Run("a reclaimed replica is rebuilt from its original", func(t *testing.T) {
		root := t.TempDir()
		cleaner, err := NewReplicaCleaner(root)
		if err != nil {
			t.Fatalf("assemble the cleaner: %v", err)
		}
		store := memory.NewMemoryOriginalStore()
		materializer, err := NewMaterializer(root, store, invariantScope(), WithCleaner(cleaner))
		if err != nil {
			t.Fatalf("assemble the materializer: %v", err)
		}
		body := []byte("bytes that outlive their replica")
		ref := invariantPut(t, store, "original-1", body)
		first, err := materializer.Materialize(ctx, ref, "tool")
		if err != nil {
			t.Fatalf("first materialization: %v", err)
		}
		if err := cleaner.CleanupStartup(ctx); err != nil {
			t.Fatalf("reclaim the replica: %v", err)
		}
		if _, err := os.Lstat(first.Path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("reclaimed replica = %v, want it gone", err)
		}
		second, err := materializer.Materialize(ctx, ref, "tool")
		if err != nil {
			t.Fatalf("rebuild: %v", err)
		}
		if second.Path != first.Path {
			t.Fatalf("rebuilt path = %q, want the stable path %q", second.Path, first.Path)
		}
		written, err := os.ReadFile(second.Path)
		if err != nil {
			t.Fatalf("read the rebuilt replica: %v", err)
		}
		if string(written) != string(body) {
			t.Fatalf("rebuilt replica = %q, want the original bytes", written)
		}
	})

	t.Run("the read face serves bytes only inside its scope and rebuilds a missing replica", func(t *testing.T) {
		materializer, store, _ := invariantMaterializer(t)
		body := []byte("segmented original bytes")
		ref := invariantPut(t, store, "original-1", body)
		reader := materializer.Reader()
		if reader.Scope() != invariantScope() {
			t.Fatalf("reader scope = %+v, want the trusted scope", reader.Scope())
		}
		outside := invariantRef("original-1")
		outside.Owner = event.Address{AgentID: "agent-b", SessionID: "session-9"}
		if _, err := reader.ReadSegment(ctx, outside, "tool", memory.SegmentRequest{}); !errors.Is(err, ErrReplicaUnauthorized) {
			t.Fatalf("read outside the scope = %v, want %v", err, ErrReplicaUnauthorized)
		}
		if _, err := reader.ReadSegment(ctx, invariantRef("no-such-original"), "tool", memory.SegmentRequest{}); !errors.Is(err, ErrOriginalIneffective) {
			t.Fatalf("read of an ineffective original = %v, want %v", err, ErrOriginalIneffective)
		}

		var collected []byte
		request := memory.SegmentRequest{MaxBytes: 5}
		for range 20 {
			segment, err := reader.ReadSegment(ctx, ref, "tool", request)
			if err != nil {
				t.Fatalf("read a segment: %v", err)
			}
			collected = append(collected, segment.Body...)
			if segment.EOF {
				if segment.Next != (memory.SegmentPosition{}) {
					t.Fatalf("EOF next = %+v, want no resumption point", segment.Next)
				}
				break
			}
			request.From = segment.Next
		}
		if string(collected) != string(body) {
			t.Fatalf("segments = %q, want them to concatenate back to the original", collected)
		}
		if _, err := os.Stat(materializer.replicaPath(ref, "tool")); err != nil {
			t.Fatalf("replica after the read = %v, want it materialized before the bytes were served", err)
		}
	})

	t.Run("a working copy needs a cleaner to protect it", func(t *testing.T) {
		materializer, store, _ := invariantMaterializer(t)
		ref := invariantPut(t, store, "original-1", []byte("bytes"))
		if _, err := materializer.Open(ctx, ref, "tool"); !errors.Is(err, ErrReplicaProtectionUnavailable) {
			t.Fatalf("open without a cleaner = %v, want %v", err, ErrReplicaProtectionUnavailable)
		}
		if _, err := materializer.Open(ctx, invariantRef(""), "tool"); !errors.Is(err, memory.ErrKeyRequired) {
			t.Fatalf("open without a key = %v, want %v", err, memory.ErrKeyRequired)
		}
	})
}

// TestReclamationInvariants pins what one reclamation pass may ever take.
// - the construction and pass bounds are refused by name when they cannot hold
// - age reclamation takes only replicas idle past the bound and keeps fresher ones
// - count reclamation keeps the newest replicas and never counts a held one against the bound
// - reclamation never follows a link, never takes a staging file and never leaves its own area
// - a held replica stays whatever the bound says and the loop refuses a second run
// 契约: docs/wiki/platform/platform-subsystems.md#workspace-scratch
func TestReclamationInvariants(t *testing.T) {
	ctx := context.Background()

	t.Run("the bounds are refused by name", func(t *testing.T) {
		if _, err := NewReplicaCleaner(""); !errors.Is(err, ErrReplicaRootRequired) {
			t.Fatalf("empty root = %v, want %v", err, ErrReplicaRootRequired)
		}
		if _, err := NewReplicaCleaner(t.TempDir(), WithCleanupInterval(0)); !errors.Is(err, ErrCleanupIntervalInvalid) {
			t.Fatalf("zero interval = %v, want %v", err, ErrCleanupIntervalInvalid)
		}
		if _, err := NewReplicaCleaner(t.TempDir(), WithReplicaMaxAge(-time.Second)); !errors.Is(err, ErrCleanupAgeInvalid) {
			t.Fatalf("negative age = %v, want %v", err, ErrCleanupAgeInvalid)
		}
		if _, err := NewReplicaCleaner(t.TempDir(), WithReplicaMaxCount(-1)); !errors.Is(err, ErrCleanupCountInvalid) {
			t.Fatalf("negative count = %v, want %v", err, ErrCleanupCountInvalid)
		}
		cleaner, err := NewReplicaCleaner(t.TempDir())
		if err != nil {
			t.Fatalf("assemble the cleaner: %v", err)
		}
		if err := cleaner.CleanupByAge(ctx, -time.Second); !errors.Is(err, ErrCleanupAgeInvalid) {
			t.Fatalf("negative pass age = %v, want %v", err, ErrCleanupAgeInvalid)
		}
		if err := cleaner.CleanupByCount(ctx, -1); !errors.Is(err, ErrCleanupCountInvalid) {
			t.Fatalf("negative pass count = %v, want %v", err, ErrCleanupCountInvalid)
		}
		if _, err := cleaner.Acquire(filepath.Join(t.TempDir(), "replica", "x")); !errors.Is(err, ErrCleanupTargetOutOfRange) {
			t.Fatalf("outside target = %v, want %v", err, ErrCleanupTargetOutOfRange)
		}
		if _, err := cleaner.Acquire(filepath.Join(cleaner.replicas, "missing")); !errors.Is(err, ErrCleanupTargetMissing) {
			t.Fatalf("missing target = %v, want %v", err, ErrCleanupTargetMissing)
		}
	})

	t.Run("age reclamation takes only the replicas idle past the bound", func(t *testing.T) {
		root := t.TempDir()
		cleaner, err := NewReplicaCleaner(root)
		if err != nil {
			t.Fatalf("assemble the cleaner: %v", err)
		}
		area := filepath.Join(root, "replica", "original-1")
		if err := os.MkdirAll(area, 0o755); err != nil {
			t.Fatalf("create the replica area: %v", err)
		}
		old := filepath.Join(area, "old")
		fresh := filepath.Join(area, "fresh")
		for path, content := range map[string]string{old: "old", fresh: "fresh"} {
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write %q: %v", path, err)
			}
		}
		past := time.Now().Add(-2 * time.Hour)
		if err := os.Chtimes(old, past, past); err != nil {
			t.Fatalf("age the old replica: %v", err)
		}
		if err := cleaner.CleanupByAge(ctx, time.Hour); err != nil {
			t.Fatalf("age reclamation: %v", err)
		}
		if _, err := os.Lstat(old); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("old replica = %v, want it reclaimed", err)
		}
		if _, err := os.Lstat(fresh); err != nil {
			t.Fatalf("fresh replica = %v, want it kept below the age bound", err)
		}
	})

	t.Run("count reclamation keeps the newest and never takes a held replica", func(t *testing.T) {
		root := t.TempDir()
		cleaner, err := NewReplicaCleaner(root)
		if err != nil {
			t.Fatalf("assemble the cleaner: %v", err)
		}
		area := filepath.Join(root, "replica", "original-1")
		if err := os.MkdirAll(area, 0o755); err != nil {
			t.Fatalf("create the replica area: %v", err)
		}
		paths := []string{filepath.Join(area, "oldest"), filepath.Join(area, "middle"), filepath.Join(area, "newest")}
		for i, path := range paths {
			if err := os.WriteFile(path, []byte("body"), 0o644); err != nil {
				t.Fatalf("write %q: %v", path, err)
			}
			stamp := time.Now().Add(-time.Duration(3-i) * time.Hour)
			if err := os.Chtimes(path, stamp, stamp); err != nil {
				t.Fatalf("stamp %q: %v", path, err)
			}
		}
		release, err := cleaner.Acquire(paths[0])
		if err != nil {
			t.Fatalf("hold the oldest replica: %v", err)
		}
		if err := cleaner.CleanupByCount(ctx, 0); err != nil {
			t.Fatalf("count reclamation: %v", err)
		}
		if _, err := os.Lstat(paths[0]); err != nil {
			t.Fatalf("held replica = %v, want it kept whatever the count bound", err)
		}
		if _, err := os.Lstat(paths[1]); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("middle replica = %v, want the oldest idle one reclaimed", err)
		}
		if _, err := os.Lstat(paths[2]); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("newest replica = %v, want it reclaimed under a zero bound", err)
		}
		release()
		release()
		if err := cleaner.CleanupByCount(ctx, 1); err != nil {
			t.Fatalf("count reclamation after release: %v", err)
		}
		if _, err := os.Lstat(paths[0]); err != nil {
			t.Fatalf("released replica = %v, want it kept as the one newest left", err)
		}
	})

	t.Run("reclamation never follows a link or takes a staging file", func(t *testing.T) {
		root := t.TempDir()
		cleaner, err := NewReplicaCleaner(root)
		if err != nil {
			t.Fatalf("assemble the cleaner: %v", err)
		}
		area := filepath.Join(root, "replica")
		if err := os.MkdirAll(area, 0o755); err != nil {
			t.Fatalf("create the replica area: %v", err)
		}
		outside := t.TempDir()
		userFile := filepath.Join(outside, "user-file")
		if err := os.WriteFile(userFile, []byte("user bytes"), 0o644); err != nil {
			t.Fatalf("write the user file: %v", err)
		}
		if err := os.Symlink(userFile, filepath.Join(area, "linked-file")); err != nil {
			t.Fatalf("plant the file link: %v", err)
		}
		if err := os.Symlink(outside, filepath.Join(area, "linked-dir")); err != nil {
			t.Fatalf("plant the directory link: %v", err)
		}
		staging := filepath.Join(area, replicaStagingPrefix+"half-written")
		if err := os.WriteFile(staging, []byte("half"), 0o644); err != nil {
			t.Fatalf("write the staging file: %v", err)
		}
		userOutside := filepath.Join(root, "user-file")
		if err := os.WriteFile(userOutside, []byte("user bytes"), 0o644); err != nil {
			t.Fatalf("write the replica-root user file: %v", err)
		}
		if err := cleaner.CleanupStartup(ctx); err != nil {
			t.Fatalf("startup reclamation: %v", err)
		}
		for _, path := range []string{userFile, staging, userOutside} {
			if _, err := os.Lstat(path); err != nil {
				t.Fatalf("%q after reclamation = %v, want it untouched", path, err)
			}
		}
		if _, err := os.Lstat(filepath.Join(area, "linked-file")); err != nil {
			t.Fatalf("linked file = %v, want the link left alone as no replica", err)
		}
		target, err := os.ReadDir(outside)
		if err != nil {
			t.Fatalf("read the link target: %v", err)
		}
		if len(target) != 1 || target[0].Name() != "user-file" {
			t.Fatalf("link target entries = %v, want only the user file left untouched", target)
		}
	})

	t.Run("the loop refuses a second run and stops on request", func(t *testing.T) {
		root := t.TempDir()
		cleaner, err := NewReplicaCleaner(root, WithCleanupInterval(time.Hour))
		if err != nil {
			t.Fatalf("assemble the cleaner: %v", err)
		}
		done := make(chan error, 1)
		go func() { done <- cleaner.Run(ctx) }()
		invariantWaitRunning(t, cleaner)
		if err := cleaner.Run(ctx); !errors.Is(err, ErrCleanerAlreadyRunning) {
			t.Fatalf("second run = %v, want %v", err, ErrCleanerAlreadyRunning)
		}
		cleaner.Stop()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("run return = %v, want a clean stop", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("the reclamation loop never ended")
		}
		if err := cleaner.Wait(ctx); err != nil {
			t.Fatalf("wait after stop = %v, want the ended loop reported", err)
		}
	})
}

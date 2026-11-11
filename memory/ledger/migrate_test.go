// The contract of the one-time migration from the retired sharded-snapshot root
// to the journal root the build opens now: the copy lands every key and byte, it
// leaves the source exactly as it read it, and it refuses a target that already
// holds data instead of overwriting a root in use. The source fixture is written
// as the retired layout itself, because the build has no engine that writes it.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// migrateSourceKeys are the keys one migration moves: two namespaces, a key that
// carries no namespace, and a value large enough to cross one page.
func migrateSourceKeys() []Pair {
	return []Pair{
		{Key: "tagent/task/v1/fact:7", Value: []byte(`{"task":"7","state":"done"}`)},
		{Key: "tagent/task/v1/fact:8", Value: []byte(`{"task":"8","state":"running"}`)},
		{Key: "7:evt:1760000000123:1", Value: bytes.Repeat([]byte("segment"), 64)},
		{Key: "7:idx:fact-7", Value: []byte("1760000000123:1:fact-7")},
		{Key: "standalone", Value: nil},
	}
}

// migrateSourceWrite writes the source pairs as the retired sharded-snapshot
// layout — one directory per key namespace under the key-value directory, one
// snapshot file per shard — and returns its directory.
func migrateSourceWrite(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	pairs := map[string]map[string][]byte{}
	for _, pair := range migrateSourceKeys() {
		label := keyNamespaceOf(pair.Key)
		if pairs[label] == nil {
			pairs[label] = map[string][]byte{}
		}
		pairs[label][pair.Key] = pair.Value
	}
	labels := make([]string, 0, len(pairs))
	for label := range pairs {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, label := range labels {
		dir := filepath.Join(root, kvDirName, url.PathEscape(label))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create the retired shard %s: %v", dir, err)
		}
		snapshot, err := json.Marshal(struct {
			Version int               `json:"version"`
			Entries map[string][]byte `json:"entries"`
		}{Version: legacyShardVersion, Entries: pairs[label]})
		if err != nil {
			t.Fatalf("encode the retired shard %s: %v", label, err)
		}
		if err := os.WriteFile(filepath.Join(dir, legacyShardFileName), snapshot, 0o600); err != nil {
			t.Fatalf("write the retired shard %s: %v", label, err)
		}
	}
	return root
}

// legacySnapshotFiles answers the bytes of every snapshot file under root, keyed
// by its path relative to root, so a check can compare the retired layout with
// itself across the copy.
func legacySnapshotFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != legacyShardFileName {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[relative] = content
		return nil
	})
	if err != nil {
		t.Fatalf("list the retired snapshots under %s: %v", root, err)
	}
	return files
}

// TestMigrateShardedRootContract pins the migration of a retired root.
//   - a copy reads back every key and byte through a freshly opened journal
//   - the source answers exactly what it held before the copy
//   - a target that already holds keys is refused by name
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestMigrateShardedRootContract(t *testing.T) {
	ctx := context.Background()
	sourceRoot := migrateSourceWrite(t)
	want := migrateSourceKeys()
	var wantBytes int64
	for _, pair := range want {
		wantBytes += int64(len(pair.Value))
	}

	t.Run("copies_every_key_and_byte", func(t *testing.T) {
		targetRoot := t.TempDir()
		report, err := MigrateShardedRoot(ctx, sourceRoot, targetRoot)
		if err != nil {
			t.Fatalf("migrate the sharded root into %s: %v", targetRoot, err)
		}
		if report.Keys != len(want) || report.Bytes != wantBytes || report.Batches == 0 {
			t.Fatalf("migration report = %+v, want %d keys, %d bytes over at least one batch", report, len(want), wantBytes)
		}
		copied, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: targetRoot})
		if err != nil {
			t.Fatalf("open the journal root %s: %v", targetRoot, err)
		}
		for _, pair := range want {
			held, err := copied.Read(ctx, Fetch{Key: pair.Key})
			if err != nil {
				t.Fatalf("read the copied key %q: %v", pair.Key, err)
			}
			if len(held) != 1 {
				t.Fatalf("the copied root answers %d values for %q, want one", len(held), pair.Key)
			}
			if !bytes.Equal(held[0].Value, pair.Value) {
				t.Fatalf("the copied value of %q = %q, want %q", pair.Key, held[0].Value, pair.Value)
			}
		}
		namespaces, err := copied.Namespaces(ctx)
		if err != nil {
			t.Fatalf("enumerate the copied namespaces: %v", err)
		}
		wantNamespaces := []string{"7", "global", "tagent/task/v1/fact"}
		if len(namespaces) != len(wantNamespaces) {
			t.Fatalf("the copied namespaces = %v, want %v", namespaces, wantNamespaces)
		}
		for at, namespace := range wantNamespaces {
			if namespaces[at] != namespace {
				t.Fatalf("the copied namespaces = %v, want %v", namespaces, wantNamespaces)
			}
		}

		t.Run("source_stays_readable_and_unchanged", func(t *testing.T) {
			before := legacySnapshotFiles(t, sourceRoot)
			if len(before) != 3 {
				t.Fatalf("the retired root holds %d shard snapshots, want 3", len(before))
			}
			secondRoot := t.TempDir()
			again, err := MigrateShardedRoot(ctx, sourceRoot, secondRoot)
			if err != nil {
				t.Fatalf("migrate the same source a second time: %v", err)
			}
			if again.Keys != len(want) || again.Bytes != wantBytes {
				t.Fatalf("the second migration copied %+v, want %d keys and %d bytes", again, len(want), wantBytes)
			}
			after := legacySnapshotFiles(t, sourceRoot)
			if len(after) != len(before) {
				t.Fatalf("the copy left %d shard snapshots, the source held %d", len(after), len(before))
			}
			for path, content := range before {
				if !bytes.Equal(after[path], content) {
					t.Fatalf("the copy rewrote the source snapshot %s", path)
				}
			}
		})

		t.Run("target_not_empty_refused", func(t *testing.T) {
			if _, err := MigrateShardedRoot(ctx, sourceRoot, targetRoot); !errors.Is(err, ErrMigrateTargetNotEmpty) {
				t.Fatalf("a second migration into the filled target returned %v, want the target-not-empty refusal", err)
			}
			if _, err := MigrateShardedRoot(ctx, sourceRoot, sourceRoot); !errors.Is(err, ErrMigrateTargetNotEmpty) {
				t.Fatalf("a migration onto its own source returned %v, want the target-not-empty refusal", err)
			}
		})
	})
}

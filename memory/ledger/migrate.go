// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
//
// The one place that reads the retired sharded-snapshot layout: an operator
// migrating a deployment root reads that layout as the read-only source and
// writes the journal the build opens now. Every other caller reaches storage
// through the atomic store seam and names no layout at all.
package ledger

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	// legacyShardFileName is the snapshot file of one retired shard: every
	// key-value pair of that shard in one JSON object.
	legacyShardFileName = "snapshot.json"
	// legacyShardBackupName is the snapshot a retired commit kept beside the new
	// one until every shard of it landed, so the one left alone is the state that
	// commit did not replace.
	legacyShardBackupName = legacyShardFileName + ".old"
	// legacyShardVersion is the only snapshot version this reader accepts.
	legacyShardVersion = 1
)

// ErrMigrateTargetNotEmpty reports a migration target that already holds keys:
// the copy rewrites nothing, so an operator points it at the new, still empty
// directory instead of a live one.
var ErrMigrateTargetNotEmpty = errors.New("memory: migration target already holds data")

// ErrMigrateSourceUnchanged reports a source that does not read the same way
// twice: the verifying pass over the retired layout names a different key set
// than the copy landed, so the result is refused instead of reporting a copy of a
// root that moved under it.
var ErrMigrateSourceUnchanged = errors.New("memory: migration source changed under the copy")

// ErrMigrateMismatch reports a copied root that does not read back exactly what
// the source held. The copy does not report success for a root it could not
// verify.
var ErrMigrateMismatch = errors.New("memory: migrated root does not read back the source")

// migrateBatchKeys bounds how many keys one migration commit carries, so an
// operator copying a large root pays bounded memory for it.
const migrateBatchKeys = 256

// MigrateReport counts what one migration copied.
type MigrateReport struct {
	// Keys is the number of keys the target now holds.
	Keys int
	// Bytes is the payload the copy committed, summed over its keys.
	Bytes int64
	// Batches is the number of commits the copy took to land them.
	Batches int
}

// MigrateShardedRoot copies every key of a retired sharded-snapshot root into a
// journal root under targetRoot, in bounded batches, and reads the copy back
// through a freshly opened view before it reports success.
//
// The source is read-only: this function only enumerates and reads it, so the
// build that wrote that layout still reads it after the copy, and a migration
// interrupted anywhere leaves the source exactly as it was. The target is
// refused when it already holds keys, so no copy overwrites a root in use. A
// root whose copy does not read back byte for byte returns ErrMigrateMismatch
// naming the key that differs.
func MigrateShardedRoot(ctx context.Context, sourceRoot, targetRoot string) (MigrateReport, error) {
	if sourceRoot == "" || targetRoot == "" {
		return MigrateReport{}, ErrRootRequired
	}
	if sourceRoot == targetRoot {
		return MigrateReport{}, fmt.Errorf("%w: the source and the target are the same directory %s", ErrMigrateTargetNotEmpty, targetRoot)
	}
	target, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: targetRoot})
	if err != nil {
		return MigrateReport{}, fmt.Errorf("open the migration target %s: %w", targetRoot, err)
	}
	held, err := target.Keys(ctx, "")
	if err != nil {
		return MigrateReport{}, fmt.Errorf("enumerate the migration target %s: %w", targetRoot, err)
	}
	if len(held) > 0 {
		return MigrateReport{}, fmt.Errorf("%w: %s holds %d keys", ErrMigrateTargetNotEmpty, targetRoot, len(held))
	}
	report := MigrateReport{}
	batch := Batch{Puts: make([]Pair, 0, migrateBatchKeys)}
	commitBatch := func() error {
		if len(batch.Puts) == 0 {
			return nil
		}
		if err := target.Commit(ctx, batch); err != nil {
			return fmt.Errorf("commit the migration batch at key %q: %w", batch.Puts[0].Key, err)
		}
		report.Batches++
		batch = Batch{Puts: make([]Pair, 0, migrateBatchKeys)}
		return nil
	}
	if err := eachLegacyPair(ctx, sourceRoot, func(pair Pair) error {
		batch.Puts = append(batch.Puts, pair)
		report.Keys++
		report.Bytes += int64(len(pair.Value))
		if len(batch.Puts) < migrateBatchKeys {
			return nil
		}
		return commitBatch()
	}); err != nil {
		return report, err
	}
	if err := commitBatch(); err != nil {
		return report, err
	}
	if err := verifyMigratedRoot(ctx, sourceRoot, targetRoot, report.Keys); err != nil {
		return report, err
	}
	return report, nil
}

// verifyMigratedRoot reads the copied root back through a freshly opened view and
// compares every key the retired layout names with the bytes under it, so a copy
// reported as complete is one a restart of the new build reads whole. A source
// that names a different key set on this second pass is refused as changed under
// the copy instead of being half compared.
func verifyMigratedRoot(ctx context.Context, sourceRoot, targetRoot string, copied int) error {
	reopened, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: targetRoot})
	if err != nil {
		return fmt.Errorf("reopen the migration target %s: %w", targetRoot, err)
	}
	holding, err := reopened.Keys(ctx, "")
	if err != nil {
		return fmt.Errorf("enumerate the copied root %s: %w", targetRoot, err)
	}
	if len(holding) != copied {
		return fmt.Errorf("%w: %s holds %d keys, the copy landed %d", ErrMigrateMismatch, targetRoot, len(holding), copied)
	}
	seen := 0
	if err := eachLegacyPair(ctx, sourceRoot, func(pair Pair) error {
		seen++
		got, err := reopened.Read(ctx, Fetch{Key: pair.Key})
		if err != nil {
			return fmt.Errorf("read the copied key %q: %w", pair.Key, err)
		}
		if len(got) != 1 || !bytes.Equal(got[0].Value, pair.Value) {
			return fmt.Errorf("%w: key %q", ErrMigrateMismatch, pair.Key)
		}
		return nil
	}); err != nil {
		return err
	}
	if seen != copied {
		return fmt.Errorf("%w: %s names %d keys on the verifying pass, the copy landed %d", ErrMigrateSourceUnchanged, sourceRoot, seen, copied)
	}
	return nil
}

// eachLegacyPair streams every pair the retired root holds, one shard at a time,
// so a copy pays bounded memory for a shard of any size. The pairs arrive in the
// shard order and the entry order the snapshots carry.
func eachLegacyPair(ctx context.Context, sourceRoot string, visit func(pair Pair) error) error {
	shards, err := legacyShards(sourceRoot)
	if err != nil {
		return err
	}
	for _, shard := range shards {
		if err := ctx.Err(); err != nil {
			return err
		}
		var visitErr error
		if err := scanLegacyShard(shard.dir, func(key string, value []byte) bool {
			visitErr = visit(Pair{Key: key, Value: value})
			return visitErr == nil
		}); err != nil {
			return err
		}
		if visitErr != nil {
			return visitErr
		}
	}
	return nil
}

// legacyShard is the directory of one shard of a retired root.
type legacyShard struct {
	dir string
}

// legacyShards names the shard directories a retired root holds. A missing
// directory is an empty root, so an operator copying a root nothing ever wrote
// gets an empty copy instead of a failure.
func legacyShards(sourceRoot string) ([]legacyShard, error) {
	dir := filepath.Join(sourceRoot, kvDirName)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("enumerate the retired root %s: %w", sourceRoot, err)
	}
	shards := make([]legacyShard, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		shards = append(shards, legacyShard{dir: filepath.Join(dir, entry.Name())})
	}
	return shards, nil
}

// scanLegacyShard streams the snapshot of one retired shard entry by entry,
// reading one decoded value at a time so a shard is never held whole in memory.
// A missing snapshot is an empty shard, except when the commit that last replaced
// it stopped before landing the new one: the backup it left is then the state the
// shard answers, and this reader takes it without writing the source. Content that
// does not parse as the recorded key-value state is reported as ErrSnapshotCorrupt,
// and a visit that answers false ends the scan without reading the rest.
func scanLegacyShard(dir string, visit func(key string, value []byte) bool) error {
	path := filepath.Join(dir, legacyShardFileName)
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		path = filepath.Join(dir, legacyShardBackupName)
		if file, err = os.Open(path); errors.Is(err, os.ErrNotExist) {
			return nil
		}
	}
	if err != nil {
		return err
	}
	defer file.Close()
	corrupt := func(cause error) error {
		return fmt.Errorf("%w: %s: %w", ErrSnapshotCorrupt, path, cause)
	}
	decoder := json.NewDecoder(bufio.NewReaderSize(file, 64<<10))
	token, err := decoder.Token()
	if err != nil {
		return corrupt(err)
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return corrupt(errors.New("snapshot is not a JSON object"))
	}
	versionSeen := false
	for decoder.More() {
		nameToken, err := decoder.Token()
		if err != nil {
			return corrupt(err)
		}
		name, ok := nameToken.(string)
		if !ok {
			return corrupt(errors.New("snapshot field name is not a string"))
		}
		switch name {
		case "version":
			var version int
			if err := decoder.Decode(&version); err != nil {
				return corrupt(err)
			}
			if version != legacyShardVersion {
				return corrupt(fmt.Errorf("snapshot version %d", version))
			}
			versionSeen = true
		case "entries":
			entriesToken, err := decoder.Token()
			if err != nil {
				return corrupt(err)
			}
			if delimiter, ok := entriesToken.(json.Delim); !ok || delimiter != '{' {
				return corrupt(errors.New("snapshot entries is not a JSON object"))
			}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return corrupt(err)
				}
				key, ok := keyToken.(string)
				if !ok {
					return corrupt(errors.New("snapshot entry key is not a string"))
				}
				var value []byte
				if err := decoder.Decode(&value); err != nil {
					return corrupt(err)
				}
				if visit != nil && !visit(key, value) {
					return nil
				}
			}
			ending, err := decoder.Token()
			if err != nil {
				return corrupt(err)
			}
			if delimiter, ok := ending.(json.Delim); !ok || delimiter != '}' {
				return corrupt(errors.New("snapshot entries does not end"))
			}
		default:
			var raw json.RawMessage
			if err := decoder.Decode(&raw); err != nil {
				return corrupt(err)
			}
		}
	}
	ending, err := decoder.Token()
	if err != nil {
		return corrupt(err)
	}
	if delimiter, ok := ending.(json.Delim); !ok || delimiter != '}' {
		return corrupt(errors.New("snapshot does not end"))
	}
	if !versionSeen {
		return corrupt(errors.New("snapshot carries no version"))
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return corrupt(errors.New("snapshot carries trailing content"))
	}
	return nil
}

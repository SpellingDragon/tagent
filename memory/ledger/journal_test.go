// The checks of the append-only journal: one writer appends whole frames, the
// key index follows a successful write, a torn tail is cut off, and a complete
// frame that fails its checksum refuses the open instead of vanishing.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// journalTestRead opens one log and answers the value held under key beside
// whether the log holds it at all.
func journalTestRead(t *testing.T, dir, key string) ([]byte, bool) {
	t.Helper()
	log, err := openJournalLog(dir)
	if err != nil {
		t.Fatalf("reopen the journal: %v", err)
	}
	defer log.close()
	value, held, err := log.read(key)
	if err != nil {
		t.Fatalf("read %q: %v", key, err)
	}
	return value, held
}

// journalTestAppend commits one record of the given puts.
func journalTestAppend(t *testing.T, log *journalLog, puts ...journalPut) {
	t.Helper()
	if err := log.append(journalRecord{Version: journalFormatVersion, Puts: puts}); err != nil {
		t.Fatalf("append %d put(s): %v", len(puts), err)
	}
}

// TestJournalFraming pins the frame format and the index a write publishes.
// - a complete frame carries its payload length and checksum, and replays in commit order
// - a complete frame whose checksum fails refuses the open
// - a half-written last frame is cut off, keeping every complete record
// - a record whose write failed enters no readable state
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalFraming(t *testing.T) {
	t.Run("round_trips_frames_in_commit_order", func(t *testing.T) {
		dir := t.TempDir()
		log, err := openJournalLog(dir)
		if err != nil {
			t.Fatalf("open the journal: %v", err)
		}
		journalTestAppend(t, log,
			journalPut{Key: "0:evt:00001", Value: []byte("first")},
			journalPut{Key: "0:evt:00002", Value: []byte("second")})
		journalTestAppend(t, log, journalPut{Key: "1:evt:00001", Value: []byte("third")})
		if err := log.sync(); err != nil {
			t.Fatalf("complete the barrier: %v", err)
		}
		size := log.size
		if err := log.close(); err != nil {
			t.Fatalf("close the journal: %v", err)
		}
		raw, err := os.ReadFile(filepath.Join(dir, journalFileName))
		if err != nil {
			t.Fatalf("read the journal file: %v", err)
		}
		if int64(len(raw)) != size {
			t.Fatalf("journal holds %d bytes and the log counted %d", len(raw), size)
		}
		var payloads [][]byte
		end, err := journalReplay(mustOpen(t, filepath.Join(dir, journalFileName)), size,
			func(payload []byte, _ int64) error {
				payloads = append(payloads, payload)
				return nil
			})
		if err != nil {
			t.Fatalf("replay the frames: %v", err)
		}
		if end != size {
			t.Fatalf("replay ended at %d, want the whole %d-byte log", end, size)
		}
		if len(payloads) != 2 {
			t.Fatalf("replay answered %d frame(s), want the 2 commits", len(payloads))
		}
		if got, held := journalTestRead(t, dir, "0:evt:00002"); !held || string(got) != "second" {
			t.Fatalf("read of the first commit = (%q, %v), want the second value", got, held)
		}
		if got, held := journalTestRead(t, dir, "1:evt:00001"); !held || string(got) != "third" {
			t.Fatalf("read of the second commit = (%q, %v), want the third value", got, held)
		}
	})

	t.Run("rejects_corrupt_middle_frame", func(t *testing.T) {
		dir := t.TempDir()
		log, err := openJournalLog(dir)
		if err != nil {
			t.Fatalf("open the journal: %v", err)
		}
		journalTestAppend(t, log, journalPut{Key: "0:evt:00001", Value: []byte("first")})
		firstEnd := log.size
		journalTestAppend(t, log, journalPut{Key: "0:evt:00002", Value: []byte("second")})
		journalTestAppend(t, log, journalPut{Key: "0:evt:00003", Value: []byte("third")})
		if err := log.close(); err != nil {
			t.Fatalf("close the journal: %v", err)
		}
		path := filepath.Join(dir, journalFileName)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read the journal file: %v", err)
		}
		damaged := append([]byte(nil), raw...)
		damaged[firstEnd+journalFrameHeaderBytes+2] ^= 0xff
		if err := os.WriteFile(path, damaged, 0o600); err != nil {
			t.Fatalf("damage the middle frame: %v", err)
		}
		if _, err := openJournalLog(dir); !errors.Is(err, ErrJournalCorrupt) {
			t.Fatalf("open of a log with a damaged middle frame = %v, want the named corruption", err)
		}
	})

	t.Run("truncates_torn_tail", func(t *testing.T) {
		dir := t.TempDir()
		log, err := openJournalLog(dir)
		if err != nil {
			t.Fatalf("open the journal: %v", err)
		}
		journalTestAppend(t, log, journalPut{Key: "0:evt:00001", Value: []byte("first")})
		journalTestAppend(t, log, journalPut{Key: "0:evt:00002", Value: []byte("second")})
		complete := log.size
		journalTestAppend(t, log, journalPut{Key: "0:evt:00003", Value: []byte("third")})
		if err := log.close(); err != nil {
			t.Fatalf("close the journal: %v", err)
		}
		path := filepath.Join(dir, journalFileName)
		if err := os.Truncate(path, complete+(journalFrameHeaderBytes+4)); err != nil {
			t.Fatalf("tear the last frame: %v", err)
		}
		reopened, err := openJournalLog(dir)
		if err != nil {
			t.Fatalf("open of a log with a torn tail: %v", err)
		}
		if reopened.size != complete {
			t.Fatalf("the log kept %d bytes, want the %d complete ones", reopened.size, complete)
		}
		if _, held, err := reopened.read("0:evt:00003"); err != nil || held {
			t.Fatalf("read of the torn record = (%v, %v), want no record", held, err)
		}
		if err := reopened.close(); err != nil {
			t.Fatalf("close the reopened journal: %v", err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read the journal file after the cut: %v", err)
		}
		if int64(len(raw)) != complete {
			t.Fatalf("the file holds %d bytes after the cut, want %d", len(raw), complete)
		}
		if got, held := journalTestRead(t, dir, "0:evt:00002"); !held || string(got) != "second" {
			t.Fatalf("read of the last complete record = (%q, %v), want it kept", got, held)
		}
	})

	t.Run("failed_write_not_visible", func(t *testing.T) {
		dir := t.TempDir()
		log, err := openJournalLog(dir)
		if err != nil {
			t.Fatalf("open the journal: %v", err)
		}
		journalTestAppend(t, log, journalPut{Key: "0:evt:00001", Value: []byte("landed")})
		log.mu.Lock()
		if err := log.file.Close(); err != nil {
			t.Fatalf("close the writer: %v", err)
		}
		failed := log.append(journalRecord{Version: journalFormatVersion,
			Puts: []journalPut{{Key: "0:evt:00002", Value: []byte("unlanded")}}})
		_, held, readErr := log.read("0:evt:00002")
		log.mu.Unlock()
		if failed == nil {
			t.Fatal("an append with no writer reported success")
		}
		if readErr != nil || held {
			t.Fatalf("the failed record is readable = (%v, %v), want no record", held, readErr)
		}
	})
}

// TestJournalKVCommit pins the key-value seam over the journal.
// - a committed batch reads back, survives a reopen, and a delete stops answering
// - a barrier that did not complete reports the named failure
// - a later commit appends beside the earlier records instead of rewriting them
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalKVCommit(t *testing.T) {
	t.Run("commits_read_back_after_barrier", func(t *testing.T) {
		dir := t.TempDir()
		kv, err := openJournalKV(dir)
		if err != nil {
			t.Fatalf("open the journal view: %v", err)
		}
		ctx := context.Background()
		batch := Batch{Puts: []Pair{
			{Key: "0:evt:00001", Value: []byte("first")},
			{Key: "0:evt:00002", Value: []byte("second")},
		}}
		if err := kv.Commit(ctx, batch); err != nil {
			t.Fatalf("commit the batch: %v", err)
		}
		if err := kv.Commit(ctx, Batch{}); err != nil {
			t.Fatalf("complete the barrier: %v", err)
		}
		if err := kv.Commit(ctx, Batch{Deletes: []string{"0:evt:00002"}}); err != nil {
			t.Fatalf("commit the delete: %v", err)
		}
		reopened, err := openJournalKV(dir)
		if err != nil {
			t.Fatalf("reopen the journal view: %v", err)
		}
		pairs, err := reopened.Read(ctx, Fetch{Prefix: "0:evt:"})
		if err != nil {
			t.Fatalf("read the reopened view: %v", err)
		}
		if len(pairs) != 1 || pairs[0].Key != "0:evt:00001" || string(pairs[0].Value) != "first" {
			t.Fatalf("the reopened view answered %+v, want the one committed pair", pairs)
		}
	})

	t.Run("sync_failure_reports_error", func(t *testing.T) {
		dir := t.TempDir()
		kv, err := openJournalKV(dir)
		if err != nil {
			t.Fatalf("open the journal view: %v", err)
		}
		ctx := context.Background()
		if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "0:evt:00001", Value: []byte("first")}}}); err != nil {
			t.Fatalf("commit the batch: %v", err)
		}
		if err := kv.log.file.Close(); err != nil {
			t.Fatalf("close the writer: %v", err)
		}
		if err := kv.Commit(ctx, Batch{}); !errors.Is(err, ErrJournalBarrier) {
			t.Fatalf("barrier with no writer = %v, want the named barrier failure", err)
		}
	})

	t.Run("opens_through_the_tier", func(t *testing.T) {
		root := t.TempDir()
		ctx := context.Background()
		cfg := StorageConfig{Type: StorageJournal, Root: root}
		first, err := OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("open the journal tier: %v", err)
		}
		facts := TierFactLog(first)
		if err := facts.Put(ctx, "tagent/task/v1/fact:task-tier", []byte("the committed task fact")); err != nil {
			t.Fatalf("commit one task fact through the tier: %v", err)
		}
		if err := facts.Put(ctx, "blob:empty-body", nil); err != nil {
			t.Fatalf("commit an empty body through the tier: %v", err)
		}
		if err := facts.Sync(ctx); err != nil {
			t.Fatalf("complete the tier barrier: %v", err)
		}
		reopened, err := OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("reopen the journal tier: %v", err)
		}
		body, held, err := TierFactLog(reopened).Get(ctx, "tagent/task/v1/fact:task-tier")
		if err != nil || !held || string(body) != "the committed task fact" {
			t.Fatalf("the reopened tier answered (%q, %v, %v), want the committed fact", body, held, err)
		}
		empty, held, err := TierFactLog(reopened).Get(ctx, "blob:empty-body")
		if err != nil || !held || len(empty) != 0 {
			t.Fatalf("the reopened tier answered the empty body as (%q, %v, %v), want a held empty value", empty, held, err)
		}
	})

	t.Run("no_snapshot_rewrite", func(t *testing.T) {
		dir := t.TempDir()
		kv, err := openJournalKV(dir)
		if err != nil {
			t.Fatalf("open the journal view: %v", err)
		}
		ctx := context.Background()
		first := Batch{Puts: []Pair{{Key: "0:evt:00001", Value: []byte("first")}}}
		if err := kv.Commit(ctx, first); err != nil {
			t.Fatalf("commit the first batch: %v", err)
		}
		path := filepath.Join(dir, journalFileName)
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read the journal file: %v", err)
		}
		second := Batch{Puts: []Pair{{Key: "0:evt:00002", Value: []byte("second")}}}
		if err := kv.Commit(ctx, second); err != nil {
			t.Fatalf("commit the second batch: %v", err)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read the journal file after the second commit: %v", err)
		}
		if !bytes.HasPrefix(after, before) {
			t.Fatal("the second commit rewrote the records already committed")
		}
		payload, err := json.Marshal(journalRecord{Version: journalFormatVersion,
			Puts: []journalPut{{Key: "0:evt:00002", Value: []byte("second")}}})
		if err != nil {
			t.Fatalf("render the expected frame: %v", err)
		}
		if growth := len(after) - len(before); growth != journalFrameHeaderBytes+len(payload) {
			t.Fatalf("the second commit appended %d bytes, want the %d-byte frame alone",
				growth, journalFrameHeaderBytes+len(payload))
		}
		var snapshots []string
		walkErr := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && entry.Name() == legacyShardFileName {
				snapshots = append(snapshots, path)
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("list snapshot files: %v", walkErr)
		}
		if len(snapshots) != 0 {
			t.Fatalf("the journal directory holds %v, want no whole-shard snapshot", snapshots)
		}
	})
}

// TestJournalRejectsLegacyLayout pins the format boundary of one data directory.
// - a directory holding the retired shard layout is refused by name
// - the refused directory is left exactly as it was, with no journal created
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalRejectsLegacyLayout(t *testing.T) {
	t.Run("refuses_shard_directories", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "0"), 0o755); err != nil {
			t.Fatalf("seed a shard directory: %v", err)
		}
		if _, err := openJournalLog(dir); !errors.Is(err, ErrJournalLegacyLayout) {
			t.Fatalf("open of a legacy directory = %v, want the named layout refusal", err)
		}
	})

	t.Run("legacy_dir_untouched_after_refusal", func(t *testing.T) {
		dir := t.TempDir()
		snapshot := filepath.Join(dir, "0", legacyShardFileName)
		if err := os.MkdirAll(filepath.Dir(snapshot), 0o755); err != nil {
			t.Fatalf("seed the shard directory: %v", err)
		}
		if err := os.WriteFile(snapshot, []byte(`{"version":1,"entries":{}}`), 0o600); err != nil {
			t.Fatalf("seed the shard snapshot: %v", err)
		}
		before, err := os.ReadFile(snapshot)
		if err != nil {
			t.Fatalf("read the seeded snapshot: %v", err)
		}
		if _, err := openJournalLog(dir); !errors.Is(err, ErrJournalLegacyLayout) {
			t.Fatalf("open of a legacy directory = %v, want the named layout refusal", err)
		}
		after, err := os.ReadFile(snapshot)
		if err != nil {
			t.Fatalf("the refusal removed the shard snapshot: %v", err)
		}
		if !bytes.Equal(before, after) {
			t.Fatal("the refusal rewrote the shard snapshot")
		}
		if _, err := os.Stat(filepath.Join(dir, journalFileName)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("the refusal created a journal = %v, want nothing written", err)
		}
	})

	t.Run("tier_refuses_legacy_directory", func(t *testing.T) {
		root := t.TempDir()
		kvDir := filepath.Join(root, kvDirName)
		snapshot := filepath.Join(kvDir, "0", legacyShardFileName)
		if err := os.MkdirAll(filepath.Dir(snapshot), 0o755); err != nil {
			t.Fatalf("seed the retired shard directory: %v", err)
		}
		if err := os.WriteFile(snapshot, []byte(`{"version":1,"entries":{}}`), 0o600); err != nil {
			t.Fatalf("seed the retired shard snapshot: %v", err)
		}
		_, err := OpenTier(context.Background(), StorageConfig{Type: StorageJournal, Root: root})
		if !errors.Is(err, ErrJournalLegacyLayout) {
			t.Fatalf("the journal tier opened a directory holding the retired layout = %v, want the named refusal", err)
		}
	})
}

// mustOpen opens one file for a replay check and registers its close.
func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { file.Close() })
	return file
}

// TestJournalValueSpans pins the index a commit publishes beside its frame.
//
// - A key's index entry states the byte span of its own value token, so a read takes that value alone instead of the frame that carries its neighbours
// - The span of every write answers the bytes the commit carried, whatever the frame neighbours hold
//   - covers an empty value, a large value, an escaped-character key and a null value
//
// - A delete supersedes every span under its key, so the read answers not-held
func TestJournalValueSpans(t *testing.T) {
	t.Run("reads_one_value_without_its_frame", func(t *testing.T) {
		dir := t.TempDir()
		log, err := openJournalLog(dir)
		if err != nil {
			t.Fatalf("open the journal: %v", err)
		}
		large := bytes.Repeat([]byte("a large neighbour "), 4096)
		journalTestAppend(t, log,
			journalPut{Key: "0:evt:large", Value: large},
			journalPut{Key: "0:idx:small", Value: []byte("small")})
		if err := log.close(); err != nil {
			t.Fatalf("close the journal: %v", err)
		}
		reopened, err := openJournalLog(dir)
		if err != nil {
			t.Fatalf("reopen the journal: %v", err)
		}
		defer reopened.close()
		span, held := reopened.index["0:idx:small"]
		if !held || span.deleted {
			t.Fatalf("the small key holds no span after the replay: %+v", span)
		}
		if want := len(jsonTokenOf(t, []byte("small"))); span.valueLength != want {
			t.Fatalf("the small value occupies %d byte(s), want the %d of its own token", span.valueLength, want)
		}
		if size := int64(span.valueLength); size >= int64(len(large)) {
			t.Fatalf("the read of the small value spans %d byte(s) of a frame whose neighbour carries %d", size, len(large))
		}
		for key, want := range map[string][]byte{"0:evt:large": large, "0:idx:small": []byte("small")} {
			got, held, err := reopened.read(key)
			if err != nil || !held {
				t.Fatalf("read %q: %v (held=%v)", key, err, held)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("read %q = %d byte(s), want the %d committed", key, len(got), len(want))
			}
		}
	})

	t.Run("answers_the_bytes_of_every_value_kind", func(t *testing.T) {
		dir := t.TempDir()
		log, err := openJournalLog(dir)
		if err != nil {
			t.Fatalf("open the journal: %v", err)
		}
		puts := []journalPut{
			{Key: "plain", Value: []byte("first")},
			{Key: "empty", Value: []byte{}},
			{Key: "null", Value: nil},
			{Key: `escaped "key" \ tail`, Value: []byte("third")},
			{Key: "unicode-键", Value: []byte("fourth")},
		}
		journalTestAppend(t, log, puts...)
		if err := log.append(journalRecord{Version: journalFormatVersion, Puts: []journalPut{puts[0]}, Deletes: []string{"empty"}}); err != nil {
			t.Fatalf("append the delete: %v", err)
		}
		if err := log.close(); err != nil {
			t.Fatalf("close the journal: %v", err)
		}
		reopened, err := openJournalLog(dir)
		if err != nil {
			t.Fatalf("reopen the journal: %v", err)
		}
		defer reopened.close()
		for _, put := range puts[1:] {
			if put.Key == "empty" {
				continue
			}
			got, held, err := reopened.read(put.Key)
			if err != nil || !held {
				t.Fatalf("read %q: %v (held=%v)", put.Key, err, held)
			}
			if !bytes.Equal(got, put.Value) {
				t.Fatalf("read %q = %q, want %q", put.Key, got, put.Value)
			}
		}
		if _, held, err := reopened.read("empty"); err != nil || held {
			t.Fatalf("the deleted key answers held=%v err=%v, want a tombstone", held, err)
		}
	})
}

// jsonTokenOf answers the JSON token one value marshals to, which is the span a
// commit files beside the key.
func jsonTokenOf(t *testing.T, value []byte) []byte {
	t.Helper()
	token, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal the value: %v", err)
	}
	return token
}

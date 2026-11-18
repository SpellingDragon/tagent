// The checks of the append-only journal: one writer appends whole frames, the
// key index follows a successful write, a torn tail is cut off, and a complete
// frame that fails its checksum refuses the open instead of vanishing.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
package ledger

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	// addressableTestValueBytes is the value size the addressable-index fixtures
	// commit, so a depth of many records carries a log a full replay would read.
	addressableTestValueBytes = 64
	// addressableTestCutBytes is how many committed bytes a fixture removes to
	// build a log that lost bytes its root states.
	addressableTestCutBytes = 300
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

// journalTestWriteLogWithoutAnIndex writes one frame into the log of a directory and
// states no index beside it, which is the root a view that states an address has yet
// to write: no handle of this build opens a log without also opening its address.
func journalTestWriteLogWithoutAnIndex(t *testing.T, dir string, puts ...journalPut) {
	t.Helper()
	journalTestWriteLogBytes(t, dir, journalFrameOf(journalTestPayload(t, puts...)))
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
		end, err := journalReplay(mustOpen(t, filepath.Join(dir, journalFileName)), 0, size,
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
// - a barrier that did not complete reports the named failure, over a writer that accepts the record and refuses the sync
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
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatalf("open the failing barrier: %v", err)
		}
		kv.log.mu.Lock()
		previous := kv.log.file
		kv.log.file = writer
		kv.log.mu.Unlock()
		t.Cleanup(func() {
			reader.Close()
			writer.Close()
			previous.Close()
		})
		if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: "0:evt:00001", Value: []byte("first")}}}); !errors.Is(err, ErrJournalBarrier) {
			t.Fatalf("commit beside a writer that accepts the record and refuses the barrier = %v, want the named barrier failure", err)
		}
	})

	t.Run("opens_through_the_tier", func(t *testing.T) {
		root := t.TempDir()
		ctx := context.Background()
		cfg := StorageConfig{Type: StorageJournal, Root: root, WriteRights: true}
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
		if err := first.Close(); err != nil {
			t.Fatalf("close the tier this view wrote through: %v", err)
		}
		reopened, err := OpenTier(ctx, cfg)
		if err != nil {
			t.Fatalf("reopen the journal tier: %v", err)
		}
		body, err := reopened.Segments.kv.Read(ctx, Fetch{Key: "tagent/task/v1/fact:task-tier"})
		if err != nil || len(body) != 1 || string(body[0].Value) != "the committed task fact" {
			t.Fatalf("the reopened tier answered (%v, %v), want the committed fact", body, err)
		}
		empty, err := reopened.Segments.kv.Read(ctx, Fetch{Key: "blob:empty-body"})
		if err != nil || len(empty) != 1 || len(empty[0].Value) != 0 {
			t.Fatalf("the reopened tier answered the empty body as (%v, %v), want a held empty value", empty, err)
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
		span, held, err := reopened.addr.lookup("0:idx:small")
		if err != nil || !held {
			t.Fatalf("the small key holds no span after the replay: %+v (err=%v)", span, err)
		}
		if want := len(jsonTokenOf(t, []byte("small"))); span.Length != want {
			t.Fatalf("the small value occupies %d byte(s), want the %d of its own token", span.Length, want)
		}
		if size := int64(span.Length); size >= int64(len(large)) {
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

// checkpointFrameValueBytes is the raw size of the value the round trip commits. A
// byte value rides in a frame base64-encoded, four bytes for every three it
// holds, so this stands the payload at about 30.7 MiB: near the 32 MiB ceiling,
// and far over the checkpoint a commit actually writes (a checkpoint states
// about 2 KiB; assembly's TestTheCheckpointStaysBoundedByItsEntries
// measures it).
const checkpointFrameValueBytes = 23 << 20

// overCeilingValueBytes is the raw size of a value whose payload stands past the
// ceiling this build states: base64 puts it at about 35 MiB, over 32 MiB and
// under the 64 MiB a raised ceiling would allow, so a bound inflated back up is
// caught here instead of passing unnoticed.
const overCeilingValueBytes = 25 << 20

// TestJournalCarriesTheFrameworkCheckpointFrame pins the frame ceiling against the record a checkpoint states.
//   - a frame near the ceiling commits and replays byte for byte, so the ceiling bounds a payload the framing reads rather than a checkpoint
//   - a frame declaring a payload past the ceiling is refused by name, so the bound still bounds the reader
//   - a record whose payload stands past the ceiling is refused by name before any frame is written
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalCarriesTheFrameworkCheckpointFrame(t *testing.T) {
	t.Run("a_frame_near_the_ceiling_round_trips", func(t *testing.T) {
		dir := t.TempDir()
		log, err := openJournalLog(dir)
		if err != nil {
			t.Fatalf("open the journal: %v", err)
		}
		checkpoint := bytes.Repeat([]byte("c"), checkpointFrameValueBytes)
		journalTestAppend(t, log, journalPut{Key: "262:evt:00001:000001", Value: checkpoint})
		declared := int(log.size) - journalFrameHeaderBytes
		if declared > journalMaxFrameBytes {
			t.Fatalf("the fixture states a %d-byte payload over the %d-byte ceiling", declared, journalMaxFrameBytes)
		}
		if declared < 30<<20 {
			t.Fatalf("the fixture states a %d-byte payload, want one near the ceiling so the round trip proves the bound", declared)
		}
		if err := log.sync(); err != nil {
			t.Fatalf("sync the journal: %v", err)
		}
		if err := log.close(); err != nil {
			t.Fatalf("close the journal: %v", err)
		}
		value, held := journalTestRead(t, dir, "262:evt:00001:000001")
		if !held {
			t.Fatal("the reopened journal holds no checkpoint record")
		}
		if !bytes.Equal(value, checkpoint) {
			t.Fatalf("the reopened journal answered %d byte(s) of the checkpoint, want the %d it committed", len(value), len(checkpoint))
		}
	})

	t.Run("refuses_a_frame_past_the_ceiling", func(t *testing.T) {
		frame := make([]byte, journalFrameHeaderBytes)
		binary.BigEndian.PutUint32(frame[0:4], uint32(journalMaxFrameBytes+1))
		if _, err := journalPayloadOf(frame); !errors.Is(err, ErrJournalCorrupt) {
			t.Fatalf("a frame declaring %d payload bytes = %v, want the named corruption", journalMaxFrameBytes+1, err)
		}
	})

	t.Run("refuses_a_record_past_the_ceiling", func(t *testing.T) {
		log, err := openJournalLog(t.TempDir())
		if err != nil {
			t.Fatalf("open the journal: %v", err)
		}
		defer log.close()
		value := bytes.Repeat([]byte("c"), overCeilingValueBytes)
		err = log.append(journalRecord{Version: journalFormatVersion, Puts: []journalPut{{Key: "over-the-ceiling", Value: value}}})
		if err == nil {
			t.Fatal("a record past the frame ceiling committed, want the bound to refuse it")
		}
		if !strings.Contains(err.Error(), "frame bound") {
			t.Fatalf("the refusal = %v, want the frame bound named", err)
		}
		if log.size != 0 {
			t.Fatalf("the refused record wrote %d byte(s) of frame, want the bound to refuse it before the write", log.size)
		}
	})
}

// addressableTestOpen opens one log as the view that keeps its addressable index:
// the order file, the key directory and the watermark a commit states.
func addressableTestOpen(t *testing.T, dir string) *journalLog {
	t.Helper()
	log, err := openJournalLog(dir)
	if err != nil {
		t.Fatalf("open the journal: %v", err)
	}
	if err := log.openAddressIndex(true); err != nil {
		log.close()
		t.Fatalf("open the addressable index of %s: %v", dir, err)
	}
	return log
}

// addressableTestCommit commits one record of the given puts and deletes behind its
// barrier, which is what states it to the addressable index.
func addressableTestCommit(t *testing.T, log *journalLog, puts []journalPut, deletes []string) {
	t.Helper()
	record := journalRecord{Version: journalFormatVersion, Deletes: deletes}
	record.Puts = append(record.Puts, puts...)
	if err := log.append(record); err != nil {
		t.Fatalf("append %d put(s) and %d delete(s): %v", len(puts), len(deletes), err)
	}
	if err := log.sync(); err != nil {
		t.Fatalf("confirm %d put(s) and %d delete(s): %v", len(puts), len(deletes), err)
	}
}

// addressableTestPopulate commits depth records of five keys each and closes the
// view, so a later open meets a log whose index files state every one of them.
func addressableTestPopulate(t *testing.T, dir string, depth int) {
	t.Helper()
	log := addressableTestOpen(t, dir)
	body := bytes.Repeat([]byte("v"), addressableTestValueBytes)
	for at := 0; at < depth; at++ {
		puts := make([]journalPut, 0, 5)
		for slot := 0; slot < 5; slot++ {
			puts = append(puts, journalPut{Key: fmt.Sprintf("0:evt:%05d:%d", at, slot), Value: body})
		}
		addressableTestCommit(t, log, puts, nil)
	}
	if err := log.close(); err != nil {
		t.Fatalf("close the journal after %d record(s): %v", depth, err)
	}
}

// addressableTestSnapshot reads every file of one directory, so a test rebuilds the
// states a stopped commit leaves instead of assuming which write landed.
func addressableTestSnapshot(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	walkErr := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		name, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		files[name] = body
		return nil
	})
	if walkErr != nil {
		t.Fatalf("snapshot %s: %v", dir, walkErr)
	}
	return files
}

// addressableTestRestore writes one snapshot into a fresh directory.
func addressableTestRestore(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("open the directory of %s: %v", path, err)
		}
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}

// addressableTestHeader reads the watermark one directory's slot table states.
func addressableTestHeader(t *testing.T, dir string) journalSlotHeader {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, journalSlotFileName))
	if err != nil {
		t.Fatalf("read the slot table of %s: %v", dir, err)
	}
	header, stated := decodeSlotHeader(body)
	if !stated {
		t.Fatalf("%s states no watermark", filepath.Join(dir, journalSlotFileName))
	}
	return header
}

// addressableTestWatermark reads the watermark one directory's slot table states and
// whether it states one at all, so a test holds a table that no barrier confirmed apart
// from a table whose watermark a barrier stated.
func addressableTestWatermark(t *testing.T, dir string) (journalSlotHeader, bool) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, journalSlotFileName))
	if err != nil {
		t.Fatalf("read the slot table of %s: %v", dir, err)
	}
	return decodeSlotHeader(body)
}

// addressableTestTrue is the walk visitor that keeps walking.
func addressableTestTrue(string, journalAddressSpan) (bool, error) { return true, nil }

// TestAddressableIndexOpenReadsTheHeaderAndTheTail pins that one open reads the watermark and the commits past it.
// - a clean open reads the header alone, so the bytes it reads stay flat as the log grows
// - an open beside no header rebuilds the index files from the log, so its bytes grow with the history
// - the control is constructible: dropping the header is what makes the same open read every frame
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestAddressableIndexOpenReadsTheHeaderAndTheTail(t *testing.T) {
	depths := []int{10, 40, 160}
	clean := make([]int64, 0, len(depths))
	control := make([]int64, 0, len(depths))
	for _, depth := range depths {
		dir := t.TempDir()
		addressableTestPopulate(t, dir, depth)

		log := addressableTestOpen(t, dir)
		clean = append(clean, log.addr.openBytes)
		if err := log.close(); err != nil {
			t.Fatalf("close the clean open at depth %d: %v", depth, err)
		}

		if err := os.Remove(filepath.Join(dir, journalSlotFileName)); err != nil {
			t.Fatalf("drop the header at depth %d: %v", depth, err)
		}
		rebuilding := addressableTestOpen(t, dir)
		control = append(control, rebuilding.addr.openBytes)
		if err := rebuilding.close(); err != nil {
			t.Fatalf("close the rebuilding open at depth %d: %v", depth, err)
		}
	}
	last := len(depths) - 1
	if clean[last] > 2*clean[0] {
		t.Fatalf("the clean open read %v bytes at depths %v, want the header alone at every depth",
			clean, depths)
	}
	if control[last] < 3*control[0] {
		t.Fatalf("the rebuilding open read %v bytes at depths %v, want the history to grow the read it pays",
			control, depths)
	}
	if clean[last] >= control[last]/10 {
		t.Fatalf("the clean open read %d bytes beside the %d the rebuilding one read, want the header alone",
			clean[last], control[last])
	}
}

// TestAddressableIndexPointReadAnswersTheValueAtItsAddress pins that one key reads back from its own span.
// - a key committed many commits ago answers its bytes through the address alone
// - a key the log never held and a key a later commit deleted answer absence
// - the read reads one value: the value it answers, never the frame beside it
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestAddressableIndexPointReadAnswersTheValueAtItsAddress(t *testing.T) {
	dir := t.TempDir()
	log := addressableTestOpen(t, dir)
	addressableTestCommit(t, log, []journalPut{
		{Key: "0:evt:00001", Value: []byte("first")},
		{Key: "0:evt:00002", Value: []byte("second")},
	}, nil)
	for at := 0; at < 40; at++ {
		addressableTestCommit(t, log, []journalPut{
			{Key: fmt.Sprintf("0:evt:filler:%03d", at), Value: bytes.Repeat([]byte("f"), addressableTestValueBytes)},
		}, nil)
	}
	addressableTestCommit(t, log, nil, []string{"0:evt:00002"})
	if err := log.close(); err != nil {
		t.Fatalf("close the journal: %v", err)
	}

	reopened := addressableTestOpen(t, dir)
	defer reopened.close()
	value, held, err := reopened.addr.read("0:evt:00001")
	if err != nil || !held {
		t.Fatalf("read the key its address locates: held=%v err=%v, want the committed value", held, err)
	}
	if string(value) != "first" {
		t.Fatalf("the address answered %q, want %q", value, "first")
	}
	for _, key := range []string{"0:evt:00002", "0:evt:absent"} {
		value, held, err := reopened.addr.read(key)
		if err != nil {
			t.Fatalf("read %q: %v", key, err)
		}
		if held {
			t.Fatalf("%q answered %q, want absence: one commit deleted it and one never wrote it", key, value)
		}
	}
	before := reopened.addr.valueReads
	if _, _, err := reopened.addr.read("0:evt:00001"); err != nil {
		t.Fatalf("read the key a second time: %v", err)
	}
	if reads := reopened.addr.valueReads - before; reads != 1 {
		t.Fatalf("the point read read %d value(s), want the one value it answers", reads)
	}
}

// TestAddressableIndexStatesOneCommitOrNoneAcrossAStop pins that a stopped commit lands whole or not at all.
// - every state a stopped commit leaves converges on the same held keys and watermark
// - the keys of a commit whose frame is in the log are all readable, never a prefix of them
// - a barrier changes no byte, so the points that differ are the index files a stop left behind
// - a record written without the watermark behind it states no commit
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestAddressableIndexStatesOneCommitOrNoneAcrossAStop(t *testing.T) {
	base := t.TempDir()
	addressableTestPopulate(t, base, 4)
	before := addressableTestSnapshot(t, base)

	full := t.TempDir()
	addressableTestRestore(t, full, before)
	committing := addressableTestOpen(t, full)
	keys := make([]string, 0, 5)
	puts := make([]journalPut, 0, 5)
	for slot := 0; slot < 5; slot++ {
		key := fmt.Sprintf("0:evt:stopped:%d", slot)
		keys = append(keys, key)
		puts = append(puts, journalPut{Key: key, Value: []byte(fmt.Sprintf("body-%d", slot))})
	}
	addressableTestCommit(t, committing, puts, nil)
	if err := committing.close(); err != nil {
		t.Fatalf("close the committing view: %v", err)
	}
	after := addressableTestSnapshot(t, full)
	want := addressableTestHeader(t, full)

	states := map[string]map[string][]byte{
		"stopped_before_the_frame": addressableTestState(before),
		"stopped_after_the_frame": addressableTestState(before,
			map[string][]byte{journalFileName: after[journalFileName]}),
		"stopped_after_the_order_statement": addressableTestState(before,
			map[string][]byte{journalFileName: after[journalFileName], journalOrderFileName: after[journalOrderFileName]}),
		"stopped_after_the_slot_records": addressableTestState(before,
			map[string][]byte{journalFileName: after[journalFileName], journalOrderFileName: after[journalOrderFileName],
				journalSlotFileName: addressableTestStale(after, before)}),
		"stopped_after_the_watermark": addressableTestState(before,
			map[string][]byte{journalFileName: after[journalFileName], journalOrderFileName: after[journalOrderFileName],
				journalSlotFileName: after[journalSlotFileName]}),
	}
	baseWatermark := int64(len(before[journalFileName]))
	for name, files := range states {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			addressableTestRestore(t, dir, files)
			opened := addressableTestOpen(t, dir)
			defer opened.close()
			held := 0
			for slot, key := range keys {
				value, found, err := opened.addr.read(key)
				if err != nil {
					t.Fatalf("read %q after the stop: %v", key, err)
				}
				if !found {
					continue
				}
				held++
				if string(value) != fmt.Sprintf("body-%d", slot) {
					t.Fatalf("%q answered %q, want %q", key, value, fmt.Sprintf("body-%d", slot))
				}
			}
			if held != 0 && held != len(keys) {
				t.Fatalf("%d of %d key(s) landed after the stop, want the commit whole or absent", held, len(keys))
			}
			watermark := addressableTestHeader(t, dir).ValuesSize
			if watermark != want.ValuesSize && watermark != baseWatermark {
				t.Fatalf("the watermark after the stop states %d, want the commit's %d or the base %d",
					watermark, want.ValuesSize, baseWatermark)
			}
			if watermark == want.ValuesSize && held != len(keys) {
				t.Fatalf("the watermark states the commit at %d beside %d of %d held key(s)",
					watermark, held, len(keys))
			}
			if watermark == baseWatermark && held != 0 {
				t.Fatalf("the watermark states the base at %d beside %d held key(s), want none",
					watermark, held)
			}
		})
	}
}

// addressableTestState answers one base snapshot with the given files written over
// it, which is how a state a stopped commit leaves is rebuilt from files alone.
func addressableTestState(base map[string][]byte, overrides ...map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(base))
	for name, body := range base {
		out[name] = body
	}
	for _, override := range overrides {
		for name, body := range override {
			out[name] = body
		}
	}
	return out
}

// addressableTestStale answers the slot table one snapshot landed with the header the
// snapshot before it stated: the state a commit leaves when its records reached the
// file and the watermark behind them did not. The slot table writes the watermark in
// one place instead of one file per key, so the state is rebuilt by cutting that
// header back rather than by naming the entries that landed.
func addressableTestStale(after, before map[string][]byte) []byte {
	landed := append([]byte(nil), after[journalSlotFileName]...)
	copy(landed, before[journalSlotFileName][:journalSlotHeaderBytes])
	return landed
}

// TestAddressableIndexDiscardsAStatementItsFrameNeverCarried keeps the order file from becoming a second truth.
// - a torn frame is cut at the watermark the root states, so the key it names is absent
// - a complete order statement naming that frame is discarded instead of answered
// - the root and the order file both return to the watermark the commit before it reached
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestAddressableIndexDiscardsAStatementItsFrameNeverCarried(t *testing.T) {
	base := t.TempDir()
	addressableTestPopulate(t, base, 4)
	files := addressableTestSnapshot(t, base)
	baseHeader := addressableTestHeader(t, base)
	baseValues := int64(len(files[journalFileName]))
	baseOrder := int64(len(files[journalOrderFileName]))

	dir := t.TempDir()
	addressableTestRestore(t, dir, files)

	payload, err := json.Marshal(journalRecord{Version: journalFormatVersion,
		Puts: []journalPut{{Key: "0:evt:orphan", Value: []byte("ghost")}}})
	if err != nil {
		t.Fatalf("render the frame of the stopped commit: %v", err)
	}
	frame := journalFrameOf(payload)
	torn := frame[:len(frame)-7]
	tornPath := filepath.Join(dir, journalFileName)
	tornFile, err := os.OpenFile(tornPath, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open the journal to append the torn frame: %v", err)
	}
	if _, err := tornFile.Write(torn); err != nil {
		tornFile.Close()
		t.Fatalf("append the torn frame: %v", err)
	}
	tornFile.Close()

	spans, err := journalSpansAt(payload, baseValues+journalFrameHeaderBytes)
	if err != nil || len(spans.puts) != 1 {
		t.Fatalf("read the spans of the stopped commit: %v", err)
	}
	statement := journalOrderStatement{Seq: baseHeader.Seq + 1, ValuesEnd: baseValues + int64(len(frame)),
		Puts: []journalOrderPut{{Key: spans.puts[0].key, journalAddressSpan: spanOf(spans.puts[0].value)}}}
	line, err := json.Marshal(statement)
	if err != nil {
		t.Fatalf("render the orphan statement: %v", err)
	}
	orderPath := filepath.Join(dir, journalOrderFileName)
	orderFile, err := os.OpenFile(orderPath, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open the order file to append the orphan statement: %v", err)
	}
	if _, err := orderFile.Write(append(line, '\n')); err != nil {
		orderFile.Close()
		t.Fatalf("append the orphan statement: %v", err)
	}
	orderFile.Close()

	opened := addressableTestOpen(t, dir)
	defer opened.close()
	if value, held, err := opened.addr.read("0:evt:orphan"); err != nil || held {
		t.Fatalf("the orphan key answered %q (err=%v), want absence: its frame never landed", value, err)
	}
	keys, err := opened.addr.keys("")
	if err != nil {
		t.Fatalf("list the held keys: %v", err)
	}
	for _, key := range keys {
		if key == "0:evt:orphan" {
			t.Fatalf("the enumeration named the orphan key beside %d held key(s)", len(keys))
		}
	}
	if err := opened.addr.eachNewestFirst(func(key string, _ journalAddressSpan) (bool, error) {
		if key == "0:evt:orphan" {
			t.Fatal("the walk named the orphan key: a statement whose frame never landed is not a commit")
		}
		return true, nil
	}); err != nil {
		t.Fatalf("walk the commits newest first: %v", err)
	}
	if got := addressableTestHeader(t, opened.dir).ValuesSize; got != baseValues {
		t.Fatalf("the watermark states %d, want the %d the whole frames end at", got, baseValues)
	}
	if stat, err := os.Stat(orderPath); err != nil || stat.Size() != baseOrder {
		t.Fatalf("the order file states %v byte(s) (err=%v), want the %d before the orphan statement",
			stat, err, baseOrder)
	}
	if stat, err := os.Stat(tornPath); err != nil || stat.Size() != baseValues {
		t.Fatalf("the journal holds %v byte(s) (err=%v), want the torn frame cut at %d", stat, err, baseValues)
	}
}

// TestAddressableIndexStatesADamagedStatementAgainFromTheLog keeps a statement its checksum refuses from answering a value its commit never wrote.
//   - a write view answers the log's values for a statement whose two offsets were swapped, once per key and in commit order
//   - the write view states the order file again from the log, so the bytes it reads are the bytes its commits wrote
//   - a read view answers the same values and writes no byte, so the walk and the point read answer one value
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestAddressableIndexStatesADamagedStatementAgainFromTheLog(t *testing.T) {
	ctx := context.Background()
	seed := journalTestSeedStatedRoot(t, ctx)
	files := addressableTestSnapshot(t, seed)
	original, err := os.ReadFile(filepath.Join(seed, kvDirName, journalOrderFileName))
	if err != nil {
		t.Fatalf("read the order file of the seeded root: %v", err)
	}
	damaged := journalTestSwappedOffsets(t, original)
	state := addressableTestState(files, map[string][]byte{filepath.Join(kvDirName, journalOrderFileName): damaged})
	want := []string{"0:evt:00003=third", "0:idx:alpha=00001", "0:evt:00001=first"}

	writing := t.TempDir()
	addressableTestRestore(t, writing, state)
	writer, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: writing, WriteRights: true})
	if err != nil {
		t.Fatalf("open the writing view over a damaged statement: %v", err)
	}
	written, ok := writer.(*journalKV)
	if !ok {
		t.Fatalf("the journal engine opened as %T, want the key-value view", writer)
	}
	journalTestRequireWalkAndPointReads(t, ctx, written, want)
	if err := written.Close(); err != nil {
		t.Fatalf("close the writing view: %v", err)
	}
	stated, err := os.ReadFile(filepath.Join(writing, kvDirName, journalOrderFileName))
	if err != nil {
		t.Fatalf("read the order file the writing view left: %v", err)
	}
	if !bytes.Equal(stated, original) {
		t.Fatalf("the writing view left %q, want the %q its commits state: a statement a checksum refuses is stated again from the log", stated, original)
	}

	reading := t.TempDir()
	addressableTestRestore(t, reading, state)
	before := journalTestRootState(t, reading)
	reader, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: reading})
	if err != nil {
		t.Fatalf("open the reading view over a damaged statement: %v", err)
	}
	read, ok := reader.(*journalKV)
	if !ok {
		t.Fatalf("the journal engine opened as %T, want the key-value view", reader)
	}
	journalTestRequireWalkAndPointReads(t, ctx, read, want)
	assertWholeLogNotice(t, read, journalOrderFileName)
	if err := read.Close(); err != nil {
		t.Fatalf("close the reading view: %v", err)
	}
	journalTestRequireSameFiles(t, before, journalTestRootState(t, reading))
	kept, err := os.ReadFile(filepath.Join(reading, kvDirName, journalOrderFileName))
	if err != nil {
		t.Fatalf("read the order file the reading view left: %v", err)
	}
	if !bytes.Equal(kept, damaged) {
		t.Fatalf("the reading view left %q, want the %q it opened: a view holding no write rights states no byte", kept, damaged)
	}
}

// journalTestRequireWalkAndPointReads requires the walk to name the given entries in
// order, twice, and the point read of every one of them to answer the same value.
func journalTestRequireWalkAndPointReads(t *testing.T, ctx context.Context, read *journalKV, want []string) {
	t.Helper()
	for walk := 1; walk <= 2; walk++ {
		walked := make([]string, 0, len(want))
		if err := read.EachCommittedNewestFirst(ctx, func(key string, value []byte) (bool, error) {
			walked = append(walked, key+"="+string(value))
			return true, nil
		}); err != nil {
			t.Fatalf("walk %d beside a damaged order statement: %v", walk, err)
		}
		if got, expected := strings.Join(walked, ","), strings.Join(want, ","); got != expected {
			t.Fatalf("walk %d named %s, want %s: the keys a damaged statement states are named once, at the values their commits wrote", walk, got, expected)
		}
	}
	for _, entry := range want {
		key, value, _ := strings.Cut(entry, "=")
		pairs, err := read.Read(ctx, Fetch{Key: key})
		if err != nil || len(pairs) != 1 || string(pairs[0].Value) != value {
			t.Fatalf("the point read of %q answered %v (err=%v), want %q: the walk and the point read answer one value", key, pairs, err, value)
		}
	}
}

// journalTestSwappedOffsets swaps the two offsets of the first statement of an order
// file, which is the span a flipped byte leaves: every line still parses and names the
// value of another commit.
func journalTestSwappedOffsets(t *testing.T, order []byte) []byte {
	t.Helper()
	out := append([]byte(nil), order...)
	line := out
	if end := bytes.IndexByte(line, '\n'); end >= 0 {
		line = line[:end]
	}
	spans := journalTestOffsetDigits(t, line)
	if len(spans) < 2 {
		t.Fatalf("the first statement states %d offset(s), want the two writes a swap takes", len(spans))
	}
	first, second := spans[0], spans[1]
	if second[1]-second[0] != first[1]-first[0] {
		t.Fatalf("the two offsets are %d and %d digit(s) wide, want one width so the swap keeps the line's byte count",
			first[1]-first[0], second[1]-second[0])
	}
	held := append([]byte(nil), line[first[0]:first[1]]...)
	copy(line[first[0]:first[1]], line[second[0]:second[1]])
	copy(line[second[0]:second[1]], held)
	if bytes.Equal(out, order) {
		t.Fatal("the two offsets are equal, so the damaged statement names the commit it carried")
	}
	return out
}

// journalTestOffsetDigits answers the digit spans of the offsets one order line states.
func journalTestOffsetDigits(t *testing.T, line []byte) [][2]int {
	t.Helper()
	marker := []byte(`"offset":`)
	var out [][2]int
	for at := 0; ; {
		next := bytes.Index(line[at:], marker)
		if next < 0 {
			return out
		}
		start := at + next + len(marker)
		end := start
		for end < len(line) && line[end] >= '0' && line[end] <= '9' {
			end++
		}
		if end == start {
			t.Fatalf("the order line states an offset at %d that carries no digit", start)
		}
		out = append(out, [2]int{start, end})
		at = end
	}
}

// TestAddressableIndexRefusesALogThatLostTheBytesItsHeaderStates pins that a log cut behind its watermark refuses the open.
// - a header stating bytes the log does not hold refuses the open by name
// - the refusal is the journal corruption, so a caller reads it as a fact lost rather than a layout it does not know
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestAddressableIndexRefusesALogThatLostTheBytesItsHeaderStates(t *testing.T) {
	dir := t.TempDir()
	addressableTestPopulate(t, dir, 4)
	header := addressableTestHeader(t, dir)
	if err := os.Truncate(filepath.Join(dir, journalFileName), header.ValuesSize-addressableTestCutBytes); err != nil {
		t.Fatalf("cut the bytes the header covers: %v", err)
	}
	log, err := openJournalLog(dir)
	if !errors.Is(err, ErrJournalCorrupt) {
		if log != nil {
			log.close()
		}
		t.Fatalf("the open of the cut log = %v, want the named corruption", err)
	}
	if !strings.Contains(err.Error(), "beside a header stating") {
		t.Fatalf("the refusal = %v, want the header and the bytes it states named", err)
	}
}

// TestAddressableIndexEnumeratesFromTheOrderFileAlone pins that a newest-first walk reads no key entry and no value.
// - the walk answers the newest statement of each key, so a rewritten key is named once
// - a key a commit deleted is not held, and its older statement does not bring it back
// - the enumeration reads zero values, and the key and namespace listings read none either
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestAddressableIndexEnumeratesFromTheOrderFileAlone(t *testing.T) {
	dir := t.TempDir()
	log := addressableTestOpen(t, dir)
	addressableTestCommit(t, log, []journalPut{
		{Key: "0:evt:alpha", Value: []byte("a1")},
		{Key: "0:evt:beta", Value: []byte("b1")},
	}, nil)
	addressableTestCommit(t, log, []journalPut{
		{Key: "0:evt:alpha", Value: []byte("a2")},
		{Key: "0:evt:gamma", Value: []byte("g1")},
	}, []string{"0:evt:beta"})
	addressableTestCommit(t, log, []journalPut{{Key: "0:evt:gamma", Value: []byte("g2")}}, nil)
	if err := log.close(); err != nil {
		t.Fatalf("close the journal: %v", err)
	}

	opened := addressableTestOpen(t, dir)
	defer opened.close()
	var walked []string
	if err := opened.addr.eachNewestFirst(func(key string, _ journalAddressSpan) (bool, error) {
		walked = append(walked, key)
		return true, nil
	}); err != nil {
		t.Fatalf("walk the commits newest first: %v", err)
	}
	want := []string{"0:evt:gamma", "0:evt:alpha"}
	if strings.Join(walked, ",") != strings.Join(want, ",") {
		t.Fatalf("the walk answered %v, want %v: one statement per held key, newest first", walked, want)
	}
	if opened.addr.valueReads != 0 {
		t.Fatalf("the walk read %d value(s), want none: the order file states the spans", opened.addr.valueReads)
	}
	keys, err := opened.addr.keys("0:evt:")
	if err != nil {
		t.Fatalf("list the held keys: %v", err)
	}
	if wantKeys := "0:evt:alpha,0:evt:gamma"; strings.Join(keys, ",") != wantKeys {
		t.Fatalf("the key listing answered %v, want %s in key order", keys, wantKeys)
	}
	namespaces, err := opened.addr.namespaces()
	if err != nil {
		t.Fatalf("list the namespaces: %v", err)
	}
	if strings.Join(namespaces, ",") != "0" {
		t.Fatalf("the namespaces answered %v, want the one namespace the held keys carry", namespaces)
	}
	if opened.addr.valueReads != 0 {
		t.Fatalf("the listings read %d value(s), want none", opened.addr.valueReads)
	}
	if value, held, err := opened.addr.read("0:evt:gamma"); err != nil || !held || string(value) != "g2" {
		t.Fatalf("the rewritten key answered %q (held=%v err=%v), want its newest value %q", value, held, err, "g2")
	}
}

// TestAddressableIndexIsKeptOnlyByTheViewThatDeclaresIt pins which view states the index.
// - a view that declares the index states the order file, the key directory and the watermark
// - a view that declares none keeps the read face of that index, which states no index of its own
// - the commits a writer stated read back through the address after the writer closes
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func TestAddressableIndexIsKeptOnlyByTheViewThatDeclaresIt(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	writer, err := openJournalKV(dir)
	if err != nil {
		t.Fatalf("open the writing view: %v", err)
	}
	if writer.log.addr == nil {
		t.Fatal("the view that declared the index kept none")
	}
	batch := Batch{Puts: []Pair{
		{Key: "0:evt:00001", Value: []byte("first")},
		{Key: "0:evt:00002", Value: []byte("second")},
		{Key: "0:evt:00003", Value: []byte("third")},
	}}
	if err := writer.Commit(ctx, batch); err != nil {
		t.Fatalf("commit three keys: %v", err)
	}
	reader, err := openJournalView(dir, false)
	if err != nil {
		t.Fatalf("open the reading view beside the writer: %v", err)
	}
	if reader.log.addr == nil || reader.log.addr.window == nil {
		t.Fatal("the view holding no write rights kept no read face, want it to read beside the writer")
	}
	if reader.log.addr.statesIndexFiles() {
		t.Fatal("the view holding no write rights states the index files, want it to read them alone")
	}
	if pairs, err := reader.Read(ctx, Fetch{Key: "0:evt:00002"}); err != nil || len(pairs) != 1 ||
		string(pairs[0].Value) != "second" {
		t.Fatalf("the reading view read %v (err=%v), want the committed bytes", pairs, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close the reading view: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close the writing view: %v", err)
	}
	reopened, err := openJournalKV(dir)
	if err != nil {
		t.Fatalf("reopen the view: %v", err)
	}
	defer reopened.Close()
	for key, want := range map[string]string{"0:evt:00001": "first", "0:evt:00003": "third"} {
		value, held, err := reopened.log.addr.read(key)
		if err != nil || !held || string(value) != want {
			t.Fatalf("%q answered %q (held=%v err=%v), want %q through the address", key, value, held, err, want)
		}
	}
	if reopened.log.addr.openBytes >= journalOrderChunkBytes {
		t.Fatalf("the reopened view read %d bytes, want the root and the tail alone", reopened.log.addr.openBytes)
	}
}

// slotTestRecordOffset answers the byte one key's record sits at inside the table a
// directory holds, by walking that key's probe chain the way the index does.
func slotTestRecordOffset(t *testing.T, dir, key string) (int64, int64) {
	t.Helper()
	header := addressableTestHeader(t, dir)
	body, err := os.ReadFile(filepath.Join(dir, journalSlotFileName))
	if err != nil {
		t.Fatalf("read the slot table of %s: %v", dir, err)
	}
	base := slotAddressOf(key, header.Capacity)
	for step := int64(0); step < journalSlotMaxProbe; step++ {
		index := (base + step) % header.Capacity
		at := journalSlotHeaderBytes + index*header.Stride
		state, name, _, ok := decodeSlotRecord(body[at : at+header.Stride])
		if !ok {
			t.Fatalf("the table states no record at %d", index)
		}
		if state == journalSlotEmpty {
			break
		}
		if name == key {
			return at, header.Stride
		}
	}
	t.Fatalf("the table states no record for %q", key)
	return 0, 0
}

// TestAddressableIndexStatesTheTableAgainBesideARecordItCannotPlace pins the rule a torn record takes.
// - the table has no watermark that truncates a record, so the record carries its own checksum
// - a record cut by a stop is met, and the table is stated again from the order file
// - the key the record held answers its value after the repair, so a cut record loses no fact
// - a listing reads the same held keys, so the repair is not a point-read path alone
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestAddressableIndexStatesTheTableAgainBesideARecordItCannotPlace(t *testing.T) {
	dir := t.TempDir()
	addressableTestPopulate(t, dir, 4)
	wanted := "0:evt:00003:2"
	at, stride := slotTestRecordOffset(t, dir, wanted)
	path := filepath.Join(dir, journalSlotFileName)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the slot table: %v", err)
	}
	before := append([]byte(nil), body...)
	body[at+stride/2] ^= 0xff
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("cut one record of the slot table: %v", err)
	}

	opened := addressableTestOpen(t, dir)
	defer opened.close()
	value, held, err := opened.addr.read(wanted)
	if err != nil {
		t.Fatalf("read %q beside a record it cannot place: %v", wanted, err)
	}
	if !held || !bytes.Equal(value, bytes.Repeat([]byte("v"), addressableTestValueBytes)) {
		t.Fatalf("%q answered %d byte(s) held=%v, want the %d its commit wrote",
			wanted, len(value), held, addressableTestValueBytes)
	}
	listed, err := opened.addr.keys("0:evt:")
	if err != nil {
		t.Fatalf("list the held keys beside a record it cannot place: %v", err)
	}
	if len(listed) != 20 {
		t.Fatalf("the listing named %d key(s), want the 20 every commit wrote", len(listed))
	}
	repaired, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the repaired slot table: %v", err)
	}
	if bytes.Equal(repaired, body) {
		t.Fatal("the table still holds the record it cannot place, want it stated again from the order file")
	}
	if len(repaired) != len(before) {
		t.Fatalf("the repaired table holds %d byte(s), want the %d this depth states", len(repaired), len(before))
	}
}

// TestAddressableIndexDiscardsARecordItsFrameNeverCarried keeps the table from becoming a second truth.
// - a valid record naming a span past the watermark answers absence
// - the listing does not name that key and the namespaces do not name its namespace
// - the record is a whole one, so only the watermark rule rejects it and not the checksum
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestAddressableIndexDiscardsARecordItsFrameNeverCarried(t *testing.T) {
	dir := t.TempDir()
	addressableTestPopulate(t, dir, 4)
	header := addressableTestHeader(t, dir)
	orphan := "0:evt:orphan"
	path := filepath.Join(dir, journalSlotFileName)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the slot table: %v", err)
	}
	base := slotAddressOf(orphan, header.Capacity)
	written := false
	for step := int64(0); step < journalSlotMaxProbe && !written; step++ {
		index := (base + step) % header.Capacity
		at := journalSlotHeaderBytes + index*header.Stride
		state, _, _, ok := decodeSlotRecord(body[at : at+header.Stride])
		if !ok {
			t.Fatalf("the table states no record at %d", index)
		}
		if state != journalSlotEmpty {
			continue
		}
		span := journalAddressSpan{Offset: header.ValuesSize + 4, Length: 8}
		copy(body[at:at+header.Stride], encodeSlotRecord(header.Stride, journalSlotHeld, orphan, span))
		written = true
	}
	if !written {
		t.Fatal("the table holds no empty slot inside the chain of the orphan key")
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("state the orphan record: %v", err)
	}

	opened := addressableTestOpen(t, dir)
	defer opened.close()
	if value, held, err := opened.addr.read(orphan); err != nil || held {
		t.Fatalf("the orphan key answered %q (err=%v), want absence: its span is past the watermark",
			value, err)
	}
	held, err := opened.addr.keys("")
	if err != nil {
		t.Fatalf("list the held keys: %v", err)
	}
	for _, key := range held {
		if key == orphan {
			t.Fatalf("the listing named the orphan key beside %d held key(s)", len(held))
		}
	}
	if len(held) != 20 {
		t.Fatalf("the listing named %d key(s), want the 20 every commit wrote", len(held))
	}
	spaces, err := opened.addr.namespaces()
	if err != nil {
		t.Fatalf("list the namespaces: %v", err)
	}
	if len(spaces) != 1 || spaces[0] != "0" {
		t.Fatalf("the namespaces = %v, want the one namespace the held keys carry", spaces)
	}
}

// TestAddressableIndexKeepsItsAddressIndependentOfTheKeyFamily pins that the address spreads a key family.
// - the keys of one family spread over the slots the table states
// - the longest chain stays inside the probe bound the lookup relies on
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestAddressableIndexKeepsItsAddressIndependentOfTheKeyFamily(t *testing.T) {
	const keys = 20000
	capacity := int64(65536)
	taken := make([]bool, capacity)
	longest := 0
	for at := 0; at < keys/5; at++ {
		for slot := 0; slot < 5; slot++ {
			key := fmt.Sprintf("0:evt:%06d:%d", at, slot)
			base := slotAddressOf(key, capacity)
			step := int64(0)
			for ; step < capacity; step++ {
				index := (base + step) % capacity
				if !taken[index] {
					taken[index] = true
					break
				}
			}
			if int(step)+1 > longest {
				longest = int(step) + 1
			}
		}
	}
	if longest >= journalSlotMaxProbe {
		t.Fatalf("the longest chain of %d keys reached %d slots, want it inside the %d bound",
			keys, longest, journalSlotMaxProbe)
	}
	if longest > 40 {
		t.Fatalf("the longest chain of %d keys reached %d slots, want the family spread", keys, longest)
	}
}

// TestJournalNamespacesAnswerOnlyTheHeldKeys states that the engine answers the namespaces its held keys carry.
// - a namespace whose last held key a commit deleted answers nothing, while the namespace of the keys still held keeps answering
// - the key listing beside it names the surviving key, so the fixture tombstoned a key instead of losing an uncommitted one
// - a fresh open of the same log answers the same namespaces, so the answer follows the log instead of a set the view accumulates
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalNamespacesAnswerOnlyTheHeldKeys(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	kv, err := openJournalKV(root)
	if err != nil {
		t.Fatalf("open the journal view: %v", err)
	}
	commit := func(view *journalKV, batch Batch, what string) {
		t.Helper()
		if err := view.Commit(ctx, batch); err != nil {
			t.Fatalf("commit the %s: %v", what, err)
		}
	}
	commit(kv, Batch{Puts: []Pair{
		{Key: "0:evt:kept", Value: []byte("k1")},
		{Key: "ref:dropped", Value: []byte("r1")},
	}}, "two keys of two namespaces")
	commit(kv, Batch{Deletes: []string{"ref:dropped"}}, "tombstone of the only ref key")

	assertHeld := func(view *journalKV, when string) {
		t.Helper()
		namespaces, err := view.Namespaces(ctx)
		if err != nil {
			t.Fatalf("list the namespaces %s: %v", when, err)
		}
		if want := "0"; strings.Join(namespaces, ",") != want {
			t.Fatalf("the namespaces %s = %v, want %s: the only ref key is tombstoned", when, namespaces, want)
		}
		keys, err := view.Keys(ctx, "")
		if err != nil {
			t.Fatalf("list the keys %s: %v", when, err)
		}
		if want := "0:evt:kept"; strings.Join(keys, ",") != want {
			t.Fatalf("the held keys %s = %v, want %s", when, keys, want)
		}
	}
	assertHeld(kv, "of the view that committed the tombstone")
	if err := kv.Close(); err != nil {
		t.Fatalf("close the journal view: %v", err)
	}
	reopened, err := openJournalKV(root)
	if err != nil {
		t.Fatalf("reopen the journal view: %v", err)
	}
	defer reopened.Close()
	assertHeld(reopened, "of a fresh open of the same log")
	commit(reopened, Batch{Puts: []Pair{{Key: "ref:held", Value: []byte("r2")}}}, "second ref key")
	namespaces, err := reopened.Namespaces(ctx)
	if err != nil {
		t.Fatalf("list the namespaces beside a held ref key: %v", err)
	}
	if want := "0,ref"; strings.Join(namespaces, ",") != want {
		t.Fatalf("the namespaces beside a held ref key = %v, want %s", namespaces, want)
	}
}

// TestAddressableIndexKeepsTheLoadOfATableItReopens states that a table reopened beside its log holds the load its slots carry.
// - a session past the load bound closes a table that states the doubled shape
// - the sessions beside it file fewer keys than the bound of the table they meet, so the slots they fill are load no count of their own reaches
// - the commits that fill those slots and the one after them answer their keys instead of a table that states no free slot
// - the shape the last close states is wider than the one the first session closed, so the filled slots asked for the room
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestAddressableIndexKeepsTheLoadOfATableItReopens(t *testing.T) {
	dir := t.TempDir()
	body := bytes.Repeat([]byte("v"), addressableTestValueBytes)
	shape := int64(journalSlotCapacity * 2)
	first := journalSlotCapacity*journalSlotLoadPercent/100 + 8
	second := int(shape/2 - 20)
	third := int(shape) - first - second
	at := 0
	file := func(keys int) {
		t.Helper()
		log := addressableTestOpen(t, dir)
		puts := make([]journalPut, 0, keys)
		for step := 0; step < keys; step++ {
			puts = append(puts, journalPut{Key: fmt.Sprintf("0:load:%05d", at+step), Value: body})
		}
		addressableTestCommit(t, log, puts, nil)
		at += keys
		if err := log.close(); err != nil {
			t.Fatalf("close the journal after %d key(s): %v", at, err)
		}
	}

	file(first)
	firstShape := addressableTestHeader(t, dir)
	if firstShape.Capacity != shape {
		t.Fatalf("the table states %d slot(s) after %d key(s), want the %d the load bound doubles to",
			firstShape.Capacity, at, shape)
	}

	file(second)
	file(third)
	log := addressableTestOpen(t, dir)
	addressableTestCommit(t, log, []journalPut{{Key: "0:load:next", Value: []byte("next")}}, nil)
	if err := log.close(); err != nil {
		t.Fatalf("close the journal beside the slots the sessions filled: %v", err)
	}

	filled := addressableTestHeader(t, dir)
	if filled.Capacity <= firstShape.Capacity {
		t.Fatalf("the table states %d slot(s) beside %d filled, want the slots past the bound to double it",
			filled.Capacity, at)
	}
	value, held := journalTestRead(t, dir, "0:load:next")
	if !held || !bytes.Equal(value, []byte("next")) {
		t.Fatalf("the key past the filled slots answered %q (held=%v), want the bytes it committed", value, held)
	}
}

// TestAddressableIndexStatesTheTableAgainBesideALoadNoTableHolds states that a table whose header states a load its slots cannot carry is not read through.
// - a header stating more used slots than the table holds, and one stating a negative count, are both refused
// - the open counts the load from the log instead of reading the count that header stated
// - the header the repair writes states that load, and the keys the table held answer their bytes
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestAddressableIndexStatesTheTableAgainBesideALoadNoTableHolds(t *testing.T) {
	body := bytes.Repeat([]byte("v"), addressableTestValueBytes)
	holds := journalSlotCapacity*journalSlotLoadPercent/100 + 38
	wanted := "0:load:00007"
	for name, stated := range map[string]func(journalSlotHeader) int64{
		"more_used_slots_than_the_table_holds": func(header journalSlotHeader) int64 { return header.Capacity + 1 },
		"a_negative_count":                     func(journalSlotHeader) int64 { return -1 },
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			log := addressableTestOpen(t, dir)
			puts := make([]journalPut, 0, holds)
			for at := 0; at < holds; at++ {
				puts = append(puts, journalPut{Key: fmt.Sprintf("0:load:%05d", at), Value: body})
			}
			addressableTestCommit(t, log, puts, nil)
			if err := log.close(); err != nil {
				t.Fatalf("close the journal after %d key(s): %v", holds, err)
			}

			path := filepath.Join(dir, journalSlotFileName)
			table, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read the slot table of %s: %v", dir, err)
			}
			header := addressableTestHeader(t, dir)
			if header.Used != int64(holds) {
				t.Fatalf("the header states %d used slot(s) beside %d key(s), want the count the slots hold",
					header.Used, holds)
			}
			header.Used = stated(header)
			copy(table[:journalSlotHeaderBytes], encodeSlotHeader(header))
			if err := os.WriteFile(path, table, 0o600); err != nil {
				t.Fatalf("state the header of %s: %v", path, err)
			}
			if patched := addressableTestHeader(t, dir); patched.Used != header.Used {
				t.Fatalf("the header states %d used slot(s), want the %d this case wrote", patched.Used, header.Used)
			}

			opened := addressableTestOpen(t, dir)
			defer opened.close()
			if opened.addr.used != int64(holds) {
				t.Fatalf("the table reopened holding %d used slot(s), want the %d the log states instead of the count its header stated",
					opened.addr.used, holds)
			}
			repaired := addressableTestHeader(t, dir)
			if repaired.Used != int64(holds) {
				t.Fatalf("the repaired header states %d used slot(s), want the %d its slots hold", repaired.Used, holds)
			}
			value, held, err := opened.addr.read(wanted)
			if err != nil {
				t.Fatalf("read %q beside a repaired header: %v", wanted, err)
			}
			if !held || !bytes.Equal(value, body) {
				t.Fatalf("%q answered %d byte(s) held=%v, want the %d its commit wrote", wanted, len(value), held, len(body))
			}
		})
	}
}

// journalTestRootState states every file under dir by size and modification time, so a
// test compares a root as it stands instead of assuming which files it holds.
func journalTestRootState(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	walkErr := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		stat, statErr := entry.Info()
		if statErr != nil {
			return statErr
		}
		name, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		files[name] = fmt.Sprintf("%d byte(s), modified %d", stat.Size(), stat.ModTime().UnixNano())
		return nil
	})
	if walkErr != nil {
		t.Fatalf("state the files of %s: %v", dir, walkErr)
	}
	return files
}

// journalTestRequireSameFiles fails with the first file whose size or modification time
// two states of one root differ in, which is the write a reading view must not make.
func journalTestRequireSameFiles(t *testing.T, before, after map[string]string) {
	t.Helper()
	for name, state := range before {
		if after[name] != state {
			t.Fatalf("%s read %q after the reading view, want the %q it held before: a view holding no write rights writes no file", name, after[name], state)
		}
	}
	for name, state := range after {
		if _, held := before[name]; !held {
			t.Fatalf("the reading view created %s holding %q, want no file a root did not hold before", name, state)
		}
	}
}

// TestJournalReadViewWritesNothingBesideTheWriter pins that a view holding no write rights opens the journal and its index without writing either.
//   - a settled root, a root whose writer stands between barriers and a root stating no index all keep every file as it stood
//   - the root stating no index answers the whole log and gains no index file
//   - a read view answers the commits of its window, at spans the stated watermark alone would reject
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func TestJournalReadViewWritesNothingBesideTheWriter(t *testing.T) {
	ctx := context.Background()

	t.Run("settled_root", func(t *testing.T) {
		root := t.TempDir()
		writer, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: root, WriteRights: true})
		if err != nil {
			t.Fatalf("open the writing view: %v", err)
		}
		if err := writer.Commit(ctx, Batch{Puts: []Pair{
			{Key: "0:evt:00001", Value: []byte("first")},
			{Key: "0:idx:alpha", Value: []byte("00001")},
		}}); err != nil {
			t.Fatalf("commit two keys: %v", err)
		}
		written, ok := writer.(*journalKV)
		if !ok {
			t.Fatalf("the journal engine opened as %T, want the key-value view", writer)
		}
		if err := written.Close(); err != nil {
			t.Fatalf("close the writing view: %v", err)
		}
		before := journalTestRootState(t, root)
		reader, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: root})
		if err != nil {
			t.Fatalf("open the reading view of a settled root: %v", err)
		}
		read, ok := reader.(*journalKV)
		if !ok {
			t.Fatalf("the journal engine opened as %T, want the key-value view", reader)
		}
		value, held, err := read.log.addr.read("0:evt:00001")
		if err != nil || !held || string(value) != "first" {
			t.Fatalf("the reading view read %q (held=%v err=%v), want the committed bytes", value, held, err)
		}
		if err := read.Close(); err != nil {
			t.Fatalf("close the reading view: %v", err)
		}
		journalTestRequireSameFiles(t, before, journalTestRootState(t, root))
		if got := read.log.addr.window.end - read.log.addr.valuesSize; got != 0 {
			t.Fatalf("the settled root left a window of %d byte(s), want none: its writer stated every commit before it closed", got)
		}
	})

	t.Run("writer_between_barriers", func(t *testing.T) {
		root := t.TempDir()
		writer, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: root, WriteRights: true})
		if err != nil {
			t.Fatalf("open the writing view: %v", err)
		}
		written, ok := writer.(*journalKV)
		if !ok {
			t.Fatalf("the journal engine opened as %T, want the key-value view", writer)
		}
		if err := writer.Commit(ctx, Batch{Puts: []Pair{
			{Key: "0:evt:00001", Value: []byte("first")},
			{Key: "0:evt:00002", Value: []byte("second")},
			{Key: "0:idx:alpha", Value: []byte("00001")},
		}}); err != nil {
			t.Fatalf("commit three keys: %v", err)
		}
		if err := writer.Commit(ctx, Batch{
			Puts:    []Pair{{Key: "0:evt:00003", Value: []byte("third")}},
			Deletes: []string{"0:evt:00002"},
		}); err != nil {
			t.Fatalf("commit a key and a delete: %v", err)
		}
		before := journalTestRootState(t, root)
		reader, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: root})
		if err != nil {
			t.Fatalf("open the reading view beside the writer: %v", err)
		}
		read, ok := reader.(*journalKV)
		if !ok {
			t.Fatalf("the journal engine opened as %T, want the key-value view", reader)
		}
		stated, windowEnd := read.log.addr.valuesSize, read.log.addr.window.end
		if windowEnd <= stated {
			t.Fatalf("the reading view found no window: the log reads to %d and the stated watermark is %d", windowEnd, stated)
		}
		if windowEnd != read.log.addr.log.size {
			t.Fatalf("the window ends at %d, want the log's readable end %d", windowEnd, read.log.addr.log.size)
		}
		if staged := written.log.addr.staged; staged >= journalIndexSyncBytes {
			t.Fatalf("the writer staged %d index byte(s) at the open, over the %d a statement budget covers", staged, journalIndexSyncBytes)
		}
		t.Logf("window: watermark %d, log end %d, %d journal byte(s) past the watermark, %d index byte(s) staged, committed %d key(s)",
			stated, windowEnd, windowEnd-stated, written.log.addr.staged, len(read.log.addr.window.order))
		for _, key := range []string{"0:evt:00001", "0:evt:00003", "0:idx:alpha"} {
			folded, named := read.log.addr.window.values[key]
			span := spanOf(folded)
			committed, committedHeld, commitErr := written.log.read(key)
			reached, reachedHeld, readErr := read.log.addr.read(key)
			if commitErr != nil || readErr != nil {
				t.Fatalf("read %q: writer %v, reading view %v", key, commitErr, readErr)
			}
			if !committedHeld || !reachedHeld || !bytes.Equal(committed, reached) {
				t.Fatalf("%q reads %q (held=%v) beside the writer and %q (held=%v) through the reading view, want the same value",
					key, committed, committedHeld, reached, reachedHeld)
			}
			if !named || span.Offset < stated {
				t.Fatalf("%q sits at %d in the window, want a key the stated watermark %d leaves unstated", key, span.Offset, stated)
			}
			if !read.log.addr.stated(span) {
				t.Fatalf("%q states a span past the watermark %d that the reading view rejects, want the window's own end %d", key, stated, windowEnd)
			}
		}
		if _, held, err := read.log.addr.read("0:evt:00002"); err != nil || held {
			t.Fatalf("the deleted key reads held=%v (err=%v) through the reading view, want the tombstone its window states", held, err)
		}
		if err := read.Close(); err != nil {
			t.Fatalf("close the reading view: %v", err)
		}
		journalTestRequireSameFiles(t, before, journalTestRootState(t, root))
		if err := written.Close(); err != nil {
			t.Fatalf("close the writing view: %v", err)
		}
	})

	t.Run("window_beside_the_history", func(t *testing.T) {
		root := t.TempDir()
		writer, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: root, WriteRights: true})
		if err != nil {
			t.Fatalf("open the writing view: %v", err)
		}
		written, ok := writer.(*journalKV)
		if !ok {
			t.Fatalf("the journal engine opened as %T, want the key-value view", writer)
		}
		large := bytes.Repeat([]byte("h"), 200<<10)
		for at := 0; at < 20; at++ {
			puts := []Pair{{Key: fmt.Sprintf("0:evt:hist:%03d", at), Value: large}}
			for slot := 0; slot < 4; slot++ {
				puts = append(puts, Pair{Key: fmt.Sprintf("0:idx:hist:%03d:%d", at, slot), Value: []byte("history")})
			}
			if err := writer.Commit(ctx, Batch{Puts: puts}); err != nil {
				t.Fatalf("commit the %dth history record: %v", at, err)
			}
		}
		small := bytes.Repeat([]byte("s"), 40)
		widest, measured := int64(0), 0
		for at := 0; at < 150; at++ {
			puts := make([]Pair, 0, 5)
			for slot := 0; slot < 5; slot++ {
				puts = append(puts, Pair{Key: fmt.Sprintf("0:evt:recent:%03d:%d", at, slot), Value: small})
			}
			if err := writer.Commit(ctx, Batch{Puts: puts}); err != nil {
				t.Fatalf("commit the %dth recent record: %v", at, err)
			}
			if at%10 != 9 {
				continue
			}
			sampled, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: root})
			if err != nil {
				t.Fatalf("open a reading view beside the writer: %v", err)
			}
			view, ok := sampled.(*journalKV)
			if !ok {
				t.Fatalf("the journal engine opened as %T, want the key-value view", sampled)
			}
			if staged := written.log.addr.staged; staged >= journalIndexSyncBytes {
				t.Fatalf("the writer staged %d index byte(s), over the %d one statement budget covers", staged, journalIndexSyncBytes)
			}
			window := view.log.addr.window.end - view.log.addr.valuesSize
			if window > widest {
				widest = window
			}
			measured++
			t.Logf("window after %d recent commit(s) beside a %d byte history: watermark %d, log end %d, %d journal byte(s) past the watermark, %d index byte(s) staged, open read %d byte(s)",
				at+1, written.log.size, view.log.addr.valuesSize, view.log.addr.window.end, window, written.log.addr.staged, view.log.addr.openBytes)
			if err := view.Close(); err != nil {
				t.Fatalf("close the reading view: %v", err)
			}
		}
		if widest <= 0 {
			t.Fatalf("the reading view found no window across %d sample(s) of a writer staging index bytes", measured)
		}
		if err := written.Close(); err != nil {
			t.Fatalf("close the writing view: %v", err)
		}
	})

	t.Run("root_stating_no_index", func(t *testing.T) {
		root := t.TempDir()
		kvDir := filepath.Join(root, kvDirName)
		journalTestWriteLogWithoutAnIndex(t, kvDir, journalPut{Key: "0:evt:00001", Value: []byte("first")})
		before := journalTestRootState(t, root)
		reader, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: root})
		if err != nil {
			t.Fatalf("open the reading view of a root stating no index: %v", err)
		}
		read, ok := reader.(*journalKV)
		if !ok {
			t.Fatalf("the journal engine opened as %T, want the key-value view", reader)
		}
		value, held, err := read.log.read("0:evt:00001")
		if err != nil || !held || string(value) != "first" {
			t.Fatalf("the reading view of a root stating no index read %q (held=%v err=%v), want the committed bytes", value, held, err)
		}
		if end := read.log.addr.window.end; end != read.log.size {
			t.Fatalf("the window of a root stating no index ends at %d, want the log's readable end %d", end, read.log.size)
		}
		assertWholeLogNotice(t, read, journalOrderFileName)
		if err := read.Close(); err != nil {
			t.Fatalf("close the reading view: %v", err)
		}
		journalTestRequireSameFiles(t, before, journalTestRootState(t, root))
		for _, name := range []string{journalOrderFileName, journalSlotFileName} {
			if _, statErr := os.Stat(filepath.Join(kvDir, name)); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("the reading view created %s (err=%v), want a root holding no index file it did not hold before", name, statErr)
			}
		}
	})
}

// assertWholeLogNotice requires the fallback one read view states to name the file its
// index files fail to state a watermark from and the whole log it replayed instead.
func assertWholeLogNotice(t *testing.T, read *journalKV, cause string) {
	t.Helper()
	notice := read.IndexUnstatedNotice()
	if !strings.Contains(notice, "replayed the whole log") || !strings.Contains(notice, cause) {
		t.Fatalf("the reading view states %q, want the fallback named with %s and the whole log it replayed", notice, cause)
	}
	if !strings.Contains(notice, fmt.Sprintf("%d byte(s)", read.log.size)) {
		t.Fatalf("the notice %q does not state the %d byte(s) of the log it read", notice, read.log.size)
	}
}

// TestJournalReadViewAnswersTheLogBesideNoStatedIndex pins that a read view answers a root whose index files state no watermark from its whole log.
//   - a slot table of another version and an order file shorter than its header states both open read only, and the view states that fallback by name
//   - the view answers every committed key, the keys a commit deleted and the namespace of the keys it holds, all from the log it replayed whole
//   - a header stating bytes the log does not hold still refuses the open as corruption, because that is the log losing bytes rather than an index stating none
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalReadViewAnswersTheLogBesideNoStatedIndex(t *testing.T) {
	ctx := context.Background()
	held := map[string]string{
		"0:evt:00001": "first",
		"0:evt:00003": "third",
		"0:idx:alpha": "00001",
	}

	t.Run("table_of_another_version", func(t *testing.T) {
		root := journalTestSeedStatedRoot(t, ctx)
		journalTestStateAnotherVersion(t, filepath.Join(root, kvDirName))
		assertReadViewAnswersTheLog(t, ctx, root, held, journalSlotFileName)
	})

	t.Run("order_file_shorter_than_the_header_states", func(t *testing.T) {
		root := journalTestSeedStatedRoot(t, ctx)
		if err := os.Truncate(filepath.Join(root, kvDirName, journalOrderFileName), 0); err != nil {
			t.Fatalf("discard the order file of %s: %v", root, err)
		}
		assertReadViewAnswersTheLog(t, ctx, root, held, journalOrderFileName)
	})

	t.Run("damaged_frame_beside_an_unstated_index", func(t *testing.T) {
		root := journalTestSeedStatedRoot(t, ctx)
		kvDir := filepath.Join(root, kvDirName)
		journalTestStateAnotherVersion(t, kvDir)
		journalTestFlipLastByte(t, filepath.Join(kvDir, journalFileName))
		if _, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: root}); !errors.Is(err, ErrJournalCorrupt) {
			t.Fatalf("the whole-log replay of a damaged log = %v, want the named corruption: the fallback checks every frame it reads", err)
		}
	})

	t.Run("header_stating_bytes_the_log_lost", func(t *testing.T) {
		root := journalTestSeedStatedRoot(t, ctx)
		kvDir := filepath.Join(root, kvDirName)
		logPath := filepath.Join(kvDir, journalFileName)
		stat, err := os.Stat(logPath)
		if err != nil {
			t.Fatalf("state the log of %s: %v", root, err)
		}
		if err := os.Truncate(logPath, stat.Size()-int64(len("third"))); err != nil {
			t.Fatalf("cut the bytes the header covers: %v", err)
		}
		if _, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: root}); !errors.Is(err, ErrJournalCorrupt) {
			t.Fatalf("the reading view of a log that lost the bytes its header states = %v, want the named corruption", err)
		} else if !strings.Contains(err.Error(), "beside a header stating") {
			t.Fatalf("the refusal = %v, want the header and the bytes it states named", err)
		}
	})
}

// journalTestSeedStatedRoot commits the keys one degraded root holds through a writing view
// of a fresh root and closes it, so the root stands with every commit its writer stated.
func journalTestSeedStatedRoot(t *testing.T, ctx context.Context) string {
	t.Helper()
	root := t.TempDir()
	writer, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the writing view: %v", err)
	}
	written, ok := writer.(*journalKV)
	if !ok {
		t.Fatalf("the journal engine opened as %T, want the key-value view", writer)
	}
	if err := written.Commit(ctx, Batch{Puts: []Pair{
		{Key: "0:evt:00001", Value: []byte("first")},
		{Key: "0:evt:00002", Value: []byte("second")},
		{Key: "0:idx:alpha", Value: []byte("00001")},
	}}); err != nil {
		t.Fatalf("commit three keys: %v", err)
	}
	if err := written.Commit(ctx, Batch{
		Puts:    []Pair{{Key: "0:evt:00003", Value: []byte("third")}},
		Deletes: []string{"0:evt:00002"},
	}); err != nil {
		t.Fatalf("commit a key and a delete: %v", err)
	}
	if err := written.Close(); err != nil {
		t.Fatalf("close the writing view: %v", err)
	}
	return root
}

// journalTestFlipLastByte damages the last byte of path, so the frame ending there states a checksum its payload does not hold.
func journalTestFlipLastByte(t *testing.T, path string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		t.Fatalf("state %s: %v", path, err)
	}
	if stat.Size() == 0 {
		file.Close()
		t.Fatalf("%s holds no byte to damage", path)
	}
	last := make([]byte, 1)
	if _, err := file.ReadAt(last, stat.Size()-1); err != nil {
		file.Close()
		t.Fatalf("read the last byte of %s: %v", path, err)
	}
	last[0] ^= 0xFF
	if _, err := file.WriteAt(last, stat.Size()-1); err != nil {
		file.Close()
		t.Fatalf("damage the last byte of %s: %v", path, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
}

// journalTestStateAnotherVersion states a slot table header of another version over the
// table of dir, which is the header a build that wrote another format left and this build
// states no watermark from.
func journalTestStateAnotherVersion(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, journalSlotFileName)
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open the slot table of %s: %v", dir, err)
	}
	version := make([]byte, 4)
	if _, err := file.ReadAt(version, 4); err != nil {
		file.Close()
		t.Fatalf("read the version of %s: %v", path, err)
	}
	binary.LittleEndian.PutUint32(version, binary.LittleEndian.Uint32(version)+1)
	if _, err := file.WriteAt(version, 4); err != nil {
		file.Close()
		t.Fatalf("state another version in %s: %v", path, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close the slot table of %s: %v", dir, err)
	}
}

// assertReadViewAnswersTheLog requires the reading view of root to answer every held key
// from the log, to answer the key its newest commit deleted as unheld, to name the fallback
// its open took with the file the index files fail to state a watermark from, and to leave
// every file of root as it stood.
func assertReadViewAnswersTheLog(t *testing.T, ctx context.Context, root string, held map[string]string, cause string) {
	t.Helper()
	before := journalTestRootState(t, root)
	reader, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("open the reading view of %s: %v", root, err)
	}
	read, ok := reader.(*journalKV)
	if !ok {
		t.Fatalf("the journal engine opened as %T, want the key-value view", reader)
	}
	for key, want := range held {
		value, named, readErr := read.log.read(key)
		if readErr != nil || !named || string(value) != want {
			t.Fatalf("%q reads %q (held=%v err=%v) through the reading view, want the %q the log carries", key, value, named, readErr, want)
		}
	}
	if _, named, err := read.log.read("0:evt:00002"); err != nil || named {
		t.Fatalf("the deleted key reads held=%v (err=%v) through the reading view, want the tombstone its log carries", named, err)
	}
	keys, err := read.Keys(ctx, "0:evt:")
	if err != nil {
		t.Fatalf("list the keys of %s: %v", root, err)
	}
	if want := []string{"0:evt:00001", "0:evt:00003"}; strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("the reading view lists %v, want %v", keys, want)
	}
	namespaces, err := read.Namespaces(ctx)
	if err != nil {
		t.Fatalf("list the namespaces of %s: %v", root, err)
	}
	if len(namespaces) != 1 || namespaces[0] != "0" {
		t.Fatalf("the reading view lists the namespaces %v, want the one its held keys carry", namespaces)
	}
	if end := read.log.addr.window.end; end != read.log.size {
		t.Fatalf("the window of the reading view ends at %d, want the log's readable end %d", end, read.log.size)
	}
	assertWholeLogNotice(t, read, cause)
	if err := read.Close(); err != nil {
		t.Fatalf("close the reading view: %v", err)
	}
	journalTestRequireSameFiles(t, before, journalTestRootState(t, root))
}

// TestJournalWindowStaysWithinTheLogBudget pins that the log bytes an open replays stay within the log budget whatever a commit states.
//   - a small key commit, a large value commit and a checkpoint shaped commit all state a watermark before the window passes the budget
//   - every sample of a reading view holds a window within the budget, and the samples of the two accumulating loads hold one at all
//   - the bytes the index files leave unstated are the bytes the log counter holds after every commit, so no append path drops them
//   - the budget is the 65536 byte one the assembly's window bound restates, so a budget that moves leaves this anchor red
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalWindowStaysWithinTheLogBudget(t *testing.T) {
	ctx := context.Background()
	if journalLogSyncBytes != 65536 {
		t.Fatalf("the log budget stands at %d byte(s) beside the 65536 the assembly's window bound restates: a budget that moves must move the bound with it", journalLogSyncBytes)
	}
	cases := []struct {
		// name is the commit shape this case drives.
		name string
		// commits is how many commits the shape states.
		commits int
		// keys is how many keys one commit of the shape states.
		keys int
		// value is how many value bytes each of those keys carries.
		value int
		// sample is the interval in commits at which a reading view states the window.
		sample int
		// accumulating reports whether the shape is one whose commits stage index bytes
		// below the budget, so a sample must find a window rather than none.
		accumulating bool
	}{
		{name: "five_key_commits", commits: 200, keys: 5, value: 40, sample: 20, accumulating: true},
		{name: "a_large_value_commit", commits: 12, keys: 1, value: 300 << 10, sample: 1, accumulating: false},
		{name: "checkpoint_shaped_commits", commits: 120, keys: 36, value: 56, sample: 10, accumulating: true},
	}
	for _, shape := range cases {
		t.Run(shape.name, func(t *testing.T) {
			root := t.TempDir()
			writer, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: root, WriteRights: true})
			if err != nil {
				t.Fatalf("open the writing view: %v", err)
			}
			written, ok := writer.(*journalKV)
			if !ok {
				t.Fatalf("the journal engine opened as %T, want the key-value view", writer)
			}
			payload := bytes.Repeat([]byte("v"), shape.value)
			widest := int64(0)
			for at := 0; at < shape.commits; at++ {
				puts := make([]Pair, 0, shape.keys)
				for slot := 0; slot < shape.keys; slot++ {
					puts = append(puts, Pair{Key: fmt.Sprintf("0:evt:%05d:%02d", at, slot), Value: payload})
				}
				if err := writer.Commit(ctx, Batch{Puts: puts}); err != nil {
					t.Fatalf("commit %d: %v", at, err)
				}
				unstated := written.log.size - written.log.addr.statedValuesSize
				if staged := written.log.addr.stagedLog; staged != unstated {
					t.Fatalf("the index files leave %d byte(s) unstated after commit %d and the log counter holds %d, want the same", unstated, at, staged)
				}
				if (at+1)%shape.sample != 0 {
					continue
				}
				view, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: root})
				if err != nil {
					t.Fatalf("open a reading view after commit %d: %v", at, err)
				}
				read, ok := view.(*journalKV)
				if !ok {
					t.Fatalf("the journal engine opened as %T, want the key-value view", view)
				}
				window := read.log.addr.window.end - read.log.addr.valuesSize
				if window > journalLogSyncBytes {
					t.Fatalf("the window holds %d byte(s) after %d commit(s), over the %d the log budget covers", window, at+1, journalLogSyncBytes)
				}
				if window > widest {
					widest = window
				}
				if err := read.Close(); err != nil {
					t.Fatalf("close the reading view: %v", err)
				}
			}
			if shape.accumulating && widest == 0 {
				t.Fatalf("no sample of %d commit(s) held a window, want a load whose commits stage index bytes below the budget sampled inside one", shape.commits)
			}
			if err := written.Close(); err != nil {
				t.Fatalf("close the writing view: %v", err)
			}
		})
	}
}

// TestJournalReadViewLeavesTheTornTailOfTheWriter pins that a view holding no write rights never cuts the tail a writer left half written.
//   - the bytes past the last complete frame stay in the log, at the size the file held before the open
//   - the open serves the complete frames and states the byte the torn tail starts at
//   - a record the file holds whole still reads through the index of the reading view
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func TestJournalReadViewLeavesTheTornTailOfTheWriter(t *testing.T) {
	dir := t.TempDir()
	log := addressableTestOpen(t, dir)
	addressableTestCommit(t, log, []journalPut{{Key: "0:evt:00001", Value: []byte("first")}}, nil)
	complete := log.size
	if err := log.close(); err != nil {
		t.Fatalf("close the writing view: %v", err)
	}
	torn := make([]byte, journalFrameHeaderBytes+20)
	binary.BigEndian.PutUint32(torn[0:4], 64)
	path := filepath.Join(dir, journalFileName)
	appended, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open the log to append a torn frame: %v", err)
	}
	if _, err := appended.Write(torn); err != nil {
		appended.Close()
		t.Fatalf("append a torn frame: %v", err)
	}
	if err := appended.Close(); err != nil {
		t.Fatalf("close the log after appending a torn frame: %v", err)
	}
	before := journalTestRootState(t, dir)
	reader, err := openJournalView(dir, false)
	if err != nil {
		t.Fatalf("open a reading view beside a torn tail: %v", err)
	}
	kept, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the log after the reading view opened: %v", err)
	}
	if int64(len(kept)) != complete+int64(len(torn)) {
		t.Fatalf("the log holds %d byte(s) after the reading view, want the %d the writer left: the reading view cut the frame it found torn",
			len(kept), complete+int64(len(torn)))
	}
	if !bytes.Equal(kept[complete:], torn) {
		t.Fatal("the reading view rewrote the torn tail, want the bytes the writer left")
	}
	if reader.log.size != complete {
		t.Fatalf("the reading view serves %d byte(s), want the %d the complete frames end at", reader.log.size, complete)
	}
	if reader.log.tornTail != complete {
		t.Fatalf("the reading view states the torn tail at %d, want the %d the last complete frame ends at", reader.log.tornTail, complete)
	}
	value, held, err := reader.log.addr.read("0:evt:00001")
	if err != nil || !held || string(value) != "first" {
		t.Fatalf("the reading view read %q (held=%v err=%v), want the record the log holds whole", value, held, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close the reading view: %v", err)
	}
	journalTestRequireSameFiles(t, before, journalTestRootState(t, dir))
}

// TestJournalTornTailStatesZeroBytesThatAreNotWholeLog pins that a tail of zero bytes is a torn tail, which its byte alone cannot state.
//   - a log of whole frames states no torn tail, so the zero byte it stops at is no tail
//   - a log whose only frame is torn states a torn tail at zero, through both faces
//   - a log of whole frames followed by a torn one states the byte the last whole frame ends at, and the writing view cuts the log there
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalTornTailStatesZeroBytesThatAreNotWholeLog(t *testing.T) {
	frames := journalFrameOf(journalTestPayload(t, journalPut{Key: "0:evt:00001", Value: []byte("first")}))
	torn := make([]byte, journalFrameHeaderBytes+20)
	binary.BigEndian.PutUint32(torn[0:4], 64)
	cases := []struct {
		// name is the shape of the log this case writes.
		name string
		// log is the whole body the fixture writes into the log.
		log []byte
		// bytes is the byte both faces state the torn tail at.
		bytes int64
		// torn reports whether the log ends in a torn frame at all.
		torn bool
	}{
		{name: "whole_frames", log: frames, bytes: 0, torn: false},
		{name: "one_torn_frame_alone", log: torn, bytes: 0, torn: true},
		{name: "whole_frames_then_a_torn_one", log: append(append([]byte{}, frames...), torn...), bytes: int64(len(frames)), torn: true},
	}
	for _, shape := range cases {
		t.Run(shape.name, func(t *testing.T) {
			dir := t.TempDir()
			journalTestWriteLogBytes(t, dir, shape.log)
			reader, err := openJournalView(dir, false)
			if err != nil {
				t.Fatalf("open the reading view: %v", err)
			}
			assertTornTail(t, "the reading view", reader, shape.bytes, shape.torn)
			if err := reader.Close(); err != nil {
				t.Fatalf("close the reading view: %v", err)
			}
			writer, err := openJournalView(dir, true)
			if err != nil {
				t.Fatalf("open the writing view: %v", err)
			}
			assertTornTail(t, "the writing view", writer, shape.bytes, shape.torn)
			if shape.torn && writer.log.size != shape.bytes {
				t.Fatalf("the writing view left a log of %d byte(s), want the %d byte(s) its last whole frame ends at", writer.log.size, shape.bytes)
			}
			if err := writer.Close(); err != nil {
				t.Fatalf("close the writing view: %v", err)
			}
		})
	}
}

// assertTornTail requires one view to answer the torn tail a case states, naming the view
// that answered.
func assertTornTail(t *testing.T, face string, view *journalKV, want int64, torn bool) {
	t.Helper()
	bytes, answered := view.TornTailBytesOpen()
	if bytes != want || answered != torn {
		t.Fatalf("%s answers a torn tail at %d byte(s) torn=%v, want %d byte(s) torn=%v", face, bytes, answered, want, torn)
	}
}

// journalTestPayload states the payload of one record carrying puts.
func journalTestPayload(t *testing.T, puts ...journalPut) []byte {
	t.Helper()
	payload, err := json.Marshal(journalRecord{Version: journalFormatVersion, Puts: puts})
	if err != nil {
		t.Fatalf("state the frame of %d put(s): %v", len(puts), err)
	}
	return payload
}

// journalTestWriteLogBytes writes body as the whole log of a directory, so a case states
// the frames and the torn fragment a writer left.
func journalTestWriteLogBytes(t *testing.T, dir string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, journalFileName), body, 0o600); err != nil {
		t.Fatalf("write the log of %s: %v", dir, err)
	}
}

// journalTestReplayLog replays one log whole into the positions and the commit order
// the parity check compares against, so the benchmark of every read shape is the log
// itself and not the index the open keeps.
func journalTestReplayLog(t *testing.T, log *journalLog) (map[string]journalValue, []string) {
	t.Helper()
	index := map[string]journalValue{}
	var order []string
	if _, err := journalReplay(log.file, 0, log.size, func(payload []byte, at int64) error {
		record, err := journalSpansAt(payload, at)
		if err != nil {
			return err
		}
		for _, put := range record.puts {
			index[put.key] = put.value
			order = append(order, put.key)
		}
		for _, key := range record.deletes {
			index[key] = journalValue{deleted: true}
			order = append(order, key)
		}
		return nil
	}); err != nil {
		t.Fatalf("replay the log of %s: %v", log.dir, err)
	}
	return index, order
}

// journalTestIndexRead answers one key the way the test's own replay of the log
// states it.
func journalTestIndexRead(t *testing.T, log *journalLog, index map[string]journalValue, key string) ([]byte, bool) {
	t.Helper()
	value, held := index[key]
	if !held || value.deleted {
		return nil, false
	}
	if value.valueNull {
		return nil, true
	}
	token := make([]byte, value.valueLength)
	if _, err := log.file.ReadAt(token, value.valueOffset); err != nil {
		t.Fatalf("read the value of %q the replay states: %v", key, err)
	}
	var body []byte
	if err := json.Unmarshal(token, &body); err != nil {
		t.Fatalf("decode the value of %q the replay states: %v", key, err)
	}
	return body, true
}

// journalTestIndexKeys answers the held keys carrying prefix from the test's own
// replay of the log.
func journalTestIndexKeys(index map[string]journalValue, prefix string) []string {
	out := make([]string, 0, len(index))
	for key, value := range index {
		if value.deleted || !strings.HasPrefix(key, prefix) {
			continue
		}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// journalTestIndexNamespaces answers the namespaces of the held keys from the test's
// own replay of the log.
func journalTestIndexNamespaces(index map[string]journalValue) []string {
	seen := map[string]struct{}{}
	for key, value := range index {
		if value.deleted {
			continue
		}
		seen[keyNamespaceOf(key)] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for label := range seen {
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}

// journalTestIndexOrder answers the held keys the test's own replay of the log names
// in commit order, from the newest record to the oldest: the walk a key-value
// enumeration makes, stated by the order the frames were appended in.
func journalTestIndexOrder(index map[string]journalValue, order []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(index))
	for at := len(order) - 1; at >= 0; at-- {
		key := order[at]
		if _, walked := seen[key]; walked {
			continue
		}
		seen[key] = struct{}{}
		value, held := index[key]
		if !held || value.deleted {
			continue
		}
		out = append(out, key)
	}
	return out
}

// journalTestRequireAddressParity compares every read shape the key-value face
// answers with the test's own replay of the same log.
func journalTestRequireAddressParity(t *testing.T, log *journalLog) {
	t.Helper()
	index, order := journalTestReplayLog(t, log)
	keys := make([]string, 0, len(index))
	for key := range index {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		wantValue, wantHeld := journalTestIndexRead(t, log, index, key)
		gotValue, gotHeld, err := log.read(key)
		if err != nil {
			t.Fatalf("read %q from the address: %v", key, err)
		}
		if gotHeld != wantHeld || !bytes.Equal(gotValue, wantValue) {
			t.Fatalf("%q reads (%q, held=%t) from the address and (%q, held=%t) from the replay",
				key, gotValue, gotHeld, wantValue, wantHeld)
		}
	}
	for _, prefix := range []string{"", "0:idx:", "0:evt:"} {
		got, err := log.keys(prefix)
		if err != nil {
			t.Fatalf("list the keys carrying %q: %v", prefix, err)
		}
		want := journalTestIndexKeys(index, prefix)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("the keys carrying %q are %v from the address and %v from the replay", prefix, got, want)
		}
	}
	gotNamespaces, err := log.namespaces()
	if err != nil {
		t.Fatalf("list the namespaces: %v", err)
	}
	if want := journalTestIndexNamespaces(index); strings.Join(gotNamespaces, ",") != strings.Join(want, ",") {
		t.Fatalf("the namespaces are %v from the address and %v from the replay", gotNamespaces, want)
	}
	var walked []string
	if err := log.eachKeyNewestFirstValue(func(key string, _ []byte) (bool, error) {
		walked = append(walked, key)
		return true, nil
	}); err != nil {
		t.Fatalf("walk the commit order: %v", err)
	}
	if want := journalTestIndexOrder(index, order); strings.Join(walked, ",") != strings.Join(want, ",") {
		t.Fatalf("the commit order names %v from the address and %v from the replay", walked, want)
	}
}

// TestJournalReadPathsAnswerFromTheAddress pins that the four read shapes answer the facts the replayed log states.
//   - a rewritten key answers its newest value, including a key one commit writes twice
//   - a tombstoned key is not held, and a commit after the tombstone holds it again
//   - a value the commit wrote as JSON null is held and empty
//   - a key committed past the stated watermark is held at a span the watermark rejects
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalReadPathsAnswerFromTheAddress(t *testing.T) {
	t.Run("settled_root", func(t *testing.T) {
		dir := t.TempDir()
		log := addressableTestOpen(t, dir)
		addressableTestCommit(t, log, []journalPut{
			{Key: "0:evt:alpha", Value: []byte("a1")},
			{Key: "0:idx:alpha", Value: []byte("00001")},
			{Key: "0:evt:null", Value: nil},
			{Key: "1:evt:gone", Value: []byte("g1")},
			{Key: "2:evt:doomed", Value: []byte("doomed")},
			{Key: "global", Value: []byte("unprefixed")},
		}, nil)
		addressableTestCommit(t, log, []journalPut{
			{Key: "0:evt:alpha", Value: []byte("a2")},
			{Key: "0:evt:twice", Value: []byte("first")},
			{Key: "0:evt:twice", Value: []byte("second")},
		}, []string{"1:evt:gone", "2:evt:doomed"})
		addressableTestCommit(t, log, []journalPut{{Key: "1:evt:gone", Value: []byte("g2")}}, nil)
		if err := log.close(); err != nil {
			t.Fatalf("close the writing view: %v", err)
		}
		opened := addressableTestOpen(t, dir)
		defer opened.close()
		if opened.addr.window != nil {
			t.Fatalf("the settled root opened with a window: its writer stated every commit before it closed")
		}
		journalTestRequireAddressParity(t, opened)
		if got := opened.addr.valuesSize; got != opened.size {
			t.Fatalf("the settled root states a watermark at %d beside a %d byte log", got, opened.size)
		}
		if value, held, err := opened.read("0:evt:null"); err != nil || !held || len(value) != 0 {
			t.Fatalf("the null value reads (%q, held=%t, err=%v), want a held key of no bytes", value, held, err)
		}
		if _, held, err := opened.read("1:evt:gone"); err != nil || !held {
			t.Fatalf("the key a later commit wrote again reads held=%t (err=%v), want it held", held, err)
		}
		if _, held, err := opened.read("2:evt:doomed"); err != nil || held {
			t.Fatalf("the tombstoned key reads held=%t (err=%v), want the tombstone its only commit after the write states", held, err)
		}
	})

	t.Run("settled_root_read_view", func(t *testing.T) {
		dir := t.TempDir()
		log := addressableTestOpen(t, dir)
		addressableTestCommit(t, log, []journalPut{
			{Key: "0:evt:alpha", Value: []byte("a1")},
			{Key: "0:idx:alpha", Value: []byte("00001")},
			{Key: "0:evt:null", Value: nil},
			{Key: "1:evt:doomed", Value: []byte("doomed")},
		}, nil)
		addressableTestCommit(t, log, []journalPut{{Key: "0:evt:beta", Value: []byte("b1")}}, []string{"1:evt:doomed"})
		if err := log.close(); err != nil {
			t.Fatalf("close the writing view: %v", err)
		}
		reader, err := openJournalView(dir, false)
		if err != nil {
			t.Fatalf("open the reading view of the settled root: %v", err)
		}
		defer reader.log.close()
		if reader.log.addr.window.end != reader.log.addr.valuesSize {
			t.Fatalf("the settled root left a window of %d byte(s), want none",
				reader.log.addr.window.end-reader.log.addr.valuesSize)
		}
		if reader.log.addr.used == 0 {
			t.Fatal("the reading view of the settled root addresses no key, want the table it reads")
		}
		journalTestRequireAddressParity(t, reader.log)
		if _, held, err := reader.log.addr.read("1:evt:doomed"); err != nil || held {
			t.Fatalf("the tombstoned key reads held=%t (err=%v) from the table, want absence", held, err)
		}
	})

	t.Run("watermark_behind_the_log", func(t *testing.T) {
		dir := t.TempDir()
		writer := addressableTestOpen(t, dir)
		large := bytes.Repeat([]byte("h"), 300<<10)
		addressableTestCommit(t, writer, []journalPut{
			{Key: "0:evt:history", Value: large},
			{Key: "0:idx:history", Value: []byte("history")},
		}, nil)
		addressableTestCommit(t, writer, []journalPut{
			{Key: "0:evt:alpha", Value: []byte("a1")},
			{Key: "0:evt:null", Value: nil},
			{Key: "1:evt:gone", Value: []byte("g1")},
		}, nil)
		addressableTestCommit(t, writer, []journalPut{{Key: "0:evt:alpha", Value: []byte("a2")}}, []string{"1:evt:gone"})
		reader, err := openJournalView(dir, false)
		if err != nil {
			t.Fatalf("open the reading view beside the writer: %v", err)
		}
		defer reader.log.close()
		window := reader.log.addr.window
		stated := reader.log.addr.valuesSize
		if window.end <= stated {
			t.Fatalf("the reading view found no window: the log reads to %d and the stated watermark is %d", window.end, stated)
		}
		journalTestRequireAddressParity(t, reader.log)
		for _, key := range []string{"0:evt:alpha", "0:evt:null"} {
			folded, named := window.values[key]
			span := spanOf(folded)
			if !named {
				t.Fatalf("%q sits in no window, want the commit the stated watermark %d leaves unstated", key, stated)
			}
			if !span.Null && span.Offset < stated {
				t.Fatalf("%q sits at %d in the window, want a span the stated watermark %d leaves unstated", key, span.Offset, stated)
			}
			if !reader.log.addr.stated(span) {
				t.Fatalf("%q states a span past the watermark %d the reading view rejects, want the window's own end %d", key, stated, window.end)
			}
			if _, held, err := reader.log.addr.read(key); err != nil || !held {
				t.Fatalf("%q reads held=%t (err=%v) through the window, want the commit the writer has not stated", key, held, err)
			}
		}
		if _, folded := window.values["0:evt:history"]; folded {
			t.Fatalf("the 300 KiB commit sits in the window, want the watermark at %d the log budget made it state", stated)
		}
		if value, held, err := reader.log.addr.read("0:evt:history"); err != nil || !held || len(value) != len(large) {
			t.Fatalf("the 300 KiB commit reads %d byte(s) (held=%t, err=%v), want the whole value the table states", len(value), held, err)
		}
		if _, held, err := reader.log.addr.read("1:evt:gone"); err != nil || held {
			t.Fatalf("the tombstoned key reads held=%t (err=%v) through the window, want the tombstone it states", held, err)
		}
		if err := writer.close(); err != nil {
			t.Fatalf("close the writing view: %v", err)
		}
	})
}

// TestAddressReadCostByKeyCount measures what an enumeration and a point read pay at growing key counts.
//   - a key listing reads the slot table alone, at the capacity times the stride
//   - a root whose few keys are rewritten many times keeps the starting capacity
//   - a point read pays a probe chain beside its value, a read view the header and the window
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestAddressReadCostByKeyCount(t *testing.T) {
	cases := []struct {
		name     string
		keys     int
		rewrites int
	}{
		{name: "512_keys", keys: 512},
		{name: "1024_keys", keys: 1024},
		{name: "6400_keys", keys: 6400},
		{name: "100_keys_2000_rewrites", keys: 100, rewrites: 2000},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			log := addressableTestOpen(t, dir)
			body := bytes.Repeat([]byte("v"), addressableTestValueBytes)
			for at := 0; at < testCase.keys; at += 256 {
				puts := make([]journalPut, 0, 256)
				for slot := at; slot < testCase.keys && slot < at+256; slot++ {
					puts = append(puts, journalPut{Key: fmt.Sprintf("0:evt:%06d", slot), Value: body})
				}
				addressableTestCommit(t, log, puts, nil)
			}
			for round := 0; round < testCase.rewrites/20; round++ {
				puts := make([]journalPut, 0, 20)
				for slot := 0; slot < 20; slot++ {
					puts = append(puts, journalPut{Key: fmt.Sprintf("0:evt:%06d", slot), Value: body})
				}
				addressableTestCommit(t, log, puts, nil)
			}
			if err := log.close(); err != nil {
				t.Fatalf("close the writing view: %v", err)
			}
			opened := addressableTestOpen(t, dir)
			defer opened.close()
			addr := opened.addr
			keys, err := opened.keys("")
			if err != nil {
				t.Fatalf("list the held keys: %v", err)
			}
			if int64(len(keys)) != int64(testCase.keys) {
				t.Fatalf("the root holds %d key(s), want the %d it committed", len(keys), testCase.keys)
			}
			if addr.used*100 > addr.capacity*journalSlotLoadPercent {
				t.Fatalf("the table states %d used slot(s) of %d, over the %d percent load bound",
					addr.used, addr.capacity, journalSlotLoadPercent)
			}
			scan := time.Duration(0)
			for round := 0; round < 3; round++ {
				started := time.Now()
				if _, err := opened.keys(""); err != nil {
					t.Fatalf("list the held keys: %v", err)
				}
				scan += time.Since(started)
			}
			scan /= 3
			started := time.Now()
			for _, key := range keys {
				if _, _, err := opened.read(key); err != nil {
					t.Fatalf("read %q: %v", key, err)
				}
			}
			probed := time.Since(started)
			spans := make(map[string]journalAddressSpan, len(keys))
			err = addr.eachRecord(func(state uint8, key string, span journalAddressSpan) error {
				if state == journalSlotHeld && addr.stated(span) {
					spans[key] = span
				}
				return nil
			})
			if err != nil {
				t.Fatalf("walk the slot table: %v", err)
			}
			if len(spans) != len(keys) {
				t.Fatalf("the table walk holds %d key(s) beside the %d the listing answers", len(spans), len(keys))
			}
			started = time.Now()
			for _, key := range keys {
				if _, _, err := addr.value(spans[key]); err != nil {
					t.Fatalf("read the value of %q: %v", key, err)
				}
			}
			values := time.Since(started)
			t.Logf("%s: capacity=%d stride=%d used=%d, listing reads %d byte(s) per call in %v, %d point read(s) (probe chain and value) in %v, %d value read(s) alone in %v",
				testCase.name, addr.capacity, addr.stride, addr.used, addr.capacity*addr.stride, scan, len(keys), probed, len(keys), values)
			if testCase.rewrites > 0 && addr.capacity != journalSlotCapacity {
				t.Fatalf("the table of %d rewritten key(s) grew to %d slot(s), want the %d it started at: the listing reads no history",
					testCase.keys, addr.capacity, int64(journalSlotCapacity))
			}
		})
	}

	t.Run("read_view_pays_the_header_and_the_window", func(t *testing.T) {
		root := t.TempDir()
		writer, err := OpenAtomicStore(context.Background(), StorageConfig{Type: StorageJournal, Root: root, WriteRights: true})
		if err != nil {
			t.Fatalf("open the writing view: %v", err)
		}
		written, ok := writer.(*journalKV)
		if !ok {
			t.Fatalf("the journal engine opened as %T, want the key-value view", writer)
		}
		body := bytes.Repeat([]byte("v"), 4096)
		for round := 0; round < 40; round++ {
			puts := make([]Pair, 0, 16)
			for slot := 0; slot < 16; slot++ {
				puts = append(puts, Pair{Key: fmt.Sprintf("0:evt:%06d", round*16+slot), Value: body})
			}
			if err := written.Commit(context.Background(), Batch{Puts: puts}); err != nil {
				t.Fatalf("commit round %d: %v", round, err)
			}
		}
		if err := written.Close(); err != nil {
			t.Fatalf("close the writing view: %v", err)
		}
		reader, err := OpenAtomicStore(context.Background(), StorageConfig{Type: StorageJournal, Root: root})
		if err != nil {
			t.Fatalf("open the reading view of the settled root: %v", err)
		}
		read, ok := reader.(*journalKV)
		if !ok {
			t.Fatalf("the journal engine opened as %T, want the key-value view", reader)
		}
		defer read.Close()
		stat, err := os.Stat(filepath.Join(root, kvDirName, journalFileName))
		if err != nil {
			t.Fatalf("stat the log of the root: %v", err)
		}
		t.Logf("read view of a %d byte log: open read %d byte(s), window %d byte(s)",
			stat.Size(), read.log.addr.openBytes, read.log.addr.window.end-read.log.addr.valuesSize)
		if window := read.log.addr.window.end - read.log.addr.valuesSize; window != 0 {
			t.Fatalf("the settled root left a window of %d byte(s), want none", window)
		}
		if got := read.JournalBytesOpen(); got != 0 {
			t.Fatalf("the read view reports %d journal byte(s) open beside a %d byte log, want none beside a watermark at the log's end", got, stat.Size())
		}
	})
}

// journalTestKey names one key of the fixture journal in the form the page keys
// carry, so the fixture reads like the store it stands for.
func journalTestKey(at int) string {
	return "0:evt:" + string(rune('a'+at))
}

// TestOpenReadsNoByteBesideAStatedWatermarkAtTheLogEnd pins the open beside a watermark the index files state at the log's end.
//   - a key past the last commit answers from the log alone, and the open replays no byte beside a watermark that reaches the log's end
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestOpenReadsNoByteBesideAStatedWatermarkAtTheLogEnd(t *testing.T) {
	dir := t.TempDir()
	kv, err := openJournalKV(dir)
	if err != nil {
		t.Fatalf("open the journal: %v", err)
	}
	ctx := context.Background()
	for at := 0; at < 4; at++ {
		key := journalTestKey(at)
		if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: key, Value: []byte("body-" + key)}}}); err != nil {
			t.Fatalf("commit %s: %v", key, err)
		}
	}
	if err := kv.log.close(); err != nil {
		t.Fatalf("close the journal: %v", err)
	}
	stat, err := os.Stat(filepath.Join(dir, journalFileName))
	if err != nil {
		t.Fatalf("stat the journal: %v", err)
	}
	opened, err := openJournalKV(dir)
	if err != nil {
		t.Fatalf("reopen the journal: %v", err)
	}
	defer opened.log.close()
	if got := opened.JournalBytesOpen(); got != 0 {
		t.Fatalf("the open beside a stated watermark replayed %d byte(s) of a %d byte log, want none beside a watermark at the log's end", got, stat.Size())
	}
	key := journalTestKey(3)
	pairs, err := opened.Read(ctx, Fetch{Key: key})
	if err != nil || len(pairs) != 1 || string(pairs[0].Value) != "body-"+key {
		t.Fatalf("%s reads %v (err=%v), want the bytes the last commit wrote", key, pairs, err)
	}
}

// TestJournalOpenRefusesCorruptionBehindAStatedWatermark pins that a damaged frame is refused even when the index files state a watermark at the log's end.
//   - a settled root states a watermark covering every committed frame, so the index replay reads no window
//   - the open verifies the checksum of the whole log, so a frame damaged behind that watermark refuses both faces by name
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestJournalOpenRefusesCorruptionBehindAStatedWatermark(t *testing.T) {
	dir := t.TempDir()
	log := addressableTestOpen(t, dir)
	addressableTestCommit(t, log, []journalPut{{Key: "0:evt:00001", Value: []byte("first")}}, nil)
	addressableTestCommit(t, log, []journalPut{{Key: "0:evt:00002", Value: []byte("second")}}, nil)
	if err := log.close(); err != nil {
		t.Fatalf("close the writing view: %v", err)
	}
	watermark, stated := journalStatedWatermark(dir)
	stat, err := os.Stat(filepath.Join(dir, journalFileName))
	if err != nil {
		t.Fatalf("stat the log: %v", err)
	}
	if !stated || watermark != stat.Size() {
		t.Fatalf("the settled root states watermark=%d (stated=%v) beside a %d byte log, want a watermark at the log's end", watermark, stated, stat.Size())
	}
	journalTestFlipLastByte(t, filepath.Join(dir, journalFileName))
	if _, err := openJournalLog(dir); !errors.Is(err, ErrJournalCorrupt) {
		t.Fatalf("the writing face over a frame damaged behind the watermark = %v, want %v", err, ErrJournalCorrupt)
	}
	if _, err := openJournalView(dir, false); !errors.Is(err, ErrJournalCorrupt) {
		t.Fatalf("the reading face over a frame damaged behind the watermark = %v, want %v", err, ErrJournalCorrupt)
	}
}

// TestOpenIntegrityScanIsTheWholeLogAndGrowsWithIt pins the integrity pass measure apart from the index replay window.
//   - a settled root states a watermark at the log's end, so the index replay reads no byte while the integrity pass verifies every frame of the log
//   - the integrity measure answers the log's own length at both depths, so a probe that answered the replay window instead fails the length assertion
//   - ten times the commits answer ten times the verified bytes within the linear band: the intrinsic O(history) of the open, declared and measured
//   - the reading face answers the same span as the writing face, because the open verifies the log whatever rights the view holds
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestOpenIntegrityScanIsTheWholeLogAndGrowsWithIt(t *testing.T) {
	measure := func(commits int) (integrity, window, logBytes int64) {
		t.Helper()
		dir := t.TempDir()
		kv, err := openJournalKV(dir)
		if err != nil {
			t.Fatalf("open the writing view: %v", err)
		}
		body := bytes.Repeat([]byte("v"), addressableTestValueBytes)
		for at := 0; at < commits; at++ {
			if err := kv.Commit(context.Background(), Batch{Puts: []Pair{{Key: fmt.Sprintf("0:evt:%06d", at), Value: body}}}); err != nil {
				t.Fatalf("commit %d: %v", at, err)
			}
		}
		if err := kv.Close(); err != nil {
			t.Fatalf("close the writing view: %v", err)
		}
		stat, err := os.Stat(filepath.Join(dir, journalFileName))
		if err != nil {
			t.Fatalf("stat the log: %v", err)
		}
		opened, err := openJournalKV(dir)
		if err != nil {
			t.Fatalf("reopen the journal: %v", err)
		}
		defer opened.Close()
		return opened.IntegrityBytesScanned(), opened.JournalBytesOpen(), stat.Size()
	}
	shallowIntegrity, shallowWindow, shallowLog := measure(200)
	deepIntegrity, deepWindow, deepLog := measure(2000)
	if shallowIntegrity != shallowLog || deepIntegrity != deepLog {
		t.Fatalf("the integrity pass verified (%d, %d) byte(s) beside logs of (%d, %d), want the whole log at both depths: the pass reads the history behind the watermark, and the window measure is a separate span",
			shallowIntegrity, deepIntegrity, shallowLog, deepLog)
	}
	if shallowWindow != 0 || deepWindow != 0 {
		t.Fatalf("the index replay read (%d, %d) byte(s) beside watermarks at the log's end, want none: the two measures state different reads", shallowWindow, deepWindow)
	}
	if deepIntegrity < 9*shallowIntegrity || deepIntegrity > 11*shallowIntegrity {
		t.Fatalf("the verified bytes grew from %d at 200 commits to %d at 2000, want ten times within the linear band: the integrity pass is the intrinsic O(history) of the open", shallowIntegrity, deepIntegrity)
	}
	reader, err := openJournalView(t.TempDir(), false)
	if err != nil {
		t.Fatalf("open the reading face of an uncreated root: %v", err)
	}
	if got := reader.IntegrityBytesScanned(); got != 0 {
		t.Fatalf("the reading face of a log no writer created verified %d byte(s), want none", got)
	}
	reader.Close()
	t.Logf("integrity pass verified %d byte(s) at 200 commits and %d at 2000, window %d and %d", shallowIntegrity, deepIntegrity, shallowWindow, deepWindow)
}

// TestSlotScanBytesFollowsTheTableNotTheAnswer pins the enumeration measure of the slot table.
//   - a point read adds nothing: the probe locates one record and is not an enumeration
//   - one enumeration adds the capacity records of stride bytes the table holds, even beside a prefix no key carries: the scan follows the table and never the hits
//   - two tables of different capacity answering the same zero hits report their own capacities: the cost shape a boot pays when it enumerates instead of addressing
//   - a second enumeration adds the table again, so the count states what the view read since its open
//   - the baseline is read after the build, because a commit that doubles the table states it again through one enumeration of the table it replaces
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestSlotScanBytesFollowsTheTableNotTheAnswer(t *testing.T) {
	build := func(keys int) *journalKV {
		t.Helper()
		kv, err := openJournalKV(t.TempDir())
		if err != nil {
			t.Fatalf("open the writing view: %v", err)
		}
		body := bytes.Repeat([]byte("v"), addressableTestValueBytes)
		for at := 0; at < keys; at += 256 {
			puts := make([]Pair, 0, 256)
			for slot := at; slot < keys && slot < at+256; slot++ {
				puts = append(puts, Pair{Key: fmt.Sprintf("0:evt:%06d", slot), Value: body})
			}
			if err := kv.Commit(context.Background(), Batch{Puts: puts}); err != nil {
				t.Fatalf("commit the batch at %d: %v", at, err)
			}
		}
		return kv
	}
	small, large := build(100), build(3000)
	defer small.Close()
	defer large.Close()
	for _, kv := range []*journalKV{small, large} {
		base := kv.SlotTableBytesScanned()
		if _, err := kv.Read(context.Background(), Fetch{Key: "0:evt:000000"}); err != nil {
			t.Fatalf("point read: %v", err)
		}
		if got := kv.SlotTableBytesScanned() - base; got != 0 {
			t.Fatalf("the point read added %d slot byte(s), want none: a probe is not an enumeration", got)
		}
		if _, err := kv.Read(context.Background(), Fetch{Prefix: "0:nomatch:"}); err != nil {
			t.Fatalf("zero-hit enumeration: %v", err)
		}
		want := kv.log.addr.capacity * kv.log.addr.stride
		if got := kv.SlotTableBytesScanned() - base; got != want {
			t.Fatalf("the zero-hit enumeration read %d slot byte(s), want the %d the table holds: the scan pays the capacity whatever the prefix answers", got, want)
		}
		if _, err := kv.Read(context.Background(), Fetch{Prefix: "0:nomatch:"}); err != nil {
			t.Fatalf("second enumeration: %v", err)
		}
		if got := kv.SlotTableBytesScanned() - base; got != 2*want {
			t.Fatalf("two enumerations read %d slot byte(s), want %d: the count states what the view read since its open", got, 2*want)
		}
	}
	if small.log.addr.capacity == large.log.addr.capacity {
		t.Fatalf("the two tables hold the same capacity %d, want different shapes so the count is read beside two capacities", small.log.addr.capacity)
	}
	t.Logf("zero-hit enumerations read %d byte(s) of the %d-slot table and %d of the %d-slot table",
		small.log.addr.capacity*small.log.addr.stride, small.log.addr.capacity,
		large.log.addr.capacity*large.log.addr.stride, large.log.addr.capacity)
}

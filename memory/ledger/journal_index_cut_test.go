// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
package ledger

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// cutFixture writes one journal through a retirement pass, so the directory holds a
// log, a cold root and the index cut the rewrite placed beside them: the shape every
// anchor below opens.
func cutFixture(t *testing.T) (dir string, opened *journalKV, settled []string) {
	t.Helper()
	dir = t.TempDir()
	cold := t.TempDir()
	kv, err := openJournalKV(dir)
	if err != nil {
		t.Fatalf("open the fixture journal: %v", err)
	}
	ctx := context.Background()
	for at := 0; at < 12; at++ {
		key := journalTestKey(at)
		if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: key, Value: []byte("body-" + key)}}}); err != nil {
			t.Fatalf("commit %s: %v", key, err)
		}
	}
	settle := make([]string, 0, 11)
	for at := 0; at < 11; at++ {
		settle = append(settle, journalTestKey(at))
	}
	if _, err := kv.retireSettled(ctx, cold, settle, nil); err != nil {
		t.Fatalf("relocate the settled keys: %v", err)
	}
	settled = []string{journalTestKey(0), journalTestKey(6)}
	return dir, kv, settled
}

// journalTestKey names one key of the fixture journal in the form the page keys
// carry, so the fixture reads like the store it stands for.
func journalTestKey(at int) string {
	return "0:evt:" + string(rune('a'+at))
}

// TestOpenReplaysOnlyPastTheIndexCut pins the read one open pays beside an index
// cut: the bytes from the cut to the end of the log, never the frames the cut
// already places. The negative of the fixture is stated beside it — a directory
// holding no cut reads the whole log — so a green anchor is a claim about the cut
// and not about a log that held nothing to read.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestOpenReplaysOnlyPastTheIndexCut(t *testing.T) {
	dir, kv, _ := cutFixture(t)
	if err := kv.log.close(); err != nil {
		t.Fatalf("close the fixture journal: %v", err)
	}
	stat, err := os.Stat(filepath.Join(dir, journalFileName))
	if err != nil {
		t.Fatalf("stat the fixture journal: %v", err)
	}
	cut, held := readJournalIndexCut(dir)
	if !held {
		t.Fatal("the retirement pass placed no index cut, want the cut the rewrite states")
	}
	if cut.Offset <= 0 || cut.Size > stat.Size() {
		t.Fatalf("the cut states offset=%d size=%d beside a %d byte log, want an offset inside the log", cut.Offset, cut.Size, stat.Size())
	}
	reopened, err := openJournalKV(dir)
	if err != nil {
		t.Fatalf("reopen the fixture journal: %v", err)
	}
	defer reopened.log.close()
	if got, want := reopened.JournalBytesOpen(), stat.Size()-cut.Offset; got != want {
		t.Fatalf("the open replayed %d byte(s) beside the %d the cut leaves, want the tail past the cut", got, want)
	}
	if got, want := reopened.JournalBytesOpen(), stat.Size(); got == want {
		t.Fatalf("the open replayed the whole %d byte log, want the tail past the cut", got)
	}
	// The positive of the placement: the key the pass kept answers its own bytes from
	// the span the cut states, so a bounded open is a placement and never a lost fact.
	// Every other key of the fixture left the hot log for the cold root, which is the
	// retention pass's own contract and not this anchor's.
	for _, key := range []string{journalTestKey(11)} {
		pairs, err := reopened.Read(context.Background(), Fetch{Key: key})
		if err != nil || len(pairs) != 1 {
			t.Fatalf("read %s through the cut = (%d pair(s), %v), want the held bytes", key, len(pairs), err)
		}
		if want := "body-" + key; string(pairs[0].Value) != want {
			t.Fatalf("read %s = %q, want %q", key, pairs[0].Value, want)
		}
	}
	// The negative: the same log with no cut beside it is read whole.
	if err := os.Remove(filepath.Join(dir, journalIndexCutFileName)); err != nil {
		t.Fatalf("remove the index cut: %v", err)
	}
	full, err := openJournalKV(dir)
	if err != nil {
		t.Fatalf("reopen the log with no cut: %v", err)
	}
	defer full.log.close()
	if got := full.JournalBytesOpen(); got != stat.Size() {
		t.Fatalf("the open beside no cut replayed %d byte(s) of a %d byte log, want the whole log", got, stat.Size())
	}
}

// TestOpenRefusesToTrustAnIndexCutItCannotPlace pins the two cuts a build refuses to
// read: one stating a tail past the log it sits beside, and one that does not decode.
// Both are answered by reading the log whole, which is the fallback that keeps the
// facts reachable, and the open states the fallback by name so it is never mistaken
// for a log whose cut was placed.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestOpenRefusesToTrustAnIndexCutItCannotPlace(t *testing.T) {
	dir, kv, _ := cutFixture(t)
	if err := kv.log.close(); err != nil {
		t.Fatalf("close the fixture journal: %v", err)
	}
	stat, err := os.Stat(filepath.Join(dir, journalFileName))
	if err != nil {
		t.Fatalf("stat the fixture journal: %v", err)
	}
	path := filepath.Join(dir, journalIndexCutFileName)
	cases := []struct {
		name string
		body []byte
	}{
		{name: "a tail past the log", body: []byte(`{"version":1,"offset":` + strconv.FormatInt(stat.Size()+1, 10) + `,"size":` + strconv.FormatInt(stat.Size()+1, 10) + `,"index":{}}`)},
		{name: "an unreadable cut", body: []byte(`{"version":1,`)},
		{name: "another format", body: []byte(`{"version":99,"offset":0,"size":0}`)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := os.WriteFile(path, testCase.body, 0o600); err != nil {
				t.Fatalf("write the cut: %v", err)
			}
			opened, err := openJournalKV(dir)
			if err != nil {
				t.Fatalf("open the log beside %s: %v", testCase.name, err)
			}
			defer opened.log.close()
			if got := opened.JournalBytesOpen(); got != stat.Size() {
				t.Fatalf("the open beside %s replayed %d byte(s) of a %d byte log, want the whole log", testCase.name, got, stat.Size())
			}
		})
	}
}

// TestOpenReadsTheLogWholeBesideNoIndexCut pins the store that never relocated: a
// directory holding no cut answers the whole log, and a key past the last commit is
// answered by the log alone. It is the negative the bounded open is measured
// against, so a flat open beside a cut is never a flat open beside nothing.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestOpenReadsTheLogWholeBesideNoIndexCut(t *testing.T) {
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
	if got := opened.JournalBytesOpen(); got != stat.Size() {
		t.Fatalf("the open beside no cut replayed %d byte(s) of a %d byte log, want the whole log", got, stat.Size())
	}
	if got := opened.TailKeys(); got != nil {
		t.Fatalf("the open beside no cut states the tail %v, want nil: a whole read covers the log", got)
	}
}

// TestRewriteKeepsTheHandleOnTheLogItLeft pins what a rewrite answers with when it
// cannot place the index cut it states: the handle is on the log the rename left, so
// the pass answers a cut failure beside a log that reads whole, and never beside a
// handle on the inode the rename unlinked.
//
//   - positive: the rewritten log holds the key the pass kept, and no cut sits beside it
//   - negative: the commit a later call files after the failed pass is read by a view that opens the same directory, so a handle left on the unlinked inode loses it
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
func TestRewriteKeepsTheHandleOnTheLogItLeft(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cold := t.TempDir()
	kv, err := openJournalKV(dir)
	if err != nil {
		t.Fatalf("open the fixture journal: %v", err)
	}
	defer kv.log.close()
	for at := 0; at < 12; at++ {
		key := journalTestKey(at)
		if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: key, Value: []byte("body-" + key)}}}); err != nil {
			t.Fatalf("commit %s: %v", key, err)
		}
	}
	settle := make([]string, 0, 11)
	for at := 0; at < 11; at++ {
		settle = append(settle, journalTestKey(at))
	}
	// The cut is placed through a temporary file inside the log's directory, so a
	// directory at that path is the one obstruction the pass meets after the rename.
	blocked := filepath.Join(dir, journalIndexCutFileName+".tmp")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatalf("obstruct the index cut: %v", err)
	}
	if _, err := kv.retireSettled(ctx, cold, settle, nil); err == nil {
		t.Fatal("the pass placed its index cut beside a directory that admits no cut file, want the named cut failure")
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatalf("clear the obstruction: %v", err)
	}
	if _, held := readJournalIndexCut(dir); held {
		t.Fatal("the failed pass left an index cut beside the log, want none: a cut never sits beside a log it does not describe")
	}
	// The positive: the log the rename left holds the key the pass kept.
	kept := journalTestKey(11)
	if got, held, err := kv.log.read(kept); err != nil || !held || string(got) != "body-"+kept {
		t.Fatalf("the kept key after the failed cut = (%q, %v, %v), want the bytes the rewrite filed", got, held, err)
	}
	added := journalTestKey(12)
	if err := kv.Commit(ctx, Batch{Puts: []Pair{{Key: added, Value: []byte("body-" + added)}}}); err != nil {
		t.Fatalf("commit %s after the failed cut: %v", added, err)
	}
	reopened, err := openJournalKV(dir)
	if err != nil {
		t.Fatalf("reopen the rewritten journal: %v", err)
	}
	defer reopened.log.close()
	pairs, err := reopened.Read(ctx, Fetch{Key: added})
	if err != nil {
		t.Fatalf("read %s from the reopened log: %v", added, err)
	}
	if len(pairs) != 1 || string(pairs[0].Value) != "body-"+added {
		t.Fatalf("a view that opened the directory reads %s = %+v, want the commit filed after the failed cut: a handle on the unlinked inode writes where no open reads",
			added, pairs)
	}
}

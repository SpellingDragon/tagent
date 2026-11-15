// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
//
// The two anchors below pin the recoverability a retirement pass owes a reader
// that never reads the cold journal: a family the boot scans by key — the task
// records a durable commit rebuilds its chains from — stays in the hot journal for
// every pass that ran, and a key a writer filed after the pass read its snapshot is
// not settled by that pass at all.
package ledger

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// lateKeyEngine is one journal whose snapshot runs a moment late: the key a writer
// files after the pass read the keys it keeps is committed while that snapshot is
// being answered, so the pass that follows reads a log holding one more key than
// its own decision named.
type lateKeyEngine struct {
	*journalKV
	late string
	once bool
}

// Keys answers the snapshot the pass decides on and files the late key once, after
// the snapshot the caller received was read: the commit lands in the log and in the
// index, and in no key set the pass holds.
func (e *lateKeyEngine) Keys(ctx context.Context, prefix string) ([]string, error) {
	keys, err := e.journalKV.Keys(ctx, prefix)
	if err != nil {
		return nil, err
	}
	if !e.once && prefix == "" {
		e.once = true
		if err := e.journalKV.Commit(ctx, Batch{Puts: []Pair{{Key: e.late, Value: []byte(`{"late":1}`)}}}); err != nil {
			return nil, err
		}
	}
	return keys, nil
}

// lateRetirementFixture opens one journal tier over the engine above, writes one
// settled event beside the late key and answers the tier with its root.
func lateRetirementFixture(t *testing.T) (context.Context, *Tier, *lateKeyEngine, string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	opened, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("open the fixture journal: %v", err)
	}
	engine := &lateKeyEngine{journalKV: opened.(*journalKV), late: "tagent/task/v1/spec:task-late"}
	tier, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root, RetireFacts: true, Engine: engine})
	if err != nil {
		t.Fatalf("open the fixture tier: %v", err)
	}
	if err := tier.Segments.StoreEvent(ctx, SegmentRecord{
		PartitionID: 1,
		WindowTS:    1,
		Seq:         1,
		EventKey:    "settled-event",
		Body:        []byte(`{"settled":1}`),
	}); err != nil {
		t.Fatalf("store the settled event: %v", err)
	}
	return ctx, tier, engine, root
}

// TestRetirementSettlesOnlyTheKeysItsSnapshotRead pins the pass settling the keys it
// read and nothing else: a key a writer filed after that snapshot is kept in the hot
// journal, because the family that reads it at boot scans the hot journal alone. The
// negative is stated beside it — the late key is absent from the cold journal — so a
// green anchor is a claim about the pass, not about a key nothing tried to move.
func TestRetirementSettlesOnlyTheKeysItsSnapshotRead(t *testing.T) {
	ctx, tier, engine, root := lateRetirementFixture(t)
	report, err := tier.Segments.RetireFacts(ctx, map[event.EventID]struct{}{})
	if err != nil {
		t.Fatalf("retire the settled facts: %v", err)
	}
	if report.Settled != 3 {
		t.Fatalf("settled keys = %d, want the event the snapshot judged with its index entry and window meta", report.Settled)
	}
	pairs, err := tier.Store.Read(ctx, Fetch{Key: engine.late})
	if err != nil {
		t.Fatalf("read the late key: %v", err)
	}
	if len(pairs) != 1 || string(pairs[0].Value) != `{"late":1}` {
		t.Fatalf("late key after the pass = %+v, want the bytes the writer filed kept hot", pairs)
	}
	cold, err := openJournalLog(filepath.Join(root, coldDirName, kvDirName))
	if err != nil {
		t.Fatalf("open the cold journal: %v", err)
	}
	defer cold.close()
	if _, held, err := cold.read(engine.late); err != nil || held {
		t.Fatalf("cold journal holds the late key (held=%v err=%v), want the pass to leave what it never read", held, err)
	}
}

// TestTaskAdmissionMaterialStaysHotAcrossARetirementPass pins the read a boot owes a
// task record: the admission material of a durable task is in the hot journal after
// a pass settled the events beside it, and a store opened afterwards reads it there.
// The negative is the settled event, which the same pass moved: a hot journal that
// kept everything would answer both.
func TestTaskAdmissionMaterialStaysHotAcrossARetirementPass(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	tier, err := OpenTier(ctx, StorageConfig{Type: StorageJournal, Root: root, RetireFacts: true})
	if err != nil {
		t.Fatalf("open the fixture tier: %v", err)
	}
	if err := tier.Segments.StoreEvent(ctx, SegmentRecord{
		PartitionID: 1,
		WindowTS:    1,
		Seq:         1,
		EventKey:    "settled-event",
		Body:        []byte(`{"settled":1}`),
	}); err != nil {
		t.Fatalf("store the settled event: %v", err)
	}
	if err := tier.Store.Commit(ctx, Batch{Puts: []Pair{{
		Key: "tagent/task/v1/spec:task-hot", Value: []byte(`{"spec":1}`),
	}}}); err != nil {
		t.Fatalf("commit the admission material: %v", err)
	}
	if _, err := tier.Segments.RetireFacts(ctx, map[event.EventID]struct{}{}); err != nil {
		t.Fatalf("retire the settled facts: %v", err)
	}
	reopened, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("reopen the journal: %v", err)
	}
	pairs, err := reopened.Read(ctx, Fetch{Prefix: "tagent/task/v1/spec:"})
	if err != nil {
		t.Fatalf("read the admission material: %v", err)
	}
	if len(pairs) != 1 || pairs[0].Key != "tagent/task/v1/spec:task-hot" {
		t.Fatalf("admission material after the pass = %+v, want the record a boot scans kept hot", pairs)
	}
	if events, err := reopened.Read(ctx, Fetch{Prefix: "1:idx:"}); err != nil {
		t.Fatalf("read the settled index: %v", err)
	} else if len(events) != 0 {
		t.Fatalf("settled index entries = %d, want the pass to have moved the event it judged", len(events))
	}
}

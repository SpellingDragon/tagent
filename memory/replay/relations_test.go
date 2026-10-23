// 契约: docs/wiki/memory/memory-architecture.md#causal-chain
//
// The relation ledger's durable record: a committed link replays into the chain
// a later open serves, and a link whose barrier fails never reaches the chain.
package replay

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// failingJournal is a journal whose barrier never completes, so every link a
// ledger over it commits is refused.
type failingJournal struct {
	err error
}

func (j failingJournal) Append(ledger.RelationRecord) error { return j.err }

func (j failingJournal) Records(context.Context) ([]ledger.RelationRecord, error) {
	return nil, nil
}

func TestRelationsJournalReplaysTheCommittedChain(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	relations, err := OpenMemoryRelations(root)
	if err != nil {
		t.Fatalf("OpenMemoryRelations: %v", err)
	}
	commits := []struct {
		child  event.EventID
		parent event.EventID
	}{
		{child: "child-a", parent: "parent-1"},
		{child: "child-b", parent: "parent-1"},
		{child: "child-b", parent: "parent-2"},
		{child: "child-c", parent: "parent-3"},
	}
	for _, commit := range commits {
		if err := relations.SetParent(commit.child, commit.parent); err != nil {
			t.Fatalf("SetParent(%q, %q): %v", commit.child, commit.parent, err)
		}
	}
	if err := relations.SetParent("child-c", ""); err != nil {
		t.Fatalf("clear child-c: %v", err)
	}
	want := map[event.EventID]event.EventID{
		"child-a": "parent-1",
		"child-b": "parent-2",
	}
	if got := relations.Links(); !maps.Equal(got, want) {
		t.Fatalf("held links = %v, want %v", got, want)
	}
	if err := relations.VerifyReplay(ctx); err != nil {
		t.Fatalf("VerifyReplay over the committed chain: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, ledger.RelationsJournalName))
	if err != nil {
		t.Fatalf("stat the journal: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("the journal holds no record after a committed link")
	}
	reopened, err := OpenMemoryRelations(root)
	if err != nil {
		t.Fatalf("reopen the relation ledger: %v", err)
	}
	if got := reopened.Links(); !maps.Equal(got, want) {
		t.Fatalf("replayed links = %v, want %v", got, want)
	}
	if err := reopened.VerifyReplay(ctx); err != nil {
		t.Fatalf("VerifyReplay over the replayed chain: %v", err)
	}
	if parent, err := reopened.GetParent("child-b"); err != nil || parent != "parent-2" {
		t.Fatalf("replayed parent of child-b = (%q, %v), want parent-2", parent, err)
	}
}

func TestRelationsJournalRefusesALinkItCouldNotCommit(t *testing.T) {
	refused := errors.New("barrier refused")
	relations := &MemoryRelations{
		parents: map[event.EventID]event.EventID{"child": "parent-old"},
		journal: failingJournal{err: refused},
	}
	if err := relations.SetParent("child", "parent-new"); !errors.Is(err, refused) {
		t.Fatalf("SetParent over a refused barrier = %v, want the journal error", err)
	}
	if parent, err := relations.GetParent("child"); err != nil || parent != "parent-old" {
		t.Fatalf("held parent after a refused append = (%q, %v), want the earlier link", parent, err)
	}
	if err := relations.SetParent("fresh", "parent-new"); !errors.Is(err, refused) {
		t.Fatalf("SetParent of a fresh child over a refused barrier = %v, want the journal error", err)
	}
	if links := relations.Links(); len(links) != 1 {
		t.Fatalf("held links after a refused append = %v, want only the earlier link", links)
	}
}

func TestRelationsJournalReadsBackOnlyCommittedRecords(t *testing.T) {
	root := t.TempDir()
	journal, err := ledger.OpenRelationsJournal(root)
	if err != nil {
		t.Fatalf("OpenRelationsJournal: %v", err)
	}
	for _, record := range []ledger.RelationRecord{
		{Child: "child-a", Parent: "parent-1"},
		{Child: "child-b", Parent: "parent-2"},
		{Child: "child-b", Parent: ""},
	} {
		if err := journal.Append(record); err != nil {
			t.Fatalf("Append(%+v): %v", record, err)
		}
	}
	file, err := os.OpenFile(journal.Path(), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open the journal for a torn append: %v", err)
	}
	if _, err := file.WriteString(`{"child":"torn"`); err != nil {
		t.Fatalf("write the torn append: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close the torn append: %v", err)
	}
	reopened, err := ledger.OpenRelationsJournal(root)
	if err != nil {
		t.Fatalf("reopen the journal over a torn tail: %v", err)
	}
	if err := reopened.Append(ledger.RelationRecord{Child: "child-d", Parent: "parent-4"}); err != nil {
		t.Fatalf("append after a torn tail: %v", err)
	}
	relations, err := OpenMemoryRelations(root)
	if err != nil {
		t.Fatalf("OpenMemoryRelations over a torn tail: %v", err)
	}
	want := map[event.EventID]event.EventID{"child-a": "parent-1", "child-d": "parent-4"}
	if got := relations.Links(); !maps.Equal(got, want) {
		t.Fatalf("replayed links = %v, want only the committed records", got)
	}
}

func TestRelationsJournalRefusesCorruptRecord(t *testing.T) {
	root := t.TempDir()
	corrupt := []byte("{not a record}\n")
	if err := os.WriteFile(filepath.Join(root, ledger.RelationsJournalName), corrupt, 0o644); err != nil {
		t.Fatalf("write the corrupt journal: %v", err)
	}
	if _, err := OpenMemoryRelations(root); !errors.Is(err, ledger.ErrRelationsJournalCorrupt) {
		t.Fatalf("OpenMemoryRelations over a corrupt record = %v, want ErrRelationsJournalCorrupt", err)
	}
	if err := NewMemoryRelations().VerifyReplay(context.Background()); !errors.Is(err, ErrRelationsJournalUnwired) {
		t.Fatalf("VerifyReplay without a journal = %v, want ErrRelationsJournalUnwired", err)
	}
}

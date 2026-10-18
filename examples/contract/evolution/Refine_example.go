package evolutionexample

import (
	"context"
	"errors"
	"time"

	"github.com/SpellingDragon/tagent/evolution"
)

// controlledEdits is the canonical improvement ledger: it records the listed controlled
// files as one improvement, lists the ledger back and reverts one recorded improvement by
// its commit.
type controlledEdits struct {
	controlled []string
	records    []evolution.RefineRecord
}

// Register records the listed controlled files as one improvement.
func (e *controlledEdits) Register(_ context.Context, paths []string, note string) (evolution.RefineRecord, error) {
	for _, path := range paths {
		if !e.controls(path) {
			return evolution.RefineRecord{}, errors.New("evolution: the path is outside the controlled set: " + path)
		}
	}
	record := evolution.RefineRecord{
		Paths:       append([]string(nil), paths...),
		Note:        note,
		SHA:         "improvement-" + note,
		CommittedAt: time.Now(),
	}
	e.records = append(e.records, record)
	return record, nil
}

// Status lists the improvement ledger.
func (e *controlledEdits) Status(context.Context) ([]evolution.RefineRecord, error) {
	return append([]evolution.RefineRecord(nil), e.records...), nil
}

// Rollback reverts one recorded improvement, named by its full commit or an unambiguous
// prefix. A commit that carries no improvement record is refused.
func (e *controlledEdits) Rollback(_ context.Context, sha string) error {
	for index, record := range e.records {
		if record.SHA == sha {
			e.records = append(e.records[:index], e.records[index+1:]...)
			return nil
		}
	}
	return errors.New("evolution: no recorded improvement carries that commit: " + sha)
}

// controls reports whether one path belongs to the controlled set.
func (e *controlledEdits) controls(path string) bool {
	for _, controlled := range e.controlled {
		if controlled == path {
			return true
		}
	}
	return false
}

var _ evolution.Refine = (*controlledEdits)(nil)

// ExampleRefine registers one improvement over a controlled file, lists the ledger and
// reverts a commit the ledger does not record.
//
// 契约: docs/wiki/evolution/evolution-architecture.md#refine
func ExampleRefine() {
	ctx := context.Background()
	ledger := &controlledEdits{controlled: []string{"prompts/system.md"}}

	record, err := ledger.Register(ctx, []string{"prompts/system.md"}, "tighten-the-tone")
	if err != nil {
		return
	}
	listed, err := ledger.Status(ctx)
	if err != nil {
		return
	}
	if len(listed) != 1 {
		return
	}
	if err := ledger.Rollback(ctx, "commit-of-the-user"); err == nil {
		return
	}
	_ = record.SHA
}

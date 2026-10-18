package stateexample

import (
	"context"

	"github.com/SpellingDragon/tagent/state"
)

// publicationLog is the canonical publication record port: one self-contained record per
// publication, stored beside the task commit port and never inside a task commit record.
type publicationLog struct {
	committed []state.PublicationRecord
}

// CommitPublication stores one publication record.
func (l *publicationLog) CommitPublication(_ context.Context, record state.PublicationRecord) error {
	l.committed = append(l.committed, record)
	return nil
}

var _ state.PublicationRecorder = (*publicationLog)(nil)

// ExamplePublicationRecorder commits one publication record carrying every check disposition
// the publication recorded.
//
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func ExamplePublicationRecorder() {
	ctx := context.Background()
	log := &publicationLog{}

	record := state.PublicationRecord{
		Generation:  4,
		Fingerprint: "fingerprint-1",
		Result:      state.PublishApplied,
		Checks: []state.CheckAnswer{
			{Name: "structure", Verdict: state.CheckPass},
		},
	}
	if err := log.CommitPublication(ctx, record); err != nil {
		return
	}
	if len(log.committed) != 1 || log.committed[0].Result != state.PublishApplied {
		return
	}
	if len(log.committed[0].Checks) != 1 {
		return
	}
}

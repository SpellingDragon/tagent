package task

import (
	"context"

	"github.com/SpellingDragon/tagent/event"
)

// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
//
// MutationID identifies one task state mutation for idempotent commit. A repeated
// commit of the same mutation does not record a second fact, and a commit that
// reuses an identity with different content is rejected.
type MutationID string

// CommitRecord is the self-contained unit a commit port stores: the expected
// revision it applies to, the status change it carries, and the notifications
// the transition intends to send.
type CommitRecord struct {
	// MutationID identifies this mutation for idempotent commit.
	MutationID MutationID
	// TaskID is the task whose status this mutation changes.
	TaskID event.TaskID
	// ExpectedRevision is the revision the mutation expects to apply to.
	ExpectedRevision int64
	// Status is the status the task moves to.
	Status Status
	// Outbox lists the notifications the transition intends to send, each with a
	// stable identity, so a repeated delivery does not record a second fact.
	Outbox []Notice
}

// CommitResult is the outcome a commit port reports for one CommitRecord. The
// three outcomes are reported separately and are never merged into one value.
type CommitResult int

const (
	// CommitUncertain is the zero value and the conservative outcome: a commit
	// that was not observed is treated as unknown, never as stored. It is
	// surfaced and reconciled before any automatic re-run.
	CommitUncertain CommitResult = iota
	// CommitCommitted is a commit that is durably stored. Only a committed
	// record may have its status and notifications made visible.
	CommitCommitted
	// CommitNotCommitted is a commit known not to have been stored.
	CommitNotCommitted
)

// Commit is the port through which a task status transition is stored. A
// registry takes this port at the transition and submits the status change and
// its pending notifications as one record.
type Commit interface {
	// Commit stores one record and reports the commit outcome. A committed
	// record may be made visible; an uncertain record is reconciled before any
	// automatic re-run.
	Commit(ctx context.Context, record CommitRecord) (CommitResult, error)
}

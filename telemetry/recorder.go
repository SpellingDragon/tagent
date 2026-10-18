package telemetry

import (
	"context"

	"github.com/SpellingDragon/tagent/event"
)

// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
//
// Recorder is the bounded, sealable sink behind the observation bypass: it
// admits observation records without ever blocking the caller that produced the
// fact, and it reports what a writer confirmed once the run is sealed.
//
// The redline list (4 rules):
// MUST refuse a record that would cross the queue length or the byte budget, and count it in the loss ledger.
// MUST read the loss ledger from counters that stay readable while the queue is full.
// MUST NOT block the caller that produced the fact.
// MUST NOT report a run whose writer never confirmed a seal as complete.
type Recorder interface {
	// Record admits one committed fact for asynchronous delivery.
	//
	// A refusal at the queue bound or the byte budget is an error and
	// increments the loss ledger; it never blocks the caller.
	Record(ctx context.Context, evt event.Event) error

	// FlushAndWait drains the admitted records through the same admission
	// protocol Close uses, then returns the writer-confirmed seal.
	FlushAndWait(ctx context.Context) (SealResult, error)

	// Close stops admission and drains the admitted records, and returns once
	// the writer has finished.
	Close(ctx context.Context) error
}

// SealStatus names the recorded seal status of one recorder run.
type SealStatus string

// SealStatusSealed, SealStatusPartial and SealStatusUnknown name the recorded
// seal statuses: a run is sealed only when a real writer confirmed the seal; a
// run whose writer never confirmed one is partial, and a run whose seal evidence
// is missing is unknown rather than complete.
const (
	// SealStatusSealed marks a run the writer confirmed as sealed.
	SealStatusSealed SealStatus = "sealed"

	// SealStatusPartial marks a run that ended without a writer seal.
	SealStatusPartial SealStatus = "partial"

	// SealStatusUnknown marks a run whose seal evidence is missing.
	SealStatusUnknown SealStatus = "unknown"
)

// SealResult reports the outcome of sealing one recorder run: the status the
// writer confirmed, the reason a run is not sealed, and the loss ledger the run
// accumulated.
type SealResult struct {
	// Status is the recorded seal status of the run.
	Status SealStatus
	// Reason names why the run is partial or unknown; it is empty for a sealed
	// run.
	Reason string
	// Dropped counts records refused by the bound on the queue or the byte
	// budget.
	Dropped uint64
	// Errors counts records that failed to serialize, write or sync.
	Errors uint64
}

package memory

import (
	"context"

	"github.com/SpellingDragon/tagent/event"
)

// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
//
// Guard is the retention protection port: it holds and releases protection on
// originals, and reports whether a reference is still held, so a reclaimer
// consults it before any destructive reclamation rather than assuming that an
// absence of protection is safety.
//
// Protection is reference counted and owned by the shared resource, so a hold
// outlives any single agent that relies on it. Constraints:
//
//   - a hold is released only when the last holder releases it, and both operations are idempotent;
//   - the holder is the owner of the shared resource, not an individual agent, so closing one agent never releases a hold the shared store still needs;
//   - a released hold means the reference takes part in the retention policy again, not that it became unreadable;
//   - a storage tier whose recovery inventory or protection coordination is not ready reports that incomplete capability by name and performs no destructive reclamation, since a timeout or an absent capability is never a safe default.
type Guard interface {
	// Hold registers one protection hold on the original of ref and is
	// idempotent for the same holder.
	Hold(ctx context.Context, ref OriginalRef) error

	// Release drops one hold on the original of ref, and is idempotent once the
	// last holder has released it.
	Release(ctx context.Context, ref OriginalRef) error

	// Protected reports whether the original of ref is still held, so a
	// reclaimer asks before it reclaims anything.
	Protected(ctx context.Context, ref OriginalRef) (bool, error)
}

// RecallOutcome names the classification of one exact recall result.
type RecallOutcome int

const (
	// RecallUnclassified is the zero value: the outcome was not determined, so
	// it is never reported as a successful recall.
	RecallUnclassified RecallOutcome = iota
	// RecallFound means the original bytes were returned.
	RecallFound
	// RecallMissing means no original is stored under the key.
	RecallMissing
	// RecallExpired means the original is outside the retention window of its policy.
	RecallExpired
	// RecallFailed means the read itself failed and Err carries the cause.
	RecallFailed
)

// RecallResult is the outcome of exact recall for one key: the original bytes
// when they were found, and otherwise a named classification of why they were
// not, so a miss is reported instead of silently omitted.
type RecallResult struct {
	// Key is the identity that was recalled.
	Key event.EventID
	// Outcome is the named classification of this result.
	Outcome RecallOutcome
	// Body holds the returned original bytes, and is set only when Outcome is
	// RecallFound.
	Body []byte
	// Err carries the cause when Outcome is RecallFailed, and is nil otherwise.
	Err error
}

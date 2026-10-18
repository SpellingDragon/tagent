package memory

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/event"
)

// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
//
// Guard is the retention protection port: it holds and releases protection on
// originals, and reports whether a reference is still held, so a reclaimer
// consults it before any destructive reclamation rather than assuming that an
// absence of protection is safety.
//
// The redline list (4 rules):
// MUST keep a hold owned by the shared resource, so it outlives any single agent relying on it.
// MUST answer whether a reference is still held before any destructive reclamation.
// MUST NOT treat the absence of protection, a timeout or an absent capability as safety.
// MUST NOT treat a released hold as an unreadable reference.
type Guard interface {
	// Hold registers one protection hold on the original of ref for holder and
	// returns the lease its release must carry back. Holding again as the same
	// holder while the hold stands is idempotent: the hold counts once and the
	// retry gets the lease it already holds, while a hold made after that holder
	// released is a new hold with a fresh lease. A hold that names no holder is
	// refused by name with ErrHolderRequired, so its release could always be
	// recognized as the same hold.
	Hold(ctx context.Context, ref OriginalRef, holder HolderID) (ProtectionLease, error)

	// Release drops the hold of holder on the original of ref under the lease
	// that hold returned. Releasing a hold that has already been released is a
	// no-op, so a repeated release is not an error; a release whose lease is not
	// the current lease of the named hold is refused by name with ErrLeaseMismatch
	// and leaves every hold standing, so a late or foreign lease never releases
	// the protection another hold keeps. The original is unprotected only once
	// the last holder has released it.
	Release(ctx context.Context, ref OriginalRef, holder HolderID, lease ProtectionLease) error

	// Protected reports whether the original of ref is still held, so a
	// reclaimer asks before it reclaims anything.
	Protected(ctx context.Context, ref OriginalRef) (bool, error)
}

// HolderID is the stable identity of one protection holder: the recovery
// resource or unsettled obligation the hold protects, not the agent name and
// not the owner of the reference it protects. It is stable across retries, so a
// retried hold is recognized as the same hold and counts once, while two
// holders on one original hold and release independently of each other.
type HolderID string

// ProtectionLease is the opaque token one accepted hold returns and its release
// must carry back. A lease is fresh per hold: a retry of a hold that still
// stands gets the same token, while a hold made after an earlier release gets a
// new one, so a late release carrying the earlier token matches nothing and
// releases nothing. Callers treat a lease as opaque: they never construct one,
// and its encoding is not a contract.
type ProtectionLease string

// ErrLeaseMismatch reports a release whose lease is not the current lease of
// the hold it names: a superseded token from before a re-hold, or a token that
// belongs to another hold. The named hold stays held.
var ErrLeaseMismatch = errors.New("memory: release carries a lease that is not the hold's current lease")

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

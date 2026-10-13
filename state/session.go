package state

import (
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/task"
)

// 契约: docs/wiki/platform/org-hot-reload.md#close-drain
//
// Session is the state owner's site for one explicit (agent, session) address:
// the mailbox bookkeeping of the site and the reduction projection of each
// invocation it admitted.
type Session struct {
	// Address is the explicit address this site was established for.
	Address event.Address
	// Instance is the runtime instance that owns the site; a site rebuilt under
	// the same name belongs to a different instance.
	Instance event.InstanceID
	// Mailbox is the acceptance log of the site: one entry per accepted input,
	// in the monotonic order acceptance assigned.
	Mailbox []MailboxEntry
	// Consumed is the consumption position of the mailbox: the highest
	// acceptance sequence whose consumption has completed. A new input is
	// accepted without waiting for this position to advance.
	Consumed AcceptSeq
	// Invocations holds the reduction projection of every invocation the site
	// admitted.
	Invocations map[event.InvocationID]InvocationProjection
}

// AcceptSeq is the monotonic acceptance sequence of one accepted input. It is
// assigned at acceptance, and a later claim or consumption never rewrites it.
type AcceptSeq uint64

// ClaimState is the claim state of one accepted input, from pending through
// claimed to consumed.
type ClaimState int

const (
	// ClaimPending is the zero value: the input was accepted but no executor
	// has claimed it yet.
	ClaimPending ClaimState = iota
	// ClaimClaimed is an input one executor has claimed.
	ClaimClaimed
	// ClaimConsumed is an input whose consumption has completed.
	ClaimConsumed
)

// ConsumeReceipt is the third mailbox time point: the receipt recorded when one
// accepted input was fully consumed.
type ConsumeReceipt struct {
	// Completed reports whether the consumption of the input finished.
	Completed bool
}

// MailboxEntry is one accepted input in a session's mailbox. The three time
// points it carries — acceptance, claim and consumption — are recorded
// separately and never overwrite one another.
type MailboxEntry struct {
	// AcceptSeq is the monotonic acceptance sequence assigned at acceptance.
	AcceptSeq AcceptSeq
	// Claim is the claim state of the input.
	Claim ClaimState
	// Receipt is the consumption receipt, recorded when consumption completed.
	Receipt ConsumeReceipt
}

// InvocationProjection is the reduction a session holds for one invocation: the
// identity of the invocation and the notifications whose obligations are still
// pending for it.
//
// Pending is a projection and not a second source of truth: every entry is the
// notification identity projected from the task Manager's obligation ledger, so
// a notice is pending while — and only while — its obligation is still open in
// that ledger. Closing or retiring a site keeps an undelivered obligation in
// the ledger for a recoverable path, so the projection never drops it.
type InvocationProjection struct {
	// InvocationID identifies the invocation this projection belongs to.
	InvocationID event.InvocationID
	// Pending lists the notification identities whose obligations are still
	// open.
	Pending []task.Notice
}

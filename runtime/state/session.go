package state

import (
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// 契约: docs/wiki/platform/org-hot-reload.md#close-drain
//
// Session is the state owner's site for one explicit (agent, session) address:
// the mailbox bookkeeping of the site.
type Session struct {
	// Address is the explicit address this site was established for.
	Address event.Address
	// Instance is the runtime instance that owns the site; a site rebuilt under
	// the same name belongs to a different instance.
	Instance event.InstanceID
	// Mailbox is the acceptance log of the site: one entry per accepted input,
	// in the monotonic order acceptance assigned.
	Mailbox []MailboxEntry
	// Consumed is the consumption position of the mailbox: the longest
	// contiguous completed prefix of the acceptance sequences. Every sequence
	// at or below the position completed its consumption, and the position
	// never advances past an earlier sequence left unfinished: a completion
	// beside a hole is recorded on its own entry while the position stays
	// before the hole. The position is therefore safe to resume after and
	// never a watermark that discards the entries below it. A new input is
	// accepted without waiting for this position to advance.
	Consumed AcceptSeq
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
// separately and never overwrite one another. An entry is not an anonymous
// payload: beside the stable identity of the notification it accepted, it names
// the committed fact the input points at and the responsibility range the
// producing call belongs to, so a consumer traces the event, the invocation and
// the notification behind the entry back to their sources.
type MailboxEntry struct {
	// Notice is the stable identity of the notification this entry accepted.
	Notice task.Notice
	// EventID is the identity of the committed fact this input points at. It is
	// recorded beside the notice's own fact reference and never chosen against
	// it: the entry's EventID and its Notice.Event always name the same
	// committed fact.
	EventID event.EventID
	// InvocationID is the responsibility range the call that produced the
	// accepted notification belongs to. It is recorded from the call identity
	// the delivery carries, and an entry beside no call identity keeps it
	// empty and resolves its invocation through its notice instead.
	InvocationID event.InvocationID
	// AcceptSeq is the monotonic acceptance sequence assigned at acceptance.
	AcceptSeq AcceptSeq
	// Claim is the claim state of the input.
	Claim ClaimState
	// Receipt is the consumption receipt, recorded when consumption completed.
	Receipt ConsumeReceipt
}

package task

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
)

// 契约: docs/wiki/reliability/durable-delivery.md#envelope-states
//
// Notice is the stable identity of one notification a task intends to deliver. A
// repeated delivery of the same identity does not record a second fact, and a
// retry keeps the original identity.
type Notice struct {
	// TaskID is the task the notification belongs to.
	TaskID event.TaskID
	// Round is the execution round the notification belongs to; one round may
	// produce several notifications.
	Round int64
	// Kind is the notification category within the round.
	Kind NoticeKind
	// Seq is the stable sequence number of the notification within its kind, so
	// repeated transport of one notification collapses to a single delivery.
	Seq int64
	// Event is the identity of the already-committed fact this notification
	// points at. A consumer restores the input through that fact, while the
	// notification's own identity stays TaskID+Round+Kind+Seq and the fact
	// reference never replaces it.
	Event event.EventID
	// Text is the result material a terminal notification carries beside its
	// identity: the body the settled call produced, bounded by the settle that
	// carries it. It is not part of the notification identity — FactKey reads
	// TaskID+Round+Kind+Seq alone — so a retry carrying the same body keeps the
	// fact key of the first delivery.
	Text string
}

// NoticeKind identifies the category of a notification within an execution
// round.
//
// A round may produce an accepted, a progress and a settled notification, and a
// terminal round carries its polarity as a failed or a cancelled one; the
// category is part of each notification's own identity and a retry keeps it.
type NoticeKind string

const (
	// NoticeAccepted reports that the round admitted the input it carries.
	NoticeAccepted NoticeKind = "accepted"
	// NoticeProgress reports in-flight progress of a running round.
	NoticeProgress NoticeKind = "progress"
	// NoticeSettled reports the terminal outcome the round settled on.
	NoticeSettled NoticeKind = "settled"
	// NoticeFailed reports a round whose execution ended in failure.
	NoticeFailed NoticeKind = "failed"
	// NoticeCancelled reports a round cancelled by its caller.
	NoticeCancelled NoticeKind = "cancelled"
)

// Registry is the task table through which a task is registered, looked up,
// cancelled and resumed. A registry carries a status transition on the commit
// port, so a transition is stored before it becomes visible.
//
// The redline list (4 rules):
// MUST store a transition on the commit port before it becomes visible.
// MUST recognise a repeated mutation identity as the same mutation.
// MUST NOT store a mutation identity that carries different content.
// MUST NOT cancel other invocations of the same session when one task is cancelled.
type Registry interface {
	// Register admits one task declaration and returns the admission receipt.
	// Admission is not a result and does not clear the task's obligation.
	Register(ctx context.Context, spec Spec) (AdmissionReceipt, error)

	// Lookup returns the declaration and status of the task with the given
	// identity.
	Lookup(ctx context.Context, taskID event.TaskID) (Spec, Status, error)

	// Cancel requests cancellation of the named task only; it does not cancel
	// other invocations of the same session.
	Cancel(ctx context.Context, taskID event.TaskID) error

	// Resume feeds new input into an existing task and re-enters the task's
	// standard lifecycle under the same task identity.
	Resume(ctx context.Context, taskID event.TaskID, input event.Event) (AdmissionReceipt, error)
}

// Obligations classifies the obligations a manager still holds for a task. An
// obligation stays open from admission until it is explicitly cleared, so a
// successful admission keeps its pre-registered obligation open.
type Obligations struct {
	// Count is the number of obligations still open.
	Count int
	// Owner is the invocation that owns the open obligations.
	Owner event.InvocationID
	// Blocked lists the obligations that cannot proceed, each with its reason.
	Blocked []BlockedObligation
	// ExitReason is the reason an obligation ended without settlement; it is
	// empty while no obligation has exited.
	ExitReason string
	// Logical is the logical status the task is read under at the instant of the
	// report: the silence its recorded state has stood in, beside the status the
	// machine holds. It states which silence the task is in and moves nothing.
	Logical LogicalStatus
	// Open lists the obligations still open as the classified detail, one entry
	// per obligation with the class it waits in.
	Open []Obligation
}

// ObligationKind classifies one open obligation by the wait it is held under.
type ObligationKind string

const (
	// ObligationAwaitingTool is an obligation whose task has not settled: the
	// admission that opened it stays owed until an outcome is explicitly settled.
	ObligationAwaitingTool ObligationKind = "awaiting-tool"
	// ObligationAwaitingDelivery is an obligation whose committed notification has
	// no acceptance from its target yet.
	ObligationAwaitingDelivery ObligationKind = "awaiting-delivery"
	// ObligationHandedOff is an obligation whose notification moves to the target
	// instance an explicit handoff named.
	ObligationHandedOff ObligationKind = "handed-off"
)

// Obligation is one obligation a manager still holds: the class it waits in beside
// the task and the notification it points at.
type Obligation struct {
	// Kind is the class the obligation waits in.
	Kind ObligationKind
	// TaskID is the task the obligation belongs to.
	TaskID event.TaskID
	// Notice is the notification the obligation points at; its zero value marks an
	// obligation that points at the task itself.
	Notice Notice
}

// BlockedObligation is one obligation that cannot proceed, paired with the
// reason it is blocked.
type BlockedObligation struct {
	// Notice is the identity of the notification whose obligation is blocked.
	Notice Notice
	// Reason is the readable reason the obligation cannot proceed.
	Reason string
}

// HandoffTarget names where a task's future notifications are delivered after an
// explicit handoff: the receiving address and the target instance. The source
// instance and the target instance are kept apart.
type HandoffTarget struct {
	// Target is the address future notifications are delivered to.
	Target event.Address
	// TargetInstance is the instance that receives the future notifications.
	TargetInstance event.InstanceID
}

// Manager is the task admission and settlement face: it admits a task, reports
// the obligations it still holds, hands a task's future notifications to another
// target, and flushes notifications whose obligations are still open.
//
// The redline list (4 rules):
// MUST keep the obligation an admission opened until an outcome explicitly settles it.
// MUST report each outstanding obligation by kind, with the reason a blocked one cannot proceed.
// MUST NOT read a successful admission as a settled obligation.
// MUST NOT re-resolve the target a committed notification froze at its transition.
type Manager interface {
	// Admit accepts one task declaration and returns the admission receipt.
	Admit(ctx context.Context, spec Spec) (AdmissionReceipt, error)

	// Obligations reports the manager's outstanding obligations for a task,
	// classified by kind.
	Obligations(ctx context.Context, taskID event.TaskID) (Obligations, error)

	// Handoff moves a task's future notifications to the target address and
	// target instance, keeping the source and target instances distinct.
	Handoff(ctx context.Context, taskID event.TaskID, target HandoffTarget) error

	// Flush delivers the notifications of a task whose obligations are still
	// open.
	Flush(ctx context.Context, taskID event.TaskID) error
}

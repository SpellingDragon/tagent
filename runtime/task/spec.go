// 契约: docs/wiki/agent/task-lifecycle.md#spawn-dedup-origin
//
// Package task is the runtime's task admission and settlement layer. It declares
// the declaration a tool, sub-agent or result-delivery call is admitted under,
// the status a task moves through, and the ports through which a transition is
// committed and its notifications are registered.
//
// A task is the runtime's unit of responsibility and accounting. The admission,
// commit and scheduling behaviour runs behind those ports in this package: the
// registry owns the life of a task, the manager owns its obligations, and the
// commit carrier records the transitions.
package task

import "github.com/SpellingDragon/tagent/api/event"

// Spec is the declaration a task is admitted under. It names the native call the
// task answers, the identity a duplicate admission is resolved against, the
// addresses the task belongs to, and the execution shape the task runs in.
type Spec struct {
	// CallID is the native provider call identifier, kept byte-identical and
	// never regenerated.
	CallID string
	// DedupKey is the identity a duplicate admission is resolved against: a
	// repeated admission of the same key returns the existing task rather than a
	// copy.
	DedupKey DedupKey
	// InvocationID scopes the task to one responsibility range.
	InvocationID event.InvocationID
	// InstanceID is the runtime instance the task belongs to.
	InstanceID event.InstanceID
	// Description is the short human-readable summary carried into the task's
	// notifications.
	Description string
	// Source is the address the task was admitted from.
	Source event.Address
	// Target is the address the task's notifications are delivered to.
	Target event.Address
	// TriggerSource is the trigger-source stamp the admission carries, recorded
	// on the task_spawned material. A delegated call carries
	// event.LineageDelegation; an empty stamp records no source, so an
	// admission that names none behaves exactly as it did before the stamp
	// existed.
	TriggerSource string
	// Execution references the serialized-execution declaration the task runs
	// under.
	Execution Serialization
}

// DedupKey is the identity a duplicate admission is resolved against. The
// runtime composes it from the instance, invocation, decision and native call
// identifiers, because a native CallID is not globally unique; an empty key
// disables deduplication.
type DedupKey string

// Serialization names the serialized-execution declaration a task was admitted
// under. Serialized execution admits at most one effective model decision per
// session at a time and queues the rest; the declaration names the shape the
// queueing follows.
type Serialization string

// Status is the state a task moves through from admission to settlement. The
// zero value is StatusUnknown, so a task whose status was never set is read as
// unknown rather than as accepted.
//
// Every value here is a state a transition commits. In-flight progress is not
// one of them: a progress report is a Notification the chain carries, and
// ProgressReported derives that answer from the chain, so a reader asks for it
// instead of reading a status the machine would have to keep in step.
type Status int

const (
	// StatusUnknown is the zero value: the task's status is not determinable. It
	// is surfaced rather than downgraded to a settled outcome.
	StatusUnknown Status = iota
	// StatusAccepted is a task the registry admitted. Admission is not a result;
	// the task's outcome arrives later.
	StatusAccepted
	// StatusRunning is a task whose execution is under way.
	StatusRunning
	// statusProgressRetired is the ordinal the progress status held while it was
	// still storable. A progress report travels as a NoticeProgress beside the
	// fact that carries it, so a status value for it repeated information the
	// committed chain already holds and moved no task anywhere the chain could
	// not read back; its write authority is gone with it. Every transition that
	// used to name it moves the task to StatusRunning instead, and no transition
	// writes this ordinal.
	//
	// The ordinal stays reserved rather than closed, because a status is stored
	// as its ordinal: closing the gap would shift every later status by one, so
	// a chain written before this removal would replay its settled fact as a
	// failed one. replayedStatus reads the reserved ordinal back as
	// StatusRunning, and the progress read of that task still answers progress
	// from the notification the chain carries.
	statusProgressRetired
	// StatusSettled is a task whose terminal outcome has been committed.
	StatusSettled
	// StatusFailed is a task whose execution ended in failure.
	StatusFailed
	// StatusCancelled is a task cancelled by its caller; cancelling one
	// invocation does not cancel other invocations of the same session.
	StatusCancelled
	// StatusAwaitingApproval is a task whose call is parked in the approval wait:
	// the call is registered and executed nothing yet, and the task reports the
	// wait rather than a final answer. The wait is execution-phase information
	// and never occupies a decision slot: it consumes no DecisionID and the
	// decision-stream semantics are unchanged by it.
	StatusAwaitingApproval
)

// AdmissionKind classifies one admission receipt. A fresh admission adds an
// obligation; a dedup and a rejected admission add none.
type AdmissionKind int

const (
	// AdmissionFresh is the receipt of an admission that newly registered the
	// task: it added an obligation, so the admission keeps that obligation open
	// until it is explicitly settled.
	AdmissionFresh AdmissionKind = iota
	// AdmissionDedup is the receipt of a repeated admission of an existing
	// identity. It adds no new obligation, so only this admission's
	// pre-registration is withdrawn and the existing obligations are untouched.
	AdmissionDedup
	// AdmissionRejected is the receipt of an admission that was refused. Like a
	// dedup it adds no new obligation, so only this admission's pre-registration
	// is withdrawn and the existing obligations are untouched.
	AdmissionRejected
)

// AdmissionReceiptText is the whole model-visible content of one admission
// receipt: the minimal confirmation that the call was admitted. The task id,
// the state word and the settlement promise never travel to the model — an
// admission is the system's own confirmation, never speaking material.
const AdmissionReceiptText = "ok"

// Text reports the model-visible receipt: the minimal confirmation, never the
// task identity or any internal state. The answer still pairs with its call
// under the tool role and the call id. The receipt renders through String and
// MarshalJSON to the same minimal text, so every path that materializes a
// receipt for a model — the loop's typed dispatch and a tool framework's own
// serialization alike — carries the confirmation and never the internal
// identity.
func (AdmissionReceipt) Text() string {
	return AdmissionReceiptText
}

// String renders the receipt as its minimal model-visible text.
func (r AdmissionReceipt) String() string {
	return r.Text()
}

// MarshalJSON renders the receipt as its minimal model-visible text, so a
// framework that serializes tool results as JSON carries the confirmation and
// never the task identity.
func (r AdmissionReceipt) MarshalJSON() ([]byte, error) {
	return []byte(`"` + AdmissionReceiptText + `"`), nil
}

// AdmissionReceipt is the single receipt a registration wrapper returns when
// it admits one call. Admission is not a result, so the receipt reports only
// that the call was admitted; the task's outcome arrives later as an
// independent notification.
type AdmissionReceipt struct {
	// CallID is the native provider call identifier the receipt pairs with.
	CallID string
	// TaskID is the task the admitted call was registered as.
	TaskID event.TaskID
	// Accepted reports whether the call was admitted.
	Accepted bool
	// Kind classifies the receipt. A fresh admission keeps the obligation it
	// added open until it is settled; only a dedup or a rejected admission, which
	// add no new obligation, withdraw this admission's pre-registration.
	Kind AdmissionKind
}

package task

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/event"
)

// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
//
// errNotImplemented is the sentinel every skeleton stub returns; the phase 2
// implementation replaces each stub with real behaviour.
var errNotImplemented = errors.New("phase 2: not implemented")

// The declared task, commit and notice types and their fields.
var (
	_ = Spec{
		CallID:       "",
		DedupKey:     "",
		InvocationID: "",
		InstanceID:   "",
		Description:  "",
		Source:       event.Address{},
		Target:       event.Address{},
		Execution:    "",
	}

	_ DedupKey      = ""
	_ Serialization = ""

	_ = AdmissionReceipt{
		CallID:   "",
		TaskID:   "",
		Accepted: false,
		Kind:     AdmissionFresh,
	}

	_ = CommitRecord{
		MutationID:       "",
		TaskID:           "",
		ExpectedRevision: 0,
		Status:           StatusUnknown,
		Outbox:           []NoticeEnvelope{},
	}

	_ = NoticeEnvelope{
		Notice:         Notice{},
		Target:         event.Address{},
		Source:         event.Address{},
		TargetInstance: "",
	}

	_ = Notice{
		TaskID: "",
		Round:  0,
		Kind:   NoticeAccepted,
		Seq:    0,
		Event:  "",
	}

	_ = Hold{
		TaskID:         "",
		Reason:         "",
		ApprovalID:     "",
		ExpectedDigest: "",
	}

	_ = ApprovalAnswer{
		ApprovalID:     "",
		ExpectedDigest: "",
		Granted:        false,
		Reason:         "",
	}

	_ = Obligations{
		Count:      0,
		Owner:      "",
		Blocked:    []BlockedObligation{},
		ExitReason: "",
	}

	_ = BlockedObligation{
		Notice: Notice{},
		Reason: "",
	}

	_ = HandoffTarget{
		Target:         event.Address{},
		TargetInstance: "",
	}
)

// The status and commit-result vocabularies stay compile-time constants.
const (
	_ = StatusUnknown
	_ = StatusAccepted
	_ = StatusRunning
	_ = StatusProgress
	_ = StatusSettled
	_ = StatusFailed
	_ = StatusCancelled
	_ = StatusAwaitingApproval

	_ = CommitUncertain
	_ = CommitCommitted
	_ = CommitNotCommitted

	_ = NoticeAccepted
	_ = NoticeProgress
	_ = NoticeSettled
	_ = NoticeFailed
	_ = NoticeCancelled

	_ = AdmissionFresh
	_ = AdmissionDedup
	_ = AdmissionRejected
)

// skeletonCommit is a compile-time placeholder for the commit port. It carries
// the stub method that satisfies Commit and holds no logic.
type skeletonCommit struct{}

// Commit implements Commit.Commit as a phase 2 stub.
func (skeletonCommit) Commit(ctx context.Context, record CommitRecord) (CommitResult, error) {
	return CommitUncertain, errNotImplemented
}

// skeletonRegistry is a compile-time placeholder for the task registry. It
// carries the stub methods that satisfy Registry and holds no logic.
type skeletonRegistry struct{}

// Register implements Registry.Register as a phase 2 stub.
func (skeletonRegistry) Register(ctx context.Context, spec Spec) (AdmissionReceipt, error) {
	return AdmissionReceipt{}, errNotImplemented
}

// Lookup implements Registry.Lookup as a phase 2 stub.
func (skeletonRegistry) Lookup(ctx context.Context, taskID event.TaskID) (Spec, Status, error) {
	return Spec{}, StatusUnknown, errNotImplemented
}

// Cancel implements Registry.Cancel as a phase 2 stub.
func (skeletonRegistry) Cancel(ctx context.Context, taskID event.TaskID) error {
	return errNotImplemented
}

// Resume implements Registry.Resume as a phase 2 stub.
func (skeletonRegistry) Resume(ctx context.Context, taskID event.TaskID, input event.Event) (AdmissionReceipt, error) {
	return AdmissionReceipt{}, errNotImplemented
}

// skeletonManager is a compile-time placeholder for the task manager. It carries
// the stub methods that satisfy Manager and holds no logic.
type skeletonManager struct{}

// Admit implements Manager.Admit as a phase 2 stub.
func (skeletonManager) Admit(ctx context.Context, spec Spec) (AdmissionReceipt, error) {
	return AdmissionReceipt{}, errNotImplemented
}

// Obligations implements Manager.Obligations as a phase 2 stub.
func (skeletonManager) Obligations(ctx context.Context, taskID event.TaskID) (Obligations, error) {
	return Obligations{}, errNotImplemented
}

// Handoff implements Manager.Handoff as a phase 2 stub.
func (skeletonManager) Handoff(ctx context.Context, taskID event.TaskID, target HandoffTarget) error {
	return errNotImplemented
}

// Flush implements Manager.Flush as a phase 2 stub.
func (skeletonManager) Flush(ctx context.Context, taskID event.TaskID) error {
	return errNotImplemented
}

// The port interfaces are satisfied by their skeleton placeholders.
var (
	_ Commit   = (*skeletonCommit)(nil)
	_ Registry = (*skeletonRegistry)(nil)
	_ Manager  = (*skeletonManager)(nil)
)

// The port method signatures are pinned exactly.
var (
	_ func(context.Context, CommitRecord) (CommitResult, error)                  = skeletonCommit{}.Commit
	_ func(context.Context, Spec) (AdmissionReceipt, error)                      = skeletonRegistry{}.Register
	_ func(context.Context, event.TaskID) (Spec, Status, error)                  = skeletonRegistry{}.Lookup
	_ func(context.Context, event.TaskID) error                                  = skeletonRegistry{}.Cancel
	_ func(context.Context, event.TaskID, event.Event) (AdmissionReceipt, error) = skeletonRegistry{}.Resume
	_ func(context.Context, Spec) (AdmissionReceipt, error)                      = skeletonManager{}.Admit
	_ func(context.Context, event.TaskID) (Obligations, error)                   = skeletonManager{}.Obligations
	_ func(context.Context, event.TaskID, HandoffTarget) error                   = skeletonManager{}.Handoff
	_ func(context.Context, event.TaskID) error                                  = skeletonManager{}.Flush
)

// The durable restore face keeps its documented shapes: the self-contained
// restore link beside its codec, the owner-binding port and the named
// refusals of a restore.
var (
	_ = TaskRecord{
		Spec: Spec{},
		Fact: CommitRecord{},
	}

	_ = RestoreOutcome{
		TaskID:    "",
		Status:    StatusUnknown,
		Owner:     "",
		Recovered: false,
		Refused:   nil,
	}

	_ = RestoreReport{
		Tasks: []RestoreOutcome{},
	}

	_ error = ErrTargetRemoved
	_ error = ErrNoOwnerBinding
	_ error = ErrChainBroken

	_ OwnerBinding = (OwnerBinding)(nil)

	_ func(OwnerBinding, context.Context, event.AgentID, event.AgentID) (event.AgentID, error) = OwnerBinding.OwnerOf

	_ func(TaskRecord) ([]byte, error) = EncodeTaskRecord
	_ func([]byte) (TaskRecord, error) = DecodeTaskRecord

	_ func(*MemoryRegistry, context.Context, []TaskRecord, OwnerBinding) (RestoreReport, error) = (*MemoryRegistry).Restore
)

// The handoff, retry and instance-close faces keep their documented shapes: the
// recorded handoff link, the stable notice sequencer, the frozen-envelope
// deliverer beside its manager option, and the named refusals and retained
// material of a closed instance.
var (
	_ = HandoffRecord{
		TaskID:         "",
		PreviousTarget: event.Address{},
		SourceInstance: "",
		Target:         event.Address{},
		TargetInstance: "",
	}

	_ EnvelopeDeliverer = (EnvelopeDeliverer)(nil)
	_ ManagerOption     = (ManagerOption)(nil)

	_ error = ErrNoHandoff
	_ error = ErrInstanceClosed

	_ func(*NoticeSequencer, event.TaskID, int64, NoticeKind, event.EventID) Notice = (*NoticeSequencer).Issue
	_ func(*NoticeSequencer, Notice)                                                = (*NoticeSequencer).Observe

	_ func(*MemoryRegistry, context.Context, event.TaskID, NoticeKind, event.EventID) (NoticeEnvelope, error) = (*MemoryRegistry).IssueNotice

	_ func(*MemoryManager, context.Context, event.TaskID) (HandoffRecord, error)        = (*MemoryManager).HandoffOf
	_ func(*MemoryManager, context.Context, event.InstanceID) error                     = (*MemoryManager).CloseInstance
	_ func(*MemoryManager, context.Context, event.InstanceID) ([]NoticeEnvelope, error) = (*MemoryManager).Retained
	_ func(*MemoryManager, context.Context, event.InstanceID) ([]TaskRecord, error)     = (*MemoryManager).RecoveryMaterial

	_ func(EnvelopeDeliverer) ManagerOption = WithEnvelopeDeliverer
)
